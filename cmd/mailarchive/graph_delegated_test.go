package main

import (
	"os"
	"path/filepath"
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
// naming the sign-in remedy. The device job never carries a secret.
func TestScheduleDeviceTokenCache(t *testing.T) {
	out := t.TempDir()
	cache := filepath.Join(t.TempDir(), "graph-token.json")
	if err := os.WriteFile(cache, []byte(`{"upn":"a@contoso.com","token":{"refresh_token":"RT1","access_token":"AT1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{"schedule", "--", "graph", "-auth", "device",
		"-out", out, "-tenant", "contoso.com", "-client-id", "APPID"}

	// Primed cache → preview (not applied) carries the device auth + cache.
	code, stdout, stderr := runCLIOut(t, append(append([]string{}, base...), "-token-cache", cache)...)
	if code != 0 {
		t.Fatalf("device schedule preview exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "-auth device") || !strings.Contains(stdout, "-token-cache") {
		t.Errorf("preview does not carry -auth device -token-cache:\n%s", stdout)
	}
	if strings.Contains(stdout, "-client-secret-file") {
		t.Errorf("a device job must not carry a secret file:\n%s", stdout)
	}

	// No -token-cache → refused with the sign-in remedy.
	code, _, stderr = runCLIOut(t, base...)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("token-cache"))

	// A not-yet-created cache → refused naming the sign-in remedy.
	missing := filepath.Join(t.TempDir(), "nope.json")
	code, _, stderr = runCLIOut(t, append(append([]string{}, base...), "-token-cache", missing)...)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("sign in"))
}
