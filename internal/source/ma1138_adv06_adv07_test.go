package source

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/tbrestore"
)

// copyTree copies a folder tree (files and folders only), as a person copying a profile
// or a folder onto a USB disk would.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// fileInto appends one message to the mbox at p (made if absent), as Thunderbird files
// mail into a folder.
func fileInto(t *testing.T, p, subject string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("From a@b Mon Jan  1 00:00:00 2026\nSubject: " + subject + "\n\nmine\n\n"); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1138, R41, S70
// ADV-06 (the R3–R5 adversarial pass, settled 2026-11-07): the skip is tied to where the
// restore wrote. A marked folder is left out only when the folder the backup met it in
// is, by identity, the Local Folders the restore's started line records (and the
// marker's profile key is the record's, as before). A committed restore copied with its
// marker into another profile's Local Folders, or onto a disk the person backs up — and
// the mail later filed into that copy — is backed up like any other mail, and so is a
// copied unfinished leftover; the original stays left out (positive twin).
func TestBackupSkipIsTiedToTheLocalFoldersTheRestoreWrote(t *testing.T) {
	b := newRestoreBed(t)
	committed := b.restore("Committed", "restored mail", b.log)
	unfinished := b.restore("Unfinished", "leftover mail", b.log)
	if err := os.Remove(filepath.Join(b.local, unfinished)); err != nil {
		t.Fatal(err)
	}
	copyLocal := filepath.Join(t.TempDir(), "q.default", "Mail", "Local Folders")
	copyTree(t, b.local, copyLocal)
	copyLocal, _ = filepath.EvalSymlinks(copyLocal)
	fileInto(t, filepath.Join(copyLocal, committed), "filed in the copy")
	c := &restoreBed{t: t, local: copyLocal}

	subjects, left := c.walk(tbrestore.LoadSkipSet(b.log.Path))
	for _, want := range []string{"restored mail", "filed in the copy", "leftover mail", "kept in Trash"} {
		if !has(subjects, want) {
			t.Errorf("in the copy, %q is backed up: %v (left out %v)", want, subjects, left)
		}
	}
	if len(left) != 0 {
		t.Errorf("nothing is left out of the copy: %v", left)
	}

	subjects, left = b.walk(tbrestore.LoadSkipSet(b.log.Path))
	if has(subjects, "restored mail") || has(subjects, "leftover mail") || len(left) != 2 {
		t.Errorf("the original stays left out: %v (left out %v)", subjects, left)
	}
}

// covers: MA-1138, R41, S70
// ADV-07 (the R3–R5 adversarial pass; the lead's call, 2026-11-07): an "abandoned"
// leftover — one a cleanup kept whole because something not MailArchive's is in it,
// typically Thunderbird's .msf after it showed the tree, with mail the person moved into
// the restored folder — is backed up like an ordinary folder: a folder that may hold the
// person's mail is never left out. The cost, accepted: a cut-off last message of a failed
// restore may be copied into the archive once. Positive twins: a restore with no closing
// line (still being written, or cut off by a crash: Thunderbird does not show it) and a
// committed one stay left out.
func TestBackupBacksUpAnAbandonedLeftover(t *testing.T) {
	b := newRestoreBed(t)
	// Cut off before its commit, then a later Start found Thunderbird's .msf beside the
	// mail the person moved in, kept it whole and closed it "abandoned" (case 3b).
	top := b.unfinished("Shown", "restored mail")
	inbox := filepath.Join(b.local, top+".sbd", "Inbox")
	fileInto(t, inbox, "moved in by the person")
	if err := os.WriteFile(inbox+".msf", []byte("summary"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.closeLast(tbrestore.EvAbandoned, "settled (case 3b)")
	// An abandoned W-COMMITERR (checked, no taken) whose parent file is regular: still
	// backed up, with that file.
	ce := b.unfinished("CommitErr", "commit error mail")
	b.closeLast(tbrestore.EvAbandoned, "commiterr")
	fileInto(t, filepath.Join(b.local, ce), "filed into the commit error's top")

	open := b.unfinished("Open", "open restore's mail")
	committed := b.restore("Committed", "committed mail", b.log)

	subjects, left := b.walk(tbrestore.LoadSkipSet(b.log.Path))
	for _, want := range []string{"moved in by the person", "restored mail", "commit error mail", "filed into the commit error's top"} {
		if !has(subjects, want) {
			t.Errorf("%q is backed up: %v (left out %v)", want, subjects, left)
		}
	}
	for _, not := range []string{"open restore's mail", "committed mail"} {
		if has(subjects, not) {
			t.Errorf("%q stays left out: %v", not, subjects)
		}
	}
	if len(left) != 2 || !has(left, open) || !has(left, committed) {
		t.Errorf("left out: %v", left)
	}
}

// unfinished writes a restore whose record has no closing line and whose parent file is
// not there (a crash after "checked", before the commit), returning its parent name.
func (b *restoreBed) unfinished(archive, subject string) string {
	b.t.Helper()
	scratch := tbrestore.NewLog(filepath.Join(b.t.TempDir(), tbrestore.LogName))
	top := b.restore(archive, subject, scratch)
	lines, err := scratch.Read()
	if err != nil {
		b.t.Fatal(err)
	}
	for _, l := range lines {
		if l.Event == tbrestore.EvEnded {
			continue
		}
		if err := b.log.Append(l); err != nil {
			b.t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(b.local, top)); err != nil {
		b.t.Fatal(err)
	}
	return top
}

// closeLast gives the last restore started in the record its closing line.
func (b *restoreBed) closeLast(event, result string) {
	b.t.Helper()
	lines, err := b.log.Read()
	if err != nil || len(lines) == 0 {
		b.t.Fatal(err)
	}
	id := ""
	for _, l := range lines {
		if l.Event == tbrestore.EvStarted {
			id = l.ID
		}
	}
	if err := b.log.Append(tbrestore.LogLine{Event: event, ID: id, Result: result}); err != nil {
		b.t.Fatal(err)
	}
}
