package graph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// unreadableVault is a vault this session cannot open: LoadToken fails with err
// (fault injection standing in for Credential Manager outside an interactive
// logon). It counts writes, which the schedule-time check must never make.
type unreadableVault struct {
	err    error
	writes int
}

func (v *unreadableVault) LoadToken() ([]byte, error) { return nil, v.err }
func (v *unreadableVault) StoreToken([]byte) error    { v.writes++; return nil }
func (v *unreadableVault) ClearToken() error          { v.writes++; return nil }

// covers: MA-453, R14, R12, S28, S20
// Whatever keeps the schedule-time check from reading the run's sign-in
// backend, the refusal says where the check looked and how to sign in, and
// keeps the cause with its own remedy — never a bare store error. The vault:
// one this session cannot open (Credential Manager answers
// ERROR_NO_SUCH_LOGON_SESSION outside an interactive logon; injected here),
// even beside a signed-in file the run would not read. The file: one readable
// by other users (`chmod 600`), and one that is not valid JSON. The unattended
// run cannot load any of them either (the fixture's ground truth), and the
// check writes nothing. The positive twins: each backend healthy is accepted.
func TestCheckSignInNamesWhereOnAnyLoadFailure(t *testing.T) {
	primed := []byte(`{"upn":"a@contoso.com","token":{"refresh_token":"RT1"}}`)
	writeFile := func(data []byte, mode os.FileMode) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "graph.token")
		if err := os.WriteFile(p, data, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil { // past the umask
			t.Fatal(err)
		}
		return p
	}
	sameBytes := func(p string, want []byte) bool {
		got, err := os.ReadFile(p)
		return err == nil && bytes.Equal(got, want)
	}

	// Positive twins first: a healthy vault, and a healthy file with no vault.
	for _, cfg := range []Config{
		{Tenant: "contoso.com", ClientID: "APPID", TokenCachePath: filepath.Join(t.TempDir(), "absent.token"), TokenStore: &memTokenStore{data: primed}, Unattended: true},
		{Tenant: "contoso.com", ClientID: "APPID", TokenCachePath: writeFile(primed, 0o600), Unattended: true},
	} {
		if err := CheckSignIn(cfg); err != nil {
			t.Fatalf("a healthy sign-in (vault=%v) was refused: %v", cfg.TokenStore != nil, err)
		}
	}

	const sessionGone = "A specified logon session does not exist. It may already have been terminated."
	besideVault := writeFile(primed, 0o600)
	vault := &unreadableVault{err: fmt.Errorf("CredRead graph-token:1a2b3c4d-5e6f7a8b: %w", errors.New(sessionGone))}
	torn := []byte(`{"upn":"a@contoso.com","tok`)
	tornFile := writeFile(torn, 0o600)
	type fixture struct {
		name      string
		cfg       Config
		names     []string    // where the check looked, and the cause
		unchanged func() bool // the check wrote nothing
	}
	fixtures := []fixture{
		{"vault this session cannot open, beside a signed-in file",
			Config{Tenant: "contoso.com", ClientID: "APPID", TokenCachePath: besideVault, TokenStore: vault, Unattended: true},
			[]string{"credential store", "contoso.com", "APPID", sessionGone},
			func() bool { return vault.writes == 0 && sameBytes(besideVault, primed) }},
		{"file that is not valid JSON",
			Config{Tenant: "contoso.com", ClientID: "APPID", TokenCachePath: tornFile, Unattended: true},
			[]string{tornFile, "not valid JSON"},
			func() bool { return sameBytes(tornFile, torn) }},
	}
	if runtime.GOOS != "windows" { // the file's mode is checked on Unix only
		loose := writeFile(primed, 0o644)
		fixtures = append(fixtures, fixture{"file readable by other users",
			Config{Tenant: "contoso.com", ClientID: "APPID", TokenCachePath: loose, Unattended: true},
			[]string{loose, "chmod 600"},
			func() bool {
				fi, err := os.Stat(loose)
				return err == nil && fi.Mode().Perm() == 0o644 && sameBytes(loose, primed)
			}})
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			if _, runErr := NewDelegated(context.Background(), f.cfg); runErr == nil {
				t.Fatalf("fixture wrong: the unattended run loaded a sign-in from %s", f.name)
			}
			err := CheckSignIn(f.cfg)
			code, msg := 0, ""
			if err != nil {
				code, msg = 1, err.Error()
			}
			assure.Refused(t, code, msg, assure.Code(1),
				assure.Names(append([]string{"sign in", "mailarchive graph -auth device"}, f.names...)...),
				assure.NoSideEffect(f.unchanged))
		})
	}
}
