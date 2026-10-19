package app

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
)

// cePruneCached is what the retry offers of one card in a chain were last found to
// be, with the signature of the chain's records at that time: nothing left to retry
// (failed), nothing left to export (remaining). An action is looked at only once per
// signature (EW-AC8).
type cePruneCached struct {
	sig        string
	haveFailed bool
	failedNone bool
	haveRemain bool
	remainNone bool
}

// chainSignature is the sizes and modification times of the run records of a chain
// (records are append-only, so any change to one changes its size). "" when a record
// could not be looked at, in which case nothing is kept.
func (e *CopyExportEnv) chainSignature(run string, parts []string) string {
	var b strings.Builder
	for _, p := range append([]string{run}, parts...) {
		fi, err := os.Stat(e.recordPath(p))
		if errors.Is(err, fs.ErrNotExist) {
			b.WriteString(p + "|gone;") // a deleted record is a state of the chain too
			continue
		}
		if err != nil {
			return ""
		}
		b.WriteString(p)
		b.WriteByte('|')
		b.WriteString(strconv.FormatInt(fi.Size(), 10))
		b.WriteByte('|')
		b.WriteString(strconv.FormatInt(fi.ModTime().UnixNano(), 10))
		b.WriteByte(';')
	}
	return b.String()
}

// ceSumCached is a run record's summary and the size and modification time of the file
// it was read from.
type ceSumCached struct {
	size int64
	mod  time.Time
	sum  *ceRecSummary
}

// readSummaryCached is readSummary for the Export activity page, which polls History
// every few seconds: a record whose size and modification time are what they were at
// the last pass is not scanned again (EW-AC8). Records are append-only, so a changed
// record always changes its size. The caller must not change the summary it gets.
// Only a clean read is kept: an unreadable or newer-format record is read afresh.
func (e *CopyExportEnv) readSummaryCached(path string) (*ceRecSummary, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return readSummary(path)
	}
	e.sumMu.Lock()
	c, ok := e.sumCache[path]
	e.sumMu.Unlock()
	if ok && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		return c.sum, nil
	}
	sum, err := readSummary(path)
	if err != nil {
		return sum, err
	}
	e.sumMu.Lock()
	if e.sumCache == nil || len(e.sumCache) > 4096 {
		e.sumCache = map[string]ceSumCached{}
	}
	e.sumCache[path] = ceSumCached{size: fi.Size(), mod: fi.ModTime(), sum: sum}
	e.sumMu.Unlock()
	return sum, nil
}
