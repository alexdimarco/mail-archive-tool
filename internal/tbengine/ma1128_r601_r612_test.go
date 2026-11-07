package tbengine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// grantOthersRead gives Local Folders an inheritable entry letting other accounts read
// what is made in it: macOS "everyone allow read … file_inherit,directory_inherit";
// Windows BUILTIN\Users (OI)(CI)R, as a folder on a second drive gets by default.
// Linux: none (mode 0600 masks a default access list there).
func grantOthersRead(t *testing.T, dir string) bool {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		if b, err := exec.Command("chmod", "+a", "everyone allow read,readattr,readextattr,list,search,file_inherit,directory_inherit", dir).CombinedOutput(); err != nil {
			t.Skipf("chmod +a: %v %s", err, b)
		}
		return true
	case "windows":
		if b, err := exec.Command("icacls", dir, "/grant", "*S-1-5-32-545:(OI)(CI)R").CombinedOutput(); err != nil {
			t.Skipf("icacls: %v %s", err, b)
		}
		return true
	}
	return false
}

// othersMayRead is what the OS's own tool says of path: macOS `ls -led` names an
// "everyone" entry, Windows `icacls` names BUILTIN\Users; anywhere, a group or other
// mode bit.
func othersMayRead(t *testing.T, path string) string {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	switch runtime.GOOS {
	case "darwin":
		b, _ := exec.Command("ls", "-led", path).CombinedOutput()
		if strings.Contains(string(b), "everyone") {
			return string(b)
		}
	case "windows":
		b, _ := exec.Command("icacls", path).CombinedOutput()
		if strings.Contains(string(b), "BUILTIN\\Users") || strings.Contains(string(b), "Everyone") {
			return string(b)
		}
		return ""
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fi.Mode().String()
	}
	return ""
}

// filesOf is snapshot's listing of the profile's files without restores.log and
// Thunderbird's own lock file, for a run that began writing: its log lines are the record
// of what it did, and the lock file is the one file R40 lets a restore make outside Local
// Folders (empty, made only when absent).
func filesOf(t *testing.T, root string) string {
	t.Helper()
	var keep []string
	for _, l := range strings.Split(snapshot(t, root), "\n") {
		if !strings.HasPrefix(l, "LOG ") && !strings.Contains(l, " "+tbrestore.ParentLockName+" ") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// restoredEntries are the container and every folder and file the restore made.
func restoredEntries(t *testing.T, local, top string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(filepath.Join(local, top+".sbd"), func(p string, d fs.DirEntry, err error) error {
		if err == nil {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// covers: MA-1128, R37, R40, S72
// R6-01 (settled 2026-11-07, the lead's call): what a restore makes is readable by this
// account alone, whatever Local Folders lets others read. Local Folders is given an
// inheritable "others may read" entry first (macOS, Windows); an encrypted archive's
// restore leaves no entry or mode letting anyone else read any folder or file it made —
// the OS's own tool (`ls -le`, `icacls`) reading each back — and the plan says whose the
// files are; a plain archive's restore is made owner-only too, with no extra question.
func TestRestoredFilesAreReadableOnlyByThisAccount(t *testing.T) {
	out, u := encryptedArchive(t)
	pr := newProfile(t, "")
	grantOthersRead(t, pr.local)
	p := plan(t, Request{Out: out, Keys: u, Profile: pr.choice()})
	assure.Reached(t, !p.AskTwice() && strings.Contains(p.Text(), "readable only by your account"), fmt.Sprintf("the plan says the files are this account's alone, no second question: %v", p.Codes()))
	r := mustRun(t, p, Start{NotEncryptedOK: true})
	assure.Reached(t, r.Done(), "the encrypted restore finished: "+r.Words)
	made := restoredEntries(t, pr.local, r.Write.Top)
	assure.Reached(t, len(made) >= 5, fmt.Sprintf("the container, its folder and its files: %v", made))
	for _, e := range made {
		if why := othersMayRead(t, e); why != "" {
			t.Fatalf("%s can be read by other accounts:\n%s", e, why)
		}
	}

	plain := plainArchive(t, true)
	pr2 := newProfile(t, "")
	grantOthersRead(t, pr2.local)
	pp := plan(t, Request{Out: plain, Profile: pr2.choice()})
	assure.Reached(t, !pp.AskTwice(), "a plain archive is not asked a second time")
	r2 := mustRun(t, pp, Start{})
	assure.Reached(t, r2.Done(), "the plain restore finished: "+r2.Words)
	for _, e := range restoredEntries(t, pr2.local, r2.Write.Top) {
		if why := othersMayRead(t, e); why != "" {
			t.Fatalf("plain: %s can be read by other accounts:\n%s", e, why)
		}
	}
}

// covers: MA-1128, R37, R40, S72
// R6-01: a drive that cannot limit who reads a file (FAT32 or exFAT, here through the
// file-system seam, since none can be made in a test) makes an encrypted archive's plan
// say plainly that other accounts can read the restored mail and ask a second time: Start
// without ConfirmAgain is W-NOTAGAIN naming why, nothing written; with it, it restores.
// A plain archive on the same drive is asked nothing more. Then a drive that should hold
// owner-only but does not (the writer's owner-only step failing, through the FS seam):
// an encrypted restore stops with the one cleanup and says why, nothing left; a plain one
// goes ahead.
func TestADriveThatCannotLimitReadersIsAskedTwice(t *testing.T) {
	out, u := encryptedArchive(t)
	pr := newProfile(t, "")
	was := fsTypeOf
	fsTypeOf = func(tbrestore.LocalFolders) string { return "exfat" }
	p := plan(t, Request{Out: out, Keys: u, Profile: pr.choice()})
	assure.Reached(t, p.AskTwice() && strings.Contains(codesOf(p), "W-READERS") && strings.Contains(p.Text(), "other accounts on this computer"), fmt.Sprintf("the plan names the readers and asks twice: %s\n%s", codesOf(p), p.Text()))
	before := snapshot(t, pr.dir)
	_, err := Run(context.Background(), p, Start{NotEncryptedOK: true})
	refusedAs(t, err, "W-NOTAGAIN", func() bool { return snapshot(t, pr.dir) == before }, "cannot limit who reads a file", "not confirmed a second time")
	assure.Reached(t, mustRun(t, p, Start{NotEncryptedOK: true, ConfirmAgain: true}).Done(), "confirmed twice, it restores")

	pp := plan(t, Request{Out: plainArchive(t, true), Profile: newProfile(t, "").choice()})
	assure.Reached(t, !pp.AskTwice() && !strings.Contains(codesOf(pp), "W-READERS"), "a plain archive on that drive is asked nothing more")
	fsTypeOf = was

	failPrivate := errors.New("the access list could not be set")
	fsWas := fsFor
	fsFor = func(r *os.Root) tbrestore.FS {
		return &faultFS{inner: tbrestore.NewRootFS(r), privateErr: failPrivate}
	}
	defer func() { fsFor = fsWas }()
	pr3 := newProfile(t, "")
	p3 := plan(t, Request{Out: out, Keys: u, Profile: pr3.choice()})
	before = filesOf(t, pr3.dir)
	r3 := mustRun(t, p3, Start{NotEncryptedOK: true})
	assure.Refused(t, map[bool]int{true: 1}[!r3.Done()], r3.Words, assure.Code(1), assure.Names("readable only by your account", pr3.local, "nothing was added"),
		assure.NoSideEffect(func() bool { return filesOf(t, pr3.dir) == before }))
	assure.Reached(t, noFolderNames(r3.Words), "the words name no folder of the encrypted archive")

	pr4 := newProfile(t, "")
	r4 := mustRun(t, plan(t, Request{Out: plainArchive(t, true), Profile: pr4.choice()}), Start{})
	assure.Reached(t, r4.Done(), "a plain archive's restore passes the failure over: "+r4.Words)
}

// covers: MA-1128, R37, S72
// R6-12 (settled 2026-11-07): an encrypted archive's plan and refusal words give counts,
// never its folders' names — W-FAT32 (through the file-system seam) and W-LONGPATH's line
// (its words, since Windows' path budget cannot be reached on every leg). Positive twin: a
// plain archive's W-FAT32 names the folder.
func TestAnEncryptedPlanNamesNoFolderInItsSizeAndDepthWords(t *testing.T) {
	out, u := encryptedArchive(t)
	pr := newProfile(t, "")
	wasT, wasL := fsTypeOf, fat32Limit
	fsTypeOf = func(tbrestore.LocalFolders) string { return "msdos" }
	fat32Limit = 10
	defer func() { fsTypeOf, fat32Limit = wasT, wasL }()
	before := snapshot(t, pr.dir)
	_, err := MakePlan(Request{Out: out, Keys: u, Profile: pr.choice()})
	refusedAs(t, err, "W-FAT32", func() bool { return snapshot(t, pr.dir) == before }, "Of the folders chosen, 3 are 4 GB or more", "FAT32")
	assure.Reached(t, noFolderNames(err.Error()), "W-FAT32 names no folder: "+err.Error())

	_, perr := MakePlan(Request{Out: plainArchive(t, true), Profile: pr.choice()})
	refusedAs(t, perr, "W-FAT32", nil, "FAT32", "too big for the drive")
	assure.Reached(t, perr != nil && !noFolderNames(perr.Error()), "a plain archive's W-FAT32 names its folder")

	enc := longPathWords([]string{folderA + "/" + folderB, folderC}, true)
	assure.Reached(t, noFolderNames(enc) && strings.Contains(enc, "2 folders are nested too deep"), "W-LONGPATH for an encrypted archive: "+enc)
	plainWords := longPathWords([]string{folderC}, false)
	assure.Reached(t, strings.Contains(plainWords, folderC), "W-LONGPATH for a plain archive names them: "+plainWords)
}
