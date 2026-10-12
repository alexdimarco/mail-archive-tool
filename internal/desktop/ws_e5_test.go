package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The extraction workspace's slice E5, the desktop's half (design
// docs/design-extraction-workspace.md §4.1, §4.11, §4.17): the Readable HTML toggle, its
// words, its default and what it decides.

// covers: MA-445, MA-423, R12, S52
// The toggle decides the export: with Readable HTML on (the default) the preview says so and
// sizes it, the run writes a readable copy beside each original (role derived-html, listed in
// the manifest) and the manifest's options say so; with it off the same selection writes
// originals only; the page's script sends the toggle with the selection and shows the
// preview's Readable HTML line only when it is on; and the choice is remembered by the start.
func TestReadableHTMLToggleDecidesTheExport(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	run := func(html bool) (map[string]any, map[string]any) {
		t.Helper()
		sel := e.selection(entry(ids["Sent Items"], false))
		sel["options"] = map[string]any{"preserve": true, "html": html}
		id, res := e.preview(sel)
		e.ok("/api/exports/start", map[string]any{"previewId": id})
		e.waitRun()
		return res, e.manifestOf(res["exportName"].(string))
	}

	res, man := run(true)
	if res["html"] != true {
		t.Errorf("the preview does not say Readable HTML is on: %v", res["html"])
	}
	opts := man["options"].(map[string]any)
	if opts["readable_html"] != true {
		t.Errorf("the manifest's options: %v", opts)
	}
	msgs := man["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatal("the export holds no message")
	}
	root := filepath.Join(e.dest, res["exportName"].(string))
	for i, raw := range msgs {
		files := raw.(map[string]any)["files"].([]any)
		if len(files) != 2 || files[0].(map[string]any)["role"] != "original" || files[1].(map[string]any)["role"] != "derived-html" {
			t.Errorf("message %d: %v, want the original and its readable copy", i+1, files)
			continue
		}
		page := readFileString(t, filepath.Join(root, filepath.FromSlash(files[1].(map[string]any)["path"].(string))))
		if !strings.Contains(page, "Original message (.eml)") || !strings.HasPrefix(page, "<!DOCTYPE html>") {
			t.Errorf("message %d: the readable copy is not a MailArchive page:\n%.200s", i+1, page)
		}
	}
	if c := man["counts"].(map[string]any); c["original_only"] != nil && c["original_only"] != 0.0 {
		t.Errorf("counts: %v", c)
	}
	s := LoadSettings(e.cfg.SettingsPath)
	if s.ExportHTML == nil || !*s.ExportHTML {
		t.Errorf("a start with Readable HTML on did not remember it: %+v", s)
	}

	res, man = run(false)
	if res["html"] != false || man["options"].(map[string]any)["readable_html"] != false {
		t.Errorf("a start with Readable HTML off: preview %v, options %v", res["html"], man["options"])
	}
	for i, raw := range man["messages"].([]any) {
		if files := raw.(map[string]any)["files"].([]any); len(files) != 1 {
			t.Errorf("message %d: %v, want the original alone", i+1, files)
		}
	}
	if _, err := os.Stat(filepath.Join(e.dest, res["exportName"].(string))); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.ExportHTML == nil || *s.ExportHTML || s.ReadableHTML() {
		t.Errorf("a start with Readable HTML off did not remember it: %+v", s)
	}

	// the page: the toggle is sent with the selection, and the preview names it only when on
	for _, want := range []string{"html:$('optHTML').checked", "$('optHTML').checked=s.html!==false", "if(p.html)body.appendChild(line('Readable HTML'"} {
		if !strings.Contains(workspaceJS, want) {
			t.Errorf("workspace.js lacks %q", want)
		}
	}
}
