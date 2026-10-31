package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
)

type navLink struct {
	text, href, id string
	isLink         bool
}

// sidebarNav parses the Overview page and returns every entry of the sidebar
// nav (links and disabled placeholders), the set of element ids in the page,
// and every inline event-handler attribute (on*) found anywhere in it.
func sidebarNav(t *testing.T, page string) (nav []navLink, ids map[string]bool, inline []string) {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("overview does not parse: %v", err)
	}
	ids = map[string]bool{}
	attr := func(n *xhtml.Node, k string) (string, bool) {
		for _, a := range n.Attr {
			if a.Key == k {
				return a.Val, true
			}
		}
		return "", false
	}
	text := func(n *xhtml.Node) string {
		var b strings.Builder
		var f func(*xhtml.Node)
		f = func(m *xhtml.Node) {
			if m.Type == xhtml.TextNode {
				b.WriteString(m.Data)
			}
			for c := m.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		}
		f(n)
		return strings.TrimSpace(b.String())
	}
	var inNav func(n *xhtml.Node)
	inNav = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && (c.Data == "a" || c.Data == "span") {
				if cls, _ := attr(c, "class"); c.Data == "a" || strings.Contains(cls, "nav-disabled") {
					href, _ := attr(c, "href")
					id, _ := attr(c, "id")
					nav = append(nav, navLink{text: text(c), href: href, id: id, isLink: c.Data == "a"})
					continue
				}
			}
			inNav(c)
		}
	}
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			if id, ok := attr(n, "id"); ok {
				ids[id] = true
			}
			for _, a := range n.Attr {
				if strings.HasPrefix(strings.ToLower(a.Key), "on") {
					inline = append(inline, n.Data+"["+a.Key+"]")
				}
			}
			if n.Data == "nav" {
				inNav(n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return nav, ids, inline
}

// covers: MA-302, R12, S41
// Every sidebar entry of the desktop dashboard does something. The page runs
// under script-src 'self', so an inline on* handler is dead (the sidebar's
// "Archive now" was one); a section link must name an anchor that exists on the
// page (resolve-by-name — the Diagnostics/Setup links all pointed at "/", which
// only reloaded the page); the reader links are real URLs when the reader is
// running and visibly disabled when it is not (an empty href reloads, and a bare
// "goback" resolves against the dashboard and 404s); and every element id that
// dashboard.js binds exists on the page.
func TestSidebarNavEveryEntryWorks(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "none.json")
	for _, tc := range []struct {
		name   string
		reader string
	}{
		{"reader running", "http://127.0.0.1:8099/"},
		{"no reader", ""},
	} {
		page := getOverview(t, Config{ConfigPath: cfgPath, ReaderURL: tc.reader})
		nav, ids, inline := sidebarNav(t, page)
		assure.Reached(t, nav, tc.name+": sidebar entries")
		if len(inline) > 0 {
			t.Errorf("%s: inline event handlers are blocked by the dashboard CSP (script-src 'self'): %v", tc.name, inline)
		}
		seen := map[string]bool{}
		for _, l := range nav {
			seen[l.text] = true
			switch l.text {
			case "Overview":
				continue
			case "Open archive", "Go back in time":
				if tc.reader == "" {
					if l.isLink {
						t.Errorf("%s: %q is a link (href %q) although no reader is running", tc.name, l.text, l.href)
					}
				} else if !strings.HasPrefix(l.href, tc.reader) {
					t.Errorf("%s: %q must point into the reader %s, got %q", tc.name, l.text, tc.reader, l.href)
				}
				continue
			}
			if !strings.HasPrefix(l.href, "#") || len(l.href) < 2 {
				t.Errorf("%s: sidebar %q must link to a section on the page, got href %q", tc.name, l.text, l.href)
				continue
			}
			if !ids[l.href[1:]] {
				t.Errorf("%s: sidebar %q links to %q, but no element has id %q", tc.name, l.text, l.href, l.href[1:])
			}
		}
		for _, want := range []string{"Overview", "Archive now", "Open archive", "Go back in time", "Status & health", "Activity log", "Microsoft 365 connection", "Scheduled backup"} {
			if !seen[want] {
				t.Errorf("%s: sidebar entry %q is missing", tc.name, want)
			}
		}
		for _, id := range jsBoundIDs() {
			if !ids[id] && !optionalJSIDs[id] {
				t.Errorf("%s: dashboard.js binds #%s, which is not on the page", tc.name, id)
			}
		}
	}
}

// optionalJSIDs are ids dashboard.js looks up only when present (it checks the
// lookup before use): the clear-sign-in button appears only when signed in, the
// Microsoft 365 Edit/Cancel buttons only when a configuration already exists, the
// Weekly backup card's Repair and "Run now" only for a corroborated run-job schedule
// (§4.6.3/MA-617/MA-694, Q18(b)), and the "Add another archive" button
// and its note live in their own always-present #sec-add-archive card (MA-696, gated on
// CanAddArchive, i.e. at least one known archive), so they are absent only in the shell. Each is bound behind an `if($('…'))` null-check in dashboard.js.
var optionalJSIDs = map[string]bool{"clearSignin": true, "editM365": true, "cancelM365": true, "schedRepair": true, "runBackupBtn": true, "addArchiveBtn": true, "addArchiveNote": true,
	// the Backup-alerts card's "clear the stored password" checkbox shows only when a password is already stored (MA-699); the JS null-checks it
	"alertClearPassword": true,
	// the encrypted Settings card's sync-only box shows only for an encrypted archive whose Run now job can sync
	// Thunderbird (design-presync-attended §8.1, MA-911); the JS null-checks the button, its note and its box
	"encSyncSave": true, "encSyncNote": true, "encSyncPresync": true,
	// the Danger zone's forget-every-encrypted-archive button shows only when this computer recorded one (K8, MA-807); the JS null-checks it
	"forgetAllBtn": true,
	// the local-schedule card's pre-sync checkbox shows only for a Thunderbird-sourced local archive on an adapter platform (MA-582/MA-583); schedPost null-checks it (ps?ps.checked)
	"schedPresync": true}

// jsBoundIDs lists every $('id') lookup in dashboard.js.
func jsBoundIDs() []string {
	var out []string
	s := dashboardJS
	for {
		i := strings.Index(s, "$('")
		if i < 0 {
			return out
		}
		s = s[i+3:]
		j := strings.Index(s, "'")
		if j < 0 {
			return out
		}
		out = append(out, s[:j])
		s = s[j:]
	}
}
