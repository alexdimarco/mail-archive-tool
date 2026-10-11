package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pst "github.com/mooijtech/go-pst/v6/pkg"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// The helpers of the local-run fault tests (MA-318..MA-320, MA-324): byte-patched
// copies of testdata/support.pst, the F23 PoC technique, and ledger readers.

const supportPST = "../../testdata/support.pst"

func pstFixtureData(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(supportPST)
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	return b
}

// pstMessageNIDs are the fixture's 17 messages' NIDs, in the order go-pst's own
// iterators yield them (Drafts' six, then Sent Messages' eleven).
func pstMessageNIDs(t *testing.T, data []byte) []pst.Identifier {
	t.Helper()
	f, err := pst.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var nids []pst.Identifier
	err = f.WalkFolders(func(fo *pst.Folder) error {
		it, ierr := fo.GetMessageIterator()
		if ierr != nil {
			return nil
		}
		for it.Next() {
			nids = append(nids, it.Value().Identifier)
		}
		return it.Err()
	})
	if err != nil || len(nids) != 17 {
		t.Fatalf("the fixture's messages: %d, err %v; want 17", len(nids), err)
	}
	return nids
}

// corruptNode returns a copy of data with the first byte of nid's data block XORed:
// go-pst's GetMessage then fails for that one node (the F23 PoC mutation).
func corruptNode(t *testing.T, data []byte, nid pst.Identifier) []byte {
	t.Helper()
	f, err := pst.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bn, err := f.GetDataBTreeNode(nid)
	if err != nil {
		t.Fatalf("NID %d: %v", nid, err)
	}
	mut := append([]byte(nil), data...)
	mut[bn.FileOffset] ^= 0xFF
	return mut
}

// breakAttachmentTable returns a copy of data with the first byte of nid's
// attachment-table block XORed: the message still reads (envelope and body intact)
// and only its attachment table fails.
func breakAttachmentTable(t *testing.T, data []byte, nid pst.Identifier) []byte {
	t.Helper()
	f, err := pst.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	nb, err := f.GetNodeBTreeNode(nid)
	if err != nil {
		t.Fatal(err)
	}
	lds, err := f.GetLocalDescriptors(nb)
	if err != nil {
		t.Fatal(err)
	}
	for _, ld := range lds {
		if ld.Identifier.GetType() != pst.IdentifierTypeAttachmentTable {
			continue
		}
		bn, err := f.GetBlockBTreeNode(ld.DataIdentifier)
		if err != nil {
			t.Fatal(err)
		}
		mut := append([]byte(nil), data...)
		mut[bn.FileOffset] ^= 0xFF
		return mut
	}
	t.Fatalf("NID %d has no attachment table", nid)
	return nil
}

// writeData writes data as dir/name and returns the path.
func writeData(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runLocal is one incremental run of inputs into out.
func runLocal(t *testing.T, out string, inputs ...string) (Result, error) {
	t.Helper()
	return Run(context.Background(), Options{Inputs: inputs, Out: out, Mode: export.Incremental}, discard(), nil)
}

func runLocalFull(t *testing.T, out string, inputs ...string) (Result, error) {
	t.Helper()
	return Run(context.Background(), Options{Inputs: inputs, Out: out, Mode: export.Full}, discard(), nil)
}

func loadArchive(t *testing.T, out string) *state.Manifest {
	t.Helper()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func ledgerOf(t *testing.T, out string) []state.CaptureGap {
	t.Helper()
	return loadArchive(t, out).CaptureGapsSorted()
}

// ageLedger moves every ledger observation back by d, so the next run is a later
// day's: the ledger counts a run once per UTC day (OR5).
func ageLedger(t *testing.T, out string, d time.Duration) {
	t.Helper()
	m := loadArchive(t, out)
	for k, g := range m.CaptureGaps {
		g.FirstRun, g.LastRun = g.FirstRun.Add(-d), g.LastRun.Add(-d)
		m.CaptureGaps[k] = g
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
}

// messageFiles hashes every page, zip and preserved original the archive holds,
// by archive-relative path: the files a recovery or a refusal must neither write
// nor delete.
func messageFiles(t *testing.T, out string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".html") && !strings.HasSuffix(name, "-attachments.zip") && !strings.HasSuffix(name, ".eml") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(b)
		rel, _ := filepath.Rel(out, p)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// tokenOf is the store token a run of path would give its store: it runs path into
// a scratch archive and reads the token back. The token depends on the source's
// path and name, not on the archive, so the real archive's tests can plant files
// or seed ledger entries under it before their own first run.
func tokenOf(t *testing.T, path string) string {
	t.Helper()
	scratch := tmpDir(t)
	if _, err := runLocal(t, scratch, path); err != nil {
		t.Fatalf("the scratch run of %s: %v", path, err)
	}
	m := loadArchive(t, scratch)
	for id, tok := range m.Stores {
		if id == state.PathSourceID(path) {
			return tok
		}
	}
	t.Fatalf("the scratch run left no token for %s", path)
	return ""
}
