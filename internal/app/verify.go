package app

import (
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

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// The suffixes of the files a record owns. Kept in step with the exporter's own
// baseName/zipSuffix (a record's html is <stem>.html; its siblings are the same
// stem + these).
const (
	verifyHTMLSuffix = ".html"
	verifyZipSuffix  = "-attachments.zip"
	verifyEMLSuffix  = ".eml"
)

// maxVerifyDetail bounds each per-category detail list so a wholly-drifted
// archive cannot accumulate O(N) findings in memory or O(N) lines in the report
// (FC14). The exact counts are always reported; only the listed examples are
// capped. A package var so a test can shrink it.
var maxVerifyDetail = 10000

// VerifyOptions configures a verify run.
type VerifyOptions struct {
	// Record baselines fixity: for every recorded file that has no digest yet,
	// hash its current bytes and store them as the fixity from now on (FC4).
	// Labelled as a baseline from current bytes, never proof the bytes were
	// pristine.
	Record bool
}

// Problem is one classified file that is not plainly ok.
type Problem struct {
	Path   string `json:"path"` // archive-relative, forward-slashed (or the raw recorded path when it fails containment)
	Kind   string `json:"kind"` // "modified" | "missing" | "unrecorded" | "unexpected"
	Detail string `json:"detail,omitempty"`
}

// Report is the verify verdict: exact per-category counts, coverage, and a
// bounded sample of the problems.
type Report struct {
	Out string `json:"out"`

	Records    int `json:"records"`     // records in the manifest (M)
	WithFixity int `json:"with_fixity"` // records carrying at least one digest (N)
	Checked    int `json:"checked"`     // recorded files hashed and compared (or baselined)

	OK         int `json:"ok"`
	Modified   int `json:"modified"`
	Missing    int `json:"missing"`
	Unrecorded int `json:"unrecorded"`
	Unexpected int `json:"unexpected"`
	Recorded   int `json:"recorded,omitempty"` // files baselined this run (-record)

	Problems  []Problem      `json:"problems"`            // bounded per category (see maxVerifyDetail)
	Truncated map[string]int `json:"truncated,omitempty"` // kind → count omitted from Problems
}

// Attested reports whether every recorded file was checked and intact and no
// record lacks fixity: nothing modified, missing, or unrecorded (FC4).
// `unexpected` is reported but does not un-attest the recorded set.
func (r Report) Attested() bool {
	return r.Modified == 0 && r.Missing == 0 && r.Unrecorded == 0
}

// ExitCode is verify's process exit for a report that was produced (a refusal
// or error is exit 1, handled by the caller): 0 attested, 2 not attested (FC4).
func (r Report) ExitCode() int {
	if r.Attested() {
		return 0
	}
	return 2
}

// Verify checks the archive at out against its recorded fixity: it holds the
// exclusive archive lock for the whole check (so an export cannot race it —
// verify belongs outside the backup window, S25/FC5), loads the manifest, and
// classifies every recorded file. It trusts nothing in the manifest paths:
// each path must be clean, relative, forward-slashed, contain no ".." and have
// a store-token first segment; it is resolved component-wise inside the root
// and any symlink at any level is an integrity failure, never followed; a
// non-regular file is classified without being opened; reads stop at the
// recorded size + 1 byte (FC6). It writes nothing unless opts.Record is set.
// onProgress, if non-nil, is called with a running snapshot as records are
// checked. A refusal (no manifest, locked archive, unreadable manifest) returns
// an error (the caller maps it to exit 1); otherwise the Report's ExitCode is
// the verdict.
func Verify(out string, opts VerifyOptions, logger *log.Logger, onProgress func(Report)) (Report, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	lock, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "verify")
	if err != nil {
		return Report{}, err
	}
	defer lock.Release()

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	if _, statErr := os.Stat(mpath); errors.Is(statErr, fs.ErrNotExist) {
		return Report{}, fmt.Errorf("no archive to verify at %s: no manifest (%s) — run an export into it first, or check the path", out, mpath)
	}
	manifest, err := state.Load(mpath)
	if err != nil {
		return Report{}, err
	}
	if manifest.Rekeyed > 0 {
		logger.Printf("re-scoped %d manifest entr%s by store (one-time upgrade)", manifest.Rekeyed, plural(manifest.Rekeyed, "y", "ies"))
	}

	v := &verifier{
		out:      out,
		opts:     opts,
		manifest: manifest,
		rep:      Report{Out: out},
		shown:    map[string]int{},
		owned:    map[string]bool{},
	}

	records := manifest.All()
	v.rep.Records = len(records)

	keys := make([]string, 0, len(records))
	for k := range records {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// First pass: record which relative paths belong to some record (its html
	// and both possible siblings), so the unexpected walk can tell an archived
	// file apart from a stray.
	for _, k := range keys {
		if p := records[k].Path; p != "" && strings.HasSuffix(p, verifyHTMLSuffix) {
			stem := strings.TrimSuffix(p, verifyHTMLSuffix)
			v.owned[p] = true
			v.owned[stem+verifyZipSuffix] = true
			v.owned[stem+verifyEMLSuffix] = true
		}
	}

	// Second pass: classify each record's files.
	for _, k := range keys {
		v.checkRecord(k, records[k])
		if onProgress != nil {
			onProgress(v.rep)
		}
	}

	// Third pass: files under the store directories that no record owns.
	v.walkUnexpected()

	// Coverage is reported from the manifest as it stands now — after -record
	// has baselined anything — so with_fixity reflects current coverage.
	v.rep.WithFixity, _ = manifest.FixityCounts()

	if opts.Record && v.rep.Recorded > 0 {
		if err := manifest.Save(); err != nil {
			return v.rep, fmt.Errorf("record fixity baseline: %w", err)
		}
		logger.Printf("recorded fixity for %d file(s) (baseline from current bytes)", v.rep.Recorded)
	}
	return v.rep, nil
}

// verifier carries the per-run state Verify accumulates.
type verifier struct {
	out      string
	opts     VerifyOptions
	manifest *state.Manifest
	rep      Report
	shown    map[string]int  // kind → problems already appended (for the per-category cap)
	owned    map[string]bool // relative paths any record owns
}

// checkRecord classifies a single record's html and (when present or recorded)
// its zip and eml siblings.
func (v *verifier) checkRecord(key string, rec state.Record) {
	path := rec.Path
	segs, ok := validRelPath(path)
	if !ok {
		// A path that fails containment is an integrity failure, not something
		// we resolve or read (FC6). Report the raw recorded path so the operator
		// can see what the manifest claims.
		v.add("modified", rawPath(path), "recorded path is not a safe, relative in-archive path")
		return
	}

	var fx state.Fixity
	if rec.Fixity != nil {
		fx = *rec.Fixity
	}
	stem := strings.TrimSuffix(path, verifyHTMLSuffix)
	baseSegs := segs[:len(segs)-1] // the html's directory segments

	// The html is always expected.
	if updated, save := v.checkFile(path, segs, fx.HTML, true); save {
		fx.HTML = updated
		v.saveFixity(key, &rec, &fx)
	}

	// The zip and eml: expected when a digest is recorded, or when the file is
	// actually present on disk (a legacy record whose sibling we can baseline).
	for _, sib := range []struct {
		suffix string
		digest **state.FileDigest
	}{
		{verifyZipSuffix, &fx.Zip},
		{verifyEMLSuffix, &fx.EML},
	} {
		relSlash := stem + sib.suffix
		sibSegs := append(append([]string{}, baseSegs...), lastSegment(relSlash))
		if updated, save := v.checkSibling(relSlash, sibSegs, *sib.digest); save {
			*sib.digest = updated
			v.saveFixity(key, &rec, &fx)
		}
	}
}

// checkFile classifies one expected file (a digest may be recorded or not).
// When required is true the file's absence of a digest is `unrecorded` (the
// html of a legacy record); it returns a new digest + true when -record
// baselined the file, so the caller persists it.
func (v *verifier) checkFile(relSlash string, segs []string, digest *state.FileDigest, required bool) (*state.FileDigest, bool) {
	kind, size, full := v.inspect(segs)
	switch kind {
	case "missing":
		v.add("missing", relSlash, "")
		return nil, false
	case "symlink":
		v.add("modified", relSlash, "a symlink stands where an archived file should be (not followed)")
		return nil, false
	case "irregular":
		v.add("modified", relSlash, "not a regular file")
		return nil, false
	}
	// kind == "ok": a regular file is present.
	if digest != nil {
		v.rep.Checked++
		if ok, detail := compare(full, *digest, size); ok {
			v.rep.OK++
		} else {
			v.add("modified", relSlash, detail)
		}
		return nil, false
	}
	// No digest recorded for a present, regular file.
	if v.opts.Record {
		if nd, ok := v.baseline(full); ok {
			return nd, true
		}
		// Could not read it to baseline: it is an integrity problem, not ok.
		v.add("modified", relSlash, "could not be read to baseline")
		return nil, false
	}
	if required {
		v.add("unrecorded", relSlash, "no recorded fixity")
	}
	return nil, false
}

// baseline hashes the whole file and returns its digest, counting it as
// checked/ok/recorded (used by -record). Returns nil,false if it cannot be read.
func (v *verifier) baseline(full string) (*state.FileDigest, bool) {
	sum, n, err := hashFile(full, 0)
	if err != nil {
		return nil, false
	}
	v.rep.Checked++
	v.rep.OK++
	v.rep.Recorded++
	return &state.FileDigest{SHA256: sum, Size: n}, true
}

// checkSibling classifies an optional sibling (zip/eml): a recorded digest is
// checked like any file; with no digest, an absent sibling is simply not owned
// by fixity (skipped), a present regular file is `unrecorded` (baselined under
// -record), and a symlink / non-regular file present at the sibling path is an
// integrity failure.
func (v *verifier) checkSibling(relSlash string, segs []string, digest *state.FileDigest) (*state.FileDigest, bool) {
	if digest != nil {
		return v.checkFile(relSlash, segs, digest, true)
	}
	kind, _, full := v.inspect(segs)
	switch kind {
	case "missing":
		return nil, false // a record need not have a zip or an eml
	case "symlink":
		v.add("modified", relSlash, "a symlink stands where an archived file should be (not followed)")
		return nil, false
	case "irregular":
		v.add("modified", relSlash, "not a regular file")
		return nil, false
	}
	if v.opts.Record {
		if nd, ok := v.baseline(full); ok {
			return nd, true
		}
		v.add("modified", relSlash, "could not be read to baseline")
		return nil, false
	}
	v.add("unrecorded", relSlash, "no recorded fixity")
	return nil, false
}

// saveFixity writes an updated Fixity back into the manifest (used only under
// -record; the manifest is saved once at the end of the run).
func (v *verifier) saveFixity(key string, rec *state.Record, fx *state.Fixity) {
	rec.Fixity = fx
	v.manifest.Add(key, *rec)
}

// inspect resolves relSlash component-wise under the archive root, refusing to
// follow a symlink at any level. It returns:
//   - "missing"   the file (or an intermediate component) does not exist
//   - "symlink"   a symlink was found at some component (never followed)
//   - "irregular" the final component exists but is not a regular file
//   - "ok"        a regular file is present (size is its length)
//
// full is the resolved absolute path (for the ok case).
func (v *verifier) inspect(segs []string) (kind string, size int64, full string) {
	cur := v.out
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
			// An intermediate component is a file: the recorded path cannot exist.
			return "missing", 0, cur
		}
	}
	return "missing", 0, cur
}

// compare hashes the file once, capped at the recorded size + 1 byte, and
// reports whether the length and digest both match plus a detail string for a
// mismatch (FC6): a file longer than recorded stops the read at size+1 and
// fails on the length, so an oversized file never forces reading gigabytes.
func compare(full string, d state.FileDigest, statSize int64) (bool, string) {
	sum, n, err := hashFile(full, d.Size+1)
	if err != nil {
		return false, "could not be read"
	}
	switch {
	case n != d.Size:
		return false, fmt.Sprintf("size %d, recorded %d", statSize, d.Size)
	case sum != d.SHA256:
		return false, "sha256 mismatch"
	default:
		return true, ""
	}
}

// walkUnexpected reports .html/.zip/.eml files under the store directories that
// no record owns. It never descends into a symlinked directory (WalkDir uses
// Lstat, so a symlink is a leaf it does not follow) and skips the tool's own
// files (folder index.html pages, README.txt, the report, logs, dotfiles).
func (v *verifier) walkUnexpected() {
	filepath.WalkDir(v.out, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != v.out && strings.HasPrefix(name, ".") {
				return filepath.SkipDir // dot-directories are not archive stores
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || name == "index.html" {
			return nil // folder pages and dotfiles are the tool's own
		}
		if !isArchivedFileName(name) {
			return nil
		}
		rel, relErr := filepath.Rel(v.out, path)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if !strings.Contains(relSlash, "/") {
			return nil // a bare root-level file is not under a store directory
		}
		if v.owned[relSlash] {
			return nil
		}
		v.add("unexpected", relSlash, "")
		return nil
	})
}

// isArchivedFileName reports whether name is one of the archived artifact kinds
// (an exported message page, its attachment zip, or its kept raw eml).
func isArchivedFileName(name string) bool {
	return strings.HasSuffix(name, verifyZipSuffix) ||
		strings.HasSuffix(name, verifyEMLSuffix) ||
		(strings.HasSuffix(name, verifyHTMLSuffix) && name != "index.html")
}

// add records a classified problem: the exact per-category counter always
// increments; the sampled Problems list is bounded per category (FC14).
func (v *verifier) add(kind, path, detail string) {
	switch kind {
	case "modified":
		v.rep.Modified++
	case "missing":
		v.rep.Missing++
	case "unrecorded":
		v.rep.Unrecorded++
	case "unexpected":
		v.rep.Unexpected++
	}
	if v.shown[kind] < maxVerifyDetail {
		v.rep.Problems = append(v.rep.Problems, Problem{Path: path, Kind: kind, Detail: detail})
		v.shown[kind]++
	} else {
		if v.rep.Truncated == nil {
			v.rep.Truncated = map[string]int{}
		}
		v.rep.Truncated[kind]++
	}
}

// validRelPath validates a recorded path as a clean, relative, forward-slashed
// in-archive path whose first segment is a store token: no absolute path, no
// backslash or drive colon, no empty/"."/".." segment, and a first segment that
// is not a dotfile (FC6). It returns the split segments on success.
func validRelPath(p string) ([]string, bool) {
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return nil, false
	}
	if strings.ContainsAny(p, `\:`) {
		return nil, false
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if s == "" || s == "." || s == ".." {
			return nil, false
		}
		if i == 0 && strings.HasPrefix(s, ".") {
			return nil, false
		}
	}
	return parts, true
}

// lastSegment returns the final forward-slash segment of a relative path.
func lastSegment(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// rawPath renders a rejected recorded path safely for the report, stripping
// control characters (the manifest is untrusted).
func rawPath(p string) string {
	if p == "" {
		return "(empty path)"
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, p)
}

// hashFile returns the sha256 (hex) and byte count of full, reading at most
// limit bytes (limit <= 0 reads the whole file, for a baseline). It opens the
// file only after inspect confirmed a regular file at every path component; a
// non-regular file is never opened.
func hashFile(full string, limit int64) (string, int64, error) {
	f, err := os.Open(full)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	var r io.Reader = f
	if limit > 0 {
		r = io.LimitReader(f, limit)
	}
	n, err := io.Copy(h, r)
	if err != nil {
		return "", n, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// VerifySummary renders the report as the lines `verify` prints (stdout), in
// verify's plain voice: a coverage line, the per-category counts, a bounded
// sample of the problems, the `verify -record` remedy when anything is
// unrecorded, a WARN when nothing was checkable, and the final verdict.
func VerifySummary(r Report) []string {
	var out []string
	out = append(out, fmt.Sprintf("Fixity check of %s", r.Out))
	out = append(out, fmt.Sprintf("records=%d with-fixity=%d checked=%d", r.Records, r.WithFixity, r.Checked))
	out = append(out, fmt.Sprintf("ok=%d modified=%d missing=%d unrecorded=%d unexpected=%d",
		r.OK, r.Modified, r.Missing, r.Unrecorded, r.Unexpected))
	if r.Recorded > 0 {
		out = append(out, fmt.Sprintf("recorded fixity for %d file(s) (baseline from current bytes)", r.Recorded))
	}
	for _, p := range r.Problems {
		line := "  " + p.Kind + ": " + rawPath(p.Path)
		if p.Detail != "" {
			line += " (" + rawPath(p.Detail) + ")"
		}
		out = append(out, line)
	}
	for _, kind := range []string{"modified", "missing", "unrecorded", "unexpected"} {
		if n := r.Truncated[kind]; n > 0 {
			out = append(out, fmt.Sprintf("  … %d more %s (list truncated at %d)", n, kind, maxVerifyDetail))
		}
	}
	if r.Unrecorded > 0 {
		out = append(out, fmt.Sprintf("WARN: %d file(s) carry no recorded fixity — run `mailarchive verify -record -out %q` to baseline them (records the bytes as they are now, not proof they were pristine)", r.Unrecorded, r.Out))
	}
	if r.Checked == 0 {
		out = append(out, "WARN: nothing was checked — no archived file in this archive carries recorded fixity yet")
	}
	if r.Attested() {
		out = append(out, "attested: every recorded file was checked and intact")
	} else {
		out = append(out, "NOT attested: the archive has files that are modified, missing, or unrecorded")
	}
	return out
}

// VerifyResultLine is the one-line result for a run log.
func VerifyResultLine(r Report) string {
	verdict := "attested"
	if !r.Attested() {
		verdict = "NOT-ATTESTED"
	}
	return fmt.Sprintf("%s records=%d with-fixity=%d checked=%d ok=%d modified=%d missing=%d unrecorded=%d unexpected=%d",
		verdict, r.Records, r.WithFixity, r.Checked, r.OK, r.Modified, r.Missing, r.Unrecorded, r.Unexpected)
}
