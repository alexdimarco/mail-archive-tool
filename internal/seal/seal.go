// Package seal is the archive-local "paused" (hard read-only) sentinel for the
// archive manager's Pause / Resume (docs/design-archive-manager.md §5.3.1, S2-C6).
// A sealed archive refuses every content-mutating MailArchive operation until it is
// resumed; the seal is ONE file in the archive folder, and its PRESENCE is the whole
// signal — the content is only a human-readable reason.
//
// It is a LEAF: the engine (internal/app), the wizard and the Desktop import it to
// check / seal / unseal; it imports only the standard library and internal/util.
//
// Presence is read FAIL-CLOSED (S2-C6): an absent sentinel means writable; a present
// regular file means sealed; and a sentinel that is a symlink, a FIFO, a directory,
// or whose absence cannot be confirmed (a permission error) also reads as SEALED. It
// is never followed or opened to decide this, so a hostile or damaged sentinel can
// neither hang an unattended run nor silently fail open.
package seal

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/util"
)

// Name is the archive-local sentinel file; its presence seals the archive.
const Name = ".mailarchive-paused.json"

// maxReasonBytes caps the human-readable reason the sentinel may carry.
const maxReasonBytes = 16 * 1024

// ErrSealed wraps every write refused because the archive is paused, so a caller or a
// test can detect the cause with errors.Is.
var ErrSealed = errors.New("this archive is paused (read-only)")

// RefuseWrite returns a Resume-routing error wrapping ErrSealed when out is sealed,
// and nil otherwise. A content-mutating operation (capture/export, graph, reindex
// redaction, verify -record) calls it at its entry so a paused archive is never
// written; a read (a plain verify, extract, the health probe) does not (S2-C1/C2).
func RefuseWrite(out string) error {
	if !IsSealed(out) {
		return nil
	}
	detail := "resume it from MailArchive Desktop (Archives) to make changes"
	if r := Reason(out); r != "" {
		detail = "paused: " + r + "; " + detail
	}
	return fmt.Errorf("%w — %s", ErrSealed, detail)
}

// IsSealed reports whether the archive at out is paused (hard read-only). Presence of
// the sentinel is the signal, read FAIL-CLOSED: only a confirmed "not there" means
// writable; a present entry of any kind, or an unconfirmable one, reads as sealed and
// is never followed or opened. An empty out is not an archive, so not sealed.
func IsSealed(out string) bool {
	out = strings.TrimSpace(out)
	if out == "" {
		return false
	}
	if _, err := os.Lstat(filepath.Join(out, Name)); err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	return true
}

// Seal pauses the archive at out, recording an optional human-readable reason. The
// sentinel is written atomically and owner-only (S2-C6) and the parent directory is
// fsynced so the pause survives a crash. Sealing an already-sealed archive rewrites
// the reason.
func Seal(out, reason string) error {
	out = strings.TrimSpace(out)
	if out == "" {
		return errors.New("seal: no archive location")
	}
	if len(reason) > maxReasonBytes {
		reason = reason[:maxReasonBytes]
	}
	if err := util.WriteFileAtomic0600(filepath.Join(out, Name), []byte(reason)); err != nil {
		return err
	}
	fsyncDir(out)
	return nil
}

// Unseal resumes the archive at out by removing the sentinel. It is LOCK-FREE — it
// must never pass through the gated write path, or a sealed archive could never be
// resumed (S2-C4) — and idempotent: an already-resumed archive is not an error.
func Unseal(out string) error {
	out = strings.TrimSpace(out)
	if out == "" {
		return errors.New("unseal: no archive location")
	}
	if err := os.Remove(filepath.Join(out, Name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	fsyncDir(out)
	return nil
}

// Reason is the human-readable reason recorded when the archive was sealed, or ""
// when it is not sealed or the reason cannot be read safely. Best-effort and bounded;
// never used for the sealed/not-sealed decision (that is IsSealed).
func Reason(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	data, err := util.ReadSecureFile(filepath.Join(out, Name), "archive pause marker", maxReasonBytes)
	if err != nil {
		return ""
	}
	// Sanitize at the read boundary: the reason is written by whoever can write the archive
	// folder and is echoed into terminals, logs and dialogs (via RefuseWrite), so strip ANSI
	// escapes / embedded newlines (StripControl) and bidi overrides (StripBidi) before any
	// consumer sees it — the same treatment the other archive-directory records get.
	return strings.TrimSpace(util.StripBidi(util.StripControl(string(data))))
}

// fsyncDir flushes a directory entry so the atomic write/remove is durable across a
// crash (matches internal/archindex). Best-effort: some filesystems and Windows do
// not support a directory fsync, and a failure here does not undo the change.
func fsyncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
