package export

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

func nestedHTML(depth int, inner string) string {
	return strings.Repeat("<div>", depth) + inner + strings.Repeat("</div>", depth)
}

func inlinePNG(payload []byte) model.Attachment {
	return model.Attachment{
		Filename:  "logo.png",
		MimeType:  "image/png",
		ContentID: "<img1@host>",
		WriteTo:   func(w io.Writer) (int64, error) { n, err := w.Write(payload); return int64(n), err },
	}
}

// covers: MA-303, R1, R7, R19
// A mail too deeply nested for the parser (x/net refuses more than 512 open
// elements; browsers render it) is shown as its escaped ORIGINAL source — never
// silently: the run counts it, the verification issues name it, and the
// manifest record carries the reason, so the report regenerated from the
// manifest keeps saying so. Its inline image is not lost: the source shows the
// cid: reference (not a base64 blob), and the image goes into the attachment
// zip. The positive twin, the same mail nested 10 deep, renders as HTML with the
// image embedded and no zip.
func TestExportDeepHTMLFallsBackVisiblyAndKeepsImages(t *testing.T) {
	payload := []byte("PNGDATA-deep")
	mk := func(depth int, subject string) *model.Message {
		return &model.Message{
			Subject: subject, SenderEmail: "a@example.com", InternetMessageID: "<" + subject + "@x>",
			Received:    testDate,
			HTMLBody:    "<html><body>" + nestedHTML(depth, `<p>deep words `+subject+` <img src="cid:img1@host"></p>`) + "</body></html>",
			Attachments: []model.Attachment{inlinePNG(payload)},
		}
	}

	// Positive twin first: shallow mail renders as HTML, image inline, no zip.
	out := t.TempDir()
	man := mustManifest(t)
	e := incExporter(out, man)
	if _, err := e.Export("Store", []string{"Inbox"}, mk(10, "shallowmsg")); err != nil {
		t.Fatal(err)
	}
	shallow := readOnly(t, out, ".html")
	if strings.Contains(shallow, "data-mailarchive-fallback") || !strings.Contains(shallow, "data:image/png;base64,") {
		t.Fatalf("positive twin: shallow mail must render as HTML with the image embedded:\n%s", clipped(shallow))
	}
	if e.Stats.HTMLFallback != 0 || countFiles(t, out, ".zip") != 0 {
		t.Fatalf("positive twin: no fallback and no zip expected (fallback=%d zips=%d)", e.Stats.HTMLFallback, countFiles(t, out, ".zip"))
	}

	out = t.TempDir()
	man = mustManifest(t)
	e = incExporter(out, man)
	if _, err := e.Export("Store", []string{"Inbox"}, mk(600, "deepmsg")); err != nil {
		t.Fatal(err)
	}
	page := assure.Reached(t, readOnly(t, out, ".html"), "deep page")
	if !strings.Contains(page, `<pre class="plain" data-mailarchive-fallback="`+FallbackTooDeep+`">`) {
		t.Errorf("deep mail must be shown as source with reason %q:\n%s", FallbackTooDeep, clipped(page))
	}
	if !strings.Contains(page, "cid:img1@host") || strings.Contains(page, "base64,") {
		t.Errorf("the fallback must show the original source (cid: reference), not an embedded blob:\n%s", clipped(page))
	}
	if !strings.Contains(page, "deep words deepmsg") {
		t.Errorf("the mail's words must still be on the page")
	}
	if e.Stats.HTMLFallback != 1 {
		t.Errorf("Stats.HTMLFallback = %d, want 1 (the fallback must be counted)", e.Stats.HTMLFallback)
	}
	var issue *Issue
	for i := range e.Issues {
		if e.Issues[i].Kind == "html-shown-as-source" {
			issue = &e.Issues[i]
		}
	}
	if issue == nil || issue.Detail != FallbackTooDeep {
		t.Errorf("expected an html-shown-as-source issue with detail %q, got %+v", FallbackTooDeep, e.Issues)
	}
	found := false
	for _, r := range man.Entries {
		if r.BodyFallback == FallbackTooDeep && r.HasIssues() && r.Subject == "deepmsg" {
			found = true
		}
	}
	if !found {
		t.Errorf("the manifest record must carry BodyFallback=%q (and the subject, so the report can name it): %+v", FallbackTooDeep, man.Entries)
	}
	zips := assure.Reached(t, listFiles(t, out, ".zip"), "attachment zips")
	zr, err := zip.OpenReader(zips[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if len(names) != 1 || names[0] != "logo.png" {
		t.Errorf("the inline image must be archived in the zip when the body falls back, got %v", names)
	}
}

func listFiles(t *testing.T, root, suffix string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, suffix) && !strings.Contains(p, string(filepath.Separator)+".") && filepath.Base(p) != "index.html" {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func countFiles(t *testing.T, root, suffix string) int { return len(listFiles(t, root, suffix)) }

func readOnly(t *testing.T, root, suffix string) string {
	t.Helper()
	files := listFiles(t, root, suffix)
	if len(files) != 1 {
		t.Fatalf("expected exactly one %s under %s, got %v", suffix, root, files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func clipped(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}
