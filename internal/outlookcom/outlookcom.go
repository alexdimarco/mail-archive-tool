// Package outlookcom drives classic Outlook for Windows through COM automation to
// export each mail account to a fresh, standard .pst file. It exists for the case
// go-pst cannot read directly: a live Exchange/IMAP .ost cache, whose on-disk
// format varies by Outlook build. Letting Outlook itself write a clean Unicode
// PST sidesteps that — go-pst reads a freshly created PST reliably — and the rest
// of the pipeline is unchanged.
//
// It is Windows-only and requires *classic* Outlook installed with a configured
// profile; "New Outlook" does not expose the classic Object Model. On any other
// platform the calls return ErrUnsupported. The actual COM behaviour cannot run
// in this repo's CI (no Outlook), so it is validated on a real Windows box — see
// the lab-tier rows in docs/scenario-catalog.md.
package outlookcom

import (
	"errors"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/util"
)

// ErrUnsupported is returned when Outlook COM automation is not available: any
// non-Windows OS, or Windows without classic Outlook installed.
var ErrUnsupported = errors.New("Outlook automation requires Windows with classic Outlook installed (and a configured mail profile)")

// CompletenessNote warns that a COM export can only capture mail Outlook has
// actually downloaded locally. The "keep offline" window is a per-account profile
// setting we can't reliably read or change, so callers surface this every time.
const CompletenessNote = "Note: only mail Outlook has downloaded to this computer is included.\n" +
	"For a complete archive, set the account's \"Mail to keep offline\" to All\n" +
	"(File > Account Settings > Account Settings > your account > Change), let\n" +
	"Send/Receive finish, and then run this."

// Options tunes a COM export run.
type Options struct {
	// Sync runs a Send/Receive before copying, so the offline window is current.
	Sync bool
	// SyncWait bounds how long to wait for that Send/Receive (there is no reliable
	// COM completion signal, so the wait is time-bounded). Zero uses a default.
	SyncWait time.Duration
}

// Store is one account exported to a .pst.
type Store struct {
	Name string // Outlook account/store display name
	Path string // the .pst file created for it
}

// Skip is one folder or account that Outlook's export did not carry into a PST
// (fault-isolation §4.6, FC28). Today the export only logged these; each is now
// returned, so the run records it as a gap and ends partial instead of leaving a
// silently smaller archive.
type Skip struct {
	Account string // Outlook's display name of the account (the store)
	Folder  string // the top-level folder whose CopyTo failed; "" = the whole account
	// Path is the .pst the account is exported to (the store the run archives, so
	// the gap sits under its token); for AlreadyPST it is the file to archive
	// directly instead.
	Path   string
	Reason string // the cause, as Outlook or COM reported it (untrusted text)
	// AlreadyPST says the account is already a .pst file on disk: nothing is lost,
	// it only is not copied, and its Path can be archived directly.
	AlreadyPST bool
}

// PSTPath is the .pst the export writes for the account named account under outDir.
func PSTPath(outDir, account string) string {
	return filepath.Join(outDir, pstFileName(account))
}

// pstFileName derives a safe .pst file name for an Outlook account display name,
// so an account named e.g. "user@example.com" or containing path separators can
// never escape the target directory (R4).
func pstFileName(account string) string {
	base := util.SanitizeSegment(account)
	if base == "" {
		base = "outlook-account"
	}
	return base + ".pst"
}
