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

	"mail-archive-tool/internal/util"
)

// ErrUnsupported is returned when Outlook COM automation is not available: any
// non-Windows OS, or Windows without classic Outlook installed.
var ErrUnsupported = errors.New("Outlook automation requires Windows with classic Outlook installed (and a configured mail profile)")

// Store is one account exported to a .pst.
type Store struct {
	Name string // Outlook account/store display name
	Path string // the .pst file created for it
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
