package app

import (
	"strings"

	"mail-archive-tool/internal/state"
)

// emlPresent reports whether a record's preserved original (`<stem>.eml`) is
// present as a REGULAR FILE on disk under out — the single file-presence signal
// `extract` emits on (extractInspect "ok"), `verify` counts (WithEML) and
// `status` reports (design K4). It resolves the `.eml` sibling through the same
// validRelPath gate and the same component-wise, no-symlink-follow inspect
// `extract` uses (extractInspect), so the three surfaces cannot disagree (QC1).
// It Lstats only — it never hashes and never reads a byte of the file, so a
// stale recorded digest can neither add a since-deleted `.eml` nor mask a
// present one: a symlink or a non-regular file at the `.eml` path is not
// "present". A record whose recorded path fails containment is not present.
func emlPresent(out string, rec state.Record) bool {
	segs, ok := validRelPath(rec.Path)
	if !ok {
		return false
	}
	stem := strings.TrimSuffix(rec.Path, verifyHTMLSuffix)
	baseSegs := segs[:len(segs)-1]
	emlSegs := append(append([]string{}, baseSegs...), lastSegment(stem+verifyEMLSuffix))
	kind, _, _ := extractInspect(out, emlSegs)
	return kind == "ok"
}

// ExtractableCount reports how many of the manifest's records have a preserved
// original (`<stem>.eml`) present on disk under out (withEML) out of the total
// record count (total). Extractability is a filesystem fact, not a manifest one:
// this is FILE PRESENCE, never a recorded digest (design K4/QC1). It Lstats each
// record's `.eml` through the shared emlPresent predicate — the SAME signal
// `extract` drains and `verify` counts — so `status`, `verify` and `extract`
// cannot diverge. It hashes nothing and reads no message bytes, costing one
// Lstat per record (O(records), cheap next to a hash). A record whose recorded
// path fails the containment gate contributes to total but never to withEML.
func ExtractableCount(out string, m *state.Manifest) (withEML, total int) {
	records := m.All()
	total = len(records)
	for _, rec := range records {
		if emlPresent(out, rec) {
			withEML++
		}
	}
	return withEML, total
}
