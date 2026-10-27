package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// covers: MA-849, R37
// restore-key-file -remembered says the computer remembers the key only when it does:
// on a plain folder, and on an archive whose key this computer does not remember, it
// refuses before any question, naming -from FILE, nothing written (P3-8 friction
// review, B6). The positive twin is TestTheCLIRebuildsKeyFilesFromTheRememberedKey.
func TestRestoreFromTheRememberedKeyChecksFirst(t *testing.T) {
	flowRecord(t)
	plain := t.TempDir()
	a := &scripted{isAttended: true, answers: []string{"y"}}
	err := restoreKeyFile(a, plain, "", noKeyFlags())
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("does not remember", "-from FILE"),
		assure.NoSideEffect(func() bool { return len(a.prompts) == 0 && !exists(filepath.Join(plain, vault.FileName)) }))

	out := filepath.Join(t.TempDir(), "archive")
	c := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "", "", keyAnswer}}
	u, err := createEncrypted(c, out, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, c.transcript())
	}
	u.Destroy()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	flowRecord(t) // another computer's record: nothing remembered here
	a = &scripted{isAttended: true, answers: []string{"y"}}
	err = restoreKeyFile(a, out, "", noKeyFlags())
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("does not remember"),
		assure.NoSideEffect(func() bool { return len(a.prompts) == 0 && !exists(filepath.Join(out, vault.FileName)) }))
}

// covers: MA-850, R37
// repair-record on a record that needs no repair succeeds (exit 0), saying there is
// nothing to repair — the owner's X1 ruling, 2026-10-27 — and repairs nothing.
func TestRepairRecordOnAHealthyRecordSucceeds(t *testing.T) {
	flowRecord(t)
	if _, err := encrecord.Load(); err != nil {
		t.Fatalf("a fresh record is usable: %v", err)
	}
	err := runRepairRecord(nil)
	rec, lerr := encrecord.Load()
	assure.Reached(t, err == nil && lerr == nil && len(rec.Archives) == 0, "nothing to repair: exit 0")
}

// covers: MA-849, R37
// A keyed `reindex -rebuild` stopped by Ctrl-C or a terminate exits 130 or 143,
// saying nothing was saved, only when the stop ended the rebuild; a stop that came
// after the rebuild was saved leaves a saved rebuild, exit 0 (P3-8 friction review,
// B4). Positive twin: no stop, exit 0.
func TestAStopAfterTheRebuildWasSavedIsNotTold(t *testing.T) {
	assure.Reached(t, reindexStopCode(0, nil) == 0 && reindexStopCode(130, nil) == 0 &&
		reindexStopCode(130, context.Canceled) == 130 && reindexStopCode(143, errors.New("stopped")) == 143 &&
		reindexStopCode(0, errors.New("failed")) == 0,
		"a stop is told only when it ended the rebuild")
}
