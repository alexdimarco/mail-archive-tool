package desktop

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/graphconfig"
)

// covers: MA-956, R36
// The Desktop's scheduled capture (`mailarchive desktop -capture`, HeadlessCapture) and the
// emptied-folder guard (design-archive-delete §7): a folder of the person's files that an
// archive was deleted from gets nothing written — no capture log (the schedule points its
// -log into the folder), no start-failure record (MA-646), no archive — and the refusal goes
// through the alert streak beside the Desktop's settings, so it is told. Positive twins
// first: the same launch into an empty folder opens its log there (so the test can see a
// write), and a signed-in capture into an empty folder archives, its own log beside it.
func TestMA956_ScheduledCaptureRefusesEmptiedFolder(t *testing.T) {
	env := &launchEnv{Stderr: io.Discard}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	settings := filepath.Join(dir, "desktop-settings.json")
	failing := func(out string) bool {
		b, err := os.ReadFile(backupalert.StreakPath(settings, backupalert.KeyFor(out)))
		return err == nil && strings.Contains(string(b), `"failing":true`)
	}
	names := func(p string) []string {
		entries, err := os.ReadDir(p)
		if err != nil {
			t.Fatal(err)
		}
		var n []string
		for _, e := range entries {
			n = append(n, e.Name())
		}
		slices.Sort(n)
		return n
	}

	// Twin: Microsoft 365 not set up, an empty folder — the log is opened in it and the
	// start failure is recorded there, as before.
	twin := t.TempDir()
	_ = launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", twin, "-log", filepath.Join(twin, "mailarchive-capture.log")}, env)
	assure.Reached(t, slices.Contains(names(twin), "mailarchive-capture.log"), "the twin's capture log in its folder")

	// The kept folder: nothing written into it, the refusal alerted.
	kept := t.TempDir()
	mine := []byte("my own notes\n")
	if err := os.WriteFile(filepath.Join(kept, "notes.txt"), mine, 0o600); err != nil {
		t.Fatal(err)
	}
	err := launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", kept, "-log", filepath.Join(kept, "mailarchive-capture.log")}, env)
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(err), assure.Code(1),
		assure.Names(kept, "holds no MailArchive archive", "remove its scheduled backup"),
		assure.NoSideEffect(func() bool {
			got, _ := os.ReadFile(filepath.Join(kept, "notes.txt"))
			return slices.Equal(names(kept), []string{"notes.txt"}) && bytes.Equal(got, mine)
		}))
	if !failing(kept) {
		t.Error("the emptied-folder refusal of a scheduled capture must go through the alert streak")
	}

	// A configured, signed-in scheduled capture: a first one into an empty folder archives
	// beside its own log; into the kept folder it is refused by the engine as well.
	srv := newFakeM365(t)
	tok := filepath.Join(dir, "tok.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := func(out string) Config {
		return Config{ConfigPath: cfgPath, SettingsPath: settings, TokenCachePath: tok, Out: out,
			BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode"}
	}
	first := t.TempDir()
	c := cfg(first)
	c.CaptureLog = filepath.Join(first, "mailarchive-capture.log")
	if err := os.WriteFile(c.CaptureLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := HeadlessCapture(c, io.Discard); err != nil {
		t.Fatalf("a first scheduled capture beside its own log must work: %v", err)
	}
	assure.Reached(t, countHTML(first), "the first capture's messages")
	err = HeadlessCapture(cfg(kept), io.Discard)
	rc = 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(err), assure.Code(1), assure.Names(kept, "holds no MailArchive archive"),
		assure.NoSideEffect(func() bool { return slices.Equal(names(kept), []string{"notes.txt"}) }))
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
