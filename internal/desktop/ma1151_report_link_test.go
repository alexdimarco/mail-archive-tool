package desktop

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1151, R18, S29
// The page script shows the report address as a real link wherever it says it — Archive now's
// result, Run now's note, Status & health — to that fixed address alone, opening apart from the
// page, made through DOM properties; any other address and markup in the words stay words.
// Positive twin first (in the scenario): words with no report address hold no link. Run under
// Node (testdata/pagescript/harness.js, reportLink).
func TestTheReportAddressIsALinkOnThePage(t *testing.T) {
	if passed := runPageScenario(t, "reportLink"); len(passed) != 4 {
		t.Errorf("the reportLink scenario passed %d of its 4 checks: %v", len(passed), passed)
	}
	// The helper builds the link with properties, never markup.
	i := strings.Index(dashboardJS, "function sayText(")
	if i < 0 {
		t.Fatal("no sayText in the page script")
	}
	helper := dashboardJS[i:]
	helper = helper[:strings.Index(helper, "\n}\n")]
	for _, never := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "setAttribute"} {
		if strings.Contains(helper, never) {
			t.Errorf("sayText uses %s:\n%s", never, helper)
		}
	}
	assure.Reached(t, strings.Contains(dashboardJS, "const reportAddress='"+state.ReportAddress+"';"), "the script's address is the fixed constant")
}

// covers: MA-1151, R18, S29
// Recent backups shows the report address in a partial run's row as a real link to that fixed
// address (rel="noopener noreferrer", opening apart from the page); an older entry's text, which
// anyone who can write the archive folder can write, never becomes a link to anything else, and
// markup in it stays words. Positive twin first: a row with no report address holds no link.
func TestRecentBackupsLinksTheReportAddress(t *testing.T) {
	out := t.TempDir()
	writeManifest(t, out)
	at := time.Date(2026, 11, 7, 17, 4, 0, 0, time.Local)
	p := &state.PartialCounts{Mistake: state.PartialKind{Messages: 2}}
	for _, e := range []state.RunHistoryEntry{
		{When: at.Add(-2 * time.Hour), Status: state.RunFailed, Error: `see <a href="https://evil.example/">https://evil.example/</a> now`},
		{When: at.Add(-time.Hour), Status: state.RunFailed, Error: "the archive drive is full"},
		{When: at, Status: state.RunFailed, Error: state.PartialText(*p, true), Partial: p},
	} {
		if err := state.AppendRunHistory(out, e); err != nil {
			t.Fatal(err)
		}
	}
	rows := recentRunRows(out)
	if len(rows) != 3 || len(rows[1].DetailParts()) != 1 || rows[1].DetailParts()[0].Link {
		t.Fatalf("the positive twin: a row with no report address: %+v", rows)
	}
	cfg := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), ConfigPath: filepath.Join(t.TempDir(), "none.json"), Out: out}
	page := cfg.overview("csrf-token")
	link := `<a href="` + state.ReportAddress + `" rel="noopener noreferrer" target="_blank">` + state.ReportAddress + `</a>`
	if strings.Count(page, link) != 1 {
		t.Errorf("the partial run's row does not link the report address once:\n%s", page[strings.Index(page, `id="sec-history"`):][:2000])
	}
	assure.Reached(t, !strings.Contains(page, `href="https://evil.example/"`) && strings.Contains(page, "see &lt;a href=&#34;https://evil.example/&#34;&gt;"),
		"an older entry's address and markup stay words")
}
