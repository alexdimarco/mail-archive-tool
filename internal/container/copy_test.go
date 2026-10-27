package container

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mail-archive-tool/internal/vault"
)

// covers: MA-811, R37
// The history an unencrypted copy is given (Phase 3 P3-4): the whole timeline when
// every chunk reads; a chunk damaged or gone stands as a counted barrier and the copy
// goes on (the archive itself has that hole); a key locked meanwhile stops it with
// vault.ErrKeyDestroyed — never a barrier the archive does not have.
func TestTheCopysHistoryCountsHolesAndStopsWhenLocked(t *testing.T) {
	out := readable(t)
	_, s, err := LoadManifest(out, readOnly(keys[0]))
	if err != nil {
		t.Fatal(err)
	}
	whole, lost, err := s.HistoryForCopy()
	if err != nil || lost != 0 || len(whole) == 0 || !reflect.DeepEqual(whole, s.History()) {
		t.Fatalf("positive twin: %d events, %d lost, %v", len(whole), lost, err)
	}
	first := filepath.Join(out, filepath.FromSlash(s.ptr.History[0].Name.Path()))
	raw, _ := os.ReadFile(first)

	bad := bytes.Clone(raw)
	bad[len(bad)-1] ^= 1
	os.WriteFile(first, bad, 0o600)
	ev, lost, err := s.HistoryForCopy()
	if err != nil || lost != 1 || !ev[0].Barrier {
		t.Fatalf("a damaged chunk: want one counted barrier, got %d lost, %v", lost, err)
	}

	os.Remove(first)
	ev, lost, err = s.HistoryForCopy()
	if err != nil || lost != 1 || !ev[0].Barrier {
		t.Fatalf("a chunk gone: want one counted barrier, got %d lost, %v", lost, err)
	}
	os.WriteFile(first, raw, 0o600)

	k := keyCopy(t)
	_, ls, err := LoadManifest(out, readOnly(k))
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	if ev, _, err := ls.HistoryForCopy(); !errors.Is(err, vault.ErrKeyDestroyed) || ev != nil {
		t.Fatalf("a locked key: want vault.ErrKeyDestroyed and no events, got %d events, %v", len(ev), err)
	}
}
