//go:build darwin || linux

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/vault"
)

// lostBatchChildEnv marks the re-executed test binary that runs the capped run of
// TestAnIndexBatchLostAtItsCommitMarksTheSealedIndexBehind (its value: the archive).
const lostBatchChildEnv = "MAILARCHIVE_TEST_LOSTBATCH_CHILD"

// lostBatchOutcome is what the capped child reports back.
type lostBatchOutcome struct {
	Err         string
	IndexErrors int
	AddFailed   bool
}

// bigMessage is an mbox message whose body grows an index past a megabyte.
func bigMessage(subject, id string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSuffix(indexMbox(subject, id), "\n"))
	for i := 0; i < 60000; i++ {
		fmt.Fprintf(&b, "tok%d ", i)
		if i%12 == 11 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n\n")
	return b.String()
}

// lostBatchChild runs in the child process: after the run's first add has loaded the
// scratch index (over a megabyte) and added its row, writes past 300 KB are refused
// (RLIMIT_FSIZE; the Go runtime ignores SIGXFSZ, so the writes fail with EFBIG) — a
// cache drive that filled during the run. The add itself succeeded; the batch's
// commit cannot write. Every archive file the run writes is smaller than the limit.
func lostBatchChild(t *testing.T, out string) {
	forceScratchIndex(t)
	o, err := vault.Open(out, vault.ID{}, vault.PasswordSecret("encrypted run test passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	add := indexAdd
	var report lostBatchOutcome
	limited := false
	indexAdd = func(ix *index.Index, s string, f []string, m *model.Message, rel, k string) error {
		err := add(ix, s, f, m, rel, k)
		report.AddFailed = report.AddFailed || err != nil
		if !limited {
			limited = true
			capped := syscall.Rlimit{Cur: 300 << 10, Max: 300 << 10}
			if serr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &capped); serr != nil {
				t.Skipf("cannot lower RLIMIT_FSIZE: %v", serr)
			}
		}
		return err
	}
	res, err := Run(context.Background(), Options{Inputs: []string{os.Getenv(lostBatchChildEnv + "_SRC")}, Out: out, Key: o.Key, Index: true}, discard(), nil)
	if err != nil {
		report.Err = err.Error()
	}
	report.IndexErrors = res.IndexErrors
	b, _ := json.Marshal(report)
	os.Stdout.Write(append(b, '\n'))
}

// covers: MA-761, R37
// Rows whose adds succeeded but whose batch is lost when it is written — the scratch
// drive full by then — mark the sealed index behind in the same commit, and the index
// that failed part-way through writing is not sealed: the record keeps the last sound
// image, and the next run catches it up whole. Never sealed as a whole index with the
// rows left out for good.
func TestAnIndexBatchLostAtItsCommitMarksTheSealedIndexBehind(t *testing.T) {
	if out := os.Getenv(lostBatchChildEnv); out != "" {
		lostBatchChild(t, out)
		return
	}
	forceScratchIndex(t)
	store := filepath.Join(tmpDir(t), "store")
	mbox := filepath.Join(store, "Inbox")
	p14Write(t, mbox, bigMessage("first", "one"))
	out, key := encryptedOut(t)
	opts := Options{Inputs: []string{store}, Out: out, Key: key, Index: true}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireWholeIndex(t, out, key)
	p14Write(t, mbox, bigMessage("first", "one")+indexMbox("second", "two"))

	cmd := exec.Command(os.Args[0], "-test.run=^TestAnIndexBatchLostAtItsCommitMarksTheSealedIndexBehind$", "-test.count=1")
	cmd.Env = append(os.Environ(), lostBatchChildEnv+"="+out, lostBatchChildEnv+"_SRC="+store)
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("the capped child failed: %v\n%s", err, stdout)
	}
	var o lostBatchOutcome
	for _, line := range strings.Split(string(stdout), "\n") {
		if strings.HasPrefix(line, "{") {
			if err := json.Unmarshal([]byte(line), &o); err != nil {
				t.Fatal(err)
			}
		}
	}
	if o.Err != "" || o.AddFailed || o.IndexErrors == 0 {
		t.Fatalf("fixture: want a run that succeeds, every add accepted and the batch lost at its commit, got %+v\n%s", o, stdout)
	}
	m, s, idx := sealedIndexOf(t, out, key)
	if n, _ := idx.Count(); n == m.Len() || !s.IndexBehind() {
		t.Fatalf("a lost batch left the index at %d of %d rows and must mark it behind (%v)", n, m.Len(), s.IndexBehind())
	}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireWholeIndex(t, out, key)
}
