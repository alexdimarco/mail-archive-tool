package desktop

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-407, MA-408, R19, R12, S52
// The workspace page's Source control (design §4.1, §7.6): a select offering Microsoft 365, the
// MailArchive archive and Another archive folder… (read-only), the folder's path box with Browse, the
// archive's line and the list's note, each an element the script binds; the script sends the source with
// every request, offers the panel note ruling Q8 words ("Exports the original EML where your archive kept
// one … otherwise the archived page and attachments"), and re-enables Load folders after each of the
// archive's refusals (R-o, R-o3, R-o4, R-p, R-l2); the page still has no mailbox-address input.
func TestWorkspacePageOffersTheThreeSources(t *testing.T) {
	e := newWsEnv(t)
	page := e.get("/workspace").Body.String()
	root, _ := xhtml.Parse(strings.NewReader(page))
	var opts []string
	ids := map[string]bool{}
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" {
					ids[a.Val] = true
				}
			}
			if n.Data == "option" {
				var v string
				for _, a := range n.Attr {
					if a.Key == "value" {
						v = a.Val
					}
				}
				opts = append(opts, v+"="+strings.TrimSpace(n.FirstChild.Data))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	want := []string{"m365=Microsoft 365 — your mailbox", "archive=MailArchive archive", "archive-other=Another archive folder… (read-only)"}
	if strings.Join(opts, "|") != strings.Join(want, "|") {
		t.Errorf("the Source control offers %v, want %v", opts, want)
	}
	for _, id := range []string{"srcKind", "otherPath", "browseOther", "archLine", "listNote", "m365Part", "archivePart", "otherPart", "otherNote", "panelNote", "loadFolders"} {
		if !ids[id] {
			t.Errorf("the page has no #%s", id)
		}
	}
	for _, s := range []string{"srcReq(", "otherArchive:p", "api/workspace/browse", "'R-o'", "'R-o3'", "'R-o4'", "'R-p'", "'R-l2'", "d.listUnavailable",
		"Exports the original EML where your archive kept one", "otherwise the archived page and attachments", "Always writes a manifest with hashes"} {
		if !strings.Contains(workspaceJS, s) {
			t.Errorf("workspace.js does not carry %q", s)
		}
	}
	if !strings.Contains(page, "nothing is ever written in this folder") {
		t.Error("the page does not say that another archive folder is read-only")
	}
	if !strings.Contains(page, "Shared mailboxes aren't available") {
		t.Error("the Microsoft 365 part no longer says shared mailboxes aren't available")
	}
}

// covers: MA-407, R19, S52
// Mail-derived text from an archive (a subject, a sender, a folder name holding markup) reaches the page
// only as JSON-escaped strings, with nosniff and no-store (X15), in the tree, the list and the preview.
func TestArchiveMailTextIsInert(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	const evil = `<img src=x onerror=alert(1)>`
	key := f.key("hello")
	f.edit(func(m *state.Manifest) {
		r, _ := m.Get(key)
		r.Folder, r.Subject = evil, evil
		m.Add(key, r)
	})
	db, err := sqlOpen(e.out + "/search.db") // the index says the same (a capture writes both)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`PRAGMA journal_mode=DELETE`} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE docs SET folder = ?, subject = ?, sender_name = ? WHERE key = ?`, evil, evil, evil, key); err != nil {
		t.Fatal(err)
	}
	db.Close()
	arch := archSource("archive", e.out)
	for _, call := range []struct {
		route string
		body  map[string]any
	}{
		{"/api/workspace/folders", map[string]any{"source": arch}},
		{"/api/workspace/messages", map[string]any{"folder": "alice/" + evil, "source": arch}},
	} {
		rec := e.postRaw(call.route, call.body)
		raw := rec.Body.String()
		if rec.Code != 200 || strings.Contains(raw, "<img") || !strings.Contains(raw, "\\u003cimg src=x onerror=alert(1)\\u003e") {
			t.Errorf("%s: %d: the markup is not a JSON-escaped string: %.300s", call.route, rec.Code, raw)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: headers %v", call.route, rec.Header())
		}
	}
	_, res := e.preview(e.archSel(arch, entry("alice/"+evil, false)))
	if raw, _ := json.Marshal(res); strings.Contains(string(raw), "<img") {
		t.Errorf("the preview result carries raw markup: %s", raw)
	}
}

// covers: MA-435, MA-522, R20, R29, S52
// The archive snapshot is released after 30 idle minutes, with the session's tree, seen-set and cursors
// (XC18), and no index handle is held: the next request is the page being out of date, never a stale read.
func TestArchiveSessionIsReleasedWhenIdle(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	sel := e.archSel(arch, entry("alice", true))
	e.ok("/api/workspace/count", sel)
	e.d.ws.reap(time.Now().Add(sessionIdle - time.Minute)) // not yet
	e.ok("/api/workspace/count", sel)
	e.d.ws.reap(time.Now().Add(sessionIdle + time.Minute))
	code, out := e.post("/api/workspace/count", sel)
	refusedBody(t, code, out, "R-j2", "wasn't listed in this session")
	if e.d.ws.archNow() != nil {
		t.Error("the archive session is still held after it was released")
	}
	e.archFolders(arch) // Load folders again brings it back
	assure.Reached(t, e.ok("/api/workspace/count", sel)["count"], "the count after the reload")
}
