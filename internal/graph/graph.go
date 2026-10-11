// Package graph is a minimal Microsoft Graph client for app-only (client-
// credentials) mail archiving: it enumerates a user's mail folders, lists the
// messages in each, and fetches each message's raw MIME. It issues only GET
// requests — the app is granted the read-only Mail.Read application permission —
// so it can never modify a mailbox. This is the server-side capture path for a
// tenant whose admin has consented a scoped app (see docs/graph-app-setup.md).
package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"mail-archive-tool/internal/util"
)

const defaultBaseURL = "https://graph.microsoft.com/v1.0"

// Config configures an app-only Graph client. BaseURL and TokenURL default to
// the Microsoft production endpoints; tests override them to a local server.
type Config struct {
	Tenant       string
	ClientID     string
	ClientSecret string
	BaseURL      string
	TokenURL     string

	// Deadlines (zero = defaults). An unattended job must never hang on a
	// stalled connection while holding the archive lock: every JSON request is
	// bounded by RequestTimeout (the token endpoint too, whose transport gives up
	// waiting for response headers after it), and capture's MIME download by
	// MIMETimeout, its overall cap, MIMEMaxDuration (2 h) since fault-isolation S3
	// (a deprecated alias for that cap, §4.8.6).
	RequestTimeout time.Duration
	MIMETimeout    time.Duration

	// How long get sleeps for a Retry-After (zero = the defaults): at most
	// RetryAfterMax each, and at most RetryAfterBudget in all for one request,
	// whatever the server asks for (fault-isolation §4.8.6, FC10), until F17 lands.
	// Without the clamp the 2 h overall cap would let a throttled mailbox hold the
	// archive lock for hours. Fixed by design; only tests scale them.
	RetryAfterMax    time.Duration
	RetryAfterBudget time.Duration

	// The $value download's idle window and its own response-header window
	// (zero = the defaults, 2 minutes each; the header window is never below the
	// idle one), and the page cap of one paging loop (zero = 100 000). They are
	// fixed by design (fault-isolation §4.8.5); only tests scale them.
	MIMEIdleTimeout   time.Duration
	MIMEHeaderTimeout time.Duration
	PageCap           int

	// WrapTransport, when set, wraps each base transport the client sends
	// through (the JSON requests' and token endpoint's, and the $value
	// download's): fault injection for tests. Production callers leave it nil.
	WrapTransport func(http.RoundTripper) http.RoundTripper

	// Delegated (per-user, device-code) fields — used only by NewDelegated.
	// DeviceAuthURL defaults to the Microsoft devicecode endpoint; tests override
	// it. TokenCachePath is where the per-user refresh/access token is persisted
	// (0600, under the caller's OS config dir). Unattended refuses an interactive
	// device prompt (a scheduled run has no console): with no usable cache it
	// fails naming the sign-in remedy rather than blocking. Prompt receives the
	// verification URI + user code on a first sign-in; nil prints to stderr.
	DeviceAuthURL  string
	TokenCachePath string
	Unattended     bool
	Prompt         func(DeviceAuth)

	// TokenStore, when set, persists the delegated token cache somewhere other
	// than a file — e.g. Windows Credential Manager — so a sign-in need not live
	// as a plaintext file (design-mailarchive-desktop P4/DC2). When nil the file
	// at TokenCachePath is used, exactly as before (the cross-platform default,
	// which the shipped device tests exercise).
	TokenStore TokenStore
}

// TokenStore persists the delegated token cache's JSON bytes. A missing entry is
// reported as errors.Is(err, os.ErrNotExist).
type TokenStore interface {
	LoadToken() ([]byte, error)
	StoreToken(data []byte) error
	ClearToken() error
}

const (
	defaultRequestTimeout    = 60 * time.Second
	defaultMIMETimeout       = MIMEMaxDuration
	defaultMIMEIdleTimeout   = 2 * time.Minute
	defaultMIMEHeaderTimeout = 2 * time.Minute
	defaultPageCap           = 100000
	defaultRetryAfterMax     = 60 * time.Second
	defaultRetryAfterBudget  = 5 * time.Minute
)

// MIMEMaxDuration is the shared overall cap on one $value download, Retry-After
// sleeps included: the copy export's from its slice E1, and capture's since
// fault-isolation S3, which raised it from the 10 minutes capture had until then
// (RR6-C2) in the same slice as the Retry-After clamp and the breakers, so that
// the 2 h never reaches a scheduled run without them.
const MIMEMaxDuration = 2 * time.Hour

// MIMEMaxBytes is the shared size cap on one $value body, confirmed against
// Microsoft's documentation on 2026-10-09 (fault-isolation §4.8.6). Every route
// the documentation bounds (send and receive, migration, PST import) holds an
// item to 150 MB, and Exchange's MB is binary, so the worst $value is 150 MiB of
// attachments as base64 with CRLF line breaks (×4/3 ×78/76, about 205.3 MiB)
// plus at most 256 KB of headers. The documentation bounds no item as stored (a
// draft built from upload-session attachments has no documented total), so the
// cap does not reach capture: Client.MIME passes none, as today (owner ruling
// RR6-OR1, ESC-8's safe interim), and the copy export carries this one from E1.
const MIMEMaxBytes = 256 << 20

// Client talks to Microsoft Graph with an auto-refreshing token — app-only
// (client-credentials) or delegated per-user (device-code, meMode).
type Client struct {
	base        string
	hc          *http.Client
	reqTimeout  time.Duration
	mimeTimeout time.Duration

	// mimeHC carries the $value request on its own base transport
	// (mimeTransport) under the same token source as hc; mimeIdle and
	// mimeHeader are MIMETo's idle and response-header windows (§4.8.6).
	mimeHC     *http.Client
	tokens     oauth2.TokenSource // the one caching source both clients share; MIMETo fetches the token through it before it arms a timer
	mimeIdle   time.Duration
	mimeHeader time.Duration

	// retryMax and retryBudget clamp get's Retry-After sleeps: each, and in all
	// for one request (§4.8.6).
	retryMax, retryBudget time.Duration

	// pageCap bounds each paging loop's pages (pageGuard); each loop counts its
	// own.
	pageCap int

	// meMode addresses the signed-in user's OWN mailbox at /me instead of
	// /users/{upn}: it is set by the delegated (device-code) constructor, where
	// the token is a per-user delegated Mail.Read grant that can reach no other
	// mailbox (design-graph-delegated OR3/P3).
	meMode bool
}

// userSeg is the mailbox path segment: /me for a delegated per-user client (the
// token reaches only the signer's own mailbox, so userID is ignored), or
// /users/{upn} for an app-only client addressing a named mailbox.
func (c *Client) userSeg(userID string) string {
	if c.meMode {
		return "/me"
	}
	return "/users/" + url.PathEscape(userID)
}

// New builds a client whose HTTP transport injects (and refreshes) an app-only
// bearer token via the client-credentials grant.
func New(ctx context.Context, cfg Config) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	tokenURL := cfg.TokenURL
	if tokenURL == "" {
		tokenURL = "https://login.microsoftonline.com/" + cfg.Tenant + "/oauth2/v2.0/token"
	}
	cc := &clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     tokenURL,
		Scopes:       []string{"https://graph.microsoft.com/.default"},
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	b := cfg.settings()
	// The oauth2 client wraps this transport (the token request uses it too):
	// a server that accepts the connection and then goes silent is cut off.
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpTransport(b.req, b.wrap))
	return newClient(ctx, base, cc.TokenSource(ctx), b, false)
}

// settings are a client's deadlines and page cap, each resolved to its default
// when unset, and its tests' transport hook.
type settings struct {
	req, mime, idle, header time.Duration
	retryMax, retryBudget   time.Duration
	pageCap                 int
	wrap                    func(http.RoundTripper) http.RoundTripper
}

func (cfg Config) settings() settings {
	b := settings{req: cfg.RequestTimeout, mime: cfg.MIMETimeout, idle: cfg.MIMEIdleTimeout, header: cfg.MIMEHeaderTimeout,
		retryMax: cfg.RetryAfterMax, retryBudget: cfg.RetryAfterBudget, pageCap: cfg.PageCap, wrap: cfg.WrapTransport}
	if b.retryMax <= 0 {
		b.retryMax = defaultRetryAfterMax
	}
	if b.retryBudget <= 0 {
		b.retryBudget = defaultRetryAfterBudget
	}
	if b.req <= 0 {
		b.req = defaultRequestTimeout
	}
	if b.mime <= 0 {
		b.mime = defaultMIMETimeout
	}
	if b.idle <= 0 {
		b.idle = defaultMIMEIdleTimeout
	}
	if b.header <= 0 {
		b.header = defaultMIMEHeaderTimeout
	}
	if b.pageCap <= 0 {
		b.pageCap = defaultPageCap
	}
	return b
}

// newClient builds a Client on base whose requests carry src's token. JSON
// requests go through ctx's oauth2 HTTP client, the request-timeout-bounded
// transport the token endpoint shares; the $value request goes through its own
// base transport (mimeTransport), under the same token source.
func newClient(ctx context.Context, base string, src oauth2.TokenSource, b settings, meMode bool) *Client {
	src = &refreshSource{inner: src}
	// The $value client is built here, over its own base transport and the
	// shared token source, never by oauth2.NewClient from a context that carries
	// httpTransport's client: NewClient copies that client's Timeout (RR2b-C3),
	// which would cut every download longer than the JSON request timeout. It
	// takes the redirect refusal (XC26) and no Timeout.
	mime := mimeTransport(b.wrap)
	return &Client{
		base:        strings.TrimRight(base, "/"),
		hc:          clientOver(ctx, src),
		reqTimeout:  b.req,
		mimeTimeout: b.mime,
		mimeHC: &http.Client{
			Transport:     &oauth2.Transport{Base: mime.Transport, Source: src},
			CheckRedirect: mime.CheckRedirect,
		},
		tokens:      src,
		mimeIdle:    b.idle,
		mimeHeader:  b.header,
		retryMax:    b.retryMax,
		retryBudget: b.retryBudget,
		pageCap:     b.pageCap,
		meMode:      meMode,
	}
}

// clientOver is oauth2.NewClient(ctx, src) without the ReuseTokenSource it wraps
// around src: a second token cache that ForceRefresh could not reach. It takes
// what NewClient takes from the context's client (its transport, CheckRedirect,
// Jar and Timeout: oauth2.go:357-366, RR2b-C3), so the JSON clients carry the
// redirect refusal and the request timeout exactly as before.
func clientOver(ctx context.Context, src oauth2.TokenSource) *http.Client {
	base, _ := ctx.Value(oauth2.HTTPClient).(*http.Client)
	if base == nil {
		base = http.DefaultClient
	}
	return &http.Client{
		Transport:     &oauth2.Transport{Base: base.Transport, Source: src},
		CheckRedirect: base.CheckRedirect,
		Jar:           base.Jar,
		Timeout:       base.Timeout,
	}
}

// httpTransport builds the deadline-bounded HTTP client the oauth2 layer wraps for
// both the token exchange and the Graph API calls: a server that accepts the
// connection and then goes silent is cut off after reqTimeout (app-only and
// delegated share it, so device sign-in is bounded too — R17/MA-98, D-C5). A
// test's wrap, when set, wraps its transport.
//
// The client also carries the two bounds of the extraction design's XC26: a
// CheckRedirect that refuses any redirect off the host the request was sent to
// (oauth2's Transport adds the bearer token on every hop), and Timeout:
// reqTimeout, the F15 fix, which bounds a token fetch's whole exchange, its body
// included, and so the deviceSource mutex hold it runs under. oauth2.NewClient
// copies both into every Graph client built over this one (oauth2.go:357-366),
// so the Timeout bounds every request through them as well, which changes
// nothing for the JSON requests, each of which already runs under a reqTimeout
// context.
func httpTransport(reqTimeout time.Duration, wrap func(http.RoundTripper) http.RoundTripper) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = reqTimeout
	transport.TLSHandshakeTimeout = 30 * time.Second
	return &http.Client{Transport: wrapped(transport, wrap), CheckRedirect: refuseRedirect, Timeout: reqTimeout}
}

// mimeTransport is the $value request's own base transport (fault-isolation
// §4.8.6). The shared transport's ResponseHeaderTimeout is the JSON request
// timeout (60 s), which would fail a large conversion before its first byte on
// every run, and a context timer can only shorten it; this one sets none, and
// MIMETo runs its own header timer instead. It sets no Client.Timeout either,
// which would bound the whole download. It does refuse a redirect off the host
// (XC26, MA-528): the bearer token is added on every hop.
func mimeTransport(wrap func(http.RoundTripper) http.RoundTripper) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSHandshakeTimeout = 30 * time.Second
	return &http.Client{Transport: wrapped(transport, wrap), CheckRedirect: refuseRedirect}
}

func wrapped(t http.RoundTripper, wrap func(http.RoundTripper) http.RoundTripper) http.RoundTripper {
	if wrap == nil {
		return t
	}
	return wrap(t)
}

// Folder is a mail folder with its full, sanitized path (root → leaf).
type Folder struct {
	ID   string
	Path []string
}

// Well-known folder names Graph resolves in place of a concrete folder id. Trash
// and Junk are excluded from a walk by default (T7, operator ruling); resolution
// is by these stable, locale-independent names — never a display-name path,
// which is locale-fragile (§3.6).
const (
	WellKnownDeletedItems = "deletedItems"
	WellKnownJunkEmail    = "junkemail"
)

// FolderFilter selects which by-default-excluded well-known folders a walk keeps.
// Deleted Items and Junk Email are excluded by default (T7): the walk resolves
// each to its concrete folder ID and skips that folder AND its whole subtree — an
// id-based skip, so it holds whatever the mailbox's display language is. Setting
// a field true keeps that folder (and its subtree) in the walk.
type FolderFilter struct {
	IncludeDeleted bool // include the Deleted Items subtree (default: excluded)
	IncludeJunk    bool // include the Junk Email subtree (default: excluded)
}

// Folders returns every mail folder for userID (a UPN or object id), recursing
// through child folders and carrying each folder's display-name path. The
// well-known folders the filter excludes (Deleted Items and Junk Email by
// default) are resolved to their concrete folder IDs and skipped WITH their
// subtrees, so the exclusion is locale-independent (T7/§3.6). A tenant that
// lacks an excluded folder (its resolution 404s) simply has nothing to skip.
func (c *Client) Folders(ctx context.Context, userID string, filter FolderFilter) ([]Folder, error) {
	excluded := map[string]bool{}
	exclude := func(wellKnown string) error {
		id, err := c.WellKnownFolderID(ctx, userID, wellKnown)
		if err != nil {
			return err
		}
		if id != "" {
			excluded[id] = true
		}
		return nil
	}
	if !filter.IncludeDeleted {
		if err := exclude(WellKnownDeletedItems); err != nil {
			return nil, err
		}
	}
	if !filter.IncludeJunk {
		if err := exclude(WellKnownJunkEmail); err != nil {
			return nil, err
		}
	}

	var out []Folder
	// One visited folder-id set across the whole call (fault-isolation §4.8.7,
	// RR6-C6): the client builds each level's first URL, so a cyclic or endless
	// childFolders response never repeats a nextLink. A folder already visited
	// is neither listed nor expanded again, so a cycle ends; a revisit is
	// skipped, never fatal, because a live mailbox lists a folder twice when it
	// moves during the walk. level counts as Graph lists folders (/mailFolders is
	// level 1), and a fresh folder past maxFolderDepth stops the walk, typed.
	visited := map[string]bool{}
	var walk func(parentID string, prefix []string, level int) error
	walk = func(parentID string, prefix []string, level int) error {
		path := c.userSeg(userID) + "/mailFolders"
		if parentID != "" {
			path = c.userSeg(userID) + "/mailFolders/" + url.PathEscape(parentID) + "/childFolders"
		}
		next := c.base + path + "?$select=id,displayName,childFolderCount&$top=100"
		// Each level is a paging loop of its own, with its own nextLink guard
		// (the repeat set and the page cap; no host/path pin until step SGc).
		guard := c.newPageGuard(false)
		for next != "" {
			if err := guard.admit(next); err != nil {
				return err
			}
			var body struct {
				Value *[]struct {
					ID               string `json:"id"`
					DisplayName      string `json:"displayName"`
					ChildFolderCount int    `json:"childFolderCount"`
				} `json:"value"`
				Next string `json:"@odata.nextLink"`
			}
			if _, err := c.getJSON(ctx, next, &body); err != nil {
				return err
			}
			// A page with no value array is no page of folders (S3-AO1); see Messages.
			if body.Value == nil {
				return &ListingError{Kind: ListingMalformed}
			}
			for _, f := range *body.Value {
				// An excluded well-known folder (Deleted Items / Junk Email) and its
				// entire subtree are dropped by resolved ID before any message in
				// them is listed, so nothing under them is walked (T7). Never a
				// display-name match, which would break under a non-English mailbox.
				if excluded[f.ID] || visited[f.ID] {
					continue
				}
				if level > maxFolderDepth {
					return &DepthError{Limit: maxFolderDepth}
				}
				visited[f.ID] = true
				fp := append(append([]string{}, prefix...), util.SanitizeSegment(f.DisplayName))
				out = append(out, Folder{ID: f.ID, Path: fp})
				if f.ChildFolderCount > 0 {
					if err := walk(f.ID, fp, level+1); err != nil {
						return err
					}
				}
			}
			next = body.Next
		}
		return nil
	}
	if err := walk("", nil, 1); err != nil {
		return nil, err
	}
	// A mailbox always has folders (its Inbox, at least): a folder list with none
	// would end the walk at once with every record unobserved and nothing wrong
	// said, so it is a malformed answer, not an empty mailbox (S3-AO1). A mailbox
	// whose only folders are the excluded ones cannot exist.
	if len(out) == 0 {
		return nil, &ListingError{Kind: ListingMalformed}
	}
	return out, nil
}

// WellKnownFolderID resolves a Graph well-known folder name (e.g. "deletedItems",
// "junkemail") to its concrete, locale-independent folder ID via the well-known-
// folder endpoint. A mailbox that lacks the folder yields ("", nil) — Graph
// answers 404 — so a caller treats "no such folder" as "nothing to exclude"
// rather than a run-ending error; any other HTTP failure is returned. GET-only,
// so it keeps the client read-only (R17).
func (c *Client) WellKnownFolderID(ctx context.Context, userID, wellKnown string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()
	u := c.base + c.userSeg(userID) + "/mailFolders/" + url.PathEscape(wellKnown) + "?$select=id"
	resp, err := c.get(ctx, u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", statusErr(resp)
	}
	var body struct {
		ID string `json:"id"`
	}
	// Bounded like every other Graph JSON body (XC26, MA-528).
	page := &io.LimitedReader{R: resp.Body, N: maxPageBytes + 1}
	err = json.NewDecoder(page).Decode(&body)
	if page.N <= 0 {
		return "", &ListingError{Kind: ListingPageSize, Limit: maxPageBytes}
	}
	if err != nil {
		return "", err
	}
	return body.ID, nil
}

// MessageRef is the lightweight per-message metadata used to decide whether a
// message needs fetching (its InternetMessageID drives incremental dedup without
// downloading the body). The envelope scalars (Subject/From/To/Cc/Received/
// HasAttachments) ride along on the SAME widened listing request so an envelope
// signature is computable BEFORE download (§3.2): a mailbox-wide id hit is
// confirmed as the same already-archived message — and skipped without a body
// fetch — only when that signature matches an archived sibling's stored
// fingerprint (R17), while a mismatch (a distinct message reusing the id) is
// downloaded and split. The message-state scalars (importance/isRead/
// sensitivity/categories) ride along too — no extra round-trip — and the caller
// maps them onto the message after parsing its MIME.
type MessageRef struct {
	ID                string
	InternetMessageID string // angle brackets stripped, matching go-message
	Received          time.Time

	// PhysID is the message's per-PHYSICAL-message immutable id when the run
	// obtained one (Prefer: IdType="ImmutableId" honored on the listing, so ID is
	// itself the immutable id) — empty otherwise. The live path uses it to tell a
	// distinct reuse of a Message-ID apart before download (closure rev-6); when
	// empty the path degrades to Message-ID membership (the shipped floor).
	PhysID string

	// Envelope fields for the pre-download signature (§3.2). To/Cc are the bare
	// e-mail addresses (display names dropped) so the signature is independent of
	// header formatting; HasAttachments is Graph's boolean (attachment NAMES are
	// not in the listing, so the signature uses only presence).
	Subject        string
	From           string // sender e-mail address; "" when the tenant omits it
	To             []string
	Cc             []string
	HasAttachments bool

	// Set by the extraction workspace's MessagePage only (Messages leaves them
	// zero): the sender's display name (raw, untrusted), the folder the message is
	// in as Graph says (parentFolderId), and the message's size in bytes as
	// PidTagMessageSize, best-effort — -1 when Graph did not return it.
	FromName       string
	ParentFolderID string
	SizeHint       int64

	Importance string   // Graph "low"/"normal"/"high"; "" when the tenant omits it
	IsRead     *bool    // nil when the tenant omits it (read state then unknown)
	Categories []string // the message's category tags; nil when the tenant omits them
	// Sensitivity is "personal"/"private"/"confidential" (model form), decoded from
	// the MAPI PidTagSensitivity extended property carried by sensitivityExpand —
	// Graph does NOT expose sensitivity on $select (MA-284). "" means Normal/absent,
	// OR that the sensitivity $expand was unsupported here (best-effort, MA-285).
	Sensitivity string
}

// graphRecipient is Graph's {emailAddress:{name,address}} recipient shape.
type graphRecipient struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

// addrsOf pulls the bare e-mail addresses out of a recipient list.
func addrsOf(rs []graphRecipient) []string {
	if len(rs) == 0 {
		return nil
	}
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		if a := strings.TrimSpace(r.EmailAddress.Address); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// messageSelectFields is the $select for the message listing (§3.2): the envelope
// fields the pre-download signature needs plus the message-state fields, on one
// request. EVERY entry MUST be a property of microsoft.graph.message that is
// selectable on the v1.0 endpoint (messageSelectableV1) — a non-schema property
// makes real Graph reject the WHOLE page with 400 "Could not find a property named
// X". 'sensitivity' is intentionally ABSENT: Graph's message resource does not
// expose it on $select (it lives only in the MAPI property PidTagSensitivity,
// "Integer 0x0036", reachable via $expand=singleValueExtendedProperties), so
// selecting it 400s on a real tenant; Graph-sourced messages carry no sensitivity.
var messageSelectFields = []string{
	"id", "internetMessageId", "subject", "from", "toRecipients", "ccRecipients",
	"receivedDateTime", "hasAttachments", "importance", "isRead", "categories",
}

// messageSelectableV1 is the set of $select-able properties of
// microsoft.graph.message on graph.microsoft.com/v1.0, from the live CSDL
// $metadata (the entity → outlookItem → message chain). It is the schema our
// $select is checked against (TestMessageSelectWithinSchema), so a non-v1.0
// property — e.g. the MAPI-only 'sensitivity' a real tenant rejects — fails CI at
// the source rather than only against a live mailbox (the fake server does not
// validate $select).
var messageSelectableV1 = map[string]bool{
	"id": true, "bccRecipients": true, "body": true, "bodyPreview": true,
	"ccRecipients": true, "categories": true, "changeKey": true,
	"conversationId": true, "conversationIndex": true, "createdDateTime": true,
	"flag": true, "from": true, "hasAttachments": true, "importance": true,
	"inferenceClassification": true, "internetMessageHeaders": true,
	"internetMessageId": true, "isDeliveryReceiptRequested": true, "isDraft": true,
	"isRead": true, "isReadReceiptRequested": true, "lastModifiedDateTime": true,
	"parentFolderId": true, "receivedDateTime": true, "replyTo": true,
	"sender": true, "sentDateTime": true, "subject": true, "toRecipients": true,
	"uniqueBody": true, "webLink": true,
}

// sensitivityExpand carries the message's MAPI PidTagSensitivity (canonical id
// 0x0036, PtypInteger32) alongside the listing — the only way Graph exposes
// sensitivity on a message (it is not $select-able, MA-284). Spaces are
// percent-encoded; the parens and quotes are the literal OData the service wants.
// It is BEST-EFFORT: a tenant/endpoint that rejects the $expand is handled by the
// first-page fallback in Messages, so capture never fails for it (MA-285).
const sensitivityExpand = "singleValueExtendedProperties($filter=id%20eq%20'Integer%200x0036')"

// singleValueExtProp is one Graph single-value extended property {id, value}.
type singleValueExtProp struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// sensitivityFromExtProps decodes the message sensitivity (model form) from the
// PidTagSensitivity single-value extended property requested by sensitivityExpand.
// We ask for exactly that one property, so the slice holds 0 or 1 entries; the
// value is the MAPI integer 0=Normal/1=Personal/2=Private/3=Confidential (matched
// regardless of how the service spells the returned id). Absent/0/garbage → ""
// (Normal), which is also what a tenant that doesn't support the $expand yields.
func sensitivityFromExtProps(props []singleValueExtProp) string {
	for _, p := range props {
		switch strings.TrimSpace(p.Value) {
		case "1":
			return "personal"
		case "2":
			return "private"
		case "3":
			return "confidential"
		}
	}
	return ""
}

// Messages streams every message reference in folderID to fn (paged). The
// $select is widened to carry the envelope fields (subject, from, toRecipients,
// ccRecipients, receivedDateTime, hasAttachments) the pre-download signature
// needs, alongside the message-state fields — all on one request (§3.2). It also
// $expands the PidTagSensitivity extended property (sensitivityExpand) to recover
// sensitivity (MA-285), best-effort: if the first page is rejected with the expand,
// it retries that page WITHOUT it so capture always completes.
func (c *Client) Messages(ctx context.Context, userID, folderID string, fn func(MessageRef) error) error {
	sel := strings.Join(messageSelectFields, ",")
	msgs := c.base + c.userSeg(userID) + "/mailFolders/" + url.PathEscape(folderID) + "/messages"
	next := msgs + "?$select=" + sel + "&$expand=" + sensitivityExpand + "&$top=1000"
	honored, firstPage := false, true // whether Prefer: IdType="ImmutableId" is in effect this run
	// The listing's nextLink guard: the repeat set and the page cap; capture's
	// loops get no host/path pin until fault-isolation step SGc (XC25's gate).
	guard := c.newPageGuard(false)
	for next != "" {
		if err := guard.admit(next); err != nil {
			return err
		}
		// Value is a pointer so a page with no value array (absent or null) is told
		// from an empty one: a real page always carries the array.
		var body struct {
			Value *[]struct {
				ID                string               `json:"id"`
				InternetMessageID string               `json:"internetMessageId"`
				Subject           string               `json:"subject"`
				From              *graphRecipient      `json:"from"`
				ToRecipients      []graphRecipient     `json:"toRecipients"`
				CcRecipients      []graphRecipient     `json:"ccRecipients"`
				Received          string               `json:"receivedDateTime"`
				HasAttachments    bool                 `json:"hasAttachments"`
				Importance        string               `json:"importance"`
				IsRead            *bool                `json:"isRead"`
				Categories        []string             `json:"categories"`
				ExtProps          []singleValueExtProp `json:"singleValueExtendedProperties"`
			} `json:"value"`
			Next string `json:"@odata.nextLink"`
		}
		pref, err := c.getJSON(ctx, next, &body)
		if err != nil {
			// Best-effort sensitivity (MA-285): a tenant/endpoint that does not
			// support the singleValueExtendedProperties $expand must NOT fail the
			// capture. On the first page (the one we build), retry once WITHOUT the
			// $expand; sensitivity is then simply not captured. A real error (auth,
			// network, a genuinely bad folder) fails the no-$expand retry too and so
			// still surfaces. Later pages follow server nextLinks that consistently
			// carry — or omit — the $expand the first page settled on.
			if firstPage && strings.Contains(next, "$expand=") {
				next = msgs + "?$select=" + sel + "&$top=1000"
				continue
			}
			return err
		}
		// A 2xx page with no value array ({}, null, an error object: a proxy, a
		// captive portal, an impersonator) is not an empty folder: reading it as one
		// would count the folder walked and let the sweep mark every record in it
		// gone. It ends the loop as a typed stop, a folder-list-failed gap (S3-AO1).
		if body.Value == nil {
			return &ListingError{Kind: ListingMalformed}
		}
		if firstPage {
			// Namespace-once (rev-6 §2): the first page's Preference-Applied says
			// whether the tenant honored the immutable-id preference. When honored,
			// each message id IS its immutable id and becomes PhysID; otherwise
			// PhysID stays empty and the live path degrades to the Message-ID floor.
			honored = strings.Contains(strings.ToLower(pref), "immutableid")
			firstPage = false
		}
		for _, m := range *body.Value {
			ref := MessageRef{
				ID:                m.ID,
				InternetMessageID: strings.Trim(m.InternetMessageID, "<>"),
				Subject:           m.Subject,
				To:                addrsOf(m.ToRecipients),
				Cc:                addrsOf(m.CcRecipients),
				HasAttachments:    m.HasAttachments,
				Importance:        m.Importance,
				IsRead:            m.IsRead,
				Categories:        m.Categories,
				Sensitivity:       sensitivityFromExtProps(m.ExtProps),
			}
			if m.From != nil {
				ref.From = strings.TrimSpace(m.From.EmailAddress.Address)
			}
			if t, err := time.Parse(time.RFC3339, m.Received); err == nil {
				ref.Received = t
			}
			if honored {
				ref.PhysID = m.ID // the immutable id (rev-6): the per-physical-message hint
			}
			if err := fn(ref); err != nil {
				return err
			}
		}
		next = body.Next
	}
	return nil
}

// MIME fetches a message's raw RFC 5322 bytes (including attachments) via
// /messages/{id}/$value, ready for the shared parser. It is capture's MIMETo:
// the client's idle window, Config.MIMETimeout (MIMEMaxDuration by default) as
// the overall cap, and no size cap (owner ruling RR6-OR1's safe interim). On any
// error it returns no bytes.
func (c *Client) MIME(ctx context.Context, userID, messageID string) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := c.MIMETo(ctx, userID, messageID, &buf, 0, c.mimeIdle, c.mimeTimeout); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ProbeFolder is capture's control request for its breaker (fault-isolation
// §4.8.5, FC10): GET /mailFolders/{id}?$select=id, a request unrelated to the
// failing items, sent once with no retry loop and bounded by the request timeout.
// It is never sent through get, whose Retry-After sleep would turn a throttle into
// a wait: a 429 or a 503 comes back at once as the typed *StatusError, and a
// server that does not answer as the transport's error. nil is a 2xx: the service
// answers, so what fails is the items.
func (c *Client) ProbeFolder(ctx context.Context, userID, folderID string) error {
	ctx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+c.userSeg(userID)+"/mailFolders/"+url.PathEscape(folderID)+"?$select=id", nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return statusErr(resp)
	}
	return nil
}

// TimeoutKind names which of MIMETo's own timers stopped a download.
type TimeoutKind string

const (
	TimeoutIdle    TimeoutKind = "idle"    // no body byte within the idle window
	TimeoutHeader  TimeoutKind = "header"  // no response headers within the $value header window
	TimeoutOverall TimeoutKind = "overall" // the overall cap on the whole call, Retry-After sleeps included
)

// TimeoutError is one of MIMETo's own stops. It names the message and the limit
// that fired (MA-98, X2), and it never matches context.Canceled or
// context.DeadlineExceeded: a stalled download is that item's failure, never
// read as an operator's interrupt (fault-isolation §4.8.6, RR6-C1).
type TimeoutError struct {
	Kind      TimeoutKind
	Limit     time.Duration
	MessageID string
}

func (e *TimeoutError) Error() string {
	switch e.Kind {
	case TimeoutIdle:
		return fmt.Sprintf("download message %s: no data arrived for %s (the download's idle limit)", e.MessageID, e.Limit)
	case TimeoutHeader:
		return fmt.Sprintf("download message %s: Microsoft 365 did not answer within %s (the download's response limit)", e.MessageID, e.Limit)
	default:
		return fmt.Sprintf("download message %s: not finished within %s (the download's overall limit)", e.MessageID, e.Limit)
	}
}

// TooLargeError is MIMETo's size-cap stop: the body is larger than the cap its
// caller passed. It names the message and the cap, and the body is never
// truncated into a success. Declared is the response's Content-Length (-1 when it
// sent none), for the early refusal and for a body that streams past the limit
// alike: a caller that passes a limit below the shared cap (the copy export's
// free-space budget) tells an item above the cap from its own budget by it
// (extraction design §4.13, RR2b-C8; fault-isolation §4.8.6).
type TooLargeError struct {
	MessageID string
	Limit     int64
	Declared  int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("download message %s: larger than the %d-byte size cap", e.MessageID, e.Limit)
}

// MIMETo streams a message's raw RFC 5322 bytes (/messages/{id}/$value) into w
// and returns how many it wrote. It is the one bounded download capture and the
// copy export share (fault-isolation §4.8.6), and it runs its bounds itself, on
// a child of ctx:
//   - idle: the body read stops when no byte arrives for idle (zero = the
//     client's MIMEIdleTimeout);
//   - header: each HTTP attempt must bring its response headers within the
//     client's MIMEHeaderTimeout, never less than idle; the timer starts again
//     on every attempt and never runs during get's Retry-After sleeps;
//   - overall: the whole call, those sleeps included, ends at overall (zero =
//     MIMEMaxDuration);
//   - maxBytes > 0: a body larger than maxBytes is refused, before it is read
//     when its Content-Length already says so; maxBytes <= 0 sets no cap.
//
// Its own stops return *TimeoutError and *TooLargeError, which never match
// context.Canceled or context.DeadlineExceeded, also when the overall cap fires
// inside a Retry-After sleep; when ctx itself is done it returns ctx's error.
// On any error w holds no message: the caller discards what it was given
// (Client.MIME returns nothing; the copy export removes its temp file).
func (c *Client) MIMETo(ctx context.Context, userID, messageID string, w io.Writer, maxBytes int64, idle, overall time.Duration) (int64, error) {
	if idle <= 0 {
		idle = c.mimeIdle
	}
	if overall <= 0 {
		overall = MIMEMaxDuration
	}
	dctx, halt := context.WithCancelCause(ctx)
	defer halt(nil)
	stopOn := func(kind TimeoutKind, limit time.Duration) func() {
		return func() { halt(&TimeoutError{Kind: kind, Limit: limit, MessageID: messageID}) }
	}
	overallTimer := time.AfterFunc(overall, stopOn(TimeoutOverall, overall))
	defer overallTimer.Stop()
	header := max(c.mimeHeader, idle)
	perAttempt := func() func() {
		t := time.AfterFunc(header, stopOn(TimeoutHeader, header))
		return func() { t.Stop() }
	}

	u := c.base + c.userSeg(userID) + "/messages/" + url.PathEscape(messageID) + "/$value"
	resp, err := c.getOn(dctx, c.mimeHC, u, perAttempt)
	if err != nil {
		return 0, mimeStop(ctx, dctx, err)
	}
	defer resp.Body.Close()
	body := &idleReader{r: resp.Body, idle: idle, timer: time.AfterFunc(idle, stopOn(TimeoutIdle, idle))}
	defer body.timer.Stop()
	if resp.StatusCode != http.StatusOK {
		// An error answer's body is under the idle watchdog too, and a read that a
		// timer cut short is that typed stop, not a *StatusError holding a cut text.
		se, rerr := statusErrFrom(resp, body)
		if rerr != nil {
			return 0, mimeStop(ctx, dctx, se)
		}
		return 0, se
	}
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return 0, &TooLargeError{MessageID: messageID, Limit: maxBytes, Declared: resp.ContentLength}
	}
	// A body whose end the framing does not mark (HTTP/1.x with neither a
	// Content-Length nor chunked encoding) ends where the connection closes, and
	// a clean close of a cut-short body reads as a complete one: net/http reports
	// EOF, not an error. Such an answer is refused as a dropped connection (a
	// transport fault), never kept as a message, whose record would be terminal
	// (S3-AO3). A gzip answer reports no length either, but its trailer marks the
	// end (a cut one is an unexpected EOF), and HTTP/2 frames its own end.
	if resp.ProtoMajor == 1 && resp.ContentLength < 0 && !resp.Uncompressed && !slices.Contains(resp.TransferEncoding, "chunked") {
		return 0, mimeStop(ctx, dctx, fmt.Errorf("download message %s: %w (the answer is neither length-delimited nor chunked, so a cut-short body cannot be told from a whole one)", messageID, io.ErrUnexpectedEOF))
	}
	var src io.Reader = body
	if maxBytes > 0 {
		src = io.LimitReader(body, maxBytes)
	}
	// The idle window times the server: it is paused while the destination writes.
	n, err := io.Copy(&pausingWriter{w: w, ir: body}, src)
	if err == nil && maxBytes > 0 && n == maxBytes {
		// The cap is reached; one more byte means the body is larger than it.
		var one [1]byte
		switch _, perr := io.ReadFull(body, one[:]); {
		case perr == nil:
			return n, &TooLargeError{MessageID: messageID, Limit: maxBytes, Declared: resp.ContentLength}
		case perr != io.EOF:
			err = perr
		}
	}
	if err != nil {
		return n, mimeStop(ctx, dctx, fmt.Errorf("download message %s: %w", messageID, err))
	}
	return n, nil
}

// mimeStop maps a failed download to MIMETo's error: the caller's own context
// error once ctx is done; MIMETo's typed stop when one of its timers fired (the
// cause of dctx), so the error never unwraps to the cancel that stop raised;
// otherwise err itself.
func mimeStop(ctx, dctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var te *TimeoutError
	if errors.As(context.Cause(dctx), &te) {
		return te
	}
	return err
}

// idleReader is MIMETo's idle watchdog: each read that returns data pushes the
// timer back by idle, and the timer firing stops the request. It runs only while
// a body is read.
type idleReader struct {
	r     io.Reader
	idle  time.Duration
	timer *time.Timer
}

func (ir *idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.timer.Reset(ir.idle)
	}
	return n, err
}

// pausingWriter stops the idle timer while the destination's Write runs and
// starts it again after, so a slow destination (a disk, a NAS) is never counted
// as a server that went quiet.
type pausingWriter struct {
	w  io.Writer
	ir *idleReader
}

func (p *pausingWriter) Write(b []byte) (int, error) {
	p.ir.timer.Stop()
	n, err := p.w.Write(b)
	p.ir.timer.Reset(p.ir.idle)
	return n, err
}

// getJSON decodes a GET's JSON body and returns the response's Preference-Applied
// header (empty when absent) so a caller can tell whether Prefer: IdType was
// honored this request.
func (c *Client) getJSON(ctx context.Context, u string, v any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()
	resp, err := c.get(ctx, u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", statusErr(resp)
	}
	// A page is bounded in size as well as time: an endpoint that streams a page
	// without end would otherwise grow the heap until the request timeout.
	page := &io.LimitedReader{R: resp.Body, N: maxPageBytes + 1}
	err = json.NewDecoder(page).Decode(v)
	if page.N <= 0 {
		return "", &ListingError{Kind: ListingPageSize, Limit: maxPageBytes}
	}
	return resp.Header.Get("Preference-Applied"), err
}

// get issues a GET with bounded retries that honour Graph throttling
// (429 / 503 + Retry-After). GET-only keeps the whole client read-only.
func (c *Client) get(ctx context.Context, u string) (*http.Response, error) {
	return c.getOn(ctx, c.hc, u, nil)
}

// getOn is get through hc. When perAttempt is set it is called as each attempt
// is sent and returns the func that stops that attempt's timer once its
// response headers arrive: MIMETo's header timer, which so times each attempt
// and never runs during a Retry-After sleep (fault-isolation §4.8.6).
func (c *Client) getOn(ctx context.Context, hc *http.Client, u string, perAttempt func() (disarm func())) (*http.Response, error) {
	const maxRetry = 5
	slept := time.Duration(0) // the Retry-After sleeps of this request so far (§4.8.6, FC10)
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		// Request immutable message ids (closure rev-6): when honored, a message's
		// `id` is its stable per-physical-message immutable id on BOTH the listing
		// and the /$value fetch, so the two agree. Harmless on non-message GETs.
		// Whether the tenant honored it is read from the listing's Preference-Applied.
		req.Header.Set("Prefer", `IdType="ImmutableId"`)
		disarm := func() {}
		if perAttempt != nil {
			// The token is fetched before the attempt's timer starts: oauth2's
			// transport asks its source without the request's context, so a refresh
			// would neither be cut by MIMETo's stops nor stay out of the header window.
			if err := c.warmToken(ctx, u); err != nil {
				return nil, err
			}
			disarm = perAttempt()
		}
		resp, err := hc.Do(req)
		disarm()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
			return resp, nil
		}
		// The server's Retry-After is clamped, each sleep and the sum of them for this
		// request: a service that asks for an hour must not hold an unattended run, and
		// the archive lock with it, for the 2 h overall cap (FC10). A request whose
		// budget is spent is answered as one that is still throttled.
		wait := min(retryAfter(resp), c.retryMax)
		resp.Body.Close()
		if attempt >= maxRetry || slept >= c.retryBudget {
			return nil, &throttledError{code: resp.StatusCode, retries: attempt}
		}
		wait = min(wait, c.retryBudget-slept)
		slept += wait
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// refreshSource caches its inner source's token until it is no longer valid, as
// oauth2.ReuseTokenSource does, and can be told to drop it: ForceRefresh, the copy
// export's one forced refresh after a 401 (extraction design §4.13, XC9). A cached
// token the server has stopped accepting (revoked, or clock skew) is otherwise
// reused until its own expiry.
type refreshSource struct {
	inner oauth2.TokenSource
	mu    sync.Mutex
	tok   *oauth2.Token
}

func (s *refreshSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tok.Valid() {
		return s.tok, nil
	}
	t, err := s.inner.Token()
	if err != nil {
		return nil, err
	}
	s.tok = t
	return t, nil
}

// force drops the cached token, and the inner source's own when it keeps one.
func (s *refreshSource) force() {
	s.mu.Lock()
	s.tok = nil
	s.mu.Unlock()
	if f, ok := s.inner.(interface{ forceRefresh() }); ok {
		f.forceRefresh()
	}
}

// ForceRefresh drops the client's cached access token and fetches a fresh one
// from the refresh token now. A nil result means the sign-in still refreshes; an
// error is the refresh's own (an *oauth2.RetrieveError inside the returned
// *url.Error when the token endpoint refused it), which the copy export reads as
// "sign-in refused" (R-c). It is a no-op for a source that keeps no cache.
func (c *Client) ForceRefresh(ctx context.Context) error {
	rs, ok := c.tokens.(*refreshSource)
	if !ok {
		return nil
	}
	rs.force()
	return c.warmToken(ctx, c.base)
}

// warmToken makes the client's token current before a request that runs its own
// timers, and gives up as soon as ctx is done (the refresh itself runs on, and
// its result is cached for the next request). A refresh error reads as the one
// http.Client.Do would return for it.
func (c *Client) warmToken(ctx context.Context, u string) error {
	if c.tokens == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.tokens.Token()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return &url.Error{Op: "Get", URL: u, Err: err}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// retryAfterCeil bounds the seconds retryAfter converts to a Duration: far above
// any clamp (a year), and far enough below the 9.2e9 seconds at which a Duration
// overflows int64 that a server cannot name a number that wraps negative and so
// waits for nothing (S3-AO6).
const retryAfterCeil = 366 * 24 * 60 * 60

func retryAfter(resp *http.Response) time.Duration {
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 0 {
			return time.Duration(min(n, retryAfterCeil)) * time.Second
		}
	}
	return 5 * time.Second
}

// StatusError is a Graph response whose status is not a success. Callers branch
// on it with errors.As (Code, and GraphCode for Graph's own error.code), never
// on its text; Error() keeps the text the client has always returned, which the
// last-run record, status's remedy needles and the CLI read (fault-isolation
// §4.8.1, MA-402).
type StatusError struct {
	Code      int    // the HTTP status code
	GraphCode string // the body's error.code; "" when the body carries none
	Text      string // the body, bounded to its first statusTextMax bytes and trimmed

	status string // the response's status line after the protocol ("401 Unauthorized"), as the server sent it
}

func (e *StatusError) Error() string {
	status := e.status
	if status == "" {
		status = strconv.Itoa(e.Code) + " " + http.StatusText(e.Code)
	}
	return "graph " + status + ": " + e.Text
}

// statusTextMax bounds the error body a StatusError keeps.
const statusTextMax = 2048

// statusLineMax bounds the server's status line a StatusError keeps; every real
// reason phrase is far shorter.
const statusLineMax = 128

// Class is graph.Classify's verdict on an error from the client: the one
// classifier capture and the copy export share (fault-isolation §4.8.1,
// extraction §4.13). Each consumer maps it onto its own outcomes — capture's
// app.classifyFetch (fault-isolation S3), the copy export's outcome table (its
// E2) — and keeps its own 404 class, 403 handling and breaker thresholds.
type Class int

const (
	ClassNone      Class = iota // no error
	ClassCancelled              // the caller's walk context is done
	ClassAuth                   // 401, or a failed token refresh
	ClassForbidden              // 403
	ClassNotFound               // 404, or Graph's ErrorItemNotFound
	ClassTransport              // 429, 502, 503 or 504 after get's retries, or no response at all
	ClassTimeout                // one of MIMETo's own timers (idle, header, overall)
	ClassTooLarge               // a body over the size cap its caller passed
	ClassFailed                 // any other response or error, 500 with any Graph code included
)

func (c Class) String() string {
	switch c {
	case ClassNone:
		return "none"
	case ClassCancelled:
		return "cancelled"
	case ClassAuth:
		return "auth"
	case ClassForbidden:
		return "forbidden"
	case ClassNotFound:
		return "not-found"
	case ClassTransport:
		return "transport"
	case ClassTimeout:
		return "timeout"
	case ClassTooLarge:
		return "too-large"
	}
	return "failed"
}

// Classify sorts an error from the client into its Class, and reports whether
// the fault is about the service rather than the item (transport: the faults a
// breaker counts). It reads the error's types through errors.As, never its
// text, in this order:
//  1. walkCtx is done: cancelled, whatever err says (a download's own timers
//     are not consulted);
//  2. a failed token refresh (*oauth2.RetrieveError) is auth, before the
//     network error it travels inside could make it transport;
//  3. a *StatusError by its code: 401 auth, 403 forbidden, 404 (or
//     ErrorItemNotFound) not found, 429/502/503/504 transport, any other one
//     failed;
//  4. MIMETo's stops: *TimeoutError is a timeout and transport, *TooLargeError
//     too large and not;
//  5. get's exhausted throttling retries, and no response at all (a request the
//     transport could not complete, a dropped connection), are transport;
//  6. anything else is failed.
func Classify(err error, walkCtx context.Context) (class Class, transport bool) {
	if err == nil {
		return ClassNone, false
	}
	if walkCtx != nil && walkCtx.Err() != nil {
		return ClassCancelled, false
	}
	var refresh *oauth2.RetrieveError
	if errors.As(err, &refresh) {
		return ClassAuth, false
	}
	var se *StatusError
	if errors.As(err, &se) {
		switch {
		case se.Code == http.StatusUnauthorized:
			return ClassAuth, false
		case se.Code == http.StatusForbidden:
			return ClassForbidden, false
		case se.Code == http.StatusNotFound || se.GraphCode == "ErrorItemNotFound":
			return ClassNotFound, false
		case se.Code == http.StatusTooManyRequests || se.Code == http.StatusBadGateway ||
			se.Code == http.StatusServiceUnavailable || se.Code == http.StatusGatewayTimeout:
			return ClassTransport, true
		}
		return ClassFailed, false
	}
	var te *TimeoutError
	if errors.As(err, &te) {
		return ClassTimeout, true
	}
	var tl *TooLargeError
	if errors.As(err, &tl) {
		return ClassTooLarge, false
	}
	var thr *throttledError
	var ue *url.Error
	var ne net.Error
	if errors.As(err, &thr) || errors.As(err, &ue) || errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ClassTransport, true
	}
	return ClassFailed, false
}

// throttledError is get's answer once Graph still throttles (429 or 503) after
// every retry; its text is the one the client has always returned.
type throttledError struct {
	code, retries int
}

func (e *throttledError) Error() string {
	return fmt.Sprintf("graph throttled (HTTP %d) after %d retries", e.code, e.retries)
}

// statusErr reads a failed response's bounded body into a *StatusError. Its
// GraphCode comes from the body's {"error":{"code":…}}; a body that is not that
// JSON (or is cut by the bound) leaves it empty.
func statusErr(resp *http.Response) error {
	e, _ := statusErrFrom(resp, resp.Body)
	return e
}

// statusErrFrom is statusErr reading the body from r, which may be a guarded
// reader of resp.Body. It also returns the error that cut the read short, if one
// did (a stop of MIMETo's timers, a dropped connection); the *StatusError holds
// what was read either way.
func statusErrFrom(resp *http.Response, r io.Reader) (*StatusError, error) {
	b, rerr := io.ReadAll(io.LimitReader(r, statusTextMax))
	return &StatusError{
		Code: resp.StatusCode, GraphCode: graphErrorCode(b), Text: strings.TrimSpace(string(b)),
		status: boundStatusLine(resp.Status),
	}, rerr
}

// boundStatusLine cuts the server's status line at statusLineMax bytes.
func boundStatusLine(s string) string {
	if len(s) > statusLineMax {
		return s[:statusLineMax]
	}
	return s
}

// graphErrorCode reads error.code from the start of a Graph error body, which
// may be cut by statusTextMax in the middle of a later field: the body is walked
// token by token, so the code survives a truncation after it. A body that is not
// {"error":{"code":"…"}} yields "".
func graphErrorCode(b []byte) string {
	dec := json.NewDecoder(bytes.NewReader(b))
	if !enterObject(dec) || !findKey(dec, "error") || !enterObject(dec) || !findKey(dec, "code") {
		return ""
	}
	if s, ok := stringToken(dec); ok {
		return s
	}
	return ""
}

func enterObject(dec *json.Decoder) bool {
	t, err := dec.Token()
	return err == nil && t == json.Delim('{')
}

// findKey advances to the value of key in the object dec is inside, skipping the
// values of the keys before it; false when the object ends or the input is cut.
func findKey(dec *json.Decoder, key string) bool {
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return false
		}
		if k, ok := t.(string); ok && strings.EqualFold(k, key) {
			return true
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return false
		}
	}
	return false
}

func stringToken(dec *json.Decoder) (string, bool) {
	t, err := dec.Token()
	s, ok := t.(string)
	return s, err == nil && ok
}
