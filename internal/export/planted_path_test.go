package export

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// covers: MA-232, R13, S30
// S2B-AI9: a manifest planted by someone else cannot make a re-export delete files outside
// the archive. After a re-capture under a new name the exporter removes the record's
// previous page, zip and (with -raw) .eml, named by the record's Path; the Path was joined
// to the archive root as it stood, so a record whose Path is "../victim.html" (or an
// absolute path, or one that walks out through "..") deleted the owner's files beside the
// archive, whenever a run re-wrote that record (a Full re-export, a stub replacing a
// legacy placeholder, a stale stub healing). A recorded Path that is not a safe in-archive
// path is not deleted from. The twin first: an honest renamed capture still removes its own
// previous files.
func TestAPlantedManifestPathDeletesNothingOutsideTheArchive(t *testing.T) {
	mail := func(subject string) []byte {
		return []byte("From: a@example.com\r\nSubject: " + subject + "\r\nMessage-ID: <planted@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody\r\n")
	}
	run := func(t *testing.T, planted string) (root string, gone []string) {
		root = t.TempDir()
		out := filepath.Join(root, "out")
		manifest, err := state.Load(filepath.Join(out, "m.json"))
		if err != nil {
			t.Fatal(err)
		}
		e := &Exporter{OutDir: out, Manifest: manifest, Mode: Full, KeepRaw: true, Log: log.New(io.Discard, "", 0)}
		first := source.ParseRFC822(mail("first subject"))
		if _, err := e.Export("store", []string{"Inbox"}, first); err != nil {
			t.Fatal(err)
		}
		key := state.Key("store", "Inbox", first.Identity())
		rec, ok := manifest.Get(key)
		if !ok {
			t.Fatal("no record for the first capture")
		}
		for _, f := range []string{"victim.html", "victim.eml", "victim.zip"} {
			if err := os.WriteFile(filepath.Join(root, f), []byte("the owner's file"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// The attacker's edit of the manifest: the record's Path points out of the archive.
		own := filepath.Join(out, filepath.FromSlash(rec.Path))
		if planted != "" {
			rec.Path = planted
			manifest.Add(key, rec)
		}
		// A Full run writes the same message again: its page is named as before, and the
		// record's Path (planted, or the honest earlier one) is the one it deletes.
		if planted == "" {
			rec.Path = "store/Inbox/an-older-name.html"
			manifest.Add(key, rec)
			own = filepath.Join(out, "store", "Inbox", "an-older-name.html")
			if err := os.WriteFile(own, []byte("the page under its older name"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		second := source.ParseRFC822(mail("first subject"))
		if _, err := e.Export("store", []string{"Inbox"}, second); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"victim.html", "victim.eml", "victim.zip"} {
			if _, err := os.Stat(filepath.Join(root, f)); err != nil {
				gone = append(gone, f)
			}
		}
		if planted == "" {
			if _, err := os.Stat(own); err == nil {
				t.Errorf("twin: the record's own previous page %s is still there; an honest re-export removes it", own)
			}
		}
		return root, gone
	}

	if _, gone := run(t, ""); len(gone) != 0 {
		t.Fatalf("twin: %v; the victim files are not the record's, and are never removed", gone)
	}
	for _, planted := range []string{"../victim.html", "../../" + "victim.html", "sub/../../victim.html"} {
		if _, gone := run(t, planted); len(gone) != 0 {
			t.Errorf("a record planted with the path %q: the re-export deleted %v outside the archive", planted, gone)
		}
	}
}
