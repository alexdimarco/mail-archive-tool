package app

import (
	"testing"
)

// covers: MA-1236, R45, S82
// The Gmail question is asked once per run however many Gmail accounts the archive holds
// (docs/design-gmail-dedup.md §7.4 fold c: "once"). An existing archive with two Gmail stores,
// both off. Positive twin first: a run with nobody to ask asks nothing and both wait. A person
// at a terminal who says Not now is asked once and both stay off; one who says Yes is asked
// once and both are switched on in that run.
func TestTheGmailQuestionIsAskedOncePerRun(t *testing.T) {
	_, a := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	_, b := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	existing := func() string {
		t.Helper()
		out := tmpDir(t)
		tbCheckOff = true
		runTB(t, Options{Inputs: []string{a, b}, Out: out})
		tbCheckOff = false
		if v, on := manifestOnDisk(t, out); v != 6 || len(on) != 0 {
			t.Fatalf("fixture: version %d, %v switched on", v, on)
		}
		return out
	}

	out := existing()
	if res := runTB(t, Options{Inputs: []string{a, b}, Out: out, Unattended: true}); len(res.Gmail.Off) != 2 || len(res.Gmail.Switched) != 0 {
		t.Fatalf("positive twin: an unattended run: %+v", res.Gmail)
	}

	asked := 0
	res := runTB(t, Options{Inputs: []string{a, b}, Out: out, AskGmail: func(string) bool { asked++; return false }})
	if asked != 1 || len(res.Gmail.Switched) != 0 || res.Gmail.Waiting() {
		t.Fatalf("Not now: asked %d times, outcome %+v", asked, res.Gmail)
	}

	out = existing()
	asked = 0
	res = runTB(t, Options{Inputs: []string{a, b}, Out: out, AskGmail: func(string) bool { asked++; return true }})
	if v, on := manifestOnDisk(t, out); asked != 1 || len(res.Gmail.Switched) != 2 || v != 7 || len(on) != 2 {
		t.Fatalf("Yes: asked %d times, version %d, on %v, outcome %+v", asked, v, on, res.Gmail)
	}
}
