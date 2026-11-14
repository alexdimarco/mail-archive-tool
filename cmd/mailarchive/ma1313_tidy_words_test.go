package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// stampManifest rewrites the archive's manifest's version stamp.
func stampManifest(t *testing.T, dir, v string) {
	t.Helper()
	mp := filepath.Join(dir, ".mailarchive-manifest.json")
	b, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`"version":\s*\d+`)
	if err := os.WriteFile(mp, []byte(re.ReplaceAllString(string(b), `"version": `+v)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1313, R45
// The tidy-up words say only what is true of an archive another computer is tidying (the B3 UX
// walk's U8, checked against the code): this computer cannot back up into it or add it, and it
// can still open it to read, so the words do not say "open it". Proven on the real program: a
// backup is refused, `add-archive` is refused, `search` still reads it. The words are exactly the
// lead's, less "open it". Positive twin first: at version 6 the backup runs.
func TestTheTidyUpWordsSayOnlyWhatIsTrue(t *testing.T) {
	want := "Another computer started tidying this archive with a newer MailArchive (it removes extra copies of Gmail emails). Nothing is lost. Until it finishes, this computer cannot back up into it or add it. To finish it, open MailArchive on that computer, or install the newest MailArchive here and click Resume tidy-up on this archive's card."
	if state.TidyUpWords != want {
		t.Fatalf("the tidy-up words: %q", state.TidyUpWords)
	}
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "archive")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("positive twin: the backup: exit %d, %s", code, stderr)
	}
	stampManifest(t, dir, "8")
	if code, _ := runCLI("-out", dir, "-input", src); code == 0 {
		t.Fatal("a backup into an archive another computer is tidying ran")
	}
	if out, err := exec.Command(testBin, "add-archive", dir).CombinedOutput(); err == nil || !strings.Contains(string(out), "cannot back up into it or add it") {
		t.Fatalf("add-archive of a version-8 archive: %v\n%s", err, out)
	}
	out, err := exec.Command(testBin, "search", "-out", dir, "Hello").CombinedOutput()
	if err != nil {
		t.Fatalf("search can no longer read a version-8 archive, so the words could say \"open it\": %v\n%s", err, out)
	}
}

// covers: MA-1310, R45
// A backup typed at a terminal into an archive another computer is tidying is not a failed run
// (the B3 UX walk's U2): it records nothing in the archive (status stays WARN, never RED for a
// failed run) and says "Not backed up:", never "FAILED:"; its exit stays 1, since nothing was
// backed up. Positive twin first: at version 6 the backup runs and exits 0.
func TestATerminalBackupMeetingATidyUpIsNotAFailure(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "archive")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("positive twin: exit %d, %s", code, stderr)
	}
	stampManifest(t, dir, "8")
	before, _ := os.ReadFile(filepath.Join(dir, state.LastRunName))
	code, stderr := runCLI("-out", dir, "-input", src)
	if code != 1 || strings.Contains(stderr, "FAILED:") || !strings.Contains(stderr, "Not backed up: "+state.TidyUpWords) {
		t.Fatalf("exit %d, said:\n%s", code, stderr)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, state.LastRunName)); string(after) != string(before) {
		t.Fatal("the refused backup wrote a record into the archive")
	}
	if got, _ := statusOf(t, dir); strings.Contains(got, "RED") || !strings.Contains(got, "WARN: "+state.TidyUpWords) {
		t.Fatalf("status after the refused backup:\n%s", got)
	}
}
