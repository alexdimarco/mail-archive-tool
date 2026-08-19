package source

import "testing"

// covers: MA-12
func TestDecodeBytes(t *testing.T) {
	// Valid UTF-8 passes through unchanged (the modern PidTagHtml case).
	if got := decodeBytes([]byte("<p>héllo · 世界</p>")); got != "<p>héllo · 世界</p>" {
		t.Errorf("utf-8 passthrough failed: %q", got)
	}

	// Windows-1252 smart quotes (0x93/0x94) are not valid UTF-8 and must be
	// decoded to their Unicode equivalents.
	got := decodeBytes([]byte{0x93, 'h', 'i', 0x94})
	if got != "“hi”" {
		t.Errorf("windows-1252 decode = %q, want %q", got, "“hi”")
	}

	if decodeBytes(nil) != "" {
		t.Error("nil input should decode to empty string")
	}
}
