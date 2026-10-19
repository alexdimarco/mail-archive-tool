package desktop

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-499, R18, R1
// RV-10 (X10, one status truth): the dashboard's Status & health card prints the
// same "Shown as source" line `status` does, for an archive with a message shown
// as plain text, and not for one without. (The GUI's health view prints the same
// health.Summary lines.)
func TestHealthCardSaysMessagesAreShownAsSource(t *testing.T) {
	card := func(fallback string) string {
		out := t.TempDir()
		m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
		m.Add("k", state.Record{Path: "x.html", Folder: "F", BodyFallback: fallback})
		if err := m.Save(); err != nil {
			t.Fatal(err)
		}
		h := DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: out})
		_, _ = getPage(t, h)
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.Host = "127.0.0.1:8097"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	body := card("too-deeply-nested")
	for _, want := range []string{"Shown as source: 1 message(s)", "nested too deeply", "Nothing to do"} {
		if !strings.Contains(body, want) {
			t.Errorf("health card lacks %q: %s", want, body)
		}
	}
	if strings.Contains(card(""), "Shown as source") {
		t.Error("positive twin: the card names shown-as-source for an archive with none")
	}
}
