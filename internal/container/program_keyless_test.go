package container

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-833, R37
// The keyless check counts what in program/ is not a copy MailArchive keeps there
// (program design §3, C9): a link, a folder, a special file, a name outside §3's
// pattern or ending -attachments.zip, a file over 256 MB — naming up to ten, in quoted
// form when the name is not plain. The program's own temporary files and the
// operating system's litter are not counted, and regular files of the pattern are
// accepted without the key. The count never changes the outcome: the archive stays
// intact. The positive twin: an archive with no program/ counts none.
func TestTheKeylessCheckCountsWhatInProgramIsOdd(t *testing.T) {
	out := wholeArchive(t)
	if c := keyless(t, out, 0); c.ProgramOdd != 0 || c.Outcome != state.OutcomeIntact {
		t.Fatalf("positive twin: %+v", c)
	}
	dir := filepath.Join(out, "program")
	os.Mkdir(dir, 0o700)
	write := func(name string) { os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600) }
	write("mailarchive-linux-amd64")                // kept
	write("SHA256SUMS-v1.5.0")                      // kept
	write("mailarchive-future-asset.tar.gz")        // a newer MailArchive may keep others
	write(".DS_Store")                              // litter
	write(".mailarchive-program-123.tmp")           // the program's own temporary file
	write("bad name!")                              // odd: outside the pattern
	write("x-attachments.zip")                      // odd
	os.Mkdir(filepath.Join(dir, "unzipped"), 0o700) // odd: a folder
	big := filepath.Join(dir, "mailarchive-big")
	f, _ := os.Create(big)
	f.Truncate(ProgramMaxFile + 1) // odd: too large (sparse)
	f.Close()
	odd := 4
	if runtime.GOOS != "windows" {
		os.Symlink(os.TempDir(), filepath.Join(dir, "link")) // odd: a link
		odd++
	}
	c := keyless(t, out, 0)
	names := strings.Join(c.ProgramOddNames, "|")
	assure.Reached(t, c.ProgramOdd == odd && c.Outcome == state.OutcomeIntact && strings.Contains(names, `"bad name!"`) &&
		strings.Contains(names, "unzipped") && !strings.Contains(names, "mailarchive-linux-amd64") && !strings.Contains(names, "DS_Store"),
		"odd: "+names)

	for i := 0; i < 12; i++ {
		os.Mkdir(filepath.Join(dir, "more"+string(rune('a'+i))), 0o700)
	}
	c = keyless(t, out, 0)
	assure.Reached(t, c.ProgramOdd == odd+12 && len(c.ProgramOddNames) == 10, "at most ten named")

	os.RemoveAll(dir)
	os.Symlink(os.TempDir(), dir)
	if runtime.GOOS != "windows" {
		c = keyless(t, out, 0)
		assure.Reached(t, c.ProgramOdd == 1 && c.Outcome == state.OutcomeIntact, "program/ itself a link is odd, never followed")
	}
}
