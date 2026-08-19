package source

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// covers: MA-31, R10
// Malformed input must never crash the run: every input yields a message (real
// or a stub), never nil and never a panic escaping the reader.
func TestMalformedMessagesDoNotCrash(t *testing.T) {
	inputs := map[string][]byte{
		"nil":              nil,
		"header-only":      []byte("From \r\nSubject: x\r\n"),
		"broken multipart": []byte("From \r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n"),
		"binary garbage":   {0x00, 0x01, 0x02, 0x03, 0xff, 0xfe},
		"huge non-message": bytes.Repeat([]byte("A"), 200000),
		"bad encoding":     []byte("From \r\nContent-Transfer-Encoding: base64\r\nContent-Type: text/plain\r\n\r\n!!!not base64!!!\r\n"),
	}
	for name, in := range inputs {
		m := safeParseMessage(in)
		if m == nil {
			t.Errorf("%s: safeParseMessage returned nil — every message must be exported or stubbed, never dropped", name)
		}
	}
}

// covers: MA-36, R9
// Reading a mail store must not modify it.
func TestSourceReadOnly(t *testing.T) {
	store := filepath.Join(t.TempDir(), "acct")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "Inbox"), []byte(sampleMbox), 0o644); err != nil {
		t.Fatal(err)
	}

	before := hashTree(t, store)

	src, err := Open(store)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := src.Walk(func(fp []string, m *model.Message) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	src.Close()
	assure.Reached(t, count, "messages read")

	if after := hashTree(t, store); after != before {
		t.Errorf("reading the store changed its bytes (R9 violated): %s != %s", before, after)
	}
}

func hashTree(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		h.Write([]byte(p))
		h.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}
