package export

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// covers: MA-30, R4
// Zip-slip tombstone: an attacker-named attachment must not become a zip entry
// whose extraction escapes the target directory.
func TestZipEntryNamesContained(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "a.zip")
	atts := []model.Attachment{
		{Filename: "../../../../etc/cron.d/evil", WriteTo: writeString("x")},
		{Filename: `..\..\Windows\System32\evil.dll`, WriteTo: writeString("y")},
		{Filename: "/abs/olute/passwd", WriteTo: writeString("z")},
	}
	n, _, err := WriteZip(zipPath, atts, map[int]bool{})
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, n, "archived attachments")

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	assure.Reached(t, zr.File, "zip entries")
	for _, f := range zr.File {
		if strings.ContainsAny(f.Name, `/\`) || strings.Contains(f.Name, "..") {
			t.Errorf("zip entry %q could escape the extraction directory", f.Name)
		}
	}
}

func writeString(s string) func(io.Writer) (int64, error) {
	return func(w io.Writer) (int64, error) {
		n, err := io.WriteString(w, s)
		return int64(n), err
	}
}
