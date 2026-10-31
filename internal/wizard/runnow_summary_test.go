package wizard

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

// covers: MA-943
// The Desktop's Run now result (owner report from the Windows lab of 2026-10-31: the note
// under Run now was a wall of text — the wizard's whole export summary, with "Newly
// downloaded this run: 0" beside "Exported: 20351", "Your data stays on this computer",
// the output folder and the command-line search advice). runNowSummary leads with one
// plain line — the backup finished and how many new messages it archived (the archive's
// growth, ManifestSize less ManifestBefore, as Archive now and the command line count it,
// F13) — then says only what explains that count when it is not zero (messages an
// earlier backup could not fully download, now complete: the wizard's "Newly downloaded
// this run", Stats.Filled; messages already there saved again) and what a person must act
// on (not fully downloaded, cut short, the search index). It never carries the command
// line's search advice, the output folder or the zero counts. The wizard's own dialog
// (exportSummary) is unchanged: positive twin first. Amended 2026-10-31 (friction review L6):
// "Which ones:" names the report file and where it is (writeReport keeps it in the archive's
// folder), not a full path the person cannot click.
func TestRunNowSummary(t *testing.T) {
	plain := app.Result{Stats: export.Stats{Exported: 135, SkippedManifest: 20351}, ManifestBefore: 20351, ManifestSize: 20486}
	if got := exportSummary("/archive", false, plain); !strings.Contains(got, "Newly downloaded this run: 0") || !strings.Contains(got, "mailarchive serve -out") {
		t.Fatalf("the positive twin: the wizard's own summary changed:\n%s", got)
	}

	for name, c := range map[string]struct {
		r    app.Result
		want string
	}{
		"new mail": {plain, "Backup finished: 135 new messages archived."},
		"one":      {app.Result{Stats: export.Stats{Exported: 1}, ManifestBefore: 4, ManifestSize: 5}, "Backup finished: 1 new message archived."},
		"none":     {app.Result{Stats: export.Stats{SkippedManifest: 9}, ManifestBefore: 9, ManifestSize: 9}, "Backup finished: no new messages."},
		"thousands": {app.Result{Stats: export.Stats{Exported: 20351}, ManifestBefore: 0, ManifestSize: 20351},
			"Backup finished: 20,351 new messages archived."},
		"completed and saved again": {app.Result{Stats: export.Stats{Exported: 10, Filled: 3}, ManifestBefore: 100, ManifestSize: 105},
			"Backup finished: 5 new messages archived.\n3 messages an earlier backup could not fully download are now complete.\n2 messages already in the archive were saved again (no copies are made)."},
		"not downloaded": {app.Result{Stats: export.Stats{Exported: 2}, ManifestBefore: 1, ManifestSize: 3, Fillable: 4, Unknown: 1, ReportPath: "/a/attachments-report.tsv"},
			"Backup finished: 2 new messages archived.\n5 messages in the archive are not fully downloaded yet: open them in your mail app (such as Thunderbird) so it downloads them, then click Run now again. 1 of them is from an older archive and is checked again by each backup.\nWhich ones: the file attachments-report.tsv in the archive's folder (it opens in a spreadsheet program)."},
		"cut short only": {app.Result{Fillable: 2, FillableParseGap: 2, FillableOnlyParseGap: 2, ReportPath: "/a/r.tsv"},
			"Backup finished: no new messages.\n2 messages are cut short in the mail files, or have parts that could not be told apart, so they could not be read in full. To add them, replace that mail file with a complete export from your mail app, then click Run now again. A message that was already damaged when it was sent stays listed.\nWhich ones: the file r.tsv in the archive's folder (it opens in a spreadsheet program)."},
		"some cut short": {app.Result{Fillable: 3, FillableParseGap: 1, ReportPath: "/a/r.tsv"},
			"Backup finished: no new messages.\n3 messages in the archive are not fully downloaded yet: open them in your mail app (such as Thunderbird) so it downloads them, then click Run now again.\n1 of them is cut short in the mail file: replace that file with a complete export from your mail app.\nWhich ones: the file r.tsv in the archive's folder (it opens in a spreadsheet program)."},
		"search": {app.Result{Stats: export.Stats{Exported: 3}, ManifestBefore: 0, ManifestSize: 3, IndexErrors: 2, IndexBehind: true},
			"Backup finished: 3 new messages archived.\n2 messages could not be added to search: click Maintenance → Rebuild search index.\nThe search index is missing some messages: click Maintenance → Rebuild search index, or the next backup brings it up to date."},
	} {
		got := runNowSummary(c.r)
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, c.want)
		}
		for _, gone := range []string{"Newly downloaded this run", "Exported:", "Your data stays on this computer", "Output folder", "mailarchive serve", "index.html", ": 0"} {
			if strings.Contains(got, gone) {
				t.Errorf("%s: the Run now result still carries %q", name, gone)
			}
		}
	}
}
