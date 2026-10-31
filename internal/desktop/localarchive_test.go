package desktop

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// tbLocalFolders writes a Thunderbird-style Mail/Local Folders tree (mbox files, an .msf index, a
// subfolder, one message with an attachment) and returns the Local Folders directory.
func tbLocalFolders(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Mail", "Local Folders")
	if err := os.MkdirAll(filepath.Join(root, "Archives.sbd"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := func(id, subj, body string) string {
		return "From - Mon Mar  3 09:00:00 2025\nFrom: alice@example.com\nTo: bob@example.com\nSubject: " + subj + "\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + id + "@ex>\n\n" + body + "\n\n"
	}
	att := "From - Mon Mar  3 09:00:00 2025\nFrom: bob@example.com\nTo: alice@example.com\nSubject: with attachment\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <id4@ex>\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=\"BND\"\n\n--BND\nContent-Type: text/plain; charset=utf-8\n\nsee the attached file\n--BND\nContent-Type: application/pdf; name=\"doc.pdf\"\nContent-Disposition: attachment; filename=\"doc.pdf\"\nContent-Transfer-Encoding: base64\n\nSGVsbG8gd29ybGQ=\n--BND--\n\n"
	for name, data := range map[string]string{
		"Inbox":             m("id1", "hello", "body one") + m("id2", "second", "body two") + att,
		"Inbox.msf":         "// <!-- <mdb:mork:z v=\"1.4\"/> -->",
		"Archives.sbd/2025": m("id5", "old", "body five"),
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// tbArchive archives a Thunderbird-style source into a fresh archive with the real engine.
func tbArchive(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "arch")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{tbLocalFolders(t)}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	return out
}

func apiGet(h http.Handler, p string) string {
	req := httptest.NewRequest("GET", p, nil)
	req.Host = "127.0.0.1:8097"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}

// runJobAndWait posts a job endpoint and returns the finished activity text.
func runJobAndWait(t *testing.T, h http.Handler, csrf, path string) string {
	t.Helper()
	r := postAction(h, path, csrf, "127.0.0.1:8097", "http://127.0.0.1:8097")
	if !strings.Contains(r.Body.String(), `"ok":true`) {
		t.Fatalf("%s did not start: %s", path, r.Body.String())
	}
	for i := 0; i < 200; i++ {
		a := apiGet(h, "/api/activity")
		if strings.Contains(a, `"running":false`) {
			return a
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not finish", path)
	return ""
}

// covers: MA-546, R19, R20, S41
// An archive made from local mail files (a Thunderbird Local Folders tree) is read, checked and
// rebuilt by the desktop as any archive is, says what it is, turns Archive now and the weekly backup
// off with the reason and the exact steps that add new mail (the GUI's and the mailarchive command's),
// refuses a direct capture request with the same text, and its Status & health view says where new
// mail comes from. A Microsoft 365, an empty and a mixed archive keep Archive now.
func TestLocalOriginArchiveInDesktop(t *testing.T) {
	out := tbArchive(t)
	cfg := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: out, GOOS: "darwin"}
	h := DashboardHandler(cfg)
	page, csrf := getPage(t, h)

	for _, want := range []string{
		"This archive was made from local mail files (for example Thunderbird), not from Microsoft 365.",
		`id="archiveNowOff"`,
		"Archive now is off for this archive.",
		"Thunderbird / mbox mail folder",
		"Select the output folder",
		"Choose Incremental",
		out,
		"Not used by this archive",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page for a local-origin archive lacks %q", want)
		}
	}
	// NB: this archive is reconstructable (a last-run record with existing inputs), so the
	// Weekly-backup card now shows the local-schedule create form (schedInstall) in place of
	// the backupOff note — the Desktop can schedule a local archive (TP-D). That card shape is
	// pinned by TestLocalScheduleCardUI (create form) and TestMA691... (a SCHEDULED local card
	// has no interval inputs); here we only pin the local-origin read-only messaging below.
	for _, bad := range []string{`id="archiveBtn"`, "Your Microsoft 365 mail archive", "Not configured</div>"} {
		if strings.Contains(page, bad) {
			t.Errorf("the page for a local-origin archive still shows %q", bad)
		}
	}

	// a direct capture request is refused with the steps, and starts nothing
	r := postAction(h, "/api/capture", csrf, "127.0.0.1:8097", "http://127.0.0.1:8097")
	if !strings.Contains(r.Body.String(), `"ok":false`) || !strings.Contains(r.Body.String(), "Archive now is off for this archive") || strings.Contains(r.Body.String(), "open Microsoft 365 setup") {
		t.Errorf("capture on a local-origin archive = %s", r.Body.String())
	}

	// the maintenance actions work on it
	if a := assure.Reached(t, runJobAndWait(t, h, csrf, "/api/verify"), "verify result"); !strings.Contains(a, "attested") || !strings.Contains(a, `"error":""`) {
		t.Errorf("verify on a local-origin archive = %s", a)
	}
	if a := assure.Reached(t, runJobAndWait(t, h, csrf, "/api/rebuild"), "rebuild result"); !strings.Contains(a, "Search index rebuilt: 4 message(s) indexed") {
		t.Errorf("rebuild on a local-origin archive = %s", a)
	}

	// the health view names where new mail comes from
	health := apiGet(h, "/api/health")
	for _, want := range []string{"New mail: this archive was made from local mail files", "mailarchive -input", "-out"} {
		if !strings.Contains(health, want) {
			t.Errorf("health for a local-origin archive lacks %q: %s", want, health)
		}
	}

	// twins: every other kind keeps Archive now
	for kind, edit := range map[string]func(m *state.Manifest){
		"m365":  func(m *state.Manifest) { m.Stores = map[string]string{"mailbox:a@contoso.org": "a"} },
		"mixed": func(m *state.Manifest) { m.Stores["mailbox:a@contoso.org"] = "a" },
		"empty": func(m *state.Manifest) { m.Stores = map[string]string{} },
	} {
		dir := filepath.Join(t.TempDir(), kind)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		mm, err := state.Load(filepath.Join(dir, manifestName))
		if err != nil {
			t.Fatal(err)
		}
		mm.Stores = map[string]string{"path:/mail/Local Folders": "Local Folders"}
		edit(mm)
		if err := mm.Save(); err != nil {
			t.Fatal(err)
		}
		p, _ := getPage(t, DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: dir, GOOS: "darwin"}))
		if !strings.Contains(p, `id="archiveBtn"`) || strings.Contains(p, "Archive now is off") {
			t.Errorf("a %s archive lost its Archive now button", kind)
		}
	}
	// an archive folder with no manifest at all is not judged
	p, _ := getPage(t, DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: t.TempDir()}))
	if !strings.Contains(p, `id="archiveBtn"`) {
		t.Error("an archive with no manifest lost its Archive now button")
	}
}

// covers: MA-547, R19, S41
// With no archive set (no saved location, no launch -out) the dashboard offers the archive the GUI
// wizard used last when that folder still holds an archive, and switches to it only on the owner's
// click (the save a typed location makes); with nothing to offer, a missing or stale folder, or an
// archive already set, it offers nothing, and the empty state names the exact steps in Settings.
func TestOffersLastGUIArchive(t *testing.T) {
	arch := tbArchive(t)
	settings := filepath.Join(t.TempDir(), "s.json")
	cfg := Config{SettingsPath: settings, LastArchive: func() string { return arch }}
	h := DashboardHandler(cfg)
	page, csrf := getPage(t, h)
	for _, want := range []string{"No archive chosen yet", "Use this archive?", `id="useOffer"`, `data-path="` + arch + `"`, "Settings → Archive location"} {
		if !strings.Contains(page, want) {
			t.Errorf("the empty state lacks %q", want)
		}
	}
	if got := cfg.effectiveOut(); got != "" {
		t.Errorf("the dashboard switched to %q by itself", got)
	}
	// accept: the same save a typed location makes
	req := httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"Out":`+jsonQuote(arch)+`}`))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("accepting the offer = %s", rec.Body.String())
	}
	after, _ := getPage(t, h)
	if strings.Contains(after, "Use this archive?") || !strings.Contains(after, "made from local mail files") {
		t.Error("after accepting, the offer is still shown or the archive is not read")
	}

	// nothing to offer
	for name, c := range map[string]Config{
		"no helper":      {SettingsPath: filepath.Join(t.TempDir(), "a.json")},
		"missing folder": {SettingsPath: filepath.Join(t.TempDir(), "b.json"), LastArchive: func() string { return filepath.Join(t.TempDir(), "gone") }},
		"not an archive": {SettingsPath: filepath.Join(t.TempDir(), "c.json"), LastArchive: func() string { return t.TempDir() }},
		"already set":    {SettingsPath: filepath.Join(t.TempDir(), "d.json"), Out: arch, LastArchive: func() string { return arch }},
	} {
		p, _ := getPage(t, DashboardHandler(c))
		if strings.Contains(p, "Use this archive?") || (name != "already set" && !strings.Contains(p, `id="useOffer" type="button" data-path="" hidden`)) {
			t.Errorf("%s: an offer is shown", name)
		}
		if name != "already set" && !strings.Contains(p, "Settings → Archive location") {
			t.Errorf("%s: the empty state does not name the steps", name)
		}
	}
	// a maintenance action with no archive says what to do, and starts nothing
	e := DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "e.json")})
	_, ecsrf := getPage(t, e)
	r := postAction(e, "/api/verify", ecsrf, "127.0.0.1:8097", "http://127.0.0.1:8097")
	if !strings.Contains(r.Body.String(), `"ok":false`) || !strings.Contains(r.Body.String(), "Settings → Archive location") {
		t.Errorf("verify with no archive = %s", r.Body.String())
	}
}

func jsonQuote(s string) string {
	b := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	return `"` + b + `"`
}

// covers: MA-546
// The add-mail steps the dashboard prints are built from the argv the mailarchive command parses
// (cmd/mailarchive checks that, MA-548); here the printed text carries exactly that argv, per OS.
func TestAddMailTextCarriesTheArgv(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		out := `/archives/my mail`
		if goos == "windows" {
			out = `D:\Mail Archive`
		}
		txt := addMailHelp(goos, out)
		if !strings.Contains(txt, shellJoin(AddMailArgv("<your Thunderbird mail folder>", out), goos)) {
			t.Errorf("%s: the text does not carry the documented command: %s", goos, txt)
		}
		if !strings.Contains(txt, shellJoin(AddMailAutoArgv(out), goos)) {
			t.Errorf("%s: the text does not carry the auto command", goos)
		}
		if !strings.Contains(txt, thunderbirdFolderExample(goos)) {
			t.Errorf("%s: no example folder", goos)
		}
	}
}
