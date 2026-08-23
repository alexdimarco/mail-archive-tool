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
}

// Client talks to Microsoft Graph with an auto-refreshing app-only token.
type Client struct {
	base string
	hc   *http.Client
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
	return &Client{base: strings.TrimRight(base, "/"), hc: cc.Client(ctx)}
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
// downloading the body).
type MessageRef struct {
	ID                string
	InternetMessageID string // angle brackets stripped, matching go-message
	Received          time.Time
}

// Messages streams every message reference in folderID to fn (paged).
func (c *Client) Messages(ctx context.Context, userID, folderID string, fn func(MessageRef) error) error {
	next := c.base + "/users/" + url.PathEscape(userID) + "/mailFolders/" + url.PathEscape(folderID) +
		"/messages?$select=id,internetMessageId,receivedDateTime&$top=1000"
	for next != "" {
		var body struct {
			Value []struct {
				ID                string `json:"id"`
				InternetMessageID string `json:"internetMessageId"`
				Received          string `json:"receivedDateTime"`
			} `json:"value"`
			Next string `json:"@odata.nextLink"`
		}
		if err := c.getJSON(ctx, next, &body); err != nil {
			return err
		}
		for _, m := range body.Value {
			ref := MessageRef{ID: m.ID, InternetMessageID: strings.Trim(m.InternetMessageID, "<>")}
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
	u := c.base + "/users/" + url.PathEscape(userID) + "/messages/" + url.PathEscape(messageID) + "/$value"
	resp, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusErr(resp)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
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
