package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// The Extraction workspace's session, selection, preview and run, end to end through the
// dashboard's own handler against a fake Microsoft 365 (ws_testutil_test.go).

// treeState maps every entry under root to a digest of its bytes (a directory to "dir",
// a symlink to its target), skipping the named relative paths: two states are equal when
// nothing under root was written, made or removed.
func treeState(t *testing.T, root string, skip ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		for _, s := range skip {
			if filepath.ToSlash(rel) == s {
				return nil
			}
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			tgt, _ := os.Readlink(p)
			out[filepath.ToSlash(rel)] = "link->" + tgt
		case d.IsDir():
			out[filepath.ToSlash(rel)] = "dir"
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h := sha256.Sum256(b)
			out[filepath.ToSlash(rel)] = hex.EncodeToString(h[:])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func diffKeys(a, b map[string]string) []string {
	var out []string
	for k, v := range b {
		if a[k] != v {
			out = append(out, k)
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, "-"+k)
		}
	}
	return out
}

// refusedBody asserts a refusal: the status the page sends refusals with, its typed id and
// that its words name each needle (assure.Refused).
func refusedBody(t *testing.T, code int, out map[string]any, id string, names ...string) {
	t.Helper()
	refusedAs(t, code, statusRefused, out, id, names...)
}

func refusedAs(t *testing.T, code, want int, out map[string]any, id string, names ...string) {
	t.Helper()
	if out["ok"] != false || out["id"] != id {
		t.Fatalf("want refusal %s, got %d %v", id, code, out)
	}
	msg, _ := out["error"].(string)
	assure.Refused(t, code, msg, assure.Code(want), assure.Names(names...))
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// handlesOf lists a folder's first page and returns its rows' handles.
func handlesOf(t *testing.T, e *wsEnv, folder string) []string {
	t.Helper()
	h, _ := e.rows(folder)
	return h
}

// previewFailure starts a preview that is refused after it started and returns the job's
// refusal as the status endpoint gives it.
func (e *wsEnv) previewFailure(sel map[string]any) map[string]any {
	e.t.Helper()
	id := e.ok("/api/workspace/preview", sel)["previewId"].(string)
	for i := 0; i < 300; i++ {
		_, out := e.post("/api/workspace/preview-status", map[string]any{"previewId": id})
		if out["state"] == "failed" {
			return out
		}
		if out["state"] == "done" {
			e.t.Fatalf("the preview was meant to be refused but finished: %v", out)
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("the preview neither finished nor failed")
	return nil
}

// covers: MA-506, MA-403, R17, R9, R29, S52
// The workspace's Graph session is the device (/me) client even when the saved config says
// auth=app: a whole Microsoft 365 folder load, preview and export, with the saved sign-in
// due for refresh, issues only GET (and the refresh-token POST to the token endpoint, never
// the app-only client-credentials grant), and no request ever addresses /users/.
func TestWorkspaceSessionIsTheDeviceClientEvenWithAppAuth(t *testing.T) {
	e := newWsEnv(t)
	e.writeToken("alice@contoso.org", -time.Hour) // the first request refreshes it
	if c := e.d.cfg.load(); c == nil || c.Auth != "app" {
		t.Fatalf("the fixture's config should say auth=app: %+v", c)
	}
	ids := e.folders()
	id, res := e.preview(e.selection(entry(ids["Sent Items"], false)))
	if res["count"] != float64(5) {
		t.Fatalf("the preview found %v messages, want 5", res["count"])
	}
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["exported"] != float64(5) {
		t.Fatalf("the export ended as %v", last)
	}
	for _, r := range assure.Reached(t, e.fg.requests(), "requests the workspace made") {
		if strings.Contains(r, "/users/") {
			t.Errorf("a request addressed /users/: %s", r)
		}
		if !strings.HasPrefix(r, "GET ") && r != "POST /token" {
			t.Errorf("a request that is not a GET: %s", r)
		}
	}
	for _, g := range assure.Reached(t, e.fg.grantList(), "token requests") {
		if g != "refresh_token" {
			t.Errorf("the token endpoint was asked for a %q grant, want only refresh_token (never client_credentials)", g)
		}
	}
}

// covers: MA-449, R17, R29
// The Graph session is built on the process-lifetime context, never on a request's: a
// session built inside a request whose context is cancelled the moment it answers, with a
// token that is due for refresh at every use (a 2 s token), still refreshes for a later
// list and for a whole run (13 messages, each refreshing the token).
func TestSessionOutlivesTheRequestThatBuiltIt(t *testing.T) {
	e := newWsEnv(t)
	e.fg.mutate(func() { e.fg.ttl = 2 })
	e.writeToken("alice@contoso.org", -time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	rec := e.postCtx(ctx, "/api/workspace/folders", nil)
	cancel() // the request that built the session is over
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("the folder load: %d %s", rec.Code, rec.Body.String())
	}
	before := e.fg.tokenCount()
	if _, out := e.rows("F-INBOX"); len(out["rows"].([]any)) != 7 {
		t.Fatalf("a later list after the session's request ended: %v", out)
	}
	id, _ := e.preview(e.selection(entry("F-INBOX", true)))
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["exported"] != float64(13) {
		t.Fatalf("a run on a session whose request had ended: %v", last)
	}
	if n := e.fg.tokenCount() - before; n < 5 {
		t.Errorf("only %d token refreshes: the 2 s token was not refreshed through the session's own context", n)
	}
}

// covers: MA-410, R29, R12
// A pick or exclusion handle the session never listed is refused (R-j2), and a folder id
// that is not in the session's cached tree, `recoverableitemsdeletions` and `..` included,
// is refused, each with no Graph request made; more than 10,000 picks plus exclusions is
// R-j; 10,000 handles fit in 1 MiB even for a folder of ~150-character Graph ids; and a
// body over 1 MiB is a 413 worded as a refusal. The positive twin, listed handles, first.
func TestSelectionHandlesAreAdmittedAndBounded(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	handles := handlesOf(t, e, ids["Inbox"])
	if len(handles) != 7 {
		t.Fatalf("the Inbox lists %d rows, want 7", len(handles))
	}
	sel := e.selection()
	sel["messages"] = []any{map[string]any{"h": handles[0]}, map[string]any{"h": handles[1]}}
	if out := e.ok("/api/workspace/count", sel); out["count"] != float64(2) {
		t.Fatalf("the positive twin: %v", out)
	}

	requests := len(e.fg.requests())
	code, out := e.post("/api/workspace/count", e.selection(entry(ids["Inbox"], false, "zzzzzzzz")))
	refusedBody(t, code, out, "R-j2", "wasn't listed in this session", "Reload the folder")
	pick := e.selection()
	pick["messages"] = []any{map[string]any{"h": "nope1234"}}
	code, out = e.post("/api/workspace/count", pick)
	refusedBody(t, code, out, "R-j2", "wasn't listed in this session", "Reload the folder")
	out = e.previewFailure(pick)
	refusedAs(t, 200, 200, out, "R-j2", "wasn't listed in this session")
	for _, bad := range []string{"recoverableitemsdeletions", "..", "AAMk-never-listed"} {
		code, out = e.post("/api/workspace/count", e.selection(entry(bad, true)))
		refusedBody(t, code, out, "R-j2", "A folder in this selection wasn't listed in this session", "Load folders again")
		code, out = e.post("/api/workspace/messages", map[string]any{"folder": bad})
		refusedBody(t, code, out, "R-j2", "wasn't listed in this session")
	}
	if got := len(e.fg.requests()); got != requests {
		t.Errorf("a refused selection reached Graph: %d new request(s): %v", got-requests, e.fg.requests()[requests:])
	}

	// 10,001 messages with ~150-character ids
	e.fg.addFolder("F-BIG", "Big", "", 0)
	e.fg.mutate(func() {
		big := e.fg.folder("F-BIG")
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 10001; i++ {
			id := fmt.Sprintf("AAMkAGI2NzM3YTM1LTU1ZWMtNDlmOS04YzQxLTFhNWNmZDU3N2I4OQBGAAAAAAB%s%05d", strings.Repeat("Q", 80), i)
			big.Msgs = append(big.Msgs, fgMsg{ID: id, Subject: "s", From: "a@example.com", Received: base.Add(-time.Duration(i) * time.Minute), Size: 10})
		}
	})
	e.folders()
	var all []string
	var cursor, after string
	for {
		out := e.ok("/api/workspace/messages", map[string]any{"folder": "F-BIG", "cursor": cursor, "after": after})
		rows := out["rows"].([]any)
		if len(rows) > 100 {
			t.Fatalf("a page of %d rows", len(rows))
		}
		for _, r := range rows {
			all = append(all, r.(map[string]any)["h"].(string))
		}
		if out["more"] != true {
			break
		}
		cursor, after = out["cursor"].(string), all[len(all)-1]
	}
	if len(all) != 10001 {
		t.Fatalf("listed %d rows", len(all))
	}
	pickN := func(n int) map[string]any {
		s := e.selection()
		var ms []any
		for _, h := range all[:n] {
			ms = append(ms, map[string]any{"h": h})
		}
		s["messages"] = ms
		return s
	}
	body, _ := json.Marshal(pickN(10000))
	if len(body) >= 1<<20 {
		t.Fatalf("10,000 handles are %d bytes: they must fit in 1 MiB", len(body))
	}
	if out := e.ok("/api/workspace/count", pickN(10000)); out["count"] != float64(10000) {
		t.Errorf("10,000 listed picks counted %v", out["count"])
	}
	code, out = e.post("/api/workspace/count", pickN(10001))
	refusedBody(t, code, out, "R-j", "most one selection can hold", "Select current folder")

	rec := e.postRaw("/api/workspace/count", `{"dest":"`+strings.Repeat("x", 1<<20+10)+`"}`)
	var tooBig map[string]any
	json.Unmarshal(rec.Body.Bytes(), &tooBig)
	refusedAs(t, rec.Code, http.StatusRequestEntityTooLarge, tooBig, "R-j", "Select current folder")
}

// covers: MA-409, R29, S52
// A selection resolves to the folder plus every descendant of a freshly fetched tree
// (including a folder made after the load; a vanished one is a preview WARN), and counts
// come from the cached tree with no Graph request: a subtree entry counts its descendants,
// a plain entry its folder alone, exclusions come off, picks inside a selected subtree are
// counted once and picks outside add, and overlapping entries are de-duplicated; a whole
// folder's request holds no per-message list.
func TestSelectionResolutionAndCounts(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	inbox, sent, clients := handlesOf(t, e, ids["Inbox"]), handlesOf(t, e, ids["Sent Items"]), handlesOf(t, e, ids["Clients"])
	pick := func(sel map[string]any, hs ...string) map[string]any {
		var ms []any
		for _, h := range hs {
			ms = append(ms, map[string]any{"h": h})
		}
		sel["messages"] = ms
		return sel
	}
	for _, c := range []struct {
		name string
		sel  map[string]any
		want float64
	}{
		{"a subtree entry: the folder and every descendant", e.selection(entry(ids["Inbox"], true)), 13},
		{"a plain entry: the folder alone", e.selection(entry(ids["Inbox"], false)), 7},
		{"overlapping entries are de-duplicated", e.selection(entry(ids["Inbox"], true), entry(ids["Clients"], true), entry(ids["Acme"], false)), 13},
		{"two folders", e.selection(entry(ids["Inbox"], false), entry(ids["Sent Items"], false)), 12},
		{"exclusions come off", e.selection(entry(ids["Inbox"], true, inbox[0], inbox[1])), 11},
		{"a pick inside a selected subtree is counted once", pick(e.selection(entry(ids["Inbox"], true)), clients[0]), 13},
		{"a pick outside adds", pick(e.selection(entry(ids["Inbox"], true)), sent[0]), 14},
		{"picks alone", pick(e.selection(), sent[0], sent[1], inbox[2]), 3},
	} {
		before := len(e.fg.requests())
		out := e.ok("/api/workspace/count", c.sel)
		if out["count"] != c.want {
			t.Errorf("%s: counted %v, want %v", c.name, out["count"], c.want)
		}
		if got := len(e.fg.requests()); got != before {
			t.Errorf("%s: a count made %d Graph request(s)", c.name, got-before)
		}
	}
	body, _ := json.Marshal(e.selection(entry(ids["Inbox"], true)))
	if len(body) > 400 {
		t.Errorf("a whole folder's selection is %d bytes: it must hold no per-message list", len(body))
	}

	// resolution: a folder made after the load is in a subtree; a vanished one is a WARN
	e.fg.addFolder("F-NEW", "Newbie", "F-INBOX", 2)
	_, res := e.preview(e.selection(entry(ids["Inbox"], true)))
	if res["count"] != float64(15) {
		t.Errorf("the preview found %v messages, want 15 (the Inbox, its subfolders and the one made after the load)", res["count"])
	}
	names := map[string]bool{}
	for _, f := range res["folders"].([]any) {
		names[f.(map[string]any)["folder"].(string)] = true
	}
	for _, want := range []string{"Inbox", "Inbox/Clients", "Inbox/Clients/Acme", "Inbox/Newbie"} {
		if !names[want] {
			t.Errorf("the preview's folders lack %q: %v", want, names)
		}
	}
	e.fg.removeFolder("F-SENT")
	_, res = e.preview(e.selection(entry(ids["Inbox"], false), entry(ids["Sent Items"], false)))
	if res["count"] != float64(7) || !strings.Contains(strings.Join(toStrings(res["warnings"]), "\n"), "Sent Items is no longer in the mailbox") {
		t.Errorf("a vanished folder: %v %v, want 7 messages and a WARN naming Sent Items", res["count"], res["warnings"])
	}
}

// covers: MA-411, R29, R12
// A selection or preview made under signer A is refused after a sign-in as B (R-l, naming
// both accounts) and, once the sign-in is cleared, as not signed in (R-b); the session is
// dropped each time, so every outstanding selection and preview is refused, and a folder
// load after the change starts over under the new account.
func TestSelectionIsBoundToTheSigner(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	handles := handlesOf(t, e, ids["Inbox"])
	sel := e.selection(entry(ids["Inbox"], true))
	pid, _ := e.preview(sel)
	pick := e.selection()
	pick["messages"] = []any{map[string]any{"h": handles[0]}}
	if out := e.ok("/api/workspace/count", pick); out["count"] != float64(1) {
		t.Fatalf("the positive twin: %v", out)
	}

	// a sign-in as bob
	e.writeToken("bob@contoso.org", time.Hour)
	e.fg.mutate(func() { e.fg.upn = "bob@contoso.org" })
	e.d.signInChanged("bob@contoso.org")
	code, out := e.post("/api/workspace/count", pick)
	refusedBody(t, code, out, "R-l", "alice@contoso.org", "bob@contoso.org", "Load folders again")
	code, out = e.post("/api/exports/start", map[string]any{"previewId": pid})
	refusedBody(t, code, out, "R-l", "alice@contoso.org", "bob@contoso.org")
	code, out = e.post("/api/workspace/preview", sel)
	refusedBody(t, code, out, "R-l", "alice@contoso.org", "bob@contoso.org")
	e.d.ws.mu.Lock()
	dropped := e.d.ws.sess == nil
	e.d.ws.mu.Unlock()
	if !dropped {
		t.Error("the session survived a sign-in as another account")
	}
	if got := e.ok("/api/workspace/folders", nil); got["upn"] != "bob@contoso.org" {
		t.Errorf("a folder load after the change is for %v, want bob@contoso.org", got["upn"])
	}
	code, out = e.post("/api/workspace/count", pick) // the old selection is still alice's
	refusedBody(t, code, out, "R-l", "alice@contoso.org", "bob@contoso.org")

	// the sign-in cleared
	rec := e.postRaw("/api/clear-signin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear sign-in: %d %s", rec.Code, rec.Body.String())
	}
	e.d.signInChanged("")
	code, out = e.post("/api/workspace/count", pick)
	refusedBody(t, code, out, "R-b", "not signed in", "Sign in")
	code, out = e.post("/api/exports/start", map[string]any{"previewId": pid})
	refusedBody(t, code, out, "R-b", "Sign in")
}

// covers: MA-415, R29, R1, S52
// Reconciliation: when a folder's reported count changes between the reads before and
// after its listing the folder is listed again and the union taken (an arrival during a
// listing is found by the second one, and no WARN); a mismatch that persists on the second
// listing's own reads is a WARN naming the folder and the numbers; a stable folder whose
// reported count exceeds what it lists is read once, with only the info line and no re-list.
func TestPreviewReconcilesAFolderThatChanged(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	sent := ids["Sent Items"]

	// an arrival during the first listing: counts 5, 6, 6; the second listing sees 6
	lists := 0
	e.fg.mutate(func() {
		e.fg.counts = func(folder string, read int) int { return []int{5, 6, 6}[min(read, 3)-1] }
		e.fg.listFn = func(folder string, r *http.Request) {
			if folder != "F-SENT" {
				return
			}
			lists++
			if lists == 2 {
				e.fg.mutate(func() {
					f := e.fg.folder("F-SENT")
					f.Msgs = append([]fgMsg{{ID: "F-SENT-NEW", Subject: "arrived", From: "a@example.com", Received: time.Date(2026, 3, 29, 9, 0, 0, 0, time.UTC), Size: 30}}, f.Msgs...)
				})
			}
		}
	})
	_, res := e.preview(e.selection(entry(sent, false)))
	if res["count"] != float64(6) || res["reported"] != float64(6) {
		t.Errorf("after an arrival the preview found %v messages (reported %v), want 6", res["count"], res["reported"])
	}
	if w := res["warnings"].([]any); len(w) != 0 {
		t.Errorf("an arrival produced WARNs: %v", w)
	}
	if n := e.fg.readCount("F-SENT"); n != 3 {
		t.Errorf("a changed folder's count was read %d times, want 3 (before, after, after the second listing)", n)
	}

	// a mismatch that persists on the second listing's own reads: a WARN
	e.fg.mutate(func() {
		e.fg.reads = map[string]int{}
		e.fg.counts = func(folder string, read int) int { return 4 + read } // 5, 6, 7: always moving
		e.fg.listFn = nil
	})
	_, res = e.preview(e.selection(entry(sent, false)))
	warn := strings.Join(toStrings(res["warnings"]), "\n")
	for _, want := range []string{"Sent Items changed while it was being read twice in a row", "5, then 6, then 7", "Preview again for an exact copy"} {
		if !strings.Contains(warn, want) {
			t.Errorf("a persistent mismatch: %q lacks %q", warn, want)
		}
	}

	// a stable folder that reports more than it lists: one read pair, an info line, no WARN
	e.fg.mutate(func() {
		e.fg.reads, e.fg.counts = map[string]int{}, nil
		e.fg.folder("F-SENT").Bias = 2
	})
	_, res = e.preview(e.selection(entry(sent, false)))
	if n := e.fg.readCount("F-SENT"); n != 2 {
		t.Errorf("a stable folder's count was read %d times, want 2 (no re-list)", n)
	}
	if info := strings.Join(toStrings(res["info"]), "\n"); !strings.Contains(info, "Microsoft 365 reports 8 items in Sent Items; 6 are messages and will be copied.") {
		t.Errorf("the info line is %q", info)
	}
	if w := res["warnings"].([]any); len(w) != 0 {
		t.Errorf("a stable folder produced WARNs: %v", w)
	}
}

// covers: MA-511, R29, S52
// The list endpoint never returns more than 100 rows, even from a server that ignores
// $top (and the rest stays reachable), its cursor is opaque (it holds no Graph URL), the
// seen-set's byte cap holds under a large browse, and its retained bytes stay bounded;
// progress counters advance and an ETA is present while a run goes.
func TestListEndpointIsBoundedAndProgressAdvances(t *testing.T) {
	e := newWsEnv(t)
	e.fg.addFolder("F-LONG", "Long", "", 2500)
	e.fg.mutate(func() { e.fg.ignoreTop = true })
	e.folders()
	out := e.ok("/api/workspace/messages", map[string]any{"folder": "F-LONG"})
	if n := len(out["rows"].([]any)); n != 100 {
		t.Fatalf("a server that ignores $top made a page of %d rows, want the 100-row bound", n)
	}
	if c, _ := out["cursor"].(string); c == "" || out["more"] != true || strings.ContainsAny(c, "/?:") {
		t.Errorf("the cursor %q (more %v) is empty or looks like a URL", c, out["more"])
	}
	if s := fmt.Sprint(out); strings.Contains(s, "nextLink") || strings.Contains(s, e.fg.srv.URL) {
		t.Errorf("a Graph URL reached the page: %v", out)
	}
	// ...and every row is still reachable, once, through the cursors
	listed := map[string]bool{}
	var cursor string
	for pages := 0; pages < 40; pages++ {
		out := e.ok("/api/workspace/messages", map[string]any{"folder": "F-LONG", "cursor": cursor})
		rows := out["rows"].([]any)
		if len(rows) > 100 {
			t.Fatalf("page %d holds %d rows", pages, len(rows))
		}
		for _, r := range rows {
			listed[r.(map[string]any)["h"].(string)] = true
		}
		if out["more"] != true {
			break
		}
		cursor = out["cursor"].(string)
	}
	if len(listed) != 2500 {
		t.Errorf("the cursors reached %d distinct rows of 2,500", len(listed))
	}
	e.fg.mutate(func() { e.fg.ignoreTop = false })

	// the seen-set's cap holds under a large browse
	sess := e.d.ws.sess
	sess.release() // a fresh seen-set, so the browse below adds every row to it
	e.folders()
	sess.seen.maxBytes = 150 << 10
	cursor = ""
	for {
		out := e.ok("/api/workspace/messages", map[string]any{"folder": "F-LONG", "cursor": cursor})
		if out["more"] != true {
			break
		}
		cursor = out["cursor"].(string)
	}
	n, bytes := sess.seen.len()
	if bytes > sess.seen.maxBytes || n >= 2500 || n == 0 {
		t.Errorf("the seen-set holds %d entries, %d bytes: the byte cap %d was not held", n, bytes, sess.seen.maxBytes)
	}
	sess.release()
	e.folders()
	cursor = ""
	for {
		out := e.ok("/api/workspace/messages", map[string]any{"folder": "F-LONG", "cursor": cursor})
		if out["more"] != true {
			break
		}
		cursor = out["cursor"].(string)
	}
	if _, bytes := sess.seen.len(); bytes > 4<<20 {
		t.Errorf("browsing 2,500 rows retains %d bytes", bytes)
	}

	// progress advances, with an ETA
	ids := e.folders()
	e.fg.mutate(func() {
		for _, m := range e.fg.folder("F-INBOX").Msgs {
			id := m.ID
			e.fg.valueFn[id] = func(w http.ResponseWriter, r *http.Request) { time.Sleep(120 * time.Millisecond); w.Write(fgBody(id)) }
		}
	})
	pid, _ := e.preview(e.selection(entry(ids["Inbox"], false)))
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	seenDone, seenETA, logged := map[float64]bool{}, false, 0
	for i := 0; i < 200; i++ {
		_, p := e.post("/api/exports/progress", nil)
		if p["running"] == false {
			break
		}
		seenDone[p["done"].(float64)] = true
		if p["etaSeconds"].(float64) > 0 {
			seenETA = true
		}
		if l, ok := p["log"].([]any); ok {
			logged = len(l)
		}
		time.Sleep(40 * time.Millisecond)
	}
	if len(seenDone) < 3 {
		t.Errorf("progress advanced through %d distinct counts, want several", len(seenDone))
	}
	if !seenETA {
		t.Error("no ETA while the export ran")
	}
	if logged == 0 || logged > exportLogMax {
		t.Errorf("the progress log holds %d lines", logged)
	}
	e.waitRun()
}

// covers: MA-522, R29, R12, S52
// Session lifecycle: "Load more" after the cursor's TTL continues by a keyset query with no
// duplicate or skipped row on a static folder, ties at the page boundary included; a cursor
// that is gone (and no row to continue from) is R-x; a POST from a page made before a
// dashboard restart is R-y, never a bare 403; after 30 idle minutes (clock-injected) the
// tree, the seen-set and the cursors are released, and are not while an export runs;
// resolving a selection never adds to the seen-set, and the handles of the live selection
// survive LRU pressure that evicts the others.
func TestSessionLifecycle(t *testing.T) {
	e := newWsEnv(t)
	e.fg.addFolder("F-LONG", "Long", "", 250)
	e.fg.mutate(func() { // messages 99, 100 and 101 share one timestamp: a tie across the page boundary
		long := e.fg.folder("F-LONG")
		tie := long.Msgs[99].Received
		long.Msgs[100].Received, long.Msgs[101].Received = tie, tie
		for i := 102; i < 250; i++ {
			long.Msgs[i].Received = tie.Add(-time.Duration(i-100) * time.Hour)
		}
	})
	e.folders()
	clock := time.Now()
	e.d.now = func() time.Time { return clock }

	page := func(folder, cursor, after string) map[string]any {
		return e.ok("/api/workspace/messages", map[string]any{"folder": folder, "cursor": cursor, "after": after})
	}
	rowIDs := func(out map[string]any) (hs []string) {
		for _, r := range out["rows"].([]any) {
			hs = append(hs, r.(map[string]any)["h"].(string))
		}
		return
	}
	p1 := page("F-LONG", "", "")
	got := rowIDs(p1)
	if len(got) != 100 || p1["more"] != true {
		t.Fatalf("page 1: %d rows", len(got))
	}
	cursor, after := p1["cursor"].(string), got[99]
	clock = clock.Add(11 * time.Minute) // past the cursor's 10-minute TTL
	for i := 0; i < 5; i++ {
		out := page("F-LONG", cursor, after)
		hs := rowIDs(out)
		got = append(got, hs...)
		if out["more"] != true {
			break
		}
		cursor, after = out["cursor"].(string), hs[len(hs)-1]
	}
	if len(got) != 250 {
		t.Fatalf("a keyset continuation listed %d rows, want 250", len(got))
	}
	sess := e.d.ws.sess
	seen := map[string]bool{}
	for i, h := range got {
		if seen[h] {
			t.Fatalf("row %s was listed twice", h)
		}
		seen[h] = true
		if ent, _ := sess.seen.peek(h); ent.id != fmt.Sprintf("F-LONG-%04d", i+1) {
			t.Fatalf("row %d is %s, want F-LONG-%04d: the continuation skipped or reordered", i, ent.id, i+1)
		}
	}

	// a cursor that is gone, with nothing to continue from: R-x
	code, out := e.post("/api/workspace/messages", map[string]any{"folder": "F-LONG", "cursor": "gone-cursor"})
	refusedBody(t, code, out, "R-x", "This list expired", "Long")
	if out["reload"] != true {
		t.Error("R-x does not tell the page to reload the list")
	}
	code, out = e.post("/api/workspace/messages", map[string]any{"folder": "F-LONG", "cursor": "gone-cursor", "after": "unknown1"})
	refusedBody(t, code, out, "R-x", "This list expired")

	// a page made before a restart
	restarted := newDashboard(e.cfg).router()
	req := httptest.NewRequest(http.MethodPost, "/api/workspace/count", strings.NewReader("{}"))
	req.Host, req.Header = "127.0.0.1:8097", http.Header{"Origin": {"http://127.0.0.1:8097"}, "X-Csrf-Token": {e.d.csrf}, "X-Epoch": {e.d.boot}}
	rec := httptest.NewRecorder()
	restarted.ServeHTTP(rec, req)
	var ry map[string]any
	json.Unmarshal(rec.Body.Bytes(), &ry)
	refusedAs(t, rec.Code, http.StatusForbidden, ry, "R-y", "restarted — reload the page", "your selection can't be reused")
	bare := httptest.NewRequest(http.MethodPost, "/api/workspace/count", strings.NewReader("{}"))
	bare.Host, bare.Header = "127.0.0.1:8097", http.Header{"Origin": {"http://127.0.0.1:8097"}}
	rec = httptest.NewRecorder()
	restarted.ServeHTTP(rec, bare)
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "R-y") {
		t.Errorf("a forged POST (no token, no epoch) was told it was a restart: %d %s", rec.Code, rec.Body.String())
	}

	// resolution never adds to the seen-set; the live selection's handles survive pressure
	ids := e.folders()
	inbox := handlesOf(t, e, ids["Inbox"])
	n0, _ := sess.seen.len()
	e.preview(e.selection(entry(ids["Inbox"], false, inbox[0])))
	if n1, _ := sess.seen.len(); n1 != n0 {
		t.Errorf("resolving a selection changed the seen-set: %d -> %d entries", n0, n1)
	}
	live := e.selection()
	live["messages"] = []any{map[string]any{"h": inbox[1]}, map[string]any{"h": inbox[2]}}
	e.ok("/api/workspace/count", live) // pins them
	sess.seen.maxN = 40
	for _, folder := range []string{"F-LONG", "F-SENT", "F-CLIENTS"} {
		page(folder, "", "")
	}
	if out := e.ok("/api/workspace/count", live); out["count"] != float64(2) {
		t.Errorf("the live selection's handles did not survive LRU pressure: %v", out)
	}
	evicted := e.selection()
	evicted["messages"] = []any{map[string]any{"h": inbox[5]}}
	code, out = e.post("/api/workspace/count", evicted)
	refusedBody(t, code, out, "R-j2", "wasn't listed in this session")

	// idle release after 30 minutes, but not while an export runs
	sess.seen.maxN = seenMaxEntries
	block := make(chan struct{})
	e.fg.mutate(func() {
		for _, m := range e.fg.folder("F-SENT").Msgs {
			e.fg.valueFn[m.ID] = func(w http.ResponseWriter, r *http.Request) { <-block; w.Write(fgBody("x")) }
		}
	})
	pid, _ := e.preview(e.selection(entry(ids["Sent Items"], false)))
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	clock = clock.Add(31 * time.Minute)
	e.d.ws.reap(clock)
	if sess.treeNow() == nil {
		t.Error("the session was released while an export ran")
	}
	close(block)
	e.waitRun()
	clock = clock.Add(31 * time.Minute)
	e.d.ws.reap(clock)
	sess.mu.Lock()
	cursors := len(sess.cursors)
	sess.mu.Unlock()
	if n, _ := sess.seen.len(); sess.treeNow() != nil || n != 0 || cursors != 0 {
		t.Errorf("after 30 idle minutes the tree (%v), seen-set (%d) and cursors (%d) were not all released", sess.treeNow() != nil, n, cursors)
	}
	code, out = e.post("/api/workspace/count", live)
	refusedBody(t, code, out, "R-j2", "wasn't listed in this session")
}

// covers: MA-412, R29, S52
// The preview writes nothing: the destination, the archive and everything under the
// config directory (the settings, the config, the state directory) are byte-for-byte what
// they were, except the sign-in token, whose normal refresh is asserted separately; and its
// result carries the exact count, each folder's reported and found, the size estimate with
// its unknown split, free bytes and the reserve, the would-be directory name, up to 10
// sample paths, the longest path and how many pass 259, the posture line and the
// filesystem warnings, and a warning naming the cloud service for a synced destination.
func TestPreviewWritesNothing(t *testing.T) {
	e := newWsEnv(t)
	e.fg.mutate(func() { e.fg.folder("F-SENT").Msgs[0].Size, e.fg.folder("F-SENT").Msgs[1].Size = -1, -1 })
	e.writeToken("alice@contoso.org", -time.Hour) // the first call refreshes it, and writes it back
	tokBefore := readFileString(t, e.tok)
	ids := e.folders()
	os.WriteFile(filepath.Join(e.dest, "keep.txt"), []byte("mine"), 0o600)
	os.WriteFile(filepath.Join(e.out, "index.html"), []byte("archive"), 0o600)
	cfgBefore := treeState(t, e.dir, filepath.Base(e.tok))

	_, res := e.preview(e.selection(entry(ids["Inbox"], true), entry(ids["Sent Items"], false)))
	if got := treeState(t, e.dir, filepath.Base(e.tok)); !reflect.DeepEqual(got, cfgBefore) {
		t.Errorf("the preview wrote under the config directory, the destination or the archive: %v", diffKeys(cfgBefore, got))
	}

	if res["count"] != float64(18) || res["reported"] != float64(18) {
		t.Errorf("count %v reported %v, want 18", res["count"], res["reported"])
	}
	if res["unknownSize"] != float64(2) || res["estimateBytes"].(float64) <= 0 {
		t.Errorf("the size estimate: %v bytes with %v unknown, want a figure with 2 unknown", res["estimateBytes"], res["unknownSize"])
	}
	if res["freeBytes"].(float64) <= 0 || res["reserve"].(float64) <= 0 || res["recordBound"].(float64) <= 0 {
		t.Errorf("free %v reserve %v record bound %v", res["freeBytes"], res["reserve"], res["recordBound"])
	}
	if !regexp.MustCompile(`^MailExport-\d{4}-\d\d-\d\d_\d{6}-[0-9a-z]{4}$`).MatchString(res["exportName"].(string)) {
		t.Errorf("the would-be directory is %q", res["exportName"])
	}
	if sp := res["samplePaths"].([]any); len(sp) != 10 || !strings.HasSuffix(sp[0].(string), ".eml") {
		t.Errorf("sample paths: %v", sp)
	}
	if res["longestPath"].(float64) <= 0 || res["over259"] == nil {
		t.Errorf("longest path %v, over 259 %v", res["longestPath"], res["over259"])
	}
	if p := res["posture"].(map[string]any); p["line"] == "" || p["kind"] == "" {
		t.Errorf("the posture: %v", p)
	}
	folders := res["folders"].([]any)
	if len(folders) != 4 || folders[0].(map[string]any)["reported"] != folders[0].(map[string]any)["found"] {
		t.Errorf("the per-folder counts: %v", folders)
	}

	// the token's own refresh is the one write, and it happened
	if readFileString(t, e.tok) == tokBefore {
		t.Error("the sign-in token was not refreshed and written back")
	}

	// a cloud-synced destination warns, naming the service
	cloud := filepath.Join(e.dir, "OneDrive", "exports")
	os.MkdirAll(cloud, 0o755)
	sel := e.selection(entry(ids["Sent Items"], false))
	sel["dest"] = cloud
	_, res = e.preview(sel)
	if !strings.Contains(strings.Join(toStrings(res["warnings"]), "\n"), "uploaded to OneDrive") {
		t.Errorf("no warning names the cloud service: %v", res["warnings"])
	}
	// a destination whose paths pass 259 characters says how many
	deep := e.dest
	for i := 0; i < 3; i++ {
		deep = filepath.Join(deep, strings.Repeat(string(rune('a'+i)), 100))
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Skipf("this host cannot make a deep folder: %v", err)
	}
	sel["dest"] = deep
	_, res = e.preview(sel)
	if res["over259"].(float64) < 1 || res["longestPath"].(float64) <= 259 {
		t.Errorf("a deep destination: longest %v, over 259: %v", res["longestPath"], res["over259"])
	}
}

// covers: MA-413, R29, S52
// The run copies exactly the previewed ids: a message that arrives after the preview is
// not in the export, one deleted after it is a listed "no longer in the mailbox" failure,
// and a preview that is expired (clock-injected) or superseded is R-k.
func TestRunCopiesExactlyThePreview(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	pid, res := e.preview(e.selection(entry(ids["Inbox"], false)))
	if res["count"] != float64(7) {
		t.Fatalf("previewed %v", res["count"])
	}
	gone := "F-INBOX-0004"
	e.fg.mutate(func() {
		inbox := e.fg.folder("F-INBOX")
		inbox.Msgs = append([]fgMsg{{ID: "F-INBOX-LATE", Subject: "late", From: "a@example.com", Received: time.Now(), Size: 40}}, inbox.Msgs...)
		e.fg.valueFn[gone] = func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":{"code":"ErrorItemNotFound","message":"gone"}}`, http.StatusNotFound)
		}
	})
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	last := e.waitRun()["last"].(map[string]any)
	if last["exported"] != float64(6) || last["failed"] != float64(1) {
		t.Fatalf("the run ended exported %v, failed %v, want 6 and 1: %v", last["exported"], last["failed"], last)
	}
	var man struct {
		Messages []struct {
			SourceID string `json:"source_id"`
			Status   string `json:"status"`
		} `json:"messages"`
		Failures []struct {
			SourceID string `json:"source_id"`
			Reason   string `json:"reason"`
		} `json:"failures"`
	}
	if err := json.Unmarshal([]byte(readFileString(t, filepath.Join(e.dest, last["rootName"].(string), "export-manifest.json"))), &man); err != nil {
		t.Fatal(err)
	}
	if len(man.Messages) != 7 {
		t.Errorf("the manifest lists %d messages, want exactly the previewed 7", len(man.Messages))
	}
	for _, m := range man.Messages {
		if m.SourceID == "F-INBOX-LATE" {
			t.Errorf("a message that arrived after the preview is in the export: %v", m)
		}
	}
	if len(man.Failures) != 1 || man.Failures[0].SourceID != gone || !strings.Contains(man.Failures[0].Reason, "no longer in the mailbox") {
		t.Errorf("the deleted message's failure: %+v", man.Failures)
	}

	// an expired and a superseded preview are R-k, and nothing is made
	clock := time.Now()
	e.d.now = func() time.Time { return clock }
	old, _ := e.preview(e.selection(entry(ids["Sent Items"], false)))
	fresh, _ := e.preview(e.selection(entry(ids["Sent Items"], false)))
	before := treeState(t, e.dest)
	code, out := e.post("/api/exports/start", map[string]any{"previewId": old})
	refusedBody(t, code, out, "R-k", "out of date", "Preview copy export again")
	clock = clock.Add(31 * time.Minute)
	code, out = e.post("/api/exports/start", map[string]any{"previewId": fresh})
	refusedBody(t, code, out, "R-k", "out of date")
	if got := treeState(t, e.dest); !reflect.DeepEqual(got, before) {
		t.Errorf("a refused start changed the destination: %v", diffKeys(before, got))
	}
}

// covers: MA-431, R29, R5, S25, S52
// A Microsoft 365 export runs beside a capture that holds the archive lock: it never
// takes the lock and never writes the archive (whose bytes are what they were), it backs
// off while the capture runs (its count does not move) and completes once it ends; a second
// start while it runs is R-m, at the preview and at Start.
func TestM365ExportRunsBesideALockedArchive(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	os.WriteFile(filepath.Join(e.out, "index.html"), []byte("archive"), 0o600)
	pid, _ := e.preview(e.selection(entry(ids["Sent Items"], false)))
	e.d.ws.mu.Lock()
	saved := *e.d.ws.prev
	e.d.ws.mu.Unlock()

	lock, err := lockfile.AcquireAs(filepath.Join(e.out, lockfile.Name), "export") // a capture holds the archive
	if err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			lock.Release()
		}
	}()
	archive := treeState(t, e.out)
	e.ok("/api/exports/start", map[string]any{"previewId": pid})

	// it backs off while the capture runs
	time.Sleep(1500 * time.Millisecond)
	_, p := e.post("/api/exports/progress", nil)
	if p["running"] != true || p["done"] != float64(0) {
		t.Errorf("while a capture held the lock the export was %v with %v done, want running with 0", p["running"], p["done"])
	}
	// a second start while it runs is R-m
	code, out := e.post("/api/workspace/preview", e.selection(entry(ids["Inbox"], false)))
	refusedBody(t, code, out, "R-m", "already running", "cancel it")
	e.d.ws.mu.Lock()
	e.d.ws.prev = &saved
	e.d.ws.mu.Unlock()
	code, out = e.post("/api/exports/start", map[string]any{"previewId": pid})
	refusedBody(t, code, out, "R-m", "already running")

	lock.Release()
	held = false
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["exported"] != float64(5) {
		t.Fatalf("the export after the capture ended: %v", last)
	}
	if got := treeState(t, e.out); !reflect.DeepEqual(got, archive) {
		t.Errorf("the export changed the archive: %v", diffKeys(archive, got))
	}
}
