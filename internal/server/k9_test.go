package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
)

// k9Page renders a message page as the export writes it, beside base-attachments.zip
// and base.eml — the real renderer, so the links the reader looks for cannot drift
// from the ones it writes.
func k9Page(t *testing.T, base string) []byte {
	t.Helper()
	m := &model.Message{
		Subject: "Quarterly plan", SenderName: "Bob", SenderEmail: "bob@example.com", PlainBody: "see attached",
		Attachments: []model.Attachment{{Filename: "plan.pdf", MimeType: "application/pdf",
			WriteTo: func(w io.Writer) (int64, error) { n, err := w.Write([]byte("%PDF")); return int64(n), err }}},
	}
	rr, err := export.RenderWith(m, export.RenderContext{RootRel: "../", FolderIndexRel: "index.html",
		ZipName: base + "-attachments.zip", RawName: base + ".eml"})
	if err != nil {
		t.Fatal(err)
	}
	return rr.HTML
}

// get serves path through h and returns the status and body.
func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

// covers: MA-852, R37
// K9 (P3-8, B8): a reader of an encrypted archive says a download is not encrypted.
// Its message page carries the line beside the attachment zip's link and beside the
// original .eml's — a page name the export escapes included — and its search page
// carries it once. The positive twin first: a plaintext folder's reader, whose files
// are plain already, carries no such line.
func TestAnEncryptedReaderSaysADownloadIsNotEncrypted(t *testing.T) {
	plain := Serve(Folder(t.TempDir(), nil))
	if _, body := get(t, plain, "/"); strings.Contains(body, DownloadNotEncrypted) || !strings.Contains(body, `id="meta"`) {
		t.Fatalf("positive twin: a plaintext reader's search page:\n%s", body)
	}

	const base = "2024-01-02_plan&notes_ab12"
	page := k9Page(t, base)
	if !strings.Contains(string(page), "download zip</a>") || !strings.Contains(string(page), "Original message (.eml)</a>") {
		t.Fatalf("the renderer wrote no download links:\n%s", page)
	}
	open := func(r *http.Request, rel string) ([]byte, func(), error) { return page, func() {}, nil }
	sealed := http.StripPrefix("/files/", SealedFiles(open, func() string { return "" }, time.Minute))
	code, body := get(t, sealed, "/files/Inbox/"+base+".html")
	zip := strings.Index(body, "download zip</a>")
	eml := strings.Index(body, "Original message (.eml)</a>")
	if code != http.StatusOK || zip < 0 || eml < 0 ||
		!strings.HasPrefix(body[zip+len("download zip</a>"):], ` <span class="mailarchive-k9">(`+DownloadNotEncrypted) ||
		!strings.HasPrefix(body[eml+len("Original message (.eml)</a>"):], ` <span class="mailarchive-k9">(`+DownloadNotEncrypted) {
		t.Fatalf("the message page's download links carry no K9 line: %d\n%s", code, body)
	}
	if string(page) != string(k9Page(t, base)) {
		t.Fatal("the page's own bytes were changed in place")
	}
	// A page with no download link, and a download itself, are served as they are.
	if got := withDownloadLine("x.html", []byte("<p>no links</p>")); string(got) != "<p>no links</p>" {
		t.Fatalf("a page with no download link: %s", got)
	}
	if got := withDownloadLine(base+"-attachments.zip", page); string(got) != string(page) {
		t.Fatal("a download itself was changed")
	}

	enc := Serve(sealedSource{open: open})
	_, body = get(t, enc, "/")
	assure.Reached(t, strings.Count(body, DownloadNotEncrypted) == 1 && strings.Contains(body, `id="k9"`), "the encrypted reader's search page carries K9's line once")
}

// sealedSource is a Source that serves through SealedFiles, as an encrypted archive's
// reader does; it holds no index.
type sealedSource struct {
	open SealedOpen
}

func (s sealedSource) UseIndex(f func(*index.Index) error) error { return f(nil) }
func (s sealedSource) Files() http.Handler {
	return SealedFiles(s.open, s.LockedNote, time.Minute)
}
func (s sealedSource) GoBack() (GoBack, error) { return GoBack{Encrypted: true}, nil }
func (s sealedSource) LockedNote() string      { return "" }
