package app

import "testing"

// MA-329's golden harness, for the Phase 2c closure in package app_test
// (design-encrypted-archives-phase3 §10): an encrypted run over the same inputs,
// decrypted through keyring.DecryptCopy, is compared with the same frozen golden.
// Nothing here writes the golden.

// P14Fixture is one archive as the golden records it.
type P14Fixture = p14Fixture

// P14Inputs are the local inputs of MA-329's fixtures, made under parent, by name.
func P14Inputs(t *testing.T, parent string) map[string]string {
	t.Helper()
	return map[string]string{
		"pst":            "../../testdata/support.pst",
		"mbox":           p14Mbox(t, parent),
		"maildir":        p14Maildir(t, parent),
		"evolution-pp":   p14EvolutionMaildirPP(t, parent),
		"evolution-imap": p14EvolutionCache(t, parent),
	}
}

// P14GraphOptions starts MA-329's fake Graph server.
func P14GraphOptions(t *testing.T) GraphOptions { return p14GraphOptions(t) }

// P14Collect is the archive at out as the golden records it.
func P14Collect(t *testing.T, out string) P14Fixture { return p14Collect(t, out) }

// P14Compare compares got with the frozen golden, reporting each difference; it
// never writes the golden.
func P14Compare(t *testing.T, got map[string]P14Fixture) bool { return p14Compare(t, got, false) }
