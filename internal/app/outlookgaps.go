package app

import (
	"log"
	"time"

	"mail-archive-tool/internal/outlookcom"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// outlookGap is one ledger gap for something Outlook's export did not carry into a
// PST, with the .pst whose store it belongs to.
type outlookGap struct {
	path string // the .pst (the store's source id seed)
	name string // the account's display name, for a store not yet seen
	gap  state.CaptureGap
}

// outlookGaps maps what `-outlook` skipped to ledger gaps (fault-isolation §4.6,
// FC28), with no manifest and no COM in it, so the mapping is proven on every OS:
//   - a folder whose CopyTo failed, or an account Outlook could not export, is an
//     outlook-export-skipped folder gap (fillable: a later export may carry it),
//     locator outlook:<account>/<folder>, so the run is partial;
//   - an account that is already a .pst on disk is an outlook-account-skipped gap of
//     class info (nothing is lost), whose detail names the path to archive directly.
//
// The folder is spelled as a message page's folder is (a sanitized segment), so the
// gap sits where the folder's own pages would.
func outlookGaps(skips []outlookcom.Skip) []outlookGap {
	out := make([]outlookGap, 0, len(skips))
	for _, s := range skips {
		g := state.CaptureGap{Scope: state.GapScopeFolder, Locator: "outlook:" + s.Account + "/" + s.Folder, Detail: s.Reason}
		if s.AlreadyPST {
			g.Reason, g.Class = "outlook-account-skipped", state.ClassInfo
			g.Detail = "this account is already the .pst file " + s.Path + ": archive it directly (-input)"
		} else {
			g.Reason, g.Class = "outlook-export-skipped", state.ClassFillable
			if s.Folder != "" {
				g.Folder = util.SanitizeSegment(s.Folder)
			}
		}
		out = append(out, outlookGap{path: s.Path, name: s.Account, gap: g})
	}
	return out
}

// recordOutlookSkips puts what Outlook's export skipped into the capture-gap ledger
// as this run's observations, under the token of the PST each belongs to, and says
// so in the run log (they were only logged before). A later run whose export
// carries them closes them when that PST's walk completes. It returns how many it
// recorded.
func recordOutlookSkips(m *state.Manifest, skips []outlookcom.Skip, runAt time.Time, logger *log.Logger) int {
	n := 0
	for _, og := range outlookGaps(skips) {
		og.gap.Token = m.Token(state.PathSourceID(og.path), og.name)
		m.ObserveGap(og.gap, runAt)
		if og.gap.Class == state.ClassInfo {
			logger.Printf("Outlook: account %q is already a .pst file: archive %s directly", util.StripControl(og.name), util.StripControl(og.path))
		} else {
			logger.Printf("Outlook: %s of %q was not exported (%s): recorded as not captured", util.StripControl(skipWhat(og.gap)), util.StripControl(og.name), util.StripControl(og.gap.Detail))
		}
		n++
	}
	return n
}

func skipWhat(g state.CaptureGap) string {
	if g.Folder != "" {
		return "folder " + g.Folder
	}
	return "the account"
}
