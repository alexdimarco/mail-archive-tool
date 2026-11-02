package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/state"
)

// covers: MA-1035, MA-1023, R36
// add-archive refuses, in its "cannot be read — run mailarchive status" words, an archive whose
// manifest's capture_gaps or collapsed_tokens is of a type no reader of the whole manifest takes
// (MA-1035 amended 2026-11-02, the bounded walk's second adversarial pass): before, recognition
// skipped both fields unchecked, so add-archive listed a folder that status and every backup then
// refuse. Nothing is listed and the folder is unchanged. Positive twin first: the same archive with
// each field well typed is listed.
func TestAddArchiveRefusesAManifestWhoseFieldsBesideTheRecordsNoReaderTakes(t *testing.T) {
	e := newCLIDel(t)
	set := func(dir, name, raw string) {
		t.Helper()
		man := filepath.Join(dir, ".mailarchive-manifest.json")
		b, err := os.ReadFile(man)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("the fixture: %v", err)
		}
		doc[name] = json.RawMessage(raw)
		if b, err = json.Marshal(doc); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(man, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	twin := filepath.Join(t.TempDir(), "Twin")
	e.archiveAt(twin)
	set(twin, "capture_gaps", `{}`)
	set(twin, "collapsed_tokens", `{"a":true}`)
	if _, err := state.LoadFrom("twin", readFile(t, filepath.Join(twin, ".mailarchive-manifest.json"))); err != nil {
		t.Fatalf("the positive twin's manifest must be read whole: %v", err)
	}
	if err := addArchiveWith("", twin, io.Discard); err != nil || !e.listed(twin) {
		t.Fatalf("the positive twin: %v (listed %v)", err, e.listed(twin))
	}

	for _, c := range []struct{ name, raw string }{
		{"capture_gaps", `5`},
		{"capture_gaps", `[1,2]`},
		{"collapsed_tokens", `"yes"`},
		{"collapsed_tokens", `{"a":"x"}`},
	} {
		dir := filepath.Join(t.TempDir(), "Damaged")
		e.archiveAt(dir)
		set(dir, c.name, c.raw)
		if _, err := state.LoadFrom("damaged", readFile(t, filepath.Join(dir, ".mailarchive-manifest.json"))); err == nil {
			t.Fatalf("%s %s: the fixture: a whole read must refuse it", c.name, c.raw)
		}
		listBefore, folderBefore := readFile(t, e.indexPath()), tree(t, dir)
		err := addArchiveWith("", dir, io.Discard)
		assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
			assure.Names(dir+" holds an archive whose record (.mailarchive-manifest.json) cannot be read — run mailarchive status -out "+encmark.CommandPath(dir)+" to see why and how to repair it. Nothing was changed."),
			assure.NoSideEffect(func() bool {
				return bytes.Equal(listBefore, readFile(t, e.indexPath())) && sameTree(folderBefore, tree(t, dir)) && !e.listed(dir)
			}))
	}
}
