package desktop

import (
	"fmt"

	"mail-archive-tool/internal/state"
)

// runRow is one line of the Recent backups card (MA-701).
type runRow struct {
	When   string
	Status string // "OK" | "Failed" | "Cancelled"
	Failed bool
	Detail string
}

// recentRunRows reads the archive's rolling run history and formats the most recent runs,
// newest first (up to 10), for the Recent backups card. Best effort: a missing or
// unreadable history shows no rows rather than an error.
func recentRunRows(out string) []runRow {
	if out == "" {
		return nil
	}
	h, err := state.ReadRunHistory(out)
	if err != nil {
		return nil
	}
	rows := make([]runRow, 0, 10)
	for i := len(h) - 1; i >= 0 && len(rows) < 10; i-- {
		e := h[i]
		r := runRow{When: e.When.Local().Format("Jan 2, 2006 3:04 PM"), Failed: e.Status == state.RunFailed}
		switch e.Status {
		case state.RunOK:
			r.Status = "OK"
			switch {
			case e.Encrypted: // its counts are sealed with the archive (F15)
				r.Detail = "backup completed (its counts are sealed)"
			case e.Exported > 0 && e.Filled > 0:
				r.Detail = fmt.Sprintf("%d messages, %d filled", e.Exported, e.Filled)
			case e.Exported > 0:
				r.Detail = fmt.Sprintf("%d messages", e.Exported)
			default:
				r.Detail = "no new mail"
			}
		case state.RunFailed:
			r.Status, r.Detail = "Failed", e.Error
			if e.ErrorKind != "" { // an encrypted archive's entry: the kind's fixed words
				r.Detail = state.FailureText(e.ErrorKind)
			}
		case state.RunCancelled:
			r.Status, r.Detail = "Cancelled", "stopped before it finished"
		default:
			r.Status = e.Status
		}
		rows = append(rows, r)
	}
	return rows
}
