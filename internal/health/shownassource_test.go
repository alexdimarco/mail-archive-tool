package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

func shownInput(now time.Time) Input {
	in := healthyInput(now)
	in.ShownAsSource = map[string]int{"too-deeply-nested": 2, "unparseable": 1}
	return in
}

// covers: MA-494, R1, R7, R18
// Gather carries the manifest's per-reason count on Input; a manifest with no
// fallback records reads as none.
func TestGatherCarriesShownAsSource(t *testing.T) {
	out := t.TempDir()
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	m.Add("a", state.Record{Path: "a.html", Folder: "F"})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if in := Gather(out, ""); len(in.ShownAsSource) != 0 {
		t.Fatalf("no fallback records: %v, want none", in.ShownAsSource)
	}
	m.Add("b", state.Record{Path: "b.html", Folder: "F", BodyFallback: "too-large"})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if in := Gather(out, ""); in.ShownAsSource["too-large"] != 1 {
		t.Errorf("Gather ShownAsSource = %v, want too-large:1", in.ShownAsSource)
	}
}

// covers: MA-495, R18, R1
// Shown-as-source is worth knowing, not wrong: one note with the stable code
// shown_as_source, no WARN/RED prefix, and the posture is whatever it was — a
// healthy backup stays GREEN, a WARN or RED one keeps its own verdict.
func TestShownAsSourceIsANoteNotAWarn(t *testing.T) {
	t.Skip("open (RV-10): Assess does not mention messages shown as source yet")
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

	in := shownInput(now)
	rep := Assess(in, now)
	if rep.Posture != "GREEN" {
		t.Fatalf("healthy backup with messages shown as source: posture %s, want GREEN (%v)", rep.Posture, rep.Reasons)
	}
	idx := -1
	for i, c := range rep.Codes {
		if c == "shown_as_source" {
			idx = i
		}
	}
	if idx < 0 || len(rep.Codes) != len(rep.Reasons) {
		t.Fatalf("want one shown_as_source code parallel to reasons: codes %v reasons %v", rep.Codes, rep.Reasons)
	}
	if strings.HasPrefix(rep.Reasons[idx], "WARN") || strings.HasPrefix(rep.Reasons[idx], "RED") {
		t.Errorf("the note carries a posture prefix: %q", rep.Reasons[idx])
	}

	if rep := Assess(healthyInput(now), now); len(rep.Codes) != 0 {
		t.Errorf("positive twin: none shown as source but codes = %v", rep.Codes)
	}

	in = shownInput(now)
	in.LastRun.Status = state.RunFailed
	in.LastRun.Error = "boom"
	if rep := Assess(in, now); rep.Posture != "RED" {
		t.Errorf("a failed run with messages shown as source: posture %s, want RED", rep.Posture)
	}
	in = shownInput(now)
	in.Fillable = 3
	if rep := Assess(in, now); rep.Posture != "WARN" {
		t.Errorf("missing content with messages shown as source: posture %s, want WARN", rep.Posture)
	}
}

// covers: MA-497, R18, R1
// The status line says what happened, why in plain words, and that there is
// nothing to do; it is absent when nothing is shown as source.
func TestStatusSaysMessagesAreShownAsSourceInPlainWords(t *testing.T) {
	t.Skip("open (RV-10): Summary has no Shown as source line yet")
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	in := shownInput(now)
	lines := Summary(in, Assess(in, now))
	text := strings.Join(lines, "\n")
	var line string
	for _, l := range lines {
		if strings.HasPrefix(l, "Shown as source:") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no Shown as source line:\n%s", text)
	}
	for _, want := range []string{"3 message(s)", "plain text", "nested too deeply", "could not be read", "(too-deeply-nested)", "the original is kept", "Nothing to do"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q: %s", want, line)
		}
	}
	if !strings.Contains(text, "Incomplete: none") {
		t.Errorf("a shown-as-source page is complete, want Incomplete: none:\n%s", text)
	}
	healthy := healthyInput(now)
	if strings.Contains(strings.Join(Summary(healthy, Assess(healthy, now)), "\n"), "Shown as source") {
		t.Error("positive twin: the line appears with none shown as source")
	}
}

// covers: MA-499, R18, R1
// The README lists the reason code, the JSON key and the report kind with every
// reason (RV-10).
func TestReadmeNamesShownAsSource(t *testing.T) {
	t.Skip("open (RV-10): the README does not list shown_as_source yet")
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)
	for _, want := range []string{"`shown_as_source`", "`html-shown-as-source`", "`unsafe-after-reparse`", "`unparseable`", "`too-deeply-nested`", "`too-large`"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README lacks %s", want)
		}
	}
	if strings.Count(readme, "`shown_as_source`") < 2 {
		t.Error("README names shown_as_source once: want the reason code AND the JSON key")
	}
}

// covers: MA-498, R18
// status -json: additive shown_as_source key, omitted when none, version stays 2.
func TestStatusJSONCarriesShownAsSource(t *testing.T) {
	t.Skip("open (RV-10): the JSON report has no shown_as_source key yet")
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	in := shownInput(now)
	b, err := json.Marshal(JSON(in, Assess(in, now)))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	sas, _ := got["shown_as_source"].(map[string]any)
	reasons, _ := sas["reasons"].(map[string]any)
	if sas["count"] != float64(3) || reasons["too-deeply-nested"] != float64(2) || reasons["unparseable"] != float64(1) {
		t.Errorf("shown_as_source = %v, want count 3 with reasons", got["shown_as_source"])
	}
	if got["version"] != float64(2) {
		t.Errorf("version = %v, want 2 (additive key)", got["version"])
	}
	healthy := healthyInput(now)
	b, _ = json.Marshal(JSON(healthy, Assess(healthy, now)))
	var h map[string]any
	_ = json.Unmarshal(b, &h)
	if _, present := h["shown_as_source"]; present {
		t.Error("positive twin: the key is present with none shown as source")
	}
}
