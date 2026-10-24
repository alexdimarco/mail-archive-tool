package vault

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"

	"mail-archive-tool/internal/assure"
)

// covers: MA-703, R37
func TestEverySealDrawsAFreshNonce(t *testing.T) {
	k := testKey(t, fixedID(), 0x42)
	rec := recordDraws(t)
	pt := []byte("the same message, sealed for the same place")

	type sealFn func() ([]byte, error)
	seals := map[string]sealFn{
		"unit":    func() ([]byte, error) { return k.SealUnit(UnitMessage, "msg-1", pt) },
		"chunk":   func() ([]byte, error) { return k.SealChunk(ContainerIndex, 7, 3, pt) },
		"pointer": func() ([]byte, error) { return k.SealPointer(ContainerManifest, 9, pt) },
	}
	for name, fn := range seals {
		rec.draws = nil
		first, err := fn()
		if err != nil {
			t.Fatalf("%s seal: %v", name, err)
		}
		second, err := fn() // the re-seal of an unchanged place
		if err != nil {
			t.Fatalf("%s re-seal: %v", name, err)
		}
		draws := assure.Reached(t, rec.draws, name+" nonce draws")
		if len(draws) != 2 || len(draws[0]) != nonceSize || len(draws[1]) != nonceSize {
			t.Fatalf("%s: want one %d-byte draw per seal, got %d draws", name, nonceSize, len(draws))
		}
		if !bytes.Equal(first[:nonceSize], draws[0]) || !bytes.Equal(second[:nonceSize], draws[1]) {
			t.Fatalf("%s: the stored nonce is not the bytes drawn for that seal", name)
		}
		if bytes.Equal(first[:nonceSize], second[:nonceSize]) || bytes.Equal(first, second) {
			t.Fatalf("%s: re-sealing the same place repeated the nonce", name)
		}
		if len(first) != nonceSize+len(pt)+overhead {
			t.Fatalf("%s: sealed length %d, want nonce+plaintext+tag = %d", name, len(first), nonceSize+len(pt)+overhead)
		}
	}
}

// covers: MA-703, R37
func TestSealIsXChaCha20Poly1305UnderTheDataKey(t *testing.T) {
	id := fixedID()
	k := testKey(t, id, 0x42)
	pt := []byte("interop")
	sealed, err := k.SealUnit(UnitAttachment, "msg-1", pt)
	if err != nil {
		t.Fatal(err)
	}
	a, err := chacha20poly1305.NewX(k.data[:])
	if err != nil {
		t.Fatal(err)
	}
	aad, _ := unitAAD(id, UnitAttachment, "msg-1")
	got, err := a.Open(nil, sealed[:chacha20poly1305.NonceSizeX], sealed[chacha20poly1305.NonceSizeX:], aad)
	if err != nil {
		t.Fatalf("the standard XChaCha20-Poly1305 open under the data key and A2 AAD failed: %v", err)
	}
	if string(got) != string(pt) {
		t.Fatalf("decrypted %q, want %q", got, pt)
	}
	if bytes.Equal(k.data[:], k.master[:]) || bytes.Equal(k.data[:], k.mac[:]) {
		t.Fatal("the data key must be a derived sub-key, distinct from the master and MAC keys")
	}
}

// covers: MA-703, R37
func TestOpenRefusesAnyChangeToSealedBytes(t *testing.T) {
	k := testKey(t, fixedID(), 0x42)
	sealed, err := k.SealChunk(ContainerManifest, 1, 0, []byte("manifest chunk zero"))
	if err != nil {
		t.Fatal(err)
	}
	// Positive twin: unchanged, it opens.
	if got, err := k.OpenChunk(ContainerManifest, 1, 0, sealed); err != nil || string(got) != "manifest chunk zero" {
		t.Fatalf("an unchanged seal must open: %q, %v", got, err)
	}
	for i := range sealed {
		bad := bytes.Clone(sealed)
		bad[i] ^= 0x01
		if _, err := k.OpenChunk(ContainerManifest, 1, 0, bad); !errors.Is(err, ErrAuth) {
			t.Fatalf("flipping byte %d: want ErrAuth, got %v", i, err)
		}
	}
	for _, n := range []int{0, 1, nonceSize, nonceSize + overhead - 1, len(sealed) - 1} {
		if _, err := k.OpenChunk(ContainerManifest, 1, 0, sealed[:n]); !errors.Is(err, ErrAuth) {
			t.Fatalf("truncated to %d bytes: want ErrAuth, got %v", n, err)
		}
	}
}

// covers: MA-703, R37
func TestSealFailsClosedWithoutRandomness(t *testing.T) {
	k := testKey(t, fixedID(), 0x42)
	orig := randReader
	randReader = failingReader{}
	t.Cleanup(func() { randReader = orig })
	out, err := k.SealUnit(UnitMessage, "msg-1", []byte("x"))
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("nonce"))
	if out != nil {
		t.Fatalf("a failed seal returned %d bytes; it must return nothing", len(out))
	}
}

// covers: MA-704, R37
func TestAADLayoutsAreTheFrozenBytes(t *testing.T) {
	id := fixedID()
	const idHex = "000102030405060708090a0b0c0d0e0f"
	unit, err := unitAAD(id, UnitMessage, "m1")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		got  []byte
		want string
	}{
		"unit":    {unit, "4d415531" + "0001" + idHex + "01" + "0002" + "6d31"},
		"chunk":   {chunkAAD(id, ContainerIndex, 7, 3), "4d414331" + "0001" + idHex + "02" + "0000000000000007" + "00000003"},
		"pointer": {pointerAAD(id, ContainerManifest, 9), "4d415031" + "0001" + idHex + "01" + "0000000000000009"},
		"wrap":    {wrapAAD(id, entryRecovery), "4d415731" + "0001" + idHex + "02"},
	}
	for name, c := range cases {
		if got := hex.EncodeToString(c.got); got != c.want {
			t.Errorf("%s AAD:\n got  %s\n want %s", name, got, c.want)
		}
	}
}

// covers: MA-704, R37
func TestSealedBytesCannotMove(t *testing.T) {
	id := fixedID()
	k := testKey(t, id, 0x42)
	otherID := id
	otherID[0] ^= 0xff
	sameMasterOtherArchive := testKey(t, otherID, 0x42)

	unit, err := k.SealUnit(UnitMessage, "msg-1", []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.OpenUnit(UnitMessage, "msg-1", unit); err != nil {
		t.Fatalf("positive twin: the unit must open in its own place: %v", err)
	}
	moves := map[string]func() error{
		"another unit id":   func() error { _, e := k.OpenUnit(UnitMessage, "msg-2", unit); return e },
		"another unit kind": func() error { _, e := k.OpenUnit(UnitAttachment, "msg-1", unit); return e },
		"another archive":   func() error { _, e := sameMasterOtherArchive.OpenUnit(UnitMessage, "msg-1", unit); return e },
		"opened as a chunk": func() error { _, e := k.OpenChunk(ContainerIndex, 0, 0, unit); return e },
		"opened as pointer": func() error { _, e := k.OpenPointer(ContainerIndex, 0, unit); return e },
		"unit id prefix":    func() error { _, e := k.OpenUnit(UnitMessage, "msg-", unit); return e },
		"unit id extension": func() error { _, e := k.OpenUnit(UnitMessage, "msg-1x", unit); return e },
	}
	chunk, _ := k.SealChunk(ContainerIndex, 5, 2, []byte("rows"))
	moves["chunk at another index"] = func() error { _, e := k.OpenChunk(ContainerIndex, 5, 3, chunk); return e }
	moves["chunk at another generation"] = func() error { _, e := k.OpenChunk(ContainerIndex, 4, 2, chunk); return e }
	moves["chunk in another container"] = func() error { _, e := k.OpenChunk(ContainerManifest, 5, 2, chunk); return e }
	ptr, _ := k.SealPointer(ContainerManifest, 5, []byte("chunk list"))
	moves["pointer at another generation"] = func() error { _, e := k.OpenPointer(ContainerManifest, 6, ptr); return e }
	moves["pointer of another container"] = func() error { _, e := k.OpenPointer(ContainerIndex, 5, ptr); return e }

	for name, try := range moves {
		if err := try(); !errors.Is(err, ErrAuth) {
			t.Errorf("%s: want ErrAuth, got %v", name, err)
		}
	}
}

// covers: MA-704, R37
func TestUnitIDMustFitItsLengthPrefix(t *testing.T) {
	k := testKey(t, fixedID(), 0x42)
	if _, err := k.SealUnit(UnitMessage, strings.Repeat("a", maxUnitIDLen), []byte("x")); err != nil {
		t.Fatalf("positive twin: a %d-byte unit id fits: %v", maxUnitIDLen, err)
	}
	for _, id := range []string{"", strings.Repeat("a", maxUnitIDLen+1)} {
		_, err := k.SealUnit(UnitMessage, id, []byte("x"))
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("unit id"))
	}
}
