package container

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// base is a fixed clock for the tests' runs: run n starts at base + n hours.
var base = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func runAt(n int) time.Time { return base.Add(time.Duration(n) * time.Hour) }

// unitLister names each record's page unit by its revision's digest,
// units/<hh>/<first 32 hex of its sha256>, so every revision has its own name: the
// shape the unit store's lister gives, without deriving keyed names.
func unitLister(entries map[string]state.Record) (map[string]Digest, error) {
	out := map[string]Digest{}
	for _, r := range entries {
		if r.Sealed == nil || r.Sealed.HTML == nil {
			continue
		}
		var d Digest
		if _, err := hex.Decode(d.SHA256[:], []byte(r.Sealed.HTML.SHA256)); err != nil {
			return nil, err
		}
		d.Size = r.Sealed.HTML.Size
		name := r.Sealed.HTML.SHA256[:32]
		out["units/"+name[:2]+"/"+name] = d
	}
	return out, nil
}

func opts(run int) LoadOptions {
	return LoadOptions{Key: keys[0], RunStart: runAt(run), Units: unitLister}
}

// open loads out's record for run, failing the test on error.
func open(t *testing.T, out string, o LoadOptions) (*state.Manifest, *Store) {
	t.Helper()
	m, s, err := LoadManifest(out, o)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return m, s
}

// fresh is a new archive folder whose first commit has been made.
func fresh(t *testing.T) string {
	t.Helper()
	out := t.TempDir()
	m, s := open(t, out, opts(0))
	if !s.Fresh() {
		t.Fatal("an empty folder must load as a fresh archive")
	}
	if err := s.Init(m); err != nil {
		t.Fatal(err)
	}
	return out
}

func unitOf(i int) string { return fmt.Sprintf("%02x%030x", i%256, i) }

func rec(i int) state.Record {
	at := base.Add(time.Duration(i) * time.Minute)
	h := sha256.Sum256([]byte(fmt.Sprintf("unit %d rev 1", i)))
	sum := hex.EncodeToString(h[:])
	return state.Record{Path: fmt.Sprintf("s/Inbox/m%d.html", i), Folder: "Inbox", ExportedAt: at, FirstSeen: at, FirstFolder: "Inbox", Present: true,
		Unit: unitOf(i), Sealed: &state.SealedSet{HTML: &state.SealedUnit{Rev: 1, SHA256: sum, Size: int64(800 + i)}}}
}

func key(i int) string { return fmt.Sprintf("s\x00msg-%d", i) }

// chunkFiles lists every chunk file under out.
func chunkFiles(t *testing.T, out string) map[string]bool {
	t.Helper()
	files := map[string]bool{}
	filepath.WalkDir(filepath.Join(out, ChunksDir), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(out, p)
			files[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	return files
}

func newIn(after, before map[string]bool) int {
	n := 0
	for f := range after {
		if !before[f] {
			n++
		}
	}
	return n
}

// covers: MA-752, R37
// A fresh archive's first commit writes the envelope, an empty generation-1 pointer
// and its mirror, the sealed empty record and inventory 1 — before any unit (P2b-C3);
// records saved through the Store load back exactly, stores, gaps and collapsed
// tokens included, and the plaintext manifest file is never written.
func TestASealedRecordRoundTrips(t *testing.T) {
	out := fresh(t)
	for _, name := range []string{EnvelopeName, PointerName, MirrorName, InventoryName} {
		if !exists(out, name) {
			t.Fatalf("the first commit did not write %s", name)
		}
	}
	env, _ := os.ReadFile(filepath.Join(out, EnvelopeName))
	if strings.Contains(string(env), "entries") || !strings.Contains(string(env), `"encrypted": true`) {
		t.Fatalf("the record file is not the envelope: %s", env)
	}
	m, s := open(t, out, opts(1))
	if s.Fresh() || s.Generation() != 1 || m.Len() != 0 {
		t.Fatalf("after the first commit: fresh %v, generation %d, %d records", s.Fresh(), s.Generation(), m.Len())
	}
	for i := 0; i < 40; i++ {
		m.Add(key(i), rec(i))
	}
	m.Stores = map[string]string{"mailbox:a@example.com": "s"}
	m.CollapsedTokens = map[string]bool{"s": true}
	gap := state.CaptureGap{Token: "s", Folder: "Inbox", Scope: state.GapScopeItem, Locator: "graph:x", Reason: "fetch_failed", Class: state.ClassFillable, Subject: "a subject", Detail: "a detail", Runs: 1}
	m.CaptureGaps = map[string]state.CaptureGap{state.GapKey("s", state.GapScopeItem, "graph:x"): gap}
	// One record carrying the fields a bare one leaves empty (the adversarial pass, FID-6).
	rich := rec(77)
	rich.Missing, rich.Terminal, rich.Unresolved = []string{"body"}, []string{"attachment 2"}, []string{"cid:x"}
	rich.PhysID, rich.AltPhysIDs, rich.LastSeen = "phys-77", []string{"phys-alt"}, base.Add(-72*time.Hour)
	rich.Fixity = &state.Fixity{}
	m.Add(key(77), rich)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	m2, s2 := open(t, out, opts(2))
	if s2.Generation() != 2 {
		t.Fatalf("generation %d after one more save", s2.Generation())
	}
	if !reflect.DeepEqual(m2.All(), m.All()) {
		t.Fatal("the records did not round-trip")
	}
	if !reflect.DeepEqual(m2.Stores, m.Stores) || !reflect.DeepEqual(m2.CollapsedTokens, m.CollapsedTokens) || !reflect.DeepEqual(m2.CaptureGaps, m.CaptureGaps) {
		t.Fatalf("stores, collapsed tokens or gaps did not round-trip:\n %v %v\n %+v\nwant\n %+v", m2.Stores, m2.CollapsedTokens, m2.CaptureGaps, m.CaptureGaps)
	}
}

// covers: MA-752, R37
// Only what changed is re-sealed, each section twice (P2b-C5, C12): one new record
// re-seals the header pair and one bucket pair; a save with no change re-seals
// nothing; and every manifest chunk is mirrored — two files per section.
func TestOnlyChangedSectionsAreResealed(t *testing.T) {
	out := fresh(t)
	m, _ := open(t, out, opts(1))
	for i := 0; i < 30; i++ {
		m.Add(key(i), rec(i))
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	m, s := open(t, out, opts(2))
	byPos := map[uint32]int{}
	for _, e := range s.ptr.Manifest {
		byPos[e.I]++
	}
	for pos, n := range byPos {
		if n != 2 {
			t.Fatalf("section %d has %d copies, want 2", pos, n)
		}
	}
	before := chunkFiles(t, out)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if n := newIn(chunkFiles(t, out), before); n != 0 {
		t.Fatalf("a save with no change wrote %d chunks", n)
	}
	before = chunkFiles(t, out)
	m.Add(key(99), rec(99))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if n := newIn(chunkFiles(t, out), before); n != 2 {
		t.Fatalf("one new record wrote %d chunks, want exactly one bucket's two copies (the header is unchanged)", n)
	}
}

// covers: MA-752, R37
// LastSeen is sealed relative to the header (P2b-C12): a Microsoft 365 run that
// restamps every record it observes changes no bucket mid-walk, and the commit that
// ends the walk re-seals only the header pair; the records load back last seen at
// that walk. A record last seen when captured, or at another time, keeps its time.
func TestAScheduledRunThatOnlyRestampsLastSeenResealsOnlyTheHeader(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	walk1 := runAt(1)
	for i := 0; i < 30; i++ {
		r := rec(i)
		r.LastSeen = walk1
		m.Add(key(i), r)
	}
	captured := rec(50)
	captured.LastSeen = captured.ExportedAt
	m.Add(key(50), captured)
	odd := rec(51)
	odd.LastSeen = base.Add(-48 * time.Hour)
	m.Add(key(51), odd)
	s.WalkDone("s", walk1)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	m, s = open(t, out, opts(2))
	walk2 := runAt(2)
	s.SetRunStamp(walk2)
	before := chunkFiles(t, out)
	for i := 0; i < 30; i++ {
		m.TouchSeen(key(i), walk2)
	}
	if err := m.Save(); err != nil { // a mid-walk checkpoint
		t.Fatal(err)
	}
	if n := newIn(chunkFiles(t, out), before); n != 0 {
		t.Fatalf("a mid-walk checkpoint that only restamped LastSeen wrote %d chunks", n)
	}
	before = chunkFiles(t, out)
	s.WalkDone("s", walk2)
	if err := m.Save(); err != nil { // the commit that ends the walk
		t.Fatal(err)
	}
	if n := newIn(chunkFiles(t, out), before); n != 2 {
		t.Fatalf("the walk's end wrote %d chunks, want the header pair only", n)
	}
	m, _ = open(t, out, opts(3))
	for i := 0; i < 30; i++ {
		if r, _ := m.Get(key(i)); !r.LastSeen.Equal(walk2) {
			t.Fatalf("record %d loads last seen %v, want the walk's %v", i, r.LastSeen, walk2)
		}
	}
	if r, _ := m.Get(key(50)); !r.LastSeen.Equal(captured.ExportedAt) {
		t.Fatalf("a captured record loads last seen %v", r.LastSeen)
	}
	if r, _ := m.Get(key(51)); !r.LastSeen.Equal(odd.LastSeen) {
		t.Fatalf("a record seen at another time loads last seen %v", r.LastSeen)
	}
}

// covers: MA-752, R37
// Each pointer copy and each manifest chunk copy covers for the other (P2b-C4, C5):
// with the primary pointer missing or damaged the mirror loads; the newest generation
// that authenticates wins; one damaged copy of a section loads from the other and the
// next commit re-seals that section. Both pointer copies damaged, or both copies of a
// section, refuse — the second naming the chunk and the date to restore it from.
func TestMirrorsCoverForEachOther(t *testing.T) {
	out := fresh(t)
	m, _ := open(t, out, opts(1))
	m.Add(key(1), rec(1))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	ptr, _ := os.ReadFile(filepath.Join(out, PointerName))

	os.Remove(filepath.Join(out, PointerName))
	if _, s := open(t, out, opts(2)); s.Generation() != 2 {
		t.Fatalf("with the primary missing the mirror loads generation %d", s.Generation())
	}
	bad := append([]byte(nil), ptr...)
	bad[len(bad)-1] ^= 1
	os.WriteFile(filepath.Join(out, PointerName), bad, 0o600)
	if _, s := open(t, out, opts(2)); s.Generation() != 2 {
		t.Fatal("with the primary damaged the mirror must load")
	}
	os.WriteFile(filepath.Join(out, MirrorName), bad, 0o600)
	_, _, err := LoadManifest(out, opts(2))
	if !errors.Is(err, ErrRecord) {
		t.Fatalf("both pointers damaged: want ErrRecord, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(out, PointerName, MirrorName, "nothing was changed"))
	os.WriteFile(filepath.Join(out, PointerName), ptr, 0o600)
	os.WriteFile(filepath.Join(out, MirrorName), ptr, 0o600)

	_, s := open(t, out, opts(2))
	first := s.ptr.Manifest[0] // one copy of the header
	flip := func(e Entry) {
		p := filepath.Join(out, filepath.FromSlash(e.Name.Path()))
		b, _ := os.ReadFile(p)
		b[0] ^= 1
		os.WriteFile(p, b, 0o600)
	}
	flip(first)
	m, s = open(t, out, opts(3))
	if !s.reseal[first.I] {
		t.Fatal("a section loaded from its second copy must be marked for re-sealing")
	}
	before := chunkFiles(t, out)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if n := newIn(chunkFiles(t, out), before); n != 2 {
		t.Fatalf("the commit after a damaged copy wrote %d chunks, want that section's two", n)
	}

	out2 := fresh(t)
	m, s = open(t, out2, opts(1))
	m.Add(key(1), rec(1))
	m.Save()
	_, s = open(t, out2, opts(2))
	for _, e := range s.ptr.Manifest {
		if e.I == posHeader {
			p := filepath.Join(out2, filepath.FromSlash(e.Name.Path()))
			fi, _ := os.Stat(p)
			b, _ := os.ReadFile(p)
			b[0] ^= 1
			os.WriteFile(p, b, 0o600)
			// Bit rot keeps a file's time; rewriting it here would not, and the refusal
			// dates the damaged file by it (otherwise the test fails once the clock's
			// UTC date passes the run's).
			os.Chtimes(p, fi.ModTime(), fi.ModTime())
		}
	}
	_, _, err = LoadManifest(out2, opts(3))
	if !errors.Is(err, ErrRecord) {
		t.Fatalf("both copies of a section damaged: want ErrRecord, got %v", err)
	}
	rc, msg = refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(out2, "chunks/", runAt(1).Format("2006-01-02"), "nothing was changed"))
}

// covers: MA-752, R37
// Of the two pointer copies, the newest generation that authenticates wins, whichever
// copy holds it: a mirror one save behind loses to the primary, and a primary one
// save behind (its rename lost, the mirror's landed) loses to the mirror.
func TestTheNewestPointerCopyWins(t *testing.T) {
	out := fresh(t)
	saveN(t, out, 1, 0, 2)
	older, _ := os.ReadFile(filepath.Join(out, PointerName))
	saveN(t, out, 2, 2, 2)
	newer, _ := os.ReadFile(filepath.Join(out, PointerName))
	for label, pair := range map[string][2][]byte{"mirror behind": {newer, older}, "primary behind": {older, newer}} {
		os.WriteFile(filepath.Join(out, PointerName), pair[0], 0o600)
		os.WriteFile(filepath.Join(out, MirrorName), pair[1], 0o600)
		m, s := open(t, out, opts(3))
		if s.Generation() != 3 || m.Len() != 4 {
			t.Fatalf("%s: loaded generation %d with %d records, want 3 and 4", label, s.Generation(), m.Len())
		}
	}
}

// covers: MA-752, R37
// The sweep's other two keeps: a chunk younger than the run's start is never removed
// (another run's in-flight write, or a clock that moved back), and a chunk the
// pointer names is kept even when the file list is a save behind and does not list it
// (an inventory write that failed: the commit landed, its list did not).
func TestTheSweepKeepsYoungFilesAndWhatThePointerNames(t *testing.T) {
	out := fresh(t)
	saveN(t, out, 1, 0, 3)
	top, _ := os.ReadFile(filepath.Join(out, InventoryName))
	saveN(t, out, 2, 3, 3)
	// The second commit's file list "failed": the top file is the first's again. The
	// pointer saw that one land (inv), so this is I < P, I >= inv: proceed.
	os.WriteFile(filepath.Join(out, InventoryName), top, 0o600)
	m, s := open(t, out, opts(3))
	if !s.SweepAllowed() {
		t.Fatal("fixture: a file list one save behind still allows the sweep")
	}
	named := []string{}
	for _, e := range s.ptr.Manifest {
		named = append(named, e.Name.Path())
		os.Chtimes(filepath.Join(out, filepath.FromSlash(e.Name.Path())), runAt(0), runAt(0))
	}
	young := "chunks/cd/cd" + strings.Repeat("3", 30)
	os.MkdirAll(filepath.Join(out, "chunks", "cd"), 0o755)
	os.WriteFile(filepath.Join(out, filepath.FromSlash(young)), []byte("in flight"), 0o600)
	os.Chtimes(filepath.Join(out, filepath.FromSlash(young)), runAt(4), runAt(4))
	if _, err := s.SweepContainers(runAt(3)); err != nil {
		t.Fatal(err)
	}
	for _, p := range named {
		if !exists(out, p) {
			t.Fatalf("the sweep removed %s, which the pointer names", p)
		}
	}
	if !exists(out, young) {
		t.Fatal("the sweep removed a chunk younger than the run's start")
	}
	_ = m
}

// snapshotRecord copies the record files a restore of the record puts back: the
// envelope, both pointers and chunks/ — never the inventory.
func snapshotRecord(t *testing.T, out string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	for _, name := range []string{EnvelopeName, PointerName, MirrorName} {
		b, _ := os.ReadFile(filepath.Join(out, name))
		snap[name] = b
	}
	for f := range chunkFiles(t, out) {
		b, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(f)))
		snap[f] = b
	}
	return snap
}

func restore(t *testing.T, out string, snap map[string][]byte) {
	t.Helper()
	for name, b := range snap {
		p := filepath.Join(out, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func saveN(t *testing.T, out string, run, from, n int) {
	t.Helper()
	m, _ := open(t, out, opts(run))
	for i := from; i < from+n; i++ {
		m.Add(key(i), rec(i))
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-753, R37
// The F12 rule (P2b-C7, §3): a record restored one save older than its file list is
// "the last save did not finish, or an older copy was restored"; more than one save
// older is "older than its file list"; an older file list under a pointer that saw a
// newer one is refused the same way; a missing file list once one existed is refused;
// and each refusal names the archive, writes nothing and deletes nothing. A missing
// file list that never landed (the pointer saw none) proceeds with no sweep.
func TestTheF12RuleRefusesARestoredOlderRecord(t *testing.T) {
	check := func(t *testing.T, out string, want error, names ...string) {
		t.Helper()
		before := treeBytes(t, out)
		_, _, err := LoadManifest(out, opts(9))
		if !errors.Is(err, want) {
			t.Fatalf("want %v, got %v", want, err)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(append([]string{out, "nothing was changed"}, names...)...),
			assure.NoSideEffect(func() bool { return reflect.DeepEqual(before, treeBytes(t, out)) }))
	}
	t.Run("one save older", func(t *testing.T) {
		out := fresh(t)
		saveN(t, out, 1, 0, 3)
		snap := snapshotRecord(t, out)
		saveN(t, out, 2, 3, 3)
		restore(t, out, snap)
		check(t, out, ErrUnfinishedSave)
	})
	t.Run("two saves older", func(t *testing.T) {
		out := fresh(t)
		saveN(t, out, 1, 0, 3)
		snap := snapshotRecord(t, out)
		saveN(t, out, 2, 3, 3)
		saveN(t, out, 3, 6, 3)
		restore(t, out, snap)
		check(t, out, ErrRestoredOlder, "older copy")
	})
	t.Run("an older file list", func(t *testing.T) {
		out := fresh(t)
		saveN(t, out, 1, 0, 3)
		top, _ := os.ReadFile(filepath.Join(out, InventoryName))
		saveN(t, out, 2, 3, 3)
		saveN(t, out, 3, 6, 3)
		os.WriteFile(filepath.Join(out, InventoryName), top, 0o600)
		check(t, out, ErrRestoredOlder, "file list")
	})
	t.Run("a lost file list", func(t *testing.T) {
		out := fresh(t)
		saveN(t, out, 1, 0, 3)
		saveN(t, out, 2, 3, 3)
		os.Remove(filepath.Join(out, InventoryName))
		check(t, out, ErrInventoryDamaged, "file list")
	})
	t.Run("a file list that never landed", func(t *testing.T) {
		out := fresh(t) // inventory 1 under a pointer that saw none (inv 0)
		os.Remove(filepath.Join(out, InventoryName))
		_, s := open(t, out, opts(9))
		if s.SweepAllowed() {
			t.Fatal("with no file list the sweep must not run")
		}
	})
}

// covers: MA-753, R37
// RecordedGen (A25, P2b-C8): an archive older than this computer's last save, with
// no file its record or file list does not name (a clean restore, a rotated drive),
// proceeds and says it was restored; with such a file (a merge-restore's signature)
// it is refused, writing nothing.
func TestRecordedGenerationRefusesOnlyAMergeRestore(t *testing.T) {
	out := fresh(t)
	saveN(t, out, 1, 0, 3)
	o := opts(2)
	o.RecordedGen = 7
	_, s := open(t, out, o)
	if len(s.Notices) == 0 || !strings.Contains(strings.Join(s.Notices, " "), "restored to an older copy") {
		t.Fatalf("a clean restore must proceed and say so: %v", s.Notices)
	}
	stray := filepath.Join(out, "units", "ee", "ee"+strings.Repeat("0", 30))
	os.MkdirAll(filepath.Dir(stray), 0o755)
	os.WriteFile(stray, []byte("a newer message's unit"), 0o600)
	before := treeBytes(t, out)
	_, _, err := LoadManifest(out, o)
	if !errors.Is(err, ErrRestoredOlder) {
		t.Fatalf("a merge-restore: want ErrRestoredOlder, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(out, "older than the last backup this computer made"),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(before, treeBytes(t, out)) }))
}

// treeBytes is every file under out with its bytes.
func treeBytes(t *testing.T, out string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(out, p)
			snap[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	return snap
}

// covers: MA-752, R37
// The strict keyed load (P2b-C4, C11): a plaintext manifest at the record's name is
// not a sealed record and is refused, never migrated; an envelope naming another
// archive, or from a newer MailArchive, is refused; an envelope that is not JSON at
// all beside an authentic pointer is healed by the next commit; an envelope with no
// pointer and empty folders resumes the first commit; with sealed files present and
// no pointer it is refused; and a record sealed under another archive's key is
// refused. Every refusal names the archive and changes nothing.
func TestTheKeyedLoadIsStrict(t *testing.T) {
	refused := func(t *testing.T, out string, o LoadOptions, want error) {
		t.Helper()
		before := treeBytes(t, out)
		_, _, err := LoadManifest(out, o)
		if !errors.Is(err, want) {
			t.Fatalf("want %v, got %v", want, err)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(out),
			assure.NoSideEffect(func() bool { return reflect.DeepEqual(before, treeBytes(t, out)) }))
	}
	t.Run("a plaintext manifest", func(t *testing.T) {
		out := t.TempDir()
		os.WriteFile(filepath.Join(out, EnvelopeName), []byte(`{"version":6,"entries":{}}`), 0o600)
		refused(t, out, opts(1), ErrNotSealed)
	})
	t.Run("a plaintext manifest larger than any envelope", func(t *testing.T) {
		out := t.TempDir()
		big := `{"version":6,"entries":{"k":{"path":"` + strings.Repeat("x", 2*maxEnvelopeSize) + `"}}}`
		os.WriteFile(filepath.Join(out, EnvelopeName), []byte(big), 0o600)
		refused(t, out, opts(1), ErrNotSealed)
	})
	// The envelope decides only where no pointer authenticates: beside an authentic
	// pointer, an envelope naming another archive, or reading as newer, is damage the
	// pointer outranks — healed, never a refusal (the adversarial pass, CRIT-1).
	t.Run("another archive's envelope and no record", func(t *testing.T) {
		out := t.TempDir()
		b, _ := vault.NewManifestEnvelope(keys[1].ArchiveID(), PointerName).Marshal()
		os.WriteFile(filepath.Join(out, EnvelopeName), b, 0o600)
		refused(t, out, opts(1), ErrRecord)
	})
	t.Run("a newer envelope and no record", func(t *testing.T) {
		out := t.TempDir()
		os.WriteFile(filepath.Join(out, EnvelopeName), []byte(`{"version":101,"encrypted":true}`), 0o600)
		refused(t, out, opts(1), ErrNewerFormat)
	})
	for label, body := range map[string][]byte{
		"another archive's envelope beside an authentic pointer": func() []byte {
			b, _ := vault.NewManifestEnvelope(keys[1].ArchiveID(), PointerName).Marshal()
			return b
		}(),
		"a newer envelope beside an authentic pointer": []byte(`{"version":101,"encrypted":true}`),
	} {
		t.Run(label, func(t *testing.T) {
			out := fresh(t)
			os.WriteFile(filepath.Join(out, EnvelopeName), body, 0o600)
			m, s := open(t, out, opts(1))
			if len(s.Notices) == 0 {
				t.Fatal("the heal must be noted")
			}
			if err := m.Save(); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(out, EnvelopeName))
			if !strings.Contains(string(b), keys[0].ArchiveID().String()) || !strings.Contains(string(b), `"version": 100`) {
				t.Fatalf("the envelope was not rewritten as this archive's: %s", b)
			}
		})
	}
	t.Run("another archive's key", func(t *testing.T) {
		out := fresh(t)
		b, _ := vault.NewManifestEnvelope(keys[1].ArchiveID(), PointerName).Marshal()
		os.WriteFile(filepath.Join(out, EnvelopeName), b, 0o600)
		o := opts(1)
		o.Key = keys[1]
		refused(t, out, o, ErrRecord)
	})
	t.Run("sealed files and no pointer", func(t *testing.T) {
		out := fresh(t)
		saveN(t, out, 1, 0, 2)
		os.Remove(filepath.Join(out, PointerName))
		os.Remove(filepath.Join(out, MirrorName))
		refused(t, out, opts(2), ErrRecord)
	})
	t.Run("an unreadable envelope is healed", func(t *testing.T) {
		out := fresh(t)
		os.WriteFile(filepath.Join(out, EnvelopeName), []byte("\x00garbage"), 0o600)
		m, s := open(t, out, opts(1))
		if len(s.Notices) == 0 {
			t.Fatal("the heal must be noted")
		}
		if err := m.Save(); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join(out, EnvelopeName))
		if !strings.Contains(string(b), `"encrypted": true`) {
			t.Fatalf("the envelope was not rewritten: %q", b)
		}
	})
	t.Run("an interrupted first commit resumes", func(t *testing.T) {
		out := t.TempDir()
		b, _ := vault.NewManifestEnvelope(keys[0].ArchiveID(), PointerName).Marshal()
		os.WriteFile(filepath.Join(out, EnvelopeName), b, 0o600)
		if _, s := open(t, out, opts(1)); !s.Fresh() {
			t.Fatal("an envelope with no pointer and empty folders is a first commit to resume")
		}
	})
	// A newer-format pointer decides only when nothing outranks it (CRIT-2): both
	// copies newer, or a newer copy at a later generation than the authentic one, ask
	// for an upgrade; one copy reading as newer at the authentic copy's generation is
	// damage the other copy covers for.
	newerCopy := func(t *testing.T, out, name string, gen uint64) {
		t.Helper()
		p := filepath.Join(out, name)
		b, _ := os.ReadFile(p)
		b[5] = 2
		binary.BigEndian.PutUint64(b[6:14], gen)
		os.WriteFile(p, b, 0o600)
	}
	t.Run("both pointers newer", func(t *testing.T) {
		out := fresh(t)
		newerCopy(t, out, PointerName, 1)
		newerCopy(t, out, MirrorName, 1)
		refused(t, out, opts(1), ErrNewerFormat)
	})
	t.Run("a newer pointer at a later generation", func(t *testing.T) {
		out := fresh(t)
		newerCopy(t, out, PointerName, 2)
		refused(t, out, opts(1), ErrNewerFormat)
	})
	t.Run("one pointer copy reading as newer is damage", func(t *testing.T) {
		out := fresh(t)
		newerCopy(t, out, MirrorName, 1)
		if _, s := open(t, out, opts(1)); s.Generation() != 1 || len(s.Notices) == 0 {
			t.Fatalf("the authentic primary must load, noted: gen %d, %v", s.Generation(), s.Notices)
		}
	})
	// An inventory format change comes with a pointer format change; a top line that
	// alone reads as newer is a damaged file list (refused once one has landed).
	t.Run("a file list reading as newer", func(t *testing.T) {
		out := fresh(t)
		saveN(t, out, 1, 0, 2)
		os.WriteFile(filepath.Join(out, InventoryName), []byte("mailarchive-inventory 2\n"), 0o600)
		refused(t, out, opts(2), ErrInventoryDamaged)
	})
}

// covers: MA-752, R37
// The start sweep (P2b-C9): a chunk the pointer does not name and the file list does
// not list as live, older than the run's start, is removed — a crashed commit's orphan
// or a chunk superseded during the last run — while every named chunk stays; a shard
// file the top file does not name is removed the same way; with no file list nothing
// is removed. Nothing is deleted during a run.
func TestTheStartSweepRemovesOnlyWhatNothingNames(t *testing.T) {
	out := fresh(t)
	saveN(t, out, 1, 0, 5)
	beforeRun := chunkFiles(t, out)
	saveN(t, out, 2, 5, 5) // supersedes some chunks; nothing deleted during the run
	for f := range beforeRun {
		if !exists(out, f) {
			t.Fatalf("a chunk was deleted during a run: %s", f)
		}
	}
	orphan := "chunks/ab/ab" + strings.Repeat("1", 30)
	os.MkdirAll(filepath.Join(out, "chunks", "ab"), 0o755)
	os.WriteFile(filepath.Join(out, filepath.FromSlash(orphan)), []byte("a crashed commit's chunk"), 0o600)
	os.Chtimes(filepath.Join(out, filepath.FromSlash(orphan)), runAt(2), runAt(2))
	oldShard := "inventory/zz-not-named"
	os.WriteFile(filepath.Join(out, filepath.FromSlash(oldShard)), []byte("x"), 0o600)
	os.Chtimes(filepath.Join(out, filepath.FromSlash(oldShard)), runAt(2), runAt(2))

	m, s := open(t, out, opts(3))
	named := map[string]bool{}
	for _, e := range s.ptr.Manifest {
		named[e.Name.Path()] = true
	}
	n, err := s.SweepContainers(runAt(3))
	if err != nil || n == 0 {
		t.Fatalf("sweep removed %d, %v", n, err)
	}
	if exists(out, orphan) || exists(out, oldShard) {
		t.Fatal("the orphan chunk or the unnamed shard file survived the sweep")
	}
	for f := range named {
		if !exists(out, f) {
			t.Fatalf("the sweep removed a named chunk %s", f)
		}
	}
	for _, ref := range s.inv.Shards {
		if !exists(out, ref.Name) {
			t.Fatalf("the sweep removed %s, a shard the top file names", ref.Name)
		}
	}
	if _, s4, err := LoadManifest(out, opts(4)); err != nil || !s4.SweepAllowed() {
		t.Fatalf("after the sweep the record and its whole file list must load: %v", err)
	}
	_ = m

	out2 := fresh(t)
	os.Remove(filepath.Join(out2, InventoryName))
	stray := filepath.Join(out2, "chunks", "cd", "cd"+strings.Repeat("2", 30))
	os.MkdirAll(filepath.Dir(stray), 0o755)
	os.WriteFile(stray, []byte("x"), 0o600)
	os.Chtimes(stray, runAt(0), runAt(0))
	_, s2 := open(t, out2, opts(5))
	if n, _ := s2.SweepContainers(runAt(5)); n != 0 {
		t.Fatalf("with no file list the sweep removed %d files", n)
	}
}

// covers: MA-752, R37
// A commit re-proves the archive lock right before the commit point: with the lock
// lost nothing is renamed — both pointers keep their bytes.
func TestALostLockRenamesNothing(t *testing.T) {
	out := fresh(t)
	o := opts(1)
	o.StillHeld = func() error { return errors.New("the lock file was replaced") }
	m, _ := open(t, out, o)
	m.Add(key(1), rec(1))
	ptr, _ := os.ReadFile(filepath.Join(out, PointerName))
	mir, _ := os.ReadFile(filepath.Join(out, MirrorName))
	if err := m.Save(); !errors.Is(err, ErrLockLost) {
		t.Fatalf("want ErrLockLost, got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(out, PointerName))
	afterMir, _ := os.ReadFile(filepath.Join(out, MirrorName))
	if string(after) != string(ptr) || string(afterMir) != string(mir) {
		t.Fatal("a commit whose lock was lost renamed a pointer")
	}
}

// covers: MA-752, R37
// File times (P2b-C10): chunks and shards take the run's start; the pointer pair and
// the top file take run start + 2 s per commit, and never repeat or go back across runs.
func TestCommitFileTimesAreNumberedAndNeverRepeat(t *testing.T) {
	out := fresh(t)
	mt := func(name string) time.Time {
		fi, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return fi.ModTime().UTC()
	}
	m, _ := open(t, out, opts(1))
	m.Add(key(1), rec(1))
	m.Save()
	if got := mt(PointerName); !got.Equal(runAt(1).Add(2 * time.Second)) {
		t.Fatalf("first commit's pointer mtime %v", got)
	}
	m.Add(key(2), rec(2))
	m.Save()
	if got := mt(PointerName); !got.Equal(runAt(1).Add(4*time.Second)) || !mt(MirrorName).Equal(got) || !mt(InventoryName).Equal(got) {
		t.Fatalf("second commit's pointer, mirror, top mtimes %v %v %v", got, mt(MirrorName), mt(InventoryName))
	}
	for f := range chunkFiles(t, out) {
		if fi, _ := os.Stat(filepath.Join(out, filepath.FromSlash(f))); !fi.ModTime().UTC().Equal(runAt(1)) && !fi.ModTime().UTC().Equal(runAt(0)) {
			t.Fatalf("chunk %s mtime %v, want a run's start", f, fi.ModTime())
		}
	}
	prev := mt(PointerName)
	o := opts(1) // a run that starts at the same time as the last (a clock that did not move)
	m, _ = open(t, out, o)
	m.Add(key(3), rec(3))
	m.Save()
	if got := mt(PointerName); !got.After(prev) {
		t.Fatalf("a later run's pointer mtime %v is not after %v", got, prev)
	}
}

// covers: MA-753, R37
// The file list is exactly what the pointer and the records name (live) plus what was
// superseded during this run (retained): rebuilt from the pointer's chunk tables, the
// records' sealed units and the files' digests, it matches line for line; a shard that
// is damaged under a current top file is tolerated with no sweep and every shard
// rewritten at the next commit.
func TestTheFileListIsWhatThePointerAndRecordsName(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	for i := 0; i < 8; i++ {
		m.Add(key(i), rec(i))
	}
	m.Save()
	prevChunks := map[string]bool{}
	for _, e := range s.ptr.Manifest {
		prevChunks[e.Name.Path()] = true
	}
	oldUnits, _ := unitLister(map[string]state.Record{key(0): rec(0)})
	m.Add(key(8), rec(8))
	// Re-export record 0: its page's next revision supersedes the first (FID-8).
	r0 := rec(0)
	h := sha256.Sum256([]byte("unit 0 rev 2"))
	r0.Sealed = &state.SealedSet{HTML: &state.SealedUnit{Rev: 2, SHA256: hex.EncodeToString(h[:]), Size: 900}}
	m.Add(key(0), r0)
	m.Save()
	live, _ := s.liveFiles(state.View{Entries: m.All()})
	listed := map[string]FileState{}
	for prefix, b := range s.shards {
		sh, err := ParseShard(prefix, b)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range sh.Files {
			listed[f.Path] = f.State
			if f.State == Live {
				if d, ok := live[f.Path]; !ok || d != f.Digest {
					t.Fatalf("%s is listed live but the record and pointer do not name it so", f.Path)
				}
			}
		}
	}
	for p := range live {
		if listed[p] != Live {
			t.Fatalf("%s is named but not listed live (%v)", p, listed[p])
		}
	}
	retained := 0
	for p := range prevChunks {
		if _, still := live[p]; !still {
			if listed[p] != Retained {
				t.Fatalf("a chunk superseded this run is not listed retained: %s", p)
			}
			retained++
		}
	}
	if retained == 0 {
		t.Fatal("fixture: the second save superseded no chunk")
	}
	for p := range oldUnits {
		if listed[p] != Retained {
			t.Fatalf("the superseded unit revision %s is listed %v, want retained", p, listed[p])
		}
	}

	var shardName string
	for _, r := range s.inv.Shards {
		shardName = r.Name
		break
	}
	p := filepath.Join(out, filepath.FromSlash(shardName))
	b, _ := os.ReadFile(p)
	b[0] ^= 1
	os.WriteFile(p, b, 0o600)
	m, s = open(t, out, opts(2))
	if s.SweepAllowed() || !s.invTolerated {
		t.Fatal("a damaged shard under a current top file must be tolerated with no sweep")
	}
	m.Add(key(9), rec(9))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if _, s = open(t, out, opts(3)); !s.SweepAllowed() {
		t.Fatal("after the next commit the file list must be whole again")
	}
}
