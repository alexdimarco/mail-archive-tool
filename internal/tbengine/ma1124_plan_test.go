package tbengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbrestore"
)

func codesOf(p *Plan) string { return strings.Join(p.Codes(), " ") }

func lineOf(p *Plan, code string) string {
	for _, l := range p.Lines {
		if l.Code == code {
			return l.Words
		}
	}
	return ""
}

// covers: MA-1124, R40, S70
// The plan the person sees (design §6.4 as folded), in its order: the destination first
// (W-RECAPTURE above it when the archive backs up the profile, RT-10), what goes back,
// what cannot, the space with the indexing margin, "Thunderbird is closed.", the
// warnings that apply, the scheduled-backup line, the check, and W-WAIT always, last.
// Each warning only where it applies: W-OLDTB for a profile last opened by Thunderbird
// 115; the deleted-after-backup line only for an archive that keeps the go-back timeline
// (none counted, then one); a missing original counted (W-NORAW's count); Gmail's All
// Mail note only beside other folders. The plan writes nothing.
func TestThePlanSaysItInOrder(t *testing.T) {
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	before := snapshot(t, pr.dir, out)
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, snapshot(t, pr.dir, out) == before, fmt.Sprintf("the plan writes nothing"))
	want := "DEST WHAT DAMAGED-LATER SPACE CLOSED SCHEDULED CHECKS W-WAIT"
	assure.Reached(t, codesOf(p) == want, fmt.Sprintf("the plan's lines: %s, want %s", codesOf(p), want))
	assure.Reached(t, strings.Contains(lineOf(p, "DEST"), pr.local) && strings.Contains(lineOf(p, "DEST"), "'Restored — Work mail'"), fmt.Sprintf("the destination names Local Folders and the new folder"))
	assure.Reached(t, strings.Contains(lineOf(p, "WHAT"), "4 messages in 3 folders"), fmt.Sprintf("what goes back: %s", lineOf(p, "WHAT")))
	assure.Reached(t, lineOf(p, "W-WAIT") == tbrestore.WaitWords(), fmt.Sprintf("W-WAIT always"))

	// W-RECAPTURE leads when a job of the archive reads this profile (§7.2, O1 (b)).
	p = plan(t, Request{Out: out, Profile: pr.choice(), Inputs: []string{filepath.Join(pr.dir, "ImapMail")}})
	assure.Reached(t, p.Codes()[0] == "W-RECAPTURE" && p.Lines[0].Words == wRecapture, fmt.Sprintf("W-RECAPTURE first: %s", codesOf(p)))

	// W-OLDTB for a profile last opened by Thunderbird before 122.
	write(t, filepath.Join(pr.dir, "compatibility.ini"), "[Compatibility]\nLastVersion=115.10.0_20240401000000/20240401000000\n")
	p = plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, strings.Contains(lineOf(p, "W-OLDTB"), "Thunderbird 115.10.0"), fmt.Sprintf("W-OLDTB: %s", codesOf(p)))
	os.Remove(filepath.Join(pr.dir, "compatibility.ini"))

	// The deleted-after-backup line, only for an archive that keeps the timeline.
	man, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var first string
	for k := range man.All() {
		if first == "" || k < first {
			first = k
		}
	}
	man.TouchSeen(first, time.Now())
	man.SetPresent(first, true)
	if err := man.Save(); err != nil {
		t.Fatal(err)
	}
	p = plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, strings.HasPrefix(lineOf(p, "DELETED"), "None of these messages"), fmt.Sprintf("a timeline with nothing deleted: %q", lineOf(p, "DELETED")))
	man.SetPresent(first, false)
	if err := man.Save(); err != nil {
		t.Fatal(err)
	}
	p = plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, strings.HasPrefix(lineOf(p, "DELETED"), "1 of these messages were deleted from the mailbox after they were backed up; they are restored too."), fmt.Sprintf("one deleted: %q", lineOf(p, "DELETED")))

	// A missing original is counted, with its remedy.
	var eml string
	filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err == nil && eml == "" && strings.HasSuffix(path, ".eml") {
			eml = path
		}
		return nil
	})
	os.Remove(eml)
	p = plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, strings.HasPrefix(lineOf(p, "W-NORAW"), "1 message has no original message kept in the archive"), fmt.Sprintf("W-NORAW's count: %q", lineOf(p, "W-NORAW")))
}

// covers: MA-1124, R40
// Gmail's All Mail note (§6.5, Q4): shown when All Mail is restored beside other folders,
// not when it is the only one chosen.
func TestThePlanNotesGmailsAllMail(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Mail")
	write(t, filepath.Join(src, "Inbox"), mboxOf("inbox one"))
	write(t, filepath.Join(src, "[Gmail]"), "")
	write(t, filepath.Join(src, "[Gmail].sbd", "All Mail"), mboxOf("all one"))
	out := filepath.Join(t.TempDir(), "Gmail")
	backup(t, src, out, true, nil)
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, lineOf(p, "ALLMAIL") == wAllMail, fmt.Sprintf("All Mail beside Inbox: %s", codesOf(p)))
	p = plan(t, Request{Out: out, Profile: pr.choice(), FolderPaths: []string{"[Gmail]/All Mail"}})
	assure.Reached(t, lineOf(p, "ALLMAIL") == "", fmt.Sprintf("All Mail alone: no note"))
}

// covers: MA-1124, R40
// Free space (§6.3 as folded, RT-22): refused, W-SPACE naming the drive, the space it
// needs and the space it has, when the drive has less than needed × 1.30 + 512 MiB; one
// byte more and it plans. A drive whose free space cannot be read refuses too.
func TestThePlanRefusesWithoutRoom(t *testing.T) {
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	need := plan(t, Request{Out: out, Profile: pr.choice()}).NeedBytes
	assure.Reached(t, Required(need) == need*13/10+512<<20, fmt.Sprintf("the margin"))
	was := freeBytes
	defer func() { freeBytes = was }()
	before := snapshot(t, pr.dir, out)
	freeBytes = func(string) (uint64, error) { return uint64(Required(need)) - 1, nil }
	_, err := MakePlan(Request{Out: out, Profile: pr.choice()})
	refusedAs(t, err, "W-SPACE", func() bool { return snapshot(t, pr.dir, out) == before }, "not enough free space", pr.local, "Free some space")
	freeBytes = func(string) (uint64, error) { return uint64(Required(need)), nil }
	plan(t, Request{Out: out, Profile: pr.choice()})
	freeBytes = func(string) (uint64, error) { return 0, errors.New("statfs failed") }
	_, err = MakePlan(Request{Out: out, Profile: pr.choice()})
	refusedAs(t, err, "W-SPACE", nil, "could not read how much free space", "statfs failed")
}

// covers: MA-1124, R40
// The plan's refusals (§7.5), each before anything is written — the profile, the
// archive and restores.log as they were: no originals kept (W-NORAW), an Outlook-only
// archive (W-PST), a folder too big for a FAT32 Local Folders (W-FAT32), too many folders
// or levels (W-TOOMANY), every "Restored — NAME" to " (99)" taken (W-NAMES), Thunderbird
// running (W-RUNNING), another restore into the profile (W-RESTORING), a backup running
// (W-BUSY), no Local Folders (W-NOLOCAL), maildir (W-MAILDIR), Local Folders overlapping
// the archive (W-OVERLAP), an unreadable setting (W-PREFS), an unreadable restores.log
// (W-LOGBAD), no profile at all (W-NOPROF), and a -profile-name that names none
// (W-NOPROFNAME). Positive twin first: the same profile and archive plan; with several
// profiles the default is one the archive does not back up (RT-10), and -profile-name
// picks by name.
func TestThePlanRefusesBeforeWritingAnything(t *testing.T) {
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	plan(t, Request{Out: out, Profile: pr.choice()})

	check := func(name string, req Request, code string, names ...string) {
		t.Helper()
		before := snapshot(t, pr.dir, out)
		_, err := MakePlan(req)
		if tbrestore.RefusalCode(err) != code {
			t.Errorf("%s: want %s, got %q: %v", name, code, tbrestore.RefusalCode(err), err)
			return
		}
		refusedAs(t, err, code, func() bool { return snapshot(t, pr.dir, out) == before }, names...)
	}

	check("no originals", Request{Out: plainArchive(t, false), Profile: pr.choice()}, "W-NORAW", "without keeping the original messages", "its Settings card shows this", "make the archive with keeping originals on")
	pst := filepath.Join(t.TempDir(), "Outlook")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{"../../testdata/support.pst"}, Out: pst, Mode: export.Incremental, KeepRaw: true}, quiet, nil); err != nil {
		t.Fatal(err)
	}
	check("an Outlook archive", Request{Out: pst, Profile: pr.choice()}, "W-PST", "Outlook data files", "none of it")

	fsWas, limWas := fsTypeOf, fat32Limit
	fsTypeOf = func(tbrestore.LocalFolders) string { return "FAT32" }
	fat32Limit = 100
	check("FAT32", Request{Out: out, Profile: pr.choice()}, "W-FAT32", "FAT32", "4 GB", "Choose fewer folders")
	fsTypeOf, fat32Limit = fsWas, limWas

	assure.Reached(t, tbrestore.RefusalCode(selectRefusal(&app.RestoreTooMany{Folders: 10001})) == "W-TOOMANY" &&
		strings.Contains(selectRefusal(&app.RestoreTooMany{Folders: 10001}).Error(), "make 10,001 folders") &&
		strings.Contains(selectRefusal(&app.RestoreTooMany{Depth: 301}).Error(), "301 levels deep"), "W-TOOMANY names the count, or the depth")

	for n := 1; n <= 99; n++ {
		name := "Restored — Work mail"
		if n > 1 {
			name += " (" + itoa(n) + ")"
		}
		write(t, filepath.Join(pr.local, name), "")
	}
	check("99 names taken", Request{Out: out, Profile: pr.choice()}, "W-NAMES", "and 98 more like it")
	for n := 1; n <= 99; n++ {
		name := "Restored — Work mail"
		if n > 1 {
			name += " (" + itoa(n) + ")"
		}
		os.Remove(filepath.Join(pr.local, name))
	}

	undo := tbrestore.SetProcessProbeForTest(func() (int, bool) { return 1, true })
	check("Thunderbird running", Request{Out: out, Profile: pr.choice()}, "W-RUNNING", "Thunderbird is open", "Close Thunderbird")
	undo()

	rl, err := tbrestore.AcquireRestoreLock(pr.dir)
	if err != nil {
		t.Fatal(err)
	}
	check("another restore", Request{Out: out, Profile: pr.choice()}, "W-RESTORING", "Another restore into this Thunderbird profile")
	rl.Release()

	al, err := lockfile.Acquire(filepath.Join(out, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	check("a backup running", Request{Out: out, Profile: pr.choice()}, "W-BUSY", "A backup of this archive is running")
	al.Release()

	prefs := filepath.Join(pr.dir, "prefs.js")
	good, _ := os.ReadFile(prefs)
	setPrefs := func(lines ...string) {
		write(t, prefs, strings.Join(lines, "\n")+"\n")
	}
	setPrefs(`user_pref("mail.server.server2.type", "imap");`)
	check("no Local Folders", Request{Out: out, Profile: pr.choice()}, "W-NOLOCAL", "no Local Folders")
	setPrefs(strings.Split(strings.ReplaceAll(string(good), "berkeleystore", "maildirstore"), "\n")...)
	check("maildir", Request{Out: out, Profile: pr.choice()}, "W-MAILDIR", "maildir")
	setPrefs(`user_pref("mail.server.server1.type", "none");`, `user_pref("mail.server.server1.directory", "`+strings.ReplaceAll(out, `\`, `\\`)+`");`)
	check("overlap", Request{Out: out, Profile: pr.choice()}, "W-OVERLAP", "the archive folder")
	setPrefs(`user_pref("mail.server.server1.type", "none");`, `user_pref("mail.server.server1.directory", "C:\q");`)
	check("unreadable setting", Request{Out: out, Profile: pr.choice()}, "W-PREFS", "prefs.js")
	write(t, prefs, string(good))

	lp, _ := tbrestore.LogPath()
	os.MkdirAll(filepath.Dir(lp), 0o700)
	keep, _ := os.ReadFile(lp)
	write(t, lp, string(keep)+"not a line of the record\n")
	check("restores.log unreadable", Request{Out: out, Profile: pr.choice()}, "W-LOGBAD", "restores.log", "can't be read")
	write(t, lp, string(keep))

	check("no profile", Request{Out: out, Profile: ProfileChoice{Bases: []string{t.TempDir()}}}, "W-NOPROF", "No Thunderbird profile was found")
	base := t.TempDir()
	other := newProfile(t, "")
	profilesINI(t, base, []string{"Main", "Other"}, []string{pr.dir, other.dir})
	def := plan(t, Request{Out: out, Profile: ProfileChoice{Bases: []string{base}}, Inputs: []string{filepath.Join(pr.dir, "Mail")}})
	assure.Reached(t, def.Profile.Dir == other.dir, fmt.Sprintf("the default prefers a profile the archive does not back up (RT-10)"))
	all := plan(t, Request{Out: out, Profile: ProfileChoice{Bases: []string{base}}, Inputs: []string{pr.dir, other.dir}})
	assure.Reached(t, all.Profile.Dir == pr.dir && all.Codes()[0] == "W-RECAPTURE", fmt.Sprintf("every profile backed up: the source's, with W-RECAPTURE first"))
	check("a name that names none", Request{Out: out, Profile: ProfileChoice{Name: "Nobody", Bases: []string{base}}}, "W-NOPROFNAME", "'Nobody'", "-list-profiles")
	byName := plan(t, Request{Out: out, Profile: ProfileChoice{Name: "Other", Bases: []string{base}}})
	assure.Reached(t, byName.Profile.Dir == other.dir, fmt.Sprintf("-profile-name picks the profile so named"))
}
