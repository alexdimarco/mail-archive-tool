package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-374, R19, S22, S45
// P9's location rules judge the path the open actually reaches, not the one
// the checks Lstat-ed: a folder along the way swapped for a link AFTER its own
// Lstat — so the next Lstat and the open both follow it — never lets a page in
// the _outlook-pst staging folder, a dot-folder or the archive root be served
// through /files/store/Inbox/. Positive twin: the same request with nothing
// swapped reaches the real page. (Adversarial pass AC1.)
func TestFilesJudgesTheOpenedPathNotTheLstat(t *testing.T) {
	cases := []struct {
		name, planted, marker, target string
	}{
		{"the staging folder", "_outlook-pst/stray.html", "SECRET-pst-page", filepath.Join("..", "_outlook-pst")},
		{"the archive root", "stray.html", "SECRET-root-page", ".."},
		{"a dot-folder", "store/.dotdir/stray.html", "SECRET-dotdir-stray", ".dotdir"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, dir := filesFixture(t)
			if err := os.Symlink("search.db", filepath.Join(dir, "probe-link")); err != nil {
				t.Skipf("symlinks unavailable here: %v", err)
			}
			planted := filepath.Join(dir, filepath.FromSlash(c.planted))
			if err := os.MkdirAll(filepath.Dir(planted), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(planted, []byte(c.marker), 0o600); err != nil {
				t.Fatal(err)
			}
			inbox := filepath.Join(dir, "store", "Inbox")
			restore := probeEntry
			t.Cleanup(func() { probeEntry = restore })

			// Positive twin: a real page in the folder is served, and the planted
			// one is not reachable through it.
			expectServed(t, h, "store/Inbox/index-2.html", "FOLDER-PAGE-TWO")
			expectNotServed(t, h, "store/Inbox/stray.html", c.marker)

			swapped := 0
			probeEntry = func(root *os.Root, name string) (fs.FileInfo, fs.FileMode, bool, error) {
				fi, mode, sur, err := restore(root, name)
				if filepath.ToSlash(name) == "store/Inbox" && swapped == 0 {
					swapped++
					if err := os.Rename(inbox, inbox+"-moved"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(c.target, inbox); err != nil {
						t.Fatal(err)
					}
				}
				return fi, mode, sur, err
			}
			rec := getFile(h, "store/Inbox/stray.html")
			assure.Reached(t, swapped, "the swap after the folder's own Lstat")
			assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusNotFound), assure.Forbid(c.marker))
		})
	}
}
