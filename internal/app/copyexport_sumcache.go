package app

import (
	"os"
	"time"
)

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
