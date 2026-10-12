package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// The raw-preserving stub end to end (design-fault-isolation §4.5, F33, FC25; slice
// FI-S2b). No real input panics the frozen v1 parse, so a message whose bytes carry a
// marker word is made to, through the fault-injection seam of the source package.

// panicOn makes the MIME parse panic on every message whose bytes hold word, until the
// test ends or the returned function is called.
func panicOn(t *testing.T, word string) (restore func()) {
	t.Helper()
	restore = source.InjectParsePanics(func(data []byte) bool { return bytes.Contains(data, []byte(word)) })
	t.Cleanup(restore)
	return restore
}

// archiveText is every page and the search index of the archive (never the .eml): what
// a reader is served and what a search can find.
func archiveText(t *testing.T, out string) []byte {
	t.Helper()
	var all bytes.Buffer
	err := filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".db") || strings.HasSuffix(p, "-wal") {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			all.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return all.Bytes()
}

// stubRecord is the archive's one record whose subject is the stub's, with its key.
func stubRecord(t *testing.T, out string) (string, state.Record) {
	t.Helper()
	var key string
	var rec state.Record
	n := 0
	for k, r := range loadArchive(t, out).All() {
		if r.Subject == state.LegacyPlaceholderSubject {
			key, rec, n = k, r, n+1
		}
	}
	if n != 1 {
		t.Fatalf("%d stub records in the archive; want exactly 1", n)
	}
	return key, rec
}

// covers: MA-318, R1, R3, R7, R10, S46
// A MIME-panicking maildir message keeps its bytes, end to end (F33, FC25): its record
// is the "(unreadable message)" stub, unparsed (terminal), stamped with the parser
// generation and the sha-256 of its bytes; its page and the search index carry the
// start of the message as text (the headers and what follows, up to 64 KiB) and NOT the
// rest; the whole message is the .eml under -raw, byte for byte; the report lists it as
// an unparsed-message row with a why and what to do; and a message that merely panics
// is NOT a partial run (unparsed is terminal for this parser generation, nothing the
// archive could still capture is missing). The healthy message beside it is archived as
// ever.
func TestRawStubsKeepTheirBytesAndTheirRecord(t *testing.T) {
	panicOn(t, "POISON")
	store := filepath.Join(tmpDir(t), "Inbox")
	poison := "From: a@example.com\r\nSubject: POISON subject\r\nMessage-ID: <poison@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\n" +
		"HEAD-TOKEN <b>not html</b>\r\n" + strings.Repeat("filler line of the message\r\n", 5000) + "TAIL-TOKEN-BEYOND-64KIB\r\n"
	p14Write(t, filepath.Join(store, "cur", "p:2,S"), poison)
	p14Write(t, filepath.Join(store, "cur", "ok:2,S"), p14Plain("healthy", "ok"))

	out := tmpDir(t)
	res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: true}, discard(), nil)
	if err != nil || res.Partial {
		t.Fatalf("run: err %v, partial %v; want nil, false: a stub that keeps its bytes loses nothing", err, res.Partial)
	}
	if res.Stats.Exported != 2 || res.Stats.Unparsed != 1 || res.Unparsed != 1 {
		t.Errorf("exported %d, unparsed %d (manifest %d); want 2, 1, 1", res.Stats.Exported, res.Stats.Unparsed, res.Unparsed)
	}

	key, rec := stubRecord(t, out)
	sum := sha256.Sum256([]byte(poison))
	if len(rec.Terminal) != 1 || rec.Terminal[0] != state.LabelUnparsed || len(rec.Missing) != 0 {
		t.Errorf("stub record gaps = missing %v terminal %v; want terminal [unparsed] only", rec.Missing, rec.Terminal)
	}
	if rec.ParserGen != state.ParserGenCurrent || rec.RawDigest != hex.EncodeToString(sum[:]) {
		t.Errorf("stub record stamps = gen %d digest %q; want gen %d and the sha-256 of its bytes", rec.ParserGen, rec.RawDigest, state.ParserGenCurrent)
	}
	if rec.Fixity == nil || rec.Fixity.EML == nil {
		t.Fatalf("stub record %q has no preserved original: %+v", key, rec.Fixity)
	}
	eml, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(strings.TrimSuffix(rec.Path, ".html")+".eml")))
	if err != nil || string(eml) != poison {
		t.Errorf("the .eml under -raw is %d bytes (err %v); want the message's %d bytes exactly", len(eml), err, len(poison))
	}
	text := archiveText(t, out)
	if !bytes.Contains(text, []byte("HEAD-TOKEN")) {
		t.Error("neither the page nor the index carries the start of the stub's message")
	}
	if bytes.Contains(text, []byte("TAIL-TOKEN-BEYOND-64KIB")) {
		t.Error("the page or the index holds text from beyond the 64 KiB bound (a hostile message bloats the index)")
	}
	page, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(rec.Path)))
	if !bytes.Contains(page, []byte("mailarchive-notice")) || !bytes.Contains(page, []byte("kept in the .eml file when the archive is made with -raw")) || len(page) > 200<<10 {
		t.Errorf("the stub's page (%d bytes) lacks its notice, or is not bounded", len(page))
	}
	if bytes.Contains(page, []byte("<b>not html</b>")) {
		t.Error("the stub's text was not escaped on the page")
	}
	rows := readLines(t, res.ReportPath)
	var row string
	for _, l := range rows[1:] {
		if strings.HasPrefix(l, "unparsed-message\tterminal\t") {
			row = l
		}
	}
	if !strings.Contains(assure.Reached(t, row, "the unparsed-message report row"), "why:") || !strings.Contains(row, "what to do:") {
		t.Errorf("the unparsed row %q lacks a why and a what to do", row)
	}
}

// covers: R1
// K2 (rulings 13.17 item 1), closed by FI-S2b's raw-preserving stubs: the run that
// re-classes an older build's collapsed "(unreadable message)" record (FC27) must never
// hide a stub THIS build minted. The stub is a record that keeps its bytes and is
// labelled unparsed (terminal), so the re-class, which takes only the old Missing-body
// shape, leaves it alone and it stays listed. And where an older build's placeholder
// sits at the very key the stub is minted at, the stub REPLACES it (it is no archived
// copy of anything), in Incremental and Full mode alike, instead of being held back as
// "the archived copy was kept".
func TestAReclassDoesNotHideAStubThisBuildMinted(t *testing.T) {
	panicOn(t, "POISON")
	mbox := func(msgs ...string) string {
		var b strings.Builder
		for _, m := range msgs {
			b.WriteString("From alice@example.com Mon Mar  3 09:00:00 2025\n" + strings.ReplaceAll(m, "\r\n", "\n") + "\n")
		}
		return b.String()
	}
	poison := p14Mail("POISON subject", "poison", "", "poison body")

	t.Run("the next run's re-class leaves this build's stub listed", func(t *testing.T) {
		store := filepath.Join(tmpDir(t), "tb")
		p14Write(t, filepath.Join(store, "Inbox"), mbox(p14Plain("healthy", "ok"), poison))
		out := tmpDir(t)
		if _, err := runLocal(t, out, store); err != nil {
			t.Fatalf("run 1: %v", err)
		}
		_, before := stubRecord(t, out)
		if len(before.Terminal) != 1 || before.Terminal[0] != state.LabelUnparsed {
			t.Fatalf("the minted stub = %+v; want terminal [unparsed]", before)
		}
		var logs bytes.Buffer
		res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental}, log.New(&logs, "", 0), nil)
		if err != nil {
			t.Fatalf("run 2 (the re-class runs): %v", err)
		}
		key, after := stubRecord(t, out)
		if len(after.Terminal) != 1 || after.Terminal[0] != state.LabelUnparsed || after.RawDigest != before.RawDigest {
			t.Errorf("after the re-class run the stub %q = %+v; want it still unparsed, not %q", key, after, state.LabelLegacyPlaceholder)
		}
		if strings.Contains(logs.String(), "re-classed") {
			t.Errorf("the run re-classed a record of this build:\n%s", logs.String())
		}
		if res.Unparsed != 1 || res.Stats.Unparsed != 1 {
			t.Errorf("run 2 sees %d unparsed in the manifest and %d this run; want the stub counted (status shows it)", res.Unparsed, res.Stats.Unparsed)
		}
		var row string
		for _, l := range readLines(t, res.ReportPath)[1:] {
			if strings.HasPrefix(l, "unparsed-message\t") {
				row = l
			}
			if strings.HasPrefix(l, "unreadable-legacy-placeholder\t") {
				t.Errorf("the report lists this build's stub as a legacy placeholder: %q", l)
			}
		}
		assure.Reached(t, row, "the stub's unparsed-message report row")
	})

	for _, mode := range []export.Mode{export.Incremental, export.Full} {
		name := map[export.Mode]string{export.Incremental: "incremental", export.Full: "full"}[mode]
		t.Run("an older build's placeholder at the stub's key is replaced, "+name, func(t *testing.T) {
			store := filepath.Join(tmpDir(t), "tb")
			p14Write(t, filepath.Join(store, "Inbox"), mbox(p14Plain("healthy", "ok"), poison))
			tok := tokenOf(t, store)
			out := tmpDir(t)
			m := loadArchive(t, out)
			m.Token(state.PathSourceID(store), tok)
			legacyKey := state.Key(tok, "Inbox", model.LegacyStubIdentity())
			m.Add(legacyKey, state.Record{Path: tok + "/Inbox/0000-00-00_0000_unreadable-message_deadbeef.html", Folder: "Inbox",
				Missing: []string{state.MissingBody}, Subject: state.LegacyPlaceholderSubject})
			if err := m.Save(); err != nil {
				t.Fatal(err)
			}
			res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: mode, Index: true, Pages: true}, discard(), nil)
			if err != nil || res.Partial {
				t.Fatalf("run: err %v, partial %v", err, res.Partial)
			}
			r, ok := loadArchive(t, out).Get(legacyKey)
			if !ok || len(r.Terminal) != 1 || r.Terminal[0] != state.LabelUnparsed || r.RawDigest == "" {
				t.Errorf("the record at the stub's key = %+v, %v; want the stub of this build (unparsed, with its digest), not the legacy placeholder", r, ok)
			}
			if res.Stats.Kept != 0 {
				t.Errorf("%d captures held back as 'the archived copy was kept': the placeholder is no archived copy", res.Stats.Kept)
			}
			for _, g := range ledgerOf(t, out) {
				if g.Reason == "refresh-failed" {
					t.Errorf("gap %+v: the stub was held back for a legacy placeholder", g)
				}
			}
			if !bytes.Contains(archiveText(t, out), []byte("poison body")) {
				t.Error("the stub's text is not in the archive: the message was lost behind the placeholder")
			}
		})
	}
}

// covers: MA-318, R1
func TestTheUnparseableFallbackStubIsLabelledUnparsedAndBounded(t *testing.T) {
	t.Skip("MA-318 (FC25): the fallback parser's \"(unparseable message)\" stub (a message net/mail cannot read at all) also keeps its bytes and is labelled unparsed, with its text bounded to 64 KiB. Labelling it makes the KEEP guard hold back every archived record of such a message (older records carry no label), and TestMixedVersionsKeepIdentityAndStems pins that such a re-read is stamped; bounding its text moves MA-330's content golden. Both are the owner's call (rulings 0, rule 2), or MF-S2's header-fallback branch (J5); closes when the owner decides")
	// The body, written when the fresh-eyes check of the S2b adversarial pass found this
	// encoder empty (it would have passed vacuously when un-skipped): a message net/mail
	// cannot read at all keeps its bytes as a record that is labelled unparsed (terminal)
	// and stamped, and its page and index carry at most the first 64 KiB of it.
	store := filepath.Join(tmpDir(t), "Inbox")
	// CRLF line ends, as the corpus's fallback-no-headers.eml has: the parse reads bare LF
	// lines differently.
	junk := strings.ReplaceAll("just some text with no headers at all\nHEAD-TOKEN and no blank line\n"+strings.Repeat("filler line of the message\n", 5000)+"TAIL-TOKEN-BEYOND-64KIB\n", "\n", "\r\n")
	p14Write(t, filepath.Join(store, "cur", "j:2,S"), junk)
	out := tmpDir(t)
	res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: true}, discard(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// The archive's one record is the fallback stub (the manifest keeps a subject only
	// for a record with a gap, so the record is found by being the only one).
	var rec state.Record
	n := 0
	for _, r := range loadArchive(t, out).All() {
		rec, n = r, n+1
	}
	if n != 1 || !bytes.Contains(archiveText(t, out), []byte("(unparseable message)")) {
		t.Fatalf("%d records in the archive; want exactly 1, the fallback stub of a message net/mail cannot read", n)
	}
	if len(rec.Terminal) != 1 || rec.Terminal[0] != state.LabelUnparsed || rec.ParserGen != state.ParserGenCurrent || rec.RawDigest == "" {
		t.Errorf("the fallback record = terminal %v, gen %d, digest %q; want it labelled unparsed, stamped and digested", rec.Terminal, rec.ParserGen, rec.RawDigest)
	}
	if res.Unparsed != 1 {
		t.Errorf("the run counts %d unparsed; want the fallback stub listed", res.Unparsed)
	}
	text := archiveText(t, out)
	if !bytes.Contains(text, []byte("HEAD-TOKEN")) {
		t.Error("neither the page nor the index carries the start of the message")
	}
	if bytes.Contains(text, []byte("TAIL-TOKEN-BEYOND-64KIB")) {
		t.Error("the page or the index holds text from beyond the 64 KiB bound")
	}
}

// stubServer is a mailbox u1 with an OK message and a STUB message whose $value is
// served through count (a counter of its downloads) and whose bytes carry the marker the
// parse panics on.
func stubServer(count *int) (*graphtest.Server, string) {
	s := graphtest.NewServer()
	s.AddFolder("u1", "F_IN", "Inbox", 10)
	s.AddMessage("u1", "F_IN", "OK")
	s.AddMessage("u1", "F_IN", "STUB")
	body := "From: a@example.com\r\nSubject: subj-STUB\r\nMessage-ID: <stub@x>\r\nDate: Sat, 01 Mar 2025 09:00:00 +0000\r\n\r\nSTUB-TOKEN body\r\n"
	s.SetValue("STUB", func(w http.ResponseWriter, r *http.Request) {
		*count++
		io.WriteString(w, body)
	})
	return s, body
}

func graphRunRaw(g GraphOptions, out string, mode export.Mode) (Result, error) {
	return RunGraph(context.Background(), g, Options{Out: out, Mode: mode, KeepRaw: true}, log.New(io.Discard, "", 0))
}

// covers: MA-318, R1, R3, R7, R10, S46
// The Graph half of the raw-preserving stub (FI-S3 gave it its listing Message-ID and the
// hold-back; FI-S2b gives it its bytes): a message whose parse panics is a record under
// its listing's Message-ID that keeps its bytes (the .eml under -raw), is labelled unparsed
// with its parser generation and digest, shows the start of the message on its page, and
// is not downloaded again by the next run (the fast path skips it).
func TestGraphStubKeepsItsBytes(t *testing.T) {
	panicOn(t, "subj-STUB")
	var downloads int
	s, body := stubServer(&downloads)
	defer s.Close()
	out := tmpDir(t)
	if res, err := graphRunRaw(isoOptions(s), out, export.Incremental); err != nil || res.Partial || res.Stats.Exported != 2 {
		t.Fatalf("run 1: exported %d, err %v, partial %v; want 2, nil, false", res.Stats.Exported, err, res.Partial)
	}
	key, rec := recordForID(t, out, "stub@x")
	sum := sha256.Sum256([]byte(body))
	if len(rec.Terminal) != 1 || rec.Terminal[0] != state.LabelUnparsed || rec.ParserGen != state.ParserGenCurrent || rec.RawDigest != hex.EncodeToString(sum[:]) {
		t.Errorf("record %q = %+v; want an unparsed stub with its generation and digest", key, rec)
	}
	eml, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(strings.TrimSuffix(rec.Path, ".html")+".eml")))
	if err != nil || string(eml) != body {
		t.Errorf("the .eml = %q (err %v); want the message's bytes", eml, err)
	}
	if page, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(rec.Path))); !bytes.Contains(page, []byte("STUB-TOKEN body")) {
		t.Error("the stub's page does not show the start of the message")
	}
	if res, err := graphRunRaw(isoOptions(s), out, export.Incremental); err != nil || downloads != 1 || res.Stats.Exported != 0 {
		t.Errorf("run 2: err %v, %d downloads, exported %d; want the stub skipped by the fast path (1 download in all)", err, downloads, res.Stats.Exported)
	}
}

// covers: MA-323, R1, R5, S46
// A stale-ParserGen unparsed Graph stub is re-fetched exactly once (FC25), end to end:
// a stub of an older parser generation (or none: an older binary's save drops the stamp)
// is not skipped by the fast path, it is downloaded in that run only. If the parse still
// fails the stub is stamped with the current generation, held back from nothing (it is
// no archived copy), and not downloaded again; if the new parse succeeds the record
// heals in place, under its own key, with no #fp-split sibling.
func TestStaleStubIsFetchedOnceByTheGraphWalk(t *testing.T) {
	restore := panicOn(t, "subj-STUB")
	var downloads int
	s, _ := stubServer(&downloads)
	defer s.Close()
	out := tmpDir(t)
	if _, err := graphRunRaw(isoOptions(s), out, export.Incremental); err != nil || downloads != 1 {
		t.Fatalf("run 1: err %v, %d downloads; want the stub made, 1 download", err, downloads)
	}
	stale := func() string {
		t.Helper()
		key, rec := recordForID(t, out, "stub@x")
		m := loadArchive(t, out)
		rec.ParserGen = 0
		m.Add(key, rec)
		if err := m.Save(); err != nil {
			t.Fatal(err)
		}
		return key
	}

	// Stale, the parse still fails: downloaded once, stamped, then left alone.
	key := stale()
	res, err := graphRunRaw(isoOptions(s), out, export.Incremental)
	if err != nil || downloads != 2 || res.Stats.Kept != 0 {
		t.Fatalf("run 2 (stale): err %v, %d downloads, kept %d; want 1 more download and no hold-back", err, downloads, res.Stats.Kept)
	}
	if k2, rec := recordForID(t, out, "stub@x"); k2 != key || rec.ParserGen != state.ParserGenCurrent || len(rec.Terminal) != 1 || rec.Terminal[0] != state.LabelUnparsed {
		t.Errorf("after the re-fetch the record %q = %+v; want the same stub, stamped with generation %d", k2, rec, state.ParserGenCurrent)
	}
	for _, g := range ledgerOf(t, out) {
		t.Errorf("gap %+v: re-fetching a stale stub records no gap", g)
	}
	if _, err := graphRunRaw(isoOptions(s), out, export.Incremental); err != nil || downloads != 2 {
		t.Errorf("run 3: err %v, %d downloads; want the stamped stub skipped again (still 2)", err, downloads)
	}

	// Stale, and a parser that now parses it: healed in place.
	key = stale()
	restore()
	before := loadArchive(t, out).Len()
	if _, err := graphRunRaw(isoOptions(s), out, export.Incremental); err != nil || downloads != 3 {
		t.Fatalf("run 4 (stale, parser fixed): err %v, %d downloads; want 1 more", err, downloads)
	}
	k2, rec := recordForID(t, out, "stub@x")
	if k2 != key || len(rec.Terminal) != 0 || len(rec.Missing) != 0 || rec.ParserGen != 0 || rec.RawDigest != "" || rec.Subject == state.LegacyPlaceholderSubject {
		t.Errorf("the healed record %q = %+v; want it whole, under its own key, with no stub stamp", k2, rec)
	}
	if after := loadArchive(t, out).Len(); after != before {
		t.Errorf("%d records after the heal, %d before: a healthy read of a stub's key is no #fp-split sibling", after, before)
	}
}
