package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// Restore to Thunderbird, slice R1: the read side (docs/design-restore-thunderbird.md
// §2, §3.4, §6.1, §6.4, §6.5). Fixtures are real archives (a real export) or real
// manifests over real files in a temp folder; nothing is mocked.

// covers: MA-1124, R40, R20
// FaithfulOriginal gives, record by record, exactly what extract emits or skips: the
// same bytes for a healthy original, and the same reason and counter for a link, an
// original longer than its recorded fixity, and a missing one. extract now emits
// through it, so the two cannot disagree.
func TestRestoreReadGivesWhatExtractGives(t *testing.T) {
	out := buildRawArchive(t, map[string][][]byte{
		"Alpha": {msg("Keep", "keep", "healthy"), msg("Link", "link", "symlinked"), msg("Rot", "rot", "will be rotted"), msg("Gone", "gone", "removed")},
		"Beta":  {msg("Other", "other", "another store")},
	})
	man, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Positive twin first: on the healthy archive every record's original is given,
	// byte for byte the archived .eml.
	for key, rec := range man.All() {
		orig, skip, ferr := FaithfulOriginal(out, rec, nil)
		if ferr != nil || skip != nil {
			t.Fatalf("healthy %q: skip=%+v err=%v", key, skip, ferr)
		}
		emlRel := strings.TrimSuffix(rec.Path, ".html") + ".eml"
		want, rerr := os.ReadFile(filepath.Join(out, filepath.FromSlash(emlRel)))
		if rerr != nil || !bytes.Equal(orig.Data, want) || orig.NoFixity {
			t.Fatalf("healthy %q: the bytes given are not the archived original (noFixity=%v, err=%v)", key, orig.NoFixity, rerr)
		}
	}

	archiveEML := collectFiles(t, out, ".eml")
	var linkRel, rotRel, goneRel string
	for rel := range archiveEML {
		switch l := strings.ToLower(rel); {
		case strings.Contains(l, "link"):
			linkRel = rel
		case strings.Contains(l, "rot"):
			rotRel = rel
		case strings.Contains(l, "gone"):
			goneRel = rel
		}
	}
	if linkRel == "" || rotRel == "" || goneRel == "" {
		t.Fatalf("could not find the link/rot/gone .eml among %v", keys(archiveEML))
	}
	secret := filepath.Join(tmpDir(t), "secret.txt")
	if werr := os.WriteFile(secret, []byte("TOP SECRET"), 0o600); werr != nil {
		t.Fatal(werr)
	}
	linkFull := filepath.Join(out, filepath.FromSlash(linkRel))
	if rerr := os.Remove(linkFull); rerr != nil {
		t.Fatal(rerr)
	}
	if serr := os.Symlink(secret, linkFull); serr != nil {
		t.Skipf("symlinks unavailable: %v", serr)
	}
	rotFull := filepath.Join(out, filepath.FromSlash(rotRel))
	if werr := os.WriteFile(rotFull, append(append([]byte{}, archiveEML[rotRel]...), "APPENDED"...), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if rerr := os.Remove(filepath.Join(out, filepath.FromSlash(goneRel))); rerr != nil {
		t.Fatal(rerr)
	}
	snap := snapshotArchive(t, out)

	dest := tmpDir(t)
	rep, xerr := Extract(out, FormatEML, dest, false, discard(), nil)
	if xerr != nil {
		t.Fatalf("extract: %v", xerr)
	}
	extracted := map[string]string{} // path → reason
	for _, s := range rep.Samples {
		extracted[s.Path] = s.Reason
	}
	counts := map[OriginalSkipKind]int{}
	given := 0
	for key, rec := range man.All() {
		orig, skip, ferr := FaithfulOriginal(out, rec, nil)
		if ferr != nil {
			t.Fatalf("%q: a hard error where a skip is due: %v", key, ferr)
		}
		if skip != nil {
			counts[skip.Kind]++
			if extracted[skip.Path] != skip.Reason {
				t.Errorf("%q: restore skips with %q, extract with %q", skip.Path, skip.Reason, extracted[skip.Path])
			}
			if bytes.Contains(orig.Data, []byte("TOP SECRET")) {
				t.Errorf("%q: the link was followed", key)
			}
			continue
		}
		given++
		emlRel := strings.TrimSuffix(rec.Path, ".html") + ".eml"
		got, rerr := os.ReadFile(filepath.Join(dest, filepath.FromSlash(emlRel)))
		if rerr != nil || !bytes.Equal(got, orig.Data) {
			t.Errorf("%q: restore's bytes are not the ones extract wrote (%v)", key, rerr)
		}
	}
	if given != rep.Emitted || given != 2 {
		t.Errorf("given=%d, extract emitted=%d, want 2 each (Keep and Other)", given, rep.Emitted)
	}
	if counts[SkipGate] != rep.SkippedGate || counts[SkipFixity] != rep.SkippedFixity || counts[SkipNoOriginal] != rep.SkippedNoEML ||
		counts[SkipGate] != 1 || counts[SkipFixity] != 1 || counts[SkipNoOriginal] != 1 {
		t.Errorf("skip kinds %v differ from extract's gate=%d fixity=%d no-eml=%d (want one of each)", counts, rep.SkippedGate, rep.SkippedFixity, rep.SkippedNoEML)
	}
	if after := snapshotArchive(t, out); after != snap {
		t.Errorf("the read side changed the archive")
	}
}

// restoreFixture is a manifest over real files: each added record's page and, unless
// noEML, its preserved original are written under out at the record's path.
type restoreFixture struct {
	t   *testing.T
	out string
	m   *state.Manifest
	n   int
}

func newRestoreFixture(t *testing.T) *restoreFixture {
	t.Helper()
	m, err := state.LoadFrom("restore-fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	return &restoreFixture{t: t, out: tmpDir(t), m: m}
}

type fixRec struct {
	source, store, folder string
	body                  string
	noEML, noFixity       bool
	recordedSize          int64 // overrides the recorded fixity size when set
	lastSeen              time.Time
	present               bool
}

func (f *restoreFixture) add(r fixRec) (key string) {
	f.t.Helper()
	tok := f.m.Token(r.source, r.store)
	f.n++
	stem := "m" + string(rune('a'+f.n%26)) + strings.Repeat("x", f.n/26)
	rel := tok + "/f/" + stem
	dir := filepath.Join(f.out, tok, "f")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stem+".html"), []byte("<p>page</p>"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	rec := state.Record{Path: rel + ".html", Folder: r.folder, LastSeen: r.lastSeen, Present: r.present}
	if !r.noEML {
		data := []byte("Subject: " + r.body + "\r\n\r\n" + r.body + "\r\n")
		if err := os.WriteFile(filepath.Join(dir, stem+".eml"), data, 0o644); err != nil {
			f.t.Fatal(err)
		}
		if !r.noFixity {
			sum := sha256.Sum256(data)
			d := &state.FileDigest{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
			if r.recordedSize > 0 {
				d.Size = r.recordedSize
			}
			rec.Fixity = &state.Fixity{EML: d}
		}
	}
	key = tok + "\x00" + stem
	f.m.Add(key, rec)
	return key
}

func (f *restoreFixture) tree() *ArchiveTree {
	f.t.Helper()
	tr, err := NewArchiveTree(f.m)
	if err != nil {
		f.t.Fatal(err)
	}
	return tr
}

func folderNames(sel *RestoreSelection) [][]string {
	var out [][]string
	for _, f := range sel.Folders {
		out = append(out, f.Names)
	}
	return out
}

// covers: MA-1124, R40, S70
// The plan's read side counts what a restore would hold from the manifest and one Lstat
// per record: restorable, size, no fixity, Outlook, no original, over the 2 GiB cap,
// and the deleted-after-backup count by the workspace's rule, shown only when the
// archive keeps that timeline. It lays the folders out as §3.4 says: a level per store
// only when more than one store has something to restore, "(no folder)" for records
// with none, parents before children, and a chosen folder takes its subfolders. It reads
// no message: with every original unreadable the counts are the same.
func TestRestoreSelectionCountsWithoutReadingAMessage(t *testing.T) {
	f := newRestoreFixture(t)
	f.add(fixRec{source: "/src/a", store: "a", folder: "Inbox", body: "one"})
	f.add(fixRec{source: "/src/a", store: "a", folder: "Inbox", body: "two", noFixity: true})
	f.add(fixRec{source: "/src/a", store: "a", folder: "Inbox/Receipts", body: "three"})
	f.add(fixRec{source: "/src/a", store: "a", folder: "", body: "four"})
	f.add(fixRec{source: "/src/a", store: "a", folder: "Sent", body: "missing", noEML: true})
	f.add(fixRec{source: "/src/a", store: "a", folder: "Big", body: "big", recordedSize: 3 << 30})
	f.add(fixRec{source: "/src/b", store: "b", folder: "Inbox", body: "five"})
	f.add(fixRec{source: "/src/p.pst", store: "p", folder: "Inbox", body: "outlook"})
	tr := f.tree()
	labels := map[string]string{}
	var aTok string
	for _, s := range tr.Stores() {
		labels[s.Token] = s.Label()
		if strings.HasPrefix(s.Token, "a") {
			aTok = s.Token
		}
	}

	whole, err := SelectForRestore(f.out, tr, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	size := func(body string) int64 { return int64(len("Subject: " + body + "\r\n\r\n" + body + "\r\n")) }
	wantBytes := size("one") + size("two") + size("three") + size("four") + size("five")
	if whole.Records != 8 || whole.Restorable != 5 || whole.Bytes != wantBytes || whole.NoFixity != 1 ||
		whole.PST != 1 || whole.NoOriginal != 1 || whole.TooBig != 1 {
		t.Fatalf("whole archive counts: %+v (want records 8, restorable 5, bytes %d, no-fixity 1, pst 1, no-original 1, too-big 1)", *whole, wantBytes)
	}
	a, b := labels[aTok], ""
	for tok, l := range labels {
		if strings.HasPrefix(tok, "b") {
			b = l
		}
	}
	want := [][]string{{a}, {a, RestoreNoFolder}, {a, "Inbox"}, {a, "Inbox", "Receipts"}, {b}, {b, "Inbox"}}
	if got := folderNames(whole); !reflect.DeepEqual(got, want) {
		t.Errorf("whole archive folders = %q, want %q (a level per store, no Sent, Big or Outlook folder)", got, want)
	}
	for _, fo := range whole.Folders {
		if len(fo.Names) == 2 && fo.Names[1] == "Inbox" && fo.Names[0] == a && (len(fo.Keys) != 2 || fo.Bytes != size("one")+size("two")) {
			t.Errorf("a's Inbox holds %d keys, %d bytes", len(fo.Keys), fo.Bytes)
		}
	}
	if whole.KeepsTimeline || whole.Deleted != 0 {
		t.Errorf("a local archive keeps no timeline: keeps=%v deleted=%d", whole.KeepsTimeline, whole.Deleted)
	}

	// One folder chosen: its subfolder comes with it, and one store has no level of its own.
	inbox, err := SelectForRestore(f.out, tr, []string{aTok + "/Inbox"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, w := folderNames(inbox), [][]string{{"Inbox"}, {"Inbox", "Receipts"}}; !reflect.DeepEqual(got, w) || inbox.Records != 3 || inbox.Restorable != 3 {
		t.Errorf("Inbox chosen: folders %q records %d restorable %d, want %q, 3, 3", got, inbox.Records, inbox.Restorable, w)
	}
	// A folder and its own parent chosen together: each record once.
	both, err := SelectForRestore(f.out, tr, []string{aTok + "/Inbox", aTok + "/Inbox/Receipts"}, false)
	if err != nil || both.Records != 3 {
		t.Errorf("parent and child chosen: records %d err %v, want 3", both.Records, err)
	}
	// A node id the tree does not hold is refused.
	if _, err := SelectForRestore(f.out, tr, []string{aTok + "/Nowhere"}, false); err == nil || !strings.Contains(err.Error(), "not in this archive") {
		t.Errorf("an unknown folder id was not refused: %v", err)
	}

	// No message is read: with every original unreadable the counts are the same.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		var emls []string
		_ = filepath.WalkDir(f.out, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".eml") {
				emls = append(emls, p)
			}
			return err
		})
		for _, p := range emls {
			if err := os.Chmod(p, 0); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			for _, p := range emls {
				_ = os.Chmod(p, 0o644)
			}
		})
		blind, err := SelectForRestore(f.out, tr, nil, false)
		if err != nil || !reflect.DeepEqual(blind, whole) {
			t.Errorf("with the originals unreadable the plan changed (err %v): %+v vs %+v", err, blind, whole)
		}
		if _, rerr := os.ReadFile(emls[0]); rerr == nil {
			t.Fatal("the chmod did not make the originals unreadable; the no-read check proves nothing")
		}
	}
}

// covers: MA-1124, R40, S70
// The deleted-after-backup count follows the workspace's rule: a record counts as gone
// when !(Present || LastSeen.IsZero()), and the line is shown only when some record of
// the archive has a non-zero LastSeen.
func TestRestoreDeletedCountFollowsTheWorkspaceRule(t *testing.T) {
	seen := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f := newRestoreFixture(t)
	f.add(fixRec{source: "mailbox:me@example.com", store: "me", folder: "Inbox", body: "kept", lastSeen: seen, present: true})
	f.add(fixRec{source: "mailbox:me@example.com", store: "me", folder: "Inbox", body: "deleted", lastSeen: seen, present: false})
	f.add(fixRec{source: "mailbox:me@example.com", store: "me", folder: "Inbox", body: "never-walked"})
	sel, err := SelectForRestore(f.out, f.tree(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !sel.KeepsTimeline || sel.Deleted != 1 || sel.Restorable != 3 {
		t.Errorf("Microsoft 365 archive: keeps=%v deleted=%d restorable=%d, want true, 1, 3 (the deleted one is restored too)", sel.KeepsTimeline, sel.Deleted, sel.Restorable)
	}
}

// covers: MA-1124, R40, R37
// An encrypted archive's manifest, loaded with its key, gives the same tree a plain one
// does, and its records are judged from the record alone (a sealed original named, its
// sealed size): no file is looked at, and a record that names none has no original.
func TestRestoreTreeFromASealedManifest(t *testing.T) {
	plain, sealed := newRestoreFixture(t), newRestoreFixture(t)
	for i, folder := range []string{"Inbox", "Inbox/Receipts", "Archive/2019"} {
		plain.add(fixRec{source: "/src/a", store: "a", folder: folder, body: "x"})
		tok := sealed.m.Token("/src/a", "a")
		rec := state.Record{Path: tok + "/f/s" + string(rune('a'+i)) + ".html", Folder: folder,
			Unit: strings.Repeat(string(rune('a'+i)), 32), Sealed: &state.SealedSet{EML: &state.SealedUnit{Rev: 1, SHA256: strings.Repeat("0", 64), Size: int64(100 * (i + 1))}}}
		sealed.m.Add(tok+"\x00s"+string(rune('a'+i)), rec)
	}
	tok := sealed.m.Token("/src/a", "a")
	sealed.m.Add(tok+"\x00none", state.Record{Path: tok + "/f/none.html", Folder: "Inbox"})

	pt, st := plain.tree(), sealed.tree()
	var pn, sn []string
	for _, n := range pt.Tree() {
		pn = append(pn, n.ID)
	}
	for _, n := range st.Tree() {
		sn = append(sn, n.ID)
	}
	if !reflect.DeepEqual(pn, sn) {
		t.Errorf("sealed tree %q differs from the plain tree %q", sn, pn)
	}
	sel, err := SelectForRestore("", st, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Restorable != 3 || sel.NoOriginal != 1 || sel.Bytes != 600 {
		t.Errorf("sealed counts: restorable %d no-original %d bytes %d, want 3, 1, 600", sel.Restorable, sel.NoOriginal, sel.Bytes)
	}
	want := [][]string{{"Archive"}, {"Archive", "2019"}, {"Inbox"}, {"Inbox", "Receipts"}}
	if got := folderNames(sel); !reflect.DeepEqual(got, want) {
		t.Errorf("sealed folders %q, want %q", got, want)
	}
}

// covers: MA-1088, R40, S69
// The restore's own caps (design §6.5, RT-7, lead's call L1): 10,000 folders and 300
// levels in one restore, whatever the tree builder allows for the owner's own archive.
// One over either is W-TOOMANY, before anything is written.
func TestRestoreFolderCaps(t *testing.T) {
	sealedFixture := func(folders []string, stores int) (*restoreFixture, *ArchiveTree) {
		f := newRestoreFixture(t)
		for s := 0; s < stores; s++ {
			src := "/src/" + string(rune('a'+s))
			tok := f.m.Token(src, string(rune('a'+s)))
			for i, folder := range folders {
				id := tok + "\x00" + string(rune('a'+s)) + "-" + itoa(i)
				unit := sha256.Sum256([]byte(id))
				f.m.Add(id, state.Record{Path: tok + "/f/" + itoa(i) + ".html", Folder: folder,
					Unit: hex.EncodeToString(unit[:16]), Sealed: &state.SealedSet{EML: &state.SealedUnit{Rev: 1, SHA256: strings.Repeat("0", 64), Size: 1}}})
			}
		}
		return f, f.tree()
	}
	flat := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "F" + itoa(i)
		}
		return out
	}
	deep := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = "d"
		}
		return strings.Join(parts, "/")
	}
	refused := func(name string, f *restoreFixture, tr *ArchiveTree, names ...string) {
		t.Helper()
		before := snapshotArchive(t, f.out)
		_, err := SelectForRestore(f.out, tr, nil, true)
		var tm *RestoreTooMany
		rc := 0
		if errors.As(err, &tm) {
			rc = 1
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(append(names, "nothing was written", "choose fewer folders")...),
			assure.NoSideEffect(func() bool { return snapshotArchive(t, f.out) == before }))
	}

	// Positive twins first: exactly at each cap.
	f, tr := sealedFixture(flat(RestoreMaxFolders), 1)
	if sel, err := SelectForRestore(f.out, tr, nil, true); err != nil || len(sel.Folders) != RestoreMaxFolders {
		t.Fatalf("10,000 folders: %v (%d folders)", err, len(sel.Folders))
	}
	f, tr = sealedFixture([]string{deep(RestoreMaxDepth)}, 1)
	if sel, err := SelectForRestore(f.out, tr, nil, true); err != nil || len(sel.Folders) != RestoreMaxDepth {
		t.Fatalf("300 levels: %v", err)
	}

	f, tr = sealedFixture(flat(RestoreMaxFolders+1), 1)
	refused("10,001 folders", f, tr, "10000")
	// Two stores of 5,000 folders each: 10,000 folders and 2 store levels.
	f, tr = sealedFixture(flat(RestoreMaxFolders/2), 2)
	refused("two stores over the cap", f, tr, "10000")
	// 300 levels in each of two stores: the store's own level makes 301.
	f, tr = sealedFixture([]string{deep(RestoreMaxDepth)}, 2)
	refused("301 levels with the store level", f, tr, "300")
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{digits[i%10]}, b...)
	}
	return string(b)
}
