package state

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1158, R18, S48
// A notice names a Windows folder that ends in a backslash (a drive root, a share root) so the
// command still reads back as that folder: by the C runtime's rule a backslash right before the
// closing quote would escape it, so the trailing backslashes are doubled inside the quotes
// (each is read back as one), and no other backslash is touched. The integrity notice names
// its folder the same way, never in Go's quoted form. Positive twin first: a folder that ends
// in a name keeps every backslash single.
func TestANoticeNamesADriveRootSoItReadsBack(t *testing.T) {
	if got := typedPath(`C:\Users\Public\Archive`, "windows"); got != `"C:\Users\Public\Archive"` {
		t.Fatalf("the positive twin: %s", got)
	}
	for path, want := range map[string]string{`E:\`: `"E:\\"`, `\\nas\backup\`: `"\\nas\backup\\"`} {
		if got := typedPath(path, "windows"); got != want {
			t.Errorf("typedPath(%s) = %s, want %s", path, got, want)
		}
	}
	if got := typedPath(`/Volumes/Mail/`, "darwin"); got != `'/Volumes/Mail/'` {
		t.Errorf("a POSIX folder keeps its slash: %s", got)
	}

	out := t.TempDir()
	if err := WriteLastVerify(out, LastVerify{Status: VerifyDone, Modified: 1}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, IntegrityAttentionName))
	assure.Reached(t, strings.Contains(string(b), "mailarchive verify -out "+typedPath(out, runtime.GOOS)+"\n"),
		"the integrity notice names its folder as it is typed:\n"+string(b))
}
