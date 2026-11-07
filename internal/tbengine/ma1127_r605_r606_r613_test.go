package tbengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/tbrestore"
)

// profileWithLocal is a fake profile whose Local Folders is the absolute folder local,
// outside the profile, holding Thunderbird's own Trash.
func profileWithLocal(t *testing.T, local string) profile {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "p.default")
	write(t, filepath.Join(dir, "prefs.js"), "// Mozilla User Preferences\n"+strings.Join([]string{
		`user_pref("mail.accountmanager.localfoldersserver", "server1");`,
		`user_pref("mail.server.server1.directory", "` + strings.ReplaceAll(local, `\`, `\\`) + `");`,
		`user_pref("mail.server.server1.hostname", "Local Folders");`,
		`user_pref("mail.server.server1.type", "none");`,
		`user_pref("mail.server.server1.storeContractID", "@mozilla.org/msgstore/berkeleystore;1");`,
	}, "\n")+"\n")
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(local, "Trash")); err != nil {
		write(t, filepath.Join(local, "Trash"), "From - x\nSubject: old\n\nkept\n")
	}
	dir, _ = filepath.EvalSymlinks(dir)
	local, _ = filepath.EvalSymlinks(local)
	return profile{dir: dir, local: local}
}

// covers: MA-1127, R40, S67
// R6-05 (settled 2026-11-07): a profile folder moved aside after the re-check, with a
// decoy put at its path (its Local Folders elsewhere, so that one is still the plan's),
// is refused W-CHANGED once Thunderbird's lock is taken — the lock taken is the decoy's,
// not the pinned profile's — with nothing written and every lock free after. Positive
// twin: the same profile, untouched, restores.
func TestAProfileSwappedAfterTheRecheckIsRefused(t *testing.T) {
	out := plainArchive(t, true)
	twin := profileWithLocal(t, filepath.Join(t.TempDir(), "LF"))
	assure.Reached(t, mustRun(t, plan(t, Request{Out: out, Profile: twin.choice()}), Start{}).Done(), "positive twin: restores")

	pr := profileWithLocal(t, filepath.Join(t.TempDir(), "LF"))
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	moved := pr.dir + "-real"
	was := networkWarning
	n := 0
	networkWarning = func(lf tbrestore.LocalFolders) string {
		if n++; n == 1 { // the re-check's last step
			if err := os.Rename(pr.dir, moved); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(pr.dir, "prefs.js"), "// a decoy\n")
		}
		return ""
	}
	defer func() { networkWarning = was }()
	before := snapshot(t, pr.dir, pr.local)
	_, err := Run(context.Background(), p, Start{})
	// The real profile, now at moved, and Local Folders are as they were.
	refusedAs(t, err, "W-CHANGED", func() bool { return strings.ReplaceAll(snapshot(t, moved, pr.local), moved, pr.dir) == before }, "is not the folder it was", pr.dir)
	allFree(t, out, moved, "after the refusal")
}

// covers: MA-1127, R40, S67
// R6-06 (settled 2026-11-07): two listed profiles whose prefs name one Local Folders. The
// plan names the other one; while the restore writes, another process can take neither
// profile's Thunderbird lock; after it, both are free. With the other profile's Thunderbird
// open, nothing is written (W-RUNNING).
func TestTwoProfilesSharingLocalFoldersAreBothHeldOut(t *testing.T) {
	out := plainArchive(t, true)
	local := filepath.Join(t.TempDir(), "Shared LF")
	a := profileWithLocal(t, local)
	b := profileWithLocal(t, local)
	base := t.TempDir()
	profilesINI(t, base, []string{"A", "B"}, []string{a.dir, b.dir})
	choice := ProfileChoice{Dir: a.dir, Bases: []string{base}}
	p := plan(t, Request{Out: out, Profile: choice})
	assure.Reached(t, strings.Contains(codesOf(p), "SHARED") && strings.Contains(p.Text(), b.dir), fmt.Sprintf("the plan names the other profile: %s", p.Text()))
	var takes []string
	withFS(t, atFirstFolder(func() error {
		takes = childTakes(t, []string{filepath.Join(a.dir, tbrestore.ParentLockName), filepath.Join(b.dir, tbrestore.ParentLockName)})
		return nil
	}))
	r := mustRun(t, p, Start{})
	assure.Reached(t, r.Done() && strings.Join(takes, " ") == "refused refused", fmt.Sprintf("mid-write both Thunderbird locks are held: %v (%s)", takes, r.Words))
	got := childTakes(t, []string{filepath.Join(a.dir, tbrestore.ParentLockName), filepath.Join(b.dir, tbrestore.ParentLockName)})
	assure.Reached(t, strings.Join(got, " ") == "took took", fmt.Sprintf("after it both are free: %v", got))

	undo := tbrestore.SetProcessProbeForTest(func() (int, bool) { return 0, true })
	defer undo()
	hold := holdThunderbirdLock(t, b.dir)
	before := snapshot(t, a.dir, local)
	_, err := MakePlan(Request{Out: out, Profile: choice})
	refusedAs(t, err, "W-RUNNING", func() bool { return snapshot(t, a.dir, local) == before }, "Thunderbird is open")
	hold()
}

// makeReadOnly makes the file read-only for this account (Windows: its read-only
// attribute), as on another account's archive; makeWritable undoes it.
func makeReadOnly(t *testing.T, p string) {
	t.Helper()
	if err := os.Chmod(p, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o600) })
}

func makeWritable(t *testing.T, p string) {
	t.Helper()
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
}

// holdThunderbirdLock holds dir's Thunderbird lock in this process, as an open
// Thunderbird would hold it; the returned func lets it go.
func holdThunderbirdLock(t *testing.T, dir string) func() {
	t.Helper()
	l, err := tbrestore.TakeProfileLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	return l.Release
}

// covers: MA-1127, R40
// R6-13 (settled 2026-11-07, the lead's call): an archive whose lock this account cannot
// take is refused at the plan in words (W-ARCHLOCK), never a plan that says yes and a raw
// "permission denied" at Start; nothing written. On a medium that is genuinely read-only
// (the read-only check's seam: none can be made here) the restore is ALLOWED: the plan
// says it reads the archive without its lock, and the run restores without making or
// taking it. Positive twin: an ordinary archive restores under its lock.
func TestAnArchiveWhoseLockCannotBeTaken(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("root can open any file")
	}
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	lock := filepath.Join(out, lockfile.Name)
	if _, err := os.Stat(lock); err != nil {
		write(t, lock, "")
	}
	makeReadOnly(t, lock)
	before := snapshot(t, pr.dir)
	_, err := MakePlan(Request{Out: out, Profile: pr.choice()})
	refusedAs(t, err, "W-ARCHLOCK", func() bool { return snapshot(t, pr.dir) == before }, "cannot take this archive's lock", out, "click Check again")
	assure.Reached(t, !strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "Access is denied"), "the words carry no raw error: "+err.Error())

	was := readOnlyVolume
	readOnlyVolume = func(string) bool { return true }
	defer func() { readOnlyVolume = was }()
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, p.ReadOnly && strings.Contains(codesOf(p), "READONLY"), fmt.Sprintf("a read-only medium is read without its lock, and the plan says so: %s", codesOf(p)))
	r := mustRun(t, p, Start{})
	assure.Reached(t, r.Done(), "restored from a read-only archive: "+r.Words)
	readOnlyVolume = was
	makeWritable(t, lock)
	assure.Reached(t, mustRun(t, plan(t, Request{Out: out, Profile: newProfile(t, "").choice()}), Start{}).Done(), "positive twin: an ordinary archive restores under its lock")
}
