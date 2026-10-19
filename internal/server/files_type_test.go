package server

import (
	"mime"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-374, R19, S22, S45
// /files/ pins the Content-Type of what is not a page: an archived .eml is
// message/rfc822 and an attachment zip application/zip, whatever the host's
// mime database says and whatever the bytes look like, so a raw message that
// begins with HTML is never rendered as a page (nosniff makes the browser obey
// the type). A page stays text/html. The host mapping is simulated by
// registering .eml as text/html, as a host with such an entry would.
// (Adversarial pass AO3.)
func TestFilesPinsTheTypeOfAnEmlAndAZip(t *testing.T) {
	h, dir := filesFixture(t)
	hostile := "<!DOCTYPE html><html><body>PHISH</body></html>"
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(msgStem+".eml")), []byte(hostile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(msgStem+"-attachments.zip")), []byte(hostile), 0o600); err != nil {
		t.Fatal(err)
	}
	// Both suffixes are mapped to a page by the simulated host, so each pin is
	// proved on its own whatever this machine's mime table says (re-verification).
	for _, ext := range []string{".eml", ".zip"} {
		prior := mime.TypeByExtension(ext)
		t.Cleanup(func() {
			if prior != "" {
				mime.AddExtensionType(ext, prior)
			}
		})
		if err := mime.AddExtensionType(ext, "text/html; charset=utf-8"); err != nil {
			t.Fatal(err)
		}
	}

	// Positive twin: a page is text/html.
	if ct := getFile(h, msgStem+".html").Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("page Content-Type %q, want text/html", ct)
	}
	for rel, want := range map[string]string{msgStem + ".eml": "message/rfc822", msgStem + "-attachments.zip": "application/zip"} {
		rec := getFile(h, rel)
		if rec.Code != 200 {
			t.Fatalf("/files/%s: status %d", rel, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != want {
			t.Errorf("/files/%s: Content-Type %q, want %q", rel, ct, want)
		}
	}
}
