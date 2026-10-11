package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-384, R23, X11
// `serve`'s start-up text no longer says it "has no authentication": the warning
// for a bind beyond loopback says the archive and the session cookie cross the
// network in clear text and that each browser still needs its private link,
// naming how to get one (`mailarchive serve -link -out <folder>`, the folder
// quoted for a shell); and a loopback run's own output names the link and never
// the old claim.
func TestServeStartUpTextNamesTheLinkAndNeverSaysNoAuthentication(t *testing.T) {
	w := lanWarning("0.0.0.0:8099", "/data/my archive")
	for _, want := range []string{"WARNING", "0.0.0.0:8099", "clear text", "private link", "press Enter", "mailarchive serve -link -out '/data/my archive'", "127.0.0.1 (the default)"} {
		if !strings.Contains(w, want) {
			t.Errorf("the LAN warning lacks %q: %s", want, w)
		}
	}
	if strings.Contains(strings.ToLower(w), "no authentication") {
		t.Errorf("the LAN warning still says there is no authentication: %s", w)
	}

	out := buildSearchFixture(t)
	g := startSurface(t, rigOpts{stdoutTTY: true, display: true}, serveMain, out, serveArgs(out)...)
	text := g.stdout.String() + g.stderr.String()
	assure.Reached(t, text, "serve's start-up text")
	if strings.Contains(strings.ToLower(text), "no authentication") || !strings.Contains(text, "If your browser did not open, use: ") {
		t.Errorf("serve's start-up text:\n%s", text)
	}
}

// covers: MA-384, R23
// The README's `serve` section matches: it explains the private link, `serve
// -link`, -no-browser, the tunnel, the scripts' move to `mailarchive search`, the
// macOS and Linux residual and the clear-text LAN warning, and no longer says
// "no authentication" or tells the reader to type the old address.
func TestREADMEServeSectionMatches(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := strings.ToLower(strings.Join(strings.Fields(string(b)), " ")) // one line, whatever the wrapping
	for _, want := range []string{
		"private link", "mailarchive serve -link -out ./export", "-no-browser", "ssh -l 9000:127.0.0.1:8099",
		"mailarchive search", "already used", "clear text", "other accounts on this computer",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("the README lacks %q", want)
		}
	}
	for _, bad := range []string{"no authentication", "then open http://127.0.0.1:8099/", "search + read at http://127.0.0.1:8099/"} {
		if strings.Contains(readme, bad) {
			t.Errorf("the README still says %q", bad)
		}
	}
}
