package localwebtest

import "regexp"

// sameOriginAbsolute matches the ways a surface's HTML or script names a
// same-origin absolute URL, which resolves against the server's root instead of
// the session's /s/<P>/ prefix (design §4.C2, CC17): src, href or action set to a
// path that starts with a single slash, fetch of one, an assignment of one to
// location, and a template.URL built from one. A protocol-relative "//host" is a
// foreign origin, not this.
var sameOriginAbsolute = regexp.MustCompile(
	`(?i)\b(?:src|href|action)\s*=\s*["']/(?:["']|[^/"'])` +
		`|\bfetch\(\s*["'` + "`" + `]/(?:["'` + "`" + `]|[^/])` +
		`|\blocation(?:\.href)?\s*=\s*["']/(?:["']|[^/"'])` +
		`|template\.URL\("/`)

// SameOriginAbsoluteURLs returns each same-origin absolute URL in content (the
// served HTML or JS of a surface, or its Go source): the structural check that no
// page escapes the session prefix [CC17].
func SameOriginAbsoluteURLs(content string) []string {
	return sameOriginAbsolute.FindAllString(content, -1)
}
