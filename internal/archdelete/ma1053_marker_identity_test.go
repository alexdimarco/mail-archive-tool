package archdelete

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
)

// markerSpellings are spellings of the marker's name that a fold joins and a file
// system may keep apart (the slice-1 pass's LOW-1): upper case (one name on APFS and
// NTFS, two on a case-sensitive volume), the dotless ı (U+0131, which util.FoldKey
// widens to i but APFS and this NTFS keep apart) and the long ſ (U+017F, which Unicode
// folding makes s: one name on APFS, two on NTFS).
var markerSpellings = map[string]string{
	"exact":     delmark.Name,
	"upper":     ".MAILARCHIVE-DELETING.JSON",
	"dotless i": ".maılarchive-deleting.json",
	"long s":    ".mailarchive-deleting.jſon",
}

// fsSaysMarker reports whether the file system gives, for the marker's exact name, the
// very entry at spelling in dir: what delmark.Present looks up.
func fsSaysMarker(t *testing.T, dir, spelling string) bool {
	t.Helper()
	fi, err := os.Lstat(filepath.Join(dir, spelling))
	if err != nil {
		t.Fatal(err)
	}
	mfi, err := os.Lstat(filepath.Join(dir, delmark.Name))
	return err == nil && os.SameFile(fi, mfi)
}

// listsOf names the plan's lists that hold rel.
func listsOf(p *DeletePlan, rel string) []string {
	var in []string
	for name, l := range map[string][]string{"remove": removeRels(p), "kept": p.Kept, "litter": p.Litter, "unrecorded": unrecordedRels(p)} {
		if contains(l, rel) {
			in = append(in, name)
		}
	}
	return in
}

// depsFor is testDeps with the name rules of goos ("" is this computer's).
func depsFor(goos string) Deps {
	d := testDeps()
	d.GOOS = goos
	return d
}

// covers: MA-1053, R36
// Every lister skips what delmark.Present calls the marker and nothing else (§15.1,
// §15.2; the slice-1 pass's LOW-1): the very entry the file system gives for the exact
// name. A spelling the platform's fold joins but the volume keeps apart (ı and ſ on
// NTFS, ı on APFS, any case on a case-sensitive volume) is not the marker — Present says
// so, and encmark's exact names say so — and is judged by the lists' own rules: a plain
// plan names it in exactly one list (never in none, where it would be neither removed
// nor shown as kept), an encrypted one refuses it as more than one kind. A spelling the
// volume makes one name with the exact one is the marker even where the platform's
// rules compare exactly (Linux's, on a case-insensitive drive), so the plan never keeps
// or removes a name Present calls the marker. Positive twin: the exact name, skipped
// under every platform's rules.
func TestEveryListerSkipsWhatPresentCallsTheMarkerAndNothingElse(t *testing.T) {
	for _, goos := range []string{"", "linux", "darwin"} {
		for label, spelling := range markerSpellings {
			rules := goos
			if rules == "" {
				rules = "this computer's"
			}
			t.Run(rules+" rules, "+label, func(t *testing.T) {
				dir := plainBesidePersonsFiles(t)
				writeFile(t, filepath.Join(dir, spelling), `{"version":1}`)
				isMarker := fsSaysMarker(t, dir, spelling)
				if label == "exact" && !isMarker {
					t.Fatal("the exact name is not the marker")
				}
				if st, err := delmark.Present(dir); (st == delmark.Regular) != isMarker || err != nil {
					t.Fatalf("Present = %v %v; the file system says marker = %v", st, err, isMarker)
				}
				in := listsOf(mustPlan(t, dir, depsFor(goos)), spelling)
				switch {
				case isMarker && len(in) != 0:
					t.Fatalf("plain: the marker (as %q) is in the plan's lists %q", spelling, in)
				case !isMarker && len(in) != 1:
					t.Fatalf("plain: %q, not the marker, is in the plan's lists %q, want exactly one", spelling, in)
				}

				enc := encryptedArchive(t)
				writeFile(t, filepath.Join(enc, spelling), `{"version":1}`)
				if isMarker {
					p := mustPlan(t, enc, depsFor(goos))
					if p.Kind != Encrypted || len(listsOf(p, spelling)) != 0 {
						t.Fatalf("encrypted: kind %v, the marker in %q", p.Kind, listsOf(p, spelling))
					}
					return
				}
				rc, msg, opts := refusedCase(t, enc, depsFor(goos), CaseMixed, "This folder holds files of more than one kind. Delete it by hand.")
				assure.Refused(t, rc, msg, opts...)
			})
		}
	}
}

// covers: MA-1053, R36
// A second name for the marker's file (a hard link at another case of its name, which
// only a case-sensitive volume can hold beside it) is not the marker: the exact name,
// listed, is the marker before any other spelling, so the plan skips the exact name and
// judges the other by its own rules — never the other way round, which would list the
// marker itself (LOW-1's fix). Positive twin first: the marker alone is skipped.
func TestASecondNameForTheMarkersFileIsNotTheMarker(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	plantMarker(t, dir)
	if in := listsOf(mustPlan(t, dir, testDeps()), delmark.Name); len(in) != 0 {
		t.Fatalf("the positive twin: the marker in %q", in)
	}
	second := ".Mailarchive-deleting.json" // sorts before the exact name
	if err := os.Link(filepath.Join(dir, delmark.Name), filepath.Join(dir, second)); err != nil {
		t.Skip("this volume cannot hold another case of the name beside it:", err)
	}
	p := mustPlan(t, dir, testDeps())
	if in := listsOf(p, delmark.Name); len(in) != 0 {
		t.Fatalf("the marker, beside a second name for its file, is in %q", in)
	}
	if in := listsOf(p, second); len(in) != 1 {
		t.Fatalf("the second name is in %q, want exactly one list", in)
	}
}

// noAnchorRig is a folder whose delete stopped after removing its manifest (§5.4): this
// computer's log has the started line, the folder keeps a state file.
func noAnchorRig(t *testing.T, kind string) *rig {
	t.Helper()
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	line := map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "folder": dir, "row": dir, "kind": kind, "identity": idOf(t, dir)}
	if kind == "encrypted" {
		line["record_id"], line["record_spelling"] = testArchiveID, dir
	}
	appendRawLog(t, r.x.Log, line)
	if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".mailarchive-lastrun.json"), "{}")
	return r
}

// covers: MA-1053, R36
// A link at the marker's name is refused as a link by every lister — the plain and the
// encrypted ones and the no-anchor re-run of either kind (§15.3: "Delete's plan refuses
// such a folder too: a link by §7's link row"; the slice-1 pass's LOW-2) — in the link
// row's words, never followed, the folder unchanged. Before, the encrypted lister said
// "more than one kind" and the encrypted re-run planned it as kept. Positive twin first,
// in each: a regular marker is planned. On a case-insensitive volume under Linux's
// rules (an exFAT drive), a link at another case of the name is the marker's link too.
func TestALinkAtTheMarkerNameIsRefusedAsALinkByEveryLister(t *testing.T) {
	linkWords := func(name string) []string {
		return []string{"This archive's folder is, or holds where its files are, a link:", name, "Delete it by hand."}
	}
	check := func(t *testing.T, dir string, deps Deps) {
		t.Helper()
		plantMarker(t, dir)
		mustPlan(t, dir, deps)
		if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, t.TempDir(), filepath.Join(dir, delmark.Name))
		rc, msg, opts := refusedCase(t, dir, deps, CaseLink, linkWords(delmark.Name)...)
		assure.Refused(t, rc, msg, opts...)
	}
	t.Run("plain", func(t *testing.T) { check(t, plainBesidePersonsFiles(t), testDeps()) })
	t.Run("encrypted", func(t *testing.T) { check(t, encryptedArchive(t), testDeps()) })
	for _, kind := range []string{"plain", "encrypted"} {
		t.Run("no anchor "+kind, func(t *testing.T) {
			r := noAnchorRig(t, kind)
			if p := r.plan(t); p.Rerun != RerunNoAnchor {
				t.Fatalf("rerun %v", p.Rerun)
			}
			check(t, r.dir, r.x.Deps)
		})
	}
	t.Run("another case under Linux's rules", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Linux's rules on a Windows host do not see a junction as a link; the junction is the cases above")
		}
		dir := plainBesidePersonsFiles(t)
		upper := markerSpellings["upper"]
		symlinkOrSkip(t, t.TempDir(), filepath.Join(dir, upper))
		if !fsSaysMarker(t, dir, upper) {
			t.Skip("this volume tells case apart: another case is not the marker here (the agreement test covers it)")
		}
		if st, err := delmark.Present(dir); st != delmark.NotRegular || err != nil {
			t.Fatalf("Present = %v %v", st, err)
		}
		rc, msg, opts := refusedCase(t, dir, depsFor("linux"), CaseLink, linkWords(upper)...)
		assure.Refused(t, rc, msg, opts...)
	})
}

// covers: MA-1053, R36
// The store trees' check stays wide on purpose (§4.1, §15.7): there a name the
// platform's fold joins with the marker's — another case, ı, ſ — is another archive's
// marker and refuses the whole delete as mixed, failing closed, while the root listers
// skip only the very file Present finds. Positive twin first: the tree without it.
func TestAStoreTreeHoldingTheMarkerInAnySpellingIsMixed(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux's rules compare names exactly; the exact name is TestAStoreTreeHoldingAnotherArchivesDeleteMarkerIsMixed")
	}
	for label, spelling := range markerSpellings {
		t.Run(label, func(t *testing.T) {
			dir := smallArchive(t, t.TempDir())
			mustPlan(t, dir, testDeps())
			writeFile(t, filepath.Join(dir, "Mailbox", "Inbox", "Old", spelling), "{}")
			rc, msg, opts := refusedCase(t, dir, testDeps(), CaseMixed, "This folder holds files of more than one kind. Delete it by hand.")
			assure.Refused(t, rc, msg, opts...)
		})
	}
}
