package app

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// rbMsg is one message to export into a rebuild-test archive.
type rbMsg struct {
	folder []string
	m      *model.Message
}

// buildRebuildArchive writes msgs into out with a live index, manifest and
// folder pages, keeping each message's original .eml when it carries raw bytes
// (KeepRaw). It returns the exported html paths (relative to out), keyed by
// subject, exactly as a real `-raw` export would leave them on disk.
func buildRebuildArchive(t *testing.T, out string, msgs []rbMsg) map[string]string {
	t.Helper()
	idx, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	rels := map[string]string{}
	exp := &export.Exporter{
		OutDir:   out,
		Manifest: manifest,
		Mode:     export.Full,
		KeepRaw:  true,
		Log:      log.New(io.Discard, "", 0),
		OnExported: func(store string, fp []string, m *model.Message, rel, key string) {
			rels[m.Subject] = rel
			if addErr := idx.Add(store, fp, m, rel, key); addErr != nil {
				t.Fatalf("index add: %v", addErr)
			}
		},
	}
	for _, mm := range msgs {
		if _, err := exp.Export("Store", mm.folder, mm.m); err != nil {
			t.Fatalf("export %s: %v", mm.m.Subject, err)
		}
	}
	if err := idx.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := pages.Generate(out, idx, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(); err != nil {
		t.Fatal(err)
	}
	return rels
}

// firstResult returns the first search hit for q (fatal if none).
func firstResult(t *testing.T, out, q string) index.Result {
	t.Helper()
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	res, _, err := ix.Search(index.Query{Text: q})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatalf("no search hit for %q", q)
	}
	return res[0]
}

// deleteSearchDB removes the live index (and any WAL/SHM), modelling the P4a
// headline: search.db lost while the archive's files survive.
func deleteSearchDB(t *testing.T, out string) {
	t.Helper()
	for _, s := range []string{"search.db", "search.db-wal", "search.db-shm"} {
		if err := os.Remove(filepath.Join(out, s)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

const alphaRaw = "From: Alice <alice@example.com>\r\n" +
	"To: bob@example.com\r\n" +
	"Subject: alpha-subject\r\n" +
	"Message-ID: <alpha@x>\r\n" +
	"Date: Sat, 01 Mar 2025 09:00:00 +0000\r\n\r\n" +
	"alpha body zulu\r\n"

const betaRaw = "From: Bob <bob@example.com>\r\n" +
	"To: carol@example.com\r\n" +
	"Subject: beta-subject\r\n" +
	"Message-ID: <beta@x>\r\n" +
	"Date: Sat, 01 Mar 2025 10:00:00 +0000\r\n\r\n" +
	"beta body yankee\r\n"

// covers: MA-171, R13, R8, S33
// The headline P4a recovery: after search.db is deleted, `reindex -rebuild`
// reconstructs the index from the archive alone. A record with a preserved .eml
// is parsed from those bytes (from-eml); a record with none is re-derived from
// its archived page (from-html) with the right subject/sender/recipients/date —
// including the separate Sent/Received case — and the same queries return the
// same hits as before.
func TestRebuildReconstructsFromArchive(t *testing.T) {
	out := tmpDir(t)
	pst := time.FixedZone("PST", -8*3600)
	gamma := &model.Message{
		Subject:           "gamma-subject",
		SenderName:        "Carol",
		SenderEmail:       "carol@example.com",
		To:                "dave@example.com",
		Cc:                "erin@example.com",
		Sent:              time.Date(2025, 3, 1, 14, 0, 0, 0, pst),
		Received:          time.Date(2025, 3, 1, 14, 30, 0, 0, pst),
		InternetMessageID: "<gamma@x>",
		HTMLBody:          "<p>gamma body xray</p>",
	}
	buildRebuildArchive(t, out, []rbMsg{
		{[]string{"Inbox"}, source.ParseRFC822([]byte(alphaRaw))}, // has .eml → from-eml
		{[]string{"Inbox"}, source.ParseRFC822([]byte(betaRaw))},  // has .eml → from-eml
		{[]string{"Inbox"}, gamma},                                // no .eml → from-html
	})

	// The .eml siblings prove the from-eml path has real bytes to read.
	if n := countEML(t, out); n != 2 {
		t.Fatalf("expected 2 preserved .eml files (alpha, beta), found %d", n)
	}

	queries := []string{"alpha", "beta", "gamma", "zulu", "yankee", "xray", "dave", "carol"}
	pre := map[string]int{}
	for _, q := range queries {
		pre[q] = indexTotal(t, out, q)
	}
	if pre["gamma"] != 1 || pre["zulu"] != 1 || pre["dave"] != 1 {
		t.Fatalf("pre-rebuild sanity: gamma=%d zulu=%d dave=%d, want 1 each", pre["gamma"], pre["zulu"], pre["dave"])
	}

	deleteSearchDB(t, out)
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.FromEML != 2 || rep.FromHTML != 1 || rep.Rebuilt != 3 || rep.Pruned != 0 {
		t.Fatalf("rebuild report = %+v, want FromEML=2 FromHTML=1 Rebuilt=3 Pruned=0", rep)
	}

	// Same hits as before, for from-eml body/subject terms and for the from-html
	// record's subject/sender/recipient terms (R8 parity).
	for _, q := range queries {
		if got := indexTotal(t, out, q); got != pre[q] {
			t.Errorf("post-rebuild %q = %d, want %d (same hits as before)", q, got, pre[q])
		}
	}

	// The from-html record recovered the right fields.
	r := firstResult(t, out, "gamma")
	if r.Subject != "gamma-subject" {
		t.Errorf("from-html subject = %q, want gamma-subject", r.Subject)
	}
	if r.SenderName != "Carol" || r.SenderEmail != "carol@example.com" {
		t.Errorf("from-html sender = %q/%q, want Carol/carol@example.com", r.SenderName, r.SenderEmail)
	}
	if r.Folder != "Inbox" {
		t.Errorf("from-html folder = %q, want Inbox", r.Folder)
	}
	if !r.Date.Equal(gamma.Received) {
		t.Errorf("from-html date = %v, want %v (Received, via the UTC parenthetical)", r.Date, gamma.Received.UTC())
	}
}

// countEML counts the preserved .eml files under out.
func countEML(t *testing.T, out string) int {
	t.Helper()
	n := 0
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".eml") {
			n++
		}
		return nil
	})
	return n
}

// covers: MA-172, R5, R8, S33
// Rebuild builds a fresh temp index renamed over search.db, so it carries no
// orphaned FTS content: a term present only in a record whose file was removed
// returns no hit after a rebuild done OVER the existing index, and that record
// is pruned from both the index and the manifest.
func TestRebuildFreshIndexNoOrphanPrunesMissing(t *testing.T) {
	out := tmpDir(t)
	rels := buildArchive(t, out) // keep-alpha, prune-beta, keep-gamma (no raw)

	if got := indexTotal(t, out, "beta"); got != 1 {
		t.Fatalf("pre-rebuild 'beta' = %d, want 1", got)
	}

	// The victim's file is removed; its manifest entry still points at it.
	victim := rels["prune-beta"]
	if err := os.Remove(filepath.Join(out, filepath.FromSlash(victim))); err != nil {
		t.Fatal(err)
	}

	// Rebuild OVER the still-present search.db (not deleted first): a correct
	// fresh-temp rebuild must not carry the victim's stale row forward.
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.Rebuilt != 2 || rep.Pruned != 1 {
		t.Fatalf("rebuild report = %+v, want Rebuilt=2 Pruned=1", rep)
	}

	if got := indexTotal(t, out, "beta"); got != 0 {
		t.Errorf("post-rebuild 'beta' = %d, want 0 (orphaned FTS content survived)", got)
	}
	if got := indexTotal(t, out, "keep"); got != 2 {
		t.Errorf("post-rebuild 'keep' = %d, want 2 (survivors dropped)", got)
	}

	manifest, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	betaKey := state.Key("Store", "Inbox", reindexMsg("prune-beta", "<beta@x>").Identity())
	if manifest.Has(betaKey) {
		t.Error("manifest still records the removed message after rebuild")
	}
	if manifest.Len() != 2 {
		t.Errorf("manifest len after rebuild = %d, want 2", manifest.Len())
	}
}

// covers: MA-173, R4, S33
// Rebuild validates every recorded path with the exact verify gate before
// opening anything: a `..`, absolute, symlinked-component or oversized path is
// pruned-and-reported and NEVER read — its out-of-archive or oversized content
// never enters the rebuilt index — while the safe records rebuild normally.
func TestRebuildSkipsUnsafePathsWithoutReading(t *testing.T) {
	base := tmpDir(t)
	out := filepath.Join(base, "archive")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	buildArchive(t, out) // 3 safe records (keep-alpha, prune-beta, keep-gamma)

	// Shrink the read cap so an "oversized" fixture need not be gigabytes; the
	// safe records' rendered pages are a few KB and stay well under it.
	old := rebuildMaxFileBytes
	rebuildMaxFileBytes = 50000
	defer func() { rebuildMaxFileBytes = old }()

	// Secret files whose content must never reach the index.
	writeSecret := func(path, term string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("<html><body>"+term+"</body></html>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSecret(filepath.Join(base, "escape-secret.html"), "escapesecretterm")
	absSecret := filepath.Join(base, "abs-secret.html")
	writeSecret(absSecret, "abssecretterm")

	manifest, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	bad := 0
	addBad := func(id, path string) {
		manifest.Add(state.Key("Store", "Inbox", id), state.Record{Path: path, Folder: "Inbox"})
		bad++
	}
	addBad("<escape@x>", "../escape-secret.html")
	addBad("<abs@x>", filepath.ToSlash(absSecret))

	// A symlinked directory component: "link" → an external dir holding a real
	// html. validRelPath passes; the component-wise Lstat refuses to follow it.
	external := tmpDir(t)
	if err := os.MkdirAll(filepath.Join(external, "Inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSecret(filepath.Join(external, "Inbox", "foo.html"), "symsecretterm")
	symlinkOK := os.Symlink(external, filepath.Join(out, "link")) == nil
	if symlinkOK {
		addBad("<sym@x>", "link/Inbox/foo.html")
	}

	// An oversized in-archive file: valid path, regular file, but over the cap.
	bigDir := filepath.Join(out, "Store", "Inbox")
	if err := os.MkdirAll(bigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bigBody := "bigsecretterm " + string(make([]byte, 100000))
	if err := os.WriteFile(filepath.Join(bigDir, "big.html"), []byte("<html><body>"+bigBody+"</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	addBad("<big@x>", "Store/Inbox/big.html")

	if err := manifest.Save(); err != nil {
		t.Fatal(err)
	}

	deleteSearchDB(t, out)
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	// The safe records were actually rebuilt (the test exercised something).
	assure.Reached(t, indexTotal(t, out, "keep"), "safe records rebuilt")
	if got := indexTotal(t, out, "keep"); got != 2 {
		t.Errorf("safe records after rebuild: 'keep' = %d, want 2", got)
	}
	if rep.Rebuilt != 3 {
		t.Errorf("Rebuilt = %d, want 3 (only the safe records)", rep.Rebuilt)
	}
	if rep.Pruned != bad {
		t.Errorf("Pruned = %d, want %d (every unsafe/oversized record)", rep.Pruned, bad)
	}

	// NoSideEffect: none of the skipped files was read into the index.
	for _, term := range []string{"escapesecretterm", "abssecretterm", "bigsecretterm"} {
		if got := indexTotal(t, out, term); got != 0 {
			t.Errorf("skipped-path content %q reached the index (%d hits) — it was read", term, got)
		}
	}
	if symlinkOK {
		if got := indexTotal(t, out, "symsecretterm"); got != 0 {
			t.Errorf("symlinked-component content reached the index (%d hits) — the symlink was followed", got)
		}
		// The symlink target file is untouched (never opened for writing).
		if _, err := os.Stat(filepath.Join(external, "Inbox", "foo.html")); err != nil {
			t.Errorf("symlink target disturbed: %v", err)
		}
	}
}

// covers: MA-174, R13, S33
// A per-record fault is isolated, never fatal: a corrupt sibling attachment zip
// on one record leaves that record indexed (with no recovered attachment names)
// and every other record still rebuilt.
func TestRebuildIsolatesCorruptZip(t *testing.T) {
	out := tmpDir(t)
	good := &model.Message{
		Subject:    "zipgood",
		SenderName: "S", SenderEmail: "s@example.com",
		Received:          time.Date(2025, 3, 2, 8, 0, 0, 0, time.UTC),
		InternetMessageID: "<zg@x>",
		HTMLBody:          "<p>zipgoodterm</p>",
		Attachments:       []model.Attachment{{Filename: "report.pdf", WriteTo: bytesWriter("PDF")}},
	}
	bad := &model.Message{
		Subject:    "zipbad",
		SenderName: "S", SenderEmail: "s@example.com",
		Received:          time.Date(2025, 3, 2, 9, 0, 0, 0, time.UTC),
		InternetMessageID: "<zb@x>",
		HTMLBody:          "<p>zipbadterm</p>",
		Attachments:       []model.Attachment{{Filename: "data.bin", WriteTo: bytesWriter("BIN")}},
	}
	rels := buildRebuildArchive(t, out, []rbMsg{
		{[]string{"Inbox"}, good},
		{[]string{"Inbox"}, bad},
	})

	// Corrupt the bad record's zip in place.
	badStem := strings.TrimSuffix(rels["zipbad"], ".html")
	zipPath := filepath.Join(out, filepath.FromSlash(badStem)+"-attachments.zip")
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("expected an attachments zip for the bad record: %v", err)
	}
	if err := os.WriteFile(zipPath, []byte("this is not a zip file"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleteSearchDB(t, out)
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild aborted on a corrupt zip: %v", err)
	}
	if rep.Rebuilt != 2 || rep.FromHTML != 2 || rep.Pruned != 0 {
		t.Fatalf("rebuild report = %+v, want Rebuilt=2 FromHTML=2 Pruned=0", rep)
	}

	// Both records are indexed by body term (the corrupt-zip one was not dropped).
	if got := indexTotal(t, out, "zipbadterm"); got != 1 {
		t.Errorf("corrupt-zip record 'zipbadterm' = %d, want 1 (record dropped)", got)
	}
	if got := indexTotal(t, out, "zipgoodterm"); got != 1 {
		t.Errorf("good record 'zipgoodterm' = %d, want 1", got)
	}
	// The good zip's attachment name is recovered; the corrupt one's is not.
	if got := indexTotal(t, out, "report"); got != 1 {
		t.Errorf("good attachment name 'report' = %d, want 1", got)
	}
	if got := indexTotal(t, out, "data"); got != 0 {
		t.Errorf("attachment name from the corrupt zip 'data' = %d, want 0 (should be unrecoverable)", got)
	}
}

func bytesWriter(s string) func(w io.Writer) (int64, error) {
	return func(w io.Writer) (int64, error) {
		n, err := w.Write([]byte(s))
		return int64(n), err
	}
}

// covers: MA-175, R12, S33
// The recovery is discoverable and scoped: rebuild with no manifest refuses,
// naming the manifest file and the dotfile-copy hint; plain `reindex` on an
// archive whose index is lost but whose manifest is intact refuses, naming
// `reindex -rebuild`. The positive twin (a healthy rebuild) runs first.
func TestRebuildRefusalsAreLegible(t *testing.T) {
	// Positive twin: a healthy archive rebuilds cleanly through the same entry.
	okOut := tmpDir(t)
	buildArchive(t, okOut)
	deleteSearchDB(t, okOut)
	okRep, err := Rebuild(okOut, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("healthy rebuild failed: %v", err)
	}
	assure.Reached(t, okRep.Rebuilt, "records rebuilt from a healthy archive")

	// Rebuild with no manifest: refused, naming the file and the dotfile hint.
	empty := tmpDir(t)
	_, rErr := Rebuild(empty, log.New(io.Discard, "", 0))
	code, msg := 0, ""
	if rErr != nil {
		code, msg = 1, rErr.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(".mailarchive-manifest.json", "dotfile"),
		assure.NoSideEffect(func() bool {
			_, e := os.Stat(filepath.Join(empty, "search.db"))
			return os.IsNotExist(e)
		}))

	// Plain reindex on a lost index (manifest intact): names `reindex -rebuild`.
	lost := tmpDir(t)
	buildArchive(t, lost)
	deleteSearchDB(t, lost)
	_, _, pErr := Reindex(lost, log.New(io.Discard, "", 0))
	pcode, pmsg := 0, ""
	if pErr != nil {
		pcode, pmsg = 1, pErr.Error()
	}
	assure.Refused(t, pcode, pmsg, assure.Code(1),
		assure.Names("reindex -rebuild"),
		assure.NoSideEffect(func() bool {
			_, e := os.Stat(filepath.Join(lost, "search.db"))
			return os.IsNotExist(e) // a refusal must not create an empty index
		}))
}

// covers: MA-176, R13, S33
// The rebuild summary is honest: it reports the from-eml vs re-derived split and
// the count of core fields (subject/from/date) that could not be recovered. A
// page missing its header entirely contributes those three unrecovered fields
// and is still indexed by its recoverable body, never dropped.
func TestRebuildSummaryReportsSplitAndUnrecovered(t *testing.T) {
	out := tmpDir(t)
	htmlOne := &model.Message{
		Subject: "html-one", SenderName: "C", SenderEmail: "c@example.com",
		Received:          time.Date(2025, 3, 3, 8, 0, 0, 0, time.UTC),
		InternetMessageID: "<h1@x>", HTMLBody: "<p>htmloneterm</p>",
	}
	htmlTwo := &model.Message{
		Subject: "html-two", SenderName: "D", SenderEmail: "d@example.com",
		Received:          time.Date(2025, 3, 3, 9, 0, 0, 0, time.UTC),
		InternetMessageID: "<h2@x>", HTMLBody: "<p>htmltwoterm</p>",
	}
	rels := buildRebuildArchive(t, out, []rbMsg{
		{[]string{"Inbox"}, source.ParseRFC822([]byte(alphaRaw))}, // from-eml
		{[]string{"Inbox"}, source.ParseRFC822([]byte(betaRaw))},  // from-eml
		{[]string{"Inbox"}, htmlOne},                              // from-html, all fields present
		{[]string{"Inbox"}, htmlTwo},                              // from-html, header will be stripped
	})

	// Strip html-two's header entirely, leaving only a recoverable body: a
	// worst-case installed-base page the parser cannot read a header from.
	twoPath := filepath.Join(out, filepath.FromSlash(rels["html-two"]))
	stripped := `<!DOCTYPE html><html><body><div class="mailarchive-body">lonelybodyterm</div></body></html>`
	if err := os.WriteFile(twoPath, []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}

	deleteSearchDB(t, out)
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.FromEML != 2 || rep.FromHTML != 2 || rep.Rebuilt != 4 || rep.Pruned != 0 {
		t.Fatalf("rebuild report = %+v, want FromEML=2 FromHTML=2 Rebuilt=4 Pruned=0", rep)
	}
	if rep.Unrecovered != 3 {
		t.Errorf("Unrecovered = %d, want 3 (subject+from+date of the header-stripped page)", rep.Unrecovered)
	}
	// The header-stripped page is still indexed by its body (never dropped).
	if got := indexTotal(t, out, "lonelybodyterm"); got != 1 {
		t.Errorf("header-stripped page body 'lonelybodyterm' = %d, want 1 (dropped)", got)
	}
	// The well-formed from-html page recovered its subject and body normally.
	if got := indexTotal(t, out, "htmloneterm"); got != 1 {
		t.Errorf("well-formed from-html body 'htmloneterm' = %d, want 1", got)
	}
}
