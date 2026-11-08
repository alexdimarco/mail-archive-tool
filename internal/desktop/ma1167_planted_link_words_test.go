package desktop

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// plantedNearTheAddress are history words anyone who can write the archive folder can plant around,
// inside and next to the report address (the polish-161 adversarial pass, L1 and L2).
func plantedNearTheAddress() []string {
	A := state.ReportAddress
	return []string{
		A + "@evil.example/x",          // the address followed by extra text
		"https://evil.example/?u=" + A, // the address inside another one
		A + A,                          // twice, touching
		"javascript:alert(1)//" + A,    // a scheme before it
		`"><a href="https://evil.example/">x</a>` + A,             // markup and an attribute break
		"‮" + A + " ‭see evil.example ⁦x⁩",                        // direction overrides and isolates around it
		"https://github.com/alexdimarcо/mail-archive-tool/issues", // a Cyrillic look-alike
		strings.ToUpper(A), // the address in another case
	}
}

// recentBackupsList is the page's Recent backups list for an archive whose history holds words.
func recentBackupsList(t *testing.T, words []string) string {
	t.Helper()
	out := t.TempDir()
	writeManifest(t, out)
	at := time.Date(2026, 11, 7, 17, 4, 0, 0, time.Local)
	for i, w := range words {
		if err := state.AppendRunHistory(out, state.RunHistoryEntry{When: at.Add(time.Duration(i) * time.Minute), Status: state.RunFailed, Error: w}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), ConfigPath: filepath.Join(t.TempDir(), "none.json"), Out: out}
	page := cfg.overview("csrf-token")
	i := strings.Index(page, `<ul class="run-history">`)
	if i < 0 {
		t.Fatal("no Recent backups list")
	}
	sec := page[i:]
	return sec[:strings.Index(sec, "</ul>")]
}

// covers: MA-1151, R18, S29
// Planted history words around, inside and next to the report address never make a link to
// anything but the fixed address, and no element but that link is drawn from them (the adversarial
// pass, L1, refuted: kept as a guard). Positive twin first: words with no report address hold no
// link.
func TestPlantedWordsNeverMakeAnotherLink(t *testing.T) {
	if plain := recentBackupsList(t, []string{"the archive drive is full"}); strings.Contains(plain, "<a") {
		t.Fatalf("the positive twin: %s", plain)
	}
	sec := recentBackupsList(t, plantedNearTheAddress())
	links := 0
	for _, m := range regexp.MustCompile(`<([a-zA-Z]+)`).FindAllStringSubmatch(sec, -1) {
		switch strings.ToLower(m[1]) {
		case "ul", "li", "span":
		case "a":
			links++
		default:
			t.Errorf("a planted element <%s> in Recent backups", m[1])
		}
	}
	hrefs := regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(sec, -1)
	for _, h := range hrefs {
		if h[1] != state.ReportAddress {
			t.Errorf("a link to %q", h[1])
		}
	}
	// A@…, ?u=A, A A (2), js…A, markup…A, the overridden A: 7 links, all to the address.
	assure.Reached(t, links == 7 && len(hrefs) == 7, "only the report address is a link")
}

// covers: MA-1167, R18, S29
// Direction-changing characters (bidi overrides and isolates, U+202A to U+202E and U+2066 to
// U+2069) in run-history words are removed before Recent backups shows them (the adversarial pass,
// L2: beside the report link they can reverse or move how the address looks); the page script's
// sayText removes them wherever it shows words beside the link (Archive now, Run now, Status &
// health; the plantedReportLink scenario). Positive twin first: the words around them are kept.
func TestRecentBackupsDropsDirectionChangingCharacters(t *testing.T) {
	sec := recentBackupsList(t, plantedNearTheAddress())
	if !strings.Contains(sec, "see evil.example x") {
		t.Fatalf("the positive twin: the words around the characters are gone:\n%s", sec)
	}
	for _, r := range []rune{'‪', '‫', '‬', '‭', '‮', '⁦', '⁧', '⁨', '⁩'} {
		if strings.ContainsRune(sec, r) {
			t.Errorf("Recent backups keeps %U", r)
		}
	}
	if passed := runPageScenario(t, "plantedReportLink"); len(passed) != 3 {
		t.Errorf("the plantedReportLink scenario passed %d of its 3 checks: %v", len(passed), passed)
	}
	assure.Reached(t, strings.Contains(sec, `">`+state.ReportAddress+`</a> see evil.example`), "the overridden address is drawn as the plain link")
}
