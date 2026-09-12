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
}

const (
	defaultRequestTimeout = 60 * time.Second
	defaultMIMETimeout    = 10 * time.Minute
)

// Client talks to Microsoft Graph with an auto-refreshing app-only token.
type Client struct {
	base        string
	hc          *http.Client
	reqTimeout  time.Duration
	mimeTimeout time.Duration
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
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = reqTimeout
	transport.TLSHandshakeTimeout = 30 * time.Second
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Transport: transport})
	return &Client{base: strings.TrimRight(base, "/"), hc: cc.Client(ctx), reqTimeout: reqTimeout, mimeTimeout: mimeTimeout}
}

// Folder is a mail folder with its full, sanitized path (root → leaf).
type Folder struct {
	ID   string
	Path []string
}

// Folders returns every mail folder for userID (a UPN or object id), recursing
// through child folders and carrying each folder's display-name path.
func (c *Client) Folders(ctx context.Context, userID string) ([]Folder, error) {
	var out []Folder
	var walk func(parentID string, prefix []string) error
	walk = func(parentID string, prefix []string) error {
		path := "/users/" + url.PathEscape(userID) + "/mailFolders"
		if parentID != "" {
			path = "/users/" + url.PathEscape(userID) + "/mailFolders/" + url.PathEscape(parentID) + "/childFolders"
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
			if err := c.getJSON(ctx, next, &body); err != nil {
				return err
			}
			for _, f := range body.Value {
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

	// Envelope fields for the pre-download signature (§3.2). To/Cc are the bare
	// e-mail addresses (display names dropped) so the signature is independent of
	// header formatting; HasAttachments is Graph's boolean (attachment NAMES are
	// not in the listing, so the signature uses only presence).
	Subject        string
	From           string // sender e-mail address; "" when the tenant omits it
	To             []string
	Cc             []string
	HasAttachments bool

	Importance  string   // Graph "low"/"normal"/"high"; "" when the tenant omits it
	Sensitivity string   // Graph "normal"/"personal"/"private"/"confidential"; "" when omitted
	IsRead      *bool    // nil when the tenant omits it (read state then unknown)
	Categories  []string // the message's category tags; nil when the tenant omits them
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

// Messages streams every message reference in folderID to fn (paged). The
// $select is widened to carry the envelope fields (subject, from, toRecipients,
// ccRecipients, receivedDateTime, hasAttachments) the pre-download signature
// needs, alongside the message-state fields — all on one request (§3.2).
func (c *Client) Messages(ctx context.Context, userID, folderID string, fn func(MessageRef) error) error {
	next := c.base + "/users/" + url.PathEscape(userID) + "/mailFolders/" + url.PathEscape(folderID) +
		"/messages?$select=id,internetMessageId,subject,from,toRecipients,ccRecipients,receivedDateTime,hasAttachments,importance,isRead,sensitivity,categories&$top=1000"
	for next != "" {
		var body struct {
			Value []struct {
				ID                string           `json:"id"`
				InternetMessageID string           `json:"internetMessageId"`
				Subject           string           `json:"subject"`
				From              *graphRecipient  `json:"from"`
				ToRecipients      []graphRecipient `json:"toRecipients"`
				CcRecipients      []graphRecipient `json:"ccRecipients"`
				Received          string           `json:"receivedDateTime"`
				HasAttachments    bool             `json:"hasAttachments"`
				Importance        string           `json:"importance"`
				IsRead            *bool            `json:"isRead"`
				Sensitivity       string           `json:"sensitivity"`
				Categories        []string         `json:"categories"`
			} `json:"value"`
			Next string `json:"@odata.nextLink"`
		}
		if err := c.getJSON(ctx, next, &body); err != nil {
			return err
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
				Sensitivity:       m.Sensitivity,
				IsRead:            m.IsRead,
				Categories:        m.Categories,
			}
			if m.From != nil {
				ref.From = strings.TrimSpace(m.From.EmailAddress.Address)
			}
			if t, err := time.Parse(time.RFC3339, m.Received); err == nil {
				ref.Received = t
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
	u := c.base + "/users/" + url.PathEscape(userID) + "/messages/" + url.PathEscape(messageID) + "/$value"
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

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()
	resp, err := c.get(ctx, u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusErr(resp)
	}
	return json.NewDecoder(resp.Body).Decode(v)
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
