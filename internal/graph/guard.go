package graph

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// ListingKind names a paging loop's guard stop.
type ListingKind string

const (
	ListingRepeat      ListingKind = "repeat"       // a nextLink the loop has already fetched
	ListingPageCap     ListingKind = "page-cap"     // a page past the loop's page cap
	ListingForeignHost ListingKind = "foreign-host" // a nextLink off the client's base scheme, host, port or path
	ListingPageSize    ListingKind = "page-size"    // one page whose body runs past maxPageBytes
)

// maxPageBytes bounds one JSON page's body. A listing page is at most a few
// hundred kilobytes in practice (1000 message refs with their selected fields);
// a page that streams past this is an endpoint that does not end, and decoding
// it would grow the heap until the request timeout.
const maxPageBytes = 64 << 20

// ListingError is a paging loop's guard stop (fault-isolation §4.8.7). Every
// stop ends its loop with this error, never as a quiet end of listing: a quiet
// end would let capture mark the folder walked and sweep every record past the
// stop gone (H2). It names the stop and its value, never the raw link.
type ListingError struct {
	Kind  ListingKind
	Limit int    // the page cap, for ListingPageCap
	Host  string // the foreign host, for ListingForeignHost
}

func (e *ListingError) Error() string {
	switch e.Kind {
	case ListingRepeat:
		return "graph listing did not terminate: it pointed back at a page it had already fetched"
	case ListingPageCap:
		return fmt.Sprintf("graph listing did not terminate: it ran past %d pages (the listing's page limit)", e.Limit)
	case ListingPageSize:
		return fmt.Sprintf("graph listing did not terminate: one page ran past %d bytes (the listing's page size limit)", e.Limit)
	default:
		return fmt.Sprintf("graph listing left Microsoft Graph: its next page is on %q, and no request was sent there", e.Host)
	}
}

// pageGuard is the nextLink guard of one paging loop (fault-isolation §4.8.7,
// ruling J4): the set of links the loop has fetched, its page cap and, on the
// extraction workspace's loops only (from its E1), a host/path pin. Capture's
// Folders and Messages carry no pin until fault-isolation step SGc (XC25's
// gate; ESC-7). A guard belongs to one loop, so the cap counts that loop's
// pages, never the client's.
type pageGuard struct {
	seen  map[[sha256.Size]byte]bool
	pages int
	cap   int
	pin   *url.URL // nil: no host/path pin
}

// newPageGuard is the guard for one new paging loop; pinned pins its links to
// the client's base scheme, host, port and path.
func (c *Client) newPageGuard(pinned bool) *pageGuard {
	g := &pageGuard{seen: map[[sha256.Size]byte]bool{}, cap: c.pageCap}
	if pinned {
		g.pin, _ = url.Parse(c.base)
	}
	return g
}

// admit is called before the loop fetches link. It refuses a link off the pin,
// a link the loop has already fetched and a page past the cap, each with a
// typed *ListingError, and nothing is sent for a refused link. The repeat set
// keeps each link's digest, so a hostile run of long links costs little memory.
func (g *pageGuard) admit(link string) error {
	if g.pin != nil {
		if host, ok := onBase(g.pin, link); !ok {
			return &ListingError{Kind: ListingForeignHost, Host: host}
		}
	}
	h := sha256.Sum256([]byte(link))
	if g.seen[h] {
		return &ListingError{Kind: ListingRepeat}
	}
	if g.pages >= g.cap {
		return &ListingError{Kind: ListingPageCap, Limit: g.cap}
	}
	g.seen[h] = true
	g.pages++
	return nil
}

// forget undoes admit for a link whose request failed, so the loop's own retry of
// that page is not read as a repeat: the guard counts a page once it has been
// fetched. A link that was fetched stays in the set.
func (g *pageGuard) forget(link string) {
	h := sha256.Sum256([]byte(link))
	if g.seen[h] {
		delete(g.seen, h)
		g.pages--
	}
}

// onBase reports whether link stays on base, compared after url.Parse with the
// extraction review's XC25 normalization: the same scheme; the same host,
// case-insensitively; the same port once each scheme's default is filled in;
// a path under base's path on a segment boundary, dot segments resolved first.
// It also returns the host link names (bounded), for the refusal.
func onBase(base *url.URL, link string) (host string, ok bool) {
	u, err := url.Parse(link)
	if err != nil {
		return "an unreadable nextLink", false
	}
	host = u.Hostname()
	if len(host) > 253 {
		host = host[:253]
	}
	if u.Scheme != base.Scheme || !strings.EqualFold(u.Hostname(), base.Hostname()) || portOf(u) != portOf(base) {
		return host, false
	}
	prefix := strings.TrimRight(base.Path, "/")
	p := path.Clean("/" + u.Path)
	return host, prefix == "" || p == prefix || strings.HasPrefix(p, prefix+"/")
}

// maxFolderDepth is Folders' depth cap: Exchange Online's documented maximum
// folder hierarchy depth, 300 for every plan (confirmed 2026-10-09,
// fault-isolation §4.8.7). Levels count as Graph lists them: a folder from
// /mailFolders is level 1 and one in its childFolders level 2, so the cap is at
// or above the documented limit whether or not Exchange also counts the root or
// Top of Information Store.
const maxFolderDepth = 300

// DepthError is Folders' depth-cap stop (fault-isolation §4.8.7, RR6-C6): the
// mailbox's folder hierarchy goes deeper than the cap. The mailbox fails
// loudly, and no level is dropped silently.
type DepthError struct {
	Limit int
}

func (e *DepthError) Error() string {
	return fmt.Sprintf("graph folder hierarchy is deeper than %d levels (Exchange Online's maximum folder depth), so its walk stopped", e.Limit)
}

// maxRedirects is the number of redirects a request follows before the next one
// is refused: net/http's own limit of ten, which a CheckRedirect of our own
// replaces (extraction design §4.5, RR2b-C4).
const maxRedirects = 10

// RedirectError is the refusal of a redirect (extraction design XC26, RR2b-C4):
// the answer pointed off the host the request was sent to, or the request was
// redirected more than maxRedirects times. net/http returns it inside a
// *url.Error, so a caller finds it with errors.As; graph.Classify would read that
// as a network failure, which is why a caller matches this type first. It names
// the host the redirect pointed at, never the link, and the request that was
// refused was never sent: the bearer token did not leave.
type RedirectError struct {
	Host string // the host (and port) the redirect pointed at, bounded
	Hops int    // how many redirects the request had met, this one included
}

func (e *RedirectError) Error() string {
	if e.Hops > maxRedirects {
		return fmt.Sprintf("graph redirected the request more than %d times (to %q), so it was not followed any further", maxRedirects, e.Host)
	}
	return fmt.Sprintf("graph redirected the request to %q, which is not the host it was sent to; the redirect was not followed and no sign-in token was sent there", e.Host)
}

// refuseRedirect is the CheckRedirect of every client the package builds. A
// redirect is followed only to the origin (scheme, host and port, after the
// default-port normalization of XC25) its request was first sent to, at most
// maxRedirects times: oauth2's Transport adds the bearer token to every hop, so a
// redirect to another host would otherwise carry it there.
func refuseRedirect(req *http.Request, via []*http.Request) error {
	host := req.URL.Host
	if len(host) > 253 {
		host = host[:253]
	}
	first := via[0].URL
	if req.URL.Scheme != first.Scheme || !strings.EqualFold(req.URL.Hostname(), first.Hostname()) || portOf(req.URL) != portOf(first) {
		return &RedirectError{Host: host, Hops: len(via)}
	}
	if len(via) > maxRedirects {
		return &RedirectError{Host: host, Hops: len(via)}
	}
	return nil
}

// portOf is u's port, or its scheme's default.
func portOf(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch u.Scheme {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}
