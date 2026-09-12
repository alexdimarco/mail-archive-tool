package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// writeFullManifest marshals a version + a full Record map (fingerprints and
// times included, unlike store_scope_test's legacyRec) to path, exactly the
// on-disk shape Load reads back.
func writeFullManifest(t *testing.T, path string, version int, entries map[string]Record) {
	t.Helper()
	doc := struct {
		Version int               `json:"version"`
		Entries map[string]Record `json:"entries"`
	}{Version: version, Entries: entries}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-198, R1, R2, S39, S30
// The fingerprint-safe v3→v4 collapse (§3.1) unifies records that share a
// (token, identity) AND an equal, non-empty stored Fingerprint into ONE
// mailbox-wide LiveKey record — the survivor keeps the first-captured file and
// FirstFolder/FirstSeen (earliest ExportedAt) while its current Folder/LastSeen
// follow the most-recently-observed sibling — so a message moved between folders
// is one file with a recorded folder-over-time, not a second copy. Two genuinely
// different messages that reused one Message-ID have DIFFERENT fingerprints and
// stay separate #fp-qualified siblings: neither is silently dropped (R1). Each
// unified-away loser is returned so the caller records it and prunes its index
// row (the file stays on disk, R13), and the identity index maps the message id
// to every surviving key.
func TestV3ToV4FingerprintSafeCollapse(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1, t2, t3, t4 := t0, t0.Add(24*time.Hour), t0.Add(48*time.Hour), t0.Add(72*time.Hour)

	kA1 := Key("mbox", "Inbox", "mid:<a@x>") // first-captured copy of message A
	kA2 := Key("mbox", "Trash", "mid:<a@x>") // A moved to Trash — SAME fingerprint
	kB1 := Key("mbox", "Inbox", "mid:<b@x>") // message B
	kB2 := Key("mbox", "Sent", "mid:<b@x>")  // a DIFFERENT message reusing B's id

	path := filepath.Join(t.TempDir(), "v3.json")
	writeFullManifest(t, path, 3, map[string]Record{
		kA1: {Path: "mbox/Inbox/a.html", Folder: "Inbox", ExportedAt: t1, Fingerprint: "aaaaaaaaaaaaaaaa",
			Fixity: &Fixity{HTML: &FileDigest{SHA256: "sha-a", Size: 11}}},
		kA2: {Path: "mbox/Trash/a.html", Folder: "Trash", ExportedAt: t2, Fingerprint: "aaaaaaaaaaaaaaaa"},
		kB1: {Path: "mbox/Inbox/b.html", Folder: "Inbox", ExportedAt: t3, Fingerprint: "bbbbbbbbbbbbbbbb"},
		kB2: {Path: "mbox/Sent/b2.html", Folder: "Sent", ExportedAt: t4, Fingerprint: "cccccccccccccccc"},
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.LoadedVersion != 3 {
		t.Fatalf("LoadedVersion = %d, want 3", m.LoadedVersion)
	}

	losses, _ := m.CollapseByIdentity()

	if m.Len() != 3 {
		t.Errorf("entries after collapse = %d, want 3 (A collapsed; B kept as two distinct siblings)", m.Len())
	}

	// Message A: one survivor at the base live key, first file wins.
	baseA := LiveKey("mbox", "mid:<a@x>")
	recA, ok := m.Get(baseA)
	if !ok {
		t.Fatalf("collapsed message A not found under its live key %q", baseA)
	}
	if recA.Path != "mbox/Inbox/a.html" {
		t.Errorf("survivor kept the wrong file: Path=%q, want the first-captured mbox/Inbox/a.html", recA.Path)
	}
	if recA.FirstFolder != "Inbox" || !recA.FirstSeen.Equal(t1) {
		t.Errorf("FirstFolder/FirstSeen wrong: %q/%v, want Inbox/%v (earliest)", recA.FirstFolder, recA.FirstSeen, t1)
	}
	if recA.Folder != "Trash" || !recA.LastSeen.Equal(t2) {
		t.Errorf("current Folder/LastSeen wrong: %q/%v, want Trash/%v (latest observed)", recA.Folder, recA.LastSeen, t2)
	}
	if !recA.Present {
		t.Error("collapsed survivor is not Present")
	}
	if recA.Fingerprint != "aaaaaaaaaaaaaaaa" {
		t.Errorf("survivor fingerprint = %q, want the shared aaaa", recA.Fingerprint)
	}
	if recA.Fixity == nil || recA.Fixity.HTML == nil || recA.Fixity.HTML.SHA256 != "sha-a" {
		t.Errorf("collapse dropped the survivor's recorded fixity: %+v", recA.Fixity)
	}

	// Message B: two distinct messages reused one id — both survive (R1, no drop).
	baseB := LiveKey("mbox", "mid:<b@x>")
	qualB := Qualify(baseB, "cccccccccccccccc")
	recB1, ok1 := m.Get(baseB)
	recB2, ok2 := m.Get(qualB)
	if !ok1 || !ok2 {
		t.Fatalf("a distinct message reusing an id was dropped: base=%v qualified=%v", ok1, ok2)
	}
	if recB1.Path != "mbox/Inbox/b.html" || recB1.Fingerprint != "bbbbbbbbbbbbbbbb" {
		t.Errorf("base B sibling wrong: %+v", recB1)
	}
	if recB2.Path != "mbox/Sent/b2.html" || recB2.Fingerprint != "cccccccccccccccc" {
		t.Errorf("qualified B sibling wrong: %+v", recB2)
	}

	// Losers are reported (no location lost), not silently discarded.
	assure.Reached(t, losses, "collapse losses")
	if len(losses) != 1 {
		t.Fatalf("collapse reported %d losses, want 1 (A's moved copy)", len(losses))
	}
	if m.Collapsed != 1 {
		t.Errorf("m.Collapsed = %d, want 1", m.Collapsed)
	}
	if losses[0].LoserKey != kA2 || losses[0].LoserPath != "mbox/Trash/a.html" ||
		losses[0].Folder != "Trash" || losses[0].SurvivorKey != baseA {
		t.Errorf("loss record wrong: %+v (want the Trash copy pointing at survivor %q)", losses[0], baseA)
	}

	// The identity index maps each id to every surviving key with its fingerprint.
	if refs := m.KeysForIdentity("mid:<a@x>"); len(refs) != 1 || refs[0].Key != baseA {
		t.Errorf("identity index for A = %+v, want one entry keyed %q", refs, baseA)
	}
	refsB := assure.Reached(t, m.KeysForIdentity("mid:<b@x>"), "identity index for B")
	if len(refsB) != 2 {
		t.Errorf("identity index for B has %d keys, want 2 (#fp siblings coexist)", len(refsB))
	}
}

// covers: MA-199, R2, R5, S39
// Two v4 primitives. (1) Load fills the timeline defaults on a pre-v4 record
// (GB-05): Present=true, FirstFolder=Folder, FirstSeen=LastSeen=ExportedAt, so a
// migrated archive has a coherent starting timeline without a download. (2) The
// in-place field merge (§3.2) rewrites ONLY Folder/LastSeen/Present — the
// no-download move merge — and preserves every capture-time field: Fixity,
// Fingerprint, ExportedAt, the set-once FirstFolder/FirstSeen, Path and the
// completeness lists. A merge on an absent key is a no-op returning false.
func TestV4LoadDefaultsAndFieldMerge(t *testing.T) {
	t1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	t3 := t1.Add(48 * time.Hour)
	k := Key("mbox", "Inbox", "mid:<d@x>")

	path := filepath.Join(t.TempDir(), "v3.json")
	writeFullManifest(t, path, 3, map[string]Record{
		k: {Path: "mbox/Inbox/d.html", Folder: "Inbox", ExportedAt: t1, Fingerprint: "dddddddddddddddd",
			Missing: []string{"body"},
			Fixity:  &Fixity{HTML: &FileDigest{SHA256: "sha-d", Size: 42}}},
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := m.Get(k)
	if !ok {
		t.Fatalf("record missing after load of a v3 manifest under key %q", k)
	}
	if !rec.Present || rec.FirstFolder != "Inbox" || !rec.FirstSeen.Equal(t1) || !rec.LastSeen.Equal(t1) {
		t.Errorf("v4 load defaults not filled: Present=%v FirstFolder=%q FirstSeen=%v LastSeen=%v (want true/Inbox/%v/%v)",
			rec.Present, rec.FirstFolder, rec.FirstSeen, rec.LastSeen, t1, t1)
	}

	if !m.MergeFields(k, "Archive", t3, true) {
		t.Fatal("MergeFields returned false for a present key")
	}
	merged, _ := m.Get(k)
	if merged.Folder != "Archive" || !merged.LastSeen.Equal(t3) || !merged.Present {
		t.Errorf("merge did not update the timeline fields: Folder=%q LastSeen=%v Present=%v", merged.Folder, merged.LastSeen, merged.Present)
	}
	// Capture-time fields are preserved (no Record rebuild).
	if merged.Fixity == nil || merged.Fixity.HTML == nil || merged.Fixity.HTML.SHA256 != "sha-d" || merged.Fixity.HTML.Size != 42 {
		t.Errorf("merge did not preserve Fixity: %+v", merged.Fixity)
	}
	if merged.Fingerprint != "dddddddddddddddd" || !merged.ExportedAt.Equal(t1) {
		t.Errorf("merge altered Fingerprint/ExportedAt: fp=%q exportedAt=%v", merged.Fingerprint, merged.ExportedAt)
	}
	if merged.FirstFolder != "Inbox" || !merged.FirstSeen.Equal(t1) || merged.Path != "mbox/Inbox/d.html" {
		t.Errorf("merge altered a set-once field: FirstFolder=%q FirstSeen=%v Path=%q", merged.FirstFolder, merged.FirstSeen, merged.Path)
	}
	if len(merged.Missing) != 1 || merged.Missing[0] != "body" {
		t.Errorf("merge altered completeness: Missing=%v", merged.Missing)
	}

	if m.MergeFields("mbox\x00Inbox\x00mid:<absent@x>", "X", t3, true) {
		t.Error("MergeFields on an absent key returned true (should be a no-op)")
	}
}

// covers: MA-200, R5, R2, S39
// The append-only history log round-trips its run header / per-message
// folder-assertion & gone / folder-rename / run-completed footer. A torn tail is
// defended on both sides: the reader SKIPS an unparseable last line, and
// open-for-append TRUNCATES it so the next event is a clean whole line (GB-4).
// Fold replays to a date: the latest folder-assertion / gone / present-again
// transition with a time ≤ D wins, and a message with no event by D is absent.
func TestHistoryLogRoundTripTornTailAndFold(t *testing.T) {
	dir := t.TempDir()
	t1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	hp := filepath.Join(dir, HistoryName)
	w, err := OpenHistory(hp)
	if err != nil {
		t.Fatal(err)
	}
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(w.WriteRunHeader(1, t1, []string{"user@x"}))
	must(w.WriteFolder("k1", "Inbox"))
	must(w.WriteGone("k2"))
	must(w.WriteRename("Old", "New"))
	must(w.WriteRunFooter(1, t1))
	must(w.Sync())
	must(w.Close())

	events := assure.Reached(t, mustRead(t, hp), "history events")
	var sawHeader, sawFolder, sawGone, sawRename, sawFooter bool
	for _, ev := range events {
		switch {
		case ev.Run == 1 && ev.At != "":
			sawHeader = len(ev.Mailboxes) == 1 && ev.Mailboxes[0] == "user@x"
		case ev.K == "k1" && ev.Folder == "Inbox" && !ev.Gone:
			sawFolder = true
		case ev.K == "k2" && ev.Gone:
			sawGone = true
		case ev.From == "Old" && ev.To == "New":
			sawRename = true
		case ev.Run == 1 && ev.Completed != "":
			sawFooter = true
		}
	}
	if !sawHeader || !sawFolder || !sawGone || !sawRename || !sawFooter {
		t.Errorf("round-trip lost a line kind: header=%v folder=%v gone=%v rename=%v footer=%v",
			sawHeader, sawFolder, sawGone, sawRename, sawFooter)
	}

	// A crash mid-line leaves a torn trailing partial line (no newline).
	f, err := os.OpenFile(hp, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"k":"torn","fol`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Read side skips it — the good events read back unchanged.
	ev2 := mustRead(t, hp)
	for _, e := range ev2 {
		if e.K == "torn" {
			t.Error("the read did not skip the torn last line")
		}
	}
	if len(ev2) != len(events) {
		t.Errorf("torn line changed the read count: %d vs %d", len(ev2), len(events))
	}

	// Write side truncates it on the next open-for-append.
	w2, err := OpenHistory(hp)
	if err != nil {
		t.Fatal(err)
	}
	must(w2.WriteFolder("k3", "Done"))
	must(w2.Close())

	raw, err := os.ReadFile(hp)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("torn")) {
		t.Error("open-for-append did not truncate the torn partial line")
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Error("the log does not end in a newline after the post-truncation append")
	}
	ev3 := mustRead(t, hp)
	var sawK3 bool
	for _, e := range ev3 {
		if e.K == "k3" && e.Folder == "Done" {
			sawK3 = true
		}
		if e.K == "torn" {
			t.Error("the torn line survived the truncation")
		}
	}
	if !sawK3 {
		t.Error("the post-truncation append was not read back")
	}
	if len(ev3) != len(events)+1 {
		t.Errorf("expected the original events plus one, got %d (had %d)", len(ev3), len(events))
	}

	// Fold: assert / gone / present-again across three run dates.
	fp := filepath.Join(dir, "fold.jsonl")
	d1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	d2, d3 := d1.Add(24*time.Hour), d1.Add(48*time.Hour)
	fw, err := OpenHistory(fp)
	if err != nil {
		t.Fatal(err)
	}
	must(fw.WriteRunHeader(1, d1, nil))
	must(fw.WriteFolder("K", "Inbox"))
	must(fw.WriteRunFooter(1, d1))
	must(fw.WriteRunHeader(2, d2, nil))
	must(fw.WriteGone("K"))
	must(fw.WriteRunFooter(2, d2))
	must(fw.WriteRunHeader(3, d3, nil))
	must(fw.WriteFolder("K", "Archive"))
	must(fw.WriteRunFooter(3, d3))
	must(fw.Close())

	if st, ok := mustFold(t, fp, d1)["K"]; !ok || !st.Present || st.Folder != "Inbox" {
		t.Errorf("fold@D1 = %+v ok=%v, want Inbox/present", st, ok)
	}
	if st, ok := mustFold(t, fp, d2)["K"]; !ok || st.Present || st.Folder != "Inbox" {
		t.Errorf("fold@D2 = %+v ok=%v, want gone (present=false), last folder Inbox", st, ok)
	}
	if st, ok := mustFold(t, fp, d3)["K"]; !ok || !st.Present || st.Folder != "Archive" {
		t.Errorf("fold@D3 = %+v ok=%v, want Archive/present (present-again)", st, ok)
	}
	if st, ok := mustFold(t, fp, d1.Add(-time.Hour))["K"]; ok {
		t.Errorf("fold before D1 shows K = %+v, want absent", st)
	}
}

func mustRead(t *testing.T, path string) []HistoryEvent {
	t.Helper()
	ev, err := ReadHistory(path)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	return ev
}

func mustFold(t *testing.T, path string, upTo time.Time) map[string]FoldState {
	t.Helper()
	s, err := FoldHistory(path, upTo)
	if err != nil {
		t.Fatalf("FoldHistory: %v", err)
	}
	return s
}

// covers: MA-201, R5, R12, S30, S39
// v4 key-format discrimination is VERSION-gated, not NUL-count-gated: a
// folder-less one-NUL live key in a v4 manifest is left untouched (Rekeyed==0),
// whereas the IDENTICAL bytes in a v2 manifest ARE re-scoped — proving the gate
// is version-sensitive, not a no-op that would silently break either format
// (GB-01/F1). And Load refuses a manifest whose stored version exceeds the
// current constant (version 6), naming the file and the upgrade remedy and
// leaving the bytes untouched (fail-closed) — an old binary likewise refuses a
// newer archive. The v4 positive twin (loads clean under v5) fronts the refusal.
func TestVersionGateAndRefusesFuture(t *testing.T) {
	lk := LiveKey("mbox", "mid:<e@x>") // one NUL: a legitimate v4 live key
	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// v4: the folder-less key is NOT re-scoped (version-gated positive twin).
	v4 := filepath.Join(t.TempDir(), "v4.json")
	writeFullManifest(t, v4, 4, map[string]Record{
		lk: {Path: "mbox/e.html", Folder: "Inbox", ExportedAt: t1, Fingerprint: "eeeeeeeeeeeeeeee",
			FirstFolder: "Inbox", FirstSeen: t1, LastSeen: t1, Present: true},
	})
	m4, err := Load(v4)
	if err != nil {
		t.Fatalf("a current-version manifest must load: %v", err)
	}
	if m4.Rekeyed != 0 {
		t.Errorf("a v4 folder-less live key was re-scoped (Rekeyed=%d); the discriminator must be version-gated", m4.Rekeyed)
	}
	if _, ok := m4.Get(lk); !ok {
		t.Errorf("the live key %q was not preserved on load", lk)
	}

	// v2: the SAME bytes ARE re-scoped (the gate is version-sensitive).
	v2 := filepath.Join(t.TempDir(), "v2.json")
	writeFullManifest(t, v2, 2, map[string]Record{
		lk: {Path: "store/e.html", Folder: "mbox", ExportedAt: t1},
	})
	m2, err := Load(v2)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Rekeyed != 1 {
		t.Errorf("the identical one-NUL key was NOT re-scoped under v2 (Rekeyed=%d, want 1); the gate is not version-sensitive", m2.Rekeyed)
	}

	// version 6 (above the current constant, 5) is refused, byte-unchanged.
	v6 := filepath.Join(t.TempDir(), "v6.json")
	body := []byte(`{"version":6,"entries":{"whatever":{"path":"s/Inbox/a.html"}}}`)
	if err := os.WriteFile(v6, body, 0o644); err != nil {
		t.Fatal(err)
	}
	_, lerr := Load(v6)
	rc, msg := 0, ""
	if lerr != nil {
		rc, msg = 2, lerr.Error()
	}
	assure.Refused(t, rc, msg,
		assure.Names(v6, "newer mailarchive", "upgrade"),
		assure.NoSideEffect(func() bool {
			after, readErr := os.ReadFile(v6)
			return readErr == nil && bytes.Equal(after, body)
		}))
}
