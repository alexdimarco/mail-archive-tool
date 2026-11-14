package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// ma1331StripID takes the archive id out of out's last-run record, as an older MailArchive wrote it.
func ma1331StripID(t *testing.T, out string) {
	t.Helper()
	p := filepath.Join(out, state.LastRunName)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "archive")
	if b, err = json.Marshal(m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1331, R45
// The B3 adversarial pass, round 3's H2: a wait kept under no id (an archive whose record an older
// MailArchive wrote) cannot be told from another id-less archive at the same folder, so it never
// outlives a backup that gets past the manifest there, whichever archive that backup reached: here
// the waited archive itself, given an id by the other computer's newer MailArchive when its tidy-up
// finished. A later id-less archive at the folder that meets a tidy-up starts its own wait, never
// "waited since" the old date. Waits kept under an id other than the backup's stay (a rotation's
// other drive, MA-1319). Positive twin first: a backup refused at version 8 keeps the wait.
func TestAWaitKeptUnderNoIDEndsAtAnyBackupPastTheManifest(t *testing.T) {
	ma1316Settings(t)
	_, plain := imapProfile(t, "imap.example.org", nil, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("p", 2)})
	out := tmpDir(t)
	runTB(t, Options{Inputs: []string{plain}, Out: out})
	ma1331StripID(t, out)
	ma1316SetVersion(t, out, 8)
	opts := Options{Inputs: []string{plain}, Out: out, Unattended: true, Mode: export.Incremental}
	if _, err := Run(context.Background(), opts, discard(), nil); !TidyingElsewhere(err) {
		t.Fatalf("premise: not a tidy-up skip: %v", err)
	}
	old := time.Now().Add(-10 * 24 * time.Hour)
	if _, err := gmailanswer.NoteTidyWait(out, old); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), opts, discard(), nil); !TidyingElsewhere(err) {
		t.Fatalf("positive twin: not a tidy-up skip: %v", err)
	}
	if _, ok := gmailanswer.ReadTidyWaitFor(out, ""); !ok {
		t.Fatal("positive twin: a backup refused at version 8 ended the wait kept under no id")
	}
	rotation := state.NewArchiveID() // another drive's wait, kept for the same folder
	if _, err := gmailanswer.NoteTidyWaitFor(out, rotation, old); err != nil {
		t.Fatal(err)
	}

	// The other computer finishes; its newer MailArchive's run gave the record an id.
	ma1316SetVersion(t, out, 6)
	lr, _, _ := state.ReadLastRun(out)
	lr.Archive = state.NewArchiveID()
	if err := state.WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatalf("this computer's backup after the tidy-up: %v", err)
	}
	if w, ok := gmailanswer.ReadTidyWaitFor(out, ""); ok {
		t.Fatalf("the wait kept under no id since %v outlived a backup past the manifest", w.Since)
	}
	if _, ok := gmailanswer.ReadTidyWaitFor(out, rotation); !ok {
		t.Fatal("the backup ended another archive's wait kept under its own id")
	}
}
