package vault

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// VaultFormat is the vault file's own format version. A vault with a higher
// one was written by a newer MailArchive and is refused, never guessed at.
const VaultFormat = 1

// wrappedSize is a wrapped master key: nonce ‖ 32-byte key ‖ tag.
const wrappedSize = nonceSize + keySize + overhead

var (
	// ErrWrongSecret refuses a password or recovery key that does not unlock
	// this archive.
	ErrWrongSecret = errors.New("vault: that password or recovery key does not unlock this archive")
	// ErrTampered refuses a vault whose public fields fail the header MAC: an
	// entry stripped, the archive id swapped, the generation or a cost changed.
	ErrTampered = errors.New("vault: the archive's key file was altered")
	// ErrNewerVault refuses a vault written by a newer MailArchive.
	ErrNewerVault = errors.New("vault: written by a newer MailArchive")
	// ErrCorrupt refuses a vault that does not parse as one.
	ErrCorrupt = errors.New("vault: the archive's key file is damaged")
)

// Entry is one wrap of the master key: under the password, or under the
// recovery key, each with its own salt and cost (A3).
type Entry struct {
	Salt    [saltSize]byte
	Params  Params
	Wrapped []byte
}

// Vault is the key half of the keyless facade (A7): the archive id, a
// generation that rises on every rewrite, the two wrap entries and the header
// MAC. It holds nothing about the archive's contents, names or structure, and
// changes only on create, password change and recovery-key regeneration.
type Vault struct {
	ArchiveID  ID
	Generation uint64
	Password   Entry
	Recovery   Entry
	mac        [sha256.Size]byte
}

// Create makes the vault for a newly encrypted archive: a random archive id
// and master key, wrapped under password and under a new recovery key. The
// recovery key is returned once, for the person to save; it is not stored.
func Create(password string) (*Vault, *Key, RecoveryKey, error) {
	var none RecoveryKey
	if err := checkNewPassword(password); err != nil {
		return nil, nil, none, err
	}
	var id ID
	master := make([]byte, keySize)
	defer clear(master)
	if _, err := io.ReadFull(randReader, id[:]); err != nil {
		return nil, nil, none, fmt.Errorf("vault: draw archive id: %w", err)
	}
	if _, err := io.ReadFull(randReader, master); err != nil {
		return nil, nil, none, fmt.Errorf("vault: draw master key: %w", err)
	}
	rk, err := newRecoveryKey()
	if err != nil {
		return nil, nil, none, err
	}
	v := &Vault{ArchiveID: id, Generation: 1}
	if v.Password, err = wrap(id, entryPassword, passwordBytes(password), master); err != nil {
		return nil, nil, none, err
	}
	if v.Recovery, err = wrap(id, entryRecovery, rk[:], master); err != nil {
		return nil, nil, none, err
	}
	k, err := newKey(id, master)
	if err != nil {
		return nil, nil, none, err
	}
	v.mac = v.computeMAC(k)
	return v, k, rk, nil
}

// wrap derives a fresh-salted KEK from secret and seals master under it.
func wrap(id ID, kind entryKind, secret, master []byte) (Entry, error) {
	e := Entry{Params: floor}
	if _, err := io.ReadFull(randReader, e.Salt[:]); err != nil {
		return e, fmt.Errorf("vault: draw salt: %w", err)
	}
	kek, err := deriveKEK(secret, e.Salt[:], e.Params)
	if err != nil {
		return e, err
	}
	defer clear(kek)
	if e.Wrapped, err = seal(kek, wrapAAD(id, kind), master); err != nil {
		return e, err
	}
	return e, nil
}

// unwrap recovers the master key from e with secret, then proves the whole
// vault header against it before handing the key out.
func (v *Vault) unwrap(kind entryKind, e Entry, secret []byte) (*Key, error) {
	kek, err := deriveKEK(secret, e.Salt[:], e.Params)
	if err != nil {
		return nil, err
	}
	defer clear(kek)
	master, err := open(kek, wrapAAD(v.ArchiveID, kind), e.Wrapped)
	if err != nil {
		return nil, ErrWrongSecret
	}
	defer clear(master)
	k, err := newKey(v.ArchiveID, master)
	if err != nil {
		return nil, err
	}
	if err := v.Verify(k); err != nil {
		k.Destroy()
		return nil, err
	}
	return k, nil
}

// Unlock opens the archive with its password.
func (v *Vault) Unlock(password string) (*Key, error) {
	return v.unwrap(entryPassword, v.Password, passwordBytes(password))
}

// UnlockRecovery opens the archive with its recovery key, as typed or pasted.
func (v *Vault) UnlockRecovery(code string) (*Key, error) {
	rk, err := ParseRecoveryKey(code)
	if err != nil {
		return nil, err
	}
	return v.unwrap(entryRecovery, v.Recovery, rk[:])
}

// Verify proves the vault's header MAC under k: the key belongs to this vault
// and no public field has been changed since a key-holder last wrote it.
func (v *Vault) Verify(k *Key) error {
	if k.id != v.ArchiveID {
		return fmt.Errorf("%w: the key belongs to archive %s, not %s", ErrTampered, k.id, v.ArchiveID)
	}
	want := v.computeMAC(k)
	if !hmac.Equal(want[:], v.mac[:]) {
		return ErrTampered
	}
	return nil
}

// WithPassword is the vault after a password change (G3) or a reset with the
// recovery key (C9): the password entry re-wrapped under newPassword with a
// fresh salt, the recovery entry kept, the generation raised. The master key —
// and so every sealed byte of the archive — is unchanged. k must already be
// proven against v (Unlock/UnlockRecovery do this).
func (v *Vault) WithPassword(k *Key, newPassword string) (*Vault, error) {
	if err := checkNewPassword(newPassword); err != nil {
		return nil, err
	}
	if err := v.Verify(k); err != nil {
		return nil, err
	}
	n := *v
	n.Generation++
	var err error
	if n.Password, err = wrap(v.ArchiveID, entryPassword, passwordBytes(newPassword), k.master[:]); err != nil {
		return nil, err
	}
	n.mac = n.computeMAC(k)
	return &n, nil
}

// WithNewRecovery is the vault with a newly generated recovery key (C9): the
// recovery entry replaced, the password entry kept, the generation raised. The
// old recovery key stops working; the new one is returned once.
func (v *Vault) WithNewRecovery(k *Key) (*Vault, RecoveryKey, error) {
	var none RecoveryKey
	if err := v.Verify(k); err != nil {
		return nil, none, err
	}
	rk, err := newRecoveryKey()
	if err != nil {
		return nil, none, err
	}
	n := *v
	n.Generation++
	if n.Recovery, err = wrap(v.ArchiveID, entryRecovery, rk[:], k.master[:]); err != nil {
		return nil, none, err
	}
	n.mac = n.computeMAC(k)
	return &n, rk, nil
}

// computeMAC is HMAC-SHA256 under the MAC sub-key over the canonical binary
// header (A7): "MAV1" ‖ vault format u16 ‖ archive id ‖ generation u64 ‖ the
// password entry ‖ the recovery entry, each entry as kind u8 ‖ memory u32 ‖
// time u32 ‖ threads u8 ‖ salt ‖ len u16 ‖ wrapped. Fixed order and lengths, so
// no two different headers encode alike.
func (v *Vault) computeMAC(k *Key) [sha256.Size]byte {
	b := []byte("MAV1")
	b = binary.BigEndian.AppendUint16(b, VaultFormat)
	b = append(b, v.ArchiveID[:]...)
	b = binary.BigEndian.AppendUint64(b, v.Generation)
	for _, e := range []struct {
		kind entryKind
		e    Entry
	}{{entryPassword, v.Password}, {entryRecovery, v.Recovery}} {
		b = append(b, byte(e.kind))
		b = binary.BigEndian.AppendUint32(b, e.e.Params.MemoryKiB)
		b = binary.BigEndian.AppendUint32(b, e.e.Params.Time)
		b = append(b, e.e.Params.Threads)
		b = append(b, e.e.Salt[:]...)
		b = binary.BigEndian.AppendUint16(b, uint16(len(e.e.Wrapped)))
		b = append(b, e.e.Wrapped...)
	}
	m := hmac.New(sha256.New, k.mac[:])
	m.Write(b)
	var out [sha256.Size]byte
	copy(out[:], m.Sum(nil))
	return out
}

// The on-disk JSON form. Unknown fields are refused (a newer writer must raise
// VaultFormat), so nothing unauthenticated can ride along in the file.
type vaultFile struct {
	Format     int         `json:"format"`
	ArchiveID  string      `json:"archive_id"`
	Generation uint64      `json:"generation"`
	Entries    []entryFile `json:"entries"`
	MAC        string      `json:"mac"`
}

type entryFile struct {
	Kind      string `json:"kind"`
	KDF       string `json:"kdf"`
	Salt      string `json:"salt"`
	MemoryKiB uint32 `json:"memory_kib"`
	Time      uint32 `json:"time"`
	Threads   uint8  `json:"threads"`
	Wrapped   string `json:"wrapped"`
}

const kdfArgon2id = "argon2id"

var entryKindNames = map[entryKind]string{entryPassword: "password", entryRecovery: "recovery"}

func (e Entry) file(kind entryKind) entryFile {
	return entryFile{
		Kind:      entryKindNames[kind],
		KDF:       kdfArgon2id,
		Salt:      base64.StdEncoding.EncodeToString(e.Salt[:]),
		MemoryKiB: e.Params.MemoryKiB,
		Time:      e.Params.Time,
		Threads:   e.Params.Threads,
		Wrapped:   base64.StdEncoding.EncodeToString(e.Wrapped),
	}
}

// Marshal is the vault's on-disk bytes.
func (v *Vault) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(vaultFile{
		Format:     VaultFormat,
		ArchiveID:  v.ArchiveID.String(),
		Generation: v.Generation,
		Entries:    []entryFile{v.Password.file(entryPassword), v.Recovery.file(entryRecovery)},
		MAC:        hex.EncodeToString(v.mac[:]),
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Parse reads a vault's bytes and validates their SHAPE only: it trusts none of
// it. Authenticity is proven by Unlock/UnlockRecovery/Verify with a key; a
// keyless reader may use the archive id and generation only to recognise and
// order copies.
func Parse(data []byte) (*Vault, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f vaultFile
	if err := dec.Decode(&f); err != nil {
		var probe struct {
			Format int `json:"format"`
		}
		if json.Unmarshal(data, &probe) == nil && probe.Format > VaultFormat {
			return nil, fmt.Errorf("%w (vault format %d; this build understands %d): upgrade MailArchive", ErrNewerVault, probe.Format, VaultFormat)
		}
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: trailing data after the vault", ErrCorrupt)
	}
	switch {
	case f.Format > VaultFormat:
		return nil, fmt.Errorf("%w (vault format %d; this build understands %d): upgrade MailArchive", ErrNewerVault, f.Format, VaultFormat)
	case f.Format < 1:
		return nil, fmt.Errorf("%w: missing or invalid format", ErrCorrupt)
	case f.Generation == 0:
		return nil, fmt.Errorf("%w: missing generation", ErrCorrupt)
	case len(f.Entries) != 2:
		return nil, fmt.Errorf("%w: %d key entries, want exactly a password and a recovery entry", ErrCorrupt, len(f.Entries))
	}
	v := &Vault{Generation: f.Generation}
	var err error
	if v.ArchiveID, err = parseID(f.ArchiveID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	mac, err := hex.DecodeString(f.MAC)
	if err != nil || len(mac) != len(v.mac) {
		return nil, fmt.Errorf("%w: header MAC is not %d hex bytes", ErrCorrupt, len(v.mac))
	}
	copy(v.mac[:], mac)
	seen := map[entryKind]bool{}
	for _, ef := range f.Entries {
		kind, e, err := ef.parse()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if seen[kind] {
			return nil, fmt.Errorf("%w: two %s entries", ErrCorrupt, entryKindNames[kind])
		}
		seen[kind] = true
		if kind == entryPassword {
			v.Password = e
		} else {
			v.Recovery = e
		}
	}
	return v, nil
}

func (ef entryFile) parse() (entryKind, Entry, error) {
	var e Entry
	var kind entryKind
	for k, name := range entryKindNames {
		if ef.Kind == name {
			kind = k
		}
	}
	if kind == 0 {
		return 0, e, fmt.Errorf("unknown key entry kind %q", ef.Kind)
	}
	if ef.KDF != kdfArgon2id {
		return 0, e, fmt.Errorf("%s entry: unknown key derivation %q", ef.Kind, ef.KDF)
	}
	salt, err := base64.StdEncoding.DecodeString(ef.Salt)
	if err != nil || len(salt) != saltSize {
		return 0, e, fmt.Errorf("%s entry: salt is not %d base64 bytes", ef.Kind, saltSize)
	}
	copy(e.Salt[:], salt)
	if e.Wrapped, err = base64.StdEncoding.DecodeString(ef.Wrapped); err != nil || len(e.Wrapped) != wrappedSize {
		return 0, e, fmt.Errorf("%s entry: wrapped key is not %d base64 bytes", ef.Kind, wrappedSize)
	}
	e.Params = Params{MemoryKiB: ef.MemoryKiB, Time: ef.Time, Threads: ef.Threads}
	return kind, e, nil
}
