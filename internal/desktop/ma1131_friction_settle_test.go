package desktop

import (
	"context"
	"html"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// covers: MA-1131, R40
// The R7 friction review's proposals, settled 2026-11-07 (the lead's calls), on the server:
// P1 — the profile list says which profiles cannot take a restore and never pre-selects one:
// two profiles, the one Thunderbird opens by default with no Local Folders (in the walk it was
// the pre-selected one), the other ready; the first is listed not usable with its reason, the
// default is the second. P4 — an archive whose every store is an Outlook data file says so on
// the card, the button disabled with the reason beside it; positive twin first, a Thunderbird
// archive's button is enabled. F15 — W-NOTAGAIN on the page names the page's own box.
func TestTheFrictionReviewIsSettledOnTheServer(t *testing.T) {
	e := newRtbEnv(t, rtbPlainArchive(t, filepath.Join(t.TempDir(), "Mail")), "", 2)
	btn := regexp.MustCompile(`<button[^>]*id="rtbBtn"[^>]*>`)
	page := html.UnescapeString(e.req(http.MethodGet, "/", nil, nil).Body.String())
	assure.Reached(t, !strings.Contains(btn.FindString(page), "disabled") && !strings.Contains(page, restoreOutlookWhy), "twin: a Thunderbird archive's button is enabled")

	rtbWrite(t, filepath.Join(e.profiles[0].dir, "prefs.js"), "// Mozilla User Preferences\n") // no Local Folders
	pr := e.profilesReply()
	ps, _ := pr["profiles"].([]any)
	assure.Reached(t, len(ps) == 2, "both profiles listed")
	p0, _ := ps[0].(map[string]any)
	p1, _ := ps[1].(map[string]any)
	assure.Reached(t, p0["usable"] == false && strings.Contains(str(p0["unusable"]), "no Local Folders yet") && p1["usable"] == true,
		"the profile with no Local Folders is listed, not usable, with its reason: "+str(p0["unusable"]))
	assure.Reached(t, pr["default"] == float64(1), "the pre-selected profile is the one that can take a restore")

	opts := app.Options{Inputs: []string{filepath.Join("..", "..", "testdata", "support.pst")}, Out: filepath.Join(t.TempDir(), "Outlook mail"),
		Mode: export.Incremental, Index: true, Pages: true, KeepRaw: true}
	if _, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatalf("backup of the Outlook file: %v", err)
	}
	o := newRtbEnv(t, opts.Out, "", 1)
	page = html.UnescapeString(o.req(http.MethodGet, "/", nil, nil).Body.String())
	assure.Reached(t, strings.Contains(btn.FindString(page), "disabled") && strings.Contains(page, `<span class="card-sub" id="rtbWhy">`+restoreOutlookWhy+`</span>`),
		"an Outlook-only archive: the button disabled, the reason beside it")

	notAgain := "The restored mail will not be encrypted, and X. That was not confirmed a second time, so nothing was restored. Confirm it again, or choose a profile on this computer's own drive."
	got := restoreAgainWords(notAgain)
	assure.Reached(t, strings.Contains(got, "Tick “Restore it there anyway” and click Restore again, or choose a profile") && !strings.Contains(got, "Confirm it again"), got)
}
