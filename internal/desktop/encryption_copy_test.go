package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// "Make an unencrypted copy" on the Encryption card (phase 3 §10, K5, X14; P3-4
// step 7; MA-824).

// pickingParent makes the folder dialog answer parent.
func pickingParent(t *testing.T, parent string) {
	t.Helper()
	orig := pickFolder
	pickFolder = func(context.Context, string) (string, error) { return parent, nil }
	t.Cleanup(func() { pickFolder = orig })
}

// covers: MA-824, R37
// The plan is shown first — the new folder and its size — writing nothing; confirmed,
// the copy runs as the Desktop's task on a lease of its own, so a Lock after the click
// does not stop it; the copy is a plaintext archive a plain verify attests whole.
func TestTheCardMakesAnUnencryptedCopy(t *testing.T) {
	out := backedUpEncrypted(t)
	parent := t.TempDir()
	pickingParent(t, parent)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if !strings.Contains(sessionPage(t, e.h), `id="encCopy"`) {
		t.Fatal("the unlocked card offers no copy")
	}
	_, p := e.post(t, "api/encryption/copy/plan", nil)
	lines := strings.Join(toLines(p["lines"]), "\n")
	entries, _ := os.ReadDir(parent)
	if p["ok"] != true || !strings.Contains(lines, "UNENCRYPTED") || !strings.Contains(lines, "New folder: "+parent) || len(entries) != 0 || p["askTwice"] != false {
		t.Fatalf("the plan: %v (entries %d)", p, len(entries))
	}
	if _, s := e.post(t, "api/encryption/copy/start", map[string]any{"token": p["token"], "confirm": true}); s["ok"] != true {
		t.Fatalf("start: %v", s)
	}
	e.post(t, "api/encryption/lock", nil) // after the click: the copy goes on
	res, errText := waitIdle(t, e)
	if errText != "" || !strings.Contains(res, "Unencrypted copy made at") || !strings.Contains(res, "1 of 1 messages") {
		t.Fatalf("the copy: %q %q", res, errText)
	}
	entries, _ = os.ReadDir(parent)
	if len(entries) != 1 {
		t.Fatalf("the parent holds %d entries", len(entries))
	}
	v, err := app.Verify(filepath.Join(parent, entries[0].Name()), app.VerifyOptions{}, nil, nil)
	assure.Reached(t, err == nil && v.Attested(), "the copy is attested whole")
}

// covers: MA-824, R37
// A folder that may leave this computer is copied to only when the copy is confirmed
// twice — checked by the Desktop, not only the page (K5); a plan of another archive's,
// or one whose archive locked, is not started.
func TestTheCardAsksTwiceAndChecksItsPlan(t *testing.T) {
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveCommercial", "")
	t.Setenv("OneDriveConsumer", "")
	out := backedUpEncrypted(t)
	synced := filepath.Join(t.TempDir(), "Dropbox")
	os.Mkdir(synced, 0o700)
	pickingParent(t, synced)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, p := e.post(t, "api/encryption/copy/plan", nil)
	if p["askTwice"] != true || !strings.Contains(strings.Join(toLines(p["leaves"]), " "), "Dropbox") {
		t.Fatalf("a synced folder's plan: %v", p)
	}
	_, s := e.post(t, "api/encryption/copy/start", map[string]any{"token": p["token"], "confirm": true})
	entries, _ := os.ReadDir(synced)
	assure.Refused(t, 1, str(s["error"]), assure.Code(1), assure.Names("not confirmed"),
		assure.NoSideEffect(func() bool { return s["ok"] != true && len(entries) == 0 }))

	e.post(t, "api/encryption/lock", nil) // the plan goes with the archive's lock
	_, s = e.post(t, "api/encryption/copy/start", map[string]any{"token": p["token"], "confirm": true, "confirmAgain": true})
	entries, _ = os.ReadDir(synced)
	assure.Refused(t, 1, str(s["error"]), assure.Code(1), assure.Names("no longer planned"),
		assure.NoSideEffect(func() bool { return s["ok"] != true && len(entries) == 0 }))
}

// toLines is a reply's list of lines.
func toLines(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			out = append(out, str(x))
		}
	}
	return out
}
