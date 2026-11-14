package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mail-archive-tool/internal/state"
)

// manifestOnDisk is the archive manifest's stamped version and its Gmail switch, read raw.
func manifestOnDisk(t *testing.T, out string) (int, map[string]string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Version     int               `json:"version"`
		DedupStores map[string]string `json:"dedup_stores"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m.Version, m.DedupStores
}

// existingGmailArchive is a real archive of a Gmail store, made by a run that did not look at
// the store's account (the archive an older MailArchive made): version 6, nothing switched on.
func existingGmailArchive(t *testing.T) (store, out string) {
	t.Helper()
	_, store = imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	out = tmpDir(t)
	gmailDetectOff = true // an older MailArchive did not look at the account (was tbCheckOff, which no longer turns Gmail detection off)
	runTB(t, Options{Inputs: []string{store}, Out: out})
	gmailDetectOff = false
	if v, on := manifestOnDisk(t, out); v != 6 || len(on) != 0 {
		t.Fatalf("the existing archive is version %d with %v switched on, want 6 and none", v, on)
	}
	return store, out
}

// covers: MA-1236, R45, S82
// The Gmail switch in a backup run (Q-B, slice B3 wiring B2's GmailDecision). Positive twin
// first: a new archive of a store that is not Gmail is version 6 and nothing is switched. A new
// archive of a Gmail store is switched on at its first run (version 7), and asks nobody. An
// existing archive of a Gmail store: a run with nobody to ask (a scheduled run) never asks and
// never switches, leaves it version 6, and keeps "the question waits" in its last-run record;
// Not now kept on this computer: never asked, nothing waits; a person at a terminal is asked
// once, with the question's exact words: Not now leaves it version 6 and nothing waits, Yes
// switches it on from this run (version 7) and the next attended run asks nothing; the yes given
// before the run (the Desktop card's, Options.GmailYes) switches it on without asking.
func TestTheBackupRunAsksTheGmailQuestionOnlyOfAPerson(t *testing.T) {
	_, plain := imapProfile(t, "imap.example.org", nil, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("p", 1)})
	out := tmpDir(t)
	if res := runTB(t, Options{Inputs: []string{plain}, Out: out}); len(res.Gmail.Switched)+len(res.Gmail.Off) != 0 {
		t.Fatalf("a store that is not Gmail: %+v", res.Gmail)
	}
	if v, on := manifestOnDisk(t, out); v != 6 || len(on) != 0 {
		t.Fatalf("a new archive of a store that is not Gmail: version %d, %v", v, on)
	}

	// A new Gmail archive is on from its first run.
	_, store := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	out = tmpDir(t)
	asked := 0
	res := runTB(t, Options{Inputs: []string{store}, Out: out, AskGmail: func(string) bool { asked++; return false }})
	if v, on := manifestOnDisk(t, out); v != 7 || len(on) != 1 || asked != 0 || len(res.Gmail.Switched) != 1 {
		t.Fatalf("a new Gmail archive: version %d, on %v, asked %d, outcome %+v", v, on, asked, res.Gmail)
	}

	// A new archive is new for the whole of its first run: a Gmail store read after another
	// store's mail is switched on too (a scheduled first run included).
	_, plain2 := imapProfile(t, "imap.example.org", nil, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("q", 1)})
	_, store2 := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	out = tmpDir(t)
	res = runTB(t, Options{Inputs: []string{plain2, store2}, Out: out, Unattended: true})
	if v, on := manifestOnDisk(t, out); v != 7 || len(on) != 1 || len(res.Gmail.Switched) != 1 || res.Gmail.Waiting() {
		t.Fatalf("a new archive's second store, Gmail: version %d, on %v, outcome %+v", v, on, res.Gmail)
	}

	// Existing: a scheduled run never asks or switches; the question waits.
	store, out = existingGmailArchive(t)
	res = runTB(t, Options{Inputs: []string{store}, Out: out, Unattended: true})
	if v, on := manifestOnDisk(t, out); v != 6 || len(on) != 0 || !res.Gmail.Waiting() || res.Gmail.Question != state.GmailQuestion {
		t.Fatalf("a scheduled run: version %d, on %v, outcome %+v", v, on, res.Gmail)
	}
	if lr, _, _ := state.ReadLastRun(out); !reflect.DeepEqual(lr.GmailOff, res.Gmail.Off) || len(lr.GmailOff) != 1 {
		t.Fatalf("the last-run record keeps %q waiting, want the store %q", lr.GmailOff, res.Gmail.Off)
	}
	offStore := res.Gmail.Off

	// Not now kept on this computer: never asked, nothing waits.
	res = runTB(t, Options{Inputs: []string{store}, Out: out, GmailNotNow: true, AskGmail: func(string) bool { asked++; return true }})
	if v, _ := manifestOnDisk(t, out); v != 6 || asked != 0 || res.Gmail.Waiting() {
		t.Fatalf("Not now kept: version %d, asked %d, outcome %+v", v, asked, res.Gmail)
	}
	if lr, _, _ := state.ReadLastRun(out); len(lr.GmailOff) != 0 {
		t.Fatal("the last-run record says the question waits after Not now")
	}
	// A yes for another store only (the card's yes names stores) switches nothing.
	res = runTB(t, Options{Inputs: []string{store}, Out: out, GmailYesStores: []string{"another-store"}, Unattended: true})
	if v, _ := manifestOnDisk(t, out); v != 6 || len(res.Gmail.Switched) != 0 {
		t.Fatalf("a yes for another store: version %d, outcome %+v", v, res.Gmail)
	}

	// A person at a terminal, Not now.
	var words []string
	res = runTB(t, Options{Inputs: []string{store}, Out: out, AskGmail: func(q string) bool { words = append(words, q); return false }})
	if v, on := manifestOnDisk(t, out); v != 6 || len(on) != 0 || res.Gmail.Waiting() || !reflect.DeepEqual(words, []string{state.GmailQuestion}) {
		t.Fatalf("Not now at a terminal: version %d, on %v, asked %q, outcome %+v", v, on, words, res.Gmail)
	}

	// Yes: on from this run; the next attended run asks nothing.
	words = nil
	res = runTB(t, Options{Inputs: []string{store}, Out: out, AskGmail: func(q string) bool { words = append(words, q); return true }})
	if v, on := manifestOnDisk(t, out); v != 7 || len(on) != 1 || len(words) != 1 || len(res.Gmail.Switched) != 1 || res.Gmail.Waiting() {
		t.Fatalf("Yes at a terminal: version %d, on %v, asked %d, outcome %+v", v, on, len(words), res.Gmail)
	}
	runTB(t, Options{Inputs: []string{store}, Out: out, AskGmail: func(q string) bool { words = append(words, q); return true }})
	if len(words) != 1 {
		t.Fatalf("an archive already on was asked again (%d)", len(words))
	}

	// The yes given before the run: for every store (-gmail-folders), and for the stores the
	// card named (a scheduled run applies it).
	for _, o := range []Options{{GmailYes: true}, {GmailYesStores: offStore}} {
		store, out = existingGmailArchive(t)
		o.Inputs, o.Out, o.Unattended = []string{store}, out, true
		res = runTB(t, o)
		if v, on := manifestOnDisk(t, out); v != 7 || len(on) != 1 || len(res.Gmail.Switched) != 1 {
			t.Fatalf("the yes given before the run (%+v): version %d, on %v, outcome %+v", o, v, on, res.Gmail)
		}
	}
}
