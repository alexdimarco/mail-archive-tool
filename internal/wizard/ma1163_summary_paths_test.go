package wizard

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// covers: MA-1163, R18
// The wizard's summary dialog names a Windows archive folder in its search command as it is typed,
// in double quotes with each backslash single (the polish-161 friction walk: Go's quoted form,
// every backslash doubled), a drive root keeping its closing quote; the folder on its own line is
// unchanged. Positive twin first: a POSIX folder is named as before.
func TestTheWizardSummaryNamesAWindowsFolderAsItIsTyped(t *testing.T) {
	r := app.Result{Stats: export.Stats{Exported: 3}, ManifestSize: 3}
	if got := exportSummaryFor("/Users/a/Mail Archive", false, r, "darwin"); !strings.HasSuffix(got, `mailarchive serve -out "/Users/a/Mail Archive"`) {
		t.Fatalf("the positive twin: %s", got)
	}
	folder := `C:\Users\Public\polishlab\Archives\Polish Archive`
	got := exportSummaryFor(folder, false, r, "windows")
	if !strings.HasSuffix(got, `mailarchive serve -out "`+folder+`"`) || !strings.Contains(got, "Output folder:\n"+folder+"\n") {
		t.Errorf("the wizard's summary:\n%s", got)
	}
	if root := exportSummaryFor(`E:\`, false, r, "windows"); !strings.HasSuffix(root, `mailarchive serve -out "E:\\"`) {
		t.Errorf("a drive root loses its closing quote:\n%s", root)
	}
	assure.Reached(t, !strings.Contains(got, `\\Users`), "no doubled backslash in the wizard's command:\n"+got)
}
