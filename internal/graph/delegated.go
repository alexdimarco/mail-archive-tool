package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	if cfg.TokenStore == nil && strings.TrimSpace(cfg.TokenCachePath) == "" {
		return nil, errors.New("internal: NewDelegated requires a token cache path or a token store")
	}
	be := tokenBackend{store: cfg.TokenStore, path: cfg.TokenCachePath}
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

	cached, err := be.load()
	var tok *oauth2.Token
	var upn string
	switch {
	case err == nil:
		tok, upn = cached.Token, cached.UPN
	case errors.Is(err, os.ErrNotExist):
		if cfg.Unattended {
			return nil, errors.New("no saved sign-in: run `mailarchive graph -auth device …` (or MailArchive Desktop) once interactively to sign in (a scheduled run cannot prompt for the device code)")
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
		if err := be.put(upn, tok); err != nil {
			return nil, err
		}
	default:
		return nil, err // a malformed or insecure cache is a hard error, never a silent re-consent
	}

	src := newDeviceSource(ctx, conf, be, upn, tok)
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
	be   tokenBackend
	upn  string

	mu   sync.Mutex
	cur  oauth2.TokenSource
	last *oauth2.Token
}

func newDeviceSource(ctx context.Context, conf *oauth2.Config, be tokenBackend, upn string, tok *oauth2.Token) *deviceSource {
	return &deviceSource{ctx: ctx, conf: conf, be: be, upn: upn,
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
		_ = s.be.putIfNewer(s.upn, t) // best-effort; a failed persist never fails the run
		s.last = t
	}
	return t, nil
}

// reload re-reads the cache when a refresh failed, returning a token worth
// retrying only if a concurrent run wrote a DIFFERENT refresh token than ours.
func (s *deviceSource) reload() *oauth2.Token {
	tc, err := s.be.load()
	if err != nil || tc.Token == nil {
		return nil
	}
	if s.last != nil && tc.Token.RefreshToken == s.last.RefreshToken {
		return nil
	}
	return tc.Token
}

// tokenBackend is where the delegated token cache lives: a TokenStore (e.g.
// Credential Manager) when set, otherwise a 0600 file at path (the cross-platform
// default). Routing load/store/ifNewer through it keeps the file path byte-for-byte
// as the shipped tests exercise it while letting the dashboard move the token into
// the OS vault on Windows (DC2).
type tokenBackend struct {
	store TokenStore
	path  string
}

func (b tokenBackend) load() (*tokenCache, error) {
	var data []byte
	var err error
	if b.store != nil {
		data, err = b.store.LoadToken()
	} else {
		data, err = util.ReadSecureFile(b.path, "token cache file", tokenCacheMaxBytes)
	}
	if err != nil {
		return nil, err
	}
	var tc tokenCache
	if err := json.Unmarshal(data, &tc); err != nil {
		return nil, fmt.Errorf("saved sign-in is not valid JSON (sign in again): %w", err)
	}
	return &tc, nil
}

// put writes the cache: to the store, or atomically to the 0600 file (temp +
// fsync + rename, so a crash never leaves a torn token — it fails toward re-consent).
func (b tokenBackend) put(upn string, tok *oauth2.Token) error {
	data, err := json.Marshal(tokenCache{UPN: upn, Token: tok})
	if err != nil {
		return err
	}
	if b.store != nil {
		return b.store.StoreToken(data)
	}
	return util.WriteFileAtomic0600(b.path, data)
}

// putIfNewer persists only a token newer (later expiry) than what is stored, so a
// concurrent run's freshly-rotated token is not clobbered (D-C2).
func (b tokenBackend) putIfNewer(upn string, tok *oauth2.Token) error {
	if existing, err := b.load(); err == nil && existing.Token != nil {
		if !tok.Expiry.IsZero() && !existing.Token.Expiry.IsZero() && !tok.Expiry.After(existing.Token.Expiry) {
			return nil
		}
	}
	return b.put(upn, tok)
}

func (b tokenBackend) clear() error {
	if b.store != nil {
		return b.store.ClearToken()
	}
	if err := os.Remove(b.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// File-path wrappers — the signatures the shipped tests use (store == nil).
func loadTokenCache(path string) (*tokenCache, error) { return tokenBackend{path: path}.load() }
func storeTokenCache(path, upn string, tok *oauth2.Token) error {
	return tokenBackend{path: path}.put(upn, tok)
}
func storeTokenIfNewer(path, upn string, tok *oauth2.Token) error {
	return tokenBackend{path: path}.putIfNewer(upn, tok)
}

// ClearSignIn removes the saved delegated sign-in (the token store entry or the
// file cache), so the next NewDelegated performs a fresh device consent. Used by
// the dashboard's "sign in again" / "clear sign-in" actions.
func ClearSignIn(cfg Config) error {
	return tokenBackend{store: cfg.TokenStore, path: cfg.TokenCachePath}.clear()
}

// SignedIn reports whether a saved delegated sign-in exists (a token in the store
// or file cache) and, if so, the mailbox it is for. It reads no network — just the
// local cache — so the dashboard can show the sign-in card cheaply.
func SignedIn(cfg Config) (upn string, ok bool) {
	tc, err := tokenBackend{store: cfg.TokenStore, path: cfg.TokenCachePath}.load()
	if err != nil || tc.Token == nil || strings.TrimSpace(tc.Token.RefreshToken) == "" {
		return "", false
	}
	return tc.UPN, true
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
