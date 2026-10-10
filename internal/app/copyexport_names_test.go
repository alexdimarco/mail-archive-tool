package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/util"
)

// folderMsgs makes one message in each of the named top-level folders.
func folderMsgs(names []string) []CopyMessage {
	out := ceMsgs(len(names))
	for i, n := range names {
		out[i].Folder = []CopyFolderRef{{ID: fmt.Sprintf("F%02d", i+1), Name: n}}
	}
	return out
}

// segmentsUnder lists the directory names directly under root/messages, sorted.
func segmentsUnder(t *testing.T, root string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(root, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// outsideRoot lists everything in dest that is not the export root.
func outsideRoot(t *testing.T, dest, rootName string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(dest, p)
		if rel == "." || rel == rootName || strings.HasPrefix(rel, rootName+string(filepath.Separator)) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	return out
}

// covers: MA-417, R4, R6, R29, S52
// Hostile names: folders named "..", "/", "\", an absolute path, NUL, CON, COM1.txt,
// a trailing dot or space, 300 bytes of CJK, an RLO bidi control, COM¹, CONIN$ and
// "CON .txt" are all exported (a positive twin first: ordinary folders), each as a
// distinct, creatable, in-root directory name through the export-only naming pass that
// is layered over util.SanitizeSegment (ruling Q15): no separator, no reserved
// device name, no bidi control, no trailing dot or space, at most 255 bytes, and
// nothing appears outside the export root.
func TestCopyExportHostileFolderNames(t *testing.T) {
	hostile := []string{"..", "/", "\\", "/etc/passwd", "NUL", "CON", "COM1.txt", "trailing dot.", "trailing space ",
		strings.Repeat("日", 100), "evil‮gnp.exe", "COM¹", "CONIN$", "CON .txt"}
	e, src := newCEEnv(t), newFakeSrc()

	// Positive twin: ordinary folders are exported under their own names.
	plain := mustRun(t, e, cePlan(t, e, src, folderMsgs([]string{"Inbox", "Sent Items"})), src)
	if got := segmentsUnder(t, plain.Root); strings.Join(got, "|") != "Inbox|Sent Items" {
		t.Fatalf("ordinary folders became %v", got)
	}

	dest := filepath.Join(t.TempDir(), "dest")
	os.MkdirAll(dest, 0o755)
	p := cePlanAt(t, e, src, folderMsgs(assure.Reached(t, hostile, "hostile names")), dest)
	res := mustRun(t, e, p, src)
	if res.Outcome != CopyOutcomeComplete || res.Counts.Exported != len(hostile) {
		t.Fatalf("hostile folder names were not all exported: %s, %+v", res.Label, res.Counts)
	}
	segs := segmentsUnder(t, res.Root)
	if len(segs) != len(hostile) {
		t.Fatalf("%d hostile folders became %d directories %q: names merged", len(hostile), len(segs), segs)
	}
	keys := map[string]bool{}
	for _, s := range segs {
		up := strings.ToUpper(strings.TrimRight(strings.SplitN(s, ".", 2)[0], " "))
		switch {
		case s == "" || s == "." || s == "..", strings.ContainsAny(s, "/\\\x00"):
			t.Errorf("segment %q is not one creatable name", s)
		case len(s) > util.ExportSegmentMaxBytes || !utf8.ValidString(s):
			t.Errorf("segment %q is %d bytes or not UTF-8", s, len(s))
		case util.StripBidi(s) != s:
			t.Errorf("segment %q keeps a bidi control", s)
		case strings.HasSuffix(s, ".") || strings.HasSuffix(s, " "):
			t.Errorf("segment %q ends in a dot or space", s)
		case up == "CON" || up == "NUL" || up == "COM1" || up == "CONIN$" || up == "COM¹":
			t.Errorf("segment %q is a reserved device name", s)
		}
		if keys[util.FoldKey(s)] {
			t.Errorf("segment %q has the same fold key as another", s)
		}
		keys[util.FoldKey(s)] = true
	}
	if out := outsideRoot(t, dest, p.ExportName); len(out) != 0 {
		t.Errorf("something appeared beside the export root: %v", out)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("the hostile-name export fails its check: %v %v %v", bad, missing, extra)
	}
}

// covers: MA-418, R4, R6, R29
// FoldKey de-duplication end to end: sibling folders Work/work, I/ı (the NTFS and
// exFAT upcase-table equivalents), an NFC and an NFD spelling of one name, and an 8.3
// short-name alias get distinct directories whose fold keys differ, the same names on
// a second run (deterministic); the casefold-unique helper the attachment writer uses
// turns Report.pdf, report.PDF and report.pdf into three names, deterministically.
func TestCopyExportFoldKeyDedup(t *testing.T) {
	const school = "école"
	names := []string{"Work", "work", "I", string(rune(0x131)), norm.NFC.String(school), norm.NFD.String(school), "PROJEC~1", "projec~1"}
	e, src := newCEEnv(t), newFakeSrc()
	var runs [][]string
	for range 2 {
		res := mustRun(t, e, cePlan(t, e, src, folderMsgs(names)), src)
		segs := segmentsUnder(t, res.Root)
		runs = append(runs, segs)
		keys := map[string]bool{}
		for _, s := range segs {
			if keys[util.FoldKey(s)] {
				t.Errorf("segments %q share a fold key: %q", segs, s)
			}
			keys[util.FoldKey(s)] = true
			if util.IsShortNameAlias(s) {
				t.Errorf("segment %q is left as an 8.3 short-name alias", s)
			}
		}
		if len(segs) != len(names) {
			t.Errorf("%d folders became %d directories %q", len(names), len(segs), segs)
		}
	}
	if strings.Join(runs[0], "|") != strings.Join(runs[1], "|") {
		t.Errorf("two runs named the folders differently:\n%q\n%q", runs[0], runs[1])
	}

	// The attachment writer's helper (slice E6) is casefold-unique and deterministic.
	for range 2 {
		used := map[string]struct{}{}
		var got []string
		for _, n := range []string{"Report.pdf", "report.PDF", "report.pdf", "REPORT~1.PDF"} {
			got = append(got, ceUniqueName(used, n))
		}
		if want := []string{"Report.pdf", "report (2).PDF", "report (3).pdf"}; strings.Join(got[:3], "|") != strings.Join(want, "|") {
			t.Errorf("ceUniqueName gave %q, want %q", got[:3], want)
		}
		if util.IsShortNameAlias(got[3]) || !strings.HasSuffix(got[3], ".PDF") {
			t.Errorf("an 8.3 alias attachment name became %q", got[3])
		}
	}
}

// covers: MA-418, R4, R29
// A file planted under a message's final name is never replaced: the placement is a
// no-replace rename, so the message takes the next unique variant of its name, and
// the planted file's bytes are unchanged.
func TestCopyExportPlantedFileIsNeverReplaced(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	msgs := ceMsgs(2)
	p := cePlan(t, e, src, msgs)
	root := filepath.Join(p.Dest, p.ExportName)
	pl := newCeNamer(true, filepath.ToSlash(root)).place(&msgs[0], 1)
	planted := filepath.Join(root, filepath.FromSlash(pl.Path()))
	e.faults = &copyFaults{hook: func(step string) {
		if step == "W5tmp:1" {
			os.WriteFile(planted, []byte("planted by someone else"), 0o600)
		}
	}}
	res := mustRun(t, e, p, src)
	if b, _ := os.ReadFile(planted); string(b) != "planted by someone else" {
		t.Fatalf("the planted file was replaced: %q", b)
	}
	if res.Counts.Exported != 2 || res.Counts.Failed != 0 {
		t.Fatalf("the message beside the planted file was not exported: %+v", res.Counts)
	}
	variant := strings.TrimSuffix(planted, ".eml") + "~2.eml"
	if b, err := os.ReadFile(variant); err != nil || string(b) != string(ceBody(msgs[0].SourceID)) {
		t.Errorf("the message did not take the next variant name: %v", err)
	}
}

// covers: MA-417, R4, R29
// The stem is the archive's own (export.StemName), so an inheritor sees one
// convention: <received UTC 2006-01-02_1504>_<slug>_<8 hex of the source id>.
func TestCopyExportStemIsTheArchivesScheme(t *testing.T) {
	m := ceMsgs(1)[0]
	m.Subject = "Quarterly review"
	pl := newCeNamer(true, "/dest/MailExport-x").place(&m, 1)
	want := export.StemName(m.Received, util.Slug(m.Subject, 60), m.SourceID, 8)
	if pl.Stem != want || !strings.HasPrefix(pl.Stem, "2026-03-28_0900_Quarterly-review_") {
		t.Errorf("stem = %q, want %q", pl.Stem, want)
	}
}
