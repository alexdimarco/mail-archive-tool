package main

import (
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
)

// covers: MA-265, R19, R12, S40
// `mailarchive setup` refuses a non-loopback bind: the wizard writes credentials and
// must never be reachable from the network.
func TestSetupRefusesNonLoopback(t *testing.T) {
	code, stderr := runCLI("setup", "-addr", "0.0.0.0:8097")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("loopback"))
}

// covers: MA-269, R17, R12, S40
// `graph` fills an unset -tenant/-client-id/-auth from the saved setup config and
// resolves the app secret from the store: with a config present (tenant/client/auth=app)
// but no stored secret, a `graph` run with no credential flags gets PAST the
// required-field checks (proving the config was applied) and stops at the secret check,
// naming `setup` — never the "-tenant is required" error.
func TestGraphFillsFromSetupConfig(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("overriding the OS config dir via XDG_CONFIG_HOME is Linux-specific")
	}
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	p, err := graphconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := graphconfig.Save(p, &graphconfig.Config{Tenant: "contoso", ClientID: "APPID", Auth: "app"}); err != nil {
		t.Fatal(err)
	}

	code, stderr := runCLI("graph", "-out", t.TempDir(), "-mailbox", "a@contoso.com")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("setup"))
	if strings.Contains(stderr, "-tenant is required") || strings.Contains(stderr, "-client-id is required") {
		t.Errorf("the saved config was not applied (hit a required-field error): %s", stderr)
	}
}
