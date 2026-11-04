package desktop

import (
	"bytes"
	"encoding/base64"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1081
// MailArchive Desktop's pages show MailArchive's icon in the browser tab: the
// Overview, the Extraction workspace and the Exports page each carry, in their head,
// one <link rel="icon" type="image/png"> whose href is a data: URL of exactly
// packaging/icon/png/mailarchive-32.png (the 32 px drawing, also in the .ico and the
// .icns). The page's Content-Security-Policy is unchanged (img-src data: is what lets
// the browser show it; nothing is fetched from anywhere), so the favicon adds no
// request and no route.
//
// Prove-fail (recorded in the commit): without the link, or with favicon.png not the
// 32 px PNG, each red.
func TestMA1081_TheDesktopPagesHaveTheIcon(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "packaging", "icon", "png", "mailarchive-32.png"))
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, want, "the 32 px icon")
	h := DashboardHandler(Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	linkRe := regexp.MustCompile(`<link rel="icon" type="image/png" href="data:image/png;base64,([A-Za-z0-9+/=]+)">`)
	for _, p := range []string{"/", "/workspace", "/exports"} {
		req := httptest.NewRequest("GET", p, nil)
		req.Host = "127.0.0.1:8097"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		page := rec.Body.String()
		if rec.Code != 200 {
			t.Errorf("%s: status %d", p, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != dashCSP || !strings.Contains(got, "img-src data:") {
			t.Errorf("%s: the Content-Security-Policy is %q, want the unchanged %q", p, got, dashCSP)
		}
		head, _, _ := strings.Cut(page, "</head>")
		links := linkRe.FindAllStringSubmatch(head, -1)
		if len(links) != 1 {
			t.Errorf("%s: %d favicon links in the head, want 1", p, len(links))
			continue
		}
		got, err := base64.StdEncoding.DecodeString(links[0][1])
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: the favicon is not packaging/icon/png/mailarchive-32.png (%v)", p, err)
		}
		if n := strings.Count(page, `rel="icon"`); n != 1 {
			t.Errorf("%s: %d rel=\"icon\" in the page, want 1", p, n)
		}
	}
}
