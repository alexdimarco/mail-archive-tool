package source_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// End-to-end tests of slice S2 over the real pipeline (reader, exporter, manifest,
// report, index), run with the walker's content switched on (source.UseFidelity).

const e2eCorpus = "../../testdata/mime"

func corpusFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e2eCorpus, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func e2eTmp(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "s2e2e")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func quiet() *log.Logger { return log.New(io.Discard, "", 0) }

// maildir writes the messages as the files of one maildir folder and returns it.
func maildir(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := filepath.Join(e2eTmp(t), "store")
	for name, data := range files {
		p := filepath.Join(dir, "cur", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runLocal(t *testing.T, in, out string, index bool) app.Result {
	t.Helper()
	res, err := app.Run(context.Background(), app.Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, Index: index}, quiet(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

func loadManifest(t *testing.T, out string) *state.Manifest {
	t.Helper()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// recordOf is the one record whose key names the Message-ID.
func recordOf(t *testing.T, m *state.Manifest, messageID string) (string, state.Record) {
	t.Helper()
	var key string
	var rec state.Record
	n := 0
	for k, r := range m.All() {
		if strings.HasSuffix(k, "mid:"+messageID) {
			key, rec = k, r
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d records for %s, want 1", n, messageID)
	}
	return key, rec
}

// zipOf reads the attachments zip of a record: entry name to bytes.
func zipOf(t *testing.T, out string, rec state.Record) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(filepath.Join(out, filepath.FromSlash(strings.TrimSuffix(rec.Path, ".html")+"-attachments.zip")))
	if err != nil {
		t.Fatalf("open the zip of %s: %v", rec.Path, err)
	}
	defer zr.Close()
	got := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = b
	}
	return got
}

func zipNames(z map[string][]byte) []string {
	var out []string
	for n := range z {
		out = append(out, n)
	}
	return out
}

// reportRows is the verification report's rows (kind, class, folder, date, subject,
// detail, path), without its header.
func reportRows(t *testing.T, out string) [][]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "attachments-report.tsv"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var rows [][]string
	for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")[1:] {
		rows = append(rows, strings.Split(l, "\t"))
	}
	return rows
}

// parseGapKinds is the kinds of the parse-gap labels in a gap list.
func parseGapKinds(labels []string) []string {
	var out []string
	for _, l := range labels {
		if k, _, ok := state.ParseGapParts(l); ok {
			out = append(out, k)
		}
	}
	return out
}

// graphRun archives one message through a fake Graph tenant that serves it as the
// $value of its only message, and returns the manifest.
func graphRun(t *testing.T, data []byte) (*state.Manifest, string) {
	t.Helper()
	s := graphtest.NewServer()
	t.Cleanup(s.Close)
	s.AddFolder("u1", "IN", "Inbox", 10)
	s.AddMessage("u1", "IN", "M1")
	s.SetValue("M1", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) })
	out := e2eTmp(t)
	g := app.GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: s.URL, TokenURL: s.URL + "/token"}
	if _, err := app.RunGraph(context.Background(), g, app.Options{Out: out, Mode: export.Incremental}, quiet()); err != nil {
		t.Fatalf("graph run: %v", err)
	}
	return loadManifest(t, out), out
}

// covers: MA-334, R1, R10, R26
// The re-run and fill half of MA-334, on the real pipeline. An unknown transfer encoding
// keeps its bytes as <name>.undecoded and records a terminal undecoded-part gap even on a
// local source: the report row has kind undecoded-part and class terminal, and a re-run
// neither retries nor rewrites. A base64 part truncated on the first run records a
// fillable corrupt-part gap on an on-demand source, and when the complete message lands
// the next run fills it; the same truncation on a Graph fake stays terminal (MC20).
func TestUndecodedPartsAreTerminalAndTruncationIsFillableOnAnOnDemandSource(t *testing.T) {
	source.UseFidelity(t)

	t.Run("an unknown transfer encoding is terminal and never retried", func(t *testing.T) {
		in := maildir(t, map[string][]byte{"m1": corpusFile(t, "f24-cte-x-uuencode.eml")})
		out := e2eTmp(t)
		first := runLocal(t, in, out, false)
		if first.Stats.Exported != 1 {
			t.Fatalf("exported %d, want 1", first.Stats.Exported)
		}
		_, rec := recordOf(t, loadManifest(t, out), "f24-cte-x-uuencode@corpus.example")
		if kinds := parseGapKinds(rec.Terminal); len(kinds) != 1 || kinds[0] != state.GapKindUndecodedPart || len(rec.Missing) != 0 {
			t.Fatalf("terminal %q missing %q, want one terminal undecoded-part and nothing fillable", rec.Terminal, rec.Missing)
		}
		z := zipOf(t, out, rec)
		if got := z["note.txt.undecoded"]; !bytes.HasPrefix(got, []byte("begin 644 note.txt")) {
			t.Errorf("the still-encoded part is %q in the zip (entries %v)", got, zipNames(z))
		}
		if _, ok := z["r.pdf"]; !ok {
			t.Errorf("r.pdf, the part after it, is not archived (entries %v)", zipNames(z))
		}
		rows := reportRows(t, out)
		if len(rows) != 1 || rows[0][0] != "undecoded-part" || rows[0][1] != "terminal" ||
			!strings.Contains(rows[0][5], "note.txt.undecoded") || !strings.Contains(rows[0][5], " · why: ") || !strings.Contains(rows[0][5], " · what to do: ") {
			t.Errorf("report rows %q, want one undecoded-part row, class terminal, with its why and what to do", rows)
		}
		fillable, terminal, _ := loadManifest(t, out).Counts()
		if fillable != 0 || terminal != 0 || loadManifest(t, out).UndecodableCount() != 1 {
			t.Errorf("counts fillable %d terminal %d undecodable %d, want 0 0 1", fillable, terminal, loadManifest(t, out).UndecodableCount())
		}

		zipPath := filepath.Join(out, filepath.FromSlash(strings.TrimSuffix(rec.Path, ".html")+"-attachments.zip"))
		before, _ := os.Stat(zipPath)
		second := runLocal(t, in, out, false)
		after, _ := os.Stat(zipPath)
		if second.Stats.Exported != 0 || second.Stats.Retried != 0 || !after.ModTime().Equal(before.ModTime()) {
			t.Errorf("a re-run exported %d, retried %d, rewrote the zip %t; want nothing", second.Stats.Exported, second.Stats.Retried, !after.ModTime().Equal(before.ModTime()))
		}
	})

	t.Run("a truncated part is fillable locally and filled when the complete message lands", func(t *testing.T) {
		truncated := corpusFile(t, "f24-cte-base64-truncated.eml")
		complete := bytes.Replace(truncated, []byte("ZW5kb2JqCnRyY\r\n"), []byte("ZW5kb2JqCnRyYWlsZXI8PC9Sb290IDEgMCBSPj4KJSVFT0YK\r\n"), 1)
		if bytes.Equal(complete, truncated) {
			t.Fatal("the complete message is the truncated one")
		}
		in := maildir(t, map[string][]byte{"m1": truncated})
		out := e2eTmp(t)
		runLocal(t, in, out, false)
		key, rec := recordOf(t, loadManifest(t, out), "f24-cte-base64-truncated@corpus.example")
		if kinds := parseGapKinds(rec.Missing); len(kinds) != 1 || kinds[0] != state.GapKindCorruptPart || len(parseGapKinds(rec.Terminal)) != 0 {
			t.Fatalf("missing %q terminal %q, want one fillable corrupt-part", rec.Missing, rec.Terminal)
		}
		if _, ok := zipOf(t, out, rec)["cut.pdf.partial"]; !ok {
			t.Errorf("no cut.pdf.partial in the zip")
		}
		if rows := reportRows(t, out); len(rows) != 1 || rows[0][0] != "corrupt-part" || rows[0][1] != "fillable" {
			t.Errorf("report rows %q, want one corrupt-part row, class fillable", rows)
		}
		if fillable, _, _ := loadManifest(t, out).Counts(); fillable != 1 {
			t.Errorf("fillable %d, want 1", fillable)
		}
		// Still truncated: a re-run re-examines the record and finds nothing new.
		if again := runLocal(t, in, out, false); again.Stats.Retried != 1 || again.Stats.Filled != 0 {
			t.Errorf("re-run over the same truncation: retried %d filled %d, want 1 and 0", again.Stats.Retried, again.Stats.Filled)
		}

		// The complete message lands where the truncated one was.
		if err := os.WriteFile(filepath.Join(in, "cur", "m1"), complete, 0o644); err != nil {
			t.Fatal(err)
		}
		fill := runLocal(t, in, out, false)
		if fill.Stats.Retried != 1 || fill.Stats.Filled != 1 {
			t.Fatalf("after the complete message landed: retried %d filled %d, want 1 and 1", fill.Stats.Retried, fill.Stats.Filled)
		}
		m := loadManifest(t, out)
		key2, rec2 := recordOf(t, m, "f24-cte-base64-truncated@corpus.example")
		if key2 != key || len(rec2.Missing) != 0 || len(rec2.Terminal) != 0 {
			t.Errorf("filled record %q missing %q terminal %q, want the same key and nothing missing", key2, rec2.Missing, rec2.Terminal)
		}
		z := zipOf(t, out, rec2)
		if _, ok := z["cut.pdf"]; !ok || len(z) != 1 {
			t.Errorf("filled zip entries %v, want cut.pdf alone", zipNames(z))
		}
		if m.UndecodableCount() != 0 || len(reportRows(t, out)) != 0 {
			t.Errorf("the gap is not closed: undecodable %d, report rows %q", m.UndecodableCount(), reportRows(t, out))
		}
	})

	t.Run("the same truncation on a Graph fake stays terminal", func(t *testing.T) {
		m, out := graphRun(t, corpusFile(t, "f24-cte-base64-truncated.eml"))
		_, rec := recordOf(t, m, "f24-cte-base64-truncated@corpus.example")
		if kinds := parseGapKinds(rec.Terminal); len(kinds) != 1 || kinds[0] != state.GapKindCorruptPart || len(rec.Missing) != 0 {
			t.Fatalf("terminal %q missing %q, want one terminal corrupt-part and nothing fillable", rec.Terminal, rec.Missing)
		}
		if rows := reportRows(t, out); len(rows) != 1 || rows[0][0] != "corrupt-part" || rows[0][1] != "terminal" {
			t.Errorf("report rows %q, want one corrupt-part row, class terminal", rows)
		}
		if fillable, _, _ := m.Counts(); fillable != 0 {
			t.Errorf("fillable %d, want 0: a Graph message is delivered whole", fillable)
		}
	})

	t.Run("an unsplit multipart is fillable locally and terminal on Graph", func(t *testing.T) {
		data := corpusFile(t, "c14-no-boundary.eml")
		in := maildir(t, map[string][]byte{"m1": data})
		out := e2eTmp(t)
		runLocal(t, in, out, false)
		_, rec := recordOf(t, loadManifest(t, out), "c14-no-boundary@corpus.example")
		if kinds := parseGapKinds(rec.Missing); len(kinds) != 1 || kinds[0] != state.GapKindUnsplitMultipart || len(rec.Terminal) != 0 {
			t.Errorf("local: missing %q terminal %q, want one fillable unsplit-multipart", rec.Missing, rec.Terminal)
		}
		if rows := reportRows(t, out); len(rows) != 1 || rows[0][0] != "unsplit-multipart" || rows[0][1] != "fillable" {
			t.Errorf("local report rows %q, want one unsplit-multipart row, class fillable", rows)
		}
		m, _ := graphRun(t, data)
		_, grec := recordOf(t, m, "c14-no-boundary@corpus.example")
		if kinds := parseGapKinds(grec.Terminal); len(kinds) != 1 || kinds[0] != state.GapKindUnsplitMultipart || len(grec.Missing) != 0 {
			t.Errorf("Graph: terminal %q missing %q, want one terminal unsplit-multipart", grec.Terminal, grec.Missing)
		}
		// The body is there: this is not a "missing body".
		for _, l := range append(append([]string(nil), rec.Missing...), rec.Terminal...) {
			if l == state.MissingBody {
				t.Errorf("a body gap was recorded for a body that is shown as text")
			}
		}
	})
}

// covers: MA-337, R1, R5, R26
// The zip and verify half of MA-337, on the real pipeline: each text attachment's zip
// entry has the sha256 of the transfer-decoded source bytes, the zip's fixity is recorded
// over those bytes and `verify` attests the archive (exit 0); a Latin-1 body part is valid
// UTF-8 on the page.
func TestTextAttachmentsReachTheZipAsTheirSourceBytes(t *testing.T) {
	source.UseFidelity(t)
	latin1Mail := []byte("Message-ID: <s2-latin1@x.example>\r\nDate: Mon, 02 Jan 2023 15:04:05 +0000\r\nFrom: A <a@x.example>\r\nTo: b@x.example\r\nSubject: Latin-1\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"lb\"\r\n\r\n" +
		"--lb\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: 8bit\r\n\r\nGr\xfc\xdfe aus K\xf6ln\r\n" +
		"--lb\r\nContent-Type: text/csv; charset=iso-8859-1; name=\"a.csv\"\r\nContent-Disposition: attachment; filename=\"a.csv\"\r\nContent-Transfer-Encoding: 8bit\r\n\r\nM\xfcller;K\xf6ln\r\n--lb--\r\n")
	in := maildir(t, map[string][]byte{"m1": corpusFile(t, "c11-text-attachments.eml"), "m2": latin1Mail})
	out := e2eTmp(t)
	runLocal(t, in, out, false)
	m := loadManifest(t, out)

	_, rec := recordOf(t, m, "c11-text-attachments@corpus.example")
	z := zipOf(t, out, rec)
	// Each entry's sha256 is that of the transfer-decoded source bytes (the fixture's own
	// base64 and quoted-printable, decoded here), never of a UTF-8 conversion of them.
	b64 := func(s string) []byte {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	sourceBytes := map[string][]byte{
		"latin1.csv": b64("bmFtZTtjaXR5DQpN/GxsZXI7S/Zsbg0KSm9z6Tta/HJpY2gNCg=="),
		"latin1.xml": []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\r\n<invoice><name>Gr\xfc\xdfe K\xf6ln</name></invoice>\r\n"),
		"utf16.txt":  b64("//5HAHIA/ADfAGUAIADlZSxnnooNAAoA"),
		"sjis.txt":   b64("k/qWe4zqgsyDgYOCDQo="),
		"cp1252.ics": b64("QkVHSU46VkNBTEVOREFSDQpCRUdJTjpWRVZFTlQNClNVTU1BUlk6Q2Fm6SCTcXVvdGVklA0KRU5EOlZFVkVOVA0KRU5EOlZDQUxFTkRBUg0K"),
		"page.html":  b64("PGh0bWw+PGJvZHk+PHA+R3L832UgYXVzIEv2bG48L3A+PC9ib2R5PjwvaHRtbD4NCg=="),
	}
	for name, want := range sourceBytes {
		data, ok := z[name]
		if !ok {
			t.Errorf("%s is not in the zip (entries %v)", name, zipNames(z))
			continue
		}
		if sha256.Sum256(data) != sha256.Sum256(want) {
			t.Errorf("%s: the zip entry is %q, want the transfer-decoded source bytes %q", name, data, want)
		}
	}
	if !bytes.HasPrefix(z["utf16.txt"], []byte{0xff, 0xfe}) {
		t.Errorf("utf16.txt does not start with its UTF-16 byte order mark: %q", z["utf16.txt"])
	}
	if rec.Fixity == nil || rec.Fixity.Zip == nil {
		t.Fatal("the record has no zip fixity")
	}
	zb, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(strings.TrimSuffix(rec.Path, ".html")+"-attachments.zip")))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(zb)
	if rec.Fixity.Zip.SHA256 != hex.EncodeToString(sum[:]) || rec.Fixity.Zip.Size != int64(len(zb)) {
		t.Errorf("the recorded zip fixity is not the zip's own digest")
	}
	if rep, err := app.Verify(out, app.VerifyOptions{}, quiet(), nil); err != nil || rep.ExitCode() != 0 {
		t.Errorf("verify: exit %d, err %v, problems %v; want an attested archive", rep.ExitCode(), err, rep.Problems)
	}

	// The Latin-1 body part is valid UTF-8 on the page, and its CSV is as sent.
	_, lrec := recordOf(t, m, "s2-latin1@x.example")
	page, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(lrec.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(page) || !bytes.Contains(page, []byte("Grüße aus Köln")) || bytes.ContainsRune(page, utf8.RuneError) {
		t.Errorf("the page does not show the Latin-1 body as clean UTF-8")
	}
	if csv := zipOf(t, out, lrec)["a.csv"]; !bytes.Equal(csv, []byte("M\xfcller;K\xf6ln")) {
		t.Errorf("a.csv in the zip is %q, want the Latin-1 bytes as sent", csv)
	}
}

// covers: MA-340, R7, R8, R26, R3
// The page and search half of the bodies half of MA-340, on the real pipeline: an
// unlabelled and a us-ascii-labelled 8-bit body show "Grüße aus Köln" on the page, a
// full-text search for Grüße hits them, there is no U+FFFD, and the record's key is the
// frozen replay's.
func TestLegacy8BitBodiesReachThePageAndSearch(t *testing.T) {
	source.UseFidelity(t)
	in := maildir(t, map[string][]byte{
		"m1": corpusFile(t, "c18-latin1-body-unlabelled.eml"),
		"m2": corpusFile(t, "c18-latin1-body-usascii.eml"),
	})
	out := e2eTmp(t)
	runLocal(t, in, out, true)
	m := loadManifest(t, out)
	for _, id := range []string{"c18-latin1-body-unlabelled@corpus.example", "c18-latin1-body-usascii@corpus.example"} {
		key, rec := recordOf(t, m, id)
		if !strings.HasSuffix(key, "mid:"+id) {
			t.Errorf("%s: key %q is not the Message-ID's", id, key)
		}
		page, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rec.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.Valid(page) || !bytes.Contains(page, []byte("Grüße aus Köln")) || bytes.ContainsRune(page, utf8.RuneError) {
			t.Errorf("%s: the page does not show Grüße aus Köln as clean UTF-8", id)
		}
	}
	idx, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	hits, _, err := idx.Search(index.Query{Text: "Grüße", Limit: 10})
	if err != nil || len(hits) != 2 {
		t.Errorf("a search for Grüße hits %d messages (err %v), want 2", len(hits), err)
	}
}

// covers: MA-349, R10, R1, R26
// More than 32 parse gaps in one message: the record carries 32 labels and one summary,
// "and N more undecodable parts", so a hostile message cannot grow the manifest; every
// part is still archived in the zip, and the manifest stays small.
func TestManyParseGapsAreBoundedInTheRecord(t *testing.T) {
	source.UseFidelity(t)
	var b strings.Builder
	b.WriteString("Message-ID: <s2-many@x.example>\r\nDate: Mon, 02 Jan 2023 15:04:05 +0000\r\nFrom: A <a@x.example>\r\nTo: b@x.example\r\nSubject: many\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"bd\"\r\n\r\n")
	const parts = 100
	b.WriteString("--bd\r\nContent-Type: text/plain\r\n\r\nhello\r\n")
	for i := 0; i < parts; i++ {
		fmt.Fprintf(&b, "--bd\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"p%d.bin\"\r\nContent-Transfer-Encoding: x-foo\r\n\r\nopaque %d\r\n", i, i)
	}
	b.WriteString("--bd--\r\n")
	in := maildir(t, map[string][]byte{"m1": []byte(b.String())})
	out := e2eTmp(t)
	runLocal(t, in, out, false)
	_, rec := recordOf(t, loadManifest(t, out), "s2-many@x.example")
	if len(rec.Terminal) != 33 || len(rec.Missing) != 0 {
		t.Fatalf("%d terminal labels, %d fillable; want 32 and one summary, none fillable", len(rec.Terminal), len(rec.Missing))
	}
	if kind, detail, ok := state.ParseGapParts(rec.Terminal[32]); !ok || kind != state.GapKindStructureLimit || detail != fmt.Sprintf("and %d more undecodable parts", parts-32) {
		t.Errorf("the summary label is %q", rec.Terminal[32])
	}
	if z := zipOf(t, out, rec); len(z) != parts {
		t.Errorf("%d zip entries, want all %d parts kept", len(z), parts)
	}
	if rows := reportRows(t, out); len(rows) != 33 {
		t.Errorf("%d report rows, want 32 and one summary row", len(rows))
	}
	raw, err := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 16<<10 {
		t.Errorf("the manifest is %d bytes for one message with %d gaps", len(raw), parts)
	}
}
