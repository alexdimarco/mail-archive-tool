package localweb_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/localweb/localwebtest"
)

// covers: MA-372, R23, X9, MA-378
// The sources of the pages the surfaces serve — the reader and setup wizard in
// internal/server, the dashboard in internal/desktop — contain no same-origin
// absolute URL literal (src="/…, href="/…, action="/…, fetch('/…, template.URL("/…):
// a belt over the served-content check, which sees only the branches a fixture
// renders. A page template added later is held to it the day it is written [CC17].
func TestSurfaceSourcesHaveNoSameOriginAbsoluteURL(t *testing.T) {
	var scanned []string
	for _, dir := range []string{"../server", "../desktop"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			scanned = append(scanned, f)
			if bad := localwebtest.SameOriginAbsoluteURLs(string(b)); len(bad) != 0 {
				t.Errorf("%s names same-origin absolute URLs %q; make them relative to the page", f, bad)
			}
		}
	}
	assure.Reached(t, scanned, "surface source files scanned")
}
