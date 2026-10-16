package source

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-349, R10, R26
// The rules of the check "fidelity contains v1" (design-mime-fidelity §4.2 step 3a, MC4),
// each on its own, so none can be dropped unseen: every v1 attachment with bytes needs an
// attachment with identical bytes (a text attachment v1 converted must convert, through
// v1's own table, to v1's bytes); every non-empty v1 body field needs a non-empty one; a
// v1 body that is valid UTF-8 and was not read differently by design must be contained
// verbatim. TestHostileMIMEIsBoundedAndNeverYieldsAStub injects one failure of the first
// rule through the whole routing; this pins the others, with a positive twin for each.
func TestFidelityContainsV1CheckRules(t *testing.T) {
	att := func(name string, data string) model.Attachment {
		return model.Attachment{Filename: name, WriteTo: bytesWriterTo([]byte(data))}
	}
	v1 := &model.Message{
		PlainBody:   "hello wörld",
		HTMLBody:    "<p>hello wörld</p>",
		Attachments: []model.Attachment{att("a.bin", "AAAA"), att("b.csv", "Müller;Köln")}, // v1 converted b.csv from Latin-1
	}
	inv := v1InventoryOf(v1)
	whole := func() *model.Message {
		return &model.Message{
			PlainBody:   "an intro\n\nhello wörld",
			HTMLBody:    "<div><p>hello wörld</p></div>",
			Attachments: []model.Attachment{att("a.bin", "AAAA"), att("b.csv", "M\xfcller;K\xf6ln")},
		}
	}
	declared := walkMeta{declared: map[int]string{1: "iso-8859-1"}}

	if got := holdsV1(v1, inv, whole(), declared); got != "" {
		t.Fatalf("a capture that holds all of v1 fails the check: %q", got)
	}

	for _, c := range []struct {
		name   string
		mutate func(m *model.Message, meta *walkMeta)
		want   string // what the failure names; "" is a pass
	}{
		{"an attachment is missing", func(m *model.Message, _ *walkMeta) { m.Attachments = m.Attachments[1:] }, "a.bin"},
		{"an attachment's bytes differ", func(m *model.Message, _ *walkMeta) { m.Attachments[0] = att("a.bin", "AAAB") }, "a.bin"},
		{"a converted text attachment has no declared charset to convert from", func(_ *model.Message, meta *walkMeta) { meta.declared = nil }, "b.csv"},
		{"a converted text attachment declares another charset", func(_ *model.Message, meta *walkMeta) { meta.declared = map[int]string{1: "windows-1251"} }, "b.csv"},
		{"the plain body is empty", func(m *model.Message, _ *walkMeta) { m.PlainBody = " \n" }, "plain body is empty"},
		{"the HTML body is empty", func(m *model.Message, _ *walkMeta) { m.HTMLBody = "" }, "HTML body is empty"},
		{"the plain body does not hold v1's text", func(m *model.Message, _ *walkMeta) { m.PlainBody = "something else" }, "plain body does not hold"},
		{"the HTML body does not hold v1's text", func(m *model.Message, _ *walkMeta) { m.HTMLBody = "<p>something else</p>" }, "HTML body does not hold"},
		// Positive twins: what the check lets through.
		{"a body read differently by design need not hold v1's text", func(m *model.Message, _ *walkMeta) {
			m.PlainBody = "something else"
			m.ParseNotes = []model.ParseNote{newNote(noteLegacy8bit, "d")}
		}, ""},
		{"a gap that decoded a body is a body-decoding note", func(m *model.Message, _ *walkMeta) {
			m.PlainBody = "something else"
			m.ParseNotes = []model.ParseNote{newNote("corrupt-part", "d")}
		}, ""},
		{"a note that is not about a body changes nothing", func(m *model.Message, _ *walkMeta) {
			m.PlainBody = "something else"
			m.ParseNotes = []model.ParseNote{newNote(noteLenientParams, "d")}
		}, "plain body does not hold"},
		{"an attachment more than v1 held is fine", func(m *model.Message, _ *walkMeta) { m.Attachments = append(m.Attachments, att("c.pdf", "x")) }, ""},
	} {
		m, meta := whole(), declared
		c.mutate(m, &meta)
		got := holdsV1(v1, inv, m, meta)
		switch {
		case c.want == "" && got != "":
			t.Errorf("%s: the check fails (%q), want a pass", c.name, got)
		case c.want != "" && !strings.Contains(got, c.want):
			t.Errorf("%s: the check says %q, want a failure naming %q", c.name, got, c.want)
		}
	}

	// A v1 body that is not valid UTF-8 has no verbatim rule (its text is read as
	// Windows-1252 now); an empty v1 body needs nothing; a v1 attachment with no bytes
	// needs nothing.
	legacy := &model.Message{PlainBody: "caf\xe9", Attachments: []model.Attachment{{Filename: "empty.txt"}}}
	if got := holdsV1(legacy, v1InventoryOf(legacy), &model.Message{PlainBody: "café"}, walkMeta{}); got != "" {
		t.Errorf("a v1 body that was not UTF-8 fails the check: %q", got)
	}
	if got := holdsV1(&model.Message{}, nil, &model.Message{PlainBody: "new text"}, walkMeta{}); got != "" {
		t.Errorf("a v1 message with nothing fails the check: %q", got)
	}
}
