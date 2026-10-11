package desktop

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/instance"
)

// covers: MA-384, R23, X11, S41
// The dashboard's security copy states the new boundary: other accounts on this
// computer and web pages cannot open the dashboard or the archive, and a program
// running as you can; the OS-aware vault wording (MA-272) stays; the old "served
// only on 127.0.0.1" assurance, which said nothing of other accounts, is gone; the
// banner names the OS account the dashboard runs as, and the device-sign-in box
// says whose sign-in it will be and that a sign-in code is shown only in a window
// opened from the shortcut [CC3, CC23]; on macOS and Linux the copy names the
// residual — another account that can list running programs can race a link — and
// on Windows it does not [CC5].
func TestSecurityCopyStatesTheBoundary(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "contoso", ClientID: "APPID", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ConfigPath: cfgPath, TokenCachePath: filepath.Join(t.TempDir(), "tok.json"), Account: `DESKTOP-7\alex`, GOOS: "darwin"}
	page := getOverview(t, cfg)
	for _, want := range []string{
		"other accounts on this computer and web pages cannot open the dashboard or the archive",
		"A program running as you can",
		"private link MailArchive makes each time it starts",
		vaultPhrase(),
		`MailArchive Desktop is running as <b>DESKTOP-7\alex</b>`,
		`This sign-in will be saved for <b>DESKTOP-7\alex</b>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if got := strings.Count(page, "shows a Microsoft sign-in code only in a window it opened from its shortcut"); got < 2 {
		t.Errorf("the sign-in-code sentence appears %d times, want it in the sign-in box and the security panel", got)
	}
	if strings.Contains(page, "served only on 127.0.0.1") || strings.Contains(page, "refuses any network address") {
		t.Errorf("the old assurance, silent about other accounts, is still on the page")
	}
	if !strings.Contains(page, "macOS") || !strings.Contains(page, "list running programs") || !strings.Contains(page, `"already used"`) {
		t.Errorf("the macOS copy does not name the argv residual and what to do about it")
	}
	if p := getOverview(t, Config{ConfigPath: cfgPath, GOOS: "linux"}); !strings.Contains(p, "One residual on Linux") {
		t.Errorf("the Linux copy does not name the residual")
	}
	if p := getOverview(t, Config{ConfigPath: cfgPath, GOOS: "windows"}); strings.Contains(p, "One residual") {
		t.Errorf("the Windows copy names a residual Windows does not have (a standard account cannot read another's command lines)")
	}
}

var sessionsLineRe = regexp.MustCompile(`<span id="sessionsLine">([^<]*)</span>`)

// covers: MA-384, R23
// The header shows "browser sessions opened this run: N (last at HH:MM)" at rest
// — none, then after redemptions — read from the session itself, not from the
// capture log, whose 400-line ring would evict it during a capture: after 500
// log lines the line is still there and the ring still holds 400 [CC24].
func TestHeaderShowsTheBrowserSessionCount(t *testing.T) {
	last := time.Date(2026, 10, 10, 14, 2, 0, 0, time.Local)
	n := 0
	cfg := Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), Sessions: func() (int, time.Time) { return n, last }}
	line := func(page string) string {
		m := sessionsLineRe.FindStringSubmatch(page)
		if m == nil {
			t.Fatalf("the page has no sessions line:\n%s", page)
		}
		return m[1]
	}
	if got := line(getOverview(t, cfg)); got != "browser sessions opened this run: 0" {
		t.Errorf("at rest with none opened: %q", got)
	}
	n = 2
	if got := line(getOverview(t, cfg)); got != "browser sessions opened this run: 2 (last at 14:02)" {
		t.Errorf("after two redemptions: %q", got)
	}

	d := &dashboard{cfg: cfg, csrf: "x", cap: &captureState{}}
	for i := 0; i < 500; i++ {
		d.cap.appendLine("capture line")
	}
	if len(d.cap.lines) != logRingMax {
		t.Fatalf("the ring holds %d lines, want %d", len(d.cap.lines), logRingMax)
	}
	if got := line(d.cfg.overview("x")); got != "browser sessions opened this run: 2 (last at 14:02)" {
		t.Errorf("after a capture filled the ring: %q", got)
	}
	rec := httptest.NewRecorder()
	d.activity(rec, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
	if strings.Contains(rec.Body.String(), "browser sessions") {
		t.Errorf("the session count is in the capture log: %s", rec.Body.String())
	}
}

// covers: MA-384, MA-376, R23
// A start that found another program at MailArchive's former address
// 127.0.0.1:8097, or an older MailArchive Desktop it could not end, says so in
// the header in the §7.6 words, and a start that found nothing says nothing
// [CC22, CC23, ruling OQ-9].
func TestHeaderNamesWhatHoldsTheFormerAddress(t *testing.T) {
	cfg := Config{ConfigPath: filepath.Join(t.TempDir(), "none.json")}
	if p := getOverview(t, cfg); strings.Contains(p, "formerNotice") || strings.Contains(p, "former address") || strings.Contains(p, "older MailArchive") {
		t.Errorf("a clean start names a former address")
	}
	for _, c := range []struct{ former, text string }{
		{instance.FormerProgram, instance.FormerProgramNotice},
		{instance.FormerPreUpgrade, instance.PreUpgradeNotice},
	} {
		cfg.Former = c.former
		p := getOverview(t, cfg)
		assure.Reached(t, p, "the page")
		if !strings.Contains(p, html.EscapeString(c.text)) || !strings.Contains(p, `id="formerNotice"`) {
			t.Errorf("former=%s: the header lacks %q", c.former, c.text)
		}
	}
	if !strings.Contains(instance.FormerProgramNotice, "former address 127.0.0.1:8097") || !strings.Contains(instance.FormerProgramNotice, "only from its shortcut") {
		t.Errorf("the notice lost its §7.6 words: %q", instance.FormerProgramNotice)
	}
}

// covers: MA-384, R23, X9
// "Use another browser…" mints a fresh one-time link through a guarded POST: with
// the CSRF token and a loopback Origin and Host it returns the link, without
// them it is refused; with no way to make one it says so.
func TestAnotherBrowserLinkIsAGuardedPost(t *testing.T) {
	cfg := Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), NewLink: func() string { return "http://127.0.0.1:1/_open?t=FRESH" }}
	h := DashboardHandler(cfg)
	get := httptest.NewRequest(http.MethodGet, "/", nil)
	get.Host = "127.0.0.1:8097"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get)
	m := regexp.MustCompile(`name="csrf" content="([0-9a-f]+)"`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("no CSRF token on the page")
	}
	post := func(token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/browser-link", nil)
		r.Host = "127.0.0.1:8097"
		if token != "" {
			r.Header.Set("X-CSRF-Token", token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		return rr
	}
	var ok struct {
		OK   bool   `json:"ok"`
		Link string `json:"link"`
	}
	got := post(m[1], "http://127.0.0.1:8097")
	if err := json.Unmarshal(got.Body.Bytes(), &ok); err != nil || !ok.OK || ok.Link != "http://127.0.0.1:1/_open?t=FRESH" {
		t.Errorf("a guarded POST: %d %q, want the link", got.Code, got.Body.String())
	}
	for _, rr := range []*httptest.ResponseRecorder{
		post("", "http://127.0.0.1:8097"),     // no token
		post("0000", "http://127.0.0.1:8097"), // a wrong token
		post(m[1], "http://attacker.example"), // a foreign Origin
	} {
		assure.Refused(t, rr.Code, rr.Body.String(), assure.Code(http.StatusForbidden), assure.Forbid("_open?t="))
	}
	wrongMethod := httptest.NewRecorder()
	h.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodGet, "/api/browser-link", nil))
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/browser-link: %d, want 405", wrongMethod.Code)
	}
	none := DashboardHandler(Config{ConfigPath: cfg.ConfigPath})
	rec = httptest.NewRecorder()
	none.ServeHTTP(rec, get)
	m = regexp.MustCompile(`name="csrf" content="([0-9a-f]+)"`).FindStringSubmatch(rec.Body.String())
	r := httptest.NewRequest(http.MethodPost, "/api/browser-link", nil)
	r.Host = "127.0.0.1:8097"
	r.Header.Set("X-CSRF-Token", m[1])
	r.Header.Set("Origin", "http://127.0.0.1:8097")
	rr := httptest.NewRecorder()
	none.ServeHTTP(rr, r)
	if !strings.Contains(rr.Body.String(), `"ok":false`) {
		t.Errorf("with no way to make a link: %q, want ok:false", rr.Body.String())
	}
}

// covers: MA-384, R23
// Skip encoder (open by design): the extraction workspace's own sign-in surface
// (`/api/m365/signin`) carries the account banner and the "a sign-in code only in
// a window opened from the shortcut" copy [CC3, CC23].
func TestWorkspaceSignInSurfaceCarriesTheAccountBanner(t *testing.T) {
	t.Skip("MA-384: the extraction workspace's sign-in surface does not exist yet; its banner and sign-in-code copy land with slice E3 (rulings §9 row 24)")
}
