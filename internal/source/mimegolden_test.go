package source

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// The two goldens over the MIME-fidelity corpus (docs/design-mime-fidelity.md §9,
// MC7, MC29), recorded by slice S0 from the UNMODIFIED build at a named release
// tag, before any fidelity change:
//
//   - testdata/mime/golden-identity.txt: Identity, Fingerprint, ContentDigest and
//     the local and live manifest keys (plain and #fp-qualified) of every corpus
//     item, of the v1 stub, and of every item of testdata/support.pst;
//   - testdata/mime/golden-content.txt: every model.Message field, and each
//     attachment's name, type, size and byte digest, for the same items.
//
// Both are canonical text (Go-quoted strings, so the bytes are exact), headed by
// their provenance. They are baselines in the CLAUDE.md sense: regenerating one is
// an operator-accepted act under the design's §5 drift policy, never a fix. This
// file records them and holds the guard that today's build still reproduces them.
const (
	mimeGoldenIdentity = "golden-identity.txt"
	mimeGoldenContent  = "golden-content.txt"

	// goldenToken and goldenFolder are the fixed store token and folder the keys
	// are built from, so the keys depend only on the message.
	goldenToken  = "tok"
	goldenFolder = "Inbox"

	mimeV1StubName = "v1-stub (constructed: the message safeParseMessage's recover returns)"
)

// The recorder runs only when asked:
//
//	go test ./internal/source -run '^TestRecordMimeGoldens$' -args \
//	  -mime-record-dir=<dir> -mime-record-provenance=<file>
//
// <file> holds the provenance lines; each is written as a "# " header line.
var (
	mimeRecordDir        = flag.String("mime-record-dir", "", "write the MIME-fidelity goldens into this directory (TestRecordMimeGoldens)")
	mimeRecordProvenance = flag.String("mime-record-provenance", "", "the provenance lines for the goldens' header")
)

// corpusV1Parse is the parser whose view the goldens pin: the frozen v1 replay
// (replayV1, parsev1.go), so the goldens keep pinning v1 after the content parser
// moves; it never gets a different expectation. It does what the v1 build's
// safeParseMessage did around the parse: the transport headers on a message, the
// constant stub when v1 panics.
var corpusV1Parse = func(data []byte) *model.Message {
	m, panicked := replayV1(data)
	if panicked || m == nil {
		return unreadableStub()
	}
	m.TransportHeaders = headerBlock(data)
	return m
}

// gq renders a string losslessly and deterministically: Go-quoted, non-ASCII and
// invalid bytes escaped.
func gq(s string) string { return strconv.QuoteToASCII(s) }

func goldenTime(t time.Time) string {
	if t.IsZero() {
		return "zero"
	}
	_, off := t.Zone()
	return fmt.Sprintf("unixnano=%d offset=%d", t.UnixNano(), off)
}

func writeIdentityGolden(sb *strings.Builder, name string, m *model.Message) {
	id, fp := m.Identity(), m.Fingerprint()
	local := state.Key(goldenToken, goldenFolder, id)
	live := state.LiveKey(goldenToken, id)
	fmt.Fprintf(sb, "== %s\n", name)
	fmt.Fprintf(sb, "identity: %s\n", gq(id))
	fmt.Fprintf(sb, "fingerprint: %s\n", gq(fp))
	fmt.Fprintf(sb, "content_digest: %s\n", gq(m.ContentDigest()))
	fmt.Fprintf(sb, "local_key: %s\n", gq(local))
	fmt.Fprintf(sb, "live_key: %s\n", gq(live))
	fmt.Fprintf(sb, "local_fp_key: %s\n", gq(state.Qualify(local, fp)))
	fmt.Fprintf(sb, "live_fp_key: %s\n", gq(state.Qualify(live, fp)))
}

// writeContentGolden renders the model.Message fields the v1 parse sets. It leaves
// out Locator, ReadGaps and ReadError, which the tag's model does not have and
// which no source in this golden's scope sets for identity or content.
//
// digests renders each text field as its length and SHA-256 instead of its text.
// The corpus is synthetic and is written out in full; support.pst is somebody
// else's real mail, so its content is pinned by digest and not copied into a
// second file.
func writeContentGolden(sb *strings.Builder, name string, m *model.Message, digests bool) {
	fmt.Fprintf(sb, "== %s\n", name)
	text := func(s string) string {
		if digests {
			return fmt.Sprintf("len=%d sha256=%x", len(s), sha256.Sum256([]byte(s)))
		}
		return gq(s)
	}
	for _, f := range []struct{ k, v string }{
		{"subject", m.Subject}, {"sender_name", m.SenderName}, {"sender_email", m.SenderEmail},
		{"to", m.To}, {"cc", m.Cc}, {"bcc", m.Bcc}, {"reply_to", m.ReplyTo},
		{"in_reply_to", m.InReplyTo}, {"references", m.References},
		{"internet_message_id", m.InternetMessageID},
		{"importance", m.Importance}, {"sensitivity", m.Sensitivity}, {"phys_id", m.PhysID},
		{"transport_headers", m.TransportHeaders},
		{"html_body", m.HTMLBody}, {"plain_body", m.PlainBody}, {"rtf_body", m.RTFBody},
	} {
		switch f.k {
		case "importance", "sensitivity", "phys_id":
			fmt.Fprintf(sb, "%s: %s\n", f.k, gq(f.v))
		default:
			fmt.Fprintf(sb, "%s: %s\n", f.k, text(f.v))
		}
	}
	fmt.Fprintf(sb, "unread: %t\n", m.Unread)
	cats := make([]string, len(m.Categories))
	for i, c := range m.Categories {
		cats[i] = text(c)
	}
	fmt.Fprintf(sb, "categories: [%s]\n", strings.Join(cats, ", "))
	fmt.Fprintf(sb, "sent: %s\n", goldenTime(m.Sent))
	fmt.Fprintf(sb, "received: %s\n", goldenTime(m.Received))
	fmt.Fprintf(sb, "identity_date: %s\n", goldenTime(m.IdentityDate))
	if m.Raw == nil {
		fmt.Fprintf(sb, "raw: nil\n")
	} else {
		fmt.Fprintf(sb, "raw: len=%d sha256=%x\n", len(m.Raw), sha256.Sum256(m.Raw))
	}
	fmt.Fprintf(sb, "attachments: %d\n", len(m.Attachments))
	for i, a := range m.Attachments {
		fmt.Fprintf(sb, "attachment %d: filename=%s mime_type=%s content_id=%s size=%d ", i, text(a.Filename), gq(a.MimeType), text(a.ContentID), a.Size)
		if a.WriteTo == nil {
			fmt.Fprintf(sb, "bytes=nil\n")
			continue
		}
		var buf bytes.Buffer
		n, err := a.WriteTo(&buf)
		if err != nil {
			fmt.Fprintf(sb, "bytes_error=%s len=%d sha256=%x\n", gq(err.Error()), n, sha256.Sum256(buf.Bytes()))
			continue
		}
		fmt.Fprintf(sb, "bytes=len=%d sha256=%x\n", buf.Len(), sha256.Sum256(buf.Bytes()))
	}
}

// renderMimeGoldens parses every corpus item, and every item of support.pst, with
// the v1 parser and renders the two goldens' bodies.
func renderMimeGoldens(t *testing.T, items []mimeCorpusItem) (identity, content string) {
	t.Helper()
	// What safeParseMessage's recover returned when v1 panicked in the build that wrote
	// the goldens. No corpus input panics v1 (see TestMimeCorpusIsComplete), so the stub
	// is built directly: its identity is the one stubIdentityTerms reproduces (MC7,
	// ruling J5, MA-330).
	return renderGoldens(t, items, corpusV1Parse, &model.Message{Subject: "(unreadable message)"})
}

// renderGoldens is renderMimeGoldens over any parser of a corpus item and any stub:
// MA-330 renders the identity golden through the production routing with it.
func renderGoldens(t *testing.T, items []mimeCorpusItem, parse func([]byte) *model.Message, stub *model.Message) (identity, content string) {
	t.Helper()
	var idb, cb strings.Builder
	for _, it := range items {
		m := parse(it.data)
		if m == nil {
			t.Fatalf("%s: the parse returned no message", it.name)
		}
		writeIdentityGolden(&idb, it.name, m)
		writeContentGolden(&cb, it.name, m, false)
	}

	writeIdentityGolden(&idb, mimeV1StubName, stub)
	writeContentGolden(&cb, mimeV1StubName, stub, false)

	r, err := Open(mimePSTFile)
	if err != nil {
		t.Fatalf("open %s: %v", mimePSTFile, err)
	}
	defer r.Close()
	n := 0
	err = r.Walk(func(folderPath []string, m *model.Message) error {
		name := fmt.Sprintf("support.pst#%03d %s", n, strings.Join(folderPath, "/"))
		n++
		writeIdentityGolden(&idb, name, m)
		writeContentGolden(&cb, name, m, true)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", mimePSTFile, err)
	}
	if n == 0 {
		t.Fatalf("%s yielded no messages", mimePSTFile)
	}
	return idb.String(), cb.String()
}

// goldenBody returns a golden file's text without its "# " provenance header, and
// the header lines.
func goldenBody(file string) (body string, header []string, err error) {
	data, err := os.ReadFile(filepath.Join(mimeCorpusDir, file))
	if err != nil {
		return "", nil, err
	}
	lines := strings.SplitAfter(string(data), "\n")
	i := 0
	for ; i < len(lines) && strings.HasPrefix(lines[i], "# "); i++ {
		header = append(header, strings.TrimSuffix(strings.TrimPrefix(lines[i], "# "), "\n"))
	}
	return strings.Join(lines[i:], ""), header, nil
}

// firstDiff names the first line where got departs from want, with the item it is
// in.
func firstDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	item := ""
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if strings.HasPrefix(w, "== ") {
			item = w
		}
		if w != g {
			return fmt.Sprintf("line %d, in %q:\n  golden: %s\n  build:  %s", i+1, item, w, g)
		}
	}
	return "no difference"
}

// covers: R27
// The recorder for the two goldens. It writes only when given a directory, so a
// plain test run leaves the checked-in goldens alone: regenerating one is an
// operator-accepted act (design §5), never part of a test run.
func TestRecordMimeGoldens(t *testing.T) {
	if *mimeRecordDir == "" {
		t.Skip("records the goldens only when -mime-record-dir is given (an operator-accepted act, design §5)")
	}
	if *mimeRecordProvenance == "" {
		t.Fatal("-mime-record-provenance is required: a golden without provenance is not recorded")
	}
	prov, err := os.ReadFile(*mimeRecordProvenance)
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	var header strings.Builder
	for _, l := range strings.Split(strings.TrimSuffix(string(prov), "\n"), "\n") {
		header.WriteString("# " + l + "\n")
	}
	identity, content := renderMimeGoldens(t, loadMimeCorpus(t))
	for file, body := range map[string]string{mimeGoldenIdentity: identity, mimeGoldenContent: content} {
		if err := os.WriteFile(filepath.Join(*mimeRecordDir, file), []byte(header.String()+body), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
}

// covers: R27, R3
// Today's build still produces the identity and content goldens, recorded from the
// unmodified release build: every corpus item, the v1 stub and every item of
// support.pst. A fidelity change that moves one is a drift, handled by the design's
// §5 policy (an owner decision), never by regenerating the file. The goldens'
// headers hold their provenance.
func TestMimeGoldensHoldForTheBuild(t *testing.T) {
	identity, content := renderMimeGoldens(t, loadMimeCorpus(t))
	for _, g := range []struct{ file, got string }{{mimeGoldenIdentity, identity}, {mimeGoldenContent, content}} {
		want, header, err := goldenBody(g.file)
		if err != nil {
			t.Fatalf("%s: %v", g.file, err)
		}
		if want != g.got {
			t.Errorf("%s: the build no longer reproduces the golden at %s", g.file, firstDiff(want, g.got))
		}
		for _, key := range []string{"tag:", "commit:", "built:", "toolchain:", "dependencies:", "recorded:"} {
			found := false
			for _, h := range header {
				if strings.HasPrefix(h, key) && strings.TrimSpace(strings.TrimPrefix(h, key)) != "" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: the provenance header has no %q line", g.file, key)
			}
		}
	}
}
