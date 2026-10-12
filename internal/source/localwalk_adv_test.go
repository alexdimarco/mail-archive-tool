package source

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// releasePipe lets a goroutine that blocked opening the pipe at path go, once the test is
// over (a test that finds the hang must not leave a goroutine in the process).
func releasePipe(t *testing.T, path string) {
	t.Helper()
	t.Cleanup(func() {
		if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})
}

// The tombstones of FI-S2b's adversarial pass (docs/review-fault-isolation-s2b-adversarial.md).

// deepChain is the plain-directory chain "d/d/.../d", n levels.
func deepChain(n int) string { return strings.TrimSuffix(strings.Repeat("d/", n), "/") }

// covers: MA-322, R10, S47
// S2B-AO1 (and S2B-AC6): a store of plain directories nested thousands deep is bounded
// like the PST and Graph hierarchies are (the design's hostile-store row: cycle and
// depth bounded). Every descent re-opens its whole relative path through the root, so an
// unbounded chain made the walk's cost grow faster than its depth (800 levels took 14 s,
// 2,000 did not finish). A chain within the cap is read in full; one past it is a terminal
// folder-too-deep gap on the folder where the walk stopped, the messages above it are
// delivered, and the walk is quick.
func TestAnOverDeepStoreIsBoundedByTheDepthCap(t *testing.T) {
	store := filepath.Join(t.TempDir(), "Inbox")
	writeLocal(t, filepath.Join(store, "cur", "top:2,S"), localMail("top"))

	shallow := filepath.Join(store, "cur", filepath.FromSlash(deepChain(100)), "low:2,S")
	writeLocal(t, shallow, localMail("low"))
	got := walkLocal(t, store, OpenOptions{})
	if len(got.subjects) != 2 || len(got.faults) != 0 {
		t.Fatalf("a chain 100 deep: delivered %v, faults %+v; want both messages and no fault", got.subjects, got.faults)
	}

	deep := filepath.Join(store, "new", filepath.FromSlash(deepChain(maxFolderDepth+60)), "bottom:2,S")
	writeLocal(t, deep, localMail("bottom"))
	start := time.Now()
	got = walkLocal(t, store, OpenOptions{})
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the walk took %v; an over-deep chain must be cut, not walked", took)
	}
	for _, s := range got.subjects {
		if strings.Contains(s, "bottom") {
			t.Errorf("delivered %v: a message %d levels down was read, past the cap of %d", got.subjects, maxFolderDepth+61, maxFolderDepth)
		}
	}
	if len(got.subjects) != 2 {
		t.Errorf("delivered %v; want the message above the cap and the one 100 deep", got.subjects)
	}
	f, ok := got.fault("folder-too-deep")
	if !ok || got.count("folder-too-deep") != 1 || f.Class != state.ClassTerminal || f.Scope != state.GapScopeFolder || !strings.HasPrefix(f.Locator, "folder:new/d/d") {
		t.Fatalf("faults %+v; want ONE terminal folder-too-deep folder gap under new/d/d/...", got.faults)
	}
	if rem := state.GapRemedy(f.Reason, state.SourceLocal, ""); strings.Contains(rem, "scanpst") || strings.Contains(rem, ".pst") {
		t.Errorf("the remedy of a local store's deep folder names a .pst repair: %q", rem)
	}
}

// covers: MA-322, R25, R10, S47
// S2B-AO5: a cloud-placeholder FOLDER (OneDrive Files-On-Demand, Dropbox online-only) is a
// folder. Go reports a directory whose reparse tag is not a name surrogate as
// ModeDir|ModeIrregular (a junction is ModeIrregular alone), and the classifier tested
// ModeIrregular first, so the folder was read as a placeholder FILE: a mailbox folder in a
// cloud-synced store was dropped silently (an mbox store), or filed as "nothing to do"
// (a maildir), against FC1. The rows are the classifier's table on any OS; the
// placeholder FILE and the junction keep their rows of TestClassifyModeTable.
func TestAPlaceholderFolderIsAFolder(t *testing.T) {
	for _, c := range []struct {
		name       string
		goos       string
		mode       fs.FileMode
		readlinkOK bool
		want       entryKind
	}{
		{"windows placeholder folder", "windows", fs.ModeDir | fs.ModeIrregular, false, kindDir},
		{"windows placeholder folder, Readlink answers: a link", "windows", fs.ModeDir | fs.ModeIrregular, true, kindDirLink},
		{"windows placeholder file is still a file to read", "windows", fs.ModeIrregular, false, kindPlaceholder},
		{"windows junction is still a directory link", "windows", fs.ModeIrregular, true, kindDirLink},
		{"nothing on unix is a placeholder folder", "linux", fs.ModeDir | fs.ModeIrregular, false, kindOutside},
	} {
		if got := classifyMode(c.goos, c.mode, c.readlinkOK); got != c.want {
			t.Errorf("%s: classifyMode(%s, %v, %v) = %d, want %d", c.name, c.goos, c.mode, c.readlinkOK, got, c.want)
		}
	}
	// A directory descriptor is never a file to read, placeholder or not (the folder is
	// listed, not opened as a mailbox).
	if util.DescriptorReadable("windows", fs.ModeDir|fs.ModeIrregular) {
		t.Errorf("a placeholder folder's descriptor is readable as a mail file")
	}
}

// mboxText is a one-message mbox file, the message named id.
func mboxText(id string) string {
	return "From a@example.com Mon Mar  3 09:00:00 2025\n" + strings.ReplaceAll(localMail(id), "\r\n", "\n") + "\n"
}

// covers: MA-322, R25, R1, R10
// S2B-AO6 (and S2B-AC3): the mailboxes of a Thunderbird store are not dropped by the
// "From " sniff. A parent folder with no messages of its own is a 0-byte file beside its
// "<name>.sbd" directory, and the subtree was reached only through a parent that sniffed
// as a mailbox, so every subfolder below an empty parent (and below an orphan .sbd)
// vanished with no trace and the run said it was complete. A mailbox that begins with a
// UTF-8 byte order mark or a blank line is a mailbox. The twin: the plain store reads
// the same way it did.
func TestAThunderbirdStoreIsNotDroppedBySniffingItsParents(t *testing.T) {
	store := filepath.Join(t.TempDir(), "Mail")
	writeLocal(t, filepath.Join(store, "Inbox"), mboxText("inbox"))
	writeLocal(t, filepath.Join(store, "Real"), mboxText("real"))
	writeLocal(t, filepath.Join(store, "Real.sbd", "Sub"), mboxText("sub"))
	twin := walkLocal(t, store, OpenOptions{})
	if len(twin.subjects) != 3 || len(twin.faults) != 0 {
		t.Fatalf("the plain store: %v, %+v; want 3 messages and no fault", twin.subjects, twin.faults)
	}

	writeLocal(t, filepath.Join(store, "Parent"), "")
	writeLocal(t, filepath.Join(store, "Parent.sbd", "Child"), mboxText("child"))
	writeLocal(t, filepath.Join(store, "Parent.sbd", "Deeper"), "")
	writeLocal(t, filepath.Join(store, "Parent.sbd", "Deeper.sbd", "Leaf"), mboxText("leaf"))
	writeLocal(t, filepath.Join(store, "Orphan.sbd", "Alone"), mboxText("alone"))
	writeLocal(t, filepath.Join(store, "Bom"), "\xef\xbb\xbf"+mboxText("bom"))
	writeLocal(t, filepath.Join(store, "Blank"), "\r\n\n"+mboxText("blank"))
	got := walkLocal(t, store, OpenOptions{})
	have := strings.Join(got.subjects, " ")
	if len(got.subjects) != 8 {
		t.Errorf("delivered %v; want the 8 messages of Inbox, Real, Real/Sub, Parent/Child, Parent/Deeper/Leaf, Orphan/Alone, Bom and Blank", got.subjects)
	}
	for _, id := range []string{"child", "leaf", "alone", "bom", "blank"} {
		if !strings.Contains(have, ": "+id) {
			t.Errorf("the message %q was dropped: delivered %v", id, got.subjects)
		}
	}
	if len(got.faults) != 0 {
		t.Errorf("faults %+v; want none: the empty parents and the .sbd containers are nothing refused", got.faults)
	}
}

// covers: MA-322, MA-316, R1, R10
// S2B-AI7: a mailbox whose first read fails is recorded, not read as "not a mailbox". The
// sniff dropped the read error with the count (`n, _ := f.Read(buf)`), against its own
// comment (a mailbox the run cannot open must be recorded), so a file whose open worked
// and whose first read gave EIO (a dying disk, a share, a cloud placeholder) was skipped
// with no row. The failure is the seam wrapMboxStream's (no regular file on a healthy
// disk fails its first read): the folder is a fillable folder-unreadable gap carrying the
// disk's error, the next folder is read, and its .sbd subtree is not lost with it.
func TestAMailboxWhoseFirstReadFailsIsRecorded(t *testing.T) {
	store := filepath.Join(t.TempDir(), "Mail")
	writeLocal(t, filepath.Join(store, "Good"), mboxText("good"))
	writeLocal(t, filepath.Join(store, "Bad"), mboxText("bad"))
	writeLocal(t, filepath.Join(store, "Bad.sbd", "Child"), mboxText("child"))
	old := wrapMboxStream
	wrapMboxStream = func(rel string, r io.Reader) io.Reader {
		if rel == "Bad" {
			return &failingRead{err: errors.New("input/output error")}
		}
		return r
	}
	t.Cleanup(func() { wrapMboxStream = old })
	got := walkLocal(t, store, OpenOptions{})
	if strings.Join(got.subjects, " ") != "Bad/Child: child Good: good" {
		t.Errorf("delivered %v; want Good and the subfolder below the unreadable mailbox", got.subjects)
	}
	f, ok := got.fault("folder-unreadable")
	if !ok || got.count("folder-unreadable") != 1 || f.Class != state.ClassFillable || f.Scope != state.GapScopeFolder || f.Locator != "folder:Bad" || !strings.Contains(f.Detail, "input/output error") {
		t.Errorf("faults %+v; want ONE fillable folder-unreadable at folder:Bad with the disk's error: the mailbox was skipped with no row", got.faults)
	}
}

// covers: MA-322, R25, R10, S47
// S2B-AO7: with the directory-link opt-in, a link never takes a real folder's place in
// the walk. The visited set reads a directory once, under whichever path reaches it
// first; the links were followed in name order, so an alias that sorted before the real
// folder claimed it: the mail was filed under the alias's name and the real folder was
// reported "read already". The walk reads every plain directory first and follows the
// links after, so the real folder keeps its name wherever the alias sorts or sits.
func TestADirectoryLinkNeverTakesARealFoldersPlace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	store := filepath.Join(t.TempDir(), "Mail")
	writeLocal(t, filepath.Join(store, "Inbox", "cur", "m:2,S"), localMail("inbox"))
	writeLocal(t, filepath.Join(store, "Real", "cur", "r:2,S"), localMail("real"))
	writeLocal(t, filepath.Join(store, "Aaa"), mboxText("aaa"))
	if err := os.Symlink("Inbox", filepath.Join(store, "Aaa-Evil")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store, "Aaa.sbd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../Real", filepath.Join(store, "Aaa.sbd", "Alias")); err != nil {
		t.Fatal(err)
	}

	got := walkLocal(t, store, OpenOptions{FollowLinks: true})
	if strings.Join(got.subjects, " ") != "Aaa: aaa Inbox: inbox Real: real" {
		t.Errorf("delivered %v; want the real folders' own names (Inbox, Real), the alias taking neither", got.subjects)
	}
	if n := got.count("folder-cycle"); n != 2 {
		t.Errorf("faults %+v; want one info folder-cycle for each alias, which is the folder that was read already", got.faults)
	}
	for _, f := range got.faults {
		if f.Reason == "folder-cycle" && !(f.Locator == "folder:Aaa-Evil" || f.Locator == "folder:Aaa.sbd/Alias") {
			t.Errorf("folder-cycle at %q; want it on the alias, never on the real folder", f.Locator)
		}
	}
}

// covers: MA-322, R25, R1, R10, S47
// S2B-AC2: with the directory-link opt-in, a link to a folder that holds MBOX files (a
// Thunderbird account directory, the usual target of a folder link) is read as that
// directory of mailboxes. The followed target was read only if it was a maildir; any
// other directory was read as nothing and recorded as nothing, while the gap the
// refused link had left said the folder would be read once the opt-in was given: the
// owner followed the remedy and lost the mail with a clean run. Without the opt-in the
// link is still a terminal gap (the twin).
func TestAFollowedLinkToADirectoryOfMailboxesIsRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	parent := t.TempDir()
	store := filepath.Join(parent, "Mail")
	writeLocal(t, filepath.Join(store, "Inbox"), mboxText("m3"))
	writeLocal(t, filepath.Join(parent, "other", "Archive"), mboxText("m1")+mboxText("m2"))
	writeLocal(t, filepath.Join(parent, "other", "Archive.sbd", "Old"), mboxText("m0"))
	if err := os.Symlink(filepath.Join(parent, "other"), filepath.Join(store, "Linked")); err != nil {
		t.Fatal(err)
	}

	without := walkLocal(t, store, OpenOptions{})
	if strings.Join(without.subjects, " ") != "Inbox: m3" || without.count("symlink-outside-store") != 1 {
		t.Fatalf("without the opt-in: delivered %v, faults %+v; want the link refused and recorded", without.subjects, without.faults)
	}
	got := walkLocal(t, store, OpenOptions{FollowLinks: true})
	if strings.Join(got.subjects, " ") != "Inbox: m3 Linked/Archive/Old: m0 Linked/Archive: m1 Linked/Archive: m2" {
		t.Errorf("with the opt-in: delivered %v; want the mailboxes of the linked directory under Linked, and its .sbd", got.subjects)
	}
	if len(got.faults) != 0 {
		t.Errorf("with the opt-in: faults %+v; want none", got.faults)
	}
}

// covers: MA-322, R25, R1, R10, S47
// S2B-AO8: a link to a link is judged for what it is. With the opt-in, an in-store link
// whose target is another link (to a folder out there) is followed like any directory
// link, instead of a terminal "a link in the store points outside it" whose remedy is
// the opt-in already in use; without it, a chain at a folder's place is the directory link
// it is (symlinked-folder-skipped), not a link out of the store.
func TestALinkToALinkIsJudgedForWhatItIs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	parent := t.TempDir()
	store := filepath.Join(parent, "Mail")
	writeLocal(t, filepath.Join(store, "Inbox", "cur", "i:2,S"), localMail("inbox"))
	writeLocal(t, filepath.Join(parent, "outside", "cur", "o:2,S"), localMail("outside"))
	if err := os.Symlink(filepath.Join(parent, "outside"), filepath.Join(store, "Real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("Real", filepath.Join(store, "Chain")); err != nil {
		t.Fatal(err)
	}

	got := walkLocal(t, store, OpenOptions{FollowLinks: true})
	if strings.Join(got.subjects, " ") != "Chain: outside Inbox: inbox" {
		t.Errorf("with the opt-in: delivered %v; want the linked folder read once, through the first link that reaches it", got.subjects)
	}
	for _, f := range got.faults {
		if f.Class != state.ClassInfo {
			t.Errorf("with the opt-in: fault %+v; want nothing but the info row for the second path to a folder read already", f)
		}
	}

	// At a folder's place, with no opt-in: Other/cur -> cur2 -> outside.
	writeLocal(t, filepath.Join(store, "Other", "new", "n:2,"), localMail("n"))
	if err := os.Symlink(filepath.Join(parent, "outside", "cur"), filepath.Join(store, "Other", "cur2")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("cur2", filepath.Join(store, "Other", "cur")); err != nil {
		t.Fatal(err)
	}
	plain := walkLocal(t, store, OpenOptions{})
	var chain []Fault
	for _, f := range plain.faults {
		if f.Locator == "folder:Other/cur" {
			chain = append(chain, f)
		}
	}
	if len(chain) != 1 || chain[0].Reason != "symlinked-folder-skipped" || chain[0].Class != state.ClassTerminal {
		t.Errorf("without the opt-in: faults at Other/cur %+v; want one terminal symlinked-folder-skipped (a link to a link at a folder's place), not a link out of the store", chain)
	}
}

// covers: MA-322, R25, R1, R10, S47
// S2B-AI8: with the opt-in on, a link whose target cannot be reached (a drive that is not
// mounted, a folder that moved) is not told to use the opt-in. It was a terminal
// symlink-outside-store gap whose remedy sent the owner to the opt-in the input already
// had. It is a fillable folder-unreadable gap that says the target could not be reached,
// and the remedy it gets does not name the opt-in.
func TestAnUnreachableLinkIsNotToldToUseTheOptInItHas(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	parent := t.TempDir()
	store := filepath.Join(parent, "Mail")
	writeLocal(t, filepath.Join(store, "Inbox", "cur", "i:2,S"), localMail("inbox"))
	if err := os.Symlink(filepath.Join(parent, "unmounted", "Mail", "Old"), filepath.Join(store, "Old")); err != nil {
		t.Fatal(err)
	}
	got := walkLocal(t, store, OpenOptions{FollowLinks: true})
	if strings.Join(got.subjects, " ") != "Inbox: inbox" {
		t.Fatalf("delivered %v; want the one message", got.subjects)
	}
	f, ok := got.fault("folder-unreadable")
	if !ok || len(got.faults) != 1 || f.Class != state.ClassFillable || f.Scope != state.GapScopeFolder || f.Locator != "folder:Old" || !strings.Contains(f.Detail, "could not be reached") {
		t.Fatalf("faults %+v; want ONE fillable folder-unreadable at folder:Old saying the link's target could not be reached", got.faults)
	}
	rem := state.GapRemedy(f.Reason, state.SourceLocal, state.GapErrnoClass(f.Detail))
	if strings.Contains(rem, "follow-links") || strings.Contains(rem, "opt-in") {
		t.Errorf("the remedy %q sends the owner to the opt-in the input already has", rem)
	}
}

// covers: MA-322, R25, R10, S47
// S2B-AO4 (and S2B-AC1): a pipe named like an Outlook data file is never opened. The PST
// open was a plain os.Open, which waits for a writer on a pipe, so a FIFO named *.pst or
// *.ost in an input folder held the archive lock until someone opened it (FC2 says a pipe
// is never opened). The positive twin first: a regular file of that name is opened (and
// refused by the parser, not by the open).
func TestAPipeNamedLikeADataFileIsNeverOpened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs need privileges on Windows")
	}
	dir := t.TempDir()
	writeLocal(t, filepath.Join(dir, "plain.pst"), "not a pst")
	if _, err := Open(filepath.Join(dir, "plain.pst")); err == nil || strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a regular file named .pst: error %v; want the parser's refusal, not the open's", err)
	}
	for _, name := range []string{"evil.pst", "evil.ost", "Upper.PST"} {
		pipe := filepath.Join(dir, name)
		if err := mkfifo(pipe); err != nil {
			t.Skip("no FIFOs here:", err)
		}
		releasePipe(t, pipe)
		done := make(chan error, 1)
		go func() {
			s, err := Open(pipe)
			if err == nil {
				s.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "not a regular file") {
				t.Errorf("%s: error %v; want the refusal of a pipe", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("%s: Open waited on a pipe", name)
		}
	}
}
