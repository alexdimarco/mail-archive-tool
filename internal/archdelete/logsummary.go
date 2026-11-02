package archdelete

import (
	"bytes"
	"encoding/json"
	"os"
	"time"
)

// LogSummary says what the deletion log at path holds, for a plan that removes it (the Desktop's
// uninstall; MA-1008, the 1.5.3 re-walk's guards pass, G5): its lines (blank lines aside, whether
// MailArchive can parse them or not, as LogHoldsLines counts them) and when its last line that
// gives a time was written (zero when none does). It reads the log as readLog does (an ordinary
// file only, never through a link, within its bound); an absent log is os.ErrNotExist, so a plan
// says nothing of a log that is not there.
func LogSummary(path string) (int, time.Time, error) {
	if _, err := os.Lstat(path); err != nil {
		return 0, time.Time{}, err
	}
	data, err := readLogData(path)
	if err != nil {
		return 0, time.Time{}, err
	}
	n, last := 0, time.Time{}
	for _, b := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		n++
		var l logLine
		if json.Unmarshal(b, &l) == nil {
			if t, perr := time.Parse(time.RFC3339Nano, l.At); perr == nil {
				last = t
			}
		}
	}
	return n, last, nil
}
