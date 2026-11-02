package desktop

import (
	"bytes"
	"context"
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
	"mail-archive-tool/internal/health"
)

// The v1.5.3 patch's Desktop test of one hostile email, brought onto this line when main was
// merged after the patch shipped (its other two tests are covered here by
// TestALargeArchiveIsANormalRowAndOpens and TestARowTheListCannotRecogniseIsGreyedAndStillOfferedDelete).

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
