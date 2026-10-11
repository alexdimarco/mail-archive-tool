package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/util"
)

// The export slot, the activity page's endpoints and the background reconciler
// (extraction design §4.13, §4.16): one export at a time in its own slot, independent of
// the capture slot; its progress served from memory only, never touching a path (XC4); the
// history built from the journal and the run records, with the folder probes kept off the
// request path.

const (
	wedgeAfterCancel = 30 * time.Second // a run still blocked this long after Cancel is wedged (XC10)
	exportLogMax     = 400              // the progress log's bound, as the capture ring's
)

// exportsState is the dashboard's export state: the engine environment over the local
// state directory, the reconciler, and the one run slot.
type exportsState struct {
	d   *dashboard
	env *app.CopyExportEnv
	rec *app.CopyReconciler

	mu         sync.Mutex
	slot       *exportSlot
	wedgeAfter time.Duration
	results    map[string]cardResult // the last refusal of a card's button, by run (F1)
}

// cardResult is what a card's last refused button said: kept on the server so the page's
// next history answer, a reload or a second window still shows it, instead of the rebuild
// of the cards wiping the one sentence that says what to do (friction F1).
type cardResult struct {
	id, text string
	at       time.Time
}

// cardResultTTL is how long a refusal stays on its card.
const cardResultTTL = 30 * time.Minute

// refuseRun answers a card button's refusal and keeps it on that run's card.
func (d *dashboard) refuseRun(w http.ResponseWriter, run string, ref refusal) {
	d.ex.mu.Lock()
	if d.ex.results == nil {
		d.ex.results = map[string]cardResult{}
	}
	d.ex.results[run] = cardResult{id: ref.ID, text: ref.Text, at: d.now()}
	d.ex.mu.Unlock()
	d.refuse(w, ref)
}

// clearResult drops a run's kept refusal: a new click replaces it.
func (d *dashboard) clearResult(run string) {
	d.ex.mu.Lock()
	delete(d.ex.results, run)
	d.ex.mu.Unlock()
}

// resultsNow is the live kept refusals.
func (d *dashboard) resultsNow() map[string]cardResult {
	d.ex.mu.Lock()
	defer d.ex.mu.Unlock()
	out := map[string]cardResult{}
	for run, r := range d.ex.results {
		if d.now().Sub(r.at) > cardResultTTL {
			delete(d.ex.results, run)
			continue
		}
		out[run] = r
	}
	return out
}

// exportSlot is the running export, or the last one until the next starts.
type exportSlot struct {
	runID    string
	run      *app.CopyRun
	started  time.Time
	selected int
	mailbox  string
	done     chan struct{}

	mu       sync.Mutex
	finished bool
	released bool // the slot was released from a wedged run
	res      *app.CopyResult
	err      error
	log      []string
	logged   int
	logAt    time.Time
	lastB    int64
	lastAt   time.Time
	rate     float64
}

func (s *exportSlot) live() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.finished && !s.released
}

func (s *exportSlot) addLog(line string) {
	s.log = append(s.log, line)
	if len(s.log) > exportLogMax {
		s.log = s.log[len(s.log)-exportLogMax:]
	}
}

func newExportsState(d *dashboard) *exportsState {
	host, _ := os.Hostname()
	if d.cfg.Host != "" {
		host = d.cfg.Host
	}
	ex := &exportsState{d: d, wedgeAfter: wedgeAfterCancel}
	ex.env = app.NewCopyExportEnv(d.cfg.StateDir, host, d.cfg.Version)
	ex.rec = ex.env.NewReconciler()
	return ex
}

// copyEnv is the engine environment (never nil: planning needs no state directory).
func (d *dashboard) copyEnv() *app.CopyExportEnv { return d.ex.env }

func (d *dashboard) exportLive() bool {
	_, live := d.exportState()
	return live
}

// exportInfo is what R-m names about the running export.
type exportInfo struct {
	started        time.Time
	done, selected int
}

func (d *dashboard) exportState() (exportInfo, bool) {
	d.ex.mu.Lock()
	s := d.ex.slot
	d.ex.mu.Unlock()
	if s == nil || !s.live() {
		return exportInfo{}, false
	}
	p := s.run.Progress()
	return exportInfo{started: s.started, done: p.Done, selected: p.Selected}, true
}

// ---- start, cancel, progress ------------------------------------------------

// startAction is "Start export": it takes only a preview id and copies exactly the plan
// that was previewed (P5, X14). It refuses an old or superseded preview (R-k), a second
// export (R-m), a sign-in that is no longer the one the preview was made under (R-l, R-b),
// and whatever the engine refuses at Start (the destination again by path and identity,
// the free space, the posture, the state directory).
func (d *dashboard) startAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PreviewID string `json:"previewId"`
	}
	if !d.readBody(w, r, &in) {
		return
	}
	ex, ws := d.ex, d.ws
	ws.mu.Lock()
	prev := ws.prev
	ws.mu.Unlock()
	if prev == nil || prev.id != in.PreviewID || d.now().Sub(prev.created) > previewTTL {
		d.refuse(w, refusalK(ws.inRetryMode()))
		return
	}
	if st, live := d.exportState(); live {
		d.refuse(w, refusalM(st.started, st.done, st.selected))
		return
	}
	if ref := d.signerRefusal(prev.upn); ref != nil {
		d.refuse(w, *ref)
		return
	}
	if d.cfg.StateDir == "" {
		d.refuse(w, refusedWith("R-d", "MailArchive Desktop has no folder of its own to record this export in — nothing was exported. Sign out and back in to Windows (or set up your profile folder), then start MailArchive again."))
		return
	}
	sess, ref := ws.session(r.Context())
	if ref != nil {
		d.refuse(w, *ref)
		return
	}
	if !strings.EqualFold(sess.upn, prev.upn) {
		d.refuse(w, refusalL(prev.upn, sess.upn))
		return
	}

	src := app.NewM365Source(sess.client, sess.upn, sess.tenant, func() (string, bool) { return d.signedInNow() })
	src.Idle, src.Overall = d.cfg.MIMEIdleTimeout, d.cfg.MIMETimeout
	run := ex.env.NewCopyRun(prev.plan, src)
	started := make(chan string, 1)
	run.OnStart = func(id string) { started <- id }
	slot := &exportSlot{run: run, started: d.now(), selected: len(prev.plan.Messages), mailbox: sess.upn, done: make(chan struct{})}

	ex.mu.Lock()
	if ex.slot != nil && ex.slot.live() { // another start won the race
		old := ex.slot
		ex.mu.Unlock()
		p := old.run.Progress()
		d.refuse(w, refusalM(old.started, p.Done, p.Selected))
		return
	}
	ex.slot = slot
	ex.mu.Unlock()
	ws.mu.Lock()
	ws.prev = nil // the preview is spent: starting it again is R-k
	ws.mu.Unlock()

	go ex.runSlot(slot, prev.plan)

	select {
	case id := <-started:
		slot.mu.Lock()
		slot.runID = id
		slot.addLog(fmt.Sprintf("%s Started: %s messages to %s", slot.started.Format("15:04:05"), commaInt(slot.selected), prev.plan.ExportName))
		slot.mu.Unlock()
		d.saveExportSettings(prev.plan)
		d.reply(w, map[string]any{"ok": true, "runId": id})
	case <-slot.done:
		ex.mu.Lock()
		if ex.slot == slot {
			ex.slot = nil
		}
		ex.mu.Unlock()
		slot.mu.Lock()
		err := slot.err
		slot.mu.Unlock()
		if err == nil {
			err = errors.New("the export did not start")
		}
		d.refuse(w, *copyRefusal(err))
	}
}

// saveExportSettings remembers the destination and options when an export starts
// (design §4.17): under the settings lock, changing only the four export fields (XC34).
func (d *dashboard) saveExportSettings(plan *app.CopyPlan) {
	if d.cfg.SettingsPath == "" {
		return
	}
	preserve := plan.Options.PreserveFolders
	html, att := plan.Options.ReadableHTML, plan.Options.Attachments
	_ = UpdateSettings(d.cfg.SettingsPath, "export", func(s *Settings) {
		s.ExportDest, s.ExportPreserve, s.ExportHTML, s.ExportAttachments = plan.Dest, &preserve, &html, &att
	})
}

// runSlot runs the export on the process-lifetime context in a recovered goroutine
// (DC3: a panic is the engine's "Stopped: an internal error", and the marker stays), and
// then reconciles a run that stopped before its seal so its card says why.
func (ex *exportsState) runSlot(slot *exportSlot, plan *app.CopyPlan) {
	d := ex.d
	defer close(slot.done)
	var res *app.CopyResult
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("the export stopped unexpectedly: %v", p)
			}
		}()
		res, err = slot.run.Run(d.life.Context())
	}()
	slot.mu.Lock()
	slot.finished, slot.res, slot.err = true, res, err
	if res != nil && res.Stop != nil && res.Stop.RID == "R-c" {
		d.noteRefused(refusalC()) // the saved sign-in is refused: the workspace says so, and Export activity shows Sign in
	}
	if res != nil {
		slot.addLog(fmt.Sprintf("%s %s", d.now().Format("15:04:05"), res.Label))
	}
	slot.mu.Unlock()
	if res != nil && res.RunID != "" {
		ex.rec.Forget(res.RunID)
		if !ex.env.Ended(res.RunID) {
			// stopped before its end: the page says what and offers Finish as partial. This
			// process knows why it stopped, so its card keeps the run's own "Stopped: ..." words
			// (a panic is "Stopped: an internal error"); after a restart the reconciled
			// "interrupted: <reason>" is the design's.
			if obs := ex.rec.LookAgain(res.RunID); obs.Kind == "interrupted" && strings.HasPrefix(res.Label, "Stopped:") {
				obs.Label = res.Label
				ex.rec.Record(obs)
			}
		}
		_ = ex.env.CompactJournal()
	}
}

// cancelAction cancels the running export: it stops after the message in flight (its
// download aborted, its temp removed). A run still blocked 30 s later is wedged: the slot
// is released, the run is abandoned (it makes no further writes), and the stop is recorded
// as a `stopped` event, never an `end` (XC10).
func (d *dashboard) cancelAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RunID string `json:"runId"`
	}
	if !d.readBody(w, r, &in) {
		return
	}
	ex := d.ex
	ex.mu.Lock()
	s := ex.slot
	ex.mu.Unlock()
	if s == nil || !s.live() || s.runIDNow() != in.RunID {
		d.reply(w, map[string]any{"ok": false, "error": "That export isn't running."})
		return
	}
	s.run.Cancel()
	s.mu.Lock()
	s.addLog(d.now().Format("15:04:05") + " Cancel asked: stopping after the message in flight")
	s.mu.Unlock()
	time.AfterFunc(ex.wedgeAfter, func() { ex.wedged(s) })
	d.reply(w, map[string]any{"ok": true})
}

func (s *exportSlot) runIDNow() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runID
}

// wedged releases the slot of a run that did not stop after its Cancel.
func (ex *exportsState) wedged(s *exportSlot) {
	s.mu.Lock()
	if s.finished || s.released {
		s.mu.Unlock()
		return
	}
	s.released = true
	id := s.runID
	s.addLog("Stopped: the destination is not responding")
	s.mu.Unlock()
	s.run.Abandon()
	if id != "" {
		_ = ex.env.WedgeStop(id)
		ex.rec.Forget(id)
	}
}

// progressAction is the running export, served from the in-memory run slot: it never
// touches a path and never waits on the reconciler (XC4). After a run ends it carries that
// run's result until the next one starts.
func (d *dashboard) progressAction(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !d.readBody(w, r, &in) {
		return
	}
	ex := d.ex
	ex.mu.Lock()
	s := ex.slot
	ex.mu.Unlock()
	if s == nil {
		d.reply(w, map[string]any{"ok": true, "running": false})
		return
	}
	p := s.run.Progress()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := d.now()
	if !s.lastAt.IsZero() && now.After(s.lastAt) {
		inst := float64(p.Bytes-s.lastB) / now.Sub(s.lastAt).Seconds()
		s.rate = 0.7*s.rate + 0.3*inst
	}
	s.lastB, s.lastAt = p.Bytes, now
	if p.Done != s.logged && now.Sub(s.logAt) >= 2*time.Second {
		s.logged, s.logAt = p.Done, now
		s.addLog(fmt.Sprintf("%s %s of %s done, %s failed — %s (%s)", now.Format("15:04:05"), commaInt(p.Done), commaInt(p.Selected), commaInt(p.Failed),
			util.StripControl(p.Folder), humanSize(p.Bytes)))
	}
	out := map[string]any{"ok": true, "running": !s.finished && !s.released, "runId": s.runID, "started": s.started.UTC().Format(time.RFC3339),
		"selected": p.Selected, "done": p.Done, "failed": p.Failed, "bytes": p.Bytes, "bytesText": humanSize(p.Bytes), "folder": util.StripControl(p.Folder),
		"rate": int64(s.rate), "etaSeconds": int(p.ETA.Seconds()), "log": append([]string(nil), s.log...), "waiting": p.Waiting}
	if s.released {
		out["released"] = true
		out["label"] = "Stopped: the destination is not responding"
	}
	if s.finished && s.res != nil {
		last := map[string]any{"label": s.res.Label, "status": s.res.Status, "outcome": int(s.res.Outcome), "fingerprint": s.res.Fingerprint,
			"exported": s.res.Counts.Exported, "failed": s.res.Counts.Failed, "notAttempted": s.res.Counts.NotAttempted, "rootName": s.res.RootName}
		if s.res.Stop != nil {
			last["stopId"], last["stop"] = s.res.Stop.RID, s.res.Stop.Message
		}
		out["last"] = last
	}
	d.reply(w, out)
}

// ---- history and the card actions -------------------------------------------

type cardOut struct {
	Run          string    `json:"run"`
	Started      string    `json:"started"`
	Source       string    `json:"source"`
	Mailbox      string    `json:"mailbox"`
	Dest         string    `json:"dest"`
	RootName     string    `json:"rootName"`
	Label        string    `json:"label"`
	Kind         string    `json:"kind"`
	Running      bool      `json:"running"`
	Planned      int       `json:"planned"`
	Exported     int       `json:"exported"`
	Failed       int       `json:"failed"`
	NotAttempted int       `json:"notAttempted"`
	Fingerprint  string    `json:"fingerprint"`
	Grouped      string    `json:"fingerprintGrouped"`
	NoFinger     string    `json:"fingerprintNote,omitempty"`
	Actions      []string  `json:"actions"`
	Stop         string    `json:"stop,omitempty"`
	StopID       string    `json:"stopId,omitempty"`
	Remaining    int       `json:"remaining,omitempty"`
	Part         int       `json:"part"`
	RetryOf      string    `json:"retryOf,omitempty"`
	Set          *setOut   `json:"set,omitempty"`
	Receipt      string    `json:"receipt,omitempty"`
	Result       *resOut   `json:"result,omitempty"`   // what the last refused button said, kept until the next click (F1)
	Next         string    `json:"next,omitempty"`     // what to do with a card that waits on the owner (F13)
	SetNote      string    `json:"setNote,omitempty"`  // how much of its retry set is exported (F10)
	Failures     []failOut `json:"failures,omitempty"` // "Which ones?": the messages not exported and why (F7)
	FailuresMore int       `json:"failuresMore,omitempty"`
}

type failOut struct {
	Subject string `json:"subject"`
	Folder  string `json:"folder"`
	Reason  string `json:"reason"`
}

type resOut struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// folderLostActions are the three answers to a folder that is not where it was: shown on a
// card after Open folder, Check this export or a retry was refused with R-r or R-r2, the
// refusals whose own words tell the owner to click them (F4).
var folderLostActions = []string{app.ActLookAgain, app.ActLocate, app.ActGone}

type setOut struct {
	Fingerprint string    `json:"fingerprint"`
	Grouped     string    `json:"grouped"`
	Parts       []partOut `json:"parts"`
}

type partOut struct {
	Run         string `json:"run"`
	RootName    string `json:"rootName"`
	Part        int    `json:"part"`
	Fingerprint string `json:"fingerprint"`
	Grouped     string `json:"grouped"`
	Gone        bool   `json:"gone"`
}

// historyAction is the cached journal view: each run's label from its sealed line or the
// reconciler's observation, its counts, its fingerprint and the actions its status
// allows. It never probes a folder.
func (d *dashboard) historyAction(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !d.readBody(w, r, &in) {
		return
	}
	live := ""
	d.ex.mu.Lock()
	if s := d.ex.slot; s != nil && s.live() {
		live = s.runIDNow()
	}
	d.ex.mu.Unlock()
	hist, err := d.ex.env.History(d.ex.rec.Observations(), live)
	if err != nil {
		d.refuse(w, refusedWith("R-d2", "MailArchive Desktop couldn't read its export history in "+d.cfg.StateDir+" — nothing was changed."))
		return
	}
	cards := make([]cardOut, 0, len(hist))
	results := d.resultsNow()
	for _, h := range hist {
		c := cardOut{Run: h.Run, Started: h.Started.UTC().Format(time.RFC3339), Source: h.SourceKind, Mailbox: h.Mailbox, Dest: h.Dest, RootName: h.RootName,
			Label: h.Label, Kind: h.Kind, Running: h.Running, Planned: h.Planned, Exported: h.Exported, Failed: h.Failed, NotAttempted: h.NotAttempted,
			Fingerprint: h.Fingerprint, Actions: h.Actions, Remaining: h.Remaining, Part: h.Part, RetryOf: h.RetryOf, Receipt: h.Receipt, Next: h.Next, SetNote: h.SetNote,
			FailuresMore: h.FailuresMore}
		for _, f := range h.Failures {
			c.Failures = append(c.Failures, failOut{Subject: f.Subject, Folder: f.Folder, Reason: f.Reason})
		}
		if c.Actions == nil {
			c.Actions = []string{}
		}
		if r, ok := results[h.Run]; ok && !h.Running {
			c.Result = &resOut{ID: r.id, Error: r.text}
			if (r.id == "R-r" || r.id == "R-r2") && h.Kind != "gone" {
				for _, a := range folderLostActions {
					if !hasString(c.Actions, a) {
						c.Actions = append(c.Actions, a)
					}
				}
			}
		}
		if h.Fingerprint != "" {
			c.Grouped = app.GroupFingerprint(h.Fingerprint)
		} else if h.Running {
			c.NoFinger = "" // still writing: it has no fingerprint yet, and the card says so by saying "writing"
		} else if h.Status == "" {
			c.NoFinger = "no fingerprint: this export was never finished"
		}
		if h.Stop != nil {
			c.Stop, c.StopID = h.Stop.Message, h.Stop.RID
		}
		if h.Set != nil {
			so := &setOut{Fingerprint: h.Set.Fingerprint, Grouped: app.GroupFingerprint(h.Set.Fingerprint)}
			for _, p := range h.Set.Parts {
				so.Parts = append(so.Parts, partOut{Run: p.Run, RootName: p.RootName, Part: p.Part, Fingerprint: p.Fingerprint, Grouped: app.GroupFingerprint(p.Fingerprint), Gone: p.Gone})
			}
			c.Set = so
		}
		cards = append(cards, c)
	}
	d.reply(w, map[string]any{"ok": true, "runs": cards, "signIn": d.signInBlock(cards)})
}

// signInResultIDs are the refusals that are about the sign-in: the page shows Sign in for them.
var signInResultIDs = map[string]bool{"R-b": true, "R-c": true, "R-l": true, "R-z": true}

type signInOut struct {
	Needed bool   `json:"needed"`
	As     string `json:"as,omitempty"`  // the account to sign in as
	Why    string `json:"why,omitempty"` // in words
}

// signInBlock says whether Export activity should show its Sign in button, and why (F2): the
// "Sign in again" of R-b, R-c, R-l and R-z names a control the owner has to be able to reach
// from the page the refusal appears on. It is needed when a retry was refused for the sign-in,
// when the saved sign-in was refused, or when nobody is signed in and something could be
// retried.
func (d *dashboard) signInBlock(cards []cardOut) signInOut {
	if c := d.cfg.load(); c == nil || strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.ClientID) == "" {
		return signInOut{}
	}
	upn, signed := d.signedInNow()
	lapsed := d.signInLapsed()
	retryable := func(c cardOut) bool {
		return hasString(c.Actions, app.ActExportRemaining) || hasString(c.Actions, app.ActExportFailed) || hasString(c.Actions, app.ActExportThese)
	}
	for _, c := range cards {
		if c.Result != nil && signInResultIDs[c.Result.ID] {
			return signInOut{Needed: true, As: c.Mailbox, Why: c.Result.Error}
		}
	}
	for _, c := range cards {
		if !retryable(c) || c.Mailbox == "" {
			continue
		}
		switch {
		case !signed:
			return signInOut{Needed: true, As: c.Mailbox, Why: "You're not signed in to Microsoft 365."}
		case lapsed != "":
			return signInOut{Needed: true, As: c.Mailbox, Why: "Microsoft 365 refused your saved sign-in as " + lapsed + " (it expired or access was revoked)."}
		case (c.StopID == "R-c" || c.StopID == "R-z") && !strings.EqualFold(upn, c.Mailbox):
			return signInOut{Needed: true, As: c.Mailbox, Why: c.Stop}
		}
	}
	if lapsed != "" {
		return signInOut{Needed: true, As: lapsed, Why: "Microsoft 365 refused your saved sign-in as " + lapsed + " (it expired or access was revoked)."}
	}
	return signInOut{}
}

func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// runBody is the body of the endpoints that act on one run.
type runBody struct {
	RunID  string `json:"runId"`
	Which  string `json:"which"`
	Action string `json:"action"`
	Path   string `json:"path"`
	Dest   string `json:"dest"`
}

// engineRefusal turns an error of the engine's per-run actions into the page's refusal.
func engineRefusal(err error) refusal {
	var ref *app.CopyRefusal
	if errors.As(err, &ref) {
		return refusal{ID: ref.ID, Text: ref.Message, Status: statusRefused}
	}
	msg := err.Error()
	if app.IsCheckFailed(err) { // Check and finish's own refusal
		return refusal{ID: "R-check", Text: msg, Status: statusRefused}
	}
	if app.IsNewerFormat(err) {
		return refusal{ID: "R-newer", Text: msg, Status: statusRefused}
	}
	if app.IsRecordDamaged(err) {
		return refusal{ID: "R-rec", Status: statusRefused, Text: "MailArchive can't read the record it keeps of this export (its file in MailArchive Desktop's own folder is damaged), so nothing was changed. The export folder itself is untouched: Open folder and Check this export still work, or you can keep or delete the folder yourself."}
	}
	// Anything else: no raw error and no file name for the owner to decode; the detail rides in the
	// answer's `detail` for whoever reports it (friction F20).
	return refusal{ID: "R-x2", Status: statusRefused, Text: "MailArchive Desktop couldn't do that, and nothing was changed. Reload the page and try again; if it happens again, restart MailArchive Desktop.",
		Extra: map[string]any{"detail": util.StripControl(msg)}}
}

// guardRun reads the run body and refuses an action on the run the slot is running now.
func (d *dashboard) runBodyOf(w http.ResponseWriter, r *http.Request) (runBody, bool) {
	var in runBody
	if !d.readBody(w, r, &in) {
		return in, false
	}
	if strings.TrimSpace(in.RunID) == "" {
		d.refuse(w, refusedWith("R-h", "Choose an export on Export activity first."))
		return in, false
	}
	d.clearResult(in.RunID) // a new click replaces what the last one said
	return in, true
}

func (d *dashboard) isLiveRun(run string) bool {
	d.ex.mu.Lock()
	defer d.ex.mu.Unlock()
	return d.ex.slot != nil && d.ex.slot.live() && d.ex.slot.runIDNow() == run
}

// openFolderAction reveals the recorded export folder. It takes no path from the request:
// the run id names the run, and the folder is the one recorded, after the identity check.
// It reveals and never launches anything (XC33).
func (d *dashboard) openFolderAction(w http.ResponseWriter, r *http.Request) {
	in, ok := d.runBodyOf(w, r)
	if !ok {
		return
	}
	t, err := d.ex.env.OpenTarget(in.RunID)
	if err != nil {
		d.refuseRun(w, in.RunID, engineRefusal(err))
		return
	}
	name, args := revealArgv(runtime.GOOS, t.Root)
	if rerr := d.reveal(name, args); rerr != nil {
		d.refuseRun(w, in.RunID, refusedWith("R-w", "MailArchive couldn't open that folder in your file manager — open it yourself: "+t.Root))
		return
	}
	d.reply(w, map[string]any{"ok": true})
}

// revealArgv is the command that shows the verified folder without launching anything in
// it: macOS `open -R` on its README, Windows `explorer.exe /select,` on its README, and
// `xdg-open` on the directory elsewhere.
func revealArgv(goos, root string) (string, []string) {
	readme := filepath.Join(root, "README.txt")
	switch goos {
	case "darwin":
		return "open", []string{"-R", readme}
	case "windows":
		return "explorer.exe", []string{"/select," + readme}
	}
	return "xdg-open", []string{root}
}

// reveal starts the command detached; a test replaces it with a recorder.
func defaultReveal(name string, args []string) error { return exec.Command(name, args...).Start() }

// retryAction builds a new preview from a run's RUN RECORD, never the destination's
// manifest (XC2): "failed" is its retryable failures, "rest" the remaining messages. It
// goes through the normal preview, then Start, into the next part of the set. The signer
// must match (R-l).
func (d *dashboard) retryAction(w http.ResponseWriter, r *http.Request) {
	in, ok := d.runBodyOf(w, r)
	if !ok {
		return
	}
	if st, live := d.exportState(); live {
		d.refuseRun(w, in.RunID, refusalM(st.started, st.done, st.selected))
		return
	}
	mailbox := d.ex.env.RunMailbox(in.RunID)
	now, signed := d.signedInNow()
	if !signed {
		d.refuseRun(w, in.RunID, retrySignInRefusal(refusalB(), mailbox, in.Which))
		return
	}
	if _, ref := d.ws.session(r.Context()); ref != nil {
		d.refuseRun(w, in.RunID, retrySignInRefusal(*ref, mailbox, in.Which))
		return
	}
	input, err := d.ex.env.RetryInput(in.RunID, in.Which, now)
	if err != nil {
		ref := engineRefusal(err)
		if ref.ID == "R-l" {
			ref = retryLRefusal(mailbox, now, in.Which)
		}
		d.refuseRun(w, in.RunID, ref)
		return
	}
	if dest := strings.TrimSpace(in.Dest); dest != "" {
		input.Dest = dest
	}
	input.Archives = d.archives()
	job := d.ws.startJob(func(ctx context.Context, job *wsPreviewJob) (*app.CopyPlan, map[string]any, *refusal) {
		job.set(func() { job.which = in.Which })
		plan, perr := d.copyEnv().PlanCopyExport(*input)
		if perr != nil {
			return nil, nil, copyRefusal(perr)
		}
		res := planResult(plan, job.id, foldersOut(input.Folders), len(input.Messages), len(input.Messages), nil, nil)
		res["retryWhich"] = in.Which // so the page can preview this retry again for another destination (F5)
		return plan, res, nil
	}, now, in.RunID)
	d.reply(w, map[string]any{"ok": true, "previewId": job.id})
}

func foldersOut(f []app.CopyPlanFolder) []foundOut {
	out := make([]foundOut, len(f))
	for i, x := range f {
		out[i] = foundOut{Folder: x.Folder, Reported: x.Messages, Found: x.Messages}
	}
	return out
}

// finishAction is "Finish as partial" for any unsealed run whose folder is found and
// confirmed, and "Check and finish" for a sealed one, from the run record alone (XC2, XC5).
func (d *dashboard) finishAction(w http.ResponseWriter, r *http.Request) {
	in, ok := d.runBodyOf(w, r)
	if !ok {
		return
	}
	if d.isLiveRun(in.RunID) {
		d.refuseRun(w, in.RunID, refusedWith("R-m", "That export is still running — cancel it on Export activity first."))
		return
	}
	res, err := d.ex.env.FinishRun(in.RunID)
	if err != nil {
		d.refuseRun(w, in.RunID, engineRefusal(err))
		return
	}
	d.ex.rec.Forget(in.RunID)
	_ = d.ex.env.CompactJournal()
	d.reply(w, map[string]any{"ok": true, "label": res.Label, "status": res.Status, "fingerprint": res.Fingerprint})
}

// locateAction is the card's "Look again", "This folder is gone" and "Locate…" (RR2-C3).
func (d *dashboard) locateAction(w http.ResponseWriter, r *http.Request) {
	in, ok := d.runBodyOf(w, r)
	if !ok {
		return
	}
	if d.isLiveRun(in.RunID) {
		d.refuseRun(w, in.RunID, refusedWith("R-m", "That export is still running — cancel it on Export activity first."))
		return
	}
	ex := d.ex
	switch in.Action {
	case "again":
		obs := ex.rec.LookAgain(in.RunID)
		d.reply(w, map[string]any{"ok": true, "kind": obs.Kind, "label": obs.Label})
	case "gone":
		obs, err := ex.env.MarkGone(in.RunID)
		if err != nil {
			d.refuseRun(w, in.RunID, engineRefusal(err))
			return
		}
		ex.rec.Record(obs)
		d.reply(w, map[string]any{"ok": true, "kind": obs.Kind, "label": obs.Label})
	case "locate":
		if err := ex.env.Locate(in.RunID, strings.TrimSpace(in.Path)); err != nil {
			d.refuseRun(w, in.RunID, engineRefusal(err))
			return
		}
		obs := ex.rec.LookAgain(in.RunID)
		d.reply(w, map[string]any{"ok": true, "kind": obs.Kind, "label": obs.Label})
	default:
		d.refuse(w, refusedWith("R-h", "That action isn't one MailArchive knows."))
	}
}

// checkAction is "Check this export" (XC14): re-hash every listed file against its
// SHA256SUMS.txt, compare the sums with the sealed fingerprint, list unlisted files. It
// reads only; GREEN or RED per X6.
func (d *dashboard) checkAction(w http.ResponseWriter, r *http.Request) {
	in, ok := d.runBodyOf(w, r)
	if !ok {
		return
	}
	chk, err := d.ex.env.CheckExport(in.RunID)
	if err != nil {
		d.refuseRun(w, in.RunID, engineRefusal(err))
		return
	}
	posture := "RED"
	if chk.OK {
		posture = "GREEN"
	}
	list := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	d.reply(w, map[string]any{"ok": true, "posture": posture, "green": chk.OK, "files": chk.Files, "changed": list(chk.Changed), "missing": list(chk.Missing),
		"unlisted": list(chk.Unlisted), "problems": list(chk.Problems), "fingerprint": app.GroupFingerprint(chk.Fingerprint), "fingerprintOK": chk.FingerprintOK,
		"noFingerprint": chk.NoFingerprint, "markerPresent": chk.MarkerPresent, "remedy": chk.Remedy})
}

// drain is the shutdown path (XC6): cancel the preview and the running export and wait
// for the export to finalize (its manifest says cancelled, its marker CANCELLED).
func (d *dashboard) drain(ctx context.Context) error {
	d.ws.mu.Lock()
	job := d.ws.job
	d.ws.mu.Unlock()
	if job != nil {
		job.cancel()
	}
	d.ex.mu.Lock()
	s := d.ex.slot
	d.ex.mu.Unlock()
	if s == nil {
		return nil
	}
	s.mu.Lock()
	finished, released := s.finished, s.released
	s.mu.Unlock()
	if finished || released {
		return nil
	}
	s.run.Cancel()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
