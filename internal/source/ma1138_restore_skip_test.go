package source

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/tbrestore"
)

// A Local Folders (the backup's input) in a temp folder, with Thunderbird's own Trash
// and restores written into it by the real writer (tbrestore.Write) — so the marker
// this package reads is the one the restore writes (RestoreMarkerName and
// tbrestore.MarkerName kept in step by behaviour).

type restoreBed struct {
	t       *testing.T
	local   string
	profile string
	log     *tbrestore.Log
}

func newRestoreBed(t *testing.T) *restoreBed {
	t.Helper()
	root := t.TempDir()
	profile := filepath.Join(root, "p.default")
	local := filepath.Join(profile, "Mail", "Local Folders")
	if err := os.MkdirAll(local, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "Trash"), []byte("From - x\nSubject: kept in Trash\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, _ := filepath.EvalSymlinks(local)
	p, _ := filepath.EvalSymlinks(profile)
	return &restoreBed{t: t, local: r, profile: p, log: tbrestore.NewLog(filepath.Join(t.TempDir(), tbrestore.LogName))}
}

// restore writes one restore named for archive, with one folder holding one message
// whose subject is subject; it returns the parent name.
func (b *restoreBed) restore(archive, subject string, log *tbrestore.Log) string {
	b.t.Helper()
	id, err := tbrestore.DirIdentity(b.local)
	if err != nil {
		b.t.Fatal(err)
	}
	lf := tbrestore.LocalFolders{Dir: b.local, Identity: id, ServerKey: "server1"}
	layout, err := tbrestore.PlanLayout(archive, []tbrestore.FolderIn{{Names: []string{"Inbox"}, Messages: 1}}, b.local, "")
	if err != nil {
		b.t.Fatal(err)
	}
	entries, _ := os.ReadDir(b.local)
	top, _, err := tbrestore.FreeTopName(layout.Top, entries)
	if err != nil {
		b.t.Fatal(err)
	}
	root, err := tbrestore.OpenLocalRoot(lf)
	if err != nil {
		b.t.Fatal(err)
	}
	defer root.Close()
	rid, _ := tbrestore.NewID()
	res := tbrestore.Write(context.Background(), tbrestore.NewRootFS(root), tbrestore.WriteInput{
		ID: rid, ProfileDir: b.profile, Local: lf, Archive: "/a/" + archive, Layout: layout, Top: top, Log: log,
		Messages: func(int, func([]byte) error) error { return nil },
	})
	if res.Outcome != tbrestore.OutDone {
		b.t.Fatalf("restore: %s %v", res.Outcome, res.Err)
	}
	// One message in its Inbox, written as the restore writes it (mboxrd framing).
	inbox := filepath.Join(b.local, top+".sbd", "Inbox")
	if err := os.WriteFile(inbox, []byte("From a@b Mon Jan  1 00:00:00 2026\nSubject: "+subject+"\n\nbody\n\n"), 0o600); err != nil {
		b.t.Fatal(err)
	}
	return top
}

// walk backs up the Local Folders with the skip set, returning the subjects backed up
// and the folders left out.
func (b *restoreBed) walk(skip *tbrestore.SkipSet) (subjects, left []string) {
	b.t.Helper()
	src, err := OpenWith(b.local, OpenOptions{})
	if err != nil {
		b.t.Fatal(err)
	}
	defer src.Close()
	opts := WalkOptions{RestoreSkipped: func(p []string) { left = append(left, strings.Join(p, "/")) }}
	if skip != nil {
		opts.RestoreSkip = skip
	}
	err = src.(FaultWalker).WalkWithFaults(func(folder []string, m *model.Message) error {
		subjects = append(subjects, strings.Join(folder, "/")+": "+m.Subject)
		return nil
	}, opts)
	if err != nil {
		b.t.Fatal(err)
	}
	sort.Strings(subjects)
	return subjects, left
}

func has(list []string, needle string) bool {
	for _, s := range list {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// covers: MA-1138, R41, S70
// Backups leave out MailArchive's own restore folders (design §7.2 as folded, R41): a
// marked container whose ID restores.log knows for that profile, with no entry of its
// parent name beside it, is left out with its marker and made-list; a committed
// restore (checked, no taken line) with a regular parent file is left out with that
// file — mail filed into either is not backed up while it is there (O1's answer (b),
// the cost the plan states, and the residual case's cost pinned); Thunderbird's own
// folders are backed up as before. Positive twins: an unknown ID, a restore that did
// not commit (a "taken" line) beside its parent, and a parent that is a folder are each
// backed up as ordinary mail, the hidden marker and made-list never read as mailboxes.
func TestBackupLeavesOutRestoreFolders(t *testing.T) {
	b := newRestoreBed(t)
	committed := b.restore("Committed", "restored mail", b.log)
	if err := os.WriteFile(filepath.Join(b.local, committed), []byte("From a@b Mon Jan  1 00:00:00 2026\nSubject: filed into the top folder\n\nx\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unfinished := b.restore("Unfinished", "leftover mail", b.log)
	if err := os.Remove(filepath.Join(b.local, unfinished)); err != nil { // no parent file: a leftover
		t.Fatal(err)
	}
	other := tbrestore.NewLog(filepath.Join(t.TempDir(), tbrestore.LogName))
	b.restore("Unknown", "unknown restore's mail", other) // logged elsewhere: not this computer's record
	notCommitted := b.restore("Taken", "taken restore's mail", b.log)
	lines, _ := b.log.Read()
	_ = b.log.Append(tbrestore.LogLine{Event: tbrestore.EvTaken, ID: lines[len(lines)-1].ID})
	asFolder := b.restore("Folder", "parent-folder restore's mail", b.log)
	_ = os.Remove(filepath.Join(b.local, asFolder))
	_ = os.Mkdir(filepath.Join(b.local, asFolder), 0o700)

	subjects, left := b.walk(tbrestore.LoadSkipSet(b.log.Path))
	assure.Reached(t, subjects, "subjects backed up")
	for _, want := range []string{"kept in Trash", "unknown restore's mail", "taken restore's mail", "parent-folder restore's mail"} {
		if !has(subjects, want) {
			t.Errorf("%q not backed up: %v", want, subjects)
		}
	}
	for _, not := range []string{"restored mail", "filed into the top folder", "leftover mail"} {
		if has(subjects, not) {
			t.Errorf("%q backed up: %v", not, subjects)
		}
	}
	if len(left) != 2 || !has(left, committed) || !has(left, unfinished) {
		t.Errorf("left out: %v", left)
	}
	for _, s := range subjects {
		if strings.Contains(s, ".mailarchive") {
			t.Errorf("a hidden restore file read as a mailbox: %s", s)
		}
	}
	_ = notCommitted

	// No skip set (a caller that passes none): everything is backed up, as before R41.
	all, _ := b.walk(nil)
	if !has(all, "restored mail") || !has(all, "leftover mail") {
		t.Fatalf("without the record nothing is left out: %v", all)
	}
}

// covers: MA-1138, R41, S70
// A skip set built before a restore starts, then that restore's container met mid-walk,
// is left out after one re-read of the log (foldcheck M3); an unknown ID with the log
// unchanged is walked as ordinary mail (the log is not re-read: one stat). With the log
// unreadable, only marked containers with no entry of their parent name are left out.
func TestBackupSkipSetRefreshAndUnreadableLog(t *testing.T) {
	b := newRestoreBed(t)
	early := tbrestore.LoadSkipSet(b.log.Path) // before any restore
	top := b.restore("Late", "late restore's mail", b.log)
	_ = os.Remove(filepath.Join(b.local, top))
	subjects, left := b.walk(early)
	if has(subjects, "late restore's mail") || len(left) != 1 {
		t.Fatalf("a restore that began after the set was built is left out after a re-read: %v %v", subjects, left)
	}

	other := tbrestore.NewLog(filepath.Join(t.TempDir(), tbrestore.LogName))
	stranger := b.restore("Stranger", "stranger's mail", other)
	_ = os.Remove(filepath.Join(b.local, stranger))
	subjects, _ = b.walk(tbrestore.LoadSkipSet(b.log.Path))
	if !has(subjects, "stranger's mail") {
		t.Fatalf("an unknown ID is walked as ordinary mail: %v", subjects)
	}

	committed := b.restore("Committed", "committed mail", b.log)
	_ = committed
	if err := os.WriteFile(b.log.Path, []byte("not a log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unread := tbrestore.LoadSkipSet(b.log.Path)
	if !unread.LogUnreadable() {
		t.Fatal("the log is unreadable")
	}
	subjects, _ = b.walk(unread)
	if has(subjects, "late restore's mail") || has(subjects, "stranger's mail") {
		t.Errorf("unreadable log: marked containers with no parent are left out: %v", subjects)
	}
	if !has(subjects, "committed mail") {
		t.Errorf("unreadable log: a container with its parent beside it is backed up: %v", subjects)
	}
}
