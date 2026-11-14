package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-1311, R45
// The Desktop's Status & health view says the Gmail waiting line `status` says, from the same
// source (the B3 UX walk's U7, UX contract X10). Positive twin first: with no Gmail store off it
// says none.
func TestTheHealthViewSaysTheGmailQuestionWaits(t *testing.T) {
	e := newDeleteEnv(t)
	out := e.plainArchive("Gmail")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	view := func() string {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Host = "127.0.0.1:8097"
		e.h.ServeHTTP(rec, req)
		var m struct{ Summary []string }
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &m) != nil {
			t.Fatalf("the health view: %d %s", rec.Code, rec.Body.String())
		}
		return strings.Join(m.Summary, "\n")
	}
	line := state.GmailWaitingLine("gmail-store")
	if strings.Contains(view(), "Gmail: not switched on") {
		t.Fatal("positive twin: the view says a question waits with no Gmail store off")
	}
	gmailWaits(t, out, "gmail-store")
	if got := view(); !strings.Contains(got, line) {
		t.Fatalf("the health view does not say %q:\n%s", line, got)
	}
}
