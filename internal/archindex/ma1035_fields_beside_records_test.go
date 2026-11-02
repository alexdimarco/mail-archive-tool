package archindex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// withTopLevel is the manifest body with its top-level field name set to raw (the field's other
// spellings removed first, so the one given is the only one). raw "" removes the field.
func withTopLevel(t *testing.T, body []byte, name, raw string) []byte {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("the fixture is not a JSON object: %v", err)
	}
	for k := range doc {
		if equalFoldASCII(k, name) {
			delete(doc, k)
		}
	}
	if raw != "" {
		doc[name] = json.RawMessage(raw)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("the fixture's field %s %s: %v", name, raw, err)
	}
	return out
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// covers: MA-1035, MA-1034
// The two fields a manifest holds beside its records, "capture_gaps" and "collapsed_tokens", are
// checked as a whole LoadFrom types them (MA-1035 amended 2026-11-02, the bounded walk's second
// adversarial pass, its one finding): a manifest whose capture_gaps is a number, an array, a
// string or true, or holds a gap that is not an object, or whose collapsed_tokens is not an object
// of true or false, is refused by every reader that reads it whole (status, a backup), so it is
// not an available archive and is not offered Switch, nor listed by Open an archive folder… or
// add-archive without the "cannot be read" words. Before, the walk skipped both fields unchecked.
// Positive twins first: a real archive the state package wrote with a capture gap and a collapse
// mark, and the same archive with each field null, empty, holding a null, false, or named in
// other letter case, each recognised with the kind LoadFrom gives.
func TestTheFieldsBesideTheRecordsAreCheckedAsLoadFromTypesThem(t *testing.T) {
	real := filepath.Join(t.TempDir(), "Real")
	realArchive(t, real, 20, localSource)
	man := filepath.Join(real, manifestName)
	m, err := state.Load(man)
	if err != nil {
		t.Fatal(err)
	}
	var tok string
	for _, v := range m.Stores {
		tok = v
	}
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "file:Inbox/x",
		Reason: "file-unreadable", Class: state.ClassFillable, Subject: "Lost"}, time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC))
	m.MarkCollapsed(tok)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil || len(doc["capture_gaps"]) < 3 || len(doc["collapsed_tokens"]) < 3 {
		t.Fatalf("the fixture: the real manifest must hold a capture gap and a collapse mark (%v): %s", err, body)
	}
	if k := assure.Reached(t, Kind(real), "the real archive's kind"); k != "local" || !Available(Entry{Path: real}) {
		t.Fatalf("the positive twin: the real archive reads %q, available %v", k, Available(Entry{Path: real}))
	}

	check := func(what string, b []byte, wantOK bool) {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "A")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, manifestName), b, 0o644); err != nil {
			t.Fatal(err)
		}
		wantKind, oracleOK := loadFromKind(t, dir)
		if oracleOK != wantOK {
			t.Fatalf("%s: the oracle: a whole LoadFrom reads it %v, the case expects %v: %s", what, oracleOK, wantOK, b)
		}
		gotKind, gotOK := inspect(dir)
		if gotOK != wantOK || gotKind != wantKind {
			t.Errorf("%s: recognition reads (%q, %v); a whole LoadFrom reads (%q, %v)", what, gotKind, gotOK, wantKind, oracleOK)
		}
		if wantOK != Available(Entry{Path: dir}) || wantOK != IsArchive(dir) {
			t.Errorf("%s: available %v, an archive %v; want %v", what, Available(Entry{Path: dir}), IsArchive(dir), wantOK)
		}
	}

	// Positive twins: well-typed fields agree and are recognised.
	for _, c := range []struct{ name, raw string }{
		{"capture_gaps", `null`}, {"capture_gaps", `{}`}, {"capture_gaps", `{"k":null}`},
		{"collapsed_tokens", `null`}, {"collapsed_tokens", `{}`}, {"collapsed_tokens", `{"a":false,"b":null,"c":true}`},
		{"Capture_Gaps", `{"k":{}}`}, {"COLLAPSED_TOKENS", `{"a":true}`},
	} {
		check("the positive twin "+c.name+" "+c.raw, withTopLevel(t, body, c.name, c.raw), true)
	}
	check("the real manifest as written", body, true)
	check("a repeated well-typed field", []byte(`{"version":6,"stores":{"path:/m":"a"},"collapsed_tokens":{"a":true},"collapsed_tokens":null,"capture_gaps":null,"capture_gaps":{}}`), true)

	// The finding's shapes, and their neighbours: each refused by LoadFrom, so by recognition.
	for _, c := range []struct{ name, raw string }{
		{"capture_gaps", `5`}, {"capture_gaps", `[1,2]`}, {"capture_gaps", `"gaps"`}, {"capture_gaps", `true`},
		{"capture_gaps", `{"k":5}`}, {"capture_gaps", `{"k":"gap"}`}, {"capture_gaps", `{"k":[]}`},
		{"collapsed_tokens", `"yes"`}, {"collapsed_tokens", `1`}, {"collapsed_tokens", `[true]`}, {"collapsed_tokens", `true`},
		{"collapsed_tokens", `{"a":"x"}`}, {"collapsed_tokens", `{"a":1}`}, {"collapsed_tokens", `{"a":{}}`}, {"collapsed_tokens", `{"a":[]}`},
		{"Capture_Gaps", `5`}, {"Collapsed_Tokens", `"yes"`},
	} {
		check(c.name+" "+c.raw, withTopLevel(t, body, c.name, c.raw), false)
	}
	check("a well-typed field then an ill-typed one", []byte(`{"version":6,"stores":{"path:/m":"a"},"collapsed_tokens":{"a":true},"collapsed_tokens":{"a":"x"}}`), false)
	check("an ill-typed field then a well-typed one", []byte(`{"version":6,"stores":{"path:/m":"a"},"capture_gaps":5,"capture_gaps":{}}`), false)
}
