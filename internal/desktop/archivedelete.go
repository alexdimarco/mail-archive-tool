package desktop

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
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
	deleteLogLine = "A line is added to this computer's deletion log (deletions.log), which keeps the date, the folder and what was removed."
	deleteKeyLine = "This computer will forget this archive's key. Any other copy of this archive will need its password or recovery key to open."
	deleteThreeLine = "Remove from list forgets the row (and, after asking, stops its schedules) and keeps the folder. Delete erases the archive's files. Forget (encrypted archives) only stops this computer protecting the folder."
	deleteSaveLine  = "Save this record now: MailArchive keeps no copy after you close this result."
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

// matchRow is §3.1's row rule [C20]: the row of Your archives that path is — by file identity
// when the folder can be read (a link to it, another case or an 8.3 name is that row), else
// only by the row's own recorded spelling, exactly (an unplugged drive, a folder gone after an
// unfinished delete). It returns the row's own spelling.
func (cfg Config) matchRow(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" || strings.TrimSpace(cfg.IndexPath) == "" {
		return "", false
	}
	idx, err := archindex.ReadFile(cfg.IndexPath)
	if err != nil {
		return "", false
	}
	if fi, err := os.Stat(path); err == nil {
		for _, e := range idx.Entries {
			if fe, err := os.Stat(e.Path); err == nil && os.SameFile(fi, fe) {
				return e.Path, true
			}
		}
		return "", false
	}
	for _, e := range idx.Entries {
		if e.Path == path {
			return e.Path, true
		}
	}
	return "", false
}

// knownArchives are the archive folders the Desktop knows (its list, and this computer's
// record of encrypted archives), read only: the uninstall's kept list and the delete's
// protected "another known archive".
func (cfg Config) knownArchives() []string {
	var paths []string
	seen := map[string]bool{}
	if strings.TrimSpace(cfg.IndexPath) != "" {
		if idx, err := archindex.ReadFile(cfg.IndexPath); err == nil {
			for _, e := range idx.Entries {
				if p := strings.TrimSpace(e.Path); p != "" {
					paths = append(paths, p)
					seen[encrecord.Spelling(p)] = true
				}
			}
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
	}
	return paths
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
func (cfg Config) deleteDeps(busy func(string) bool, sealed func(string) (archdelete.Captured, bool)) archdelete.Deps {
	logPath, _ := archdelete.DefaultLogPath()
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
	}
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
// (a backup, Run now and its pre-sync, a capture, a verify, a rebuild) on the folder, or an
// export reading it. It never counts the archive lock, which the delete itself holds when it
// asks again (archdelete.ExecDeps.Deps).
func (d *dashboard) busyOn(folder string) bool {
	c := d.cap
	c.mu.Lock()
	running, slot := c.running, c.slotOut
	c.mu.Unlock()
	if running && (slot == "" || sameArchiveFolder(slot, folder)) {
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
	ok, unfinished bool
	why            string
}

// deleteRowFacts is a row's Delete state: offered, or why not, in §7's words; and whether a
// delete of the folder did not finish (§5.4), whose row offers Delete again.
func deleteRowFacts(path string, deps archdelete.Deps, knownArchive bool) deleteFacts {
	var f deleteFacts
	if deps.Log != "" {
		if u, err := archdelete.UnfinishedDelete(deps.Log, path, ""); err == nil && u != nil {
			f.unfinished = true
		}
	}
	if err := archdelete.RowRefusal(path, deps, knownArchive); err != nil {
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
	plan, err := archdelete.Plan(row, d.cfg.deleteDeps(d.planBusy, nil))
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
		stores = append(stores, map[string]any{"number": i + 1, "name": s})
	}
	m := map[string]any{
		"folder": p.Folder, "name": p.Name, "kind": p.Kind.String(),
		"files": p.Files, "bytes": p.Bytes, "size": sizeText(p.Bytes),
		"schedules": nonNil(p.Schedules.Names()), "checkYourself": nonNil(p.Schedules.CheckYourself), "notes": nonNil(notes),
		"paused": p.Paused, "synced": p.Synced,
		"kept": len(p.Kept), "unrecorded": len(p.Unrecorded), "unrecordedNames": nonNil(unrecorded), "stores": stores,
		"rerun":        p.Rerun != archdelete.RerunNone,
		"typeLine":     fmt.Sprintf("Type the folder's name, %s, to delete it", p.Name),
		"doesNotReach": "What Delete does not reach: " + archdelete.DoesNotReach,
		"logLine":      deleteLogLine,
		"threeLine":    deleteThreeLine,
	}
	if p.Paused {
		m["pausedLine"] = "This archive is paused."
	}
	if p.Synced != "" {
		m["syncedLine"] = fmt.Sprintf("This folder is synced with %s. Deleting it here also deletes it there, and %s may keep it in its own recycle bin: empty that too.", p.Synced, p.Synced)
	}
	if n := len(p.Unrecorded); n > 0 {
		m["unrecordedBox"] = fmt.Sprintf("Also delete these %d files (for retention)", n)
	}
	if p.Kind == archdelete.Encrypted {
		m["keyLine"] = deleteKeyLine
	}
	return m
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
	}
	if !readDeleteBody(w, r, &in) {
		return
	}
	it := d.pending.get(in.Token, isDeletePlan)
	if it == nil {
		refuseDelete(w, deleteNotWaiting)
		return
	}
	// A name that does not match changes nothing and keeps the plan: the person types again.
	if name := it.deletePlan.plan.Name; !archdelete.NameMatches(in.Typed, name) {
		refuseDelete(w, fmt.Sprintf("The name you typed does not match this folder's name, %s, so nothing was deleted. Type it exactly as shown.", name))
		return
	}
	if it = d.pending.take(in.Token, isDeletePlan); it == nil {
		refuseDelete(w, deleteNotWaiting)
		return
	}
	plan, row := it.deletePlan.plan, it.deletePlan.row
	release := func() {}
	if d.cfg.ReaderHold != nil {
		release = d.cfg.ReaderHold(plan.Folder)
	}
	res := archdelete.Execute(plan, in.Typed, in.Unrecorded, d.execDeps(row))
	release()
	v := heldView{Row: row, Folder: plan.Folder, Name: plan.Name, Status: res.Status, Lines: append([]string(nil), res.Lines...),
		RowRemoved: res.RowRemoved, Unfinished: res.Unfinished}
	if res.Record != nil {
		v.Record, v.RecordBase64 = string(res.Record), base64.StdEncoding.EncodeToString(res.Record)
		v.RecordName = fileNameFor("MailArchive deletion record", plan.Name, d.now().Format("2006-01-02"))
		v.Lines = append(v.Lines, deleteSaveLine)
		v.ID = d.deletes.add(v)
	}
	out := map[string]any{"ok": res.Refusal == nil, "id": v.ID, "row": row, "folder": v.Folder, "name": v.Name, "status": v.Status,
		"lines": v.Lines, "rowRemoved": v.RowRemoved, "unfinished": v.Unfinished,
		"record": v.Record, "recordBase64": v.RecordBase64, "recordName": v.RecordName}
	if res.Refusal != nil {
		out["error"] = res.Message()
	}
	writeJSON(w, out)
}

// execDeps are Execute's seams on this computer, for the row's delete.
func (d *dashboard) execDeps(row string) archdelete.ExecDeps {
	cfg := d.cfg
	x := archdelete.ExecDeps{
		Deps:       cfg.deleteDeps(d.busyOn, nil),
		Row:        row,
		Scheduler:  cfg.deleteScheduler(),
		Pointers:   desktopPointers{settings: cfg.SettingsPath},
		ScratchDir: app.ArchiveScratchFor,
		Keyring:    desktopKeyring{store: cfg.Store},
		Now:        d.now,
		Computer:   d.computer(),
		Account:    cfg.account(),
		Version:    cfg.Version,
	}
	if cfg.Keys != nil {
		x.Keys = cfg.Keys
	}
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
type desktopPointers struct{ settings string }

func (p desktopPointers) Load() (archdelete.Pointers, error) {
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

// Save writes what changed. last-archive.txt goes first and is put back when the settings
// cannot be saved, so a failed Save leaves both as they were.
func (p desktopPointers) Save(np archdelete.Pointers) error {
	old, err := p.Load()
	if err != nil {
		return err
	}
	lf, lerr := job.LastArchiveFile()
	var oldLast []byte
	lastChanged := lerr == nil && np.LastArchive != old.LastArchive
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
	err = UpdateSettings(p.settings, "delete", func(s *Settings) {
		if strings.TrimSpace(s.Out) == old.Out {
			s.Out = np.Out
		}
		if strings.TrimSpace(s.BackupFor) == old.BackupFor {
			s.BackupFor = np.BackupFor
		}
	})
	if err != nil && lastChanged && oldLast != nil {
		_ = os.WriteFile(lf, oldLast, 0o600)
	}
	return err
}

// desktopKeyring is this computer's record of encrypted archives and their remembered keys as
// step 8 forgets them (archdelete.Keyring): keyring.PlanForget at the recorded spelling, its
// ID carried into KeyPlan.ID (step 8 forgets only the record the plan found, §14), and Forget
// of the whole record — with its remembered key — only from its last copy.
type desktopKeyring struct{ store graphconfig.SecretStore }

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
	return archdelete.KeyPlan{ID: p.ID, Gone: gone, Last: p.Last, Others: others, Opaque: p}, nil
}

func (k desktopKeyring) Forget(kp archdelete.KeyPlan, whole bool) error {
	p, ok := kp.Opaque.(keyring.ForgetPlan)
	if !ok {
		return errors.New("this computer's record of the archive was not planned")
	}
	o := keyring.ForgetOptions{Whole: whole && p.Last}
	if o.Whole && p.Remembered && k.store != nil {
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
		refuseDelete(w, notHeldText)
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
	x := archdelete.UnlistDeps{Deps: d.cfg.deleteDeps(nil, nil), Scheduler: d.cfg.deleteScheduler(),
		Pointers: desktopPointers{settings: d.cfg.SettingsPath}, Named: in.Named}
	x.SettingsDir, x.ForgetRow = d.cfg.deleteListSeams()
	if !in.Confirm {
		q, err := archdelete.PlanUnlist(row, x)
		if err != nil {
			refuseDelete(w, refusalText(err))
			return
		}
		writeJSON(w, map[string]any{"ok": true, "ask": true, "row": row, "question": q.Question, "named": nonNil(q.Named)})
		return
	}
	res := archdelete.Unlist(row, x)
	out := map[string]any{"ok": res.Removed, "row": row, "lines": nonNil(res.Lines), "stopped": nonNil(res.Stopped)}
	if !res.Removed {
		out["error"] = strings.Join(res.Lines, " ")
	}
	writeJSON(w, out)
}
