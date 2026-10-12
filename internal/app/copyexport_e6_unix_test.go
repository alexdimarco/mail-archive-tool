//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-424, R4, R29, S52
// Extracted files go through the contained writer: a symlink planted at the name of the
// message's attachments directory before it is made, and the directory swapped for a symlink
// to the outside after it was made, are each not written through. The positive twin, no
// plant, first. With the plant the message is still exported with its original, nothing appears
// outside the export root, the part is a note that says it could not be written, and the other
// message goes on.
func TestExtractedFilesAreNotWrittenThroughAPlantedLink(t *testing.T) {
	msgs := ceMsgs(2)
	for i := range msgs {
		msgs[i].Received = msgs[0].Received // one month directory
	}
	mime := func(id string) []byte {
		return e6Mime(id, "one file", e6Part{name: "report.pdf", ctype: "application/pdf", body: []byte("%PDF " + id)})
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	// the positive twin: nothing planted, both messages' files are extracted
	e := newCEEnv(t)
	res, _ := e6Run(t, e, msgs, false, mime)
	lines := e5Files(t, e, res)
	if len(e6Derived(lines[1])) != 1 || len(e6Derived(lines[2])) != 1 || len(listOutside(t, outside)) != 0 {
		t.Fatalf("the positive twin: %+v, %+v", lines[1].Files, lines[2].Files)
	}

	plants := []struct {
		name string
		set  func(t *testing.T, e *CopyExportEnv, root string)
	}{
		{"a link where the directory goes", func(t *testing.T, e *CopyExportEnv, root string) {
			dir := filepath.Join(root, "messages", "Inbox", "2026-03")
			e.faults = &copyFaults{hook: func(step string) {
				if step != "W5read:1" {
					return
				}
				originals, _ := filepath.Glob(filepath.Join(dir, "*.eml"))
				if len(originals) != 1 {
					t.Errorf("the hook found %d originals, want 1", len(originals))
					return
				}
				if err := os.Symlink(outside, strings.TrimSuffix(originals[0], ".eml")+".attachments"); err != nil {
					t.Fatal(err)
				}
			}}
		}},
		{"the directory swapped for a link", func(t *testing.T, e *CopyExportEnv, root string) {
			e.fsWrap = func(f ceFS) ceFS { return &swapOnTemp{ceFS: f, t: t, outside: outside, root: root} }
		}},
	}
	for _, pl := range plants {
		t.Run(pl.name, func(t *testing.T) {
			e := newCEEnv(t)
			src := newFakeSrc()
			src.body = mime
			p := cePlan(t, e, src, msgs)
			p.Options.Attachments = true
			pl.set(t, e, filepath.Join(p.Dest, p.ExportName))
			res := mustRun(t, e, p, src)
			if got := listOutside(t, outside); len(got) != 0 {
				t.Fatalf("a link in the export was written through: %v appeared outside", got)
			}
			first := assure.Reached(t, e5Files(t, e, res)[1], "the first message's line")
			if first.Status != statusExported || len(first.Files) != 1 || first.Files[0].Role != roleOriginal {
				t.Errorf("the first message: %s, %+v", first.Status, first.Files)
			}
			if len(first.Notes) != 1 || !strings.Contains(first.Notes[0], "could not be written") || !strings.Contains(first.Notes[0], "report.pdf") {
				t.Errorf("the first message's notes: %q", first.Notes)
			}
			if res.Counts.Exported != 2 {
				t.Errorf("%s, %+v", res.Label, res.Counts)
			}
		})
	}
}

// swapOnTemp is a contained writer that, when the first temp file is made inside an
// attachments directory, swaps that directory for a link to the outside first, as a co-writer
// on the destination would between the directory's making and the file's.
type swapOnTemp struct {
	ceFS
	t       *testing.T
	outside string
	root    string
	done    bool
}

func (s *swapOnTemp) CreateTemp(dir string) (ceFile, string, error) {
	if !s.done && strings.HasSuffix(dir, ".attachments") {
		s.done = true
		swapForSymlink(s.t, filepath.Join(s.root, filepath.FromSlash(dir)), s.outside)
	}
	return s.ceFS.CreateTemp(dir)
}
