package main

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// workspaceReq makes a request to the instance's dashboard under its session prefix, with
// the session cookie, as the page's own scripts do (relative URL, the CSRF token, the
// epoch).
func (g *dRig) workspaceReq(method, rel string, c *http.Cookie, csrf, epoch string) (int, string) {
	g.t.Helper()
	u := "http://" + g.info.Dashboard + g.prefix + rel
	req, _ := http.NewRequest(method, u, strings.NewReader("{}"))
	if c != nil {
		req.AddCookie(c)
	}
	if method == http.MethodPost {
		req.Header.Set("Origin", "http://"+g.info.Dashboard)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("X-Epoch", epoch)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		g.t.Fatalf("%s %s: %v", method, u, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b)
}

var metaRe = func(name string) *regexp.Regexp {
	return regexp.MustCompile(`name="` + name + `" content="([^"]+)"`)
}

// covers: MA-526, MA-405, MA-408, R12, R19
// The binary serves the Extraction workspace through its session mux with relative URLs:
// under the per-launch /s/<P>/ prefix the two pages load, carry no absolute link or script,
// and a guarded POST from the page reaches its endpoint; without the session cookie the
// pages and endpoints are not served. The positive twin of the deployment switch.
func TestDesktopServesTheWorkspaceUnderItsSessionPrefix(t *testing.T) {
	g := newDRig(t)
	g.exportDir = filepath.Join(t.TempDir(), "state")
	g.start()
	_, c := g.cookieFromLink()

	for _, page := range []string{"workspace", "exports"} {
		code, body := g.workspaceReq(http.MethodGet, page, c, "", "")
		if code != http.StatusOK || !strings.Contains(body, "<title>") {
			t.Fatalf("GET %s%s: %d", g.prefix, page, code)
		}
		for _, abs := range []string{`href="/`, `src="/`, `action="/`, `src="http`} { // the reader's own absolute link is the one allowed href
			if strings.Contains(body, abs) {
				t.Errorf("%s has the absolute URL %q: it would escape the session prefix", page, abs)
			}
		}
		if code, _ := g.workspaceReq(http.MethodGet, page, nil, "", ""); code == http.StatusOK {
			t.Errorf("GET %s with no session was served", page)
		}
	}
	_, ws := g.workspaceReq(http.MethodGet, "workspace", c, "", "")
	csrf, epoch := metaRe("csrf").FindStringSubmatch(ws)[1], metaRe("epoch").FindStringSubmatch(ws)[1]
	code, body := g.workspaceReq(http.MethodPost, "api/workspace/source", c, csrf, epoch)
	var out map[string]any
	if json.Unmarshal([]byte(body), &out) != nil || code != http.StatusOK || out["ok"] != true || out["epoch"] != epoch {
		t.Errorf("the workspace's source endpoint under the prefix: %d %s", code, body)
	}
	code, body = g.workspaceReq(http.MethodPost, "api/exports/history", c, csrf, epoch)
	if code != http.StatusOK || !strings.Contains(body, `"runs"`) {
		t.Errorf("the history endpoint with the state directory: %d %s", code, body)
	}
	if code, _ := g.workspaceReq(http.MethodPost, "api/workspace/source", nil, csrf, epoch); code == http.StatusOK {
		t.Error("a POST with no session cookie was served")
	}
	code, overview := g.workspaceReq(http.MethodGet, "", c, "", "")
	if code != http.StatusOK || !strings.Contains(overview, `id="sec-export"`) {
		t.Errorf("the Overview with the workspace on: %d, no Export card", code)
	}
}

// covers: MA-526, R12
// The deployment switch: -no-workspace, and the machine policy, each turn the workspace off
// in the binary: its pages, scripts and endpoints answer 404 and the Overview has no Export
// group (the positive twin is the test above).
func TestDesktopWorkspaceSwitchTurnsItOff(t *testing.T) {
	for name, set := range map[string]func(g *dRig) []string{
		"the -no-workspace flag": func(g *dRig) []string { return []string{"-no-workspace"} },
		"the machine policy":     func(g *dRig) []string { g.policyOff = true; return nil },
	} {
		t.Run(name, func(t *testing.T) {
			g := newDRig(t)
			g.exportDir = filepath.Join(t.TempDir(), "state")
			extra := set(g)
			g.start(extra...)
			_, c := g.cookieFromLink()
			for _, p := range []string{"workspace", "exports", "workspace.js", "exports.js", "signin.js"} {
				if code, _ := g.workspaceReq(http.MethodGet, p, c, "", ""); code != http.StatusNotFound {
					t.Errorf("GET %s with the switch off: %d, want 404", p, code)
				}
			}
			for _, p := range []string{"api/workspace/source", "api/exports/history", "api/m365/signin", "api/workspace/browse"} {
				if code, _ := g.workspaceReq(http.MethodPost, p, c, "x", "x"); code != http.StatusNotFound {
					t.Errorf("POST %s with the switch off: %d, want 404", p, code)
				}
			}
			code, overview := g.workspaceReq(http.MethodGet, "", c, "", "")
			assure.Reached(t, overview, "the Overview")
			if code != http.StatusOK || strings.Contains(overview, `id="sec-export"`) || strings.Contains(overview, ">Export</div>") {
				t.Errorf("the Overview with the switch off: %d, Export group present", code)
			}
		})
	}
}
