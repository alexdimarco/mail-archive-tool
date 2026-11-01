package desktop

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-984, R36, S41
// Every reload the page starts itself is held while a delete's result is held (§3.1 "After a
// Delete"; R3:NF-R3-RESULT-RELOAD): dashboard.js, encryption.js and the sign-in script reload
// only through reloadPage, which does not reload while a result is on the page and says the
// page refreshes once it is dismissed; the page reloads itself after the last result is
// dismissed, and nowhere else. Positive twin first: the page script still reloads after the
// actions that did (a Switch, a Settings save of a new location).
func TestEveryReloadThePageStartsIsHeldWhileAResultIsHeld(t *testing.T) {
	sw := jsHandlerFor(t, "document.querySelectorAll('[data-switch-archive]')")
	if !strings.Contains(sw, "setTimeout(()=>reloadPage(),500)") {
		t.Fatalf("the positive twin: a Switch no longer reloads the page: %s", sw)
	}
	rp := jsFunc(t, "reloadPage")
	held, rl := strings.Index(rp, "if(delInFlight>0||document.querySelector('[data-held-id]')){"), strings.Index(rp, "location.reload()")
	if held < 0 || rl < 0 || held > rl || !strings.Contains(rp[held:rl], "return;") {
		t.Errorf("reloadPage reloads while a result is held: %s", rp)
	}
	if !strings.Contains(rp, "delWait=true") || !strings.Contains(rp, "data-held-wait") {
		t.Errorf("reloadPage does not say the page refreshes once the result is dismissed: %s", rp)
	}
	// Only reloadPage and the last dismissal reload the page.
	if n := strings.Count(dashboardJS, "location.reload("); n != 2 {
		t.Errorf("dashboard.js reloads in %d places, want 2 (reloadPage and the last dismissal)", n)
	}
	dis := jsFunc(t, "delDismiss")
	if !strings.Contains(dis, "if((reload||delWait)&&delInFlight===0&&!document.querySelector('[data-held-id]')){location.reload();return 'reload';}") {
		t.Errorf("the last dismissal does not reload the page, or reloads while a result is held: %s", dis)
	}
	if strings.Contains(encryptionJS, "location.reload(") || strings.Count(encryptionJS, "reloadPage()") != 6 {
		t.Errorf("encryption.js reloads other than through reloadPage (%d through it)", strings.Count(encryptionJS, "reloadPage()"))
	}
	if strings.Count(signinJS, "location.reload(") != 1 || !strings.Contains(signinJS, "const reload=()=>typeof reloadPage==='function'?reloadPage():location.reload();") {
		t.Error("the sign-in script reloads the Overview other than through reloadPage")
	}
}

// covers: MA-984, R36, S41
// A delete's result is shown with its lines, Save deletion record and Dismiss (§3.1, §8.1;
// R3:NEW-FINDING-record-held-vs-sent, R4:NF-R4-HELD-RESULTS-MANY): from the delete's reply and,
// when the page is drawn, from every held result the Overview carries. Save deletion record is a
// Blob of the record's exact bytes (decoded from base64, never re-encoded text), under the name
// the reply gives, any number of times; Dismiss posts the result's id. Where it sits follows
// "Where the result sits" (R3:NF-R3-ROW-KEPT, R4:NF-R4-ROW-KEPT-BUTTONS). Every key the result
// reads is in the delete's reply and in data-held-deletes: positive twin first, a real delete.
func TestADeletesResultIsShownSavableAndDismissable(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Shown")
	reply := e.planAndDelete(dir)
	assure.Reached(t, reply["ok"] == true && reply["id"] != "", "a real delete's reply, the result's source")
	held := e.heldOnPage()
	if len(held) != 1 {
		t.Fatalf("the page holds %d results", len(held))
	}
	var tags = map[string]bool{}
	for _, f := range reflect.VisibleFields(reflect.TypeOf(heldView{})) {
		tags[strings.Split(f.Tag.Get("json"), ",")[0]] = true
	}

	show := jsFunc(t, "delShowHeld")
	read := readKeys(show, "v")
	if strings.Join(read, ",") != "folder,id,lines,recordBase64,recordName,row,rowRemoved" {
		t.Fatalf("the result reads %v", read)
	}
	for _, k := range read {
		if _, ok := reply[k]; !ok {
			t.Errorf("the result reads v.%s, which the delete's reply does not carry", k)
		}
		if _, ok := held[0][k]; !ok || !tags[k] {
			t.Errorf("the result reads v.%s, which data-held-deletes does not carry", k)
		}
	}
	for _, want := range []string{
		"v.lines", "'Save deletion record'", "'Dismiss'", "Close without saving the deletion record?", "'Close without saving'", "'Keep it'",
		"h.dataset.heldId=v.id", "h.dataset.row=v.row",
		// where it sits
		"const li=v.rowRemoved?null:delRowOf(v.row);", "if(li)li.append(h);", "$('sec-archives')", "$('heldDeletes')",
	} {
		if !strings.Contains(show, want) {
			t.Errorf("the result lacks %q", want)
		}
	}
	noBrowserDialogs(t, "delShowHeld", show)
	// Drawing a result changes no row and no card: at a page load the server drew them as they are
	// now (MA-986, del-s3b-A1-04); a delete's reply changes them, as the server says.
	for _, bad := range []string{"delRowGone(", "delMarkUnfinished(", "delPageArchive(", "drawnFor"} {
		if strings.Contains(show, bad) {
			t.Errorf("drawing a result calls %s: %s", bad, show)
		}
	}
	after := jsFunc(t, "delAfterDelete")
	for _, want := range []string{"(d.rows||[]).forEach(p=>{const li=delRowOf(p);", "if(d.rowRemoved)delRowGone(li,", "else if(d.unfinished)delMarkUnfinished(li);", "if(d.page)delPageArchive(d.page);"} {
		if !strings.Contains(after, want) {
			t.Errorf("a delete's reply does not change the rows and the page as the server says: %q missing from %s", want, after)
		}
	}
	for _, k := range readKeys(after, "d") {
		if _, ok := reply[k]; !ok {
			t.Errorf("a delete's reply is read for d.%s, which it does not carry", k)
		}
	}
	noBrowserDialogs(t, "delAfterDelete", after)
	gone := jsFunc(t, "delRowGone")
	// Its own buttons and state only: the page's note and question moved into the row (noteBeside)
	// keep theirs.
	if !strings.Contains(gone, "Array.from(li.children).forEach(x=>{if(x.tagName==='BUTTON'||x.classList.contains('row-state'))x.remove();});") {
		t.Errorf("delRowGone does not take the row's own buttons away: %s", gone)
	}

	// Save deletion record: the exact bytes, any number of times.
	save := show[strings.Index(show, "'Save deletion record'"):]
	save = save[:strings.Index(save, "});")]
	for _, want := range []string{"Uint8Array.from(atob(v.recordBase64),c=>c.charCodeAt(0))", "delSaveFile(v.recordName,", "h.dataset.saved='1'"} {
		if !strings.Contains(save, want) {
			t.Errorf("Save deletion record lacks %q: %s", want, save)
		}
	}
	for _, bad := range []string{"v.record)", "v.record,", "disabled=true", ".remove()", "delPost("} {
		if strings.Contains(save, bad) {
			t.Errorf("Save deletion record %q: it must save the decoded bytes, any number of times, sending nothing: %s", bad, save)
		}
	}
	// Dismiss: asks first when the record was not saved here, then posts the id.
	dis := show[strings.Index(show, "'Dismiss'"):]
	dis = dis[:strings.Index(dis, "});")]
	a, d := strings.Index(dis, "if(h.dataset.saved!=='1'){delAsk(h,null);return;}"), strings.Index(dis, "delDismiss(h,true)")
	if a < 0 || d < 0 || a > d || !strings.Contains(dis, "busy(") {
		t.Errorf("Dismiss does not ask before closing an unsaved record: %s", dis)
	}
	if f := jsFunc(t, "delDismiss"); !strings.Contains(f, "delPost('api/archive/delete/dismiss',{id:h.dataset.heldId})") || !strings.Contains(f, "h.remove()") {
		t.Errorf("delDismiss does not post the result's id and take the result away: %s", f)
	}
	ask := jsFunc(t, "delAsk")
	noBrowserDialogs(t, "delAsk", ask)
	if !strings.Contains(ask, "busy(yes,") || !strings.Contains(ask, "delDismiss(h,!then)") || !strings.Contains(ask, "if(then&&r!=='reload')then();") {
		t.Errorf("the in-page question does not close the result, then go on: %s", ask)
	}

	// A new Delete… of a row whose result was not saved asks first.
	h := jsHandlerFor(t, "document.querySelectorAll('[data-delete-archive]')")
	u, p := strings.Index(h, "const h=delUnsavedFor(b.dataset.path);if(h){delAsk(h,()=>delPlan(b,li));return;}"), strings.LastIndex(h, "delPlan(b,li)")
	if u < 0 || p < 0 || u > p {
		t.Errorf("a new Delete… of a row with an unsaved record does not ask first: %s", h)
	}
	if f := jsFunc(t, "delUnsavedFor"); !strings.Contains(f, "h.dataset.row===path&&h.dataset.saved!=='1'") {
		t.Errorf("delUnsavedFor does not find the row's unsaved result: %s", f)
	}

	// The page draws every held result when it loads, and a delete's reply at once.
	if !strings.Contains(dashboardJS, "JSON.parse(hd.dataset.heldDeletes||'[]')") || !strings.Contains(dashboardJS, ".forEach(v=>delShowHeld(v))") {
		t.Error("the page does not draw the held results when it loads")
	}
	panel := jsFunc(t, "delPanel")
	if !strings.Contains(panel, "if(d.id){done(true);delClose();delShowHeld(d);delAfterDelete(d);return;}") {
		t.Error("a delete's reply that names a held result is not shown at once")
	}
}

// covers: MA-984, R36, S41
// When a result marks the row not finished, the row shows "Delete not finished: click Delete
// again" in place of Switch, Pause and Resume at once, keeping Delete… and Forget; when it is
// the archive the page was drawn for, Archive now and Run now and their sidebar links go too
// (R4:NF-R4-ROW-KEPT-BUTTONS). Positive twin first: the server's own row for a delete not
// finished says the same words the page puts in.
func TestAResultMarksTheRowAndThePageAtOnce(t *testing.T) {
	e := newDeleteEnv(t)
	stopped := e.plainArchive("Stopped")
	stopAt2(t, e, stopped)
	row := rowFor(t, e.page(), stopped)
	tag := `<span class="row-state"><span class="archive-tag">Delete not finished: click Delete again</span></span>`
	if !strings.Contains(row, tag) {
		t.Fatalf("the positive twin: the server's row for a delete not finished: %s", row)
	}
	mark := jsFunc(t, "delMarkUnfinished")
	for _, want := range []string{"li.querySelector('.row-state')", "while(st.firstChild)st.removeChild(st.firstChild);", "t.className='archive-tag'",
		"t.textContent=delUnfinishedText", "li.setAttribute('data-delete-unfinished','')"} {
		if !strings.Contains(mark, want) {
			t.Errorf("delMarkUnfinished lacks %q: %s", want, mark)
		}
	}
	if strings.Contains(mark, "data-delete-archive") || strings.Contains(mark, "data-forget-archive") || strings.Contains(mark, "querySelectorAll('button')") {
		t.Errorf("delMarkUnfinished takes Delete… or Forget away: %s", mark)
	}
	if !strings.Contains(dashboardJS, "const delUnfinishedText='Delete not finished: click Delete again';") {
		t.Error("the page's words are not the row's")
	}
	pa := jsFunc(t, "delPageArchive")
	for _, id := range []string{"'archiveBtn'", "'runBackupBtn'", "'navArchive'", "'navRunNow'"} {
		if !strings.Contains(pa, id) {
			t.Errorf("delPageArchive leaves %s on the page", id)
		}
	}
	if !strings.Contains(pa, "const text=delPageText[kind]||delUnfinishedText;") || !strings.Contains(dashboardJS, "const delPageText={unfinished:delUnfinishedText,") {
		t.Errorf("delPageArchive does not say the delete is not finished: %s", pa)
	}
	noBrowserDialogs(t, "delMarkUnfinished", mark)
	noBrowserDialogs(t, "delPageArchive", pa)
	var v []map[string]any
	if err := json.Unmarshal([]byte(heldJSON(e.d.deletes.views())), &v); err != nil || len(v) != 1 || v[0]["unfinished"] != true || v[0]["rowRemoved"] != false {
		t.Errorf("the held result does not say the row stays not finished: %v %v", v, err)
	}
}
