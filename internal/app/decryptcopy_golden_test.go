package app_test

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/keyring"
)

// The Phase 2c closure (design-encrypted-archives-phase3 §10): an encrypted archive's
// unencrypted copy is the plaintext archive the same inputs make. It is the first
// test in package app_test, so it can reach the copy through keyring.DecryptCopy —
// the path a person uses — and fails when that path changes a byte.

const closurePassword = "the phase 2c closure's passphrase"

var quiet = log.New(io.Discard, "", 0)

// encryptedThenCopied makes an encrypted archive, backs up into it twice with run
// (the second must export nothing, R2), and returns its unencrypted copy's folder.
func encryptedThenCopied(t *testing.T, run func(opts app.Options) (app.Result, error)) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "Enc")
	p, err := keyring.Create(out, closurePassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	for i, want := range []bool{true, false} {
		opts := app.Options{Out: out, Mode: export.Incremental, KeepRaw: true}
		done, err := keyring.Apply(&opts, u)
		if err != nil {
			t.Fatal(err)
		}
		res, err := run(opts)
		done()
		if err != nil {
			t.Fatalf("encrypted run %d: %v", i+1, err)
		}
		if exported := res.Stats.Exported > 0; exported != want {
			t.Fatalf("encrypted run %d exported %d", i+1, res.Stats.Exported)
		}
	}
	e := app.NewCopyExportEnv("", "", "test")
	plan, err := e.PlanDecryptCopy(out, t.TempDir(), nil, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rep, err := keyring.DecryptCopy(context.Background(), u, e, plan, nil, nil, quiet, nil)
	if err != nil || rep.ExitCode() != 0 {
		t.Fatalf("the copy: %+v %v", rep, err)
	}
	if _, err := os.Stat(plan.Dest); err != nil {
		t.Fatal(err)
	}
	return plan.Dest
}

// covers: MA-822, R37
// Phase 2c closed: an encrypted run over MA-329's healthy inputs — support.pst, the
// mbox, maildir and Evolution fixtures and the fake Graph server — decrypted to an
// unencrypted copy through keyring.DecryptCopy, matches MA-329's frozen golden byte
// for byte under its own masks: every record (its unit placement stripped, its fixity
// kept) and every per-message file. The golden is never regenerated.
func TestAnEncryptedArchivesCopyMatchesThePlaintextGolden(t *testing.T) {
	got := map[string]app.P14Fixture{}
	for name, input := range app.P14Inputs(t, t.TempDir()) {
		dest := encryptedThenCopied(t, func(opts app.Options) (app.Result, error) {
			opts.Inputs = []string{input}
			return app.Run(context.Background(), opts, quiet, nil)
		})
		got[name] = app.P14Collect(t, dest)
	}
	g := app.P14GraphOptions(t)
	dest := encryptedThenCopied(t, func(opts app.Options) (app.Result, error) {
		return app.RunGraph(context.Background(), g, opts, quiet)
	})
	got["graph"] = app.P14Collect(t, dest)
	if !app.P14Compare(t, got) {
		t.Fatal("an encrypted archive's unencrypted copy differs from the plaintext golden (the differences are listed above)")
	}
}
