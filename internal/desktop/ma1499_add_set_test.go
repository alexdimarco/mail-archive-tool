package desktop

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/wizard"
)

// setRig is a Desktop with a current archive of its own and a list of archives, ready for Make a new
// archive…, with the add flow stood in by the seam (no dialog opens). Its clock ticks a second at
// each look, so archives listed by separate writes would carry separate times.
type setRig struct {
	t       *testing.T
	d       *dashboard
	idx     string
	current string
	parent  string
}

func newSetRig(t *testing.T) *setRig {
	t.Helper()
	dir := t.TempDir()
	g := &setRig{t: t, idx: filepath.Join(dir, archindex.Name), parent: filepath.Join(dir, "Mail archives")}
	g.current = archiveAt(t, dir, "Current", map[string]string{"path:/mail/a.mbox": "a"})
	if err := archindex.WriteFile(g.idx, archindex.Index{Entries: []archindex.Entry{{Path: g.current, AddedAt: at0, LastSeenAt: at0}}}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "s.json")
	if err := SaveSettings(settings, Settings{Out: g.current}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(g.parent, 0o700); err != nil {
		t.Fatal(err)
	}
	g.d = newDashboard(Config{SettingsPath: settings, IndexPath: g.idx, GOOS: "darwin"})
	tick := 0
	g.d.now = func() time.Time { tick++; return at0.Add(time.Duration(tick) * time.Second) }
	return g
}

// archive makes the set's archive folder name in the parent, holding an archive (a manifest) when
// holds is true, and returns its path.
func (g *setRig) archive(name string, holds bool) string {
	g.t.Helper()
	p := filepath.Join(g.parent, name)
	if err := os.Mkdir(p, 0o700); err != nil {
		g.t.Fatal(err)
	}
	if holds {
		writeManifest(g.t, p)
	}
	return p
}

// add runs Make a new archive… with the add flow answering made and err, and returns the reply's
// body and its decoded fields.
func (g *setRig) add(made []wizard.Made, err error) (string, setReply) {
	g.t.Helper()
	restore := addArchivesFn
	defer func() { addArchivesFn = restore }()
	addArchivesFn = func() ([]wizard.Made, error) { return made, err }
	req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/add-archive", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", g.d.csrf)
	rec := httptest.NewRecorder()
	g.d.addArchive(rec, req)
	var r setReply
	if jerr := json.Unmarshal(rec.Body.Bytes(), &r); jerr != nil {
		g.t.Fatalf("the reply is not JSON: %v %s", jerr, rec.Body.String())
	}
	return rec.Body.String(), r
}

type setReply struct {
	OK      bool     `json:"ok"`
	Stopped bool     `json:"stopped"`
	Message string   `json:"message"`
	Paths   []string `json:"paths"`
}

// listed is the list of archives' entries, by path.
func (g *setRig) listed() map[string]archindex.Entry {
	g.t.Helper()
	idx, err := archindex.ReadFile(g.idx)
	if err != nil {
		g.t.Fatal(err)
	}
	out := map[string]archindex.Entry{}
	for _, e := range idx.Entries {
		out[cleanPath(e.Path)] = e
	}
	return out
}

// stillCurrent reports whether the Desktop's current archive is still the one it had (MA-696).
func (g *setRig) stillCurrent() bool {
	return cleanPath(g.d.cfg.effectiveOut()) == cleanPath(g.current)
}

func setMade(folder, name string, o wizard.MadeOutcome, reason, parent string) wizard.Made {
	return wizard.Made{Folder: folder, Name: name, Outcome: o, Reason: reason, New: folder != "", Parent: parent}
}

// covers: MA-1499, MA-696, S90
// MailArchive Desktop's Make a new archive… after One archive for each mailbox
// (docs/design-per-mailbox-archives.md §10, revision 2's C8 and C11): every folder of the set that
// holds an archive is listed under Your archives in one write of the list, none is made current, and
// the note says §10's words. Positive twin first: three archives that all finished are listed (with
// one time: one write), the current archive stays current, the note is "3 archives added — they are
// now in Your archives." and the page reloads; the page then lists each with Switch to this archive.
// A set of one archive says today's words. Some did not finish: the count of those that finished,
// each that did not by name with its reason (an archive not started "was not started"), and how to
// finish them, naming the set's folder; an archive stopped part way with the mail copied so far is
// listed, one with no archive in its folder and one never made are not; the note stays on the page.
// Stopped part way with one left (today's stopped case): the same, "To finish it", naming the folder.
// The singular and zero leads. An error after the archives: they are still listed, and the note says
// the error after. A list that cannot be written: the note says they are safe. A name with markup
// reaches the page only as JSON text (X15).
func TestMakeANewArchiveListsEveryArchiveOfASet(t *testing.T) {
	// Positive twin: every archive finished.
	g := newSetRig(t)
	a, b, c := g.archive("ann@qz-a.example", true), g.archive("bob@qz-b.example", true), g.archive("Local Folders", true)
	_, r := g.add([]wizard.Made{
		setMade(a, "ann@qz-a.example", wizard.MadeFinished, "", g.parent),
		setMade(b, "bob@qz-b.example", wizard.MadeFinished, "", g.parent),
		setMade(c, "Local Folders", wizard.MadeFinished, "", g.parent),
	}, nil)
	if !r.OK || r.Stopped || r.Message != "3 archives added — they are now in Your archives." {
		t.Fatalf("positive twin: %+v", r)
	}
	l := g.listed()
	if len(l) != 4 {
		t.Fatalf("positive twin: listed %v", l)
	}
	at := l[cleanPath(a)].AddedAt
	for _, p := range []string{a, b, c} {
		if e, ok := l[cleanPath(p)]; !ok || !e.AddedAt.Equal(at) {
			t.Errorf("positive twin: %s listed %v at %v, want all three in one write at %v", p, ok, e.AddedAt, at)
		}
	}
	if !g.stillCurrent() {
		t.Errorf("a set made an archive current: %q", g.d.cfg.effectiveOut())
	}
	page := getOverview(t, g.d.cfg)
	list := pageSpan(page, "sec-archives", `id="sec-add-archive"`)
	for _, p := range []string{a, b, c} {
		if !strings.Contains(list, `data-switch-archive data-path="`+p+`"`) {
			t.Errorf("Your archives does not offer Switch to this archive for %s", p)
		}
	}

	// A set of one archive: today's words.
	g = newSetRig(t)
	a = g.archive("ann@qz-a.example", true)
	if _, r := g.add([]wizard.Made{setMade(a, "ann@qz-a.example", wizard.MadeFinished, "", g.parent)}, nil); !r.OK || r.Message != "Archive added — it is now in your list." || len(g.listed()) != 2 {
		t.Errorf("a set of one: %+v", r)
	}

	// Some did not finish.
	g = newSetRig(t)
	a, b = g.archive("ann@qz-a.example", true), g.archive("bob@qz-b.example", true)
	stopped, failed := g.archive("cy@qz-c.example", true), g.archive("dee@qz-d.example", false)
	why := "its folder " + failed + " is not there any more, so nothing was copied into it"
	_, r = g.add([]wizard.Made{
		setMade(a, "ann@qz-a.example", wizard.MadeFinished, "", g.parent),
		setMade(b, "bob@qz-b.example", wizard.MadeFinished, "", g.parent),
		setMade(stopped, "cy@qz-c.example", wizard.MadeStopped, "the export was cancelled", g.parent),
		setMade(failed, "dee@qz-d.example", wizard.MadeFailed, why, g.parent),
		setMade("", "Local Folders", wizard.MadeNotStarted, "", g.parent),
	}, nil)
	want := "2 archives added — they are now in Your archives. cy@qz-c.example did not finish: the export was cancelled. dee@qz-d.example did not finish: " + why + ". Local Folders was not started. To finish them, click Make a new archive… again, choose One archive for each mailbox and choose the same folder, " + g.parent + "."
	if r.OK || !r.Stopped || r.Message != want {
		t.Errorf("some did not finish:\n got %+v\nwant %q", r, want)
	}
	l = g.listed()
	_, okStopped := l[cleanPath(stopped)]
	_, okFailed := l[cleanPath(failed)]
	if len(l) != 4 || !okStopped || okFailed {
		t.Errorf("some did not finish: listed %v (want the two finished and the stopped one with its mail; never a folder with no archive)", l)
	}
	if !g.stillCurrent() {
		t.Error("some did not finish: an archive was made current")
	}

	// Stopped part way, one left (today's stopped case): the same, naming the folder.
	g = newSetRig(t)
	a, b, stopped = g.archive("ann@qz-a.example", true), g.archive("bob@qz-b.example", true), g.archive("cy@qz-c.example", true)
	_, r = g.add([]wizard.Made{
		setMade(a, "ann@qz-a.example", wizard.MadeFinished, "", g.parent),
		setMade(b, "bob@qz-b.example", wizard.MadeFinished, "", g.parent),
		setMade(stopped, "cy@qz-c.example", wizard.MadeStopped, "the export was cancelled", g.parent),
	}, nil)
	want = "2 archives added — they are now in Your archives. cy@qz-c.example did not finish: the export was cancelled. To finish it, click Make a new archive… again, choose One archive for each mailbox and choose the same folder, " + g.parent + "."
	if r.OK || r.Message != want || len(g.listed()) != 4 {
		t.Errorf("stopped part way:\n got %+v\nwant %q", r, want)
	}

	// The singular and zero leads.
	g = newSetRig(t)
	a = g.archive("ann@qz-a.example", true)
	_, r = g.add([]wizard.Made{
		setMade(a, "ann@qz-a.example", wizard.MadeFinished, "", g.parent),
		setMade("", "bob@qz-b.example", wizard.MadeNotStarted, "", g.parent),
	}, nil)
	if want := "1 archive added — it is now in Your archives. bob@qz-b.example was not started. To finish it, click Make a new archive… again, choose One archive for each mailbox and choose the same folder, " + g.parent + "."; r.Message != want {
		t.Errorf("one finished:\n got %q\nwant %q", r.Message, want)
	}
	g = newSetRig(t)
	stopped = g.archive("ann@qz-a.example", true)
	_, r = g.add([]wizard.Made{
		setMade(stopped, "ann@qz-a.example", wizard.MadeStopped, "the export was cancelled", g.parent),
		setMade("", "bob@qz-b.example", wizard.MadeNotStarted, "", g.parent),
	}, nil)
	if want := "No archive was finished. ann@qz-a.example did not finish: the export was cancelled. bob@qz-b.example was not started. To finish them, click Make a new archive… again, choose One archive for each mailbox and choose the same folder, " + g.parent + "."; r.Message != want || len(g.listed()) != 2 {
		t.Errorf("none finished:\n got %q\nwant %q (listed %d)", r.Message, want, len(g.listed()))
	}

	// An error after the archives: they are listed, and the note says it after.
	g = newSetRig(t)
	a, b = g.archive("ann@qz-a.example", true), g.archive("bob@qz-b.example", true)
	_, r = g.add([]wizard.Made{
		setMade(a, "ann@qz-a.example", wizard.MadeFinished, "", g.parent),
		setMade(b, "bob@qz-b.example", wizard.MadeFinished, "", g.parent),
	}, errors.New("the schedule question could not be shown."))
	if want := "2 archives added — they are now in Your archives. The wizard then ended with an error: the schedule question could not be shown."; r.OK || r.Message != want || len(g.listed()) != 3 {
		t.Errorf("an error after the archives:\n got %+v\nwant %q", r, want)
	}

	// A list that cannot be written: the archives are safe, and the note says so.
	g = newSetRig(t)
	a, b = g.archive("ann@qz-a.example", true), g.archive("bob@qz-b.example", true)
	notADir := filepath.Join(t.TempDir(), "a file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	g.d.cfg.IndexPath = filepath.Join(notADir, archindex.Name)
	_, r = g.add([]wizard.Made{
		setMade(a, "ann@qz-a.example", wizard.MadeFinished, "", g.parent),
		setMade(b, "bob@qz-b.example", wizard.MadeFinished, "", g.parent),
	}, nil)
	if want := "The archives were made in " + g.parent + ", but they could not be added to the list automatically — they are safe; reload to see them."; r.OK || r.Message != want {
		t.Errorf("a list that cannot be written:\n got %+v\nwant %q", r, want)
	}

	// A name with markup reaches the page only as JSON text, which the page sets as textContent.
	g = newSetRig(t)
	a = g.archive("ann@qz-a.example", true)
	body, r := g.add([]wizard.Made{
		setMade(a, "ann@qz-a.example", wizard.MadeFinished, "", g.parent),
		setMade("", `<img src=x onerror=alert(1)>`, wizard.MadeNotStarted, "", g.parent),
	}, nil)
	if strings.Contains(body, "<img") || !strings.Contains(assure.Reached(t, r.Message, "the note"), `<img src=x onerror=alert(1)> was not started.`) {
		t.Errorf("a name with markup: %s", body)
	}
	if !strings.Contains(dashboardJS, "n.textContent=d.message||d.error||'Could not add an archive.'") {
		t.Error("the page does not set the add's note as text")
	}

	// The one write: the set's handler lists its folders through registerArchives, once.
	src, err := os.ReadFile("addarchive.go")
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout writes the source with "\r\n": read it with "\n" line ends, as written.
	src = []byte(strings.ReplaceAll(string(src), "\r\n", "\n"))
	set := string(src)[strings.Index(string(src), "func (d *dashboard) addedSet("):]
	set = set[:strings.Index(set, "\n}\n")]
	if strings.Count(set, "registerArchives(") != 1 || strings.Contains(set, "registerArchive(") || strings.Contains(set, "SaveSettings") {
		t.Error("the set's archives are not listed in one write, or one is made current")
	}
}
