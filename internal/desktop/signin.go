package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// The Microsoft 365 sign-in slot (extraction design §4.2, §4.16, XC7, XC8): a sign-in only,
// as a job of its own, under one process-wide sign-in mutex that capture's device flow
// shares, so two device flows never run at once. It signs in against an in-memory token
// store first and persists only after the answer a weekly backup needs: when one is
// installed and the signer is not the account it was archiving, nothing is saved until the
// user answers Keep or Sign out. The device code is served only by the guarded
// signin-status POST, never by GET /api/activity.

// deviceFlow is the process-wide sign-in mutex: who holds the one device flow, if anyone.
type deviceFlow struct {
	mu    sync.Mutex
	owner string
}

func (f *deviceFlow) hold(who string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.owner != "" && f.owner != who {
		return false
	}
	f.owner = who
	return true
}

func (f *deviceFlow) release(who string) {
	f.mu.Lock()
	if f.owner == who {
		f.owner = ""
	}
	f.mu.Unlock()
}

func (f *deviceFlow) busy() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.owner
}

// memTokenStore is the in-memory token store a sign-in runs against first.
type memTokenStore struct {
	mu   sync.Mutex
	data []byte
}

func (m *memTokenStore) LoadToken() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), m.data...), nil
}

func (m *memTokenStore) StoreToken(b []byte) error {
	m.mu.Lock()
	m.data = append([]byte(nil), b...)
	m.mu.Unlock()
	return nil
}

func (m *memTokenStore) ClearToken() error {
	m.mu.Lock()
	m.data = nil
	m.mu.Unlock()
	return nil
}

// signInJob is the one sign-in in progress, or the last one.
type signInJob struct {
	mu      sync.Mutex
	state   string // running | confirm | done | failed
	device  *graph.DeviceAuth
	upn     string // the account that signed in
	prev    string // the account the weekly backup was archiving
	archive string
	signOut bool // the user chose Sign out
	ref     *refusal
	answer  chan string
}

func (j *signInJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]any{"state": j.state}
	if j.device != nil && j.state == "running" {
		out["device"] = map[string]any{"code": j.device.UserCode, "url": j.device.VerificationURI}
	}
	switch j.state {
	case "confirm":
		out["confirm"] = map[string]any{"upn": j.upn, "previous": j.prev, "archive": j.archive,
			"text": fmt.Sprintf("Signed in as %s — your weekly backup will now archive %s into %s.", j.upn, j.upn, j.archive)}
	case "done":
		out["upn"], out["signedOut"] = j.upn, j.signOut
	case "failed":
		if j.ref != nil {
			out["id"], out["error"] = j.ref.ID, j.ref.Text
		}
	}
	return out
}

// signInNotice is the sign-in surface's own copy: the account the dashboard runs as, and
// that a Microsoft sign-in code is shown only in a window opened from the shortcut
// (design-local-confidentiality CC3, CC23; MA-384).
func (d *dashboard) signInNotice() (account, notice string) {
	a := d.cfg.account()
	return a, fmt.Sprintf("MailArchive Desktop is running as %s on this computer. This sign-in will be saved for %s. MailArchive shows a Microsoft sign-in code only in a window it opened from its shortcut — do not trust a code on a page you reached by typing or bookmarking an address.", a, a)
}

// backupInstalled reports whether the weekly backup is installed for the configured archive.
func (d *dashboard) backupInstalled() bool {
	if d.cfg.BackupInstalled != nil {
		return d.cfg.BackupInstalled()
	}
	out := d.cfg.effectiveOut()
	return out != "" && schedule.Query(schedule.DefaultNameFor(out)) == schedule.Installed
}

// signInAction starts the sign-in job and returns at once; the page polls signin-status.
func (d *dashboard) signInAction(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !d.readBody(w, r, &in) {
		return
	}
	c := d.cfg.load()
	if c == nil || strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.ClientID) == "" {
		d.refuse(w, refusalA())
		return
	}
	d.si.mu.Lock()
	cur := d.si.job
	d.si.mu.Unlock()
	if cur != nil && cur.snapshot()["state"] == "running" {
		d.reply(w, map[string]any{"ok": true, "already": true})
		return
	}
	if !d.flow.hold("workspace") {
		d.refuse(w, refusedWith("R-m", "A Microsoft sign-in is already in progress (started from Archive now) — finish it, or wait for it to end, before signing in here."))
		return
	}
	job := &signInJob{state: "running", answer: make(chan string, 1)}
	d.si.mu.Lock()
	d.si.job = job
	d.si.mu.Unlock()
	go d.runSignIn(job)
	d.reply(w, map[string]any{"ok": true})
}

// signInState is the dashboard's sign-in slot.
type signInSlot struct {
	mu  sync.Mutex
	job *signInJob
}

func (d *dashboard) runSignIn(job *signInJob) {
	defer d.flow.release("workspace")
	defer func() {
		if p := recover(); p != nil {
			job.mu.Lock()
			job.state, job.ref = "failed", &refusal{ID: "internal", Text: "MailArchive Desktop stopped the sign-in unexpectedly. Nothing was saved; try again.", Status: statusRefused}
			job.mu.Unlock()
		}
	}()
	fail := func(ref refusal) {
		job.mu.Lock()
		job.state, job.ref = "failed", &ref
		job.mu.Unlock()
	}
	c := d.cfg.load()
	if c == nil {
		fail(refusalA())
		return
	}
	ctx, cancel := context.WithTimeout(d.life.Context(), 20*time.Minute)
	defer cancel()
	mem := &memTokenStore{}
	cfg := graph.Config{Tenant: c.Tenant, ClientID: c.ClientID, TokenStore: mem, BaseURL: d.cfg.BaseURL, TokenURL: d.cfg.TokenURL,
		DeviceAuthURL: d.cfg.DeviceAuthURL, RequestTimeout: d.cfg.RequestTimeout,
		Prompt: func(da graph.DeviceAuth) {
			job.mu.Lock()
			job.device = &da
			job.mu.Unlock()
		}}
	client, err := graph.NewDelegated(ctx, cfg)
	if err != nil {
		fail(d.signInRefusal(err, c.Auth))
		return
	}
	upn, err := client.Me(ctx)
	if err != nil {
		fail(d.signInRefusal(err, c.Auth))
		return
	}
	prev, _ := d.signedInNow()
	job.mu.Lock()
	job.upn, job.prev, job.archive = upn, prev, d.cfg.effectiveOut()
	job.mu.Unlock()
	// XC7: with a weekly backup installed, a sign-in as another account than the one it was
	// archiving is not persisted until the user says so.
	if d.backupInstalled() && prev != "" && !strings.EqualFold(prev, upn) {
		job.mu.Lock()
		job.state = "confirm"
		job.mu.Unlock()
		select {
		case a := <-job.answer:
			if a != "keep" {
				job.mu.Lock()
				job.state, job.signOut = "done", true
				job.mu.Unlock()
				return
			}
		case <-ctx.Done():
			fail(refusalTimeout())
			return
		}
	}
	data, lerr := mem.LoadToken()
	if lerr != nil {
		fail(refusalB())
		return
	}
	if perr := d.persistSignIn(data); perr != nil {
		fail(refusedWith("R-d2", "MailArchive couldn't save your Microsoft 365 sign-in on this computer — nothing was saved: "+util.StripControl(perr.Error())))
		return
	}
	d.signInChanged(upn)
	job.mu.Lock()
	job.state = "done"
	job.mu.Unlock()
}

// persistSignIn writes the signed-in token where the saved sign-in lives: the OS vault, or
// the 0600 file cache.
func (d *dashboard) persistSignIn(data []byte) error {
	cfg := d.cfg.load()
	if cfg == nil {
		return errors.New("not configured")
	}
	t := d.cfg.tokenCfg(cfg)
	if t.TokenStore != nil {
		return t.TokenStore.StoreToken(data)
	}
	return util.WriteFileAtomic0600(t.TokenCachePath, data)
}

// signInRefusal is the refusal of a sign-in that did not complete: an app-only
// registration that refuses the device sign-in is R-a2, which names the fix.
func (d *dashboard) signInRefusal(err error, configAuth string) refusal {
	var rerr *oauth2.RetrieveError
	if strings.EqualFold(configAuth, "app") {
		if errors.As(err, &rerr) || strings.Contains(err.Error(), "start device sign-in") {
			return refusalA2()
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "did not complete") {
		return refusedWith("R-timeout", "The Microsoft sign-in didn't finish — the code may have expired. Click Sign in to get a new code.")
	}
	return graphRefusal(err, "your sign-in", configAuth)
}

// signInStatusAction polls the sign-in job: the device code, then the result. With an
// answer ("keep" or "signout") it settles the weekly-backup question. It is a guarded
// POST, never GET /api/activity (XC8).
func (d *dashboard) signInStatusAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Answer string `json:"answer"`
		// Initial is a page that has just loaded asking what is going on (F28). It is told of a
		// sign-in that is still running or waiting for its answer, and nothing of one that
		// finished: the server keeps the finished job, and a page that reloaded on "done" would
		// be told "done" again by the page it loaded, and reload again.
		Initial bool `json:"initial"`
	}
	if !d.readBody(w, r, &in) {
		return
	}
	account, notice := d.signInNotice()
	out := map[string]any{"ok": true, "state": "idle", "account": account, "notice": notice}
	d.si.mu.Lock()
	job := d.si.job
	d.si.mu.Unlock()
	if job != nil && in.Initial && in.Answer == "" {
		if s := job.snapshot(); s["state"] == "done" || s["state"] == "failed" {
			job = nil
		}
	}
	if job != nil {
		if in.Answer == "keep" || in.Answer == "signout" {
			select {
			case job.answer <- in.Answer:
			default:
			}
			time.Sleep(20 * time.Millisecond) // let the job act on it before it is read back
		}
		for k, v := range job.snapshot() {
			out[k] = v
		}
	}
	d.reply(w, out)
}

// lapse remembers that the saved sign-in was refused by Microsoft 365 (R-c): the token file
// is still there, so "is a sign-in saved" says yes, but the mailbox is not ready until the
// owner signs in again. It lives for the process; a restart finds out again on first use.
type lapse struct {
	mu  sync.Mutex
	upn string
}

// noteRefused records the refusal when it is the sign-in's (R-c), for the account now saved.
func (d *dashboard) noteRefused(ref refusal) {
	if ref.ID != "R-c" {
		return
	}
	upn, _ := d.signedInNow()
	d.lapse.mu.Lock()
	d.lapse.upn = upn
	d.lapse.mu.Unlock()
}

// signInLapsed is the account whose saved sign-in was refused, "" when it was not.
func (d *dashboard) signInLapsed() string {
	d.lapse.mu.Lock()
	defer d.lapse.mu.Unlock()
	return d.lapse.upn
}

// signInChanged is what a sign-in cleared or changed does (XC7): the session, every
// outstanding selection and preview are dropped, and a running Microsoft 365 export stops
// with R-z, finalized and retryable from its run record, so nothing is read with the wrong
// account. now is the account signed in now, "" when the sign-in was cleared.
func (d *dashboard) signInChanged(now string) {
	d.lapse.mu.Lock()
	d.lapse.upn = ""
	d.lapse.mu.Unlock()
	d.ex.mu.Lock()
	for run, r := range d.ex.results { // what a card said about the sign-in is out of date now
		if signInResultIDs[r.id] {
			delete(d.ex.results, run)
		}
	}
	d.ex.mu.Unlock()
	d.ws.dropSession()
	d.ex.mu.Lock()
	s := d.ex.slot
	d.ex.mu.Unlock()
	if s != nil && s.live() && !strings.EqualFold(s.mailbox, now) {
		s.run.StopForSignInChange(now)
	}
}
