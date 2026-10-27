package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// The unencrypted copy itself (phase 3 §10, P3-4): DecryptSealed executes a
// DecryptPlan over the archive's sealed side, which internal/keyring provides so no
// key leaves it. It writes only inside the new folder, marked unfinished until the
// last step; it logs counts only — never a path, folder or subject — and lists what
// it left out in a file inside the copy.

// SealedCopy is the encrypted archive's side of a copy.
type SealedCopy interface {
	// Load reads the record read-only.
	Load() (*state.Manifest, error)
	// Unit hands use one unit's plaintext, read whole within the memory budget; the
	// plaintext is valid only during the call.
	Unit(ctx context.Context, rec state.Record, kind vault.UnitKind, su state.SealedUnit, use func(plain []byte) error) error
	// History is the timeline for the copy and how many of its chunks could not be read.
	History() (events []state.HistoryEvent, lost int, err error)
	// HistoryGap says the record itself marks part of the timeline lost.
	HistoryGap() bool
	// IndexBehind says the sealed index is marked as missing messages the archive holds.
	IndexBehind() bool
	// IndexImage streams the sealed search index's image to w; IndexSize is its size.
	IndexImage(w io.Writer) error
	IndexSize() int64
}

// ErrCopyLocked: the archive was locked while the copy was made. It is never damage.
var ErrCopyLocked = errors.New("the archive was locked during the unencrypted copy")

// ErrCopyStopped: the copy was stopped before it finished.
var ErrCopyStopped = errors.New("the unencrypted copy was stopped")

// ErrCopyFailed: the copy could not be finished; its folder is marked unfinished.
var ErrCopyFailed = errors.New("the unencrypted copy could not be finished")

const (
	// CopyMarkerName marks a copy that is not finished: written first, removed last.
	CopyMarkerName = state.UnfinishedCopyName
	// CopyProblemsName lists, inside the copy, the messages it left out.
	CopyProblemsName = "_COPY-PROBLEMS.txt"
)

// ErrUnfinishedCopy refuses a backup into a folder holding an unencrypted copy that
// did not finish: new mail is never mixed into it (phase 3 §16).
var ErrUnfinishedCopy = errors.New("this folder holds an unencrypted copy that did not finish")

// RefuseUnfinishedCopy refuses out when it holds an unencrypted copy that did not
// finish.
func RefuseUnfinishedCopy(out string) error {
	if state.UnfinishedCopy(out) {
		return fmt.Errorf("%w: %s — delete it, then make the copy again; nothing is backed up into it", ErrUnfinishedCopy, out)
	}
	return nil
}

// DecryptReport is what a copy did.
type DecryptReport struct {
	Dest        string
	Messages    int // the records in the archive
	Copied      int // records whose files were all written
	Skipped     int // records with a file left out
	Problems    []string
	HistoryLost int    // parts of the timeline that could not be read
	HistoryGap  bool   // the archive's record marks part of its timeline lost
	Index       string // "copied", "rebuilt" or "none"
	Unfinished  bool   // the folder is still marked unfinished
}

// ExitCode is 0 for a whole copy and 3 for one that left something out (as extract).
func (r DecryptReport) ExitCode() int {
	if r.Skipped > 0 || r.Index == "none" {
		return 3
	}
	return 0
}

// copyLock takes the archive's lock with no verb: a verb left beside an encrypted
// archive would tell anyone with the folder that a readable copy of it was made
// (phase 3 §16).
var copyLock = func(path string) (*lockfile.Lock, error) { return lockfile.AcquireAs(path, "") }

var indexPageName = regexp.MustCompile(`^index(-[0-9]+)?\.html$`)

// copyTop are the names the copy writes at its top, in lower case: no message's files
// go there, in any letter case (a destination may not tell case apart).
var copyTop = map[string]bool{
	strings.ToLower(CopyMarkerName): true, strings.ToLower(CopyProblemsName): true, "readme.txt": true, "search.db": true,
	"search.db-wal": true, "search.db-shm": true, "search.db-journal": true, "attachments-report.tsv": true, "index.html": true,
}

// hasPageName says a segment takes a folder page's name, in any case.
func hasPageName(segs []string) bool {
	for _, s := range segs {
		if indexPageName.MatchString(strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// DecryptSealed makes the unencrypted copy p planned.
func (e *CopyExportEnv) DecryptSealed(ctx context.Context, p *DecryptPlan, archives, settings []string, src SealedCopy, logger *log.Logger, onProgress func(DecryptReport)) (DecryptReport, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if src == nil || p == nil {
		return DecryptReport{}, errors.New("app: DecryptSealed needs a plan and the archive's sealed side")
	}
	if !UnlockableAt(p.Out) {
		return DecryptReport{}, fmt.Errorf("%s is not an encrypted archive MailArchive can open here", p.Out)
	}
	lockPath := filepath.Join(p.Out, lockfile.Name)
	lockFree := false
	lock, lerr := copyLock(lockPath)
	switch {
	case lerr == nil:
		defer lock.Release()
	case export.FatalKindAt(lerr, p.Out) == export.FatalReadOnly:
		if lockfile.HeldReadOnly(lockPath) {
			return DecryptReport{}, fmt.Errorf("%w: %s is being written by another MailArchive; make the copy once that finishes", lockfile.ErrHeld, p.Out)
		}
		lockFree = true
	default:
		return DecryptReport{}, lerr
	}
	// The folder is held first, and the checks must read the folder held: it is
	// compared with the folder its path names before and after them (CreateFolder's
	// order). A swap there and back while they run is not caught (§14).
	pr, err := os.OpenRoot(p.Parent)
	if err != nil {
		if rerr := p.Recheck(e, archives, settings); rerr != nil {
			return DecryptReport{}, rerr
		}
		return DecryptReport{}, err
	}
	defer pr.Close()
	if err := e.sameFolder(pr, p.Parent); err != nil {
		return DecryptReport{}, err
	}
	if err := p.Recheck(e, archives, settings); err != nil {
		return DecryptReport{}, err
	}
	if err := e.sameFolder(pr, p.Parent); err != nil {
		return DecryptReport{}, err
	}
	m, err := src.Load()
	if err != nil {
		return DecryptReport{}, err
	}
	need := copyNeed(m, src.IndexSize())
	if free, ferr := e.freeOnDest(p.Parent); ferr == nil && int64(free) < need {
		return DecryptReport{}, refuse("R-g2", fmt.Sprintf("%s has %s free and the unencrypted copy needs %s — free some space or choose another folder; nothing was written.", p.Parent, humanBytes(int64(free)), humanBytes(need)))
	}
	if err := pr.Mkdir(p.Name, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return DecryptReport{}, fmt.Errorf("%w: %s now exists — plan the copy again", ErrDecryptStale, p.Dest)
		}
		return DecryptReport{}, err
	}
	cr, err := pr.OpenRoot(p.Name)
	if err != nil {
		return DecryptReport{}, err
	}
	defer cr.Close()
	c := &copier{e: e, p: p, m: m, src: src, root: cr, logger: logger, onProgress: onProgress, lockFree: lockFree,
		rep: DecryptReport{Dest: p.Dest, Messages: m.Len(), Unfinished: true}}
	if err := writeNew(cr, CopyMarkerName, []byte(markerText(""))); err != nil {
		cr.Close()
		pr.Remove(p.Name) // the folder is empty: nothing readable is left
		return DecryptReport{}, fmt.Errorf("%w: the folder's unfinished mark could not be written: %w", ErrCopyFailed, err)
	}
	if err := c.run(ctx); err != nil {
		return c.rep, err
	}
	e.step("decrypt-unmark")
	if ctx.Err() != nil { // a stop before the mark comes off leaves the copy unfinished
		return c.rep, c.stop(ErrCopyStopped)
	}
	if err := cr.Remove(CopyMarkerName); err != nil {
		return c.rep, fmt.Errorf("%w: its unfinished mark could not be removed: %w", ErrCopyFailed, err)
	}
	syncRoot(cr)
	c.rep.Unfinished = false
	logger.Printf("Unencrypted copy: %d of %d messages copied, %d left out", c.rep.Copied, c.rep.Messages, c.rep.Skipped)
	return c.rep, nil
}

// copyNeed is the room a copy needs: its files' recorded sizes, the index, and the
// reserve for pages, report and README.
func copyNeed(m *state.Manifest, indexSize int64) int64 {
	var n int64
	for _, r := range m.All() {
		if r.Fixity == nil {
			continue
		}
		for _, d := range []*state.FileDigest{r.Fixity.HTML, r.Fixity.Zip, r.Fixity.EML} {
			if d != nil {
				n += d.Size
			}
		}
	}
	return n + indexSize + decryptReserve(n)
}

type copier struct {
	e          *CopyExportEnv
	p          *DecryptPlan
	m          *state.Manifest
	src        SealedCopy
	root       *os.Root
	logger     *log.Logger
	onProgress func(DecryptReport)
	lockFree   bool // the archive could not be locked (a read-only medium): its record is compared with the plan's at the end
	rep        DecryptReport
}

// sameFolder refuses, before the copy writes anything, unless the folder r holds is
// the one dir names now.
func (e *CopyExportEnv) sameFolder(r *os.Root, dir string) error {
	if e.holds(r, dir) {
		return nil
	}
	return refuse("R-e", fmt.Sprintf("The folder %s changed while MailArchive was checking it — nothing was written. Choose it again.", dir))
}

// holds says the folder r holds is the one dir names now.
func (e *CopyExportEnv) holds(r *os.Root, dir string) bool {
	f, err := r.Open(".")
	if err != nil {
		return false
	}
	id, ierr := readIdentity(f)
	f.Close()
	k, ok := e.folderKey(dir)
	return ierr == nil && ok && k == dirKeyOf(id)
}

func (c *copier) run(ctx context.Context) error {
	recs := c.m.All()
	keys := make([]string, 0, len(recs))
	for k := range recs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return recs[keys[i]].Path < recs[keys[j]].Path })
	for _, k := range keys {
		if ctx.Err() != nil {
			return c.stop(ErrCopyStopped)
		}
		if err := c.record(ctx, recs[k]); err != nil {
			return err
		}
		if c.onProgress != nil {
			c.onProgress(c.rep)
		}
	}
	if ctx.Err() != nil {
		return c.stop(ErrCopyStopped)
	}
	if err := c.history(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return c.stop(ErrCopyStopped)
	}
	// The index, the pages, the report and the README are written by path: the
	// folder the path names must be the one the copy holds, before and after.
	if !c.e.holds(c.root, c.p.Dest) {
		return c.failErr(errCopyMoved)
	}
	if err := c.index(ctx); err != nil {
		return err
	}
	writeReport(c.p.Dest, c.m, c.m.Migrated > 0, silentLogger())
	writeArchiveReadme(c.p.Dest, silentLogger())
	if !c.e.holds(c.root, c.p.Dest) {
		return c.failErr(errCopyMoved)
	}
	if !c.regular("README.txt") {
		return c.failErr(errors.New("its README could not be written"))
	}
	if len(c.rep.Problems) > 0 {
		if err := writeNew(c.root, CopyProblemsName, []byte(problemsText(c.rep.Problems))); err != nil {
			return c.failErr(err)
		}
	}
	if bad := strayEntry(c.p.Dest); bad != "" {
		return c.failErr(fmt.Errorf("it holds a link or a special file (%s)", bad))
	}
	if c.lockFree {
		if now, err := container.PeekFile(c.p.Out); err != nil || now != c.p.pointer {
			return c.failErr(fmt.Errorf("%s changed while the copy was made", c.p.Out))
		}
	}
	// The manifest last: until it is there, nothing takes the folder for an archive.
	if err := c.m.DetachPlaintext(filepath.Join(c.p.Dest, plainManifestName)); err != nil {
		return c.failErr(err)
	}
	if err := c.m.Save(); err != nil {
		return c.failErr(err)
	}
	if !c.e.holds(c.root, c.p.Dest) || !c.regular(plainManifestName) {
		return c.failErr(errCopyMoved)
	}
	return nil
}

const plainManifestName = ".mailarchive-manifest.json"

var errCopyMoved = errors.New("its folder was moved or replaced while the copy was made")

// regular says name, at the copy's top, is a regular file.
func (c *copier) regular(name string) bool {
	fi, err := c.root.Lstat(name)
	return err == nil && fi.Mode().IsRegular()
}

// record copies one message's files, or lists it as left out.
func (c *copier) record(ctx context.Context, r state.Record) error {
	segs, ok := validRelPath(r.Path)
	switch {
	case !ok || len(segs) < 2 || !strings.HasSuffix(r.Path, ".html"):
		return c.leftOut(rawPath(r.Path), "its recorded path is not a safe path inside an archive")
	case r.Unit == "" || r.Sealed == nil:
		return c.leftOut(r.Path, "the archive holds no sealed files for it")
	case copyTop[strings.ToLower(segs[0])] || hasPageName(segs) || hasDotSegment(segs):
		return c.leftOut(r.Path, "its name is one the copy keeps for its own files")
	}
	stem := strings.TrimSuffix(r.Path, ".html")
	var fx state.Fixity
	if r.Fixity != nil {
		fx = *r.Fixity
	}
	for _, u := range []struct {
		kind   vault.UnitKind
		su     *state.SealedUnit
		suffix string
		want   *state.FileDigest
	}{
		{vault.UnitMessage, r.Sealed.HTML, ".html", fx.HTML},
		{vault.UnitAttachment, r.Sealed.Zip, "-attachments.zip", fx.Zip},
		{vault.UnitOriginal, r.Sealed.EML, ".eml", fx.EML},
	} {
		if u.su == nil {
			continue
		}
		rel := stem + u.suffix
		err := c.src.Unit(ctx, r, u.kind, *u.su, func(plain []byte) error {
			if u.want != nil && !digestMatches(u.want, plain) {
				return errCopyFixity
			}
			return writeNewAll(c.root, rel, plain)
		})
		switch {
		case err == nil:
		case errors.Is(err, ErrCopyLocked):
			return c.stop(err)
		case ctx.Err() != nil:
			return c.stop(ErrCopyStopped)
		case errors.Is(err, errCopyExists):
			return c.leftOut(rel, "a file of the same name is already in the copy (two names this system treats as one)")
		case errors.Is(err, errCopyFixity):
			return c.leftOut(rel, "it does not match its recorded checksum")
		case errors.Is(err, fs.ErrNotExist):
			return c.leftOut(rel, "its sealed file is missing from the archive")
		case errors.Is(err, ErrUnitDoesNotOpen) || errors.Is(err, export.ErrUnitStore):
			return c.leftOut(rel, "its sealed file is damaged")
		case errors.Is(err, memguard.ErrTooLarge):
			return c.leftOut(rel, "it is larger than this computer's memory allows copying")
		default:
			_, ferr := c.fail(err)
			return ferr
		}
	}
	c.rep.Copied++
	return nil
}

var (
	errCopyExists = errors.New("a file of that name is already in the copy")
	errCopyFixity = errors.New("does not match its recorded checksum")
)

// leftOut lists a message the copy leaves out, its path without control or
// direction-changing characters (the record is untrusted).
func (c *copier) leftOut(rel, why string) error {
	c.rep.Skipped++
	c.rep.Problems = append(c.rep.Problems, util.StripBidi(rawPath(rel))+": "+why)
	return nil
}

// indexCopied builds the copy's index from the files it wrote, as reindex -rebuild
// does but pruning nothing and logging nothing: a message whose files were left out
// is simply not searchable.
func indexCopied(ctx context.Context, dest string, m *state.Manifest, idx *index.Index) error {
	verf := &verifier{out: dest}
	var scratch RebuildReport
	recs := m.All()
	keys := make([]string, 0, len(recs))
	for k := range recs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		rec := recs[key]
		segs, ok := validRelPath(rec.Path)
		if !ok || len(segs) < 2 {
			continue
		}
		kind, size, htmlFull := verf.inspect(segs)
		if kind != "ok" || size > rebuildMaxFileBytes {
			continue
		}
		msg, _ := deriveMessage(dest, rec.Path, htmlFull, verf, &scratch, silentLogger())
		if msg == nil {
			continue
		}
		_ = idx.Add(segs[0], append([]string{}, segs[1:len(segs)-1]...), msg, rec.Path, key)
	}
	if err := idx.Flush(); err != nil {
		return err
	}
	if n := idx.Lost(); n > 0 {
		return fmt.Errorf("%d rows were lost from the rebuilt index", n)
	}
	return nil
}

// history writes the timeline.
func (c *copier) history() error {
	events, lost, err := c.src.History()
	if err != nil {
		if errors.Is(err, ErrCopyLocked) {
			return c.stop(err)
		}
		_, err = c.fail(err)
		return err
	}
	c.rep.HistoryLost, c.rep.HistoryGap = lost, c.src.HistoryGap()
	if c.rep.HistoryGap {
		// The record marks part of the timeline lost: the copy's says so, as a lost
		// chunk's does, so its go-back view is partial too.
		events = append([]state.HistoryEvent{{Barrier: true}}, events...)
	}
	f, err := c.root.OpenFile(state.HistoryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		err = state.WriteHistory(f, events)
		if serr := f.Sync(); err == nil {
			err = serr
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		_, err = c.fail(fmt.Errorf("its history: %w", err))
		return err
	}
	return nil
}

// index writes search.db from the sealed image, or rebuilds it from the copied files
// when the image cannot be used as it is — unreadable, marked behind (it misses
// messages the copy holds), or listing messages the copy left out; then the folder
// pages from it.
func (c *copier) index(ctx context.Context) error {
	dbPath := filepath.Join(c.p.Dest, "search.db")
	small := c.src.IndexSize() <= copyIndexMemory
	err := errIndexNotAsIs
	var idx *index.Index
	if c.rep.Skipped == 0 && !c.src.IndexBehind() {
		f, ferr := c.root.OpenFile("search.db", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if ferr != nil {
			return c.failErr(fmt.Errorf("its search index: %w", ferr))
		}
		err = c.src.IndexImage(f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if errors.Is(err, ErrCopyLocked) {
			return c.stop(err)
		}
		if err == nil {
			if err = index.CheckImageFile(dbPath); err == nil {
				idx, err = openCopyIndex(dbPath, small)
			}
		}
	}
	c.rep.Index = "copied"
	if err != nil {
		removeRootDB(c.root)
		c.rep.Index = "rebuilt"
		if idx, err = openCopyIndex(dbPath, small); err == nil {
			if err = indexCopied(ctx, c.p.Dest, c.m, idx); err != nil {
				idx.Close()
				idx = nil
			}
		}
		if ctx.Err() != nil {
			removeRootDB(c.root)
			return c.stop(ErrCopyStopped)
		}
		if err != nil {
			removeRootDB(c.root)
			c.rep.Index = "none"
			return nil
		}
	}
	defer idx.Close()
	if ctx.Err() != nil {
		return c.stop(ErrCopyStopped)
	}
	if err := pages.Generate(c.p.Dest, idx, silentLogger(), gapBannerCount(c.m)); err != nil {
		return c.failErr(fmt.Errorf("its folder pages: %w", err))
	}
	return nil
}

var errIndexNotAsIs = errors.New("the archive's index is not used as it is")

// copyIndexMemory is the largest index whose work SQLite keeps in memory, as the
// reader's (§7.4): the folder pages' sort then never spills the subjects and senders
// to the system's temporary folder. A larger one may spill there (§14).
var copyIndexMemory int64 = 256 << 20

// openCopyIndex opens the copy's index, its temporary work kept in memory when small.
func openCopyIndex(path string, small bool) (*index.Index, error) {
	if small {
		return index.OpenTempInMemory(path)
	}
	return index.Open(path)
}

// stop marks the copy stopped — the mark rewritten with counts only — and returns why.
func (c *copier) stop(why error) error {
	c.rewriteMarker(fmt.Sprintf("It was stopped after %d of %d messages.\n", c.rep.Copied+c.rep.Skipped, c.rep.Messages))
	return fmt.Errorf("%w: %s is marked unfinished — delete it, then make the copy again", why, c.p.Dest)
}

// fail leaves the copy marked unfinished and says why.
func (c *copier) fail(err error) (DecryptReport, error) {
	c.rewriteMarker("It could not be finished.\n")
	return c.rep, fmt.Errorf("%w: %s is marked unfinished — delete it, then make the copy again: %w", ErrCopyFailed, c.p.Dest, err)
}

func (c *copier) failErr(err error) error {
	_, err = c.fail(err)
	return err
}

// rewriteMarker marks the copy unfinished, taking its manifest away: an unfinished
// copy holds none, so nothing takes it for an archive.
func (c *copier) rewriteMarker(extra string) {
	c.root.Remove(plainManifestName)
	if f, err := c.root.OpenFile(CopyMarkerName, os.O_WRONLY|os.O_TRUNC, 0); err == nil {
		f.Write([]byte(markerText(extra)))
		f.Sync()
		f.Close()
	}
}

func markerText(extra string) string {
	return "This folder is an UNFINISHED, UNENCRYPTED copy of an encrypted MailArchive archive.\n" +
		"It holds readable mail, and it is not complete.\n" + extra +
		"Delete this folder, then make the copy again.\n"
}

func problemsText(problems []string) string {
	return "These messages were left out of this unencrypted copy. The encrypted archive itself was not changed.\n\n" +
		strings.Join(problems, "\n") + "\n"
}

func hasDotSegment(segs []string) bool {
	for _, s := range segs {
		if strings.HasPrefix(s, ".") {
			return true
		}
	}
	return false
}

// writeNewAll writes data at rel inside r, making its folders; never over a file.
func writeNewAll(r *os.Root, rel string, data []byte) error {
	if dir := path.Dir(rel); dir != "." {
		if err := r.MkdirAll(dir, 0o755); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return errCopyExists
			}
			return err
		}
	}
	return writeNew(r, rel, data)
}

// writeNew creates rel inside r exclusively, writes data and syncs it. A name
// already taken — by another spelling the system treats as the same — is
// errCopyExists; nothing is overwritten.
func writeNew(r *os.Root, rel string, data []byte) error {
	f, err := r.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return errCopyExists
	}
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if serr := f.Sync(); err == nil {
		err = serr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		r.Remove(rel)
	}
	return err
}

func removeRootDB(r *os.Root) {
	for _, n := range []string{"search.db", "search.db-wal", "search.db-shm", "search.db-journal"} {
		r.Remove(n)
	}
}

func syncRoot(r *os.Root) {
	if d, err := r.Open("."); err == nil {
		d.Sync()
		d.Close()
	}
}

// strayEntry is the first entry under dir that is neither a folder nor a regular
// file, or "".
func strayEntry(dir string) string {
	bad := ""
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			bad = p
			return fs.SkipAll
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			bad = p
			return fs.SkipAll
		}
		return nil
	})
	if bad != "" {
		if rel, err := filepath.Rel(dir, bad); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return bad
}

func silentLogger() *log.Logger { return log.New(io.Discard, "", 0) }
