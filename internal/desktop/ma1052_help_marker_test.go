package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1052, R36
// The guide (How to use MailArchive) says what slice 4 built for a delete begun on another computer
// (docs/design-archive-delete.md §15.4's DM-17; §15.10 slice 5's help item): its row says Delete not
// finished here too, Delete… on its row finishes it, and a folder not yet under Your archives is
// added with Open an archive folder… (or add-archive) without becoming the archive the page shows —
// on every platform, and naming only controls the page draws (TestTheGuidesDeletePartsNameRealControls
// pins Open an archive folder… and Delete…). Positive twin first: the guide's "Delete not finished"
// sentence for this computer's own delete is still there.
func TestMA1052_TheGuideSaysHowToFinishADeleteBegunElsewhere(t *testing.T) {
	const want = "A delete begun on another computer: a shared folder or a drive can hold an archive whose delete was begun on another computer or user account and did not finish. Its row here says Delete not finished too, and Archive now and Run now start nothing on it. To finish the delete, click Delete… on its row. If the folder is not under Your archives, click Open an archive folder… and choose it, or run mailarchive add-archive FOLDER: the folder is added to Your archives but does not become the archive this page shows, and Delete… on its row finishes the delete."
	for _, p := range helpDeletePlatforms {
		page := getOverview(t, Config{GOOS: p.goos, ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
		h := readHelpCard(t, page)
		assure.Reached(t, h.text, p.goos+": the guide's text")
		if !strings.Contains(h.text, "Delete not finished: if a delete stops part way") {
			t.Fatalf("%s: positive twin: the guide's own Delete not finished part", p.goos)
		}
		if !strings.Contains(h.text, want) {
			t.Errorf("%s: the guide lacks %q", p.goos, want)
		}
	}
}
