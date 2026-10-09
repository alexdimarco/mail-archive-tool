package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// msgStem is an exported message's file stem, as the exporter names it.
const msgStem = "store/Inbox/2025-07-03_0900_quarterly-merger_ab12cd34"

// servedContent are the exported files /files/ must serve, each with a marker.
var servedContent = map[string]string{
	"index.html":                   "ROOT-PAGE",
	"README.txt":                   "README-TEXT",
	"store/Inbox/index.html":       "FOLDER-PAGE",
	"store/Inbox/index-2.html":     "FOLDER-PAGE-TWO",
	msgStem + ".html":              "MESSAGE-PAGE",
	msgStem + ".eml":               "MESSAGE-EML",
	msgStem + "-attachments.zip":   "MESSAGE-ZIP",
	"store/Sent/Old/index.html":    "NESTED-FOLDER-PAGE",
	"store/Sent/Old/2024-x_a.html": "NESTED-MESSAGE",
}

// operatorFiles are files an archive holds that /files/ must never serve, each
// with a secret marker: MA-374's list, at the root and inside a store folder.
var operatorFiles = map[string]string{
	"search.db":                        "SECRET-search-db",
	"search.db-wal":                    "SECRET-search-db-wal",
	"search.db-shm":                    "SECRET-search-db-shm",
	".mailarchive-manifest.json":       "SECRET-manifest",
	".mailarchive-history.jsonl":       "SECRET-history",
	".mailarchive-lastrun.json":        "SECRET-lastrun",
	".mailarchive-schedule.json":       "SECRET-descriptor",
	".mailarchive.lock":                "SECRET-lock",
	"mailarchive.log":                  "SECRET-run-log",
	"mailarchive.log.1":                "SECRET-run-log-1",
	"x.stderr.log":                     "SECRET-stderr-log",
	"attachments-report.tsv":           "SECRET-report",
	"attachments-report-legacy.tsv":    "SECRET-legacy-report",
	"graph.token":                      "SECRET-token",
	"x.secret":                         "SECRET-client-secret",
	"_outlook-pst/a.pst":               "SECRET-pst",
	"_outlook-pst/stray.html":          "SECRET-pst-page",
	"BACKUP-NEEDS-ATTENTION.txt":       "SECRET-attention",
	"ARCHIVE-INTEGRITY-ATTENTION.txt":  "SECRET-integrity",
	"stray.html":                       "SECRET-root-page",
	"store/Inbox/graph.token":          "SECRET-store-token",
	"store/Inbox/x.secret":             "SECRET-store-secret",
	"store/Inbox/notes.log":            "SECRET-store-log",
	"store/Inbox/report.tsv":           "SECRET-store-tsv",
	"store/Inbox/.hidden.html":         "SECRET-dot-page",
	"store/.mailarchive-x.tmp":         "SECRET-temp",
	"store/Inbox/archive.zip":          "SECRET-plain-zip",
	"store/Inbox/README.txt":           "SECRET-store-readme",
	"store/.dotdir/index.html":         "SECRET-dotdir-page",
	"store/Unlisted/data.json":         "SECRET-folder-no-index",
	"store/Inbox/x.html.bak":           "SECRET-backup",
	"store/Inbox/2025-07-03_x.eml.txt": "SECRET-eml-txt",
}

// filesFixture writes every served and operator file into a fresh archive
// folder and returns it with its reader (no index is needed for /files/).
func filesFixture(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := tmpDir(t)
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for rel, marker := range servedContent {
		write(rel, marker)
	}
	for rel, marker := range operatorFiles {
		write(rel, marker)
	}
	return New(dir, nil), dir
}

// getFile requests /files/<rel>. A folder page is fetched through its folder's
// URL, as the archive's links reach it: http.FileServer answers any request
// ending in "/index.html" with a redirect to "./" before it opens anything.
func getFile(h http.Handler, rel string) *httptest.ResponseRecorder {
	if rel == "index.html" || strings.HasSuffix(rel, "/index.html") {
		rel = strings.TrimSuffix(rel, "index.html")
	}
	req := httptest.NewRequest(http.MethodGet, "/files/"+rel, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// expectServed asserts rel is served with its marker.
func expectServed(t *testing.T, h http.Handler, rel, marker string) {
	t.Helper()
	rec := getFile(h, rel)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), marker) {
		t.Errorf("/files/%s: status %d, body %q — want it served (%s)", rel, rec.Code, clip(rec.Body.String()), marker)
	}
}

// expectNotServed asserts rel is 404 and its content (marker) never leaks.
func expectNotServed(t *testing.T, h http.Handler, rel, marker string) {
	t.Helper()
	rec := getFile(h, rel)
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusNotFound), assure.Forbid(marker))
}

func clip(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

// covers: MA-374, R19, S22, S45
// /files/ serves exported content only (P9): the root index.html and README.txt,
// a folder's index.html (a folder is reached only through it, never listed), a
// message page, its .eml and its attachment zip. Everything else the archive
// holds — search.db and its siblings, the manifest, history, last-run record,
// schedule descriptor, lock, run logs, reports, a token or secret, the
// _outlook-pst staging folder, attention notices, dotfiles — is 404 and its
// bytes never leave the archive.
func TestFilesServesOnlyExportedContent(t *testing.T) {
	h, _ := filesFixture(t)

	// Positive twin first: every exported file is served.
	for rel, marker := range servedContent {
		expectServed(t, h, rel, marker)
	}
	// The archive root and a folder, each through its index.html.
	expectServed(t, h, "", "ROOT-PAGE")
	expectServed(t, h, "store/Inbox/", "FOLDER-PAGE")
	expectServed(t, h, "store/Sent/Old/", "NESTED-FOLDER-PAGE")

	for rel, marker := range assure.Reached(t, operatorFiles, "operator files planted") {
		expectNotServed(t, h, rel, marker)
	}
	// A folder with no index.html is not listed, and the staging folder and a
	// dot-folder are not reachable at all.
	expectNotServed(t, h, "store/Unlisted/", "data.json")
	expectNotServed(t, h, "store/", "Inbox")
	expectNotServed(t, h, "_outlook-pst/", "a.pst")
	expectNotServed(t, h, "store/.dotdir/", "SECRET-dotdir-page")
	// Spellings that reach an operator file another way: each ends at a 404,
	// whatever redirects net/http's path cleaning answers on the way.
	for _, rel := range []string{"store/../search.db", "store/Inbox/../../graph.token", "SEARCH.DB", "search.db.", "search.db%20", "store/Inbox/notes.log::$DATA", "store%5C..%5Csearch.db", "store/Inbox/%2e%2e/%2e%2e/search.db"} {
		code, bodies := getFollowing(h, "/files/"+rel)
		assure.Refused(t, code, bodies, assure.Code(http.StatusNotFound), assure.Forbid("SECRET-"))
	}
}

// covers: MA-374, R19, S22, S45
// The open is race-free (N10): a link swapped in after the checks is never
// followed, although os.Root alone would follow it while it stays inside the
// archive — the page replaced by a relative link to search.db, or its folder by
// a relative link to a folder holding a same-named page. Positive twin: with the
// fault-injection point reached and nothing swapped, the page is served.
func TestFilesNeverFollowsALinkSwappedInAfterTheCheck(t *testing.T) {
	h, dir := filesFixture(t)
	if err := os.Symlink("search.db", filepath.Join(dir, "probe-link")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	pageRel := filepath.FromSlash(msgStem + ".html")
	page := filepath.Join(dir, pageRel)
	inbox := filepath.Join(dir, "store", "Inbox")
	restore := afterCheck
	t.Cleanup(func() { afterCheck = restore })

	reached := 0
	afterCheck = func(rel string) {
		if rel == pageRel {
			reached++
		}
	}
	expectServed(t, h, msgStem+".html", "MESSAGE-PAGE")
	assure.Reached(t, reached, "the fault-injection point between the checks and the open")

	// The page swapped for a link to search.db.
	afterCheck = func(rel string) {
		if rel != pageRel {
			return
		}
		if err := os.Rename(page, page+".orig"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("..", "..", "search.db"), page); err != nil {
			t.Fatal(err)
		}
	}
	rec := getFile(h, msgStem+".html")
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusNotFound), assure.Forbid("SECRET-search-db"))
	if err := os.Remove(page); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(page+".orig", page); err != nil {
		t.Fatal(err)
	}

	// The page's folder swapped for a link to another folder with a same-named page.
	other := filepath.Join(dir, "store", "Other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, filepath.Base(page)), []byte("SECRET-other-folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterCheck = func(rel string) {
		if rel != pageRel {
			return
		}
		if err := os.Rename(inbox, inbox+"-moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("Other", inbox); err != nil {
			t.Fatal(err)
		}
	}
	rec = getFile(h, msgStem+".html")
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusNotFound), assure.Forbid("SECRET-other-folder"))
}

// covers: MA-374, R19, S22, S45
// Windows reparse points are judged by the shared link rule (fault-isolation
// FC1) [CC13], presented on any OS through the probe seam: a page Go reports as
// ModeIrregular whose reparse tag is not a name surrogate — an online-only cloud
// placeholder (OneDrive Files-On-Demand) — is served as the file it represents,
// and a placeholder folder (ModeIrregular with ModeDir) is walked into, while a
// name surrogate (a junction or mount point: ModeIrregular without ModeDir,
// Readlink succeeds) is 404 at any depth. The rule's table holds on its own too.
func TestFilesServesACloudPlaceholderAndRefusesAJunction(t *testing.T) {
	h, _ := filesFixture(t)
	restore := probeEntry
	t.Cleanup(func() { probeEntry = restore })
	page := filepath.FromSlash(msgStem + ".html")
	inbox := filepath.FromSlash("store/Inbox")
	sent := filepath.FromSlash("store/Sent")
	presented := 0
	probeEntry = func(root *os.Root, name string) (fs.FileInfo, fs.FileMode, bool, error) {
		fi, mode, surrogate, err := restore(root, name)
		switch name {
		case page: // an online-only cloud placeholder page
			mode, surrogate = fs.ModeIrregular|0o644, false
			presented++
		case inbox: // a cloud placeholder folder
			mode, surrogate = fs.ModeIrregular|fs.ModeDir|0o755, false
		case sent: // a junction
			mode, surrogate = fs.ModeIrregular|0o755, true
		}
		return fi, mode, surrogate, err
	}
	expectServed(t, h, msgStem+".html", "MESSAGE-PAGE")
	expectServed(t, h, "store/Inbox/", "FOLDER-PAGE")
	assure.Reached(t, presented, "requests that met the injected placeholder")
	for _, rel := range []string{"store/Sent/Old/2024-x_a.html", "store/Sent/Old/", "store/Sent/"} {
		rec := getFile(h, rel)
		assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusNotFound), assure.Forbid("NESTED-"))
	}

	for _, c := range []struct {
		mode      fs.FileMode
		surrogate bool
		want      entryKind
	}{
		{0o644, false, entryFile},
		{fs.ModeDir | 0o755, false, entryDir},
		{fs.ModeSymlink | 0o777, false, entryRefused},
		{fs.ModeSymlink | 0o777, true, entryRefused},
		{fs.ModeIrregular | 0o644, false, entryFile},             // cloud placeholder file
		{fs.ModeIrregular | fs.ModeDir | 0o755, false, entryDir}, // cloud placeholder folder
		{fs.ModeIrregular | 0o755, true, entryRefused},           // junction or mount point
		{fs.ModeNamedPipe | 0o644, false, entryRefused},
		{fs.ModeSocket | 0o644, false, entryRefused},
		{fs.ModeDevice | 0o644, false, entryRefused},
		{fs.ModeDevice | fs.ModeCharDevice | 0o644, false, entryRefused},
	} {
		if got := classify(c.mode, c.surrogate); got != c.want {
			t.Errorf("classify(%v, surrogate=%v) = %d, want %d", c.mode, c.surrogate, got, c.want)
		}
	}
}

// getFollowing requests target and follows the redirects the server answers (at
// most five), returning the final status and every response body seen.
func getFollowing(h http.Handler, target string) (int, string) {
	var bodies strings.Builder
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		bodies.WriteString(rec.Body.String())
		loc := rec.Header().Get("Location")
		if rec.Code/100 != 3 || loc == "" {
			return rec.Code, bodies.String()
		}
		u, err := url.Parse(target)
		if err != nil {
			return rec.Code, bodies.String()
		}
		next, err := u.Parse(loc)
		if err != nil {
			return rec.Code, bodies.String()
		}
		target = next.RequestURI()
	}
	return 0, bodies.String()
}

// covers: MA-374, R19, S22, S45
// A link at any depth is 404 even when it stays inside the archive: a linked
// folder at the root, a linked folder below it, a linked page pointing at an
// archive page, at search.db or at a token, and a link out of the archive. The
// in-archive links are relative, which os.Root would follow; only the link rule
// refuses them. The same real paths are served (positive twin).
func TestFilesRefusesALinkAtAnyDepth(t *testing.T) {
	h, dir := filesFixture(t)
	outside := filepath.Join(tmpDir(t), "outside.html")
	if err := os.WriteFile(outside, []byte("SECRET-outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{ // link (relative to dir) → target (relative to the link's folder)
		"linked-store":                  "store",
		"store/Linked":                  "Inbox",
		"store/Inbox/link.html":         filepath.Base(msgStem) + ".html",
		"store/Inbox/db.html":           filepath.Join("..", "..", "search.db"),
		"store/Inbox/x-attachments.zip": filepath.Join("..", "..", "graph.token"),
		"store/Inbox/out.html":          outside,
		"store/Inbox/2025-x_out.eml":    outside,
	}
	for link, target := range links {
		if err := os.Symlink(target, filepath.Join(dir, filepath.FromSlash(link))); err != nil {
			t.Skipf("symlinks unavailable here: %v", err)
		}
	}
	expectServed(t, h, msgStem+".html", "MESSAGE-PAGE")
	expectServed(t, h, "store/Inbox/", "FOLDER-PAGE")
	base := filepath.Base(msgStem)
	for _, rel := range []string{
		"linked-store/Inbox/" + base + ".html",
		"linked-store/Inbox/",
		"store/Linked/" + base + ".html",
		"store/Linked/",
		"store/Inbox/link.html",
		"store/Inbox/db.html",
		"store/Inbox/out.html",
		"store/Inbox/2025-x_out.eml",
		"store/Inbox/x-attachments.zip",
	} {
		rec := getFile(h, rel)
		assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusNotFound),
			assure.Forbid("MESSAGE-PAGE", "FOLDER-PAGE", "SECRET-"))
	}
}
