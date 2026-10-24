package vault

import "encoding/json"

// EncryptedManifestVersion is the manifest format an encrypted archive's
// cleartext envelope declares (A4, C8). It is far above any plaintext manifest
// version, so a MailArchive that predates encryption — reading the envelope as
// a manifest — parses it, reaches its refuse-newer guard and asks to be
// upgraded, instead of failing to parse ciphertext and suggesting the manifest
// be deleted (which would re-export the mail in plaintext into the archive).
// The shipped v2 binary has no refuse-newer guard and is not protected by any
// format choice.
const EncryptedManifestVersion = 100

// ManifestEnvelope is `.mailarchive-manifest.json` for an encrypted archive: a
// small cleartext header; the real manifest lives in the chunked container its
// Pointer names (A11).
type ManifestEnvelope struct {
	Version   int    `json:"version"`
	Encrypted bool   `json:"encrypted"`
	ArchiveID string `json:"archive_id"`
	Pointer   string `json:"pointer"`
}

// NewManifestEnvelope is the envelope for archive id whose manifest container
// pointer file is pointer.
func NewManifestEnvelope(id ID, pointer string) ManifestEnvelope {
	return ManifestEnvelope{Version: EncryptedManifestVersion, Encrypted: true, ArchiveID: id.String(), Pointer: pointer}
}

// Marshal is the envelope's on-disk bytes.
func (e ManifestEnvelope) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
