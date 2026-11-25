package wizard

import (
	"reflect"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// setRowsAt words rows as the list does, at now, and gives the set's rows in the list's order.
func setRowsAt(t *testing.T, rows []pickRow, now time.Time, goos string) []setRow {
	t.Helper()
	old := pickNow
	pickNow = func() time.Time { return now }
	t.Cleanup(func() { pickNow = old })
	return setRows(listRows(rows, goos), goos)
}

// setSummary is each row of a set as "name: made|once|empty|…", for a comparison that reads.
func setSummary(rows []setRow) []string {
	var out []string
	for _, r := range rows {
		s := r.name + ":"
		switch {
		case r.empty:
			s += " not made, empty"
		case r.unnamed:
			s += " not made, no name left"
		case r.make && r.once:
			s += " made, saved once"
		case r.make:
			s += " made"
		}
		out = append(out, s)
	}
	return out
}

// covers: MA-1491
// Which rows of the list get an archive (§5): every row except an empty one (counted in full, size
// 0) that is a feeds account, a folder no account uses now, or an older copy. Kept on purpose: Local
// Folders even when empty, an account with nothing downloaded yet, a row whose size was not
// counted (and one whose count stopped early), an empty Outlook file. A folder no account uses now
// and an older copy are saved once. Positive twin first: the owner-shaped profile read from disk,
// whose six rows (one with nothing downloaded yet, an empty Local Folders) all get an archive, none
// saved once, in the list's order. Then a table of rows as the list reads them.
func TestWhichRowsOfASetGetAnArchive(t *testing.T) {
	named := pickProfile(t, t.TempDir(), "a1b2c3d4.default-release", ownerShaped(true), ownerStores...)
	found := storesOf(named, ownerStores)
	l := listRows(gatherPickRows(found, time.Now().Add(time.Minute), sizeEntryMax), "darwin")
	set := setRows(l, "darwin")
	want := []string{
		"qz.student@mail.qzuni.example: made",
		"qzsam.walker@gmail.com: made",
		"qzsam.work@gmail.com: made",
		"qzsam@qzname.example: made",
		"qzwork@qzwork.example: made",
		"Local Folders: made",
	}
	if got := setSummary(set); !reflect.DeepEqual(got, want) {
		t.Fatalf("positive twin: the owner-shaped profile:\n got %q\nwant %q", got, want)
	}
	if !reflect.DeepEqual(pathsOf(set), l.paths) || setMakes(set) != 6 {
		t.Fatalf("the set is the list's rows in the list's order: %q, want %q (%d made)", pathsOf(set), l.paths, setMakes(set))
	}

	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	day := 24 * time.Hour
	rows := []pickRow{
		// A mail account with nothing downloaded yet: its mail arrives later.
		{path: "/p1/ImapMail/a", group: groupTBMail, kind: identFolder, folder: "a", tb: true, profile: "/p1", name: "sam@a.example", acct: "imap", downloads: true, host: "imap.a.example", sized: true, changed: now.Add(-time.Hour)},
		// Local Folders, empty: POP mail, filters and moved mail land there.
		{path: "/p1/Mail/Local Folders", group: groupTBLocal, kind: identFolder, folder: "Local Folders", tb: true, profile: "/p1", name: "Local Folders", acct: "none", sized: true},
		// Feeds: empty (not made), and one with mail (made).
		{path: "/p1/Mail/Feeds", group: groupTBLocal, kind: identFolder, folder: "Feeds", tb: true, profile: "/p1", name: "Blogs and News Feeds", acct: "rss", sized: true},
		{path: "/p1/Mail/Feeds-1", group: groupTBLocal, kind: identFolder, folder: "Feeds-1", tb: true, profile: "/p1", name: "Podcasts", acct: "rss", sized: true, size: 4096},
		// Folders no account uses now: empty (not made), with mail (saved once), not counted (saved
		// once), counted only part way (saved once).
		{path: "/p1/ImapMail/old.example", group: groupUnused, kind: identFolder, folder: "old.example", tb: true, profile: "/p1", unused: true, sized: true},
		{path: "/p1/ImapMail/kept.example", group: groupUnused, kind: identFolder, folder: "kept.example", tb: true, profile: "/p1", unused: true, sized: true, size: 9000},
		{path: "/p1/ImapMail/slow.example", group: groupUnused, kind: identFolder, folder: "slow.example", tb: true, profile: "/p1", unused: true},
		{path: "/p1/ImapMail/part.example", group: groupUnused, kind: identFolder, folder: "part.example", tb: true, profile: "/p1", unused: true, sized: true, floor: true},
		// One account in two profiles: the copy in use, and an older copy, empty (not made). And
		// another in two profiles whose older copy holds mail (saved once).
		{path: "/p2/ImapMail/b", group: groupTBMail, kind: identFolder, folder: "b", tb: true, profile: "/p2", name: "pat@b.example", acct: "imap", downloads: true, host: "imap.b.example", sized: true, size: 100, changed: now.Add(-time.Hour)},
		{path: "/p3/ImapMail/b", group: groupTBMail, kind: identFolder, folder: "b", tb: true, profile: "/p3", name: "pat@b.example", acct: "imap", downloads: true, host: "imap.b.example", sized: true, changed: now.Add(-40 * day)},
		{path: "/p2/ImapMail/c", group: groupTBMail, kind: identFolder, folder: "c", tb: true, profile: "/p2", name: "lee@c.example", acct: "imap", downloads: true, host: "imap.c.example", sized: true, size: 100, changed: now.Add(-time.Hour)},
		{path: "/p3/ImapMail/c", group: groupTBMail, kind: identFolder, folder: "c", tb: true, profile: "/p3", name: "lee@c.example", acct: "imap", downloads: true, host: "imap.c.example", sized: true, size: 5000, changed: now.Add(-40 * day)},
		// An empty Outlook file is not one of the kinds left out.
		{path: "/files/Empty.pst", group: groupOutlook, kind: identFile, folder: "Empty.pst", sized: true},
	}
	got := setSummary(setRowsAt(t, rows, now, "darwin"))
	want = []string{
		"Empty (Outlook): made",
		"lee@c.example: made",
		"lee@c.example (older copy): made, saved once",
		"pat@b.example: made",
		"pat@b.example (older copy): not made, empty",
		"sam@a.example: made",
		"Blogs and News Feeds: not made, empty",
		"Local Folders: made",
		"Podcasts: made",
		"kept.example (not in Thunderbird now): made, saved once",
		"old.example (not in Thunderbird now): not made, empty",
		"part.example (not in Thunderbird now): made, saved once",
		"slow.example (not in Thunderbird now): made, saved once",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the rows that get an archive:\n got %q\nwant %q", got, want)
	}
	assure.Reached(t, setMakes(setRowsAt(t, rows, now, "darwin")), "rows that get an archive")
}

func pathsOf(rows []setRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.path)
	}
	return out
}
