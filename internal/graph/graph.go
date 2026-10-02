// Package graph is a minimal Microsoft Graph client for app-only (client-
// credentials) mail archiving: it enumerates a user's mail folders, lists the
// messages in each, and fetches each message's raw MIME. It issues only GET
// requests — the app is granted the read-only Mail.Read application permission —
// so it can never modify a mailbox. This is the server-side capture path for a
// tenant whose admin has consented a scoped app (see docs/graph-app-setup.md).
package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	// bounded by RequestTimeout and every MIME download by MIMETimeout, and the
	// transport gives up waiting for response headers after RequestTimeout.
	RequestTimeout time.Duration
	MIMETimeout    time.Duration

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
	defaultRequestTimeout = 60 * time.Second
	defaultMIMETimeout    = 10 * time.Minute
)

// Client talks to Microsoft Graph with an auto-refreshing token — app-only
// (client-credentials) or delegated per-user (device-code, meMode).
type Client struct {
	base        string
	hc          *http.Client
	reqTimeout  time.Duration
	mimeTimeout time.Duration

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
	reqTimeout, mimeTimeout := cfg.RequestTimeout, cfg.MIMETimeout
	if reqTimeout <= 0 {
		reqTimeout = defaultRequestTimeout
	}
	if mimeTimeout <= 0 {
		mimeTimeout = defaultMIMETimeout
	}
	// The oauth2 client wraps this transport (the token request uses it too):
	// a server that accepts the connection and then goes silent is cut off.
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpTransport(reqTimeout))
	return &Client{base: strings.TrimRight(base, "/"), hc: cc.Client(ctx), reqTimeout: reqTimeout, mimeTimeout: mimeTimeout}
}

// httpTransport builds the deadline-bounded HTTP client the oauth2 layer wraps for
// both the token exchange and the Graph API calls: a server that accepts the
// connection and then goes silent is cut off after reqTimeout (app-only and
// delegated share it, so device sign-in is bounded too — R17/MA-98, D-C5).
func httpTransport(reqTimeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = reqTimeout
	transport.TLSHandshakeTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
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
	var walk func(parentID string, prefix []string) error
	walk = func(parentID string, prefix []string) error {
		path := c.userSeg(userID) + "/mailFolders"
		if parentID != "" {
			path = c.userSeg(userID) + "/mailFolders/" + url.PathEscape(parentID) + "/childFolders"
		}
		next := c.base + path + "?$select=id,displayName,childFolderCount&$top=100"
		for next != "" {
			var body struct {
				Value []struct {
					ID               string `json:"id"`
					DisplayName      string `json:"displayName"`
					ChildFolderCount int    `json:"childFolderCount"`
				} `json:"value"`
				Next string `json:"@odata.nextLink"`
			}
			if _, err := c.getJSON(ctx, next, &body); err != nil {
				return err
			}
			for _, f := range body.Value {
				// An excluded well-known folder (Deleted Items / Junk Email) and its
				// entire subtree are dropped by resolved ID before any message in
				// them is listed, so nothing under them is walked (T7). Never a
				// display-name match, which would break under a non-English mailbox.
				if excluded[f.ID] {
					continue
				}
				fp := append(append([]string{}, prefix...), util.SanitizeSegment(f.DisplayName))
				out = append(out, Folder{ID: f.ID, Path: fp})
				if f.ChildFolderCount > 0 {
					if err := walk(f.ID, fp); err != nil {
						return err
					}
				}
			}
			next = body.Next
		}
		return nil
	}
	if err := walk("", nil); err != nil {
		return nil, err
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
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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
	for next != "" {
		var body struct {
			Value []struct {
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
		if firstPage {
			// Namespace-once (rev-6 §2): the first page's Preference-Applied says
			// whether the tenant honored the immutable-id preference. When honored,
			// each message id IS its immutable id and becomes PhysID; otherwise
			// PhysID stays empty and the live path degrades to the Message-ID floor.
			honored = strings.Contains(strings.ToLower(pref), "immutableid")
			firstPage = false
		}
		for _, m := range body.Value {
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
// /messages/{id}/$value, ready for the shared parser.
func (c *Client) MIME(ctx context.Context, userID, messageID string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.mimeTimeout)
	defer cancel()
	u := c.base + c.userSeg(userID) + "/messages/" + url.PathEscape(messageID) + "/$value"
	resp, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusErr(resp)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("download message %s: %w", messageID, err)
	}
	return data, nil
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
	return resp.Header.Get("Preference-Applied"), json.NewDecoder(resp.Body).Decode(v)
}

// get issues a GET with bounded retries that honour Graph throttling
// (429 / 503 + Retry-After). GET-only keeps the whole client read-only.
func (c *Client) get(ctx context.Context, u string) (*http.Response, error) {
	const maxRetry = 5
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
		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
			return resp, nil
		}
		wait := retryAfter(resp)
		resp.Body.Close()
		if attempt >= maxRetry {
			return nil, fmt.Errorf("graph throttled (HTTP %d) after %d retries", resp.StatusCode, attempt)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func retryAfter(resp *http.Response) time.Duration {
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 5 * time.Second
}

func statusErr(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("graph %s: %s", resp.Status, strings.TrimSpace(string(b)))
}
