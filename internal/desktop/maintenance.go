package desktop

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/server"
)

// runJob runs one long-running archive job (capture / verify / rebuild) in the
// shared captureState goroutine. They are mutually exclusive — the engine takes
// the archive's exclusive lock — recovered so a crafted message or a bug never
// kills the dashboard (DC3), with the run log streamed into the bounded ring and
// the result surfaced via /api/activity.
func (d *dashboard) runJob(fn func(*log.Logger) (string, error)) error {
	c := d.cap
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("a task is already running — wait for it to finish")
	}
	c.running, c.device, c.lines, c.result, c.errMsg = true, nil, nil, "", ""
	c.mu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.finish("", fmt.Sprintf("the task stopped unexpectedly: %v", r))
			}
		}()
		result, err := fn(log.New(c, "", 0))
		if err != nil {
			c.finish("", err.Error())
			return
		}
		c.finish(result, "")
	}()
	return nil
}

func (d *dashboard) requireOut() (string, bool) {
	out := d.cfg.effectiveOut()
	return out, out != ""
}

// verifyAction re-hashes every archived file against its recorded checksum
// (read-only integrity check, R20). Loopback-only, CSRF-guarded; runs as a job.
func (d *dashboard) verifyAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}
	out, ok := d.requireOut()
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "choose an archive location first"})
		return
	}
	err := d.runJob(func(l *log.Logger) (string, error) {
		rep, verr := app.Verify(out, app.VerifyOptions{}, l, nil)
		if verr != nil {
			return "", verr
		}
		return app.VerifyResultLine(rep), nil
	})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// rebuildAction reconstructs the search index + folder pages from the archive
// itself — recovery for a lost/corrupt index (renames into place on success, R8).
func (d *dashboard) rebuildAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}
	out, ok := d.requireOut()
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "choose an archive location first"})
		return
	}
	err := d.runJob(func(l *log.Logger) (string, error) {
		rr, rerr := app.Rebuild(out, l)
		if rerr != nil {
			return "", rerr
		}
		return fmt.Sprintf("Search index rebuilt: %d message(s) indexed, %d pruned.", rr.Rebuilt, rr.Pruned), nil
	})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// healthAction reports the archive's GREEN/WARN/RED posture — the SAME truth the
// `status` verb reports (X10) — for the Status & health view (GET; no side effect).
func (d *dashboard) healthAction(w http.ResponseWriter, r *http.Request) {
	out := d.cfg.effectiveOut()
	if out == "" {
		writeJSON(w, map[string]any{"posture": "", "summary": []string{"No archive location set yet."}})
		return
	}
	in := health.Gather(out, "")
	rep := health.Assess(in, time.Now())
	writeJSON(w, map[string]any{"posture": rep.Posture, "summary": health.Summary(in, rep)})
}
