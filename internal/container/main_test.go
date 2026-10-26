package container

import (
	"os"
	"testing"

	"mail-archive-tool/internal/vault"
)

// keys are two real archives' keys, made once (each costs one Argon2id
// derivation, at the cheap test cost, MA-776) in folders this test binary owns
// and removes.
var keys [2]*vault.Key

// keyDirs are the folders holding their key files, so a test can open a copy of a
// key it may destroy (vault.KeySecret).
var keyDirs [2]string

func TestMain(m *testing.M) {
	vault.UseTestCost()
	dir, err := os.MkdirTemp("", "containertest")
	if err != nil {
		panic(err)
	}
	for i := range keys {
		out, err := os.MkdirTemp(dir, "archive")
		if err != nil {
			panic(err)
		}
		keyDirs[i] = out
		p, err := vault.PrepareCreate(out, "container test passphrase")
		if err != nil {
			panic(err)
		}
		if keys[i], err = p.Commit(); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
