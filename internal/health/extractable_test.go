package health

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// covers: MA-196, R18, R20, S29, S34
// The extractability line is source-aware and display-only: with N<M it reports
// "N of M ... on disk" and names BOTH remedies (-raw for a raw-capable source,
// keep the .pst for Outlook, which never carries an original) — never a
// categorical "re-archive" verdict; with N==M it is a bare line with no
// follow-up; it is omitted when the manifest has no records; status -json
// carries an extractable object with records_with_eml + records; and it never
// changes the posture.
func TestExtractableStatusLineAndPosture(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	base := func(withEML, total int) Input {
		in := healthyInput(now)
		in.Messages = total
		in.WithFixity = total
		in.Extractable = Extractable{WithEML: withEML, Total: total}
		return in
	}

	// N=0 of M (a PST-only archive has no preserved originals): the "have none"
	// case names both remedies and is not a re-archive verdict.
	none := strings.Join(Summary(base(0, 5), Assess(base(0, 5), now)), "\n")
	if !strings.Contains(none, "Extractable: 0 of 5 records have a preserved original (.eml) on disk") {
		t.Errorf("N=0 status line missing or wrong:\n%s", none)
	}
	if !strings.Contains(none, "-raw") || !strings.Contains(none, "keep the .pst") {
		t.Errorf("N<M wording must name both remedies (-raw and keep the .pst):\n%s", none)
	}
	if strings.Contains(none, "re-archive") {
		t.Errorf("status must never print a categorical re-archive verdict:\n%s", none)
	}

	// N=M: a bare line, no "have none" follow-up.
	all := strings.Join(Summary(base(5, 5), Assess(base(5, 5), now)), "\n")
	if !strings.Contains(all, "Extractable: 5 of 5 records have a preserved original (.eml) on disk") {
		t.Errorf("N=M status line missing or wrong:\n%s", all)
	}
	if strings.Contains(all, "keep the .pst") {
		t.Errorf("N=M must not carry the have-none follow-up:\n%s", all)
	}

	// Omitted when the manifest has no records.
	empty := healthyInput(now)
	empty.Messages = 0
	empty.Extractable = Extractable{WithEML: 0, Total: 0}
	if l := strings.Join(Summary(empty, Assess(empty, now)), "\n"); strings.Contains(l, "Extractable:") {
		t.Errorf("the extractability line must be omitted when there are no records:\n%s", l)
	}

	// status -json carries the extractable object with both keys and values.
	doc := JSON(base(2, 5), Assess(base(2, 5), now))
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	ex, ok := m["extractable"].(map[string]any)
	if !ok {
		t.Fatalf("status -json lacks an extractable object:\n%s", data)
	}
	for _, k := range []string{"records", "records_with_eml"} {
		if _, ok := ex[k]; !ok {
			t.Errorf("extractable missing key %q:\n%s", k, data)
		}
	}
	if doc.Extractable.RecordsWithEML != 2 || doc.Extractable.Records != 5 {
		t.Errorf("extractable = {records_with_eml:%d, records:%d}, want {2,5}", doc.Extractable.RecordsWithEML, doc.Extractable.Records)
	}

	// extractable is null when there is no manifest (parallel to fixity).
	noMan := healthyInput(now)
	noMan.HasManifest = false
	nj, _ := json.Marshal(JSON(noMan, Assess(noMan, now)))
	var nm map[string]any
	json.Unmarshal(nj, &nm)
	if v, ok := nm["extractable"]; !ok || v != nil {
		t.Errorf("extractable should be present and null when no manifest: %v\n%s", v, nj)
	}

	// Posture is UNCHANGED by the extractability count: 0-of-M and M-of-M judge
	// the same (the line is informational, never a WARN/RED).
	if p0, pM := Assess(base(0, 5), now).Posture, Assess(base(5, 5), now).Posture; p0 != pM {
		t.Errorf("extractability changed the posture: 0-of-M=%s vs M-of-M=%s", p0, pM)
	}
}
