package export

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The write path under slice S1 of docs/design-mime-fidelity.md: identity-view stems
// (MC8), the original carried forward through a re-capture (MC8), ContentHash and the
// parse scheme on every write (MC5, MC12), and the mixed-version run (MA-347).

const idMimeDir = "../../testdata/mime"

// termsFor is the identity terms a message's own content makes: what the v1 parser
// handed the identity view for it.
func termsFor(m *model.Message) *model.IdentityTerms {
	t := &model.IdentityTerms{
		InternetMessageID: m.InternetMessageID, Subject: m.Subject, SenderEmail: m.SenderEmail,
		To: m.To, Cc: m.Cc, Bcc: m.Bcc, IdentityDate: m.IdentityDate,
		HTMLBody: m.HTMLBody, PlainBody: m.PlainBody, RTFBody: m.RTFBody,
	}
	for _, a := range m.Attachments {
		t.Attachments = append(t.Attachments, model.IdentityAttachment{Filename: a.Filename, Size: a.Size})
	}
	return t
}

// v1Message is what a build from before the identity view made of data: the frozen v1
// replay's message and no terms (MA-347: the exporter is fed replayV1 messages). It is
// the replay itself, not the content parser stripped of its terms, so it stays v1 when
// a later slice changes the content parser.
func v1Message(t *testing.T, data []byte) *model.Message {
	t.Helper()
	return source.ParseRFC822V1(data)
}

// contentMoved is m as a later parser would make it: every field a parser may change
// moved (subject decoded, sender, recipients, bodies split, attachments renamed and
// added), with the identity terms left as the v1 parse set them.
func contentMoved(m *model.Message) *model.Message {
	p := *m
	p.Subject = "décodé: " + m.Subject
	p.SenderName = "Changed Sender"
	p.To = m.To + ", extra@example.org"
	p.HTMLBody = m.HTMLBody + "<p>split body</p>"
	p.PlainBody = m.PlainBody + "\n\nsplit body"
	p.Attachments = []model.Attachment{{Filename: "inline.png", MimeType: "image/png", WriteTo: func(w io.Writer) (int64, error) {
		n, err := w.Write([]byte("png"))
		return int64(n), err
	}}}
	for _, a := range m.Attachments {
		a.Filename = "renamed-" + a.Filename
		p.Attachments = append(p.Attachments, a)
	}
	return &p
}

func digestOf(data []byte) state.FileDigest {
	sum := sha256.Sum256(data)
	return state.FileDigest{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
}

// emlPath is the path of the preserved original beside a record's page.
func emlPath(out string, rec state.Record) string {
	return filepath.Join(out, strings.TrimSuffix(filepath.FromSlash(rec.Path), ".html")+".eml")
}

// covers: MA-346, R4, R20, R13, R5
// A re-capture of a -raw record that is run without -raw keeps the record's original:
// the .eml stays, the record keeps its Fixity.EML and the page still links it (MC8).
// A run that does write an original removes the previous one only when it writes a
// new one at another stem. (A -raw REPAIR never rewriting an existing .eml, the other
// half of case (a), is S7's.)
func TestRecaptureWithoutRawCarriesTheOriginalForward(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	att := &blob{}
	raw := []byte("From: a@x\r\nSubject: Quarterly\r\nMessage-ID: <q@x>\r\n\r\nbody\r\n")
	msg := func() *model.Message {
		return &model.Message{Subject: "Quarterly", Received: testDate, InternetMessageID: "<q@x>", PlainBody: "body",
			Raw: raw, Attachments: []model.Attachment{att.att("q.pdf")}}
	}
	key := state.Key("store", "Inbox", msg().Identity())

	first := incExporter(out, manifest)
	first.KeepRaw = true
	if _, err := first.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	before, ok := manifest.Get(key)
	if !ok || before.Fixity == nil || before.Fixity.EML == nil {
		t.Fatalf("the -raw capture recorded no original: %+v", before)
	}
	original, err := os.ReadFile(emlPath(out, before))
	if err != nil || !bytes.Equal(original, raw) {
		t.Fatalf("the -raw capture did not keep the original: %v", err)
	}

	// The attachment's bytes arrive: the record is filled, by a run without -raw.
	att.data = []byte("PDF")
	fill := incExporter(out, manifest)
	if _, err := fill.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	after, _ := manifest.Get(key)
	if fill.Stats.Filled != 1 || after.Path != before.Path {
		t.Fatalf("filled %d, path %q then %q; want one fill at the same path", fill.Stats.Filled, before.Path, after.Path)
	}
	if after.Fixity == nil || after.Fixity.EML == nil || *after.Fixity.EML != *before.Fixity.EML {
		t.Errorf("Fixity.EML = %+v, want the original's %+v carried forward", after.Fixity, before.Fixity.EML)
	}
	if kept, err := os.ReadFile(emlPath(out, after)); err != nil || digestOf(kept) != *before.Fixity.EML {
		t.Errorf("the original is not on disk as its fixity says: %v", err)
	}
	page, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(after.Path)))
	if err != nil || !strings.Contains(string(page), "Original message (.eml)") {
		t.Errorf("the page no longer links the original: %v", err)
	}
	if fill.Stats.RawWritten != 0 {
		t.Errorf("RawWritten = %d on a run without -raw", fill.Stats.RawWritten)
	}

	t.Run("(a) a -raw repair never rewrites an existing .eml", func(t *testing.T) {
		t.Skip("MA-346 (a), second half: the repair branch (V1C, restamp, write discipline) does not exist; lands in MF-S7")
	})
}

// covers: MA-346, R20, R13
// When a re-capture does land at another stem (here the message's date moved) and
// preserves no original of its own, the old original is not deleted with the old page:
// it is the record's only copy of the message. The page no longer links it and the
// record no longer vouches for it; moving it with its record is the re-capture commit's
// (ruling J6).
func TestRecaptureAtAnotherStemKeepsTheOriginalOnDisk(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	raw := []byte("From: a@x\r\nSubject: Moved\r\nMessage-ID: <mv@x>\r\n\r\nbody\r\n")
	// The MIME Date (the identity's) stays; the delivery time (the stem's) moves.
	msg := func(at time.Time) *model.Message {
		return &model.Message{Subject: "Moved", IdentityDate: testDate, Received: at, InternetMessageID: "<mv@x>", PlainBody: "body", Raw: raw}
	}
	key := state.Key("store", "Inbox", msg(testDate).Identity())
	first := incExporter(out, manifest)
	first.KeepRaw = true
	if _, err := first.Export("store", []string{"Inbox"}, msg(testDate)); err != nil {
		t.Fatal(err)
	}
	before, _ := manifest.Get(key)

	second := fullExporter(out, manifest) // no -raw
	if _, err := second.Export("store", []string{"Inbox"}, msg(testDate.Add(48*time.Hour))); err != nil {
		t.Fatal(err)
	}
	after, _ := manifest.Get(key)
	if after.Path == before.Path {
		t.Fatal("the stem did not change, so this test proves nothing")
	}
	if kept, err := os.ReadFile(emlPath(out, before)); err != nil || !bytes.Equal(kept, raw) {
		t.Errorf("the old original was deleted with the old page: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(before.Path))); err == nil {
		t.Error("the old page was not replaced")
	}
	if after.Fixity != nil && after.Fixity.EML != nil {
		t.Errorf("the new record vouches for an original it does not hold at its stem: %+v", after.Fixity.EML)
	}

	// The date moves back: the re-capture lands at the first stem again, where the first
	// capture's original still lies, an orphan of the record that now sits at the other
	// stem (MS1-AC4 b). The record does not own it, so the page must not link it and the
	// record must not vouch for it: the carry is tied to the record's own path.
	third := fullExporter(out, manifest)
	if _, err := third.Export("store", []string{"Inbox"}, msg(testDate)); err != nil {
		t.Fatal(err)
	}
	back, _ := manifest.Get(key)
	if back.Path != before.Path {
		t.Fatalf("the re-capture landed at %q, want the first stem %q, so this test proves nothing", back.Path, before.Path)
	}
	if orphan, err := os.ReadFile(emlPath(out, before)); err != nil || !bytes.Equal(orphan, raw) {
		t.Fatalf("fixture: the orphan original is not on disk at the first stem: %v", err)
	}
	if back.Fixity != nil && back.Fixity.EML != nil {
		t.Errorf("the record vouches for an orphan original it did not carry from its own stem: %+v", back.Fixity.EML)
	}
	page, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(back.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), "Original message (.eml)") {
		t.Error("the page links an original that is not the record's own (an orphan at the new stem)")
	}
}

// covers: MA-346, R4, R20, R13, R5
// A fill and a -mode full run on a record whose v1 subject is legacy 8-bit, in a -raw
// archive, run without -raw and by a build whose parser decodes the subject, keep the
// record's path, its original and its Fixity.EML: the stem is the identity view's,
// never the decoded subject's (MC8).
func TestFillAndFullKeepTheStemAndOriginalOfALegacy8BitSubject(t *testing.T) {
	raw := []byte("From: a@x\r\nSubject: Caf\xe9 menu\r\nMessage-ID: <cafe@x>\r\n\r\nbody\r\n")
	v1 := func(att *blob) *model.Message {
		return &model.Message{Subject: "Caf\xe9 menu", Received: testDate, InternetMessageID: "<cafe@x>", PlainBody: "body",
			Raw: raw, Attachments: []model.Attachment{att.att("menu.pdf")}}
	}
	// The same message as a parser that decodes legacy 8-bit text makes it.
	decoded := func(att *blob) *model.Message {
		m := v1(att)
		terms := termsFor(m)
		m.Subject, m.IdentityTerms = "Café menu", terms
		return m
	}
	if util.Slug("Caf\xe9 menu", slugMax) == util.Slug("Café menu", slugMax) {
		t.Fatal("the two subjects have the same slug, so this test would prove nothing")
	}

	for _, mode := range []struct {
		name string
		mode Mode
	}{{"fill", Incremental}, {"full", Full}} {
		t.Run(mode.name, func(t *testing.T) {
			out := t.TempDir()
			manifest := mustManifest(t)
			att := &blob{}
			key := state.Key("store", "Inbox", v1(att).Identity())

			first := incExporter(out, manifest) // the v1 build, with -raw
			first.KeepRaw = true
			if _, err := first.Export("store", []string{"Inbox"}, v1(att)); err != nil {
				t.Fatal(err)
			}
			before, _ := manifest.Get(key)
			if before.Fixity == nil || before.Fixity.EML == nil {
				t.Fatalf("no original recorded: %+v", before)
			}

			att.data = []byte("PDF") // a fill is due
			second := incExporter(out, manifest)
			second.Mode = mode.mode
			if _, err := second.Export("store", []string{"Inbox"}, decoded(att)); err != nil {
				t.Fatal(err)
			}
			after, _ := manifest.Get(key)
			if after.Path != before.Path {
				t.Errorf("the stem moved: %q then %q", before.Path, after.Path)
			}
			if manifest.Len() != 1 {
				t.Errorf("the manifest holds %d records, want 1", manifest.Len())
			}
			if after.Fixity == nil || after.Fixity.EML == nil || *after.Fixity.EML != *before.Fixity.EML {
				t.Errorf("Fixity.EML = %+v, want %+v", after.Fixity, before.Fixity.EML)
			}
			kept, err := os.ReadFile(emlPath(out, before))
			if err != nil || !bytes.Equal(kept, raw) {
				t.Errorf("the original is gone from its stem: %v", err)
			}
			if n := countSuffix(t, out, ".html"); n != 1 {
				t.Errorf("%d pages on disk, want 1", n)
			}
			page, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(after.Path)))
			if err != nil || !strings.Contains(string(page), "Café menu") {
				t.Errorf("the page does not show the decoded subject the new parse made: %v", err)
			}
		})
	}

	t.Run("(b) the stem a repair's re-capture commit gives", func(t *testing.T) {
		t.Skip("MA-346 (b): a repair is a re-capture through the verdict and write path of re-capture integrity (ruling J6; ESC-2, ESC-3); lands in MF-S7")
	})
	t.Run("(d) a tampered Path is never written through", func(t *testing.T) {
		t.Skip("MA-346 (d): the stem gate and the containment guard on removals belong to the repair; lands in MF-S7")
	})
}

// covers: R27, R3
// A live-path write records the v1-view content digest as ContentHash, the terms'
// digest and not the displayed content's (MC5), local records included (the owner
// accepted the two additive fields on MA-329's golden on 2026-10-11).
func TestEveryWriteRecordsTheV1ViewContentHash(t *testing.T) {
	for _, live := range []bool{false, true} {
		out := t.TempDir()
		manifest := mustManifest(t)
		m := &model.Message{Subject: "Café", Received: testDate, InternetMessageID: "<ch@x>", PlainBody: "décodé"}
		m.IdentityTerms = termsFor(&model.Message{Subject: "Caf\xe9", Received: testDate, InternetMessageID: "<ch@x>", PlainBody: "d\xe9cod\xe9"})
		e := incExporter(out, manifest)
		e.DedupMailboxWide = live
		if _, err := e.Export("store", []string{"Inbox"}, m); err != nil {
			t.Fatal(err)
		}
		key := state.Key("store", "Inbox", m.Identity())
		if live {
			key = state.LiveKey("store", m.Identity())
		}
		rec, ok := manifest.Get(key)
		if !ok {
			t.Fatalf("live=%t: no record under %q", live, key)
		}
		if want := m.ContentDigest(); rec.ContentHash != want || want == "" {
			t.Errorf("live=%t: ContentHash = %q, want the v1-view digest %q", live, rec.ContentHash, want)
		}
		bare := *m
		bare.IdentityTerms = nil
		if rec.ContentHash == bare.ContentDigest() {
			t.Errorf("live=%t: ContentHash is the displayed content's digest, not the v1 view's", live)
		}
	}
}

// covers: R27, MA-347
// Every write stamps the current parse scheme, on a new capture, a fill and a full
// run, local and live.
func TestEveryWriteStampsTheParseScheme(t *testing.T) {
	for _, live := range []bool{false, true} {
		out := t.TempDir()
		manifest := mustManifest(t)
		att := &blob{}
		m := func() *model.Message {
			return &model.Message{Subject: "S", Received: testDate, InternetMessageID: "<ps@x>", PlainBody: "b", Attachments: []model.Attachment{att.att("a.bin")}}
		}
		key := state.Key("store", "Inbox", m().Identity())
		if live {
			key = state.LiveKey("store", m().Identity())
		}
		step := func(name string, e *Exporter) {
			t.Helper()
			e.DedupMailboxWide = live
			if _, err := e.Export("store", []string{"Inbox"}, m()); err != nil {
				t.Fatal(err)
			}
			if rec, ok := manifest.Get(key); !ok || rec.ParseScheme != state.ParseSchemeCurrent {
				t.Errorf("live=%t, %s: ParseScheme = %d, want %d", live, name, rec.ParseScheme, state.ParseSchemeCurrent)
			}
		}
		step("new capture", incExporter(out, manifest))
		att.data = []byte("x")
		step("fill", incExporter(out, manifest))
		step("full run", fullExporter(out, manifest))
	}
}

// covers: MA-347, R27
// A seen record that this run re-read is stamped with the parse scheme in memory, on both
// skip branches: a complete record, and a fillable one that is still incomplete (MS1-AC4
// a). The stamp records an examination, so it is withheld from a record whose re-read
// carries a fidelity note introduced after the scheme the record holds: the repair has
// not looked at it, and stamping would hide it from the repair that the note calls for
// (MS1-AI1). No note exists before S2, so this build's own re-reads are all stamped.
func TestReReadStampsTheParseSchemeUnlessANoteIsUnexamined(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fillable bool
	}{{"complete record", false}, {"still-incomplete fillable record", true}} {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir()
			path := filepath.Join(t.TempDir(), "m.json")
			manifest, err := state.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			att := &blob{} // never given bytes: the fillable record stays incomplete
			msg := func(notes []model.ParseNote) *model.Message {
				m := &model.Message{Subject: "S", Received: testDate, InternetMessageID: "<rr@x>", PlainBody: "b", ParseNotes: notes}
				if tc.fillable {
					m.Attachments = []model.Attachment{att.att("a.bin")}
				}
				return m
			}
			key := state.Key("store", "Inbox", msg(nil).Identity())
			if _, err := incExporter(out, manifest).Export("store", []string{"Inbox"}, msg(nil)); err != nil {
				t.Fatal(err)
			}
			if rec, ok := manifest.Get(key); !ok || rec.Fillable() != tc.fillable {
				t.Fatalf("fixture: record %+v (present %t), want fillable=%t", rec, ok, tc.fillable)
			}
			if err := manifest.Save(); err != nil {
				t.Fatal(err)
			}
			manifest = dropParseScheme(t, path) // an older binary's save: parse_scheme 0
			reread := func(notes []model.ParseNote) state.Record {
				t.Helper()
				e := incExporter(out, manifest)
				exported, err := e.Export("store", []string{"Inbox"}, msg(notes))
				if err != nil {
					t.Fatal(err)
				}
				if exported || e.Stats.SkippedManifest+e.Stats.StillIncomplete != 1 {
					t.Fatalf("the re-read exported %t (skipped %d, still incomplete %d), want one skip", exported, e.Stats.SkippedManifest, e.Stats.StillIncomplete)
				}
				rec, _ := manifest.Get(key)
				return rec
			}

			note := []model.ParseNote{{Code: "future-note", Since: state.ParseSchemeCurrent}}
			if rec := reread(note); rec.ParseScheme != 0 {
				t.Errorf("ParseScheme = %d after a re-read that carries a note the record's scheme predates, want 0 (not examined)", rec.ParseScheme)
			}
			if rec := reread(nil); rec.ParseScheme != state.ParseSchemeCurrent {
				t.Errorf("ParseScheme = %d after a re-read with no unexamined note, want %d", rec.ParseScheme, state.ParseSchemeCurrent)
			}
		})
	}
}

// covers: MA-347, R27
// The edge of the stamp gate (MS1-AI1 re-verification): a note is unexamined only when it
// was introduced AFTER the scheme the record holds (Since above ParseScheme). A note the
// record's own scheme already carried (Since equal) is one that scheme examined, so the
// re-read stamps; reading the edge as unexamined would leave a record at scheme N that a
// build at N+1 re-reads, with a standing note of Since N, unstamped for good. The gate is
// pinned directly because with a single scheme no run can put a record at Since.
func TestExaminedByGateEdge(t *testing.T) {
	note := func(since int) *model.Message {
		return &model.Message{ParseNotes: []model.ParseNote{{Code: "n", Since: since}}}
	}
	for _, tc := range []struct {
		name   string
		m      *model.Message
		scheme int
		want   bool
	}{
		{"no notes", &model.Message{}, 0, true},
		{"note newer than the record's scheme", note(2), 1, false},
		{"note introduced by the record's own scheme", note(1), 1, true},
		{"note older than the record's scheme", note(1), 2, true},
		{"note newer than an unexamined record", note(1), 0, false},
	} {
		if got := examinedBy(tc.m, state.Record{ParseScheme: tc.scheme}); got != tc.want {
			t.Errorf("%s: examinedBy = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// dropParseScheme is what an older binary's save does to a manifest: it rewrites it
// without the field it does not know, parse_scheme, on every record. It returns the
// manifest reloaded from the rewritten file.
func dropParseScheme(t *testing.T, path string) *state.Manifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	entries, _ := doc["entries"].(map[string]any)
	for _, r := range entries {
		delete(r.(map[string]any), "parse_scheme")
	}
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// covers: MA-347, R2, R3, R27
// Mixed versions, identity half. A run by v1 semantics archives mail; an older
// binary's save drops parse_scheme; this build then reads the same mail with its
// content changed by a later parser, and the run exports nothing new, splits no key,
// keeps every stem and rewrites no file; the v1 build then runs again over this build's
// archive with the same result. Local and live paths. (The v1 run's records carry
// parse_scheme, so the older binary's save really drops a written field.)
// (The repair half, three alternations rewriting a record at most once, is S7's.)
func TestMixedVersionsKeepIdentityAndStems(t *testing.T) {
	var corpus [][]byte
	for _, name := range []string{
		"ctl-plain-ascii.eml", "ctl-no-message-id.eml", "ctl-no-message-id-no-date.eml", "ctl-mid-reuse-a.eml", "ctl-mid-reuse-b.eml",
		"f25-apple-mail.eml", "c18-latin1-headers.eml", "c11-text-attachments.eml", "fallback-header-error.eml",
	} {
		data, err := os.ReadFile(filepath.Join(idMimeDir, name))
		if err != nil {
			t.Fatal(err)
		}
		corpus = append(corpus, data)
	}

	for _, live := range []bool{false, true} {
		name := map[bool]string{false: "local", true: "live"}[live]
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			path := filepath.Join(t.TempDir(), "m.json")
			manifest, err := state.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			run := func(manifest *state.Manifest, parse func(*testing.T, []byte) *model.Message) *Exporter {
				t.Helper()
				e := incExporter(out, manifest)
				e.DedupMailboxWide = live
				e.RunAt = testDate
				for _, data := range corpus {
					if _, err := e.Export("store", []string{"Inbox"}, parse(t, data)); err != nil {
						t.Fatal(err)
					}
				}
				if err := manifest.Save(); err != nil {
					t.Fatal(err)
				}
				return e
			}
			snapshot := func(manifest *state.Manifest) map[string]string {
				paths := map[string]string{}
				for k, r := range manifest.All() {
					paths[k] = r.Path
				}
				return paths
			}
			sameArchive := func(label string, got, want map[string]string) {
				t.Helper()
				if len(got) != len(want) {
					t.Errorf("%s: %d records, want %d (a new export or a #fp split)", label, len(got), len(want))
				}
				for k, p := range want {
					if got[k] != p {
						t.Errorf("%s: record %q is at %q, want %q", label, k, got[k], p)
					}
				}
			}
			fidelity := func(t *testing.T, data []byte) *model.Message { return contentMoved(source.ParseRFC822(data)) }
			legacy := func(t *testing.T, data []byte) *model.Message { return v1Message(t, data) }

			// The v1 build writes the archive.
			if e := run(manifest, legacy); e.Stats.Exported == 0 {
				t.Fatal("the v1 run exported nothing")
			}
			want := snapshot(manifest)
			pages := map[string]time.Time{}
			fixity := map[string]state.FileDigest{}
			for k, r := range manifest.All() {
				if r.Fillable() || r.Fixity == nil || r.Fixity.HTML == nil {
					t.Fatalf("fixture: %q is fillable or has no page fixity (%+v), so a later run would re-capture it", k, r)
				}
				pages[r.Path] = mtime(t, filepath.Join(out, filepath.FromSlash(r.Path)))
				fixity[k] = *r.Fixity.HTML
			}

			// An older binary saved it (dropping parse_scheme, which the v1 run wrote);
			// this build reads the mail with its content changed.
			for k, r := range manifest.All() {
				if r.ParseScheme != state.ParseSchemeCurrent {
					t.Fatalf("fixture: %q was written with ParseScheme %d, so the drop below would be nominal", k, r.ParseScheme)
				}
			}
			manifest = dropParseScheme(t, path)
			for k, r := range manifest.All() {
				if r.ParseScheme != 0 {
					t.Fatalf("the older binary's save kept ParseScheme %d on %q", r.ParseScheme, k)
				}
			}
			e := run(manifest, fidelity)
			if e.Stats.Exported != 0 {
				t.Errorf("this build exported %d messages the archive already holds", e.Stats.Exported)
			}
			sameArchive("this build after the old binary", snapshot(manifest), want)
			for k, r := range manifest.All() {
				if r.ParseScheme != state.ParseSchemeCurrent {
					t.Errorf("%q: re-read by this build, ParseScheme = %d, want %d", k, r.ParseScheme, state.ParseSchemeCurrent)
				}
			}
			for k, r := range manifest.All() {
				if r.Fixity == nil || r.Fixity.HTML == nil || *r.Fixity.HTML != fixity[k] {
					t.Errorf("%q: the page's fixity changed, so it was rewritten with the moved content", k)
				}
			}
			for p, at := range pages {
				if !mtime(t, filepath.Join(out, filepath.FromSlash(p))).Equal(at) {
					t.Errorf("%s was rewritten", p)
				}
			}

			// The v1 build runs again over what this build saved.
			manifest = dropParseScheme(t, path)
			if e := run(manifest, legacy); e.Stats.Exported != 0 {
				t.Errorf("the v1 build exported %d messages after this build", e.Stats.Exported)
			}
			sameArchive("the v1 build after this build", snapshot(manifest), want)
		})
	}

	t.Run("a re-read record is stamped with the parse scheme", func(t *testing.T) {
		out := t.TempDir()
		path := filepath.Join(t.TempDir(), "m.json")
		manifest, err := state.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		pass := func(manifest *state.Manifest) {
			t.Helper()
			e := incExporter(out, manifest)
			e.RunAt = testDate
			for _, data := range corpus {
				if _, err := e.Export("store", []string{"Inbox"}, v1Message(t, data)); err != nil {
					t.Fatal(err)
				}
			}
			if err := manifest.Save(); err != nil {
				t.Fatal(err)
			}
		}
		pass(manifest)
		manifest = dropParseScheme(t, path)
		for k, r := range manifest.All() {
			if r.ParseScheme != 0 {
				t.Fatalf("the drop left ParseScheme %d on %q", r.ParseScheme, k)
			}
		}
		pass(manifest)
		if len(manifest.All()) == 0 {
			t.Fatal("no records")
		}
		for k, r := range manifest.All() {
			if r.ParseScheme != state.ParseSchemeCurrent {
				t.Errorf("%q: re-read, ParseScheme = %d, want %d", k, r.ParseScheme, state.ParseSchemeCurrent)
			}
		}
	})

	t.Run("repair half: three alternations", func(t *testing.T) {
		t.Skip("MA-347 (repair half): Record.ParseScheme drives no repair yet, and V1C, the restamp and the repair branch do not exist; lands in MF-S7")
	})
}
