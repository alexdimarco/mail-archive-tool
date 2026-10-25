//go:build !windows

package export

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"mail-archive-tool/internal/vault"
)

// covers: MA-724, R37
func TestTheUnitStoreNeverOpensAFifoOrFollowsALinkedFolder(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	_, rec := onlyRecord(t, exp.Manifest)
	if _, err := exp.Units.Read(rec.Unit, vault.UnitMessage, *rec.Sealed.HTML); err != nil {
		t.Fatalf("positive twin: the page opens: %v", err)
	}
	page := filepath.Join(out, exp.Units.Name(rec.Unit, vault.UnitMessage, 1))
	if err := os.Remove(page); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(page, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := exp.Units.Read(rec.Unit, vault.UnitMessage, *rec.Sealed.HTML); !errors.Is(err, ErrUnitStore) {
		t.Fatalf("a FIFO at a unit's name: want ErrUnitStore (never opened), got %v", err)
	}

	planted := t.TempDir()
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(planted, UnitsDir)); err != nil {
		t.Fatal(err)
	}
	u := &Units{Root: planted, Key: testUnitsKey(t)}
	_, err := u.Write("0123456789abcdef0123456789abcdef", vault.UnitMessage, 1, []byte("x"))
	if !errors.Is(err, ErrUnitStore) {
		t.Fatalf("a linked units/ folder: want ErrUnitStore, got %v", err)
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatal("a write followed the planted link out of the archive")
	}
}
