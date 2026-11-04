package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// The recovery key and the keyboard (MA-1076): the owner's pick Q4 of 2026-11-04 (Enter in the box
// the recovery key is typed back in uses it) and the independent review's LOW-4 and LOW-6 of bc84ca5.

// covers: MA-1076, R39, S41
// When a key appears the keyboard is on the key itself, not on Save the recovery file (Enter there
// began the owner's second report's confusion); Enter in the type-back box uses the key, as Use this
// recovery key does, and other keys do not; the recovery file's contents refused there are taken out
// of the box, a typing mistake is left to fix; once a new recovery key takes effect the keyboard is
// on the line saying so. Positive twin first: the button still uses the key. Run under Node
// (testdata/pagescript/harness.js, keyKeyboard).
func TestTheRecoveryKeyStepsKeepTheKeyboardWhereItIsUseful(t *testing.T) {
	if passed := runPageScenario(t, "keyKeyboard", true); len(passed) != 4 {
		t.Errorf("the keyKeyboard scenario passed %d of its 4 checks: %v", len(passed), passed)
	}
}

// covers: MA-1076, X15
// The served markup and script carry what the scenario stands in for: the recovery key's line can
// take the keyboard (tabindex="-1") and is where its step puts it; the type-back box answers Enter
// through the same button. Positive twin first: the steps card is on the page.
func TestTheServedKeyStepTakesTheKeyboardOnTheKey(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	page := sessionPage(t, e.h)
	assure.Reached(t, strings.Contains(page, `id="encNewKeyBox"`), "the steps card")
	if !strings.Contains(page, `<p class="mono" id="encNewKey" tabindex="-1"></p>`) {
		t.Error("the recovery key's line cannot take the keyboard")
	}
	for _, want := range []string{"showStep(box,$('encNewKey'),", "tk.addEventListener('keydown',ev=>{if(ev.key==='Enter'&&!ev.isComposing){ev.preventDefault();if(uk&&!uk.disabled)uk.click();}});"} {
		if !strings.Contains(encryptionJS, want) {
			t.Errorf("encryption.js does not carry %q", want)
		}
	}
}
