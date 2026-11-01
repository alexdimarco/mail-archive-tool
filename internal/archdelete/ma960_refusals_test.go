package archdelete

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/vault"
)

// smallArchive is a plain archive with one message.
func smallArchive(t *testing.T, dir string) string {
	t.Helper()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}})
	writeFile(t, filepath.Join(dir, filepath.FromSlash(stemA+".html")), "page")
	return dir
}

// refusedCase plans dir and returns the refusal with what §7 asks of it, for
// assure.Refused in the test: exit code 1, the needles named, the folder
// byte-identical. A refusal of another case fails the test here.
func refusedCase(t *testing.T, dir string, deps Deps, want Case, needles ...string) (int, string, []assure.RefuseOption) {
	t.Helper()
	before := tree(t, dir)
	rc, msg, c := refusalOf(t, dir, deps)
	if c != want {
		t.Fatalf("case = %q, want %q (%s)", c, want, msg)
	}
	return rc, msg, []assure.RefuseOption{assure.Code(1), assure.Names(needles...),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) })}
}

// covers: MA-960, R36
func TestAnUnreachableFolderIsRefused(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "unplugged")
	rc, msg, c := refusalOf(t, gone, testDeps())
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("Reconnect the drive or folder first: nothing can be checked."))
	if c != CaseUnreachable {
		t.Fatalf("case = %s", c)
	}
}

// covers: MA-960, R36
func TestProtectedFoldersAreRefusedByIdentity(t *testing.T) {
	// Positive twin first: an ordinary folder is planned.
	mustPlan(t, smallArchive(t, t.TempDir()), testDeps())

	cfg, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	inCfg := smallArchive(t, filepath.Join(cfg, "archive-inside-settings"))
	{
		rc, msg, opts := refusedCase(t, inCfg, testDeps(), CaseProtected, "MailArchive will not delete this folder", cfg, "Delete the archive's files by hand.")
		assure.Refused(t, rc, msg, opts...)
	}

	// Another spelling of the settings folder: through a link above it.
	alias := filepath.Join(t.TempDir(), "alias")
	symlinkOrSkip(t, filepath.Dir(cfg), alias)
	{
		rc, msg, opts := refusedCase(t, filepath.Join(alias, filepath.Base(cfg), "archive-inside-settings"), testDeps(), CaseProtected, "will not delete this folder")
		assure.Refused(t, rc, msg, opts...)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		// …and in another case, where the file system ignores case.
		rc, msg, opts := refusedCase(t, strings.ToUpper(inCfg[:len(inCfg)-len("archive-inside-settings")])+"archive-inside-settings", testDeps(), CaseProtected, "will not delete this folder")
		assure.Refused(t, rc, msg, opts...)
	}

	// A folder that holds the home folder.
	holder := smallArchive(t, t.TempDir())
	deps := testDeps()
	deps.Protected.Home = filepath.Join(holder, "Users", "me")
	if err := os.MkdirAll(deps.Protected.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	{
		rc, msg, opts := refusedCase(t, holder, deps, CaseProtected, deps.Protected.Home)
		assure.Refused(t, rc, msg, opts...)
	}

	// A folder inside another known archive, and one holding it.
	outer := smallArchive(t, t.TempDir())
	inner := smallArchive(t, filepath.Join(outer, "nested"))
	deps = testDeps()
	deps.Protected.Archives = []string{outer, inner}
	{
		rc, msg, opts := refusedCase(t, inner, deps, CaseProtected, outer)
		assure.Refused(t, rc, msg, opts...)
	}
	{
		rc, msg, opts := refusedCase(t, outer, deps, CaseProtected, inner)
		assure.Refused(t, rc, msg, opts...)
	}

	// A drive root.
	if err := checkProtected(filepath.VolumeName(cfg)+string(filepath.Separator), Protected{}); AsRefusal(err) == nil || AsRefusal(err).Case != CaseProtected {
		t.Fatalf("a drive root must be protected, got %v", err)
	}
}

// covers: MA-960, R36
func TestAMixedFolderIsRefused(t *testing.T) {
	// A plaintext manifest beside an encrypted archive's key file.
	dir := smallArchive(t, t.TempDir())
	writeFile(t, filepath.Join(dir, vault.FileName), "{}")
	{
		rc, msg, opts := refusedCase(t, dir, testDeps(), CaseMixed, "This folder holds files of more than one kind. Delete it by hand.")
		assure.Refused(t, rc, msg, opts...)
	}

	// Another archive's marker in a store tree.
	dir = smallArchive(t, t.TempDir())
	writeFile(t, filepath.Join(dir, "Mailbox", "Inbox", "Old", manifestName), `{"version":6,"entries":{}}`)
	{
		rc, msg, opts := refusedCase(t, dir, testDeps(), CaseMixed, "more than one kind")
		assure.Refused(t, rc, msg, opts...)
	}
}

// covers: MA-960, R36
func TestARunningBackupIsRefused(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	deps := testDeps()
	deps.Busy = func(string) bool { return false }
	mustPlan(t, dir, deps) // positive twin
	deps.Busy = func(f string) bool { return sameFolder(f, dir, runtime.GOOS) }
	{
		rc, msg, opts := refusedCase(t, dir, deps, CaseBusy, "A backup of this archive is running. Try again when it ends.")
		assure.Refused(t, rc, msg, opts...)
	}
}

// covers: MA-960, R36
func TestAnUnreadableDescriptorIsRefusedWithItsRemedy(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	deps := testDeps()
	deps.Discover = FileDiscovery{}
	mustPlan(t, dir, deps) // positive twin: no descriptor
	desc := filepath.Join(dir, schedule.DescriptorName)
	writeFile(t, desc, "{this is not json")
	{
		rc, msg, opts := refusedCase(t, dir, deps, CaseDescriptor, "This archive's schedule record "+desc+" cannot be read, so MailArchive cannot tell which schedule it records. If no schedule for this archive is installed, delete "+desc+", then click Delete again; otherwise reinstall that schedule to rewrite it.")
		assure.Refused(t, rc, msg, opts...)
	}
	// The remedy followed literally lets the next plan go ahead.
	if err := os.Remove(desc); err != nil {
		t.Fatal(err)
	}
	mustPlan(t, dir, deps)
}

// covers: MA-960, R36
func TestTheCurrentArchivesCaptureRegisteredHereIsRefused(t *testing.T) {
	x := smallArchive(t, t.TempDir())
	y := smallArchive(t, t.TempDir())
	deps := testDeps()
	// Positive twin: the current archive itself, whose capture is registered under
	// its own name, is planned (§5.1 (c) is the delete's).
	deps.Settings = Settings{Current: x, CaptureBackupFor: x}
	mustPlan(t, x, deps)
	if err := UnlistRefusal(y, deps); err != nil {
		t.Fatalf("Remove from list of another archive: %v", err)
	}

	deps.Settings = Settings{Current: y, CaptureBackupFor: x}
	{
		rc, msg, opts := refusedCase(t, x, deps, CaseBackupFor, "The scheduled backup of your current archive "+y+" is still registered under this folder's name.",
			"In Settings, remove the scheduled backup and set it up again (it is then registered under "+y+"), then delete this one.")
		assure.Refused(t, rc, msg, opts...)
	}
	r := AsRefusal(UnlistRefusal(x, deps))
	if r == nil {
		t.Fatal("Remove from list must be refused too (§5.1 (b))")
	}
	assure.Refused(t, r.ExitCode(), r.Message, assure.Code(1), assure.Names("still registered under this folder's name", "then remove this one from the list."))
}

// covers: MA-960, R36
func TestADesktopStartedWithOutNamingTheFolderIsRefused(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	deps := testDeps()
	deps.Settings.LaunchOut = t.TempDir()
	mustPlan(t, dir, deps) // positive twin: -out names another folder
	deps.Settings.LaunchOut = dir
	{
		rc, msg, opts := refusedCase(t, dir, deps, CaseLaunchOut, "Restart MailArchive without -out, then delete.")
		assure.Refused(t, rc, msg, opts...)
	}
	r := AsRefusal(UnlistRefusal(dir, deps))
	if r == nil {
		t.Fatal("Remove from list must be refused too")
	}
	assure.Refused(t, r.ExitCode(), r.Message, assure.Code(1), assure.Names("Restart MailArchive without -out, then remove it from the list."))
}
