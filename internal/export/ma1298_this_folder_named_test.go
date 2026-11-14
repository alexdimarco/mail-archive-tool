package export

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-1298, R45
// A Gmail email's message page names the folder its "This folder" link goes to, "This folder: All
// Mail" (design GD-18b, the B3 friction review's F10), so a message reached from Sent Mail's or a
// label's page does not silently lead to All Mail. Positive twin first: a page with no folder name
// in its context (every archive that is not a switched-on Gmail account) keeps "This folder"
// exactly as before, so its pages' bytes do not change. The name is escaped.
func TestTheMessagePageNamesItsFolderForGmail(t *testing.T) {
	m := &model.Message{Subject: "s", SenderEmail: "a@example.com", PlainBody: "b"}
	plain, err := RenderWith(m, RenderContext{RootRel: "../../", FolderIndexRel: "index.html"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain.HTML), `<a href="index.html">This folder</a>`) {
		t.Fatalf("positive twin: the link changed for a page with no folder name:\n%s", plain.HTML)
	}
	named, err := RenderWith(m, RenderContext{RootRel: "../../", FolderIndexRel: "index.html", FolderName: "All Mail"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(named.HTML), `<a href="index.html">This folder: All Mail</a>`) {
		t.Fatalf("the Gmail page does not name its folder:\n%s", named.HTML)
	}
	hostile, _ := RenderWith(m, RenderContext{RootRel: "../../", FolderIndexRel: "index.html", FolderName: `<b>x</b>`})
	if strings.Contains(string(hostile.HTML), "<b>x</b>") {
		t.Fatal("the folder name is not escaped")
	}
}
