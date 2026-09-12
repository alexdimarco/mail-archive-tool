package app

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-227, R21, R13, S39
// Redaction reaches EVERY copy (rev-4 §6, #4/#10). A collapsed record carries its
// move-duplicate loser's file in AlsoFiles. When the operator redacts by deleting
// the survivor's .html and runs reindex, reindex deletes the survivor's OWN
// orphaned siblings (.eml + -attachments.zip) AND the whole loser triple — so no
// copy of the redacted message (the more-sensitive .eml/.zip included) is left
// served at /files/. This runs only in reindex, never a normal run (R13).
func TestReindexRedactionReachesSiblingsAndLoserCopies(t *testing.T) {
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)

	survivor := filepath.ToSlash(filepath.Join("store", "Inbox", "m.html"))
	loser := filepath.ToSlash(filepath.Join("store", "Trash", "m.html"))
	// Write each message's full triple (html + eml + attachments zip) on disk.
	write := func(relHTML string) {
		stem := filepath.Join(out, filepath.FromSlash(relHTML[:len(relHTML)-len(".html")]))
		if err := os.MkdirAll(filepath.Dir(stem), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, suf := range []string{".html", ".eml", "-attachments.zip"} {
			if err := os.WriteFile(stem+suf, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(survivor)
	write(loser)

	key := state.LiveKey("store", "mid:m@x")
	doc := struct {
		Version int                     `json:"version"`
		Entries map[string]state.Record `json:"entries"`
	}{
		Version: 5,
		Entries: map[string]state.Record{
			key: {Path: survivor, Folder: "Inbox", FirstFolder: "Inbox", Present: true,
				Fingerprint: "aaaaaaaaaaaaaaaa", FpScheme: state.FpSchemeCurrent, AlsoFiles: []string{loser}},
		},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Add("store", []string{"Inbox"}, &model.Message{Subject: "m", InternetMessageID: "<m@x>"}, survivor, key); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	ix.Close()

	// Redact: the operator deletes only the survivor's .html.
	if err := os.Remove(filepath.Join(out, filepath.FromSlash(survivor))); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Reindex(out, logger); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	// Nothing under either stem may remain served.
	gone := func(relHTML string) {
		stem := filepath.Join(out, filepath.FromSlash(relHTML[:len(relHTML)-len(".html")]))
		for _, suf := range []string{".html", ".eml", "-attachments.zip"} {
			if _, err := os.Stat(stem + suf); err == nil {
				t.Errorf("redaction left %s on disk (still served at /files/)", relHTML[:len(relHTML)-len(".html")]+suf)
			}
		}
	}
	gone(survivor) // its .eml + -attachments.zip (the .html was already deleted)
	gone(loser)    // the whole collapse-loser triple
}
