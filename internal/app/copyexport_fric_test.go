package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

// covers: MA-414, R29, R12, S52
// The destination box's example path is one this computer has: R-e's two sentences that give an
// example name a Windows path only on Windows, and a macOS or Linux owner who typed ~/Downloads
// is told a path of their own system's shape. The positive twin first: the examples per system.
func TestFricDestinationExampleIsThisComputersOwn(t *testing.T) {
	for goos, want := range map[string]string{"windows": `C:\Users\you\Downloads`, "darwin": "/Users/you/Downloads", "linux": "/home/you/Downloads"} {
		if got := DestExampleFor(goos); got != want {
			t.Errorf("the example for %s: %q, want %q", goos, got, want)
		}
	}
	_, arch, _ := destFixture(t)
	e := newCEEnv(t)
	for _, typed := range []string{"~/Downloads", "dest"} {
		_, err := e.CheckDestination(typed, []string{arch})
		refusedWith(t, err, "R-e", []string{"full path", "Browse", DestExampleFor(runtime.GOOS)})
		if runtime.GOOS != "windows" && strings.Contains(err.Error(), `C:\`) {
			t.Errorf("%s: a %s owner is told to type a Windows path: %q", typed, runtime.GOOS, err)
		}
	}
}

// covers: MA-438, R12, S52
// F15 (the half that needs no owner): R-i for a selection that is one folder does not tell the
// owner to "split it by folder", which is a dead end for a lone oversized folder. It says what
// MailArchive cannot do (split one folder across exports in this version) and what the owner can
// do (pick messages by hand, up to the pick cap, or export its subfolders one at a time). A
// selection of several folders, and one that names no folder, keep the design's words
// (TestSelectionPastTheCapIsRIInTheDesignsWords). The positive twin first: the cap itself plans.
func TestFricRIForOneFolderDoesNotSaySplitByFolder(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	dest := t.TempDir()
	mk := func(n int, folders ...CopyFolderRef) []CopyMessage {
		ms := make([]CopyMessage, n)
		for i := range ms {
			ms[i] = CopyMessage{SourceID: "x", SizeHint: -1}
			if len(folders) > 0 {
				ms[i].Folder = []CopyFolderRef{folders[i%len(folders)]}
			}
		}
		return ms
	}
	inbox, sent := CopyFolderRef{ID: "F-INBOX", Name: "Inbox"}, CopyFolderRef{ID: "F-SENT", Name: "Sent Items"}
	plan := func(ms []CopyMessage) error {
		_, err := e.PlanCopyExport(CopyPlanInput{Source: src.Describe(), Messages: ms, Dest: dest, Options: CopyOptions{PreserveFolders: true}})
		return err
	}
	if err := plan(mk(copyMaxMessages, inbox)); err != nil {
		t.Fatalf("one folder at the cap itself was refused: %v", err)
	}

	// one folder over the cap
	var ref *CopyRefusal
	if err := plan(mk(copyMaxMessages+1, inbox)); !errors.As(err, &ref) || ref.ID != "R-i" {
		t.Fatalf("one folder over the cap: %v, want R-i", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1),
		assure.Names("This folder has 200,001 messages", "up to 200,000", "can't split one folder", "by hand", "10,000", "subfolders"),
		assure.Forbid("Split it into several exports", "for example by folder"))

	// two folders, and no folder named: the design's words
	for name, ms := range map[string][]CopyMessage{"two folders": mk(copyMaxMessages+1, inbox, sent), "no folder named": mk(copyMaxMessages + 1)} {
		ref = nil
		if err := plan(ms); !errors.As(err, &ref) || ref.ID != "R-i" {
			t.Fatalf("%s over the cap: %v, want R-i", name, err)
		}
		assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names("This selection has 200,001 messages", "Split it into several exports", "for example by folder"))
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
