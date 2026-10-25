package app

import (
	"errors"
	"fmt"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// kindError tags a run error with its failure kind (state.Fail*) without changing
// a byte of its text: a plaintext archive's records keep today's wording, and an
// encrypted archive's say the kind alone (P2a-C16).
type kindError struct {
	kind string
	err  error
}

func (e *kindError) Error() string { return e.err.Error() }
func (e *kindError) Unwrap() error { return e.err }

// withKind tags err with kind; nil stays nil.
func withKind(kind string, err error) error {
	if err == nil {
		return nil
	}
	return &kindError{kind: kind, err: err}
}

// FailureKind maps a run error onto the closed list of failure kinds
// (state.Fail*, design-encrypted-archives-phase2a §4, P2a-C16). It never returns
// error text: anything it does not recognise is state.FailRunFailed.
func FailureKind(err error) string {
	var tagged *kindError
	var unreachable *sourceUnreachableError
	var fatal *RunFatalError
	var stopped *SourceStoppedError
	var partial *PartialError
	var outage *outageError
	var ioe *IndexOpenError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrEncryptedNeedsKey):
		return state.FailEncryptedNeedsKey
	case errors.Is(err, ErrEncryptedArchive):
		return state.FailEncryptedRefused
	case errors.As(err, &unreachable):
		return state.FailSourceUnreachable
	case errors.As(err, &fatal) && fatal.scratch:
		return state.FailScratch
	case errors.As(err, &fatal):
		return state.FailArchiveUnwritable
	case errors.As(err, &outage):
		return state.ErrKindServiceUnavailable
	case errors.As(err, &ioe):
		return errorKindOf(err)
	case errors.As(err, &stopped):
		return state.FailSourceUnreadable
	case errors.As(err, &partial):
		return state.FailPartial
	// A tag names what the typed errors above cannot (a lost lock, a failed save, a
	// mailbox); a run that failed two ways is named by its most serious typed cause.
	case errors.As(err, &tagged):
		return tagged.kind
	case errors.Is(err, lockfile.ErrHeld):
		return state.FailArchiveBusy
	}
	return state.FailRunFailed
}

// ErrorTextFor is the text a sink outside the archive's own records shows for a
// run error at out — stderr, the config-dir breadcrumbs, the Desktop activity log:
// the error itself for a plaintext archive; for an encrypted one, the archive's
// location and its failure kind's fixed words, never the error's text, which can
// carry source paths, mailbox addresses and server text (P2a-C16).
func ErrorTextFor(out string, err error) string {
	if err == nil {
		return ""
	}
	if !encryptedAt(out) {
		return err.Error()
	}
	return fmt.Sprintf("%s is an encrypted archive: %s — open it in MailArchive with its password or recovery key for the details", out, state.FailureText(FailureKind(err)))
}

// ArchiveEncrypted reports whether the archive at out is an encrypted one
// (vault.EncryptedMarker), for a sink that must choose its words by it.
func ArchiveEncrypted(out string) bool { return encryptedAt(out) }

// encryptedAt reports whether the archive at out is an encrypted one, by the one
// recognition predicate; a folder it cannot read is not called encrypted here (the
// run's own handling meets that error).
func encryptedAt(out string) bool {
	enc, _, _ := vault.EncryptedMarker(out)
	return enc
}
