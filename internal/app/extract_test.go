package app

import (
	"bytes"
	"context"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
)

// writeMaildirMsg writes one RFC 5322 message file into a maildir folder's cur/
// dir (the source layer reads such a directory as one store, one folder). The
// bytes become the message's preserved original (.eml) under -raw.
func writeMaildirMsg(t *testing.T, folderDir, name string, raw []byte) {
	t.Helper()
	cur := filepath.Join(folderDir, "cur")
	if err := os.MkdirAll(cur, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cur, name), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildRawArchive runs a real export (KeepRaw) over the given maildir folders and
// returns the archive dir. inputs maps a store folder name to its messages.
func buildRawArchive(t *testing.T, inputs map[string][][]byte) string {
	t.Helper()
	src := tmpDir(t)
	var paths []string
	for name, msgs := range inputs {
		folder := filepath.Join(src, name)
		for i, m := range msgs {
			writeMaildirMsg(t, folder, mailName(i), m)
		}
		paths = append(paths, folder)
	}
	out := tmpDir(t)
	opts := Options{Inputs: paths, Out: out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: true}
	r, err := Run(context.Background(), opts, discard(), nil)
	if err != nil {
		t.Fatalf("build raw archive: %v", err)
	}
	if r.Stats.RawWritten == 0 {
		t.Fatalf("fixture wrote no .eml (RawWritten=0); the source is not raw-capable")
	}
	return out
}

func mailName(i int) string { return "msg" + string(rune('a'+i)) + ".eml" }

func msg(subject, id, body string) []byte {
	return []byte("From: alice@example.com\r\nTo: bob@example.com\r\nSubject: " + subject +
		"\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <" + id + "@ex>\r\n\r\n" + body + "\r\n")
}

// collectFiles returns every file under root with the given suffix, keyed by its
// path relative to root (forward-slashed), with its bytes.
func collectFiles(t *testing.T, root, suffix string) map[string][]byte {
	t.Helper()
	got := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), suffix) {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		got[filepath.ToSlash(rel)] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// readMboxMessages un-frames an mboxrd stream into individual message byte
// blocks (splitting on "From " postmark lines and un-quoting `>+From ` lines),
// so a test can count boundaries and parse each with net/mail.
func readMboxMessages(stream []byte) [][]byte {
	var starts []int
	if bytes.HasPrefix(stream, []byte("From ")) {
		starts = append(starts, 0)
	}
	for i := 0; i+1 < len(stream); i++ {
		if stream[i] == '\n' && bytes.HasPrefix(stream[i+1:], []byte("From ")) {
			starts = append(starts, i+1)
		}
	}
	var msgs [][]byte
	for si, s := range starts {
		nl := bytes.IndexByte(stream[s:], '\n')
		if nl < 0 {
			continue
		}
		end := len(stream)
		if si+1 < len(starts) {
			end = starts[si+1]
		}
		block := bytes.TrimSuffix(stream[s+nl+1:end], []byte("\n"))
		lines := bytes.Split(block, []byte("\n"))
		for i, ln := range lines {
			j := 0
			for j < len(ln) && ln[j] == '>' {
				j++
			}
			if j > 0 && bytes.HasPrefix(ln[j:], []byte("From ")) {
				lines[i] = ln[1:]
			}
		}
		msgs = append(msgs, bytes.Join(lines, []byte("\n")))
	}
	return msgs
}

// covers: MA-181, R20, S34
// extract -format mbox writes one mboxrd file per folder whose messages round-trip
// through net/mail with correct boundaries, and a body "From " line is >-quoted so
// a reader does not mistake it for a boundary.
func TestExtractMboxPerFolderRoundTrips(t *testing.T) {
	out := buildRawArchive(t, map[string][][]byte{
		"Alpha": {msg("Beach", "beach", "Hello.\r\nFrom the shore you can see far."), msg("Lunch", "lunch", "Meet at noon.")},
		"Beta":  {msg("Report", "report", "Numbers attached.")},
	})
	dest := tmpDir(t)

	rep, err := Extract(out, FormatMbox, dest, false, discard(), nil)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if rep.Emitted != 3 || rep.Skipped != 0 {
		t.Fatalf("emitted=%d skipped=%d, want 3/0 (%+v)", rep.Emitted, rep.Skipped, rep)
	}

	mboxes := collectFiles(t, dest, ".mbox")
	if len(mboxes) != 2 {
		t.Fatalf("want one .mbox per folder (2), got %d: %v", len(mboxes), keys(mboxes))
	}
	total := 0
	quoted := false
	for name, data := range mboxes {
		if strings.Contains(string(data), "\n>From the shore you can see far.") {
			quoted = true
		}
		msgs := assure.Reached(t, readMboxMessages(data), "messages in "+name)
		for _, m := range msgs {
			if _, perr := mail.ReadMessage(bytes.NewReader(m)); perr != nil {
				t.Errorf("%s: a message does not parse as RFC 822: %v", name, perr)
			}
			total++
		}
	}
	if total != 3 {
		t.Errorf("round-tripped %d messages across the mboxes, want 3", total)
	}
	if !quoted {
		t.Errorf("the body 'From ' line was not >-quoted in any mbox output")
	}
}

// covers: MA-182, R20, S34, R5
// extract -format eml mirrors the folder tree with one byte-exact .eml per record;
// a re-run into the same -dest does not double (temp+rename over the existing
// file), and a non-empty -dest is refused without --overwrite.
func TestExtractEMLMirrorsTreeIdempotent(t *testing.T) {
	out := buildRawArchive(t, map[string][][]byte{
		"Alpha": {msg("One", "one", "first"), msg("Two", "two", "second")},
		"Beta":  {msg("Three", "three", "third")},
	})
	archiveEML := collectFiles(t, out, ".eml")
	if len(archiveEML) != 3 {
		t.Fatalf("archive should hold 3 .eml, got %d", len(archiveEML))
	}
	dest := tmpDir(t)

	rep, err := Extract(out, FormatEML, dest, false, discard(), nil)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if rep.Emitted != 3 {
		t.Fatalf("emitted=%d, want 3", rep.Emitted)
	}
	destEML := collectFiles(t, dest, ".eml")
	if len(destEML) != len(archiveEML) {
		t.Fatalf("extracted %d .eml, archive has %d — tree not mirrored", len(destEML), len(archiveEML))
	}
	for rel, want := range archiveEML {
		got, ok := destEML[rel]
		if !ok {
			t.Errorf("archive .eml %s has no mirrored output", rel)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s not byte-exact after extract", rel)
		}
	}

	// A second run into a non-empty -dest is refused unless --overwrite.
	if _, e2 := Extract(out, FormatEML, dest, false, discard(), nil); e2 == nil {
		t.Errorf("a re-run into a non-empty -dest should be refused without --overwrite")
	} else if !strings.Contains(e2.Error(), "not empty") {
		t.Errorf("refusal should name the non-empty -dest: %v", e2)
	}
	// With --overwrite it re-runs and does NOT double.
	if _, e3 := Extract(out, FormatEML, dest, true, discard(), nil); e3 != nil {
		t.Fatalf("--overwrite re-run failed: %v", e3)
	}
	if again := collectFiles(t, dest, ".eml"); len(again) != len(archiveEML) {
		t.Errorf("re-run doubled output: %d .eml, want %d", len(again), len(archiveEML))
	}
}

// covers: MA-184, R20, R4, R5, S34
// extract trusts nothing and writes nothing into the archive: a symlinked .eml, a
// fixity-mismatched .eml, and an oversized .eml are each skipped-and-reported
// (never read or emitted), and every archived file is byte-identical afterwards.
func TestExtractGateAndFixitySkips(t *testing.T) {
	out := buildRawArchive(t, map[string][][]byte{
		"Alpha": {msg("Keep", "keep", "healthy"), msg("Link", "link", "symlinked"), msg("Rot", "rot", "will be rotted")},
	})
	// Positive twin: the healthy archive emits everything.
	dest0 := tmpDir(t)
	base, err := Extract(out, FormatEML, dest0, false, discard(), nil)
	if err != nil || base.Emitted != 3 || base.Skipped != 0 {
		t.Fatalf("positive twin: emitted=%d skipped=%d err=%v", base.Emitted, base.Skipped, err)
	}

	archiveEML := collectFiles(t, out, ".eml")
	var linkRel, rotRel string
	for rel := range archiveEML {
		switch {
		case strings.Contains(strings.ToLower(rel), "link"):
			linkRel = rel
		case strings.Contains(strings.ToLower(rel), "rot"):
			rotRel = rel
		}
	}
	if linkRel == "" || rotRel == "" {
		t.Fatalf("could not identify the link/rot .eml among %v", keys(archiveEML))
	}

	// Replace one .eml with a symlink pointing at a secret outside the archive;
	// extract must never follow it.
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
	// Corrupt the other .eml by APPENDING bytes to the valid original: its prefix
	// still matches the recorded digest, so only reading one byte past the
	// recorded size (as verify does) catches the length mismatch (PC13).
	rotFull := filepath.Join(out, filepath.FromSlash(rotRel))
	if werr := os.WriteFile(rotFull, append(append([]byte{}, archiveEML[rotRel]...), []byte("EXTRA-APPENDED-BYTES")...), 0o644); werr != nil {
		t.Fatal(werr)
	}

	snap := snapshotArchive(t, out)

	dest := tmpDir(t)
	rep, xerr := Extract(out, FormatEML, dest, false, discard(), nil)
	if xerr != nil {
		t.Fatalf("extract errored instead of skipping: %v", xerr)
	}
	if rep.Emitted != 1 {
		t.Errorf("emitted=%d, want 1 (only the healthy record)", rep.Emitted)
	}
	if rep.SkippedGate < 1 || rep.SkippedFixity != 1 {
		t.Errorf("want the symlink skipped by the gate and the fixity mismatch counted: %+v", rep)
	}
	// The symlink was never followed: the secret's bytes appear in no output.
	for _, data := range collectFiles(t, dest, ".eml") {
		if bytes.Contains(data, []byte("TOP SECRET")) {
			t.Errorf("extract followed a symlinked .eml and copied the secret")
		}
	}
	// extract wrote nothing into the archive.
	if after := snapshotArchive(t, out); after != snap {
		t.Errorf("extract mutated the archive (a message file changed)")
	}

	// Oversized: shrink the cap and confirm the healthy .eml is skipped, not slurped.
	saved := maxExtractEMLBytes
	maxExtractEMLBytes = 4
	t.Cleanup(func() { maxExtractEMLBytes = saved })
	dest2 := tmpDir(t)
	rep2, e2 := Extract(out, FormatEML, dest2, false, discard(), nil)
	if e2 != nil {
		t.Fatalf("extract errored on oversized: %v", e2)
	}
	if rep2.Emitted != 0 {
		t.Errorf("with a 4-byte cap nothing should be emitted, got emitted=%d", rep2.Emitted)
	}
	if !summaryContains(ExtractSummary(rep2), "larger than") {
		t.Errorf("oversized skip not named in the summary: %v", ExtractSummary(rep2))
	}
}

// covers: MA-184, R20, R4, S34
// extract refuses a -dest that equals, sits inside, or contains -out
// (symlink-resolved), naming the overlap and writing nothing (fail-closed).
func TestExtractRefusesDestOverlap(t *testing.T) {
	out := buildRawArchive(t, map[string][][]byte{"Alpha": {msg("One", "one", "body")}})
	// Positive twin first: a non-overlapping dest emits.
	good := tmpDir(t)
	if r, err := Extract(out, FormatEML, good, false, discard(), nil); err != nil || r.Emitted != 1 {
		t.Fatalf("positive twin failed: emitted=%d err=%v", r.Emitted, err)
	}

	inside := filepath.Join(out, "sub", "dir")
	parent := filepath.Dir(out)
	for _, dest := range []string{out, inside, parent} {
		snap := snapshotArchive(t, out)
		_, err := Extract(out, FormatEML, dest, false, discard(), nil)
		if err == nil {
			t.Errorf("-dest %q overlapping -out was not refused", dest)
			continue
		}
		if !strings.Contains(err.Error(), "-dest") || !strings.Contains(err.Error(), "-out") {
			t.Errorf("overlap refusal does not name -dest/-out: %v", err)
		}
		for _, bad := range []string{"panic", "goroutine"} {
			if strings.Contains(err.Error(), bad) {
				t.Errorf("refusal leaked a crash: %v", err)
			}
		}
		if after := snapshotArchive(t, out); after != snap {
			t.Errorf("a refused overlap mutated the archive")
		}
		// The refused inside-dest must not have populated an output under -out.
		if _, statErr := os.Stat(filepath.Join(out, "sub")); statErr == nil {
			t.Errorf("refused inside -dest created output under -out")
		}
	}
}

// covers: MA-183, R20, S34, R12
// A PST-only archive (support.pst, no preserved originals) emits nothing: every
// record is skipped with the "no preserved original" reason, and the report's
// exit code is the partial code (3), never 0 and never verify's 2.
func TestExtractPSTArchiveEmitsNothing(t *testing.T) {
	out := tmpDir(t)
	opts := Options{Inputs: []string{"../../testdata/support.pst"}, Out: out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: true}
	r, err := Run(context.Background(), opts, discard(), nil)
	if err != nil {
		t.Fatalf("export pst: %v", err)
	}
	if r.Stats.Exported == 0 {
		t.Fatal("pst fixture exported nothing")
	}
	dest := tmpDir(t)
	rep, xerr := Extract(out, FormatMbox, dest, false, discard(), nil)
	if xerr != nil {
		t.Fatalf("extract refused a valid PST archive: %v", xerr)
	}
	if rep.Emitted != 0 {
		t.Errorf("a PST archive has no originals; emitted=%d, want 0", rep.Emitted)
	}
	if rep.Skipped == 0 || rep.SkippedNoEML != rep.Skipped {
		t.Errorf("every record should be skipped as no-eml: %+v", rep)
	}
	if rep.ExitCode() != ExtractPartial {
		t.Errorf("exit code = %d, want the partial code %d", rep.ExitCode(), ExtractPartial)
	}
	if rep.ExitCode() == 2 {
		t.Errorf("extract must not reuse verify's exit 2")
	}
	if !summaryContains(ExtractSummary(rep), "NOTHING extractable") {
		t.Errorf("summary should say nothing is extractable: %v", ExtractSummary(rep))
	}
	// No .mbox was written for an archive with nothing to emit.
	if m := collectFiles(t, dest, ".mbox"); len(m) != 0 {
		t.Errorf("an empty extract wrote %d .mbox file(s)", len(m))
	}
}

// covers: MA-183, R20, S34, R12
// extract refuses an archive with no manifest, naming the file, and an unknown
// -format, each with a typed error (never a crash).
func TestExtractRefusals(t *testing.T) {
	empty := tmpDir(t)
	dest := tmpDir(t)
	_, err := Extract(empty, FormatMbox, dest, false, discard(), nil)
	if err == nil || !strings.Contains(err.Error(), ".mailarchive-manifest.json") {
		t.Errorf("extract on a manifest-less archive should refuse naming the manifest: %v", err)
	}
	out := buildRawArchive(t, map[string][][]byte{"Alpha": {msg("One", "one", "body")}})
	if _, ferr := Extract(out, ExtractFormat("tgz"), tmpDir(t), false, discard(), nil); ferr == nil || !strings.Contains(ferr.Error(), "format") {
		t.Errorf("an unknown -format should be refused naming it: %v", ferr)
	}
}

// covers: MA-186, R20, S34
// verify on an archive with no preserved original bytes prints the not-extractable
// note (a heads-up before the source is deleted); an archive built with -raw does
// not carry the note (WithEML > 0).
func TestVerifyNotExtractableNote(t *testing.T) {
	// No-raw maildir archive: raw-capable source, but nothing preserved.
	src := filepath.Join(tmpDir(t), "Alpha")
	writeMaildirMsg(t, src, "1.eml", msg("One", "one", "body"))
	noRaw := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: noRaw, Mode: export.Incremental, Index: true, Pages: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	rep, err := Verify(noRaw, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rep.WithEML != 0 {
		t.Fatalf("a no-raw archive should have WithEML=0, got %d", rep.WithEML)
	}
	if !summaryContains(VerifySummary(rep), "no preserved original bytes (.eml)") {
		t.Errorf("verify summary lacks the not-extractable note:\n%s", strings.Join(VerifySummary(rep), "\n"))
	}

	// A -raw archive of the same source carries the originals; no note.
	raw := buildRawArchive(t, map[string][][]byte{"Alpha": {msg("One", "one", "body")}})
	rrep, err := Verify(raw, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify raw: %v", err)
	}
	if rrep.WithEML == 0 {
		t.Fatalf("a -raw archive should have WithEML>0")
	}
	if summaryContains(VerifySummary(rrep), "no preserved original bytes (.eml)") {
		t.Errorf("a -raw archive must not carry the not-extractable note")
	}
}

// snapshotArchive returns a stable digest of every message file the archive
// holds (its .html/.zip/.eml and the manifest), so a test can prove extract
// changed none of them. The lock file is excluded (extract legitimately holds it).
func snapshotArchive(t *testing.T, out string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Name() == lockfile.Name {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		rel, _ := filepath.Rel(out, path)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		b.WriteString(filepath.ToSlash(rel))
		b.WriteByte(':')
		b.WriteString(string(rune(info.Size())))
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func summaryContains(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func keys(m map[string][]byte) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	return k
}
