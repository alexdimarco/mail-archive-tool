package source

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/model"
)

const sampleMsg = "From: Dana <dana@example.com>\r\n" +
	"To: Me <me@example.com>\r\n" +
	"Subject: Maildir hello\r\n" +
	"Date: Wed, 03 Jan 2024 08:00:00 +0000\r\n" +
	"Message-ID: <mdir1@example.com>\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"maildir body text\r\n"

// covers: MA-15
func TestMaildirReader(t *testing.T) {
	store := t.TempDir()
	// A maildir folder "Inbox" with a message in cur/.
	folder := filepath.Join(store, "Inbox")
	for _, sub := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(folder, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// No ':' in the name — it's illegal on Windows (an NTFS alternate-data-stream
	// separator). The reader reads every file in cur/ regardless of name.
	if err := os.WriteFile(filepath.Join(folder, "cur", "1700000000.abc.2,S"), []byte(sampleMsg), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsMailStoreDir(store) {
		t.Fatal("directory with a maildir folder should be a mail store")
	}

	src, err := Open(store)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	var msgs []*model.Message
	var folders [][]string
	if err := src.Walk(func(fp []string, m *model.Message) error {
		folders = append(folders, fp)
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Subject != "Maildir hello" || msgs[0].SenderEmail != "dana@example.com" {
		t.Errorf("headers wrong: %q / %q", msgs[0].Subject, msgs[0].SenderEmail)
	}
	if len(folders[0]) != 1 || folders[0][0] != "Inbox" {
		t.Errorf("folder = %v, want [Inbox]", folders[0])
	}
}
