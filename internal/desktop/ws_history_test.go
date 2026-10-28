package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph"
)

// The Export activity's history, the retry chain, Check this export, Finish as partial, the
// reconciler's actions and Create folder, through the dashboard's own handler.

func (e *wsEnv) history() []map[string]any {
	e.t.Helper()
	out := e.ok("/api/exports/history", nil)
	var cards []map[string]any
	for _, c := range out["runs"].([]any) {
		cards = append(cards, c.(map[string]any))
	}
	return cards
}

func (e *wsEnv) card(run string) map[string]any {
	e.t.Helper()
	for _, c := range e.history() {
		if c["run"] == run {
			return c
		}
	}
	e.t.Fatalf("the history has no card for run %s", run)
	return nil
}

func actionsOf(c map[string]any) []string {
	var out []string
	for _, a := range c["actions"].([]any) {
		out = append(out, a.(string))
	}
	return out
}

// failWith makes a message's download answer a status (a retryable failure for 400).
func (e *wsEnv) failWith(id string, code int) {
	e.fg.mutate(func() {
		if code == 0 {
			delete(e.fg.valueFn, id)
			return
		}
		e.fg.valueFn[id] = func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":{"code":"BadRequest","message":"no"}}`, code)
		}
	})
}

func sha256Hex(b ...[]byte) string {
	h := sha256.New()
	for _, x := range b {
		h.Write(x)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (e *wsEnv) sums(root string) []byte {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.dest, root, "SHA256SUMS.txt"))
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

// emlsOf lists a root's exported .eml files, relative to it.
func (e *wsEnv) emlsOf(root string) []string {
	e.t.Helper()
	var out []string
	filepath.WalkDir(filepath.Join(e.dest, root), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".eml") {
			rel, _ := filepath.Rel(filepath.Join(e.dest, root), p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// restart makes a new dashboard on the same state directory, as the next launch does.
func (e *wsEnv) restart() {
	e.d = newDashboard(e.cfg)
	e.h = e.d.router()
}

var eightGroups = regexp.MustCompile(`^([0-9a-f]{8} ){7}[0-9a-f]{8}$`)

// covers: MA-437, MA-521, MA-520, MA-427, R29, S52
// Retry from the run record: "Export failed messages again" on a partial run yields a preview
// of exactly its retryable failed ids, into a new directory named as the next part (-part2,
// -part3) whose manifest names retry_of and carries the whole chain, with the earlier export
// byte-unchanged; it works after a restart, after Clear sign-in and a sign-in as the same
// account, and with the destination manifest deleted (what is retried is unchanged); a signer
// mismatch is R-l. The set's fingerprint is SHA-256 over the parts' SHA256SUMS.txt files in
// seal order, a part marked gone stays in it and is named gone. Check this export reports a
// flipped byte, a missing file, a planted file and a changed SUMS file, each RED naming the
// file, and a clean export GREEN.
func TestRetryBuildsFromTheRunRecord(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	e.failWith("F-INBOX-0002", 400)
	e.failWith("F-INBOX-0005", 400)
	run1 := e.startExport(e.selection(entry(ids["Inbox"], false)))
	last1 := e.waitRun()["last"].(map[string]any)
	if last1["status"] != "partial" || last1["exported"] != float64(5) || last1["failed"] != float64(2) {
		t.Fatalf("the first run: %v", last1)
	}
	root1 := last1["rootName"].(string)

	// the card says what it offers (MA-427's offered labels)
	c1 := e.card(run1)
	if c1["label"] != "Finished with problems — 2 messages not exported" || !reflect.DeepEqual(actionsOf(c1), []string{"open-folder", "check-this-export", "export-failed-again"}) {
		t.Errorf("the partial run's card: %v %v", c1["label"], c1["actions"])
	}
	if fp := c1["fingerprint"].(string); len(fp) != 64 || fp != sha256Hex(e.sums(root1)) || !eightGroups.MatchString(c1["fingerprintGrouped"].(string)) ||
		!strings.Contains(c1["receipt"].(string), c1["fingerprintGrouped"].(string)) || c1["set"] != nil {
		t.Errorf("the card's fingerprint: %v", c1)
	}

	// Check this export: GREEN, then each kind of change RED naming the file
	check := func() map[string]any { return e.ok("/api/exports/check", map[string]any{"runId": run1}) }
	if g := check(); g["posture"] != "GREEN" || g["fingerprintOK"] != true || g["files"].(float64) < 5 || len(g["changed"].([]any))+len(g["missing"].([]any))+len(g["unlisted"].([]any)) != 0 {
		t.Fatalf("a clean export: %v", g)
	}
	emls := e.emlsOf(root1)
	if len(emls) != 5 {
		t.Fatalf("the export holds %v", emls)
	}
	abs := func(rel string) string { return filepath.Join(e.dest, root1, filepath.FromSlash(rel)) }
	orig, _ := os.ReadFile(abs(emls[0]))
	flipped := append([]byte(nil), orig...)
	flipped[len(flipped)-3] ^= 0xff
	os.WriteFile(abs(emls[0]), flipped, 0o600)
	if r := check(); r["posture"] != "RED" || !strings.Contains(toJSON(r["changed"]), emls[0]) {
		t.Errorf("a flipped byte: %v", r)
	}
	os.WriteFile(abs(emls[0]), orig, 0o600)
	orig1, _ := os.ReadFile(abs(emls[1]))
	os.Remove(abs(emls[1]))
	if r := check(); r["posture"] != "RED" || !strings.Contains(toJSON(r["missing"]), emls[1]) {
		t.Errorf("a missing file: %v", r)
	}
	os.WriteFile(abs(emls[1]), orig1, 0o600)
	os.WriteFile(abs("planted.txt"), []byte("x"), 0o600)
	if r := check(); r["posture"] != "RED" || !strings.Contains(toJSON(r["unlisted"]), "planted.txt") {
		t.Errorf("a planted file: %v", r)
	}
	os.Remove(abs("planted.txt"))
	sums := e.sums(root1)
	os.WriteFile(abs("SHA256SUMS.txt"), append(append([]byte(nil), sums...), []byte("\n")...), 0o600)
	if r := check(); r["posture"] != "RED" || r["fingerprintOK"] != false {
		t.Errorf("a changed SUMS file: %v", r)
	}
	os.WriteFile(abs("SHA256SUMS.txt"), sums, 0o600)
	if g := check(); g["posture"] != "GREEN" {
		t.Fatalf("after restoring every file: %v", g)
	}
	part1 := treeState(t, filepath.Join(e.dest, root1))

	// a signer mismatch is R-l, and nothing is previewed
	e.writeToken("bob@contoso.org", time.Hour)
	e.fg.mutate(func() { e.fg.upn = "bob@contoso.org" })
	e.d.signInChanged("bob@contoso.org")
	code, out := e.post("/api/exports/retry", map[string]any{"runId": run1, "which": "failed"})
	refusedBody(t, code, out, "R-l", "alice@contoso.org", "bob@contoso.org")
	e.writeToken("alice@contoso.org", time.Hour)
	e.fg.mutate(func() { e.fg.upn = "alice@contoso.org" })

	// after Clear sign-in, a sign-in as the same account, a restart, and the destination
	// manifest deleted: what is retried is unchanged
	e.postRaw("/api/clear-signin", "{}")
	e.writeToken("alice@contoso.org", time.Hour)
	e.restart()
	manifest, _ := os.ReadFile(abs("export-manifest.json"))
	os.Remove(abs("export-manifest.json"))
	e.failWith("F-INBOX-0002", 0)
	pid := e.ok("/api/exports/retry", map[string]any{"runId": run1, "which": "failed"})["previewId"].(string)
	res := e.waitPreview(pid)
	var retried []string
	for _, m := range e.d.ws.prev.plan.Messages {
		retried = append(retried, m.SourceID)
	}
	sort.Strings(retried)
	if !reflect.DeepEqual(retried, []string{"F-INBOX-0002", "F-INBOX-0005"}) || res["count"] != float64(2) || res["retryOf"] != run1 || res["part"] != float64(2) ||
		!strings.HasSuffix(res["exportName"].(string), "-part2") {
		t.Fatalf("the retry preview retries %v: %v", retried, res)
	}
	os.WriteFile(abs("export-manifest.json"), manifest, 0o600)
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	last2 := e.waitRun()["last"].(map[string]any)
	root2 := last2["rootName"].(string)
	if last2["status"] != "partial" || last2["exported"] != float64(1) || last2["failed"] != float64(1) || root2 != root1+"-part2" {
		t.Fatalf("the second part: %v", last2)
	}
	m2 := e.manifestOf(root2)
	run2 := m2["run"].(map[string]any)["id"].(string)
	if m2["run"].(map[string]any)["retry_of"] != run1 || len(m2["run"].(map[string]any)["chain"].([]any)) != 2 {
		t.Errorf("part 2's manifest: %v", m2["run"])
	}
	c2 := e.card(run2)
	set := c2["set"].(map[string]any)
	if c2["part"] != float64(2) || set["fingerprint"] != sha256Hex(e.sums(root1), e.sums(root2)) || len(set["parts"].([]any)) != 2 {
		t.Errorf("part 2's card: %v", c2)
	}

	// part 3, from part 2's own record
	e.failWith("F-INBOX-0005", 0)
	pid = e.ok("/api/exports/retry", map[string]any{"runId": run2, "which": "failed"})["previewId"].(string)
	if res := e.waitPreview(pid); res["count"] != float64(1) || res["part"] != float64(3) || !strings.HasSuffix(res["exportName"].(string), "-part3") {
		t.Fatalf("the third part's preview: %v", res)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	last3 := e.waitRun()["last"].(map[string]any)
	root3 := last3["rootName"].(string)
	if last3["status"] != "complete" || root3 != root1+"-part3" {
		t.Fatalf("the third part: %v", last3)
	}
	m3 := e.manifestOf(root3)
	run3 := m3["run"].(map[string]any)["id"].(string)
	if len(m3["run"].(map[string]any)["chain"].([]any)) != 3 {
		t.Errorf("part 3's manifest chain: %v", m3["run"].(map[string]any)["chain"])
	}
	if readme := readFileString(t, filepath.Join(e.dest, root3, "README.txt")); !strings.Contains(readme, root1) || !strings.Contains(readme, root2) {
		t.Errorf("part 3's README does not list its siblings: %s", readme)
	}
	wantSet := sha256Hex(e.sums(root1), e.sums(root2), e.sums(root3))
	for _, run := range []string{run1, run2, run3} {
		c := e.card(run)
		s := c["set"].(map[string]any)
		if s["fingerprint"] != wantSet || len(s["parts"].([]any)) != 3 || !eightGroups.MatchString(s["grouped"].(string)) {
			t.Errorf("run %s's set: %v", run, s)
		}
		for i, p := range s["parts"].([]any) {
			if p.(map[string]any)["run"] != []string{run1, run2, run3}[i] || p.(map[string]any)["gone"] != false {
				t.Errorf("set part %d: %v", i+1, p)
			}
		}
	}
	if got := treeState(t, filepath.Join(e.dest, root1)); !reflect.DeepEqual(got, part1) {
		t.Errorf("the earlier export changed: %v", diffKeys(part1, got))
	}
	c3 := e.card(run3)
	if c3["label"] != "Complete" || !reflect.DeepEqual(actionsOf(c3), []string{"open-folder", "check-this-export"}) {
		t.Errorf("a complete part's card: %v %v", c3["label"], c3["actions"])
	}

	// part 2 marked gone after it sealed stays in the set, named gone, and the receipt says so
	_, gone := e.post("/api/exports/locate", map[string]any{"runId": run2, "action": "gone"})
	if gone["ok"] != true {
		t.Fatalf("This folder is gone: %v", gone)
	}
	c3 = e.card(run3)
	s3 := c3["set"].(map[string]any)
	parts := s3["parts"].([]any)
	if s3["fingerprint"] != wantSet || parts[1].(map[string]any)["gone"] != true || parts[0].(map[string]any)["gone"] != false ||
		!strings.Contains(c3["receipt"].(string), "gone") || !strings.Contains(c3["receipt"].(string), parts[0].(map[string]any)["grouped"].(string)) {
		t.Errorf("the set after part 2 is marked gone: %v", s3)
	}
	if c2 := e.card(run2); c2["label"] != "This folder is gone" || !hasAct(c2, "export-these-again") {
		t.Errorf("the gone part's card: %v", c2)
	}
}

func hasAct(c map[string]any, a string) bool {
	for _, x := range actionsOf(c) {
		if x == a {
			return true
		}
	}
	return false
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// wedge starts an export of the Inbox, blocks the run's destination after message 3 (a hung
// network mount), asks for Cancel, and lets the 30 s timer (shortened) release the slot. It
// returns the run's id and its folder once the abandoned run has returned.
func (e *wsEnv) wedge() (run, root string, release func()) {
	e.t.Helper()
	ids := e.folders()
	e.d.ex.wedgeAfter = 150 * time.Millisecond
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	e.t.Cleanup(release)
	e.d.ex.env.SetStepHook(func(step string) {
		if step == "W6:3" {
			<-gate
		}
	})
	run = e.startExport(e.selection(entry(ids["Inbox"], false)))
	e.waitDone(3)
	e.ok("/api/exports/cancel", map[string]any{"runId": run})
	for i := 0; ; i++ {
		_, p := e.post("/api/exports/progress", nil)
		if p["released"] == true {
			if p["label"] != "Stopped: the destination is not responding" || p["running"] != false {
				e.t.Fatalf("the released slot: %v", p)
			}
			break
		}
		if i > 300 {
			e.t.Fatal("the wedged run's slot was never released")
		}
		time.Sleep(20 * time.Millisecond)
	}
	slot := e.d.ex.slot
	release()
	select {
	case <-slot.done:
	case <-time.After(10 * time.Second):
		e.t.Fatal("the abandoned run never returned")
	}
	slot.mu.Lock()
	root = slot.res.RootName
	slot.mu.Unlock()
	return run, root, release
}

// covers: MA-437, MA-448, MA-427, R29, S52
// A wedged run (its destination stopped answering after Cancel) releases the slot, is
// recorded as stopped, never ended, and after a restart is offered Finish as partial; that
// finishes it from the run record alone: the ledger's fingerprint equals SHA-256 of the SUMS
// file written, the journal has its end, the manifest says failed and the marker STOPPED;
// "Export the remaining messages" then yields exactly the messages after the ones exported,
// into part 2 of the set.
func TestAWedgedRunIsFinishedAsPartialAfterARestart(t *testing.T) {
	e := newWsEnv(t)
	run, root, _ := e.wedge()
	if e.d.ex.env.Ended(run) {
		t.Fatal("a wedged run was recorded as ended")
	}
	e.restart()
	var card map[string]any
	for i := 0; i < 300; i++ {
		began := time.Now()
		card = e.card(run)
		if took := time.Since(began); took > time.Second {
			t.Fatalf("the history took %v: it waited on a probe", took)
		}
		// the action and the probe's "not responding" can land on different draws: wait for both
		if l, _ := card["label"].(string); hasAct(card, "finish-as-partial") && strings.Contains(l, "the destination is not responding") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !hasAct(card, "finish-as-partial") || !strings.Contains(card["label"].(string), "interrupted") || !strings.Contains(card["label"].(string), "the destination is not responding") {
		t.Fatalf("the wedged run after a restart: %v %v", card["label"], card["actions"])
	}
	fin := e.ok("/api/exports/finish", map[string]any{"runId": run})
	sum := sha256Hex(e.sums(root))
	if fin["status"] != "finished-as-partial" || fin["fingerprint"] != sum || !strings.Contains(fin["label"].(string), "finished as partial") {
		t.Fatalf("Finish as partial: %v (the SUMS hash is %s)", fin, sum)
	}
	if !e.d.ex.env.Ended(run) {
		t.Error("Finish as partial left no journal end")
	}
	ledger := readFileString(t, filepath.Join(e.state, "export-fingerprints.jsonl"))
	if !strings.Contains(ledger, run) || !strings.Contains(ledger, sum) {
		t.Errorf("the ledger does not hold the SUMS hash %s for %s: %s", sum, run, ledger)
	}
	if manifestStatus(e.manifestOf(root)) != "failed" {
		t.Errorf("the manifest's status: %v", manifestStatus(e.manifestOf(root)))
	}
	if marker := readFileString(t, filepath.Join(e.dest, root, "_EXPORT-NOT-FINISHED.txt")); !strings.Contains(marker, "STOPPED") {
		t.Errorf("the marker: %q", marker)
	}
	card = e.card(run)
	if !strings.Contains(card["label"].(string), "finished as partial") || !hasAct(card, "export-remaining") || hasAct(card, "finish-as-partial") {
		t.Errorf("the card after Finish as partial: %v %v", card["label"], card["actions"])
	}

	// the remaining messages: exactly those after the three exported
	pid := e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})["previewId"].(string)
	res := e.waitPreview(pid)
	var rest []string
	for _, m := range e.d.ws.prev.plan.Messages {
		rest = append(rest, m.SourceID)
	}
	sort.Strings(rest)
	if res["count"] != float64(4) || !reflect.DeepEqual(rest, []string{"F-INBOX-0004", "F-INBOX-0005", "F-INBOX-0006", "F-INBOX-0007"}) {
		t.Fatalf("the remaining messages: %v %v", res["count"], rest)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["rootName"] != root+"-part2" || last["exported"] != float64(4) {
		t.Fatalf("the remaining messages' run: %v", last)
	}
}

// covers: MA-520, R29, S52
// Check and finish on a sealed cancelled run verifies the folder and rewrites the marker
// CANCELLED, never removing it; a changed file refuses with the marker kept.
func TestCheckAndFinishKeepsACancelledRunsMarker(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	e.slow("F-INBOX", 80*time.Millisecond)
	run := e.startExport(e.selection(entry(ids["Inbox"], false)))
	e.waitDone(2)
	e.ok("/api/exports/cancel", map[string]any{"runId": run})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "cancelled" {
		t.Fatalf("the cancelled run: %v", last)
	}
	root := last["rootName"].(string)
	markerPath := filepath.Join(e.dest, root, "_EXPORT-NOT-FINISHED.txt")
	if c := e.card(run); c["label"] != "Cancelled" || !hasAct(c, "export-remaining") {
		t.Errorf("the cancelled run's card: %v %v", c["label"], c["actions"])
	}
	fin := e.ok("/api/exports/finish", map[string]any{"runId": run})
	if fin["status"] != "cancelled" {
		t.Errorf("Check and finish: %v", fin)
	}
	if marker := readFileString(t, markerPath); !strings.Contains(marker, "CANCELLED") {
		t.Errorf("the marker after Check and finish: %q", marker)
	}
	emls := e.emlsOf(root)
	p := filepath.Join(e.dest, root, filepath.FromSlash(emls[0]))
	orig, _ := os.ReadFile(p)
	os.WriteFile(p, append(orig, 'x'), 0o600)
	code, out := e.post("/api/exports/finish", map[string]any{"runId": run})
	refusedBody(t, code, out, "R-check", "the marker is kept", "Check this export")
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("the marker was removed by a refused Check and finish: %v", err)
	}
	if strings.Contains(out["error"].(string), "panic") {
		t.Errorf("the refusal: %v", out["error"])
	}
}

// covers: MA-446, R12, S41, S52
// The Export activity page's data: a running export shows counts, bytes, the current folder,
// an ETA and a runId its Cancel acts on (and Cancel works); a finished run shows its status
// label, counts, fingerprint in groups of 8 with a receipt, its path and the actions its
// status allows; a panic in the run surfaces as "Stopped: an internal error" on its card,
// never a traceback, and the dashboard stays up.
func TestExportActivityPage(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	e.slow("F-INBOX", 100*time.Millisecond)
	run := e.startExport(e.selection(entry(ids["Inbox"], true)))
	e.waitDone(3)
	_, p := e.post("/api/exports/progress", nil)
	if p["running"] != true || p["runId"] != run || p["done"].(float64) < 3 || p["selected"] != float64(13) || p["bytes"].(float64) <= 0 ||
		p["folder"] == "" || p["etaSeconds"].(float64) <= 0 || p["bytesText"] == "" {
		t.Errorf("the running export: %v", p)
	}
	if c := e.card(run); c["running"] != true || c["label"] != "Running" {
		t.Errorf("the running card: %v", c)
	}
	e.ok("/api/exports/cancel", map[string]any{"runId": run})
	if last := e.waitRun()["last"].(map[string]any); last["status"] != "cancelled" {
		t.Errorf("after Cancel: %v", last)
	}
	// Cancel for a run that is not running says so
	if _, out := e.post("/api/exports/cancel", map[string]any{"runId": run}); out["ok"] != false {
		t.Errorf("Cancel of a finished run: %v", out)
	}
	c := e.card(run)
	if c["label"] != "Cancelled" || c["rootName"] == "" || c["dest"] != filepath.Join(e.dest, c["rootName"].(string)) || !eightGroups.MatchString(c["fingerprintGrouped"].(string)) || c["receipt"] == "" ||
		c["planned"] != float64(13) || c["exported"].(float64) < 3 || !hasAct(c, "open-folder") || !hasAct(c, "check-this-export") {
		t.Errorf("a finished card: %v", c)
	}

	// Open folder reveals the verified folder and launches nothing
	var revealed []string
	e.d.reveal = func(name string, args []string) error { revealed = append([]string{name}, args...); return nil }
	e.ok("/api/exports/open-folder", map[string]any{"runId": run})
	if len(revealed) == 0 || !strings.Contains(strings.Join(revealed, " "), c["rootName"].(string)) {
		t.Errorf("Open folder ran %v", revealed)
	}
	if runtime.GOOS == "darwin" && (revealed[0] != "open" || revealed[1] != "-R") {
		t.Errorf("the macOS reveal is %v, want open -R <README>", revealed)
	}

	// a panic in the run: "Stopped: an internal error", never a traceback
	e.d.ex.env.SetStepHook(func(step string) {
		if step == "W6:2" {
			panic("boom: a bug in the run goroutine")
		}
	})
	pid, _ := e.preview(e.selection(entry(ids["Sent Items"], false)))
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "failed" || last["stopId"] != "internal" || !strings.Contains(last["label"].(string), "Stopped: an internal error") {
		t.Fatalf("a run that panicked: %v", last)
	}
	for _, c := range e.history() {
		if c["rootName"] == last["rootName"] {
			text := toJSON(c)
			if !strings.Contains(c["label"].(string), "Stopped: an internal error") || strings.Contains(text, "boom") || strings.Contains(text, "goroutine") || strings.Contains(text, ".go:") {
				t.Errorf("the panicked run's card: %s", text)
			}
		}
	}
	if code, _ := e.post("/api/exports/progress", nil); code != http.StatusOK {
		t.Errorf("the dashboard after a panic in a run: %d", code)
	}
}

// covers: MA-448, R29, R12, S52
// Reconciliation is off the request path: with a probe of an old run's folder blocked (a
// dead network mount), progress and history answer within 1 s and a new export starts and
// Cancel works; each run is probed once per launch; "did not answer", "folder not found"
// and a mismatch are observations (cached, never journaled as terminal); Look again probes
// again; Locate accepts a moved folder only when its identity confirms and refuses another;
// This folder is gone is terminal and journaled once.
func TestReconciliationIsOffTheRequestPath(t *testing.T) {
	e := newWsEnv(t)
	run, root, _ := e.wedge()
	rootPath := filepath.Join(e.dest, root)
	// the process that ran it was killed before it reconciled: the journal holds no
	// terminal `reconciled` line for it (a reconciled marker-and-seal outcome is not probed again)
	jpath := filepath.Join(e.state, "desktop-exports.jsonl")
	var kept []string
	for _, l := range strings.Split(strings.TrimSuffix(readFileString(t, jpath), "\n"), "\n") {
		if !strings.Contains(l, "reconciled") {
			kept = append(kept, l)
		}
	}
	os.WriteFile(jpath, []byte(strings.Join(kept, "\n")+"\n"), 0o600)

	var mu sync.Mutex
	probes := 0
	gate := make(chan struct{})
	blocked := true
	restore := app.SetProbeHook(func(path string) {
		if !strings.Contains(path, root) {
			return
		}
		mu.Lock()
		probes++
		b := blocked
		mu.Unlock()
		if b {
			<-gate
		}
	}, 400*time.Millisecond)
	t.Cleanup(func() {
		restore()
		mu.Lock()
		if blocked {
			blocked = false
			close(gate)
		}
		mu.Unlock()
	})
	probeCount := func() int { mu.Lock(); defer mu.Unlock(); return probes }

	began := time.Now()
	e.restart()
	if took := time.Since(began); took > time.Second {
		t.Errorf("the launch waited %v for the probe", took)
	}
	within := func(what string, fn func()) {
		t.Helper()
		b := time.Now()
		fn()
		if took := time.Since(b); took > time.Second {
			t.Errorf("%s took %v while a probe was blocked", what, took)
		}
	}
	within("progress", func() { e.post("/api/exports/progress", nil) })
	var card map[string]any
	within("history", func() { card = e.card(run) })
	if !strings.Contains(card["label"].(string), "looking for its folder") && !strings.Contains(card["label"].(string), "did not answer") {
		t.Errorf("an unprobed run's card: %v", card["label"])
	}
	// a new export starts and Cancel works while the probe is blocked
	ids := e.folders()
	e.slow("F-SENT", 100*time.Millisecond)
	var newRun string
	within("Start", func() { newRun = e.startExport(e.selection(entry(ids["Sent Items"], false))) })
	e.waitDone(1)
	within("Cancel", func() { e.ok("/api/exports/cancel", map[string]any{"runId": newRun}) })
	e.waitRun()

	// the probe timed out: an observation, not a terminal record; probed once this launch
	for i := 0; i < 100 && !strings.Contains(e.card(run)["label"].(string), "did not answer"); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	card = e.card(run)
	if !strings.Contains(card["label"].(string), "did not answer") || !reflect.DeepEqual(actionsOf(card), []string{"look-again"}) {
		t.Fatalf("a probe that timed out: %v %v", card["label"], card["actions"])
	}
	for i := 0; i < 5; i++ {
		e.card(run)
	}
	if n := probeCount(); n != 1 {
		t.Errorf("the run was probed %d times in one launch, want once", n)
	}
	journal := readFileString(t, filepath.Join(e.state, "desktop-exports.jsonl"))
	if strings.Contains(journal, `"no-answer"`) || strings.Contains(journal, `"not-found"`) {
		t.Errorf("an observation was journaled as terminal: %s", journal)
	}

	// the mount answers again, but the folder moved meanwhile: Look again probes again and
	// says not found, which is still an observation
	mu.Lock()
	blocked = false
	close(gate)
	mu.Unlock()
	moved := filepath.Join(e.dir, "elsewhere")
	os.MkdirAll(moved, 0o755)
	newPath := filepath.Join(moved, root)
	if err := os.Rename(rootPath, newPath); err != nil {
		t.Fatal(err)
	}
	if out := e.ok("/api/exports/locate", map[string]any{"runId": run, "action": "again"}); out["kind"] != "not-found" || probeCount() != 2 {
		t.Fatalf("Look again on a moved folder: %v after %d probes", out, probeCount())
	}
	if journal := readFileString(t, jpath); strings.Contains(journal, `"not-found"`) {
		t.Errorf("not found was journaled as terminal: %s", journal)
	}
	if card = e.card(run); !reflect.DeepEqual(actionsOf(card), []string{"look-again", "locate", "this-folder-is-gone"}) || !strings.Contains(card["label"].(string), "folder not found") {
		t.Errorf("the not-found card: %v %v", card["label"], card["actions"])
	}
	other := filepath.Join(e.dir, "other-folder")
	os.MkdirAll(other, 0o755)
	code, out := e.post("/api/exports/locate", map[string]any{"runId": run, "action": "locate", "path": other})
	if code != statusRefused || out["ok"] != false || (out["id"] != "R-r" && out["id"] != "R-r2") {
		t.Errorf("Locate… on a folder that is not the export: %d %v", code, out)
	}
	if out := e.ok("/api/exports/locate", map[string]any{"runId": run, "action": "locate", "path": newPath}); out["kind"] != "interrupted" {
		t.Errorf("Locate… on the moved folder: %v", out)
	}
	if card = e.card(run); card["dest"] != newPath {
		t.Errorf("the located card: %v", card)
	}

	// This folder is gone is terminal, journaled once, and offers the messages again
	e.ok("/api/exports/locate", map[string]any{"runId": run, "action": "gone"})
	card = e.card(run)
	if card["label"] != "This folder is gone" || !reflect.DeepEqual(actionsOf(card), []string{"export-these-again"}) || card["remaining"].(float64) < 1 {
		t.Errorf("a folder marked gone: %v %v %v", card["label"], card["actions"], card["remaining"])
	}
	journal = readFileString(t, filepath.Join(e.state, "desktop-exports.jsonl"))
	if n := strings.Count(journal, `"event":"reconciled"`); n < 1 || strings.Count(journal, `"gone"`) != 1 {
		t.Errorf("the journal after This folder is gone: %s", journal)
	}
	if out := e.ok("/api/exports/locate", map[string]any{"runId": run, "action": "again"}); out["kind"] != "gone" {
		t.Errorf("Look again on a gone folder: %v", out)
	}
}

// covers: MA-438, R12, S52
// Every Microsoft 365 and destination refusal the desktop raises carries its what, why and
// remedy in the design's words, none contains a token, a Graph URL query, panic or
// goroutine, and a POST after a restart is R-y, never a bare 403. The engine's own are the
// engine's TestEngineRefusalsCarryWhatWhyAndRemedy.
func TestRefusalsCarryWhatWhyAndRemedy(t *testing.T) {
	e := newWsEnv(t)
	forbid := assure.Forbid("panic", "goroutine", "access_token", "refresh_token", "Bearer", "SECRET", e.fg.srv.URL, "$select", "$filter")
	check := func(what string, code int, out map[string]any, id string, names ...string) {
		t.Helper()
		if out["ok"] != false || out["id"] != id {
			t.Errorf("%s: want %s, got %d %v", what, id, code, out)
			return
		}
		msg, _ := out["error"].(string)
		assure.Refused(t, code, msg, assure.Code(statusRefused), assure.Names(names...), forbid)
	}
	ids := e.folders()
	handles := handlesOf(t, e, ids["Inbox"])

	// R-h, R-j2, R-k, R-m
	code, out := e.post("/api/workspace/preview", e.selection())
	check("R-h", code, out, "R-h", "Select at least one folder or message")
	code, out = e.post("/api/workspace/count", e.selection(entry("nope", true)))
	check("R-j2", code, out, "R-j2", "A folder in this selection wasn't listed in this session", "Load folders again", "select again")
	code, out = e.post("/api/exports/start", map[string]any{"previewId": "stale"})
	check("R-k", code, out, "R-k", "out of date", "older than 30 minutes", "Preview copy export again")
	code, out = e.post("/api/workspace/count", e.selection(entry(ids["Inbox"], false, "zzzzzzzz")))
	check("R-j2 message", code, out, "R-j2", "A message in this selection wasn't listed in this session", "Reload the folder")

	// R-e, R-f, R-q
	sel := e.selection(entry(ids["Sent Items"], false))
	sel["dest"] = filepath.Join(e.dir, "nope")
	check("R-e", statusRefused, e.previewFailure(sel), "R-e", "doesn't exist", "an export is written into an existing folder", "Browse", "Create folder")
	os.MkdirAll(filepath.Join(e.out, "store"), 0o755)
	sel["dest"] = filepath.Join(e.out, "store")
	check("R-f", statusRefused, e.previewFailure(sel), "R-f", "inside your archive", "would mix into the archive", "Choose a folder outside")
	code, out = e.post("/api/workspace/mkdir", map[string]any{"parent": e.dest, "name": "a/b"})
	check("R-q separator", code, out, "R-q", "'a/b' can't be a folder name", "try 'a b'")
	os.Mkdir(filepath.Join(e.dest, "Archive Folder"), 0o755)
	code, out = e.post("/api/workspace/mkdir", map[string]any{"parent": e.dest, "name": "Archive Folder"})
	check("R-q exists", code, out, "R-q", "A folder named 'Archive Folder' already exists here", "choose it with Browse", "use another name")

	// R-b, R-c, R-a, R-a2 through the endpoints
	pick := e.selection()
	pick["messages"] = []any{map[string]any{"h": handles[0]}}
	e.writeToken("bob@contoso.org", time.Hour)
	code, out = e.post("/api/workspace/count", pick)
	check("R-l", code, out, "R-l", "alice@contoso.org's mailbox", "you're now signed in as bob@contoso.org", "Load folders again")
	os.Remove(e.tok)
	e.d.signInChanged("")
	code, out = e.post("/api/workspace/folders", nil)
	check("R-b", code, out, "R-b", "not signed in to Microsoft 365", "reads your mailbox with your own sign-in", "Click Sign in")

	e.writeToken("alice@contoso.org", -time.Hour)
	e.fg.mutate(func() { e.fg.tokenErr = "invalid_grant" })
	code, out = e.post("/api/workspace/folders", nil)
	check("R-c", code, out, "R-c", "refused the request", "sign-in expired or access was revoked", "Sign in again")
	e.fg.mutate(func() { e.fg.tokenErr = "unauthorized_client" })
	code, out = e.post("/api/workspace/folders", nil)
	check("R-a2", code, out, "R-a2", "app-only access", "device sign-in", "Allow public client flows", "Delegated Mail.Read")
	e.fg.mutate(func() { e.fg.tokenErr = "" })
	os.Remove(e.cfg.ConfigPath)
	code, out = e.post("/api/workspace/folders", nil)
	check("R-a", code, out, "R-a", "isn't set up yet", "tenant and application ID", "mailarchive setup")

	// the ones only a constructor or an injected error reaches: R-j, R-n, R-s, R-v, R-x, R-i, R-timeout
	for _, c := range []struct {
		id    string
		ref   refusal
		names []string
	}{
		{"R-j", refusalJ(10001), []string{"You've picked 10,001 individual messages", "the most one selection can hold", "Select current folder"}},
		{"R-n", refusalN(), []string{"folder picker isn't available", "`zenity` program on Linux", "Type the folder path"}},
		{"R-s", refusalS(), []string{"more than 10,000 folders", "more than the workspace can show", "Export from the archive instead"}},
		{"R-v", refusalV("Inbox"), []string{"limiting requests right now (throttled)", "couldn't list Inbox", "Try again in a few minutes"}},
		{"R-x", refusalX("Inbox"), []string{"This list expired", "reloading Inbox", "from the newest message"}},
		{"R-y", refusalY(), []string{"restarted", "reload the page", "selection can't be reused"}},
		{"R-m", refusalM(time.Date(2026, 10, 9, 14, 2, 0, 0, time.UTC), 812, 2088), []string{"An export is already running (started 14:02, 812 of 2,088 done)", "only one runs at a time", "cancel it on Export activity"}},
		{"R-timeout", refusalTimeout(), []string{"didn't answer in time", "try again in a few minutes"}},
		{"R-v via 429", graphRefusal(&graph.StatusError{Code: 429, Text: "Too Many Requests"}, "Inbox", "device"), []string{"throttled", "Try again in a few minutes"}},
		{"R-s via the folder cap", graphRefusal(&graph.FolderCapError{}, "your folders", "device"), []string{"more than 10,000 folders"}},
		{"a server error", graphRefusal(&graph.StatusError{Code: 500, Text: "SECRET-BODY"}, "Inbox", "device"), []string{"HTTP 500", "Inbox", "try again in a few minutes"}},
	} {
		t.Run(c.id, func(t *testing.T) {
			if c.ref.Status == 0 {
				c.ref.Status = statusRefused
			}
			msg := c.ref.Text
			code := c.ref.Status
			if c.id == "R-y" {
				assure.Refused(t, code, msg, assure.Code(http.StatusForbidden), assure.Names(c.names...), forbid)
				return
			}
			assure.Refused(t, code, msg, assure.Code(statusRefused), assure.Names(c.names...), forbid, assure.Forbid("SECRET-BODY"))
		})
	}
}

// covers: MA-434, R4, R29, R12
// Create folder at the endpoint: exactly one directory, at 0700, with a sanitized single
// segment name, under an existing destination outside the archive; an existing name (R-q), a
// separator, "..", a reserved name and an empty name (R-q), a parent inside the archive (R-f)
// and a missing parent (R-e) are refused with nothing made. The positive twin first.
func TestCreateFolderThroughTheEndpoint(t *testing.T) {
	e := newWsEnv(t)
	before := treeState(t, e.dest)
	out := e.ok("/api/workspace/mkdir", map[string]any{"parent": e.dest, "name": "My Export"})
	want := filepath.Join(e.dest, "My Export")
	if out["path"] != want {
		t.Fatalf("Create folder: %v", out)
	}
	after := treeState(t, e.dest)
	if len(after) != len(before)+1 || after["My Export"] != "dir" {
		t.Errorf("Create folder made %v", diffKeys(before, after))
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(want); err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("the new folder's mode: %v %v", fi, err)
		}
	}
	os.MkdirAll(filepath.Join(e.out, "store"), 0o755)
	state := treeState(t, e.dir)
	for _, c := range []struct {
		name, parent, dirName, id string
	}{
		{"an existing name", e.dest, "My Export", "R-q"},
		{"a separator", e.dest, "a/b", "R-q"},
		{"a backslash", e.dest, `a\b`, "R-q"},
		{"dot dot", e.dest, "..", "R-q"},
		{"a reserved name", e.dest, "CON", "R-q"},
		{"an empty name", e.dest, "  ", "R-q"},
		{"a parent inside the archive", filepath.Join(e.out, "store"), "fresh", "R-f"},
		{"the archive itself", e.out, "fresh", "R-f"},
		{"a missing parent", filepath.Join(e.dir, "nope"), "fresh", "R-e"},
	} {
		code, out := e.post("/api/workspace/mkdir", map[string]any{"parent": c.parent, "name": c.dirName})
		if out["ok"] != false || out["id"] != c.id || code != statusRefused {
			t.Errorf("%s: %d %v, want %s", c.name, code, out, c.id)
		}
	}
	if got := treeState(t, e.dir); !reflect.DeepEqual(got, state) {
		t.Errorf("a refused Create folder made something: %v", diffKeys(state, got))
	}
}
