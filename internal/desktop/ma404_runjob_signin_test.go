package desktop

import (
	"testing"
	"time"
)

// signInOutcome starts a sign-in and returns the first state it settles in: "confirm" or "done".
func (e *wsEnv) signInOutcome() map[string]any {
	e.t.Helper()
	e.ok("/api/m365/signin", nil)
	for i := 0; i < 600; i++ {
		out := e.signInStatus("")
		switch out["state"] {
		case "confirm", "done":
			return out
		case "failed":
			e.t.Fatalf("the sign-in failed: %v", out)
		}
		time.Sleep(25 * time.Millisecond)
	}
	e.t.Fatal("the sign-in never settled")
	return nil
}

// covers: MA-404, R17
// A sign-in as another account asks nothing when the installed scheduled backup is a run-job (the
// part A re-verification, 2026-10-28): a run-job backs up the local mail folder fixed in its own
// job file, so "your scheduled backup will now archive B" would be false. The sign-in is saved at
// once, as with no backup installed. Positive twin first: with a capture installed (no job file),
// the same sign-in waits for the answer, as XC7 says.
func TestASignInAsAnotherAccountAsksNothingForARunJob(t *testing.T) {
	e := newWsEnv(t)
	useBackupFake(e, &backupFake{tasks: map[string]bool{e.out: true}})
	e.fg.mutate(func() { e.fg.upn = "bob@contoso.org" })

	c := e.signInOutcome()
	if c["state"] != "confirm" {
		t.Fatalf("the positive twin: a capture's sign-in as another account did not ask: %v", c)
	}
	e.signInStatus("signout")
	e.waitSignIn("done")
	if upn, _ := e.d.signedInNow(); upn != "alice@contoso.org" {
		t.Fatalf("fixture: after Sign out the saved sign-in is %q", upn)
	}

	plantRunJob(t, e.out)
	done := e.signInOutcome()
	if done["state"] != "done" || done["signedOut"] == true {
		t.Fatalf("a run-job's sign-in as another account asked: %v", done)
	}
	if upn, _ := e.d.signedInNow(); upn != "bob@contoso.org" {
		t.Errorf("the new sign-in was not saved: signed in as %q", upn)
	}
}
