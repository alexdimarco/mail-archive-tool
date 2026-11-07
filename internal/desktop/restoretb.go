package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/tbengine"
	"mail-archive-tool/internal/tbrestore"
)

// Restore to Thunderbird, MailArchive Desktop's side (docs/design-restore-thunderbird.md
// §7.3, §6.2, §7.4, §7.5 and their folded and settled notes; slice R7; MA-1128, MA-1130,
// MA-1131). The card's steps call four guarded POSTs through d.api: the profiles (the
// server's list, by position and list ID — never a path from the page), the folder tree with
// restorable counts, the plan (kept server-side under a one-use token of its own kind, under
// a key of its own, RT-13), and the start, which checks the "will not be encrypted" tick and
// the second confirmation here, on the server, before it spends the token, and runs
// tbengine.Run as the Desktop's task in the job slot. The command line
// (cmd/mailarchive/restoretb.go) is the engine's other caller; this mirrors its use.
//
// INS-07 (the R6 adversarial pass, carried here): nothing here shows, serialises or logs an
// error's own text or the raw tbengine.Result / tbrestore.WriteResult — only the engine's
// words (a *tbrestore.Refusal's Words, Result.Words, the plan's lines) and fixed words; and
// for an encrypted archive never a folder name (the engine's words already hold none).

// Seams: the folders this computer's profiles.ini are read from (a test points them at its own
// fake profiles in a temp folder, never the person's), the engine's run (a test holds it at a
// step through Start's callbacks), and how often progress is said.
var (
	restoreTBBases = tbrestore.ProfileBases
	restoreTBRun   = tbengine.Run
	restoreTBEvery = 3 * time.Second
)

// The words of this surface (the design's where it gives them; the rest the builder's, under the
// owner-delegated rule: what happened first, then the remedy).
const (
	// restoreGoesOnText is the Lock reply while a restore of the archive runs (§6.2 note, Q6):
	// the design's words, as the copy's copyGoesOnText.
	restoreGoesOnText = "Locked. The restore into Thunderbird already started goes on until it finishes — Shut down stops it, and nothing is added to Thunderbird."
	// restoreLockedWhy: the button's reason on a locked encrypted archive (§7.3 "disabled with a
	// reason when the archive is locked").
	restoreLockedWhy  = "This archive is encrypted and locked: unlock it on its Encryption card first."
	restoreNotWaiting = "That restore is no longer planned, so nothing was restored. Click Check again."
	restoreNoFolders  = "No folder is ticked, so nothing was planned. Tick the folders to restore, or choose The whole archive, then click Check."
	restoreFolderGone = "A folder you ticked is no longer in this archive, so nothing was planned. Click Cancel, then Restore to Thunderbird… again to load its folders."
	// restorePlanFailText and restoreStartFailText stand for an error that is not one of the
	// engine's refusals: its text may name a file of the archive or of Thunderbird (INS-07), so
	// the person is told where its own words are said instead.
	restorePlanFailText  = "MailArchive could not check this restore, so nothing was written. Click Check again; if it repeats, run mailarchive restore-thunderbird -out FOLDER -plan in a terminal (FOLDER is this archive's folder), which says what stopped it."
	restoreStartFailText = "The restore did not start, so nothing was added to Thunderbird. Click Check again; if it repeats, run mailarchive restore-thunderbird -out FOLDER -plan in a terminal (FOLDER is this archive's folder), which says what stopped it."
	restoreListFailText  = "MailArchive could not read this computer's list of Thunderbird profiles, so nothing was restored. Open Thunderbird once and close it, then click Restore to Thunderbird… again."
	restorePanicText     = "The restore stopped unexpectedly, before it could say how it ended. Open Thunderbird to see whether the restored folder is there; anything an unfinished restore left is hidden in Thunderbird, and MailArchive removes it at the start of your next restore into this profile."
	// restoreCheckingText is the CLI's line when the post-write check begins (R6-10).
	restoreCheckingText = "Checking what was written: every message is read back before Thunderbird can show the folder…"
	restoreCancelLine   = "Cancelled from MailArchive Desktop: the restore stops at its next safe point and removes what it wrote, so nothing is added to Thunderbird."
	// restoreRecordNote is said beside Save this record for an encrypted archive (§6.2 note, §7.4 RT-4).
	// The R7 friction review (2026-11-07): the browser saves it with its downloads, by default
	// without asking, so the note says where it lands (lead's words).
	restoreRecordNote = "The record names the restored folders and is not encrypted. Your browser saves it with your downloads (usually your Downloads folder): move it somewhere private, or delete it when you no longer need it."
	restoreTickLine   = "I understand the restored mail will not be encrypted"
	restoreAgainHead  = "Asked a second time: "
)

// heldRestore is a restore planned on the current archive's card, awaiting Restore.
type heldRestore struct {
	plan *tbengine.Plan
	out  string
}

// restorePlanKey is the pending store's key of a restore planned into the profile whose
// tbrestore.ProfileKeyOf is profileKey: its own kind, made as deletePlanKey is, never an
// archive's key (§7.3 foldcheck RT-13) — so a second plan into the same profile replaces the
// first, and nothing else is replaced by it.
func restorePlanKey(profileKey string) string { return "restore\x00" + profileKey }

// restoreWords are what the person reads for err: a refusal's own words, else fixed (INS-07).
func restoreWords(err error, fixed string) string {
	var r *tbrestore.Refusal
	if errors.As(err, &r) && strings.TrimSpace(r.Words) != "" {
		return r.Words
	}
	return fixed
}

// readRestoreBody decodes a restore route's JSON body (an empty one is {}), capped as the
// delete routes' are; ok is false when it has answered.
func readRestoreBody(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, deleteBodyMax)).Decode(v)
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		writeJSONStatus(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": deleteTooLargeText})
		return false
	}
	writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": deleteNotUnderstood})
	return false
}

func refuseRestore(w http.ResponseWriter, msg string) {
	writeJSON(w, map[string]any{"ok": false, "error": msg})
}

// restoreArchive is the current archive a restore reads, with the registry's open of it when it
// is encrypted (nil when locked: the engine then refuses, W-LOCKED); ok is false when it answered.
// A remembered key opens it as a job would (O3). Not from a folder a delete of which is under way
// or did not finish (MA-1009's rule for every action on the current archive).
func (d *dashboard) restoreArchive(w http.ResponseWriter) (string, *keyring.Unlocked, bool) {
	out, ok := d.requireOut()
	if !ok {
		refuseRestore(w, noArchiveText)
		return "", nil, false
	}
	if msg := d.unfinishedRefusal(out); msg != "" {
		refuseRestore(w, msg)
		return "", nil, false
	}
	var keys *keyring.Unlocked
	if app.ArchiveEncrypted(out) && d.cfg.Keys != nil {
		e := d.cfg.Keys.get(out)
		if e == nil {
			e = d.cfg.Keys.remembered(out)
		}
		if e != nil {
			keys = e.u
		}
	}
	return out, keys, true
}

// restoreProfile is the profile at position in the list the page was sent, read again (§3.1
// step 6 note, RT-15): a list that changed, or an entry that is not the folder it was, is
// W-PROFCHANGED.
func restoreProfile(listID string, position int) (tbrestore.Profile, error) {
	list, err := tbrestore.ReadProfiles(restoreTBBases())
	if err != nil {
		return tbrestore.Profile{}, err
	}
	return list.Resolve(listID, position)
}

// restoreProfilesAction is step 1 (§3.1, §7.3): this computer's Thunderbird profiles, each by
// its position in a list the server names by its ID, with the one pre-selected (§3.1 step 4
// as folded: one the archive does not back up first). Read-only.
func (d *dashboard) restoreProfilesAction(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !readRestoreBody(w, r, &in) {
		return
	}
	out, _, ok := d.restoreArchive(w)
	if !ok {
		return
	}
	list, err := tbrestore.ReadProfiles(restoreTBBases())
	if err != nil {
		refuseRestore(w, restoreWords(err, restoreListFailText))
		return
	}
	if len(list.Profiles) == 0 {
		refuseRestore(w, restoreWords(tbrestore.NoProfileRefusal(), restoreListFailText))
		return
	}
	src := tbengine.ArchiveSources(out)
	backsUp := func(dir string) bool { return src.Auto || tbrestore.BacksUp(src.Paths, dir) }
	source := ""
	for _, p := range list.Profiles {
		if source == "" && backsUp(p.Dir) {
			source = p.Dir
		}
	}
	def, hasDef := list.Default(backsUp, source)
	if !hasDef {
		def = -1
	}
	profiles := make([]map[string]any, 0, len(list.Profiles))
	for i, p := range list.Profiles {
		profiles = append(profiles, map[string]any{"position": i, "name": p.Name, "dir": p.Dir, "usable": p.Usable,
			"unusable": p.Unusable, "opensByDefault": p.OpensByDefault, "backedUp": backsUp(p.Dir)})
	}
	writeJSON(w, map[string]any{"ok": true, "listID": list.ID, "profiles": profiles, "default": def})
}

// restoreFoldersAction is step 2's tree (§6.5): the archive's folders with how many of their
// messages can go back, for the profile chosen (its binding checked again). Read-only.
func (d *dashboard) restoreFoldersAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ListID   string `json:"listID"`
		Position int    `json:"position"`
	}
	if !readRestoreBody(w, r, &in) {
		return
	}
	out, keys, ok := d.restoreArchive(w)
	if !ok {
		return
	}
	if _, err := restoreProfile(in.ListID, in.Position); err != nil {
		refuseRestore(w, restoreWords(err, restoreListFailText))
		return
	}
	fs, err := tbengine.Folders(out, keys, true)
	if err != nil {
		refuseRestore(w, restoreWords(err, restorePlanFailText))
		return
	}
	folders := make([]map[string]any, 0, len(fs))
	total := 0
	for _, f := range fs {
		folders = append(folders, map[string]any{"id": f.ID, "parent": f.Parent, "name": f.Name, "store": f.Store, "count": f.Count, "total": f.Total})
		if f.Parent == "" {
			total += f.Total
		}
	}
	writeJSON(w, map[string]any{"ok": true, "folders": folders, "total": total})
}

// restorePlanAction is step 3 (§6.4, §7.3): the plan, writing nothing, kept for ten minutes
// under a one-use token of its own kind and key (RT-13). Folder ids are checked against the
// archive's own tree here, never used to build a path; the profile by its list binding.
func (d *dashboard) restorePlanAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ListID   string   `json:"listID"`
		Position int      `json:"position"`
		Whole    bool     `json:"whole"`
		Folders  []string `json:"folders"`
	}
	if !readRestoreBody(w, r, &in) {
		return
	}
	out, keys, ok := d.restoreArchive(w)
	if !ok {
		return
	}
	req := tbengine.Request{Out: out, Keys: keys, Profile: tbengine.ProfileChoice{ListID: in.ListID, Position: in.Position, Bases: restoreTBBases()}}
	if !in.Whole {
		if len(in.Folders) == 0 {
			refuseRestore(w, restoreNoFolders)
			return
		}
		fs, err := tbengine.Folders(out, keys, false)
		if err != nil {
			refuseRestore(w, restoreWords(err, restorePlanFailText))
			return
		}
		known := make(map[string]bool, len(fs))
		for _, f := range fs {
			known[f.ID] = true
		}
		for _, id := range in.Folders {
			if !known[id] {
				refuseRestore(w, restoreFolderGone)
				return
			}
		}
		req.Folders = in.Folders
	}
	src := tbengine.ArchiveSources(out)
	req.Sources = &src
	plan, err := tbengine.MakePlan(req)
	if err != nil {
		refuseRestore(w, restoreWords(err, restorePlanFailText))
		return
	}
	token, ok := d.keep(w, &pendingItem{key: restorePlanKey(tbrestore.ProfileKeyOf(plan.Profile.Dir)), out: out,
		restorePlan: &heldRestore{plan: plan, out: out}})
	if !ok {
		return
	}
	lines := make([]map[string]any, 0, len(plan.Lines))
	for _, l := range plan.Lines {
		lines = append(lines, map[string]any{"code": l.Code, "words": l.Words})
	}
	reply := map[string]any{"ok": true, "token": token, "lines": lines, "encrypted": plan.Encrypted, "askTwice": plan.AskTwice(),
		"profile": plan.Profile.Name, "top": plan.Top}
	if plan.Encrypted {
		reply["tickLine"] = restoreTickLine
	}
	if plan.AskTwice() {
		reply["againLine"] = restoreAgainHead + strings.Join(plan.AskReasons(), "; ") + "."
	}
	writeJSON(w, reply)
}

// restoreStartAction is step 4 (§7.3, §6.2): the plan its token holds, started as the Desktop's
// task. The tick and the second confirmation are checked here first (tbengine's own Confirmed),
// so one missing leaves the plan waiting for it; then the token is spent and tbengine.Run makes
// every check again under its locks (§5.3).
func (d *dashboard) restoreStartAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token          string `json:"token"`
		NotEncryptedOK bool   `json:"notEncryptedOK"`
		ConfirmAgain   bool   `json:"confirmAgain"`
	}
	if !readRestoreBody(w, r, &in) {
		return
	}
	it := d.pending.get(in.Token, isRestorePlan)
	if it == nil {
		writeJSON(w, map[string]any{"ok": false, "error": restoreNotWaiting, "planUsed": true})
		return
	}
	p := it.restorePlan.plan
	start := tbengine.Start{NotEncryptedOK: in.NotEncryptedOK, ConfirmAgain: in.ConfirmAgain}
	if err := p.Confirmed(start); err != nil {
		need := "again"
		if tbrestore.RefusalCode(err) == "W-TICK" {
			need = "tick"
		}
		writeJSON(w, map[string]any{"ok": false, "error": restoreWords(err, restoreStartFailText), "need": need})
		return
	}
	if it = d.pending.take(in.Token, isRestorePlan); it == nil {
		writeJSON(w, map[string]any{"ok": false, "error": restoreNotWaiting, "planUsed": true})
		return
	}
	out := it.restorePlan.out
	var slot uint64
	err := d.runCancellableJobOnClaiming(out, func() {
		slot = d.cap.slot // under the slot's lock, before the job's goroutine starts
		d.rtb.begin(slot, out, p.Encrypted)
	}, func(j keyedJob, logger *log.Logger) (string, error) {
		return d.restoreJob(j.ctx, slot, p, start, logger)
	})
	if err != nil {
		// The slot refused it (a task already running, a delete of the folder): fixed words. The
		// token is spent, as a delete's is when it is refused after its take.
		writeJSON(w, map[string]any{"ok": false, "error": err.Error() + " Then click Check again.", "planUsed": true})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "job": strconv.FormatUint(slot, 10), "wait": tbrestore.WaitWords(), "encrypted": p.Encrypted})
}

// restoreJob runs the plan in the job slot: progress said in Activity at most every
// restoreTBEvery and at the last message — counts only for an encrypted archive (§6.2) —, the
// post-write check said when it begins (R6-10), then the result words. A panic is recovered
// here into fixed words, so its text never reaches Activity (INS-07); the engine's deferred
// releases have run by then.
func (d *dashboard) restoreJob(ctx context.Context, slot uint64, p *tbengine.Plan, s tbengine.Start, logger *log.Logger) (result string, err error) {
	defer func() {
		if r := recover(); r != nil {
			d.rtb.end(slot, nil, restorePanicText)
			result, err = "", spokenErr(restorePanicText)
		}
	}()
	var last time.Time
	s.Progress = func(g tbengine.Progress) {
		if now := time.Now(); last.IsZero() || now.Sub(last) >= restoreTBEvery || g.Done == g.Total {
			last = now
			line := fmt.Sprintf("Restoring: %s of %s messages", restoreCommas(g.Done), restoreCommas(g.Total))
			if g.Folder != "" && !p.Encrypted {
				line += " (" + g.Folder + ")"
			}
			logger.Print(line)
			d.rtb.progress(slot, line)
		}
	}
	s.Checking = func() {
		logger.Print(restoreCheckingText)
		d.rtb.checking(slot)
	}
	res, rerr := restoreTBRun(ctx, p, s)
	if rerr != nil {
		words := restoreWords(rerr, restoreStartFailText)
		d.rtb.end(slot, nil, words)
		return "", spokenErr(words)
	}
	v := &restoreView{Words: res.Words, Done: res.Done(), Stopped: res.Stopped(),
		Record:     restoreRecordText(p, res.Words, d.now(), d.computer(), d.cfg.account(), d.cfg.Version),
		RecordName: fileNameFor("MailArchive restore record", filepath.Base(p.Out), d.now().Format("2006-01-02"))}
	if p.Encrypted {
		v.RecordNote = restoreRecordNote
	}
	d.rtb.end(slot, v, "")
	if res.Done() {
		return res.Words, nil
	}
	return "", spokenErr(res.Words)
}

// restoreRecordText is the record Save this record saves (§7.4): the plan and the result as the
// person read them, with when, where and by whom — only the engine's words (INS-07).
func restoreRecordText(p *tbengine.Plan, result string, when time.Time, computer, account, version string) string {
	var b strings.Builder
	b.WriteString("MailArchive — Restore to Thunderbird record\n")
	b.WriteString("Date: " + when.Format("2 January 2006, 15:04 MST") + "\n")
	b.WriteString("Computer: " + computer + "\nAccount: " + account + "\n")
	if version != "" {
		b.WriteString("MailArchive: " + version + "\n")
	}
	b.WriteString("Archive: " + p.Out + "\n\nThe plan:\n" + p.Text() + "\n\nThe result:\n" + result + "\n")
	return b.String()
}

// restoreCommas is n with thousands separators ("4,310").
func restoreCommas(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return s
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// restoreView is a restore's result as the panel shows it: the engine's words, how it ended,
// and the record to save.
type restoreView struct {
	Words      string `json:"words"`
	Done       bool   `json:"done"`
	Stopped    bool   `json:"stopped"`
	Record     string `json:"record"`
	RecordName string `json:"recordName"`
	RecordNote string `json:"recordNote,omitempty"`
}

// restoreState is the restore in the job slot (one at a time: the slot runs one task), kept
// until another task takes the slot: its slot id, its archive, whether it runs, its last
// progress line and whether the post-write check began, then its result or refusal words.
type restoreState struct {
	mu        sync.Mutex
	slot      uint64
	out       string
	encrypted bool
	running   bool
	isChecked bool
	line      string
	result    *restoreView
	failed    string
	done      chan struct{}
}

func (s *restoreState) begin(slot uint64, out string, encrypted bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.slot, s.out, s.encrypted, s.running = slot, out, encrypted, true
	s.isChecked, s.line, s.result, s.failed = false, "", nil, ""
	s.done = make(chan struct{})
	s.mu.Unlock()
}

func (s *restoreState) progress(slot uint64, line string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.slot == slot {
		s.line = line
	}
	s.mu.Unlock()
}

func (s *restoreState) checking(slot uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.slot == slot {
		s.isChecked = true
	}
	s.mu.Unlock()
}

func (s *restoreState) end(slot uint64, v *restoreView, failed string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.slot != slot || !s.running {
		return
	}
	s.running, s.result, s.failed = false, v, failed
	close(s.done)
}

// isSlot reports a restore that runs as the job with this slot id.
func (s *restoreState) isSlot(slot uint64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running && s.slot == slot
}

// runningOn reports a restore from the archive at out that runs now.
func (s *restoreState) runningOn(out string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running && samePath(s.out, out)
}

// view is what /api/activity says of the restore when it is the job with this slot id, nil
// otherwise. W-WAIT is said for the whole run (§5.2 note, lab finding 3).
func (s *restoreState) view(slot uint64) map[string]any {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.slot == 0 || s.slot != slot {
		return nil
	}
	v := map[string]any{"running": s.running, "encrypted": s.encrypted, "line": s.line, "checking": s.isChecked}
	if s.running {
		v["wait"] = tbrestore.WaitWords()
		if s.isChecked {
			v["checkingLine"] = restoreCheckingText
		}
	}
	if s.result != nil {
		v["result"] = s.result
	}
	if s.failed != "" {
		v["error"] = s.failed
	}
	return v
}

// wait waits, within ctx, for a running restore to end: Shut down's drain, after the
// Desktop's own context has stopped it, while it removes what it wrote (§8).
func (s *restoreState) wait(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	running, done := s.running, s.done
	s.mu.Unlock()
	if !running {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
