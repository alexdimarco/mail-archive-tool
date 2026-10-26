package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// fakeOriginals is an encrypted archive's side of extract, in memory.
type fakeOriginals struct {
	m         *state.Manifest
	originals map[string][]byte // by record path; absent: no original
}

func (f fakeOriginals) Load() (*state.Manifest, error) { return f.m, nil }

func (f fakeOriginals) Original(rec state.Record) ([]byte, error) {
	b, ok := f.originals[rec.Path]
	if !ok {
		return nil, ErrNoOriginal
	}
	return b, nil
}

// covers: MA-797, R37
// The sealed extract checks each original against its recorded fixity, as the
// plaintext one does (PC13): one that matches is written; one that does not is
// skipped as a fixity mismatch, never written; one with none is "as stored"; a
// record with no original is counted as such (exit 3).
func TestASealedExtractChecksEachOriginalsFixity(t *testing.T) {
	m, err := state.Load(filepath.Join(t.TempDir(), "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	good, bad := []byte("Message-ID: <good@x>\r\n\r\ngood"), []byte("Message-ID: <bad@x>\r\n\r\nbad")
	digest := func(b []byte) *state.FileDigest {
		s := sha256.Sum256(b)
		return &state.FileDigest{SHA256: hex.EncodeToString(s[:]), Size: int64(len(b))}
	}
	sealed := &state.SealedSet{EML: &state.SealedUnit{Rev: 1, Size: 10}}
	m.Add("k1", state.Record{Path: "s/Inbox/good.html", Folder: "Inbox", Sealed: sealed, Fixity: &state.Fixity{EML: digest(good)}})
	m.Add("k2", state.Record{Path: "s/Inbox/bad.html", Folder: "Inbox", Sealed: sealed, Fixity: &state.Fixity{EML: digest(good)}})
	m.Add("k3", state.Record{Path: "s/Inbox/asis.html", Folder: "Inbox", Sealed: sealed})
	m.Add("k4", state.Record{Path: "s/Inbox/none.html", Folder: "Inbox"})
	src := fakeOriginals{m: m, originals: map[string][]byte{"s/Inbox/good.html": good, "s/Inbox/bad.html": bad, "s/Inbox/asis.html": good}}

	out, dest := t.TempDir(), filepath.Join(t.TempDir(), "x")
	rep, err := ExtractSealed(out, FormatEML, dest, false, nil, nil, src)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Emitted != 2 || rep.SkippedFixity != 1 || rep.SkippedNoEML != 1 || rep.EmittedNoFixity != 1 || rep.ExitCode() != ExtractPartial {
		t.Fatalf("report: %+v", rep)
	}
	written, _ := os.ReadFile(filepath.Join(dest, "s", "Inbox", "good.eml"))
	_, err = os.Stat(filepath.Join(dest, "s", "Inbox", "bad.eml"))
	assure.Reached(t, string(written) == string(good) && errors.Is(err, os.ErrNotExist), "the matching original written, the mismatched one never")
}
