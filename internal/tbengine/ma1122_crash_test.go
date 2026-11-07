package tbengine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// runCrashingAt runs p through the engine with a crash (a panic, as a power cut would
// stop the process) just before its k-th file operation; it reports whether the crash
// came. The run's deferred releases unwind with it, as the OS releases a dead process's
// locks.
func runCrashingAt(t *testing.T, p *Plan, k int) (crashed bool) {
	t.Helper()
	n := 0
	withFS(t, func(op, name string) error {
		if n++; n == k {
			panic(crash{})
		}
		return nil
	})
	defer withFS(t, nil)
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(crash); !ok {
				panic(r)
			}
			crashed = true
		}
	}()
	if _, err := Run(context.Background(), p, Start{}); err != nil {
		t.Fatalf("crash run %d: %v", k, err)
	}
	return false
}

// opsOfACleanRun are a clean run's file operations, in order.
func opsOfACleanRun(t *testing.T, out string) []string {
	t.Helper()
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	var ops []string
	withFS(t, func(op, name string) error { ops = append(ops, op+" "+name); return nil })
	defer withFS(t, nil)
	if !mustRun(t, p, Start{}).Done() {
		t.Fatal("the clean run did not finish")
	}
	return ops
}

func entriesOf(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// covers: MA-1122, MA-1123, MA-1127, R40, S68
// A crash at each step of a clean run, through the engine, then the next Start through
// the engine: every lock is free after the crash; the next plan says what Start will do
// (a partial folder "will be removed first"); and the next Start settles the crashed
// restore and restores again, leaving Local Folders as R40 says — Thunderbird's own
// folders byte for byte as they were, the new restore whole, and of the crashed one only
// what §8's table keeps: nothing when it was cleaned (no parent file, nothing unlisted);
// the finished folder when it had committed (case 2, "ended"); and an empty container
// when it could not be proven MailArchive's (case 1, no marker). No restore into the
// profile is left open in restores.log, and every lock is free at the end.
func TestACrashAtEachStepIsSettledByTheNextStart(t *testing.T) {
	out := plainArchive(t, true)
	ops := opsOfACleanRun(t, out)
	if len(ops) < 20 {
		t.Fatalf("a clean run's steps: %v", ops)
	}
	settledAs := map[string]int{}
	for k := 1; k <= len(ops); k++ {
		pr := newProfile(t, "")
		trash, _ := os.ReadFile(filepath.Join(pr.local, "Trash"))
		msf, _ := os.ReadFile(filepath.Join(pr.local, "Trash.msf"))
		p := plan(t, Request{Out: out, Profile: pr.choice()})
		if !runCrashingAt(t, p, k) {
			t.Fatalf("step %d (%s): no crash came", k, ops[k-1])
		}
		allFree(t, out, pr.dir, "after a crash at "+ops[k-1])

		next := plan(t, Request{Out: out, Profile: pr.choice()})
		res := mustRun(t, next, Start{})
		if !res.Done() {
			t.Fatalf("step %d (%s): the next Start did not restore: %s", k, ops[k-1], res.Words)
		}
		if len(res.Earlier) != 1 {
			t.Fatalf("step %d (%s): the next Start settles exactly the crashed restore: %+v", k, ops[k-1], res.Earlier)
		}
		s := res.Earlier[0]
		settledAs[s.Case+" "+s.Action]++
		old := filepath.Join(pr.local, p.Top)
		var want []string
		switch {
		case s.Case == "3b" && s.Action == tbrestore.EvCleaned:
			hadPrediction := false
			for _, l := range next.Lines {
				hadPrediction = hadPrediction || (l.Code == "EARLIER" && strings.Contains(l.Words, "will be removed first"))
			}
			assure.Reached(t, hadPrediction, fmt.Sprintf("step %d: the plan said the partial folder would be removed first", k))
			want = []string{"Trash", "Trash.msf", next.Top, next.Top + ".sbd"}
		case s.Case == "2" && s.Action == tbrestore.EvEnded:
			want = []string{"Trash", "Trash.msf", p.Top, p.Top + ".sbd", next.Top, next.Top + ".sbd"}
		case s.Case == "1" && s.Action == tbrestore.EvAbandoned:
			want = []string{"Trash", "Trash.msf", next.Top, next.Top + ".sbd"}
			if _, err := os.Lstat(old + ".sbd"); err == nil && p.Top != next.Top {
				want = append(want, p.Top+".sbd")
				for _, e := range entriesOf(t, old+".sbd") {
					if e != tbrestore.MarkerName {
						t.Fatalf("step %d: a container left as not provably MailArchive's holds only a torn marker, not %q", k, e)
					}
				}
			}
		default:
			t.Fatalf("step %d (%s): settled as case %s %q, which a clean crash never gives", k, ops[k-1], s.Case, s.Action)
		}
		sort.Strings(want)
		if got := entriesOf(t, pr.local); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("step %d (%s), case %s: Local Folders holds %v, want %v", k, ops[k-1], s.Case, got, want)
		}
		t2, _ := os.ReadFile(filepath.Join(pr.local, "Trash"))
		m2, _ := os.ReadFile(filepath.Join(pr.local, "Trash.msf"))
		assure.Reached(t, bytes.Equal(trash, t2) && bytes.Equal(msf, m2), fmt.Sprintf("step %d: Thunderbird's own folders untouched", k))
		ev := events(t, pr.dir)
		opened, closed := 0, 0
		for _, e := range ev {
			switch e {
			case tbrestore.EvStarted:
				opened++
			case tbrestore.EvEnded, tbrestore.EvCleaned, tbrestore.EvAbandoned:
				closed++
			}
		}
		assure.Reached(t, opened == 2 && closed == 2, fmt.Sprintf("step %d: both restores closed in restores.log: %v", k, ev))
		allFree(t, out, pr.dir, "after the next Start")
	}
	assure.Reached(t, settledAs["3b cleaned"] > 0 && settledAs["2 ended"] > 0 && settledAs["1 abandoned"] > 0, fmt.Sprintf("the crash points reach cleaned, committed and unproven states: %v", settledAs))
}
