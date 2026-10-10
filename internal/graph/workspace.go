package graph

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
