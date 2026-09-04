package export

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
)

// covers: MA-17
func TestRenderPlainBody(t *testing.T) {
	m := &model.Message{
		Subject:     "Hi <there>",
		SenderName:  "Alice",
		SenderEmail: "alice@example.com",
		To:          "bob@example.com",
		Received:    time.Date(2026, 7, 15, 10, 32, 0, 0, time.UTC),
		PlainBody:   "line1\n<script>evil</script>",
	}
	out, consumed, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if len(consumed) != 0 {
		t.Errorf("expected no inline attachments, got %v", consumed)
	}
	if !strings.Contains(s, "<meta charset=\"utf-8\">") {
		t.Error("missing charset meta")
	}
	// Subject and body must be HTML-escaped.
	if !strings.Contains(s, "Hi &lt;there&gt;") {
		t.Error("subject not escaped")
	}
	if strings.Contains(s, "<script>evil</script>") {
		t.Error("plain body was not escaped")
	}
	if !strings.Contains(s, "alice@example.com") || !strings.Contains(s, "bob@example.com") {
		t.Error("header missing sender/recipient")
	}
}

// covers: MA-18
func TestRenderHTMLDocumentInjectsHeader(t *testing.T) {
	m := &model.Message{
		Subject:  "Report",
		HTMLBody: "<html><head><style>p{color:red}</style></head><body><p>Hello</p></body></html>",
	}
	out, _, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Original document styles preserved.
	if !strings.Contains(s, "p{color:red}") {
		t.Error("original <style> lost")
	}
	// Our metadata header injected inside the body.
	if !strings.Contains(s, "mailarchive-header") {
		t.Error("metadata header not injected")
	}
	bodyIdx := strings.Index(s, "<body>")
	headerIdx := strings.Index(s, `class="mailarchive-header"`)
	if bodyIdx < 0 || headerIdx < bodyIdx {
		t.Error("header should appear after <body>")
	}
}

// covers: MA-19
func TestRenderEmbedsInlineImage(t *testing.T) {
	payload := []byte("PNGDATA")
	m := &model.Message{
		Subject:  "Inline",
		HTMLBody: `<html><body><img src="cid:img1@host"></body></html>`,
		Attachments: []model.Attachment{
			{
				Filename:  "logo.png",
				MimeType:  "image/png",
				ContentID: "<img1@host>",
				WriteTo:   func(w io.Writer) (int64, error) { n, err := w.Write(payload); return int64(n), err },
			},
		},
	}
	out, consumed, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "data:image/png;base64,") {
		t.Error("inline image not embedded as data URI")
	}
	if strings.Contains(s, "cid:img1@host") {
		t.Error("cid reference not replaced")
	}
	if !consumed[0] {
		t.Error("attachment 0 should be marked consumed inline")
	}
	if hasArchivable(m.Attachments, consumed) {
		t.Error("inline-only attachment should not be archivable")
	}
}

// covers: MA-20
func TestWriteZipSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	zipPath := dir + "/a.zip"
	atts := []model.Attachment{
		{Filename: "empty.txt", WriteTo: func(w io.Writer) (int64, error) { return 0, nil }},
		{Filename: "doc.txt", WriteTo: func(w io.Writer) (int64, error) {
			return io.Copy(w, bytes.NewReader([]byte("content")))
		}},
	}
	zr, err := WriteZip(zipPath, atts, map[int]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if zr.Written != 1 {
		t.Errorf("expected 1 archived attachment, got %d", zr.Written)
	}
	if len(zr.Empty) != 1 || zr.Empty[0] != "empty.txt" {
		t.Errorf("expected empty=[empty.txt], got %v", zr.Empty)
	}
}

// covers: MA-80, R19, S21
// An exported document is inert when opened from disk (file://), where no HTTP
// header can protect it: on BOTH render paths (a message carrying its own
// <html> document, and a fragment/plain body placed in our template) the
// document's <head> starts with the archive Content-Security-Policy meta — the
// same policy `serve` sends — and a no-referrer meta, placed BEFORE any
// mail-supplied <base>/<link>/<style>; navigation-triggering and policy-relaxing
// http-equiv metas from the mail (refresh, content-security-policy) are
// neutralized; a document with <html> but no <head> gets one.
func TestRenderIsInertOffline(t *testing.T) {
	cspMeta := `<meta http-equiv="Content-Security-Policy" content="` + ArchiveCSP + `">`

	// 1. Full document with hostile head + body.
	full := &model.Message{
		Subject: "Your account needs attention",
		HTMLBody: `<html><head>` +
			`<base href="http://tracker.evil.example/">` +
			`<meta http-equiv="Refresh" content="0;url=http://tracker.evil.example/go">` +
			`<meta HTTP-EQUIV='content-security-policy' content="default-src *">` +
			`<link rel="stylesheet" href="http://tracker.evil.example/x.css">` +
			`</head><body><img src="http://tracker.evil.example/pixel.png"><script>alert(1)</script></body></html>`,
	}
	out, _, err := Render(full)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	cspAt := strings.Index(s, cspMeta)
	if cspAt < 0 {
		t.Fatalf("full document lacks the archive CSP meta:\n%s", s)
	}
	headAt := strings.Index(strings.ToLower(s), "<head>")
	baseAt := strings.Index(s, "<base ")
	if headAt < 0 || cspAt < headAt || (baseAt >= 0 && cspAt > baseAt) {
		t.Errorf("CSP meta must be the first child of <head>, before mail-supplied <base>: head=%d csp=%d base=%d", headAt, cspAt, baseAt)
	}
	if !strings.Contains(s, `<meta name="referrer" content="no-referrer">`) {
		t.Error("missing no-referrer meta")
	}
	low := strings.ToLower(s)
	if strings.Contains(low, `http-equiv="refresh"`) || strings.Contains(low, `http-equiv='refresh'`) || strings.Contains(low, `http-equiv=refresh`) {
		t.Error("mail-supplied meta refresh survived")
	}
	if strings.Count(low, `http-equiv="content-security-policy"`)+strings.Count(low, `http-equiv='content-security-policy'`) != 1 {
		t.Errorf("exactly one live Content-Security-Policy meta (ours) expected:\n%s", s)
	}
	if !strings.Contains(s, "tracker.evil.example/pixel.png") {
		t.Error("body content must be preserved verbatim (the CSP, not stripping, is the control)")
	}

	// 2. Fragment body in our template: the meta refresh in the body is neutralized
	//    and the template head carries the policy.
	frag := &model.Message{
		Subject:  "Fragment",
		HTMLBody: `<p>hi</p><meta http-equiv=refresh content="5;url=http://tracker.evil.example/f">`,
	}
	out, _, err = Render(frag)
	if err != nil {
		t.Fatal(err)
	}
	s = string(out)
	if !strings.Contains(s, cspMeta) {
		t.Error("template document lacks the archive CSP meta")
	}
	if strings.Contains(strings.ToLower(s), "http-equiv=refresh") {
		t.Error("meta refresh inside a fragment body survived")
	}

	// 3. A document with <html> but no <head> gets one, carrying the policy.
	noHead := &model.Message{Subject: "NoHead", HTMLBody: `<html><body><p>x</p></body></html>`}
	out, _, err = Render(noHead)
	if err != nil {
		t.Fatal(err)
	}
	s = string(out)
	if !strings.Contains(s, cspMeta) || strings.Index(s, cspMeta) > strings.Index(s, "<body") {
		t.Errorf("head-less document must gain a <head> with the policy before <body>:\n%s", s)
	}

	// 4. Plain-text messages render through the template too.
	plain := &model.Message{Subject: "Plain", PlainBody: "hello"}
	out, _, err = Render(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), cspMeta) {
		t.Error("plain-body document lacks the archive CSP meta")
	}
}

// covers: MA-80, R19, S21
// Neutralization must match what the BROWSER parses, not a regex: an
// entity-encoded http-equiv, a ">" inside a quoted attribute before http-equiv,
// markup placed before <head>, a comment that looks like a head, a mail-supplied
// charset, and a <body> with its own attributes must all end up with the
// archive policy as the first thing in the head, hostile metas defanged, the
// mail's styles kept, and the body attributes preserved on the content wrapper.
// Unresolved cid: images become a caption, not a broken-image icon.
func TestRenderNeutralizesParserTricks(t *testing.T) {
	cspMeta := `<meta http-equiv="Content-Security-Policy" content="` + ArchiveCSP + `">`
	cases := map[string]string{
		"entity-encoded http-equiv": `<html><head></head><body><meta http-equiv="&#114;efresh" content="0;url=https://evil.example/">hi</body></html>`,
		"gt inside quoted attr":     `<html><head></head><body><meta content="0; url=https://evil.example/?x=>" http-equiv="refresh">hi</body></html>`,
		"markup before head":        `<html><link rel="stylesheet" href="https://evil.example/x.css"><head><style>p{color:red}</style></head><body>hi</body></html>`,
		"comment head":              `<!--<head>--><html><head><meta http-equiv="refresh" content="0;url=https://evil.example/"></head><body>hi</body></html>`,
		"fragment with early link":  `<link rel="stylesheet" href="https://evil.example/y.css"><p>hi</p>`,
		"uppercase and spacing":     `<HTML><HEAD><META HTTP-EQUIV = "Refresh" CONTENT="1"></HEAD><BODY>hi</BODY></HTML>`,
	}
	for name, body := range cases {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := string(out.HTML)
		low := strings.ToLower(s)
		cspAt := strings.Index(s, cspMeta)
		if cspAt < 0 {
			t.Errorf("%s: no archive CSP meta", name)
			continue
		}
		if evil := strings.Index(low, "evil.example"); evil >= 0 && evil < cspAt {
			t.Errorf("%s: mail markup (%d) precedes the policy (%d):\n%s", name, evil, cspAt, s)
		}
		for _, live := range []string{`http-equiv="refresh"`, `http-equiv=refresh`, `http-equiv="&#114;efresh"`, `http-equiv='refresh'`} {
			if strings.Contains(low, live) {
				t.Errorf("%s: a live refresh survived: %s\n%s", name, live, s)
			}
		}
		if !strings.Contains(s, "hi") {
			t.Errorf("%s: body content lost", name)
		}
	}

	// The mail's own head styles survive (moved under our head); its charset
	// and title do not compete with ours; body attributes are kept.
	m := &model.Message{Subject: "Styled", HTMLBody: `<html><head><meta charset="iso-8859-1"><title>THEIRS</title><style>p{color:red}</style></head><body style="background:#eee" bgcolor="#ffffff"><p>Hello</p></body></html>`}
	out, err := RenderWith(m, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out.HTML)
	if !strings.Contains(s, "p{color:red}") {
		t.Error("mail <style> lost")
	}
	if strings.Count(strings.ToLower(s), "<meta charset") != 1 || strings.Contains(strings.ToLower(s), "iso-8859-1") {
		t.Errorf("mail charset must not compete with ours:\n%s", s)
	}
	if strings.Contains(s, "<title>THEIRS</title>") {
		t.Error("mail <title> replaced ours")
	}
	if !strings.Contains(s, `background:#eee`) {
		t.Error("body style attribute lost")
	}

	// An unresolved cid: image becomes a caption, and is reported.
	dangling := &model.Message{Subject: "Dangling", HTMLBody: `<p>see <img src="cid:gone@x" width="10"></p>`}
	out, err = RenderWith(dangling, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	s = string(out.HTML)
	if strings.Contains(s, `src="cid:`) {
		t.Error("dangling cid src left in place (broken-image icon)")
	}
	if !strings.Contains(s, "inline image not included") {
		t.Error("no caption for the missing inline image")
	}
	if len(out.Unresolved) != 1 || out.Unresolved[0] != "gone@x" {
		t.Errorf("unresolved refs = %v, want [gone@x]", out.Unresolved)
	}
}

// covers: MA-144, R7, R19, S32
// The page shows the transport-header block in a collapsed panel after the
// header <dl>: labelled "unverified", HTML-escaped inside <pre> (so a forged
// header line carrying <script> and an & entity is shown as text, inert under
// the archive CSP), capped at 64 KiB with a visible truncation note, and
// omitted entirely when the block is empty.
func TestRenderTransportHeaders(t *testing.T) {
	// A hostile header line: a script tag and a bare ampersand must be escaped.
	m := &model.Message{
		Subject:          "Re: invoice",
		SenderEmail:      "alice@example.com",
		TransportHeaders: "Received: from mx by mail\r\nX-Evil: <script>alert(1)</script>\r\nX-Amp: a & b",
	}
	out, _, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	if !strings.Contains(s, `<details class="mailarchive-headers">`) {
		t.Error("transport-header panel missing")
	}
	if !strings.Contains(s, "<summary>Transport headers as stored (unverified)</summary>") {
		t.Error("panel is not labelled as unverified transport headers")
	}
	// The panel explains, on the page itself, WHY the headers are unverified.
	if !strings.Contains(s, "supplied by the sending and relaying servers and can be forged") ||
		!strings.Contains(s, "not proof of origin") {
		t.Errorf("panel lacks the forgery/provenance note:\n%s", s)
	}
	// The note sits inside the panel, after the summary and before the escaped
	// header <pre>.
	sum := strings.Index(s, "</summary>")
	note := strings.Index(s, "can be forged")
	pre := strings.Index(s, "<pre>")
	if sum < 0 || note < 0 || pre < 0 || !(sum < note && note < pre) {
		t.Errorf("forgery note not placed between the summary and the <pre> (summary=%d note=%d pre=%d)", sum, note, pre)
	}
	// The panel sits AFTER the field list, not inside it.
	if di, dl := strings.Index(s, "mailarchive-headers"), strings.Index(s, "</dl>"); di < 0 || dl < 0 || di < dl {
		t.Errorf("panel not placed after the header <dl> (details=%d dl=%d)", di, dl)
	}
	// The script tag and entity survive only in escaped form; never live.
	if strings.Contains(s, "<script>alert(1)</script>") {
		t.Error("transport headers rendered a live <script> tag")
	}
	if !strings.Contains(s, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("script line not HTML-escaped")
	}
	if !strings.Contains(s, "a &amp; b") {
		t.Error("ampersand in a header line not escaped")
	}

	// Empty block → no panel at all.
	none, _, err := Render(&model.Message{Subject: "no headers", PlainBody: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(none), "mailarchive-headers") {
		t.Error("an empty transport-header block still rendered a panel")
	}

	// Over the 64 KiB cap → truncation note, and the emitted run is bounded.
	big := &model.Message{Subject: "huge", TransportHeaders: strings.Repeat("A", 70*1024)}
	bout, _, err := Render(big)
	if err != nil {
		t.Fatal(err)
	}
	bs := string(bout)
	if !strings.Contains(bs, "(truncated)") {
		t.Error("oversized transport headers carried no truncation note")
	}
	// Without the cap this would be 70 KiB of 'A'; capped it is ~64 KiB (plus a
	// couple of stray 'A's from the template CSS, e.g. "Arial").
	if n := strings.Count(bs, "A"); n >= 66*1024 || n < 60*1024 {
		t.Errorf("transport-header block not capped near 64 KiB: %d 'A' bytes emitted", n)
	}
}

// covers: MA-177, R7, S35
// When a message carries capture-time state, the page shows one "Status" row —
// tagged data-mailarchive-field="status" for rebuild — combining the read flag,
// importance and sensitivity. Like every header value it is HTML-escaped, so an
// unexpected metacharacter in a field is inert (R7).
func TestRenderStatusRowEscaped(t *testing.T) {
	m := &model.Message{
		Subject:     "Board memo",
		Unread:      true,
		Importance:  "high",
		Sensitivity: "private<x>&y", // a hostile/unexpected value: must be escaped
	}
	out, _, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `data-mailarchive-field="status"`) {
		t.Errorf("no Status row tagged for rebuild:\n%s", s)
	}
	if !strings.Contains(s, "<dt>Status</dt>") {
		t.Error("Status row has no label")
	}
	// The row combines all three parts, joined by the middle dot, escaped.
	if !strings.Contains(s, "Unread · Importance: high · Sensitivity: private&lt;x&gt;&amp;y") {
		t.Errorf("Status line wrong or not escaped:\n%s", s)
	}
	if strings.Contains(s, "private<x>") {
		t.Error("a metacharacter in a status field rendered live (not escaped)")
	}
}

// covers: MA-180, R7, S35
// A message with no read/importance/sensitivity state shows no "Status" row at
// all — the row appears only when there is state to show.
func TestRenderStatusRowAbsentWhenUnset(t *testing.T) {
	m := &model.Message{Subject: "Ordinary", PlainBody: "hi"}
	out, _, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, `data-mailarchive-field="status"`) {
		t.Errorf("Status row present with no state set:\n%s", s)
	}
	if strings.Contains(s, "<dt>Status</dt>") {
		t.Error("Status label present with no state set")
	}
}

// covers: MA-187, R19, R7, S33
// Mail may not wear the tool's private namespace: a hostile HTML body that
// carries class="mailarchive-header" or a data-mailarchive-field attribute has
// them stripped on render, so the exported page holds exactly one real header
// marker — the tool's own — and reindex -rebuild cannot be fooled into reading
// a mail-injected element as the record's header.
func TestNeutralizeStripsReservedNamespace(t *testing.T) {
	m := &model.Message{
		Subject:     "Real Subject",
		SenderName:  "Real Person",
		SenderEmail: "real@example.com",
		HTMLBody: `<div class="mailarchive-header" data-mailarchive-field="from">` +
			`<span class="mailarchive-subject" data-mailarchive-field="subject">FORGED</span>injected</div>` +
			`<p>hello zebrafinch</p>`,
	}
	out, _, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if n := strings.Count(s, `class="mailarchive-header"`); n != 1 {
		t.Errorf(`class="mailarchive-header" appears %d times, want 1 (only the tool's own header div; the mail-injected one must be stripped)`, n)
	}
	if strings.Contains(s, `data-mailarchive-field="from">`+"injected") || strings.Contains(s, `data-mailarchive-field="subject">FORGED`) {
		t.Errorf("a mail-supplied data-mailarchive-field survived into the page:\n%s", s)
	}
	if !strings.Contains(s, "injected") || !strings.Contains(s, "zebrafinch") {
		t.Errorf("stripping the reserved namespace must keep the element's text content:\n%s", s)
	}
}

// covers: MA-193, R7, R19, S36
// Categories render in their OWN dedicated "Categories" field
// (data-mailarchive-field="categories"), never the " · "-joined Status line
// (QC3): each value is HTML-escaped and control-stripped, so a value carrying a
// <script>, the Status separator " · ", and the display separator ", " is inert
// and cannot forge a Status segment. The count and total length are capped with
// a visible truncation note (QC4), and a message with no categories shows no
// Categories row at all.
func TestRenderCategoriesField(t *testing.T) {
	// A single hostile category value that also mimics a full Status line.
	m := &model.Message{
		Subject:    "Filing",
		Categories: []string{"Unread · Importance: high, Sensitivity: confidential <script>alert(1)</script>\x07"},
	}
	out, _, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	if !strings.Contains(s, `data-mailarchive-field="categories"`) {
		t.Fatalf("no Categories field rendered:\n%s", s)
	}
	if !strings.Contains(s, "<dt>Categories</dt>") {
		t.Error("Categories row has no label")
	}
	// The value is escaped and the control char stripped; the live script never
	// survives.
	if strings.Contains(s, "<script>alert(1)</script>") {
		t.Error("a category rendered a live <script> tag")
	}
	if !strings.Contains(s, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("category not HTML-escaped:\n%s", s)
	}
	if strings.Contains(s, "\x07") {
		t.Error("control character not stripped from a category")
	}
	// The category text lives in its OWN field, never inside a Status dd — there
	// is no Status row here, and the " · " in the value must not create one.
	if strings.Contains(s, `data-mailarchive-field="status"`) {
		t.Errorf("a category forged a Status row (QC3):\n%s", s)
	}
	// The value rides inside a category span within the categories dd.
	if !strings.Contains(s, `<span class="mailarchive-category">`) {
		t.Errorf("category value not wrapped in its own span:\n%s", s)
	}

	// No categories → no Categories row at all.
	none, _, err := Render(&model.Message{Subject: "plain", PlainBody: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(none), `data-mailarchive-field="categories"`) {
		t.Errorf("a message with no categories still rendered a Categories field:\n%s", string(none))
	}
	// Whitespace/empty-only categories are dropped, so no row appears.
	blank, _, err := Render(&model.Message{Subject: "blank", Categories: []string{"  ", "\x07"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blank), `data-mailarchive-field="categories"`) {
		t.Error("blank/control-only categories still rendered a Categories field")
	}

	// QC4: a pathological set is capped in number with a visible truncation note.
	many := make([]string, 500)
	for i := range many {
		many[i] = "tag" + strconv.Itoa(i)
	}
	big, _, err := Render(&model.Message{Subject: "many", Categories: many})
	if err != nil {
		t.Fatal(err)
	}
	bs := string(big)
	if !strings.Contains(bs, "(truncated)") {
		t.Error("an oversized category set carried no truncation note (QC4)")
	}
	if n := strings.Count(bs, `class="mailarchive-category"`); n > 64 {
		t.Errorf("category count not capped: %d spans rendered, want <= 64", n)
	}
}
