package graph

import (
	"crypto/sha256"
	"fmt"
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
)

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
