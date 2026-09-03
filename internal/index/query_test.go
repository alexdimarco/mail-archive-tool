package index

import "testing"

// covers: MA-162, R8
// A token value may be quoted so it can carry spaces: folder:"Sent Messages" and
// from:'a b' each parse to one field value instead of a token plus a silently
// dropped free term (the friction-6 defect). An unterminated quote runs to the
// end of the input, an unquoted single-word value is unchanged, and — since the
// terminal `search` verb and the serve box both call ParseQuery — this is one
// grammar for both surfaces.
func TestParseQueryQuotedTokenValue(t *testing.T) {
	// A double-quoted folder value keeps its space and the trailing free term.
	q := ParseQuery(`folder:"Sent Messages" invoice`, Query{})
	if q.Folder != "Sent Messages" {
		t.Errorf("folder value = %q, want %q", q.Folder, "Sent Messages")
	}
	if q.Text != "invoice" {
		t.Errorf("free text = %q, want %q", q.Text, "invoice")
	}

	// Single quotes work too, for the sender.
	q = ParseQuery(`from:'a b' report`, Query{})
	if q.Sender != "a b" {
		t.Errorf("sender value = %q, want %q", q.Sender, "a b")
	}
	if q.Text != "report" {
		t.Errorf("free text = %q, want %q", q.Text, "report")
	}

	// An unterminated quote runs to the end of the input.
	q = ParseQuery(`folder:"Sent Messages`, Query{})
	if q.Folder != "Sent Messages" {
		t.Errorf("unterminated-quote folder = %q, want %q", q.Folder, "Sent Messages")
	}
	if q.Text != "" {
		t.Errorf("unterminated-quote free text = %q, want empty", q.Text)
	}

	// Regression: an unquoted single-word value is parsed exactly as before.
	q = ParseQuery(`from:bob invoice`, Query{})
	if q.Sender != "bob" || q.Text != "invoice" {
		t.Errorf("plain token drifted: sender=%q text=%q", q.Sender, q.Text)
	}

	// The defect it fixes: without quoting the value ends at the first space, so
	// the trailing word becomes free text rather than part of the folder.
	q = ParseQuery(`folder:Sent Messages`, Query{})
	if q.Folder != "Sent" || q.Text != "Messages" {
		t.Errorf("unquoted space value = folder %q text %q, want folder \"Sent\" text \"Messages\"", q.Folder, q.Text)
	}
}
