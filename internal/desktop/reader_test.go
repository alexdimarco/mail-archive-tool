package desktop

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/index"
)

// covers: MA-277, R19, S41
// The embedded reader serves the archive (reusing server.New) for the current
// archive location once an index exists, and returns a legible 503 — not a crash —
// before any capture has created one.
func TestReaderEmbed(t *testing.T) {
	out := t.TempDir()
	rm := ReaderHandler(Config{Out: out})
	// The reader holds its index open to serve; release it before t.TempDir's
	// cleanup removes the dir (on Windows an open file cannot be deleted). This
	// defer runs before the TempDir Cleanup, which is registered after it.
	defer rm.(io.Closer).Close()

	// No index yet → a legible 503.
	rec := httptest.NewRecorder()
	rm.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no archive yet should be 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "run a capture first") {
		t.Errorf("503 body should guide the user: %s", rec.Body.String())
	}

	// Create an index → the reader serves the search UI.
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	ix.Close()
	rec = httptest.NewRecorder()
	rm.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("with an index the reader should serve, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Archive Search") {
		t.Errorf("reader did not serve the search UI")
	}
}
