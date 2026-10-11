package app

import (
	"fmt"
	"os"
	"strings"

	"mail-archive-tool/internal/util"
)

// A retry set's offers and totals (friction F9, F10). The offers on a card are per run
// (design XC2: read from the run record), so once a later part had exported what an earlier
// part still lists as remaining or failed, the earlier card kept offering to export it again,
// which would make a duplicate part and move the set fingerprint. The offers are now taken from
// what the whole set holds: an offer whose messages are all exported in some part of the set is
// not made, and every card of a set says how much of the set is exported.

// chainOffer is what the records of one retry set say, kept while they are unchanged.
type chainOffer struct {
	sig           string
	dropRemaining map[string]bool          // runs whose "Export the remaining messages" would only repeat
	dropFailed    map[string]bool          // runs whose "Export failed messages again" would only repeat
	retryable     map[string]int           // each run's retryable failures
	exported      int                      // distinct messages exported across the set's parts
	failures      map[string][]CopyFailure // each sealed run's failures that no part of the set exported
	failuresMore  map[string]int
}

// failCached is a run's listed failures while its record is unchanged.
type failCached struct {
	sig  string
	list []CopyFailure
	more int
}

// failuresCached is failuresOf kept against the record's size and modification time, so a poll of
// an untouched record walks nothing (EW-AC8).
func (e *CopyExportEnv) failuresCached(run string) ([]CopyFailure, int) {
	path := e.recordPath(run)
	sig := ""
	if fi, err := os.Stat(path); err == nil {
		sig = fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano())
	}
	e.offerMu.Lock()
	c, ok := e.failCache[run]
	e.offerMu.Unlock()
	if ok && sig != "" && c.sig == sig {
		return c.list, c.more
	}
	list, more := failuresOf(path)
	if sig != "" {
		e.offerMu.Lock()
		if e.failCache == nil || len(e.failCache) > 4096 {
			e.failCache = map[string]failCached{}
		}
		e.failCache[run] = failCached{sig: sig, list: list, more: more}
		e.offerMu.Unlock()
	}
	return list, more
}

// setSignature changes when a part's record does: its size and modification time.
func (e *CopyExportEnv) setSignature(set *CopySetInfo) string {
	var b strings.Builder
	for _, p := range set.Parts {
		b.WriteString(p.Run)
		if fi, err := os.Stat(e.recordPath(p.Run)); err == nil {
			fmt.Fprintf(&b, ":%d:%d", fi.Size(), fi.ModTime().UnixNano())
		} else {
			b.WriteString(":none")
		}
		if p.Gone {
			b.WriteString(":gone")
		}
		b.WriteByte(';')
	}
	return b.String()
}

// setOffers reads the set's records (cached while they are unchanged): the messages exported in
// any part not marked gone, and which parts' retry offers would only repeat them.
func (e *CopyExportEnv) setOffers(chain string, set *CopySetInfo) chainOffer {
	sig := e.setSignature(set)
	e.offerMu.Lock()
	if c, ok := e.offerCache[chain]; ok && c.sig == sig {
		e.offerMu.Unlock()
		return c
	}
	e.offerMu.Unlock()

	covered := map[string]bool{}
	type failedMsg struct {
		id string
		f  CopyFailure
	}
	failedOf := map[string][]failedMsg{}
	for _, p := range set.Parts {
		if p.Gone {
			continue
		}
		if sum, err := readSummary(e.recordPath(p.Run)); err != nil || sum.Sealed == nil || sum.Gone {
			continue
		}
		failedOf[p.Run] = nil
		_ = walkMessages(e.recordPath(p.Run), func(pl *cePlanLine, m *ceMsgLine) error {
			if m == nil {
				return nil
			}
			switch m.Status {
			case statusExported, statusWithoutOrig, statusOriginalOnly:
				covered[pl.SourceID] = true
			case statusFailed:
				failedOf[p.Run] = append(failedOf[p.Run], failedMsg{pl.SourceID, CopyFailure{Subject: util.StripControl(pl.Subject), Folder: pl.FolderPath(), Reason: plainReason(util.StripControl(m.Reason))}})
			}
			return nil
		})
	}
	off := chainOffer{sig: sig, dropRemaining: map[string]bool{}, dropFailed: map[string]bool{}, retryable: map[string]int{}, exported: len(covered),
		failures: map[string][]CopyFailure{}, failuresMore: map[string]int{}}
	// the failures a card lists are those no part of the set exported: one source of truth with the offers
	for run, list := range failedOf {
		var left []CopyFailure
		for _, fm := range list {
			if !covered[fm.id] {
				left = append(left, fm.f)
			}
		}
		off.failuresMore[run] = max(0, len(left)-copyFailuresShown)
		off.failures[run] = left[:min(len(left), copyFailuresShown)]
	}
	allCovered := func(msgs []CopyMessage, err error) bool {
		if err != nil {
			return false
		}
		for i := range msgs {
			if !covered[msgs[i].SourceID] {
				return false
			}
		}
		return true
	}
	for _, p := range set.Parts {
		if p.Gone {
			continue
		}
		if sum, err := readSummary(e.recordPath(p.Run)); err != nil || sum.Sealed == nil || sum.Gone {
			continue
		}
		if allCovered(e.RunRemaining(p.Run)) {
			off.dropRemaining[p.Run] = true
		}
		failed, ferr := e.RunFailed(p.Run)
		off.retryable[p.Run] = len(failed)
		if allCovered(failed, ferr) {
			off.dropFailed[p.Run] = true
		}
	}
	e.offerMu.Lock()
	if e.offerCache == nil {
		e.offerCache = map[string]chainOffer{}
	}
	e.offerCache[chain] = off
	e.offerMu.Unlock()
	return off
}

// applySet drops the offers that would repeat what the set holds and words the set's total on
// the card (and so in its receipt): "Part 2 of a set: 7 of 7 messages exported across 3 parts."
// planned is how many messages the set's first part selected.
func (e *CopyExportEnv) applySet(info *CopyRunInfo, planned int) {
	if info.Set == nil || len(info.Set.Parts) < 2 {
		return
	}
	off := e.setOffers(info.Chain, info.Set)
	var keep []string
	for _, a := range info.Actions {
		if (a == ActExportRemaining && off.dropRemaining[info.Run]) || (a == ActExportFailed && off.dropFailed[info.Run]) {
			continue
		}
		keep = append(keep, a)
	}
	if keep == nil && info.Actions != nil {
		keep = []string{}
	}
	info.Actions = keep
	if left, ok := off.failures[info.Run]; ok && info.Failed > 0 {
		info.Failures, info.FailuresMore = left, off.failuresMore[info.Run]
		if len(left) == 0 {
			info.Failures = nil
		}
	}
	if planned > 0 {
		if off.exported >= planned {
			if info.Kind == "" || info.Kind == obsSettledRun { // a card the reconciler is still reporting on keeps its label
				switch info.Status {
				case CopyStatusPartial, CopyStatusCancelled, CopyStatusFailed, CopyStatusFinishedAsPartial:
					info.Label += " — the rest was exported in a later part of this set"
				}
			}
			info.SetNote = fmt.Sprintf("All %s messages are exported, across the %d parts of this set.", commas(planned), len(info.Set.Parts))
		} else {
			info.SetNote = fmt.Sprintf("%s of %s messages are exported so far, across the %d parts of this set.", commas(off.exported), commas(planned), len(info.Set.Parts))
		}
	}
}
