package desktop

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// growManifest adds n records to the real archive in dir through the state package, under its
// first store, as a backup of n more messages records them, so its manifest is that of an archive
// of thousands of messages; it requires the manifest to end over 1 MiB.
func growManifest(t *testing.T, dir string, n int) {
	t.Helper()
	man := filepath.Join(dir, manifestName)
	m, err := state.Load(man)
	if err != nil {
		t.Fatal(err)
	}
	tok := ""
	for _, v := range m.Stores {
		if tok == "" || v < tok {
			tok = v
		}
	}
	if tok == "" {
		t.Fatalf("the archive in %s records no store", dir)
	}
	at := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		m.Add(fmt.Sprintf("%s\x00<grown-%08d@mail.example.com>", tok, i), state.Record{
			Path:        fmt.Sprintf("%s/Inbox/2024-03-01_0900_a-subject-slug-for-a-real-message_%08x.html", tok, i),
			Folder:      "Inbox",
			ExportedAt:  at,
			Fingerprint: fmt.Sprintf("%016x", i),
			FpScheme:    state.FpSchemeCurrent,
		})
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(man); err != nil || fi.Size() <= 1<<20 {
		t.Fatalf("the grown manifest must be over 1 MiB: %v %v", fi, err)
	}
}

// largeEnv is a Desktop with no archive chosen whose Your archives lists a real archive (the
// exporter's own files) and a second folder, so the card is drawn.
type largeEnv struct {
	*wsEnv
	dir    string
	picked string
}

func newLargeEnv(t *testing.T) *largeEnv {
	t.Helper()
	w := newWsEnv(t, func(e *wsEnv) {
		e.cfg.Out = ""
		e.cfg.IndexPath = filepath.Join(e.dir, archindex.Name)
	})
	e := &largeEnv{wsEnv: w, dir: filepath.Join(t.TempDir(), "Large")}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildArchiveAt(t, e.dir)
	other := archiveAt(t, t.TempDir(), "Other", map[string]string{"path:/mail/o.mbox": "o"})
	now := time.Now()
	if err := archindex.WriteFile(e.cfg.IndexPath, archindex.Index{Entries: []archindex.Entry{
		{Path: e.dir, AddedAt: now, LastSeenAt: now}, {Path: other, AddedAt: now, LastSeenAt: now},
	}}); err != nil {
		t.Fatal(err)
	}
	e.d.pick = func(context.Context, string) (string, error) { return e.picked, nil }
	return e
}

// row is the archive's row as archiveRows gives it.
func (e *largeEnv) row() ArchiveRow {
	e.t.Helper()
	idx, err := archindex.ReadFile(e.cfg.IndexPath)
	if err != nil {
		e.t.Fatal(err)
	}
	r, ok := rowByPath(e.cfg.archiveRows(idx), e.dir)
	if !ok {
		e.t.Fatalf("no row for %s", e.dir)
	}
	return r
}

// pageRow is the archive's row on the Overview as a browser loads it: its <li> and its text.
func (e *largeEnv) pageRow() string {
	e.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8097"
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("the Overview: %d", rec.Code)
	}
	page := rec.Body.String()
	at := strings.Index(page, `id="sec-archives"`)
	if at < 0 {
		e.t.Fatal("the Your archives card is not drawn")
	}
	want := `<span class="mono archive-path">` + html.EscapeString(e.dir) + `</span>`
	for _, li := range strings.Split(page[at:], `<li class="archive-row`)[1:] {
		li = li[:strings.Index(li, "</li>")]
		if strings.Contains(li, want) {
			return `<li class="archive-row` + html.UnescapeString(li)
		}
	}
	e.t.Fatalf("no row for %s on the page", e.dir)
	return ""
}

func (e *largeEnv) open(path string) map[string]any {
	e.t.Helper()
	e.picked = path
	_, m := e.post("/api/open-archive", map[string]any{})
	return m
}

func (e *largeEnv) listed(path string) bool {
	idx, err := archindex.ReadFile(e.cfg.IndexPath)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, en := range idx.Entries {
		if cleanPath(en.Path) == cleanPath(path) {
			return true
		}
	}
	return false
}

func replyError(m map[string]any) string {
	s, _ := m["error"].(string)
	return s
}

// covers: MA-1034
// An archive of thousands of messages, its manifest over 1 MiB as a real backup writes it, is a
// normal row of Your archives and opens with Open an archive folder… (from v1.1.0 such a row was
// greyed with no Switch, "Not reachable right now", and Open an archive folder… refused the
// folder as one whose record "cannot be read"). The row is available, local, and offers Switch to
// this archive and Pause; the Desktop's words for a folder with no archive, and for a record that
// cannot be read, are unchanged. Positive twin first: the same archive before it grew.
func TestALargeArchiveIsANormalRowAndOpens(t *testing.T) {
	e := newLargeEnv(t)
	switchAttr := `data-switch-archive data-path="` + e.dir + `"`

	// Positive twin: the archive as exported.
	if r := e.row(); !r.Available || r.Kind != kindLocal {
		t.Fatalf("the positive twin: %+v", r)
	}
	if row := e.pageRow(); strings.Contains(row, "unavailable") || !strings.Contains(row, switchAttr) {
		t.Fatalf("the positive twin on the page: %s", row)
	}
	if m := e.open(e.dir); m["ok"] != true || m["path"] != e.dir {
		t.Fatalf("the positive twin: Open an archive folder… %v", m)
	}

	growManifest(t, e.dir, 6000)
	if r := e.row(); !r.Available || r.Kind != kindLocal || r.Problem != "" {
		t.Fatalf("a large archive's row: available %v, kind %q, problem %q", r.Available, r.Kind, r.Problem)
	}
	row := e.pageRow()
	if strings.Contains(row, "unavailable") || strings.Contains(row, "Not reachable right now") ||
		!strings.Contains(row, switchAttr) || !strings.Contains(row, "data-pause-archive") {
		t.Fatalf("a large archive's row on the page: %s", row)
	}
	if m := e.open(e.dir); m["ok"] != true || m["path"] != e.dir {
		t.Fatalf("Open an archive folder… refused a large archive: %v", m)
	}

	// The Desktop's words for a folder with no archive, and for a record that cannot be read.
	empty := t.TempDir()
	m := e.open(empty)
	assure.Refused(t, 1, replyError(m), assure.Code(1), assure.Names(empty, "isn't a MailArchive archive — choose the folder that holds the .mailarchive-manifest.json file."),
		assure.NoSideEffect(func() bool { return m["ok"] == false && !e.listed(empty) }))
	newer := filepath.Join(t.TempDir(), "Newer")
	if err := os.MkdirAll(newer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newer, manifestName), []byte(`{"version":99,"stores":{"path:/m":"a"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m = e.open(newer)
	assure.Refused(t, 1, replyError(m), assure.Code(1), assure.Names(newer, "cannot be read — type the folder in Settings → Archive location, and Status & health shows how to repair it."),
		assure.NoSideEffect(func() bool { return m["ok"] == false && !e.listed(newer) }))
}

// hostileMbox is two messages from outside senders. The first has an HTML body with a dangling
// cid: image, so the exporter records its subject, and a subject of 180,000 '<' (about 176 KiB,
// under the 1 MiB header cap), which json.Marshal writes in the manifest as about 1.03 MiB.
var hostileMbox = "From a@example.com Mon Jan  1 00:00:00 2024\n" +
	"From: a@example.com\nTo: me@example.com\nSubject: " + strings.Repeat("<", 180000) + "\n" +
	"Message-ID: <hostile@x>\nDate: Mon, 01 Jan 2024 00:00:00 +0000\nContent-Type: text/html\n\n" +
	"<html><body><p>hi</p><img src=\"cid:logo\"></body></html>\n\n" +
	"From b@example.com Mon Jan  1 00:00:00 2024\n" +
	"From: b@example.com\nSubject: ordinary\nMessage-ID: <ok@x>\nDate: Mon, 01 Jan 2024 00:00:00 +0000\n\nbody\n"

// covers: MA-1035
// One email from an outside sender does not make a real archive "not an archive": its subject,
// written over 1 MiB in the manifest, would make a walk that held each value whole refuse the
// archive (MA-1034's adversarial pass, finding 1), greying its row while status found nothing
// wrong. The archive built by the real export is read by status, recognised, an available row
// offering Switch, and opened by Open an archive folder…. Positive twin first: an archive of
// ordinary mail is a normal row.
func TestOneHostileEmailIsANormalRowAndOpens(t *testing.T) {
	e := newLargeEnv(t)
	if r := e.row(); !r.Available || r.Kind != kindLocal {
		t.Fatalf("the positive twin: %+v", r)
	}

	// The archive built from the hostile mailbox, listed in place of the ordinary one.
	if err := os.RemoveAll(e.dir); err != nil {
		t.Fatal(err)
	}
	box := filepath.Join(t.TempDir(), "Inbox")
	if err := os.WriteFile(box, []byte(hostileMbox), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{box}, Out: e.dir, Index: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatalf("build: %v", err)
	}
	os.Remove(filepath.Join(e.dir, ".mailarchive.lock"))
	data, err := os.ReadFile(filepath.Join(e.dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	escaped := []byte("\\" + "u003c") // '<' as json.Marshal writes it: six bytes
	if !bytes.Contains(data, bytes.Repeat(escaped, 180000)) {
		t.Fatalf("the fixture: the manifest must hold the subject as a value over 1 MiB (%d bytes)", len(data))
	}
	in := health.Gather(e.dir, "")
	if !in.HasManifest || in.ManifestLoadErr != nil || in.Messages != 2 {
		t.Fatalf("the fixture: status reads the archive: manifest %v, error %v, %d messages", in.HasManifest, in.ManifestLoadErr, in.Messages)
	}

	if r := e.row(); !r.Available || r.Kind != kindLocal {
		t.Fatalf("an archive with one hostile email: available %v, kind %q", r.Available, r.Kind)
	}
	if row := e.pageRow(); strings.Contains(row, "unavailable") || !strings.Contains(row, `data-switch-archive data-path="`+e.dir+`"`) {
		t.Fatalf("an archive with one hostile email on the page: %s", row)
	}
	if m := e.open(e.dir); m["ok"] != true || m["path"] != e.dir {
		t.Fatalf("Open an archive folder… refused an archive with one hostile email: %v", m)
	}
}

// covers: MA-1035, MA-666
// A row whose manifest the list's read cannot recognise is still greyed with no Switch, as in
// 1.5.2 (UX-3): recognition stays bounded. The stand-in is the real archive's manifest with one
// value nested 70 deep (the walk allows 64), which state.LoadFrom reads. Positive twin first: the
// same archive with its usual manifest is an available row offering Switch.
func TestARowTheListCannotRecogniseIsStillGreyed(t *testing.T) {
	e := newLargeEnv(t)
	switchAttr := `data-switch-archive data-path="` + e.dir + `"`
	if row := e.pageRow(); strings.Contains(row, "unavailable") || !strings.Contains(row, switchAttr) {
		t.Fatalf("the positive twin: %s", row)
	}

	man := filepath.Join(e.dir, manifestName)
	data, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.IndexByte(data, '{')
	if i < 0 {
		t.Fatalf("the fixture: no object in %s", man)
	}
	deep := `"nested":` + strings.Repeat("[", 70) + strings.Repeat("]", 70) + `,`
	nested := append(append(append([]byte(nil), data[:i+1]...), deep...), data[i+1:]...)
	if err := os.WriteFile(man, nested, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := state.LoadFrom(manifestName, nested); err != nil {
		t.Fatalf("the stand-in: LoadFrom must read it: %v", err)
	}
	if archindex.IsArchive(e.dir) {
		t.Fatal("the stand-in: the list's walk must not recognise a value nested 70 deep")
	}
	row := e.pageRow()
	if !strings.Contains(row, "archive-row unavailable") || strings.Contains(row, "data-switch-archive") {
		t.Fatalf("a row the list cannot recognise must be greyed with no Switch: %s", row)
	}
	if r := e.row(); r.Available || r.Kind != "" {
		t.Fatalf("its row: available %v, kind %q", r.Available, r.Kind)
	}
}
