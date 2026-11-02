package desktop

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// Delete an archive and Remove from list, the Desktop's server side
// (docs/design-archive-delete.md §3, §5, §7, §8, slice 3; MA-971..MA-977). The four routes
// are guarded POSTs through d.api; the plan and Remove from list act only on a row of Your
// archives; a delete runs only through its plan's one-use token; and each delete's result
// and deletion record are held here until the person dismisses it or shuts MailArchive
// down. What a delete removes, in which order and with which words, is archdelete's: this
// file only says how its steps reach this computer's scheduler, settings and keys.

// deleteBodyMax caps the four routes' bodies, inside d.api's 1 MiB: a path, a token, a name.
const deleteBodyMax = 64 << 10

// The routes' own refusals (builder's words).
const (
	notARowText         = "This folder is not in Your archives, so MailArchive does not change it here. Reload the page and use its row."
	deleteNotWaiting    = "That delete is no longer waiting — click Delete… again; nothing was deleted."
	notHeldText         = "That result is no longer held: reload the page."
	deleteTooLargeText  = "That request is too large for MailArchive Desktop (64 KiB), so nothing was changed. Reload the page and try again."
	deleteNotUnderstood = "That request was not understood — reload the page and try again."
)

// The plan's fixed lines (§3.1, §2, §6, §9).
const (
	deleteLogLine = archdelete.PlanLogLine
	// deleteThreeLine and its encrypted sentence (the 1.5.3 friction review's wording item 49: "the
	// row" and "protecting" were jargon, and Forget was named on a plain archive, which has none).
	// Forget's sentence says what Forget does: this folder, then for the last folder it asks about
	// the whole archive and its key (the uninstall-docs group's adversarial pass, finding 3).
	deleteThreeLine          = "Remove from list takes this archive off the list and keeps the folder. Delete erases its files."
	deleteThreeLineEncrypted = " Forget this encrypted archive here makes this computer forget this folder; for the archive's last folder it asks whether to forget the whole archive and its remembered key."
	deleteSaveLine           = "Save this record now: MailArchive keeps no copy after you close this result."
)

// heldPlan is a delete planned from a row and awaiting its typed name: the plan, and the
// row's own spelling in Your archives, which step 9 removes.
type heldPlan struct {
	plan *archdelete.DeletePlan
	row  string
}

// deletePlanKey is the pending store's key of a delete planned for row: its own kind, so a
// new plan of the folder replaces the old one and nothing else.
func deletePlanKey(row string) string {
	_, k := keyOf(row)
	return "delete\x00" + k
}

// readDeleteBody decodes one of the four routes' JSON bodies, refusing one over the cap with
// 413 and one it cannot read with 400; ok is false when it has answered.
func readDeleteBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, deleteBodyMax)).Decode(v); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			writeJSONStatus(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": deleteTooLargeText})
			return false
		}
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": deleteNotUnderstood})
		return false
	}
	return true
}

func refuseDelete(w http.ResponseWriter, msg string) {
	writeJSON(w, map[string]any{"ok": false, "error": msg})
}

// refusalText is a plan's or a check's refusal in its own words, control characters gone.
func refusalText(err error) string {
	if r := archdelete.AsRefusal(err); r != nil {
		return r.Message
	}
	return util.StripControl(err.Error())
}

// matchRow is §3.1's row rule [C20]: the row of Your archives that path is — the row spelled
// exactly as path first, so of two rows naming one folder (a mapped drive and its network path,
// an 8.3 name, a junction above it: archindex keeps both) the one the person clicked is the one
// planned, locked and removed (del-s3a-A1-01); else by file identity when the folder can be
// read (a link to it, another case or an 8.3 name is that row); else nothing (an unplugged
// drive, a folder gone after an unfinished delete, is matched only by its own spelling). It
// returns the row's own spelling.
func (cfg Config) matchRow(path string) (string, bool) {
	row, ok, _ := cfg.findRow(path)
	return row, ok
}

// findRow is matchRow saying why it found no row when the list cannot be read (corrupt, a
// newer build's, held for a moment by an antivirus): the command line refuses then, since it
// cannot tell (del-s4-A2-02), where the page simply shows no rows.
func (cfg Config) findRow(path string) (string, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.TrimSpace(cfg.IndexPath) == "" {
		return "", false, nil
	}
	idx, err := archindex.ReadFile(cfg.IndexPath)
	if err != nil {
		return "", false, err
	}
	row, ok := rowIn(idx, path)
	return row, ok, nil
}

// rowIn is matchRow's rule over the entries of idx.
func rowIn(idx archindex.Index, path string) (string, bool) {
	for _, e := range idx.Entries {
		if e.Path == path {
			return e.Path, true
		}
	}
	if fi, err := os.Stat(path); err == nil {
		for _, e := range idx.Entries {
			if fe, err := os.Stat(e.Path); err == nil && os.SameFile(fi, fe) {
				return e.Path, true
			}
		}
	}
	return "", false
}

// knownArchives are the archive folders the Desktop knows (its list, and this computer's
// record of encrypted archives), read only: the uninstall's kept list and the delete's
// protected "another known archive".
func (cfg Config) knownArchives() []string {
	paths, _ := cfg.knownArchivesErr()
	return paths
}

// knownArchivesErr is knownArchives with the first error that left part of it unread (the
// list, or this computer's record of encrypted archives): the command line refuses a saved
// file it cannot check against every known archive (del-s4-A3-03). The paths it could read
// are returned all the same.
func (cfg Config) knownArchivesErr() ([]string, error) {
	var paths []string
	var first error
	seen := map[string]bool{}
	if strings.TrimSpace(cfg.IndexPath) != "" {
		if idx, err := archindex.ReadFile(cfg.IndexPath); err == nil {
			for _, e := range idx.Entries {
				if p := strings.TrimSpace(e.Path); p != "" {
					paths = append(paths, p)
					seen[encrecord.Spelling(p)] = true
				}
			}
		} else {
			first = err
		}
	}
	if rec, err := encrecord.Load(); err == nil {
		var recorded []string
		for _, e := range rec.Archives {
			for _, p := range e.Paths {
				if !seen[p.Spelling] {
					seen[p.Spelling] = true
					recorded = append(recorded, p.Spelling)
				}
			}
		}
		sort.Strings(recorded)
		paths = append(paths, recorded...)
	} else if first == nil {
		first = err
	}
	return paths, first
}

// deleteProtected are the folders §7's protected row names, the Desktop's own settings
// folders among them (a -config one is not job.Dir(), MA-973).
func (cfg Config) deleteProtected() archdelete.Protected {
	p := archdelete.DefaultProtected(cfg.knownArchives())
	p.Settings = cfg.settingsDirs()
	for _, f := range []string{cfg.SettingsPath, cfg.IndexPath} {
		if strings.TrimSpace(f) != "" {
			p.Settings = append(p.Settings, filepath.Dir(f))
		}
	}
	return p
}

// ownSettingsFolders are the folders the Desktop keeps its settings in, which the emptied-folder
// guard counts as MailArchive's own when they sit in an archive folder (CC16, MA-956).
func (cfg Config) ownSettingsFolders() []string {
	dirs := cfg.settingsDirs()
	if strings.TrimSpace(cfg.SettingsPath) != "" {
		dirs = append(dirs, filepath.Dir(cfg.SettingsPath))
	}
	return dirs
}

// deleteDeps are the facts and seams a delete's plan reads, for the Desktop as it is now:
// discovery with the scheduler asked (§5.1), this computer's records only for Remove from
// list and a re-run (§5.2), the settings §7 reads, the protected folders, deletions.log.
// busy reports a run on the folder; sealed an encrypted archive's sealed values (nil: "not
// known").
func (cfg Config) deleteDeps(busy func(string) bool, sealed func(folder, archiveID string) (archdelete.Captured, bool)) archdelete.Deps {
	logPath := cfg.deletionsLogPath()
	return archdelete.Deps{
		Discover: archdelete.HostDiscovery{Installed: cfg.DeleteInstalled},
		Records:  archdelete.FileDiscovery{Lenient: true},
		Busy:     busy,
		Record:   archdelete.RecordLookup,
		Sealed:   sealed,
		Settings: archdelete.Settings{
			Current:          cfg.effectiveOut(),
			CaptureBackupFor: cfg.captureBackupFor(),
			LaunchOut:        strings.TrimSpace(cfg.Out),
		},
		Protected: cfg.deleteProtected(),
		Log:       logPath,
		// The plan lists only the scheduled backups step 2 would stop (the 1.5.3 friction
		// review's B1), asking the scheduler step 2 asks.
		Scheduler: cfg.deleteScheduler(),
	}
}

// sealedOnPage is a delete's Sealed seam (§8.1 [O2-05], del-s3a conformance D2): the message
// count, the mail's dates and the first run of an encrypted archive whose key is open on this
// page, read from its sealed record (keyring.ReadSealedRecord: the open archive's key, for that
// read only); ok false — "not known: encrypted, key not open" — when it is not open here or its
// record cannot be read. The command line has no page (no Keys): never open. The open archive
// counts only when it is the folder planned on disk (openAt) and its archive ID is archiveID, the
// one the folder's key files claim: never another folder's open archive whose name folds to the
// same key (the delete residuals' security pass, key-use finding 2).
func (cfg Config) sealedOnPage(folder, archiveID string) (archdelete.Captured, bool) {
	e := cfg.Keys.openAt(folder)
	if e == nil || archiveID == "" || e.id != archiveID {
		return archdelete.Captured{}, false
	}
	m, history, err := keyring.ReadSealedRecord(e.u)
	if err != nil {
		return archdelete.Captured{}, false
	}
	return archdelete.CaptureSealed(m, history), true
}

// deletionsLogPath is deletions.log in this Desktop's settings folder, beside its config file:
// the folder its uninstall plans and removes (filepath.Dir(ConfigPath), dashboard.go's
// uninstallAction), so the plan names the log and uninstall removes it for a Desktop started
// with -config too (design-archive-delete §5.4 "in the config folder", §8.1; del-s5-A1-04).
// Without -config that folder is job.Dir(), which archdelete.DefaultLogPath is, and the command
// line's delete-archive (NewCommandLineDelete: the default config path) writes the same file.
// With no ConfigPath, archdelete.DefaultLogPath ("" when it cannot be had: the delete then
// refuses before its start, as Execute does with no log).
func (cfg Config) deletionsLogPath() string {
	if p := strings.TrimSpace(cfg.ConfigPath); p != "" {
		return filepath.Join(filepath.Dir(p), archdelete.LogName)
	}
	p, _ := archdelete.DefaultLogPath()
	return p
}

// deleteScheduler is the computer's scheduler as a delete reaches it.
func (cfg Config) deleteScheduler() archdelete.Scheduler {
	if cfg.DeleteScheduler != nil {
		return cfg.DeleteScheduler
	}
	return archdelete.HostScheduler{}
}

// sameArchiveFolder is "the same archive folder" for a run the Desktop records.
func sameArchiveFolder(a, b string) bool {
	return strings.TrimSpace(a) != "" && strings.TrimSpace(b) != "" && schedule.SameArchivePath(a, b, runtime.GOOS)
}

// busyOn is §7's busy row from the Desktop's own record of what runs [C12]: a job in the slot
// (a backup, Run now and its pre-sync, a capture, a verify, a rebuild) on the folder, an
// export reading it, or a workspace preview reading it (del-s3a-A2-04). It never counts the
// archive lock, which the delete itself holds when it asks again (archdelete.ExecDeps.Deps).
// An export or a preview started after a delete began is refused by that delete's record
// (deletesUnderWay), checked under the same locks these are read under.
func (d *dashboard) busyOn(folder string) bool {
	c := d.cap
	c.mu.Lock()
	running, slot := c.running, c.slotOut
	c.mu.Unlock()
	if running && (slot == "" || sameArchiveFolder(slot, folder)) {
		return true
	}
	d.ws.mu.Lock()
	job := d.ws.job
	d.ws.mu.Unlock()
	if job != nil && job.arch && job.running() && sameArchiveFolder(job.path, folder) {
		return true
	}
	d.ex.mu.Lock()
	s := d.ex.slot
	d.ex.mu.Unlock()
	return s != nil && s.live() && sameArchiveFolder(s.archiveDir, folder)
}

// planBusy is busyOn at the plan, with a run of another program too: a scheduled backup
// holding the archive lock (lockfile.Held takes it for an instant, writing nothing).
func (d *dashboard) planBusy(folder string) bool {
	return d.busyOn(folder) || lockfile.Held(filepath.Join(folder, lockfile.Name))
}

// deleteFacts are what a row says about Delete (MA-975).
type deleteFacts struct {
	ok, unfinished, deleting bool
	why                      string
}

// deleteRowFacts is a row's Delete state: offered, or why not, in §7's words; whether a delete
// of the folder did not finish (§5.4), whose row offers Delete again; and whether one is under
// way in this process (MA-979), whose row offers nothing and says so — its started line has no
// finished line yet, which is not "not finished". rc is the page's RowsCheck: deletions.log read
// once, each path asked once (del-s3a-A3-04).
func (cfg Config) deleteRowFacts(path string, rc *archdelete.RowsCheck, knownArchive bool) deleteFacts {
	if cfg.rowDeleting != nil && cfg.rowDeleting(path) {
		return deleteFacts{deleting: true, why: deletingRowWhy}
	}
	var f deleteFacts
	u, err := rc.State(path, knownArchive)
	f.unfinished = u != nil
	if err != nil {
		f.why = refusalText(err)
		return f
	}
	f.ok = true
	return f
}

// ---- the plan -----------------------------------------------------------------------

// deletePlanAction is §3.1 step 1: the row's plan, read-only, kept under a one-use token, and
// the saved plan's text (§8.1: no route of its own, and saving it uses no token).
func (d *dashboard) deletePlanAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if !readDeleteBody(w, r, &in) {
		return
	}
	row, ok := d.cfg.matchRow(in.Path)
	if !ok {
		refuseDelete(w, notARowText)
		return
	}
	// A delete of it under way holds its archive lock, which the busy row would word as a
	// backup (del-s3a-A3-01): said as what it is.
	if d.running.of(row) != nil {
		refuseDelete(w, deletingRefusal)
		return
	}
	deps := d.cfg.deleteDeps(d.planBusy, d.cfg.sealedOnPage)
	deps.KeyPlan = desktopKeyring{store: d.cfg.Store}.PlanForget // the plan's key line (§6, §14)
	// The scratch listing (§3.1 [C7]): a reader another program holds refuses here, before the
	// started line and before this page's open archive is locked; this page's own reader is
	// excused while the archive is open here, since step 1 closes it (del-s3a conformance D1).
	deps.ScratchDir = app.ArchiveScratchFor
	// Open here by the folder on disk, under any spelling, as step 1 (b) closes it (key-use finding 4).
	deps.OpenHere = func(folder string) bool { return d.cfg.Keys.openAt(folder) != nil }
	plan, err := archdelete.Plan(row, deps)
	if err != nil {
		refuseDelete(w, refusalText(err))
		return
	}
	text := archdelete.RecordText(archdelete.RecordInput{Status: archdelete.StatusPlanned, When: d.now(),
		Computer: d.computer(), Account: d.cfg.account(), Version: d.cfg.Version, Plan: plan})
	token, ok := d.keep(w, &pendingItem{key: deletePlanKey(row), out: row, deletePlan: &heldPlan{plan: plan, row: row}})
	if !ok {
		return
	}
	out := planView(plan)
	out["ok"], out["token"], out["row"] = true, token, row
	out["logLine"] = archdelete.PlanLogLineAt(d.cfg.deletionsLogPath()) // the log's full path (item 9)
	out["planText"], out["planName"] = string(text), fileNameFor("MailArchive deletion plan", plan.Name, d.now().Format("2006-01-02"))
	writeJSON(w, out)
}

// computer names this computer in a saved plan and a deletion record.
func (d *dashboard) computer() string {
	if d.cfg.Host != "" {
		return d.cfg.Host
	}
	h, _ := os.Hostname()
	return h
}

// planView is what the panel beside the row shows (§3.1).
func planView(p *archdelete.DeletePlan) map[string]any {
	var notes []string
	for _, n := range p.Schedules.Unconfirmed {
		notes = append(notes, n.Text)
	}
	for _, n := range p.Schedules.OtherHost {
		notes = append(notes, n.Text)
	}
	var unrecorded []string
	for i, it := range p.Unrecorded {
		if i == 10 {
			break
		}
		unrecorded = append(unrecorded, it.Rel)
	}
	stores := make([]map[string]any, 0, len(p.Stores))
	for i, s := range p.Stores {
		// label: the store's folder with the mail folders inside it, as the terminal shows it (the
		// 1.5.3 re-walk's N16: "Mail folders: mbox" named the source's folder, not its Inbox).
		stores = append(stores, map[string]any{"number": i + 1, "name": s, "label": p.StoreLabel(i)})
	}
	// The kept files are named, the first ten (the 1.5.3 friction review's B6, MA-1019): "Kept:
	// not the archive's: 1" did not say which.
	var kept []string
	for i, k := range p.Kept {
		if i == 10 {
			break
		}
		kept = append(kept, util.StripControl(k))
	}
	m := map[string]any{
		"folder": p.Folder, "name": p.Name, "kind": p.Kind.String(),
		"files": p.Files, "bytes": p.Bytes, "size": sizeText(p.Bytes),
		// Only what step 2 would stop, by when it runs, or "none"; a Run now backup said once (B1,
		// B15). scheduleLines are the panel's words.
		"schedules": p.ScheduleDescriptions(), "scheduleLines": nonNil(p.ScheduleLines()),
		"checkYourself": nonNil(p.Schedules.CheckYourself), "notes": nonNil(notes),
		"paused": p.Paused, "synced": p.Synced,
		"kept": len(p.Kept), "keptNames": nonNil(kept), "unrecorded": len(p.Unrecorded), "unrecordedNames": nonNil(unrecorded), "stores": stores,
		"rerun": p.Rerun != archdelete.RerunNone,
		// The plan's first line when it finishes a delete that stopped part way (D1).
		"finishesLine": p.FinishesLine(),
		// Files of it another program still holds, at which this run would stop again (N17).
		"heldLine": p.HeldLine(),
		// Capitals count (B8): archdelete.NameMatches folds no case, though Windows names do.
		"typeLine":     fmt.Sprintf("Type the folder's name, %s, to delete it (capital letters count)", p.Name),
		"doesNotReach": doesNotReachLine(runtime.GOOS),
		"logLine":      deleteLogLine,
		"threeLine":    deleteThreeLine,
	}
	if p.Paused {
		m["pausedLine"] = "This archive is paused."
	}
	if p.Synced != "" {
		m["syncedLine"] = archdelete.SyncedLine(p.Synced)
	}
	switch n := len(p.Unrecorded); {
	case n == 1:
		m["unrecordedBox"] = "Also delete this 1 file (for retention)" // B7: never "these 1 files"
	case n > 1:
		m["unrecordedBox"] = fmt.Sprintf("Also delete these %d files (for retention)", n)
	}
	if p.Kind == archdelete.Encrypted {
		m["keyLine"] = p.KeyLine()
		m["threeLine"] = deleteThreeLine + deleteThreeLineEncrypted
	}
	return m
}

// doesNotReachOn is design-archive-delete §2's "What Delete does not reach" with only this
// computer's own items (archdelete.DoesNotReachOn, B4, MA-1019).
func doesNotReachOn(goos string) string { return archdelete.DoesNotReachOn(goos) }

// doesNotReachLine is the line the panel and the result show, with its final period (B4).
func doesNotReachLine(goos string) string { return archdelete.DoesNotReachLineOn(goos) }

// localResultLines are a delete's result lines as this page shows them (archdelete.LocalLine).
// The record keeps archdelete's own lines.
func localResultLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = archdelete.LocalLine(l, runtime.GOOS)
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// sizeText is a byte count in words.
func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// fileNameFor is a saved file's name, "<what> - NAME - DATE.txt", the folder's name made safe
// for a file name on every platform.
func fileNameFor(what, name, date string) string {
	safe := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf("%s - %s - %s.txt", what, strings.TrimSpace(safe), date)
}

// ---- the delete ---------------------------------------------------------------------

// deleteAction is §3.1 step 2: the plan the token holds, its name typed, deleted by §5.3's
// steps through this computer's seams; the result and the record held until dismissed.
func (d *dashboard) deleteAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token      string `json:"token"`
		Typed      string `json:"typed"`
		Unrecorded bool   `json:"unrecorded"`
		DrawnFor   string `json:"drawnFor"`
	}
	if !readDeleteBody(w, r, &in) {
		return
	}
	it := d.pending.get(in.Token, isDeletePlan)
	if it == nil {
		refusePlanUsed(w, deleteNotWaiting)
		return
	}
	// A name that does not match changes nothing and keeps the plan: the person types again.
	if name := it.deletePlan.plan.Name; !archdelete.NameMatches(in.Typed, name) {
		refuseDelete(w, fmt.Sprintf("The name you typed does not match this folder's name, %s, so nothing was deleted. Type it exactly as shown.", name))
		return
	}
	// Recorded as under way before its first step (MA-979), so an export, a preview, a second
	// Delete or Remove from list of the folder, and Shut down, are refused until it ends; a
	// second delete of the same folder is refused with its token kept.
	end, ok := d.running.tryBegin(it.deletePlan.plan.Folder, it.deletePlan.row)
	if !ok {
		if why := d.running.blockedWhy(); why != "" {
			refuseDelete(w, why) // an uninstall (del-s5-A2-02); the plan's token is kept
			return
		}
		if why := d.running.heldWhy(it.deletePlan.plan.Folder, it.deletePlan.row); why != "" {
			refuseDelete(w, why) // the token is kept: Delete permanently once it ends
			return
		}
		refuseDelete(w, deletingRefusal)
		return
	}
	defer end()
	if d.life.Context().Err() != nil {
		refuseDelete(w, shuttingDownText)
		return
	}
	if it = d.pending.take(in.Token, isDeletePlan); it == nil {
		refusePlanUsed(w, deleteNotWaiting)
		return
	}
	plan, row := it.deletePlan.plan, it.deletePlan.row
	// Read before the delete, while the folder is there to be compared by identity: the rows of
	// Your archives that are this folder, and whether it is the archive the page was drawn for.
	rows := d.cfg.rowsOfFolder(row, plan.Folder)
	pageArchive := strings.TrimSpace(in.DrawnFor) != "" &&
		(sameArchiveFolder(in.DrawnFor, plan.Folder) || sameArchiveFolder(in.DrawnFor, row))
	res, cleared := d.runDelete(plan, row, in.Typed, in.Unrecorded)
	// Step 4 clears the current archive when it is the one deleted: that is a switch, under the
	// switch's rules, as Remove from list's is (del-s3a re-verification, R3). Only when this
	// delete's own write cleared it: a switch made in another tab while a delete of another
	// archive ran already applied them, and a plan made after it is not this delete's to end.
	if cleared {
		d.afterSwitch()
	}
	v := heldView{Row: row, Folder: plan.Folder, Name: plan.Name, Status: res.Status, Lines: localResultLines(res.Lines),
		RowRemoved: res.RowRemoved, Unfinished: res.Unfinished}
	if res.Record != nil {
		v.Record, v.RecordBase64 = string(res.Record), base64.StdEncoding.EncodeToString(res.Record)
		v.RecordName = fileNameFor("MailArchive deletion record", plan.Name, d.now().Format("2006-01-02"))
		v.Lines = append(v.Lines, deleteSaveLine)
		v.ID = d.deletes.add(v)
	}
	out := map[string]any{"ok": res.Refusal == nil, "id": v.ID, "row": row, "folder": v.Folder, "name": v.Name, "status": v.Status,
		"lines": v.Lines, "rowRemoved": v.RowRemoved, "unfinished": v.Unfinished,
		"record": v.Record, "recordBase64": v.RecordBase64, "recordName": v.RecordName,
		"rows": rows, "page": pageAfterDelete(pageArchive, cleared, res)}
	if res.Refusal != nil {
		out["error"] = res.Message()
	}
	// Refused before its started line, the plan's one-use token is spent: Delete permanently can
	// only be told the delete is no longer waiting (del-s3b conformance gap 3).
	if res.Record == nil {
		out["planUsed"] = true
		msg, _ := out["error"].(string)
		out["error"] = planUsedText(msg)
	}
	writeJSON(w, out)
}

// runUnfinishedText refuses Archive now and Run now of a current archive whose delete did not
// finish (§5.4's words first; the rest the builder's).
const runUnfinishedText = "Delete not finished: click Delete again. This archive's delete did not finish, so it is not backed up; nothing was changed."

// pageRunRefusal is why Archive now or Run now, posted from the page, does not start (MA-987), ""
// when it may: a page drawn for another archive than the current one (C-12's binding; while a
// delete's result is held the page is not drawn again after a switch, so its cards may name the
// archive it was drawn for, del-s3b-A1-03, A3-03); a current archive being deleted; and one whose
// delete did not finish (§5.4, §3.1: neither is offered for it, and the emptied-folder guard lets
// a run into a folder a delete left part way, since its manifest goes last — del-s3b conformance
// gap 2). A post with no body (a client older than this page) is not refused as stale; one it
// cannot read (over the delete routes' 64 KiB, or not JSON) is, since which archive it was drawn
// for cannot be told; and a deletions.log that cannot be read refuses, since whether the
// archive's delete finished cannot be told (del-s3b re-verification, leftover 2).
func (d *dashboard) pageRunRefusal(w http.ResponseWriter, r *http.Request) string {
	var in struct {
		DrawnFor *string `json:"drawnFor"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, deleteBodyMax)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		return staleRefusal
	}
	if d.stalePage(in.DrawnFor) {
		return staleRefusal
	}
	out := d.cfg.effectiveOut()
	if out == "" {
		return ""
	}
	return d.unfinishedRefusal(out)
}

// unfinishedRefusal is why a run that writes into out does not start because of a delete of it
// (MA-987, MA-1009), "" when none stops it: a delete of it under way, one that did not finish, or
// a deletions.log that cannot be read.
func (d *dashboard) unfinishedRefusal(out string) string {
	if d.running.of(out) != nil {
		return deletingRefusal
	}
	if logPath := d.cfg.deletionsLogPath(); logPath != "" {
		u, err := archdelete.UnfinishedDelete(logPath, out, runtime.GOOS)
		if err != nil {
			return fmt.Sprintf(runLogUnreadableText, logPath, util.StripControl(err.Error()))
		}
		if u != nil {
			return runUnfinishedText
		}
	}
	return ""
}

// runLogUnreadableText refuses a run when deletions.log cannot be read (builder's words).
const runLogUnreadableText = "MailArchive could not read its deletion log, %s (%s), so it cannot tell whether a delete of this archive did not finish; nothing was started. Check that file, then try again."

// planAgainText ends a refusal that spent the plan's token (builder's words).
const planAgainText = "Click Delete… again to try again."

// planUsedText is a refusal that spent the plan's token, saying how to go on.
func planUsedText(msg string) string {
	if msg == "" {
		return planAgainText
	}
	if strings.Contains(msg, "Delete… again") {
		return msg
	}
	return msg + " " + planAgainText
}

// refusePlanUsed is a refusal after which the plan's token no longer waits (planUsed: the page
// leaves Delete permanently disabled, del-s3b conformance gap 3).
func refusePlanUsed(w http.ResponseWriter, msg string) {
	writeJSON(w, map[string]any{"ok": false, "error": planUsedText(msg), "planUsed": true})
}

// The page's own archive after a delete of it (§3.1 "Where the result sits", MA-986, MA-1017):
// "unfinished" — a delete that did not finish and left it the archive in use (a stop at step 2;
// §5.4: Archive now and Run now give way to "Delete not finished: click Delete again"); "partial"
// — not fully deleted once its step 4 cleared it as the archive in use (cleared: a stop at step
// 6 or later is unfinished too, and the page's cards must go, the 1.5.3 friction fixes'
// adversarial pass, H1), or not fully deleted with only files the person ticked left; "deleted";
// or "" — not the page's archive, or a delete refused after its start, which removed nothing
// (del-s3b-A2-02).
func pageAfterDelete(pageArchive, cleared bool, res *archdelete.Result) string {
	switch {
	case !pageArchive:
		return ""
	case res.Unfinished && cleared:
		return "partial"
	case res.Unfinished:
		return "unfinished"
	case res.Status == archdelete.StatusComplete:
		return "deleted"
	case res.Status == archdelete.StatusPartial:
		return "partial"
	}
	return ""
}

// rowsOfFolder are the rows of Your archives that are the folder a delete planned from row is
// about (del-s3b-A1-02): row itself, and every other spelling of the same folder — by file
// identity while it can be read, else as this computer's file system compares names — which is
// what step 9 (archdelete.ForgetRowAt) removes and what deletions.log marks not finished, so the
// page marks every one of them, not only the spelling clicked.
func (cfg Config) rowsOfFolder(row, folder string) []string {
	rows := []string{row}
	if strings.TrimSpace(cfg.IndexPath) == "" {
		return rows
	}
	idx, err := archindex.ReadFile(cfg.IndexPath)
	if err != nil {
		return rows
	}
	for _, e := range idx.Entries {
		if e.Path != row && (sameArchiveFolder(e.Path, row) || sameArchiveFolder(e.Path, folder)) {
			rows = append(rows, e.Path)
		}
	}
	return rows
}

// runDelete is Execute with the page's own reader of the archive closed and held off it for the
// delete, released however Execute ends — a panic before Execute's own cleanup is set up
// included, or the reader would refuse that archive until a restart (del-s3a gap 2, MA-978).
// cleared reports whether its step 4 cleared the current archive.
func (d *dashboard) runDelete(plan *archdelete.DeletePlan, row, typed string, unrecorded bool) (res *archdelete.Result, cleared bool) {
	if d.cfg.ReaderHold != nil {
		release := d.cfg.ReaderHold(plan.Folder)
		defer release()
	}
	x := d.execDeps(row)
	res = archdelete.Execute(plan, typed, unrecorded, x)
	return res, clearedCurrent(x.Pointers)
}

// clearedCurrent reports whether a delete's or Remove from list's settings write cleared the
// current archive.
func clearedCurrent(ps archdelete.PointerStore) bool {
	p, _ := ps.(*desktopPointers)
	return p != nil && p.clearedOut
}

// execDeps are Execute's seams on this computer, for the row's delete.
func (d *dashboard) execDeps(row string) archdelete.ExecDeps {
	return d.cfg.deleteExecDeps(row, d.busyOn, d.cfg.Store, d.now, d.computer())
}

// deleteExecDeps are Execute's seams on this computer for a delete planned from row — the
// Desktop's and the command line's (CommandLineDelete): busy is §7's busy row under the
// delete's own lock, store the secret store remembered keys are forgotten from (nil keeps
// them), now the clock and computer this computer's name for the record.
func (cfg Config) deleteExecDeps(row string, busy func(string) bool, store vault.Store, now func() time.Time, computer string) archdelete.ExecDeps {
	x := archdelete.ExecDeps{
		Deps:       cfg.deleteDeps(busy, cfg.sealedOnPage),
		Log:        cfg.deletionsLogPath(), // the file the plan and the rows read (del-s5-A1-04)
		Row:        row,
		Scheduler:  cfg.deleteScheduler(),
		Pointers:   &desktopPointers{settings: cfg.SettingsPath},
		ScratchDir: app.ArchiveScratchFor,
		Keyring:    desktopKeyring{store: store},
		Now:        now,
		Computer:   computer,
		Account:    cfg.account(),
		Version:    cfg.Version,
	}
	if cfg.Keys != nil {
		x.Keys = cfg.Keys
	}
	x.SettingsDir, x.ForgetRow = cfg.deleteListSeams()
	return x
}

// deleteUnlistDeps are Remove from list's seams on this computer, stopping only the names its
// question showed (named) — the Desktop's and the command line's.
func (cfg Config) deleteUnlistDeps(named []string) archdelete.UnlistDeps {
	x := archdelete.UnlistDeps{Deps: cfg.deleteDeps(nil, nil), Scheduler: cfg.deleteScheduler(),
		Pointers: &desktopPointers{settings: cfg.SettingsPath}, Named: named}
	x.SettingsDir, x.ForgetRow = cfg.deleteListSeams()
	return x
}

// deleteListSeams are where step 3 removes the alert files (beside the Desktop's settings
// file, backupalert.StreakPath) and how step 9 removes the row (from the Desktop's own list).
func (cfg Config) deleteListSeams() (string, func(string) error) {
	dir := ""
	if strings.TrimSpace(cfg.SettingsPath) != "" {
		dir = filepath.Dir(cfg.SettingsPath)
	}
	var forget func(string) error
	if strings.TrimSpace(cfg.IndexPath) != "" {
		forget = func(row string) error { return archdelete.ForgetRowAt(cfg.IndexPath, row, runtime.GOOS) }
	}
	return dir, forget
}

// desktopPointers are the Desktop's settings and the wizard's note as a delete clears them
// (archdelete.PointerStore, §5.3 step 4, §5.2): Out and BackupFor in the settings file, only
// those two fields written, under the settings lock; last-archive.txt beside the job files.
// Save writes a field only while it still holds what Load read (loaded): a switch in another
// tab between the two is kept (del-s3a-A2-06). One value per delete or Remove from list.
// clearedOut records that Save cleared the current archive: that write is a switch.
type desktopPointers struct {
	settings   string
	loaded     *archdelete.Pointers
	clearedOut bool
}

func (p *desktopPointers) Load() (archdelete.Pointers, error) {
	ptr, err := p.read()
	if err == nil {
		p.loaded = &ptr
	}
	return ptr, err
}

func (p *desktopPointers) read() (archdelete.Pointers, error) {
	var s Settings
	data, err := os.ReadFile(p.settings)
	switch {
	case err == nil:
		if json.Unmarshal(data, &s) != nil {
			return archdelete.Pointers{}, fmt.Errorf("the settings file %s cannot be read", p.settings)
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return archdelete.Pointers{}, err
	}
	ptr := archdelete.Pointers{Out: strings.TrimSpace(s.Out), BackupFor: strings.TrimSpace(s.BackupFor)}
	if lf, err := job.LastArchiveFile(); err == nil {
		ptr.LastArchive = job.ReadLastArchive(lf)
	}
	return ptr, nil
}

// Save writes what changed since Load, each field only while it still holds what Load read.
// last-archive.txt goes first and is put back when the settings cannot be saved, so a failed
// Save leaves both as they were.
func (p *desktopPointers) Save(np archdelete.Pointers) error {
	if p.loaded == nil {
		if _, err := p.Load(); err != nil {
			return err
		}
	}
	old := *p.loaded
	now, err := p.read()
	if err != nil {
		return err
	}
	lf, lerr := job.LastArchiveFile()
	var oldLast []byte
	lastChanged := lerr == nil && np.LastArchive != old.LastArchive && now.LastArchive == old.LastArchive
	if lastChanged {
		oldLast, _ = os.ReadFile(lf)
		if np.LastArchive == "" {
			err = os.Remove(lf)
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
		} else {
			err = os.WriteFile(lf, []byte(np.LastArchive+"\n"), 0o600)
		}
		if err != nil {
			return err
		}
	}
	if np.Out == old.Out && np.BackupFor == old.BackupFor {
		return nil
	}
	cleared := false
	err = UpdateSettings(p.settings, "delete", func(s *Settings) {
		cleared = false
		if strings.TrimSpace(s.Out) == old.Out {
			s.Out = np.Out
			cleared = old.Out != "" && np.Out == ""
		}
		if strings.TrimSpace(s.BackupFor) == old.BackupFor {
			s.BackupFor = np.BackupFor
		}
	})
	if err != nil && lastChanged && oldLast != nil {
		_ = os.WriteFile(lf, oldLast, 0o600)
	}
	p.clearedOut = err == nil && cleared
	return err
}

// desktopKeyring is this computer's record of encrypted archives and their remembered keys as
// step 8 forgets them (archdelete.Keyring): keyring.PlanForget at the recorded spelling, its
// ID carried into KeyPlan.ID (step 8 forgets only the record the plan found, §14), and Forget
// of the whole record — with its remembered key — only from its last copy.
type desktopKeyring struct{ store vault.Store }

// errNoSecretStore is the keyring adapter's refusal to forget an archive's last copy without
// the secret store its key may be remembered in.
var errNoSecretStore = errors.New("this computer's secret store could not be opened, so a remembered key could not be forgotten")

func (k desktopKeyring) PlanForget(spelling string) (archdelete.KeyPlan, error) {
	p, err := keyring.PlanForget(spelling, k.store)
	if err != nil {
		return archdelete.KeyPlan{}, err
	}
	gone := p.Case == encmark.Gone
	if _, err := os.Lstat(spelling); errors.Is(err, fs.ErrNotExist) {
		gone = true
	}
	others := make([]string, 0, len(p.Others))
	for _, o := range p.Others {
		others = append(others, o.Spelling)
	}
	// No secret store: Forget refuses the last copy's whole record (errNoSecretStore), so the
	// plan's key line says the record and key are kept (core-plan-record finding 1).
	return archdelete.KeyPlan{ID: p.ID, Gone: gone, Last: p.Last, Others: others, NoStore: k.store == nil, Remembered: p.Remembered, Opaque: p}, nil
}

func (k desktopKeyring) Forget(kp archdelete.KeyPlan, whole bool) error {
	p, ok := kp.Opaque.(keyring.ForgetPlan)
	if !ok {
		return errors.New("this computer's record of the archive was not planned")
	}
	o := keyring.ForgetOptions{Whole: whole && p.Last}
	if o.Whole && k.store == nil {
		// Without the secret store no one can tell whether the key is remembered, and forgetting
		// the whole record would leave a remembered key that nothing points to any more: step 8
		// skips instead, keeping the record, the key and the row with its Forget (del-s4-A1-04).
		return errNoSecretStore
	}
	if o.Whole && p.Remembered {
		o.Keys, o.Store = true, k.store
	}
	return keyring.Forget(p, o)
}

// ---- held results -------------------------------------------------------------------

// heldView is one delete's result as the page shows it (§3.1 "After a Delete", §8.1): its
// lines, and the deletion record's exact bytes (as text, and as base64 so a Blob saves the
// very bytes the digest is of).
type heldView struct {
	ID           string   `json:"id"`
	Row          string   `json:"row"`
	Folder       string   `json:"folder"`
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	Lines        []string `json:"lines"`
	RowRemoved   bool     `json:"rowRemoved"`
	Unfinished   bool     `json:"unfinished"`
	Record       string   `json:"record"`
	RecordBase64 string   `json:"recordBase64"`
	RecordName   string   `json:"recordName"`
}

// heldDeletes holds each delete's result under a server-made id (not a token: the record is
// saved any number of times) until it is dismissed or MailArchive shuts down.
type heldDeletes struct {
	mu    sync.Mutex
	order []string
	items map[string]heldView
}

func newHeldDeletes() *heldDeletes { return &heldDeletes{items: map[string]heldView{}} }

func (h *heldDeletes) add(v heldView) string {
	v.ID = randomID(12)
	h.mu.Lock()
	h.order = append(h.order, v.ID)
	h.items[v.ID] = v
	h.mu.Unlock()
	return v.ID
}

func (h *heldDeletes) dismiss(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.items[id]; !ok {
		return false
	}
	delete(h.items, id)
	for i, o := range h.order {
		if o == id {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
	return true
}

// views are the held results, oldest first.
func (h *heldDeletes) views() []heldView {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]heldView, 0, len(h.order))
	for _, id := range h.order {
		out = append(out, h.items[id])
	}
	return out
}

func (h *heldDeletes) clear() {
	h.mu.Lock()
	h.order, h.items = nil, map[string]heldView{}
	h.mu.Unlock()
}

// heldJSON is the held results as the page's data attribute carries them.
func heldJSON(held []heldView) string {
	if held == nil {
		held = []heldView{}
	}
	b, err := json.Marshal(held)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// deleteDismissAction is Dismiss (§3.1): the result and its record are no longer held.
func (d *dashboard) deleteDismissAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if !readDeleteBody(w, r, &in) {
		return
	}
	if !d.deletes.dismiss(in.ID) {
		// notHeld: dismissed already (in another tab), or held no more since a restart; the page
		// takes the result away as dismissed, so it does not hold the page's reloads for ever
		// (del-s3b-A2-05).
		writeJSON(w, map[string]any{"ok": false, "error": notHeldText, "notHeld": true})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---- Remove from list ---------------------------------------------------------------

// unlistAction is Remove from list (§3.2, §5.2): without confirm, the question and the
// schedules it names, nothing changed; with confirm, Remove from list stopping only what that
// question named (UnlistDeps.Named).
func (d *dashboard) unlistAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path    string   `json:"path"`
		Confirm bool     `json:"confirm"`
		Named   []string `json:"named"`
	}
	if !readDeleteBody(w, r, &in) {
		return
	}
	row, ok := d.cfg.matchRow(in.Path)
	if !ok {
		refuseDelete(w, notARowText)
		return
	}
	if d.running.of(row) != nil {
		refuseDelete(w, deletingRefusal)
		return
	}
	// Yes is held against an uninstall and a delete for the whole of its work (the delete
	// residuals' security pass, CON-06): refused while an uninstall removes (or has removed, or
	// stopped part way through) the settings it would write again (del-s5-A2-02), and, once
	// granted, an uninstall's Remove or a delete of the folder beginning meanwhile is refused
	// until it ends — never a check here and the writes later with an uninstall between.
	if in.Confirm {
		release, why := d.running.holdSettings(row, unlistingRefusal)
		if why != "" {
			refuseDelete(w, why)
			return
		}
		defer release()
	}
	x := d.cfg.deleteUnlistDeps(in.Named)
	if !in.Confirm {
		q, err := archdelete.PlanUnlist(row, x)
		if err != nil {
			refuseDelete(w, withReload(err, refusalText(err))) // a stale tab is told to reload (N15)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "ask": true, "row": row, "question": q.Question, "named": nonNil(q.Named)})
		return
	}
	res := archdelete.Unlist(row, x)
	// Remove from list clears the current archive when it is unreachable or no longer an
	// archive (§5.2): that is a switch, under the switch's rules (del-s3a-A4-04) — when its own
	// write cleared it, as a delete's.
	if clearedCurrent(x.Pointers) {
		d.afterSwitch()
	}
	out := map[string]any{"ok": res.Removed, "row": row, "lines": nonNil(res.Lines), "stopped": nonNil(res.Stopped)}
	if !res.Removed {
		out["error"] = strings.Join(res.Lines, " ")
		if res.Refusal != nil {
			out["error"] = withReload(res.Refusal, out["error"].(string)) // N15
		}
	}
	writeJSON(w, out)
}
