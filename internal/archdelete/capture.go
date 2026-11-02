package archdelete

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path"
	"strings"
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
	c := captureRecords(m)
	c.FirstRun = firstRun(root)
	c.LastBackup, c.OldestRun = runHistoryEnds(root)
	return c
}

// CaptureSealed is the record's values of an encrypted archive whose key is open here, from its
// sealed manifest and timeline (keyring.ReadSealedRecord), by the plain archive's rules: the
// message count and the mail's date range from the records, the first run header of the
// timeline. A chunk of the timeline that could not be read (a barrier) before any run header
// leaves the first run not known, never a later run's. The plan adds the last backup, which is
// not sealed (§8.1 [O2-05]).
func CaptureSealed(m *state.Manifest, history []state.HistoryEvent) Captured {
	c := captureRecords(m)
	for _, ev := range history {
		if ev.Barrier {
			break
		}
		if t, ok := runStart(ev); ok {
			c.FirstRun = t
			break
		}
	}
	return c
}

// captureRecords are the message count and the mail's date range of m.
func captureRecords(m *state.Manifest) Captured {
	c := Captured{Known: true, Messages: m.Len()}
	for _, r := range m.All() {
		t, ok := recordDate(r)
		if !ok {
			continue
		}
		if c.MailFrom.IsZero() || t.Before(c.MailFrom) {
			c.MailFrom = t
		}
		if t.After(c.MailTo) {
			c.MailTo = t
		}
	}
	return c
}

// runStart is a run header's time: an event of a run (not a copy's) with its time.
func runStart(ev state.HistoryEvent) (time.Time, bool) {
	if ev.Run > 0 && ev.At != "" && !ev.Copy {
		if t, err := time.Parse(time.RFC3339, ev.At); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// recordDate is a record's mail date: its Date when the manifest carries one —
// only a record with issues does (state.Record), and Resolve clears it — else the
// date its stem was named with (export.StemName: the message's date in UTC, to the
// minute), which every record of the exporter's has (conformance gap 1). A stem
// named without a date ("0000-00-00_0000") has none.
func recordDate(r state.Record) (time.Time, bool) {
	if r.Date != "" {
		if t, err := time.Parse(time.RFC3339, r.Date); err == nil {
			return t.UTC(), true
		}
	}
	stem := strings.TrimSuffix(path.Base(r.Path), ".html")
	if m := stemShapeRe.FindStringSubmatch(stem); m != nil {
		if t, err := time.Parse("2006-01-02_1504", m[1]); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
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
		if t, ok := runStart(ev); ok {
			return t
		}
	}
	return time.Time{}
}

// runHistoryEnds reads the run history: its newest entry whose status is ok (the last backup)
// and its oldest entry of any status (where it begins, B9), each zero when there is none.
func runHistoryEnds(root *os.Root) (lastOK, oldest time.Time) {
	f, err := util.OpenRegularFileIn(root, state.RunHistoryName)
	if err != nil {
		return time.Time{}, time.Time{}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, runHistoryCap+1))
	if err != nil || len(data) > runHistoryCap {
		return time.Time{}, time.Time{}
	}
	var h []state.RunHistoryEntry
	if json.Unmarshal(data, &h) != nil {
		return time.Time{}, time.Time{}
	}
	for _, e := range h {
		if e.Status == state.RunOK && e.When.After(lastOK) {
			lastOK = e.When
		}
		if !e.When.IsZero() && (oldest.IsZero() || e.When.Before(oldest)) {
			oldest = e.When
		}
	}
	return lastOK.UTC(), oldest.UTC()
}
