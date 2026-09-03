package pages

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
)

// covers: MA-91, R7, S27
// A large folder is paginated, never truncated: every message appears on some
// page, pages link to each other and to the root, the date column is labelled
// as UTC, and the root page links every folder's first page and the README.
func TestFolderPagesPaginate(t *testing.T) {
	out, err := os.MkdirTemp("", "pagestest")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(out) })
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()

	old := pageSize
	pageSize = 3
	t.Cleanup(func() { pageSize = old })

	for i := 0; i < 7; i++ {
		m := &model.Message{Subject: fmt.Sprintf("Subject-%d", i), SenderName: "S",
			Received: time.Date(2025, 1, 1+i, 9, 0, 0, 0, time.UTC), PlainBody: "b"}
		if err := ix.Add("store", []string{"Inbox"}, m, fmt.Sprintf("store/Inbox/m%d.html", i), fmt.Sprintf("Inbox\x00id%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := Generate(out, ix, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}

	pages := []string{"index.html", "index-2.html", "index-3.html"}
	var all strings.Builder
	for i, name := range pages {
		data, err := os.ReadFile(filepath.Join(out, "store", "Inbox", name))
		if err != nil {
			t.Fatalf("page %s missing: %v", name, err)
		}
		s := string(data)
		all.WriteString(s)
		if !strings.Contains(s, "Date (UTC)") {
			t.Errorf("%s: date column not labelled UTC", name)
		}
		if !strings.Contains(s, `href="../../index.html"`) {
			t.Errorf("%s: no link to the archive root", name)
		}
		if i > 0 && !strings.Contains(s, pages[i-1]) {
			t.Errorf("%s: no link to the previous page %s", name, pages[i-1])
		}
		if i < len(pages)-1 && !strings.Contains(s, pages[i+1]) {
			t.Errorf("%s: no link to the next page %s", name, pages[i+1])
		}
		if strings.Contains(s, "omitted") {
			t.Errorf("%s: still speaks of omitted rows", name)
		}
	}
	for i := 0; i < 7; i++ {
		if !strings.Contains(all.String(), fmt.Sprintf("Subject-%d", i)) {
			t.Errorf("Subject-%d appears on no page", i)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "store", "Inbox", "index-4.html")); err == nil {
		t.Error("an extra empty page was written")
	}

	root, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(root), "store/Inbox/index.html") || !strings.Contains(string(root), "README.txt") {
		t.Errorf("root page must link the folder's first page and README.txt:\n%s", root)
	}
}

// genFolder builds an index with the given per-folder message counts under
// store/<folder> and generates the static pages into a fresh temp dir with
// pageSize set to size. It returns the output directory.
func genFolder(t *testing.T, folders map[string]int, size int) string {
	t.Helper()
	out, err := os.MkdirTemp("", "pagertest")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(out) })
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()

	old := pageSize
	pageSize = size
	t.Cleanup(func() { pageSize = old })

	for folder, n := range folders {
		for i := 0; i < n; i++ {
			m := &model.Message{Subject: fmt.Sprintf("Subject-%d", i), SenderName: "S",
				Received: time.Date(2025, 1, 1+i, 9, 0, 0, 0, time.UTC), PlainBody: "b"}
			rel := fmt.Sprintf("store/%s/m%03d.html", folder, i)
			if err := ix.Add("store", []string{folder}, m, rel, fmt.Sprintf("%s\x00id%d", folder, i)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := Generate(out, ix, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	return out
}

func readPage(t *testing.T, out, folder, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "store", folder, name))
	if err != nil {
		t.Fatalf("page %s/%s missing: %v", folder, name, err)
	}
	return string(data)
}

// covers: MA-119, R7, S27
// A folder with many pages renders a compact numbered pager: on page 6 of 12 the
// numbers read 1 … 4 5 6 7 8 … 12, the neighbour/first/last pages carry the right
// relative hrefs, the current page is not a link, skipped pages are absent and
// stand behind an ellipsis, and the newer/older links remain. Page 1 has no
// "newer" link. Small folders (<=3 pages) show no numbered pager at all.
func TestFolderPagesNumberedPager(t *testing.T) {
	out := genFolder(t, map[string]int{"Inbox": 12}, 1) // 12 messages, 1 per page -> 12 pages

	// Page 6: 1 … 4 5 [6] 7 8 … 12.
	p6 := readPage(t, out, "Inbox", "index-6.html")
	for _, href := range []string{
		`href="index.html"`,    // first page
		`href="index-4.html"`,  // current-2
		`href="index-5.html"`,  // current-1 (also the "newer" link)
		`href="index-7.html"`,  // current+1 (also the "older" link)
		`href="index-8.html"`,  // current+2
		`href="index-12.html"`, // last page
	} {
		if !strings.Contains(p6, href) {
			t.Errorf("page 6 pager missing %s", href)
		}
	}
	for _, absent := range []string{
		`href="index-6.html"`,  // the current page never links to itself
		`href="index-2.html"`,  // inside the first ellipsis gap
		`href="index-3.html"`,  // inside the first ellipsis gap
		`href="index-9.html"`,  // inside the last ellipsis gap
		`href="index-11.html"`, // inside the last ellipsis gap
	} {
		if strings.Contains(p6, absent) {
			t.Errorf("page 6 pager should not contain %s", absent)
		}
	}
	if !strings.Contains(p6, `<span class="muted">…</span>`) {
		t.Error("page 6 pager omits the ellipsis for the skipped pages")
	}
	// The current page is present as a plain number, not as a link.
	if !strings.Contains(p6, "<b>6</b>") {
		t.Error("page 6 pager does not mark the current page")
	}
	// The single-step newer/older links stay.
	if !strings.Contains(p6, "newer") || !strings.Contains(p6, "older") {
		t.Error("page 6 dropped the newer/older links")
	}

	// Page 1 is the newest: no "newer" link, but there is a numbered pager and an
	// "older" link.
	p1 := readPage(t, out, "Inbox", "index.html")
	if strings.Contains(p1, "newer") {
		t.Error("page 1 (newest) should have no newer link")
	}
	if !strings.Contains(p1, "older") {
		t.Error("page 1 should still have an older link")
	}
	if !strings.Contains(p1, `href="index-12.html"`) || !strings.Contains(p1, `href="index-2.html"`) {
		t.Error("page 1 pager missing the last-page or neighbour link")
	}

	// A small folder (<=3 pages) shows the newer/older links but no numbered pager.
	small := genFolder(t, map[string]int{"Few": 6}, 3) // 6 messages, 3 per page -> 2 pages
	sp := readPage(t, small, "Few", "index.html")
	if !strings.Contains(sp, "older") {
		t.Error("a 2-page folder should still have an older link")
	}
	if strings.Contains(sp, `href="index-3.html"`) || strings.Contains(sp, "<b>") || strings.Contains(sp, `<span class="muted">…</span>`) {
		t.Error("a 2-page folder should show no numbered pager")
	}
}

// covers: MA-120, R7, S27
// The in-page filter box keeps the honest "Filter" label. On a paginated folder
// the help text is explicit that the filter matches this page only (N of M) and
// points the reader at `mailarchive serve` and a grep tool for whole-archive and
// body search; an unpaginated folder keeps the short placeholder and shows no
// such caveat.
func TestFolderFilterPlaceholderScope(t *testing.T) {
	out := genFolder(t, map[string]int{"Big": 12, "Small": 2}, 5) // Big -> 3 pages, Small -> 1 page

	big := readPage(t, out, "Big", "index.html")
	if !strings.Contains(big, "Filter this page") {
		t.Error("paginated folder should say the filter is page-scoped in the placeholder")
	}
	if !strings.Contains(big, "this page only") {
		t.Error("paginated folder help must say the filter matches this page only")
	}
	if !strings.Contains(big, "5 of 12") {
		t.Errorf("paginated folder help must state N of M (want 5 of 12):\n%s", big)
	}
	if !strings.Contains(big, "mailarchive serve") {
		t.Error("paginated folder help must point to `mailarchive serve` for whole-archive search")
	}
	if !strings.Contains(big, "rg ") {
		t.Error("paginated folder help must point to a grep tool for body search")
	}

	small := readPage(t, out, "Small", "index.html")
	if !strings.Contains(small, "Filter these messages") {
		t.Error("unpaginated folder should keep the short placeholder")
	}
	if strings.Contains(small, "this page only") || strings.Contains(small, "mailarchive serve") {
		t.Error("unpaginated folder should not carry the page-scope caveat")
	}
}
