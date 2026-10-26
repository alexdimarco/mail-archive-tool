package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/export"
)

// ErrEncryptedNeedsKey refuses an operation that would treat an encrypted archive
// as plaintext — read it as files on disk, prune it, re-export into it — when the
// operation was not handed the archive's key (design-encrypted-archives-phase2a
// §5, A15).
var ErrEncryptedNeedsKey = errors.New("this archive is encrypted")

// manifestDir is the folder of a -manifest override, or "" when there is none.
func manifestDir(manifest string) string {
	if manifest == "" {
		return ""
	}
	return filepath.Dir(manifest)
}

// ErrLogInEncryptedArchive refuses a run log placed inside an encrypted archive: a
// run log names stores, folders and source paths in plaintext (phase2a §4).
var ErrLogInEncryptedArchive = errors.New("a run log cannot be written inside an encrypted archive")

// ErrEncryptionUnknown refuses an operation whose target folder cannot be checked
// for encryption: it is not read as plaintext on a guess (phase2a §5).
var ErrEncryptionUnknown = errors.New("cannot tell whether the folder is an encrypted archive")

// RefuseLogInside refuses a -log path whose folder is, or sits inside, an encrypted
// archive — any encrypted archive, not only the run's own, also through a link —
// before the log is opened. An empty path (no -log) is nothing to refuse.
func RefuseLogInside(logPath string) error {
	if logPath == "" {
		return nil
	}
	abs, err := filepath.Abs(logPath)
	if err != nil {
		abs = filepath.Clean(logPath)
	}
	dir, why, uerr := encmark.AtOrAbove(filepath.Dir(abs))
	if dir != "" {
		return fmt.Errorf("%w: %s would be inside the encrypted archive %s (%s) — leave out -log, or choose a folder outside the archive",
			ErrLogInEncryptedArchive, abs, dir, why)
	}
	if uerr != nil {
		return fmt.Errorf("%w: %v — leave out -log, or choose another folder", ErrEncryptionUnknown, uerr)
	}
	return nil
}

// refuseStoreIntoEncrypted refuses a PLAINTEXT run's source whose store folder in
// the archive (out/<token>) is an encrypted archive someone placed inside this one:
// its pages and originals would be written into the encrypted archive in plaintext.
// An encrypted run writes no store folder.
func refuseStoreIntoEncrypted(exp *export.Exporter, token string) error {
	if exp.Units != nil {
		return nil
	}
	dir := filepath.Join(exp.OutDir, token)
	if enc, why, _ := encmark.Here(dir); enc {
		return fmt.Errorf("%w: the store folder %s is an encrypted archive (%s); refusing to write this source's messages into it — move that archive out of %s",
			ErrEncryptedNeedsKey, dir, why, exp.OutDir)
	}
	return nil
}

// RefuseKeyless refuses op on out — before any lock, log, read or write — when
// out is an encrypted archive, or sits inside one, or any of extra (a -manifest
// folder, an extract destination) is or sits inside one, as encmark recognises it —
// the folder's own markers or this computer's record of encrypted archives, the path
// as given and through its links. A
// target that cannot be checked at all is refused too (ErrEncryptionUnknown): the
// operation might reach it by another spelling. op names the operation in the
// refusal ("archive into it").
func RefuseKeyless(op, out string, extra ...string) error {
	for _, target := range append([]string{out}, extra...) {
		if target == "" {
			continue
		}
		abs, err := filepath.Abs(target)
		if err != nil {
			abs = filepath.Clean(target)
		}
		res, uerr := encmark.At(abs)
		if res.ByRecordErr {
			// Not known to be encrypted: this computer's record cannot be used, and the
			// folder holds MailArchive files (§4.3; the P3-0 adversarial pass, REG-02).
			return fmt.Errorf("%w: %v; refusing to %s %s until it is fixed — `mailarchive status` names the fix", encmark.ErrRecordUnusable, res.RecordErr, op, abs)
		}
		dir, why := "", ""
		if res.Encrypted {
			dir, why = res.Folder, res.Why
		}
		if dir != "" {
			where := dir
			if dir != abs {
				where = dir + " (which holds " + abs + ")"
			}
			return fmt.Errorf("%w: %s is an encrypted archive — %s; refusing to %s without its key: open it in MailArchive with its password or recovery key",
				ErrEncryptedNeedsKey, where, why, op)
		}
		if uerr != nil {
			return fmt.Errorf("%w: %v; refusing to %s", ErrEncryptionUnknown, uerr, op)
		}
	}
	return nil
}

// ErrVerifyRecordEncrypted refuses `verify -record` on an encrypted archive: every
// backup records its files' digests in the archive's file list itself
// (design-encrypted-archives-phase2b §4).
var ErrVerifyRecordEncrypted = errors.New("verify -record does not apply to an encrypted archive")

// encryptedArchiveAt reports whether out is itself an encrypted archive, as
// RefuseKeyless recognises one — not a folder inside one. A link naming the archive
// itself counts as the archive, as a linked -out does for a plaintext verify; links
// inside the archive are still refused (the adversarial pass, CRIT-2).
func encryptedArchiveAt(out string) bool {
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = filepath.Clean(out)
	}
	// Only an archive known to be encrypted and holding itself gets the keyless
	// check: never a folder judged so only because the record cannot be used
	// (CON-01), and never a recorded folder in a refusal case — verify then says
	// what status says, writing nothing (CRIT-02).
	res, _ := encmark.At(abs)
	return res.Encrypted && !res.ByRecordErr && res.Case == encmark.None && res.Folder == abs
}

// RefuseVerify is verify's guard. An encrypted archive itself is checked without
// its key (tier 1, phase2b §4), but never with -record; a folder inside one, or one
// that cannot be checked, is refused as RefuseKeyless refuses it.
func RefuseVerify(out string, record bool) error {
	if !encryptedArchiveAt(out) {
		return RefuseKeyless("verify it", out)
	}
	if record {
		return fmt.Errorf("%w: %s is an encrypted archive — every backup records its files' checksums itself; run verify without -record", ErrVerifyRecordEncrypted, out)
	}
	return nil
}
