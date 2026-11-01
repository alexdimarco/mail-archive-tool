package archdelete

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
)

// covers: MA-957, R36
// A planted record whose path runs through the person's regular file never puts
// that file among the folders Delete may remove (os.Remove deletes a file too):
// only folders checkPath found to be real folders are passed (del-s1a-A2-01).
func TestAPlantedRecordThroughAPersonsFileNamesNoFileAsAFolder(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{
		{Path: stemA + ".html"},
		{Path: "taxes.pdf/2024-01-01_0000_x_12345678.html"},
		{Path: "Mailbox/notes.txt/2024-01-01_0000_y_87654321.html"},
	})
	for _, f := range []string{stemA + ".html", "taxes.pdf", "Mailbox/notes.txt"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), "x "+f)
	}
	p := mustPlan(t, dir, testDeps())
	// Positive twin first: the record's real folders are there, deepest first.
	if d := setDiff("folders", p.Folders, []string{"Mailbox/Inbox", "Mailbox"}); d != "" {
		t.Fatal(d)
	}
	for _, f := range p.Folders {
		fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil || !fi.IsDir() {
			t.Fatalf("Folders names %s, which is not a folder (%v)", f, err)
		}
	}
	for _, kept := range []string{"taxes.pdf", "Mailbox/notes.txt"} {
		if !contains(p.Kept, kept) {
			t.Fatalf("%s must be kept, kept = %q", kept, p.Kept)
		}
	}
}

// covers: MA-957, R36
// The exporter writes names composed (NFC); a drive that stores them decomposed
// (HFS+, or a copy from one) lists them decomposed. A recorded file is listed once,
// not again as a mail copy the archive did not record, its folder once, its
// folder's litter cleared, and the store folder is not also kept as the person's
// (del-s1a-A2-02, A3-02, A1-10). APFS, like HFS+, finds a composed name for a
// decomposed one, so this runs on macOS; Linux and NTFS keep the two forms apart.
func TestNamesStoredDecomposedOnDiskAreListedOnce(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("needs a file system that finds a composed name for a decomposed one (macOS; HFS+ drives: lab)")
	}
	const (
		storeNFC = "Persönliche Ordner"
		storeNFD = "Perso\u0308nliche Ordner"
		subNFC   = "Entwürfe"
		subNFD   = "Entwu\u0308rfe"
		stemNFC  = "2024-03-01_0900_grüße_ab12cd34"
		stemNFD  = "2024-03-01_0900_gru\u0308ße_ab12cd34"
	)
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m.pst": storeNFC}, []rec{{Path: storeNFC + "/" + subNFC + "/" + stemNFC + ".html"}})
	for _, f := range []string{stemNFD + ".html", stemNFD + ".eml", ".DS_Store"} {
		writeFile(t, filepath.Join(dir, storeNFD, subNFD, f), "x")
	}
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	decomposed := false
	for _, e := range names {
		decomposed = decomposed || e.Name() == storeNFD
	}
	if !decomposed {
		t.Skip("this volume normalises names it stores; the decomposed listing is not reproduced here")
	}
	p := mustPlan(t, dir, testDeps())
	if n := len(p.Remove); n != 3 {
		t.Fatalf("remove = %q, want the page, the .eml and the manifest once each", removeRels(p))
	}
	if len(p.Unrecorded) != 0 {
		t.Fatalf("a recorded file listed again as not recorded: %q", unrecordedRels(p))
	}
	if len(p.Folders) != 2 {
		t.Fatalf("each folder once: %q", p.Folders)
	}
	if len(p.Litter) != 1 {
		t.Fatalf("the recorded folder's litter is cleared: litter %q kept %q", p.Litter, p.Kept)
	}
	if len(p.Kept) != 0 {
		t.Fatalf("nothing of the store is the person's: kept %q", p.Kept)
	}
}

// covers: MA-957, R36
// runlog.Open keeps one rotated generation of a run log (<log>.1): the archive's
// mailarchive.log.1 and each discovered schedule's <name>.log.1 are the archive's;
// a .1 of a name nothing discovered is the person's (del-s1a-A2-04).
func TestRotatedRunLogsAreTheArchives(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	def := schedule.DefaultNameFor(dir)
	for _, f := range []string{"mailarchive.log", "mailarchive.log.1", def + ".log", def + ".log.1", "notes.log.1"} {
		writeFile(t, filepath.Join(dir, f), "log")
	}
	deps := testDeps()
	deps.Discover = FileDiscovery{}
	p := mustPlan(t, dir, deps)
	for _, want := range []string{"mailarchive.log", "mailarchive.log.1", def + ".log", def + ".log.1"} {
		if !contains(removeRels(p), want) {
			t.Fatalf("%s must be removed: %q", want, removeRels(p))
		}
	}
	if !contains(p.Kept, "notes.log.1") {
		t.Fatalf("an undiscovered name's rotated log is the person's: kept %q", p.Kept)
	}
}

// covers: MA-957, R36
// The root state files that keep an archive as it was go after every other name,
// just before the manifest: a delete stopped part way leaves the history, the
// schedule records and, last, the pause seal (del-s1a-A2-05).
func TestStateFilesThatKeepTheArchiveAsItWasGoLast(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	for _, f := range []string{state.HistoryName, schedule.DescriptorName, schedule.VerifyDescriptorName, seal.Name,
		".mailarchive-lastrun.json", "search.db", "zz-README.txt"} {
		writeFile(t, filepath.Join(dir, f), "{}")
	}
	p := mustPlan(t, dir, testDeps())
	rels := removeRels(p)
	n := len(rels)
	want := []string{state.HistoryName, schedule.VerifyDescriptorName, schedule.DescriptorName, seal.Name, manifestName}
	if n < len(want) {
		t.Fatalf("remove = %q", rels)
	}
	for i, w := range want {
		if got := rels[n-len(want)+i]; got != w {
			t.Fatalf("the last names must be %q, got %q", want, rels[n-len(want):])
		}
	}
	if !p.Paused {
		t.Fatal("the plan says the archive is paused")
	}
}

// covers: MA-957, R36
// Only the .pst files inside the _outlook-pst folder are the archive's: a regular
// file of that name is the person's (del-s1a-A2-09).
func TestAnOutlookPSTFileThatIsNotAFolderIsKept(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	writeFile(t, filepath.Join(dir, "_outlook-pst"), "a file, not the export folder")
	p := mustPlan(t, dir, testDeps())
	if contains(removeRels(p), "_outlook-pst") || !contains(p.Kept, "_outlook-pst") {
		t.Fatalf("remove %q kept %q", removeRels(p), p.Kept)
	}
	// Positive twin: the folder's .pst files are listed.
	twin := smallArchive(t, t.TempDir())
	writeFile(t, filepath.Join(twin, "_outlook-pst", "Personal.pst"), "pst")
	if p := mustPlan(t, twin, testDeps()); !contains(removeRels(p), "_outlook-pst/Personal.pst") {
		t.Fatalf("remove %q", removeRels(p))
	}
}

// covers: MA-957, R36
// A plain archive can be made in a folder of the person's own — Documents, whose
// desktop.ini carries its display name. At the archive folder's root desktop.ini
// (and macOS's custom-icon file) are the folder's own, kept, never litter; in a
// store folder desktop.ini is still litter, cleared (del-s1a-A1-04, for the
// operator: §4.4).
func TestTheFoldersOwnDesktopIniIsKept(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	writeFile(t, filepath.Join(dir, "desktop.ini"), "[.ShellClassInfo]\r\nLocalizedResourceName=@%SystemRoot%\\system32\\shell32.dll,-21770\r\n")
	writeFile(t, filepath.Join(dir, "Mailbox", "Inbox", "desktop.ini"), "litter")
	p := mustPlan(t, dir, testDeps())
	if !contains(p.Kept, "desktop.ini") || contains(p.Litter, "desktop.ini") {
		t.Fatalf("kept %q litter %q", p.Kept, p.Litter)
	}
	if !contains(p.Litter, "Mailbox/Inbox/desktop.ini") {
		t.Fatalf("a store folder's desktop.ini is litter: %q", p.Litter)
	}
}

// covers: MA-957, R36
// The mail's date range is read from every record: a record's Date is kept only
// when it has issues (state.Record), so the stem's date — the message's, in UTC —
// gives the rest (conformance gap 1).
func TestTheMailDatesComeFromTheStemsWhenRecordsCarryNoDate(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{
		{Path: "Mailbox/Inbox/2019-05-06_0700_old_11111111.html"},
		{Path: "Mailbox/Inbox/2024-03-01_0900_hello_ab12cd34.html"},
		{Path: "Mailbox/Inbox/0000-00-00_0000_undated_22222222.html"},
	})
	c := mustPlan(t, dir, testDeps()).Captured
	if !c.MailFrom.Equal(time.Date(2019, 5, 6, 7, 0, 0, 0, time.UTC)) || !c.MailTo.Equal(time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("mail dates = %v to %v", c.MailFrom, c.MailTo)
	}
	// A Date, when there, is the record's own.
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{
		{Path: "Mailbox/Inbox/2024-03-01_0900_hello_ab12cd34.html", Date: "2010-01-02T03:04:05Z"},
	})
	if c := mustPlan(t, dir, testDeps()).Captured; !c.MailFrom.Equal(time.Date(2010, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("mail from = %v", c.MailFrom)
	}
}

// covers: MA-957, R36
// A name Windows reserves for a device (nul, COM1) — tool litter Git Bash or WSL
// leaves — can be in the folder: os.Root cannot reach it there, so it is kept as
// the person's and never makes the plan refuse with "reconnect" (del-s1a-A1-01). On
// Windows the files are made through the \\?\ form; elsewhere the names are
// ordinary and the same plan holds.
func TestADeviceNameInTheFolderIsKept(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	writeAnyName(t, filepath.Join(dir, "nul"), "")
	if err := os.MkdirAll(filepath.Join(dir, "Mailbox", "Inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeAnyName(t, filepath.Join(dir, "Mailbox", "Inbox", "COM1"), "")
	mkdirAnyName(t, filepath.Join(dir, "Mailbox", "aux"))
	writeAnyName(t, filepath.Join(dir, "Mailbox", "aux", "notes.txt"), "mine")
	p := mustPlan(t, dir, testDeps())
	// On Windows the device-named folder is kept whole, never walked (os.Root cannot
	// open it); elsewhere it is an ordinary folder and its file is kept.
	inAux := "Mailbox/aux/notes.txt"
	if runtime.GOOS == "windows" {
		inAux = "Mailbox/aux"
	}
	for _, k := range []string{"nul", "Mailbox/Inbox/COM1", inAux} {
		if !contains(p.Kept, k) {
			t.Fatalf("%s must be kept, kept = %q", k, p.Kept)
		}
	}
	if !contains(removeRels(p), stemA+".html") {
		t.Fatalf("the archive's own files are still listed: %q", removeRels(p))
	}
}
