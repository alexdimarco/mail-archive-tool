package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-386, R12, S33
// MailArchive Desktop speaks to its owner (g7 friction F3). With a damaged
// search.db the Status & health card names its own "Maintenance → Rebuild search
// index" button — no `mailarchive reindex` command, no SQLite text — and a
// capture or maintenance job that fails on the same index reports it in the same
// words ("Capture failed: ..."), while a failure that is not an index refusal
// keeps its own text. Positive twin: a healthy archive's card has no such line.
func TestDesktopNamesItsOwnRebuildButton(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":5,"entries":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h := DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: out})
	_, _ = getPage(t, h)
	health := func() string {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.Host = "127.0.0.1:8097"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("health = %d", rec.Code)
		}
		var v struct{ Summary []string }
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return strings.Join(v.Summary, "\n")
	}
	if got := health(); strings.Contains(got, "search index") && strings.Contains(got, "cannot be read") {
		t.Fatalf("positive twin: an archive with no search.db reads as damaged:\n%s", got)
	}

	idx := filepath.Join(out, "search.db")
	junk := bytes.Repeat([]byte("not a sqlite database "), 200)
	if err := os.WriteFile(idx, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	got := health()
	if !strings.Contains(got, "Maintenance → Rebuild search index") || strings.Contains(got, "mailarchive reindex") || strings.Contains(got, "not a database") {
		t.Fatalf("the card must name its own button and no command or SQLite text:\n%s", got)
	}

	// The same index refusal through a failing job, in the owner's words.
	src := filepath.Join(t.TempDir(), "Inbox", "cur")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "1.eml"), []byte("From: a@example.com\r\nSubject: s\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <m@x>\r\n\r\nbody\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := app.Run(context.Background(), app.Options{Inputs: []string{filepath.Dir(src)}, Out: out, Mode: export.Incremental, Index: true}, nil, nil)
	if err == nil {
		t.Fatal("fixture: a run over a damaged search.db succeeded")
	}
	msg := jobErrorText(out, err)
	if !strings.Contains(msg, "damaged") || !strings.Contains(msg, "Maintenance → Rebuild search index") || strings.Contains(msg, "mailarchive reindex") {
		t.Fatalf("the job's failure is not in the owner's words: %q", msg)
	}
	if got := jobErrorText(out, os.ErrPermission); got != os.ErrPermission.Error() {
		t.Fatalf("an unrelated failure was rewritten: %q", got)
	}
}
