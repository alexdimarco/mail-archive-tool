package app

import (
	"fmt"
	"os"
	"strings"
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
	dropRemaining map[string]bool // runs whose "Export the remaining messages" would only repeat
	dropFailed    map[string]bool // runs whose "Export failed messages again" would only repeat
	retryable     map[string]int  // each run's retryable failures
	exported      int             // distinct messages exported across the set's parts
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
	for _, p := range set.Parts {
		if p.Gone {
			continue
		}
		if sum, err := readSummary(e.recordPath(p.Run)); err != nil || sum.Sealed == nil || sum.Gone {
			continue
		}
		_ = walkMessages(e.recordPath(p.Run), func(pl *cePlanLine, m *ceMsgLine) error {
			if m != nil && (m.Status == statusExported || m.Status == statusWithoutOrig || m.Status == statusOriginalOnly) {
				covered[pl.SourceID] = true
			}
			return nil
		})
	}
	off := chainOffer{sig: sig, dropRemaining: map[string]bool{}, dropFailed: map[string]bool{}, retryable: map[string]int{}, exported: len(covered)}
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
	if planned > 0 {
		if off.exported >= planned {
			// the set is whole: what an earlier part lists as not exported is in a later one
			if info.Failed > 0 && off.dropFailed[info.Run] && off.retryable[info.Run] == info.Failed {
				info.Failures, info.FailuresMore = nil, 0
			}
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
