package archdelete

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-960, R36
// The protected row through Plan for each folder it names that the first test does
// not reach: the scratch folder and this computer's record of encrypted archives
// (is, contains or is inside), a system folder (is or contains) and a drive root
// (conformance gap 8).
func TestEveryProtectedFolderIsRefusedThroughThePlan(t *testing.T) {
	// Positive twin first: an ordinary archive is planned with the real protected set.
	mustPlan(t, smallArchive(t, t.TempDir()), testDeps())

	scratch, err := job.ScratchDir()
	if err != nil {
		t.Fatal(err)
	}
	record, err := encrecord.Dir()
	if err != nil {
		t.Fatal(err)
	}
	// Each alone (on Linux the scratch folder lies inside the settings folder, which
	// would be found first): a folder inside it, and a folder holding it.
	for _, tc := range []struct {
		dir  string
		only func(string) Protected
	}{
		{scratch, func(d string) Protected { return Protected{Scratch: d} }},
		{record, func(d string) Protected { return Protected{Record: d} }},
	} {
		deps := testDeps()
		deps.Protected = tc.only(tc.dir)
		inside := smallArchive(t, filepath.Join(tc.dir, "archive-inside"))
		rc, msg, opts := refusedCase(t, inside, deps, CaseProtected, "MailArchive will not delete this folder", tc.dir)
		assure.Refused(t, rc, msg, opts...)
		holder := smallArchive(t, t.TempDir())
		held := filepath.Join(holder, "held")
		if err := os.MkdirAll(held, 0o755); err != nil {
			t.Fatal(err)
		}
		deps.Protected = tc.only(held)
		rc, msg, opts = refusedCase(t, holder, deps, CaseProtected, held)
		assure.Refused(t, rc, msg, opts...)
	}

	// A system folder, and a folder holding one (the set injected: the real ones are
	// the operating system's, checked below where they exist).
	holder := smallArchive(t, t.TempDir())
	sys := filepath.Join(holder, "Windows")
	if err := os.MkdirAll(sys, 0o755); err != nil {
		t.Fatal(err)
	}
	deps := testDeps()
	deps.Protected.System = []string{sys}
	{
		rc, msg, opts := refusedCase(t, holder, deps, CaseProtected, sys)
		assure.Refused(t, rc, msg, opts...)
	}
	for _, real := range systemFolders() {
		if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
			continue
		}
		_, err := Plan(real, testDeps())
		if r := AsRefusal(err); r == nil || r.Case != CaseProtected {
			t.Fatalf("the system folder %s must be refused as protected, got %v", real, err)
		}
		break
	}

	// A drive root, through Plan, with no other protected folder to find first.
	root := filepath.VolumeName(scratch) + string(filepath.Separator)
	_, err = Plan(root, Deps{})
	r := AsRefusal(err)
	if r == nil || r.Case != CaseProtected {
		t.Fatalf("a drive root must be refused as protected, got %v", err)
	}
	assure.Refused(t, r.ExitCode(), r.Message, assure.Code(1), assure.Names("will not delete this folder", root))
}

// covers: MA-960, R36
// A store tree holding another archive is refused as mixed whichever of the
// encrypted marker's signals it keeps: its sealed record's mirror only, or a units/
// folder in the unit store's layout (a nested encrypted archive whose key file was
// removed by hand) — never walked, its temps never listed (del-s1a-A1-08).
func TestAStoreTreeHoldingAnEncryptedArchivesRemainsIsMixed(t *testing.T) {
	for _, tc := range []struct{ name, file string }{
		{"the pointer's mirror", container.MirrorName},
		{"the inventory file", container.InventoryName},
		{"a unit store", "units/ab/" + unitName},
		{"an inventory folder", "inventory/0a.1"},
	} {
		dir := smallArchive(t, t.TempDir())
		// Positive twin first: the same folder without the signal is walked.
		nested := filepath.Join(dir, "Mailbox", "Old")
		writeFile(t, filepath.Join(nested, ".mailarchive-3.tmp"), "a nested archive's temp")
		if p := mustPlan(t, dir, testDeps()); !contains(removeRels(p), "Mailbox/Old/.mailarchive-3.tmp") {
			t.Fatalf("%s: twin remove %q", tc.name, removeRels(p))
		}
		writeFile(t, filepath.Join(nested, filepath.FromSlash(tc.file)), "sealed")
		rc, msg, opts := refusedCase(t, dir, testDeps(), CaseMixed, "This folder holds files of more than one kind. Delete it by hand.")
		assure.Refused(t, rc, msg, opts...)
	}
}

// covers: MA-960, R36
// §5.1 (c): deleting the current archive Y while its Microsoft 365 capture is
// registered under another folder X's name stops that entry too — the plan names it
// among the schedules Delete stops, and X is not one of Y's spellings
// (del-s1a-A3-04).
func TestDeletingTheCurrentArchiveNamesItsCaptureRegisteredElsewhere(t *testing.T) {
	x := smallArchive(t, t.TempDir())
	y := smallArchive(t, t.TempDir())
	captured := schedule.DefaultNameFor(x)
	deps := testDeps()
	deps.Discover = FileDiscovery{}
	// Positive twin first: no BackupFor, no such entry; and Y not current, none.
	deps.Settings = Settings{Current: y}
	if p := mustPlan(t, y, deps); contains(p.Schedules.Names(), captured) {
		t.Fatalf("no BackupFor: names %q", p.Schedules.Names())
	}
	deps.Settings = Settings{Current: x, CaptureBackupFor: x}
	if p := mustPlan(t, y, deps); contains(p.Schedules.Names(), captured) {
		t.Fatalf("Y not current: names %q", p.Schedules.Names())
	}

	deps.Settings = Settings{Current: y, CaptureBackupFor: x}
	p := mustPlan(t, y, deps)
	var found *Entry
	for i, e := range p.Schedules.Entries {
		if e.Name == captured {
			found = &p.Schedules.Entries[i]
		}
	}
	if found == nil || found.RegisteredFor != x || found.Out != "" {
		t.Fatalf("the capture registered under %s must be stopped: %+v", x, p.Schedules.Entries)
	}
	if contains(p.Schedules.Spellings, x) {
		t.Fatalf("another folder is not one of this archive's spellings: %q", p.Schedules.Spellings)
	}
	text := string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: p}))
	if !containsLine(text, "Schedule to stop: "+captured) {
		t.Fatalf("the saved plan names it:\n%s", text)
	}
}
