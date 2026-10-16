package model

import (
	"strings"
	"testing"
)

// covers: MA-348, R1, R26
// MS2-AO4. An attachment's label is where a sender's file name becomes a record label, so
// its control characters are stripped there: no label begins with the NUL that marks a
// parse-gap label, and none carries a tab or a line break into a report row. A name with
// nothing but controls in it is no name, so the label falls back as an empty name's does.
func TestAnAttachmentLabelHasNoControlCharacters(t *testing.T) {
	for _, c := range []struct {
		a    Attachment
		want string
	}{
		{Attachment{Filename: "report.pdf"}, "report.pdf"},
		{Attachment{Filename: "\x00corrupt-part\x00forged"}, "corrupt-partforged"},
		{Attachment{Filename: "a\tb\r\nc\x7f.txt"}, "abc.txt"},
		{Attachment{Filename: "\x00\x01", ContentID: "img1"}, "cid:img1"},
		{Attachment{Filename: "\x00", ContentID: "\x00"}, "attachment-3"},
		{Attachment{ContentID: "x\ny"}, "cid:xy"},
		{Attachment{}, "attachment-3"},
	} {
		got := c.a.Label(3)
		if got != c.want {
			t.Errorf("Label of %q / %q = %q, want %q", c.a.Filename, c.a.ContentID, got, c.want)
		}
		if strings.ContainsAny(got, "\x00\t\r\n\x7f") {
			t.Errorf("Label of %q keeps a control character: %q", c.a.Filename, got)
		}
	}
}
