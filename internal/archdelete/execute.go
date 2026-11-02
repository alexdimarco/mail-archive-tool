package archdelete

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// removeFileFn removes one name (a file, or an empty folder) inside the archive
// folder through its os.Root: a test seam for a file Windows holds open, a drive
// gone, a lock file that will not go.
var removeFileFn = func(root *os.Root, rel string) error { return root.Remove(rel) }

// retryPause is the pause between tries of a removal Windows may refuse for a
// moment (a lock file just released, a folder an indexer has open).
var retryPause = 50 * time.Millisecond

// NameMatches is §3.1's typed-name check [C19]: typed equals the folder's own
// name after Unicode NFC normalisation on both sides, with no case folding and
// nothing trimmed, so a name typed or pasted decomposed (NFD) still matches and
// another case never does.
func NameMatches(typed, name string) bool {
	return typed != "" && norm.NFC.String(typed) == norm.NFC.String(name)
}

// Execute deletes the archive plan names, after the person typed its folder's name
// (typed) and answered §4.3's question (unrecorded: also delete the mail copies the
// archive did not record). It runs §5.3's steps in order, each with its success
// rule and its words, and says exactly what it did (§8): a refusal changes
// nothing; a stop after step 2 began is "not fully deleted" and leaves the row
// "Delete not finished" (§5.4). The archive lock is released, the being-deleted
// mark cleared and the finished line written in a deferred cleanup, after a
// refusal or a recovered panic too.
func Execute(plan *DeletePlan, typed string, unrecorded bool, x ExecDeps) (res *Result) {
	res = &Result{}
	e := &run{plan: plan, x: x, res: res, unrecorded: unrecorded}
	defer e.sayFinished() // deferred first, so it runs last: when step 10 did not say it
	if r := e.setUp(); r != nil {
		res.refuseBefore(r)
		return res
	}
	if !NameMatches(typed, plan.Name) {
		res.refuseBefore(&Refusal{Case: CaseName, Message: fmt.Sprintf("The name you typed does not match this folder's name, %s, so nothing was deleted. Type it exactly as shown.", plan.Name)})
		return res
	}
	if err := e.logStarted(); err != nil {
		res.refuseBefore(&Refusal{Case: CaseLog, Message: msgLogFailed})
		return res
	}
	defer e.cleanup()
	e.steps()
	return res
}

func (r *Result) refuseBefore(ref *Refusal) {
	r.Status, r.Refusal, r.Lines = StatusRefused, ref, []string{ref.Message}
}

// run is one delete's state.
type run struct {
	plan       *DeletePlan
	x          ExecDeps
	res        *Result
	unrecorded bool
	log, row   string
	// id is this delete's run id, on each of its deletions.log lines.
	id string

	logged   bool
	finished bool // ExecDeps.Finished was called
	lock     *lockfile.Lock
	marked   bool
	root     *os.Root
	// step is the step running; stage2 is set once step 2 began: any stop from then
	// on is "not fully deleted" and leaves the row unfinished.
	step    int
	stage2  bool
	outcome string
	done    []string
	notes   []string
	// emptyLeft: a kept folder that is empty or holds only litter could not be
	// removed. leftIn are the names step 7 found still in the folder beside the lock
	// file and litter.
	emptyLeft bool
	leftIn    []string
	detail    string
}

// setUp fills the defaults and checks the seams the plan needs.
func (e *run) setUp() *Refusal {
	if e.plan == nil {
		return &Refusal{Case: CaseInternal, Message: "MailArchive could not delete this archive: it was given no plan. Nothing was deleted."}
	}
	missing := func(what string) *Refusal {
		return &Refusal{Case: CaseInternal, Message: fmt.Sprintf("MailArchive could not delete this archive: it was not given %s. Nothing was deleted.", what)}
	}
	switch {
	case e.x.Scheduler == nil && len(e.plan.Schedules.Entries) > 0:
		return missing("the scheduler")
	case e.x.Pointers == nil:
		return missing("its settings")
	case e.plan.Kind == Encrypted && ((e.x.Keys == nil && !e.x.NoPage) || e.x.Keyring == nil || e.x.ScratchDir == nil):
		return missing("this computer's keys and working folder")
	}
	e.log = e.x.Log
	if e.log == "" {
		p, err := DefaultLogPath()
		if err != nil {
			return &Refusal{Case: CaseLog, Message: msgLogFailed}
		}
		e.log = p
	}
	e.row = e.x.Row
	if e.row == "" {
		e.row = e.plan.Folder
	}
	return nil
}

func (e *run) now() time.Time {
	if e.x.Now != nil {
		return e.x.Now()
	}
	return time.Now()
}

func (e *run) stamp() string { return e.now().UTC().Format(time.RFC3339Nano) }

// logStarted is step 0.
func (e *run) logStarted() error {
	p := e.plan
	choice := "no"
	if e.unrecorded {
		choice = "yes"
	}
	e.id = newRunID()
	l := logLine{At: e.stamp(), Event: evStarted, Run: e.id, Folder: p.Folder, Row: e.row, Kind: p.Kind.String(),
		Files: intp(p.Files), Bytes: int64p(p.Bytes), Unrecorded: intp(len(p.Unrecorded)), DeleteUnrecorded: choice,
		KeyForgotten: p.KeyForgottenBefore}
	if p.Identity != (Identity{}) {
		id := p.Identity
		l.Identity = &id
	}
	if p.Kind == Encrypted {
		switch {
		case p.Record.Recorded:
			l.RecordID, l.RecordSpelling = p.Record.ID, p.Record.Spelling
		case !p.RecordUnknown:
			l.NotRecorded = true
		}
	}
	switch p.Rerun {
	case RerunFolderGone:
		l.Rerun = rerunFolderGoneWord
	case RerunNoAnchor:
		l.Rerun = "no manifest"
	}
	if err := appendLog(e.log, l); err != nil {
		return err
	}
	e.logged = true
	return nil
}

// steps runs steps 1 to 9.
func (e *run) steps() {
	p := e.plan
	if p.Rerun == RerunFolderGone {
		e.step = 1
		if _, err := os.Lstat(p.Folder); !errors.Is(err, fs.ErrNotExist) {
			e.refuseUnder(&Refusal{Case: CaseChanged, Message: msgChanged})
			return
		}
		e.res.FolderGone = true
		e.notes = append(e.notes, "Steps 1 and 5 to 7 (the lock, the working folder, the files, the folder) were skipped: an earlier delete removed the folder.")
		if !e.configSide() || !e.keyAndRow() {
			return
		}
		e.finish()
		return
	}
	if !e.lockAndRecheck() {
		return
	}
	if p.Kind == Encrypted {
		if (!e.x.NoPage && !e.lockOnPage()) || !e.scratchCheck() {
			return
		}
	}
	if !e.configSide() {
		return
	}
	if p.Kind == Encrypted && !e.scratchRemove() {
		return
	}
	ok, stopped := e.removeFiles()
	if stopped {
		return
	}
	e.release()
	afterRelease(p.Folder)
	if !ok {
		e.stopAt6()
		return
	}
	e.step = 7
	gone, lockLeft, emptyLeft := e.tidyFolder()
	e.res.FolderGone, e.res.LockFileLeft, e.emptyLeft = gone, lockLeft, emptyLeft
	e.logFolder(gone)
	if p.Rerun == RerunNoAnchor {
		// §5.4: never complete, and never steps 8 and 9 here. With the folder gone its
		// `folder removed` line makes Delete again the folder-gone re-run, which
		// finishes the keyring and the row (del-s1b-A1-02, conformance 9).
		e.stopNoAnchor(gone, lockLeft)
		return
	}
	if !e.keyAndRow() {
		return
	}
	e.finish()
}

// refuseUnder is a refusal after the started line (§3.1, §7): nothing was removed.
func (e *run) refuseUnder(r *Refusal) {
	e.outcome = outRefused
	e.res.Status, e.res.Refusal = StatusRefusedAfterStart, r
	e.res.Lines = []string{r.Message}
	if e.marked {
		e.res.Lines = append(e.res.Lines, lockedLine)
	}
}

// stop is a stop after step 2 began: not fully deleted, the row unfinished.
func (e *run) stop(msg string) {
	e.outcome = outPartial
	e.res.Status, e.res.Unfinished = StatusPartial, true
	e.res.Lines = []string{msg}
	if e.marked {
		e.res.Lines = append(e.res.Lines, lockedLine)
	}
	e.stoppedLines()
}

func (e *run) stoppedLines() {
	if e.step > 2 {
		e.scheduleResultLines()
	}
}

// scheduleResultLines are the result's lines for what steps 2 and 3 did to the archive's backups
// (B1, B15): each scheduled backup stopped, described as the plan described it, and the Run now
// backup removed.
func (e *run) scheduleResultLines() {
	for _, n := range e.res.Stopped {
		e.res.Lines = append(e.res.Lines, e.plan.Schedules.stoppedLine(n))
	}
	if len(e.res.RunNowRemoved) > 0 {
		e.res.Lines = append(e.res.Lines, "Removed its Run now backup.")
	}
}

// lockAndRecheck is step 1 (a): the archive lock, then the plan again under it, except the
// scratch listing (§5.3 step 1 (a)): the page's own reader is still open until 1 (b), and
// 1 (c) lists the scratch folders again. Nor are the sealed values read again: the record
// states the plan's, so a second read would use the archive's key for nothing, under the lock.
func (e *run) lockAndRecheck() bool {
	e.step = 1
	p := e.plan
	deps := e.x.Deps
	deps.ScratchDir, deps.Sealed = nil, nil
	l, err := lockfile.AcquireAs(filepath.Join(p.Folder, lockName), "delete")
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			e.refuseUnder(&Refusal{Case: CaseBusy, Message: busyMessage(p.Folder)})
			return false
		}
		if _, perr := Plan(p.Folder, deps); perr != nil {
			if r := AsRefusal(perr); r != nil {
				e.refuseUnder(r)
				return false
			}
		}
		e.refuseUnder(AsRefusal(unreadableRefusal(filepath.Join(p.Folder, lockName), err)))
		return false
	}
	e.lock = l
	e.done = append(e.done, "archive locked")
	again, err := Plan(p.Folder, deps)
	if err != nil {
		r := AsRefusal(err)
		if r == nil {
			r = &Refusal{Case: CaseUnreadable, Message: util.StripControl(err.Error())}
		}
		e.refuseUnder(r)
		return false
	}
	if !samePlan(p, again) {
		e.refuseUnder(&Refusal{Case: CaseChanged, Message: msgChanged})
		return false
	}
	return true
}

// samePlan is §3.1's re-check: the same folder, the manifest or vault unchanged,
// the same names, unrecorded copies and schedules, and this computer's record of an
// encrypted archive as the plan found it — a record forgotten (or recorded) since would
// otherwise reach step 8 with the plan's, and the result say this computer still
// remembers a key it forgot (del-s3a-A2-01).
func samePlan(a, b *DeletePlan) bool {
	return a.Identity == b.Identity && a.Kind == b.Kind && a.Rerun == b.Rerun && a.Record == b.Record &&
		a.Anchor.Name == b.Anchor.Name && a.Anchor.Size == b.Anchor.Size && a.Anchor.SHA256 == b.Anchor.SHA256 &&
		a.Anchor.ModTime.Equal(b.Anchor.ModTime) &&
		sameList(itemRels(a.Remove), itemRels(b.Remove)) && sameList(itemRels(a.Unrecorded), itemRels(b.Unrecorded)) &&
		sameList(a.Schedules.Names(), b.Schedules.Names()) && sameList(a.Schedules.notRemoved(), b.Schedules.notRemoved())
}

func itemRels(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Rel)
	}
	return out
}

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lockOnPage is step 1 (b): the archive locked in the page, its being-deleted mark
// set, its reader closed. EndDelete runs in the cleanup whatever happens next.
func (e *run) lockOnPage() bool {
	e.marked = true
	if err := e.x.Keys.LockForDelete(e.plan.Folder); err != nil {
		e.refuseUnder(&Refusal{Case: CaseInternal, Message: fmt.Sprintf("MailArchive could not lock this archive on this page (%s), so nothing was deleted. Click Delete again.", cleanErr(err))})
		return false
	}
	e.done = append(e.done, "archive locked on this page")
	return true
}

// scratchDirs are the archive's scratch folders, one per spelling of its folder:
// the plan's, the row's, each entry's recorded -out and the record's Spelling
// (§3.1 [R3:NF-C7-SCRATCH-NAME]). None when the archive's id is not known.
func (e *run) scratchDirs() ([]string, error) {
	return scratchDirsOf(e.plan, e.row, e.x.ScratchDir)
}

// scratchCheck is step 1 (c): the scratch folders listed again, where any held
// reader lock now refuses.
func (e *run) scratchCheck() bool {
	dirs, err := e.scratchDirs()
	if err != nil {
		e.refuseUnder(scratchNotFound(err))
		return false
	}
	for _, d := range dirs {
		if _, err := scratchTargets(d); err != nil {
			e.refuseUnder(scratchRefusal(err))
			return false
		}
	}
	return true
}

// configSide is steps 2, 3 and 4: the schedules, their settings files, and the
// settings that point at the archive.
func (e *run) configSide() bool {
	p := e.plan
	if !e.proceed(2) {
		return false
	}
	e.step, e.stage2 = 2, true
	for _, en := range p.Schedules.Entries {
		absent, err := stopEntry(e.x.Scheduler, e.x.Deps.goos(), en.Name)
		if err != nil {
			e.stop(stopEntryMessage(en.Name, err, e.res.Stopped, "click Delete again", "Nothing else was changed.", e.x.Deps.goos()))
			return false
		}
		if !absent {
			e.res.Stopped = append(e.res.Stopped, en.Name)
			e.done = append(e.done, "scheduled backup "+en.Name+" stopped")
		} else if en.listed() {
			// The plan listed it, and step 2 found it gone: the record says so (B1).
			e.res.AlreadyAbsent = append(e.res.AlreadyAbsent, en.Name)
		}
	}
	e.step = 3
	if f, err := removeNameFiles(p.Schedules.Names(), e.spellings(), e.x.SettingsDir); err != nil {
		e.stop(fmt.Sprintf("Could not remove %s in MailArchive's settings folder (%s). Click Delete again.", f, cleanErr(err)))
		return false
	}
	for _, en := range p.Schedules.RunNowBackups() {
		e.res.RunNowRemoved = append(e.res.RunNowRemoved, en.Name) // its job file went with the rest (B15)
	}
	e.done = append(e.done, "their settings files removed")
	if !e.proceed(4) {
		return false
	}
	e.step = 4
	ptr, err := e.x.Pointers.Load()
	if err != nil {
		e.stop(msgSettingsFailed)
		return false
	}
	np := ptr
	current := ptr.Out != "" && e.isThisFolder(ptr.Out)
	if current {
		np.Out = ""
	}
	if current || (ptr.BackupFor != "" && e.isThisFolder(ptr.BackupFor)) {
		np.BackupFor = ""
	}
	if ptr.LastArchive != "" && e.isThisFolder(ptr.LastArchive) {
		np.LastArchive = ""
	}
	if np != ptr {
		if err := e.x.Pointers.Save(np); err != nil {
			e.stop(msgSettingsFailed)
			return false
		}
		e.done = append(e.done, "settings updated")
	}
	return true
}

// spellings are the archive folder's spellings: the plan's, the row's and each
// entry's recorded -out.
func (e *run) spellings() []string {
	return append([]string{e.plan.Folder, e.row}, e.plan.Schedules.Spellings...)
}

// isThisFolder reports whether s names the archive folder in any of its spellings.
func (e *run) isThisFolder(s string) bool {
	for _, sp := range e.spellings() {
		if sp != "" && sameFolder(s, sp, e.x.Deps.goos()) {
			return true
		}
	}
	return false
}

// stopEntry is step 2's rule for one entry [R3:NEW-FINDING-1]: absent only when
// the query says not installed and, on Windows, schtasks says the task is missing
// or neither side-file is there (an absent entry still has its side-files
// removed); any other entry goes to RemoveLiveEntry.
func stopEntry(s Scheduler, goos, name string) (absent bool, err error) {
	if s.Query(name) == schedule.NotInstalled {
		if goos != "windows" {
			return true, nil
		}
		if s.SaysMissing(name) || !s.SideFilesExist(name) {
			return true, s.RemoveSideFiles(name)
		}
	}
	return false, s.RemoveLiveEntry(name)
}

// stopEntryMessage is step 2's (and Remove from list's) stop. It ends with the way on (the
// 1.5.3 friction review's H1): the scheduler made to work for this account, or the entry removed
// by hand with goos's own tools — once it is gone, step 2 counts it absent and goes on.
func stopEntryMessage(name string, err error, stopped []string, again, tail, goos string) string {
	msg := fmt.Sprintf("Could not stop the scheduled backup %s: %s.", name, cleanErr(err))
	if len(stopped) > 0 {
		msg += fmt.Sprintf(" Already stopped: %s (set them up again in Settings, or %s).", strings.Join(stopped, ", "), again)
	}
	return msg + " " + tail + fmt.Sprintf(" To go on, make sure this computer's scheduler (%s) works for your account, or remove that scheduled backup by hand (%s), then %s.",
		schedule.SchedulerName(goos), schedule.RemoveByHand(name, goos), again)
}

// cleanErr is an error's text for a result line: control characters stripped, no
// closing full stop (nor a colon left by an error with nothing after it).
func cleanErr(err error) string {
	return strings.TrimRight(strings.TrimSpace(util.StripControl(err.Error())), ".: ")
}

// scratchRemove is step 5: the scratch folders listed again under the lock and
// removed, only those folders.
func (e *run) scratchRemove() bool {
	e.step = 5
	dirs, err := e.scratchDirs()
	if err != nil {
		e.stop(step5Message("(" + cleanErr(err) + ")"))
		return false
	}
	for _, d := range dirs {
		targets, err := scratchTargets(d)
		if err != nil {
			e.stop(step5Message(d))
			return false
		}
		for _, t := range targets {
			if err := os.Remove(t); err != nil && !errors.Is(err, fs.ErrNotExist) {
				e.stop(step5Message(d))
				return false
			}
		}
	}
	return true
}

func step5Message(folder string) string {
	return "MailArchive's working folder for this archive is in use or holds files it did not put there: " + folder +
		". Steps done: schedules stopped, their settings files removed, settings updated, archive locked. Close MailArchive serve, search or other windows, then click Delete again."
}

// removeFiles is step 6: the plan's names through an os.Root on the very folder
// planned, the late state files and the manifest or vault last, and only when every
// other name the archive recorded is gone. ok is false when a recorded name is
// left; stopped when the step stopped the delete itself.
func (e *run) removeFiles() (ok, stopped bool) {
	e.step = 6
	p := e.plan
	root, err := os.OpenRoot(p.Folder)
	if err != nil {
		e.stop(fmt.Sprintf("Not fully deleted: the archive's folder could not be opened (%s). Reconnect the drive, then click Delete again.", cleanErr(err)))
		return false, true
	}
	e.root = root
	if id, err := rootIdentity(root); err != nil || id != p.Identity {
		e.stop(msgChanged)
		return false, true
	}
	goos := e.x.Deps.goos()
	late := map[string]bool{}
	for _, n := range lateStateFiles {
		late[fold(goos, n)] = true
	}
	isLast := func(it Item) bool { return it.Kind == Anchor || late[fold(goos, it.Rel)] }
	res := e.res
	for _, it := range p.Remove {
		if isLast(it) {
			continue
		}
		if err := e.removeOne(it.Rel, true); err != nil {
			res.NotRemoved = append(res.NotRemoved, leftFile(it.Rel, err, p.Folder))
			continue
		}
		res.Removed++
		res.Bytes += it.Size
	}
	if e.unrecorded {
		for _, it := range p.Unrecorded {
			if err := e.removeOne(it.Rel, true); err != nil {
				res.TickedLeft = append(res.TickedLeft, leftFile(it.Rel, err, p.Folder))
				continue
			}
			res.Removed++
			res.Bytes += it.Size
		}
	}
	litter := p.Litter
	if e.unrecorded {
		litter = append(append([]string(nil), litter...), p.UnrecordedLitter...)
	}
	for _, rel := range litter {
		if e.removeOne(rel, false) == nil {
			res.LitterRemoved++
		}
	}
	if len(res.NotRemoved) == 0 {
		for _, it := range p.Remove {
			if !isLast(it) {
				continue
			}
			if err := e.removeOne(it.Rel, true); err != nil {
				// The rest is kept with it, so the archive is still recognised and Delete
				// can be run again (§4.1 "the manifest last").
				res.NotRemoved = append(res.NotRemoved, leftFile(it.Rel, err, p.Folder))
				break
			}
			res.Removed++
			res.Bytes += it.Size
		}
	}
	for _, d := range p.Folders {
		e.removeDir(d)
	}
	res.Kept = append(res.Kept, p.Kept...)
	if !e.unrecorded {
		res.Kept = append(append(res.Kept, itemRels(p.Unrecorded)...), p.UnrecordedLitter...)
	}
	e.done = append(e.done, fmt.Sprintf("%d files removed", res.Removed))
	return len(res.NotRemoved) == 0, false
}

func leftFile(rel string, err error, folder string) LeftFile {
	c := causeOf(err)
	if _, ferr := os.Lstat(folder); ferr != nil {
		c = CauseDeviceGone
	}
	return LeftFile{Rel: rel, Cause: c, Err: cleanErr(err)}
}

// removeOne removes one name the plan lists, after checking that no element of its
// path is a link now. A name already gone counts as removed. On Windows alone, a
// file the delete may change (mayClear: a name the archive recorded, or a ticked
// copy the plan lists) that is read-only has that mark cleared under §9's rules and
// is tried again.
func (e *run) removeOne(rel string, mayClear bool) error {
	gone, err := e.noLinkOnPath(rel)
	if gone {
		return nil
	}
	if err != nil {
		return err
	}
	native := filepath.FromSlash(rel)
	if err := readOnlyBlocked(e.root, native, mayClear); err != nil {
		return err
	}
	err = removeFileFn(e.root, native)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if mayClear && runtime.GOOS == "windows" && isAccessDenied(err) {
		if clearReadOnly(e.root, native) == nil {
			err = removeFileFn(e.root, native)
			if err == nil || errors.Is(err, fs.ErrNotExist) {
				return nil
			}
		}
	}
	return err
}

// errLinkNow is a link found on a name's path at removal.
var errLinkNow = errors.New("is a link now")

// noLinkOnPath checks each element of rel with Lstat and, on Windows, its reparse
// tag; gone is true when an element is missing.
func (e *run) noLinkOnPath(rel string) (gone bool, err error) {
	l := &lister{root: e.root, folder: e.plan.Folder, goos: runtime.GOOS}
	parts := strings.Split(rel, "/")
	for i := 1; i <= len(parts); i++ {
		sub := strings.Join(parts[:i], "/")
		fi, err := e.root.Lstat(filepath.FromSlash(sub))
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if link, lerr := l.linkLike(sub, fi); lerr != nil || link {
			return false, fmt.Errorf("%s %w", sub, errLinkNow)
		}
	}
	return false, nil
}

// removeDir removes a folder the plan lists if it is empty, retrying one that is
// briefly busy; one that holds files is kept.
func (e *run) removeDir(rel string) {
	for i := 0; i < 5; i++ {
		gone, err := e.noLinkOnPath(rel)
		if gone || err != nil {
			return
		}
		err = removeFileFn(e.root, filepath.FromSlash(rel))
		if err == nil || errors.Is(err, fs.ErrNotExist) || isNotEmpty(err) {
			return
		}
		time.Sleep(retryPause / 2)
	}
}

// release is step 7's first part: the archive lock released (its file stays,
// lockfile.go Release).
func (e *run) release() {
	if e.root != nil {
		e.root.Close()
		e.root = nil
	}
	if e.lock != nil {
		e.lock.Release()
		e.lock = nil
	}
}

// tidyFolder is the rest of step 7, and step 8's retry: .mailarchive.lock removed
// by its exact name (retried: Windows cannot delete a file just released or held),
// then the folder removed when it is empty but for litter. gone: the folder is
// gone; lockLeft: the lock file is still there; emptyLeft: the folder is kept and
// empty or holds only litter.
func (e *run) tidyFolder() (gone, lockLeft, emptyLeft bool) {
	p := e.plan
	fi, err := os.Lstat(p.Folder)
	if errors.Is(err, fs.ErrNotExist) {
		return true, false, false
	}
	if err != nil || !fi.IsDir() {
		return false, false, false
	}
	root, err := os.OpenRoot(p.Folder)
	if err != nil {
		return false, false, false
	}
	if id, err := rootIdentity(root); err != nil || id != p.Identity {
		root.Close()
		return false, false, false
	}
	lockLeft = !removeLockIn(root, p.Folder)
	ents, err := util.ReadDirIn(root, ".")
	if err != nil {
		root.Close()
		return false, lockLeft, false
	}
	goos := e.x.Deps.goos()
	own := map[string]bool{}
	if p.Kind != Encrypted {
		for _, n := range folderOwnNames {
			own[fold(goos, n)] = true
		}
	}
	var litter []string
	others := 0
	e.leftIn = nil
	for _, en := range ents {
		n := en.Name()
		switch {
		case eqName(goos, n, lockName):
		case util.IsLitter(n) && en.Type().IsRegular() && !own[fold(goos, n)]:
			litter = append(litter, n)
		default:
			others++
			e.leftIn = append(e.leftIn, n)
		}
	}
	if others > 0 || lockLeft {
		root.Close()
		return false, lockLeft, false
	}
	for _, n := range litter {
		// Litter the delete may not change: a read-only one is kept, never handed to
		// Go's removal, which would clear its mark (§9, conformance 2).
		if readOnlyBlocked(root, n, false) == nil && removeFileFn(root, n) == nil {
			e.res.LitterRemoved++
		}
	}
	root.Close()
	for i := 0; i < 10; i++ {
		err := os.Remove(p.Folder)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return true, false, false
		}
		if isNotEmpty(err) {
			break
		}
		time.Sleep(retryPause)
	}
	return false, false, true
}

// removeLockIn removes .mailarchive.lock by its exact name, a regular file only,
// never one a run holds (removeLockFile), retrying briefly; true when it is gone.
func removeLockIn(root *os.Root, folder string) bool {
	for i := 0; i < 10; i++ {
		fi, err := root.Lstat(lockName)
		if errors.Is(err, fs.ErrNotExist) {
			return true
		}
		if err != nil || !fi.Mode().IsRegular() {
			return false
		}
		err = removeLockFile(root, folder)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return true
		}
		if errors.Is(err, lockfile.ErrHeld) {
			return false // a run holds it: it is the run's, for as long as it runs
		}
		time.Sleep(retryPause)
	}
	return false
}

// afterRelease runs right after step 7 releases the archive lock: a test seam (a run
// taking the lock in that window), nothing in a real run.
var afterRelease = func(string) {}

// logFolder appends step 7's (or step 8's) durable folder line.
func (e *run) logFolder(gone bool) {
	ev := evFolderKept
	if gone {
		ev = evFolderRemoved
	}
	if err := appendLog(e.log, logLine{At: e.stamp(), Event: ev, Run: e.id, Folder: e.plan.Folder, Row: e.row}); err != nil {
		e.notes = append(e.notes, fmt.Sprintf("This step could not be added to deletions.log (%s).", cleanErr(err)))
	}
}

// stopAt6 is §8 (a)'s result: a name the archive recorded could not be removed;
// the manifest or vault is kept, so Delete can be run again.
func (e *run) stopAt6() {
	res := e.res
	e.outcome = outPartial
	res.Status, res.Unfinished = StatusPartial, true
	res.Lines = []string{leftLine("Not fully deleted. %s could not be removed: %s.", res.NotRemoved) + " " + remedies(res.NotRemoved)}
	if len(res.TickedLeft) > 0 {
		res.Lines = append(res.Lines, tickedLine(res.TickedLeft))
	}
	if e.marked {
		res.Lines = append(res.Lines, lockedLine)
	}
	e.scheduleResultLines()
	res.line("What Delete does not reach: %s", DoesNotReach)
}

// stopNoAnchor is §5.4's partial for a folder that holds no manifest or vault: never
// complete. Its words say what is left and how to go on (del-s1b-A2-08): the folder
// gone (Delete again finishes), files or folders the delete can no longer identify
// (§5.4's words), or only the lock file or the empty folder that could not be
// removed (Delete again retries; deleting the folder by hand would leave the log
// without its `folder removed` line).
func (e *run) stopNoAnchor(gone, lockLeft bool) {
	e.outcome = outPartial
	e.res.Status, e.res.Unfinished = StatusPartial, true
	files, folders := countLeft(e.plan.Folder)
	var line string
	switch {
	case gone:
		e.detail = "the folder is gone"
		line = "Not fully deleted: the files the delete could still name were removed, and the folder is gone. Click Delete again to finish."
	case files > 0 || folders > 0:
		what := plural(files, "file", "files")
		if files == 0 {
			what = plural(folders, "folder", "folders")
		}
		e.detail = fmt.Sprintf("the folder still holds %s the delete can no longer identify", what)
		line = "Not fully deleted: " + e.detail + ". Delete them by hand, or use Remove from list."
	case lockLeft:
		e.detail = "the lock file could not be removed"
		line = "Not fully deleted: one MailArchive lock file (.mailarchive.lock) could not be removed. Click Delete again; if it stays, delete that file by hand (not the folder), then click Delete again."
	default:
		e.detail = "the empty folder could not be removed"
		line = "Not fully deleted: the empty folder could not be removed. Click Delete again."
	}
	e.res.Lines = []string{line}
	if e.marked {
		e.res.Lines = append(e.res.Lines, lockedLine)
	}
	e.scheduleResultLines()
}

// countLeft counts the files and the folders under folder, links not followed,
// litter and the root lock file left out.
func countLeft(folder string) (files, folders int) {
	filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil || p == folder:
		case d.IsDir():
			folders++
		case util.IsLitter(d.Name()) || (filepath.Dir(p) == folder && d.Name() == lockName):
		default:
			files++
		}
		return nil
	})
	return files, folders
}

// busyMessage is the busy refusal for folder: msgDeleting when its lock's holder line names a
// delete, else msgBusy.
func busyMessage(folder string) string {
	if lockfile.HolderVerb(filepath.Join(folder, lockName)) == "delete" {
		return msgDeleting
	}
	return msgBusy
}

// proceed asks BeforeStep before step: an error stops the delete there; before step 2,
// when nothing has changed yet, it refuses instead.
func (e *run) proceed(step int) bool {
	if e.x.BeforeStep == nil {
		return true
	}
	err := e.x.BeforeStep(step)
	if err == nil {
		return true
	}
	if !e.stage2 {
		// Before step 2 nothing has changed: a refusal under the lock, its finished line
		// written by the cleanup, so the row is never marked "Delete not finished"
		// (del-s4-A2-01).
		e.refuseUnder(&Refusal{Case: CaseStopped, Message: cleanErr(err) + "."})
		return false
	}
	e.step = step
	done := "none"
	if len(e.done) > 0 {
		done = strings.Join(e.done, ", ")
	}
	e.stop(fmt.Sprintf("%s. The delete stopped before step %d. Steps done: %s. Click Delete again.", cleanErr(err), step, done))
	return false
}

// keyAndRow is steps 8 and 9; false when the delete stopped before one of them.
func (e *run) keyAndRow() bool {
	if !e.proceed(8) {
		return false
	}
	e.step = 8
	doneOrNA := e.keyring()
	if doneOrNA && !e.proceed(9) {
		return false
	}
	e.step = 9
	if !doneOrNA {
		return true
	}
	forget := e.x.ForgetRow
	if forget == nil {
		forget = func(row string) error { return forgetRowIn(e.x.SettingsDir, row, e.x.Deps.goos()) }
	}
	if err := forget(e.row); err != nil {
		if errors.Is(err, ErrRowNotListed) {
			e.notes = append(e.notes, msgRowNotListed)
		} else {
			e.notes = append(e.notes, msgRowFailed)
		}
		return true
	}
	e.res.RowRemoved = true
	e.done = append(e.done, "removed from Your archives")
	return true
}

// keyring is step 8; it reports whether step 8 is done or does not apply (so step 9
// may remove the row).
func (e *run) keyring() bool {
	p := e.plan
	if p.Kind != Encrypted {
		return true
	}
	if p.KeyForgottenBefore {
		// The delete this re-run finishes forgot the record already (its `key
		// forgotten` line): step 8 is done, and asking the keyring again would find
		// nothing and wrongly say it still remembers the key (del-s1b-A2-01).
		e.res.KeyForgotten = "forgotten by the delete this one finished"
		e.notes = append(e.notes, "This computer had already forgotten the archive's record in the delete this one finished")
		return true
	}
	if p.RecordUnknown {
		return e.skipKey()
	}
	if !p.Record.Recorded {
		return true // never recorded: no record is touched
	}
	if !e.goneOrOnlyLock() {
		return e.skipKey()
	}
	kp, err := e.x.Keyring.PlanForget(p.Record.Spelling)
	// The record found must be the archive's own: the ID the plan (or the original
	// started line) recorded, never another archive recorded since at that spelling
	// (del-s1b-A1-05).
	if err != nil || !kp.Gone || kp.ID != p.Record.ID {
		return e.skipKey()
	}
	ferr := e.x.Keyring.Forget(kp, kp.Last)
	if ferr == nil {
		// Durable at once, so a re-run of a delete stopped after this point knows step 8
		// is done (del-s1b-A2-01).
		if err := appendLog(e.log, logLine{At: e.stamp(), Event: evKeyForgotten, Run: e.id, Folder: p.Folder, Row: e.row, RecordID: p.Record.ID}); err != nil {
			e.notes = append(e.notes, fmt.Sprintf("This step could not be added to deletions.log (%s).", cleanErr(err)))
		}
	}
	if !e.res.FolderGone {
		// Forget took the archive's lock, re-creating .mailarchive.lock [R4:NF-NF1-LOCK]:
		// removed again by its exact name, and the folder retried.
		gone, lockLeft, emptyLeft := e.tidyFolder()
		if gone {
			e.res.FolderGone, e.res.LockFileLeft, e.emptyLeft = true, false, false
			e.logFolder(true)
		} else {
			e.res.LockFileLeft, e.emptyLeft = lockLeft, emptyLeft
		}
	}
	if ferr != nil {
		return e.skipKey()
	}
	if kp.Last {
		e.res.KeyForgotten = "forgotten"
		e.notes = append(e.notes, "This computer forgot the archive's key")
	} else {
		e.res.KeyForgotten = fmt.Sprintf("this copy forgotten; the key is kept for %s", plural(len(kp.Others), "other recorded copy", "other recorded copies"))
		e.notes = append(e.notes, "This computer forgot this copy of the archive and keeps its key for the other recorded copies: "+strings.Join(kp.Others, "; "))
	}
	e.done = append(e.done, "this computer's record forgotten")
	return true
}

func (e *run) skipKey() bool {
	e.res.KeySkipped = true
	e.notes = append(e.notes, msgKeySkipped)
	return false
}

// goneOrOnlyLock reports the folder gone, or holding only an unheld
// .mailarchive.lock and litter (encmark's case Gone).
func (e *run) goneOrOnlyLock() bool {
	ents, err := os.ReadDir(e.plan.Folder)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	for _, en := range ents {
		switch {
		case en.Name() == lockName && en.Type().IsRegular() && !lockfile.Held(filepath.Join(e.plan.Folder, lockName)):
		case util.IsLitter(en.Name()) && en.Type().IsRegular():
		default:
			return false
		}
	}
	return true
}

// finish is the result of a delete that reached step 9 (§8): complete, or not
// fully deleted when only §4.3 files the person ticked are left.
func (e *run) finish() {
	res, p := e.res, e.plan
	e.outcome = outComplete
	res.Status = StatusComplete
	var head string
	switch {
	case len(res.TickedLeft) > 0:
		e.outcome, res.Status = outPartial, StatusPartial
		head = tickedLine(res.TickedLeft)
	case p.Rerun == RerunFolderGone:
		head = "Deleted. The folder is gone: an earlier delete removed it, and this one finished what it left."
	default:
		head = fmt.Sprintf("Deleted. %s (%s) removed. ", plural(res.Removed, "file", "files"), size(res.Bytes))
		switch {
		case res.FolderGone:
			head += "The folder is gone."
		case len(res.Kept) > 0:
			head += fmt.Sprintf("The folder is kept: it holds %s that are not the archive's.", plural(len(res.Kept), "file", "files"))
		case len(e.leftIn) > 0:
			// What kept it, named (conformance 4): a folder of the archive's that stayed
			// busy past its retries, or something put there during the delete.
			head += fmt.Sprintf("The folder is kept: it still holds %s that could not be removed: %s.", plural(len(e.leftIn), "item", "items"), namesUpTo(e.leftIn))
		default:
			head += "The folder is kept."
		}
		if n := len(p.Unrecorded); n > 0 && !e.unrecorded {
			if n == 1 {
				head += " 1 mail copy the archive did not record was kept."
			} else {
				head += fmt.Sprintf(" %d mail copies the archive did not record were kept.", n)
			}
		}
	}
	res.Lines = []string{head}
	res.CheckYourself = p.Schedules.notRemoved()
	if res.LockFileLeft && !res.FolderGone {
		res.Lines = append(res.Lines, msgLockFileLeft)
	} else if e.emptyLeft && !res.FolderGone {
		res.Lines = append(res.Lines, msgEmptyFolderLeft)
		for _, n := range res.CheckYourself {
			res.line("Not removed: check it yourself: %s", n)
		}
	}
	e.scheduleResultLines()
	res.Lines = append(res.Lines, e.notes...)
	if res.RowRemoved {
		res.Lines = append(res.Lines, "Removed from Your archives")
	}
	res.line("What Delete does not reach: %s", DoesNotReach)
}

// namesUpTo joins up to ten names, then "and N more".
func namesUpTo(names []string) string {
	if len(names) > recordListMax {
		return strings.Join(names[:recordListMax], ", ") + fmt.Sprintf(", and %d more", len(names)-recordListMax)
	}
	return strings.Join(names, ", ")
}

// leftLine words a list of leftovers, up to ten names.
func leftLine(format string, left []LeftFile) string {
	names := make([]string, 0, len(left))
	for i, l := range left {
		if i == recordListMax {
			names = append(names, fmt.Sprintf("and %d more", len(left)-recordListMax))
			break
		}
		names = append(names, l.Rel)
	}
	return fmt.Sprintf(format, plural(len(left), "file", "files"), strings.Join(names, ", "))
}

// tickedLine is §4.3's result for ticked files that could not be removed
// [R3:NEW-ticked-remedy]: never "click Delete again".
func tickedLine(left []LeftFile) string {
	return leftLine("Not fully deleted. %s you chose to delete could not be removed: %s.", left) +
		" MailArchive no longer recognises this folder as an archive: delete them by hand."
}

// remedies are §8 (a)'s remedies for the causes present, in a fixed order.
func remedies(left []LeftFile) string {
	have := map[Cause]bool{}
	for _, l := range left {
		have[l.Cause] = true
	}
	var out []string
	for _, c := range []Cause{CauseInUse, CauseDenied, CauseDeviceGone, CauseOther} {
		if have[c] {
			out = append(out, c.remedy())
		}
	}
	return strings.Join(out, " ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// cleanup always runs once step 0 is written: a recovered panic is reported with
// the steps done, the archive lock released, the being-deleted mark cleared, and
// step 10 written.
func (e *run) cleanup() {
	if r := recover(); r != nil {
		e.panicked(r)
	}
	safely(e.release)
	if e.marked {
		e.marked = false
		safely(func() { e.x.Keys.EndDelete(e.plan.Folder) })
	}
	// The steps done, as "Steps done" words them (conformance 5).
	e.res.Steps = append([]string(nil), e.done...)
	safely(e.step10)
}

func safely(f func()) {
	defer func() { _ = recover() }()
	f()
}

// panicked is the result of a recovered panic [C18]: the steps done, never
// "nothing was changed" after step 0.
func (e *run) panicked(r any) {
	done := "none"
	if len(e.done) > 0 {
		done = strings.Join(e.done, ", ")
	}
	msg := fmt.Sprintf("The delete stopped unexpectedly at step %d (%s). Steps done: %s. Click Delete again.", e.step, util.StripControl(fmt.Sprint(r)), done)
	if e.stage2 {
		e.outcome = outPartial
		e.res.Status, e.res.Unfinished, e.res.Refusal = StatusPartial, true, nil
	} else {
		e.outcome = outRefused
		e.res.Status, e.res.Refusal = StatusRefusedAfterStart, &Refusal{Case: CaseInternal, Message: msg}
	}
	e.res.Lines = []string{msg}
	if e.marked {
		e.res.Lines = append(e.res.Lines, lockedLine)
	}
}

// step10 builds the deletion record once, then logs the finished line with its
// digest [O2-03].
func (e *run) step10() {
	if !e.logged {
		return
	}
	res, p := e.res, e.plan
	status := map[string]string{outComplete: StatusComplete, outPartial: StatusPartial, outRefused: StatusRefusedAfterStart}[e.outcome]
	if status == "" {
		status, e.outcome = StatusPartial, outPartial
	}
	key := res.KeyForgotten
	if res.KeySkipped {
		key = "kept: this computer still remembers its key"
	}
	var left []string
	for _, l := range append(append([]LeftFile(nil), res.NotRemoved...), res.TickedLeft...) {
		left = append(left, l.Rel)
	}
	computer, account := e.x.Computer, e.x.Account
	if computer == "" {
		computer, _ = os.Hostname()
	}
	if account == "" {
		if u, err := user.Current(); err == nil {
			account = u.Username
		}
	}
	rec := RecordText(RecordInput{
		Status: status, When: e.now(), Computer: computer, Account: account, Version: e.x.Version, Plan: p,
		FilesRemoved: res.Removed, BytesRemoved: res.Bytes, SchedulesStopped: res.Stopped,
		SchedulesAbsent: res.AlreadyAbsent, RunNowRemoved: res.RunNowRemoved,
		SchedulesNotRemoved: p.Schedules.notRemoved(), KeyForgotten: key, UnrecordedTicked: e.unrecorded,
		NotRemoved: left, KeptFiles: res.Kept, LitterRemoved: res.LitterRemoved,
	})
	res.Record = rec
	l := logLine{At: e.stamp(), Event: evFinished, Run: e.id, Folder: p.Folder, Row: e.row, Outcome: e.outcome, Unfinished: res.Unfinished,
		Left: intp(len(left)), Kept: intp(len(res.Kept)), Detail: e.detail, RecordSHA256: recordSHA(rec)}
	e.sayFinished() // before the line: once the log says finished, the delete has said it is over
	if err := appendLog(e.log, l); err != nil {
		res.Lines = append(res.Lines, msgDigestFailed)
		return
	}
	res.Lines = append(res.Lines, "Recorded in deletions.log")
}

// sayFinished calls ExecDeps.Finished, once: right before step 10 writes the finished line, so
// a signal once deletions.log says the delete finished is never a stop mid-delete (the residual
// fixes' adversarial pass, finding 5); else as Execute returns.
func (e *run) sayFinished() {
	if !e.finished && e.x.Finished != nil {
		e.finished = true
		e.x.Finished()
	}
}

// recordSHA is the hex on a record's last line.
func recordSHA(rec []byte) string {
	i := bytes.LastIndex(bytes.TrimRight(rec, "\n"), []byte("\n"))
	return strings.TrimSpace(strings.TrimPrefix(string(rec[i+1:]), "Record-SHA256: "))
}
