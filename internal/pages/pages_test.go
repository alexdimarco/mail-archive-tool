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
