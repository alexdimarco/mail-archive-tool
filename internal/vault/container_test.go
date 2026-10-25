package vault

import (
	"encoding/hex"
	"errors"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-749, R37
// The history is the third container kind (Phase 2b, A21): its chunk and pointer AADs
// are the frozen MAC1/MAP1 layouts with kind byte 03, and a history chunk never opens
// as a manifest or index chunk, nor they as it.
func TestHistoryIsTheThirdContainerKind(t *testing.T) {
	id := fixedID()
	const idHex = "000102030405060708090a0b0c0d0e0f"
	for name, c := range map[string]struct {
		got  []byte
		want string
	}{
		"history chunk":   {chunkAAD(id, ContainerHistory, 7, 3), "4d414331" + "0001" + idHex + "03" + "0000000000000007" + "00000003"},
		"history pointer": {pointerAAD(id, ContainerHistory, 9), "4d415031" + "0001" + idHex + "03" + "0000000000000009"},
	} {
		if got := hex.EncodeToString(c.got); got != c.want {
			t.Errorf("%s AAD:\n got  %s\n want %s", name, got, c.want)
		}
	}

	k := testKey(t, id, 0x42)
	sealed, err := k.SealChunk(ContainerHistory, 2, 0, []byte(`{"run":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := k.OpenChunk(ContainerHistory, 2, 0, sealed); err != nil || string(got) != `{"run":1}` {
		t.Fatalf("positive twin: a history chunk opens as history: %q %v", got, err)
	}
	for _, other := range []ContainerKind{ContainerManifest, ContainerIndex} {
		if _, err := k.OpenChunk(other, 2, 0, sealed); !errors.Is(err, ErrAuth) {
			t.Errorf("a history chunk opened as kind %d: %v", other, err)
		}
		them, err := k.SealChunk(other, 2, 0, []byte(`{"run":1}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := k.OpenChunk(ContainerHistory, 2, 0, them); !errors.Is(err, ErrAuth) {
			t.Errorf("a kind-%d chunk opened as history: %v", other, err)
		}
	}
}

// covers: MA-749, R37
// Every container operation refuses a kind outside {manifest, index, history} with the
// typed ErrContainerKind, naming the kind — so no chunk or pointer is ever sealed or
// read under a role the format does not define.
func TestAnUnknownContainerKindIsRefused(t *testing.T) {
	k := testKey(t, fixedID(), 0x42)
	good, _ := k.SealChunk(ContainerManifest, 1, 0, []byte("x"))
	for _, bad := range []ContainerKind{0, 4, 255} {
		for name, try := range map[string]func() ([]byte, error){
			"SealChunk":   func() ([]byte, error) { return k.SealChunk(bad, 1, 0, []byte("x")) },
			"OpenChunk":   func() ([]byte, error) { return k.OpenChunk(bad, 1, 0, good) },
			"SealPointer": func() ([]byte, error) { return k.SealPointer(bad, 1, []byte("x")) },
			"OpenPointer": func() ([]byte, error) { return k.OpenPointer(bad, 1, good) },
		} {
			out, err := try()
			if !errors.Is(err, ErrContainerKind) {
				t.Errorf("%s(kind %d): want ErrContainerKind, got %v", name, bad, err)
				continue
			}
			rc, msg := refusal(err)
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names("container kind"))
			if out != nil {
				t.Errorf("%s(kind %d) refused but returned %d bytes", name, bad, len(out))
			}
		}
	}
	for _, kind := range []ContainerKind{ContainerManifest, ContainerIndex, ContainerHistory} {
		if _, err := k.SealPointer(kind, 1, []byte("x")); err != nil {
			t.Errorf("positive twin: kind %d seals: %v", kind, err)
		}
	}
}

// covers: MA-749, R37
// A record's manifest bucket (Phase 2b §2) is the big-endian first 8 bytes of
// HMAC-SHA256, under the HKDF-SHA256 sub-key "mailarchive manifest buckets v1", over
// the record key, mod n. The sub-key and the buckets of fixed keys are pinned against
// an independent implementation (Python hashlib/hmac, which also reproduces the
// pinned data sub-key).
func TestBucketsAreTheFrozenKeyedDerivation(t *testing.T) {
	k := testKey(t, fixedID(), 0x42)
	if got := hex.EncodeToString(k.buckets[:]); got != "849364a5eda5a0d872664a89a4f0aa8bf2c1bf3a1c3d1946e6638dedb806d86c" {
		t.Fatalf("buckets sub-key (HKDF-SHA256, info \"mailarchive manifest buckets v1\") is %s", got)
	}
	for _, c := range []struct {
		key     string
		n, want uint32
	}{
		{"store/Inbox/msg-1", 16, 13}, {"store/Inbox/msg-1", 1024, 253},
		{"a", 16, 10}, {"a", 1024, 378},
		{"0123456789abcdef", 16, 2}, {"0123456789abcdef", 1024, 690},
	} {
		got, err := k.Bucket(c.key, c.n)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("Bucket(%q, %d) = %d, want %d", c.key, c.n, got, c.want)
		}
	}
}

// covers: MA-749, R37
// Buckets are keyed: another archive's key places the same records differently, so
// someone who knows a message's key cannot tell which chunk holds it. A bucket count
// that is not a power of two of at least 16 (up to 2^24), and an empty record key,
// are refused with the typed ErrBucket; a destroyed key refuses.
func TestBucketsAreKeyedAndTheirCountIsBounded(t *testing.T) {
	k := testKey(t, fixedID(), 0x42)
	other := testKey(t, fixedID(), 0x43)
	differ := 0
	for i := 0; i < 64; i++ {
		key := "store/Inbox/msg-" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		a, err := k.Bucket(key, 1024)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := other.Bucket(key, 1024)
		if a >= 1024 {
			t.Fatalf("Bucket(%q, 1024) = %d, out of range", key, a)
		}
		if a != b {
			differ++
		}
	}
	if differ < 60 {
		t.Fatalf("only %d of 64 records changed bucket under another archive's key: buckets are not keyed", differ)
	}

	for _, n := range []uint32{0, 1, 8, 15, 17, 24, 1000, 1 << 25} {
		_, err := k.Bucket("a", n)
		if !errors.Is(err, ErrBucket) {
			t.Errorf("Bucket count %d: want ErrBucket, got %v", n, err)
			continue
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("bucket"))
	}
	for _, n := range []uint32{16, 32, 1 << 24} {
		if _, err := k.Bucket("a", n); err != nil {
			t.Errorf("positive twin: %d buckets: %v", n, err)
		}
	}
	if _, err := k.Bucket("", 16); !errors.Is(err, ErrBucket) {
		t.Errorf("an empty record key: want ErrBucket, got %v", err)
	}
	k.Destroy()
	if _, err := k.Bucket("a", 16); !errors.Is(err, ErrKeyDestroyed) {
		t.Errorf("a destroyed key: want ErrKeyDestroyed, got %v", err)
	}
}
