package desktop

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// writeOwnNamedSchedule records, for the archive at out, a schedule set up at the command
// line with its own -name, as `mailarchive schedule -name NAME ... -out <jobOut> -install`
// writes it.
func writeOwnNamedSchedule(t *testing.T, out, name, jobOut string) {
	t.Helper()
	s := schedule.Spec{Name: name, Interval: schedule.Weekly, At: "03:00", Exe: "/opt/mailarchive",
		Args: []string{"-input", filepath.Join(out, "x"), "-out", jobOut, "-mode", "incremental"}, Out: out}
	if err := schedule.WriteDescriptor(out, schedule.DescribeSpec(s)); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// A task this tool installed leaves its definition beside the wrapper path (bound.go):
		// what lets the name bind to this archive on Windows.
		x := schedule.SchtasksXMLPath(schedule.DefaultWrapperPath(name))
		if err := os.MkdirAll(filepath.Dir(x), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(x, []byte("<Task/>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// removeReply is what /api/schedule-remove answered.
type removeReply struct {
	OK      bool   `json:"ok"`
	Removed *bool  `json:"removed"`
	Note    string `json:"note"`
	Error   string `json:"error"`
}

func postRemove(t *testing.T, g *a3Rig) (removeReply, string) {
	t.Helper()
	body := postAction(g.h, "/api/schedule-remove", g.csrf, "127.0.0.1:8097", "http://127.0.0.1:8097").Body.String()
	var r removeReply
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("Remove's reply is not JSON: %s", body)
	}
	return r, body
}

// covers: MA-948, MA-942, R37
// Remove says what it removed (the guide's friction review, "Not checked", and its
// disposition: read in the code, now driven). The Desktop's Remove takes out the scheduler
// entry named for the archive folder (removeTaskAt → DefaultNameFor). A schedule set up at
// the command line with its own -name is another entry, so Remove left it in place while
// the page said "Removed.", and MA-942's words sent its owner to that Remove. Now:
//   - Remove's reply says "removed": false and a note when it removed nothing, and the
//     page's script shows that note as it is (no "Removed.", no state line read for the
//     folder's name, no reload);
//   - for a schedule under its own name, the note names it and gives the command that
//     removes it, quoted for this computer's shell, when the name binds to this archive
//     (schedule.RecordedBackupName, the binding `-out X -remove` and the wizard's Remove
//     use); otherwise generic words, never a name from a record that is not this archive's;
//   - the Scheduled backup card and Settings no longer send such an owner to Remove: they
//     say to remove it in a terminal, with the same command, and Remove is not shown.
// Remove itself is unchanged: it never acts on the record's name. Positive twin first: a
// command-line schedule under the folder's name is removed, with no note, as before.
func TestRemoveSaysWhatItRemoved(t *testing.T) {
	isolateHome(t)
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", t.TempDir())
	}

	// The positive twin: under the folder's own name, Remove is offered and removes it.
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	writeCommandLineSchedule(t, out)
	g.fake.tasks[out] = true
	p := g.page()
	if !shows(p, "To get Run now, click Remove below") || !strings.Contains(p, `id="schedRemove"`) {
		t.Fatal("the positive twin: a command-line schedule under the folder's name is not sent to Remove")
	}
	r, body := postRemove(t, g)
	if !r.OK || r.Removed == nil || !*r.Removed || r.Note != "" || g.fake.tasks[out] {
		t.Fatalf("the positive twin's Remove: %s; the entry left: %v", body, g.fake.tasks[out])
	}

	// A schedule set up at the command line under its own name, on a local archive this
	// computer can give a backup of its own (macOS here).
	own := tbProfileArchive(t)
	o := newA3Rig(t, own, "darwin")
	writeOwnNamedSchedule(t, own, "nightly-tb", own)
	cmd := "mailarchive schedule -out " + shellQuote(own, "darwin") + " -name nightly-tb -remove"
	page := o.page()
	for _, want := range []string{
		"Run now isn't shown for it: MailArchive Desktop can't run, change or remove a backup set up at the command line under its own name (nightly-tb). To get Run now, remove it in a terminal (your archive and its mail are kept), reload this page (press Command-R), then in Settings choose How often and click Save settings. That sets up a backup MailArchive Desktop can run, and Run now then appears here.",
		"It runs weekly on Sunday at 03:00 now. Removing it does not keep that: afterwards, choose How often and the time again in Settings.",
		"The command that removes it: " + cmd,
		"This schedule was set up at the command line; change it there. To get one MailArchive Desktop can run, with Run now, remove it in a terminal first (the Scheduled backup card says how), then choose How often here and click Save settings.",
	} {
		if !shows(page, want) {
			t.Errorf("the own-named schedule's page does not say %q", want)
		}
	}
	for _, bad := range []string{"click Remove below", "click Remove on the Scheduled backup card", "Remove below", `id="schedRemove"`} {
		if strings.Contains(page, bad) {
			t.Errorf("the own-named schedule's page still has %q, which does not reach it", bad)
		}
	}

	// Remove posted anyway (a page drawn before): nothing is removed and the reply says so,
	// with the command; nothing changes.
	desc := filepath.Join(own, schedule.DescriptorName)
	descBefore, setBefore := fileBytes(desc), fileBytes(o.settings)
	assure.Reached(t, descBefore, "the own-named schedule's record")
	r, body = postRemove(t, o)
	wantNote := "Nothing was removed. This archive's scheduled backup was set up at the command line under its own name, nightly-tb, and Remove here reaches only a backup named for this archive folder. To remove it (your archive and its mail are kept), type this in a terminal: " + cmd
	if !r.OK || r.Removed == nil || *r.Removed || said(body) != wantNote {
		t.Errorf("Remove of an own-named schedule answered %s\nwant removed:false and the note %q", body, wantNote)
	}
	if !bytes.Equal(descBefore, fileBytes(desc)) || !bytes.Equal(setBefore, fileBytes(o.settings)) || len(o.fake.removed) != 0 {
		t.Error("Remove changed something although it removed nothing")
	}

	// The page's script shows that note as it is: not "Removed.", no state line read for the
	// folder's name (it would say "not installed" of a schedule that is), no reload.
	i := strings.Index(dashboardJS, "function cardPost(")
	if i < 0 {
		t.Fatal("cardPost not found in the page script")
	}
	js := dashboardJS[i:]
	js = js[:strings.Index(js, "\n}\n")]
	early := "if(path==='api/schedule-remove'&&d.ok&&d.removed===false){if(n){n.hidden=false;n.textContent=said;}return;}"
	if k := strings.Index(js, early); k < 0 || k > strings.Index(js, "return schedState(said)") {
		t.Errorf("the page's script does not show a Remove that removed nothing as it is, before the state line and the reload:\n%s", js)
	}

	// Linux here (no backup of its own on this page): the command, and nothing about Remove.
	lin := tbProfileArchive(t)
	l := newA3Rig(t, lin, "linux")
	writeOwnNamedSchedule(t, lin, "nightly-tb", lin)
	lp := l.page()
	if want := "It was set up at the command line under its own name (nightly-tb), so change, repair or remove it there; MailArchive Desktop can't remove it. The command that removes it: mailarchive schedule -out " + shellQuote(lin, "linux") + " -name nightly-tb -remove"; !shows(lp, want) {
		t.Errorf("linux: the own-named schedule's card does not say %q", want)
	}
	if strings.Contains(lp, "Remove below") || strings.Contains(lp, `id="schedRemove"`) {
		t.Error("linux: the own-named schedule's card still offers Remove")
	}

	// A record whose name is not this archive's (it records a job for another archive that
	// is still there: a copied record): generic words, never that name.
	other := tbProfileArchive(t)
	cp := tbProfileArchive(t)
	c := newA3Rig(t, cp, "darwin")
	writeOwnNamedSchedule(t, cp, "someone-elses", other)
	if _, err := schedule.RecordedBackupName(cp); err == nil {
		t.Fatal("fixture: a copied record's name binds to this archive")
	}
	r, body = postRemove(t, c)
	if want := "Nothing was removed. This archive's scheduled backup was set up under a name of its own, and Remove here reaches only a backup named for this archive folder. Remove it at the command line where it was set up: mailarchive schedule -name followed by its name, then -remove."; r.Removed == nil || *r.Removed || said(body) != want {
		t.Errorf("Remove with a copied record answered %s, want %q", body, want)
	}
	if cpage := c.page(); strings.Contains(cpage, "someone-elses") || strings.Contains(body, "someone-elses") {
		t.Error("a name from a record that is not this archive's is shown")
	}

	// Nothing set up at all: the reply says nothing was found, never "Removed.".
	none := tbProfileArchive(t)
	n := newA3Rig(t, none, "darwin")
	r, body = postRemove(t, n)
	if want := "Nothing was removed: MailArchive found no scheduled backup for this archive folder."; r.Removed == nil || *r.Removed || said(body) != want {
		t.Errorf("Remove with nothing set up answered %s, want %q", body, want)
	}

	// The command is quoted for the computer's shell.
	for _, c := range []struct{ goos, out, want string }{
		{"windows", `C:\Mail Archive`, `mailarchive schedule -out "C:\Mail Archive" -name nightly-tb -remove`},
		{"darwin", "/Users/a/Mail Archive", `mailarchive schedule -out '/Users/a/Mail Archive' -name nightly-tb -remove`},
	} {
		if got := removeByNameCommand(c.goos, c.out, "nightly-tb"); got != c.want {
			t.Errorf("%s: the command is %q, want %q", c.goos, got, c.want)
		}
	}
}
