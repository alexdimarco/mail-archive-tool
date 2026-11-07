package tbengine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// deepFolder is the long name of a folder a deep Local Folders cannot hold on Windows: its
// own ".sbd" is over the 247 a folder's path may have, while it is short enough (27, under
// the 39 that is shortened) to stay as it is.
const deepFolder = "Quarterly reports and plans"

// covers: MA-1124, R40
// On Windows, a real plan into a Local Folders 192 characters deep: "Inbox" goes back, and
// the W-LONGPATH line names the three folders below deepFolder that cannot, each quoted —
// "100%" as a name, and a name holding a line break (as the archive's record keeps it)
// with the break shown as \n — in the plan's lines and in its Text, what the terminal
// prints and the saved records keep.
func TestADeepPlanQuotesTheFoldersItLeavesOut(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Mail")
	write(t, filepath.Join(src, "Inbox"), mboxOf("inbox one"))
	write(t, filepath.Join(src, deepFolder), mboxOf("deep one"))
	write(t, filepath.Join(src, deepFolder+".sbd", "100%"), mboxOf("pct one"))
	write(t, filepath.Join(src, deepFolder+".sbd", "Linebreak"), mboxOf("break one"))
	out := filepath.Join(t.TempDir(), "Work mail")
	backup(t, src, out, true, nil)
	refolder(t, out, deepFolder+"/Linebreak", deepFolder+"/Line\nbreak")

	pr := newProfile(t, padded(t, 192-len(`\p.default\Mail\Local Folders`)))
	if len(pr.local) != 192 {
		t.Fatalf("the fixture's Local Folders is %d characters, want 192: %s", len(pr.local), pr.local)
	}
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	got := lineOf(p, "W-LONGPATH")
	for _, w := range []string{"3 folders are nested too deep", `"` + deepFolder + `"`, `"` + deepFolder + `/100%"`, `"` + deepFolder + `/Line\nbreak"`} {
		assure.Reached(t, strings.Contains(got, w), fmt.Sprintf("W-LONGPATH holds %s: %q", w, got))
	}
	assure.Reached(t, !strings.Contains(got, "\n") && strings.Contains(p.Text(), "\n"+got+"\n"),
		fmt.Sprintf("one line in the plan's text, no break from a name: %q", p.Text()))
	assure.Reached(t, p.Messages == 1, fmt.Sprintf("Inbox alone goes back: %d", p.Messages))
}

// padded is a new temp folder whose own path is n characters long.
func padded(t *testing.T, n int) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pad := n - len(base) - 1
	if pad < 1 {
		t.Fatalf("the temp folder %s is too long to pad to %d", base, n)
	}
	return filepath.Join(base, strings.Repeat("d", pad))
}

// refolder moves the records of folder from to folder to in the archive's own record, as
// an archive made elsewhere (or from Microsoft 365) can keep a name no Windows file holds.
func refolder(t *testing.T, out, from, to string) {
	t.Helper()
	man, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	n := 0
	for k, r := range man.All() {
		seen = append(seen, r.Folder)
		if r.Folder == from {
			man.MergeFields(k, to, r.LastSeen, r.Present)
			n++
		}
	}
	if n == 0 {
		t.Fatalf("no record in %q: %q", from, seen)
	}
	if err := man.Save(); err != nil {
		t.Fatal(err)
	}
}
