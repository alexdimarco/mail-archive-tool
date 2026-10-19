package desktop

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// covers: MA-375, R12, S45
// The Settings POST (the operator is present) refuses an archive location that
// is, contains or sits inside the folder MailArchive Desktop keeps its settings
// and sign-in in (P10) — its config file's folder, and the default one under the
// OS config dir — naming the location, the folder and the remedy, and the saved
// settings stay as they were. A location apart from them is saved (positive twin).
func TestSettingsRefuseALocationHoldingTheConfigFolder(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	settingsPath := filepath.Join(cfgDir, "desktop-settings.json")
	h := DashboardHandler(Config{ConfigPath: filepath.Join(cfgDir, "graph-config.json"), SettingsPath: settingsPath})
	_, csrf := getPage(t, h)
	save := func(out string) (*int, string) {
		body, _ := json.Marshal(map[string]any{"out": out})
		rec := postJSON(h, "/api/settings", csrf, string(body))
		var resp struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return &rec.Code, resp.Error
	}

	apart := filepath.Join(dir, "Archive")
	if code, msg := save(apart); *code != http.StatusOK || msg != "" {
		t.Fatalf("positive twin (an archive location apart from the settings): status %d %q", *code, msg)
	}
	saved := LoadSettings(settingsPath)
	if saved.Out != apart {
		t.Fatalf("the positive twin saved %q, want %q", saved.Out, apart)
	}

	defDir, err := graphconfig.DefaultConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ out, folder string }{
		{cfgDir, cfgDir},                        // the settings folder itself
		{dir, cfgDir},                           // a folder that contains it
		{filepath.Join(cfgDir, "Mail"), cfgDir}, // a folder inside it
		{defDir, defDir},                        // the default settings folder
	} {
		code, msg := save(c.out)
		assure.Refused(t, *code, msg, assure.Code(http.StatusBadRequest),
			assure.Names(c.out, c.folder, "settings and sign-in", "Do this:"),
			assure.NoSideEffect(func() bool { return reflect.DeepEqual(LoadSettings(settingsPath), saved) }))
	}
}

// covers: MA-375, R12, R17, S45
// The scheduled HeadlessCapture does not refuse an archive location that holds
// its settings folder — refusing there would silently stop an installed weekly
// backup [CC16]: like `graph -unattended` it logs a warning, writes the last-run
// warning config_in_archive, which `status` WARNs, and captures. A location apart
// from the settings folder captures with no warning (positive twin).
func TestHeadlessCaptureWarnsWhenTheArchiveHoldsTheConfigFolder(t *testing.T) {
	srv := newFakeM365(t)
	setup := func(archive, cfgDir string) Config {
		t.Helper()
		cfgPath := filepath.Join(cfgDir, "graph-config.json")
		if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
			t.Fatal(err)
		}
		settingsPath := filepath.Join(cfgDir, "desktop-settings.json")
		if err := SaveSettings(settingsPath, Settings{Out: archive}); err != nil {
			t.Fatal(err)
		}
		tok := filepath.Join(cfgDir, "tok.json")
		if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		return Config{ConfigPath: cfgPath, SettingsPath: settingsPath, TokenCachePath: tok,
			BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode"}
	}

	// Positive twin: the archive location is apart from the settings folder.
	apart := t.TempDir()
	var log bytes.Buffer
	if err := HeadlessCapture(setup(apart, t.TempDir()), &log); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if lr, _, _ := state.ReadLastRun(apart); lr.Warnings != nil || strings.Contains(log.String(), "warning: Your MailArchive settings") {
		t.Errorf("a capture apart from the settings folder warned: %+v\n%s", lr.Warnings, log.String())
	}

	// An existing settings.json whose archive location contains the settings folder.
	archive := t.TempDir()
	cfgDir := filepath.Join(archive, "MailArchive settings")
	log.Reset()
	if err := HeadlessCapture(setup(archive, cfgDir), &log); err != nil {
		t.Fatalf("the scheduled capture must still run: %v", err)
	}
	assure.Reached(t, countHTML(archive), "pages the scheduled capture wrote")
	if want := "warning: Your MailArchive settings and sign-in are stored inside the archive folder " + archive; !strings.Contains(log.String(), want) {
		t.Errorf("the capture log lacks %q:\n%s", want, log.String())
	}
	lr, _, _ := state.ReadLastRun(archive)
	if len(lr.Warnings) != 1 || lr.Warnings[0].Code != "config_in_archive" || !strings.Contains(lr.Warnings[0].Message, cfgDir) {
		t.Errorf("the last-run record carries warnings %+v, want one config_in_archive naming %s", lr.Warnings, cfgDir)
	}
	in := health.Gather(archive, "")
	rep := health.Assess(in, time.Now())
	found := false
	for _, c := range rep.Codes {
		found = found || c == "config_in_archive"
	}
	if !found {
		t.Errorf("status does not WARN config_in_archive: %s %v", rep.Posture, rep.Reasons)
	}
}
