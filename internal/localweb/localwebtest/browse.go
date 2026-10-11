package localwebtest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/localweb"
)

// Loaded is one URL a Browse reached.
type Loaded struct {
	URL    string
	Status int
	Type   string // the response's Content-Type
}

var (
	metaRefresh = regexp.MustCompile(`(?i)<meta[^>]+http-equiv="refresh"[^>]+content="[0-9]+;\s*url=([^"]+)"`)
	attrRef     = regexp.MustCompile(`(?i)\b(?:src|href)\s*=\s*"([^"]*)"`)
	jsAPI       = regexp.MustCompile(`['"` + "`" + `]((?:\.?/)?api/[A-Za-z0-9_./-]+)`)
)

// maxLoaded bounds one Browse, so a page that links itself forever ends.
const maxLoaded = 80

// Browse opens the surface served at origin (an httptest server's URL, behind
// localweb.Wrap with the session s) the way a browser would: it redeems a fresh
// one-time link of s, follows the Continue
// page's meta refresh with a cookie jar (which, like a browser's, sends a cookie
// for host 127.0.0.1 to every port), loads the page at prefix+entry, and then
// every script, stylesheet, image and link it names, plus every "api/…" string
// literal in the scripts it loads (the fetch calls) — resolving each relative to
// the page that names it. Anything on host 127.0.0.1 is followed, whichever port,
// so a dashboard's link to the reader's port is loaded with the same cookie; a
// foreign host is not. A POST-only endpoint is fetched with GET and answers 405,
// which is "reached". It returns everything it loaded, in order.
func Browse(t testing.TB, origin string, s *localweb.Session, entry string) []Loaded {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	link := s.Link(origin, localweb.LinkTTL)
	body, _, err := get(client, link)
	if err != nil {
		t.Fatalf("redeeming the launch link: %v", err)
	}
	m := metaRefresh.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the launch link's page navigates nowhere (no meta refresh): %s", body)
	}
	base, _ := url.Parse(link)
	start, err := base.Parse(m[1])
	if err != nil {
		t.Fatalf("the launch page's refresh target %q: %v", m[1], err)
	}
	if entry != "" {
		if start, err = start.Parse(entry); err != nil {
			t.Fatalf("entry %q: %v", entry, err)
		}
	}

	var loaded []Loaded
	seen := map[string]bool{}
	queue := []string{start.String()}
	for len(queue) > 0 && len(loaded) < maxLoaded {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		body, resp, err := get(client, cur)
		if err != nil {
			t.Fatalf("GET %s: %v", cur, err)
		}
		ct := resp.Header.Get("Content-Type")
		loaded = append(loaded, Loaded{URL: resp.Request.URL.String(), Status: resp.StatusCode, Type: ct})
		if resp.StatusCode != http.StatusOK {
			continue
		}
		curURL, _ := url.Parse(cur)
		// The tool's own pages and scripts name what they load; an archived message
		// page under /files/ is the archive's content, not the surface's.
		if strings.Contains(curURL.Path, "/files/") {
			continue
		}
		var refs []string
		switch {
		case strings.HasPrefix(ct, "text/html"):
			for _, r := range attrRef.FindAllStringSubmatch(body, -1) {
				refs = append(refs, r[1])
			}
			for _, r := range jsAPI.FindAllStringSubmatch(body, -1) {
				refs = append(refs, r[1])
			}
		case strings.Contains(ct, "javascript"):
			for _, r := range jsAPI.FindAllStringSubmatch(body, -1) {
				refs = append(refs, r[1])
			}
		}
		for _, ref := range refs {
			if ref == "" || strings.HasPrefix(ref, "#") || strings.HasPrefix(ref, "javascript:") || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "mailto:") {
				continue
			}
			next, err := curURL.Parse(ref)
			if err != nil || next.Hostname() != "127.0.0.1" {
				continue
			}
			next.Fragment = ""
			queue = append(queue, next.String())
		}
	}
	return loaded
}

// Check reports every loaded URL that left the session's prefix, and every one a
// browser would have seen as locked (401) or missing (404). prefix is the
// session's Prefix(); a URL on another port is held to the same prefix.
func Check(loaded []Loaded, prefix string) []string {
	var bad []string
	for _, l := range loaded {
		u, err := url.Parse(l.URL)
		if err != nil {
			bad = append(bad, l.URL+": not a URL")
			continue
		}
		if !strings.HasPrefix(u.Path, prefix) {
			bad = append(bad, fmt.Sprintf("%s: outside %s", l.URL, prefix))
		}
		if l.Status == http.StatusUnauthorized || l.Status == http.StatusNotFound || l.Status >= 500 {
			bad = append(bad, fmt.Sprintf("%s: status %d", l.URL, l.Status))
		}
	}
	return bad
}

func get(c *http.Client, u string) (string, *http.Response, error) {
	resp, err := c.Get(u)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), resp, err
}
