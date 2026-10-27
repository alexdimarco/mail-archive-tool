package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// covers: MA-848, R37
// What status says follows the person's choice and the copies' state (the PG4 pass):
// copies switched off, the backup that removes them says so and later backups say
// nothing of copies (LEAK-1, COR-5); a run whose program step did not run keeps the
// line the last one left (LEAK-3); a copy kept online only and unchecked since an
// earlier backup is a lasting line (R4, COR-6).
func TestStatusFollowsTheChoiceAndTheCopies(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	code := func() string {
		lr, _, _ := state.ReadLastRun(out)
		return lr.Program
	}
	programLines := func() string {
		hr := health.Assess(health.Gather(out, ""), time.Now())
		var ls []string
		for _, r := range hr.Reasons {
			if strings.Contains(r, "MailArchive") && strings.Contains(r, "cop") {
				ls = append(ls, r)
			}
		}
		return strings.Join(ls, " | ")
	}
	backup(t, out, key, ProgramCopiesOn)
	// A run that ends before the step: the line stays.
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	Run(cancelled, Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key}, log.New(io.Discard, "", 0), nil)
	afterCancel := code()

	// Online only, unchecked since an earlier backup.
	_, self := selfPlatformAndName()
	orig := programPlaceholder
	programPlaceholder = func(p string) bool { return filepath.Base(p) == self }
	backup(t, out, key, ProgramCopiesUnset)
	firstUnchecked := code()
	time.Sleep(1100 * time.Millisecond) // the next run starts a second later
	backup(t, out, key, ProgramCopiesUnset)
	unchecked := code()
	programPlaceholder = orig

	if err := SealProgramCopies(context.Background(), Options{Out: out, Key: key}, ProgramCopiesOff, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	backup(t, out, key, ProgramCopiesUnset)
	removed, removedLines := code(), programLines()
	backup(t, out, key, ProgramCopiesUnset)
	off, offLines := code(), programLines()
	assure.Reached(t, afterCancel == state.ProgramUpdated && firstUnchecked != state.ProgramUnchecked && unchecked == state.ProgramUnchecked &&
		removed == state.ProgramRemoved && strings.Contains(removedLines, "removed from this archive, as chosen") &&
		off == "" && offLines == "",
		strings.Join([]string{afterCancel, firstUnchecked, unchecked, removed, removedLines, off, offLines}, " ; "))
}

// covers: MA-847, R37
// status says, without the key, what the last backup's program step did (program
// design §4, §8; PG4): the outcome from the record's closed list — a note when the
// copies are as they should be, a warning when they are incomplete, a note when a newer
// MailArchive keeps them — and the record beside the archive holds nothing more than
// that code.
func TestStatusSaysWhatTheProgramStepDid(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	// told is status's line for code: "WARN: <text>" as a warning, the text alone as a note.
	told := func(code string) string {
		hr := health.Assess(health.Gather(out, ""), time.Now())
		for _, r := range hr.Reasons {
			if strings.HasSuffix(r, state.ProgramText(code)) {
				return r
			}
		}
		return ""
	}
	updated := told(state.ProgramUpdated)
	_, name := selfPlatformAndName()
	p := filepath.Join(out, container.ProgramDir, name)
	os.Remove(p)
	os.Mkdir(p, 0o755) // something in its place: incomplete
	backup(t, out, key, ProgramCopiesUnset)
	incomplete := told(state.ProgramIncomplete)
	raw, _ := os.ReadFile(filepath.Join(out, state.LastRunName))

	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	s.SetProgramRawForTest([]byte(`{"format":2,"keep":true}`)) // a newer MailArchive's set
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	backup(t, out, key, ProgramCopiesUnset)
	unread := told(state.ProgramUnread)
	assure.Reached(t, updated == state.ProgramText(state.ProgramUpdated) && incomplete == "WARN: "+state.ProgramText(state.ProgramIncomplete) &&
		unread == state.ProgramText(state.ProgramUnread) &&
		strings.Contains(string(raw), state.ProgramIncomplete) && !strings.Contains(string(raw), name),
		fmt.Sprintf("status: %q, %q, %q", updated, incomplete, unread))
}
