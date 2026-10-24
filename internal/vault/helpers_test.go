package vault

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// testPassword clears the 12-character floor.
const testPassword = "correct horse battery"

// lowCost runs the test at a cheap Argon2id cost so the suite stays fast; the
// shipped floor is pinned by TestShippedKDFFloor and exercised end-to-end by
// TestFullCostVaultRoundTrip. Tests that call it must not run in parallel.
func lowCost(t *testing.T) {
	t.Helper()
	orig := floor
	floor = Params{MemoryKiB: 64, Time: 2, Threads: 1}
	t.Cleanup(func() { floor = orig })
}

// refusal maps an error to the (code, message) pair assure.Refused expects: a
// nil error is code 0, so a refusal that did not happen fails the code check.
func refusal(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	return 1, err.Error()
}

// recordingReader passes reads through to the real source and records each
// read's bytes, so a test can see every nonce, salt and key that was drawn.
type recordingReader struct {
	src   io.Reader
	draws [][]byte
}

func (r *recordingReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	r.draws = append(r.draws, bytes.Clone(p[:n]))
	return n, err
}

// recordDraws swaps randReader for a recorder for the test's duration.
func recordDraws(t *testing.T) *recordingReader {
	t.Helper()
	orig := randReader
	r := &recordingReader{src: orig}
	randReader = r
	t.Cleanup(func() { randReader = orig })
	return r
}

// failingReader is a random source that has failed.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy source unavailable") }

// fixedID is a recognisable archive id: bytes 0x00..0x0f.
func fixedID() ID {
	var id ID
	for i := range id {
		id[i] = byte(i)
	}
	return id
}

// testKey builds an unlocked key for id over a fixed master key.
func testKey(t *testing.T, id ID, masterByte byte) *Key {
	t.Helper()
	k, err := newKey(id, bytes.Repeat([]byte{masterByte}, keySize))
	if err != nil {
		t.Fatalf("newKey: %v", err)
	}
	return k
}

// createAt prepares and commits a vault in one step. Test-only on purpose: a
// product caller must show the recovery key and have it confirmed between
// PrepareCreate and Commit.
func createAt(out, password string) (*Key, RecoveryKey, error) {
	p, err := PrepareCreate(out, password)
	if err != nil {
		return nil, RecoveryKey{}, err
	}
	k, err := p.Commit()
	if err != nil {
		p.Discard()
		return nil, RecoveryKey{}, err
	}
	return k, p.RecoveryKey(), nil
}

// regenerateRecoveryAt prepares and commits a new recovery key in one step,
// returning it also alongside ErrPartiallySaved. Test-only, like createAt.
func regenerateRecoveryAt(out string, want ID, current Secret) (RecoveryKey, error) {
	p, err := PrepareNewRecovery(out, want, current)
	if err != nil {
		return RecoveryKey{}, err
	}
	if err := p.Commit(); err != nil {
		if errors.Is(err, ErrPartiallySaved) {
			return p.RecoveryKey(), err
		}
		p.Discard()
		return RecoveryKey{}, err
	}
	return p.RecoveryKey(), nil
}

// newArchive creates an encrypted archive folder (a vault only) at the low
// test cost and returns the folder, the key and the recovery key.
func newArchive(t *testing.T) (string, *Key, RecoveryKey) {
	t.Helper()
	out := t.TempDir()
	k, rk, err := createAt(out, testPassword)
	if err != nil {
		t.Fatalf("createAt: %v", err)
	}
	return out, k, rk
}

// readFile reads name in out, failing the test if it cannot.
func readFile(t *testing.T, out, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// writeFile writes name in out, failing the test if it cannot.
func writeFile(t *testing.T, out, name string, b []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(out, name), b, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// removeFile removes name in out, failing the test if it cannot.
func removeFile(t *testing.T, out, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(out, name)); err != nil {
		t.Fatalf("remove %s: %v", name, err)
	}
}

// snapshot is both vault copies as bytes (nil when absent).
func snapshot(out string) [2][]byte {
	var s [2][]byte
	for i, name := range []string{FileName, BackupName} {
		s[i], _ = os.ReadFile(filepath.Join(out, name))
	}
	return s
}
