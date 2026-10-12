package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// The tombstones of FI-S2b's adversarial pass (docs/review-fault-isolation-s2b-adversarial.md).

// covers: MA-321, R1, R10, S46
// S2B-AO2 (and S2B-AI5): a healthy file never closes the gap of an unreadable one. A
// maildir file's ledger locator is its folder and the unique part of its name (the part
// a client keeps when it flags the file), so two files can share one: new/abc and
// cur/abc:2,S, or two names the ledger spells alike after it strips control characters.
// The healthy sibling's capture used to close the unreadable file's gap in the same
// run: the run exited 0, said "Recovered" and the message was nowhere. The ledger's own
// spelling is now the key the reader builds (state.BoundLocator, as Graph's), and a gap
// this run observed is closed by a capture only when it says the file vanished. The
// positive twin follows: after the permissions are fixed the file is archived and its
// gap closes as recovered.
func TestAHealthySiblingDoesNotCloseAnUnreadableFilesGap(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a unix permission check that root and Windows do not make")
	}
	for _, c := range []struct{ name, bad, good string }{
		{"the same unique part, new before cur", "Inbox/new/abc", "Inbox/cur/abc:2,S"},
		{"the same unique part, cur before new", "Inbox/cur/abc:2,S", "Inbox/new/abc"},
		{"a control character the ledger strips", "Inbox/cur/x\x01y:2,S", "Inbox/cur/xy:2,S"},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := filepath.Join(tmpDir(t), "mail")
			p14Write(t, filepath.Join(store, filepath.FromSlash(c.bad)), strings.ReplaceAll(p14Plain("LOSTSUBJECT-ONE", "lost-one"), "\r\n", "\n"))
			p14Write(t, filepath.Join(store, filepath.FromSlash(c.good)), strings.ReplaceAll(p14Plain("HEALTHYSUBJECT-TWO", "healthy-two"), "\r\n", "\n"))
			bad := filepath.Join(store, filepath.FromSlash(c.bad))
			if err := os.Chmod(bad, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(bad, 0o644) })

			out := tmpDir(t)
			res, err := runLocal(t, out, store)
			if !errors.Is(err, ErrPartial) || !res.Partial {
				t.Errorf("run 1 err = %v, partial %v; want ErrPartial: the unreadable file is still not captured", err, res.Partial)
			}
			gaps := ledgerOf(t, out)
			if len(gaps) != 1 || gaps[0].Reason != "file-unreadable" || gaps[0].Class != state.ClassFillable {
				t.Errorf("ledger %s; want the unreadable file's one fillable file-unreadable gap", gapReasons(gaps))
			}
			if res.Gaps.Recovered != 0 {
				t.Errorf("recovered %d; want 0: nothing was recovered, the healthy file is another message", res.Gaps.Recovered)
			}
			text := archiveText(t, out)
			if bytes.Contains(text, []byte("LOSTSUBJECT-ONE")) || !bytes.Contains(text, []byte("HEALTHYSUBJECT-TWO")) {
				t.Errorf("the archive holds the unreadable message or lacks the healthy one")
			}

			// The remedy followed: the file is readable, so its capture closes its own gap.
			if err := os.Chmod(bad, 0o600); err != nil {
				t.Fatal(err)
			}
			ageLedger(t, out, 48*time.Hour)
			res, err = runLocal(t, out, store)
			if err != nil || res.Partial || len(ledgerOf(t, out)) != 0 || res.Gaps.Recovered != 1 {
				t.Errorf("run 2: err %v, partial %v, ledger %s, recovered %d; want the file archived and its gap closed as recovered", err, res.Partial, gapReasons(ledgerOf(t, out)), res.Gaps.Recovered)
			}
			if !bytes.Contains(archiveText(t, out), []byte("LOSTSUBJECT-ONE")) {
				t.Errorf("the fixed file was not archived")
			}
		})
	}
}

// covers: MA-322, R25, R10, S47
// S2B-AO4 (and S2B-AC1) end to end: a pipe named like an Outlook data file, in an input
// folder or given as the input with -copy-first (whose snapshot opened it first), must
// not hold the run, and the archive lock with it. The run returns within a guard, with
// the pipe's refusal as the file's own failure.
func TestAPipeAsAnInputFileDoesNotHoldTheRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs need privileges on Windows")
	}
	for _, c := range []struct {
		name      string
		pipe      string // where the pipe is
		input     string // what is given as the input ("" = the folder holding the pipe)
		copyFirst bool
	}{
		{"a .pst pipe in an input folder", "evil.pst", "", false},
		{"an .ost pipe in an input folder", "evil.ost", "", true},
		{"a pipe given as the input with -copy-first", "box.mbox", "box.mbox", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := tmpDir(t)
			pipe := filepath.Join(dir, c.pipe)
			if err := mkfifo(pipe); err != nil {
				t.Skip("no FIFOs here:", err)
			}
			t.Cleanup(func() {
				if f, err := os.OpenFile(pipe, os.O_RDWR, 0); err == nil {
					f.Close()
				}
			})
			input := dir
			if c.input != "" {
				input = filepath.Join(dir, c.input)
			}
			done := make(chan error, 1)
			go func() {
				_, err := Run(context.Background(), Options{Inputs: []string{input}, Out: tmpDir(t), Mode: export.Incremental, CopyFirst: c.copyFirst}, discard(), nil)
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Errorf("run err = nil; want the pipe refused as a file that failed")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the run waited on a pipe: it holds the archive lock until a writer appears")
			}
		})
	}
}

// covers: MA-318, R1
// S2B-AI2 (escalated, owner decision; see the adversarial report): an older binary's save
// drops a stub's parser generation and the digest of its bytes, and the stub is then
// settled by nothing. The documented recovery (the next run reads it as generation 0 and
// re-examines it once) runs only when a message arrives at the stub's key; a message that
// now parses lands on its own key, and without the digest the supersede index cannot
// find the stub, so it stays "unparsed" in the status and the report for good though its
// message has a healthy page. A fix needs a way to recover the digest (from the preserved
// .eml, which exists only under -raw, or by re-reading the source for each digest-less
// stub), which changes what a run reads: the owner's call.
func TestAStubAnOlderBinarySavedIsSettledByAHealthyRead(t *testing.T) {
	t.Skip("R1 (S2B-AI2): a stub whose digest an older binary's save dropped is never superseded by a healthy read of its bytes; recovering the digest changes what a run reads (the .eml under -raw, or the source again): the owner's decision; closes when it is made")
	restore := panicOn(t, "POISON")
	store := filepath.Join(tmpDir(t), "tb")
	p14Write(t, filepath.Join(store, "Inbox"), "From a@example.com Mon Mar  3 09:00:00 2025\n"+
		strings.ReplaceAll(p14Mail("POISON subject", "poison", "", "poison body"), "\r\n", "\n")+"\n")
	out := tmpDir(t)
	if _, err := runLocal(t, out, store); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	key, rec := stubRecord(t, out)
	m := loadArchive(t, out)
	rec.ParserGen, rec.RawDigest = 0, "" // what an older binary's save keeps of the stub
	m.Add(key, rec)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	restore() // the parser now reads the message
	res, err := runLocal(t, out, store)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if got, _ := loadArchive(t, out).Get(key); res.Unparsed != 0 || len(got.Terminal) != 1 || got.Terminal[0] != state.LabelSuperseded {
		t.Errorf("after a healthy read of the stub's bytes: unparsed %d, the stub = %+v; want it superseded", res.Unparsed, got)
	}
}

// covers: R1
// S2B-AC4 (escalated, owner decision; see the adversarial report): a message the client
// is still appending is archived as the bytes that were there and stays that way. An
// mbox has no end marker, so a torn last message cannot be told from a short one without
// comparing the file's size and mtime across the read, which mboxFile does not do; the
// next incremental run finds the message already archived (same key, same fingerprint) and
// skips it, so the torn copy stays until a Full run. Detecting it (a stability check
// that records a fillable gap or makes the run partial) changes what a run reports about
// every live mailbox: the owner's call.
func TestATornTailOfAnMboxIsNotArchivedAsComplete(t *testing.T) {
	t.Skip("R1 (S2B-AC4): a last mbox message cut mid-append is archived as complete and skipped as archived by every later incremental run; a size/mtime stability check changes what a run reports for every live mailbox: the owner's decision; closes when it is made")
	store := filepath.Join(tmpDir(t), "tb")
	first := strings.ReplaceAll(p14Plain("first", "first"), "\r\n", "\n")
	torn := strings.ReplaceAll(p14Mail("torn", "torn", "", "first half of the body"), "\r\n", "\n")
	torn = strings.TrimSuffix(torn, "\n")
	inbox := filepath.Join(store, "Inbox")
	p14Write(t, inbox, "From a@example.com Mon Mar  3 09:00:00 2025\n"+first+"\nFrom a@example.com Mon Mar  3 09:00:00 2025\n"+torn)
	out := tmpDir(t)
	if _, err := runLocal(t, out, store); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	f, err := os.OpenFile(inbox, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(" and the second half\n\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := runLocal(t, out, store); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if !bytes.Contains(archiveText(t, out), []byte("second half")) {
		t.Error("the completed message is archived without its second half: the torn copy was kept as complete")
	}
}

// covers: MA-321, R1, R10, S46
// S2B-AI4 (and S2B-AC8): a maildir message the archive already holds that a client moves
// from new/ to cur/ while the run is reading new/ is not a loss, and the run is not
// partial for it. The walk lists new/, the client flags the message (the file moves to
// cur/b:2,S), the read of new/b finds nothing (a vanished-during-read gap) and the walk
// then reads cur/b:2,S fine; the exporter skips it as archived, so no capture closed the
// gap and the run ended RED for a message that is in the archive. The twin first: the same
// move of a message the archive does not hold yet is captured and closes its own gap.
func TestAnArchivedMessageMovedFromNewToCurIsNoPartialRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("maildir names carry a colon")
	}
	for _, c := range []struct {
		name     string
		archived bool // run 1 archives the messages before the move
	}{
		{"a message not archived yet", false},
		{"an archived message", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := filepath.Join(tmpDir(t), "mail")
			maildirStore(t, store, "Inbox/new/a", "Inbox/new/b", "Inbox/new/c")
			out := tmpDir(t)
			if c.archived {
				if res, err := runLocal(t, out, store); err != nil || res.Partial || recordCount(t, out) != 3 {
					t.Fatalf("run 1: err %v, partial %v, %d records; want 3 archived", err, res.Partial, recordCount(t, out))
				}
			}
			moved := false
			restore := source.InjectMaildirReadFaults(func(rel string) error {
				if rel == "Inbox/new/a" && !moved {
					moved = true
					return os.Rename(filepath.Join(store, "Inbox", "new", "b"), filepath.Join(store, "Inbox", "cur", "b:2,S"))
				}
				return nil
			})
			defer restore()
			// the rename needs cur/ to exist
			if err := os.MkdirAll(filepath.Join(store, "Inbox", "cur"), 0o755); err != nil {
				t.Fatal(err)
			}
			res, err := runLocal(t, out, store)
			if !moved {
				t.Fatal("the move never ran")
			}
			if err != nil || res.Partial || len(ledgerOf(t, out)) != 0 || recordCount(t, out) != 3 {
				t.Errorf("run: err %v, partial %v, ledger %s, %d records; want a clean run with 3 records and no gap: nothing is lost", err, res.Partial, gapReasons(ledgerOf(t, out)), recordCount(t, out))
			}
		})
	}
}
