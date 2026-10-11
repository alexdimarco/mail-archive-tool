package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
)

// The extraction workspace's slice E5 (Readable HTML, design
// docs/design-extraction-workspace.md §4.11, §10) lands catalog-first: each row
// starts here as a skip encoder and is replaced by its real test as the piece it
// covers is built. What stays here at the end is what a later slice or an owner
// decision closes, each with the row it cites.
//
// The plan refuses Readable HTML until the owner rules on the tests that pin the
// slice's earlier state (PlanCopyExport's R-h, the settings default and the hidden
// toggle), so these tests turn the option on in the plan they make, as a run that was
// planned with it would carry it.

// e5Mime is a single-part HTML message.
func e5Mime(id, subject, html string) []byte {
	return []byte("From: A <a@example.com>\r\nTo: b@example.com\r\nSubject: " + subject + "\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\n" +
		"Date: Sat, 28 Mar 2026 09:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + html + "\r\n")
}

// e5Wrap breaks a base64 body into 76-character lines.
func e5Wrap(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	return b.String() + s
}

// e5Related is an HTML message with one inline image (Content-ID "img1") and, when pdf is
// not empty, one ordinary attachment.
func e5Related(id, subject, html string, image []byte, pdf string) []byte {
	var b strings.Builder
	b.WriteString("From: A <a@example.com>\r\nTo: b@example.com\r\nSubject: " + subject + "\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\n" +
		"Date: Sat, 28 Mar 2026 09:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"OUT\"\r\n\r\n")
	b.WriteString("--OUT\r\nContent-Type: multipart/related; boundary=\"REL\"\r\n\r\n")
	b.WriteString("--REL\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + html + "\r\n")
	if image != nil {
		b.WriteString("--REL\r\nContent-Type: image/png\r\nContent-Transfer-Encoding: base64\r\nContent-ID: <img1>\r\nContent-Disposition: inline; filename=\"img1.png\"\r\n\r\n")
		b.WriteString(e5Wrap(base64.StdEncoding.EncodeToString(image)) + "\r\n")
	}
	b.WriteString("--REL--\r\n")
	if pdf != "" {
		b.WriteString("--OUT\r\nContent-Type: application/pdf; name=\"report.pdf\"\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=\"report.pdf\"\r\n\r\n")
		b.WriteString(e5Wrap(base64.StdEncoding.EncodeToString([]byte(pdf))) + "\r\n")
	}
	b.WriteString("--OUT--\r\n")
	return []byte(b.String())
}

// e5Files is the files of each message of a run, by its place in the plan (n, from 1).
func e5Files(t *testing.T, e *CopyExportEnv, res *CopyResult) map[int]*ceMsgLine {
	t.Helper()
	out := map[int]*ceMsgLine{}
	if err := walkMessages(e.recordPath(res.RunID), func(pl *cePlanLine, m *ceMsgLine) error {
		if m != nil {
			out[pl.N] = m
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// e5LiveMetas parses a page the way a browser will, with scripting on and off, and
// returns every meta whose http-equiv would act that is not the archive's own policy. It
// is written apart from the product's own check, so the test does not grade itself.
func e5LiveMetas(t *testing.T, page string) []string {
	t.Helper()
	var found []string
	for _, scripting := range []bool{true, false} {
		root, err := xhtml.ParseWithOptions(strings.NewReader(page), xhtml.ParseOptionEnableScripting(scripting))
		if err != nil {
			t.Fatalf("the page does not parse (scripting=%v): %v", scripting, err)
		}
		var walk func(n *xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && strings.EqualFold(n.Data, "meta") {
				var equiv, content string
				for _, a := range n.Attr {
					switch strings.ToLower(a.Key) {
					case "http-equiv":
						equiv = strings.ToLower(strings.TrimSpace(a.Val))
					case "content":
						content = a.Val
					}
				}
				switch equiv {
				case "refresh", "set-cookie", "content-type":
					found = append(found, equiv)
				case "content-security-policy":
					if content != export.ArchiveCSP {
						found = append(found, equiv)
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
	}
	return found
}

// e5Hostile is the MA-300 corpus: a meta refresh smuggled in raw-text elements, in SVG and
// MathML, and in noscript, each beside the text "marker-text".
func e5Hostile() map[string]string {
	smuggled := `&lt;/%[1]s&gt;&lt;meta http-equiv="refresh" content="0;url=http://evil.example/beacon"&gt;`
	live := `<meta http-equiv="refresh" content="0;url=http://evil.example/beacon">`
	return map[string]string{
		"svg style":        `<svg><style>` + strings.ReplaceAll(smuggled, "%[1]s", "style") + `</style></svg>`,
		"math style":       `<math><style>` + strings.ReplaceAll(smuggled, "%[1]s", "style") + `</style></math>`,
		"svg noscript":     `<svg><noscript>` + strings.ReplaceAll(smuggled, "%[1]s", "noscript") + `</noscript></svg>`,
		"svg xmp":          `<svg><xmp>` + strings.ReplaceAll(smuggled, "%[1]s", "xmp") + `</xmp></svg>`,
		"svg iframe":       `<svg><iframe>` + strings.ReplaceAll(smuggled, "%[1]s", "iframe") + `</iframe></svg>`,
		"svg noembed":      `<svg><noembed>` + strings.ReplaceAll(smuggled, "%[1]s", "noembed") + `</noembed></svg>`,
		"svg noframes":     `<svg><noframes>` + strings.ReplaceAll(smuggled, "%[1]s", "noframes") + `</noframes></svg>`,
		"svg unclosed":     `<svg><style>&lt;meta http-equiv="refresh" content="0;url=http://evil.example/beacon"&gt;`,
		"full document":    `<html><head></head><body><p>hi</p><svg><style>` + strings.ReplaceAll(smuggled, "%[1]s", "style") + `</style></svg></body></html>`,
		"noscript body":    `<noscript>` + live + `</noscript>`,
		"noscript head":    `<html><head><noscript>` + live + `</noscript></head><body><p>x</p></body></html>`,
		"noscript nested":  `<div><span><noscript>` + live + `</noscript></span></div>`,
		"noscript svg fo":  `<svg><foreignObject><noscript>` + live + `</noscript></foreignObject></svg>`,
		"noscript escaped": `<noscript>&lt;meta http-equiv="refresh" content="0;url=http://evil.example/beacon"&gt;</noscript>`,
		"live refresh":     live,
		"foreign csp":      `<meta http-equiv="Content-Security-Policy" content="default-src *">`,
		"script":           `<script>alert(1)</script>`,
	}
}

// e5Run runs a plan of msgs with Readable HTML on, the bodies served by body.
func e5Run(t *testing.T, e *CopyExportEnv, msgs []CopyMessage, body func(id string) []byte) (*CopyResult, *CopyPlan) {
	t.Helper()
	src := newFakeSrc()
	src.body = body
	p := cePlan(t, e, src, msgs)
	p.Options.ReadableHTML = true
	return mustRun(t, e, p, src), p
}

// covers: MA-423, R19, R7, R29, S21, S52
// Readable HTML on: each message whose original was written gets <stem>.html beside it,
// from export.RenderWith. The page's head starts with the ArchiveCSP meta then no-referrer
// before any mail markup, and the MA-300 hostile corpus (and a live refresh, a foreign CSP
// meta and a script) re-parses with no live refresh or CSP meta and the mail's words kept;
// it links the sibling .eml by relative name and, for a message with an attachment, says
// the attachment is inside the .eml; role derived-html, with the recorded hash and size
// equal to the file's, listed in the sums and the manifest; the listing's importance,
// read state, categories and sensitivity are overlaid, so the Status and Categories rows
// carry them. A .eml replaced on the destination before the read-back yields no .html and
// the reason "the original changed on the destination after it was written": the message
// is listed as failed, nothing of it stays in the export, and the run goes on.
func TestReadableHTMLComesFromTheSanitizingRenderer(t *testing.T) {
	hostile := e5Hostile()
	var names []string
	for name := range hostile {
		names = append(names, name)
	}
	body := map[string][]byte{}
	msgs := ceMsgs(len(names) + 3)
	for i, name := range names {
		id := msgs[i].SourceID
		body[id] = e5Mime(id, "hostile "+name, "<p>marker-text</p>"+hostile[name])
	}
	state, att, plain := msgs[len(names)], msgs[len(names)+1], msgs[len(names)+2]
	yes := false
	state.State = &CopyMsgState{Importance: "high", IsRead: &yes, Categories: []string{"Blue category", "Q3"}, Sensitivity: "confidential"}
	msgs[len(names)] = state
	body[state.SourceID] = e5Mime(state.SourceID, "state", "<p>marker-text</p>")
	body[att.SourceID] = e5Related(att.SourceID, "attachment", "<p>marker-text</p>", nil, "%PDF-1.4 fake")
	body[plain.SourceID] = e5Mime(plain.SourceID, "plain", "<p>marker-text</p>")

	e := newCEEnv(t)
	res, _ := e5Run(t, e, msgs, func(id string) []byte { return body[id] })
	if res.Counts.Exported != len(msgs) || res.Outcome != CopyOutcomeComplete || res.Counts.OriginalOnly != 0 {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	lines := e5Files(t, e, res)
	htmlPrefix := "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n" +
		`<meta http-equiv="Content-Security-Policy" content="` + export.ArchiveCSP + `">` + "\n" + `<meta name="referrer" content="no-referrer">`
	pages := map[string]string{}
	for i, m := range msgs {
		line := assure.Reached(t, lines[i+1], "the record line of "+m.SourceID)
		if line.Status != statusExported || len(line.Files) != 2 || line.Files[0].Role != roleOriginal || line.Files[1].Role != roleDerivedHTML {
			t.Fatalf("%s: %+v", m.SourceID, line)
		}
		eml, htm := line.Files[0], line.Files[1]
		if strings.TrimSuffix(eml.Path, ".eml")+".html" != htm.Path {
			t.Errorf("%s: the .html %q is not the .eml's %q beside it", m.SourceID, htm.Path, eml.Path)
		}
		data, err := os.ReadFile(filepath.Join(res.Root, filepath.FromSlash(htm.Path)))
		if err != nil {
			t.Fatalf("%s: %v", m.SourceID, err)
		}
		sum := sha256.Sum256(data)
		if htm.SHA256 != hex.EncodeToString(sum[:]) || htm.Size != int64(len(data)) {
			t.Errorf("%s: the record says %s / %d, the .html is %x / %d", m.SourceID, htm.SHA256, htm.Size, sum, len(data))
		}
		page := string(data)
		pages[m.SourceID] = page
		if !strings.HasPrefix(page, htmlPrefix) {
			t.Errorf("%s: the page does not start with the charset meta, the ArchiveCSP meta and no-referrer:\n%.400s", m.SourceID, page)
		}
		if live := e5LiveMetas(t, page); len(live) > 0 {
			t.Errorf("%s: a live hostile meta survived the readable copy: %v", m.SourceID, live)
		}
		if !strings.Contains(page, "marker-text") {
			t.Errorf("%s: the mail's own text was lost", m.SourceID)
		}
		if !strings.Contains(page, `<a href="`+filepath.Base(eml.Path)+`">Original message (.eml)</a>`) {
			t.Errorf("%s: the page does not link its .eml %q by relative name", m.SourceID, filepath.Base(eml.Path))
		}
	}
	if p := pages[plain.SourceID]; strings.Contains(p, "attachments are inside the .eml file") || strings.Contains(p, "Status") {
		t.Errorf("a message with no attachment and no listed state says so:\n%s", p)
	}
	if p := pages[att.SourceID]; !strings.Contains(p, "report.pdf") || !strings.Contains(p, "attachments are inside the .eml file") {
		t.Errorf("the page of a message with an attachment does not list it and say it is inside the .eml:\n%s", p)
	}
	p := pages[state.SourceID]
	for _, want := range []string{"Unread · Importance: high · Sensitivity: confidential", `class="mailarchive-category">Blue category</span>`, `class="mailarchive-category">Q3</span>`} {
		if !strings.Contains(p, want) {
			t.Errorf("the overlaid state is not on the page: no %q", want)
		}
	}

	// the sums and the manifest carry the readable copies
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
	man := manifestOf(t, res.Root)
	if man["options"].(map[string]any)["readable_html"] != true {
		t.Errorf("the manifest's options: %v", man["options"])
	}
	for i, raw := range man["messages"].([]any) {
		files := raw.(map[string]any)["files"].([]any)
		if len(files) != 2 || files[1].(map[string]any)["role"] != roleDerivedHTML {
			t.Errorf("manifest entry %d: %v", i, files)
		}
	}
	if readme := readText(t, res.Root, copyReadmeName); !strings.Contains(readme, "readable copy") {
		t.Errorf("README says nothing of the readable copies:\n%s", readme)
	}

	// a .eml replaced on the destination before the read-back: no .html, the reason, the run goes on
	e2 := newCEEnv(t)
	src := newFakeSrc()
	msgs2 := ceMsgs(3)
	p2 := cePlan(t, e2, src, msgs2)
	p2.Options.ReadableHTML = true
	var swapped string
	e2.faults = &copyFaults{hook: func(step string) {
		if step != "W5read:2" {
			return
		}
		_ = filepath.WalkDir(p2.Dest, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".eml") {
				if b, rerr := os.ReadFile(path); rerr == nil && strings.Contains(string(b), "subj-"+msgs2[1].SourceID) {
					swapped = path
					b[len(b)-4] ^= 0x01 // one byte of the same length: only the hash tells it from the original
					os.WriteFile(path, b, 0o600)
				}
			}
			return nil
		})
	}}
	res2 := mustRun(t, e2, p2, src)
	if swapped == "" {
		t.Fatal("the hook never found the second message's .eml to replace")
	}
	swappedRel, _ := filepath.Rel(res2.Root, swapped)
	if res2.Counts.Exported != 2 || res2.Counts.Failed != 1 || res2.Outcome != CopyOutcomePartial {
		t.Fatalf("%s, %+v", res2.Label, res2.Counts)
	}
	fail := failureOf(t, res2.Root, msgs2[1].SourceID)
	if fail == nil || fail["class"] != classRetryable || fail["reason"] != "the original changed on the destination after it was written" {
		t.Errorf("the replaced original is listed as %v", fail)
	}
	if exists(res2.Root, filepath.ToSlash(swappedRel)) || len(treeFilesWith(t, res2.Root, ".html")) != 2 {
		t.Errorf("something of the changed message stays in the export: %v", treeFiles(t, res2.Root))
	}
	if bad, missing, extra := sumsCheck(t, res2.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums after the replaced original: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
}

// treeFilesWith lists the files under root whose names end in suffix.
func treeFilesWith(t *testing.T, root, suffix string) []string {
	t.Helper()
	var out []string
	for p := range treeFiles(t, root) {
		if strings.HasSuffix(p, suffix) {
			out = append(out, p)
		}
	}
	return out
}

// e5Image is n bytes that do not compress, standing in for an inline image.
func e5Image(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(7)).Read(b)
	return b
}

// e5Refs is an HTML body that references the inline image "img1" k times.
func e5Refs(k int) string {
	return "<p>marker-text</p>" + strings.Repeat(`<img src="cid:img1">`, k)
}

// covers: MA-423, R19, R7, R29, S52
// The state of a Microsoft 365 message (importance, read flag, categories, sensitivity)
// goes from the listing, through the plan and its run record, onto the readable page: the
// adapter keeps what the widened listing carried (Graph's "normal" importance as the empty
// state, an absent flag as unknown), a retry's plan, read back from the run record, carries
// it too, and the page of the message shows the state the listing gave, whatever the MIME
// headers said.
func TestTheListingsStateReachesTheReadablePage(t *testing.T) {
	f := newMeFake(t)
	entry := func(id string, extra map[string]any) string {
		m := map[string]any{"id": id, "subject": "subj-" + id, "receivedDateTime": "2026-03-01T09:00:00Z",
			"from":                          map[string]any{"emailAddress": map[string]string{"address": "a@example.com", "name": "A"}},
			"singleValueExtendedProperties": []map[string]string{{"id": "Integer 0x0E08", "value": "300"}, {"id": "Integer 0x0036", "value": "3"}}}
		for k, v := range extra {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		return string(b)
	}
	f.page("F1", "["+entry("S1", map[string]any{"importance": "high", "isRead": false, "categories": []string{"Blue category"}})+","+
		entry("S2", map[string]any{"importance": "normal", "isRead": true})+"]")
	for _, id := range []string{"S1", "S2"} {
		body := e5Mime(id, "state "+id, "<p>marker-text</p>")
		f.value(id, func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	}
	src := f.source(t)
	msgs, err := src.ListFolder(context.Background(), []CopyFolderRef{{ID: "F1", Name: "Inbox"}})
	if err != nil || len(msgs) != 2 {
		t.Fatalf("%v, %d messages", err, len(msgs))
	}
	s1, s2 := msgs[0].State, msgs[1].State
	if s1 == nil || s1.Importance != "high" || s1.IsRead == nil || *s1.IsRead || len(s1.Categories) != 1 || s1.Sensitivity != "confidential" {
		t.Errorf("S1's state is %+v", s1)
	}
	if s2 == nil || s2.Importance != "" || s2.IsRead == nil || !*s2.IsRead {
		t.Errorf("S2's state is %+v: Graph's normal importance is the empty state, and it is read", s2)
	}

	e := newCEEnv(t)
	e.sizeCap = 1 << 20
	p := planFor(t, e, src, msgs)
	p.Options.ReadableHTML = true
	res := runWith(t, e, p, src)
	if res.Counts.Exported != 2 || res.Counts.OriginalOnly != 0 {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	lines := e5Files(t, e, res)
	page := func(n int) string { return readText(t, res.Root, lines[n].Files[1].Path) }
	if p1 := page(1); !strings.Contains(p1, "Unread · Importance: high · Sensitivity: confidential") || !strings.Contains(p1, `class="mailarchive-category">Blue category</span>`) {
		t.Errorf("S1's page does not show the listing's state:\n%s", p1)
	}
	if p2 := page(2); strings.Contains(p2, "Unread") || strings.Contains(p2, "Importance") || !strings.Contains(p2, "Sensitivity: confidential") {
		t.Errorf("S2's page shows the wrong state:\n%s", p2)
	}
	// the run record keeps it: a retry's messages come back from the record with their state
	rest, err := e.RunRemaining(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	all := 0
	_ = walkMessages(e.recordPath(res.RunID), func(pl *cePlanLine, _ *ceMsgLine) error {
		if pl.State == nil {
			t.Errorf("message %d: the plan line of the run record lost its state", pl.N)
		}
		all++
		return nil
	})
	if all != 2 || len(rest) != 0 {
		t.Errorf("%d plan lines, %d left to export", all, len(rest))
	}
}

// covers: MA-423, R19, R7, R29, S52
// The archive source's records that have an .eml get a readable copy made the same way: from
// the .eml read back from the destination (its hash the one the archive's fixity gave), by the
// sanitizing renderer, beside it as <stem>.html with the role derived-html; the records the
// archive kept no original of (their archived page and zip are the copy there is) get none, and
// their page keeps its own role; and the archive is byte-identical after the run.
func TestReadableHTMLForTheArchiveSource(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	s := a.open()
	p := a.plan(e, s, allKeys(s))
	p.Options.ReadableHTML = true
	before := treeDigest(t, a.dir, "search.db-wal", "search.db-shm")
	res := a.run(e, p, s)
	if res.Status != CopyStatusComplete || res.Counts.Exported != 6 || res.Counts.OriginalOnly != 0 || res.Counts.WithoutOriginal != 3 {
		t.Fatalf("the run: %s %+v", res.Label, res.Counts)
	}
	man := manifestOf(t, res.Root)
	for slug, word := range map[string]string{"hello": "body one", "second": "body two", "client-mail": "body three"} {
		key, rec := a.rec(slug)
		ent := entryOf(t, man, key)
		orig, derived := fileOf(ent, roleOriginal), fileOf(ent, roleDerivedHTML)
		if ent["status"] != statusExported || orig == nil || derived == nil || len(ent["files"].([]any)) != 2 {
			t.Errorf("%s: entry %v, want the original and its readable copy", slug, ent)
			continue
		}
		if orig["sha256"] != rec.Fixity.EML.SHA256 || strings.TrimSuffix(orig["path"].(string), ".eml")+".html" != derived["path"] {
			t.Errorf("%s: original %v, readable copy %v", slug, orig, derived)
		}
		page := readText(t, res.Root, derived["path"].(string))
		if !strings.Contains(page, word) || !strings.Contains(page, "Original message (.eml)") || e5LiveMetas(t, page) != nil {
			t.Errorf("%s: the readable copy lacks the message's words or its link, or holds a live meta:\n%s", slug, page)
		}
	}
	for _, slug := range []string{"with-attachment", "plain-page", "third-page"} {
		ent := entryOf(t, man, a.key(slug))
		if ent["status"] != statusWithoutOrig || fileOf(ent, roleDerivedHTML) != nil || fileOf(ent, roleArchivedPage) == nil {
			t.Errorf("%s: entry %v, want the archived page and no readable copy", slug, ent)
		}
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
	if after := treeDigest(t, a.dir, "search.db-wal", "search.db-shm"); after != before {
		t.Error("the archive changed during an export with Readable HTML on")
	}
}

// covers: MA-423, R19, R7, R29, S52
// An archive record's .eml replaced on the destination before the read-back yields no readable
// copy, as a Microsoft 365 one does: the message is listed as failed (retryable) with the
// reason "the original changed on the destination after it was written", every file the run had
// placed for it is removed, and the others are exported.
func TestReadableHTMLForTheArchiveSourceRefusesAChangedOriginal(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	s := a.open()
	hello, second := a.key("hello"), a.key("second")
	p := a.plan(e, s, []string{hello, second})
	p.Options.ReadableHTML = true
	e.faults = &copyFaults{hook: func(step string) {
		if step != "W5read:1" {
			return
		}
		_ = filepath.WalkDir(p.Dest, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".eml") {
				b, _ := os.ReadFile(path)
				b[len(b)-3] ^= 0x01
				os.WriteFile(path, b, 0o600)
			}
			return nil
		})
	}}
	res := a.run(e, p, s)
	if res.Counts.Exported != 1 || res.Counts.Failed != 1 || res.Outcome != CopyOutcomePartial {
		t.Fatalf("%s %+v", res.Label, res.Counts)
	}
	man := manifestOf(t, res.Root)
	if f := failureListed(man, hello); f == nil || f["class"] != classRetryable || f["reason"] != reasonOriginalChanged {
		t.Errorf("the message with a changed original is listed as %v", f)
	}
	if ent := entryOf(t, man, second); fileOf(ent, roleDerivedHTML) == nil || ent["status"] != statusExported {
		t.Errorf("the other message: %v", ent)
	}
	if got := len(treeFilesWith(t, res.Root, ".eml")) + len(treeFilesWith(t, res.Root, ".html")); got != 2 {
		t.Errorf("%d message files in the export, want the other message's two: %v", got, treeFiles(t, res.Root))
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
}

// covers: MA-509, R29, R10, R1, S52
// A render panic on one message keeps its .eml and the other messages: the .html is absent,
// the entry has status exported-original-only and the note "readable copy could not be made:
// …", the label says "exported without a readable copy" and the run is partial (outcome
// 3), the message is not in the failures, and neither "Export failed messages again" nor
// "Export the remaining messages" is offered for it. The render bound [RR2b-C2]: a message
// within the size cap whose HTML body references one 8 MiB inline image 10 times (a
// predicted render of over 32 MiB) is exported with its .eml and no .html, with the note
// "too large for a readable copy", the same outcome and no retry, and the heap delta of the
// run stays under the bound (the render is never started: it would need several copies of
// a 100 MiB body); the same image referenced twice renders.
func TestAReadableCopyThatCannotBeMadeKeepsTheOriginal(t *testing.T) {
	real := readableRender
	t.Cleanup(func() { readableRender = real })
	readableRender = func(m *model.Message, ctx export.RenderContext) (export.RenderResult, error) {
		if m.Subject == "PANIC" {
			panic("renderer blew up: " + m.Subject)
		}
		return real(m, ctx)
	}
	check := func(name string, res *CopyResult, e *CopyExportEnv, id, wantNote string) {
		t.Helper()
		if res.Counts.Exported != 3 || res.Counts.OriginalOnly != 1 || res.Counts.Failed != 0 || res.Status != CopyStatusPartial || res.Outcome != CopyOutcomePartial ||
			res.Label != "Finished with problems — 1 exported without a readable copy" {
			t.Fatalf("%s: %s, %+v, status %s", name, res.Label, res.Counts, res.Status)
		}
		var line *ceMsgLine
		_ = walkMessages(e.recordPath(res.RunID), func(pl *cePlanLine, m *ceMsgLine) error {
			if pl.SourceID == id {
				line = m
			}
			return nil
		})
		line = assure.Reached(t, line, name+": the record line")
		if line.Status != statusOriginalOnly || len(line.Files) != 1 || line.Files[0].Role != roleOriginal || len(line.Notes) != 1 || !strings.HasPrefix(line.Notes[0], wantNote) {
			t.Errorf("%s: %+v", name, line)
		}
		if got := len(treeFilesWith(t, res.Root, ".html")); got != 2 {
			t.Errorf("%s: %d .html files, want 2 (the message with no readable copy has none)", name, got)
		}
		if got := len(treeFilesWith(t, res.Root, ".eml")); got != 3 {
			t.Errorf("%s: %d .eml files, want 3", name, got)
		}
		if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
			t.Errorf("%s: sums: changed %v, missing %v, unlisted %v", name, bad, missing, extra)
		}
		if failed, _ := e.RunFailed(res.RunID); len(failed) != 0 {
			t.Errorf("%s: %d messages are offered to \"Export failed messages again\"", name, len(failed))
		}
		if rest, _ := e.RunRemaining(res.RunID); len(rest) != 0 {
			t.Errorf("%s: %d messages are offered to \"Export the remaining messages\"", name, len(rest))
		}
		if _, err := e.RetryInput(res.RunID, "failed", ""); err == nil {
			t.Errorf("%s: a retry preview of a run whose only problem is a missing readable copy was made", name)
		}
		man := manifestOf(t, res.Root)
		if man["counts"].(map[string]any)["original_only"] != 1.0 || len(man["failures"].([]any)) != 0 {
			t.Errorf("%s: manifest counts %v, failures %v", name, man["counts"], man["failures"])
		}
	}

	// a panic in the render
	e := newCEEnv(t)
	msgs := ceMsgs(3)
	body := map[string][]byte{}
	for i := range msgs {
		subject := "ok"
		if i == 1 {
			subject = "PANIC"
		}
		body[msgs[i].SourceID] = e5Mime(msgs[i].SourceID, subject, "<p>marker-text</p>")
	}
	res, _ := e5Run(t, e, msgs, func(id string) []byte { return body[id] })
	check("panic", res, e, msgs[1].SourceID, noteReadableFailed)

	// over the render bound: one 8 MiB image, referenced 10 times
	image := e5Image(8 << 20)
	e = newCEEnv(t)
	msgs = ceMsgs(3)
	body = map[string][]byte{}
	for i := range msgs {
		body[msgs[i].SourceID] = e5Related(msgs[i].SourceID, "ok", "<p>marker-text</p>", nil, "")
	}
	big := e5Related(msgs[1].SourceID, "big", e5Refs(10), image, "")
	body[msgs[1].SourceID] = big
	if len(big) > 12<<20 {
		t.Fatalf("the message is %d bytes: not a message within the size cap that the bound is for", len(big))
	}
	var bound *CopyResult
	if peak := peakHeap(func() { bound, _ = e5Run(t, e, msgs, func(id string) []byte { return body[id] }) }); peak > 64<<20 {
		t.Errorf("the heap grew by %d MiB over a run with a message that would render to over 32 MiB: the render was started", peak>>20)
	}
	check("bound", bound, e, msgs[1].SourceID, noteReadableTooLarge)

	// the same image referenced twice renders: the page holds it twice, as data URIs
	e = newCEEnv(t)
	msgs = ceMsgs(1)
	twice := e5Related(msgs[0].SourceID, "twice", e5Refs(2), image, "")
	res, _ = e5Run(t, e, msgs, func(string) []byte { return twice })
	if res.Counts.Exported != 1 || res.Counts.OriginalOnly != 0 || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("the image referenced twice: %s, %+v", res.Label, res.Counts)
	}
	htmls := treeFilesWith(t, res.Root, ".html")
	if len(htmls) != 1 {
		t.Fatalf("the image referenced twice: %d .html files", len(htmls))
	}
	if page := readText(t, res.Root, htmls[0]); strings.Count(page, "data:image/png;base64,") != 2 {
		t.Errorf("the page holds the image %d times, want 2", strings.Count(page, "data:image/png;base64,"))
	}
}

// covers: MA-518, R29, R12
// The preview sizes a plan with Readable HTML on for the files it will write: a plan with
// 150-character ids gives a reserve above 1 KB per message and more than the same plan with
// the option off, the estimate doubles (the design's factor 1 + 1.0, a readable copy being
// about the size of its original), a message with no original (the archive's page) is not
// given a copy, the sample paths and the 1,000-file bound count two files for a message, and
// Start refuses with R-g2 when the free space is one byte short of what the plan needs.
func TestReadableHTMLIsSizedInThePlan(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	msgs := ceMsgs(2000)
	long := strings.Repeat("x", 150)
	for i := range msgs {
		msgs[i].SourceID = long + msgs[i].SourceID
		msgs[i].SizeHint = 1000
		msgs[i].Received = msgs[0].Received // one month: the directory bound shows
	}
	msgs[1999].NoOrig = noOrigNotKept // a message with no original is given no copy
	p := cePlan(t, e, src, msgs)
	sized := func(on bool) *CopyPlan {
		q := &CopyPlan{Dest: p.Dest, ExportName: p.ExportName, Messages: msgs, Options: CopyOptions{PreserveFolders: true, ReadableHTML: on}}
		e.sizePlan(q)
		return q
	}
	off, on := sized(false), sized(true)
	if got, floor := on.Reserve, int64(len(msgs))*1024; got < floor {
		t.Errorf("the reserve is %d, below 1 KB per message (%d)", got, floor)
	}
	// a manifest entry and a sums line for the copy of each of 1,999 messages
	if on.Reserve < off.Reserve+1999*(200+64) || on.RecordBound < off.RecordBound+1999*copyRecordLineBound {
		t.Errorf("a readable copy adds too little reserve (%d against %d) or record bound (%d against %d)", on.Reserve, off.Reserve, on.RecordBound, off.RecordBound)
	}
	if on.EstimateBytes != off.EstimateBytes+1999*1000 {
		t.Errorf("the estimate is %d, want the originals %d plus a copy of each of the 1,999 with an original", on.EstimateBytes, off.EstimateBytes)
	}
	// two files a message: the 1,000-file bound splits the one month into directories of 500 messages
	namer := newCeNamer(true, "/dest/MailExport-x")
	namer.readable = true
	files := map[string]int{}
	for i := range msgs[:1999] {
		files[namer.place(&msgs[i], copyFileCountFor(&msgs[i], CopyOptions{ReadableHTML: true})).Dir] += 2
	}
	if len(files) != 4 {
		t.Errorf("1,999 messages of two files went into %d directories, want 4: %v", len(files), files)
	}
	for dir, n := range files {
		if n > copyFilesPerDir {
			t.Errorf("%s holds %d files, over the %d bound", dir, n, copyFilesPerDir)
		}
	}
	short := func(on bool) int { // the first five messages only: all have an original
		q := &CopyPlan{Dest: p.Dest, ExportName: p.ExportName, Messages: msgs[:5], Options: CopyOptions{PreserveFolders: true, ReadableHTML: on}}
		e.sizePlan(q)
		return q.LongestPath
	}
	if short(true) != short(false)+1 {
		t.Errorf("the longest path with a .html (%d) is not a byte longer than with an .eml (%d)", short(true), short(false))
	}

	// Start compares the free space with what the plan needs, the readable copies included
	p.Options.ReadableHTML = true
	p.EstimateBytes, p.Reserve, p.RecordBound = on.EstimateBytes, on.Reserve, on.RecordBound
	need := p.EstimateBytes + p.Reserve
	e.SetFreeBytes(func(string) (uint64, error) { return uint64(need - 1), nil }, nil)
	_, err := ceRunPlan(t, e, p, src)
	var ref *CopyRefusal
	if !errors.As(err, &ref) || ref.ID != "R-g2" {
		t.Fatalf("one byte short of what the plan needs: %v, want R-g2", err)
	}
}

// covers: MA-513, MA-520, R29, S52
// Check this export names a readable copy as one: a clean export with readable copies is GREEN
// (the positive twin); a readable copy that changed or is missing is RED, named as "the readable
// copy", saying it is a convenience that tells nothing of the .eml beside it, with a remedy that
// says every original is as it was; a changed original is RED with the usual remedy, never the
// readable one, and so is a readable copy together with a changed original.
func TestCheckThisExportNamesAReadableCopyAsOne(t *testing.T) {
	fresh := func() (*CopyExportEnv, *CopyResult) {
		e, src := newCEEnv(t), newFakeSrc()
		p := cePlan(t, e, src, ceMsgs(3))
		p.Options.ReadableHTML = true
		return e, mustRun(t, e, p, src)
	}
	e, res := fresh()
	chk, err := e.CheckExport(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !chk.OK || chk.Files != 8 || chk.ReadableOnly || len(chk.Problems) != 0 {
		t.Fatalf("a clean export with readable copies: %+v, want GREEN with 8 files (3 messages of two, README, manifest)", chk)
	}

	damage := func(t *testing.T, e *CopyExportEnv, res *CopyResult, html bool, eml bool) *CopyCheck {
		t.Helper()
		changed, gone := e5Find(t, res.Root, ".html", "subj-M00001"), e5Find(t, res.Root, ".html", "subj-M00002")
		if html {
			b, _ := os.ReadFile(filepath.Join(res.Root, changed))
			os.WriteFile(filepath.Join(res.Root, changed), append(b, '!'), 0o600)
			os.Remove(filepath.Join(res.Root, gone))
		}
		if eml {
			p := filepath.Join(res.Root, e5Find(t, res.Root, ".eml", "subj-M00003"))
			b, _ := os.ReadFile(p)
			os.WriteFile(p, append(b, '!'), 0o600)
		}
		chk, err := e.CheckExport(res.RunID)
		if err != nil {
			t.Fatal(err)
		}
		return chk
	}

	e, res = fresh()
	chk = damage(t, e, res, true, false)
	said := strings.Join(chk.Problems, "\n")
	if chk.OK || !chk.ReadableOnly || len(chk.Changed) != 1 || len(chk.Missing) != 1 || chk.Remedy != checkReadableRemedy ||
		!strings.Contains(said, "the readable copy "+chk.Changed[0]+" has changed") || !strings.Contains(said, "the readable copy "+chk.Missing[0]+" is missing") ||
		!strings.Contains(said, "says nothing about that .eml") || !strings.Contains(chk.Remedy, "Every original in this folder is as it was") {
		t.Errorf("a changed and a missing readable copy: %+v", chk)
	}

	e, res = fresh()
	chk = damage(t, e, res, false, true)
	if chk.OK || chk.ReadableOnly || chk.Remedy != checkRemedy || strings.Contains(strings.Join(chk.Problems, "\n"), "readable copy") {
		t.Errorf("a changed original: %+v, want the usual remedy and no word of readable copies", chk)
	}

	e, res = fresh()
	chk = damage(t, e, res, true, true)
	if chk.OK || chk.ReadableOnly || chk.Remedy != checkRemedy {
		t.Errorf("a changed original with changed readable copies: %+v, want the usual remedy", chk)
	}
}

// covers: MA-513, MA-437, R29, S52
// A retry of an export with Readable HTML on keeps the option and what the plan lines held, and
// offers only the messages that failed: a message exported without a readable copy is settled,
// so it is neither offered again nor exported a second time, and the messages that are retried
// come back with readable copies.
func TestARetryKeepsReadableHTMLAndLeavesAMessageWithoutACopyAlone(t *testing.T) {
	real := readableRender
	t.Cleanup(func() { readableRender = real })
	readableRender = func(m *model.Message, ctx export.RenderContext) (export.RenderResult, error) {
		if strings.HasPrefix(m.Subject, "subj-M00003") {
			panic("renderer blew up")
		}
		return real(m, ctx)
	}
	e, src := newCEEnv(t), newFakeSrc()
	failIDs(src, map[string]bool{"M00002": true})
	msgs := ceMsgs(4)
	msgs[1].State = &CopyMsgState{Importance: "high"}
	p := cePlan(t, e, src, msgs)
	p.Options.ReadableHTML = true
	first := mustRun(t, e, p, src)
	if first.Counts.Exported != 3 || first.Counts.Failed != 1 || first.Counts.OriginalOnly != 1 || first.Outcome != CopyOutcomePartial {
		t.Fatalf("%s %+v", first.Label, first.Counts)
	}
	in, err := e.RetryInput(first.RunID, "failed", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Messages) != 1 || in.Messages[0].SourceID != "M00002" || !in.Options.ReadableHTML || in.Messages[0].State == nil || in.Messages[0].State.Importance != "high" {
		t.Fatalf("the retry's input: %d messages %+v, options %+v", len(in.Messages), in.Messages, in.Options)
	}
	if rest, _ := e.RunRemaining(first.RunID); len(rest) != 1 || rest[0].SourceID != "M00002" {
		t.Errorf("the remaining messages of the partial run: %v", rest)
	}

	// the retry's plan is made as the first was, and its message comes back with a readable copy
	failIDs(src, nil)
	opts := in.Options
	in.Options.ReadableHTML = false
	plan, err := e.PlanCopyExport(*in)
	if err != nil {
		t.Fatal(err)
	}
	plan.Options = opts
	second := mustRun(t, e, plan, src)
	if second.Counts.Exported != 1 || second.Counts.OriginalOnly != 0 || second.Outcome != CopyOutcomeComplete {
		t.Fatalf("%s %+v", second.Label, second.Counts)
	}
	if got := len(treeFilesWith(t, second.Root, ".html")); got != 1 {
		t.Errorf("the retried message has %d readable copies, want 1", got)
	}
	if src.fetched("M00003") != 1 {
		t.Errorf("the message exported without a readable copy was fetched %d times, want once", src.fetched("M00003"))
	}
}

// e5Find is the path, relative to root, of the file with the suffix whose bytes hold the
// word, "" when there is none.
func e5Find(t *testing.T, root, suffix, word string) string {
	t.Helper()
	for p := range treeFiles(t, root) {
		if !strings.HasSuffix(p, suffix) {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p))); err == nil && strings.Contains(string(b), word) {
			return p
		}
	}
	return ""
}

// covers: MA-513, R29, R12
// A readable copy is requested and missing: one that is gone when the export is finished
// makes the run partial (outcome 3), labelled "Finished with problems — 1 exported without a
// readable copy", and its message stays exported with its original (status
// exported-original-only, the note saying the copy was no longer there), never failed: the
// original is in the sums, the missing copy is not, the message is not offered for retry and
// the run's other messages are untouched. If the original is gone as well the message is
// failed, as before. Finish as partial, which re-hashes what it finds, treats a readable copy
// that was damaged when the computer stopped the same way, and a copy that was never written
// because the run was killed first leaves its message not attempted.
func TestAMissingReadableCopyMakesARunPartialNotFailed(t *testing.T) {
	root := func(p *CopyPlan) string { // the export folder the run made: the one folder in the destination
		ents, _ := os.ReadDir(p.Dest)
		if len(ents) != 1 {
			t.Fatalf("the destination holds %d entries", len(ents))
		}
		return filepath.Join(p.Dest, ents[0].Name())
	}

	// finalization: the copy of message 2 is gone; of message 3 both files are
	e, src := newCEEnv(t), newFakeSrc()
	msgs := ceMsgs(4)
	p := cePlan(t, e, src, msgs)
	p.Options.ReadableHTML = true
	e.faults = &copyFaults{hook: func(step string) {
		if step != "W7pre" {
			return
		}
		r := root(p)
		os.Remove(filepath.Join(r, e5Find(t, r, ".html", "subj-M00002")))
		os.Remove(filepath.Join(r, e5Find(t, r, ".html", "subj-M00003")))
		os.Remove(filepath.Join(r, e5Find(t, r, ".eml", "subj-M00003")))
	}}
	res := mustRun(t, e, p, src)
	if res.Counts.Exported != 3 || res.Counts.OriginalOnly != 1 || res.Counts.Failed != 1 || res.Status != CopyStatusPartial || res.Outcome != CopyOutcomePartial {
		t.Fatalf("%s %+v", res.Label, res.Counts)
	}
	man := manifestOf(t, res.Root)
	ent := man["messages"].([]any)[1].(map[string]any)
	if ent["status"] != statusOriginalOnly || len(ent["files"].([]any)) != 1 || ent["files"].([]any)[0].(map[string]any)["role"] != roleOriginal ||
		!strings.Contains(notesOf(ent), noteReadableFailed+"it was no longer in the export folder") {
		t.Errorf("message 2: %v", ent)
	}
	if f := failureListed(man, msgs[2].SourceID); f == nil || f["class"] != classRetryable || !strings.Contains(f["reason"].(string), "no longer in the export folder") {
		t.Errorf("message 3, whose original is gone too: %v", f)
	}
	if failureListed(man, msgs[1].SourceID) != nil {
		t.Error("message 2, whose copy is gone, is listed as failed")
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
	if sums := readText(t, res.Root, copySumsName); !strings.Contains(sums, filepath.Base(e5Find(t, res.Root, ".eml", "subj-M00002"))) || strings.Contains(sums, "M00002.html") {
		t.Errorf("the sums must hold message 2's original and not its missing copy:\n%s", sums)
	}
	if failed, _ := e.RunFailed(res.RunID); len(failed) != 1 || failed[0].SourceID != "M00003" {
		t.Errorf("\"Export failed messages again\" offers %v, want only message 3", failed)
	}
	if res.Label == "" || !strings.Contains(readText(t, res.Root, copyReadmeName), "Exported without a readable copy") && !strings.Contains(readText(t, res.Root, copyReadmeName), "without a readable copy") {
		t.Errorf("README does not say a message is without a readable copy")
	}

	// the copy of message 2 is the only problem: the label says so, and nothing is offered for retry
	e, src = newCEEnv(t), newFakeSrc()
	p = cePlan(t, e, src, ceMsgs(3))
	p.Options.ReadableHTML = true
	e.faults = &copyFaults{hook: func(step string) {
		if step == "W7pre" {
			r := root(p)
			os.Remove(filepath.Join(r, e5Find(t, r, ".html", "subj-M00002")))
		}
	}}
	res = mustRun(t, e, p, src)
	if res.Label != "Finished with problems — 1 exported without a readable copy" || res.Outcome != CopyOutcomePartial || res.Counts.Failed != 0 {
		t.Fatalf("%s %+v", res.Label, res.Counts)
	}
	if failed, _ := e.RunFailed(res.RunID); len(failed) != 0 {
		t.Errorf("%d messages offered for retry", len(failed))
	}
	if readme := readText(t, res.Root, copyReadmeName); !strings.Contains(readme, "Every selected message is in this export with its original; 1 of them has no readable copy") {
		t.Errorf("README: %s", readme)
	}

	// Finish as partial: killed after message 3; the copy of message 1 gone, that of message 2
	// damaged; message 4 was never tried
	e, src = newCEEnv(t), newFakeSrc()
	p = cePlan(t, e, src, ceMsgs(4))
	p.Options.ReadableHTML = true
	e.faults = &copyFaults{killAt: "W6:3"}
	k := mustRun(t, e, p, src)
	e.faults = nil
	if k.Killed != "W6:3" {
		t.Fatalf("killed at %q", k.Killed)
	}
	gone := e5Find(t, k.Root, ".html", "subj-M00001")
	damaged := e5Find(t, k.Root, ".html", "subj-M00002")
	os.Remove(filepath.Join(k.Root, gone))
	b, _ := os.ReadFile(filepath.Join(k.Root, damaged))
	os.WriteFile(filepath.Join(k.Root, damaged), append(b, []byte("torn by a power loss")...), 0o600)
	fin, err := e.FinishAsPartial(k.RunID)
	if err != nil {
		t.Fatalf("Finish as partial: %v", err)
	}
	if fin.Counts.Exported != 3 || fin.Counts.OriginalOnly != 2 || fin.Counts.Failed != 0 || fin.Counts.NotAttempted != 1 {
		t.Fatalf("%s %+v", fin.Label, fin.Counts)
	}
	man = manifestOf(t, k.Root)
	for i, want := range []string{"it was no longer in the export folder", "it was damaged when the computer stopped"} {
		ent := man["messages"].([]any)[i].(map[string]any)
		if ent["status"] != statusOriginalOnly || len(ent["files"].([]any)) != 1 || !strings.Contains(notesOf(ent), want) {
			t.Errorf("message %d: %v, want exported-original-only, one file and %q", i+1, ent, want)
		}
	}
	bad, missing, extra := sumsCheck(t, k.Root)
	if len(bad)+len(missing) != 0 || len(extra) != 1 || extra[0] != damaged {
		t.Errorf("sums: changed %v, missing %v, unlisted %v: the damaged copy must be the one file the sums do not list", bad, missing, extra)
	}
	if rest, _ := e.RunRemaining(k.RunID); len(rest) != 1 || rest[0].SourceID != "M00004" {
		t.Errorf("\"Export the remaining messages\" offers %v, want message 4 only", rest)
	}
	if failed, _ := e.RunFailed(k.RunID); len(failed) != 0 {
		t.Errorf("a readable copy lost is offered for retry: %v", failed)
	}
}
