//go:build !windows && !darwin

package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncruces/zenity"
)

// fakeDialogTool is a stand-in for qarma, zenity and matedialog: it appends its arguments to
// $MA1286_LOG (each ended by \036, each run ended by \035) and, for a list, prints $MA1286_PICK.
const fakeDialogTool = `#!/bin/sh
for a in "$@"; do printf '%s\036' "$a"; done >> "$MA1286_LOG"
printf '\035' >> "$MA1286_LOG"
if [ "$1" = "--list" ]; then printf '%s\n' "$MA1286_PICK"; fi
`

// argAfter is the argument after flag in args, and whether flag is there.
func argAfter(args []string, flag string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1], true
		}
	}
	return "", false
}

// covers: MA-1286
// R2-01 through the real ncruces/zenity module on Linux and the BSDs: the program it runs (a fake
// qarma, zenity and matedialog on PATH, recording argv, as the testing rules allow for external
// binaries) is first given the launch health text as a message (--info), with the text exactly as
// written and --no-markup, then a list (--list) whose --text is the fixed prompt. Positive twin
// first: the fake records what it is given, so the health text reaching a list would be seen.
func TestTheHealthTextReachesTheDialogProgramAsPlainText(t *testing.T) {
	text := healthTextFromARealRecord(t)
	bin := t.TempDir()
	for _, name := range []string{"qarma", "zenity", "matedialog"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(fakeDialogTool), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logFile := filepath.Join(t.TempDir(), "argv")
	t.Setenv("PATH", bin)
	t.Setenv("MA1286_LOG", logFile)
	t.Setenv("MA1286_PICK", "Remove the scheduled backup")
	runs := func() [][]string {
		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatal(err)
		}
		var out [][]string
		for _, run := range strings.Split(strings.TrimSuffix(string(data), "\035"), "\035") {
			out = append(out, strings.Split(strings.TrimSuffix(run, "\036"), "\036"))
		}
		return out
	}

	// Positive twin: a list given the text directly hands the program the text as it is.
	if _, err := zenity.List(text, []string{"a"}); err != nil {
		t.Fatalf("the fake dialog program did not run: %v", err)
	}
	if got, _ := argAfter(runs()[0], "--text"); got != text {
		t.Fatalf("positive twin: the fake does not record the text as given:\n%q", got)
	}
	if err := os.Remove(logFile); err != nil {
		t.Fatal(err)
	}

	choices := []string{"Continue to the wizard", "Remove the scheduled backup"}
	pick, err := textThenList(text, healthListPrompt, choices, zenity.Title(appTitle), zenity.DefaultItems(choices[0]))
	if err != nil || pick != choices[1] {
		t.Fatalf("textThenList = %q, %v; want the list's pick", pick, err)
	}
	got := runs()
	if len(got) != 2 {
		t.Fatalf("the program ran %d times, want 2 (a message, then the list): %q", len(got), got)
	}
	msg, list := got[0], got[1]
	if s, ok := argAfter(msg, "--text"); !ok || s != text {
		t.Errorf("the message's text is not the health text as written:\n got %q\nwant %q", s, text)
	}
	for _, flag := range []string{"--info", "--no-markup"} {
		if !strings.Contains("\036"+strings.Join(msg, "\036")+"\036", "\036"+flag+"\036") {
			t.Errorf("the message dialog lacks %s: %q", flag, msg)
		}
	}
	if list[0] != "--list" {
		t.Errorf("the second dialog is not the list: %q", list)
	}
	if s, ok := argAfter(list, "--text"); !ok || s != healthListPrompt {
		t.Errorf("the list's text is %q, want the fixed prompt %q", s, healthListPrompt)
	}
}
