package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-414, R29, R12, S52
// F19: the destination box's refusals say what is wrong. An empty box says a folder is to be
// chosen (not "The folder  isn't a full path"), ~ and %NAME% and $NAME are said not to be
// expanded, a file is said to be a file and not a folder; the existing wording (full path,
// doesn't exist, Browse, Create folder) stays. The positive twins first: an existing folder, one
// whose name holds a % and an absolute path are accepted.
func TestFricDestinationTypingMessagesSayWhatIsWrong(t *testing.T) {
	top, arch, dest := destFixture(t)
	e := newCEEnv(t)
	archives := []string{arch}
	pct := filepath.Join(top, "100%done")
	if err := os.Mkdir(pct, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{dest, pct} {
		if _, err := e.CheckDestination(ok, archives); err != nil {
			t.Fatalf("%s was refused: %v", ok, err)
		}
	}
	file := filepath.Join(dest, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, dest string
		names      []string
	}{
		{"empty", "", []string{"No destination folder is chosen", "Browse", "Create folder"}},
		{"blank", "   ", []string{"No destination folder is chosen"}},
		{"a home folder", "~/Downloads", []string{"isn't a full path", "doesn't expand ~", "full path of an existing folder", "Browse"}},
		{"a Windows variable", `%USERPROFILE%\Downloads`, []string{"isn't a full path", "doesn't expand", "Browse"}},
		{"a shell variable", "$HOME/exports", []string{"isn't a full path", "doesn't expand", "Browse"}},
		{"a relative path", "dest", []string{"full path", "Type the full path", "Browse"}},
		{"a file", file, []string{"doesn't exist", "a file, not a folder", "Create folder"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := e.CheckDestination(c.dest, archives)
			refusedWith(t, err, "R-e", c.names)
			if strings.Contains(err.Error(), "folder  isn't") {
				t.Errorf("the refusal names a folder that is not there: %q", err)
			}
		})
	}
}

// The friction review's fixes in the engine's own words (docs/review-extraction-workspace-friction.md).

// covers: MA-438, R12, S52
// F2, F3: the stop notes of a run cut off by the sign-in name the button that is on Export
// activity (Sign in), the account to sign in as, and the button to click next; a retry's R-l
// says the same, and RetryButton names the button the retry was started from. (A retry's R-l
// reaches the owner through the desktop, which words it for the page: the engine's own R-l
// stays the design's §7.6 text, which TestRetryReadsTheRunRecord pins.)
func TestFricSignInStopsAndRetryRefusalNameTheButtons(t *testing.T) {
	plan := &CopyPlan{Dest: `E:\Exports`, Source: CopySourceInfo{Kind: "m365", Mailbox: "alex@contoso.com"}}
	forbid := assure.Forbid("Load folders again", "panic", "goroutine", "https://")
	assure.Refused(t, 1, refusalText("R-c", 1203, 2088, "", plan, "state"), assure.Code(1),
		assure.Names("click Sign in on Export activity", "Export the remaining messages"), forbid)
	assure.Refused(t, 1, refusalText("R-z", 900, 2088, "bob@contoso.com", plan, "state"), assure.Code(1),
		assure.Names("Sign in as alex@contoso.com (click Sign in on Export activity)", "Export the remaining messages"), forbid)
	if RetryButton("rest") != "Export the remaining messages" || RetryButton("failed") != "Export failed messages again" {
		t.Errorf("the retry buttons: %q %q", RetryButton("rest"), RetryButton("failed"))
	}
}
