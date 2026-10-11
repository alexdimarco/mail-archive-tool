package app

import (
	"context"
	"errors"
	"io"
	"log"

	"mail-archive-tool/internal/graph"
)

// ErrNotSignedIn is OpenGraphSession's refusal of a session with no saved sign-in:
// the workspace then shows R-b ("You're not signed in to Microsoft 365"), never a
// device-code prompt (design §4.3).
var ErrNotSignedIn = errors.New("not signed in to Microsoft 365")

// OpenGraphSession builds the extraction workspace's Graph client (design §4.3): the
// device branch of buildGraphClient, forced to Auth "device" with Unattended, whatever
// the options (or the saved config) say. It addresses /me only and never prompts: a
// missing sign-in is ErrNotSignedIn, and an interactive sign-in happens only through
// the dashboard's own sign-in endpoint. It returns the client and the signer's UPN,
// read from GET /me, never from the token.
//
// ctx is the session's own context and must outlive any request: NewDelegated keeps it
// inside the refreshing token source, which oauth2 reuses on every refresh (XC6, MA-449).
func OpenGraphSession(ctx context.Context, g GraphOptions) (*graph.Client, string, error) {
	g.Auth, g.Unattended = "device", true
	g.ClientSecret, g.Mailboxes, g.Prompt = "", nil, nil
	if _, ok := graph.SignedIn(graph.Config{TokenStore: g.TokenStore, TokenCachePath: g.TokenCachePath}); !ok {
		return nil, "", ErrNotSignedIn
	}
	client, mailboxes, err := buildGraphClient(ctx, g, log.New(io.Discard, "", 0))
	if err != nil {
		return nil, "", err
	}
	if len(mailboxes) != 1 {
		return nil, "", errors.New("internal: the workspace session is not the signer's own mailbox")
	}
	return client, mailboxes[0], nil
}
