package desktop

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// stampVersion rewrites the archive's manifest version to v, as a newer MailArchive would.
func stampVersion(t *testing.T, out, v string) {
	t.Helper()
	p := filepath.Join(out, manifestName)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`"version":\s*\d+`)
	if !re.Match(b) {
		t.Fatal("the manifest has no version")
	}
	done := false
	b = re.ReplaceAllFunc(b, func(m []byte) []byte {
		if done {
			return m
		}
		done = true
		return []byte(`"version": ` + v)
	})
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1297, R45, S82
// Every Desktop place that refuses an archive a newer MailArchive is tidying says the tidy-up words
// (the B3 friction review's F9): the Scheduled backup card, Settings, Save settings' refusal and
// Add another archive (at the Desktop and at a terminal), never "can't read this archive's record
// right now (is its drive connected?)", "cannot be read … repair it", or "a backup set up at the
// command line" for a backup the Desktop set up. Positive twin first: a damaged record keeps the
// words it had (unknown, nothing decided on a guess).
func TestEveryDesktopPlaceSaysTheTidyUpWords(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"manual","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("setting up Run now only: %s", body)
	}

	// Positive twin: a damaged record.
	mp := filepath.Join(out, manifestName)
	good, _ := os.ReadFile(mp)
	if err := os.WriteFile(mp, []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if page := g.page(); !shows(page, unknownKindLine) {
		t.Fatal("positive twin: a damaged record no longer says it can't be read")
	}
	if err := os.WriteFile(mp, good, 0o600); err != nil {
		t.Fatal(err)
	}

	stampVersion(t, out, "8")
	page := html.UnescapeString(g.page())
	if strings.Count(page, state.TidyUpWords) < 2 {
		t.Fatalf("the Scheduled backup card and Settings do not both say the tidy-up words (%d)", strings.Count(page, state.TidyUpWords))
	}
	for _, bad := range []string{unknownKindLine, "a backup set up at the command line"} {
		if strings.Contains(page, bad) {
			t.Errorf("a version-8 archive's page says %q", bad)
		}
	}
	u := g.snapshot()
	body := g.post(`{"interval":"daily","time":"04:00"}`)
	assure.Refused(t, refusedCode(body), body, assure.Names(state.TidyUpWords, "Nothing was changed"), assure.Forbid("drive connected"), assure.NoSideEffect(u.holds))

	for _, atTerminal := range []bool{false, true} {
		why := folderProblem(out, atTerminal)
		if !strings.Contains(why, state.TidyUpWords) || strings.Contains(why, "cannot be read") || strings.Contains(why, "repair") {
			t.Errorf("Add another archive (terminal %v) says %q", atTerminal, why)
		}
	}
	stampVersion(t, out, "9")
	for _, atTerminal := range []bool{false, true} {
		if why := folderProblem(out, atTerminal); !strings.Contains(why, state.NewerWords) || strings.Contains(why, "repair") {
			t.Errorf("a version-9 archive's add-archive words (terminal %v): %q", atTerminal, why)
		}
	}
}
