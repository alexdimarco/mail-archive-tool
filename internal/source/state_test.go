package source

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-177, R3, R1, S35
// The PST reader captures a message's read/importance/sensitivity state.
// Proven two ways: on the real support.pst fixture (every item is normal
// priority, no sensitivity, and READ — flags carry the mfRead bit — so the
// reader must yield Importance=="" / Sensitivity=="" / Unread==false, which
// pins the mfRead polarity through the real reader), and on the pure mapping
// functions for the low/high/confidential/unread cases the fixture does not
// contain. The state is captured but never mixed into identity (R3/R1).
func TestPSTMessageStateCaptured(t *testing.T) {
	// The mapping functions the reader uses, across every defined value.
	if got := importanceString(0); got != "low" {
		t.Errorf("importanceString(0)=%q, want low", got)
	}
	if got := importanceString(1); got != "" {
		t.Errorf("importanceString(1)=%q, want empty (normal)", got)
	}
	if got := importanceString(2); got != "high" {
		t.Errorf("importanceString(2)=%q, want high", got)
	}
	for v, want := range map[int32]string{0: "", 1: "personal", 2: "private", 3: "confidential"} {
		if got := sensitivityString(v); got != want {
			t.Errorf("sensitivityString(%d)=%q, want %q", v, got, want)
		}
	}
	// mfRead is bit 0x1: set → read, absent → unread.
	if unreadFromMessageFlags(0x1) || unreadFromMessageFlags(1025) {
		t.Error("a flags value with the mfRead bit set must read as read (Unread=false)")
	}
	if !unreadFromMessageFlags(0) || !unreadFromMessageFlags(0x8) {
		t.Error("a flags value without the mfRead bit must read as unread (Unread=true)")
	}

	// End-to-end on the real fixture.
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	r, err := Open(fixture)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	var n int
	err = r.Walk(func(_ []string, m *model.Message) error {
		n++
		// The fixture's items are all normal/none/read; the reader must reflect
		// that exactly (a spurious value here would mean it mis-read a property
		// or inverted the mfRead test).
		if m.Importance != "" {
			t.Errorf("fixture message %q captured Importance=%q, want empty", m.Subject, m.Importance)
		}
		if m.Sensitivity != "" {
			t.Errorf("fixture message %q captured Sensitivity=%q, want empty", m.Subject, m.Sensitivity)
		}
		if m.Unread {
			t.Errorf("fixture message %q captured Unread=true, want false (its mfRead bit is set)", m.Subject)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if n == 0 {
		t.Fatal("fixture yielded no messages")
	}
}

// covers: MA-178, R1, S35
// The raw-bytes readers capture message state: Importance/X-Priority and
// Sensitivity from headers, and Unread from the Thunderbird X-Mozilla-Status
// read bit when present, else the maildir "S" (Seen) info flag in the filename.
func TestMboxMaildirUnread(t *testing.T) {
	// --- header-driven state, via the shared RFC822 path (Thunderbird mbox). ---
	hdr := func(extra string) []byte {
		return []byte("From: a@example.com\r\nSubject: s\r\nMessage-ID: <s@x>\r\n" +
			"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\n" + extra + "\r\nbody\r\n")
	}
	// X-Mozilla-Status 0001 has the read bit set → read.
	if m := ParseRFC822(hdr("X-Mozilla-Status: 0001\r\n")); m.Unread {
		t.Error("X-Mozilla-Status 0001 (read bit set) must be Unread=false")
	}
	// X-Mozilla-Status 0000 lacks the read bit → unread.
	if m := ParseRFC822(hdr("X-Mozilla-Status: 0000\r\n")); !m.Unread {
		t.Error("X-Mozilla-Status 0000 (read bit clear) must be Unread=true")
	}
	if m := ParseRFC822(hdr("Importance: high\r\nSensitivity: Company-Confidential\r\n")); m.Importance != "high" || m.Sensitivity != "confidential" {
		t.Errorf("Importance/Sensitivity headers: got %q/%q, want high/confidential", m.Importance, m.Sensitivity)
	}
	if m := ParseRFC822(hdr("X-Priority: 5\r\n")); m.Importance != "low" {
		t.Errorf("X-Priority 5: got Importance=%q, want low", m.Importance)
	}
	// No read-state signal at all (plain mbox message) → not claimed unread.
	if m := ParseRFC822(hdr("")); m.Unread {
		t.Error("a message with no read-state signal must default to Unread=false")
	}

	// --- filename-driven state (maildir "S" flag), via the reader. ---
	store := t.TempDir()
	inbox := filepath.Join(store, "Inbox")
	for _, sub := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(inbox, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	msg := func(subject, extra string) []byte {
		return []byte("From: A <a@example.com>\r\nSubject: " + subject + "\r\n" +
			"Message-ID: <" + subject + "@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n" +
			extra + "\r\nbody of " + subject + "\r\n")
	}
	write := func(sub, name, subject, extra string) {
		if err := os.WriteFile(filepath.Join(inbox, sub, name), msg(subject, extra), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cur", "1700000000.a.host.2,S", "seen", "")                              // Seen flag → read
	write("cur", "1700000001.b.host.2,", "unseen-cur", "")                         // info but no S → unread
	write("new", "1700000002.c.host", "new-msg", "")                               // in new/, no info → unread
	write("cur", "1700000003.d.host.2,", "moz-read", "X-Mozilla-Status: 0001\r\n") // header wins over missing S

	src, err := Open(store)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	unread := map[string]bool{}
	if err := src.Walk(func(_ []string, m *model.Message) error {
		unread[m.Subject] = m.Unread
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"seen": false, "unseen-cur": true, "new-msg": true, "moz-read": false}
	for subj, w := range want {
		got, ok := unread[subj]
		if !ok {
			t.Errorf("maildir message %q not read", subj)
			continue
		}
		if got != w {
			t.Errorf("maildir message %q: Unread=%v, want %v", subj, got, w)
		}
	}
}
