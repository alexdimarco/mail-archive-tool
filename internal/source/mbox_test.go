package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
)

const sampleMbox = "From - Mon Jan 01 00:00:00 2024\r\n" +
	"From: Bob <bob@example.com>\r\n" +
	"To: Me <me@example.com>\r\n" +
	"Cc: team@example.com\r\n" +
	"Subject: Hello HTML\r\n" +
	"Date: Mon, 01 Jan 2024 10:00:00 +0000\r\n" +
	"Message-ID: <abc@example.com>\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"\r\n" +
	"<p>Hello <b>world</b> invoice</p>\r\n" +
	"\r\n" +
	"From - Tue Jan 02 00:00:00 2024\r\n" +
	"From: Carol <carol@example.com>\r\n" +
	"Subject: Plain note\r\n" +
	"Date: Tue, 02 Jan 2024 09:00:00 +0000\r\n" +
	"Message-ID: <def@example.com>\r\n" +
	"\r\n" +
	"just plain text here\r\n"

func writeMbox(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sampleMbox), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// covers: MA-13
func TestMboxReaderFile(t *testing.T) {
	path := writeMbox(t, "TestBox")

	if !looksLikeMbox(path) {
		t.Fatal("file not detected as mbox")
	}

	src, err := Open(path) // dispatcher: no extension + "From " → mbox
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	if src.StoreName() != "TestBox" {
		t.Errorf("store name = %q, want TestBox", src.StoreName())
	}

	var msgs []*model.Message
	var folders [][]string
	if err := src.Walk(func(fp []string, m *model.Message) error {
		folders = append(folders, fp)
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}

	// Single mbox file: messages sit directly under the store (no folder path).
	if len(folders[0]) != 0 {
		t.Errorf("single-file folder path should be empty, got %v", folders[0])
	}

	m0 := msgs[0]
	if m0.Subject != "Hello HTML" || m0.SenderEmail != "bob@example.com" {
		t.Errorf("msg0 headers wrong: %q / %q", m0.Subject, m0.SenderEmail)
	}
	if m0.InternetMessageID != "abc@example.com" {
		t.Errorf("msg0 message-id = %q", m0.InternetMessageID)
	}
	if !strings.Contains(m0.HTMLBody, "<b>world</b>") {
		t.Errorf("msg0 html body missing: %q", m0.HTMLBody)
	}
	if m0.To != "Me <me@example.com>" {
		t.Errorf("msg0 To = %q", m0.To)
	}
	if m0.Received.Year() != 2024 {
		t.Errorf("msg0 date not parsed: %v", m0.Received)
	}

	m1 := msgs[1]
	if m1.Subject != "Plain note" || !strings.Contains(m1.PlainBody, "plain text here") {
		t.Errorf("msg1 wrong: %q / %q", m1.Subject, m1.PlainBody)
	}
}

// covers: MA-14
func TestIsMailStoreDir(t *testing.T) {
	// A directory containing an mbox file is a mail store.
	path := writeMbox(t, "Inbox")
	dir := filepath.Dir(path)
	if !IsMailStoreDir(dir) {
		t.Error("directory with an mbox file should be a mail store")
	}

	// A directory of .pst files is not (it gets expanded instead).
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "a.pst"), []byte("not mbox"), 0o644)
	if IsMailStoreDir(other) {
		t.Error("directory of .pst files should not be treated as a mail store")
	}

	// Opening the mail-store directory walks the mbox as a folder.
	src, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	var count int
	var firstFolder []string
	src.Walk(func(fp []string, m *model.Message) error {
		if count == 0 {
			firstFolder = fp
		}
		count++
		return nil
	})
	if count != 2 {
		t.Errorf("walk dir got %d messages, want 2", count)
	}
	if len(firstFolder) != 1 || firstFolder[0] != "Inbox" {
		t.Errorf("folder path = %v, want [Inbox]", firstFolder)
	}
}
