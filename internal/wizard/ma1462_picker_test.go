package wizard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/source"
)

// pickStore is one mailbox folder of a test profile: its path under the profile, and the bytes of
// its INBOX mbox file. Every store also gets an INBOX.msf of pickMsfBytes, which the size leaves out.
type pickStore struct {
	rel   string
	bytes int64
}

const pickMsfBytes = 777_777

// pickProfile makes the Thunderbird profile folder name under root (prefs.js holding lines, as
// Thunderbird writes it, and compatibility.ini) with stores, and returns it.
func pickProfile(t *testing.T, root, name string, lines []string, stores ...pickStore) string {
	t.Helper()
	p := filepath.Join(root, name)
	for _, s := range stores {
		dir := filepath.Join(p, filepath.FromSlash(s.rel))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		pickFile(t, filepath.Join(dir, "INBOX"), s.bytes)
		pickFile(t, filepath.Join(dir, "INBOX.msf"), pickMsfBytes)
	}
	body := "// Mozilla User Preferences\n\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(p, "prefs.js"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCompat(t, p, "[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\n")
	return p
}

// pickFile makes the file path of n bytes.
func pickFile(t *testing.T, path string, n int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(n); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// pickServer is the prefs.js lines of one live account: accountN, serverN, its type, host and
// folder, and its name when name is not "".
func pickServer(n, typ, host, folder, name string) []string {
	lines := []string{
		`user_pref("mail.account.account` + n + `.server", "server` + n + `");`,
		`user_pref("mail.server.server` + n + `.directory-rel", "[ProfD]` + folder + `");`,
		`user_pref("mail.server.server` + n + `.hostname", "` + host + `");`,
		`user_pref("mail.server.server` + n + `.type", "` + typ + `");`,
	}
	if name != "" {
		lines = append(lines, `user_pref("mail.server.server`+n+`.name", "`+name+`");`)
	}
	return lines
}

// ownerShaped is the prefs.js of a profile shaped like the owner's (2026-11-18): three accounts on
// Google's server, kept by Thunderbird in imap.gmail.com, imap.gmail-1.com and imap.gmail-2.com (one a
// Workspace address on its own domain), one on the person's own domain, one at a university's
// Microsoft 365, and Local Folders. Every address is invented. named false leaves out every name.
func ownerShaped(named bool) []string {
	n := func(s string) string {
		if named {
			return s
		}
		return ""
	}
	var lines []string
	lines = append(lines, `user_pref("mail.accountmanager.accounts", "account1,account2,account3,account4,account5,account6");`)
	lines = append(lines, pickServer("1", "imap", "imap.gmail.com", "ImapMail/imap.gmail.com", n("qzwork@qzwork.example"))...)
	lines = append(lines, pickServer("2", "imap", "imap.gmail.com", "ImapMail/imap.gmail-1.com", n("qzsam.walker@gmail.com"))...)
	lines = append(lines, pickServer("3", "imap", "imap.gmail.com", "ImapMail/imap.gmail-2.com", n("qzsam.work@gmail.com"))...)
	lines = append(lines, pickServer("4", "imap", "mail.qzname.example", "ImapMail/mail.qzname.example", n("qzsam@qzname.example"))...)
	lines = append(lines, pickServer("5", "imap", "outlook.office365.com", "ImapMail/outlook.office365.com", n("qz.student@mail.qzuni.example"))...)
	lines = append(lines, pickServer("6", "none", "Local Folders", "Mail/Local Folders", n("Local Folders"))...)
	return lines
}

// ownerStores are ownerShaped's folders, in the order discovery lists them (A to Z by path).
var ownerStores = []pickStore{
	{"ImapMail/imap.gmail-1.com", 1_400_000},
	{"ImapMail/imap.gmail-2.com", 0},
	{"ImapMail/imap.gmail.com", 6_300_000},
	{"ImapMail/mail.qzname.example", 480_000},
	{"ImapMail/outlook.office365.com", 12_400_000},
	{"Mail/Local Folders", 0},
}

// storesOf are the paths of stores in profile p.
func storesOf(p string, stores []pickStore) []string {
	var out []string
	for _, s := range stores {
		out = append(out, filepath.Join(p, filepath.FromSlash(s.rel)))
	}
	return out
}

// labelsOf words found as the list does, with macOS's and Linux's size words, and gives the paths in
// the list's order too.
func labelsOf(t *testing.T, found []string) ([]string, []string) {
	t.Helper()
	return pickList(gatherPickRows(found, time.Now().Add(time.Minute), sizeEntryMax), "darwin")
}

// pickScript replaces the list and note dialogs for one test: the list gives answers in turn
// (an error, when one is given, after the answers run out), the note gives noteErrs in turn (nil
// after them). It records every dialog shown.
type pickScript struct {
	answers  []string
	endErr   error
	noteErrs []error
	lists    []dialogCall
	notes    []string
}

func (s *pickScript) install(t *testing.T) {
	t.Helper()
	oldMsg, oldList := messageDialog, listDialog
	t.Cleanup(func() { messageDialog, listDialog = oldMsg, oldList })
	listDialog = func(text string, items []string, _ ...zenity.Option) (string, error) {
		s.lists = append(s.lists, dialogCall{kind: "list", text: text, items: append([]string(nil), items...)})
		if len(s.answers) == 0 {
			if s.endErr != nil {
				return "", s.endErr
			}
			return "", nil
		}
		a := s.answers[0]
		s.answers = s.answers[1:]
		return a, nil
	}
	messageDialog = func(text string, _ ...zenity.Option) error {
		s.notes = append(s.notes, text)
		if len(s.noteErrs) == 0 {
			return nil
		}
		e := s.noteErrs[0]
		s.noteErrs = s.noteErrs[1:]
		return e
	}
}

// covers: MA-1462
// The list says each Thunderbird account by the name Thunderbird shows for it, read with the same
// strict read of prefs.js and user.js every backup makes (ReadStoreFacts), with how much of it is on
// this computer: so the three accounts on Google's server, kept in imap.gmail.com, imap.gmail-1.com
// and imap.gmail-2.com, read as their three addresses. Mail accounts come first, A to Z; Local
// Folders after them. Picking a row gives that store with auto off; picking All gives every store
// found, in discovery's order, with auto on. Positive twin first: the same profile with no name lines
// reads its folders' names, as before, now with sizes (the .msf summaries left out).
func TestEachRowSaysTheAccountAsThunderbirdShowsIt(t *testing.T) {
	root := t.TempDir()
	plain := pickProfile(t, root, "a1b2c3d4.default-release", ownerShaped(false), ownerStores...)
	got, _ := labelsOf(t, storesOf(plain, ownerStores))
	want := []string{
		"imap.gmail-1.com (1.4 MB)",
		"imap.gmail-2.com (nothing downloaded yet)",
		"imap.gmail.com (6.3 MB)",
		"mail.qzname.example (under 1 MB)",
		"outlook.office365.com (12 MB)",
		"Local Folders (empty)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("positive twin: no name lines read their folders:\n got %q\nwant %q", got, want)
	}

	named := pickProfile(t, t.TempDir(), "a1b2c3d4.default-release", ownerShaped(true), ownerStores...)
	found := storesOf(named, ownerStores)
	got, paths := labelsOf(t, found)
	want = []string{
		"qz.student@mail.qzuni.example (12 MB)",
		"qzsam.walker@gmail.com (1.4 MB)",
		"qzsam.work@gmail.com (nothing downloaded yet)",
		"qzsam@qzname.example (under 1 MB)",
		"qzwork@qzwork.example (6.3 MB)",
		"Local Folders (empty)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the rows say the accounts' names:\n got %q\nwant %q", got, want)
	}
	wantPaths := []string{found[4], found[0], found[1], found[3], found[2], found[5]}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("each row stands for its own store:\n got %q\nwant %q", paths, wantPaths)
	}

	// Through the wizard's own step, with a recorder for the list: no real dialog.
	labels, _ := pickList(gatherPickRows(found, time.Now().Add(time.Minute), sizeEntryMax), runtime.GOOS)
	s := &pickScript{answers: []string{labels[1]}}
	s.install(t)
	inputs, auto, err := pickAutoInputs(found)
	if err != nil || auto || !reflect.DeepEqual(inputs, []string{found[0]}) {
		t.Fatalf("picking qzsam.walker@gmail.com gave %q auto=%v %v, want imap.gmail-1.com's store", inputs, auto, err)
	}
	if len(s.lists) != 1 || s.lists[0].text != pickPrompt || !reflect.DeepEqual(s.lists[0].items, append([]string{allOfThem}, labels...)) {
		t.Fatalf("the list shown: %#v", s.lists)
	}
	if !strings.HasPrefix(s.lists[0].items[2], "qzsam.walker@gmail.com (") {
		t.Errorf("the list shown does not say the address: %q", s.lists[0].items)
	}
	s = &pickScript{answers: []string{allOfThem}}
	s.install(t)
	inputs, auto, err = pickAutoInputs(found)
	if err != nil || !auto || !reflect.DeepEqual(inputs, found) {
		t.Fatalf("All of them gave %q auto=%v %v, want every store found, auto on", inputs, auto, err)
	}
	// One store found: no list, as before.
	s = &pickScript{}
	s.install(t)
	if inputs, auto, err := pickAutoInputs(found[:1]); err != nil || auto || !reflect.DeepEqual(inputs, found[:1]) || len(s.lists) != 0 {
		t.Fatalf("one store: %q %v %v, lists %d", inputs, auto, err, len(s.lists))
	}
}

// covers: MA-1462
// A profile whose prefs.js or user.js MailArchive cannot read with certainty (a line Thunderbird
// never writes) gives no names: every row of it says its folder and size, the list still opens, and
// no row is said to be not in Thunderbird. Positive twin first: the same profile without that line
// says the names.
func TestASettingsFileNotReadWithCertaintyGivesFolderWords(t *testing.T) {
	ok := pickProfile(t, t.TempDir(), "p.default", ownerShaped(true), ownerStores...)
	got, _ := labelsOf(t, storesOf(ok, ownerStores))
	if got[0] != "qz.student@mail.qzuni.example (12 MB)" {
		t.Fatalf("positive twin: the names are said: %q", got)
	}
	lines := append(ownerShaped(true), `user_pref("mail.server.server9.name", "x"); // a comment`)
	odd := pickProfile(t, t.TempDir(), "p.default", lines, ownerStores...)
	got, _ = labelsOf(t, storesOf(odd, ownerStores))
	// No account can be told, so none is Local Folders either: every row is a Thunderbird folder
	// whose account cannot be told, A to Z by its folder's name.
	want := []string{
		"imap.gmail-1.com (1.4 MB)",
		"imap.gmail-2.com (nothing downloaded yet)",
		"imap.gmail.com (6.3 MB)",
		"Local Folders (empty)",
		"mail.qzname.example (under 1 MB)",
		"outlook.office365.com (12 MB)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a file read without certainty: every row says its folder:\n got %q\nwant %q", got, want)
	}
}

// covers: MA-1463
// A folder no live Thunderbird account uses now (a removed account's, or the old folder of an
// account added again, which Thunderbird keeps in a new imap.gmail-1.com) starts "Not in Thunderbird
// now:" and is listed after the folders in use, only when the strict match says so for certain. A
// leftover named like the host of two live accounts could be either's, so it makes no claim and says
// its folder. Positive twin first: the live folder's row says its name.
func TestAFolderNoAccountUsesSaysNotInThunderbird(t *testing.T) {
	stores := []pickStore{{"ImapMail/imap.gmail-1.com", 1_400_000}, {"ImapMail/imap.gmail.com", 6_300_000}, {"Mail/Local Folders", 0}}
	lines := []string{`user_pref("mail.accountmanager.accounts", "account1,account2");`}
	lines = append(lines, pickServer("1", "imap", "imap.gmail.com", "ImapMail/imap.gmail-1.com", "qzsam.walker@gmail.com")...)
	lines = append(lines, pickServer("2", "none", "Local Folders", "Mail/Local Folders", "Local Folders")...)
	p := pickProfile(t, t.TempDir(), "p.default", lines, stores...)
	found := storesOf(p, stores)
	got, paths := labelsOf(t, found)
	want := []string{
		"qzsam.walker@gmail.com (1.4 MB)",
		"Local Folders (empty)",
		"Not in Thunderbird now: imap.gmail.com (6.3 MB)",
	}
	if got[0] != want[0] {
		t.Fatalf("positive twin: the live folder says its name: %q", got)
	}
	if !reflect.DeepEqual(got, want) || paths[2] != found[1] {
		t.Fatalf("the leftover:\n got %q (%q)\nwant %q", got, paths, want)
	}

	// Two live accounts on imap.gmail.com kept in imap.gmail-1.com and imap.gmail-2.com: the leftover
	// imap.gmail.com could be either's by its name, so it says only its folder.
	stores = []pickStore{{"ImapMail/imap.gmail-1.com", 1_400_000}, {"ImapMail/imap.gmail-2.com", 0}, {"ImapMail/imap.gmail.com", 6_300_000}}
	lines = []string{`user_pref("mail.accountmanager.accounts", "account1,account2");`}
	lines = append(lines, pickServer("1", "imap", "imap.gmail.com", "ImapMail/imap.gmail-1.com", "qzsam.walker@gmail.com")...)
	lines = append(lines, pickServer("2", "imap", "imap.gmail.com", "ImapMail/imap.gmail-2.com", "qzsam.work@gmail.com")...)
	p = pickProfile(t, t.TempDir(), "p.default", lines, stores...)
	got, _ = labelsOf(t, storesOf(p, stores))
	want = []string{"imap.gmail.com (6.3 MB)", "qzsam.walker@gmail.com (1.4 MB)", "qzsam.work@gmail.com (nothing downloaded yet)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("a leftover two live accounts' host could fit says only its folder, with no claim:\n got %q\nwant %q", got, want)
	}
}

// covers: MA-1462, MA-1288
// The list's words are shown, never written: picking a row named by its address gives its path, the
// job the wizard writes holds that path (or auto-discovery) and no address, and the store's own name,
// which a new archive's folder and the run's log line take (StoreName), is still its folder's.
// Positive twin first: the address is in the rows the list was given.
func TestThePickersWordsAreWrittenNowhere(t *testing.T) {
	p := pickProfile(t, t.TempDir(), "p.default", ownerShaped(true), ownerStores...)
	found := storesOf(p, ownerStores)
	labels, _ := pickList(gatherPickRows(found, time.Now().Add(time.Minute), sizeEntryMax), runtime.GOOS)
	s := &pickScript{answers: []string{labels[1]}}
	s.install(t)
	inputs, auto, err := pickAutoInputs(found)
	if err != nil || len(inputs) != 1 {
		t.Fatalf("pick: %q %v", inputs, err)
	}
	if !strings.Contains(strings.Join(s.lists[0].items, "\n"), "qzsam.walker@gmail.com") {
		t.Fatalf("positive twin: the rows given to the list say the address: %q", s.lists[0].items)
	}
	data, err := json.Marshal(jobFor("qz", wizardChoice{inputs: inputs, auto: auto, out: t.TempDir()}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "@") || !strings.Contains(string(data), "imap.gmail-1.com") {
		t.Errorf("the job must hold the path and no address: %s", data)
	}
	src, err := source.Open(inputs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if name := src.StoreName(); name != "imap.gmail-1.com" {
		t.Errorf("the store's own name (the archive folder's and the log line's) is %q, want its folder imap.gmail-1.com", name)
	}
}

// pickRefusal is the picker's refusal as assure.Refused reads it: 1 when the step ended cancelled and
// gave no mailbox, else 0.
func pickRefusal(inputs []string, err error) int {
	if errors.Is(err, zenity.ErrCanceled) && inputs == nil {
		return 1
	}
	return 0
}

// covers: MA-1465
// An answer the list does not hold is never taken as a path: "" (OK with nothing selected, as Linux's
// list allows) or any other text shows a fixed note, then the list again; closing the note with its
// close button shows the list again too; Cancel ends with no mailbox. A dialog program that keeps
// answering nothing ends the step after pickTries lists, with plain words and no mailbox. The list
// is asked with its fixed prompt, All preselected and an empty pick refused where the dialog can
// (Windows and macOS). Positive twin first: answering a row gives its path.
func TestAnAnswerTheListDoesNotHoldIsAskedAgain(t *testing.T) {
	p := pickProfile(t, t.TempDir(), "p.default", ownerShaped(true), ownerStores...)
	found := storesOf(p, ownerStores)
	labels, paths := pickList(gatherPickRows(found, time.Now().Add(time.Minute), sizeEntryMax), runtime.GOOS)

	s := &pickScript{answers: []string{labels[3]}}
	s.install(t)
	if inputs, _, err := pickAutoInputs(found); err != nil || !reflect.DeepEqual(inputs, []string{paths[3]}) || len(s.notes) != 0 {
		t.Fatalf("positive twin: a row's answer: %q %v (notes %q)", inputs, err, s.notes)
	}

	s = &pickScript{answers: []string{"", labels[2]}}
	s.install(t)
	inputs, _, err := pickAutoInputs(found)
	if err != nil || !reflect.DeepEqual(inputs, []string{paths[2]}) || !reflect.DeepEqual(s.notes, []string{pickAgain}) || len(s.lists) != 2 {
		t.Fatalf("an empty answer, then a row: %q %v, notes %q, lists %d", inputs, err, s.notes, len(s.lists))
	}

	s = &pickScript{answers: []string{"no such row", labels[0]}, noteErrs: []error{zenity.ErrCanceled}}
	s.install(t)
	if inputs, _, err := pickAutoInputs(found); err != nil || !reflect.DeepEqual(inputs, []string{paths[0]}) || len(s.lists) != 2 {
		t.Fatalf("closing the note shows the list again: %q %v, lists %d", inputs, err, len(s.lists))
	}

	// A settings folder of this test's own, so a job file written by any other test is not seen.
	home := t.TempDir()
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "AppData", "LOCALAPPDATA"} {
		t.Setenv(v, home)
	}
	s = &pickScript{answers: []string{"/etc/passwd"}, endErr: zenity.ErrCanceled}
	s.install(t)
	inputs, _, err = pickAutoInputs(found)
	note := strings.Join(s.notes, "\n")
	assure.Refused(t, pickRefusal(inputs, err), note, assure.Code(1), assure.Names("Choose one line in the list"),
		assure.NoSideEffect(func() bool {
			d, derr := job.Dir()
			jobs, _ := filepath.Glob(filepath.Join(d, "*.json"))
			return inputs == nil && derr == nil && len(jobs) == 0
		}))

	s = &pickScript{}
	s.install(t)
	inputs, _, err = pickAutoInputs(found)
	if !errors.Is(err, errNoPick) || inputs != nil || len(s.lists) != pickTries || len(s.notes) != pickTries-1 {
		t.Fatalf("a program that answers nothing: %q %v, lists %d, notes %d", inputs, err, len(s.lists), len(s.notes))
	}

	// The list's own call: its fixed prompt, All preselected, an empty pick refused (Windows, macOS).
	src, err := os.ReadFile("picker.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "listDialog(pickPrompt, items, zenity.Title(appTitle), zenity.DefaultItems(allOfThem), zenity.DisallowEmpty())") {
		t.Errorf("pickAutoInputs no longer asks with its prompt, All preselected and DisallowEmpty")
	}
}
