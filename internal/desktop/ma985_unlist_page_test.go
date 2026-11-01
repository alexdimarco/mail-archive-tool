package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-985, R36, S41
// Remove from list asks its question in the row and acts only on Yes (design-archive-delete
// §3.2, §5.2; C3, C19): its handler moves the page's note beside the row (MA-955) before it says
// it is working, closes any delete panel, asks the route without confirm, and shows the route's
// own question in the row with Yes and No — never a browser confirm(). Yes posts confirm with
// exactly the names the question named, says what was done beside the row, takes the row's
// buttons away and reloads through reloadPage; No posts nothing and says nothing was changed.
// Positive twin first: the route's question reply carries the question and the names the page
// posts back.
func TestRemoveFromListAsksInTheRowAndActsOnlyOnYes(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Listed")
	_, q := e.post("/api/archive/unlist", map[string]any{"path": dir})
	assure.Reached(t, q["ask"] == true && q["question"] != nil && q["named"] != nil, "the route's question reply, the page's source")

	h := jsHandlerFor(t, "document.querySelectorAll('[data-unlist-archive]')")
	nb, cl, bz := strings.Index(h, "noteBeside(li)"), strings.Index(h, "delClose()"), strings.Index(h, "busy(")
	if nb < 0 || cl < 0 || bz < 0 || nb > bz || cl > bz {
		t.Errorf("Remove from list does not move the note beside the row and close a delete panel before it says it is working: %s", h)
	}
	if !strings.Contains(h, "delPost('api/archive/unlist',{path:b.dataset.path})") || !strings.Contains(h, "li.append(delUnlistAsk(li,b,q))") {
		t.Errorf("Remove from list does not ask the route first and show its question in the row: %s", h)
	}
	noBrowserDialogs(t, "Remove from list's handler", h)
	ask := jsFunc(t, "delUnlistAsk")
	noBrowserDialogs(t, "delUnlistAsk", ask)
	for _, k := range readKeys(ask+h, "q") {
		// error is a refusal's (MA-977), read only when ok is not true.
		if _, ok := q[k]; !ok && k != "error" {
			t.Errorf("the question reads q.%s, which the route's reply does not carry", k)
		}
	}
	for _, want := range []string{
		"w.className='note unlist-ask'", "t.textContent=q.question", "'Yes'", "'No'",
		"delPost('api/archive/unlist',{path:b.dataset.path,confirm:true,named:q.named})",
		"busy(yes,", "delRowGone(li,", "setTimeout(()=>reloadPage(),1500)",
		"Nothing was changed.",
	} {
		if !strings.Contains(ask, want) {
			t.Errorf("Remove from list's question lacks %q", want)
		}
	}
	// No sends nothing.
	no := ask[strings.Index(ask, "no.addEventListener('click',"):]
	no = no[:strings.Index(no, "});")]
	if strings.Contains(no, "delPost(") || strings.Contains(no, "fetch(") {
		t.Errorf("No sends a request: %s", no)
	}
	// Yes acts only on its reply: the row's buttons go only when the route removed it.
	yes := ask[strings.Index(ask, "yes.addEventListener('click',"):]
	if i, j := strings.Index(yes, "if(d.ok){"), strings.Index(yes, "delRowGone(li,"); i < 0 || j < i {
		t.Errorf("Yes takes the row away before the route removed it: %s", yes)
	}
}
