package health

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

func reasonOf(rep Report, code string) string {
	for i, c := range rep.Codes {
		if c == code {
			return rep.Reasons[i]
		}
	}
	return ""
}

// covers: MA-391, R12, R18, S33
// The owner-facing surfaces name their own rebuild action, not a command line
// they do not have (g7 friction F3). The desktop's Status & health card says
// "Maintenance → Rebuild search index", the mailarchive-gui health view says
// "Rebuild the search index" (the choice it now offers), and neither prints
// SQLite's own words; the CLI keeps `mailarchive reindex -rebuild` and the cause.
// Covers the damaged, missing and behind index.
func TestOwnerSurfacesNameTheirOwnRebuild(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cases := map[string]func(in *Input) string{
		"index_unreadable": func(in *Input) string {
			in.HasIndex, in.Indexed, in.HasIndexFile = false, 0, true
			in.IndexErr = os.ErrInvalid
			return "index_unreadable"
		},
		"index_missing": func(in *Input) string {
			in.HasIndex, in.Indexed = false, 0
			return "index_missing"
		},
		"index_behind": func(in *Input) string {
			in.Indexed = 40
			return "index_behind"
		},
	}
	for name, mutate := range cases {
		cli := healthyInput(now)
		code := mutate(&cli)
		cliReason := assure.Reached(t, reasonOf(Assess(cli, now), code), name+" for the CLI")
		if !strings.Contains(cliReason, "mailarchive reindex -rebuild -out") {
			t.Fatalf("positive twin: the CLI's %s lost its command: %q", name, cliReason)
		}
		for surface, want := range map[string]string{"desktop": "Maintenance → Rebuild search index", "gui": "Rebuild the search index"} {
			in := healthyInput(now)
			mutate(&in)
			in.Surface = surface
			got := assure.Reached(t, reasonOf(Assess(in, now), code), name+" for "+surface)
			if !strings.Contains(got, want) || strings.Contains(got, "mailarchive reindex") || strings.Contains(got, "invalid argument") {
				t.Fatalf("%s on %s: want %q and no command or SQLite text, got %q", name, surface, want, got)
			}
		}
	}
}

// covers: MA-391, R12, R18, S33
// status's remedy for a search.db it cannot read must work (g7 friction F6):
// `reindex -rebuild` refuses an archive with no manifest, so with none in the
// folder the RED index_unreadable names moving search.db aside (the next run
// builds a fresh one), never the rebuild. Positive twin: with the manifest
// present it names the rebuild, as before.
func TestIndexUnreadableRemedyNeedsAManifest(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.HasIndex, in.Indexed, in.HasIndexFile = false, 0, true
	in.IndexErr = os.ErrInvalid

	rep := Assess(in, now)
	with := assure.Reached(t, reasonOf(rep, "index_unreadable"), "the index_unreadable reason with a manifest")
	if !strings.Contains(with, "reindex -rebuild -out") {
		t.Fatalf("positive twin: with a manifest the remedy is the rebuild: %q", with)
	}

	in.HasManifest, in.HasManifestFile, in.Messages = false, false, 0
	rep = Assess(in, now)
	got := assure.Reached(t, reasonOf(rep, "index_unreadable"), "the index_unreadable reason with no manifest")
	if strings.Contains(got, "reindex -rebuild") || !strings.Contains(got, "no manifest") || !strings.Contains(got, "move") || !strings.Contains(got, "/a/search.db") {
		t.Fatalf("no manifest to rebuild from: the remedy must be moving search.db aside, got %q", got)
	}
}

// covers: MA-115, R18, S29
// status -json does not claim what it does not know (g7 friction F8): while the
// index is busy or unreadable its count is unknown, so `indexed_known` is false
// beside the 0 (an additive key; the version stays), and last_run carries
// `in_progress`, true only while a run really holds the archive lock — a
// `running` record whose run is dead is not the README's "in progress" shape.
// Positive twin: a healthy archive reports indexed_known true.
func TestStatusJSONSaysWhatItDoesNotKnow(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	decode := func(in Input) map[string]any {
		t.Helper()
		b, err := json.Marshal(JSON(in, Assess(in, now)))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	in := healthyInput(now)
	if m := decode(in); m["indexed_known"] != true {
		t.Fatalf("positive twin: a healthy archive must report indexed_known true: %v", m["indexed_known"])
	}
	busy := healthyInput(now)
	busy.HasIndex, busy.Indexed, busy.HasIndexFile, busy.IndexErr, busy.IndexBusy = false, 0, true, os.ErrInvalid, true
	if m := decode(busy); m["indexed_known"] != false {
		t.Fatalf("a busy index's count is unknown, indexed_known = %v", m["indexed_known"])
	}

	dead := healthyInput(now)
	dead.LastRun = state.LastRun{Status: state.RunRunning, Started: now.Add(-time.Hour), PID: 99}
	if lr, _ := decode(dead)["last_run"].(map[string]any); lr == nil || lr["in_progress"] != false || lr["status"] != "running" {
		t.Fatalf("a dead run's record must say in_progress false: %v", lr)
	}
	live := dead
	live.LockHeld, live.LockVerb = true, "export"
	if lr, _ := decode(live)["last_run"].(map[string]any); lr == nil || lr["in_progress"] != true {
		t.Fatalf("a run holding the lock must say in_progress true: %v", lr)
	}
}

// covers: MA-75, R18, S29
// The posture lines print the worst first (g7 friction F20): REDs, then WARNs,
// then notes, each reason keeping its own remedy line right below it. A RED
// that Assess finds late (the last run's) is not preceded by a WARN found
// earlier (the verify that never finished), and an authentication failure's
// "remedy:" note stays under its RED. Parallel Reasons/Codes stay parallel.
func TestPostureListsRedBeforeWarn(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.LastRun = state.LastRun{Status: state.RunFailed, Started: now.Add(-3 * time.Hour), Finished: now.Add(-2 * time.Hour), Error: "401 unauthorized"}
	in.LastVerifyState = state.LastVerifyPresent
	in.LastVerify = state.LastVerify{Status: state.VerifyRunning, Started: now.Add(-time.Hour)}
	in.Fillable = 2
	rep := Assess(in, now)
	if len(rep.Reasons) != len(rep.Codes) {
		t.Fatalf("reasons and codes diverged: %d vs %d", len(rep.Reasons), len(rep.Codes))
	}
	if !hasCode(rep, "run_failed") || !hasCode(rep, "verify_never_finished") || !hasCode(rep, "incomplete_content") || !hasCode(rep, "auth_remedy") {
		t.Fatalf("fixture lacks the reasons: %v", rep.Codes)
	}
	pos := map[string]int{}
	for i, c := range rep.Codes {
		pos[c] = i
	}
	if !(pos["run_failed"] < pos["incomplete_content"] && pos["run_failed"] < pos["verify_never_finished"]) {
		t.Fatalf("a RED must print before the WARNs: %v", rep.Codes)
	}
	if pos["auth_remedy"] != pos["run_failed"]+1 {
		t.Fatalf("the remedy note must stay directly under its RED: %v", rep.Codes)
	}
	seenNonRed := false
	for _, r := range rep.Reasons {
		isRed := strings.HasPrefix(r, "RED:")
		if isRed && seenNonRed {
			t.Fatalf("a RED after a non-RED reason: %v", rep.Reasons)
		}
		if !isRed && !strings.HasPrefix(r, "  remedy") {
			seenNonRed = true
		}
	}
}

// covers: MA-545, R18, S29
// Last run is printed in local time and Last verify in UTC (g7 friction F9), so
// the same minute read as two dates. The Last run line now names its zone, and a
// line under the verify lines says they are UTC; the verify lines themselves
// keep their documented shape. Positive twin: a UTC machine reads "UTC" on both.
func TestSummaryLabelsItsTimeZones(t *testing.T) {
	orig := time.Local
	time.Local = time.FixedZone("EDT", -4*3600)
	t.Cleanup(func() { time.Local = orig })
	now := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	started := time.Date(2026, 10, 10, 0, 36, 0, 0, time.UTC)
	in.LastRun = state.LastRun{Status: state.RunOK, Started: started, Finished: started.Add(time.Minute), PID: 1}
	fin := started.Add(time.Minute)
	in.LastVerifyState = state.LastVerifyPresent
	in.LastVerify = state.LastVerify{Status: state.VerifyDone, Started: started, Finished: &fin, Attested: true}
	lines := Summary(in, Assess(in, now))
	var run, verify, note string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "Last run:"):
			run = l
		case strings.HasPrefix(l, "Last verify:"):
			verify = l
		case strings.Contains(l, "UTC") && strings.HasPrefix(l, "            ("):
			note = l
		}
	}
	assure.Reached(t, run, "the Last run line")
	if !strings.Contains(run, "2026-10-09 20:36 EDT") {
		t.Fatalf("the Last run line must carry its zone: %q", run)
	}
	if !strings.HasSuffix(verify, "→ attested") || !strings.Contains(verify, "2026-10-10 00:37") {
		t.Fatalf("the Last verify line changed shape: %q", verify)
	}
	assure.Reached(t, note, "the line that says the verify times are UTC")
}

// covers: MA-394, R18, R20, S31
// A verify that was interrupted says so (g7 friction F10): the WARN
// verify_never_finished names Ctrl-C beside crash, kill and power loss, and still
// names the re-run and the kept verdict.
func TestVerifyNeverFinishedNamesCtrlC(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.LastVerifyState = state.LastVerifyPresent
	in.LastVerify = state.LastVerify{Status: state.VerifyRunning, Started: now.Add(-time.Hour)}
	w := assure.Reached(t, reasonOf(Assess(in, now), "verify_never_finished"), "the verify_never_finished reason")
	for _, want := range []string{"Ctrl-C", "crash, kill, or power loss", "mailarchive verify -out", "still reported"} {
		if !strings.Contains(w, want) {
			t.Fatalf("verify_never_finished lacks %q: %s", want, w)
		}
	}
}

// covers: MA-398, R18, S29
// status reads the lock and the last-run record as one snapshot (g7 friction
// F14): Gather used to probe the lock, wait up to 5 s on a busy index, then read
// the record — and a run that failed during the wait printed "a run is using
// this archive" beside "Last run → failed". The record is read with the lock
// probe, before the wait, so the two never disagree about the run. Positive
// twin: with no wait the facts are the same ones.
func TestGatherReadsLockAndRecordTogether(t *testing.T) {
	out, err := os.MkdirTemp("", "healthsnap")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(out) })
	dbPath := filepath.Join(out, "search.db")
	ix, err := index.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now().UTC().Add(-time.Minute)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunRunning, Started: t0, PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	if pos := Gather(out, ""); !pos.LockHeld || pos.LastRun.Status != state.RunRunning {
		t.Fatalf("positive twin: a live run reads as held + running: held=%v status=%q", pos.LockHeld, pos.LastRun.Status)
	}

	holder, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	holder.SetMaxOpenConns(1)
	if _, err := holder.Exec(`PRAGMA locking_mode=EXCLUSIVE; BEGIN; UPDATE meta SET version = version; COMMIT;`); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { // the run fails while status waits on the index
		defer close(done)
		time.Sleep(700 * time.Millisecond)
		_ = state.WriteLastRun(out, state.LastRun{Status: state.RunFailed, Started: t0, Finished: time.Now().UTC(), Error: "boom", PID: os.Getpid()})
		lock.Release()
	}()
	got := Gather(out, "")
	<-done
	holder.Close()
	if !got.IndexBusy {
		t.Fatalf("fixture: the index was not busy (err %v)", got.IndexErr)
	}
	if got.LockHeld && got.LastRun.Status != state.RunRunning {
		t.Fatalf("one snapshot, two stories: lock held (a run is using this archive) beside the record %q", got.LastRun.Status)
	}
}
