package desktop

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// tbProfileArchive archives a Thunderbird store that sits under a real profile
// (a prefs.js at the profile root), so the archive's manifest records a local
// "path:" store id whose directory FindProfileDir recognises as a Thunderbird
// profile. Returns the archive location.
func tbProfileArchive(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte("// prefs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(profile, "Mail", "Local Folders")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From - Mon Mar  3 09:00:00 2025\nFrom: a@example.com\nTo: b@example.com\nSubject: hi\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <id1@ex>\n\nbody\n\n"
	if err := os.WriteFile(filepath.Join(store, "Inbox"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "arch")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{store}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	return out
}

// covers: MA-582
// The Desktop decides the pre-sync toggle from the MANIFEST's local store ids, not
// LastRun.Job: an archive whose local store sits under a Thunderbird profile is
// Thunderbird-sourced (toggle offered), while an M365-only archive is not — and this
// works for an -auto archive, which records no -input in LastRun.Job.
func TestLocalArchiveThunderbirdSourced(t *testing.T) {
	out := tbProfileArchive(t)
	if !localArchiveThunderbirdSourced(out) {
		t.Error("a local store under a Thunderbird profile (prefs.js) should be Thunderbird-sourced")
	}

	// Flip the manifest to an M365-only store: not Thunderbird-sourced.
	m, err := state.Load(filepath.Join(out, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	m.Stores = map[string]string{"mailbox:a@contoso.org": "a"}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if localArchiveThunderbirdSourced(out) {
		t.Error("an M365-only archive must not be Thunderbird-sourced")
	}

	// A missing archive / unreadable manifest is a safe false.
	if localArchiveThunderbirdSourced(filepath.Join(t.TempDir(), "nope")) {
		t.Error("an archive with no manifest must not be Thunderbird-sourced")
	}
}
