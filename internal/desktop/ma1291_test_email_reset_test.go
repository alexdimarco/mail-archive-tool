package desktop

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/state"
)

// covers: MA-1291
// CAP-R3-A and FRIC-R3-1 (docs/review-thunderbird-completeness-c5-adversarial-r3.md, the lead's
// calls of 2026-11-09), through the Backup alerts card's real "Send test email" handler: an archive's
// Thunderbird download email has failed 5 times in a row an hour ago, so the next backup does not try
// it; once a test email from the card goes, the next due backup tries it and it is delivered. The
// alert_send_failed WARN sends the reader to that card by its name, and the card carries the label
// and the button the WARN names. Positive twin first: a test email that fails changes nothing.
func TestASentTestEmailLetsTheThunderbirdEmailGoAgain(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "desktop-settings.json")
	store := newMemStore()
	if err := SaveSettings(settingsPath, Settings{
		AlertEnabled: true, AlertOnFailure: true, AlertProvider: "smtp",
		AlertHost: "smtp.example.org", AlertPort: 587, AlertSecurity: "starttls",
		AlertFrom: "b@example.org", AlertTo: "t@example.org",
	}); err != nil {
		t.Fatal(err)
	}
	var sent []alert.Event
	failNext := 0
	defer backupalert.SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error {
		if failNext > 0 {
			failNext--
			return errors.New("the mail server did not answer")
		}
		sent = append(sent, ev)
		return nil
	})()

	// An archive whose download email failed 5 times in a row, an hour ago, with no email delivered.
	out := filepath.Join(t.TempDir(), "archive")
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	t0 := now.Add(-80 * time.Hour)
	s := state.TBStreak{}.StartRun(t0).Behind("k1", "imap.k.example", 100, t0)
	s = s.StartRun(now.Add(-time.Hour)).Behind("k1", "imap.k.example", 500, now.Add(-time.Hour)).Named()
	a, _ := s.Account("k1")
	for range state.AlertSendTries {
		s, _ = s.MarkFailed("k1", a.Serial, false, "", now.Add(-time.Hour))
	}
	if err := state.WriteTBStreak(out, s); err != nil {
		t.Fatal(err)
	}
	n := state.TBNotice{RecordID: s.RecordID, Store: "k1", Serial: a.Serial, Account: a.Account, OnDisk: 500, Listed: 1000,
		FirstBehind: a.FirstBehind, LastBehind: a.LastBehind, LastGrew: a.LastGrew}
	backup := func() bool {
		return backupalert.ReportTBBehind(settingsPath, store, out, []state.TBNotice{n}, false, nil)
	}

	d := newDashboard(Config{SettingsPath: settingsPath, Store: store, ConfigPath: filepath.Join(t.TempDir(), "none.json")})
	testEmail := func() string {
		req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/alerts/test", strings.NewReader(`{}`))
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("X-CSRF-Token", d.csrf)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		d.alertTest(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("the test email handler: %d %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	// Positive twin: the test email fails, and the next backup does not try the download email.
	failNext = 1
	if body := testEmail(); !strings.Contains(body, `"ok":false`) {
		t.Fatalf("positive twin: a failing test email answered %s", body)
	}
	if backup() || len(sent) != 0 {
		t.Fatalf("positive twin: after a failed test email the backup sent %d", len(sent))
	}

	// The test email goes; the next due backup sends the download email.
	if body := testEmail(); !strings.Contains(body, `"ok":true`) || len(sent) != 1 {
		t.Fatalf("the test email: %s (%d sent)", body, len(sent))
	}
	if !backup() || len(sent) != 2 || sent[1].Result != backupalert.ResultTBBehind {
		t.Errorf("CAP-R3-A: after Send test email went, the next backup did not send the download email (%d sent)", len(sent))
	}

	// FRIC-R3-1: the card the WARN names, with the label and the button it names.
	page := (Config{SettingsPath: settingsPath, Store: store, ConfigPath: filepath.Join(t.TempDir(), "none.json")}).overview("csrf-token")
	for _, want := range []string{"<h2>Backup alerts</h2>", `<div class="k">Mail server</div>`, ">Send test email</button>"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Backup alerts card is missing %q, which the alert_send_failed WARN names", want)
		}
	}
}
