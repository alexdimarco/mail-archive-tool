package export

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// covers: MA-150, R4, S30
// A tampered store token — one that is not a single safe path segment ("..",
// absolute, or containing a separator) — must never make the exporter write
// outside the output root. The last-line guard in Export refuses such a token
// with a typed error that NAMES it and creates nothing outside <out>. Positive
// twin first: a clean single-segment token still exports inside <out>.
func TestExportRefusesUnsafeStoreToken(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "archive")
	manifest := mustManifest(t)
	msg := func() *model.Message {
		return &model.Message{Subject: "Hello", Received: testDate, InternetMessageID: "<x@x>", PlainBody: "body"}
	}

	// Positive twin: a clean token exports inside <out>.
	e := incExporter(out, manifest)
	wrote, err := e.Export("Inbox", []string{"Folder"}, msg())
	if err != nil || !wrote {
		t.Fatalf("clean token failed: wrote=%v err=%v", wrote, err)
	}
	assure.Reached(t, countSuffix(t, filepath.Join(out, "Inbox"), ".html"), "html under the clean store token")

	// Each unsafe token is refused, naming it, and leaves no side effect.
	for _, tok := range []string{"../escape", filepath.Join("..", "..", "victim"), "a/b", "..", "."} {
		wrote, err := e.Export(tok, []string{"Folder"}, msg())
		rc, m := 0, ""
		if err != nil {
			rc, m = 1, err.Error()
		}
		assure.Refused(t, rc, m, assure.Code(1), assure.Names(tok),
			assure.NoSideEffect(func() bool { return !wrote }))
	}

	// The concrete escape targets outside <out> must not exist.
	for _, escaped := range []string{
		filepath.Join(root, "victim"),
		filepath.Join(filepath.Dir(root), "victim"),
		filepath.Join(root, "escape"),
	} {
		if _, err := os.Stat(escaped); !os.IsNotExist(err) {
			t.Errorf("a write escaped the archive root: %s exists", escaped)
		}
	}
}
