package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"mail-archive-tool/internal/util"
)

// The delegated (per-user) scopes: read-only mail for the signed-in user's OWN
// mailbox, plus offline_access for a refresh token so later scheduled runs need
// no re-sign-in. Never Mail.ReadWrite/Mail.Send — the client stays GET-only (R17).
var delegatedScopes = []string{"https://graph.microsoft.com/Mail.Read", "offline_access"}

const tokenCacheMaxBytes = 64 << 10 // a Graph token JSON dwarfs a client secret

// DeviceAuth is what a first-time sign-in shows the operator: open the URI in a
// browser and enter the code. It is passed to Config.Prompt.
type DeviceAuth struct {
	VerificationURI string
	UserCode        string
	Expiry          time.Time
}

// tokenCache is the on-disk shape: the oauth2 token bound to the signer it was
// minted for (design-graph-delegated D-C1 — a cache is one mailbox's, and a run
// refuses a cache whose UPN differs from -mailbox).
type tokenCache struct {
	UPN   string        `json:"upn"`
	Token *oauth2.Token `json:"token"`
}

// NewDelegated builds a per-user (device-code) Graph client that addresses /me.
// It loads the cached sign-in if there is one (refreshing silently on demand);
// otherwise it runs the interactive device-code grant and caches the result —
// unless Unattended, when a missing/unusable cache fails naming the sign-in
// remedy (a scheduled run has no console to prompt at). The signer is read back
// from /me by the caller (Me) and matched to -mailbox there.
func NewDelegated(ctx context.Context, cfg Config) (*Client, error) {
	base := strings.TrimRight(orDefault(cfg.BaseURL, defaultBaseURL), "/")
	tokenURL := orDefault(cfg.TokenURL, "https://login.microsoftonline.com/"+cfg.Tenant+"/oauth2/v2.0/token")
	deviceURL := orDefault(cfg.DeviceAuthURL, "https://login.microsoftonline.com/"+cfg.Tenant+"/oauth2/v2.0/devicecode")
	if strings.TrimSpace(cfg.TokenCachePath) == "" {
		return nil, errors.New("internal: NewDelegated requires a token cache path")
	}
	reqTimeout, mimeTimeout := cfg.RequestTimeout, cfg.MIMETimeout
	if reqTimeout <= 0 {
		reqTimeout = defaultRequestTimeout
	}
	if mimeTimeout <= 0 {
		mimeTimeout = defaultMIMETimeout
	}
	// One deadline-bounded transport for BOTH the device/token exchanges and the
	// Graph API calls, so a stalled sign-in or a stalled listing fails within the
	// deadline instead of hanging an unattended job (design D-C5, R17/MA-98).
	transport := httpTransport(reqTimeout)
	ctx = context.WithValue(ctx, oauth2.HTTPClient, transport)
	conf := &oauth2.Config{
		ClientID: cfg.ClientID,
		Scopes:   delegatedScopes,
		Endpoint: oauth2.Endpoint{TokenURL: tokenURL, DeviceAuthURL: deviceURL, AuthStyle: oauth2.AuthStyleInParams},
	}

	cached, err := loadTokenCache(cfg.TokenCachePath)
	var tok *oauth2.Token
	var upn string
	switch {
	case err == nil:
		tok, upn = cached.Token, cached.UPN
	case errors.Is(err, os.ErrNotExist):
		if cfg.Unattended {
			return nil, fmt.Errorf("no saved sign-in at %s: run `mailarchive graph -auth device …` once interactively to sign in (a scheduled run cannot prompt for the device code)", cfg.TokenCachePath)
		}
		tok, err = deviceConsent(ctx, conf, cfg.Prompt)
		if err != nil {
			return nil, err
		}
		// Read the signer from Graph (not a self-asserted token) so the cache is
		// bound to the right UPN, using the fresh token directly.
		tmp := &Client{base: base, hc: oauth2.NewClient(ctx, oauth2.StaticTokenSource(tok)), reqTimeout: reqTimeout, mimeTimeout: mimeTimeout, meMode: true}
		upn, err = tmp.Me(ctx)
		if err != nil {
			return nil, fmt.Errorf("read the signed-in user: %w", err)
		}
		if err := storeTokenCache(cfg.TokenCachePath, upn, tok); err != nil {
			return nil, err
		}
	default:
		return nil, err // a malformed or insecure cache is a hard error, never a silent re-consent
	}

	src := newDeviceSource(ctx, conf, cfg.TokenCachePath, upn, tok)
	return &Client{base: base, hc: oauth2.NewClient(ctx, src), reqTimeout: reqTimeout, mimeTimeout: mimeTimeout, meMode: true}, nil
}

// Me returns the signed-in user's userPrincipalName, read from GET /me (delegated
// only). The caller matches it to -mailbox and uses it as the one mailbox to walk
// (design P3). Reading it from Graph — not from the token — is deliberate: the
// token is never trusted to name its own subject.
func (c *Client) Me(ctx context.Context) (string, error) {
	if !c.meMode {
		return "", errors.New("Me is only valid for a delegated (device-auth) client")
	}
	var body struct {
		UPN string `json:"userPrincipalName"`
		ID  string `json:"id"`
	}
	if _, err := c.getJSON(ctx, c.base+"/me?$select=userPrincipalName,id", &body); err != nil {
		return "", err
	}
	upn := strings.TrimSpace(body.UPN)
	if upn == "" {
		return "", errors.New("graph /me returned no userPrincipalName")
	}
	return upn, nil
}

// CheckTokenCache validates a device-auth token cache the way `schedule` must at
// install time: the file passes the secure-file discipline, parses, and carries a
// refresh token (so a later unattended run can refresh without a prompt). A
// missing/unusable cache is refused naming the sign-in remedy (design P7/D-C3).
func CheckTokenCache(path string) error {
	tc, err := loadTokenCache(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no saved sign-in at %s: run `mailarchive graph -auth device …` once interactively (as the account this schedule will run as) to sign in before scheduling", path)
		}
		return err
	}
	if tc.Token == nil || strings.TrimSpace(tc.Token.RefreshToken) == "" {
		return fmt.Errorf("token cache %s carries no refresh token — sign in again with `mailarchive graph -auth device …` before scheduling", path)
	}
	return nil
}

// deviceConsent runs the interactive device-code grant: it shows the verification
// URI + code, then blocks (bounded by the code's own expiry) until the operator
// finishes signing in. The prompt goes to the console (stderr), never a -log file
// (D-C3), so an interactive run always shows it.
func deviceConsent(ctx context.Context, conf *oauth2.Config, prompt func(DeviceAuth)) (*oauth2.Token, error) {
	da, err := conf.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("start device sign-in: %w", err)
	}
	if prompt == nil {
		prompt = defaultPrompt
	}
	prompt(DeviceAuth{VerificationURI: da.VerificationURI, UserCode: da.UserCode, Expiry: da.Expiry})
	tok, err := conf.DeviceAccessToken(ctx, da)
	if err != nil {
		return nil, fmt.Errorf("device sign-in did not complete (the code may have expired — re-run to sign in): %w", err)
	}
	return tok, nil
}

func defaultPrompt(d DeviceAuth) {
	fmt.Fprintf(os.Stderr, "\nTo sign in, open %s in a browser and enter this code:\n\n    %s\n\n(waiting for you to finish signing in in the browser…)\n\n", d.VerificationURI, d.UserCode)
}

// deviceSource is an auto-refreshing TokenSource that persists the token whenever
// it changes, race-safely (design D-C2). Entra rotates the refresh token on each
// use, so two runs sharing one cache could otherwise clobber each other; on a
// refresh failure a concurrent run could explain (a rotation it won), it reloads
// the cache and retries once before failing.
type deviceSource struct {
	ctx  context.Context
	conf *oauth2.Config
	path string
	upn  string

	mu   sync.Mutex
	cur  oauth2.TokenSource
	last *oauth2.Token
}

func newDeviceSource(ctx context.Context, conf *oauth2.Config, path, upn string, tok *oauth2.Token) *deviceSource {
	return &deviceSource{ctx: ctx, conf: conf, path: path, upn: upn,
		cur: oauth2.ReuseTokenSource(tok, conf.TokenSource(ctx, tok)), last: tok}
}

func (s *deviceSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.cur.Token()
	if err != nil {
		if reloaded := s.reload(); reloaded != nil {
			s.cur = oauth2.ReuseTokenSource(reloaded, s.conf.TokenSource(s.ctx, reloaded))
			s.last = reloaded
			t, err = s.cur.Token()
		}
		if err != nil {
			return nil, err
		}
	}
	if s.last == nil || t.AccessToken != s.last.AccessToken || t.RefreshToken != s.last.RefreshToken {
		_ = storeTokenIfNewer(s.path, s.upn, t) // best-effort; a failed persist never fails the run
		s.last = t
	}
	return t, nil
}

// reload re-reads the cache when a refresh failed, returning a token worth
// retrying only if a concurrent run wrote a DIFFERENT refresh token than ours.
func (s *deviceSource) reload() *oauth2.Token {
	tc, err := loadTokenCache(s.path)
	if err != nil || tc.Token == nil {
		return nil
	}
	if s.last != nil && tc.Token.RefreshToken == s.last.RefreshToken {
		return nil
	}
	return tc.Token
}

func loadTokenCache(path string) (*tokenCache, error) {
	data, err := util.ReadSecureFile(path, "token cache file", tokenCacheMaxBytes)
	if err != nil {
		return nil, err
	}
	var tc tokenCache
	if err := json.Unmarshal(data, &tc); err != nil {
		return nil, fmt.Errorf("token cache file %s is not valid JSON (delete it and sign in again): %w", path, err)
	}
	return &tc, nil
}

// storeTokenCache writes the cache atomically (temp file + fsync + rename) at 0600
// so a crash never leaves a torn token that would be read as valid — it fails
// toward re-consent (design P5/§3.5).
func storeTokenCache(path, upn string, tok *oauth2.Token) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("token cache dir %s: %w", dir, err)
	}
	data, err := json.Marshal(tokenCache{UPN: upn, Token: tok})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".graph-token-*.tmp")
	if err != nil {
		return fmt.Errorf("token cache temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed away
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("token cache temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("token cache temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("token cache temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("token cache temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("token cache rename to %s: %w", path, err)
	}
	return nil
}

// storeTokenIfNewer persists only a token newer (later expiry) than what is on
// disk, so a concurrent run's freshly-rotated token is not clobbered (D-C2).
func storeTokenIfNewer(path, upn string, tok *oauth2.Token) error {
	if existing, err := loadTokenCache(path); err == nil && existing.Token != nil {
		if !tok.Expiry.IsZero() && !existing.Token.Expiry.IsZero() && !tok.Expiry.After(existing.Token.Expiry) {
			return nil
		}
	}
	return storeTokenCache(path, upn, tok)
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
