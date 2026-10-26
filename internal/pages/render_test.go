package pages

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
)

// generatedLine is the time stamp two renders a minute apart differ by.
var generatedLine = regexp.MustCompile(`Generated [0-9: -]+UTC`)

// covers: MA-796, R7
// Render is Generate in memory, for an encrypted archive's reader
// (design-encrypted-archives-phase3 §7.2): the same folder pages under the same
// archive-relative paths, page for page, and a root page worded for an encrypted
// archive — no README, no report file — with the gap banner's encrypted wording.
func TestRenderIsGenerateInMemoryWordedForAnEncryptedArchive(t *testing.T) {
	out := genFolder(t, map[string]int{"Inbox": 7, "Sent": 2}, 3)
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	written := map[string]string{}
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".html") {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(out, p)
			written[filepath.ToSlash(rel)] = generatedLine.ReplaceAllString(string(b), "")
		}
		return nil
	})
	rendered, err := Render(ix, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered) != len(written) {
		t.Fatalf("%d pages rendered, %d generated", len(rendered), len(written))
	}
	for rel, want := range written {
		got, ok := rendered[rel]
		if !ok {
			t.Fatalf("%s not rendered", rel)
		}
		if rel != "index.html" && generatedLine.ReplaceAllString(string(got), "") != want {
			t.Fatalf("%s differs from the generated page", rel)
		}
	}
	root := string(rendered["index.html"])
	if !strings.Contains(root, "An encrypted archive") || !strings.Contains(root, GapBannerEncrypted) ||
		strings.Contains(root, "README.txt") || strings.Contains(root, "attachments-report.tsv") || !strings.Contains(root, `href="store/Inbox/index.html"`) {
		t.Fatalf("the encrypted root page:\n%s", root)
	}
	assure.Reached(t, root, "the encrypted root page")
}
