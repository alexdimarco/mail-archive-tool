package tbengine

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/tbrestore"
)

// plaintextOutside lists every file under roots, outside skip, that holds the canary:
// plaintext mail where none may be.
func plaintextOutside(t *testing.T, skip string, roots ...string) []string {
	t.Helper()
	var hits []string
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if skip != "" && (p == skip || strings.HasPrefix(p, skip+string(filepath.Separator))) {
				return fs.SkipDir
			}
			if d.Type().IsRegular() {
				if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte(canary)) {
					hits = append(hits, p)
				}
			}
			return nil
		})
	}
	return hits
}

// scanRoots are where plaintext must never land: the temp folder the run sees, MailArchive's
// cache and scratch folders, its settings folder (restores.log, the locks), the home folder,
// the archive, and the profile outside its Local Folders.
func scanRoots(t *testing.T, tmp, out string, pr profile) []string {
	t.Helper()
	roots := []string{tmp, out, pr.dir, os.Getenv("HOME")}
	if d, err := job.ScratchDir(); err == nil {
		roots = append(roots, filepath.Dir(d))
	}
	if d, err := job.Dir(); err == nil {
		roots = append(roots, d)
	}
	return roots
}

// noFolderNames reports s names none of the archive's folders.
func noFolderNames(s string) bool {
	return !strings.Contains(s, folderA) && !strings.Contains(s, folderB) && !strings.Contains(s, folderC)
}

func logText(t *testing.T) string {
	t.Helper()
	lp, _ := tbrestore.LogPath()
	b, _ := os.ReadFile(lp)
	return string(b)
}

// covers: MA-1128, R37, R40, S72
// An encrypted archive is restored only while unlocked, through a read lease of its own
// (design §6.2, Q6, Q8): the plan says the restored mail is NOT encrypted; Start without
// the tick is W-TICK with nothing written; with it, the restore runs to its end even
// though the archive is locked while it writes; each original is decrypted in memory, so
// a scan of the temp, cache, scratch and settings folders, the home folder, the archive
// and the profile outside Local Folders finds the plaintext before, during and after only
// inside Local Folders; the progress and restores.log name no folder.
func TestAnEncryptedArchiveIsRestoredThroughItsLease(t *testing.T) {
	out, u := encryptedArchive(t)
	pr := newProfile(t, "")
	tmp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}
	roots := scanRoots(t, tmp, out, pr)
	assure.Reached(t, len(plaintextOutside(t, pr.local, roots...)) == 0, fmt.Sprintf("before: no plaintext anywhere scanned"))

	p := plan(t, Request{Out: out, Keys: u, Profile: pr.choice()})
	codes := strings.Join(p.Codes(), " ")
	assure.Reached(t, strings.Contains(codes, "NOT-ENCRYPTED") && !strings.Contains(codes, "W-CLOUD"), fmt.Sprintf("the plan says the restored mail is not encrypted: %s", codes))

	before := snapshot(t, pr.dir)
	_, err := Run(context.Background(), p, Start{})
	refusedAs(t, err, "W-TICK", func() bool { return snapshot(t, pr.dir) == before }, "will not be encrypted", "not", "ticked")

	var during []string
	var folders []string
	lockAt := atFirstFolder(func() error {
		u.Lock() // a Lock after the start does not stop it (Q6)
		return nil
	})
	scanned := false
	withFS(t, func(op, name string) error {
		if op == "open" && !scanned { // the post-write check: every message decrypted by now
			scanned = true
			during = plaintextOutside(t, pr.local, roots...)
		}
		return lockAt(op, name)
	})
	res := mustRun(t, p, Start{NotEncryptedOK: true, Progress: func(g Progress) { folders = append(folders, g.Folder) }})
	assure.Reached(t, res.Done() && res.Restored == 4, fmt.Sprintf("the restore ran to its end through its lease, the archive locked meanwhile: %s", res.Words))
	assure.Reached(t, scanned && len(during) == 0, fmt.Sprintf("during: no plaintext outside Local Folders: %v", during))
	assure.Reached(t, len(plaintextOutside(t, pr.local, roots...)) == 0, fmt.Sprintf("after: no plaintext outside Local Folders: %v", plaintextOutside(t, pr.local, roots...)))
	assure.Reached(t, len(plaintextOutside(t, "", pr.local)) > 0, fmt.Sprintf("the plaintext is in Local Folders, where it was restored"))
	assure.Reached(t, strings.Join(folders, "") == "" && len(folders) == 4, fmt.Sprintf("progress is counts only: %q", folders))
	assure.Reached(t, noFolderNames(logText(t)) && noFolderNames(res.Words), fmt.Sprintf("restores.log and the result name no folder"))
}

// covers: MA-1128, R37, R40, S72
// A check failure, an error, of an encrypted archive's restore names no file or folder:
// not in restores.log, not in the words (the writer says only the error's kind).
func TestAnEncryptedRestoresFailureNamesNoFolder(t *testing.T) {
	out, u := encryptedArchive(t)
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Keys: u, Profile: pr.choice()})
	withFS(t, func(op, name string) error {
		if op == "open" && strings.HasSuffix(name, folderB) {
			os.Truncate(filepath.Join(pr.local, filepath.FromSlash(name)), 3) // antivirus took part of it
		}
		return nil
	})
	res := mustRun(t, p, Start{NotEncryptedOK: true})
	assure.Reached(t, res.Write.Outcome == tbrestore.OutCheckFail, fmt.Sprintf("the post-write check caught it: %s", res.Write.Outcome))
	assure.Reached(t, noFolderNames(logText(t)) && noFolderNames(res.Words), fmt.Sprintf("no folder named: log %q, words %q", logText(t), res.Words))

	pr2 := newProfile(t, "")
	p2 := plan(t, Request{Out: out, Keys: u, Profile: pr2.choice()})
	withFS(t, atFirstFolder(func() error { return &os.PathError{Op: "open", Path: folderA + "/" + folderB, Err: fs.ErrPermission} }))
	res2 := mustRun(t, p2, Start{NotEncryptedOK: true})
	assure.Reached(t, res2.Write.Outcome == tbrestore.OutFailed && noFolderNames(logText(t)) && noFolderNames(res2.Words), fmt.Sprintf("an error's text is not shown or logged: %q", res2.Words))
}

// covers: MA-1128, R37, R40, S72
// A locked archive is not restored (W-LOCKED, nothing written): locked before Check, and
// locked between Check and Restore (the plan survives a Lock, Start refuses it, §7.3).
func TestALockedArchiveIsNotRestored(t *testing.T) {
	out, u := encryptedArchive(t)
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Keys: u, Profile: pr.choice()})
	assure.Reached(t, p.Messages, "positive twin: unlocked, it plans the archive's messages")
	u.Lock()
	before := snapshot(t, pr.dir)
	_, err := Run(context.Background(), p, Start{NotEncryptedOK: true})
	refusedAs(t, err, "W-LOCKED", func() bool { return snapshot(t, pr.dir) == before }, "This archive is locked", "Unlock it")
	_, err = MakePlan(Request{Out: out, Keys: u, Profile: pr.choice()})
	refusedAs(t, err, "W-LOCKED", func() bool { return snapshot(t, pr.dir) == before }, "This archive is locked")
}

// covers: MA-1128, R37, R40, S72
// When the Local Folders may leave this computer — in a folder a cloud service syncs, or on
// a removable or external drive (the copy's own test, lifted: app.LeaveReasons) — an
// encrypted archive's plan lists why and asks twice: Start with the tick but without
// ConfirmAgain is refused with nothing written; with both, it restores. An unencrypted
// archive in a synced folder is warned (W-CLOUD), not asked twice.
func TestAskTwiceWhenLocalFoldersMayLeaveThisComputer(t *testing.T) {
	out, u := encryptedArchive(t)
	synced := newProfile(t, filepath.Join(t.TempDir(), "Dropbox"))
	p := plan(t, Request{Out: out, Keys: u, Profile: synced.choice()})
	assure.Reached(t, p.AskTwice() && strings.Contains(p.Text(), "is synced to Dropbox"), fmt.Sprintf("a synced Local Folders asks twice: %v", p.Leaves))
	before := snapshot(t, synced.dir)
	_, err := Run(context.Background(), p, Start{NotEncryptedOK: true})
	refusedAs(t, err, "W-NOTAGAIN", func() bool { return snapshot(t, synced.dir) == before }, "Dropbox", "not confirmed a second time")
	assure.Reached(t, mustRun(t, p, Start{NotEncryptedOK: true, ConfirmAgain: true}).Done(), fmt.Sprintf("confirmed twice, it restores"))

	removable := newProfile(t, "")
	leaveEnv.SetRemovableForTest(func(string) bool { return true })
	defer leaveEnv.SetRemovableForTest(nil)
	p = plan(t, Request{Out: out, Keys: u, Profile: removable.choice()})
	assure.Reached(t, p.AskTwice() && strings.Contains(strings.Join(p.Leaves, " "), "removable or external drive"), fmt.Sprintf("a removable drive asks twice: %v", p.Leaves))
	_, err = Run(context.Background(), p, Start{NotEncryptedOK: true})
	refusedAs(t, err, "W-NOTAGAIN", nil, "removable or external drive")
	leaveEnv.SetRemovableForTest(nil)

	plain := plainArchive(t, true)
	cloud := newProfile(t, filepath.Join(t.TempDir(), "Dropbox"))
	pp := plan(t, Request{Out: plain, Profile: cloud.choice()})
	assure.Reached(t, !pp.AskTwice() && strings.Contains(strings.Join(pp.Codes(), " "), "W-CLOUD"), fmt.Sprintf("an unencrypted archive is warned, not asked twice: %v", pp.Codes()))
}
