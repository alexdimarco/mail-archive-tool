package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/graph"
)

// Tombstones of the extraction workspace's adversarial pass (docs/review-extraction-
// workspace-adversarial.md), the desktop half.

// pausingStore is a token store that stops, once, inside putIfNewer's load: after the
// entry was read and before the write-back decides on it.
type pausingStore struct {
	mu      sync.Mutex
	data    []byte
	paused  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *pausingStore) LoadToken() ([]byte, error) {
	s.mu.Lock()
	d := append([]byte(nil), s.data...)
	s.mu.Unlock()
	pcs := make([]uintptr, 24)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		fr, more := frames.Next()
		if strings.Contains(fr.Function, "putIfNewer") {
			s.once.Do(func() {
				close(s.paused)
				<-s.release
			})
			break
		}
		if !more {
			break
		}
	}
	return d, nil
}

func (s *pausingStore) StoreToken(b []byte) error {
	s.mu.Lock()
	s.data = append([]byte(nil), b...)
	s.mu.Unlock()
	return nil
}

func (s *pausingStore) ClearToken() error {
	s.mu.Lock()
	s.data = nil
	s.mu.Unlock()
	return nil
}

func (s *pausingStore) upn() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tc struct {
		UPN string `json:"upn"`
	}
	_ = json.Unmarshal(s.data, &tc)
	return tc.UPN
}

func tokenJSON(upn, refresh string, ttl time.Duration) []byte {
	return []byte(`{"upn":"` + upn + `","token":{"access_token":"AT0","refresh_token":"` + refresh + `","token_type":"Bearer","expiry":"` +
		time.Now().Add(ttl).UTC().Format(time.RFC3339Nano) + `"}}`)
}

// covers: MA-515, R17, R5, S20
// EW-AC4 tombstone. graph serialises a live source's token write-back with tokenWriteMu so
// that a sign-in made inside its load-compare-write window is not overwritten, but the
// dashboard saved a new sign-in straight to the store without taking it: a rotation of
// alice's token that had read the cache before bob signed in then wrote alice's token over
// bob's, and the saved sign-in silently went back to the previous account. The save now goes
// through graph.PersistSignIn, under the same mutex. The write-back is held between its
// load and its write, bob's sign-in is saved meanwhile, and bob's must be what stays.
func TestSignInSaveIsOrderedWithALiveWriteBack(t *testing.T) {
	st := &pausingStore{data: tokenJSON("alice@contoso.org", "RT-A0", 5*time.Second), paused: make(chan struct{}), release: make(chan struct{})}
	e := newWsEnv(t, func(e *wsEnv) { e.cfg.TokenStore = st })
	foldersDone := make(chan struct{})
	go func() { defer close(foldersDone); e.postRaw("/api/workspace/folders", nil) }()
	select {
	case <-st.paused:
	case <-time.After(10 * time.Second):
		t.Fatal("alice's write-back never reached its load")
	}
	persisted := make(chan error, 1)
	go func() { persisted <- e.d.persistSignIn(tokenJSON("bob@contoso.org", "RT-B0", time.Hour)) }()
	time.Sleep(250 * time.Millisecond) // bob's save is made while alice's write-back is between its load and its write
	close(st.release)
	select {
	case err := <-persisted:
		if err != nil {
			t.Fatalf("saving bob's sign-in: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("bob's sign-in was never saved")
	}
	<-foldersDone
	if got := st.upn(); got != "bob@contoso.org" {
		t.Errorf("after bob signed in the saved sign-in is %q: alice's rotated token was written over it", got)
	}
}

// covers: MA-411, R29, R12
// EW-AC5 tombstone. The first workspace request builds the Graph session (a GET /me) with
// the lock released; a sign-in that changed meanwhile dropped a session that did not exist
// yet, and the finished build then cached the OLD account's session for the life of the
// process, so every preview carried the old account and every Start was refused R-l. The
// session is now cached only if no sign-in change came in while it was built; otherwise
// it is built again for whoever is signed in now.
func TestSessionBuiltAcrossASignInChangeIsNotKept(t *testing.T) {
	e := newWsEnv(t)
	inMe, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.fg.meFn = func() {
		once.Do(func() {
			close(inMe)
			<-release
		})
	}
	done := make(chan struct{})
	go func() { defer close(done); e.postRaw("/api/workspace/folders", nil) }()
	select {
	case <-inMe:
	case <-time.After(10 * time.Second):
		t.Fatal("the session build never reached /me")
	}
	// bob signs in while alice's session is being built
	e.writeToken("bob@contoso.org", time.Hour)
	e.fg.mutate(func() { e.fg.upn = "bob@contoso.org" })
	e.d.signInChanged("bob@contoso.org")
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the folders request never finished")
	}
	e.d.ws.mu.Lock()
	s := e.d.ws.sess
	e.d.ws.mu.Unlock()
	if s != nil && s.upn != "bob@contoso.org" {
		t.Errorf("the cached session is for %s, but bob is signed in", s.upn)
	}
	if s == nil {
		t.Error("no session was cached for the signed-in account")
	}
}

// covers: MA-409, R29, S52
// EW-AC6 tombstone. buildTree builds children from the parentFolderId Graph reports, so two
// folders that report each other as parent are in the tree, are not roots, and each lists the
// other as a child; wsTree.subtree recursed with no visited set, and Go's stack overflow is a
// fatal error that recover cannot catch: the whole dashboard process died on a preview. A
// subtree now lists each folder once.
func TestSubtreeOfAParentCycleTerminates(t *testing.T) {
	tr := buildTree([]graph.FolderInfo{
		{ID: "X", ParentID: "Y", DisplayName: "X"},
		{ID: "Y", ParentID: "X", DisplayName: "Y"},
		{ID: "Z", ParentID: "", DisplayName: "Z"},
		{ID: "Z1", ParentID: "Z", DisplayName: "Z1"},
	})
	if got := tr.subtree("Z"); strings.Join(got, ",") != "Z,Z1" { // the twin: an ordinary tree
		t.Fatalf("an ordinary subtree: %v", got)
	}
	got := tr.subtree("X")
	if len(got) != 2 || got[0] != "X" || got[1] != "Y" {
		t.Errorf("the subtree of a parent cycle is %v, want each folder once", got)
	}
	// And through the selection resolver: a cached tree that knew X as a root.
	cached := buildTree([]graph.FolderInfo{{ID: "X", DisplayName: "X"}})
	var sel wsSelection
	if err := json.Unmarshal([]byte(`{"folders":[{"id":"X","subtree":true,"exclude":[]}]}`), &sel); err != nil {
		t.Fatal(err)
	}
	// No root reaches the cycle, so none of its folders is in tree order: nothing is listed,
	// and nothing overflows the stack.
	if ids, vanished, bad := folderSet(&sel, cached, tr); bad || len(vanished) != 0 || len(ids) != 0 {
		t.Errorf("folderSet over a cycle: %v %v %v", ids, vanished, bad)
	}
}

// covers: MA-438, R12, S52
// EW-AC11 tombstone. R-i was tested only after a whole folder had been listed into memory, so
// a folder reporting millions of items (or a Graph that keeps returning new nextLinks) was
// read to the end, page after page, before the refusal. The listing now stops as soon as the
// folders read so far pass the limit, and a folder that is too large is refused after the page
// that proved it.
func TestPreviewStopsListingAnOverLimitFolder(t *testing.T) {
	e := newWsEnv(t)
	e.d.planMax = 12
	e.fg.addFolder("F-BIG", "Big", "", 60)
	e.fg.mutate(func() { e.fg.pageSize = 5 })
	ids := e.folders()
	id := e.ok("/api/workspace/preview", e.selection(entry(ids["Big"], false)))["previewId"].(string)
	var res map[string]any
	for i := 0; i < 400; i++ {
		_, res = e.post("/api/workspace/preview-status", map[string]any{"previewId": id})
		if res["state"] == "failed" || res["state"] == "done" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if res["state"] != "failed" {
		t.Fatalf("an over-limit folder was previewed: %v", res)
	}
	refusedBody(t, statusRefused, res, "R-i", "more than")
	if n := e.fg.count("/F-BIG/messages"); n > 4 {
		t.Errorf("the folder was listed in %d pages before R-i, want the 3 that pass the limit of 12", n)
	}
}

// covers: MA-413, R29, S52
// EW-AC12 tombstone. After starting a run's goroutine the handler waited on `started` and
// on the slot's `done` at once; for a short export on a busy machine both were ready, Go
// picked one at random, and half the time the page was told "the export did not start" for
// an export that had started and finished (and its settings were not remembered). A run
// that started is now answered as started whichever channel the select would have taken.
func TestStartOfAnAlreadyFinishedRunIsStillStarted(t *testing.T) {
	e := newWsEnv(t)
	e.fg.addFolder("F-ONE", "Single", "", 1)
	e.d.ex.afterLaunch = func(s *exportSlot) {
		select {
		case <-s.done: // the whole export finishes before the handler waits
		case <-time.After(10 * time.Second):
		}
	}
	ids := e.folders()
	for i := 0; i < 24; i++ {
		e.startExport(e.selection(entry(ids["Single"], false)))
		e.waitRun()
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.ExportDest != e.dest {
		t.Errorf("the destination was not remembered: %+v", s)
	}
}

// covers: MA-404, R17, R19, S41
// EW-AC13 tombstone. Two sign-in POSTs at once (a double click, two tabs) both read "no sign-in
// running" before either started one, and each started its own device flow: two codes, the
// page showing the second while the first could complete and write the token. The check and the
// start are now one step: the second POST finds the first one running.
func TestTwoSignInsAtOnceStartOneDeviceFlow(t *testing.T) {
	e := newWsEnv(t)
	gate := make(chan struct{})
	e.fg.mutate(func() { e.fg.tokenGate = gate })
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})
	e.d.si.testPause = func() { time.Sleep(250 * time.Millisecond) }
	var wg sync.WaitGroup
	begin := make(chan struct{})
	var already atomic.Int32
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-begin
			rec := e.postRaw("/api/m365/signin", nil)
			var out map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			if out["already"] == true {
				already.Add(1)
			}
		}()
	}
	close(begin)
	wg.Wait()
	time.Sleep(400 * time.Millisecond)
	if n := e.fg.count("/devicecode"); n != 1 {
		t.Errorf("two sign-in POSTs started %d device flows, want 1", n)
	}
	if already.Load() != 1 {
		t.Errorf("%d of the two POSTs were told a sign-in was already running, want 1", already.Load())
	}
	close(gate)
	e.waitSignIn("done")
	if e.d.flow.busy() != "" {
		t.Errorf("the device-flow mutex is still held by %q after the sign-in", e.d.flow.busy())
	}
}

// covers: MA-404, R17, R19, S41
// EW-AC13 tombstone, the second part (the fresh-eyes check found the first test pinned only the
// check-and-start step). A sign-in that signed in as another account while a weekly backup is
// installed waits for the user's Keep or Sign out, and a second POST in that wait used to find
// "nothing running", take the same device flow (its owner is the same) and start a second
// device flow beside the waiting one. A sign-in waiting for the answer is under way: the POST is
// answered "already", no second device flow starts, and the answer still finishes the first.
func TestSignInWaitingForAnswerIsNotRestarted(t *testing.T) {
	e := newWsEnv(t)
	e.cfg.BackupInstalled = func() bool { return true }
	e.d.cfg.BackupInstalled = e.cfg.BackupInstalled
	e.fg.mutate(func() { e.fg.upn = "bob@contoso.org" })
	e.ok("/api/m365/signin", nil)
	e.waitSignIn("confirm")
	flows := e.fg.count("/devicecode")
	if out := e.ok("/api/m365/signin", nil); out["already"] != true {
		t.Errorf("a second sign-in while the first waits for Keep or Sign out: %v", out)
	}
	if n := e.fg.count("/devicecode"); n != flows {
		t.Errorf("a sign-in waiting for its answer was restarted: device flows %d -> %d", flows, n)
	}
	if out := e.waitSignIn("confirm"); out["state"] != "confirm" {
		t.Errorf("the waiting sign-in was replaced: %v", out)
	}
	e.signInStatus("signout")
	e.waitSignIn("done")
}

// covers: MA-404, R17, R19, S41
// EW-AC13 tombstone, the third part. A finished sign-in frees the process-wide device flow in
// its own deferred step, a moment after it shows "done"; if the user starts the next sign-in in
// that moment, the older job's late release freed the flow under the newer one, and a capture
// could then start its own device flow beside it. Only the job that holds the slot frees the
// flow. The seam holds the older job's release until the newer sign-in is running.
func TestOlderSignInsLateReleaseDoesNotFreeTheNewersFlow(t *testing.T) {
	e := newWsEnv(t)
	hold := make(chan struct{})
	var first atomic.Bool
	e.d.si.testBeforeRelease = func(*signInJob) {
		if first.CompareAndSwap(false, true) {
			<-hold
		}
	}
	e.ok("/api/m365/signin", nil)
	e.waitSignIn("done") // the first job is done; its release is held

	gate := make(chan struct{})
	e.fg.mutate(func() { e.fg.tokenGate = gate })
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})
	if out := e.ok("/api/m365/signin", nil); out["already"] == true {
		t.Fatalf("the sign-in after a finished one was not started: %v", out)
	}
	for i := 0; i < 200 && e.fg.count("/devicecode") < 2; i++ {
		time.Sleep(25 * time.Millisecond)
	}
	if n := e.fg.count("/devicecode"); n != 2 {
		t.Fatalf("the second sign-in started %d device flows in all, want 2", n)
	}

	close(hold) // the older job now decides whether to free the flow
	time.Sleep(300 * time.Millisecond)
	if who := e.d.flow.busy(); who != "workspace" {
		t.Errorf("the device flow is held by %q while the newer sign-in runs, want the workspace", who)
	}
	if e.d.flow.hold("capture") { // what a capture that has to sign in asks for first
		e.d.flow.release("capture")
		t.Error("a capture could take the device flow beside the newer sign-in")
	}
	close(gate)
	e.waitSignIn("done")
	for i := 0; i < 80 && e.d.flow.busy() != ""; i++ {
		time.Sleep(25 * time.Millisecond)
	}
	if who := e.d.flow.busy(); who != "" {
		t.Errorf("the device flow is still held by %q after the newer sign-in", who)
	}
}

// covers: MA-430, R29, R12, S52
// EW-AI7 skip encoder (OWNER DECISION PENDING, see the report's escalations). Open folder checks
// the recorded folder's identity and then starts `xdg-open <root>` on Linux: a destination
// co-writer who swaps the folder for a FILE between the check and the exec makes xdg-open launch
// that file with its default handler, where XC33 says Open folder reveals and never launches.
// The one-character remedy is a trailing separator on the argument ("<root>/"), which a regular
// file answers with ENOTDIR so nothing is launched; it changes the argv that
// TestOpenFolderRevealsOnlyTheVerifiedFolder pins ({"xdg-open", root}) and the design's wording
// "xdg-open on the directory", so it is the owner's to rule. Remove the Skip when it is.
func TestOpenFolderLinuxArgvCannotLaunchAFile(t *testing.T) {
	t.Skip("OWNER DECISION PENDING (EW-AI7, XC33): the Linux reveal argument needs a trailing separator so a folder swapped for a file after the identity check is not launched; it changes an argv pinned by MA-430's test")
	name, args := revealArgv("linux", "/home/u/export/MailExport-x")
	if name != "xdg-open" || len(args) != 1 || !strings.HasSuffix(args[0], "/") {
		t.Errorf("the Linux reveal is %s %q: a path with no trailing separator opens whatever is at it", name, args)
	}
}

// covers: MA-445, MA-524, R12
// EW-AC14 tombstone. UpdateSettings treated a settings file it could not read as empty and saved
// over it, so a read that failed for a moment (a sharing violation, an antivirus scan) or a
// file this version cannot parse lost the archive location, the weekly schedule and every field
// it did not know, the next time an export started. A file that cannot be read is now left as
// it is and the update is refused; a file that cannot be parsed is set aside as
// "<name>.unreadable" before the new one is written.
func TestUpdateSettingsDoesNotEraseWhatItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"version":1,"out":"/archive","weeklyDay":"Sunday","keepRaw":true,"fromTheFuture":{"a":1}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	set := func(s *Settings) { s.ExportDest = "/d" }

	// The twin: a readable file keeps its own fields.
	if err := UpdateSettings(path, "export", set); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(path); s.Out != "/archive" || s.WeeklyDay != "Sunday" || !s.KeepRaw || s.ExportDest != "/d" {
		t.Fatalf("a readable file: %+v", s)
	}

	// A file that cannot be read: refused, and left exactly as it was.
	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		before, _ := os.ReadFile(path)
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatal(err)
		}
		err := UpdateSettings(path, "export", func(s *Settings) { s.ExportDest = "/other" })
		os.Chmod(path, 0o600)
		if err == nil {
			t.Error("a settings file that could not be read was saved over")
		}
		if after, _ := os.ReadFile(path); string(after) != string(before) {
			t.Errorf("the unreadable settings file was changed:\n%s", after)
		}
	}

	// A file that cannot be parsed: set aside, not erased.
	garbage := `{"out": "/keep-me", "weeklyDay": "Monday", "x": [`
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateSettings(path, "export", set); err != nil {
		t.Fatalf("a file that cannot be parsed: %v", err)
	}
	if s := LoadSettings(path); s.ExportDest != "/d" {
		t.Errorf("the new file: %+v", s)
	}
	if b, err := os.ReadFile(path + ".unreadable"); err != nil || string(b) != garbage {
		t.Errorf("the unparseable file was not kept as %s.unreadable: %q (%v)", filepath.Base(path), b, err)
	}
}

// covers: MA-445, MA-524, R12
// EW-AC14 tombstone, the remaining half (the fresh-eyes check, NF4). A settings file written by a
// newer build parses, so it was not set aside, and saving it dropped every field this build does
// not know and rewrote its version to 1: the newer build's choices were gone the next time an
// export started. The save now keeps the keys it has no field for, and a version above its own.
// A field this build does know and the caller cleared stays cleared.
func TestSettingsSaveKeepsWhatANewerBuildWrote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"version":2,"out":"/archive","weeklyDay":"Sunday","fromTheFuture":{"a":1},"alsoNew":"x"}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := func() map[string]any {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("the saved settings do not parse: %v\n%s", err, b)
		}
		return m
	}
	if err := UpdateSettings(path, "export", func(s *Settings) { s.ExportDest = "/d" }); err != nil {
		t.Fatal(err)
	}
	m := raw()
	if m["exportDest"] != "/d" || m["out"] != "/archive" || m["weeklyDay"] != "Sunday" {
		t.Errorf("the fields this build knows: %v", m)
	}
	if fut, ok := m["fromTheFuture"].(map[string]any); !ok || fut["a"] != float64(1) || m["alsoNew"] != "x" {
		t.Errorf("the fields a newer build wrote were dropped: %v", m)
	}
	if m["version"] != float64(2) {
		t.Errorf("a newer build's version %v was rewritten", m["version"])
	}

	// A known field the caller cleared is not brought back by the keeping of the unknown ones.
	if err := UpdateSettings(path, "export", func(s *Settings) { s.Out = "" }); err != nil {
		t.Fatal(err)
	}
	if m := raw(); m["out"] != nil || m["alsoNew"] != "x" {
		t.Errorf("after clearing out: %v", m)
	}
}

// covers: MA-445, MA-524, R12
// EW-AC14 tombstone, the weekly-backup half (NF4). The install's settings step loaded the file
// (an unreadable one as empty), changed the interval and saved over it, without the refusal and the
// set-aside UpdateSettings gives: an unreadable file lost the archive location it named, and
// the install went on to schedule a backup of whatever was left. It now goes through UpdateSettings:
// an unreadable file is left as it is and the step refuses before anything is handed to the OS
// scheduler; an unparseable one is kept as <name>.unreadable. The step is called directly: a test
// never installs a real scheduled task.
func TestScheduleChoiceDoesNotEraseWhatItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{"version":1,"out":"/archive","weeklyDay":"Sunday","keepRaw":true}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveScheduleChoice(path, "Daily", " 04:15 "); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(path); s.Interval != "daily" || s.WeeklyTime != "04:15" || s.Out != "/archive" || !s.KeepRaw {
		t.Fatalf("a readable file: %+v", s)
	}

	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		before, _ := os.ReadFile(path)
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatal(err)
		}
		err := saveScheduleChoice(path, "weekly", "05:00")
		os.Chmod(path, 0o600)
		if err == nil {
			t.Error("the schedule step saved over a settings file it could not read")
		}
		if after, _ := os.ReadFile(path); string(after) != string(before) {
			t.Errorf("the unreadable settings file was changed:\n%s", after)
		}
	}

	garbage := `{"out": "/keep-me", "x": [`
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveScheduleChoice(path, "weekly", "05:00"); err != nil {
		t.Fatalf("a file that cannot be parsed: %v", err)
	}
	if b, err := os.ReadFile(path + ".unreadable"); err != nil || string(b) != garbage {
		t.Errorf("the unparseable file was not kept as %s.unreadable: %q (%v)", filepath.Base(path), b, err)
	}
}
