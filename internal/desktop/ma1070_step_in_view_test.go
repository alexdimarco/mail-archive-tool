package desktop

import (
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// The step that needs the person's input is brought into view (MA-1070; the owner's report of
// 2026-11-03: "once you select the folder it seemingly stops - but what it is waiting for is the
// password - but because the screen doesnt move to the location of where the password is - the
// user never knows"). On the Windows VM the password box was 2,600 pixels below the window and 25
// Tab presses from the keyboard, which the page gave back to Make a new encrypted archive…. Run
// under Node (testdata/pagescript/harness.js) on the served dashboard.js and encryption.js.

// covers: MA-1070, R39, S41
// Make a new encrypted archive…: when the wizard's windows answer, the password step is brought into
// view, smoothly (at once under prefers-reduced-motion), the keyboard is in its first box and its
// status line says what is needed next; no password typed is said on the page. Positive twin first:
// the wizard backed out of moves nothing.
func TestTheNewArchivesPasswordStepComesIntoView(t *testing.T) {
	if passed := runPageScenario(t, "createStep", true); len(passed) != 7 {
		t.Errorf("the createStep scenario passed %d of its 7 checks: %v", len(passed), passed)
	}
}

// covers: MA-1070, MA-1071, R39, S41
// The recovery key's steps: once a key is made (a new archive, a new recovery key, a key file put
// back after the recovery file's window), its box is in view with the keyboard on its first button;
// after I have saved the recovery key, the box to type it back is in view with the keyboard in it,
// its status line wanting the recovery key's groups of letters and digits, not the recovery file.
// Once the key typed back takes effect, its box goes and what happened is brought into view; only
// the current step's line is said. Positive twins first: a key that could not be made, and a key
// typed back that is refused, move nothing.
func TestTheRecoveryKeyStepsComeIntoView(t *testing.T) {
	if passed := runPageScenario(t, "keySteps", true); len(passed) != 7 {
		t.Errorf("the keySteps scenario passed %d of its 7 checks: %v", len(passed), passed)
	}
}

// covers: MA-1070, R39, S41
// The sweep's same-kind flow: Open an archive folder… on an encrypted archive draws the page for it,
// which is then at its Encryption card with the keyboard in the Unlock box (or the recovery key box,
// for missing key files), once, and again after the browser puts back its scroll — at once, never
// smoothly, since the browser's scroll restoration cancelled a smooth move on the Windows VM in
// Edge. Positive twins first: a page drawn with nothing asked, and an archive that needs no unlock,
// are not moved.
func TestAnEncryptedArchiveOpenedFromAFolderAsksToUnlockInView(t *testing.T) {
	if passed := runPageScenario(t, "openThenUnlock", true); len(passed) != 7 {
		t.Errorf("the openThenUnlock scenario passed %d of its 7 checks: %v", len(passed), passed)
	}
}

// covers: MA-1070, X15
// The served page and scripts carry what the scenarios stand in for: one helper, showStep, in
// dashboard.js, which honours prefers-reduced-motion and reads no field's value; encryption.js
// bringing each step in through it; and every step's own status line (role=status) in the markup —
// the steps card's three, and the locked Encryption card's. Positive twin first: the Unlock form
// itself is drawn for a locked archive.
func TestTheServedPageBringsEachStepInThroughOneHelper(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	page := sessionPage(t, e.h)
	assure.Reached(t, strings.Contains(page, `id="encUnlockForm"`), "the locked archive's Unlock form")
	status := `<p class="card-sub" role="status" data-step-status hidden></p>`
	for _, id := range []string{"encCreateForm", "encNewKeyBox", "encTypeBack", "sec-encryption"} {
		re := regexp.MustCompile(`id="` + id + `"[^>]*>\s*(?:<h2>Encryption</h2>\s*)?` + regexp.QuoteMeta(status))
		if !re.MatchString(page) {
			t.Errorf("%s does not open with its status line %s", id, status)
		}
	}
	start := strings.Index(dashboardJS, "function showStep(")
	if start < 0 {
		t.Fatal("dashboard.js has no showStep helper")
	}
	helper := dashboardJS[start : start+strings.Index(dashboardJS[start:], "\n}\n")]
	if !strings.Contains(helper, "prefers-reduced-motion: reduce") || !strings.Contains(helper, "behavior:still?'auto':'smooth'") || !strings.Contains(helper, "preventScroll:true") {
		t.Errorf("showStep does not honour reduced motion, or scrolls twice:\n%s", helper)
	}
	if strings.Contains(helper, ".value") || strings.Contains(helper, "innerHTML") {
		t.Errorf("showStep reads a field's value or writes markup:\n%s", helper)
	}
	// Amended 2026-11-04 (MA-1076, the review's LOW-4): the key's step puts the keyboard on the key
	// itself, no longer on Save the recovery file.
	for _, call := range []string{"showStep(f,$('encCreatePw'),", "showStep(box,$('encNewKey'),", "showStep(t,$('encTypedKey'),"} {
		if !strings.Contains(encryptionJS, call) {
			t.Errorf("encryption.js does not bring a step in through showStep: %q missing", call)
		}
	}
}
