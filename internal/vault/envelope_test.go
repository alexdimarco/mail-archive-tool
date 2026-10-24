package vault

import (
	"errors"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-712, R37
func TestAnOlderReaderAsksToUpgradeOnTheEncryptedManifestEnvelope(t *testing.T) {
	const name = ".mailarchive-manifest.json"
	env, err := NewManifestEnvelope(fixedID(), ".mailarchive-manifest.ptr").Marshal()
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.LoadFrom(name, env)
	var newer *state.NewerFormatError
	if !errors.As(err, &newer) {
		t.Fatalf("the shipped manifest reader must refuse the envelope as newer, got %v", err)
	}
	if newer.Version != EncryptedManifestVersion || newer.Understood >= EncryptedManifestVersion {
		t.Fatalf("envelope version %d vs understood %d: the envelope must sit above every plaintext version", newer.Version, newer.Understood)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(name, "upgrade"), assure.Forbid("delete it"))

	// Contrast: ciphertext in the manifest's place is the failure A4 prevents.
	k := testKey(t, fixedID(), 0x42)
	ct, err := k.SealPointer(ContainerManifest, 1, []byte(`{"entries":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.LoadFrom(name, ct)
	if err == nil || !strings.Contains(err.Error(), "delete it") {
		t.Fatalf("raw ciphertext is expected to hit the corrupt/delete-it path the envelope avoids, got %v", err)
	}
}
