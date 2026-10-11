package export

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-318, R1, R10
// S2A-AC7 (the export side): a panic out of an attachment's stream reaches the caller,
// which records the message as an export-panic gap, but WriteZip first removes its temp
// file and closes it. A panic skipped abort() and left a .mailarchive-*.tmp, and its
// descriptor, behind in the archive for every such message.
func TestWriteZipLeavesNoTempFileWhenAnAttachmentPanics(t *testing.T) {
	dir := t.TempDir()
	atts := []model.Attachment{
		{Filename: "fine.txt", WriteTo: func(w io.Writer) (int64, error) { n, err := w.Write([]byte("ok")); return int64(n), err }},
		{Filename: "boom.bin", WriteTo: func(io.Writer) (int64, error) { panic("go-pst: nil reader") }},
	}
	var rec any
	func() {
		defer func() { rec = recover() }()
		_, _ = WriteZip(filepath.Join(dir, "m-attachments.zip"), atts, nil)
	}()
	if rec == nil {
		t.Fatal("the panic was swallowed: the caller records it as the message's export-panic gap")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("the directory holds %d entries (err %v) after the panic; want none: no temp zip left behind", len(entries), err)
	}
}
