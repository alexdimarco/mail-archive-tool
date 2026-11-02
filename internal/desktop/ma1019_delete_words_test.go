package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
)

// The delete's words on the Desktop, as the 1.5.3 friction review's backlog asks (B4, B6, B7, B8;
// MA-1019): this computer's items only in "What Delete does not reach", the kept files named,
// "1 file" never "1 files", and "capital letters count" beside the typed name.

// otherPlatformsItems are the "What Delete does not reach" items that are not this computer's.
func otherPlatformsItems(goos string) []string {
	var out []string
	if goos != "windows" {
		out = append(out, "Windows File History and Previous Versions")
	}
	if goos != "darwin" {
		out = append(out, "macOS Time Machine")
	}
	return out
}

// covers: MA-1019, R36, S41
// "What Delete does not reach" names only this computer's own backup tools, with its final
// period, in the panel and in the result (B4): Windows File History and Previous Versions on
// Windows, macOS Time Machine on a Mac, neither on Linux; the saved plan and the record keep
// §2's whole line, since they may be read on another computer. Positive twin first: each
// platform's line keeps the sync services' recycle bins and the copies made elsewhere.
func TestWhatDeleteDoesNotReachNamesOnlyThisComputersItems(t *testing.T) {
	for goos, want := range map[string]string{
		"windows": "What Delete does not reach: a sync service's online recycle bin (OneDrive keeps deleted files about 30 days on a personal account, up to 93 on a work account) and version history; Windows File History and Previous Versions; copies, exports or decrypted copies made elsewhere; sync tools other than those detected.",
		"darwin":  "What Delete does not reach: a sync service's online recycle bin (OneDrive keeps deleted files about 30 days on a personal account, up to 93 on a work account) and version history; macOS Time Machine; copies, exports or decrypted copies made elsewhere; sync tools other than those detected.",
		"linux":   "What Delete does not reach: a sync service's online recycle bin (OneDrive keeps deleted files about 30 days on a personal account, up to 93 on a work account) and version history; copies, exports or decrypted copies made elsewhere; sync tools other than those detected.",
	} {
		if got := doesNotReachLine(goos); got != want {
			t.Errorf("%s: %q\nwant %q", goos, got, want)
		}
	}

	e := newDeleteEnv(t)
	dir := e.plainArchive("Reach")
	p := e.plan(dir)
	if p["ok"] != true {
		t.Fatalf("the plan: %v", p)
	}
	if p["doesNotReach"] != doesNotReachLine(runtime.GOOS) {
		t.Errorf("the panel's line is %q, want this computer's", p["doesNotReach"])
	}
	if !strings.Contains(p["planText"].(string), archdelete.DoesNotReach) {
		t.Error("the saved plan no longer keeps §2's whole line")
	}
	r := e.deleteWith(p["token"].(string), p["name"].(string), false)
	lines := strings.Join(toStrings(r["lines"]), "\n")
	if !strings.Contains(lines, doesNotReachLine(runtime.GOOS)) {
		t.Errorf("the result does not say this computer's line: %s", lines)
	}
	for _, other := range otherPlatformsItems(runtime.GOOS) {
		if strings.Contains(lines, other) || strings.Contains(p["doesNotReach"].(string), other) {
			t.Errorf("the panel or the result names %q, which is not on this computer", other)
		}
	}
	if !strings.Contains(string(recordBytes(t, r)), archdelete.DoesNotReach) {
		t.Error("the deletion record no longer keeps §2's whole line")
	}
}

// covers: MA-1019, R36, S41
// The plan names the files it keeps (B6), counts one file as "1 file" (B7) and says capitals
// count beside the typed name (B8): the reply carries the kept files' names (the first ten),
// "Also delete this 1 file (for retention)" for one mail copy no record names and "these N
// files" for more, and "Type the folder's name, NAME, to delete it (capital letters count)".
// Positive twin first: an archive with nothing of the person's keeps nothing and names none.
func TestThePlanNamesKeptFilesAndCountsOneFile(t *testing.T) {
	e := newDeleteEnv(t)
	bare := e.plainArchive("Bare")
	if p := e.plan(bare); p["ok"] != true || p["kept"] != float64(0) || len(toStrings(p["keptNames"])) != 0 {
		t.Fatalf("the positive twin: an archive with nothing of the person's: %v", p)
	}
	dir := e.plainArchive("Mine")
	if err := os.WriteFile(filepath.Join(dir, "my-notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := e.plan(dir)
	if p["ok"] != true {
		t.Fatalf("the plan: %v", p)
	}
	if names := toStrings(p["keptNames"]); p["kept"] != float64(1) || len(names) != 1 || names[0] != "my-notes.txt" {
		t.Errorf("the plan does not name the kept file: kept %v, names %v", p["kept"], names)
	}
	if p["typeLine"] != "Type the folder's name, Mine, to delete it (capital letters count)" {
		t.Errorf("the typed name's prompt: %q", p["typeLine"])
	}

	one := planView(&archdelete.DeletePlan{Unrecorded: []archdelete.Item{{Rel: "store/a.html"}}})
	two := planView(&archdelete.DeletePlan{Unrecorded: []archdelete.Item{{Rel: "store/a.html"}, {Rel: "store/b.html"}}})
	if one["unrecordedBox"] != "Also delete this 1 file (for retention)" || two["unrecordedBox"] != "Also delete these 2 files (for retention)" {
		t.Errorf("the box for mail copies no record names: %q, %q", one["unrecordedBox"], two["unrecordedBox"])
	}
	var many []string
	for i := 0; i < 12; i++ {
		many = append(many, "kept-"+string(rune('a'+i)))
	}
	if names := planView(&archdelete.DeletePlan{Kept: many})["keptNames"].([]string); len(names) != 10 || names[0] != "kept-a" {
		t.Errorf("the plan names %d kept files, want the first 10: %v", len(names), names)
	}
}

// covers: MA-1019, R36, S41
// The panel shows those words (B6, B7): "Delete removes 1 file (SIZE)." and "N files" for more,
// "Kept (not the archive's): NAMES" with "and N more" past the names the plan sent, and the
// prompt the plan sent with its name kept on one line (B14) — the name itself, found between its
// commas, even when it is also in the prompt's own words (the 1.5.3 friction fixes' adversarial
// pass, L2: a folder named "a"). Run on the page script under Node.
func TestThePanelSaysOneFileAndNamesKeptFiles(t *testing.T) {
	if passed := runPageScenario(t, "panelWords"); len(passed) != 5 {
		t.Errorf("the panelWords scenario passed %d of its 5 checks: %v", len(passed), passed)
	}
}
