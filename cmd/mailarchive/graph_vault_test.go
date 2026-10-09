package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graphconfig"
)

// covers: MA-453, R14, R12, S28, S20
// On Windows a device sign-in is saved to the OS vault (Credential Manager) and
// the -token-cache file is never written, so `schedule` must validate a device
// job against the vault the scheduled run will read — not the file. A real
// account-keyed store stands in for the Windows vault (deviceTokenStore is the
// one place both the run and the check resolve it). A sign-in held only in the
// vault is schedulable, the job carrying -auth device -token-cache; an empty
// vault is refused naming the sign-in remedy even when an old signed-in file
// sits at -token-cache (the run would fail from the empty vault), and the check
// writes nothing to the vault or the file.
func TestScheduleDeviceJobChecksTheVault(t *testing.T) {
	vaultDir := t.TempDir()
	orig := deviceTokenStore
	t.Cleanup(func() { deviceTokenStore = orig })
	deviceTokenStore = func(tenant, clientID string) graph.TokenStore {
		return graphconfig.NewTokenStore(graphconfig.NewFileSecretStore(vaultDir), tenant, clientID)
	}
	vault := deviceTokenStore("contoso.com", "APPID")
	primed := []byte(`{"upn":"a@contoso.com","token":{"refresh_token":"RT1"}}`)

	out := t.TempDir()
	cache := filepath.Join(t.TempDir(), "graph.token")
	job := []string{"graph", "-auth", "device", "-out", out, "-tenant", "contoso.com", "-client-id", "APPID", "-token-cache", cache}

	// Positive twin: signed in to the vault only, as the Windows CLI leaves it.
	if err := vault.StoreToken(primed); err != nil {
		t.Fatal(err)
	}
	j, err := parseJob(job)
	if err != nil {
		t.Fatalf("a device job whose sign-in is in the vault was refused: %v", err)
	}
	if got := strings.Join(assure.Reached(t, j.args, "canonical device job"), " "); !strings.Contains(got, "-auth device -token-cache "+cache) {
		t.Errorf("the canonical job lacks -auth device -token-cache: %s", got)
	}

	// An empty vault with an old signed-in file at -token-cache: the run reads
	// the vault and would fail, so the schedule is refused now.
	if err := vault.ClearToken(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, primed, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = parseJob(job)
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("sign in", "mailarchive graph -auth device"),
		assure.NoSideEffect(func() bool {
			_, verr := vault.LoadToken()
			after, ferr := os.ReadFile(cache)
			return errors.Is(verr, os.ErrNotExist) && ferr == nil && bytes.Equal(after, primed)
		}))
}
