package archdelete

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// runHistoryCap bounds the run history read: RunHistoryMax entries of a few hundred
// bytes each, with room.
const runHistoryCap = 4 << 20

// capturePlain reads the record's values from a plain archive while it is there
// (§3.1, §8.1 [R3:NEW-FINDING-record-capture-time]): the message count and the
// mail's date range from the manifest, the first recorded run from the history log
// and the last backup from the run history, each read through root.
func capturePlain(root *os.Root, m *state.Manifest) Captured {
	c := Captured{Known: true, Messages: m.Len()}
	for _, r := range m.All() {
		if r.Date == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, r.Date)
		if err != nil {
			continue
		}
		t = t.UTC()
		if c.MailFrom.IsZero() || t.Before(c.MailFrom) {
			c.MailFrom = t
		}
		if t.After(c.MailTo) {
			c.MailTo = t
		}
	}
	c.FirstRun = firstRun(root)
	c.LastBackup = lastBackup(root)
	return c
}

// firstRun is the time of the first run header in .mailarchive-history.jsonl
// (state/history.go), or zero when the log is missing, unreadable or holds none.
func firstRun(root *os.Root) time.Time {
	f, err := util.OpenRegularFileIn(root, state.HistoryName)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev state.HistoryEvent
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if ev.Run > 0 && ev.At != "" && !ev.Copy {
			if t, err := time.Parse(time.RFC3339, ev.At); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}

// lastBackup is the run history's newest entry whose status is ok, or zero.
func lastBackup(root *os.Root) time.Time {
	f, err := util.OpenRegularFileIn(root, state.RunHistoryName)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, runHistoryCap+1))
	if err != nil || len(data) > runHistoryCap {
		return time.Time{}
	}
	var h []state.RunHistoryEntry
	if json.Unmarshal(data, &h) != nil {
		return time.Time{}
	}
	var last time.Time
	for _, e := range h {
		if e.Status == state.RunOK && e.When.After(last) {
			last = e.When
		}
	}
	return last.UTC()
}
