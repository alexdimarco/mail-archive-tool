package tbrestore

import (
	"strings"
	"testing"
)

// covers: MA-1126, R40
// The Windows path budget (design §4.5 as folded; the Windows rule, tested on every
// leg): in UTF-16 units from the Local Folders path, a file plus its ".msf" at most 259
// and a folder at most 247 — the exact boundaries 259/260 and 247/248; a folder over
// budget has the names on its branch shortened (30 characters plus the hash of the
// original), deepest first, and one still over is not restored (W-LONGPATH), with every
// folder below it; the marker, the made-list and the parent file are counted, so a
// Local Folders so deep that they do not fit refuses the whole restore.
func TestRestorePathBudget(t *testing.T) {
	if !winFileOK(strings.Repeat("a", 255)) || winFileOK(strings.Repeat("a", 256)) {
		t.Error("a file of 255 units plus .msf is 259 (fits); 256 is 260 (does not)")
	}
	if !winDirOK(strings.Repeat("a", 247)) || winDirOK(strings.Repeat("a", 248)) {
		t.Error("a folder of 247 units fits; 248 does not")
	}
	if utf16Len("📨") != 2 || utf16Len("é") != 1 {
		t.Fatal("UTF-16 units are counted")
	}

	// A deep tree of long names under a long Local Folders path.
	local := `C:\Users\` + strings.Repeat("u", 40) + `\AppData\Roaming\Thunderbird\Profiles\abcd.default-release\Mail\Local Folders`
	name := strings.Repeat("L", 60)
	var in []FolderIn
	var path []string
	for d := 0; d < 6; d++ {
		path = append(path, name+itoa(d))
		in = append(in, FolderIn{Names: append([]string(nil), path...), Messages: 1})
	}
	l, err := PlanLayout("Work mail", in, local, "windows")
	if err != nil {
		t.Fatal(err)
	}
	container := local + `\` + withNumber(l.Top, 99) + `.sbd\`
	short, long := 0, 0
	for _, f := range l.Folders {
		p := container + strings.ReplaceAll(f.File(), "/", `\`)
		if f.LongPath {
			long++
			continue
		}
		if !winFileOK(p) || (f.HasChildren && !winDirOK(p+".sbd")) {
			t.Errorf("a restored folder over budget: %d units: %s", utf16Len(p), p)
		}
		for _, part := range f.Parts {
			if strings.Contains(part, "~") && len([]rune(part)) == shortRunes+hashSuffixLen {
				short++
			}
		}
	}
	if short == 0 {
		t.Error("names on an over-budget branch are shortened first")
	}
	if long == 0 {
		t.Error("the deepest folders, still over, are not restored (W-LONGPATH)")
	}
	for i, f := range l.Folders {
		if f.Parent >= 0 && l.Folders[f.Parent].LongPath && !f.LongPath {
			t.Errorf("folder %d is below one not restored, yet restored", i)
		}
	}
	// On macOS and Linux nothing is shortened or left out.
	l2, err := PlanLayout("Work mail", in, "/home/ann/.thunderbird/x/Mail/Local Folders", "linux")
	if err != nil || len(l2.Restored()) != len(in) {
		t.Fatalf("no Windows budget elsewhere: %v %d", err, len(l2.Restored()))
	}

	// A Local Folders so deep the container's own entries cannot fit: nothing restored.
	deep := `C:\` + strings.Repeat(`d\`, 115) + `Local Folders`
	_, err = PlanLayout("Work mail", in, deep, "windows")
	refusedAs(t, err, "W-LONGPATH", nil, "so deep", "nothing was restored")
}
