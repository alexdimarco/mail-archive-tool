package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1284, MA-1198
// `mailarchive status` and `status -json`, the real command, read the name Thunderbird shows for an
// account from the plaintext record (the lead's call of 2026-11-08): the text says the address,
// `-json` carries `name` beside `account` (the store folder, unchanged). A planted name reaches the
// terminal cleaned and capped, never a control character. Positive twin first: a record with no
// name says the folder words, and `-json` has no `name`.
func TestStatusSaysThunderbirdsNameForTheAccount(t *testing.T) {
	e := newCLIDel(t)
	plant := func(name string) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "Named mail")
		e.archiveAt(dir)
		lr, _, err := state.ReadLastRun(dir)
		if err != nil {
			t.Fatal(err)
		}
		lr.Status, lr.Finished = state.RunOK, time.Now().UTC()
		lr.Thunderbird = []state.TBAccount{{Account: "imap.gmail.com", Name: name, Verdict: "behind", OnDisk: 1670, Listed: 14201}}
		if err := state.WriteLastRun(dir, lr); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	entry := func(dir string) map[string]any {
		t.Helper()
		out, _ := statusOf(t, dir, "-json")
		var doc struct {
			TC []map[string]any `json:"thunderbird_completeness"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.TC) != 1 {
			t.Fatalf("status -json: %v\n%s", err, out)
		}
		return doc.TC[0]
	}
	dir := plant("")
	if out, _ := statusOf(t, dir); !strings.Contains(out, "about 1,670 of 14,201 messages in the Thunderbird account kept in the folder imap.gmail.com so far") {
		t.Fatalf("positive twin: status:\n%s", out)
	}
	if j := entry(dir); j["account"] != "imap.gmail.com" || j["name"] != nil {
		t.Fatalf("positive twin: -json: %v", j)
	}

	dir = plant("sam.walker@gmail.com")
	if out, _ := statusOf(t, dir); !strings.Contains(out, "about 1,670 of 14,201 messages in sam.walker@gmail.com so far") || strings.Contains(out, "kept in the folder") {
		t.Errorf("status:\n%s", out)
	}
	if j := entry(dir); j["account"] != "imap.gmail.com" || j["name"] != "sam.walker@gmail.com" {
		t.Errorf("-json: %v", j)
	}

	hostile := "sam\x1b]0;evil\x07@\u202egmail.com\x1b[2J" + strings.Repeat("q", 10*1024)
	dir = plant(hostile)
	out, _ := statusOf(t, dir)
	if strings.ContainsAny(out, "\x1b\x07\u202e") || strings.Contains(out, strings.Repeat("q", 81)) {
		t.Errorf("a planted name reaches the terminal raw:\n%q", out)
	}
	if clean := state.CleanFolderName(hostile); !strings.Contains(out, " in "+clean+" so far") || !strings.HasSuffix(clean, "…") {
		t.Errorf("status does not say the cleaned, capped name %q:\n%s", clean, out)
	}
	if j := entry(dir); j["name"] != state.CleanFolderName(hostile) {
		t.Errorf("-json's name: %q", j["name"])
	}
}
