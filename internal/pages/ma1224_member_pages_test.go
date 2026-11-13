package pages

import (
	"io"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
)

// memberArchive is a plaintext archive folder with a search index: in store "s", "Receipt" lives
// in All Mail (its page on disk) and is also in Sent Mail and Labels/Work; "Lunch" lives in INBOX.
func memberArchive(t *testing.T) (string, *index.Index) {
	t.Helper()
	out := t.TempDir()
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	add := func(folder, file, subject string, day int, attach bool) {
		t.Helper()
		m := &model.Message{Subject: subject, SenderName: "Ann", Received: time.Date(2026, 3, day, 9, 0, 0, 0, time.UTC), PlainBody: "b"}
		if attach {
			m.Attachments = []model.Attachment{{Filename: "r.pdf"}}
		}
		rel := "s/" + folder + "/" + file
		if err := ix.Add("s", strings.Split(folder, "/"), m, rel, "k-"+subject); err != nil {
			t.Fatal(err)
		}
		full := filepath.Join(out, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("<html>"+subject+"</html>"), 0o644)
		if attach {
			os.WriteFile(strings.TrimSuffix(full, ".html")+"-attachments.zip", []byte("zip"), 0o644)
		}
	}
	add("All Mail", "2026-03-01-receipt.html", "Receipt", 1, true)
	add("INBOX", "2026-03-02-lunch.html", "Lunch", 2, false)
	if err := ix.SetMembers("k-Receipt", "s", []string{"Sent Mail", "Labels/Work", "INBOX"}); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	return out, ix
}

var hrefRE = regexp.MustCompile(`href="([^"]+)"`)

// pageLinks is every href on the page at rel (archive-relative), resolved against the page's
// folder, unescaped, for those naming a message page or an attachments zip.
func pageLinks(t *testing.T, out, rel string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("page %s: %v", rel, err)
	}
	var got []string
	for _, m := range hrefRE.FindAllStringSubmatch(string(data), -1) {
		h, err := url.PathUnescape(m[1])
		if err != nil {
			t.Fatalf("%s: link %q does not unescape: %v", rel, m[1], err)
		}
		if strings.HasSuffix(h, ".zip") || (strings.HasSuffix(h, ".html") && !strings.Contains(path.Base(h), "index")) {
			got = append(got, path.Join(path.Dir(rel), h))
		}
	}
	return got
}

// covers: MA-1224, R45, R6, R4, R19
// A folder page lists every Gmail email in that folder, also those living elsewhere, each linking
// by a relative path to its files in its home folder inside the same store's folder (R4): Sent
// Mail, whose every email lives in All Mail, gets its own page; a nested label links two levels
// up; INBOX lists its own email and Receipt once each; every link resolves to a file on disk. The
// member row adds no script and no scripted link (R19); it says where the email is saved. The
// root page lists the member-only folder with its count. Positive twin first: INBOX's own email
// keeps its plain same-folder link.
func TestFolderPagesListEmailsLivingElsewhere(t *testing.T) {
	out, ix := memberArchive(t)
	if err := Generate(out, ix, log.New(io.Discard, "", 0), 0); err != nil {
		t.Fatal(err)
	}

	inbox := pageLinks(t, out, "s/INBOX/index.html")
	if len(inbox) != 3 || inbox[0] != "s/INBOX/2026-03-02-lunch.html" {
		t.Fatalf("INBOX links %q, want its own email first, then Receipt and its zip", inbox)
	}
	for _, c := range []struct{ page string }{{"s/Sent Mail/index.html"}, {"s/Labels/Work/index.html"}, {"s/INBOX/index.html"}} {
		links := pageLinks(t, out, c.page)
		var sawReceipt, sawZip bool
		for _, l := range links {
			if !strings.HasPrefix(l, "s/") {
				t.Fatalf("%s links outside its store's folder: %q", c.page, l)
			}
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(l))); err != nil {
				t.Fatalf("%s: link %q leads to no file: %v", c.page, l, err)
			}
			sawReceipt = sawReceipt || l == "s/All Mail/2026-03-01-receipt.html"
			sawZip = sawZip || l == "s/All Mail/2026-03-01-receipt-attachments.zip"
		}
		if !sawReceipt || !sawZip {
			t.Fatalf("%s does not link Receipt and its zip in All Mail: %q", c.page, links)
		}
		data, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(c.page)))
		s := string(data)
		if !strings.Contains(s, "Receipt</a> <span class=\"muted\">(saved in All Mail)</span>") {
			t.Fatalf("%s does not say where Receipt is saved", c.page)
		}
		if strings.Count(s, "<script") != 1 || strings.Contains(strings.ToLower(s), "javascript:") {
			t.Fatalf("%s carries a script beyond the folder filter's one", c.page)
		}
	}
	sent, _ := os.ReadFile(filepath.Join(out, "s", "Sent Mail", "index.html"))
	if !strings.Contains(string(sent), "1 message(s)") {
		t.Fatalf("Sent Mail's page does not count its one email")
	}
	if strings.Count(string(sent), "Receipt</a>") != 1 {
		t.Fatal("Sent Mail lists Receipt more or less than once")
	}
	inboxPage, _ := os.ReadFile(filepath.Join(out, "s", "INBOX", "index.html"))
	if !strings.Contains(string(inboxPage), "2 message(s)") {
		t.Fatal("INBOX's page does not count its own email and Receipt")
	}
	root, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if !strings.Contains(string(root), ">s/Sent Mail</a><span class=\"c\">1</span>") {
		t.Fatalf("the root page does not list Sent Mail with its count")
	}
	// The message page itself is untouched: All Mail lists Receipt in its own folder.
	all := pageLinks(t, out, "s/All Mail/index.html")
	if len(all) != 2 || all[0] != "s/All Mail/2026-03-01-receipt.html" {
		t.Fatalf("All Mail links %q", all)
	}
}

// covers: MA-1224, R45, R4
// GD-21 (a): a member row is linked only from the email's own recorded path, inside the same
// store's folder, and its page only in a clean folder of that store. A member folder that climbs
// out ("../../x"), is absolute-looking or holds a control character, and a member whose email's
// path is in another store's folder, are each left out: no page is written for them and nothing
// is written outside the archive. The healthy member of the same index still gets its page (the
// positive twin).
func TestAHostileMemberRowIsLeftOut(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "archive")
	os.MkdirAll(out, 0o755)
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	m := &model.Message{Subject: "Receipt", Received: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), PlainBody: "b"}
	ix.Add("s", []string{"All Mail"}, m, "s/All Mail/r.html", "k1")
	ix.Add("t", []string{"Elsewhere"}, &model.Message{Subject: "Other", PlainBody: "b"}, "t/Elsewhere/o.html", "k2")
	ix.SetMembers("k1", "s", []string{"Sent Mail", "../../escaped", "/abs", "bad\x01name", "a//b"})
	ix.SetMembers("k2", "s", []string{"Stolen"})
	ix.Flush()
	if err := Generate(out, ix, log.New(io.Discard, "", 0), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "s", "Sent Mail", "index.html")); err != nil {
		t.Fatalf("the healthy member folder has no page: %v", err)
	}
	for _, p := range []string{filepath.Join(parent, "escaped"), filepath.Join(out, "escaped"), filepath.Join(out, "s", "Stolen")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("a hostile member row wrote %s", p)
		}
	}
	var dirs []string
	filepath.WalkDir(parent, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			rel, _ := filepath.Rel(parent, p)
			dirs = append(dirs, filepath.ToSlash(rel))
		}
		return nil
	})
	want := ".|archive|archive/s|archive/s/All Mail|archive/s/Sent Mail|archive/t|archive/t/Elsewhere"
	if strings.Join(dirs, "|") != want {
		t.Fatalf("folders after the pages: %q, want %q", strings.Join(dirs, "|"), want)
	}
}

// covers: MA-1224, R45
// GD-21 (b): the encrypted reader renders the same pages in memory (Render), with the same
// relative link from Sent Mail's page to the email in All Mail.
func TestTheEncryptedReadersPagesCarryTheSameLink(t *testing.T) {
	_, ix := memberArchive(t)
	pages, err := Render(ix, 0)
	if err != nil {
		t.Fatal(err)
	}
	sent, ok := pages["s/Sent Mail/index.html"]
	if !ok {
		t.Fatalf("no Sent Mail page among %d rendered", len(pages))
	}
	if !strings.Contains(string(sent), `href="../All%20Mail/2026-03-01-receipt.html"`) {
		t.Fatal("the rendered Sent Mail page does not link Receipt in All Mail relatively")
	}
}
