package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-453, R14, R12, S28, S20
// The schedule-time sign-in check reads the SAME backend the unattended run
// loads from. With a TokenStore (the OS vault, where the CLI keeps the device
// sign-in on Windows) the vault decides and the -token-cache file is not
// consulted; without one the file decides. For every arrangement CheckSignIn
// accepts exactly when an unattended NewDelegated with the same Config loads a
// saved sign-in, and each refusal names the sign-in remedy.
func TestCheckSignInReadsTheRunsBackend(t *testing.T) {
	primed := []byte(`{"upn":"a@contoso.com","token":{"refresh_token":"RT1"}}`)
	cases := []struct {
		name     string
		store    *memTokenStore // nil: no vault (the file cache is the backend)
		withFile bool           // a signed-in file sits at TokenCachePath
		signedIn bool           // what the unattended run finds
	}{
		{"vault holds the sign-in, no file", &memTokenStore{data: primed}, false, true},
		{"vault empty, a stale file from before the vault", &memTokenStore{}, true, false},
		{"no vault, the file holds the sign-in", nil, true, true},
		{"no vault, no file", nil, false, false},
	}
	for _, c := range cases {
		file := filepath.Join(t.TempDir(), "graph.token")
		if c.withFile {
			if err := os.WriteFile(file, primed, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		cfg := Config{Tenant: "contoso.com", ClientID: "APPID", TokenCachePath: file, Unattended: true}
		if c.store != nil {
			cfg.TokenStore = c.store
		}

		// The fixture's ground truth: what the scheduled run itself loads.
		_, runErr := NewDelegated(context.Background(), cfg)
		if (runErr == nil) != c.signedIn {
			t.Fatalf("%s: fixture wrong — the unattended run loaded=%v (%v), want %v", c.name, runErr == nil, runErr, c.signedIn)
		}

		checkErr := CheckSignIn(cfg)
		if c.signedIn {
			if checkErr != nil {
				t.Errorf("%s: the run would sign in from its backend, but the schedule check refuses: %v", c.name, checkErr)
			}
			continue
		}
		code, msg := 0, ""
		if checkErr != nil {
			code, msg = 1, checkErr.Error()
		}
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("sign in", "mailarchive graph -auth device"))
	}
}
