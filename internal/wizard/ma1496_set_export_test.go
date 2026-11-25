package wizard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// fakeProgress is a progress window that opens nothing: it records its title and texts, and Close
// (the person's Cancel, or the wizard's own close) ends it.
type fakeProgress struct {
	title  string
	mu     sync.Mutex
	texts  []string
	done   chan struct{}
	once   sync.Once
	closed bool
}

func (f *fakeProgress) Text(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, s)
	return nil
}
func (f *fakeProgress) Value(int) error       { return nil }
func (f *fakeProgress) MaxValue() int         { return 100 }
func (f *fakeProgress) Complete() error       { return nil }
func (f *fakeProgress) Done() <-chan struct{} { return f.done }
func (f *fakeProgress) Close() error {
	f.once.Do(func() { close(f.done) })
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

// flowScript stands in for every dialog of a set's whole flow, so no real window opens: the folder
// chooser gives folders in turn; the plan's question is answered Make the archives, each download
// question No, export what's cached, the summary summary (nil is Open the folder); each list is
// answered with its first row (today's default), the date window with nothing; notes are recorded.
// Each progress window is a fakeProgress; onOpen runs as the k-th opens (from 1), before its export,
// and cancel lists the ones the person closes as they open. setOpen and the scheduler are stood in.
type flowScript struct {
	folders   []string
	summary   error
	cancel    map[int]bool
	onOpen    func(k int)
	questions []string
	lists     []string
	notes     []string
	entries   int
	opened    []string
	progress  []*fakeProgress
	openedDir []string
}

func (s *flowScript) install(t *testing.T) {
	t.Helper()
	oldMsg, oldList, oldQ, oldF, oldE, oldErr, oldP, oldOpen := messageDialog, listDialog, questionDialog, folderDialog, entryDialog, errorDialog, progressDialog, setOpen
	t.Cleanup(func() {
		messageDialog, listDialog, questionDialog, folderDialog, entryDialog, errorDialog, progressDialog, setOpen = oldMsg, oldList, oldQ, oldF, oldE, oldErr, oldP, oldOpen
	})
	messageDialog = func(text string, _ ...zenity.Option) error { s.notes = append(s.notes, text); return nil }
	errorDialog = func(text string, _ ...zenity.Option) error { s.notes = append(s.notes, text); return nil }
	listDialog = func(text string, items []string, _ ...zenity.Option) (string, error) {
		s.lists = append(s.lists, text)
		if len(items) == 0 {
			return "", zenity.ErrCanceled
		}
		return items[0], nil
	}
	entryDialog = func(string, ...zenity.Option) (string, error) { s.entries++; return "", nil }
	questionDialog = func(text string, _ ...zenity.Option) error {
		s.questions = append(s.questions, text)
		switch {
		case strings.HasPrefix(text, "MailArchive will make"), strings.HasPrefix(text, "No new archive"):
			return nil
		case strings.HasPrefix(text, "Account "):
			return zenity.ErrCanceled
		}
		return s.summary
	}
	folderDialog = func(...zenity.Option) (string, error) {
		if len(s.folders) == 0 {
			return "", zenity.ErrCanceled
		}
		f := s.folders[0]
		s.folders = s.folders[1:]
		return f, nil
	}
	progressDialog = func(title string) (zenity.ProgressDialog, error) {
		for _, p := range s.progress {
			if !p.closed {
				t.Errorf("%q opened while %q was still open: one archive at a time", title, p.title)
			}
		}
		p := &fakeProgress{title: title, done: make(chan struct{})}
		s.progress = append(s.progress, p)
		s.opened = append(s.opened, title)
		if s.onOpen != nil {
			s.onOpen(len(s.progress))
		}
		if s.cancel[len(s.progress)] {
			_ = p.Close()
		}
		return p, nil
	}
	setOpen = func(p string) { s.openedDir = append(s.openedDir, p) }
}

// summaryOf is the summary the set showed (the last question).
func (s *flowScript) summaryOf(t *testing.T) string {
	t.Helper()
	q := assure.Reached(t, s.questions, "the questions")
	return q[len(q)-1]
}

// mboxOf is n messages of mbox text, each its own Message-ID under tag.
func mboxOf(tag string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "From - Mon Mar  3 09:00:00 2025\nFrom: Alice <alice@example.com>\nTo: Bob <bob@example.com>\nSubject: %s %d\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <%s.%d@qz.example>\nContent-Type: text/plain\n\nbody %d\n\n", tag, i, tag, i, i)
	}
	return b.String()
}

// setMail is a Thunderbird profile with mail in every store: three IMAP accounts (ann@qz-a.example
// with 45 messages, bob@qz-b.example with 3, cy@qz-c.example with 2), Local Folders with 1, and a
// folder no account uses now (imap.old.example, 2). It returns the set's rows in the list's order
// and the profile's folder.
func setMail(t *testing.T) ([]setRow, string) {
	t.Helper()
	lines := []string{`user_pref("mail.accountmanager.accounts", "account1,account2,account3,account4");`}
	lines = append(lines, pickServer("1", "imap", "imap.qz-a.example", "ImapMail/imap.qz-a.example", "ann@qz-a.example")...)
	lines = append(lines, pickServer("2", "imap", "imap.qz-b.example", "ImapMail/imap.qz-b.example", "bob@qz-b.example")...)
	lines = append(lines, pickServer("3", "imap", "imap.qz-c.example", "ImapMail/imap.qz-c.example", "cy@qz-c.example")...)
	lines = append(lines, pickServer("4", "none", "Local Folders", "Mail/Local Folders", "Local Folders")...)
	mail := map[string]int{"ImapMail/imap.qz-a.example": 45, "ImapMail/imap.qz-b.example": 3, "ImapMail/imap.qz-c.example": 2, "Mail/Local Folders": 1, "ImapMail/imap.old.example": 2}
	var stores []pickStore
	for _, rel := range []string{"ImapMail/imap.old.example", "ImapMail/imap.qz-a.example", "ImapMail/imap.qz-b.example", "ImapMail/imap.qz-c.example", "Mail/Local Folders"} {
		stores = append(stores, pickStore{rel, 0})
	}
	// Where auto-detect finds it (the test's own home, ownSettings), so an archive exported with
	// auto-discovery on would hold every mailbox, not its one.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	p := pickProfile(t, filepath.Join(home, ".thunderbird"), "q1w2e3r4.default-release", lines, stores...)
	for rel, n := range mail {
		if err := os.WriteFile(filepath.Join(p, filepath.FromSlash(rel), "INBOX"), []byte(mboxOf(filepath.Base(rel), n)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rows := setRows(listRows(gatherPickRows(storesOf(p, stores), time.Now().Add(time.Minute), sizeEntryMax), "darwin"), "darwin")
	return rows, p
}

// setNames are the rows' folder names, in order.
func setNames(rows []setRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.name)
	}
	return out
}

// holdsOnly reports whether folder is a plaintext archive holding exactly the one store of row r.
func holdsOnly(folder string, r setRow) bool {
	st := archindex.Stores(folder)
	return st.Answer == archindex.StoresArchive && reflect.DeepEqual(st.IDs, []string{state.PathSourceID(r.path)})
}

// madeAt is the made-list's name for path, and whether it is there.
func madeAt(t *testing.T, path string) (string, bool) {
	t.Helper()
	for _, m := range madeLines(t) {
		if m.Path == path {
			return m.Name, true
		}
	}
	return "", false
}

// lastArchive is what last-archive.txt names.
func lastArchive(t *testing.T) string {
	t.Helper()
	f, err := job.LastArchiveFile()
	if err != nil {
		t.Fatal(err)
	}
	return job.ReadLastArchive(f)
}

// leadOK reports whether line starts with one of the leads a finished archive's line may have.
func leadOK(line, label string) bool {
	for _, l := range []string{state.TBLeadComplete, state.TBLeadFinished, state.TBLeadSoFar} {
		if strings.HasPrefix(line, label+": "+l+" ") {
			return true
		}
	}
	return false
}

// covers: MA-1496, S90
// Making the set (docs/design-per-mailbox-archives.md §9, revision 2's C5, C18, C21, C22): one
// archive after another in the plan's order, each through today's export steps with its one input and
// auto-discovery off (each archive holds exactly its own mailbox's store), each new folder made just
// before its own export and in the made-list (with the plan's name) before that export starts, one
// progress window per archive titled "Exporting N of M…" with the counts line and no account's name,
// the shared answers asked once, one summary ("5 archives are ready in …", each archive's line by its
// folder's name with its lead and message count, "saved once." for the leftover, R1's sentences),
// "Open the folder" opening the parent, and last-archive.txt naming the first finished archive that is
// not saved once (no schedule yet). Positive twin first: a fresh parent. Then a re-run into the same
// parent with the made-list unwritable: the one new archive's folder is made, cannot be recorded, is
// taken away again and is not exported into, and the others go on and are brought up to date (the
// lead counts them). Then a name found taken only at make time takes the next free number, the
// summary says the folder's final name and never a refusal's words, the made-list keeps the plan's
// name, and what was there is left as it was.
func TestASetIsMadeOneArchiveAfterAnother(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	if got := setNames(rows); !reflect.DeepEqual(got, []string{"ann@qz-a.example", "bob@qz-b.example", "cy@qz-c.example", "Local Folders", "imap.old.example (not in Thunderbird now)"}) {
		t.Fatalf("the rows: %q", got)
	}
	parent := filepath.Join(t.TempDir(), "Mail archives")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	folder := func(i int) string { return filepath.Join(parent, rows[i].name) }

	s := &flowScript{folders: []string{parent}}
	s.onOpen = func(k int) {
		// The k-th archive's folder is made and in the made-list before its export starts; the next
		// one's folder is not made yet.
		if name, ok := madeAt(t, folder(k-1)); !ok || name != rows[k-1].name {
			t.Errorf("archive %d: its folder is not in the made-list before its export (%q %v)", k, name, ok)
		}
		if k < len(rows) {
			if _, err := os.Lstat(folder(k)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("archive %d's folder was made before archive %d's export", k+1, k)
			}
		}
	}
	s.install(t)
	made, err := makeSet(rows, "darwin")
	if err != nil {
		t.Fatalf("the set: %v", err)
	}
	if len(made) != 5 {
		t.Fatalf("made: %+v", made)
	}
	for i, m := range made {
		if m.Folder != folder(i) || m.Name != rows[i].name || !m.New || m.Outcome != MadeFinished || m.Reason != "" {
			t.Errorf("made[%d] = %+v", i, m)
		}
		if !holdsOnly(folder(i), rows[i]) {
			t.Errorf("%s does not hold exactly its one mailbox: %+v", folder(i), archindex.Stores(folder(i)))
		}
		if fi, err := os.Stat(folder(i)); err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", folder(i), fi, err)
		}
	}
	if want := []string{"Exporting 1 of 5…", "Exporting 2 of 5…", "Exporting 3 of 5…", "Exporting 4 of 5…", "Exporting 5 of 5…"}; !reflect.DeepEqual(s.opened, want) {
		t.Errorf("the progress windows: %q", s.opened)
	}
	assure.Reached(t, s.progress[0].texts, "the counts line of the 45-message archive")
	for _, p := range s.progress {
		for _, x := range p.texts {
			if !strings.HasPrefix(x, "exported ") || strings.Contains(x, "@") || strings.Contains(x, "Local Folders") || strings.Contains(x, "imap.") {
				t.Errorf("a progress text is not the counts line, or names an account: %q", x)
			}
		}
	}
	// The shared answers, once each, after the plan and the download questions.
	if got := strings.Join(s.lists, "|"); got != "Export mode:|"+"Also keep a copy of each original message (.eml) so you can re-import it into a mail program later? (uses more disk space)|"+followLinksQuestion || s.entries != 1 {
		t.Errorf("the shared answers: %q, date window %d", s.lists, s.entries)
	}
	if len(s.questions) != 5 || !strings.HasPrefix(s.questions[1], "Account 1 of 3: ann@qz-a.example") {
		t.Errorf("the questions: %q", s.questions)
	}
	wantSummary := []string{
		"5 archives are ready in " + parent + "\n\n",
		"\nbob@qz-b.example: ", " 3 messages.",
		"\ncy@qz-c.example: ", " 2 messages.",
		"\nLocal Folders: ", " 1 message.",
		"\nimap.old.example (not in Thunderbird now): saved once. 2 messages.\n",
		"\n\n" + setSummaryEnd,
	}
	sum := s.summaryOf(t)
	for _, w := range wantSummary {
		if !strings.Contains(sum, w) {
			t.Errorf("the summary lacks %q:\n%s", w, sum)
		}
	}
	for i, label := range []string{"ann@qz-a.example", "bob@qz-b.example", "cy@qz-c.example", "Local Folders"} {
		found := false
		for _, l := range strings.Split(sum, "\n") {
			found = found || leadOK(l, label)
		}
		if !found {
			t.Errorf("archive %d has no line with its lead:\n%s", i+1, sum)
		}
	}
	if !strings.Contains(sum, "ann@qz-a.example: ") || !strings.Contains(sum, " 45 messages.") {
		t.Errorf("the first archive's count:\n%s", sum)
	}
	if !reflect.DeepEqual(s.openedDir, []string{parent}) {
		t.Errorf("Open the folder opened %q", s.openedDir)
	}
	if got := lastArchive(t); got != folder(0) {
		t.Errorf("last-archive.txt names %q, want the first finished archive %q", got, folder(0))
	}
	d, _ := job.Dir()
	if jobs, _ := filepath.Glob(filepath.Join(d, "*.json")); len(jobs) != 0 {
		t.Errorf("no schedule was asked for, yet job files: %q", jobs)
	}

	// A re-run, the made-list unwritable (a folder at its name): the new archive's folder is made,
	// not recorded, taken away again, and nothing is exported into it; the others go on.
	if err := os.RemoveAll(folder(0)); err != nil {
		t.Fatal(err)
	}
	f, err := job.MadeListFile()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f, 0o700); err != nil {
		t.Fatal(err)
	}
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	made, err = makeSet(rows, "darwin")
	// The leftover saved once is kept as it is (MA-1494's R2), so four archives: one new, three
	// brought up to date.
	if err != nil || len(made) != 4 {
		t.Fatalf("the re-run: %+v %v", made, err)
	}
	if m := made[0]; m.Folder != "" || m.New || m.Outcome != MadeFailed || !strings.Contains(m.Reason, "could not be recorded in the wizard's list of the folders it made") {
		t.Errorf("the archive not recorded: %+v", m)
	}
	if _, err := os.Lstat(folder(0)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the folder made and not recorded is still there: %v", err)
	}
	for i, m := range made[1:] {
		if m.Folder != folder(i+1) || m.New || m.Outcome != MadeFinished {
			t.Errorf("made[%d] on the re-run = %+v", i+1, m)
		}
	}
	sum = s.summaryOf(t)
	for _, w := range []string{
		"3 archives are ready in " + parent + "\n\n",
		"ann@qz-a.example: Did not finish: the new folder could not be recorded in the wizard's list of the folders it made",
		"The mail copied so far is kept; run this again with the same folder to finish it.",
		"\nbob@qz-b.example (brought up to date): ",
	} {
		if !strings.Contains(sum, w) {
			t.Errorf("the re-run's summary lacks %q:\n%s", w, sum)
		}
	}
	if !reflect.DeepEqual(s.opened, []string{"Exporting 2 of 4…", "Exporting 3 of 4…", "Exporting 4 of 4…"}) {
		t.Errorf("the re-run's progress windows: %q", s.opened)
	}
	if len(s.openedDir) != 0 {
		t.Errorf("Close opened the folder")
	}
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, saved, 0o600); err != nil {
		t.Fatal(err)
	}

	// A name taken only at make time (C18): something appears at the planned name after the plan.
	s = &flowScript{folders: []string{parent}}
	s.install(t)
	ch, err := chooseSet(rows, "darwin")
	if err != nil || ch.plan.items[0].outcome != outNew || ch.plan.items[0].folder != rows[0].name {
		t.Fatalf("the plan: %+v %v", outcomesOf(ch.plan), err)
	}
	squatter := folder(0)
	if err := os.Mkdir(squatter, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(squatter, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := treeOf(t, squatter)
	made, err = runSet(ch, "darwin")
	if err != nil || made[0].Name != rows[0].name+" (2)" || made[0].Folder != folder(0)+" (2)" || !made[0].New || made[0].Outcome != MadeFinished {
		t.Fatalf("the renumbered archive: %+v %v", made, err)
	}
	if !holdsOnly(made[0].Folder, rows[0]) || !reflect.DeepEqual(treeOf(t, squatter), before) {
		t.Errorf("the renumbered archive's folder, or what was at its name: %+v", archindex.Stores(made[0].Folder))
	}
	if name, ok := madeAt(t, made[0].Folder); !ok || name != rows[0].name {
		t.Errorf("the made-list keeps the plan's name: %q %v", name, ok)
	}
	sum = s.summaryOf(t)
	if !strings.Contains(sum, "\nann@qz-a.example (2): ") {
		t.Errorf("the summary does not say the final name:\n%s", sum)
	}
	for _, refusal := range []string{"already exists", "could not be made", "choose it with Browse", "Nothing was made"} {
		if strings.Contains(sum, refusal) {
			t.Errorf("the summary says a refusal's words %q:\n%s", refusal, sum)
		}
	}

	// The wiring: the flow's "each" branch runs the set and hands its archives to the caller's
	// hook; no person reaches it until step 4 (perMailboxBuilt stays false); each archive's export
	// refuses an unfinished delete and a keyless archive before its run log opens, as runExport does.
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	setSrc, err := os.ReadFile("set.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "made, err := makeSet(pick.set, runtime.GOOS)\n\t\t\tif madeSet != nil {\n\t\t\t\tmadeSet(made)") || !strings.Contains(string(setSrc), "const perMailboxBuilt = false") {
		t.Error("the flow no longer runs the set and hands its archives on, or a person can reach it before step 4")
	}
	exp, err := os.ReadFile("setexport.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(exp)[strings.Index(string(exp), "func exportSetArchive("):]
	del, keyless, logAt := strings.Index(body, "app.RefuseUnfinishedDelete(c.out"), strings.Index(body, `app.RefuseKeyless("archive into it", c.out)`), strings.Index(body, "newLogger(c.out)")
	if del < 0 || keyless < 0 || logAt < 0 || del > logAt || keyless > logAt {
		t.Errorf("an archive's export opens its log (offset %d) before its refusals (%d, %d)", logAt, del, keyless)
	}
}

// covers: MA-1496
// When a set stops, and when it goes on (§9, revision 2's C29). Positive twin first: an archive whose
// mailbox is gone since the plan fails with its error's first line and its log, and the next
// archives are still made. Cancel on the second archive's progress window stops that archive (it
// keeps what it copied) and starts no more: the later new archives' folders are never made and the
// made-list never names them. An error about the parent itself stops the set: the parent gone when the
// first folder is made (nothing is made anywhere, and every archive says it was not started), and the
// parent gone during the second archive's export (the archives after it are not started).
func TestCancelOrAParentErrorStopsTheSetAndAnErrorInOneGoesOn(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	fresh := func() string {
		p := filepath.Join(t.TempDir(), "Mail archives")
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	plan := func(parent string, s *flowScript) setChosen {
		t.Helper()
		s.folders = []string{parent}
		s.install(t)
		ch, err := chooseSet(rows, "darwin")
		if err != nil {
			t.Fatal(err)
		}
		return ch
	}
	inMadeList := func(paths ...string) bool {
		for _, p := range paths {
			if _, ok := madeAt(t, p); ok {
				return true
			}
		}
		return false
	}

	// Positive twin: an error in one goes on to the next.
	parent := fresh()
	s := &flowScript{summary: zenity.ErrCanceled}
	ch := plan(parent, s)
	gone := rows[1].path
	keep := gone + ".kept"
	if err := os.Rename(gone, keep); err != nil {
		t.Fatal(err)
	}
	made, err := runSet(ch, "darwin")
	if err := os.Rename(keep, gone); err != nil {
		t.Fatal(err)
	}
	if err != nil || len(made) != 5 || made[1].Outcome != MadeFailed || made[1].Reason == "" {
		t.Fatalf("the gone mailbox: %+v %v", made, err)
	}
	for _, i := range []int{0, 2, 3, 4} {
		if made[i].Outcome != MadeFinished {
			t.Errorf("an error in archive 2 stopped archive %d: %+v", i+1, made[i])
		}
	}
	sum := s.summaryOf(t)
	if !strings.Contains(sum, "bob@qz-b.example: Did not finish: "+made[1].Reason+". The mail copied so far is kept; run this again with the same folder to finish it. Log: "+filepath.Join(parent, "bob@qz-b.example", "mailarchive.log")) || !strings.HasPrefix(sum, "4 archives are ready in ") {
		t.Errorf("the failed archive's line:\n%s", sum)
	}

	// Cancel on the second archive's progress window.
	parent = fresh()
	s = &flowScript{summary: zenity.ErrCanceled, cancel: map[int]bool{2: true}}
	ch = plan(parent, s)
	made, err = runSet(ch, "darwin")
	later := []string{filepath.Join(parent, rows[2].name), filepath.Join(parent, rows[3].name), filepath.Join(parent, rows[4].name)}
	code, msg := 0, ""
	if err == nil && len(made) == 5 && made[0].Outcome == MadeFinished && made[1].Outcome == MadeStopped && len(s.opened) == 2 {
		code, msg = 1, s.summaryOf(t)
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("1 archive is ready in "+parent, "bob@qz-b.example: Did not finish: the export was cancelled. The mail copied so far is kept; run this again with the same folder to finish it.",
			"cy@qz-c.example: Not started. Run this again with the same folder to back it up.", "imap.old.example (not in Thunderbird now): Not started."),
		assure.NoSideEffect(func() bool {
			for _, p := range later {
				if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
					return false
				}
			}
			return !inMadeList(later...) && made[2].Folder == "" && made[2].Outcome == MadeNotStarted
		}))
	if _, err := os.Stat(filepath.Join(parent, rows[1].name)); err != nil {
		t.Errorf("the stopped archive's folder is not kept: %v", err)
	}

	// The parent gone before the first folder is made: nothing is made anywhere.
	parent = fresh()
	s = &flowScript{summary: zenity.ErrCanceled}
	ch = plan(parent, s)
	moved := parent + " moved"
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	listBefore := madeLines(t)
	made, err = runSet(ch, "darwin")
	code, msg = 0, ""
	if err == nil && len(made) == 5 && made[0].Outcome == MadeFailed && len(s.opened) == 0 {
		code, msg = 1, s.summaryOf(t)
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("No archive was finished in "+parent, "ann@qz-a.example: Did not finish: "+parent, "bob@qz-b.example: Not started."),
		assure.Forbid("Log: "),
		assure.NoSideEffect(func() bool {
			_, gone := os.Lstat(parent)
			kids, _ := os.ReadDir(moved)
			for _, m := range made[1:] {
				if m.Outcome != MadeNotStarted || m.Folder != "" {
					return false
				}
			}
			return errors.Is(gone, os.ErrNotExist) && len(kids) == 0 && reflect.DeepEqual(madeLines(t), listBefore)
		}))

	// The parent gone during the second archive's export: the archives after it are not started.
	parent = fresh()
	s = &flowScript{summary: zenity.ErrCanceled}
	ch = plan(parent, s)
	s.onOpen = func(k int) {
		if k == 2 {
			if err := os.Rename(parent, parent+" gone"); err != nil {
				t.Error(err)
			}
		}
	}
	made, err = runSet(ch, "darwin")
	if err != nil || len(s.opened) != 2 || made[0].Outcome != MadeFinished || made[1].Outcome != MadeFailed {
		t.Fatalf("the parent gone during an export: %+v %v %q", made, err, s.opened)
	}
	for _, m := range made[2:] {
		if m.Outcome != MadeNotStarted || m.Folder != "" {
			t.Errorf("an archive after the parent went was started: %+v", m)
		}
	}
	if kids, _ := os.ReadDir(parent + " gone"); len(kids) != 2 {
		t.Errorf("folders made after the parent went: %v", kids)
	}
}

// arcOf is one archive of a set for the summary's own tests: its folder's name, its plan outcome and
// what became of it.
func arcOf(name string, o setOutcome, out MadeOutcome, r app.Result) *setArchive {
	return &setArchive{it: setItem{row: setRow{name: name}, outcome: o, folder: name}, made: Made{Name: name, Outcome: out}, res: r}
}

// covers: MA-1496, X6
// The set's summary (§9, revision 2's C21, revision 2.1's F-10c): the design's example word for word
// (addresses invented), with its lead counting the archive brought up to date; the singular and zero
// forms of the lead ("1 archive is ready in …", "No archive was finished in …"); each archive's lead
// being that archive's own (tbcomplete.RunLead's: an account behind says "Backed up what Thunderbird
// has so far."), followed by its Thunderbird account lines (state.TBLines); each count the
// one-archive summary shows, with its remedy, only when above zero and in exportSummaryFor's very
// words; a stopped, a failed and a not-started archive's lines.
func TestASetsSummaryKeepsEachArchivesCounts(t *testing.T) {
	once := arcOf("imap.gmail.com (not in Thunderbird now)", outNew, MadeFinished, app.Result{ManifestSize: 3311})
	once.it.row.once = true
	example := []*setArchive{
		arcOf("qzsam.walker@gmail.com", outNew, MadeFinished, app.Result{ManifestSize: 48210}),
		arcOf("sam.walker@contoso.example", outNew, MadeFinished, app.Result{ManifestSize: 9870, Fillable: 12}),
		arcOf("Local Folders", outNew, MadeFinished, app.Result{ManifestSize: 1204}),
		once,
		arcOf("sam@old.example", outContinue, MadeFinished, app.Result{ManifestSize: 2045}),
	}
	want := `5 archives are ready in D:\Mail archives

qzsam.walker@gmail.com: Backup complete. 48,210 messages.
sam.walker@contoso.example: Backup complete. 9,870 messages. Not fully downloaded yet: 12 — open these in your mail app so it downloads them, then run this again to add them.
Local Folders: Backup complete. 1,204 messages.
imap.gmail.com (not in Thunderbird now): saved once. 3,311 messages.
sam@old.example (brought up to date): Backup complete. 2,045 messages.

Each archive is its own folder: you can hand over or delete one without touching the others. Open index.html in any of them to browse. MailArchive Desktop lists them all under Your archives; a row whose last backup did not finish says so.`
	if got := setSummaryText(`D:\Mail archives`, example); got != want {
		t.Fatalf("the design's example:\n got %q\nwant %q", got, want)
	}

	// Singular and zero forms; a stopped, a failed and a not-started archive.
	stopped := arcOf("b@x", outNew, MadeStopped, app.Result{})
	stopped.made.Reason = setCancelled
	failed := arcOf("c@x", outContinue, MadeFailed, app.Result{})
	failed.made.Reason = "input /m/c does not exist"
	notStarted := arcOf("d@x", outNew, MadeNotStarted, app.Result{})
	one := setSummaryText("/m", []*setArchive{arcOf("a@x", outNew, MadeFinished, app.Result{ManifestSize: 1}), stopped, failed, notStarted})
	for _, w := range []string{
		"1 archive is ready in /m\n\na@x: Backup complete. 1 message.\n",
		"\nb@x: Did not finish: the export was cancelled. The mail copied so far is kept; run this again with the same folder to finish it.\n",
		"\nc@x (brought up to date): Did not finish: input /m/c does not exist. The mail copied so far is kept; run this again with the same folder to finish it.\n",
		"\nd@x: Not started. Run this again with the same folder to back it up.\n\n",
	} {
		if !strings.Contains(one, w) {
			t.Errorf("the summary lacks %q:\n%s", w, one)
		}
	}
	if none := setSummaryText("/m", []*setArchive{notStarted}); !strings.HasPrefix(none, "No archive was finished in /m\n\n") {
		t.Errorf("the zero form: %q", none)
	}

	// An archive's own lead and its account lines.
	behind := app.Result{ManifestSize: 3, Thunderbird: []tbcomplete.Result{{Account: "server1", Name: "e@x", StoreID: "path:/p/ImapMail/imap.x", Verdict: tbcomplete.Behind, OnDisk: 3, Listed: 40, Missing: 37, Tolerance: 1}}}
	accts, more := app.TBAccounts(behind)
	tb := assure.Reached(t, state.TBLines(accts, more, state.TBSay{}), "the behind account's lines")
	got := setSummaryText("/m", []*setArchive{arcOf("e@x", outNew, MadeFinished, behind)})
	if !strings.Contains(got, "\ne@x: "+state.TBLeadSoFar+" 3 messages.\n"+strings.Join(tb, "\n")+"\n\n") {
		t.Errorf("the archive's lead or its account lines:\n%s\nwant its lines %q", got, tb)
	}

	// Every count, each in exportSummaryFor's words, only when above zero.
	if s := setCountSentences(app.Result{ManifestSize: 9}); len(s) != 0 {
		t.Errorf("counts of zero said: %q", s)
	}
	for _, r := range []app.Result{
		{Fillable: 4, Unknown: 2, FillableParseGap: 1, Terminal: 3, Undecodable: 5, FidelityFallback: 6, IndexErrors: 7, ReportPath: "/m/a/attachments-report.tsv"},
		{Fillable: 2, FillableOnlyParseGap: 2, FillableParseGap: 2, Unknown: 1},
		{Fillable: 1, FillableParseGap: 1},
	} {
		one := exportSummaryFor("/m/a", false, r, "darwin")
		sentences := assure.Reached(t, setCountSentences(r), "the counts")
		for _, s := range sentences {
			if !strings.Contains(one, strings.TrimSuffix(s, ".")) {
				t.Errorf("%q is not the one-archive summary's words:\n%s", s, one)
			}
		}
		// Not downloaded (with those from an older archive), cut short, also kept as received, empty
		// at the source, kept as received, an earlier version's, the details and the index's warning.
		if r.Terminal > 0 && len(sentences) != 8 {
			t.Errorf("every count above zero is said: %q", sentences)
		}
	}
}
