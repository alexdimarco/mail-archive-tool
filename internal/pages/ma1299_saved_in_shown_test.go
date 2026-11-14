package pages

import (
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

// covers: MA-1299, R45
// A folder page says where a Gmail email is saved by Gmail's own name, "(saved in All Mail)",
// never "(saved in [Gmail]/All Mail)" (the B3 friction review's F11). Positive twin first: a home
// folder not under "[Gmail]/" is named as it is.
func TestFolderPagesNameGmailFoldersByTheirOwnNames(t *testing.T) {
	out := t.TempDir()
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	add := func(folder, file, subject string) {
		m := &model.Message{Subject: subject, Received: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), PlainBody: "b"}
		rel := "s/" + folder + "/" + file
		if err := ix.Add("s", strings.Split(folder, "/"), m, rel, "k-"+subject); err != nil {
			t.Fatal(err)
		}
		full := filepath.Join(out, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("<html>"+subject+"</html>"), 0o644)
	}
	add("[Gmail]/All Mail", "2026-03-01-receipt.html", "Receipt")
	add("Archive", "2026-03-01-memo.html", "Memo")
	ix.SetMembers("k-Receipt", "s", []string{"Receipts"})
	ix.SetMembers("k-Memo", "s", []string{"Receipts"})
	ix.Flush()
	if err := Generate(out, ix, log.New(io.Discard, "", 0), 0); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(filepath.Join(out, "s", "Receipts", "index.html"))
	s := string(page)
	if !strings.Contains(s, "Memo</a> <span class=\"muted\">(saved in Archive)</span>") {
		t.Fatalf("positive twin: Memo's home is not named as it is:\n%s", s)
	}
	if !strings.Contains(s, "Receipt</a> <span class=\"muted\">(saved in All Mail)</span>") || strings.Contains(s, "[Gmail]") {
		t.Fatalf("Receipt's home is not named \"All Mail\":\n%s", s)
	}
}
