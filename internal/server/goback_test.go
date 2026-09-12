package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// gbArchive is a hand-built archive: a search index (for subjects), a manifest
// (the current set), a history log (the timeline) and the on-disk .html files,
// all under one dir — real primitives, no mocks. Each helper writes one store.
type gbArchive struct {
	t   *testing.T
	dir string
	ix  *index.Index
	m   *state.Manifest
}

func newGBArchive(t *testing.T) *gbArchive {
	t.Helper()
	dir := tmpDir(t)
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	m, err := state.Load(filepath.Join(dir, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &gbArchive{t: t, dir: dir, ix: ix, m: m}
}

// add writes the message file on disk, indexes it (so it carries a subject), and
// adds a manifest record with the given current folder / present-flag / times.
func (a *gbArchive) add(key, subject, relPath, curFolder, firstFolder string, present bool, firstSeen, lastSeen time.Time) {
	a.t.Helper()
	full := filepath.Join(a.dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("<p>"+subject+"</p>"), 0o644); err != nil {
		a.t.Fatal(err)
	}
	msg := &model.Message{Subject: subject, Received: firstSeen}
	// folderPath for the index mirrors the first-captured (physical) folder.
	if err := a.ix.Add("store", strings.Split(firstFolder, "/"), msg, relPath, key); err != nil {
		a.t.Fatal(err)
	}
	a.m.Add(key, state.Record{
		Path: relPath, Folder: curFolder, FirstFolder: firstFolder,
		ExportedAt: firstSeen, FirstSeen: firstSeen, LastSeen: lastSeen, Present: present,
	})
}

// save flushes the index and persists the manifest.
func (a *gbArchive) save() {
	a.t.Helper()
	if err := a.ix.Flush(); err != nil {
		a.t.Fatal(err)
	}
	if err := a.m.Save(); err != nil {
		a.t.Fatal(err)
	}
}

// history writes the timeline log via the real HistoryWriter.
func (a *gbArchive) history(fn func(w *state.HistoryWriter)) {
	a.t.Helper()
	w, err := state.OpenHistory(filepath.Join(a.dir, state.HistoryName))
	if err != nil {
		a.t.Fatal(err)
	}
	fn(w)
	if err := w.Close(); err != nil {
		a.t.Fatal(err)
	}
}

func (a *gbArchive) serve() *httptest.Server {
	a.t.Helper()
	ts := httptest.NewServer(New(a.dir, a.ix))
	a.t.Cleanup(ts.Close)
	return ts
}

func gbGet(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		b.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	return resp, b.String()
}

var (
	gbD1 = time.Date(2025, 7, 1, 9, 0, 0, 0, time.UTC)
	gbD2 = time.Date(2025, 7, 2, 9, 0, 0, 0, time.UTC)
	gbD3 = time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC)
)

// covers: MA-210, R21, R3, S37
// serve renders the go-back projection server-side over the manifest + folded
// history: the CURRENT view groups each present message under its CURRENT folder
// and hides a message swept gone; ?at=D shows that gone message under its
// then-folder for a date before the gone event, and shows a moved message under
// the folder it sat in on D — while its /files/ link stays at the first-captured
// physical path (the static pages are untouched, R13). A compact date track
// lists the observed run dates newest-first.
func TestGobackCurrentAndAtDateProjection(t *testing.T) {
	a := newGBArchive(t)
	// K1 "Alpha": captured in Inbox @D1, MOVED to Archive @D2, present through D3.
	a.add("store\x00mid:alpha", "Alpha", "store/Inbox/alpha.html", "Archive", "Inbox", true, gbD1, gbD3)
	// K2 "Beta": captured in Inbox @D1, present at D2, GONE at D3.
	a.add("store\x00mid:beta", "Beta", "store/Inbox/beta.html", "Inbox", "Inbox", false, gbD1, gbD2)
	a.save()
	a.history(func(w *state.HistoryWriter) {
		must(t, w.WriteRunHeader(1, gbD1, []string{"mailbox"}))
		must(t, w.WriteFolder("store\x00mid:alpha", "Inbox"))
		must(t, w.WriteFolder("store\x00mid:beta", "Inbox"))
		must(t, w.WriteRunFooter(1, gbD1))
		must(t, w.WriteRunHeader(2, gbD2, []string{"mailbox"}))
		must(t, w.WriteFolder("store\x00mid:alpha", "Archive")) // the move
		must(t, w.WriteRunFooter(2, gbD2))
		must(t, w.WriteRunHeader(3, gbD3, []string{"mailbox"}))
		must(t, w.WriteGone("store\x00mid:beta")) // deleted online
		must(t, w.WriteRunFooter(3, gbD3))
	})
	ts := a.serve()

	// Current view: Alpha under Archive (its current folder), Beta hidden (gone).
	resp, cur := gbGet(t, ts.URL+"/goback")
	if resp.StatusCode != 200 {
		t.Fatalf("/goback status %d", resp.StatusCode)
	}
	if !strings.Contains(cur, "Alpha") {
		t.Errorf("current view lost the present message:\n%s", cur)
	}
	if strings.Contains(cur, "Beta") {
		t.Errorf("current view shows a message gone from the live mailbox (should be hidden):\n%s", cur)
	}
	if !strings.Contains(cur, ">Archive<") {
		t.Errorf("current view should group the moved message under its CURRENT folder Archive:\n%s", cur)
	}
	// The physical file link stays at the first-captured path (static pages
	// untouched, R13): grouping follows current, the file does not move.
	if !strings.Contains(cur, `href="/files/store/Inbox/alpha.html"`) {
		t.Errorf("moved message's link must stay at its first-captured physical path:\n%s", cur)
	}

	// at=D1: both Alpha and Beta present, both under Inbox (before the move).
	_, at1 := gbGet(t, ts.URL+"/goback?at=2025-07-01")
	if !strings.Contains(at1, "Alpha") || !strings.Contains(at1, "Beta") {
		t.Errorf("?at=D1 should show both messages present that day:\n%s", at1)
	}
	if strings.Contains(at1, ">Archive<") {
		t.Errorf("?at=D1 predates the move; nothing should sit under Archive:\n%s", at1)
	}

	// at=D2: Alpha has moved to Archive; Beta still present under Inbox.
	_, at2 := gbGet(t, ts.URL+"/goback?at=2025-07-02")
	if !strings.Contains(at2, ">Archive<") || !strings.Contains(at2, "Beta") {
		t.Errorf("?at=D2 should show Alpha under Archive and Beta still present:\n%s", at2)
	}

	// at=D3: Beta is gone; Alpha remains.
	_, at3 := gbGet(t, ts.URL+"/goback?at=2025-07-03")
	if !strings.Contains(at3, "Alpha") {
		t.Errorf("?at=D3 lost Alpha:\n%s", at3)
	}
	if strings.Contains(at3, "Beta") {
		t.Errorf("?at=D3 should hide Beta (gone that day):\n%s", at3)
	}

	// The date track lists every observed run date, newest first.
	for _, d := range []string{"2025-07-01", "2025-07-02", "2025-07-03"} {
		if !strings.Contains(cur, "at="+d) {
			t.Errorf("date track missing %s:\n%s", d, cur)
		}
	}
	if strings.Index(cur, "at=2025-07-03") > strings.Index(cur, "at=2025-07-01") {
		t.Errorf("date track must be newest-first")
	}
}

// covers: MA-211, R21, S37
// The on-disk intersection is the redaction belt: deleting a message's exported
// .html from the archive — WITHOUT reindex, so its manifest row and history
// events still exist — makes it vanish from the current view AND from every
// ?at=D view, because the projection shows only keys whose file is present on
// disk. So a removed message shows at NO date even before the log is compacted.
func TestGobackRedactionByOnDiskIntersection(t *testing.T) {
	a := newGBArchive(t)
	a.add("store\x00mid:keep", "KeepMe", "store/Inbox/keep.html", "Inbox", "Inbox", true, gbD1, gbD2)
	a.add("store\x00mid:redact", "RedactMe", "store/Inbox/redact.html", "Inbox", "Inbox", true, gbD1, gbD2)
	a.save()
	a.history(func(w *state.HistoryWriter) {
		must(t, w.WriteRunHeader(1, gbD1, []string{"mailbox"}))
		must(t, w.WriteFolder("store\x00mid:keep", "Inbox"))
		must(t, w.WriteFolder("store\x00mid:redact", "Inbox"))
		must(t, w.WriteRunFooter(1, gbD1))
	})
	ts := a.serve()

	// Positive twin: both present before the deletion.
	_, before := gbGet(t, ts.URL+"/goback?at=2025-07-01")
	if !strings.Contains(before, "KeepMe") || !strings.Contains(before, "RedactMe") {
		t.Fatalf("both messages should show before deletion:\n%s", before)
	}

	// Delete one message's file only (no reindex): the manifest row and history
	// events for it remain, but the file is gone from disk.
	if err := os.Remove(filepath.Join(a.dir, "store", "Inbox", "redact.html")); err != nil {
		t.Fatal(err)
	}

	for _, url := range []string{"/goback", "/goback?at=2025-07-01"} {
		_, body := gbGet(t, ts.URL+url)
		if !strings.Contains(body, "KeepMe") {
			t.Errorf("%s: the surviving message vanished:\n%s", url, body)
		}
		if strings.Contains(body, "RedactMe") {
			t.Errorf("%s: a message whose file was deleted still appears — the on-disk intersection failed (R21):\n%s", url, body)
		}
	}
}

// covers: MA-212, R19, R21, S37, S22
// The go-back page is inert and injection-proof: it carries a script-free,
// CSP-locked policy; a hostile folder name or subject folded into it is escaped
// text (never live markup); a ?at that is not a valid YYYY-MM-DD is rejected
// without crashing and without echoing the raw value; and a history event whose
// key is not a manifest record present on disk surfaces no message.
func TestGobackPageIsInertAndEscaped(t *testing.T) {
	const evilFolder = `<script>alert('folder')</script>`
	const evilSubject = `<img src=x onerror=alert('subj')>`
	a := newGBArchive(t)
	// A real message whose CURRENT folder and subject are hostile.
	a.add("store\x00mid:evil", evilSubject, "store/Inbox/evil.html", evilFolder, "Inbox", true, gbD1, gbD1)
	a.save()
	a.history(func(w *state.HistoryWriter) {
		must(t, w.WriteRunHeader(1, gbD1, []string{"mailbox"}))
		must(t, w.WriteFolder("store\x00mid:evil", evilFolder))
		// A hostile event for a key that has NO manifest record (an off-disk
		// ghost): the fold surfaces it, but the projection must not.
		must(t, w.WriteFolder("store\x00mid:ghost", "Inbox"))
		must(t, w.WriteRunFooter(1, gbD1))
	})
	ts := a.serve()

	// CSP: script-free and locked, on both the current and at-date responses.
	for _, url := range []string{"/goback", "/goback?at=2025-07-01"} {
		resp, body := gbGet(t, ts.URL+url)
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
			t.Errorf("%s: go-back CSP must be script-free/locked, got %q", url, csp)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing nosniff", url)
		}
		// A LIVE injection would carry an unescaped tag opener; the escaped text
		// (which still contains "onerror=alert" harmlessly inside &lt;img…&gt;)
		// must not.
		if strings.Contains(body, "<script>alert") || strings.Contains(body, "<img src=x") {
			t.Errorf("%s: hostile mail markup reached the page live:\n%s", url, body)
		}
		if !strings.Contains(body, "&lt;script&gt;") && !strings.Contains(body, "&lt;img") {
			t.Errorf("%s: hostile markup should be present as ESCAPED text, not dropped:\n%s", url, body)
		}
		// The off-disk ghost key never surfaces (no manifest record on disk).
		if strings.Contains(body, "mid:ghost") || strings.Contains(body, ">ghost<") {
			t.Errorf("%s: an off-disk event key surfaced a message:\n%s", url, body)
		}
	}

	// A ?at that does not parse is rejected, not crashed, and never echoed.
	for _, bad := range []string{"not-a-date", "2025-13-40", "../../etc/passwd", "2025-07-01T00:00:00Z", "%2e%2e"} {
		resp, body := gbGet(t, ts.URL+"/goback?at="+bad)
		if resp.StatusCode != 200 {
			t.Errorf("?at=%q status %d, want 200 (rejected gracefully)", bad, resp.StatusCode)
		}
		if !strings.Contains(body, "could not be read") {
			t.Errorf("?at=%q should announce a bad date:\n%s", bad, body)
		}
		if strings.Contains(body, bad) {
			t.Errorf("?at=%q raw value was reflected into the page:\n%s", bad, body)
		}
	}
}

// covers: MA-213, R21, R18, S37
// History-log recovery legibility on the serve side (X6): a clean log lets serve
// announce "go-back available"; a MISSING log announces "unavailable" (never
// silently current-only, even for a ?at request); a corrupt/torn log announces
// "partial".
func TestGobackAnnouncesAvailability(t *testing.T) {
	// Clean log → available.
	a := newGBArchive(t)
	a.add("store\x00mid:a", "Msg", "store/Inbox/a.html", "Inbox", "Inbox", true, gbD1, gbD1)
	a.save()
	a.history(func(w *state.HistoryWriter) {
		must(t, w.WriteRunHeader(1, gbD1, []string{"mailbox"}))
		must(t, w.WriteFolder("store\x00mid:a", "Inbox"))
		must(t, w.WriteRunFooter(1, gbD1))
	})
	ts := a.serve()
	// The avail-<state> class token appears only on the rendered banner div (the
	// CSS uses the dotted form), so it identifies the announced state exactly.
	if _, body := gbGet(t, ts.URL+"/goback"); !strings.Contains(body, `class="avail avail-available"`) || !strings.Contains(body, "Go-back is available") {
		t.Errorf("clean log should announce go-back available:\n%s", body)
	}

	// Missing log → unavailable, even for a ?at request (announced, not silent).
	b := newGBArchive(t)
	b.add("store\x00mid:b", "Msg", "store/Inbox/b.html", "Inbox", "Inbox", true, gbD1, gbD1)
	b.save() // no history log written
	tsB := b.serve()
	if _, body := gbGet(t, tsB.URL+"/goback?at=2025-07-01"); !strings.Contains(body, `class="avail avail-unavailable"`) || !strings.Contains(body, "Go-back is unavailable") {
		t.Errorf("a missing log must announce go-back unavailable, not silently show current-only:\n%s", body)
	}

	// Corrupt/torn log → partial. Append a torn (newline-less, non-JSON) tail.
	c := newGBArchive(t)
	c.add("store\x00mid:c", "Msg", "store/Inbox/c.html", "Inbox", "Inbox", true, gbD1, gbD1)
	c.save()
	c.history(func(w *state.HistoryWriter) {
		must(t, w.WriteRunHeader(1, gbD1, []string{"mailbox"}))
		must(t, w.WriteFolder("store\x00mid:c", "Inbox"))
	})
	f, err := os.OpenFile(filepath.Join(c.dir, state.HistoryName), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"k":"store` + "\x00" + `mid:c","fol`) // torn, unterminated line
	f.Close()
	tsC := c.serve()
	if _, body := gbGet(t, tsC.URL+"/goback"); !strings.Contains(body, `class="avail avail-partial"`) || !strings.Contains(body, "Go-back is partial") {
		t.Errorf("a torn log must announce go-back partial:\n%s", body)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
