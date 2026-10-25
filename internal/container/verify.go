package container

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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
	// Checked counts the live files judged; Missing and Modified count exactly
	// those not as listed, and Problems names them (bounded by MaxCheckProblems).
	Checked           int
	Missing, Modified int
	Problems          []FileProblem
	// Leftovers counts files nothing lists — under units/ and chunks/, and under
	// inventory/ the top file does not name — which the next backup removes.
	Leftovers int
	// PartsUnread counts shards of the file list that could not be read.
	PartsUnread int
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
	reasonNotSealed     = "its record file holds a manifest that is not a sealed record"
	reasonRecordOlder   = "its record is older than its file list — an older copy of the record may have been restored"
	reasonUnfinished    = "its last save did not finish, or an older copy of its record was restored"
	reasonMergeRestored = "it is older than the last backup this computer made, and holds files its file list does not name"
	reasonNewer         = "it was written by a newer MailArchive"
)

// CheckKeyless checks the encrypted archive in out without its key. recordedGen
// is the highest generation this computer committed (A25; 0 until Phases 3–4
// persist it).
func CheckKeyless(out string, recordedGen uint64) Check {
	k := &keylessCheck{out: out}
	k.c.Outcome, k.c.Reason = k.decide(recordedGen)
	return k.c
}

type keylessCheck struct {
	out    string
	c      Check
	listed map[string]bool // every path the file list names, in any state
}

// pointerCopy is what a keyless reader learns of one pointer file.
type pointerCopy struct {
	present bool    // a file is at its name
	read    *Digest // its bytes' digest, when they could be read
	gen     uint64  // its cleartext generation, when it reads as a current pointer
	newer   uint64  // its cleartext generation, when it reads as a newer one
}

func (k *keylessCheck) decide(recordedGen uint64) (string, string) {
	envNewer, envPlain := k.envelope()
	primary, mirror := k.pointer(PointerName), k.pointer(MirrorName)
	P, newer := max(primary.gen, mirror.gen), max(primary.newer, mirror.newer)
	switch {
	case newer > P, P == 0 && envNewer:
		return state.OutcomeNewerFormat, reasonNewer
	case envPlain:
		return state.OutcomeDamaged, reasonNotSealed
	}
	top, topState := k.top()
	if P == 0 {
		switch {
		case primary.present || mirror.present || topState != topAbsent:
			return state.OutcomeDamaged, reasonPointersGone
		case recordedGen > 0 || !k.firstCommitOwed():
			return state.OutcomeDamaged, reasonNoRecord
		}
		return state.OutcomeNotAttested, reasonNoRecordYet
	}
	switch topState {
	case topAbsent:
		k.c.Leftovers = k.unlisted()
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
	sealedLeft := k.unlisted()
	k.c.Leftovers = sealedLeft + k.unnamedShards(top)
	if k.c.Missing+k.c.Modified > 0 {
		return state.OutcomeDamaged, reasonFilesBad
	}
	if I < P {
		return state.OutcomeNotAttested, reasonListBehind
	}
	if !sameDigest(primary.read, &top.Pointer) || (top.Mirror != nil && !sameDigest(mirror.read, top.Mirror)) {
		return state.OutcomeDamaged, reasonPointerBad
	}
	switch {
	case recordedGen == P+1:
		return state.OutcomeUnfinishedSave, reasonUnfinished
	case recordedGen > P && sealedLeft > 0:
		return state.OutcomeRestoredOlder, reasonMergeRestored
	case recordedGen > P:
		return state.OutcomeNotAttested, reasonCleanRestore
	case top.Mirror == nil:
		return state.OutcomeNotAttested, reasonMirrorNone
	case k.c.PartsUnread > 0:
		return state.OutcomeNotAttested, reasonPartsUnread
	}
	return state.OutcomeIntact, reasonIntact
}

func sameDigest(a, b *Digest) bool { return a != nil && b != nil && *a == *b }

// envelope classifies the cleartext envelope as the keyed load does, without the
// archive id it cannot check: newer, or a plaintext manifest at its name. A
// missing or unreadable envelope is healed by the next backup, never damage.
func (k *keylessCheck) envelope() (newer, plain bool) {
	raw, _, err := readFile(k.out, EnvelopeName, maxEnvelopeSize)
	if errors.Is(err, errTooLarge) {
		return false, true
	}
	if err != nil {
		return false, false
	}
	var probe struct {
		Version int             `json:"version"`
		Entries json.RawMessage `json:"entries"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return false, false
	}
	return probe.Version > vault.EncryptedManifestVersion, probe.Entries != nil
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
	case err == nil:
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
	if err != nil {
		return Top{}, topDamaged
	}
	return t, topWhole
}

// judgeListed reads every shard the top file names and judges each live file. A
// shard that cannot be read is counted and its files go unjudged; a missing
// retained file is not damage (the next run's start sweep removes them).
func (k *keylessCheck) judgeListed(top Top) {
	k.listed = map[string]bool{}
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
			k.listed[f.Path] = true
			if f.State != Live {
				continue
			}
			k.c.Checked++
			if kind, detail := judgeFile(k.out, f.Path, f.Digest); kind != "" {
				if kind == "missing" {
					k.c.Missing++
				} else {
					k.c.Modified++
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
// size, then its SHA-256 over at most size+1 bytes. "" is a match.
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
	if fi.Size() != d.Size {
		return "modified", fmt.Sprintf("size %d, listed %d", fi.Size(), d.Size)
	}
	f, err := util.OpenRegularFile(cur)
	if err != nil {
		return "modified", "could not be read"
	}
	defer f.Close()
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

// firstCommitOwed is the keyed load's checkFresh without the key: with no pointer,
// the folder is a new archive only when units/ and inventory/ are empty, chunks/
// holds at most an interrupted first commit's chunk-shaped files and temps (counted
// as leftovers), and no search index or go-back history says it was used.
func (k *keylessCheck) firstCommitOwed() bool {
	for _, name := range []string{"search.db", ".mailarchive-history.jsonl"} {
		if exists(k.out, name) {
			return false
		}
	}
	for _, d := range []string{"units", InventoryDir} {
		dir := filepath.Join(k.out, d)
		if ok, err := realDir(dir); err != nil {
			return false
		} else if ok {
			if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
				return false
			}
		}
	}
	if _, err := realDir(filepath.Join(k.out, ChunksDir)); err != nil {
		return false
	}
	owed := true
	k.walk(ChunksDir, func(rel string) {
		k.c.Leftovers++
		if !filePath.MatchString(rel) && !tempName.MatchString(filepath.Base(rel)) {
			owed = false
		}
	})
	return owed
}

// unlisted counts the files under units/ and chunks/ the file list does not name
// (every one, with no file list). Links and other non-folders where a folder
// belongs are not followed: a link at a listed path is judged by judgeFile.
func (k *keylessCheck) unlisted() int {
	n := 0
	for _, d := range []string{"units", ChunksDir} {
		k.walk(d, func(rel string) {
			if !k.listed[rel] {
				n++
			}
		})
	}
	return n
}

// unnamedShards counts the files under inventory/ the top file does not name.
func (k *keylessCheck) unnamedShards(top Top) int {
	named := map[string]bool{}
	for _, ref := range top.Shards {
		named[ref.Name] = true
	}
	n := 0
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
