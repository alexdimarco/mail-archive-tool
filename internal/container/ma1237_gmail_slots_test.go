package container

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-1237, R45
// An encrypted archive's save and load keep the Gmail fields (docs/design-gmail-dedup.md §7.1
// fold, the sealed slots): DedupStores and SummaryStamps in the sealed header, and a record's
// Folders and gm_sha256 inside its bucket, through a commit and a second save with no change. A
// MergePending survives encoding (never silently emptied by a save), and this build (which reads
// at most 7) refuses a record holding one as version 8. Positive twin first: a fresh record is
// written at version 6 with none of the fields.
func TestASealedSaveKeepsTheGmailFields(t *testing.T) {
	// A fresh record, before its first commit, is FormatVersion of the empty manifest, never
	// the highest version this build reads (F3: container/store.go's fresh path).
	if m0, s0 := open(t, t.TempDir(), opts(0)); !s0.Fresh() || m0.Version != 6 || m0.LoadedVersion != 6 {
		t.Fatalf("a fresh encrypted archive starts at version %d (loaded %d), want 6", m0.Version, m0.LoadedVersion)
	}
	out := fresh(t)
	m, _ := open(t, out, opts(1))
	if m.Version != 6 || state.FormatVersion(m) != 6 {
		t.Fatalf("positive twin: a fresh encrypted archive is version %d, want 6", m.Version)
	}

	sum := strings.Repeat("c", 64)
	r0, r1 := rec(0), rec(1)
	r1.Folders = []state.Membership{{Folder: "[Gmail]/Sent Mail", Kind: state.MemberListed, FirstSeen: base}}
	r1.GmailSHA256 = sum
	m.Add(key(0), r0)
	m.Add(key(1), r1)
	m.SwitchOnGmail("s")
	m.SummaryStamps = map[string]state.SummaryStamp{"s\x00Sent": {Size: 42, ModTime: base}}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	for run := 2; run <= 3; run++ {
		got, _ := open(t, out, opts(run))
		if got.Version != 7 {
			t.Fatalf("run %d: the sealed record reads back as version %d, want 7", run, got.Version)
		}
		if !got.DedupOn("s") || !reflect.DeepEqual(got.SummaryStamps, m.SummaryStamps) {
			t.Fatalf("run %d: the sealed header lost a Gmail slot: dedup %v, stamps %v", run, got.DedupStores, got.SummaryStamps)
		}
		back := got.All()[key(1)]
		if !reflect.DeepEqual(back.Folders, r1.Folders) || back.GmailSHA256 != sum {
			t.Fatalf("run %d: the record's Gmail fields: %+v / %q", run, back.Folders, back.GmailSHA256)
		}
		if err := got.Save(); err != nil { // a save with no change keeps them too
			t.Fatal(err)
		}
	}

	// A MergePending rides in the header (encoded and decoded whole) ...
	pending := &state.MergePending{PlanDigest: sum, Started: base, Groups: []state.MergeGroup{{Survivor: key(0), Copies: []string{key(1)}}}}
	v := state.View{Version: state.FormatVersionOf(state.View{MergePending: pending}), Entries: map[string]state.Record{key(0): r0}, MergePending: pending}
	sections, n, err := encodeSections(keys[0], v, header{}, 16, base)
	if err != nil {
		t.Fatal(err)
	}
	var head map[string]json.RawMessage
	if err := json.Unmarshal(sections[posHeader], &head); err != nil || head["merge_pending"] == nil || string(head["version"]) != "8" {
		t.Fatalf("the sealed header of a pending merge: %s", sections[posHeader])
	}
	// ... and this build, which reads at most 7, refuses it as a newer format.
	if _, _, err := decodeSections(keys[0], sections, n); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("a sealed record at version 8 read by a build that reads 7: %v, want ErrNewerFormat", err)
	}
	_, err = state.FromSealed(out, state.View{Version: 7, MergePending: pending}, nil)
	var nf *state.NewerFormatError
	if !errors.As(err, &nf) || nf.Version != 8 {
		t.Fatalf("a sealed view stamped 7 holding a MergePending: %v, want NewerFormatError 8", err)
	}

	// A version-7 record still decodes strictly: an unknown header field is damage.
	v7 := state.View{Version: 7, Entries: map[string]state.Record{key(1): r1}, DedupStores: map[string]string{"s": state.DedupGmail}}
	sections, n, err = encodeSections(keys[0], v7, header{}, 16, base)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := decodeSections(keys[0], sections, n); err != nil || got.DedupStores["s"] != state.DedupGmail {
		t.Fatalf("positive twin: a version-7 record decodes: %v", err)
	}
	sections[posHeader] = []byte(strings.Replace(string(sections[posHeader]), `{`, `{"zz_unknown":1,`, 1))
	if _, _, err := decodeSections(keys[0], sections, n); !errors.Is(err, ErrRecord) {
		t.Fatalf("a version-7 header with an unknown field: %v, want ErrRecord", err)
	}
}
