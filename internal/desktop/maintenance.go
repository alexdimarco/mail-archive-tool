package desktop

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/server"
)

// runJob runs one long-running archive job (capture / verify / rebuild) in the
// shared captureState goroutine. They are mutually exclusive — the engine takes
// the archive's exclusive lock — recovered so a crafted message or a bug never
// kills the dashboard (DC3), with the run log streamed into the bounded ring and
// the result surfaced via /api/activity.
func (d *dashboard) runJob(fn func(*log.Logger) (string, error)) error {
	return d.runJobOn(d.cfg.effectiveOut(), fn)
}

// runJobOn is runJob for a job on the archive at out, which its failure is worded
// for: a new archive's first backup is not the current archive's (MAKE-02).
func (d *dashboard) runJobOn(out string, fn func(*log.Logger) (string, error)) error {
	return d.runJobOnClaiming(out, nil, fn)
}

// runJobOnClaiming is runJobOn with claimed, when set, called under the slot's lock as
// the job takes the slot: an encrypted Run now's Lock window becomes the slot's there,
// so no Lock falls between the two (design-presync-attended §3.2; gate-2 pass, AS8).
// A job on a folder being deleted is refused as the slot is taken (MA-979's words): the
// route's own check comes earlier, and a delete beginning between the two would otherwise
// be seen by neither, since the delete's busy check reads the slot (the residual fixes'
// adversarial pass, finding 4). The folder is asked of the disk before the slot's lock.
func (d *dashboard) runJobOnClaiming(out string, claimed func(), fn func(*log.Logger) (string, error)) error {
	c := d.cap
	encrypted := out != "" && app.ArchiveEncrypted(out)
	onCurrent := out != "" && samePath(out, d.cfg.effectiveOut())
	var fi os.FileInfo
	if out != "" {
		fi = statOrNil(out)
	}
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("a task is already running — wait for it to finish")
	}
	if d.running.ofStat(out, fi) != nil {
		c.mu.Unlock()
		return errors.New(deletingRefusal)
	}
	c.running, c.device, c.lines, c.result, c.errMsg = true, nil, nil, "", ""
	// A new job in the slot (design-presync-attended §4.6, C-15, C-71): its own id, its
	// archive, and the sync fields reset together with the lines and the result.
	c.slot++
	c.jobOut, c.encrypted, c.onCurrent, c.muted, c.slotOut = out, encrypted, onCurrent, false, out
	c.resetSyncLocked()
	if claimed != nil {
		claimed()
	}
	c.mu.Unlock()
	// The archive the job runs on, taken as it starts: a switch while it runs does not
	// change how its failure is worded (the P3-3 plan, 3d).
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.finish("", fmt.Sprintf("the task stopped unexpectedly: %v", r))
			}
		}()
		result, err := fn(log.New(c, "", 0))
		if err != nil {
			c.finish("", jobErrorText(out, err))
			return
		}
		c.finish(result, "")
	}()
	return nil
}

// rebuildButton is the label of the Maintenance card's rebuild control, which
// the owner-facing texts name instead of a command line.
const rebuildButton = "Maintenance → Rebuild search index"

// checkButton is the label of the Maintenance card's integrity check (the command-line tool calls it
// verify), which the owner-facing texts name, as the engine's own words do (friction F17).
const checkButton = "Maintenance → Check integrity"

// jobErrorText is a failed job's text for the owner: an index refusal says what
// is wrong and names the Rebuild button instead of `mailarchive reindex`
// (g7 F3); any other failure keeps its own text. A job on an encrypted archive
// says its failure kind's fixed words, never the error's text (P2a-C16).
func jobErrorText(out string, err error) string {
	var s spokenErr
	if errors.As(err, &s) {
		return string(s)
	}
	if app.ArchiveEncrypted(out) {
		return app.ErrorTextFor(out, err)
	}
	var ie *app.IndexOpenError
	if errors.As(err, &ie) {
		return ie.ForOwner(rebuildButton)
	}
	return err.Error()
}

// noArchiveText is the refusal of a maintenance action with no archive location: what, and the steps.
const noArchiveText = "No archive is chosen yet, so there is nothing to work on. Click Open an archive folder… at the top of the page, or type the archive's folder in Settings → Archive location and click Save settings, then try again."

func (d *dashboard) requireOut() (string, bool) {
	out := d.cfg.effectiveOut()
	return out, out != ""
}

// refuseMaintenance refuses Check integrity and Rebuild search index of the current archive out
// while a delete of it is under way, when its delete did not finish, or when deletions.log cannot
// be read, as Archive now and Run now are (MA-1009, del-s3b re-verification, leftover 4): a check
// would report the files a delete removed as damage, and a rebuild would write a new index into a
// folder the delete left part way. It reports whether it refused.
func (d *dashboard) refuseMaintenance(w http.ResponseWriter, out string) bool {
	msg := d.unfinishedRefusal(out)
	if msg == "" {
		return false
	}
	writeJSON(w, map[string]any{"ok": false, "error": msg})
	return true
}

// verifyAction re-hashes every archived file against its recorded checksum
// (read-only integrity check, R20). Loopback-only, CSRF-guarded; runs as a job.
func (d *dashboard) verifyAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, csrfRefused, http.StatusForbidden)
		return
	}
	out, ok := d.requireOut()
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": noArchiveText})
		return
	}
	if d.refuseMaintenance(w, out) {
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
		http.Error(w, csrfRefused, http.StatusForbidden)
		return
	}
	out, ok := d.requireOut()
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": noArchiveText})
		return
	}
	if d.refuseMaintenance(w, out) {
		return
	}
	// An encrypted archive's index is rebuilt with its key, from its sealed messages
	// (§3.1, E7): only while it is open here, on a write lease taken as it is clicked
	// (the P3-3 pass, FID-02). Without the key, app.Rebuild refuses it as before.
	if app.ArchiveEncrypted(out) && d.cfg.Keys != nil {
		e := d.cfg.Keys.get(out)
		if e == nil {
			e = d.cfg.Keys.remembered(out)
		}
		if e == nil {
			writeJSON(w, map[string]any{"ok": false, "error": "This encrypted archive is locked: unlock it on its Encryption card, then click Rebuild search index again."})
			return
		}
		lease, lerr := e.u.LeaseForWrite(keyring.OpenOptions{Store: d.cfg.Store})
		if lerr != nil {
			writeJSON(w, map[string]any{"ok": false, "error": lerr.Error()})
			return
		}
		if err := d.runJob(func(l *log.Logger) (string, error) {
			defer lease.Destroy()
			c, rerr := keyring.Reindex(d.life.Context(), lease, l)
			if rerr != nil {
				return "", rerr
			}
			msg := fmt.Sprintf("Search index rebuilt: %d message(s) indexed.", c.Added)
			if c.Unreadable+c.Failed > 0 {
				msg += fmt.Sprintf(" %d could not be read or indexed — `mailarchive verify -full -out %q` names which.", c.Unreadable+c.Failed, out)
			}
			return msg, nil
		}); err != nil {
			lease.Destroy()
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	err := d.runJob(func(l *log.Logger) (string, error) {
		// The workspace's list reads search.db for one request at a time. The rebuild waits out the
		// request in flight, and holds the next ones off until it is done, so the index is never
		// open inside its rename window (XC20).
		d.beginRebuild()
		defer d.endRebuild()
		rebuild := d.rebuild
		if rebuild == nil {
			rebuild = app.Rebuild
		}
		rr, rerr := rebuild(out, l)
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
	in.Surface = "desktop" // name the Rebuild button, not a command line
	// A delete of it that did not finish is said here too (the 1.5.3 friction review's B2) — not
	// one this Desktop is running now, whose started line has no finished line yet (the 1.5.3
	// friction fixes' adversarial pass, finding 10): its result shows on the page when it ends.
	// The folder's delete marker is asked too, with or without a log here (§15.4, DM-15); a lookup
	// of it that fails does not hide the log's line (UnfinishedToSay; LOW-3).
	if d.running == nil || d.running.of(out) == nil {
		// A log that could not be read, a name at the marker's that is not a regular file, and a
		// lookup the log does not answer for are said as what they are (the slice-2 pass's LOW-4).
		if u, err := archdelete.UnfinishedToSay(d.cfg.deletionsLogPath(), out, d.cfg.goos()); err == nil && u != nil {
			in.DeleteUnfinished = true
			in.DeleteMarkerOnly, in.DeleteBegun = u.MarkerOnly, archdelete.MarkerBegun(u)
			in.DeleteLogUnreadable, in.DeleteMarkerNotRegular = u.LogUnreadable, u.Marker == delmark.NotRegular
		} else {
			in.DeleteCouldNotCheck = archdelete.CouldNotCheck(err)
		}
	}
	rep := health.Assess(in, time.Now())
	summary := health.Summary(in, rep)
	if d.cfg.localOnly() {
		summary = append(summary, d.cfg.newMailLineFor(out))
	}
	// An encrypted archive whose Run now syncs Thunderbird: the setting, read from the
	// job file in the settings folder, and while it is unlocked here the last Run now's
	// sync, kept in memory only (design-presync-attended §4.7, C-27).
	if goos := d.cfg.goos(); (goos == "darwin" || goos == "windows") && app.ArchiveEncrypted(out) {
		if _, j, reason := runNowJobFor(out); reason == "" && j.ThunderbirdSync != nil {
			last := ""
			if d.cfg.Keys != nil {
				if e := d.cfg.Keys.get(out); e != nil {
					last = e.lastSyncPhrase()
				}
			}
			summary = append(summary, encSyncSettingLine(last))
		}
	}
	reply := map[string]any{"posture": rep.Posture, "summary": summary}
	if actions := recordActions(out); len(actions) > 0 { // only when there is any (the P3-3 plan, 3b)
		reply["actions"] = actions
	}
	writeJSON(w, reply)
}
