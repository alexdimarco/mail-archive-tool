package graph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The extraction workspace's read paths (design-extraction-workspace §4.5, slice
// E1): the folder tree with counts, one folder's count, and a message listing
// page. Like the rest of the client they issue GET only, and they read only the
// signed-in user's OWN mailbox, so they refuse any client that is not the
// delegated (/me) one. Their paging loops carry the nextLink guard WITH the
// host/path pin (XC25); capture's Folders and Messages do not until FI-SGc.

// MaxWorkspaceFolders is the folder cap the workspace passes to FolderTree (its
// refusal R-s).
const MaxWorkspaceFolders = 10000

// ErrOwnMailboxOnly is the refusal of a workspace read on a client that is not
// the delegated /me one: an app-only client would address /users/{upn}, and the
// workspace reads the signed-in user's own mailbox only (MA-403).
var ErrOwnMailboxOnly = errors.New("internal: the extraction workspace reads only the signed-in user's own mailbox, through a delegated (device sign-in) client")

// requireOwnMailbox refuses, before any request, a client that is not the
// delegated /me one.
func (c *Client) requireOwnMailbox() error {
	if !c.meMode {
		return ErrOwnMailboxOnly
	}
	return nil
}

// FolderInfo is one mail folder as the workspace's tree shows it. DisplayName is
// Graph's, raw and untrusted: a caller renders it as text, never as markup.
type FolderInfo struct {
	ID               string
	ParentID         string
	DisplayName      string
	ChildFolderCount int
	TotalItemCount   int
	WellKnown        string // WellKnownDeletedItems or WellKnownJunkEmail for those two folders, else ""
}

// FolderCapError is FolderTree's refusal of a mailbox with more folders than its
// cap (R-s).
type FolderCapError struct{ Limit int }

func (e *FolderCapError) Error() string {
	return fmt.Sprintf("this mailbox has more than %d folders, more than this version of MailArchive lists", e.Limit)
}

// folderTreeSelect is FolderTree's $select: the fields the tree and its counts
// are built from.
const folderTreeSelect = "id,displayName,parentFolderId,childFolderCount,totalItemCount"

// FolderTree lists every folder of the signed-in user's mailbox with its count,
// walking the hierarchy the way Folders does ($top=100, childFolders
// recursion, one visited set, the depth cap) but with NO Deleted Items or Junk
// exclusion: this is the user's explicit selection, and T7 governs capture's
// walks only (ruling Q2). Those two folders are labelled through their
// well-known ids (WellKnown), never by name. It refuses with a *FolderCapError
// above max folders (zero = MaxWorkspaceFolders). userID is ignored: the
// delegated client addresses /me.
func (c *Client) FolderTree(ctx context.Context, userID string, max int) ([]FolderInfo, error) {
	if err := c.requireOwnMailbox(); err != nil {
		return nil, err
	}
	if max <= 0 {
		max = MaxWorkspaceFolders
	}
	wellKnown := map[string]string{}
	for _, name := range []string{WellKnownDeletedItems, WellKnownJunkEmail} {
		id, err := c.WellKnownFolderID(ctx, userID, name)
		if err != nil {
			return nil, err
		}
		if id != "" {
			wellKnown[id] = name
		}
	}

	var out []FolderInfo
	visited := map[string]bool{} // as in Folders (fault-isolation §4.8.7, RR6-C6)
	var walk func(parentID string, level int) error
	walk = func(parentID string, level int) error {
		path := c.userSeg(userID) + "/mailFolders"
		if parentID != "" {
			path += "/" + url.PathEscape(parentID) + "/childFolders"
		}
		next := c.base + path + "?$select=" + folderTreeSelect + "&$top=100"
		guard := c.newPageGuard(true) // the workspace's loops are pinned from day one (XC25)
		for next != "" {
			if err := guard.admit(next); err != nil {
				return err
			}
			var body struct {
				Value []struct {
					ID               string `json:"id"`
					DisplayName      string `json:"displayName"`
					ParentFolderID   string `json:"parentFolderId"`
					ChildFolderCount int    `json:"childFolderCount"`
					TotalItemCount   int    `json:"totalItemCount"`
				} `json:"value"`
				Next string `json:"@odata.nextLink"`
			}
			if _, err := c.getJSON(ctx, next, &body); err != nil {
				return err
			}
			for _, f := range body.Value {
				if visited[f.ID] {
					continue
				}
				if level > maxFolderDepth {
					return &DepthError{Limit: maxFolderDepth}
				}
				if len(out) >= max {
					return &FolderCapError{Limit: max}
				}
				visited[f.ID] = true
				out = append(out, FolderInfo{ID: f.ID, ParentID: f.ParentFolderID, DisplayName: f.DisplayName,
					ChildFolderCount: f.ChildFolderCount, TotalItemCount: f.TotalItemCount, WellKnown: wellKnown[f.ID]})
				if f.ChildFolderCount > 0 {
					if err := walk(f.ID, level+1); err != nil {
						return err
					}
				}
			}
			next = body.Next
		}
		return nil
	}
	if err := walk("", 1); err != nil {
		return nil, err
	}
	return out, nil
}

// PageOpts are MessagePage's options.
type PageOpts struct {
	Top     int        // $top of the first page: 100 for browsing, 1000 for resolution (zero = 100; above 1000 = 1000)
	Honored bool       // the first page's recorded "immutable ids honored" flag, for a page reached by nextLink
	Guard   *PageGuard // the caller's loop guard (NewPageGuard); nil = a guard for this one call
	// Before, when set, makes the first page a keyset continuation (extraction design
	// §4.1, XC18): only messages received at or before this time, in the same order, so a
	// listing whose cursor expired continues after the last row the page holds.
	Before time.Time
}

// PageGuard is the nextLink guard of a caller's loop over MessagePage: the repeat
// set, the page cap and the host/path pin, one guard to a loop.
type PageGuard struct{ g *pageGuard }

// NewPageGuard is the guard for one paging loop over MessagePage.
func (c *Client) NewPageGuard() *PageGuard { return &PageGuard{g: c.newPageGuard(true)} }

// The message extended properties MessagePage asks for, best-effort and in one
// $expand: PidTagMessageSize (0x0E08, the size hint) and PidTagSensitivity
// (0x0036). The filter is id-aware (`or` of both ids), and the answer is decoded
// by property id (extPropNumber), never by position, because
// sensitivityFromExtProps takes the first value that looks like a sensitivity and
// would read a size of 2 as "private".
const (
	messageSizePropID = 0x0E08
	sensitivityPropID = 0x0036

	messagePageExpand = "singleValueExtendedProperties($filter=id%20eq%20'Integer%200x0E08'%20or%20id%20eq%20'Integer%200x0036')"

	defaultPageTop = 100
	maxPageTop     = 1000
)

// extPropNumber is the MAPI property number of a single-value extended property
// id as Graph spells it ("Integer 0x0E08"), whatever the case or the zero padding
// of its hex number; ok is false for any other id.
func extPropNumber(id string) (n uint64, ok bool) {
	typ, hex, found := strings.Cut(strings.ToLower(strings.TrimSpace(id)), " ")
	if !found || typ != "integer" {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(hex), "0x"), 16, 32)
	return n, err == nil
}

// decodeExtProps reads a message's size hint (-1 when absent or unreadable) and
// sensitivity ("" for Normal or absent) from its extended properties by id.
func decodeExtProps(props []singleValueExtProp) (size int64, sensitivity string) {
	size = -1
	for _, p := range props {
		switch n, _ := extPropNumber(p.ID); n {
		case messageSizePropID:
			if v, err := strconv.ParseInt(strings.TrimSpace(p.Value), 10, 64); err == nil && v >= 0 {
				size = v
			}
		case sensitivityPropID:
			sensitivity = sensitivityFromExtProps([]singleValueExtProp{p})
		}
	}
	return size, sensitivity
}

// MessagePage lists one page of folderID's messages, newest first, for the
// workspace's browse list and for resolving a selection (extraction design §4.5,
// XC27). With next empty it builds the first request: capture's
// messageSelectFields plus parentFolderId (so each message's folder and its
// state come from Graph), $orderby receivedDateTime desc, $top o.Top, and one
// best-effort $expand of the size and sensitivity extended properties; a tenant
// that rejects the $expand gets the first page again without it (the MA-285
// fallback, sizes unknown). With next set it follows that link exactly as the
// previous page gave it, after the guard has admitted it: scheme, host, port and
// path on the client's base, no repeat, within the page cap (XC25), and nothing is
// sent otherwise. It returns the page and its nextLink ("" on the last page).
//
// Immutable ids: when the response's Preference-Applied says IdType="ImmutableId"
// was honored, or o.Honored (the first page's flag, which the caller records with
// its cursor) says so, each ref's PhysID is its ID, as in Messages; otherwise
// PhysID stays empty.
func (c *Client) MessagePage(ctx context.Context, userID, folderID, next string, o PageOpts) ([]MessageRef, string, error) {
	if err := c.requireOwnMailbox(); err != nil {
		return nil, "", err
	}
	guard := c.newPageGuard(true)
	if o.Guard != nil {
		guard = o.Guard.g
	}
	top := o.Top
	if top <= 0 {
		top = defaultPageTop
	}
	top = min(top, maxPageTop)
	plain := c.base + c.userSeg(userID) + "/mailFolders/" + url.PathEscape(folderID) + "/messages" +
		"?$select=" + strings.Join(messageSelectFields, ",") + ",parentFolderId" +
		"&$orderby=receivedDateTime%20desc&$top=" + strconv.Itoa(top)
	if !o.Before.IsZero() {
		plain += "&$filter=receivedDateTime%20le%20" + o.Before.UTC().Format("2006-01-02T15:04:05Z")
	}
	link, expanded := next, false
	if link == "" {
		link, expanded = plain+"&$expand="+messagePageExpand, true
	}
	if err := guard.admit(link); err != nil {
		return nil, "", err
	}

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
			ParentFolderID    string               `json:"parentFolderId"`
			ExtProps          []singleValueExtProp `json:"singleValueExtendedProperties"`
		} `json:"value"`
		Next string `json:"@odata.nextLink"`
	}
	pref, err := c.getJSON(ctx, link, &body)
	var se *StatusError
	if err != nil && expanded && errors.As(err, &se) && !slices.Contains([]int{401, 403, 404, 429}, se.Code) {
		// Best-effort sensitivity and size (MA-285): a tenant that does not support
		// the $expand must not fail the listing, so the first page is asked for
		// again without it. A refusal that is about the sign-in, the folder or
		// throttling is not read as that, and every other error surfaces.
		pref, err = c.getJSON(ctx, plain, &body)
	}
	if err != nil {
		guard.forget(link) // a failed page may be retried through the same guard
		return nil, "", err
	}
	honored := o.Honored || strings.Contains(strings.ToLower(pref), "immutableid")
	refs := make([]MessageRef, 0, len(body.Value))
	for _, m := range body.Value {
		size, sens := decodeExtProps(m.ExtProps)
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
			Sensitivity:       sens,
			ParentFolderID:    m.ParentFolderID,
			SizeHint:          size,
		}
		if m.From != nil {
			ref.From = strings.TrimSpace(m.From.EmailAddress.Address)
			ref.FromName = m.From.EmailAddress.Name
		}
		if t, err := time.Parse(time.RFC3339, m.Received); err == nil {
			ref.Received = t
		}
		if honored {
			ref.PhysID = m.ID
		}
		refs = append(refs, ref)
	}
	return refs, body.Next, nil
}

// probeTimeout bounds Probe's one request.
const probeTimeout = 30 * time.Second

// Probe is the copy export's one control request (extraction design §4.13,
// RR2b-C7): GET /me?$select=id, sent once, with no retry loop and a 30 s bound.
// It is never sent through get, whose Retry-After sleep under a request-timeout
// context would turn a long throttle into a deadline error (F17), so a 429 or a
// 503 comes back at once as the typed *StatusError, and a server that does not
// answer as the transport's error. nil is a 2xx: the connection is fine.
func (c *Client) Probe(ctx context.Context) error {
	if err := c.requireOwnMailbox(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+c.userSeg("")+"?$select=id", nil)
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

// FolderCount reads one folder's totalItemCount: the second read of the
// reconciliation (extraction design §4.7). A folder that is gone is a
// *StatusError with Code 404.
func (c *Client) FolderCount(ctx context.Context, userID, folderID string) (int, error) {
	if err := c.requireOwnMailbox(); err != nil {
		return 0, err
	}
	u := c.base + c.userSeg(userID) + "/mailFolders/" + url.PathEscape(folderID) + "?$select=totalItemCount"
	var body struct {
		TotalItemCount *int `json:"totalItemCount"`
	}
	if _, err := c.getJSON(ctx, u, &body); err != nil {
		return 0, err
	}
	if body.TotalItemCount == nil {
		return 0, errors.New("graph returned no totalItemCount for the folder")
	}
	return *body.TotalItemCount, nil
}
