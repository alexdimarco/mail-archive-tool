package source

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// localMail is a minimal message with a Message-ID.
func localMail(id string) string {
	return "From: a@example.com\r\nSubject: " + id + "\r\nMessage-ID: <" + id + "@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody " + id + "\r\n"
}

func writeLocal(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// walked is what one walk of a local store delivered.
type walked struct {
	subjects []string // "folder: subject", sorted
	msgs     []*model.Message
	faults   []Fault
	err      error
}

func (w walked) fault(reason string) (Fault, bool) {
	for _, f := range w.faults {
		if f.Reason == reason {
			return f, true
		}
	}
	return Fault{}, false
}

func (w walked) count(reason string) int {
	n := 0
	for _, f := range w.faults {
		if f.Reason == reason {
			n++
		}
	}
	return n
}

// walkLocal opens path and walks it with faults delivered through the callback, under
// a 5 s guard: a walk that blocks is a failure of containment.
func walkLocal(t *testing.T, path string, opts OpenOptions) walked {
	t.Helper()
	src, err := OpenWith(path, opts)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer src.Close()
	fw, ok := src.(FaultWalker)
	if !ok {
		t.Fatalf("%T is not a FaultWalker: a local reader reports what it cannot read (FC29)", src)
	}
	var out walked
	done := make(chan struct{})
	go func() {
		defer close(done)
		out.err = fw.WalkWithFaults(func(folder []string, m *model.Message) error {
			out.subjects = append(out.subjects, strings.Join(folder, "/")+": "+m.Subject)
			out.msgs = append(out.msgs, m)
			return nil
		}, WalkOptions{Faults: func(f Fault) { out.faults = append(out.faults, f) }})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("the walk of %s did not finish: a special file or a link loop was waited on or followed", path)
	}
	sort.Strings(out.subjects)
	return out
}

// covers: MA-322, R25, R10
// FC1's classifier, on any OS: what a directory entry's mode is on goos. Nothing that
// might hold mail is classed as a special file (info); a Windows reparse file is a
// directory link when Readlink answers, and a file to read when it does not.
func TestClassifyModeTable(t *testing.T) {
	cases := []struct {
		name       string
		goos       string
		mode       fs.FileMode
		readlinkOK bool
		want       entryKind
	}{
		{"regular", "linux", 0o644, false, kindFile},
		{"directory", "linux", fs.ModeDir | 0o755, false, kindDir},
		{"pipe", "linux", fs.ModeNamedPipe, false, kindSpecial},
		{"socket", "darwin", fs.ModeSocket, false, kindSpecial},
		{"device", "linux", fs.ModeDevice, false, kindSpecial},
		{"char device", "windows", fs.ModeDevice | fs.ModeCharDevice, false, kindSpecial},
		{"windows junction (Readlink answers)", "windows", fs.ModeIrregular, true, kindDirLink},
		{"windows cloud placeholder (Readlink fails)", "windows", fs.ModeIrregular, false, kindPlaceholder},
		{"irregular on unix holds no mail we know of, and is recorded", "linux", fs.ModeIrregular, false, kindOutside},
		{"irregular on unix with a readable link is still not a placeholder", "linux", fs.ModeIrregular, true, kindOutside},
	}
	for _, c := range cases {
		if got := classifyMode(c.goos, c.mode, c.readlinkOK); got != c.want {
			t.Errorf("%s: classifyMode(%s, %v, %v) = %d, want %d", c.name, c.goos, c.mode, c.readlinkOK, got, c.want)
		}
	}
	// What each kind is recorded as: only a proven non-mail special file is info; a
	// reparse file that might hold mail is never info, and a placeholder is read.
	for _, c := range []struct {
		kind          entryKind
		reason, class string
	}{
		{kindSpecial, "not-a-message-file", state.ClassInfo},
		{kindDirLink, "symlinked-folder-skipped", state.ClassTerminal},
		{kindOutside, "symlink-outside-store", state.ClassTerminal},
	} {
		if reason, class, _ := refusal(c.kind); reason != c.reason || class != c.class {
			t.Errorf("refusal(%d) = %s %s, want %s %s", c.kind, reason, class, c.reason, c.class)
		}
	}
}

// covers: MA-322, R25, R4
// A link is judged by its text alone (FC3): an absolute, UNC or drive-qualified target,
// and one that leaves the root after a lexical clean, are refused; a relative target that
// stays in the root is the path it names. Nothing the target names is touched.
func TestLexicalLinkJudgment(t *testing.T) {
	cases := []struct {
		rel, target string
		want        string
		ok          bool
	}{
		{"Inbox/cur/m", "../new/m2", "Inbox/new/m2", true},
		{"cur/m", "other", "cur/other", true},
		{"Inbox.sbd", ".", ".", true},
		{"a/b/l", "../../c", "c", true},
		{"a/l", "../..", "", false},
		{"cur/m", "../../outside/secret", "", false},
		{"cur/m", "/etc/hostname", "", false},
		{"cur/m", `\\server\share\x`, "", false},
		{"cur/m", `C:\Users\x`, "", false},
		{"cur/m", "C:x", "", false},
		{"cur/m", `\rooted`, "", false},
		{"cur/m", "", "", false},
	}
	for _, c := range cases {
		got, ok := lexicalLink(c.rel, c.target)
		if ok != c.ok || got != c.want {
			t.Errorf("lexicalLink(%q, %q) = %q, %v; want %q, %v", c.rel, c.target, got, ok, c.want, c.ok)
		}
	}
}

// covers: MA-321, R10, R15, R1, S46
// A maildir file that cannot be read costs that file and nothing else (F29a): its
// neighbours and the next folder are read, and it is a file-unreadable fillable gap
// with a stable locator and NO message (FC7). The positive twin first: a store with no
// faults delivers every message and reports nothing.
func TestMaildirFileReadFaultIsContained(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a unix permission check that root and Windows do not make")
	}
	store := filepath.Join(t.TempDir(), "store")
	for _, f := range []string{"Inbox/cur/a:2,S", "Inbox/cur/b:2,S", "Inbox/cur/c:2,S", "Work/cur/d:2,S"} {
		writeLocal(t, filepath.Join(store, f), localMail(strings.SplitN(filepath.Base(f), ":", 2)[0]))
	}
	twin := walkLocal(t, store, OpenOptions{})
	if len(twin.subjects) != 4 || len(twin.faults) != 0 || twin.err != nil {
		t.Fatalf("a healthy maildir store: %v, faults %+v, err %v; want 4 messages and no fault", twin.subjects, twin.faults, twin.err)
	}

	bad := filepath.Join(store, "Inbox", "cur", "b:2,S")
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(bad, 0o644) })
	got := walkLocal(t, store, OpenOptions{})
	want := []string{"Inbox: a", "Inbox: c", "Work: d"}
	if fmt.Sprint(got.subjects) != fmt.Sprint(want) || got.err != nil {
		t.Errorf("walk delivered %v (err %v); want %v: one unreadable file costs that file only", got.subjects, got.err, want)
	}
	f, ok := got.fault("file-unreadable")
	if !ok || len(got.faults) != 1 {
		t.Fatalf("faults %+v; want exactly one file-unreadable", got.faults)
	}
	if f.Scope != state.GapScopeItem || f.Class != state.ClassFillable || f.Locator != "maildir:Inbox/b" || f.FolderPath() != "Inbox" {
		t.Errorf("fault %+v; want an item gap, fillable, at maildir:Inbox/b in Inbox", f)
	}
	assure.Refused(t, 1, f.Detail, assure.Code(1), assure.Names("permission denied"))
}

// covers: MA-321, R10, R1, S46
// A mail client renames a maildir file between the listing and the read (its flags are
// part of its name): the file is looked for ONCE under the same unique part and read;
// one deleted in that window is a vanished-during-read gap, fillable (F29a).
func TestMaildirRenamedAndVanishedFilesAreFoundOrRecorded(t *testing.T) {
	store := filepath.Join(t.TempDir(), "Inbox")
	writeLocal(t, filepath.Join(store, "cur", mdName("b:2,S")), localMail("b"))
	sc, err := openScope(store)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.close()
	var got []string
	var faults []Fault
	w := newLocalWalk(func(folder []string, m *model.Message) error {
		got = append(got, m.Subject+"|"+m.ReadLocator)
		return nil
	}, WalkOptions{Faults: func(f Fault) { faults = append(faults, f) }}, false)

	// The listing named "b:2," (not yet seen); the client has since flagged it.
	if err := w.maildirMessage(sc, mdName("cur/b:2,"), mdName("b:2,"), []string{"Inbox"}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != "[b|maildir:Inbox/b]" || len(faults) != 0 {
		t.Fatalf("renamed file: delivered %v, faults %+v; want it read once under its new name", got, faults)
	}
	if got := w.maildirMessageUnread(t, sc); got {
		t.Errorf("the renamed file's S flag was not read from its NEW name")
	}

	// Deleted in the window: nothing to find.
	if err := os.Remove(filepath.Join(store, "cur", mdName("b:2,S"))); err != nil {
		t.Fatal(err)
	}
	got = nil
	if err := w.maildirMessage(sc, mdName("cur/b:2,S"), mdName("b:2,S"), []string{"Inbox"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || len(faults) != 1 || faults[0].Reason != "vanished-during-read" || faults[0].Class != state.ClassFillable || faults[0].Locator != "maildir:Inbox/b" {
		t.Errorf("deleted file: delivered %v, faults %+v; want one fillable vanished-during-read at maildir:Inbox/b", got, faults)
	}
}

// maildirMessageUnread reads the renamed message again and says whether it came out
// unread: the S flag is the read state, taken from the name the file now has.
func (w *localWalk) maildirMessageUnread(t *testing.T, sc *scope) bool {
	t.Helper()
	var unread bool
	inner := newLocalWalk(func(folder []string, m *model.Message) error { unread = m.Unread; return nil }, WalkOptions{}, false)
	if err := inner.maildirMessage(sc, mdName("cur/b:2,"), mdName("b:2,"), []string{"Inbox"}); err != nil {
		t.Fatal(err)
	}
	return unread
}

// covers: MA-322, R25, R10, R4, S47
// Containment of a maildir store (F29b/c/d, FC2, FC3). A FIFO in cur/ is never opened
// (the walk ends under its guard) and is an info not-a-message-file gap; a file link to
// a file outside the store is a terminal symlink-outside-store gap and its content is
// NEVER read (the marker is absent from every message); an absolute link and a ..-escaping
// link are refused before anything they name is touched; a link to a file inside the
// store is read once; a linked cur/ is a terminal symlinked-folder-skipped gap. The
// positive twin is the same store without any of them.
func TestMaildirStoreIsReadUnderContainment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links and FIFOs need privileges on Windows")
	}
	parent := t.TempDir()
	store := filepath.Join(parent, "store")
	writeLocal(t, filepath.Join(store, "Inbox", "cur", "a:2,S"), localMail("a"))
	writeLocal(t, filepath.Join(store, "Inbox", "cur", "real:2,S"), localMail("real"))
	twin := walkLocal(t, store, OpenOptions{})
	if fmt.Sprint(twin.subjects) != "[Inbox: a Inbox: real]" || len(twin.faults) != 0 {
		t.Fatalf("the store without links: %v, %+v; want both messages and no fault", twin.subjects, twin.faults)
	}

	const marker = "TOP-SECRET-CONTENT SSN 123-45-6789"
	writeLocal(t, filepath.Join(parent, "outside", "secret"), "From: x@example.com\r\nSubject: "+marker+"\r\nMessage-ID: <s@x>\r\n\r\n"+marker+"\r\n")
	writeLocal(t, filepath.Join(parent, "outsidedir", "cur", "m"), localMail("outsidedir"))
	cur := filepath.Join(store, "Inbox", "cur")
	if err := mkfifo(filepath.Join(cur, "pipe")); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	mustLink := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	mustLink(filepath.Join(parent, "outside", "secret"), filepath.Join(cur, "abs"))
	mustLink("../../../outside/secret", filepath.Join(cur, "dots"))
	mustLink("real:2,S", filepath.Join(cur, "inlink"))
	writeLocal(t, filepath.Join(store, "Other", "new", "n:2,"), localMail("n"))
	mustLink(filepath.Join(parent, "outsidedir", "cur"), filepath.Join(store, "Other", "cur"))

	got := walkLocal(t, store, OpenOptions{})
	for _, m := range got.msgs {
		if strings.Contains(m.Subject, marker) || strings.Contains(m.PlainBody, "SSN") {
			t.Errorf("the content of a file outside the store was read: %q", m.Subject)
		}
	}
	wantSubjects := "[Inbox: a Inbox: real Inbox: real Other: n]" // "inlink" reads the in-store target once more, under its own name
	if fmt.Sprint(got.subjects) != wantSubjects {
		t.Errorf("delivered %v; want %s", got.subjects, wantSubjects)
	}
	if n := got.count("symlink-outside-store"); n != 2 {
		t.Errorf("%d symlink-outside-store faults, want 2 (the absolute link and the ..-escaping one): %+v", n, got.faults)
	}
	pipe, ok := got.fault("not-a-message-file")
	if !ok || pipe.Class != state.ClassInfo || pipe.Scope != state.GapScopeItem || pipe.Locator != "file:Inbox/cur/pipe" {
		t.Errorf("pipe fault %+v, %v; want an info item gap at file:Inbox/cur/pipe", pipe, ok)
	}
	linked, ok := got.fault("symlinked-folder-skipped")
	if !ok || linked.Class != state.ClassTerminal || linked.Scope != state.GapScopeFolder || linked.Locator != "folder:Other/cur" || linked.FolderPath() != "Other" {
		t.Errorf("linked folder fault %+v, %v; want a terminal folder gap at folder:Other/cur in Other", linked, ok)
	}
	for _, f := range got.faults {
		if f.Reason == "not-a-message-file" && f.Class != state.ClassInfo {
			t.Errorf("%+v: only a proven non-mail special file is info", f)
		}
		if f.Reason != "not-a-message-file" && f.Class == state.ClassInfo {
			t.Errorf("%+v: a refused entry that might hold mail is never info (FC1)", f)
		}
	}
}

// covers: MA-322, R25, R10, S47
// A link whose target would block if it were touched is refused before anything is
// resolved (FC3): the link names a FIFO by an absolute path, and the walk, which must
// not open or even stat it, ends under its guard with the link recorded.
func TestAnAbsoluteLinkToAPipeIsRefusedUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links and FIFOs need privileges on Windows")
	}
	parent := t.TempDir()
	pipe := filepath.Join(parent, "trap")
	if err := mkfifo(pipe); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	store := filepath.Join(parent, "store")
	writeLocal(t, filepath.Join(store, "cur", "a"), localMail("a"))
	if err := os.Symlink(pipe, filepath.Join(store, "cur", "trap")); err != nil {
		t.Fatal(err)
	}
	got := walkLocal(t, store, OpenOptions{})
	if fmt.Sprint(got.subjects) != "[: a]" || got.count("symlink-outside-store") != 1 {
		t.Errorf("delivered %v, faults %+v; want the one message and the one refused link", got.subjects, got.faults)
	}
}

// covers: MA-322, R25, R10, S47
// An mbox store (Thunderbird layout) under containment: a FIFO named like a mailbox is
// never opened, at discovery (IsMailStoreDir) or in the walk, both under a guard; a link
// to a file outside the store that begins "From " is refused and its content never read;
// a .sbd container linked to the store's root is not followed (the messages are read
// once, not once per level), and recorded.
func TestMboxStoreIsReadUnderContainment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links and FIFOs need privileges on Windows")
	}
	parent := t.TempDir()
	store := filepath.Join(parent, "store")
	mbox := func(id string) string {
		return "From a@example.com Mon Mar  3 09:00:00 2025\n" + strings.ReplaceAll(localMail(id), "\r\n", "\n") + "\n"
	}
	writeLocal(t, filepath.Join(store, "Archive"), mbox("arch"))
	twin := walkLocal(t, store, OpenOptions{})
	if fmt.Sprint(twin.subjects) != "[Archive: arch]" || len(twin.faults) != 0 {
		t.Fatalf("the plain store: %v, %+v", twin.subjects, twin.faults)
	}

	const marker = "TOP-SECRET-CONTENT"
	writeLocal(t, filepath.Join(parent, "outside"), mbox(marker))
	if err := mkfifo(filepath.Join(store, "Inbox")); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	if err := os.Symlink(filepath.Join(parent, "outside"), filepath.Join(store, "Sent")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, filepath.Join(store, "Archive.sbd")); err != nil {
		t.Fatal(err)
	}

	done := make(chan bool, 1)
	go func() { done <- IsMailStoreDir(store) }()
	select {
	case ok := <-done:
		if !ok {
			t.Error("discovery did not see the store (it has an mbox file)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("IsMailStoreDir waited on a pipe")
	}

	got := walkLocal(t, store, OpenOptions{})
	for _, m := range got.msgs {
		if strings.Contains(m.Subject, marker) {
			t.Errorf("a file outside the store was read: %q", m.Subject)
		}
	}
	if fmt.Sprint(got.subjects) != "[Archive: arch]" {
		t.Errorf("delivered %v; want the one message, once", got.subjects)
	}
	if pipe, ok := got.fault("not-a-message-file"); !ok || pipe.Class != state.ClassInfo || pipe.Locator != "file:Inbox" {
		t.Errorf("pipe fault %+v, %v; want info at file:Inbox", pipe, ok)
	}
	if out, ok := got.fault("symlink-outside-store"); !ok || out.Class != state.ClassTerminal || out.Locator != "file:Sent" {
		t.Errorf("outside link fault %+v, %v; want terminal at file:Sent", out, ok)
	}
	if sbd, ok := got.fault("symlinked-folder-skipped"); !ok || sbd.Class != state.ClassTerminal || sbd.Locator != "folder:Archive.sbd" {
		t.Errorf("linked .sbd fault %+v, %v; want terminal at folder:Archive.sbd", sbd, ok)
	}
}

// covers: MA-322, R25, R10, S47
// The input's directory-link opt-in (owner ruling OR6): with it, a .sbd container linked
// back to the store's root is followed ONCE (the visited set stops the loop: each message
// is delivered once) and a linked folder elsewhere is read; a file link out of the store
// is still refused and its content never read. Without it the same store records the
// links and follows none (the case above).
func TestDirectoryLinkOptInFollowsOnceAndStillRefusesFileLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	parent := t.TempDir()
	store := filepath.Join(parent, "store")
	mbox := func(id string) string {
		return "From a@example.com Mon Mar  3 09:00:00 2025\n" + strings.ReplaceAll(localMail(id), "\r\n", "\n") + "\n"
	}
	writeLocal(t, filepath.Join(store, "Archive"), mbox("arch"))
	const marker = "TOP-SECRET-CONTENT"
	writeLocal(t, filepath.Join(parent, "outside", "secret"), mbox(marker))
	writeLocal(t, filepath.Join(parent, "elsewhere", "Projects"), mbox("proj"))
	for target, name := range map[string]string{
		store:                              filepath.Join(store, "Archive.sbd"),
		filepath.Join(parent, "elsewhere"): filepath.Join(store, "Linked.sbd"),
		filepath.Join(parent, "outside", "secret"): filepath.Join(store, "Sent"),
	} {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	// Linked.sbd belongs to a folder named Linked.
	writeLocal(t, filepath.Join(store, "Linked"), mbox("linked"))

	got := walkLocal(t, store, OpenOptions{FollowLinks: true})
	if fmt.Sprint(got.subjects) != "[Archive: arch Linked/Projects: proj Linked: linked]" && fmt.Sprint(got.subjects) != "[Archive: arch Linked: linked Linked/Projects: proj]" {
		t.Errorf("delivered %v; want arch once, linked once, and the linked container's folder", got.subjects)
	}
	for _, m := range got.msgs {
		if strings.Contains(m.Subject, marker) {
			t.Errorf("the opt-in followed a FILE link out of the store: %q", m.Subject)
		}
	}
	if out, ok := got.fault("symlink-outside-store"); !ok || out.Locator != "file:Sent" {
		t.Errorf("outside file link fault %+v, %v; want it still refused", out, ok)
	}
	cycle, ok := got.fault("folder-cycle")
	if !ok || cycle.Class != state.ClassInfo || cycle.Locator != "folder:Archive.sbd" {
		t.Errorf("loop fault %+v, %v; want an info folder-cycle at folder:Archive.sbd (nothing is missing: the folder was read)", cycle, ok)
	}
	if _, ok := got.fault("symlinked-folder-skipped"); ok {
		t.Errorf("the opt-in still recorded a refused directory link: %+v", got.faults)
	}
}

// failingReader serves data, then fails with err: a mailbox file whose disk gave up
// mid-stream.
type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// covers: MA-316, R10, R6, R1, S46
// The mbox, maildir and Evolution readers cost a fault exactly its folder (F29, §4.5):
// a maildir folder that cannot be listed (not the store's root) is folder-unreadable and
// its siblings are read; an mbox file that cannot be opened is folder-unreadable; an
// mbox stream that fails mid-file is folder-partially-read with the count read, and the
// next folder goes on; an Evolution store's folder failing costs that folder.
func TestLocalFolderFaultsCostOneFolder(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a unix permission check that root and Windows do not make")
	}
	mbox := func(ids ...string) string {
		var b strings.Builder
		for _, id := range ids {
			b.WriteString("From a@example.com Mon Mar  3 09:00:00 2025\n" + strings.ReplaceAll(localMail(id), "\r\n", "\n") + "\n")
		}
		return b.String()
	}

	t.Run("a maildir folder that cannot be listed", func(t *testing.T) {
		store := filepath.Join(t.TempDir(), "store")
		writeLocal(t, filepath.Join(store, "Inbox", "cur", "a"), localMail("a"))
		writeLocal(t, filepath.Join(store, "Locked", "cur", "x"), localMail("x"))
		writeLocal(t, filepath.Join(store, "Work", "cur", "d"), localMail("d"))
		locked := filepath.Join(store, "Locked", "cur")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
		got := walkLocal(t, store, OpenOptions{})
		if fmt.Sprint(got.subjects) != "[Inbox: a Work: d]" || got.err != nil {
			t.Errorf("delivered %v (err %v); want the siblings of the unreadable folder", got.subjects, got.err)
		}
		f, ok := got.fault("folder-unreadable")
		if !ok || f.Scope != state.GapScopeFolder || f.Class != state.ClassFillable || f.FolderPath() != "Locked" {
			t.Errorf("fault %+v, %v; want a fillable folder-unreadable at Locked (a permission is fixed outside the source)", f, ok)
		}
	})

	t.Run("the store's own root that cannot be listed is a store-level error", func(t *testing.T) {
		store := filepath.Join(t.TempDir(), "store")
		writeLocal(t, filepath.Join(store, "Inbox"), mbox("a"))
		if err := os.Chmod(store, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(store, 0o755) })
		src, err := Open(store)
		if err != nil {
			return // refused at open: also a store-level failure
		}
		defer src.Close()
		err = src.Walk(func([]string, *model.Message) error { return nil })
		assure.Refused(t, 1, errString(err), assure.Code(1), assure.Names("read mail dir"))
	})

	t.Run("an mbox file that cannot be opened", func(t *testing.T) {
		store := filepath.Join(t.TempDir(), "store")
		writeLocal(t, filepath.Join(store, "A"), mbox("a"))
		writeLocal(t, filepath.Join(store, "B"), mbox("b"))
		writeLocal(t, filepath.Join(store, "C"), mbox("c"))
		bad := filepath.Join(store, "B")
		if err := os.Chmod(bad, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(bad, 0o644) })
		got := walkLocal(t, store, OpenOptions{})
		if fmt.Sprint(got.subjects) != "[A: a C: c]" {
			t.Errorf("delivered %v; want A and C", got.subjects)
		}
		f, ok := got.fault("folder-unreadable")
		if !ok || f.Class != state.ClassFillable || f.FolderPath() != "B" {
			t.Errorf("fault %+v, %v; want a fillable folder-unreadable at B", f, ok)
		}
	})

	t.Run("an mbox stream that fails mid-file", func(t *testing.T) {
		store := filepath.Join(t.TempDir(), "store")
		writeLocal(t, filepath.Join(store, "A"), mbox("a"))
		writeLocal(t, filepath.Join(store, "B"), mbox("b1", "b2", "b3"))
		writeLocal(t, filepath.Join(store, "C"), mbox("c"))
		old := wrapMboxStream
		wrapMboxStream = func(rel string, r io.Reader) io.Reader {
			if rel != "B" {
				return r
			}
			first := mbox("b1")
			all, _ := io.ReadAll(r)
			return &failingReader{data: all[:len(first)+10], err: errors.New("input/output error")}
		}
		t.Cleanup(func() { wrapMboxStream = old })
		got := walkLocal(t, store, OpenOptions{})
		if fmt.Sprint(got.subjects) != "[A: a B: b1 C: c]" {
			t.Errorf("delivered %v; want the message read before the failure, and the next folder", got.subjects)
		}
		f, ok := got.fault("folder-partially-read")
		if !ok || f.Scope != state.GapScopeFolder || f.FolderPath() != "B" || !strings.Contains(f.Detail, "1 message(s) were read") {
			t.Errorf("fault %+v, %v; want folder-partially-read at B naming the 1 message read", f, ok)
		}
	})

	t.Run("an Evolution store's folder", func(t *testing.T) {
		store := filepath.Join(t.TempDir(), "evo")
		writeLocal(t, filepath.Join(store, "..maildir++"), "")
		writeLocal(t, filepath.Join(store, "cur", "i"), localMail("i"))
		writeLocal(t, filepath.Join(store, ".Tickets", "cur", "t"), localMail("t"))
		writeLocal(t, filepath.Join(store, ".Locked", "cur", "x"), localMail("x"))
		writeLocal(t, filepath.Join(store, ".Zed", "cur", "z"), localMail("z"))
		locked := filepath.Join(store, ".Locked", "cur")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
		got := walkLocal(t, store, OpenOptions{})
		if fmt.Sprint(got.subjects) != "[: i Tickets: t Zed: z]" {
			t.Errorf("delivered %v; want the other folders", got.subjects)
		}
		if f, ok := got.fault("folder-unreadable"); !ok || f.FolderPath() != "Locked" {
			t.Errorf("fault %+v, %v; want folder-unreadable at Locked", f, ok)
		}
	})

	t.Run("an Evolution cache folder", func(t *testing.T) {
		store := filepath.Join(t.TempDir(), "cache")
		writeLocal(t, filepath.Join(store, "folders", "INBOX", "cur", "i"), localMail("i"))
		writeLocal(t, filepath.Join(store, "folders", "Bad", "cur", "x"), localMail("x"))
		writeLocal(t, filepath.Join(store, "folders", "INBOX", "subfolders", "Kid", "cur", "k"), localMail("k"))
		locked := filepath.Join(store, "folders", "Bad", "cur")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
		got := walkLocal(t, store, OpenOptions{})
		if fmt.Sprint(got.subjects) != "[INBOX/Kid: k INBOX: i]" && fmt.Sprint(got.subjects) != "[INBOX: i INBOX/Kid: k]" {
			t.Errorf("delivered %v; want INBOX and its subfolder", got.subjects)
		}
		if f, ok := got.fault("folder-unreadable"); !ok || f.FolderPath() != "Bad" {
			t.Errorf("fault %+v, %v; want folder-unreadable at Bad", f, ok)
		}
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
