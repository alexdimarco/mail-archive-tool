package state

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1146, R18, S48
// BACKUP-NEEDS-ATTENTION.txt names the command to run with the archive's folder as a person types
// it on their system, never in Go's quoted form (friction review of plain-failure-words, Windows
// lab 2026-11-07: Notepad showed `mailarchive status -out "C:\\Users\\Public\\…\\Walk Archive"`,
// every backslash doubled). On Windows a path goes in double quotes as it is; elsewhere in single
// quotes, an embedded single quote closed and reopened. Both notices, the partial run's and the
// outright failure's, name it that way.
func TestTheAttentionNoticeNamesTheFolderAsItIsTyped(t *testing.T) {
	for _, c := range []struct{ goos, path, want string }{
		{"windows", `C:\Users\Public\pfwlab\Archives\Walk Archive`, `"C:\Users\Public\pfwlab\Archives\Walk Archive"`},
		{"windows", `\\nas\backup\Mail`, `"\\nas\backup\Mail"`},
		{"darwin", "/Users/a/Mail Archive", "'/Users/a/Mail Archive'"},
		{"linux", "/home/a/it's mail", `'/home/a/it'\''s mail'`},
	} {
		if got := typedPath(c.path, c.goos); got != c.want {
			t.Errorf("typedPath(%q, %s) = %s, want %s", c.path, c.goos, got, c.want)
		}
	}

	out := t.TempDir()
	cmd := "mailarchive status -out " + typedPath(out, runtime.GOOS)
	if err := WriteLastRun(out, LastRun{Status: RunFailed, Error: "the archive drive is full"}); err != nil {
		t.Fatal(err)
	}
	failed, _ := os.ReadFile(filepath.Join(out, AttentionName))
	p := &PartialCounts{Mistake: PartialKind{Messages: 1}}
	if err := WriteLastRun(out, LastRun{Status: RunFailed, Error: PartialText(*p, true), Partial: p}); err != nil {
		t.Fatal(err)
	}
	partial, _ := os.ReadFile(filepath.Join(out, AttentionName))
	assure.Reached(t, strings.Contains(string(failed), cmd) && strings.Contains(string(partial), cmd),
		"both notices name "+cmd+":\n"+string(failed)+"\n"+string(partial))
}
