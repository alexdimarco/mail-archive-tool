package desktop

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// F6 of the friction review: the folder tree and the page's announcements are reachable with
// the keyboard and a screen reader. The page scripts cannot run here (no browser, no JavaScript
// engine), so these read the page the server sends and the script it serves, as the walk did;
// the keyboard-only and screen-reader click-through is owed in a real browser.

// byID finds the element with id in an HTML page.
func byID(t *testing.T, page, id string) *xhtml.Node {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	var found *xhtml.Node
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" && a.Val == id {
					found = n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		t.Fatalf("the page has no #%s", id)
	}
	return found
}

func attr(n *xhtml.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}

// labelled reports whether the element has an accessible name: an aria-label, or a label for it.
func labelled(page string, n *xhtml.Node) bool {
	if attr(n, "aria-label") != "" {
		return true
	}
	id := attr(n, "id")
	return id != "" && strings.Contains(page, `<label for="`+id+`"`)
}

// covers: MA-407, MA-446, R12, S52
// F6: the page has the names and live regions a screen reader needs (the folder tree and the
// message list are named, the filter is labelled, the selection count, the note, the preview and
// every card's result are announced, the progress bar is a progressbar with a name), and the
// scripts build the tree out of buttons and labelled checkboxes: a folder's name and its
// expand arrow are real buttons, each checkbox is named from its row, the tree keeps what is
// open and where the focus was when it is redrawn, and the history is not rebuilt under a
// focused card (it keeps the focus on the button that was pressed).
func TestFricTheTreeAndTheAnnouncementsAreReachableWithoutAMouse(t *testing.T) {
	e := newWsEnv(t)
	ws := e.get("/workspace").Body.String()
	for _, c := range []struct{ id, attr, why string }{
		{"tree", "aria-label", "the folder tree is named"},
		{"msgs", "aria-label", "the message list is named"},
		{"selCount", "aria-live", "the selection count is announced"},
		{"selNote", "aria-live", "the note under Selected is announced"},
		{"shown", "aria-live", "'Showing N of M' is announced"},
		{"previewBody", "aria-live", "the preview is announced"},
		{"mbox", "aria-live", "the mailbox line is announced"},
	} {
		if attr(byID(t, ws, c.id), c.attr) == "" {
			t.Errorf("/workspace: #%s has no %s: %s", c.id, c.attr, c.why)
		}
	}
	if !labelled(ws, byID(t, ws, "filter")) {
		t.Error("/workspace: the filter has only a placeholder")
	}

	ex := e.get("/exports").Body.String()
	bar := byID(t, ex, "runBarWrap")
	if attr(bar, "role") != "progressbar" || attr(bar, "aria-label") == "" || attr(bar, "aria-valuemin") == "" || attr(bar, "aria-valuemax") == "" {
		t.Errorf("/exports: the progress bar is not a named progressbar: %v", bar.Attr)
	}
	if attr(byID(t, ex, "lastBody"), "aria-live") == "" {
		t.Error("/exports: how an export ended is not announced")
	}
	if attr(byID(t, ex, "history"), "aria-label") == "" {
		t.Error("/exports: the history is not named")
	}

	for _, want := range []string{
		"el('button',f.name,", "el('button',kids.length", // a folder's name and its arrow are buttons
		"setAttribute('aria-expanded'", "setAttribute('aria-label','Select ", // the arrow says open/closed, checkboxes are named
		"st.open[", "dataset.key", // what is open, and where the focus was, survive a redraw
		// the folder checkbox's own name (its words differ from the message row's, which the line
		// above also matches), and that of a folder covered by its parent
		"cb.setAttribute('aria-label','Select '+f.name+' ('+fmt(f.count)+' messages)'", "included with its parent folder",
		"cb.setAttribute('aria-label','Select the message '",
	} {
		if !strings.Contains(workspaceJS, want) {
			t.Errorf("workspace.js does not contain %q", want)
		}
	}
	if strings.Contains(workspaceJS, "el('span',kids.length") || strings.Contains(workspaceJS, "el('span',f.name,'name") {
		t.Error("workspace.js still builds a folder's name or arrow as a click-only span")
	}
	for _, want := range []string{
		"aria-valuenow", "aria-valuetext", // the progress bar
		"activeElement", "dataset.act", // no rebuild under a focused card; the pressed button keeps the focus
	} {
		if !strings.Contains(exportsJS, want) {
			t.Errorf("exports.js does not contain %q", want)
		}
	}
}
