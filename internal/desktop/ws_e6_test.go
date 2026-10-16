package desktop

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// The extraction workspace's slice E6, the desktop's half (design
// docs/design-extraction-workspace.md §4.1, §4.12): the Extract attachments toggle, the way the
// attachments are saved (one zip per message, the default, or loose files; owner ruling
// 2026-10-12), its words, its defaults and what it decides.

// covers: MA-424, MA-533, MA-445, R12, S52
// The toggle decides the export: with Extract attachments on the preview says so and says how, the run
// writes each message's attachments as ONE zip beside its original (the default; role
// derived-attachments-zip) or as loose files (role derived-attachment) when loose files are chosen, and
// the manifest's options say so; with it off (the default) the same selection writes originals only;
// the page's script sends the toggle and the choice with the selection, restores both, and shows the
// preview's line only when it is on; and the start remembers the toggle, and the choice when it was used.
func TestExtractAttachmentsToggleDecidesTheExport(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	for _, fo := range e.fg.folders {
		if fo.ID != "F-SENT" {
			continue
		}
		for _, m := range fo.Msgs {
			id := m.ID
			e.fg.valueFn[id] = func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("From: a@example.com\r\nSubject: with a file\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\nMIME-Version: 1.0\r\n" +
					"Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n--B\r\nContent-Type: text/plain\r\n\r\nsee the file\r\n--B\r\n" +
					"Content-Type: application/pdf; name=\"report.pdf\"\r\nContent-Disposition: attachment; filename=\"report.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nSGVsbG8gd29ybGQ=\r\n--B--\r\n"))
			}
		}
	}
	run := func(opts map[string]any) (map[string]any, map[string]any) {
		t.Helper()
		sel := e.selection(entry(ids["Sent Items"], false))
		sel["options"] = opts
		id, res := e.preview(sel)
		e.ok("/api/exports/start", map[string]any{"previewId": id})
		e.waitRun()
		return res, e.manifestOf(res["exportName"].(string))
	}

	// on, as one zip per message (the way the page sends when nothing else was chosen)
	res, man := run(map[string]any{"preserve": true, "html": false, "attachments": true, "attachmentsAs": "zip"})
	opts := man["options"].(map[string]any)
	if res["attachments"] != true || res["attachmentsZip"] != true || opts["extract_attachments"] != true || opts["attachments_as_zip"] != true {
		t.Errorf("the preview says %v / %v, the manifest's options %v", res["attachments"], res["attachmentsZip"], opts)
	}
	msgs := man["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatal("the export holds no message")
	}
	root := filepath.Join(e.dest, res["exportName"].(string))
	for i, raw := range msgs {
		files := raw.(map[string]any)["files"].([]any)
		if len(files) != 2 || files[0].(map[string]any)["role"] != "original" || files[1].(map[string]any)["role"] != "derived-attachments-zip" {
			t.Errorf("message %d: %v, want the original and its attachments zip", i+1, files)
			continue
		}
		p := files[1].(map[string]any)["path"].(string)
		zb := []byte(readFileString(t, filepath.Join(root, filepath.FromSlash(p))))
		zr, err := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
		if err != nil || !strings.HasSuffix(p, ".attachments.zip") || len(zr.File) != 1 || zr.File[0].Name != "report.pdf" {
			t.Errorf("message %d: the attachments zip is %q (%v)", i+1, p, err)
			continue
		}
		rc, _ := zr.File[0].Open()
		if body, _ := io.ReadAll(rc); string(body) != "Hello world" {
			t.Errorf("message %d: the entry holds %q", i+1, body)
		}
		rc.Close()
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.ExportAttachments == nil || !*s.ExportAttachments || !s.ExtractAttachments() || s.AttachmentsAs() != "zip" {
		t.Errorf("a start with Extract attachments on did not remember it: %+v", s)
	}

	// on, as loose files
	res, man = run(map[string]any{"preserve": true, "html": false, "attachments": true, "attachmentsAs": "loose"})
	opts = man["options"].(map[string]any)
	if res["attachments"] != true || res["attachmentsZip"] != false || opts["extract_attachments"] != true || opts["attachments_as_zip"] != nil {
		t.Errorf("loose files: the preview says %v / %v, the manifest's options %v", res["attachments"], res["attachmentsZip"], opts)
	}
	root = filepath.Join(e.dest, res["exportName"].(string))
	for i, raw := range man["messages"].([]any) {
		files := raw.(map[string]any)["files"].([]any)
		if len(files) != 2 || files[1].(map[string]any)["role"] != "derived-attachment" {
			t.Errorf("message %d: %v, want the original and its attachment", i+1, files)
			continue
		}
		p := files[1].(map[string]any)["path"].(string)
		if !strings.HasSuffix(p, ".attachments/report.pdf") || readFileString(t, filepath.Join(root, filepath.FromSlash(p))) != "Hello world" {
			t.Errorf("message %d: the attachment is %q", i+1, p)
		}
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.AttachmentsAs() != "loose" || !s.ExtractAttachments() {
		t.Errorf("a start with loose files did not remember it: %+v", s)
	}
	if src := e.ok("/api/workspace/source", nil)["settings"].(map[string]any); src["attachments"] != true || src["attachmentsAs"] != "loose" {
		t.Errorf("the page's remembered settings: %v", src)
	}

	// off (the default): originals only, and the way chosen is not forgotten by a start that did not use it
	res, man = run(map[string]any{"preserve": true, "html": false, "attachments": false, "attachmentsAs": "zip"})
	if res["attachments"] != false || man["options"].(map[string]any)["extract_attachments"] != false {
		t.Errorf("a start with it off: preview %v, options %v", res["attachments"], man["options"])
	}
	for i, raw := range man["messages"].([]any) {
		if files := raw.(map[string]any)["files"].([]any); len(files) != 1 {
			t.Errorf("message %d: %v, want the original alone", i+1, files)
		}
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.ExportAttachments == nil || *s.ExportAttachments || s.ExtractAttachments() || s.AttachmentsAs() != "loose" {
		t.Errorf("a start with it off did not remember it (or forgot the way): %+v", s)
	}

	// the page: the toggle and the choice are sent with the selection and restored from the settings,
	// the choice only while the toggle is on, and the preview names it only when on
	for _, want := range []string{"attachments:$('optAttach').checked,attachmentsAs:$('optAttachLoose').checked?'loose':'zip'", "$('optAttach').checked=s.attachments===true",
		"$(s.attachmentsAs==='loose'?'optAttachLoose':'optAttachZip').checked=true", "$('optAttachZip').disabled=!on", "if(p.attachments)body.appendChild(line('Extract attachments'"} {
		if !strings.Contains(workspaceJS, want) {
			t.Errorf("workspace.js lacks %q", want)
		}
	}
}
