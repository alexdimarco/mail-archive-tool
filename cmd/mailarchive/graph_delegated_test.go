package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-257, R17, R12, S20
// The device-auth CLI refuses a mixture that would mislead: a client secret (a
// public client takes none), an unknown -auth value, and more than one -mailbox
// (device signs in as one user). Each is a typed non-zero naming the problem and
// leaks no secret.
func TestGraphDeviceCLIRefusals(t *testing.T) {
	out := t.TempDir()
	secret := filepath.Join(t.TempDir(), "s.secret")
	if err := os.WriteFile(secret, []byte("s3cret-value"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A secret with device auth is refused (never fed to a flow that ignores it).
	code, stderr := runCLI("graph", "-auth", "device", "-out", out, "-tenant", "t", "-client-id", "c", "-client-secret-file", secret)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("secret"), assure.Forbid("s3cret-value"))

	// An unknown -auth value is refused naming the two valid modes.
	code, stderr = runCLI("graph", "-auth", "bogus", "-out", out, "-tenant", "t", "-client-id", "c")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("app", "device"))

	// More than one mailbox under device auth is refused (one signer, one mailbox).
	code, stderr = runCLI("graph", "-auth", "device", "-out", out, "-tenant", "t", "-client-id", "c",
		"-mailbox", "a@x", "-mailbox", "b@x")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("one"))
}

// covers: MA-260, R14, R12, S28, S20
// `schedule` validates a device-auth graph job's token cache at install time: a
// primed cache previews with `-auth device -token-cache PATH` in the canonical
// command; a missing -token-cache and a not-yet-signed-in cache are each refused
// naming the sign-in remedy. The device job never carries a secret. On Windows
// the run's sign-in backend is the vault, so there the primed file alone is
// refused (MA-453 covers the vault-backed twin). The binary would read the
// runner's REAL Credential Manager there, so on Windows each leg runs the same
// check in-process — parseJob, which `schedule` calls — against an empty
// stand-in vault in deviceTokenStore's place: the outcome never depends on what
// the CI user's vault holds or how it answers.
func TestScheduleDeviceTokenCache(t *testing.T) {
	out := t.TempDir()
	cache := filepath.Join(t.TempDir(), "graph-token.json")
	if err := os.WriteFile(cache, []byte(`{"upn":"a@contoso.com","token":{"refresh_token":"RT1","access_token":"AT1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	job := []string{"graph", "-auth", "device", "-out", out, "-tenant", "contoso.com", "-client-id", "APPID"}

	// scheduleJob validates `schedule -- <job> extra…` and returns its exit code,
	// stdout and stderr: through the real binary where the run's backend is the
	// -token-cache file; in-process against the stand-in where it is a vault.
	vaultBackend := runtime.GOOS == "windows"
	if vaultBackend {
		standInVault(t, "contoso.com", "APPID")
	}
	scheduleJob := func(extra ...string) (int, string, string) {
		args := append(append([]string{}, job...), extra...)
		if !vaultBackend {
			return runCLIOut(t, append([]string{"schedule", "--"}, args...)...)
		}
		if _, err := parseJob(args); err != nil {
			return 1, "", err.Error()
		}
		return 0, "", ""
	}

	// Primed cache → preview (not applied) carries the device auth + cache.
	// On Windows the run reads the sign-in from Credential Manager, not this
	// file, so a primed file with no vault sign-in is refused there instead
	// (the vault-backed positive twin is MA-453, through deviceTokenStore).
	code, stdout, stderr := scheduleJob("-token-cache", cache)
	if vaultBackend {
		assure.Refused(t, code, stderr, assure.Code(1), assure.Names("sign in", "credential store"))
	} else {
		if code != 0 {
			t.Fatalf("device schedule preview exited %d: %s", code, stderr)
		}
		if !strings.Contains(stdout, "-auth device") || !strings.Contains(stdout, "-token-cache") {
			t.Errorf("preview does not carry -auth device -token-cache:\n%s", stdout)
		}
		if strings.Contains(stdout, "-client-secret-file") {
			t.Errorf("a device job must not carry a secret file:\n%s", stdout)
		}
	}

	// No -token-cache → refused with the sign-in remedy.
	code, _, stderr = scheduleJob()
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("token-cache"))

	// A not-yet-created cache → refused naming the sign-in remedy.
	missing := filepath.Join(t.TempDir(), "nope.json")
	code, _, stderr = scheduleJob("-token-cache", missing)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("sign in"))
}
