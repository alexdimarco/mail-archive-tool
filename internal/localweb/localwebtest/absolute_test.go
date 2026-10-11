package localwebtest

import "testing"

// covers: MA-372, R23
// The structural check names every way a page can hold a same-origin absolute URL
// — the five forms the design lists (src, href, action, fetch, template.URL) and a
// location assignment, in either quote — and passes the relative forms, a
// fragment, a foreign origin and a protocol-relative one.
func TestSameOriginAbsoluteURLs(t *testing.T) {
	bad := []string{
		`<script src="/app.js"></script>`, `<a href="/">`, `<a href='/goback?at=1'>`, `<form action="/api/x">`,
		`fetch('/api/search?'+p)`, "fetch(`/api/facets`)", `fetch("/api/x")`, `template.URL("/files/" + x)`,
		`location.href='/x'`, `location = "/"`, `<IMG SRC="/x.png">`,
	}
	for _, s := range bad {
		if got := SameOriginAbsoluteURLs(s); len(got) == 0 {
			t.Errorf("%q: not flagged", s)
		}
	}
	good := []string{
		`<script src="app.js"></script>`, `<a href="./">`, `<a href="goback?at=1">`, `<a href="#sec">`,
		`<a href="https://microsoft.com/devicelogin">`, `<a href="//cdn.example/x">`, `fetch('api/search?'+p)`,
		`template.URL("files/" + x)`, `location.reload()`, `<form action="api/x">`,
	}
	for _, s := range good {
		if got := SameOriginAbsoluteURLs(s); len(got) != 0 {
			t.Errorf("%q: flagged %q", s, got)
		}
	}
}
