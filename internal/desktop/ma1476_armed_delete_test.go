package desktop

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// Delete when the check finishes, the server's half (docs/design-1.7.5-fixes.md §3.2; the owner's
// ruling of 2026-11-20, Q1: "Yes (Recommended)", a narrow exception to X14): armable(plan) says
// whether nothing in a plan needs the person's decision; the plan route sends it as armable; and the
// delete route refuses an armed request, keeping the token and deleting nothing, unless the held
// plan is armable by that same function and is the plan of the row the panel was opened for. The
// page's half is MA-1477.

// armableBase is a plain plan with nothing in it that needs a decision.
func armableBase() *archdelete.DeletePlan {
	return &archdelete.DeletePlan{Folder: "/archives/Mail", Name: "Mail", Kind: archdelete.Plain}
}

// isSet reports whether a reply's value says something: a non-empty string, list or map, true, or
// a number not zero.
func isSet(v any) bool {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return false
	}
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() > 0
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int64, reflect.Int32:
		return rv.Int() != 0
	case reflect.Float64:
		return rv.Float() != 0
	case reflect.Pointer, reflect.Interface:
		return !rv.IsNil()
	}
	return true
}

// armableKey is one key of the plan route's reply: whether a plan with it set needs the person's
// decision (blocking), and how to set it on a plan ("" set: the route sets it itself, or every plan
// has it).
type armableKey struct {
	blocking bool
	set      func(p *archdelete.DeletePlan)
}

// armableKeys classifies every key the plan route's reply carries (planView's and the route's own).
// A key not here fails MA-1476's key check: a later line that needs a decision cannot leave armable
// true without the test saying so.
var armableKeys = map[string]armableKey{
	// What needs a decision (§3.2's list).
	"unrecorded":      {true, func(p *archdelete.DeletePlan) { p.Unrecorded = []archdelete.Item{{Rel: "store/x.html"}} }},
	"unrecordedNames": {true, func(p *archdelete.DeletePlan) { p.Unrecorded = []archdelete.Item{{Rel: "store/x.html"}} }},
	"unrecordedBox":   {true, func(p *archdelete.DeletePlan) { p.Unrecorded = []archdelete.Item{{Rel: "store/x.html"}} }},
	"synced":          {true, func(p *archdelete.DeletePlan) { p.Synced = "OneDrive" }},
	"syncedLine":      {true, func(p *archdelete.DeletePlan) { p.Synced = "OneDrive" }},
	"checkYourself":   {true, func(p *archdelete.DeletePlan) { p.Schedules.CheckYourself = []string{"com.example.mail"} }},
	"notes": {true, func(p *archdelete.DeletePlan) {
		p.Schedules.Unconfirmed = []archdelete.Note{{Name: "x", Text: "A schedule MailArchive cannot confirm is this archive's."}}
	}},
	"heldLine":      {true, func(p *archdelete.DeletePlan) { p.Held = []string{"store/a.html"} }},
	"finishesLine":  {true, func(p *archdelete.DeletePlan) { p.Finishes = &archdelete.Unfinished{} }},
	"finishesNotes": {true, func(p *archdelete.DeletePlan) { p.Disagrees = true }},
	"rerun":         {true, func(p *archdelete.DeletePlan) { p.Rerun = archdelete.RerunNoAnchor }},
	"keyLine":       {true, func(p *archdelete.DeletePlan) { p.Kind = archdelete.Encrypted }},
	// Added 2026-11-20 by the 1.7.5 friction review (F7): the closed list of what needs a decision,
	// sent beside armable (decideReasons).
	"decide": {true, func(p *archdelete.DeletePlan) { p.Unrecorded = []archdelete.Item{{Rel: "store/x.html"}} }},
	"kind":          {true, func(p *archdelete.DeletePlan) { p.Kind = archdelete.Encrypted }},
	// What Delete keeps or simply does, and what every plan says.
	"kept":       {false, func(p *archdelete.DeletePlan) { p.Kept = []string{"my-notes.txt"} }},
	"keptNames":  {false, func(p *archdelete.DeletePlan) { p.Kept = []string{"my-notes.txt"} }},
	"paused":     {false, func(p *archdelete.DeletePlan) { p.Paused = true }},
	"pausedLine": {false, func(p *archdelete.DeletePlan) { p.Paused = true }},
	"files":      {false, func(p *archdelete.DeletePlan) { p.Files = 3 }},
	"bytes":      {false, func(p *archdelete.DeletePlan) { p.Bytes = 10 }},
	"stores":     {false, func(p *archdelete.DeletePlan) { p.Stores = []string{"mbox"} }},
	"schedules": {false, func(p *archdelete.DeletePlan) {
		p.Schedules.Entries = []archdelete.Entry{{Name: "mailarchive-1", When: "daily at 02:00"}}
	}},
	"scheduleLines": {false, func(p *archdelete.DeletePlan) {
		p.Schedules.Entries = []archdelete.Entry{{Name: "mailarchive-1", When: "daily at 02:00"}}
	}},
	"folder":       {false, nil},
	"name":         {false, nil},
	"size":         {false, nil},
	"typeLine":     {false, nil},
	"doesNotReach": {false, nil},
	"logLine":      {false, nil},
	"threeLine":    {false, nil},
	// The route's own, and the answer itself.
	"ok":       {false, nil},
	"token":    {false, nil},
	"row":      {false, nil},
	"planText": {false, nil},
	"planName": {false, nil},
	"armable":  {false, nil},
}

// covers: MA-1476, R36, S41
// armable is false for each line of a plan that needs the person's decision — mail copies the
// archive did not record, a synced folder, schedules to check by hand or the unconfirmed and
// other-computer notes, files another program holds, the finishing of an earlier delete, an
// encrypted archive — and true with only kept files and a paused archive; planView sends it as
// armable. Positive twin first: a plain plan with nothing to decide is armable.
func TestArmableIsFalseForEveryLineThatNeedsADecision(t *testing.T) {
	base := armableBase()
	assure.Reached(t, armable(base) && planView(base)["armable"] == true, "the positive twin: a plain plan with nothing to decide is armable, and planView says so")
	calm := armableBase()
	calm.Kept, calm.Paused = []string{"my-notes.txt", "photos"}, true
	if !armable(calm) || planView(calm)["armable"] != true {
		t.Error("kept files and a paused archive stop the armed delete; Delete keeps such files and the result names them")
	}
	for name, set := range map[string]func(p *archdelete.DeletePlan){
		"mail copies the archive did not record": armableKeys["unrecorded"].set,
		"a synced folder":                        armableKeys["synced"].set,
		"a schedule to check by hand":            armableKeys["checkYourself"].set,
		"an unconfirmed schedule":                armableKeys["notes"].set,
		"another computer's schedule": func(p *archdelete.DeletePlan) {
			p.Schedules.OtherHost = []archdelete.Note{{Name: "y", Host: "other", Text: "Another computer's schedule."}}
		},
		"files another program holds":      armableKeys["heldLine"].set,
		"an earlier delete it finishes":    armableKeys["finishesLine"].set,
		"a log that disagrees":             armableKeys["finishesNotes"].set,
		"a folder said finished elsewhere": func(p *archdelete.DeletePlan) { p.Elsewhere, p.Rerun = true, archdelete.RerunFolderGone },
		"a re-run":                         armableKeys["rerun"].set,
		"an encrypted archive":             armableKeys["keyLine"].set,
	} {
		p := armableBase()
		set(p)
		if armable(p) || planView(p)["armable"] != false {
			t.Errorf("%s: the plan is armable, so it could be deleted unseen", name)
		}
	}
}

// covers: MA-1476, R36, S41
// MA-1476's key check: armable must not drift from planView as lines are added. Every key the plan
// route's reply carries (a real reply's, and planView's for a plan with every optional line) is
// classified as needing a decision or not; a plan with any blocking key set is not armable, one with
// only a neutral key set is; and a key the table does not classify fails. Positive twin first: the
// real reply of a plain archive says armable.
func TestTheArmableKeyCheckClassifiesEveryKeyOfThePlansReply(t *testing.T) {
	e := newDeleteEnv(t)
	reply := e.plan(e.plainArchive("Keys"))
	assure.Reached(t, reply["ok"] == true && reply["armable"] == true, "the positive twin: a plain archive's plan is armable")

	keys := map[string]bool{}
	for k := range reply {
		keys[k] = true
	}
	full := armableBase()
	for _, k := range armableKeys {
		if k.set != nil {
			k.set(full)
		}
	}
	full.Schedules.OtherHost = []archdelete.Note{{Name: "y", Host: "other", Text: "Another computer's schedule."}}
	for k := range planView(full) {
		keys[k] = true
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		c, ok := armableKeys[k]
		if !ok {
			t.Errorf("the plan's reply carries %q, which MA-1476's key check does not classify: say whether it needs the person's decision, and make armable agree", k)
			continue
		}
		if c.set == nil {
			continue
		}
		p := armableBase()
		c.set(p)
		v := planView(p)
		if !isSet(v[k]) {
			t.Errorf("the key check's setter for %q does not set it: %v", k, v[k])
		}
		if armable(p) == c.blocking {
			t.Errorf("a plan with only %q set: armable is %v, want %v", k, armable(p), !c.blocking)
		}
	}
	for k := range armableKeys {
		if !keys[k] {
			t.Errorf("MA-1476's key check classifies %q, which the plan's reply no longer carries", k)
		}
	}
}

// addUnrecordedCopy puts a mail copy no record names beside a recorded message page of the archive
// in dir, so its plan holds an unrecorded copy (§4.3) and is not armable.
func addUnrecordedCopy(t *testing.T, dir string) {
	t.Helper()
	var page string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && page == "" && !d.IsDir() && strings.HasSuffix(p, ".html") && filepath.Dir(p) != dir {
			page = p
		}
		return nil
	})
	if page == "" {
		t.Fatal("the archive has no message page in a mail folder")
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(page), "zz-not-recorded.html"), []byte("<p>a copy</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1476, MA-972, R36, S41
// The delete route refuses an armed request (armed: true, from Delete when the check finishes)
// unless the held plan is armable, by the same function planView used, and is the plan of the row the
// request names; refused, it keeps the token and deletes nothing: no started line, no lock, no row
// changed. The typed name is checked first, as today. Delete permanently sends no armed, so a plan
// that is not armable still deletes once its name is typed. Positive twins first: an armable plan
// armed from its own row deletes; Delete permanently of a plan that is not armable deletes.
func TestAnArmedDeleteIsRefusedUnlessItsPlanIsArmableAndItsOwnRows(t *testing.T) {
	e := newDeleteEnv(t)
	armed := func(token, typed, row string) map[string]any {
		_, m := e.post("/api/archive/delete", map[string]any{"token": token, "typed": typed, "armed": true, "row": row})
		return m
	}

	// The positive twins.
	calm := e.plainArchive("Calm")
	p := e.plan(calm)
	if p["armable"] != true {
		t.Fatalf("the positive twin's plan is not armable: %v", p["armable"])
	}
	if r := armed(p["token"].(string), "Calm", calm); r["ok"] != true || r["status"] != archdelete.StatusComplete {
		t.Fatalf("the positive twin: an armable plan armed from its own row: status %v, error %q", r["status"], replyErr(r))
	}
	if _, err := os.Stat(calm); !os.IsNotExist(err) {
		t.Errorf("the armed delete left the folder: %v", err)
	}
	// The row is matched by the plan route's own rule (matchRow): another spelling of the same folder
	// (a link to it) is that row.
	if runtime.GOOS != "windows" {
		linked := e.plainArchive("Linked")
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(linked, link); err != nil {
			t.Fatal(err)
		}
		lp := e.plan(linked)
		if r := armed(lp["token"].(string), "Linked", link); r["ok"] != true || r["status"] != archdelete.StatusComplete {
			t.Errorf("an armed delete whose row is another spelling of its own row's folder: status %v, error %q", r["status"], replyErr(r))
		}
	}
	copied := e.plainArchive("Copied")
	addUnrecordedCopy(t, copied)
	p = e.plan(copied)
	if p["armable"] != false || p["unrecorded"] != float64(1) {
		t.Fatalf("a plan with an unrecorded copy: armable %v, unrecorded %v", p["armable"], p["unrecorded"])
	}
	if r := e.deleteWith(p["token"].(string), "Copied", false); r["ok"] != true || r["id"] == "" {
		t.Fatalf("the positive twin: Delete permanently of a plan that is not armable: status %v, error %q", r["status"], replyErr(r))
	}

	// The refusals: each keeps the token and changes nothing.
	held := e.plainArchive("Held")
	addUnrecordedCopy(t, held)
	other := e.plainArchive("Other")
	before, otherBefore := treeState(t, held), treeState(t, other)
	logBefore, heldBefore := deletionsLog(t, e.cfg), len(e.d.deletes.views())
	hp := e.plan(held)
	tok := hp["token"].(string)
	unchanged := func() bool {
		return reflect.DeepEqual(treeState(t, held), before) && reflect.DeepEqual(treeState(t, other), otherBefore) &&
			e.listed(held) && e.listed(other) && deletionsLog(t, e.cfg) == logBefore && len(e.d.deletes.views()) == heldBefore &&
			e.d.pending.get(tok, isDeletePlan) != nil && e.d.running.of(held) == nil
	}
	r := armed(tok, "Held", held)
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK),
		assure.Names("The check found something to look at before deleting, so nothing was deleted.", "Delete permanently"),
		assure.NoSideEffect(func() bool { return unchanged() && r["planUsed"] == nil }))

	// Its own row's plan, armable, armed from another row; from no row; from a folder that is no row.
	ap := e.plan(other)
	atok := ap["token"].(string)
	for what, row := range map[string]string{"another row": held, "no row": "", "a folder that is no row": t.TempDir()} {
		t.Logf("armed from %s", what)
		r := armed(atok, "Other", row)
		assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK),
			assure.Names("another row's panel", "Click Delete… on the archive's own row"),
			assure.NoSideEffect(func() bool { return unchanged() && e.d.pending.get(atok, isDeletePlan) != nil }))
	}

	// The typed name is checked first, as today: a wrong name from another row is told the name.
	r = armed(atok, "other", held)
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK), assure.Names("does not match", "Other"),
		assure.NoSideEffect(func() bool { return unchanged() && e.d.pending.get(atok, isDeletePlan) != nil }))

	// The refused plan is still deleted by Delete permanently, as the refusal says.
	if r := e.deleteWith(tok, "Held", false); r["ok"] != true || r["id"] == "" {
		t.Errorf("after the armed refusal, Delete permanently of the kept token: status %v, error %q", r["status"], replyErr(r))
	}
}
