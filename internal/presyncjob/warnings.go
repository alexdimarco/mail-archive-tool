package presyncjob

import (
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/state"
)

// warnings maps a pre-sync result to the run's warnings: a WARN outcome becomes
// one RunWarning (its code and what/why/do-this text), and so does every side
// outcome the session handed back (B6: an unreadable state record while the
// session went on to sync; A9: a record it could not save), each code once; an ok
// or note outcome adds none. Untagged, so the Linux gate tests the mapping.
func warnings(res presync.Result) []state.RunWarning {
	var out []state.RunWarning
	seen := map[string]bool{}
	for _, o := range append([]presync.Outcome{res.Outcome}, res.Side...) {
		if msg, warn := presync.WarnText(o); warn && !seen[o.Code] {
			seen[o.Code] = true
			out = append(out, state.RunWarning{Code: o.Code, Message: msg})
		}
	}
	return out
}
