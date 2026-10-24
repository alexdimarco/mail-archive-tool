package vault

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Known answers for the frozen formats (A2, A3, A7). Round-trip tests cannot
// see a change made symmetrically to writer and reader; these can, because the
// answers were computed once and are now fixed. The HKDF sub-keys and the
// recovery-key body were cross-checked against an independent implementation
// (Python hashlib/hmac/base64) when pinned.

// covers: MA-716, R37
func TestSubKeysAreTheFrozenHKDFOutputs(t *testing.T) {
	k := testKey(t, fixedID(), 0x42)
	for name, c := range map[string]struct{ got, want string }{
		"data sub-key (HKDF-SHA256, info \"mailarchive data v1\")":        {hex.EncodeToString(k.data[:]), "997ca12ac3eab098f3dca58021c7d4c25be8d181dc63cf8207e258356a225d73"},
		"MAC sub-key (HKDF-SHA256, info \"mailarchive vault header v1\")": {hex.EncodeToString(k.mac[:]), "36314e715e157b2deee66cbf3efc29280feca7de9c89bf521424aacf5752768a"},
	} {
		if c.got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", name, c.got, c.want)
		}
	}
}

// covers: MA-716, R37
func TestKeyEncryptionKeyIsTheFrozenArgon2idOutput(t *testing.T) {
	lowCost(t)
	kek, err := deriveKEK([]byte(testPassword), make([]byte, saltSize), Params{MemoryKiB: 64, Time: 2, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(kek), "07a502f2cdbae66d17c5758b8e4b86fd98f02dcdb856ce12bc3cda6873243b2e"; got != want {
		t.Fatalf("Argon2id(password, zero salt, t=2, m=64 KiB, p=1):\n got  %s\n want %s", got, want)
	}
}

// covers: MA-716, R37
func TestPasswordNormalisationIsFrozenAsNFC(t *testing.T) {
	// A ligature (NFKC would split it), a decomposed accent (NFC composes it):
	// only NFC turns this into exactly U+FB01 U+00E9.
	got := passwordBytes("ﬁé")
	if want := []byte("ﬁé"); string(got) != string(want) {
		t.Fatalf("password bytes %x, want NFC %x — the password normalisation is part of the format", got, want)
	}
}

// covers: MA-716, R37
func TestRecoveryKeyRenderingIsFrozen(t *testing.T) {
	var rk RecoveryKey
	for i := range rk {
		rk[i] = byte(i)
	}
	if got, want := rk.String(), "AAAQ-EAYE-AUDA-OCAJ-BIFQ-YDIO-B4Y"; got != want {
		t.Fatalf("recovery key 00..0f renders as %q, want %q", got, want)
	}
}

// covers: MA-716, R37
func TestAVaultWrittenByThisFormatStillOpens(t *testing.T) {
	lowCost(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "vault-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := Parse(raw)
	if err != nil {
		t.Fatalf("the pinned vault must parse: %v", err)
	}
	if got, want := hex.EncodeToString(v.mac[:]), "874bb16b2d2b927e2ebd7751ff43e4c2f4ef344eb4fb4a9e0b244f12c6648c5c"; got != want {
		t.Fatalf("pinned header MAC changed: %s", got)
	}
	k, err := v.Unlock(testPassword)
	if err != nil {
		t.Fatalf("the pinned vault must keep opening with its password (header MAC, wrap AAD and key schedule unchanged): %v", err)
	}
	if got, want := hex.EncodeToString(k.master[:]), "101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f"; got != want {
		t.Fatalf("the pinned vault unwrapped master %s, want %s", got, want)
	}
	if _, err := v.UnlockRecovery("GAYT-EMZU-GU3D-OOBZ-HI5T-YPJ6-H4Y"); err != nil {
		t.Fatalf("the pinned vault must keep opening with its recovery key: %v", err)
	}
	if want := v.computeMAC(k); want != v.mac {
		t.Fatal("computeMAC no longer reproduces the pinned header MAC (the canonical header encoding changed)")
	}
	again, err := v.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(raw) {
		t.Fatal("Marshal no longer reproduces the pinned vault bytes (the on-disk JSON layout changed)")
	}
}
