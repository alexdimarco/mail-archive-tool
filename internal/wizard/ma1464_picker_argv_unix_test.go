//go:build !windows && !darwin

package wizard

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// covers: MA-1464
// On Linux and the BSDs the list's rows reach the dialog program (qarma, zenity or matedialog) as
// arguments after --column=, with no end-of-options marker (ncruces/zenity v0.10.12 list_unix.go), so
// no row may start with "-": through the real module, run against fake programs that record their
// arguments (MA-1286's), an account named "--text=x" and a folder named "-x" arrive as rows that
// start with a lead word, --text is the fixed prompt, and the rows are exactly the All row and the
// list's own. Positive twin first: the fake records the rows it is given, in order.
func TestNoPickerRowReachesTheDialogProgramAsAnOption(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"qarma", "zenity", "matedialog"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(fakeDialogTool), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logFile := filepath.Join(t.TempDir(), "argv")
	t.Setenv("PATH", bin)
	t.Setenv("MA1286_LOG", logFile)
	t.Setenv("MA1286_PICK", allOfThem)
	lastList := func() []string {
		t.Helper()
		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatal(err)
		}
		runs := strings.Split(strings.TrimSuffix(string(data), "\035"), "\035")
		return strings.Split(strings.TrimSuffix(runs[len(runs)-1], "\036"), "\036")
	}
	rowsAfterColumn := func(args []string) []string {
		for i, a := range args {
			if a == "--column=" {
				return args[i+1:]
			}
		}
		t.Fatalf("no --column= in %q", args)
		return nil
	}

	lines := []string{`user_pref("mail.accountmanager.accounts", "account1,account2");`}
	lines = append(lines, pickServer("1", "imap", "imap.gmail.com", "ImapMail/imap.gmail.com", "qzsam.walker@gmail.com")...)
	lines = append(lines, pickServer("2", "imap", "imap.qz.example", "ImapMail/imap.qz.example", "--text=x")...)
	stores := []pickStore{{"ImapMail/imap.gmail.com", 1_400_000}, {"ImapMail/imap.qz.example", 0}, {"Mail/-x", 0}}
	p := pickProfile(t, t.TempDir(), "p.default", lines, stores...)
	found := storesOf(p, stores)
	// A mail folder outside any Thunderbird profile, named "-y", is said by its folder's name.
	other := filepath.Join(t.TempDir(), "-y")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	pickFile(t, filepath.Join(other, "INBOX"), 0)
	found = append(found, other)
	labels, _ := pickList(gatherPickRows(found, time.Now().Add(time.Minute), sizeEntryMax), "linux")

	// Positive twin: the fake records exactly the rows given.
	inputs, auto, err := pickListAlone(found)
	if err != nil || !auto || !reflect.DeepEqual(inputs, found) {
		t.Fatalf("All of them through the fake program: %q %v %v", inputs, auto, err)
	}
	args := lastList()
	if got := rowsAfterColumn(args); !reflect.DeepEqual(got, append([]string{allOfThem}, labels...)) {
		t.Fatalf("positive twin: the program was given\n%q\nwant\n%q", got, append([]string{allOfThem}, labels...))
	}
	if s, ok := argAfter(args, "--text"); !ok || s != pickPrompt {
		t.Errorf("the list's text is %q, want the fixed prompt", s)
	}
	want := []string{"Account --text=x (nothing downloaded yet)", "qzsam.walker@gmail.com (1.4 MB)", "Folder -y (empty)", "Not in Thunderbird now: -x (empty)"}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("the rows:\n got %q\nwant %q", labels, want)
	}
	for _, row := range rowsAfterColumn(args) {
		if strings.HasPrefix(row, "-") {
			t.Errorf("a row reaches the dialog program as an option: %q", row)
		}
	}
}
