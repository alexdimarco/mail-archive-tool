package state

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// covers: R27, MA-347
// Record.ParseScheme is additive and omitted when zero, so an older binary's records
// and this build's unstamped ones are byte-identical; StampParseScheme only ever
// raises it (a record re-read by this build, MC12), and does nothing for an absent key.
func TestStampParseSchemeOnlyRaises(t *testing.T) {
	m := newManifest("")
	m.Add("k", Record{Path: "a.html"})
	raw, err := json.Marshal(m.Entries["k"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "parse_scheme") {
		t.Errorf("an unstamped record serialises parse_scheme: %s", raw)
	}

	if !m.StampParseScheme("k", ParseSchemeCurrent) {
		t.Fatal("stamping an unstamped record reported no change")
	}
	if r, _ := m.Get("k"); r.ParseScheme != ParseSchemeCurrent {
		t.Errorf("ParseScheme = %d, want %d", r.ParseScheme, ParseSchemeCurrent)
	}
	raw, _ = json.Marshal(m.Entries["k"])
	if !strings.Contains(string(raw), fmt.Sprintf(`"parse_scheme":%d`, ParseSchemeCurrent)) {
		t.Errorf("a stamped record does not serialise parse_scheme: %s", raw)
	}
	if m.StampParseScheme("k", ParseSchemeCurrent) {
		t.Error("stamping an already-stamped record reported a change")
	}

	m.Add("later", Record{Path: "b.html", ParseScheme: ParseSchemeCurrent + 3})
	if m.StampParseScheme("later", ParseSchemeCurrent) {
		t.Error("stamping lowered a later scheme's record")
	}
	if r, _ := m.Get("later"); r.ParseScheme != ParseSchemeCurrent+3 {
		t.Errorf("a later scheme's stamp became %d", r.ParseScheme)
	}
	if m.StampParseScheme("absent", ParseSchemeCurrent) {
		t.Error("stamping an absent key reported a change")
	}
}
