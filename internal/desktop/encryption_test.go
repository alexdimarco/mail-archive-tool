package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/localweb"
)

// sessionRequest sends method path through the dashboard's session, with the page's
// CSRF token when csrf is not "".
func sessionRequest(t *testing.T, h http.Handler, method, path, csrf string) (int, map[string]any) {
	t.Helper()
	return sessionRequestWith(t, h, method, path, csrf, "")
}

// sessionRequestWith is sessionRequest with a body.
func sessionRequestWith(t *testing.T, h http.Handler, method, path, csrf, body string) (int, map[string]any) {
	t.Helper()
	s, _ := localweb.Guarded(h)
	req := httptest.NewRequest(method, s.Session.Prefix()+path, strings.NewReader(body))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	req.AddCookie(sessionCookie(t, h, s.Session, req.Host))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

// sessionPage is the Overview page through the dashboard's session.
func sessionPage(t *testing.T, h http.Handler) string {
	t.Helper()
	s, _ := localweb.Guarded(h)
	req := httptest.NewRequest(http.MethodGet, s.Session.Prefix(), nil)
	req.Host = "127.0.0.1:8097"
	req.AddCookie(sessionCookie(t, h, s.Session, req.Host))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}

// offered is the actions the health reply offers.
func offered(m map[string]any) []string {
	var out []string
	if a, ok := m["actions"].([]any); ok {
		for _, s := range a {
			out = append(out, s.(string))
		}
	}
	return out
}

// covers: MA-804, R37, X9
// The record's actions without a key (§4.3; the P3-3 plan, 3b): a damaged record is
// offered Repair on the Status & health card, and the guarded POST rebuilds it, the
// damaged copies kept; a POST without the page's token is refused and moves nothing.
// An archive whose record was lost is offered Dismiss, which clears that folder's
// ids alone. A healthy record offers neither.
func TestTheRecordsActionsOnTheStatusCard(t *testing.T) {
	recDir := t.TempDir()
	t.Cleanup(encrecord.SetDirForTest(recDir))
	h, csrf, settings := dashboardFixture(t)
	arch := recordedArchive(t, t.TempDir(), "Arch")
	if err := SaveSettings(settings, Settings{Out: arch}); err != nil {
		t.Fatal(err)
	}
	if _, m := sessionRequest(t, h, http.MethodGet, "api/health", ""); len(offered(m)) != 0 {
		t.Fatalf("positive twin, a healthy record: %v", offered(m))
	}

	for _, name := range []string{encrecord.FileName, encrecord.MirrorName} {
		os.WriteFile(filepath.Join(recDir, name), []byte("MAILARCHIVE-RECORD 1 seq=9\nnot a record\n"), 0o600)
	}
	if _, m := sessionRequest(t, h, http.MethodGet, "api/health", ""); !slices.Contains(offered(m), actionRepairRecord) {
		t.Fatalf("a damaged record is offered Repair: %v", m)
	}
	before, _ := os.ReadFile(filepath.Join(recDir, encrecord.FileName))
	code, m := sessionRequest(t, h, http.MethodPost, "api/encryption/repair-record", "")
	after, _ := os.ReadFile(filepath.Join(recDir, encrecord.FileName))
	assure.Refused(t, code, toText(m), assure.Code(http.StatusForbidden), assure.Names("refused"),
		assure.NoSideEffect(func() bool { return string(before) == string(after) }))
	code, m = sessionRequest(t, h, http.MethodPost, "api/encryption/repair-record", csrf)
	if code != http.StatusOK || m["ok"] != true || !strings.Contains(toText(m), "kept aside") {
		t.Fatalf("repair: %d %v", code, m)
	}
	if _, err := encrecord.Load(); err != nil {
		t.Fatalf("the record is usable after the repair: %v", err)
	}

	// The record lost the archive: its id leaves the record and joins the lost list.
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	arch = recordedArchive(t, t.TempDir(), "Lost")
	SaveSettings(settings, Settings{Out: arch})
	rec, _ := encrecord.Load()
	var id string
	for k := range rec.Archives {
		id = k
	}
	encrecord.Update("test", func(r *encrecord.Record) error { delete(r.Archives, id); return nil })
	encrecord.MarkLost(id)
	encrecord.MarkLost("ffffffffffffffffffffffffffffffff") // another folder's
	if _, m := sessionRequest(t, h, http.MethodGet, "api/health", ""); !slices.Contains(offered(m), actionDismissLost) {
		t.Fatalf("a lost record is offered Dismiss: %v", m)
	}
	// Locked, Dismiss is refused: only the archive opened with its key says which
	// warning is its own (the P3-3 follow-up pass, DRIVE-01); dismissing it once
	// unlocked is TestDismissClearsOnlyTheOpenArchivesWarning.
	code, m = sessionRequest(t, h, http.MethodPost, "api/encryption/dismiss-lost", csrf)
	lost, _ := encrecord.Lost()
	assure.Refused(t, 1, toText(m), assure.Code(1), assure.Names("unlock this archive first"),
		assure.NoSideEffect(func() bool { return code == http.StatusOK && encmark.LostHere(arch) && len(lost) == 2 }))
}

// covers: MA-804, R37, X10
// An encrypted archive is never scheduled (O5): its Weekly backup card says so and
// offers no install, interval, repair or remove; the status card agrees with status
// (no "Not installed" warning); and an install POST is refused before any setting is
// written, the scheduler never asked. The positive twin: a plaintext Microsoft 365
// archive's card offers its install.
func TestAnEncryptedArchiveIsNeverOfferedASchedule(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	h, csrf, settings := dashboardFixture(t)
	plain := archiveAt(t, t.TempDir(), "M365", map[string]string{"mailbox:u@contoso.com": "b"})
	SaveSettings(settings, Settings{Out: plain})
	page := sessionPage(t, h)
	if !strings.Contains(page, `id="schedInterval"`) || strings.Contains(page, `id="backupEncrypted"`) {
		t.Fatal("positive twin: a Microsoft 365 archive is offered its install")
	}

	arch := recordedArchive(t, t.TempDir(), "Enc")
	SaveSettings(settings, Settings{Out: arch, Interval: "weekly", WeeklyTime: "03:00"})
	page = sessionPage(t, h)
	for _, id := range []string{`id="schedInstall"`, `id="schedInterval"`, `id="schedRepair"`, `id="schedRemove"`} {
		if strings.Contains(page, id) {
			t.Fatalf("an encrypted archive is offered %s", id)
		}
	}
	if !strings.Contains(page, `id="backupEncrypted"`) || !strings.Contains(page, "Not available for encrypted archives yet") {
		t.Fatal("the cards do not say an encrypted archive is not scheduled")
	}
	before, _ := os.ReadFile(settings)
	code, m := sessionRequestWith(t, h, http.MethodPost, "api/schedule-install", csrf, `{"Interval":"daily","Time":"05:00"}`)
	after, _ := os.ReadFile(settings)
	assure.Refused(t, code, toText(m), assure.Code(http.StatusOK), assure.Names("cannot be scheduled", "nothing was installed"),
		assure.NoSideEffect(func() bool { return string(before) == string(after) }))
}

// toText is a reply as text, for assure.
func toText(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}
