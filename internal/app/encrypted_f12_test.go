package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// recordFiles are the archive's record files a restore from an older backup puts
// back: today the manifest alone. Phase 2b-1 makes them the envelope, the root
// pointer AND its mirror (.ptr.bak — else the load takes the newer mirror and the
// test fails for the wrong reason) and chunks/ — never the inventory, which a
// restore of the record leaves as it was (design-encrypted-archives-phase2b §3).
var recordFiles = []string{".mailarchive-manifest.json"}

// covers: MA-747, R37
// F12, the Phase-2a adversarial pass's deferred gap: a record restored from an older
// backup names fewer units than the archive holds, and the keyed sweep must not take
// that as leave to delete the mail archived after the backup. The next keyed run
// refuses, typed and naming the archive, and every unit stays as it was. Open until
// Phase 2b-1's inventory (design-encrypted-archives-phase2b §3, the F12 rule); remove
// the skip when it lands.
func TestARestoredOlderRecordNeverSweepsNewerMail(t *testing.T) {
	t.Skip("MA-747 (F12, R37): a restored older manifest makes the keyed sweep delete every unit archived after it; encoder lands with the Phase-2b design, turns green in 2b-1")
	out, key := encryptedOut(t)
	parent := tmpDir(t)
	first := mboxStore(t, parent, "first", []string{"Inbox"}, 2)
	if _, err := Run(context.Background(), Options{Inputs: []string{first}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	older := map[string][]byte{}
	for _, name := range recordFiles {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		older[name] = b
	}
	unitsBefore := snapshotTree(t, filepath.Join(out, export.UnitsDir))

	second := mboxStore(t, parent, "second", []string{"Sent"}, 2)
	if _, err := Run(context.Background(), Options{Inputs: []string{first, second}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	units := snapshotTree(t, filepath.Join(out, export.UnitsDir))
	newer := 0
	for p, b := range units {
		if _, ok := unitsBefore[p]; !ok && b != nil {
			newer++
		}
	}
	if newer == 0 {
		t.Fatal("fixture: the second run archived no new unit")
	}

	for name, b := range older {
		if err := os.WriteFile(filepath.Join(out, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Every unit older than the next run's start, so only the record decides what the
	// sweep may take.
	old := time.Now().Add(-time.Hour)
	for p, b := range units {
		if b != nil {
			os.Chtimes(filepath.Join(out, export.UnitsDir, filepath.FromSlash(p)), old, old)
		}
	}

	_, err := Run(context.Background(), Options{Inputs: []string{first}, Out: out, Key: key}, discard(), nil)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if !errors.Is(err, ErrEncryptedArchive) {
		t.Errorf("a run over a restored older record: want ErrEncryptedArchive, got %v", err)
	}
	assure.Refused(t, 1, msg, assure.Code(1), assure.Names(out), assure.NoSideEffect(func() bool {
		return sameTree(units, snapshotTree(t, filepath.Join(out, export.UnitsDir)))
	}))
}
