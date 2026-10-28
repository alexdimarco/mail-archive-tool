package container

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// The keyless tier-1 check (design-encrypted-archives-phase2b §4, §13; A5, A17,
// P2b-C17): what can be said of an encrypted archive without its key, from the
// inventory, the pointers' cleartext headers and the files themselves. It reads
// only — it never writes, heals or removes — and every state of the folder ends in
// one outcome of the closed list (state.Outcome*).

// Check is a keyless check's verdict.
type Check struct {
	Outcome string
	// Reason is the fixed sentence of the rule that decided the outcome.
	Reason string
	// Advice is the next step when the deciding rule has one of its own; empty, the
	// outcome's fixed sentence (state.OutcomeText) stands.
	Advice string
	// Checked counts the live files judged; Missing and Modified count exactly
	// those not as listed, and Problems names them (bounded by MaxCheckProblems).
	Checked           int
	Missing, Modified int
	Problems          []FileProblem
	// Unnamed counts the files under units/ and chunks/ the file list does not name:
	// informational, never changing the outcome. A backup removes only those a run
	// wrote since the record's last commit, which the check cannot tell without the
	// key, so nothing is said of their removal (the fix re-verification, FV-C-04).
	Unnamed int
	// Leftovers counts the parts of the file list the top file no longer names (the
	// shards the last run superseded) and an interrupted first commit's files, which
	// the next backup removes — unless LeftoversStay: with the file list missing,
	// behind or partly unread the next backup sweeps nothing it does not name, and
	// some unnamed files may be live (the adversarial pass, DEC-03).
	Leftovers     int
	LeftoversStay bool
	// PartsUnread counts shards of the file list that could not be read.
	PartsUnread int
	// ProgramOdd counts the entries of program/ that are not copies MailArchive keeps
	// there (program design §3, C9), naming up to ten: informational, never changing
	// the outcome.
	ProgramOdd      int
	ProgramOddNames []string
}

// FileProblem is one live file that is not as the file list says.
type FileProblem struct {
	Path   string // archive-relative: units/<hh>/<32 hex> or chunks/<hh>/<32 hex>
	Kind   string // "missing" | "modified"
	Detail string
}

// MaxCheckProblems bounds Check.Problems, so a wholly damaged archive cannot fill
// memory; a var so a test can shrink it.
var MaxCheckProblems = 10000

// The fixed reasons, one per rule (phase2b §4 and §13).
const (
	reasonIntact        = "every file this archive's record lists is present and unchanged"
	reasonNoRecordYet   = "no backup has saved this archive's record yet"
	reasonNoFileList    = "this archive has no file list (its first one was not saved, or it was removed)"
	reasonListBehind    = "its file list is older than its record (a backup's file list was not saved)"
	reasonMirrorNone    = "the last backup could not write the second copy of its record pointer"
	reasonPartsUnread   = "part of its file list could not be read; the next backup rewrites it"
	reasonCleanRestore  = "this archive was restored to an older copy; the next backup accepts it"
	reasonFilesBad      = "files its record lists are missing or changed"
	reasonPointersGone  = "both copies of its record pointer are missing or unreadable"
	reasonPointerBad    = "a copy of its record pointer is missing or does not match its file list"
	reasonListDamaged   = "its file list is damaged"
	reasonNoRecord      = "sealed files are present but its record is missing"
	reasonListNoRecord  = "its file list is present but its record is missing"
	reasonStrayNoRecord = "its record is missing, and files a backup does not write stand where its record's files belong"
	reasonSavedNoRecord = "this computer saved this archive's record, but its record is missing"
	reasonUsedNoRecord  = "the archive has been used (it holds a search index or go-back history), but its record is missing"
	reasonNotAFolder    = "a link or a file stands where one of its folders should be"
	reasonNotSealed     = "its record file holds a manifest that is not a sealed record"
	reasonEnvelopeBad   = "a folder, a link or a special file stands at its record file's name"
	reasonRecordCopy    = "a file of its record or search index is missing or damaged. Your mail is still there: the archive keeps two copies of its record, and only one looks affected"
	reasonRecordGone    = "every file of its record and search index is missing or changed"
	reasonRecordOlder   = "its record is older than its file list — an older copy of the record may have been restored"
	reasonUnfinished    = "its last save did not finish, or an older copy of its record was restored"
	reasonMergeRestored = "it is older than the last backup this computer made, and holds files its file list does not name"
	reasonNewer         = "it was written by a newer MailArchive"
)

// adviceRecordCopy is reasonRecordCopy's next step: the backup is the real test,
// rewriting a damaged copy from the good one or refusing, naming the file, when
// both are lost (the owner's wording, 2026-10-25).
const adviceRecordCopy = "What to do: run a backup now. It rewrites the damaged copy from the good one. Then check the archive again. If the backup stops and names a file, both copies are lost: restore the whole archive folder from your newest backup copy."

// CheckKeyless checks the encrypted archive in out without its key. recordedGen
// is the highest generation this computer committed (A25; 0 until Phases 3–4
// persist it).
func CheckKeyless(out string, recordedGen uint64) Check {
	c, _ := CheckKeylessContext(context.Background(), out, recordedGen)
	return c
}

// CheckKeylessContext is CheckKeyless that stops between files when ctx ends
// (Ctrl-C, Desktop's Cancel; phase3 §8): it then returns ctx's error and no
// verdict, since a check that did not finish attests nothing.
func CheckKeylessContext(ctx context.Context, out string, recordedGen uint64) (Check, error) {
	k := &keylessCheck{out: out, ctx: ctx}
	k.c.Outcome, k.c.Reason = k.decide(recordedGen)
	if err := ctx.Err(); err != nil {
		return Check{}, err
	}
	k.c.ProgramOdd, k.c.ProgramOddNames = programOdd(out)
	return k.c, nil
}

// ProgramDir is the folder an encrypted archive keeps copies of MailArchive in.
const ProgramDir = "program"

// programTemp is the name of the program step's own temporary file (§4.3).
var programTemp = regexp.MustCompile(`^\.mailarchive-program-[0-9]{1,20}\.tmp$`)

// maxProgramOddNames bounds the names a check gives.
const maxProgramOddNames = 10

// programOdd counts what in program/ is not a copy MailArchive could keep there: a
// link, a folder, a special file, a name outside §3's pattern or ending
// -attachments.zip, a file over ProgramMaxFile. program/ itself not a real folder is
// one. Its own temporary files and the system's litter are not counted; nothing is
// followed or opened.
func programOdd(out string) (int, []string) {
	dir := filepath.Join(out, ProgramDir)
	ok, err := realDir(dir)
	switch {
	case err != nil:
		return 1, []string{ProgramDir}
	case !ok:
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1, []string{ProgramDir}
	}
	n, names := 0, []string(nil)
	lower := map[string]int{}
	for _, e := range entries {
		lower[strings.ToLower(e.Name())]++
	}
	for _, e := range entries {
		name := e.Name()
		regular := e.Type().IsRegular() || util.IsHydratedCloudFile(filepath.Join(dir, name))
		if util.IsLitter(name) || (programTemp.MatchString(name) && regular) {
			continue
		}
		// A name twice in letter case only is a twin beside a kept copy (the PG pass,
		// SWAP-02); a folder or a link with the step's temporary name is not its own
		// (SWAP-03).
		odd := !regular || !programName.MatchString(name) || strings.HasSuffix(strings.ToLower(name), "-attachments.zip") || lower[strings.ToLower(name)] > 1
		if !odd {
			if fi, err := e.Info(); err != nil || fi.Size() > ProgramMaxFile {
				odd = true
			}
		}
		if !odd {
			continue
		}
		n++
		if len(names) < maxProgramOddNames {
			if !programName.MatchString(name) {
				name = strconv.QuoteToASCII(name)
			}
			names = append(names, ProgramDir+"/"+name)
		}
	}
	return n, names
}

type keylessCheck struct {
	out    string
	ctx    context.Context
	c      Check
	listed map[string]bool // every path the file list names, in any state
	// unitsBad counts the files under units/ among Missing and Modified; chunks and
	// chunksBad the live files under chunks/ the file list names, and those of them
	// missing or changed.
	unitsBad, chunks, chunksBad int
	// invNotFolder: inventory/ is a link or a file, which no backup can write through.
	invNotFolder bool
}

// pointerCopy is what a keyless reader learns of one pointer file.
type pointerCopy struct {
	present bool    // a file is at its name
	read    *Digest // its bytes' digest, when they could be read
	gen     uint64  // its cleartext generation, when it reads as a current pointer
	newer   uint64  // its cleartext generation, when it reads as a newer one
}

func (k *keylessCheck) decide(recordedGen uint64) (string, string) {
	env := k.envelope()
	primary, mirror := k.pointer(PointerName), k.pointer(MirrorName)
	P, newer := max(primary.gen, mirror.gen), max(primary.newer, mirror.newer)
	// In the keyed load's order: a non-file at the envelope's name is refused before
	// anything else, a plaintext manifest before a newer format, and a newer envelope
	// decides only where no pointer copy stands (the adversarial pass: DEC-01,
	// DEC-06, OUT-09).
	switch {
	case env == envelopeNotFile:
		return state.OutcomeDamaged, reasonEnvelopeBad
	case env == envelopePlain:
		return state.OutcomeDamaged, reasonNotSealed
	case newer > P, P == 0 && env == envelopeNewer && !primary.present && !mirror.present:
		return state.OutcomeNewerFormat, reasonNewer
	}
	top, topState := k.top()
	if P == 0 {
		switch {
		case primary.present || mirror.present || topState != topAbsent:
			return state.OutcomeDamaged, reasonPointersGone
		case recordedGen > 0:
			return state.OutcomeDamaged, reasonSavedNoRecord
		}
		if why := k.firstCommitOwed(); why != "" {
			return state.OutcomeDamaged, why
		}
		return state.OutcomeNotAttested, reasonNoRecordYet
	}
	switch topState {
	case topAbsent:
		k.c.Unnamed, _ = k.unlisted()
		k.c.LeftoversStay = true
		return state.OutcomeNotAttested, reasonNoFileList
	case topDamaged:
		return state.OutcomeDamaged, reasonListDamaged
	}
	I := top.Gen
	switch {
	case I == P+1:
		return state.OutcomeUnfinishedSave, reasonUnfinished
	case I > P:
		return state.OutcomeRestoredOlder, reasonRecordOlder
	}
	k.judgeListed(top)
	if k.ctx.Err() != nil {
		// Stopped: no whole-tree walk for a verdict that is thrown away (the P3-0
		// adversarial pass, CON-06).
		return "", ""
	}
	unnamed, sealedLeft := k.unlisted()
	k.c.Unnamed, k.c.Leftovers = unnamed, k.unnamedShards(top)
	k.c.LeftoversStay = I < P || k.c.PartsUnread > 0
	switch {
	case k.unitsBad > 0:
		return state.OutcomeDamaged, reasonFilesBad
	case k.invNotFolder:
		// No backup can write its file list through it (the fix re-verification,
		// FV-C-02).
		return state.OutcomeDamaged, reasonNotAFolder
	case k.c.PartsUnread > 0 && I != P:
		// The keyed load tolerates a damaged part of the file list only at I = P; here
		// the backup refuses the archive (the adversarial pass, CON-01).
		return state.OutcomeDamaged, reasonListDamaged
	case k.c.PartsUnread == 0 && k.strayFolder():
		// With the file list whole the backup's start sweep walks units/ and chunks/,
		// and refuses a link or a special file where a folder belongs (FV-C-03).
		return state.OutcomeDamaged, reasonNotAFolder
	case I < P:
		return state.OutcomeNotAttested, reasonListBehind
	case !sameDigest(primary.read, &top.Pointer) || (top.Mirror != nil && !sameDigest(mirror.read, top.Mirror)):
		return state.OutcomeDamaged, reasonPointerBad
	case recordedGen == P+1:
		return state.OutcomeUnfinishedSave, reasonUnfinished
	case recordedGen > P && sealedLeft > 0:
		return state.OutcomeRestoredOlder, reasonMergeRestored
	case recordedGen > P:
		return state.OutcomeNotAttested, reasonCleanRestore
	case k.chunksBad > 0 && k.chunksBad == k.chunks:
		// Every one of them: both copies of every record section are gone, which the
		// check can tell without the key (the fix re-verification, FV-C-01).
		return state.OutcomeDamaged, reasonRecordGone
	case k.chunksBad > 0:
		// Only some files under chunks/: a record chunk has a second copy the keyed
		// load reads and the next save rewrites, and it refuses, naming the file, when
		// both are lost; the search index is rebuildable. Without the key the two
		// copies cannot be paired, so this is not damage the check can attest (the
		// adversarial pass, CRIT-1).
		k.c.Advice = adviceRecordCopy
		return state.OutcomeNotAttested, reasonRecordCopy
	case top.Mirror == nil:
		return state.OutcomeNotAttested, reasonMirrorNone
	case k.c.PartsUnread > 0:
		return state.OutcomeNotAttested, reasonPartsUnread
	}
	return state.OutcomeIntact, reasonIntact
}

func sameDigest(a, b *Digest) bool { return a != nil && b != nil && *a == *b }

// envelopeKind is what the keyless check learns of the cleartext envelope.
type envelopeKind int

const (
	envelopeHealable envelopeKind = iota // absent, unreadable or current: the next backup heals it
	envelopeNewer                        // from a newer MailArchive
	envelopePlain                        // a plaintext manifest at its name
	envelopeNotFile                      // a folder, a link or a special file at its name
)

// envelope classifies the cleartext envelope as the keyed load's readEnvelope does,
// without the archive id it cannot check: a file too large for an envelope or JSON
// carrying records is a plaintext manifest, whatever its version; a planted non-file
// is refused; a missing or unreadable one is healed by the next backup.
func (k *keylessCheck) envelope() envelopeKind {
	raw, _, err := readFile(k.out, EnvelopeName, maxEnvelopeSize)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return envelopeHealable
	case errors.Is(err, errTooLarge):
		// errTooLarge is wrapped in ErrStore: this case comes first.
		return envelopePlain
	case errors.Is(err, ErrStore):
		return envelopeNotFile
	case err != nil:
		return envelopeHealable
	}
	var probe struct {
		Version int             `json:"version"`
		Entries json.RawMessage `json:"entries"`
	}
	switch {
	case json.Unmarshal(raw, &probe) != nil:
		return envelopeHealable
	case probe.Entries != nil:
		return envelopePlain
	case probe.Version > vault.EncryptedManifestVersion:
		return envelopeNewer
	}
	return envelopeHealable
}

// pointer reads one pointer copy's cleartext header and digest.
func (k *keylessCheck) pointer(name string) pointerCopy {
	var p pointerCopy
	if _, err := os.Lstat(filepath.Join(k.out, name)); err == nil {
		p.present = true
	}
	raw, _, err := readFile(k.out, name, MaxPointerSize)
	if err != nil {
		return p
	}
	p.read = &Digest{SHA256: sha256.Sum256(raw), Size: int64(len(raw))}
	switch _, gen, err := Peek(raw); {
	case errors.Is(err, ErrNewerFormat):
		p.newer = gen
	case err == nil && gen < math.MaxUint64:
		// No writer reaches the last generation, and P + 1 must not wrap (the
		// adversarial pass, OUT-03): such a header is not a current pointer's.
		p.gen = gen
	}
	return p
}

type topState int

const (
	topAbsent topState = iota
	topDamaged
	topWhole
)

// top reads the inventory's top file. One present but unreadable — a newer-looking
// top line included, since an inventory format change comes with a pointer format
// change (§11 CRIT-2) — is damage: a writer only ever renames it whole (§13).
func (k *keylessCheck) top() (Top, topState) {
	raw, _, err := readFile(k.out, InventoryName, int64(maxShards+5)*256)
	if errors.Is(err, fs.ErrNotExist) {
		return Top{}, topAbsent
	}
	if err != nil {
		return Top{}, topDamaged
	}
	t, err := ParseTop(raw)
	if err != nil || t.Gen == math.MaxUint64 {
		return Top{}, topDamaged
	}
	return t, topWhole
}

// judgeListed reads every shard the top file names and judges each live file. A
// shard that cannot be read is counted and its files go unjudged; a missing
// retained file is not damage (the next run's start sweep removes them).
func (k *keylessCheck) judgeListed(top Top) {
	k.listed = map[string]bool{}
	// A link or a file where inventory/ belongs: no shard is read, nor stat'd
	// through it (the adversarial pass, OUT-04).
	if ok, err := realDir(filepath.Join(k.out, InventoryDir)); err != nil || !ok {
		k.c.PartsUnread = len(top.Shards)
		k.invNotFolder = err != nil
		return
	}
	for _, ref := range top.Shards {
		limit := int64(ref.Entries) * maxShardLine
		fi, err := os.Lstat(filepath.Join(k.out, filepath.FromSlash(ref.Name)))
		if err != nil || fi.Size() > limit {
			k.c.PartsUnread++
			continue
		}
		b, _, err := readShard(k.out, ref.Name, limit)
		if err != nil || sha256.Sum256(b) != ref.SHA256 {
			k.c.PartsUnread++
			continue
		}
		sh, err := ParseShard(ref.Prefix, b)
		if err != nil || len(sh.Files) != ref.Entries {
			k.c.PartsUnread++
			continue
		}
		for _, f := range sh.Files {
			if k.ctx.Err() != nil {
				return
			}
			k.listed[f.Path] = true
			if f.State != Live {
				continue
			}
			k.c.Checked++
			chunk := strings.HasPrefix(f.Path, ChunksDir+"/")
			if chunk {
				k.chunks++
			}
			if kind, detail := judgeFile(k.out, f.Path, f.Digest); kind != "" {
				if kind == "missing" {
					k.c.Missing++
				} else {
					k.c.Modified++
				}
				if chunk {
					k.chunksBad++
				} else {
					k.unitsBad++
				}
				if len(k.c.Problems) < MaxCheckProblems {
					k.c.Problems = append(k.c.Problems, FileProblem{Path: f.Path, Kind: kind, Detail: detail})
				}
			}
		}
	}
}

// judgeFile judges one listed file as the keyed load reads one: every folder a real
// folder, the file a regular file (or a Windows cloud placeholder) and no link, its
// size, then its SHA-256 over at most size+1 bytes. "" is a match. The file opened
// must be the very file judged: a folder or the file swapped between the judgement
// and the open (a writer racing the check) is never read as matching (the
// adversarial pass, OUT-02). An os.Root open was tried and set aside: on Windows it
// refuses reparse points, which may include cloud placeholders the keyed load reads
// (the fix re-verification, FV-C-07).
func judgeFile(root, rel string, d Digest) (kind, detail string) {
	cur := root
	segs := strings.Split(rel, "/")
	var fi os.FileInfo
	for i, s := range segs {
		cur = filepath.Join(cur, s)
		var err error
		fi, err = os.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return "missing", ""
		case err != nil:
			return "modified", "could not be read"
		case fi.Mode()&fs.ModeSymlink != 0:
			return "modified", "a link stands where a sealed file should be (not followed)"
		case i < len(segs)-1 && !fi.IsDir():
			return "missing", ""
		}
	}
	if !util.DescriptorReadable(runtime.GOOS, fi.Mode()) {
		return "modified", "not a regular file"
	}
	// Pinned: compared once it is opened (P3-8, B1). A pin that did not take would
	// read its identity at the open, from whatever is there then (WINPATH-1).
	fi, pinned := util.PinIdentity(fi)
	if !pinned {
		return "modified", "could not be read"
	}
	if fi.Size() != d.Size {
		return "modified", fmt.Sprintf("size %d, listed %d", fi.Size(), d.Size)
	}
	if beforeOpen != nil {
		beforeOpen(rel)
	}
	f, err := util.OpenRegularFile(cur)
	if err != nil {
		return "modified", "could not be read"
	}
	defer f.Close()
	if opened, err := f.Stat(); err != nil || !os.SameFile(fi, opened) {
		return "modified", "changed while it was checked"
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, d.Size+1))
	switch {
	case err != nil:
		return "modified", "could not be read"
	case n != d.Size:
		return "modified", fmt.Sprintf("size %d, listed %d", n, d.Size)
	case [32]byte(h.Sum(nil)) != d.SHA256:
		return "modified", "sha256 mismatch"
	}
	return "", ""
}

// beforeOpen, when set, runs between judging a listed file's path and opening it:
// fault injection only, so a test can swap a folder in that window (OUT-02).
var beforeOpen func(rel string)

// beforeWalk, when set, runs as a walk of a whole folder starts: a test hook only,
// so a test can count the walks a stopped check makes (the P3-0 adversarial pass,
// CON-06).
var beforeWalk func(d string)

func walking(d string) {
	if beforeWalk != nil {
		beforeWalk(d)
	}
}

// firstCommitOwed is the keyed load's checkFresh (and walkLeftovers) without the
// key: with no pointer, the folder is a new archive only when units/ and inventory/
// are empty, chunks/ holds at most an interrupted first commit's leftovers — real
// two-character shard folders of regular chunk-shaped files and temps, counted as
// leftovers — and no search index or go-back history says it was used. It returns
// "" when the first commit is owed, else the reason the record is missing.
func (k *keylessCheck) firstCommitOwed() string {
	for _, name := range []string{"search.db", ".mailarchive-history.jsonl"} {
		if exists(k.out, name) {
			return reasonUsedNoRecord
		}
	}
	for _, p := range [][2]string{{"units", reasonNoRecord}, {InventoryDir, reasonListNoRecord}} {
		d, why := p[0], p[1]
		dir := filepath.Join(k.out, d)
		if ok, err := realDir(dir); err != nil {
			return reasonNotAFolder
		} else if ok {
			if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
				return why
			}
		}
	}
	root := filepath.Join(k.out, ChunksDir)
	ok, err := realDir(root)
	if err != nil {
		return reasonNotAFolder
	}
	if !ok {
		return ""
	}
	shards, err := os.ReadDir(root)
	if err != nil {
		return reasonStrayNoRecord
	}
	n := 0
	for _, sh := range shards {
		dir := filepath.Join(root, sh.Name())
		if ok, err := realDir(dir); err != nil || !ok || len(sh.Name()) != 2 {
			return reasonStrayNoRecord
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			return reasonStrayNoRecord
		}
		for _, f := range files {
			rel := ChunksDir + "/" + sh.Name() + "/" + f.Name()
			if !f.Type().IsRegular() || !(filePath.MatchString(rel) || tempName.MatchString(f.Name())) {
				return reasonStrayNoRecord
			}
			n++
		}
	}
	k.c.Leftovers = n
	return ""
}

// strayFolder reports whether units/ or chunks/, or an entry where one of their
// shard folders belongs, is a link or a special file: the keyed start sweep's
// walkFiles and the unit sweep refuse each (a regular file there they leave alone).
func (k *keylessCheck) strayFolder() bool {
	for _, d := range []string{"units", ChunksDir} {
		walking(d)
		root := filepath.Join(k.out, d)
		ok, err := realDir(root)
		if err != nil {
			return true
		}
		if !ok {
			continue
		}
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if !e.IsDir() && !e.Type().IsRegular() {
				return true
			}
		}
	}
	return false
}

// unlisted counts the files under units/ and chunks/ the file list does not name
// (every one, with no file list), and of those the unit- or chunk-shaped ones: the
// keyed load's merge signal, which a crash-left temp is not (the adversarial pass,
// DEC-05). Links and other non-folders where a folder belongs are not followed: a
// link at a listed path is judged by judgeFile.
func (k *keylessCheck) unlisted() (all, sealed int) {
	for _, d := range []string{"units", ChunksDir} {
		k.walk(d, func(rel string) {
			if !k.listed[rel] {
				all++
				if filePath.MatchString(rel) {
					sealed++
				}
			}
		})
	}
	return all, sealed
}

// unnamedShards counts the files under inventory/ the top file does not name.
func (k *keylessCheck) unnamedShards(top Top) int {
	named := map[string]bool{}
	for _, ref := range top.Shards {
		named[ref.Name] = true
	}
	n := 0
	walking(InventoryDir)
	dir := filepath.Join(k.out, InventoryDir)
	if ok, err := realDir(dir); err != nil || !ok {
		return 0
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !named[InventoryDir+"/"+e.Name()] {
			n++
		}
	}
	return n
}

// walk calls fn with every entry two levels under the folder d (units/<hh>/<name>),
// only through real folders; an entry where a shard folder belongs is reported too.
func (k *keylessCheck) walk(d string, fn func(rel string)) {
	walking(d)
	root := filepath.Join(k.out, d)
	if ok, err := realDir(root); err != nil || !ok {
		return
	}
	shards, _ := os.ReadDir(root)
	for _, sh := range shards {
		if ok, err := realDir(filepath.Join(root, sh.Name())); err != nil || !ok {
			fn(d + "/" + sh.Name())
			continue
		}
		files, _ := os.ReadDir(filepath.Join(root, sh.Name()))
		for _, f := range files {
			fn(d + "/" + sh.Name() + "/" + f.Name())
		}
	}
}
