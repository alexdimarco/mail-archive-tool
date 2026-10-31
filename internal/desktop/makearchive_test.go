package desktop

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
)

// pageSpan is the HTML of the page from the element with this id up to the next
// marker (the start of the following card), or "" when the id is not on the page.
func pageSpan(page, id, next string) string {
	i := strings.Index(page, `id="`+id+`"`)
	if i < 0 {
		return ""
	}
	rest := page[i:]
	if j := strings.Index(rest, next); j > 0 {
		return rest[:j]
	}
	return rest
}

// covers: MA-953
// A clearly labelled "Make a new archive…" button is where a person looks for one
// (the owner's ruling C for 1.5.2). Positive twin first: a fresh Desktop's "No archive
// chosen yet" card still offers Open an archive folder… and Make a new encrypted
// archive…. That card now also offers Make a new archive…, and says what it does; the
// "Add another archive" card's button, the same action, is relabelled Make a new
// archive… rather than shown twice. Both buttons go through the one existing path, the
// wizard's add flow behind api/add-archive (no new archive-making code), and after it a
// fresh Desktop lists the new archive under Your archives with Switch to this archive.
// Amended 2026-10-31 (the 1.5.2 friction review, findings 1, 2, 4 and 5): with the
// wizard's last-used note, the reloaded first card offers the new archive with Use this
// archive, as the card now says; the page then draws one Make a new archive… button, not
// two; the waiting note says the wizard's windows may be behind the browser.
func TestMakeANewArchiveIsOfferedWhereAPersonLooks(t *testing.T) {
	const label = `>Make a new archive…</button>`
	const button = `data-make-archive title=`

	// A fresh Desktop: no archive chosen, none known.
	dir := t.TempDir()
	cfg := Config{GOOS: "darwin", ConfigPath: filepath.Join(dir, "none.json"), SettingsPath: filepath.Join(dir, "s.json"), IndexPath: filepath.Join(dir, archindex.Name)}
	page := getOverview(t, cfg)
	fresh := pageSpan(page, "noArchive", `id="sec-help"`)
	assure.Reached(t, fresh, `the "No archive chosen yet" card`)
	for _, twin := range []string{`>Open an archive folder…</button>`, `>Make a new encrypted archive…</button>`} {
		if !strings.Contains(fresh, twin) {
			t.Fatalf("positive twin: the fresh card lacks %s", twin)
		}
	}
	if strings.Count(fresh, label) != 1 || strings.Count(fresh, button) != 1 || !strings.Contains(fresh, "data-make-archive-note") {
		t.Errorf("the fresh card does not offer one Make a new archive… button with its note:\n%s", fresh)
	}
	// Amended 2026-10-31 (the 1.5.2 friction review, findings 2 and 4): the card names the
	// button the person will see when the wizard is done, Use this archive at the top, and
	// leaves the do-it-yourself way to the guide right below it.
	const says = "To make a new one from Thunderbird, Outlook or other mail files on this computer, click <b>Make a new archive…</b> above: the MailArchive wizard's windows open on your computer. When the wizard is done, this page offers the new archive at the top: click <b>Use this archive</b>. To back up a Microsoft 365 mailbox,"
	if !strings.Contains(fresh, says) {
		t.Errorf("the fresh card does not say what Make a new archive… does: want %q in\n%s", says, fresh)
	}
	for _, gone := range []string{"click <b>Switch to this archive</b> to use it", "You can also open the wizard yourself."} {
		if strings.Contains(fresh, gone) {
			t.Errorf("the fresh card still says %q", gone)
		}
	}
	// While the wizard is open, the note says its windows may be behind the browser
	// (finding 1), as every folder picker's note does.
	const waiting = "'The MailArchive wizard is opening on your computer. Its windows may be behind this browser window: if they are not in view, move or minimize this window. Follow its steps to make the archive; this page then refreshes.'"
	if !strings.Contains(dashboardJS, waiting) {
		t.Errorf("the Make a new archive… waiting note does not say the wizard may be behind the browser: want %s", waiting)
	}
	// Make a new encrypted archive… opens the same wizard windows; its note says the same.
	const encWaiting = "'Answer the questions in the windows that open on your computer. They may be behind this browser window: if they are not in view, move or minimize this window.'"
	if !strings.Contains(encryptionJS, encWaiting) {
		t.Errorf("the Make a new encrypted archive… waiting note does not say the wizard may be behind the browser: want %s", encWaiting)
	}

	// The one path: both buttons are bound by one handler that posts api/add-archive, and
	// nothing else posts it.
	if !strings.Contains(dashboardJS, "document.querySelectorAll('[data-make-archive]')") || strings.Count(dashboardJS, "'api/add-archive'") != 1 {
		t.Error("the Make a new archive… buttons are not bound by the one add-archive handler")
	}
	if strings.Contains(dashboardJS, "$('addArchiveBtn')") {
		t.Error("a second, id-only binding of the add button is left beside the shared one")
	}

	// Through that path from the fresh Desktop: the wizard's add flow (its seam) makes an
	// archive; the page then lists it under Your archives with Switch to this archive, and
	// it is not yet current.
	restore := addArchiveFn
	defer func() { addArchiveFn = restore }()
	made := t.TempDir()
	writeManifest(t, made)
	addArchiveFn = func() (string, error) { return made, nil }
	d := newDashboard(cfg)
	req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/add-archive", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", d.csrf)
	rec := httptest.NewRecorder()
	d.addArchive(rec, req)
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("the add from a fresh Desktop: %s", rec.Body.String())
	}
	after := getOverview(t, cfg)
	list := pageSpan(after, "sec-archives", `id="sec-add-archive"`)
	if !strings.Contains(list, made) || !strings.Contains(list, `>Switch to this archive</button>`) {
		t.Errorf("after Make a new archive… on a fresh Desktop, Your archives does not list the new archive with Switch to this archive:\n%s", list)
	}
	if cfg.effectiveOut() != "" {
		t.Errorf("the new archive was made current (%q); the card says to click Use this archive", cfg.effectiveOut())
	}
	// The real wizard notes the archive it made as the one it last used (rememberArchive),
	// which the Desktop reads as its offer (OfferedArchive). With that note, the reloaded
	// page offers the new archive at the top with Use this archive, as the card says
	// (finding 2), and draws exactly one Make a new archive… button: the Add another
	// archive card and its sidebar entry wait until an archive is current (finding 5).
	cfg.LastArchive = func() string { return made }
	offered := getOverview(t, cfg)
	top := pageSpan(offered, "noArchive", `id="sec-help"`)
	assure.Reached(t, top, `the "No archive chosen yet" card after the wizard`)
	if want := `<p>The MailArchive wizard last made or used <span class="mono">` + made + `</span>. Use this archive?</p>`; !strings.Contains(top, want) {
		t.Errorf("after the wizard, the first card does not offer the new archive: want %q in\n%s", want, top)
	}
	if !strings.Contains(top, `id="useOffer" type="button" data-path="`+made+`" title=`) {
		t.Errorf("after the wizard, Use this archive is not offered for the new archive:\n%s", top)
	}
	if strings.Contains(offered, "(mailarchive wizard)") {
		t.Error("the offer names the wizard by its command")
	}
	if n := strings.Count(offered, label); n != 1 || strings.Count(offered, button) != 1 {
		t.Errorf("an archive known but none current: Make a new archive… shows %d times, want once (on the first card)", n)
	}
	for _, gone := range []string{`id="sec-add-archive"`, `id="navAddArchive"`, "without changing your current one"} {
		if strings.Contains(offered, gone) {
			t.Errorf("an archive known but none current: the page still draws %q", gone)
		}
	}

	// A Desktop with an archive: the Add another archive card's button reads Make a new
	// archive… (relabelled, not duplicated), and the old label is gone.
	dir2 := t.TempDir()
	idx := filepath.Join(dir2, archindex.Name)
	a := archiveAt(t, dir2, "A", map[string]string{"path:/mail/a.mbox": "a"})
	if err := archindex.WriteFile(idx, archindex.Index{Entries: []archindex.Entry{{Path: a, AddedAt: at0, LastSeenAt: at0}}}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir2, "s.json")
	if err := SaveSettings(settings, Settings{Out: a}); err != nil {
		t.Fatal(err)
	}
	withOne, _ := getPage(t, DashboardHandler(Config{SettingsPath: settings, IndexPath: idx, GOOS: "darwin"}))
	add := pageSpan(withOne, "sec-add-archive", `class="status-grid"`)
	assure.Reached(t, add, "the Add another archive card")
	if !strings.Contains(add, `id="addArchiveBtn" type="button" `+button) || strings.Count(add, label) != 1 {
		t.Errorf("the Add another archive card's button is not Make a new archive…:\n%s", add)
	}
	if strings.Count(withOne, label) != 1 || strings.Count(withOne, button) != 1 {
		t.Errorf("Make a new archive… shows %d times (%d buttons) with an archive chosen, want once", strings.Count(withOne, label), strings.Count(withOne, button))
	}
	if strings.Contains(withOne, ">Add another archive…</button>") {
		t.Error("the old Add another archive… button label is still drawn")
	}
}
