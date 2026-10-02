package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"mail-archive-tool/internal/graphconfig"
)

// setupCSP governs the wizard: its own same-origin script + fetch, inline styles,
// data: images. It POSTs via fetch (connect-src 'self'), so unlike the reader UI
// it is not under form-action 'none'. Still no remote anything, never framed.
const setupCSP = "default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'none'; base-uri 'none'"

// SetupHandler is the standalone Graph setup wizard served by `mailarchive setup`
// on a loopback address ONLY (the caller enforces the loopback bind). It writes
// the non-secret config to cfgPath and the client secret to store; the secret is
// write-only — never logged, never returned. The reader `serve` is untouched
// (design-graph-setup-wizard W-C1, R19 unchanged).
func SetupHandler(cfgPath string, store graphconfig.SecretStore) http.Handler {
	csrf := randomToken()
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/setup" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(setupPage(cfgPath, store, csrf)))
	})
	mux.HandleFunc("/setup.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write([]byte(setupJS))
	})
	mux.HandleFunc("/api/setup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		// Anti-CSRF + anti-DNS-rebind (W-C2): same-origin, tokened, loopback only.
		// Any failure refuses BEFORE touching config or the secret store.
		if !GuardLocalPOST(r, csrf) {
			http.Error(w, "refused: this setup endpoint accepts only a same-origin, tokened request from the local machine", http.StatusForbidden)
			return
		}
		var in struct {
			Tenant, ClientID, Auth, Secret string
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		in.Tenant, in.ClientID = strings.TrimSpace(in.Tenant), strings.TrimSpace(in.ClientID)
		in.Auth = strings.ToLower(strings.TrimSpace(in.Auth))
		switch {
		case in.Tenant == "":
			httpJSONError(w, "Directory (tenant) ID is required")
			return
		case in.ClientID == "":
			httpJSONError(w, "Application (client) ID is required")
			return
		case in.Auth != "device" && in.Auth != "app":
			httpJSONError(w, "choose an auth mode: device or app")
			return
		}

		// Delete a superseded registration's secret (W-C... MA-270) if the account
		// key changed.
		if prev, err := graphconfig.Load(cfgPath); err == nil && prev.Tenant != "" && prev.ClientID != "" {
			prevAcct := graphconfig.AccountKey(prev.Tenant, prev.ClientID)
			if prevAcct != graphconfig.AccountKey(in.Tenant, in.ClientID) {
				_ = store.Delete(prevAcct)
			}
		}

		acct := graphconfig.AccountKey(in.Tenant, in.ClientID)
		configured := false
		if in.Auth == "app" {
			if s := strings.TrimSpace(in.Secret); s != "" {
				if err := store.Set(acct, s); err != nil {
					httpJSONError(w, "could not store the secret: "+err.Error())
					return
				}
				configured = true
			} else if _, err := store.Get(acct); err == nil {
				configured = true // keep the already-stored secret (blank = unchanged)
			} else {
				httpJSONError(w, "a client secret is required for app-only auth (or switch to device)")
				return
			}
		} else {
			// Device mode needs no secret; drop any stored one for this account.
			_ = store.Delete(acct)
			configured = true
		}
		in.Secret = "" // drop the plaintext secret as soon as it is stored

		cfg := &graphconfig.Config{Tenant: in.Tenant, ClientID: in.ClientID, Auth: in.Auth}
		if err := graphconfig.Save(cfgPath, cfg); err != nil {
			httpJSONError(w, "could not save the configuration: "+err.Error())
			return
		}
		// Never echo the secret — only whether one is configured.
		writeJSON(w, map[string]any{"ok": true, "auth": in.Auth, "configured": configured})
	})

	return setupHeaders(mux)
}

func setupHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", setupCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "setup" // extremely unlikely; the loopback+Origin+Host checks still gate
	}
	return hex.EncodeToString(b)
}

// NewCSRFToken mints a per-process CSRF token for a loopback control surface.
func NewCSRFToken() string { return randomToken() }

// GuardLocalPOST reports whether r is a safe same-origin, tokened POST from this
// machine for a loopback control surface (the setup wizard and the desktop
// dashboard share it): the Host must be loopback, a present Origin must be a
// loopback origin, and the per-process CSRF token must match. It does NOT defend
// against a local process running as the same user (which already has the user's
// rights and could run the engine directly) — see ux-contract X9 / design DC4.
func GuardLocalPOST(r *http.Request, csrfToken string) bool {
	return IsLoopback(r.Host) && originIsLoopback(r) && tokenOK(r, csrfToken)
}

// tokenOK compares the request's CSRF token (header or JSON is header-only here)
// to the process token in constant time.
func tokenOK(r *http.Request, want string) bool {
	got := r.Header.Get("X-CSRF-Token")
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// originIsLoopback accepts a request with no Origin (a same-origin fetch may omit
// it) or one whose Origin host is loopback; a cross-site Origin is refused.
func originIsLoopback(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	return IsLoopback(u.Host)
}

func httpJSONError(w http.ResponseWriter, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, map[string]any{"ok": false, "error": msg})
}
