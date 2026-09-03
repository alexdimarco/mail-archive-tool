package app

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mail-archive-tool/internal/interchange"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// ExtractPartial is extract's process exit for a run that produced output but
// could not emit the whole set — some records had no preserved bytes (a fixity
// mismatch, a failed path gate, or no `.eml` at all, INCLUDING an archive from
// which nothing is extractable, e.g. a PST-only archive). It is a fresh code,
// distinct from verify's `2` "not attested" (docs/ux-contract.md X1, PC11):
//
//	0 = the whole requested set emitted (skipped == 0)
//	3 = partial: some records had no preserved bytes (skipped > 0, emitted may be 0)
//	1 = refusal or error (no manifest, -dest overlaps -out, ENOSPC, …), by the caller
const ExtractPartial = 3

// maxExtractEMLBytes bounds a single `.eml` extract reads into memory, so a
// pathological or hostile oversized file (a huge blob dropped at a message stem)
// is skipped-and-reported rather than OOMing the run (PC8, the size cap). A real
// message's preserved bytes are far smaller. A package var so a test can shrink
// it to exercise the oversized path.
var maxExtractEMLBytes int64 = 2 << 30 // 2 GiB

// maxExtractSkipDetail bounds the sampled skip list so a wholly-unextractable
// archive cannot accumulate O(N) lines; the exact counts are always reported.
var maxExtractSkipDetail = 10000

// ExtractFormat is the output format: mbox (one mboxrd file per folder) or eml
// (one byte-exact file per message, mirroring the tree).
type ExtractFormat string

const (
	FormatMbox ExtractFormat = "mbox"
	FormatEML  ExtractFormat = "eml"
)

// Skipped is one record extract could not emit, with the reason.
type Skipped struct {
	Path   string `json:"path"` // the .eml path it would have read (or the raw recorded path when that failed the gate)
	Reason string `json:"reason"`
}

// ExtractReport is extract's result: how many messages were emitted, how many
// skipped and why, and whether any emitted record lacked a recorded fixity (so
// its bytes are "as stored", not attested).
type ExtractReport struct {
	Archive string        `json:"archive"`
	Format  ExtractFormat `json:"format"`
	Dest    string        `json:"dest"`

	Emitted int `json:"emitted"`
	Skipped int `json:"skipped"`

	// Skip reason breakdown (each a subset of Skipped).
	SkippedNoEML    int `json:"skipped_no_eml"`    // no preserved `.eml` for the record
	SkippedFixity   int `json:"skipped_fixity"`    // `.eml` present but failed its recorded Fixity.EML
	SkippedGate     int `json:"skipped_gate"`      // path/inspect gate failure (symlink, non-regular, oversized, bad path)
	EmittedNoFixity int `json:"emitted_no_fixity"` // emitted records with no recorded fixity ("as stored")

	Samples   []Skipped `json:"samples,omitempty"` // bounded sample of skipped records
	Truncated int       `json:"truncated,omitempty"`
}

// ExitCode is extract's process exit for a produced report (a refusal/error is
// exit 1, handled by the caller): 0 when the whole set emitted, ExtractPartial
// when anything was skipped (PC11).
func (r ExtractReport) ExitCode() int {
	if r.Skipped > 0 {
		return ExtractPartial
	}
	return 0
}

// Extract copies every manifest record's preserved original bytes (`<stem>.eml`)
// out of the archive at `out` into `dest`, in the chosen interchange format,
// faithfully or not at all (design G3/R20). It holds the archive's exclusive
// lock for the whole run (PC14), writes NOTHING into the archive, and never
// synthesizes a message: a record with no `.eml`, a fixity mismatch, or a failed
// path gate is counted, listed, and skipped (PC8, PC13). Output is atomic and
// idempotent — a temp file per message (`eml`) or per folder (`mbox`) renamed on
// success, truncating any pre-existing file, never appending — and contained
// within `dest`, which may not overlap `out` (PC9, PC10). onProgress, if non-nil,
// is called with a running snapshot. A refusal (no manifest, a `-dest` overlap, a
// non-empty `-dest` without overwrite, an ENOSPC mid-write) returns an error (the
// caller maps it to exit 1); otherwise the report's ExitCode is the verdict.
func Extract(out string, format ExtractFormat, dest string, overwrite bool, logger *log.Logger, onProgress func(ExtractReport)) (rep ExtractReport, err error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	switch format {
	case FormatMbox, FormatEML:
	default:
		return ExtractReport{}, fmt.Errorf("unknown -format %q (want mbox or eml)", string(format))
	}
	if dest == "" {
		return ExtractReport{}, errors.New("-dest is required (an empty directory to write the extracted mail into)")
	}

	out = absPath(out)
	if fi, statErr := os.Stat(out); statErr != nil || !fi.IsDir() {
		return ExtractReport{}, fmt.Errorf("no archive at %s: not a directory — check the path", out)
	}

	// Hold the exclusive archive lock for the whole run (PC14): extract is a
	// cold, whole-archive read, so it belongs OUTSIDE the backup window — a
	// scheduled backup that fires meanwhile refuses.
	lock, lerr := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "extract")
	if lerr != nil {
		return ExtractReport{}, lerr
	}
	defer lock.Release()

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	if _, statErr := os.Stat(mpath); errors.Is(statErr, fs.ErrNotExist) {
		return ExtractReport{}, fmt.Errorf("no archive to extract at %s: no manifest (%s) — run an export into it first, or check the path", out, mpath)
	}
	manifest, mErr := state.Load(mpath)
	if mErr != nil {
		return ExtractReport{}, mErr
	}

	// -dest may not equal, sit inside, or contain -out (symlink-resolved,
	// component-wise) — extract must never write its output back into the archive
	// it is reading (PC10).
	dabs := absPath(dest)
	if overlap, why := destOverlapsOut(dabs, out); overlap {
		return ExtractReport{}, fmt.Errorf("refusing to extract: -dest %s %s -out %s — choose a destination outside the archive", dest, why, out)
	}
	if err := os.MkdirAll(dabs, 0o755); err != nil {
		return ExtractReport{}, fmt.Errorf("create -dest %s: %w", dest, err)
	}
	if !overwrite {
		entries, rErr := os.ReadDir(dabs)
		if rErr != nil {
			return ExtractReport{}, fmt.Errorf("read -dest %s: %w", dest, rErr)
		}
		if len(entries) > 0 {
			return ExtractReport{}, fmt.Errorf("-dest %s is not empty (%d entr%s): extract into an empty directory, or pass --overwrite to replace files this run produces", dest, len(entries), plural(len(entries), "y", "ies"))
		}
	}
	// extract copies the full volume of preserved `.eml` bytes; -dest needs that
	// much free space (PC10). Stated at the point of use, not only in -h.
	logger.Printf("Extracting preserved originals to %s (format=%s); this copies the full .eml volume, so ensure %s has room.", dest, format, dest)

	x := &extractor{
		out:    out,
		dest:   dabs,
		format: format,
		logger: logger,
		rep:    ExtractReport{Archive: out, Format: format, Dest: dabs},
	}

	// Walk folder-grouped and sorted, so every record of one output folder is
	// contiguous — one mbox handle per folder, opened lazily on the folder's
	// first emitted message and renamed into place on leaving it (PC9). Grouping
	// by the record's own output directory (not merely the key) is robust to any
	// folder-slug ordering.
	records := manifest.All()
	items := make([]walkItem, 0, len(records))
	for key, rec := range records {
		items = append(items, newWalkItem(key, rec))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].dirKey != items[j].dirKey {
			return items[i].dirKey < items[j].dirKey
		}
		return items[i].rec.Path < items[j].rec.Path
	})

	for _, it := range items {
		if ferr := x.advanceFolder(it.dirKey); ferr != nil {
			return x.rep, ferr
		}
		if perr := x.processRecord(it); perr != nil {
			return x.rep, perr
		}
		if onProgress != nil {
			onProgress(x.rep)
		}
	}
	if ferr := x.finalizeFolder(); ferr != nil {
		return x.rep, ferr
	}

	rep = x.rep
	return rep, nil
}

// walkItem is a record paired with its validated output location.
type walkItem struct {
	key    string
	rec    state.Record
	segs   []string // validated, per-component-sanitized path segments (nil if invalid)
	valid  bool
	dirKey string // "/"-joined sanitized directory segments (the output folder), for sort+group
}

// newWalkItem validates a record's recorded path and derives its sanitized
// output segments and folder key. An invalid path leaves valid=false and an
// empty dirKey (it sorts first and is skipped without opening any folder).
func newWalkItem(key string, rec state.Record) walkItem {
	it := walkItem{key: key, rec: rec}
	segs, ok := validRelPath(rec.Path)
	if !ok || len(segs) < 2 || !strings.HasSuffix(rec.Path, verifyHTMLSuffix) {
		return it
	}
	san := make([]string, len(segs))
	for i, s := range segs {
		san[i] = util.SanitizeSegment(s)
	}
	it.segs = san
	it.valid = true
	it.dirKey = strings.Join(san[:len(san)-1], "/")
	return it
}

// extractor carries the per-run state Extract accumulates.
type extractor struct {
	out, dest string
	format    ExtractFormat
	logger    *log.Logger
	rep       ExtractReport

	// mbox folder writer, open across the records of one folder.
	curKey   string
	curOpen  bool
	curFinal string
	curTmp   string
	curFile  *os.File
	curW     *bufio.Writer
}

// advanceFolder closes and renames the previous folder's mbox handle when the
// walk crosses into a new output folder (mbox only). Idempotent for eml.
func (x *extractor) advanceFolder(dirKey string) error {
	if x.format != FormatMbox {
		return nil
	}
	if x.curOpen && x.curKey != dirKey {
		if err := x.finalizeFolder(); err != nil {
			return err
		}
	}
	x.curKey = dirKey
	return nil
}

// processRecord confirms a record's preserved `.eml` through the same gate
// verify uses (validRelPath + component-wise Lstat, no symlink follow,
// regular-file-only, read bounded to the recorded size — PC8), verifies it
// against its recorded Fixity.EML when present (PC13), and emits it in the chosen
// format. Anything that fails is counted and reported, never read past the gate
// and never written (R20). A returned error is a hard failure (ENOSPC, rename)
// that aborts the run.
func (x *extractor) processRecord(it walkItem) error {
	if !it.valid {
		x.skip(rawPath(it.rec.Path), "recorded path is not a safe in-archive .html path", &x.rep.SkippedGate)
		return nil
	}
	// The `.eml` sibling shares the record's stem.
	emlRel := strings.TrimSuffix(it.rec.Path, verifyHTMLSuffix) + verifyEMLSuffix
	segs, ok := validRelPath(emlRel)
	if !ok {
		x.skip(rawPath(emlRel), "the .eml path is not a safe in-archive path", &x.rep.SkippedGate)
		return nil
	}
	kind, size, full := extractInspect(x.out, segs)
	switch kind {
	case "missing":
		x.skip(emlRel, "no preserved original (.eml) — this record has no bytes to extract", &x.rep.SkippedNoEML)
		return nil
	case "symlink":
		x.skip(emlRel, "a symlink stands where the .eml should be (not followed)", &x.rep.SkippedGate)
		return nil
	case "irregular":
		x.skip(emlRel, "the .eml is not a regular file", &x.rep.SkippedGate)
		return nil
	}

	var recorded *state.FileDigest
	if it.rec.Fixity != nil {
		recorded = it.rec.Fixity.EML
	}
	// The expected byte count (the recorded size when we have fixity, else the
	// on-disk size), capped so an oversized blob is skipped not slurped (PC8).
	expected := size
	if recorded != nil {
		expected = recorded.Size
	}
	if expected > maxExtractEMLBytes {
		x.skip(emlRel, fmt.Sprintf("the .eml is %d bytes, larger than the %d-byte extract cap", expected, maxExtractEMLBytes), &x.rep.SkippedGate)
		return nil
	}
	// With fixity, read one byte PAST the recorded size (like verify) so a file
	// longer than recorded — a correct prefix with appended bytes — fails on the
	// length instead of passing on a prefix hash (PC13).
	bound := expected
	if recorded != nil {
		bound = recorded.Size + 1
	}
	data, sum, err := readBounded(full, bound)
	if err != nil {
		x.skip(emlRel, "the .eml could not be read", &x.rep.SkippedGate)
		return nil
	}
	if recorded != nil {
		if int64(len(data)) != recorded.Size || sum != recorded.SHA256 {
			x.skip(emlRel, "the .eml does not match its recorded fixity (modified or truncated) — run `mailarchive verify` first", &x.rep.SkippedFixity)
			return nil
		}
	} else {
		x.rep.EmittedNoFixity++
	}

	if err := x.emit(it, data); err != nil {
		return err
	}
	x.rep.Emitted++
	return nil
}

// emit writes one record's preserved bytes in the chosen format, atomically.
func (x *extractor) emit(it walkItem, data []byte) error {
	switch x.format {
	case FormatEML:
		return x.emitEML(it, data)
	default:
		return x.emitMbox(it, data)
	}
}

// emitEML writes the bytes byte-exact to dest/<store>/<folder…>/<stem>.eml via a
// temp file renamed on success (PC9, PC12 byte-exact). A containment failure is
// a defensive hard error (it cannot occur for a validated, sanitized path).
func (x *extractor) emitEML(it walkItem, data []byte) error {
	// The record's segments end in the `.html` stem; the output is the sibling
	// `.eml`, mirroring the tree.
	outSegs := append([]string(nil), it.segs...)
	last := len(outSegs) - 1
	outSegs[last] = strings.TrimSuffix(outSegs[last], verifyHTMLSuffix) + verifyEMLSuffix
	full, ok := x.destPath(outSegs)
	if !ok {
		return fmt.Errorf("refusing to write %s: it would escape -dest %s", strings.Join(outSegs, "/"), x.dest)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("create output dir for %s: %w", full, err)
	}
	return writeFileAtomic(full, data)
}

// emitMbox appends the message to the current folder's mbox temp file in mboxrd
// form, opening the temp lazily on the folder's first emitted message (PC9).
func (x *extractor) emitMbox(it walkItem, data []byte) error {
	if !x.curOpen {
		if err := x.openFolder(it); err != nil {
			return err
		}
	}
	if err := interchange.WriteMessage(x.curW, data); err != nil {
		// A torn write (ENOSPC) must discard the temp and refuse legibly — never
		// leave a half-written folder file behind (PC9).
		x.discardFolder()
		return fmt.Errorf("write %s: %w (out of disk space?) — the partial temp file was removed", x.curFinal, err)
	}
	return nil
}

// openFolder creates the folder's mbox temp file (under -dest, beside its final
// path). The final path is dest/<store>/<folder parents…>/<leaf folder>.mbox.
func (x *extractor) openFolder(it walkItem) error {
	dirSegs := it.segs[:len(it.segs)-1] // drop the message filename
	// dest/<parents…>/<leaf>.mbox
	parents := dirSegs[:len(dirSegs)-1]
	leaf := dirSegs[len(dirSegs)-1]
	parentDir := filepath.Join(append([]string{x.dest}, parents...)...)
	final := filepath.Join(parentDir, leaf+".mbox")
	if !within(x.dest, final) {
		return fmt.Errorf("refusing to write %s: it would escape -dest %s", final, x.dest)
	}
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("create output dir %s: %w", parentDir, err)
	}
	tmp, err := os.CreateTemp(parentDir, ".mbox-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp mbox in %s: %w", parentDir, err)
	}
	x.curFile = tmp
	x.curTmp = tmp.Name()
	x.curFinal = final
	x.curW = bufio.NewWriter(tmp)
	x.curOpen = true
	return nil
}

// finalizeFolder flushes, fsyncs, closes and renames the open folder mbox temp
// over its final path (truncating any pre-existing file — never appending, PC9).
func (x *extractor) finalizeFolder() error {
	if !x.curOpen {
		return nil
	}
	if err := x.curW.Flush(); err != nil {
		x.discardFolder()
		return fmt.Errorf("write %s: %w (out of disk space?) — the partial temp file was removed", x.curFinal, err)
	}
	if err := x.curFile.Sync(); err != nil {
		x.discardFolder()
		return fmt.Errorf("sync %s: %w — the partial temp file was removed", x.curFinal, err)
	}
	if err := x.curFile.Close(); err != nil {
		os.Remove(x.curTmp)
		x.resetFolder()
		return fmt.Errorf("close %s: %w", x.curFinal, err)
	}
	if err := os.Rename(x.curTmp, x.curFinal); err != nil {
		os.Remove(x.curTmp)
		x.resetFolder()
		return fmt.Errorf("replace %s: %w", x.curFinal, err)
	}
	x.resetFolder()
	return nil
}

// discardFolder removes a torn temp file (a write/flush error) and clears the
// folder state, so nothing partial is ever renamed into place.
func (x *extractor) discardFolder() {
	if x.curFile != nil {
		x.curFile.Close()
	}
	if x.curTmp != "" {
		os.Remove(x.curTmp)
	}
	x.resetFolder()
}

func (x *extractor) resetFolder() {
	x.curOpen = false
	x.curFile = nil
	x.curW = nil
	x.curTmp = ""
	x.curFinal = ""
}

// destPath builds the byte-exact eml output path from the sanitized segments and
// asserts it stays within -dest.
func (x *extractor) destPath(segs []string) (string, bool) {
	full := filepath.Join(append([]string{x.dest}, segs...)...)
	if !within(x.dest, full) {
		return "", false
	}
	return full, true
}

// skip records a skipped record: the exact reason counter always increments; the
// sampled list is bounded (the exact totals are always in the report).
func (x *extractor) skip(path, reason string, counter *int) {
	x.rep.Skipped++
	*counter++
	if len(x.rep.Samples) < maxExtractSkipDetail {
		x.rep.Samples = append(x.rep.Samples, Skipped{Path: path, Reason: reason})
	} else {
		x.rep.Truncated++
	}
}

// extractInspect resolves segs component-wise under out, refusing to follow a
// symlink at any level (the same posture verify's inspect uses). It returns
// "missing" | "symlink" | "irregular" | "ok" plus the file size and resolved
// path for the ok case.
func extractInspect(out string, segs []string) (kind string, size int64, full string) {
	cur := out
	for i, s := range segs {
		cur = filepath.Join(cur, s)
		fi, err := os.Lstat(cur)
		if err != nil {
			return "missing", 0, cur
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return "symlink", 0, cur
		}
		last := i == len(segs)-1
		if last {
			if !fi.Mode().IsRegular() {
				return "irregular", 0, cur
			}
			return "ok", fi.Size(), cur
		}
		if !fi.IsDir() {
			return "missing", 0, cur
		}
	}
	return "missing", 0, cur
}

// readBounded reads at most limit bytes of full and returns the bytes and their
// sha256 (hex). A file longer than limit is truncated to limit here; the caller
// treats a length that then disagrees with the recorded size as a fixity failure.
func readBounded(full string, limit int64) ([]byte, string, error) {
	f, err := os.Open(full)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	if limit < 0 {
		limit = 0
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

// writeFileAtomic writes data to a temp file beside full and renames it over
// full on success (truncating any pre-existing file, never appending — PC9),
// fsyncing first so a crash cannot leave a half-written .eml under its final name.
func writeFileAtomic(full string, data []byte) error {
	dir := filepath.Dir(full)
	tmp, err := os.CreateTemp(dir, ".eml-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write %s: %w (out of disk space?) — the partial temp file was removed", full, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync %s: %w — the partial temp file was removed", full, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close %s: %w", full, err)
	}
	if err := os.Rename(tmpName, full); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace %s: %w", full, err)
	}
	return nil
}

// destOverlapsOut reports whether dest equals, sits inside, or contains out
// (both symlink-resolved to their deepest existing ancestor), and a phrase
// describing the overlap for the refusal (PC10).
func destOverlapsOut(dest, out string) (bool, string) {
	rd := resolveExisting(dest)
	ro := resolveExisting(out)
	switch {
	case rd == ro:
		return true, "is the same directory as"
	case within(ro, rd):
		return true, "is inside"
	case within(rd, ro):
		return true, "contains"
	default:
		return false, ""
	}
}

// within reports whether target is base itself or a path under base, compared
// component-wise on cleaned absolute paths (no string-prefix false positives
// like /a/bc under /a/b).
func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveExisting returns p as an absolute, symlink-resolved path, resolving the
// deepest ancestor that exists (so a not-yet-created -dest still resolves a
// symlinked parent) and re-appending the non-existent tail.
func resolveExisting(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	cur := abs
	var tail []string
	for {
		if resolved, rerr := filepath.EvalSymlinks(cur); rerr == nil {
			parts := append([]string{resolved}, reverseStrings(tail)...)
			return filepath.Join(parts...)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// absPath returns p as an absolute, cleaned path (best effort).
func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}

func reverseStrings(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

// ExtractSummary renders the report as the lines `extract` prints (stdout): the
// destination and format, the emitted/skipped counts with the reason breakdown,
// a bounded sample of what was skipped, the "as stored" caveat when any emitted
// record had no recorded fixity, and a closing verdict naming the exit meaning.
func ExtractSummary(r ExtractReport) []string {
	var out []string
	out = append(out, fmt.Sprintf("Extracted %s from %s to %s", r.Format, r.Archive, r.Dest))
	out = append(out, fmt.Sprintf("emitted=%d skipped=%d (no-eml=%d fixity-mismatch=%d gate=%d)",
		r.Emitted, r.Skipped, r.SkippedNoEML, r.SkippedFixity, r.SkippedGate))
	for _, s := range r.Samples {
		out = append(out, "  skipped: "+rawPath(s.Path)+" ("+rawPath(s.Reason)+")")
	}
	if r.Truncated > 0 {
		out = append(out, fmt.Sprintf("  … %d more skipped (list truncated at %d)", r.Truncated, maxExtractSkipDetail))
	}
	if r.EmittedNoFixity > 0 {
		out = append(out, fmt.Sprintf("note: %d message(s) had no recorded fixity — emitted as stored; run `mailarchive verify` first to attest the bytes.", r.EmittedNoFixity))
	}
	switch {
	case r.Emitted == 0 && r.Skipped == 0:
		out = append(out, "nothing to extract: this archive has no records.")
	case r.Skipped > 0 && r.Emitted == 0:
		out = append(out, "NOTHING extractable: no record has preserved original bytes (.eml). A PST/OST archive never has originals; an mbox/maildir/Graph archive must be captured with -raw. (exit 3)")
	case r.Skipped > 0:
		out = append(out, "PARTIAL: some records had no preserved bytes and were skipped (named above). (exit 3)")
	default:
		out = append(out, "done: every record's preserved original was emitted.")
	}
	return out
}
