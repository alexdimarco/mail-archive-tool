package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/progcache"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The program step (program design §4, R1–R15; PG3): at each keyed backup, after the
// run's last save and under the archive's lock, the archive's program folder is
// brought to what the person chose — release copies of MailArchive from the verified
// cache, this computer's own program when no copy there opens the archive — through
// one staged protocol: stage, render the note, an intent commit, renames and
// removals, a landed commit. It never fails the backup: its faults are warnings.

// ProgramOutcome is what the program step did (R1): its class and its warnings.
type ProgramOutcome struct {
	Class    string
	Warnings []string
	// Summary is the one line the backup prints when the step wrote or removed copies
	// — tens of megabytes on the archive's drive — for a front end to show with the
	// run's result too; empty when it changed nothing (P3-8 friction, B23).
	Summary string
}

// The program step's outcome classes.
const (
	ProgramNotChosen    = "not chosen"
	ProgramUnchanged    = "unchanged"
	ProgramUpdated      = "updated"
	ProgramIncomplete   = "incomplete"
	ProgramSkipped      = "skipped"
	ProgramKeptAsIs     = "kept as it is"
	ProgramStopped      = "stopped"
	ProgramCancelled    = "cancelled"     // Ctrl-C or Cancel: nothing new written (R17)
	ProgramUnchecked    = "unchecked"     // a copy kept online only, unchecked since an earlier run (R4)
	ProgramChangedFound = "changed found" // a kept copy had been changed: removed or replaced, and told
	// With copies switched off:
	ProgramRemoved    = "removed"     // the copies were removed
	ProgramNotKept    = "not kept"    // none kept, nothing to do
	ProgramNotRemoved = "not removed" // a copy could not be removed
)

// programIdentity is this program's version and whether it is a release build — an
// exact vMAJOR.MINOR.PATCH from a clean checkout (C12); a var so a test can be any
// version (R22).
var programIdentity = func() (string, bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	modified := true
	for _, s := range bi.Settings {
		if s.Key == "vcs.modified" {
			modified = s.Value != "false"
		}
	}
	return bi.Main.Version, container.ExactVersion(bi.Main.Version) && !modified
}

// ReleaseBuild reports whether this program is a release build, by the one test the
// program copies use (programIdentity, C12): MailArchive Desktop words its copies
// checkbox by it instead of keeping a copy of the check.
func ReleaseBuild() bool {
	_, release := programIdentity()
	return release
}

// programWaitMax bounds the program step's wait for its download (§5, R20).
var programWaitMax = 10 * time.Minute

// programFetcher makes the download's fetcher; a var so a test serves its own release.
var programFetcher = func(c *progcache.Cache, this string) *progcache.Fetcher {
	return &progcache.Fetcher{Cache: c, This: this, Overall: programDownloadMax}
}

// programDownloadMax is a download's overall limit (R17): a server that sends a byte
// inside every idle window cannot keep it running for the life of the process (NET-5).
const programDownloadMax = 60 * time.Minute

// programPlaceholder reports an online-only placeholder; a var so a test can inject one
// (R4).
var programPlaceholder = util.IsPlaceholder

// programFault injects a fault at a named point of the step (tests only).
var programFault func(point string) error

func programAt(point string) error {
	if programFault != nil {
		return programFault(point)
	}
	return nil
}

// programDownload is the download a keyed run started at its beginning (§5, R17).
type programDownload struct {
	task    *progcache.Task
	fetcher *progcache.Fetcher
	version string
}

// downloadContext is the context the run's program download runs under.
func (o Options) downloadContext(run context.Context) context.Context {
	if o.ProgramDownload != nil {
		return o.ProgramDownload
	}
	return run
}

// startProgramDownload starts, outside any lock, the download the program step will
// want: a release build's own version, or the newer one an archive here expects, that
// the cache lacks — only when keep is on and the once-a-day rule allows (§5, R17).
func startProgramDownload(ctx context.Context, store *container.Store) *programDownload {
	ps, st, _ := store.Program()
	if st != container.ProgramOK || ps.Keep == nil || !*ps.Keep {
		return nil
	}
	this, release := programIdentity()
	if !release {
		return nil
	}
	c, err := progcache.Open("")
	if err != nil {
		return nil
	}
	if container.CompareVersions(ps.Version, this) > 0 {
		c.SetExpects(ps.Version)
	}
	// The newer version an archive here expects first, then this program's own: one
	// that cannot be had never keeps the other from being fetched (NET-7).
	f := programFetcher(c, this)
	versions := []string{this}
	if e := c.Expects(); container.CompareVersions(e, this) > 0 {
		versions = []string{e, this}
	}
	for _, want := range versions {
		if len(c.Verified(want)) == len(progcache.Assets) {
			return nil // the best version wanted is here already
		}
		if ok, _ := f.ShouldTry(want); ok {
			return &programDownload{task: progcache.Downloads.Start(ctx, f, want), fetcher: f, version: want}
		}
	}
	return nil
}

// programEnv is what the step works with.
type programEnv struct {
	ctx       context.Context
	out       string
	store     *container.Store
	save      func() error // a plain manifest save: commits the sealed record
	stillHeld func() error
	logger    *log.Logger
	runStart  time.Time
	download  *programDownload
	skip      <-chan struct{} // closed: the person skipped the wait (R20)
	onWait    func(bool)      // told when the wait starts and ends
}

type programStepState struct {
	env    programEnv
	o      ProgramOutcome
	failed bool // a planned file could not be written: the outcome is incomplete
	// What the step put in place and removed, for its one line (B23).
	wrote, removed           int
	wroteBytes, removedBytes int64
}

// summary is the step's one line when it wrote or removed copies (P3-8 friction,
// B23): how many, and how much of the archive's drive they take or gave back.
func (s *programStepState) summary(keep bool) string {
	switch {
	case s.wrote > 0 && s.removed > 0:
		return fmt.Sprintf("Copies of MailArchive: wrote %d file(s) (%s) and removed %d (%s) in the archive's program folder; README.txt lists what is kept", s.wrote, humanBytes(s.wroteBytes), s.removed, humanBytes(s.removedBytes))
	case s.wrote > 0:
		return fmt.Sprintf("Copies of MailArchive: wrote %d file(s) (%s) into the archive's program folder, so the archive can be opened years from now; README.txt lists them", s.wrote, humanBytes(s.wroteBytes))
	case s.removed > 0 && !keep:
		return fmt.Sprintf("Copies of MailArchive: removed %d file(s) (%s) from the archive's program folder, as you chose", s.removed, humanBytes(s.removedBytes))
	case s.removed > 0:
		return fmt.Sprintf("Copies of MailArchive: removed %d file(s) (%s) from the archive's program folder", s.removed, humanBytes(s.removedBytes))
	}
	return ""
}

func (s *programStepState) warn(format string, a ...any) {
	m := fmt.Sprintf(format, a...)
	s.o.Warnings = append(s.o.Warnings, m)
	s.env.logger.Printf("warning: program copies: %s", m)
}

// programStep runs the step. Its caller runs it only for an encrypted archive whose
// lock was not lost, with no fatal fault, a successful last save and no cancel (R1).
func programStep(env programEnv) ProgramOutcome {
	s := &programStepState{env: env}
	set, st, err := env.store.Program()
	switch {
	case st == container.ProgramUnknown || st == container.ProgramInvalid:
		s.warn("the record's list of program copies is one this MailArchive cannot read; the program folder was left as it is (%v)", err)
		s.o.Class = ProgramKeptAsIs
		return s.o
	case st == container.ProgramAbsent || set.Keep == nil:
		s.o.Class = ProgramNotChosen
		return s.o
	}
	if selfInside(env.out) {
		s.warn("this MailArchive runs from the archive's own program folder; the copies were not refreshed — run MailArchive from its installed place")
		s.o.Class = ProgramSkipped
		return s.o
	}
	s.o.Class = s.run(set)
	if s.o.Summary = s.summary(*set.Keep); s.o.Summary != "" {
		env.logger.Printf("%s", s.o.Summary)
	}
	if !*set.Keep {
		// Copies switched off: the step only removes, and status says so — never that
		// copies are kept (the PG4 pass, LEAK-1, COR-5).
		switch s.o.Class {
		case ProgramUpdated:
			s.o.Class = ProgramRemoved
		case ProgramUnchanged:
			s.o.Class = ProgramNotKept
		case ProgramIncomplete:
			s.o.Class = ProgramNotRemoved
		}
	}
	return s.o
}

// selfInside says the running program lies inside out's program folder (C15).
func selfInside(out string) bool {
	exe, _ := selfExecutable()
	if exe == "" {
		return false
	}
	exe = util.ResolveExisting(exe)
	return util.Within(util.ResolveExisting(filepath.Join(out, container.ProgramDir)), exe)
}

// programSweepSkip is the folder a keyed run's crash sweep leaves alone: the archive's
// program folder, whose step sweeps its own temporary files (R11).
func programSweepSkip(opts Options) []string {
	if opts.Key == nil {
		return nil
	}
	return []string{filepath.Join(opts.Out, container.ProgramDir)}
}

// programRoot is the program folder held through an os.Root, confirmed to be the same
// folder its path names (R3, §4.1).
type programRoot struct {
	dir  string
	root *os.Root
	fi   os.FileInfo
}

func (r *programRoot) close() {
	if r != nil && r.root != nil {
		r.root.Close()
		r.root = nil // closed once: a later close is nothing
	}
}

// same says the program folder's path still names the folder held (R3).
func (r *programRoot) same() bool {
	fi, err := os.Lstat(r.dir)
	return err == nil && fi.IsDir() && os.SameFile(fi, r.fi)
}

// openProgramDir holds out's program folder: nil with no error when it is absent and
// create is false; an error when a link or anything but a real folder stands there.
func openProgramDir(dir string, create bool) (*programRoot, error) {
	if link, err := util.IsLinkLike(dir); err != nil || link {
		return nil, fmt.Errorf("the program folder is a link; it was not followed")
	}
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if !create {
			return nil, nil
		}
		if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("the program folder is %s, not a folder", util.ModeKind(fi.Mode()))
	}
	// Pinned: compared with the folder opened (B1). A pin that did not take would
	// read its identity later, from whatever is there then (WINPATH-1).
	fi, pinned := util.PinIdentity(fi)
	if !pinned {
		return nil, fmt.Errorf("the program folder changed while it was opened")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	hf, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	hfi, serr := hf.Stat()
	hf.Close()
	if serr != nil || !os.SameFile(hfi, fi) {
		root.Close()
		return nil, fmt.Errorf("the program folder changed while it was opened")
	}
	// The identity every later check compares is the open handle's own (R3): read from
	// the folder held, never from its path again.
	return &programRoot{dir: dir, root: root, fi: hfi}, nil
}

// entries lists the program folder through its handle.
func (r *programRoot) entries() []os.DirEntry {
	f, err := r.root.Open(".")
	if err != nil {
		return nil
	}
	defer f.Close()
	list, _ := f.ReadDir(-1)
	return list
}

// judgeOne judges a sealed file by its name (R3, R4): never read through a link,
// a placeholder never opened, a read error never taken as a change.
func (r *programRoot) judgeOne(f container.ProgramFile) (judged, string) {
	fi, err := r.root.Lstat(f.Name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return jAbsent, ""
	case err != nil:
		return jUnreadable, ""
	}
	// Pinned: the file judged, compared when it is opened (B1). A pin that did not
	// take would read its identity at the open, from whatever is there then: this run
	// cannot read it (WINPATH-1).
	fi, pinned := util.PinIdentity(fi)
	if !pinned {
		return jUnreadable, ""
	}
	path := filepath.Join(r.dir, f.Name)
	if programPlaceholder(path) {
		return jUnchecked, ""
	}
	if !fi.Mode().IsRegular() && !programCloudFile(path) {
		return jBlocked, ""
	}
	if fi.Size() != f.Size {
		return jChanged, ""
	}
	beforeOpen("same", path)
	if !r.same() {
		return jUnreadable, ""
	}
	beforeOpen("open", path)
	h, err := hashRegular(path, fi, f.Size)
	switch {
	case err != nil:
		return jUnreadable, ""
	case h != f.SHA256:
		return jChanged, h
	}
	return jOK, h
}

// hashRegular hashes the regular file at path, refusing one that is not the file
// judged (judgeFile's shape), reading at most size+1 bytes.
func hashRegular(path string, judgedFI os.FileInfo, size int64) (string, error) {
	file, err := util.OpenRegularFile(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if ofi, err := file.Stat(); err != nil || !os.SameFile(ofi, judgedFI) {
		return "", errors.New("changed while it was checked")
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(file, size+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// programCode is the step's outcome as the last-run record names it, from the closed
// list; "" when it did not run, the copies were never chosen, or none is kept and none
// was there to remove.
func programCode(class string) string {
	return map[string]string{
		ProgramRemoved: state.ProgramRemoved, ProgramNotRemoved: state.ProgramNotRemoved, ProgramUnchecked: state.ProgramUnchecked,
		ProgramChangedFound: state.ProgramChanged,
		ProgramUpdated:      state.ProgramUpdated, ProgramUnchanged: state.ProgramUnchanged, ProgramIncomplete: state.ProgramIncomplete,
		ProgramSkipped: state.ProgramSkipped, ProgramKeptAsIs: state.ProgramUnread, ProgramStopped: state.ProgramStopped,
		ProgramCancelled: state.ProgramCancelled,
	}[class]
}

// programTempName is a staging name: <n> a random 64-bit number (R11).
func programTempName() string {
	var b [8]byte
	rand.Read(b[:])
	return ".mailarchive-program-" + strconv.FormatUint(binary.BigEndian.Uint64(b[:]), 10) + ".tmp"
}

func isProgramTemp(name string) bool {
	return strings.HasPrefix(name, ".mailarchive-program-") && strings.HasSuffix(name, ".tmp")
}

// run is the step past its preconditions.
func (s *programStepState) run(set container.ProgramSet) string {
	env := s.env
	dir := filepath.Join(env.out, container.ProgramDir)
	pr, err := openProgramDir(dir, false)
	if err != nil {
		s.warn("%v; nothing in it was touched", err)
		return ProgramSkipped
	}
	defer pr.close()
	if set.State == container.ProgramIntent {
		set = s.resolveIntent(pr, set)
		// The note names what the finished update landed (DUR-04); its digest is sealed
		// only when it was written (DUR-03).
		note := []byte(EncryptedNoteFor(&set))
		if export.WriteFileAtomic(filepath.Join(env.out, "README.txt"), note) == nil {
			env.store.SetNoteDigest(NoteDigest(note))
		}
		if err := s.commit(set); err != nil {
			s.warn("the earlier update of the program copies could not be recorded as finished: %v", err)
			return ProgramStopped
		}
	}
	this, release := programIdentity()
	cache, cerr := progcache.Open("")
	if cerr != nil && *set.Keep && release {
		s.warn("the program cache cannot be used: %v", cerr)
	}
	if cache != nil {
		if container.CompareVersions(set.Version, this) > 0 {
			cache.SetExpects(set.Version)
		}
		defer cache.Prune(this)
	}
	if !s.awaitDownload() {
		s.warn("cancelled: nothing new was written to the program folder")
		return ProgramCancelled
	}

	state := map[string]judged{}
	if pr != nil {
		s.sweep(pr, set)
		for _, f := range set.Files {
			state[f.Name], _ = pr.judgeOne(f)
		}
	} else {
		for _, f := range set.Files {
			state[f.Name] = jAbsent
		}
	}
	v := programView{set: set, state: state, keep: *set.Keep, version: this, release: release,
		platform: container.ReleasePlatform(runtime.GOOS, runtime.GOARCH), now: time.Now().UTC().Format(time.RFC3339)}
	v.selfPlatform, v.selfName = selfPlatformAndName()
	if cache != nil && release {
		v.cacheVersion, v.cacheAssets, v.cacheSums = programCacheVersion(cache, this, set.Version)
	}
	plan := decide(v)
	for _, w := range plan.warnings {
		s.warn("%s", w)
	}
	for _, b := range plan.blocked {
		s.warn("a folder, a link or a special file stands where the copy for %s belongs; it was left as it is", systemName(b))
	}
	if len(plan.add) == 0 && len(plan.remove) == 0 {
		return s.recordKept(set, plan)
	}
	return s.write(pr, dir, set, plan, cache, v)
}

// sameFiles says two entry lists are the same.
func sameFiles(a, b []container.ProgramFile) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]container.ProgramFile{}
	for _, f := range a {
		m[f.Name] = f
	}
	for _, f := range b {
		if m[f.Name] != f {
			return false
		}
	}
	return true
}

// classOf is the outcome of a plan carried out.
func (s *programStepState) classOf(plan programPlan, wrote bool) string {
	if len(plan.changed) > 0 {
		return ProgramChangedFound // above all else: someone changed a copy (P3-8, B3)
	}
	for _, k := range plan.keep {
		if k.MissingRuns >= 2 {
			s.warn("the program copies are incomplete: the copy for %s has been missing for %d backups", systemName(k.Platform), k.MissingRuns)
			return ProgramIncomplete
		}
	}
	if len(plan.blocked) > 0 || s.failed {
		return ProgramIncomplete
	}
	// A copy kept online only and unchecked since an earlier run: the lasting line (R4;
	// the PG4 pass, COR-6).
	since := s.env.runStart.UTC().Format(time.RFC3339)
	for _, k := range plan.keep {
		if k.UncheckedSince != "" && k.UncheckedSince < since {
			return ProgramUnchecked
		}
	}
	if wrote {
		return ProgramUpdated
	}
	return ProgramUnchanged
}

// sweep removes the step's own temporary files older than the run's start (R11),
// renames a sealed file back to its sealed spelling when only its letter case changed
// (R12), and removes a file of the step's own pattern no sealed entry names — a folder,
// a link, anything else is reported, never deleted (§4.1). The system's litter is
// ignored (R13).
func (s *programStepState) sweep(pr *programRoot, set container.ProgramSet) {
	sealed := map[string]string{}
	for _, f := range set.Files {
		sealed[strings.ToLower(f.Name)] = f.Name
	}
	for _, e := range pr.entries() {
		name := e.Name()
		regular := e.Type().IsRegular() || programCloudFile(filepath.Join(pr.dir, name))
		switch {
		case util.IsLitter(name):
			continue
		case isProgramTemp(name):
			fi, err := e.Info()
			switch {
			case err == nil && regular && fi.ModTime().Before(s.env.runStart):
				pr.root.Remove(name)
			default:
				// Not one of the step's own: a folder or a link with its name, or a file
				// dated after this run began (SWAP-03).
				s.warn("something in the program folder that MailArchive did not put there was left as it is: %s", strconv.QuoteToASCII(name))
			}
			continue
		}
		want, ok := sealed[strings.ToLower(name)]
		if ok && want != name {
			nfi, e1 := pr.root.Lstat(name)
			wfi, e2 := pr.root.Lstat(want)
			switch {
			case errors.Is(e2, fs.ErrNotExist), e1 == nil && e2 == nil && os.SameFile(nfi, wfi):
				// Only its letter case changed — on a volume that ignores case the two
				// spellings are one file: renamed back to its sealed spelling (R12, CRIT-03).
				pr.root.Rename(name, want)
				continue
			}
			ok = false // a twin beside the sealed file: not MailArchive's (SWAP-02)
		}
		switch {
		case ok:
		case regular && container.ProgramNameOK(name):
			if err := pr.root.Remove(name); err == nil {
				s.warn("a file in the program folder that MailArchive did not put there was removed: %s", name)
			}
		default:
			s.warn("something in the program folder that MailArchive did not put there was left as it is: %s", strconv.QuoteToASCII(name))
		}
	}
}

// programCloudFile reports a fully downloaded cloud file (OneDrive): a reparse entry
// Go's mode bits call irregular, read as a regular file (CRIT-01); a var so a test can
// inject one.
var programCloudFile = util.IsHydratedCloudFile

// programBeforeOpen runs at the points between judging a kept file and opening it —
// fault injection only, so a test can swap the folder or the file there (CRIT-06).
var programBeforeOpen func(point, path string)

func beforeOpen(point, path string) {
	if programBeforeOpen != nil {
		programBeforeOpen(point, path)
	}
}

// resolveIntent ends an update a stopped run left (R7): each name is judged against
// its incoming and its current entry, accepting either; a name being removed that is
// gone is dropped. It returns the landed set.
func (s *programStepState) resolveIntent(pr *programRoot, set container.ProgramSet) container.ProgramSet {
	cur := map[string]container.ProgramFile{}
	for _, f := range set.Files {
		cur[f.Name] = f
	}
	removing := map[string]bool{}
	for _, n := range set.Removing {
		removing[n] = true
	}
	hashOf := func(name string, size int64) string {
		if pr == nil {
			return ""
		}
		fi, err := pr.root.Lstat(name)
		if err != nil || !(fi.Mode().IsRegular() || programCloudFile(filepath.Join(pr.dir, name))) || !pr.same() {
			return "" // R3: the folder is still the one held before each open (CRIT-06)
		}
		h, _ := hashRegular(filepath.Join(pr.dir, name), fi, size)
		return h
	}
	var landed []container.ProgramFile
	top := set.Version
	for _, in := range set.Incoming {
		h := hashOf(in.Name, in.Size)
		switch c, has := cur[in.Name]; {
		case h != "" && h == in.SHA256:
			landed = append(landed, in)
			if in.Origin == container.OriginRelease && container.CompareVersions(in.Version, top) > 0 {
				top = in.Version
			}
		case has:
			landed = append(landed, c)
		}
		delete(cur, in.Name)
	}
	for name, c := range cur {
		if removing[name] {
			if pr == nil {
				continue
			}
			if _, err := pr.root.Lstat(name); errors.Is(err, fs.ErrNotExist) {
				continue
			}
		}
		landed = append(landed, c)
	}
	set.Files, set.Incoming, set.Removing, set.Note = landed, nil, nil, nil
	set.State, set.Version = container.ProgramLanded, top
	return set
}

// commit seals set by a plain manifest save, under the lock (R1, R2): a lost lock or a
// failed save ends the step. A file list that could not be written counts as landed,
// with a warning (R1).
func (s *programStepState) commit(set container.ProgramSet) error {
	if err := s.env.store.SetProgram(set); err != nil {
		return err
	}
	if err := s.env.stillHeld(); err != nil {
		return err
	}
	err := s.env.save()
	if errors.Is(err, container.ErrInventoryWrite) {
		s.warn("the archive's file list could not be written with the program copies' record; the next backup writes it")
		return nil
	}
	return err
}

// awaitDownload waits for the run's download up to programWaitMax, until the person
// skips the wait, or until the run is cancelled (R17, R20); a wait that runs out counts
// as that day's attempt, a skipped one does not — the download goes on, for the next
// backup. It reports false when the run was cancelled: the step then writes nothing new
// (DUR-06, INT-01).
func (s *programStepState) awaitDownload() bool {
	d := s.env.download
	if d == nil {
		return s.env.ctx.Err() == nil
	}
	s.env.logger.Printf("Waiting for the download of MailArchive %s for the archive's program copies (up to %v)…", d.version, programWaitMax)
	ctx, stop := context.WithCancel(s.env.ctx)
	defer stop()
	skipped := make(chan struct{})
	if s.env.skip != nil {
		go func() {
			select {
			case <-s.env.skip:
				close(skipped)
				stop()
			case <-ctx.Done():
			}
		}()
	}
	if s.env.onWait != nil {
		s.env.onWait(true)
		defer s.env.onWait(false)
	}
	if res, ok := d.task.Wait(ctx, programWaitMax); !ok {
		switch {
		case s.env.ctx.Err() != nil:
			return false
		case isClosed(skipped):
			s.warn("the download of MailArchive %s was skipped; this computer's own program is kept instead, and the download carries on for the next backup", d.version)
			return true
		}
		d.fetcher.CountAttempt(d.version, progcache.ClassWaitRanOut)
		s.warn("the download of MailArchive %s did not finish within the wait; this computer's own program is kept instead", d.version)
	} else if res.Class != "" {
		s.warn("the download of MailArchive %s did not complete (%s)", d.version, res.Class)
	}
	return s.env.ctx.Err() == nil
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// programCacheVersion is the verified cached version release copies are written from:
// the highest of this program's version and the expects version that the cache holds
// verified, at or above what is kept (the high-water mark).
func programCacheVersion(c *progcache.Cache, this, high string) (string, map[string]progcache.Asset, progcache.Asset) {
	best := ""
	for _, v := range []string{this, c.Expects()} {
		if v == "" || container.CompareVersions(v, high) < 0 || len(c.Verified(v)) == 0 {
			continue
		}
		if best == "" || container.CompareVersions(v, best) > 0 {
			best = v
		}
	}
	if best == "" {
		return "", nil, progcache.Asset{}
	}
	assets := c.Assets(best)
	sums := assets[progcache.SumsName]
	delete(assets, progcache.SumsName)
	return best, assets, sums
}

// staged is one file staged under its temporary name.
type staged struct {
	add  programAdd
	tmp  string
	file *os.File
}

// write carries out a plan by the staged protocol (§4.3, R2, R5, R8–R11). Only what
// was written lands: a file that could not be staged or put in place keeps its earlier
// entry, and every removal that depended on it does not happen — the plan is decided
// again without it (DUR-01, DUR-02).
func (s *programStepState) write(pr *programRoot, dir string, set container.ProgramSet, plan programPlan, cache *progcache.Cache, v programView) string {
	env := s.env
	if err := env.stillHeld(); err != nil {
		return ProgramStopped // the lock is gone: nothing more is written (R2)
	}
	if len(plan.add) > 0 && pr == nil {
		var err error
		if pr, err = openProgramDir(dir, true); err != nil {
			s.warn("the program folder could not be made: %v", err)
			return ProgramIncomplete
		}
		defer pr.close()
	}
	failed := map[string]bool{}
	var need int64 = 256 << 20
	for _, a := range plan.add {
		need += a.file.Size
	}
	if free, err := programFreeBytes(env.out); err == nil && int64(free) < need && len(plan.add) > 0 {
		s.warn("the drive has too little free space for the program copies (they need %s); nothing new was written", humanBytes(need))
		for _, a := range plan.add {
			failed[a.file.Name] = true
		}
	}
	// 1. Stage.
	var stagedFiles []staged
	defer func() {
		for _, st := range stagedFiles {
			st.file.Close()
			if st.tmp != "" {
				pr.root.Remove(st.tmp)
			}
		}
	}()
	for _, a := range plan.add {
		if failed[a.file.Name] {
			continue
		}
		st, err := s.stage(pr, a, cache, v.cacheVersion)
		if err != nil {
			s.warn("the copy for %s was not written: %v", systemName(a.file.Platform), err)
			failed[a.file.Name] = true
			continue
		}
		stagedFiles = append(stagedFiles, st)
	}
	if len(failed) > 0 {
		s.failed = true
		plan = s.replan(v, failed)
	}
	pr.sync()
	if err := programAt("staged"); err != nil {
		return ProgramStopped
	}
	if env.ctx.Err() != nil {
		s.warn("cancelled: nothing new was written to the program folder")
		return ProgramCancelled
	}
	if len(stagedFiles) == 0 && len(plan.remove) == 0 {
		return s.recordKept(set, plan)
	}
	// 2. The set as planned, and its note.
	next := landedSet(set, plan, stagedFiles, nil)
	note := []byte(EncryptedNoteFor(&next))
	// 3. The intent commit.
	var incoming []container.ProgramFile
	for _, st := range stagedFiles {
		incoming = append(incoming, st.add.file)
	}
	intent := set
	intent.State, intent.Incoming, intent.Removing = container.ProgramIntent, incoming, plan.remove
	intent.Note = &container.ProgramNote{Current: env.store.NoteDigest(), Incoming: NoteDigest(note)}
	if err := s.commit(intent); err != nil {
		s.warn("the update of the program copies could not be recorded: %v", err)
		return ProgramStopped
	}
	if err := programAt("intent"); err != nil {
		return ProgramStopped
	}
	if err := env.stillHeld(); err != nil {
		return ProgramStopped
	}
	// 4. Renames; then, decided again without any that failed, removals and the note.
	renamed := map[string]bool{}
	for i := range stagedFiles {
		st := &stagedFiles[i]
		if err := pr.root.Rename(st.tmp, st.add.file.Name); err != nil {
			s.warn("the copy for %s could not be put in place: %v", systemName(st.add.file.Platform), err)
			failed[st.add.file.Name] = true
			continue
		}
		st.tmp = ""
		renamed[st.add.file.Name] = true
		ofi, err1 := st.file.Stat()
		nfi, err2 := pr.root.Lstat(st.add.file.Name)
		if err1 != nil || err2 != nil || !os.SameFile(ofi, nfi) {
			s.warn("the copy for %s changed as it was put in place", systemName(st.add.file.Platform))
		}
		if err := programAt("rename"); err != nil {
			return ProgramStopped
		}
	}
	if len(renamed) < len(stagedFiles) {
		s.failed = true
		plan = s.replan(v, failed)
		next = landedSet(set, plan, stagedFiles, renamed)
		note = []byte(EncryptedNoteFor(&next))
	}
	for _, name := range plan.remove {
		if fi, err := pr.root.Lstat(name); err == nil && (fi.Mode().IsRegular() || fi.Mode()&fs.ModeSymlink != 0) {
			pr.root.Remove(name)
		}
	}
	noteWritten := programWriteNote(filepath.Join(env.out, "README.txt"), note) == nil
	if !noteWritten {
		s.warn("could not write README.txt")
	}
	pr.sync()
	if err := programAt("renamed"); err != nil {
		return ProgramStopped
	}
	// 5. The landed commit — the note's digest only when the note was written (DUR-03).
	if noteWritten {
		env.store.SetNoteDigest(NoteDigest(note))
	}
	if err := s.commit(next); err != nil {
		s.warn("the program copies were updated but could not be recorded: %v", err)
		return ProgramStopped
	}
	if !*next.Keep && len(pr.entries()) == 0 {
		pr.close() // Windows will not remove a folder held open (P3-8 Windows lab)
		os.Remove(dir)
	}
	// What landed, for the step's one line (B23): a copy replaced in place counts as
	// written, not also removed.
	size := map[string]int64{}
	for _, f := range set.Files {
		size[f.Name] = f.Size
	}
	for _, st := range stagedFiles {
		if renamed[st.add.file.Name] {
			s.wrote++
			s.wroteBytes += st.add.file.Size
		}
	}
	for _, name := range plan.remove {
		if !renamed[name] {
			s.removed++
			s.removedBytes += size[name]
		}
	}
	return s.classOf(programPlan{keep: next.Files, blocked: plan.blocked, changed: plan.changed}, len(renamed) > 0 || len(plan.remove) > 0)
}

// replan decides again with the failed files taken out, warning nothing twice.
func (s *programStepState) replan(v programView, failed map[string]bool) programPlan {
	return decide(v.withoutFailed(failed))
}

// recordKept seals a plan that writes and removes nothing — its kept entries' counters
// may have changed.
func (s *programStepState) recordKept(set container.ProgramSet, plan programPlan) string {
	if !sameFiles(set.Files, plan.keep) {
		set.Files = plan.keep
		if err := s.commit(set); err != nil {
			s.warn("could not record the program copies' state: %v", err)
		}
	}
	return s.classOf(plan, false)
}

// landedSet is the set a plan lands: its kept entries and the staged files still in
// it — those put in place, when renamed is given. The high-water mark rises only to the
// version of a platform copy that lands, never by a checksum file alone (DUR-01).
func landedSet(set container.ProgramSet, plan programPlan, stagedFiles []staged, renamed map[string]bool) container.ProgramSet {
	inPlan := map[string]bool{}
	for _, a := range plan.add {
		inPlan[a.file.Name] = true
	}
	landed := append([]container.ProgramFile(nil), plan.keep...)
	top := set.Version
	for _, st := range stagedFiles {
		f := st.add.file
		if !inPlan[f.Name] || (renamed != nil && !renamed[f.Name]) {
			continue
		}
		landed = append(landed, f)
		if f.Origin == container.OriginRelease && f.Platform != container.PlatformChecksums && container.CompareVersions(f.Version, top) > 0 {
			top = f.Version
		}
	}
	next := set
	next.Files, next.State, next.Version = landed, container.ProgramLanded, top
	next.Incoming, next.Removing, next.Note = nil, nil, nil
	return next
}

// programFreeBytes is the archive drive's free space; a var so a test can fill it.
var programFreeBytes = util.FreeBytes

// programWriteNote writes the note; a var so a test can fail it (DUR-03).
var programWriteNote = export.WriteFileAtomic

// stage writes one file under a new temporary name, through the folder's handle,
// created exclusively (R11); its hash is the plan's for a release file and the bytes
// written for a self-copy, and the read back through the same handle must match (R10).
func (s *programStepState) stage(pr *programRoot, a programAdd, cache *progcache.Cache, cacheVersion string) (staged, error) {
	tmp := programTempName()
	pr.root.Remove(tmp)
	mode := os.FileMode(0o644)
	if a.self && !strings.HasSuffix(a.file.Name, ".zip") {
		mode = 0o755 // a bare program runs (C8)
	}
	f, err := pr.root.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return staged{}, err
	}
	st := staged{add: a, tmp: tmp, file: f}
	fail := func(err error) (staged, error) {
		f.Close()
		pr.root.Remove(tmp)
		return staged{}, err
	}
	h := sha256.New()
	w := io.MultiWriter(f, h)
	switch {
	case a.sums:
		b, err := cache.Sums(cacheVersion)
		if err != nil {
			return fail(err)
		}
		if _, err := w.Write(b); err != nil {
			return fail(err)
		}
	case a.self:
		if err := writeSelfCopy(w, a.file.Name); err != nil {
			return fail(err)
		}
	default:
		if _, err := cache.CopyOut(cacheVersion, a.asset, w); err != nil {
			return fail(err)
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	size, _ := f.Seek(0, io.SeekEnd)
	if a.self {
		st.add.file.SHA256, st.add.file.Size = sum, size
	} else if sum != a.file.SHA256 {
		return fail(errors.New("its bytes are not the release's"))
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	back := sha256.New()
	if _, err := io.Copy(back, io.LimitReader(f, size+1)); err != nil || hex.EncodeToString(back.Sum(nil)) != st.add.file.SHA256 {
		return fail(errors.New("it did not read back as written"))
	}
	if err := container.ValidateProgram(container.ProgramSet{Format: container.ProgramFormat, Files: []container.ProgramFile{st.add.file}}); err != nil {
		return fail(err)
	}
	return st, nil
}

// sync syncs the program folder through its handle, best effort (R9).
func (pr *programRoot) sync() {
	if pr == nil {
		return
	}
	if d, err := pr.root.Open("."); err == nil {
		d.Sync()
		d.Close()
	}
}
