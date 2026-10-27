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
}

// The program step's outcome classes.
const (
	ProgramNotChosen  = "not chosen"
	ProgramUnchanged  = "unchanged"
	ProgramUpdated    = "updated"
	ProgramIncomplete = "incomplete"
	ProgramSkipped    = "skipped"
	ProgramKeptAsIs   = "kept as it is"
	ProgramStopped    = "stopped"
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

// programWaitMax bounds the program step's wait for its download (§5, R20).
var programWaitMax = 10 * time.Minute

// programFetcher makes the download's fetcher; a var so a test serves its own release.
var programFetcher = func(c *progcache.Cache, this string) *progcache.Fetcher {
	return &progcache.Fetcher{Cache: c, This: this}
}

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
	want := this
	if e := c.Expects(); container.CompareVersions(e, want) > 0 {
		want = e
	}
	if len(c.Verified(want)) == len(progcache.Assets) {
		return nil
	}
	f := programFetcher(c, this)
	if ok, _ := f.ShouldTry(want); !ok {
		return nil
	}
	return &programDownload{task: progcache.Downloads.Start(ctx, f, want), fetcher: f, version: want}
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
}

type programStepState struct {
	env programEnv
	o   ProgramOutcome
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
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	r := &programRoot{dir: dir, root: root, fi: fi}
	if hf, err := root.Open("."); err != nil {
		root.Close()
		return nil, err
	} else {
		hfi, serr := hf.Stat()
		hf.Close()
		if serr != nil || !os.SameFile(hfi, fi) {
			root.Close()
			return nil, fmt.Errorf("the program folder changed while it was opened")
		}
	}
	return r, nil
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
	path := filepath.Join(r.dir, f.Name)
	if programPlaceholder(path) {
		return jUnchecked, ""
	}
	if !fi.Mode().IsRegular() {
		return jBlocked, ""
	}
	if fi.Size() != f.Size {
		return jChanged, ""
	}
	if !r.same() {
		return jUnreadable, ""
	}
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
	s.awaitDownload()

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
		if !sameFiles(set.Files, plan.keep) {
			set.Files = plan.keep
			if err := s.commit(set); err != nil {
				s.warn("could not record the program copies' state: %v", err)
			}
		}
		return s.classOf(plan, false)
	}
	return s.write(pr, dir, set, plan, cache, v.cacheVersion)
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
	for _, k := range plan.keep {
		if k.MissingRuns >= 2 {
			s.warn("the program copies are incomplete: the copy for %s has been missing for %d backups", systemName(k.Platform), k.MissingRuns)
			return ProgramIncomplete
		}
	}
	if len(plan.blocked) > 0 {
		return ProgramIncomplete
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
		switch {
		case util.IsLitter(name):
			continue
		case isProgramTemp(name):
			if fi, err := e.Info(); err == nil && fi.ModTime().Before(s.env.runStart) && fi.Mode().IsRegular() {
				pr.root.Remove(name)
			}
			continue
		}
		want, ok := sealed[strings.ToLower(name)]
		switch {
		case ok && want == name:
		case ok:
			if _, err := pr.root.Lstat(want); errors.Is(err, fs.ErrNotExist) {
				pr.root.Rename(name, want)
			}
		case e.Type().IsRegular() && container.ProgramNameOK(name):
			if err := pr.root.Remove(name); err == nil {
				s.warn("a file in the program folder that MailArchive did not put there was removed: %s", name)
			}
		default:
			s.warn("something in the program folder that MailArchive did not put there was left as it is: %s", strconv.QuoteToASCII(name))
		}
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
		if err != nil || !fi.Mode().IsRegular() {
			return ""
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

// awaitDownload waits for the run's download up to programWaitMax, or until the run is
// cancelled (R17, R20); a wait that runs out counts as that day's attempt.
func (s *programStepState) awaitDownload() {
	d := s.env.download
	if d == nil {
		return
	}
	s.env.logger.Printf("Waiting for the download of MailArchive %s for the archive's program copies (up to %v)…", d.version, programWaitMax)
	if res, ok := d.task.Wait(s.env.ctx, programWaitMax); !ok {
		if s.env.ctx.Err() == nil {
			d.fetcher.CountAttempt(d.version, progcache.ClassWaitRanOut)
			s.warn("the download of MailArchive %s did not finish within the wait; this computer's own program is kept instead", d.version)
		}
	} else if res.Class != "" {
		s.warn("the download of MailArchive %s did not complete (%s)", d.version, res.Class)
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

// write carries out a plan by the staged protocol (§4.3, R2, R5, R8–R11).
func (s *programStepState) write(pr *programRoot, dir string, set container.ProgramSet, plan programPlan, cache *progcache.Cache, cacheVersion string) string {
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
	var need int64 = 256 << 20
	for _, a := range plan.add {
		need += a.file.Size
	}
	if free, err := util.FreeBytes(env.out); err == nil && int64(free) < need && len(plan.add) > 0 {
		s.warn("the drive has too little free space for the program copies (they need %s); nothing was written", humanBytes(need))
		plan.add = nil
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
		st, err := s.stage(pr, a, cache, cacheVersion)
		if err != nil {
			s.warn("the copy for %s was not written: %v", systemName(a.file.Platform), err)
			continue
		}
		stagedFiles = append(stagedFiles, st)
	}
	pr.sync()
	if err := programAt("staged"); err != nil {
		return ProgramStopped
	}
	// 2. The landed set as planned, and its note.
	landed := append([]container.ProgramFile(nil), plan.keep...)
	var incoming []container.ProgramFile
	top := set.Version
	for _, st := range stagedFiles {
		incoming = append(incoming, st.add.file)
		landed = append(landed, st.add.file)
		if st.add.file.Origin == container.OriginRelease && container.CompareVersions(st.add.file.Version, top) > 0 {
			top = st.add.file.Version
		}
	}
	next := set
	next.Files, next.State, next.Version = landed, container.ProgramLanded, top
	next.Incoming, next.Removing, next.Note = nil, nil, nil
	note := []byte(EncryptedNoteFor(&next))
	// 3. The intent commit.
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
	// 4. Renames, removals, the note.
	for i := range stagedFiles {
		st := &stagedFiles[i]
		if err := pr.root.Rename(st.tmp, st.add.file.Name); err != nil {
			s.warn("the copy for %s could not be put in place: %v", systemName(st.add.file.Platform), err)
			continue
		}
		st.tmp = ""
		ofi, err1 := st.file.Stat()
		nfi, err2 := pr.root.Lstat(st.add.file.Name)
		if err1 != nil || err2 != nil || !os.SameFile(ofi, nfi) {
			s.warn("the copy for %s changed as it was put in place", systemName(st.add.file.Platform))
		}
		if err := programAt("rename"); err != nil {
			return ProgramStopped
		}
	}
	for _, name := range plan.remove {
		if fi, err := pr.root.Lstat(name); err == nil && (fi.Mode().IsRegular() || fi.Mode()&fs.ModeSymlink != 0) {
			pr.root.Remove(name)
		}
	}
	if err := export.WriteFileAtomic(filepath.Join(env.out, "README.txt"), note); err != nil {
		s.warn("could not write README.txt: %v", err)
	}
	pr.sync()
	if err := programAt("renamed"); err != nil {
		return ProgramStopped
	}
	// 5. The landed commit.
	env.store.SetNoteDigest(NoteDigest(note))
	if err := s.commit(next); err != nil {
		s.warn("the program copies were updated but could not be recorded: %v", err)
		return ProgramStopped
	}
	if !*next.Keep && len(pr.entries()) == 0 {
		os.Remove(dir)
	}
	return s.classOf(programPlan{keep: next.Files, blocked: plan.blocked}, len(stagedFiles) > 0 || len(plan.remove) > 0)
}

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
