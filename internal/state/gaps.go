package state

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"mail-archive-tool/internal/util"
)

// The capture-gap ledger (fault-isolation §4.2, §4.3): the items and folders a
// source holds that a run could not capture and that no record represents. It is
// INERT (P5): nothing ever consults it to decide whether to read, fetch or export
// an item, so every gap is attempted again by every run whose scope covers it. A
// gap's class is a posture and remedy label, never a skip decision.

// CaptureGap scopes.
const (
	GapScopeItem     = "item"
	GapScopeFolder   = "folder"
	GapScopeOverflow = "overflow" // the rest of a folder's items beyond the lifetime caps, counted exactly (P13)
)

// Gap classes (§4.1). Fillable and terminal gaps make a run partial; info does
// not.
const (
	ClassFillable = "fillable"
	ClassTerminal = "terminal"
	ClassInfo     = "info"
)

// Lifetime bounds (P13, FC16): per store token, over the archive's life, not per
// run. Info entries have their own cap and never consume the item cap. The total
// bounds a hostile manifest at Load.
const (
	maxGapItems   = 1000
	maxGapFolders = 1000
	maxGapInfo    = 100
	// maxGapOverflow bounds the per-folder overflow entries a token keeps: the entries
	// that absorb a folder's excess are keyed by folder, so without a bound a source
	// whose faults each name a different folder made one entry per fault (S2A-AO3).
	// Beyond it the excess is counted in the token's own overflow entry.
	maxGapOverflow = 100
	maxGapTotal    = 20000

	maxGapDetail   = 300 // bytes
	maxGapSubject  = 200 // runes
	maxGapReason   = 64  // bytes
	maxGapLocator  = 512 // bytes
	maxGapFolder   = 512 // bytes
	maxGapReceived = 40  // bytes
	maxGapSkipped  = 1 << 30
)

// Stuck threshold (owner ruling OR5): a fillable gap attempted on at least
// stuckRuns runs, the first of them at least max(stuckAge, two schedule periods)
// ago.
const (
	stuckRuns = 3
	stuckAge  = 72 * time.Hour
)

// CaptureGap is one ledger entry.
type CaptureGap struct {
	Token    string    `json:"token"`
	Folder   string    `json:"folder"`             // same spelling as Record.Folder; "" = store root. Updated on every observation [FC14]
	Scope    string    `json:"scope"`              // "item" | "folder" | "overflow"
	Locator  string    `json:"loc"`                // graph:<immutable id, else id> | pst-nid:<store-uid>/<nid> | ident:<identity> | maildir:<folder>/<unique> | file:<store-rel path> | folder:<store-rel path> | outlook:<account>/<folder> [FC8, FC28]
	Reason   string    `json:"reason"`             // §4.1 table; an unknown reason is kept, bounded and stripped [FC17]
	Class    string    `json:"class"`              // fillable | terminal | info; overflow = the most severe class absorbed [FC16]
	Subject  string    `json:"subject,omitempty"`  // listing or post-parse subject; <=200 runes, control-stripped
	Received string    `json:"received,omitempty"` // RFC 3339; Graph listing or post-parse m.Date() [FC13]
	Detail   string    `json:"detail,omitempty"`   // error text; <=300 bytes, control-stripped
	Skipped  int       `json:"skipped,omitempty"`  // folder/overflow: items not attempted (or absorbed) THIS run (reset at the run's first observation [FC15])
	FirstRun time.Time `json:"first"`
	LastRun  time.Time `json:"last"`
	Runs     int       `json:"runs"` // distinct runs that observed it, at most one per UTC day (not calls; not necessarily consecutive) [FC15, OR5]
}

// Weight is how many items the entry stands for: an overflow entry counts the
// items it absorbed this run, every other entry is one.
func (g CaptureGap) Weight() int {
	if g.Scope == GapScopeOverflow && g.Skipped > 0 {
		return g.Skipped
	}
	return 1
}

// GapKey is the ledger key: token \x00 scope \x00 locator. The ledger is a map of
// its own, so its keys can never collide with a record key or reach byIdent.
func GapKey(token, scope, locator string) string {
	return token + keySeparator + scope + keySeparator + locator
}

// boundString strips control characters and cuts s to at most max bytes on a rune
// boundary (the ledger is untrusted on read, MA-157, and its text is echoed into
// terminals, reports and pages).
func boundString(s string, max int) string {
	s = util.StripBidi(util.StripControl(s))
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 {
		if r, size := utf8.DecodeLastRuneInString(s); r == utf8.RuneError && size <= 1 {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

// boundRunes strips control characters and cuts s to at most max runes.
func boundRunes(s string, max int) string {
	s = util.StripBidi(util.StripControl(s))
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// GapFolder is the spelling the ledger stores a folder path in: control and bidi
// characters stripped, cut to its bound. A caller that compares a path it holds raw
// with the ledger's entries (the local run's closure guard) compares it in this
// spelling, or it never matches a folder whose name the ledger had to change
// (S2A-AI7).
func GapFolder(folder string) string { return boundString(folder, maxGapFolder) }

// knownClass reports whether c is one of the three classes.
func knownClass(c string) bool { return c == ClassFillable || c == ClassTerminal || c == ClassInfo }

// severity orders the classes for an overflow entry's "most severe absorbed".
func severity(c string) int {
	switch c {
	case ClassTerminal:
		return 3
	case ClassFillable:
		return 2
	case ClassInfo:
		return 1
	}
	return 0
}

func moreSevere(a, b string) string {
	if severity(b) > severity(a) {
		return b
	}
	return a
}

// sanitizeGap bounds and strips every string of g and fails closed on an unknown
// class (terminal, never info, so a newer build's gaps are never downgraded
// [FC17]); a known class with an unknown reason is kept. Times and counts are
// the caller's.
func sanitizeGap(g CaptureGap) CaptureGap {
	g.Folder = boundString(g.Folder, maxGapFolder)
	g.Locator = boundString(g.Locator, maxGapLocator)
	g.Reason = boundString(g.Reason, maxGapReason)
	if g.Reason == "" {
		g.Reason = "unknown"
	}
	g.Subject = boundRunes(g.Subject, maxGapSubject)
	g.Received = boundString(g.Received, maxGapReceived)
	g.Detail = boundString(g.Detail, maxGapDetail)
	switch g.Scope {
	case GapScopeItem, GapScopeFolder, GapScopeOverflow:
	default:
		g.Scope = GapScopeItem
	}
	if !knownClass(g.Class) {
		g.Class = ClassTerminal
	}
	if g.Skipped < 0 {
		g.Skipped = 0
	}
	if g.Skipped > maxGapSkipped {
		g.Skipped = maxGapSkipped
	}
	return g
}

// sameUTCDay reports whether a and b fall on one UTC calendar day.
func sameUTCDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

// overflowLocator is the locator of a folder's overflow entry.
func overflowLocator(folder string) string { return "overflow:" + folder }

// overflowDetail is what an overflow entry says about itself.
const overflowDetail = "more items than the per-source limit could not be captured; they are counted here, not listed"

// gapBucket says which lifetime cap an entry counts against: "info" for any
// scope, else its scope. Overflow entries count against none.
func gapBucket(g CaptureGap) string {
	switch {
	case g.Scope == GapScopeOverflow:
		return ""
	case g.Class == ClassInfo:
		return ClassInfo
	}
	return g.Scope
}

func gapBucketCap(bucket string) int {
	switch bucket {
	case GapScopeItem:
		return maxGapItems
	case GapScopeFolder:
		return maxGapFolders
	case ClassInfo:
		return maxGapInfo
	}
	return 0
}

// ObserveGap records that this run saw the gap g (the run's timestamp is runAt):
// it inserts the entry, or updates an existing one. On an existing entry it sets
// LastRun, Reason, Class, Detail and Folder, and Subject and Received when given
// [FC14]. Runs counts distinct runs, at most one per UTC day (FC15, OR5): it
// increments only when the entry was last observed by an EARLIER run on another
// day, never on a second observation in one run. Skipped (items not attempted
// this run) resets at the run's first observation [FC15] and then accumulates
// g.Skipped. A new entry beyond a lifetime cap (per token: 1000 live item entries,
// 1000 folder entries, 100 info entries) is not inserted: the folder's overflow
// entry counts it, with the most severe class it absorbed [FC16]. It returns the
// key the observation was stored under and whether it created a new entry.
func (m *Manifest) ObserveGap(g CaptureGap, runAt time.Time) (key string, created bool) {
	g = sanitizeGap(g)
	if g.Scope == GapScopeOverflow {
		g.Scope = GapScopeItem // the overflow scope is the ledger's own
	}
	if !SafeToken(g.Token) {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CaptureGaps == nil {
		m.CaptureGaps = map[string]CaptureGap{}
	}
	key = GapKey(g.Token, g.Scope, g.Locator)
	if old, ok := m.CaptureGaps[key]; ok {
		if !old.LastRun.Equal(runAt) {
			if !sameUTCDay(old.LastRun, runAt) {
				old.Runs++
			}
			old.Skipped = 0
		}
		old.Skipped += g.Skipped
		if runAt.Before(old.FirstRun) {
			old.FirstRun = runAt
		}
		old.LastRun = runAt
		old.Reason, old.Class, old.Detail, old.Folder = g.Reason, g.Class, g.Detail, g.Folder
		if g.Subject != "" {
			old.Subject = g.Subject
		}
		if g.Received != "" {
			old.Received = g.Received
		}
		m.CaptureGaps[key] = old
		return key, false
	}
	if b := gapBucket(g); b != "" && m.gapBucketCountLocked(g.Token, b) >= gapBucketCap(b) {
		return m.absorbGapLocked(g, runAt), false
	}
	g.FirstRun, g.LastRun, g.Runs = runAt, runAt, 1
	m.CaptureGaps[key] = g
	return key, true
}

// gapBucketCountLocked counts token's live entries against one lifetime cap.
// The caller holds m.mu.
func (m *Manifest) gapBucketCountLocked(token, bucket string) int {
	n := 0
	for _, e := range m.CaptureGaps {
		if e.Token == token && gapBucket(e) == bucket {
			n++
		}
	}
	return n
}

// overflowCountLocked counts token's overflow entries. The caller holds m.mu.
func (m *Manifest) overflowCountLocked(token string) int {
	n := 0
	for _, e := range m.CaptureGaps {
		if e.Token == token && e.Scope == GapScopeOverflow {
			n++
		}
	}
	return n
}

// absorbGapLocked counts g into its folder's overflow entry instead of adding a
// row (P13): the entry's Skipped is the exact number absorbed this run and its
// class the most severe class absorbed this run. It returns the overflow entry's
// key. The caller holds m.mu.
func (m *Manifest) absorbGapLocked(g CaptureGap, runAt time.Time) string {
	loc := overflowLocator(g.Folder)
	key := GapKey(g.Token, GapScopeOverflow, loc)
	o, ok := m.CaptureGaps[key]
	if !ok && m.overflowCountLocked(g.Token) >= maxGapOverflow {
		// A source whose faults name a new folder each time is bounded here, not by the
		// folder caps: the excess is counted in the token's own entry (the store root's).
		g.Folder = ""
		loc = overflowLocator("")
		key = GapKey(g.Token, GapScopeOverflow, loc)
		o, ok = m.CaptureGaps[key]
	}
	switch {
	case !ok:
		o = CaptureGap{
			Token: g.Token, Folder: g.Folder, Scope: GapScopeOverflow, Locator: loc,
			Reason: "overflow", Class: g.Class, FirstRun: runAt, Runs: 1, Detail: overflowDetail,
		}
	case !o.LastRun.Equal(runAt):
		if !sameUTCDay(o.LastRun, runAt) {
			o.Runs++
		}
		o.Skipped = 0
		o.Class = g.Class
	}
	o.Skipped++
	o.Class = moreSevere(o.Class, g.Class)
	o.LastRun = runAt
	m.CaptureGaps[key] = o
	return key
}

// CloseGap removes the entry stored under key (an item exported after all, or a
// completed walk that no longer finds it) and reports whether there was one.
func (m *Manifest) CloseGap(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.CaptureGaps[key]; !ok {
		return false
	}
	delete(m.CaptureGaps, key)
	return true
}

// exportStageReason says whether a gap comes from the export step, which a
// -since run never reached for an item outside its window.
func exportStageReason(reason string) bool {
	switch reason {
	case "export-failed", "export-panic", "folder-unwritable":
		return true
	}
	return false
}

// CloseUnobserved closes, at the end of a COMPLETED walk of token, every entry of
// that token this run did not observe (LastRun != runAt) and whose item or folder
// the run's scope covered [FC13, FC14]: "no longer in the source". scopeFn says
// whether the run's walk covered the gap's folder (nil = every folder of the
// token); a gap whose folder was not read is retained. Two more rules keep a
// gap that the run did not look for: an item gap dated before since is never
// closed by absence, and in a run with since set an export-stage gap
// (export-failed, export-panic, folder-unwritable) closes only by recovery or by
// being observed again. A walk aborted by a run-level fault closes nothing: the
// caller does not call this. It returns how many entries it closed.
func (m *Manifest) CloseUnobserved(token string, scopeFn func(CaptureGap) bool, runAt, since time.Time) (closed int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, g := range m.CaptureGaps {
		if g.Token != token || g.LastRun.Equal(runAt) {
			continue
		}
		if scopeFn != nil && !scopeFn(g) {
			continue
		}
		if !since.IsZero() {
			if exportStageReason(g.Reason) {
				continue
			}
			if g.Scope == GapScopeItem && g.Received != "" {
				if when, err := time.Parse(time.RFC3339, g.Received); err == nil && when.Before(since) {
					continue
				}
			}
		}
		delete(m.CaptureGaps, key)
		closed++
	}
	return closed
}

// CaptureGapsSorted returns a snapshot of the ledger in a stable order: store
// token, folder, reason, locator.
func (m *Manifest) CaptureGapsSorted() []CaptureGap {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]CaptureGap, 0, len(m.CaptureGaps))
	for _, g := range m.CaptureGaps {
		out = append(out, g)
	}
	SortGaps(out)
	return out
}

// SortGaps puts gaps in the ledger's stable order: store token, folder, reason,
// locator.
func SortGaps(gs []CaptureGap) {
	sort.Slice(gs, func(i, j int) bool {
		a, b := gs[i], gs[j]
		switch {
		case a.Token != b.Token:
			return a.Token < b.Token
		case a.Folder != b.Folder:
			return a.Folder < b.Folder
		case a.Reason != b.Reason:
			return a.Reason < b.Reason
		}
		return a.Locator < b.Locator
	})
}

// GapAttempted reports whether the gap was observed or retried on its store's
// last run (its LastRun equals that token's last run, FC14); any other gap is
// not attempted: its source or folder was not walked.
func GapAttempted(g CaptureGap, lastRunAt map[string]time.Time) bool {
	t, ok := lastRunAt[g.Token]
	return ok && g.LastRun.Equal(t)
}

// GapStuck reports whether a gap has escalated to WARN capture_gaps_stuck
// (owner ruling OR5): a fillable gap with Runs >= 3 whose first observation is at
// least max(72 h, two schedule periods) old. Only an attempted gap can be stuck
// (the caller checks GapAttempted); terminal and info gaps never are.
func GapStuck(g CaptureGap, now time.Time, period time.Duration) bool {
	if g.Class != ClassFillable || g.Runs < stuckRuns {
		return false
	}
	age := stuckAge
	if p := 2 * period; p > age {
		age = p
	}
	return now.Sub(g.FirstRun) >= age
}

// GapTally is the status counts of a set of gaps.
type GapTally struct {
	Fillable, Terminal, Info int       // attempted gaps, by class
	Stuck                    int       // attempted fillable gaps that escalated (a subset of Fillable)
	NotAttempted             int       // fillable and terminal gaps whose source or folder was not walked on its last run
	Oldest                   time.Time // the earliest FirstRun among the fillable and terminal gaps
}

// TallyGaps counts gaps by class and attempted state (FC14). An overflow entry
// stands for the items it absorbed. lastRunAt maps each store token to its last
// run's timestamp; period is the schedule period the stuck threshold uses.
func TallyGaps(gaps []CaptureGap, lastRunAt map[string]time.Time, now time.Time, period time.Duration) GapTally {
	var t GapTally
	for _, g := range gaps {
		w := g.Weight()
		missing := g.Class == ClassFillable || g.Class == ClassTerminal
		if missing && (t.Oldest.IsZero() || g.FirstRun.Before(t.Oldest)) {
			t.Oldest = g.FirstRun
		}
		if !GapAttempted(g, lastRunAt) {
			if missing {
				t.NotAttempted += w
			}
			continue
		}
		switch g.Class {
		case ClassFillable:
			t.Fillable += w
			if GapStuck(g, now, period) {
				t.Stuck += w
			}
		case ClassTerminal:
			t.Terminal += w
		case ClassInfo:
			t.Info += w
		}
	}
	return t
}

// CaptureGapCounts is TallyGaps over the manifest's ledger, in the shape of the
// design's accessor (§4.2): attempted fillable, terminal and info gaps, how many
// of the fillable ones are stuck, the not-attempted fillable and terminal gaps,
// and the oldest first observation among them.
func (m *Manifest) CaptureGapCounts(lastRunAt map[string]time.Time, now time.Time, period time.Duration) (fillable, terminal, info, stuck, notAttempted int, oldest time.Time) {
	t := TallyGaps(m.CaptureGapsSorted(), lastRunAt, now, period)
	return t.Fillable, t.Terminal, t.Info, t.Stuck, t.NotAttempted, t.Oldest
}

// MissingGapCount is how many items and folders the ledger says are missing from
// the archive: its fillable and terminal entries, an overflow entry for the items
// it absorbed (an info entry is nothing missing).
func (m *Manifest) MissingGapCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, g := range m.CaptureGaps {
		if g.Class == ClassFillable || g.Class == ClassTerminal {
			n += g.Weight()
		}
	}
	return n
}

// GapCount is the number of ledger entries.
func (m *Manifest) GapCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.CaptureGaps)
}

// sanitizeGaps is Load's treatment of the ledger as UNTRUSTED, like
// ReadLastRun (MA-157): it bounds and strips every string, keeps a known class
// even when the reason is unknown and maps an unknown class to terminal [FC17],
// drops an entry whose token is not a SafeToken or is not a value in Stores
// [FC14], clamps FirstRun and LastRun to now [FC14], rebuilds each key from the
// cleaned fields, and applies the lifetime caps (per token) and a total cap,
// keeping the rest as overflow entries that count it exactly [FC16]. Deterministic:
// entries are visited in key order. Load is single-threaded construction, so it
// takes no lock.
func (m *Manifest) sanitizeGaps(now time.Time) {
	if len(m.CaptureGaps) == 0 {
		m.CaptureGaps = nil
		return
	}
	tokens := make(map[string]bool, len(m.Stores))
	for _, t := range m.Stores {
		tokens[t] = true
	}
	keys := make([]string, 0, len(m.CaptureGaps))
	for k := range m.CaptureGaps {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	clean := map[string]CaptureGap{}
	for _, k := range keys {
		g := sanitizeGap(m.CaptureGaps[k])
		g.Token = util.StripControl(g.Token)
		if !SafeToken(g.Token) || !tokens[g.Token] {
			continue
		}
		if g.FirstRun.After(now) {
			g.FirstRun = now
		}
		if g.LastRun.After(now) {
			g.LastRun = now
		}
		if g.FirstRun.After(g.LastRun) {
			g.FirstRun = g.LastRun
		}
		if g.Runs < 1 {
			g.Runs = 1
		}
		nk := GapKey(g.Token, g.Scope, g.Locator)
		if prev, dup := clean[nk]; dup {
			// Two entries that clean to one key: keep the later observation.
			if prev.LastRun.After(g.LastRun) {
				g = prev
			}
		}
		clean[nk] = g
	}

	// Lifetime caps per token, then the total: the excess is counted in the
	// folder's (or, for the total, the token's) overflow entry.
	ordered := make([]string, 0, len(clean))
	for k := range clean {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	type bucketKey struct{ token, bucket string }
	used := map[bucketKey]int{}
	out := make(map[string]CaptureGap, len(clean))
	var excess []CaptureGap
	for _, k := range ordered {
		g := clean[k]
		if b := gapBucket(g); b != "" {
			bk := bucketKey{g.Token, b}
			if used[bk] >= gapBucketCap(b) {
				excess = append(excess, g)
				continue
			}
			used[bk]++
		}
		out[k] = g
	}
	fold := func(into map[string]CaptureGap, g CaptureGap, weight int, folder string) {
		loc := overflowLocator(folder)
		key := GapKey(g.Token, GapScopeOverflow, loc)
		o, ok := into[key]
		if !ok {
			o = CaptureGap{
				Token: g.Token, Folder: folder, Scope: GapScopeOverflow, Locator: loc,
				Reason: "overflow", Class: g.Class, FirstRun: g.FirstRun, LastRun: g.LastRun, Runs: g.Runs,
				Detail: overflowDetail,
			}
		}
		o.Skipped += weight
		o.Class = moreSevere(o.Class, g.Class)
		if g.LastRun.After(o.LastRun) {
			o.LastRun = g.LastRun
		}
		if g.FirstRun.Before(o.FirstRun) {
			o.FirstRun = g.FirstRun
		}
		into[key] = o
	}
	for _, g := range excess {
		fold(out, g, g.Weight(), g.Folder)
	}
	if len(out) > maxGapTotal {
		all := make([]string, 0, len(out))
		for k := range out {
			all = append(all, k)
		}
		sort.Strings(all)
		for _, k := range all[maxGapTotal:] {
			g := out[k]
			delete(out, k)
			fold(out, g, g.Weight(), "")
		}
	}
	m.CaptureGaps = out
	if len(m.CaptureGaps) == 0 {
		m.CaptureGaps = nil
	}
}

// GapSourceID is the store id (the original input path or the Graph mailbox
// address, prefixed path: or mailbox:) that owns token, from the reverse of
// Stores; "" when none does. When two ids map to one token the smaller wins, so
// the answer is stable.
func (m *Manifest) GapSourceID(token string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	best := ""
	for id, t := range m.Stores {
		if t == token && (best == "" || id < best) {
			best = id
		}
	}
	return best
}

// SourceDisplay is a store id as an operator reads it: the mailbox address or
// the input path, without the id's own prefix, control-stripped.
func SourceDisplay(id string) string {
	id = strings.TrimPrefix(strings.TrimPrefix(id, "path:"), "mailbox:")
	return util.StripBidi(util.StripControl(id))
}

// Source kinds a remedy is keyed by (fault-isolation §4.10, FC23(6)).
const (
	SourceGraph          = "graph"           // a Microsoft 365 mailbox read through Graph
	SourcePST            = "pst"             // an Outlook .pst/.ost data file
	SourceOutlookScratch = "outlook-scratch" // a .pst that -outlook's Outlook export wrote and rebuilds every run
	SourceLocal          = "local"           // an mbox, maildir or Evolution store
)

// SourceKind says what kind of source a store id names.
func SourceKind(id string) string {
	if strings.HasPrefix(id, "mailbox:") {
		return SourceGraph
	}
	p := strings.ToLower(strings.TrimPrefix(id, "path:"))
	switch {
	case strings.Contains(p, "_outlook-pst/") || strings.Contains(p, `_outlook-pst\`):
		return SourceOutlookScratch
	case strings.HasSuffix(p, ".pst") || strings.HasSuffix(p, ".ost"):
		return SourcePST
	}
	return SourceLocal
}
