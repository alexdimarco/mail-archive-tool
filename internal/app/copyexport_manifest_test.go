package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-421, R29, S52
// The manifest and the sums of a finished export: export-manifest.json holds the
// required fields (format and version, tool, run, source, selection, options,
// counts, hash, one entry per message with its files' roles, sha256 and size,
// failures, not_attempted); every file except SHA256SUMS.txt and the marker is
// listed in SHA256SUMS.txt, sorted, in coreutils format; an in-Go `sha256sum -c`
// mirror passes on the fresh export, a flipped byte in any file makes it report that
// path, and a planted, unlisted file is reported by the mirror's extra-files check.
func TestCopyExportManifestAndSums(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	msgs := ceMsgs(5)
	p := cePlan(t, e, src, msgs)
	res := mustRun(t, e, p, src)
	if res.Status != CopyStatusComplete || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("a healthy run is %s, outcome %d: %+v", res.Status, res.Outcome, res.Stop)
	}
	root := res.Root

	// Positive twin first: the fresh export verifies, and nothing is unlisted.
	bad, missing, extra := sumsCheck(t, root)
	if len(bad)+len(missing)+len(extra) != 0 {
		t.Fatalf("a fresh export fails its own check: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
	if exists(root, copyMarkerName) {
		t.Error("a complete export keeps its marker")
	}

	m := manifestOf(t, root)
	for _, k := range []string{"format", "format_version", "tool", "run", "source", "selection", "options", "counts", "hash", "messages", "failures", "not_attempted"} {
		if _, ok := m[k]; !ok {
			t.Errorf("the manifest has no %q", k)
		}
	}
	if m["format"] != "mailarchive-copy-export" || m["format_version"] != float64(1) || m["hash"] != "SHA-256" {
		t.Errorf("manifest identity wrong: %v %v %v", m["format"], m["format_version"], m["hash"])
	}
	run := m["run"].(map[string]any)
	if run["status"] != "complete" || run["valid_if_absent"] != copyMarkerName || run["id"] != res.RunID {
		t.Errorf("manifest run = %v", run)
	}
	if want := map[string]any{"selected": 5.0, "exported": 5.0, "failed": 0.0, "not_attempted": 0.0, "without_original": 0.0}; !reflect.DeepEqual(m["counts"], want) {
		t.Errorf("counts = %v, want %v", m["counts"], want)
	}
	ents := assure.Reached(t, m["messages"].([]any), "manifest messages")
	if len(ents) != 5 {
		t.Fatalf("%d manifest entries, want 5", len(ents))
	}
	listed := map[string]bool{}
	for i, raw := range ents {
		ent := raw.(map[string]any)
		if ent["source_id"] != msgs[i].SourceID || ent["status"] != "exported" || ent["folder"] != "Inbox" {
			t.Errorf("entry %d = %v", i, ent)
		}
		files := ent["files"].([]any)
		if len(files) != 1 {
			t.Fatalf("entry %d has %d files", i, len(files))
		}
		f := files[0].(map[string]any)
		if f["role"] != "original" || len(f["sha256"].(string)) != 64 || f["size"] != float64(len(ceBody(msgs[i].SourceID))) {
			t.Errorf("entry %d file = %v", i, f)
		}
		listed[f["path"].(string)] = true
	}
	// Every file but the sums and the marker is in the sums: the manifest's files, README, the manifest.
	sums := readText(t, root, copySumsName)
	for p := range treeFiles(t, root) {
		if p == copySumsName {
			continue
		}
		if !strings.Contains(sums, "  "+p+"\n") {
			t.Errorf("%s is not in %s", p, copySumsName)
		}
	}
	for p := range listed {
		if !strings.Contains(sums, "  "+p+"\n") {
			t.Errorf("%s (a manifest file) is not in the sums", p)
		}
	}
	if !strings.Contains(sums, "  "+copyReadmeName+"\n") || !strings.Contains(sums, "  "+copyManifestName+"\n") {
		t.Error("README and the manifest are not in the sums")
	}

	// A flipped byte in a message file is reported by path.
	var victim string
	for p := range listed {
		victim = p
		break
	}
	full := filepath.Join(root, filepath.FromSlash(victim))
	orig, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	flipped := append([]byte(nil), orig...)
	flipped[len(flipped)-3] ^= 0xff
	os.WriteFile(full, flipped, 0o600)
	if bad, _, _ := sumsCheck(t, root); len(bad) != 1 || bad[0] != victim {
		t.Errorf("a flipped byte in %s was reported as %v", victim, bad)
	}
	os.WriteFile(full, orig, 0o600)

	// A planted file that is not listed is reported by the extra-files check.
	os.WriteFile(filepath.Join(root, "planted.txt"), []byte("x"), 0o600)
	if _, _, extra := sumsCheck(t, root); len(extra) != 1 || extra[0] != "planted.txt" {
		t.Errorf("a planted file was reported as %v", extra)
	}
}
