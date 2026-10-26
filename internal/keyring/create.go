package keyring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// CreateOptions say how a new encrypted archive is made.
type CreateOptions struct {
	// Remember files the new archive's key in Store (on by default in every front
	// end, D1).
	Remember bool
	Store    vault.Store
	// CredentialPaths are the caller's own settings or secret files (a -config
	// folder, a client-secret file): none may be inside the new archive, nor the
	// archive inside one (P2a-C14).
	CredentialPaths []string
}

// PendingCreate is a new encrypted archive whose key file is made but not written:
// the recovery key is shown and confirmed before Commit (K2). Every way out before
// Commit calls Discard. It prints redacted.
type PendingCreate struct {
	out string
	p   *vault.PendingCreate
	o   CreateOptions
}

// Format prints a pending create without its keys.
func (p *PendingCreate) Format(f fmt.State, _ rune) {
	fmt.Fprintf(f, "keyring.PendingCreate{%s, redacted}", p.out)
}

// RecoveryKey is the recovery key to show once, in its written form.
func (p *PendingCreate) RecoveryKey() string { return p.p.RecoveryKey().String() }

// ArchiveID is the new archive's id.
func (p *PendingCreate) ArchiveID() vault.ID { return p.p.ArchiveID() }

// Discard forgets the uncommitted key file; the folder is left as it was.
func (p *PendingCreate) Discard() { p.p.Discard() }

// Create prepares a new encrypted archive in out (§5.1), refusing — before any
// key derivation — a folder that holds files, is at or inside an encrypted
// archive or a recorded folder, or holds or lies in MailArchive's settings, scratch
// or credential folders, and any create while this computer's record cannot be
// used. A missing folder is made. The password floor is checked before deriving.
func Create(out, password string, o CreateOptions) (*PendingCreate, error) {
	abs, err := filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	out = filepath.Clean(abs)
	if _, err := encrecord.Load(); err != nil {
		return nil, refuseUnusableRecord(out, err)
	}
	if err := requireEmpty(out); err != nil {
		return nil, err
	}
	if res, err := encmark.At(out); res.Encrypted {
		return nil, fmt.Errorf("%s cannot hold a new encrypted archive: %s", out, res.Why)
	} else if err != nil {
		return nil, fmt.Errorf("%s cannot hold a new encrypted archive: whether it is inside one cannot be told: %w", out, err)
	}
	if err := refuseSettingsOverlap(out, o.CredentialPaths); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	deriving.Lock()
	p, err := prepareCreate(out, password)
	deriving.Unlock()
	if err != nil {
		return nil, err
	}
	return &PendingCreate{out: out, p: p, o: o}, nil
}

// requireEmpty refuses a folder holding anything but operating-system litter. A
// folder holding a key file is one whose setting up was not finished.
func requireEmpty(out string) error {
	entries, err := os.ReadDir(out)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s cannot hold a new encrypted archive: %w", out, err)
	}
	for _, e := range entries {
		switch {
		case e.Name() == vault.FileName || e.Name() == vault.BackupName:
			return fmt.Errorf("%w: %s already holds an encrypted archive's key file — if setting it up was not finished, open it with its password (without -encrypt) to finish", ErrNotEmpty, out)
		case !encmark.IsLitter(e.Name()):
			return fmt.Errorf("%w: %s holds %s — choose a new or empty folder (to encrypt an existing archive, a later MailArchive converts it)", ErrNotEmpty, out, e.Name())
		}
	}
	return nil
}

// refuseSettingsOverlap refuses an archive folder that is, holds, or lies in
// MailArchive's settings or scratch folder, or a caller's credential path.
func refuseSettingsOverlap(out string, credentials []string) error {
	var dirs []string
	if d, err := job.Dir(); err == nil {
		dirs = append(dirs, d)
	}
	if d, err := job.ScratchDir(); err == nil {
		dirs = append(dirs, d)
	}
	dirs = append(dirs, credentials...)
	ro := util.ResolveExisting(out)
	for _, d := range dirs {
		if d == "" {
			continue
		}
		rd := util.ResolveExisting(d)
		if util.Within(ro, rd) || util.Within(rd, ro) {
			return fmt.Errorf("%s cannot hold a new encrypted archive: it overlaps %s, which keeps MailArchive's settings, sign-in or working copies in plaintext — choose a folder outside it", out, d)
		}
	}
	return nil
}

// Commit writes the key file, then records the folder (§4.4), then remembers the
// key when chosen (§5.1 step 6), and returns the archive unlocked for its first
// backup. If the record cannot be written, the key file stays — every keyless
// writer refuses the folder by it — and the error says to finish setting up.
func (p *PendingCreate) Commit() (*Unlocked, error) {
	k, err := p.p.Commit()
	if err != nil {
		return nil, err
	}
	u := &Unlocked{key: k, out: p.out, id: k.ArchiveID(), how: ByCreate, write: true}
	id := u.id.String()
	_, err = update("record "+id, func(rec *encrecord.Record) error {
		if m, ok := rec.Lookup(p.out); ok && encrecord.Spelling(m.Folder) == encrecord.Spelling(p.out) && m.ID != id {
			return fmt.Errorf("%s is recorded as the folder of the encrypted archive %s; forget it there first", p.out, m.ID)
		}
		return rec.Add(id, p.out, false, now())
	})
	if err != nil {
		u.Destroy()
		if errors.Is(err, ErrRecordBusy) {
			return nil, fmt.Errorf("the encrypted archive was created in %s, but %w; finish setting up when it is free by opening it with its password", p.out, err)
		}
		return nil, fmt.Errorf("the encrypted archive was created in %s, but this computer's record of encrypted archives could not be updated (%w); open it with its password to finish setting it up", p.out, err)
	}
	if p.o.Remember {
		if err := remember(u, p.o.Store); err != nil {
			u.warn("the key could not be remembered, so every backup and opening will ask for the password: " + err.Error())
		}
	}
	return u, nil
}
