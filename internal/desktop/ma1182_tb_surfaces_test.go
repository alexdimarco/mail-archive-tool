package desktop

import (
	"encoding/json"
	"html"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/state"
)

// Slice C3 on MailArchive Desktop (docs/design-thunderbird-completeness.md §7's surfaces table):
// Recent backups and the archive card. Their words are state's (one producer); Status & health is
// health's (MA-1183).

// tbBannedDesktopWords are the words a phone reader would have to look up (the slice's brief):
// none of them is on a Desktop surface's completeness words.
var tbBannedDesktopWords = []string{"store", "IMAP", "imap ", "cache", "msf", ".json"}

func noDesktopJargon(t *testing.T, what, s string) {
	t.Helper()
	for _, w := range tbBannedDesktopWords {
		if strings.Contains(s, w) {
			t.Errorf("%s says %q: %q", what, w, s)
		}
	}
}

// covers: MA-1182
// Recent backups (§7): a run with an account behind is "OK — Thunderbird still downloading", its
// detail the account's words with both numbers (a floor "at least", R43); an account caught up
// with nothing missing adds nothing; one MailArchive could not tell about says so. The history
// keeps only the verdict and the two numbers (§9.1). Positive twin first: a caught-up run is a
// plain "OK".
func TestRecentBackupsSaysThunderbirdIsStillDownloading(t *testing.T) {
	out := t.TempDir()
	when := time.Date(2026, 11, 8, 9, 0, 0, 0, time.Local)
	add := func(tb []state.TBCompact) {
		t.Helper()
		e := state.RunHistoryEntry{When: when, Status: state.RunOK, Exported: 3}
		if tb != nil {
			e.TB = &tb
		}
		if err := state.AppendRunHistory(out, e); err != nil {
			t.Fatal(err)
		}
		when = when.Add(time.Hour)
	}
	add([]state.TBCompact{{Account: "imap.gmail.com", Verdict: "caught_up", X: 9, Y: 9}})
	rows := recentRunRows(out)
	if len(rows) != 1 || rows[0].Status != "OK" || rows[0].Detail != "3 messages" {
		t.Fatalf("positive twin: %+v", rows)
	}
	add([]state.TBCompact{{Account: "imap.gmail.com", Verdict: "behind", X: 1670, Y: 56611, Floor: true}, {Account: "imap.example.com", Verdict: "unknown"}})
	rows = recentRunRows(out)
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	r := rows[0]
	if r.Status != "OK — Thunderbird still downloading" || r.Failed {
		t.Errorf("a behind run's status: %q failed %v", r.Status, r.Failed)
	}
	want := "3 messages · Thunderbird has downloaded about 1,670 of at least 56,611 messages in the Thunderbird account kept in the folder imap.gmail.com so far · MailArchive could not tell how much of the Thunderbird account kept in the folder imap.example.com Thunderbird has downloaded"
	if r.Detail != want {
		t.Errorf("a behind run's detail:\n got %q\nwant %q", r.Detail, want)
	}
	noDesktopJargon(t, "Recent backups", strings.ReplaceAll(strings.ReplaceAll(r.Status+" "+r.Detail, "imap.gmail.com", "ACCOUNT"), "imap.example.com", "ACCOUNT"))
	page := renderRecent(t, out)
	if !strings.Contains(page, "OK — Thunderbird still downloading") || !strings.Contains(page, html.EscapeString("about 1,670 of at least 56,611")) {
		t.Errorf("the card does not show the behind run")
	}
}

// renderRecent is the dashboard's Recent backups card for the archive at out.
func renderRecent(t *testing.T, out string) string {
	t.Helper()
	page, _ := getPage(t, DashboardHandler(Config{Out: out, SettingsPath: filepath.Join(t.TempDir(), "s.json"), GOOS: "darwin"}))
	return page
}

// covers: MA-1182
// The archive card (§7): a one-line banner for an archive whose last backup found Thunderbird
// still downloading, and a count of folders not kept on this computer, each pointing at Status &
// health. From the plaintext last-run record only: an archive whose last backup failed, and an
// encrypted archive (nothing is written beside it, O1), get none. Positive twin first: a caught-up
// archive's card has no banner.
func TestTheArchiveCardSaysThunderbirdIsStillDownloading(t *testing.T) {
	record := func(dir string, status string, accts []state.TBAccount) {
		t.Helper()
		writeManifest(t, dir)
		if err := state.WriteLastRun(dir, state.LastRun{Status: status, Started: time.Now().Add(-time.Minute), Finished: time.Now(), Thunderbird: accts}); err != nil {
			t.Fatal(err)
		}
	}
	caught, behind, failed := t.TempDir(), t.TempDir(), t.TempDir()
	record(caught, state.RunOK, []state.TBAccount{{Account: "imap.gmail.com", Verdict: "caught_up", Complete: true, OnDisk: 9, Listed: 9}})
	record(behind, state.RunOK, []state.TBAccount{{Account: "imap.gmail.com", Verdict: "behind", OnDisk: 1670, Listed: 238080,
		NotKept: []state.TBFolder{{Folder: "Projects", Listed: 772}}}})
	record(failed, state.RunFailed, []state.TBAccount{{Account: "imap.gmail.com", Verdict: "behind", OnDisk: 1, Listed: 9}})

	rows := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")}.archiveRows(
		archindex.Index{Entries: []archindex.Entry{{Path: caught}, {Path: behind}, {Path: failed}}})
	by := map[string]ArchiveRow{}
	for _, r := range rows {
		by[r.Path] = r
	}
	if b := by[caught].TBBanner; b != "" {
		t.Fatalf("positive twin: a caught-up archive's banner: %q", b)
	}
	want := "Thunderbird is still downloading the Thunderbird account kept in the folder imap.gmail.com: about 1,670 of 238,080 messages are on this computer so far; 1 folder is not kept on this computer, so it is not backed up — see Status & health."
	if b := by[behind].TBBanner; b != want {
		t.Errorf("a behind archive's banner:\n got %q\nwant %q", b, want)
	}
	if b := by[failed].TBBanner; b != "" {
		t.Errorf("a failed run's record says %q", b)
	}
	noDesktopJargon(t, "the card", strings.ReplaceAll(want, "imap.gmail.com", "ACCOUNT"))

	idxPath := filepath.Join(t.TempDir(), archindex.Name)
	if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: behind}}}); err != nil {
		t.Fatal(err)
	}
	page, _ := getPage(t, DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), IndexPath: idxPath, GOOS: "darwin"}))
	if !strings.Contains(page, html.EscapeString(want)) {
		t.Error("Your archives does not show the banner")
	}
}

// covers: MA-1182
// The C3 friction review's P10 (a small design change, the lead's call of 2026-11-08): the archive
// card also speaks for an account Thunderbird keeps only in part by its own settings (cell G: 170
// of 2,300, so most of its mail is not backed up), as `status` WARNs thunderbird_limited. Positive
// twin first: a caught-up archive's card says nothing.
func TestTheArchiveCardSaysALimitedAccount(t *testing.T) {
	caught, limited := t.TempDir(), t.TempDir()
	for dir, a := range map[string]state.TBAccount{
		caught:  {Account: "imap.fastmail.com", Verdict: "caught_up", Complete: true, OnDisk: 9, Listed: 9},
		limited: {Account: "imap.fastmail.com", Verdict: "limited", OnDisk: 170, Listed: 2300, Limit: &state.TBLimit{MaxAgeDays: 30}},
	} {
		writeManifest(t, dir)
		if err := state.WriteLastRun(dir, state.LastRun{Status: state.RunOK, Started: time.Now().Add(-time.Minute), Finished: time.Now(), Thunderbird: []state.TBAccount{a}}); err != nil {
			t.Fatal(err)
		}
	}
	rows := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")}.archiveRows(archindex.Index{Entries: []archindex.Entry{{Path: caught}, {Path: limited}}})
	by := map[string]ArchiveRow{}
	for _, r := range rows {
		by[r.Path] = r
	}
	if b := by[caught].TBBanner; b != "" {
		t.Fatalf("positive twin: %q", b)
	}
	want := "Thunderbird keeps only part of the mail of the Thunderbird account kept in the folder imap.fastmail.com on this computer (about 170 of 2,300 messages), so the rest is not backed up — see Status & health."
	if b := by[limited].TBBanner; b != want {
		t.Errorf("a limited archive's card:\n got %q\nwant %q", b, want)
	}
}

// covers: MA-1182, MA-1186
// The C3 friction review's P7: an encrypted archive's Recent backups row says "backup finished",
// never "completed", since its entry keeps nothing of completeness (O1) and the run may have been
// behind (§7's M8 fold: no record-based surface says "complete"). Positive twin first: a plaintext
// row says its count.
func TestAnEncryptedRowNeverSaysCompleted(t *testing.T) {
	if r := runRowOf(state.RunHistoryEntry{When: time.Now(), Status: state.RunOK, Exported: 3}, true); r.Detail != "3 messages" {
		t.Fatalf("positive twin: %q", r.Detail)
	}
	for kind, want := range map[string]string{
		"":                    "backup finished (its counts are sealed)",
		state.NoteIndexBehind: "backup finished; some messages are not searchable yet — the next backup adds them",
	} {
		r := runRowOf(state.RunHistoryEntry{When: time.Now(), Status: state.RunOK, Encrypted: true, ErrorKind: kind}, false)
		if r.Status != "OK" || r.Detail != want || strings.Contains(r.Detail, "complete") {
			t.Errorf("an encrypted row (%q): %q %q, want %q", kind, r.Status, r.Detail, want)
		}
	}
}

// covers: MA-1284, MA-1182
// The card, Recent backups and Status & health name the account as Thunderbird shows it, from the
// plaintext record and history (the lead's call of 2026-11-08: every surface, not only those that
// see the live run). Positive twin first: an older record and entry with no name say the folder.
func TestTheDesktopSaysThunderbirdsNameForTheAccount(t *testing.T) {
	archive := func(name string) (string, ArchiveRow, string) {
		t.Helper()
		dir := t.TempDir()
		writeManifest(t, dir)
		a := state.TBAccount{Account: "imap.gmail.com", Name: name, Verdict: "behind", OnDisk: 1670, Listed: 238080}
		lr := state.LastRun{Status: state.RunOK, Started: time.Now().Add(-time.Minute), Finished: time.Now(), Exported: 3, Thunderbird: []state.TBAccount{a}}
		if err := state.WriteLastRun(dir, lr); err != nil {
			t.Fatal(err)
		}
		if err := state.AppendRunHistory(dir, state.RunHistoryEntryFrom(lr)); err != nil {
			t.Fatal(err)
		}
		row := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")}.archiveRows(archindex.Index{Entries: []archindex.Entry{{Path: dir}}})[0]
		cfg := Config{Out: dir, SettingsPath: filepath.Join(t.TempDir(), "s.json"), GOOS: "darwin"}
		h := DashboardHandler(cfg)
		_, _ = getPage(t, h)
		return dir, row, apiGet(h, "/api/health")
	}
	healthSays := func(body string) string {
		t.Helper()
		var m struct {
			Summary []string `json:"summary"`
		}
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("/api/health: %v %s", err, body)
		}
		return strings.Join(m.Summary, "\n")
	}
	dir, row, body := archive("")
	const folder = "the Thunderbird account kept in the folder imap.gmail.com"
	if r := recentRunRows(dir); len(r) != 1 || !strings.Contains(r[0].Detail, "in "+folder+" so far") || !strings.HasPrefix(row.TBBanner, "Thunderbird is still downloading "+folder+":") ||
		!strings.Contains(healthSays(body), "WARN: Thunderbird has downloaded about 1,670 of 238,080 messages in "+folder+" so far.") {
		t.Fatalf("positive twin: no name: %+v %q %s", r, row.TBBanner, body)
	}

	dir, row, body = archive("sam.walker@gmail.com")
	if r := recentRunRows(dir); len(r) != 1 || r[0].Detail != "3 messages · Thunderbird has downloaded about 1,670 of 238,080 messages in sam.walker@gmail.com so far" {
		t.Errorf("Recent backups: %+v", r)
	}
	if want := "Thunderbird is still downloading sam.walker@gmail.com: about 1,670 of 238,080 messages are on this computer so far — see Status & health."; row.TBBanner != want {
		t.Errorf("the card:\n got %q\nwant %q", row.TBBanner, want)
	}
	if s := healthSays(body); !strings.Contains(s, "WARN: Thunderbird has downloaded about 1,670 of 238,080 messages in sam.walker@gmail.com so far.") || strings.Contains(s, "kept in the folder") {
		t.Errorf("Status & health:\n%s", s)
	}
	if page := renderRecent(t, dir); !strings.Contains(page, "in sam.walker@gmail.com so far") {
		t.Error("the page does not show the name")
	}
}

// covers: MA-1198
// A planted record's names reach Recent backups and the card cleaned and capped (TC-7), and the
// page renders them only through html/template. Positive twin first: an ordinary name as it is.
func TestHostileNamesOnTheDesktop(t *testing.T) {
	out := t.TempDir()
	tb := []state.TBCompact{{Account: "imap.gmail.com", Verdict: "behind", X: 1, Y: 9}}
	if err := state.AppendRunHistory(out, state.RunHistoryEntry{When: time.Now(), Status: state.RunOK, TB: &tb}); err != nil {
		t.Fatal(err)
	}
	if r := recentRunRows(out); len(r) != 1 || !strings.Contains(r[0].Detail, "kept in the folder imap.gmail.com so far") {
		t.Fatalf("positive twin: %+v", r)
	}
	hostile := "<b>x</b>‮\x1b" + strings.Repeat("z", 10*1024)
	tb = []state.TBCompact{{Account: hostile, Verdict: "behind", X: 1, Y: 9}}
	if err := state.AppendRunHistory(out, state.RunHistoryEntry{When: time.Now().Add(time.Hour), Status: state.RunOK, TB: &tb}); err != nil {
		t.Fatal(err)
	}
	r := recentRunRows(out)[0]
	for _, bad := range []string{"‮", "\x1b", strings.Repeat("z", 80)} {
		if strings.Contains(r.Detail, bad) {
			t.Errorf("the row holds %q", bad)
		}
	}
	page := renderRecent(t, out)
	if strings.Contains(page, "<b>x</b>") || !strings.Contains(page, "&lt;b&gt;x&lt;/b&gt;") {
		t.Error("a planted name is not escaped on the page")
	}

	dir := t.TempDir()
	writeManifest(t, dir)
	if err := state.WriteLastRun(dir, state.LastRun{Status: state.RunOK, Started: time.Now(), Finished: time.Now(),
		Thunderbird: []state.TBAccount{{Account: hostile, Verdict: "behind", OnDisk: 1, Listed: 9}}}); err != nil {
		t.Fatal(err)
	}
	row := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")}.archiveRows(archindex.Index{Entries: []archindex.Entry{{Path: dir}}})[0]
	if strings.Contains(row.TBBanner, "‮") || strings.Contains(row.TBBanner, strings.Repeat("z", 80)) || !strings.Contains(row.TBBanner, "…") {
		t.Errorf("the card: %q", row.TBBanner)
	}
}

// covers: MA-1284, MA-1198
// A planted account name (MA-1284: the record keeps Thunderbird's name for the account) reaches the
// card, Recent backups and Status & health cleaned and capped, and never as markup: the card and
// Recent backups render through html/template, Status & health through JSON and textContent.
// Positive twin first: an ordinary name as it is, on the page.
func TestAHostileAccountNameOnTheDesktop(t *testing.T) {
	plant := func(name string) (dir string, row ArchiveRow, page, health string) {
		t.Helper()
		dir = t.TempDir()
		writeManifest(t, dir)
		lr := state.LastRun{Status: state.RunOK, Started: time.Now(), Finished: time.Now(),
			Thunderbird: []state.TBAccount{{Account: "imap.gmail.com", Name: name, Verdict: "behind", OnDisk: 1, Listed: 9}}}
		if err := state.WriteLastRun(dir, lr); err != nil {
			t.Fatal(err)
		}
		if err := state.AppendRunHistory(dir, state.RunHistoryEntryFrom(lr)); err != nil {
			t.Fatal(err)
		}
		idxPath := filepath.Join(t.TempDir(), archindex.Name)
		if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: dir}}}); err != nil {
			t.Fatal(err)
		}
		row = Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")}.archiveRows(archindex.Index{Entries: []archindex.Entry{{Path: dir}}})[0]
		h := DashboardHandler(Config{Out: dir, SettingsPath: filepath.Join(t.TempDir(), "s.json"), IndexPath: idxPath, GOOS: "darwin"})
		page, _ = getPage(t, h)
		return dir, row, page, apiGet(h, "/api/health")
	}
	dir, row, page, _ := plant("sam@example.com")
	if r := recentRunRows(dir); !strings.Contains(r[0].Detail, "in sam@example.com so far") || !strings.Contains(row.TBBanner, "downloading sam@example.com:") ||
		!strings.Contains(page, "in sam@example.com so far") || !strings.Contains(page, html.EscapeString("downloading sam@example.com:")) {
		t.Fatalf("positive twin: %+v %q", r, row.TBBanner)
	}
	hostile := "<b>x</b><script>alert(1)</script>‮\x1b[31m\x07" + strings.Repeat("z", 10*1024)
	dir, row, page, body := plant(hostile)
	clean := state.CleanFolderName(hostile)
	r := recentRunRows(dir)[0]
	var m struct {
		Summary []string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("/api/health: %v", err)
	}
	said := strings.Join(m.Summary, "\n")
	for what, s := range map[string]string{"Recent backups": r.Detail, "the card": row.TBBanner, "Status & health": said} {
		for _, bad := range []string{"‮", "\x1b", "\x07", strings.Repeat("z", 80)} {
			if strings.Contains(s, bad) {
				t.Errorf("%s holds %q", what, bad)
			}
		}
		if !strings.Contains(s, clean) || !strings.HasSuffix(clean, "…") {
			t.Errorf("%s does not say the cleaned, capped name: %q", what, s)
		}
	}
	if strings.Contains(page, "<b>x</b>") || strings.Contains(page, "<script>alert(1)") || !strings.Contains(page, "&lt;b&gt;x&lt;/b&gt;&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("a planted account name is not escaped on the page")
	}
	if strings.Contains(body, "<b>") || strings.Contains(body, "<script>") {
		t.Error("Status & health's reply carries the name as markup")
	}
}
