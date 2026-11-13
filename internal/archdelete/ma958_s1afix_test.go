package archdelete

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
)

// skipWithoutPermissions skips where a file's mode does not keep this account from
// reading it: Windows (mode bits are only read-only) and root.
func skipWithoutPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows denies reading through ACLs, not mode bits (lab: a manifest whose ACL denies this account)")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
}

// unlistedAsCurrent is UnlistRefusal of dir while it is the current archive.
func unlistedAsCurrent(dir string) error {
	deps := testDeps()
	deps.Settings.Current = dir
	return UnlistRefusal(dir, deps)
}

// covers: MA-958, R36
// A manifest that is there but cannot be read (here: no permission; on Windows also
// a sharing violation while a backup or an antivirus scan holds it) is not "no
// longer an archive" and not "reconnect": the plan names the file and its cause,
// and Remove from list of the archive in use stays refused (del-s1a-A1-03).
func TestAManifestThatCannotBeReadIsNotCalledNoArchive(t *testing.T) {
	skipWithoutPermissions(t)
	dir := smallArchive(t, t.TempDir())
	// Positive twin first: readable, it is planned and Remove of it while current is
	// refused as the archive in use.
	mustPlan(t, dir, testDeps())
	if r := AsRefusal(unlistedAsCurrent(dir)); r == nil || r.Case != CaseCurrentArchive {
		t.Fatalf("Remove from list of the current archive: %v", r)
	}
	manifest := filepath.Join(dir, manifestName)
	if err := os.Chmod(manifest, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(manifest, 0o644) })
	{
		before := statTree(t, dir) // the manifest cannot be read, so not hashed
		rc, msg, c := refusalOf(t, dir, testDeps())
		if c != CaseUnreadable {
			t.Fatalf("case = %q (%s)", c, msg)
		}
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("MailArchive cannot read "+manifest, "permission denied", "then click Delete again."),
			assure.NoSideEffect(func() bool { return sameTree(before, statTree(t, dir)) }))
	}
	r := AsRefusal(unlistedAsCurrent(dir))
	if r == nil || r.Case != CaseCurrentArchive {
		t.Fatalf("Remove from list of the archive in use must stay refused while its manifest cannot be read, got %v", r)
	}
	assure.Refused(t, r.ExitCode(), r.Message, assure.Code(1), assure.Names("This is the archive in use"))
}

// covers: MA-958, R36
// A manifest a newer MailArchive wrote (a synced archive, an older build after a
// rollback) is an archive this build cannot list: the plan says so and how to
// remedy it, never "no longer holds an archive", and Remove from list of it while
// current stays refused (del-s1a-A2-03, A3-05).
func TestANewerManifestIsNotCalledNoArchive(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, 9, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}})
	{
		rc, msg, opts := refusedCase(t, dir, testDeps(), CaseNewerFormat, "written by a newer version of MailArchive", "format 9", "Update MailArchive, then delete it.")
		assure.Refused(t, rc, msg, opts...)
	}
	if r := AsRefusal(unlistedAsCurrent(dir)); r == nil || r.Case != CaseCurrentArchive {
		t.Fatalf("Remove from list of the archive in use must be refused, got %v", r)
	}
	// Positive twin: with no manifest at all the current folder may be unlisted, and
	// an unreachable one too (§5.2).
	empty := t.TempDir()
	if err := unlistedAsCurrent(empty); err != nil {
		t.Fatalf("a current folder with no archive may be removed from the list: %v", err)
	}
	if err := unlistedAsCurrent(filepath.Join(empty, "unplugged")); err != nil {
		t.Fatalf("an unreachable current archive may be removed from the list: %v", err)
	}
}

// covers: MA-958, R36
// A manifest over the reader's cap is too large to read, not "no longer an
// archive" (conformance D2); the cap is lowered here to stand in for 1 GiB.
func TestAManifestOverTheCapIsTooLargeToRead(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	mustPlan(t, dir, testDeps()) // positive twin: under the cap
	old := manifestCap
	manifestCap = 16
	t.Cleanup(func() { manifestCap = old })
	rc, msg, opts := refusedCase(t, dir, testDeps(), CaseUnreadable, filepath.Join(dir, manifestName), "is larger than MailArchive can read", "Delete the archive's files by hand.")
	assure.Refused(t, rc, msg, opts...)
	if r := AsRefusal(unlistedAsCurrent(dir)); r == nil || r.Case != CaseCurrentArchive {
		t.Fatalf("Remove from list of the archive in use must be refused, got %v", r)
	}
}

// covers: MA-958, R36
// A folder inside the archive this account cannot list (a person's private folder
// in a store tree) refuses with that folder named and its cause, never "Reconnect
// the drive" for a folder that is plainly there (del-s1a-A2-06).
func TestAFolderInsideTheArchiveThatCannotBeListedIsNamed(t *testing.T) {
	skipWithoutPermissions(t)
	dir := smallArchive(t, t.TempDir())
	private := filepath.Join(dir, "Mailbox", "Private")
	writeFile(t, filepath.Join(private, "diary.txt"), "mine")
	mustPlan(t, dir, testDeps()) // positive twin: readable
	if err := os.Chmod(private, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(private, 0o755) })
	_, err := Plan(dir, testDeps())
	r := AsRefusal(err)
	if r == nil || r.Case != CaseUnreadable {
		t.Fatalf("want the unreadable refusal, got %v", err)
	}
	assure.Refused(t, r.ExitCode(), r.Message, assure.Code(1), assure.Names("MailArchive cannot read "+private, "permission denied"),
		assure.NoSideEffect(func() bool {
			fi, err := os.Lstat(private)
			return err == nil && fi.Mode().Perm() == 0
		}))
}
