package desktop

import (
	"html"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1141, R41, S68
// Desktop start names each earlier restore into Thunderbird that left a folder
// Thunderbird does not show (design-restore-thunderbird §7.2 foldcheck2 note P2), in
// the page header beside the other notices, in tbrestore.Leftovers' words; with none,
// no such notice (positive twin).
func TestDesktopStartNamesARestoreLeftover(t *testing.T) {
	const words = "An earlier restore into Thunderbird did not finish (6 November 2026, 10:00). Thunderbird does not show it."
	was := restoreNotices
	defer func() { restoreNotices = was }()

	restoreNotices = func() []string { return nil }
	h := DashboardHandler(Config{ConfigPath: filepath.Join(t.TempDir(), "desktop-settings.json")})
	page, _ := getPage(t, h)
	if strings.Contains(page, "data-restore-leftover") {
		t.Fatal("no leftover: no notice")
	}

	restoreNotices = func() []string { return []string{words} }
	page, _ = getPage(t, h)
	if !strings.Contains(page, "data-restore-leftover") || !strings.Contains(html.UnescapeString(page), words) {
		t.Fatalf("the leftover is named at Desktop start:\n%s", page[:min(len(page), 2000)])
	}
}
