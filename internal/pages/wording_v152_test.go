package pages

import (
	"strings"
	"testing"
)

// covers: MA-940, R7, S27
// The archive's own pages point at what works (the wording pass before 1.5.2). Positive
// twin first: a one-page folder has no "Filter matches this page only" line at all. A
// big folder's line names MailArchive Desktop and a `serve -out` that works from any
// folder (an encrypted archive's reader shows the same folder pages, phase3 §7.2, so
// its own line is held for a design call).
// The plain front page says the report opens in a spreadsheet,
// not in the search page, and names the Desktop's search; the encrypted front page names
// the Desktop's Check integrity and a verify command that can run (it needs -out).
func TestTheArchivePagesPointAtWhatWorks(t *testing.T) {
	one, err := render(folderTemplate, folderPageData{Folder: "Inbox", Rows: []row{{File: "a.html"}}, Total: 1, Page: 1, Pages: 1, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(one), "Filter matches this page only") {
		t.Fatal("positive twin: a one-page folder shows the big-folder line")
	}
	big := folderPageData{Folder: "Inbox", Rows: []row{{File: "a.html"}}, Total: 6000, Page: 1, Pages: 2, Depth: 1, NextHref: "index-2.html"}
	plain, err := render(folderTemplate, big)
	if err != nil {
		t.Fatal(err)
	}
	if want := `Filter matches this page only (1 of 6000) and only the columns shown. To search the whole archive, open it in MailArchive Desktop ("Open an archive folder…"), or run <code>mailarchive serve -out</code> with the archive's top folder, or <code>rg -i "your text"</code> in that folder to search message bodies.`; !strings.Contains(string(plain), want) {
		t.Errorf("a plain big folder's line lacks %q:\n%s", want, plain)
	}

	root, err := render(rootTemplate, rootPageData{Gaps: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<a href="attachments-report.tsv">Open the report</a> (2 item(s); each row says why it is missing and what to do about it). It is the file attachments-report.tsv in this archive's folder and opens in a spreadsheet; the MailArchive search page cannot show it. Status &amp; health in MailArchive Desktop, or <code>mailarchive status -out .</code> in this folder, says the same in plain words.`,
		`<b>Full-text search:</b> open this archive in MailArchive Desktop ("Open an archive folder…"), or run <code>mailarchive serve -out .</code> in this folder and open the link it prints.`,
	} {
		if !strings.Contains(string(root), want) {
			t.Errorf("the plain front page lacks %q:\n%s", want, root)
		}
	}
	encRoot, err := render(rootTemplate, rootPageData{Encrypted: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := `<b>Check the files are intact:</b> in MailArchive Desktop, choose Maintenance → Check integrity, or at a terminal run <code>mailarchive verify -out</code> with this archive's folder.`; !strings.Contains(string(encRoot), want) {
		t.Errorf("the encrypted front page lacks %q:\n%s", want, encRoot)
	}
}
