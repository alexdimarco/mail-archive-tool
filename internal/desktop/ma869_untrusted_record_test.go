package desktop

import (
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/state"
)

// covers: MA-869, R37
// The archive's record is untrusted (C-17): a Microsoft 365 archive whose record is rewritten to
// name local mail files installs nothing and is refused in plain words — where a capture is
// counted by section 4's refusal, and where Microsoft 365 is not set up because the owner-checked
// last-run record is not a local backup the Desktop can rebuild. Positive twin first: the archive
// as it was installs its capture.
func TestARewrittenRecordInstallsNothing(t *testing.T) {
	isolateHome(t)
	out := emptyArchive(t)
	setStores(t, out, "mailbox:a@contoso.org")
	rec := `{"version":1,"status":"ok","job":["-out",` + jsonQuote(out) + `,"-mode","incremental","-graph"]}`
	if err := os.WriteFile(filepath.Join(out, state.LastRunName), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
	countCapture(t, out)
	cfgPath, _ := graphconfig.DefaultConfigPath()
	g := newA3Rig(t, out, "darwin", func(c *Config) { c.ConfigPath = cfgPath })
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) || !strings.Contains(strings.Join(g.installs[0].Args, " "), "-capture") {
		t.Fatalf("the positive twin, the archive as it was: %s", body)
	}
	g.installs, g.fake.tasks = nil, map[string]bool{}

	planted := filepath.Join(t.TempDir(), "Mail")
	if err := os.MkdirAll(planted, 0o755); err != nil {
		t.Fatal(err)
	}
	setStores(t, out, state.PathSourceID(planted))
	u := g.snapshot()
	body := g.post(`{"interval":"daily","time":"04:00","presync":true}`)
	assure.Refused(t, refusedCode(body), body, assure.Names(captureCountedRefus), assure.NoSideEffect(u.holds))

	// Microsoft 365 no longer set up: the route is the local one, and the rebuild refuses.
	os.Remove(cfgPath)
	n := newA3Rig(t, out, "darwin")
	u = n.snapshot()
	body = n.post(`{"interval":"daily","time":"04:00"}`)
	assure.Refused(t, refusedCode(body), body, assure.Names("can’t tell how this archive was built", "wizard"), assure.NoSideEffect(u.holds))
}

// probeLog records every path the Desktop looks at on a mail source's storage.
type probeLog struct {
	mu    sync.Mutex
	paths []string
}

func (p *probeLog) add(s string) { p.mu.Lock(); p.paths = append(p.paths, s); p.mu.Unlock() }
func (p *probeLog) any(prefixes ...string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.paths {
		for _, pre := range prefixes {
			if strings.HasPrefix(s, pre) {
				return s
			}
		}
	}
	return ""
}

// watchSources replaces the two looks at a source's storage with recorders.
func watchSources(t *testing.T) *probeLog {
	t.Helper()
	log := &probeLog{}
	origL, origF := lstatSource, findProfileDir
	lstatSource = func(p string) (fs.FileInfo, error) { log.add(p); return origL(p) }
	findProfileDir = func(p string) (string, bool) { log.add(p); return origF(p) }
	t.Cleanup(func() { lstatSource, findProfileDir = origL, origF })
	return log
}

// covers: MA-869, R37
// A network path planted in the archive's record causes no file check (C-17): drawing the page,
// an install and a sync request never look at it — the backup is not rebuilt from it and it does
// not count as Thunderbird. Positive twin first: a local source in the record is looked at.
func TestAPlantedNetworkPathIsNeverLookedAt(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	log := watchSources(t)
	g := newA3Rig(t, out, "darwin")
	g.page()
	if log.any(archivedStore(t, out)) == "" {
		t.Fatal("the positive twin: the local source was never looked at")
	}

	for _, host := range []string{`\\evil-host\share\Mail`, "//evil-host/share/Mail"} {
		setStores(t, out, "path:"+host)
		rec := `{"version":1,"status":"ok","job":["-out",` + jsonQuote(out) + `,"-mode","incremental","-input",` + jsonQuote(host) + `]}`
		if err := os.WriteFile(filepath.Join(out, state.LastRunName), []byte(rec), 0o600); err != nil {
			t.Fatal(err)
		}
		g.page()
		u := g.snapshot()
		body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`)
		if refusedCode(body) != 2 || !u.holds() {
			t.Errorf("%s: an install from a planted network source was not refused with nothing written: %s", host, body)
		}
		if s := log.any(`\\evil-host`, "//evil-host"); s != "" {
			t.Errorf("the planted network path %s was looked at", s)
		}
	}
}

// covers: MA-869, R37
// Drawing the page never waits on source storage (C-17): with a source whose storage stalls, the
// page is drawn within the bound, without the local-backup form (it can't be checked now). Positive
// twin first: with the storage answering, the form shows.
func TestDrawingThePageNeverWaitsOnSourceStorage(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if page := g.page(); !strings.Contains(page, `id="schedInstall"`) {
		t.Fatal("the positive twin: the local-backup form is not shown")
	}

	origL, origW := lstatSource, sourceWait
	release, returned := make(chan struct{}), make(chan struct{}, 8)
	lstatSource = func(p string) (fs.FileInfo, error) {
		<-release
		defer func() { returned <- struct{}{} }()
		return origL(p)
	}
	sourceWait = 50 * time.Millisecond
	t.Cleanup(func() {
		close(release)
		select {
		case <-returned:
		case <-time.After(5 * time.Second):
		}
		lstatSource, sourceWait = origL, origW
	})
	drawn := make(chan string, 1)
	go func() {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "127.0.0.1:8097"
		rec := httptest.NewRecorder()
		g.h.ServeHTTP(rec, req)
		drawn <- rec.Body.String()
	}()
	select {
	case page := <-drawn:
		if strings.Contains(page, `id="schedInstall"`) {
			t.Error("the form was shown although its sources could not be checked")
		}
	case <-time.After(3 * time.Second):
		t.Error("drawing the page waited on stalled source storage")
	}
}
