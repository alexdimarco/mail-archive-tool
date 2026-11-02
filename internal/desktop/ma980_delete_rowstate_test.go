package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// rowSays requires the row of path to offer Delete or not, with why's words.
func rowSays(t *testing.T, e *delEnv, path string, offered bool, why string) {
	t.Helper()
	row := rowFor(t, e.page(), path)
	want := `data-delete-ok="false"`
	if offered {
		want = `data-delete-ok="true"`
	}
	if !strings.Contains(row, want) || (why != "" && !strings.Contains(row, why)) {
		t.Errorf("the row of %s: %s\nwant %s and %q", path, row, want, why)
	}
}

// covers: MA-980, R36, S41
// A row's Delete state agrees with its plan (del-s3a-A2-02, conformance gap 1, gap 5): a row
// that is a link to an archive, a row that holds another listed archive, and a row of an archive
// a backup of which runs — in the Desktop's job slot, or another program's holding its archive
// lock — do not offer Delete, each in the plan's own words; a row whose name the current
// archive's capture is registered under (§5.1 (a)) is refused on the row, at the plan and at
// Remove from list; an archive export reading a folder refuses its plan. Positive twins first:
// each archive's row offers Delete while nothing of this holds.
func TestARowsDeleteStateAgreesWithItsPlan(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Mail")
	outer := e.plainArchive("Outer")
	inner := filepath.Join(outer, "Inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	buildArchiveAt(t, inner)
	link := filepath.Join(t.TempDir(), "MailLink")
	linkFolder(t, dir, link)
	rowSays(t, e, dir, true, "")
	rowSays(t, e, outer, true, "")
	e.list(link, inner)

	for _, c := range []struct{ row, why string }{
		{link, "is, or holds where its files are, a link: " + link},
		{outer, "MailArchive will not delete this folder: it is (or is inside, or holds) " + inner},
	} {
		rowSays(t, e, c.row, false, c.why)
		p := e.plan(c.row)
		assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names(c.why),
			assure.NoSideEffect(func() bool { return p["token"] == nil }))
	}

	// A backup of it runs: in the Desktop's slot, then another program's lock.
	c := e.d.cap
	c.mu.Lock()
	c.running, c.slotOut = true, dir
	c.mu.Unlock()
	rowSays(t, e, dir, false, "A backup of this archive is running. Try again when it ends.")
	c.mu.Lock()
	c.running, c.slotOut = false, ""
	c.mu.Unlock()
	l, err := lockfile.Acquire(filepath.Join(dir, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	rowSays(t, e, dir, false, "A backup of this archive is running.")
	l.Release()
	rowSays(t, e, dir, true, "")

	// An archive export reading it.
	e.d.ex.mu.Lock()
	e.d.ex.slot = &exportSlot{archiveDir: dir, done: make(chan struct{})}
	e.d.ex.mu.Unlock()
	p := e.plan(dir)
	e.d.ex.mu.Lock()
	e.d.ex.slot = nil
	e.d.ex.mu.Unlock()
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names("A backup of this archive is running."),
		assure.NoSideEffect(func() bool { return p["token"] == nil }))

	// The current archive's capture is registered under dir's name.
	cur := e.plainArchive("Current")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur, BackupFor: dir}); err != nil {
		t.Fatal(err)
	}
	const why = "is still registered under this folder's name"
	rowSays(t, e, dir, false, why)
	p = e.plan(dir)
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names(why, "then delete this one"),
		assure.NoSideEffect(func() bool { return p["token"] == nil }))
	_, u := e.post("/api/archive/unlist", map[string]any{"path": dir})
	assure.Refused(t, http.StatusOK, replyErr(u), assure.Code(http.StatusOK), assure.Names(why, "then remove this one from the list"),
		assure.NoSideEffect(func() bool { return e.listed(dir) }))
}

// covers: MA-980, MA-1034, R36, S41
// An archive whose manifest is over 1 MiB offers Delete (§10 [NEW-isarchive-cap], conformance gap
// 5) and its plan is made. Amended 2026-11-02 (MA-1034, the 1.5.3 re-walk's N1): the list's read
// no longer stops at 1 MiB, so the row is no longer greyed: it is available and offers Switch to
// this archive and Pause beside Delete. The stand-in was a manifest padded with 1 MiB of white
// space, which the list's walk then refused as one value over its bound; it is now a real
// manifest, grown by the state package to thousands of records. (Since MA-1035 the walk holds no
// skipped value or white space; a greyed row's Delete is proven by
// TestARowTheListCannotRecogniseIsGreyedAndStillOfferedDelete.)
// Positive twin first: the same archive with its usual manifest is offered.
func TestAnArchiveWithAManifestOverOneMiBIsStillOfferedDelete(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Large")
	rowSays(t, e, dir, true, "")
	growManifest(t, dir, 6000)
	row := rowFor(t, e.page(), dir)
	if strings.Contains(row, "unavailable") || strings.Contains(row, "Not reachable right now") {
		t.Errorf("an archive with a manifest over 1 MiB is greyed: %s", row)
	}
	if !strings.Contains(row, "data-switch-archive") || !strings.Contains(row, "data-pause-archive") {
		t.Errorf("an archive with a manifest over 1 MiB does not offer Switch to this archive and Pause: %s", row)
	}
	if !strings.Contains(row, `data-delete-ok="true"`) {
		t.Errorf("an archive with a manifest over 1 MiB does not offer Delete: %s", row)
	}
	if p := e.plan(dir); p["ok"] != true {
		t.Errorf("its plan: %v", p)
	}
}
