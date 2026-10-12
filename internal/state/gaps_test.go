package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

var gapBase = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

// gapManifest is a manifest on disk with one registered store; it returns the
// manifest, its path and the store's token (the ledger drops a token that is not
// a value in Stores at Load).
func gapManifest(t *testing.T) (*Manifest, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".mailarchive-manifest.json")
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return m, path, m.Token("mailbox:ann@contoso.com", "ann@contoso.com")
}

func reload(t *testing.T, m *Manifest, path string) *Manifest {
	t.Helper()
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	m2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return m2
}

func itemGap(token, loc string) CaptureGap {
	return CaptureGap{Token: token, Folder: "Inbox", Scope: GapScopeItem, Locator: loc, Reason: "fetch-failed", Class: ClassFillable}
}

// covers: MA-323, R1, R5, S46
// The ledger's lifecycle (§4.2, §4.3): it survives Save and Load, a manifest
// written before it exists loads empty, Runs counts runs (not calls, one per UTC
// day), Skipped resets per run, ObserveGap follows the item's folder, a completed
// walk closes the gaps its folder no longer holds and keeps the rest (shown as
// not attempted), and a rolling -since window never closes a gap dated before it.
func TestCaptureGapLedgerLifecycle(t *testing.T) {
	m, path, tok := gapManifest(t)

	// A v6 manifest that has no capture_gaps key loads with an empty ledger.
	legacy := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"version":6,"entries":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if lm, err := Load(legacy); err != nil || lm.GapCount() != 0 {
		t.Fatalf("a v6 manifest without capture_gaps: err %v, %d gaps; want nil, 0", err, lm.GapCount())
	}

	// First observation: one entry, Runs 1, FirstRun = LastRun = the run.
	g := itemGap(tok, "graph:AAA")
	g.Subject, g.Received, g.Detail = "Quarterly report", "2026-03-02T09:00:00Z", "500 ErrorMimeContentConversionFailed"
	key, created := m.ObserveGap(g, gapBase)
	if !created || key != GapKey(tok, GapScopeItem, "graph:AAA") {
		t.Fatalf("ObserveGap = (%q, %v); want a new entry under token\\x00item\\x00locator", key, created)
	}
	m2 := reload(t, m, path)
	got := m2.CaptureGapsSorted()
	if len(got) != 1 || got[0].Runs != 1 || !got[0].FirstRun.Equal(gapBase) || got[0].Subject != "Quarterly report" || got[0].Class != ClassFillable {
		t.Fatalf("after Save and Load the ledger holds %+v; want the one entry, Runs 1, unchanged", got)
	}

	// Two observations in ONE run count once, and Skipped accumulates within it.
	m = m2
	f := CaptureGap{Token: tok, Folder: "Projects", Scope: GapScopeFolder, Locator: "folder:Projects", Reason: "folder-unwritable", Class: ClassFillable, Skipped: 1}
	m.ObserveGap(f, gapBase)
	m.ObserveGap(f, gapBase)
	fk := GapKey(tok, GapScopeFolder, "folder:Projects")
	if e := m.CaptureGaps[fk]; e.Runs != 1 || e.Skipped != 2 {
		t.Fatalf("two observations in one run: Runs %d Skipped %d; want 1 and 2", e.Runs, e.Skipped)
	}

	// A LATER run (next day) increments Runs, keeps FirstRun, resets Skipped and
	// follows the folder and the subject the later observation carries [FC14].
	next := gapBase.Add(26 * time.Hour)
	moved := f
	moved.Folder, moved.Subject = "Projects/2026", "Moved item"
	m.ObserveGap(moved, next)
	e := m.CaptureGaps[fk]
	if e.Runs != 2 || !e.FirstRun.Equal(gapBase) || !e.LastRun.Equal(next) || e.Skipped != 1 || e.Folder != "Projects/2026" || e.Subject != "Moved item" {
		t.Fatalf("a later run: %+v; want Runs 2, FirstRun kept, Skipped reset to 1, Folder and Subject updated", e)
	}
	// A second run on the SAME UTC day is one observation (owner ruling OR5).
	m.ObserveGap(moved, next.Add(2*time.Hour))
	if e := m.CaptureGaps[fk]; e.Runs != 2 {
		t.Errorf("a second run on the same day: Runs %d, want 2 (at most one observation per day)", e.Runs)
	}

	// Completed walk of the token: the item gap was not re-observed this run and
	// its folder was read, so it closes; the folder gap the run did observe stays;
	// a gap in a folder the walk never read is retained and counts as not attempted.
	m.ObserveGap(CaptureGap{Token: tok, Folder: "Archive", Scope: GapScopeItem, Locator: "graph:UNWALKED", Reason: "fetch-failed", Class: ClassFillable}, gapBase)
	run3 := next.Add(48 * time.Hour)
	m.ObserveGap(moved, run3)
	walked := func(g CaptureGap) bool { return g.Folder != "Archive" }
	if n := m.CloseUnobserved(tok, walked, run3, time.Time{}); n != 1 {
		t.Fatalf("CloseUnobserved closed %d, want 1 (the item gap whose folder was fully read)", n)
	}
	if _, ok := m.CaptureGaps[GapKey(tok, GapScopeItem, "graph:AAA")]; ok {
		t.Error("the gap in the walked folder that was not re-observed is still there")
	}
	if _, ok := m.CaptureGaps[GapKey(tok, GapScopeItem, "graph:UNWALKED")]; !ok {
		t.Error("the gap in the unwalked folder was closed; it must be retained")
	}
	fill, term, info, stuck, notAtt, oldest := m.CaptureGapCounts(map[string]time.Time{tok: run3}, run3, 24*time.Hour)
	// The folder gap was attempted on 3 runs on 3 days, 74 h apart from first to
	// last: it is stuck (OR5); the unwalked folder's gap is not attempted.
	if fill != 1 || term != 0 || info != 0 || stuck != 1 || notAtt != 1 || !oldest.Equal(gapBase) {
		t.Errorf("counts = fillable %d terminal %d info %d stuck %d not-attempted %d oldest %v; want 1 0 0 1 1 %v (attempted vs not)", fill, term, info, stuck, notAtt, oldest, gapBase)
	}

	// A rolling -since window: a gap dated before it is retained although this
	// run did not re-observe it, and an export-stage gap closes only by recovery
	// or being observed again [FC13].
	old := itemGap(tok, "graph:OLD")
	old.Received = "2026-01-05T08:00:00Z"
	m.ObserveGap(old, gapBase)
	inWindow := itemGap(tok, "graph:INWINDOW")
	inWindow.Received = "2026-09-20T08:00:00Z"
	m.ObserveGap(inWindow, gapBase)
	exportStage := itemGap(tok, "file:exp")
	exportStage.Reason, exportStage.Received = "export-failed", "2026-09-21T08:00:00Z"
	m.ObserveGap(exportStage, gapBase)
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	run4 := run3.Add(24 * time.Hour)
	m.CloseUnobserved(tok, nil, run4, since)
	if _, ok := m.CaptureGaps[GapKey(tok, GapScopeItem, "graph:OLD")]; !ok {
		t.Error("a gap dated before -since was closed by absence; it must be retained and still reported")
	}
	if _, ok := m.CaptureGaps[GapKey(tok, GapScopeItem, "graph:INWINDOW")]; ok {
		t.Error("a gap inside the -since window that was not re-observed should have closed")
	}
	if _, ok := m.CaptureGaps[GapKey(tok, GapScopeItem, "file:exp")]; !ok {
		t.Error("an export-stage gap closed in a -since run; it closes only by recovery or re-observation")
	}

	// CloseGap (an item exported after all) removes exactly the one entry.
	if !m.CloseGap(GapKey(tok, GapScopeItem, "graph:OLD")) || m.CloseGap(GapKey(tok, GapScopeItem, "graph:OLD")) {
		t.Error("CloseGap must remove the entry once and report false the second time")
	}
}

// covers: MA-323, R1, R5, S46
// P13: the lifetime caps are per token and over the archive's life. Past 1000
// item entries, 1000 folder entries or 100 info entries, a new entry is not
// added: its folder's overflow entry counts it exactly, with the most severe
// class it absorbed (so it still feeds partial), and info never consumes the item
// cap.
func TestCaptureGapLifetimeCaps(t *testing.T) {
	m, path, tok := gapManifest(t)
	for i := 0; i < maxGapItems; i++ {
		m.ObserveGap(itemGap(tok, fmt.Sprintf("graph:%d", i)), gapBase)
	}
	// The 1001st and 1002nd fillable and a terminal one are counted, not added.
	_, created := m.ObserveGap(itemGap(tok, "graph:1001"), gapBase)
	m.ObserveGap(itemGap(tok, "graph:1002"), gapBase)
	term := itemGap(tok, "graph:1003")
	term.Class = ClassTerminal
	m.ObserveGap(term, gapBase)
	if created {
		t.Error("the 1001st item gap was added as an entry; it must be counted in the overflow entry")
	}
	ok := GapKey(tok, GapScopeOverflow, "overflow:Inbox")
	o, found := m.CaptureGaps[ok]
	if !found || o.Skipped != 3 || o.Class != ClassTerminal || o.Scope != GapScopeOverflow {
		t.Fatalf("overflow entry = %+v (found %v); want Skipped 3 and class terminal, the most severe absorbed", o, found)
	}
	if n := m.gapBucketCountLocked(tok, GapScopeItem); n != maxGapItems {
		t.Errorf("%d live item entries, want exactly the cap %d", n, maxGapItems)
	}

	// Info has its own pool: it is added although the item cap is full.
	for i := 0; i < maxGapInfo; i++ {
		ig := itemGap(tok, fmt.Sprintf("ident:%d", i))
		ig.Reason, ig.Class = "refresh-failed", ClassInfo
		if _, c := m.ObserveGap(ig, gapBase); !c {
			t.Fatalf("info gap %d was not added: info must not consume (or be blocked by) the item cap", i)
		}
	}
	ig := itemGap(tok, "ident:over")
	ig.Reason, ig.Class = "refresh-failed", ClassInfo
	if _, c := m.ObserveGap(ig, gapBase); c {
		t.Error("the 101st info gap was added; the info cap is 100")
	}
	// It was counted in the folder's overflow entry, which keeps the class the
	// terminal item gave it (info is the least severe).
	if o := m.CaptureGaps[ok]; o.Skipped != 4 || o.Class != ClassTerminal {
		t.Errorf("after the 101st info gap the overflow is %+v; want 4 absorbed, still terminal", o)
	}

	// Folder scope has its own cap too.
	for i := 0; i < maxGapFolders; i++ {
		m.ObserveGap(CaptureGap{Token: tok, Folder: fmt.Sprintf("F%d", i), Scope: GapScopeFolder, Locator: fmt.Sprintf("folder:F%d", i), Reason: "folder-unreadable", Class: ClassTerminal}, gapBase)
	}
	if _, c := m.ObserveGap(CaptureGap{Token: tok, Folder: "Fover", Scope: GapScopeFolder, Locator: "folder:Fover", Reason: "folder-unreadable", Class: ClassFillable}, gapBase); c {
		t.Error("the 1001st folder gap was added; the folder cap is 1000")
	}
	if fo := m.CaptureGaps[GapKey(tok, GapScopeOverflow, "overflow:Fover")]; fo.Skipped != 1 || fo.Class != ClassFillable {
		t.Errorf("the folder overflow = %+v; want 1 absorbed, fillable", fo)
	}

	// The overflow survives Save and Load, and a LATER run resets its count and
	// class to what that run absorbed (the cap is a lifetime bound, the count a
	// per-run exact one).
	m = reload(t, m, path)
	if o := m.CaptureGaps[ok]; o.Skipped != 4 || o.Class != ClassTerminal {
		t.Fatalf("after Save and Load the overflow is %+v; want it unchanged", o)
	}
	later := gapBase.Add(30 * time.Hour)
	m.ObserveGap(itemGap(tok, "graph:1001"), later)
	if o := m.CaptureGaps[ok]; o.Skipped != 1 || o.Class != ClassFillable || o.Runs != 2 {
		t.Errorf("a later run's overflow = %+v; want Skipped 1, class fillable (this run's), Runs 2", o)
	}
	if w := m.CaptureGaps[ok].Weight(); w != 1 {
		t.Errorf("an overflow entry's weight is its absorbed count: got %d, want 1", w)
	}
}

// covers: MA-323, R1, R5, S46
// Load treats the ledger as untrusted (MA-157): strings are stripped and bounded;
// an entry whose token is not a SafeToken or not a value in Stores is dropped; a
// future time is clamped; a known class survives an unknown reason; an unknown
// class fails closed to terminal (never info); the per-token caps and a total cap
// hold for a hostile manifest, the excess counted in overflow entries.
func TestCaptureGapLoadSanitisation(t *testing.T) {
	m, path, tok := gapManifest(t)
	now := time.Now().UTC()
	raw := func(g CaptureGap) CaptureGap {
		g.FirstRun, g.LastRun, g.Runs = gapBase, gapBase, 1
		return g
	}
	hostile := raw(itemGap(tok, "graph:HOSTILE"))
	hostile.Detail = "\x1b[2J\x1b]0;pwned\x07boom\n" + strings.Repeat("x", 5000)
	hostile.Subject = "line one\r\nline two " + strings.Repeat("é", 1000)
	hostile.Reason = "new-\x1breason"
	hostile.Class = "fillable"
	future := raw(itemGap(tok, "graph:FUTURE"))
	future.FirstRun, future.LastRun = now.Add(400*24*time.Hour), now.Add(500*24*time.Hour)
	unknownClass := raw(itemGap(tok, "graph:CLASS"))
	unknownClass.Class = "from-a-newer-build"
	badToken := raw(itemGap("../escape", "graph:BADTOKEN"))
	foreignToken := raw(itemGap("not-a-store", "graph:FOREIGN"))

	m.CaptureGaps = map[string]CaptureGap{}
	for _, g := range []CaptureGap{hostile, future, unknownClass, badToken, foreignToken} {
		m.CaptureGaps[GapKey(g.Token, g.Scope, g.Locator)] = g
	}
	m = reload(t, m, path)
	by := map[string]CaptureGap{}
	for _, g := range m.CaptureGapsSorted() {
		by[g.Locator] = g
	}
	assure.Reached(t, by, "the entries that survive Load")

	h := by["graph:HOSTILE"]
	if len(h.Detail) > 300 || strings.ContainsAny(h.Detail, "\x1b\x07\n") || strings.Contains(h.Detail, "pwned\x07") {
		t.Errorf("hostile Detail survived Load: %d bytes %q", len(h.Detail), h.Detail[:min(40, len(h.Detail))])
	}
	if n := len([]rune(h.Subject)); n > 200 || strings.ContainsAny(h.Subject, "\r\n") {
		t.Errorf("hostile Subject survived Load: %d runes", n)
	}
	if h.Class != ClassFillable || h.Reason != "new-reason" {
		t.Errorf("a known class with an unknown reason: class %q reason %q; want fillable kept, reason bounded and stripped", h.Class, h.Reason)
	}
	if u := by["graph:CLASS"]; u.Class != ClassTerminal {
		t.Errorf("an unknown class loaded as %q; it must fail closed to terminal, never info", u.Class)
	}
	if f := by["graph:FUTURE"]; f.LastRun.After(time.Now()) || f.FirstRun.After(time.Now()) {
		t.Errorf("a future time survived Load: first %v last %v", f.FirstRun, f.LastRun)
	}
	if _, ok := by["graph:BADTOKEN"]; ok {
		t.Error("an entry with a token that is not a SafeToken survived Load")
	}
	if _, ok := by["graph:FOREIGN"]; ok {
		t.Error("an entry whose token is not a value in Stores survived Load")
	}

	// A hostile manifest past the per-token caps: only the cap is kept, the rest
	// is counted in overflow entries.
	m.CaptureGaps = map[string]CaptureGap{}
	for i := 0; i < maxGapItems+250; i++ {
		g := raw(itemGap(tok, fmt.Sprintf("graph:%05d", i)))
		m.CaptureGaps[GapKey(tok, g.Scope, g.Locator)] = g
	}
	m = reload(t, m, path)
	items, absorbed := 0, 0
	for _, g := range m.CaptureGapsSorted() {
		switch g.Scope {
		case GapScopeItem:
			items++
		case GapScopeOverflow:
			absorbed += g.Skipped
		}
	}
	if items != maxGapItems || absorbed != 250 {
		t.Errorf("a manifest with %d item gaps loaded as %d entries + %d absorbed; want %d + 250", maxGapItems+250, items, absorbed, maxGapItems)
	}

	// The total is capped too: a manifest of many tokens' worth of gaps keeps
	// maxGapTotal entries and counts the rest, weight for weight.
	big, bpath, _ := gapManifest(t)
	total := 0
	for i := 0; i < 25; i++ {
		tk := big.Token(fmt.Sprintf("mailbox:u%d@x", i), fmt.Sprintf("u%d", i))
		for j := 0; j < 900; j++ {
			g := raw(itemGap(tk, fmt.Sprintf("graph:%d", j)))
			big.CaptureGaps = ensure(big.CaptureGaps)
			big.CaptureGaps[GapKey(tk, g.Scope, g.Locator)] = g
			total++
		}
	}
	big = reload(t, big, bpath)
	entries, weight := 0, 0
	for _, g := range big.CaptureGapsSorted() {
		entries++
		weight += g.Weight()
	}
	if entries > maxGapTotal+25 || weight != total {
		t.Errorf("a %d-gap manifest loaded as %d entries weighing %d; want at most %d entries and every gap still counted", total, entries, weight, maxGapTotal+25)
	}
}

func ensure(m map[string]CaptureGap) map[string]CaptureGap {
	if m == nil {
		return map[string]CaptureGap{}
	}
	return m
}

// covers: MA-323, R1, R5, S46
// The stuck threshold (owner ruling OR5): a fillable gap with Runs >= 3 whose first
// observation is at least max(72 h, two schedule periods) old. Terminal and info
// gaps never are, and a gap that was not attempted on its store's last run never
// is (FC14).
func TestCaptureGapStuckThreshold(t *testing.T) {
	now := gapBase.Add(10 * 24 * time.Hour)
	g := CaptureGap{Token: "t", Class: ClassFillable, Runs: 3, FirstRun: now.Add(-73 * time.Hour), LastRun: now}
	cases := []struct {
		name   string
		mutate func(*CaptureGap)
		period time.Duration
		want   bool
	}{
		{"3 runs and 73 h on a daily schedule", func(*CaptureGap) {}, 24 * time.Hour, true},
		{"2 runs is not enough", func(g *CaptureGap) { g.Runs = 2 }, 24 * time.Hour, false},
		{"71 h is not enough", func(g *CaptureGap) { g.FirstRun = now.Add(-71 * time.Hour) }, 24 * time.Hour, false},
		{"two weekly periods outlast 72 h", func(*CaptureGap) {}, 7 * 24 * time.Hour, false},
		{"two weekly periods elapsed", func(g *CaptureGap) { g.FirstRun = now.Add(-15 * 24 * time.Hour) }, 7 * 24 * time.Hour, true},
		{"terminal never escalates", func(g *CaptureGap) { g.Class = ClassTerminal }, 24 * time.Hour, false},
		{"info never escalates", func(g *CaptureGap) { g.Class = ClassInfo }, 24 * time.Hour, false},
	}
	for _, c := range cases {
		x := g
		c.mutate(&x)
		if got := GapStuck(x, now, c.period); got != c.want {
			t.Errorf("%s: GapStuck = %v, want %v", c.name, got, c.want)
		}
	}
	// Stuck feeds the tally only through an attempted gap.
	last := map[string]time.Time{"t": now}
	if tl := TallyGaps([]CaptureGap{g}, last, now, 24*time.Hour); tl.Stuck != 1 || tl.Fillable != 1 || tl.NotAttempted != 0 {
		t.Errorf("an attempted stuck gap tallies %+v; want fillable 1, stuck 1", tl)
	}
	if tl := TallyGaps([]CaptureGap{g}, map[string]time.Time{"t": now.Add(time.Hour)}, now, 24*time.Hour); tl.Stuck != 0 || tl.Fillable != 0 || tl.NotAttempted != 1 {
		t.Errorf("a gap not attempted on its token's last run tallies %+v; want not-attempted 1 and never stuck", tl)
	}
}

// covers: MA-323, R1, R5, S46
// state.GapClass is the single owner of record gap labels (J5): the partly-read
// labels are fillable (Missing), unparsed is terminal and superseded is info; an
// ordinary content label has no class of its own, so the list it sits in decides.
// Counts treats an info-class terminal label as nothing missing.
func TestGapClassOwnsTheLabels(t *testing.T) {
	for label, want := range map[string]string{
		"unreadable:body":        ClassFillable,
		"unreadable:attachments": ClassFillable,
		"unparsed":               ClassTerminal,
		"superseded":             ClassInfo,
		"body":                   "",
		"report.pdf":             "",
	} {
		if got := GapClass(label); got != want {
			t.Errorf("GapClass(%q) = %q, want %q", label, got, want)
		}
	}
	if RecordGapClass("body", true) != ClassTerminal || RecordGapClass("body", false) != ClassFillable || RecordGapClass("superseded", true) != ClassInfo {
		t.Error("RecordGapClass must let the list decide for an ordinary label and GapClass for a special one")
	}

	m, _, tok := gapManifest(t)
	m.Add(Key(tok, "Inbox", "mid:a"), Record{Path: tok + "/Inbox/a.html", Missing: []string{"body"}})
	m.Add(Key(tok, "Inbox", "mid:b"), Record{Path: tok + "/Inbox/b.html", Missing: []string{"unreadable:attachments"}, Locator: "pst-nid:u/1"})
	m.Add(Key(tok, "Inbox", "mid:c"), Record{Path: tok + "/Inbox/c.html", Terminal: []string{"unparsed"}})
	m.Add(Key(tok, "Inbox", "mid:d"), Record{Path: tok + "/Inbox/d.html", Terminal: []string{"superseded"}, RecoveredBy: Key(tok, "Inbox", "mid:a")})
	m.Add(Key(tok, "Inbox", "mid:e"), Record{Path: tok + "/Inbox/e.html", Terminal: []string{"report.pdf"}})
	fillable, terminal, unknown := m.Counts()
	if fillable != 2 || terminal != 2 || unknown != 0 {
		t.Errorf("Counts = fillable %d terminal %d unknown %d; want 2 2 0 (a superseded record is nothing missing)", fillable, terminal, unknown)
	}
	if n := m.UnreadableCount(); n != 1 {
		t.Errorf("UnreadableCount = %d, want 1", n)
	}
	if n := m.UnparsedCount(); n != 1 {
		t.Errorf("UnparsedCount = %d, want 1", n)
	}
	if any, only := m.UnreadableCount(), m.UnreadableOnlyCount(); any != 1 || only != 1 {
		t.Errorf("unreadable records: %d, of which only unreadable %d; want 1 and 1", any, only)
	}
}

// covers: MA-323, R1, R5, S46
// Load drops a Locator from a record that carries no unreadable:* label and a
// RecoveredBy that names no existing key (§4.2, FC6(4)); a Locator that belongs
// survives.
func TestRecordLocatorAndRecoveredByAreChecked(t *testing.T) {
	m, path, tok := gapManifest(t)
	k1, k2, k3 := Key(tok, "Inbox", "mid:a"), Key(tok, "Inbox", "mid:b"), Key(tok, "Inbox", "mid:c")
	m.Add(k1, Record{Path: tok + "/a.html", Missing: []string{"unreadable:body"}, Locator: "pst-nid:u/1"})
	m.Add(k2, Record{Path: tok + "/b.html", Locator: "pst-nid:u/2", RecoveredBy: "no\x00such\x00key"})
	m.Add(k3, Record{Path: tok + "/c.html", Terminal: []string{"superseded"}, RecoveredBy: k1})
	m = reload(t, m, path)
	r1, _ := m.Get(k1)
	r2, _ := m.Get(k2)
	r3, _ := m.Get(k3)
	if r1.Locator != "pst-nid:u/1" {
		t.Errorf("the partly-read record lost its Locator: %q", r1.Locator)
	}
	if r2.Locator != "" || r2.RecoveredBy != "" {
		t.Errorf("a record without an unreadable label kept Locator %q and RecoveredBy %q; Load must drop both", r2.Locator, r2.RecoveredBy)
	}
	if r3.RecoveredBy != k1 {
		t.Errorf("a RecoveredBy that names an existing key was dropped: %q", r3.RecoveredBy)
	}
	if k, ok := m.KeyForLocator(tok, "pst-nid:u/1"); !ok || k != k1 {
		t.Errorf("KeyForLocator = %q, %v; want the partly-read record", k, ok)
	}
	if _, ok := m.KeyForLocator(tok, "pst-nid:u/2"); ok {
		t.Error("KeyForLocator found a record that carries no unreadable label")
	}
}

// covers: MA-323, R1, R5, S46
// A legacy collapsed stub (F33's merged record) is re-classed to
// legacy-placeholder once, on a run only, and `status` never writes it.
func TestLegacyPlaceholderReclassOnce(t *testing.T) {
	const legacy = "sha:0123456789abcdef"
	m, path, tok := gapManifest(t)
	stub := func(folder string) Record {
		return Record{Path: tok + "/" + folder + "/stub.html", Folder: folder, Missing: []string{MissingBody}, Subject: LegacyPlaceholderSubject}
	}
	k1, k2 := Key(tok, "Inbox", legacy), Key(tok, "Sent", legacy)
	kGraph, kOther, kHealthy := Key(tok, "Inbox", "sha:graphstub"), Key(tok, "Inbox", legacy+"x"), Key(tok, "Inbox", "mid:a")
	m.Add(k1, stub("Inbox"))
	m.Add(k2, stub("Sent"))
	// A Graph legacy stub has a distinct identity (and Terminal:["body"]): it is the
	// Graph walk's to adopt (FC27, S3), never this re-class's.
	m.Add(kGraph, Record{Path: tok + "/g.html", Terminal: []string{MissingBody}, Subject: LegacyPlaceholderSubject})
	// The legacy identity with a real subject, and a real message, are not stubs.
	m.Add(kOther, Record{Path: tok + "/o.html", Missing: []string{MissingBody}, Subject: LegacyPlaceholderSubject})
	m.Add(kHealthy, Record{Path: tok + "/h.html"})
	assure.Reached(t, m.Len(), "records before the re-class")

	// Load never re-classes: a status read of the archive writes nothing.
	m = reload(t, m, path)
	if r, _ := m.Get(k1); len(r.Missing) != 1 {
		t.Fatalf("Load re-classed a legacy stub (Missing %v): only a run may", r.Missing)
	}

	if n := m.ReclassLegacyPlaceholders(legacy); n != 2 {
		t.Fatalf("re-classed %d records; want the two collapsed stubs", n)
	}
	for _, k := range []string{k1, k2} {
		r, _ := m.Get(k)
		if len(r.Missing) != 0 || len(r.Terminal) != 1 || r.Terminal[0] != LabelLegacyPlaceholder || r.Subject != LegacyPlaceholderSubject || r.Path == "" {
			t.Errorf("stub %q = %+v; want Terminal [legacy-placeholder], no Missing, the record kept whole", k, r)
		}
	}
	if r, _ := m.Get(kGraph); len(r.Terminal) != 1 || r.Terminal[0] != MissingBody {
		t.Errorf("a Graph legacy stub was re-classed: %+v", r)
	}
	if r, _ := m.Get(kOther); len(r.Missing) != 1 {
		t.Errorf("a record that merely shares the identity prefix was re-classed: %+v", r)
	}
	// The class is info: nothing is missing, so neither count carries it.
	if f, term, _ := m.Counts(); f != 1 || term != 1 {
		t.Errorf("Counts() = fillable %d, terminal %d; want 1 (the lookalike) and 1 (the Graph stub): a placeholder is info", f, term)
	}
	if GapClass(LabelLegacyPlaceholder) != ClassInfo {
		t.Errorf("GapClass(legacy-placeholder) = %q, want info", GapClass(LabelLegacyPlaceholder))
	}
	// Once: it survives a save and a load, and a second pass finds nothing.
	m = reload(t, m, path)
	if n := m.ReclassLegacyPlaceholders(legacy); n != 0 {
		t.Errorf("a second re-class touched %d records; it is a one-time upgrade", n)
	}
	if r, _ := m.Get(k1); len(r.Terminal) != 1 || r.Terminal[0] != LabelLegacyPlaceholder {
		t.Errorf("the re-class did not survive Save and Load: %+v", r)
	}
}

// covers: MA-323, R1, R5, S46
// A Graph legacy stub is adopted on the first upgraded run (matched by PhysID or
// Received) and is not swept gone (FC27). The stub an older build left for a
// message it could not parse has the placeholder subject, Terminal ["body"] and a
// content-hash identity over its received time; the adoption stamps the matches
// seen and re-classes them to the info label legacy-placeholder, and the sweep then
// spares them for good, so the change of identity never marks one gone. A record
// that merely looks like one (another subject, an unmatched id and date, a local
// folder-scoped stub, a Missing instead of a Terminal body) is no match, and an
// unmatched legacy stub is swept as any record whose message has gone.
func TestGraphLegacyStubIsAdoptedNotSwept(t *testing.T) {
	m, _, tok := gapManifest(t)
	received := time.Date(2025, 2, 1, 10, 0, 0, 0, time.UTC)
	old := gapBase.Add(-48 * time.Hour)
	stub := func(folder, date, physID string) Record {
		return Record{Path: tok + "/" + folder + "/stub.html", Folder: folder, FirstFolder: folder, Present: true, LastSeen: old,
			Subject: LegacyPlaceholderSubject, Date: date, Terminal: []string{MissingBody}, PhysID: physID}
	}
	kDate := LiveKey(tok, "sha:aaaa")
	kPhys := LiveKey(tok, "sha:bbbb")
	kOther := LiveKey(tok, "sha:cccc")
	kReal := LiveKey(tok, "sha:dddd")
	kMissing := LiveKey(tok, "sha:eeee")
	kLocal := Key(tok, "Inbox", "sha:ffff")
	m.Add(kDate, stub("Inbox", received.Format(time.RFC3339), ""))
	m.Add(kPhys, stub("Inbox", "2020-01-01T00:00:00Z", "PHYS1"))
	m.Add(kOther, stub("Inbox", "2020-01-02T00:00:00Z", "PHYS9"))
	real := stub("Inbox", received.Format(time.RFC3339), "")
	real.Subject = "Quarterly report"
	m.Add(kReal, real)
	missing := stub("Inbox", received.Format(time.RFC3339), "")
	missing.Terminal, missing.Missing = nil, []string{MissingBody}
	m.Add(kMissing, missing)
	m.Add(kLocal, stub("Inbox", received.Format(time.RFC3339), ""))

	seen := gapBase
	if n := m.AdoptLegacyGraphStubs(tok, "PHYS1", received, seen, nil); n != 2 {
		t.Fatalf("adopted %d records, want the Received match and the PhysID match", n)
	}
	for _, k := range []string{kDate, kPhys} {
		r, _ := m.Get(k)
		if len(r.Missing) != 0 || len(r.Terminal) != 1 || r.Terminal[0] != LabelLegacyPlaceholder || !r.LastSeen.Equal(seen) || !r.Present || r.Path == "" {
			t.Errorf("adopted stub %q = %+v; want it kept whole, stamped seen, Terminal [legacy-placeholder]", k, r)
		}
	}
	for _, k := range []string{kOther, kReal, kMissing, kLocal} {
		if r, _ := m.Get(k); len(r.Terminal) == 1 && r.Terminal[0] == LabelLegacyPlaceholder || r.LastSeen.Equal(seen) {
			t.Errorf("%q = %+v was adopted though it is no legacy Graph stub of this message", k, r)
		}
	}

	// The sweep of a later run (nothing stamps the adopted stubs now) spares them; every
	// other record of the walked folder that was not seen goes.
	later := gapBase.Add(24 * time.Hour)
	walked := NewWalkedFolders()
	walked.Mark("Inbox")
	gone := m.SweepGoneSparing(tok, walked, later, SweepSpare{LegacyPlaceholders: true})
	if !reflect.DeepEqual(gone, []string{kLocal, kOther, kReal, kMissing}) {
		t.Errorf("the sweep marked %q gone; want the four records that are no adopted stub", gone)
	}
	for _, k := range []string{kDate, kPhys} {
		if r, _ := m.Get(k); !r.Present {
			t.Errorf("the adopted stub %q was marked gone: it is never marked gone because its identity changed", k)
		}
	}
	// Without the spare they are records like any other.
	m2, _, tok2 := gapManifest(t)
	m2.Add(LiveKey(tok2, "sha:aaaa"), stub("Inbox", received.Format(time.RFC3339), ""))
	m2.AdoptLegacyGraphStubs(tok2, "", received, seen, nil)
	w2 := NewWalkedFolders()
	w2.Mark("Inbox")
	if g := m2.SweepGoneSparing(tok2, w2, later, SweepSpare{}); len(g) != 1 {
		t.Errorf("a sweep that spares nothing marked %q gone, want the adopted stub (the guard is the spare)", g)
	}
}

// covers: MA-323, R1, R5, S46
// A stale-ParserGen unparsed Graph stub is re-fetched exactly once (FC25). This is the
// record's half, in the manifest: the stub carries the parser generation that gave up
// on it and the digest of its bytes, a stub of an older generation (or none: an older
// binary's save drops the field) is stale until a run stamps it with the current one,
// which it does once, and Load drops both fields from any record that is no stub. The
// walk's half, one download of a stale stub in a Graph run, is
// TestStaleStubIsFetchedOnceByTheGraphWalk (internal/app).
func TestStaleParserGenStubRefetchedOnce(t *testing.T) {
	m, path, tok := gapManifest(t)
	digest := strings.Repeat("ab", 32)
	stubKey, plainKey := LiveKey(tok, "mid:stub@x"), LiveKey(tok, "mid:plain@x")
	m.Add(stubKey, Record{Path: tok + "/Inbox/stub.html", Folder: "Inbox", Terminal: []string{LabelUnparsed}, Subject: "(unreadable message)", ParserGen: 0, RawDigest: digest})
	m.Add(plainKey, Record{Path: tok + "/Inbox/plain.html", Folder: "Inbox", ParserGen: 7, RawDigest: "not a digest"})

	// A stub no binary stamped is generation 0: stale now, and stamped once.
	r, _ := m.Get(stubKey)
	if !r.StaleUnparsed(ParserGenCurrent) {
		t.Fatalf("a stub of generation 0 is not stale for generation %d", ParserGenCurrent)
	}
	if !m.StampParserGen(stubKey, ParserGenCurrent) {
		t.Fatal("the stamp did not change a stale stub")
	}
	if m.StampParserGen(stubKey, ParserGenCurrent) {
		t.Error("a second stamp changed the record again: the stub would be re-fetched on every run")
	}
	if r, _ := m.Get(stubKey); r.StaleUnparsed(ParserGenCurrent) || r.ParserGen != ParserGenCurrent {
		t.Errorf("the stamped stub = %+v; want it current", r)
	}
	if !(Record{Terminal: []string{LabelUnparsed}, ParserGen: ParserGenCurrent}).StaleUnparsed(ParserGenCurrent + 1) {
		t.Error("a stub of the current generation is not stale for the NEXT parser")
	}
	if m.StampParserGen(plainKey, ParserGenCurrent) {
		t.Error("a record that is no stub was stamped")
	}

	// Disk: a stub keeps both fields; a record that is no stub loses them at Load, and a
	// digest that is no sha-256 is dropped.
	m2 := reload(t, m, path)
	if r, _ := m2.Get(stubKey); r.ParserGen != ParserGenCurrent || r.RawDigest != digest {
		t.Errorf("the stub after a round trip = %+v; want its generation and digest kept", r)
	}
	if r, _ := m2.Get(plainKey); r.ParserGen != 0 || r.RawDigest != "" {
		t.Errorf("a healthy record after Load = %+v; want no parser stamp and no digest (P14)", r)
	}
	m2.Add(stubKey, Record{Path: tok + "/Inbox/stub.html", Terminal: []string{LabelUnparsed}, ParserGen: 1, RawDigest: "xyz"})
	if r, _ := reload(t, m2, path).Get(stubKey); r.RawDigest != "" || r.ParserGen != 1 {
		t.Errorf("a stub with a malformed digest after Load = %+v; want the digest dropped", r)
	}
}

// covers: MA-318, R1, R3, S46
// An unparsed stub is superseded, metadata only, by the capture of the same bytes
// that a later parser reads whole (§4.7.4(4), FC25): its label becomes "superseded",
// RecoveredBy names the new key, and nothing else about the record moves (no file is
// touched by this layer). It is found by token and digest, never across tokens, and
// never the record at the new key itself.
func TestUnparsedStubIsSupersededByTheSameBytes(t *testing.T) {
	m, _, tok := gapManifest(t)
	other := m.Token("mailbox:bob@contoso.com", "bob@contoso.com")
	digest := strings.Repeat("0f", 32)
	stubKey, newKey := Key(tok, "Inbox", "sha:stub"), Key(tok, "Inbox", "mid:real@x")
	m.Add(stubKey, Record{Path: tok + "/Inbox/stub.html", Terminal: []string{LabelUnparsed}, ParserGen: 1, RawDigest: digest})
	m.Add(newKey, Record{Path: tok + "/Inbox/real.html", Folder: "Inbox"})
	if !m.HasUnparsedStubs() {
		t.Fatal("the manifest holds a stub with a digest but says it holds none")
	}
	if m.SupersedeUnparsed(other, digest, newKey) {
		t.Error("a stub of another store was superseded")
	}
	if m.SupersedeUnparsed(tok, strings.Repeat("11", 32), newKey) {
		t.Error("a stub with other bytes was superseded")
	}
	if m.SupersedeUnparsed(tok, digest, stubKey) {
		t.Error("the stub was superseded by itself")
	}
	if !m.SupersedeUnparsed(tok, digest, newKey) {
		t.Fatal("the stub of the same bytes was not superseded")
	}
	r, _ := m.Get(stubKey)
	if len(r.Terminal) != 1 || r.Terminal[0] != LabelSuperseded || r.RecoveredBy != newKey || r.Path != tok+"/Inbox/stub.html" || r.RawDigest != "" || r.ParserGen != 0 {
		t.Errorf("the superseded stub = %+v; want only its label and RecoveredBy changed", r)
	}
	if m.HasUnparsedStubs() || m.SupersedeUnparsed(tok, digest, newKey) {
		t.Error("a superseded stub is still found")
	}
}

// covers: MA-327, R1, R7, S46
// Every gap kind FI-S2b records has its own words in the shared table (owner ruling
// §13.11): a why, a remedy and a report remedy that are NOT the generic fallbacks. The
// remedy of a directory link NAMES the input's opt-in, on every surface that can start
// a run (OR6), and says the input keeps its archive; a looped folder read through its
// other path is info with a "nothing to do".
func TestEveryGapKindFISlice2bRecordsHasItsOwnWords(t *testing.T) {
	reasons := []string{
		"file-unreadable", "vanished-during-read", "symlink-outside-store", "symlinked-folder-skipped",
		"not-a-message-file", "folder-cycle", "folder-unreadable", "folder-partially-read",
	}
	for _, reason := range reasons {
		if GapWhy(reason) == GapWhy("no-such-reason") {
			t.Errorf("%s has no why of its own", reason)
		}
		for _, errno := range []string{"", ErrnoPerm, ErrnoIO} {
			if r := GapRemedy(reason, SourceLocal, errno); r == remedyUnknown || r == "" {
				t.Errorf("%s (local, %q) has no remedy of its own", reason, errno)
			}
			if r := GapReportRemedy(reason, SourceLocal, errno); strings.HasPrefix(r, "run again; if it is listed again") || r == "" {
				t.Errorf("%s (local, %q) has no report remedy of its own", reason, errno)
			}
		}
	}
	r := GapRemedy("symlinked-folder-skipped", SourceLocal, "")
	for _, want := range []string{"-follow-links", "wizard", "follow_links", "keeps its archive"} {
		if !strings.Contains(r, want) {
			t.Errorf("the directory-link remedy %q does not name %q", r, want)
		}
	}
	if r := GapRemedy("symlink-outside-store", SourceLocal, ""); strings.Contains(r, "-follow-links") || !strings.Contains(r, "copy the file into the store") {
		t.Errorf("a file link out of the store is never followed, opt-in or not; its remedy %q must not offer the opt-in", r)
	}
	if r := GapRemedy("folder-cycle", SourceLocal, ""); !strings.Contains(r, "nothing to do") || strings.Contains(r, "scanpst") {
		t.Errorf("a looped local folder's remedy %q is the PST repair or no word at all", r)
	}
	if r := GapRemedy("folder-cycle", SourcePST, ""); !strings.Contains(r, "scanpst") {
		t.Errorf("a PST's looped hierarchy keeps the repair remedy, got %q", r)
	}
}

// covers: MA-327, R1, R7, S46
// Every gap kind FI-S2a records has its own words in the shared table (owner ruling
// §13.11): a why, a remedy and a report remedy that are NOT the generic fallbacks,
// for each source kind it can come from, so no summary line, status line or report
// row appears with a bare code. A PST gap's remedy is the in-place repair, except on
// the -outlook scratch .pst, which is rebuilt every run and says to open the item in
// Outlook.
func TestEveryGapKindFISlice2aRecordsHasItsOwnWords(t *testing.T) {
	reasons := []string{
		"pst-message-unreadable", "pst-messages-unreadable", "pst-row-unreadable", "pst-row-invalid",
		"folder-unreadable", "folder-partially-read", "folder-cycle", "folder-too-deep",
		"folder-unwritable", "export-failed", "export-panic", "refresh-failed",
		"outlook-export-skipped", "outlook-account-skipped",
	}
	for _, reason := range reasons {
		if GapWhy(reason) == GapWhy("no-such-reason") {
			t.Errorf("%s has no why of its own", reason)
		}
		for _, kind := range []string{SourcePST, SourceOutlookScratch, SourceLocal} {
			for _, errno := range []string{"", ErrnoPerm, ErrnoIO} {
				if r := GapRemedy(reason, kind, errno); r == remedyUnknown || r == "" {
					t.Errorf("%s (%s, %q) has no remedy of its own", reason, kind, errno)
				}
				if r := GapReportRemedy(reason, kind, errno); r == "run again; if it is listed again, keep the original, because this version of mailarchive does not know the cause" || r == "" {
					t.Errorf("%s (%s, %q) has no report remedy of its own", reason, kind, errno)
				}
			}
		}
	}
	if r := GapRemedy("pst-message-unreadable", SourcePST, ""); !strings.Contains(r, "scanpst.exe") || !strings.Contains(r, "same path") {
		t.Errorf("a .pst gap's remedy %q must be the in-place repair", r)
	}
	if r := GapRemedy("pst-messages-unreadable", SourceOutlookScratch, ""); !strings.Contains(r, "open it in Outlook") || strings.Contains(r, "scanpst") {
		t.Errorf("an -outlook scratch gap's remedy %q must say to open the item in Outlook, never to repair the rebuilt file", r)
	}
}

// covers: MA-323, R1, R5, S46
// What the ledger looks like on disk: keys are token \x00 scope \x00 locator,
// the fields are those of §4.2, an empty ledger writes no key, and the manifest
// version stays 6 (J2: no bump).
func TestCaptureGapJSONShape(t *testing.T) {
	m, _, tok := gapManifest(t)
	g := itemGap(tok, "graph:AAA")
	g.Subject = "s"
	m.ObserveGap(g, gapBase)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	var gaps map[string]map[string]any
	if err := json.Unmarshal(doc["capture_gaps"], &gaps); err != nil || len(gaps) != 1 {
		t.Fatalf("the manifest has no capture_gaps object (%v): %s", err, b)
	}
	for k, e := range gaps {
		if k != GapKey(tok, GapScopeItem, "graph:AAA") {
			t.Errorf("ledger key %q", k)
		}
		for _, f := range []string{"token", "folder", "scope", "loc", "reason", "class", "subject", "first", "last", "runs"} {
			if _, ok := e[f]; !ok {
				t.Errorf("a ledger entry has no %q field: %v", f, e)
			}
		}
	}
	// No manifest version bump, and an empty ledger writes no key (J2).
	var empty map[string]json.RawMessage
	em, _, _ := gapManifest(t)
	eb, _ := json.Marshal(em)
	json.Unmarshal(eb, &empty)
	if _, has := empty["capture_gaps"]; has {
		t.Error("an empty ledger wrote a capture_gaps key")
	}
	if string(doc["version"]) != "6" {
		t.Errorf("manifest version %s, want 6 (no bump, J2)", doc["version"])
	}
}
