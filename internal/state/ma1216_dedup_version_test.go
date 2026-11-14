package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// Slice B2 of docs/design-gmail-dedup.md: the one version function, the read-max and write
// version split, version 8 while a merge is pending, and load validation of the Gmail fields.

// withReadMax lowers the highest version this build reads for the test, standing in for an
// older build's loader (v1.6.1 and 1.7.x read at most 6), and restores it: fault injection on
// one constant, accepted by the owner on 2026-11-11.
func withReadMax(t *testing.T, v int) {
	t.Helper()
	old := readMax
	readMax = v
	t.Cleanup(func() { readMax = old })
}

// fieldDecisions is the version each persisted field of Record and Manifest makes a manifest:
// 6 for every field version 6 already had, 7 for the Gmail fields, 8 for MergePending. A field
// added later without a decision here fails TestFormatVersionDecidesEveryField.
var fieldDecisions = map[string]int{
	// Record
	"path": 6, "folder": 6, "exported_at": 6, "missing": 6, "terminal": 6, "unresolved": 6,
	"body_fallback": 6, "subject": 6, "date": 6, "fp": 6, "fp_scheme": 6, "phys_id": 6,
	"chash": 6, "alt_phys_ids": 6, "also_files": 6, "fixity": 6, "first_folder": 6,
	"first_seen": 6, "last_seen": 6, "present": 6, "loc": 6, "recovered_by": 6,
	"parse_scheme": 6, "parser_gen": 6, "raw_sha256": 6, "fidelity_fallback": 6, "unit": 6,
	"sealed":    6,
	"gm_sha256": 7, "folders": 7, "merged_files": 7,
	// Manifest
	"version": 6, "entries": 6, "stores": 6, "capture_gaps": 6, "collapsed_tokens": 6,
	"dedup_stores": 7, "summary_stamps": 7,
	"merge_pending": 8,
}

// nonZero is a non-zero value of type ty.
func nonZero(t *testing.T, ty reflect.Type) reflect.Value {
	t.Helper()
	v := reflect.New(ty).Elem()
	switch ty.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Int, reflect.Int64:
		v.SetInt(1)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:
		v.Set(reflect.MakeSlice(ty, 1, 1))
	case reflect.Map: // every map here is keyed by a string
		mp := reflect.MakeMap(ty)
		mp.SetMapIndex(reflect.ValueOf("k").Convert(ty.Key()), reflect.New(ty.Elem()).Elem())
		v.Set(mp)
	case reflect.Ptr:
		v.Set(reflect.New(ty.Elem()))
	case reflect.Struct:
		if ty == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)))
		} else {
			t.Fatalf("no non-zero value for struct %s", ty)
		}
	default:
		t.Fatalf("no non-zero value for %s", ty)
	}
	return v
}

func jsonName(f reflect.StructField) string {
	tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if tag == "-" || !f.IsExported() {
		return ""
	}
	return tag
}

// covers: MA-1216, R45
// FormatVersion has a decision for every persisted field of Record and Manifest, and each field
// set alone on an otherwise empty manifest gives exactly that decision: 6 for the fields version
// 6 had, 7 for the Gmail fields, 8 for MergePending. A field added without a decision, or a
// decision the function does not keep, turns this red (§7.4 fold a; fold check 5's M1).
func TestFormatVersionDecidesEveryField(t *testing.T) {
	if got := FormatVersion(newManifest("")); got != 6 {
		t.Fatalf("positive twin: an empty manifest is version %d, want 6", got)
	}
	seen := map[string]bool{}
	rt := reflect.TypeOf(Record{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name := jsonName(f)
		if name == "" {
			continue
		}
		seen[name] = true
		want, ok := fieldDecisions[name]
		if !ok {
			t.Errorf("Record field %s (%q) has no version decision: decide it in FormatVersion and here", f.Name, name)
			continue
		}
		m := newManifest("")
		r := Record{}
		reflect.ValueOf(&r).Elem().Field(i).Set(nonZero(t, f.Type))
		m.Entries["s\x00k"] = r
		if got := FormatVersion(m); got != want {
			t.Errorf("a record with only %s set: version %d, want %d", name, got, want)
		}
	}
	mt := reflect.TypeOf(Manifest{})
	for i := 0; i < mt.NumField(); i++ {
		f := mt.Field(i)
		name := jsonName(f)
		if name == "" {
			continue
		}
		seen[name] = true
		want, ok := fieldDecisions[name]
		if !ok {
			t.Errorf("Manifest field %s (%q) has no version decision: decide it in FormatVersion and here", f.Name, name)
			continue
		}
		if name == "version" || name == "entries" {
			continue // the stamp itself, and the records (above)
		}
		m := newManifest("")
		reflect.ValueOf(m).Elem().Field(i).Set(nonZero(t, f.Type))
		if got := FormatVersion(m); got != want {
			t.Errorf("a manifest with only %s set: version %d, want %d", name, got, want)
		}
	}
	for name := range fieldDecisions {
		if !seen[name] {
			t.Errorf("decision for %q names no field of Record or Manifest", name)
		}
	}
}

// covers: MA-1216, R45, S82
// MergePending makes the manifest version 8 whatever else it holds; without it, the same
// manifest is 7 (fold check 5's M1).
func TestAPendingMergeIsVersion8(t *testing.T) {
	m := newManifest("")
	m.Entries["s\x00a"] = Record{Path: "s/Inbox/a.html", Folder: "Inbox", Folders: []Membership{{Folder: "Sent", Kind: MemberCopy}}}
	m.SwitchOnGmail("s")
	if got := FormatVersion(m); got != 7 {
		t.Fatalf("positive twin: a switched-on manifest with a membership is %d, want 7", got)
	}
	m.MergePending = &MergePending{PlanDigest: strings.Repeat("a", 64)}
	if got := FormatVersion(m); got != 8 {
		t.Fatalf("with a MergePending: %d, want 8", got)
	}
	bare := newManifest("")
	bare.MergePending = &MergePending{}
	if got := FormatVersion(bare); got != 8 {
		t.Fatalf("a MergePending alone (no groups): %d, want 8", got)
	}
	m.MergePending = nil
	if got := FormatVersion(m); got != 7 {
		t.Fatalf("the MergePending removed: %d, want 7", got)
	}
}

// covers: MA-1216, R45, S82
// Every place that writes a version stamps what FormatVersion says, never the highest readable:
// a fresh plaintext manifest, a fresh sealed record and a loaded version-5 archive are each saved
// at 6 when no Gmail field is set, and an older build's loader (read max 6: v1.6.1 and 1.7.x)
// then accepts each (F3b). A manifest switched on is saved at 7, which that loader refuses by
// name, changing nothing.
func TestOnlyAGmailFieldWritesVersion7(t *testing.T) {
	dir := t.TempDir()
	stamp := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	olderLoads := func(path string) {
		t.Helper()
		withReadMax(t, 6)
		if _, err := Load(path); err != nil {
			t.Fatalf("an older build's loader refuses %s: %v", filepath.Base(path), err)
		}
		readMax = ManifestVersion
	}

	// A fresh plaintext manifest.
	fresh := filepath.Join(dir, "fresh.json")
	m, err := Load(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 6 || m.LoadedVersion != 6 {
		t.Fatalf("a fresh manifest is version %d (loaded %d), want 6", m.Version, m.LoadedVersion)
	}
	m.Add("s\x00a", Record{Path: "s/Inbox/a.html", Folder: "Inbox"})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(stamp(fresh), []byte(`{"version":6,`)) {
		t.Fatalf("a fresh manifest with no Gmail field was saved as %.30s, want version 6", stamp(fresh))
	}
	olderLoads(fresh)

	// A fresh sealed record (container's fresh path uses FormatVersionOf(View{})).
	p := &recordingPersister{}
	sm, err := FromSealed(filepath.Join(dir, "sealed.json"), View{Version: FormatVersionOf(View{})}, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := sm.Save(); err != nil {
		t.Fatal(err)
	}
	if got := p.views[len(p.views)-1].Version; got != 6 {
		t.Fatalf("a fresh sealed record was committed at %d, want 6", got)
	}

	// A loaded version-5 archive.
	v5 := filepath.Join(dir, "v5.json")
	if err := os.WriteFile(v5, []byte(`{"version":5,"entries":{"s\u0000a":{"path":"s/Inbox/a.html","folder":"Inbox","exported_at":"2026-01-01T00:00:00Z"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m5, err := Load(v5)
	if err != nil {
		t.Fatal(err)
	}
	if m5.Version != 6 || m5.LoadedVersion != 5 {
		t.Fatalf("a version-5 archive loads as %d (loaded %d), want 6 (5)", m5.Version, m5.LoadedVersion)
	}
	if err := m5.Save(); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(stamp(v5), []byte(`{"version":6,`)) {
		t.Fatalf("a version-5 archive was saved as %.30s, want version 6", stamp(v5))
	}
	olderLoads(v5)

	// Switched on: 7, read back by this build, refused by the older loader with the file unchanged.
	m.SwitchOnGmail("s")
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	body := stamp(fresh)
	if !bytes.HasPrefix(body, []byte(`{"version":7,`)) {
		t.Fatalf("a switched-on manifest was saved as %.30s, want version 7", body)
	}
	back, err := Load(fresh)
	if err != nil || back.Version != 7 || back.LoadedVersion != 7 || !back.DedupOn("s") {
		t.Fatalf("this build reads its own version 7: %v (version %d, on %v)", err, back.Version, back != nil && back.DedupOn("s"))
	}
	withReadMax(t, 6)
	_, lerr := Load(fresh)
	var nf *NewerFormatError
	rc, msg := 0, ""
	if lerr != nil {
		rc, msg = 2, lerr.Error()
	}
	if !errors.As(lerr, &nf) || nf.Version != 7 || nf.Understood != 6 {
		t.Fatalf("an older build's loader on version 7: %v, want NewerFormatError 7 over 6", lerr)
	}
	assure.Refused(t, rc, msg,
		assure.Names(fresh, NewerWords, "upgrade"),
		assure.NoSideEffect(func() bool { return bytes.Equal(stamp(fresh), body) }))
}

// covers: MA-1216, R45, S82, S83
// This build reads at most 7. A manifest stamped 8, and one stamped 7 that holds a MergePending
// (a stamp changed by hand), are each refused as version 8 with the tidy-up words, changing
// nothing; one stamped 9 gets the newer words. An explicit "merge_pending": null stamped 7 reads
// as no merge (fold check 5's S1).
func TestThisBuildRefusesAPendingMerge(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	rec := `"entries":{"s\u0000a":{"path":"s/Inbox/a.html","folder":"Inbox","exported_at":"2026-01-01T00:00:00Z"}},"dedup_stores":{"s":"gmail-1"}`
	pending := `"merge_pending":{"plan_digest":"` + strings.Repeat("a", 64) + `","started":"2026-11-13T00:00:00Z","groups":[]},`

	// Positive twins: 7 without a merge loads; a null merge stamped 7 loads as none.
	if m, err := Load(write("v7.json", `{"version":7,`+rec+`}`)); err != nil || m.MergePending != nil {
		t.Fatalf("positive twin: a version-7 manifest must load: %v", err)
	}
	if m, err := Load(write("null.json", `{"version":7,"merge_pending":null,`+rec+`}`)); err != nil || m.MergePending != nil {
		t.Fatalf("\"merge_pending\": null must read as no merge: %v", err)
	}

	for _, c := range []struct{ name, body string }{
		{"stamped 8", `{"version":8,` + pending + rec + `}`},
		{"stamped 7 by hand with a merge", `{"version":7,` + pending + rec + `}`},
		{"stamped 6 by hand with a merge", `{"version":6,` + pending + rec + `}`},
	} {
		path := write(strings.ReplaceAll(c.name, " ", "-")+".json", c.body)
		_, err := Load(path)
		var nf *NewerFormatError
		if !errors.As(err, &nf) || nf.Version != 8 || nf.Understood != 7 {
			t.Fatalf("%s: %v, want NewerFormatError 8 over 7", c.name, err)
		}
		assure.Refused(t, 2, err.Error(),
			assure.Names(TidyUpWords, path, "format version 8"),
			assure.NoSideEffect(func() bool {
				after, _ := os.ReadFile(path)
				return string(after) == c.body
			}))
		if nf.Plain() != TidyUpWords {
			t.Fatalf("%s: the plain words are %q, want the tidy-up words", c.name, nf.Plain())
		}
	}

	path := write("v9.json", `{"version":9,`+rec+`}`)
	_, err := Load(path)
	var nf *NewerFormatError
	if !errors.As(err, &nf) || nf.Version != 9 {
		t.Fatalf("stamped 9: %v", err)
	}
	if strings.Contains(err.Error(), TidyUpWords) || nf.Plain() != NewerWords {
		t.Fatalf("stamped 9 must say the newer words, not the tidy-up words: %v", err)
	}
	assure.Refused(t, 2, err.Error(), assure.Names(NewerWords, path, "newer mailarchive", "upgrade"))
}

// covers: MA-1216, R45, S83
// The sealed path decides the same way: a sealed view holding a MergePending is refused as
// version 8, and a sealed save commits FormatVersion's answer with every Gmail slot.
func TestTheSealedPathStampsAndRefusesTheSame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sealed.json")
	p := &recordingPersister{}
	m, err := FromSealed(path, View{Version: 7, Entries: map[string]Record{"s\x00a": {Path: "s/Inbox/a.html", Folder: "Inbox"}},
		DedupStores: map[string]string{"s": DedupGmail}, SummaryStamps: map[string]SummaryStamp{"s\x00Sent": {Size: 3}}}, p)
	if err != nil {
		t.Fatalf("positive twin: a version-7 sealed view loads: %v", err)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	got := p.views[len(p.views)-1]
	if got.Version != 7 || got.DedupStores["s"] != DedupGmail || got.SummaryStamps["s\x00Sent"].Size != 3 {
		t.Fatalf("the sealed save committed version %d, dedup %v, stamps %v; want 7 with both", got.Version, got.DedupStores, got.SummaryStamps)
	}
	_, err = FromSealed(path, View{Version: 7, MergePending: &MergePending{}}, p)
	var nf *NewerFormatError
	if !errors.As(err, &nf) || nf.Version != 8 {
		t.Fatalf("a sealed view with a MergePending: %v, want NewerFormatError 8", err)
	}
}

// covers: MA-1217, R45
// Load validates the Gmail fields: a bad membership (a folder that is not clean, the record's
// own folder, an unknown kind, a second one for one folder) is dropped and the record kept; a
// Microsoft 365 record's memberships are dropped; a gm_sha256 that is not a sha-256 is dropped;
// a merge-step file with a path that is not clean is dropped, and one with a clean path that
// fails another check moves, with every file of its stem, to the record's AlsoFiles (owned,
// never deleted). Each is noted for the log. Positive twin: healthy fields load unchanged.
func TestLoadValidatesTheGmailFields(t *testing.T) {
	sum := strings.Repeat("b", 64)
	good := Record{Path: "s/Inbox/a.html", Folder: "Inbox", GmailSHA256: sum,
		Folders: []Membership{{Folder: "[Gmail]/Sent Mail", Kind: MemberListed}, {Folder: "Receipts", Kind: MemberCopy}},
		MergedFiles: []MergedFile{
			{Path: "s/Receipts/c.html", Role: "html", Size: 10, Proof: sum},
			{Path: "s/Receipts/c.eml", Role: "eml", Size: 9, SHA256: sum, Proof: sum},
		}}
	load := func(t *testing.T, recs map[string]Record, stores map[string]string) *Manifest {
		t.Helper()
		m, err := FromSealed(filepath.Join(t.TempDir(), "m.json"), View{Version: 7, Entries: recs, Stores: stores}, &recordingPersister{})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	m := load(t, map[string]Record{"s\x00a": good}, nil)
	if got := m.Entries["s\x00a"]; !reflect.DeepEqual(got.Folders, good.Folders) || got.GmailSHA256 != sum || !reflect.DeepEqual(got.MergedFiles, good.MergedFiles) || len(m.DedupNotes) != 0 {
		t.Fatalf("positive twin: healthy Gmail fields changed at load: %+v, notes %v", got, m.DedupNotes)
	}

	bad := good
	bad.GmailSHA256 = "NOTHEX"
	bad.Folders = []Membership{
		{Folder: "../Escape", Kind: MemberCopy},
		{Folder: "Inbox", Kind: MemberCopy},
		{Folder: "Receipts", Kind: "moved"},
		{Folder: "Receipts", Kind: MemberCopy},
		{Folder: "Receipts", Kind: MemberListed},
		{Folder: "a\x00b", Kind: MemberCopy},
	}
	bad.MergedFiles = []MergedFile{
		{Path: "/etc/passwd", Role: "eml", SHA256: sum, Proof: sum},
		{Path: "s/Receipts/c.html", Role: "html", Size: 10, Proof: sum},
		{Path: "s/Receipts/c.eml", Role: "zap", Size: 9, SHA256: sum, Proof: sum},
		{Path: "s/Other/d.html", Role: "html", Size: 4, Proof: sum},
	}
	m = load(t, map[string]Record{"s\x00a": bad}, nil)
	got, ok := m.Entries["s\x00a"]
	if !ok {
		t.Fatal("load dropped the record; only its bad fields may go")
	}
	if want := []Membership{{Folder: "Receipts", Kind: MemberCopy}}; !reflect.DeepEqual(got.Folders, want) {
		t.Fatalf("memberships after load: %+v, want only %+v", got.Folders, want)
	}
	if got.GmailSHA256 != "" {
		t.Fatalf("a gm_sha256 that is not a sha-256 kept: %q", got.GmailSHA256)
	}
	if want := []MergedFile{{Path: "s/Other/d.html", Role: "html", Size: 4, Proof: sum}}; !reflect.DeepEqual(got.MergedFiles, want) {
		t.Fatalf("merge-step files after load: %+v, want only the healthy stem %+v", got.MergedFiles, want)
	}
	if want := []string{"s/Receipts/c.html", "s/Receipts/c.eml"}; !reflect.DeepEqual(got.AlsoFiles, want) {
		t.Fatalf("AlsoFiles after load: %v, want the failing stem's page and original %v (never only one of them)", got.AlsoFiles, want)
	}
	for _, f := range got.AlsoFiles {
		if strings.Contains(f, "passwd") {
			t.Fatal("a path that is not clean was moved to AlsoFiles; it must be dropped")
		}
	}
	if len(m.DedupNotes) < 8 {
		t.Fatalf("each drop and move is noted for the log: %d notes %v", len(m.DedupNotes), m.DedupNotes)
	}

	// A Microsoft 365 record's memberships are dropped; a local record's beside it are kept.
	m = load(t, map[string]Record{"g\x00x": {Path: "g/Inbox/x.html", Folder: "Inbox", Folders: good.Folders}, "s\x00a": good},
		map[string]string{"mailbox:me@example.com": "g", "path:/mail": "s"})
	if len(m.Entries["g\x00x"].Folders) != 0 || len(m.Entries["s\x00a"].Folders) != 2 {
		t.Fatalf("Microsoft 365 memberships kept (%d) or local ones dropped (%d)", len(m.Entries["g\x00x"].Folders), len(m.Entries["s\x00a"].Folders))
	}
}

// covers: MA-1236, R45, S82
// The switch's plumbing (Q-B): an archive with records is asked, once, only when a person is
// there and has not said Not now on this computer; a scheduled or unattended run never asks and
// never switches; a new archive (no records) is switched on by default; a store already on is
// left alone. The yes writes DedupStores, and the archive is version 7 from that save.
func TestTheGmailSwitchIsThePersons(t *testing.T) {
	existing := newManifest(filepath.Join(t.TempDir(), "m.json"))
	existing.Add("s\x00a", Record{Path: "s/Inbox/a.html", Folder: "Inbox"})
	empty := newManifest("")
	for _, c := range []struct {
		name             string
		m                *Manifest
		gmail            []string
		notNow, attended bool
		want             GmailAsk
	}{
		{"an existing archive, a person there", existing, []string{"s"}, false, true, GmailAskPerson},
		{"an existing archive, unattended", existing, []string{"s"}, false, false, GmailLeave},
		{"an existing archive, Not now on this computer", existing, []string{"s"}, true, true, GmailLeave},
		{"an existing archive, no Gmail store", existing, nil, false, true, GmailLeave},
		{"a new archive, a person there", empty, []string{"s"}, false, true, GmailSwitchNew},
		{"a new archive, unattended", empty, []string{"s"}, false, false, GmailSwitchNew},
	} {
		if got := c.m.GmailDecision(c.gmail, c.notNow, c.attended); got != c.want {
			t.Errorf("%s: decision %d, want %d", c.name, got, c.want)
		}
	}
	if FormatVersion(existing) != 6 {
		t.Fatal("deciding wrote a field: the archive is no longer version 6")
	}
	existing.SwitchOnGmail("s", "../bad")
	if !existing.DedupOn("s") || existing.DedupOn("../bad") || FormatVersion(existing) != 7 {
		t.Fatalf("the yes: on=%v, a bad token on=%v, version %d; want on, refused, 7", existing.DedupOn("s"), existing.DedupOn("../bad"), FormatVersion(existing))
	}
	if got := existing.GmailDecision([]string{"s"}, false, true); got != GmailLeave {
		t.Fatalf("a store already on is asked again: %d", got)
	}
	if got := existing.GmailDecision([]string{"s", "t"}, false, true); got != GmailAskPerson {
		t.Fatalf("an existing archive that gains a second Gmail store is asked: %d", got)
	}
	// The words as amended by the B3 friction review (MA-1293): "1.8 or newer", and the encrypted
	// sentence the review proposed.
	if !strings.Contains(GmailSwitchQuestion(false), "MailArchive 1.8 or newer") || strings.Contains(GmailSwitchQuestion(false), "encrypted") {
		t.Fatalf("the question's words: %q", GmailSwitchQuestion(false))
	}
	if GmailSwitchQuestion(true) != GmailQuestion+" Emails already saved twice stay as they are: MailArchive cannot tidy an encrypted archive." {
		t.Fatalf("the encrypted question's words: %q", GmailSwitchQuestion(true))
	}
}
