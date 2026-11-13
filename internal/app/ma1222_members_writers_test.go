package app

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// covers: MA-1222, MA-1243, R45
// The unencrypted copy's index (indexCopied) takes each Gmail email's other folders from the
// copy's manifest, as `reindex -rebuild` does (§8.2, Q-L4). Positive twin first: a copy of an
// archive nobody switched on gets an index with no member rows, version 3. Then the switched-on
// fixture: the copy's index holds the manifest's membership, is version 4, and finds the email
// under that folder once.
func TestTheCopysIndexTakesEachEmailsFoldersFromTheManifest(t *testing.T) {
	plain, _ := dedupArchive(t, false)
	m, err := state.Load(filepath.Join(plain, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var key string
	for k, r := range m.All() {
		if len(r.Folders) > 0 {
			key = k
		}
	}
	if key == "" {
		t.Fatal("the fixture has no record with a membership")
	}

	// Positive twin: the same records with no memberships.
	bare, err := state.Load(filepath.Join(plain, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	r, _ := bare.Get(key)
	r.Folders = nil
	bare.Add(key, r)
	ix0, err := index.Open(filepath.Join(tmpDir(t), "bare.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := indexCopied(context.Background(), plain, bare, ix0); err != nil {
		t.Fatal(err)
	}
	if n, _ := ix0.Count(); n != 3 || ix0.Version() != 3 {
		t.Fatalf("positive twin: the copy's index holds %d rows at version %d, want 3 at 3", n, ix0.Version())
	}
	ix0.Close()

	ix, err := index.Open(filepath.Join(tmpDir(t), "copy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if err := indexCopied(context.Background(), plain, m, ix); err != nil {
		t.Fatal(err)
	}
	if got, _ := ix.Members(key); !reflect.DeepEqual(got, []string{"Sent"}) {
		t.Fatalf("the copy's index holds members %q, want the manifest's [Sent]", got)
	}
	if ix.Version() != 4 {
		t.Fatalf("the copy's index is version %d, want 4", ix.Version())
	}
	if _, n, err := ix.Search(index.Query{Folder: "Sent"}); err != nil || n != 1 {
		t.Fatalf("Sent finds %d (%v), want the one email", n, err)
	}
}

// covers: MA-1222, MA-1243, R45, R37
// The keyed rebuild of an encrypted archive's sealed index (the catch-up's path) takes each
// Gmail email's other folders from the sealed record (§8.2, Q-L4). Positive twin first: the
// archive's own indexed run leaves a sealed index at version 3 with no member rows. Then a
// membership saved into the sealed record: the rebuilt sealed index holds it, is version 4,
// and finds the email under that folder once.
func TestTheKeyedRebuildTakesEachEmailsFoldersFromTheSealedRecord(t *testing.T) {
	store := filepath.Join(tmpDir(t), "store")
	p14Write(t, filepath.Join(store, "Inbox"), indexMbox("first", "one")+indexMbox("second", "two"))
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	m, _, idx := sealedIndexOf(t, out, key)
	keys := make([]string, 0, 2)
	for k := range m.All() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) != 2 {
		t.Fatalf("the fixture archived %d messages, want 2", len(keys))
	}
	if got, _ := idx.Members(keys[0]); len(got) != 0 || idx.Version() != 3 {
		t.Fatalf("positive twin: the sealed index holds members %q at version %d, want none at 3", got, idx.Version())
	}

	sealed := loadSealed(t, out, key)
	rec, _ := sealed.Get(keys[0])
	rec.Folders = []state.Membership{{Folder: "Sent", Kind: state.MemberCopy, FirstSeen: rec.ExportedAt}}
	sealed.Add(keys[0], rec)
	tok, _, _ := strings.Cut(keys[0], "\x00")
	sealed.SwitchOnGmail(tok)
	if err := sealed.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := RebuildEncrypted(context.Background(), Options{Out: out, Key: key}, discard()); err != nil {
		t.Fatal(err)
	}
	_, _, idx = sealedIndexOf(t, out, key)
	if got, _ := idx.Members(keys[0]); !reflect.DeepEqual(got, []string{"Sent"}) {
		t.Fatalf("the rebuilt sealed index holds members %q, want the record's [Sent]", got)
	}
	if idx.Version() != 4 {
		t.Fatalf("the rebuilt sealed index is version %d, want 4", idx.Version())
	}
	if _, n, err := idx.Search(index.Query{Folder: "Sent"}); err != nil || n != 1 {
		t.Fatalf("Sent finds %d (%v), want the one email", n, err)
	}
}
