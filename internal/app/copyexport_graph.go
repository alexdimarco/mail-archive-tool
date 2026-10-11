package app

import (
	"context"
	"io"
	"time"

	"mail-archive-tool/internal/graph"
)

// The Microsoft 365 adapter (design §4.5, §4.13; originals only). It reads the
// signed-in user's OWN mailbox through E1's delegated read paths (MessagePage) and
// SG's bounded MIMETo, one message at a time, and passes MIMETo a size limit that
// is the smaller of the shared cap and the free-space budget (RR2-C5). The page, the
// seen-set and the row handles that choose what to list are slice E3's.

// copyExportGraph is the part of *graph.Client the adapter uses.
type copyExportGraph interface {
	MessagePage(ctx context.Context, userID, folderID, next string, o graph.PageOpts) ([]graph.MessageRef, string, error)
	NewPageGuard() *graph.PageGuard
	MIMETo(ctx context.Context, userID, messageID string, w io.Writer, maxBytes int64, idle, overall time.Duration) (int64, error)
	Probe(ctx context.Context) error
	ForceRefresh(ctx context.Context) error
}

// M365Source is the copy export's Microsoft 365 source.
type M365Source struct {
	g        copyExportGraph
	upn      string
	tenant   string
	signedIn func() (string, bool)

	// Idle and Overall are MIMETo's windows; zero is the client's idle window and
	// the 2-hour overall cap the copy export carries (graph.MIMEMaxDuration).
	Idle, Overall time.Duration

	listed, honored int // first pages seen, and how many said "honored" (XC27)
}

// NewM365Source is the source for the signed-in user upn. signedIn reads the saved
// sign-in locally (graph.SignedIn on the same config); nil means "still upn".
func NewM365Source(g *graph.Client, upn, tenant string, signedIn func() (string, bool)) *M365Source {
	return newM365Source(g, upn, tenant, signedIn)
}

func newM365Source(g copyExportGraph, upn, tenant string, signedIn func() (string, bool)) *M365Source {
	return &M365Source{g: g, upn: upn, tenant: tenant, signedIn: signedIn}
}

// Describe names the source for the header and the manifest. immutable_ids is true
// only when every listed folder's first page said the immutable ids were honored.
func (s *M365Source) Describe() CopySourceInfo {
	return CopySourceInfo{Kind: "m365", Mailbox: s.upn, Tenant: s.tenant,
		ImmutableIDs:  s.listed > 0 && s.listed == s.honored,
		OriginalMeans: "MIME bytes returned by Microsoft Graph /$value at export time"}
}

// ListFolder lists every message of one folder, newest first, 1,000 to a page
// through one nextLink guard, as CopyMessages in the folder named by path (the
// folder's own display path and ids, which the caller took from its fresh tree).
// The first page's "honored" flag, which MessagePage carries as PhysID == ID on each
// ref, is recorded for the manifest's immutable_ids: an empty first page cannot
// carry it, so an empty folder says nothing.
func (s *M365Source) ListFolder(ctx context.Context, path []CopyFolderRef) ([]CopyMessage, error) {
	folderID := path[len(path)-1].ID
	guard := s.g.NewPageGuard()
	var out []CopyMessage
	next, first := "", true
	honored := false
	for {
		refs, link, err := s.g.MessagePage(ctx, "", folderID, next, graph.PageOpts{Top: 1000, Honored: honored, Guard: guard})
		if err != nil {
			return nil, err
		}
		if first {
			first = false
			if len(refs) > 0 {
				s.listed++
				honored = refs[0].PhysID != "" && refs[0].PhysID == refs[0].ID
				if honored {
					s.honored++
				}
			}
		}
		for _, r := range refs {
			out = append(out, CopyMessage{SourceID: r.ID, InternetMessageID: r.InternetMessageID, Folder: path, Subject: r.Subject,
				From: r.From, FromName: r.FromName, Received: r.Received, SizeHint: r.SizeHint, Mailbox: s.upn})
		}
		if link == "" {
			return out, nil
		}
		next = link
	}
}

// Fetch streams the message's /$value bytes into w, refusing a body above maxBytes
// (the smaller of the shared cap and the free-space budget).
func (s *M365Source) Fetch(ctx context.Context, m *CopyMessage, w io.Writer, maxBytes int64) (int64, error) {
	return s.g.MIMETo(ctx, "", m.SourceID, w, maxBytes, s.Idle, s.Overall)
}

// Probe is the one control request (Client.Probe).
func (s *M365Source) Probe(ctx context.Context) error { return s.g.Probe(ctx) }

// RefreshAuth is the one forced token refresh after a 401.
func (s *M365Source) RefreshAuth(ctx context.Context) error { return s.g.ForceRefresh(ctx) }

// SignedIn is the saved sign-in's UPN, read locally.
func (s *M365Source) SignedIn() (string, bool) {
	if s.signedIn == nil {
		return s.upn, true
	}
	return s.signedIn()
}
