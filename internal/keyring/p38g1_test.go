package keyring

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// covers: MA-850, R37
// The saved recovery-key file names its archive by folder and day beside its id,
// on comment lines a control character in the folder's name cannot break, and
// still reads back as the key (P3-8 friction review, B24).
func TestTheRecoveryKeyFileNamesItsFolderAndDay(t *testing.T) {
	freshRecord(t)
	out, rk, a := made(t, nil)
	day := time.Date(2026, 10, 27, 9, 0, 0, 0, time.Local)
	text := RecoveryKeyFileText(a.ArchiveID(), rk, out, day)
	assure.Reached(t, strings.Contains(text, "in "+out) && strings.Contains(text, "made 2026-10-27") && strings.Contains(text, a.ArchiveID().String()),
		"the folder, the day and the id: "+text)
	p := filepath.Join(t.TempDir(), "rk.txt")
	os.WriteFile(p, []byte(text), 0o600)
	if s, err := RecoveryKeyFile(p); err != nil || !s.IsRecoveryKey() {
		t.Fatalf("it does not read back: %v", err)
	}
	// A line break in a folder's name stays inside its comment.
	odd := RecoveryKeyFileText(a.ArchiveID(), rk, "/tmp/a\nB", day)
	os.WriteFile(p+"2", []byte(odd), 0o600)
	if s, err := RecoveryKeyFile(p + "2"); err != nil || !s.IsRecoveryKey() {
		t.Fatalf("a folder name with a line break broke the file: %v\n%s", err, odd)
	}
}

// covers: MA-850, R37
// A damaged record refuses a write naming `mailarchive repair-record`, once,
// matching ErrRecordUnusable (B13); a whole record passes.
func TestADamagedRecordRefusalNamesRepairRecord(t *testing.T) {
	dir := freshRecord(t)
	out, _, _ := made(t, nil)
	assure.Reached(t, CheckRecordUsable(out) == nil, "positive twin: a whole record is usable")
	os.WriteFile(filepath.Join(dir, encrecord.FileName), []byte("not a record\n"), 0o600)
	os.WriteFile(filepath.Join(dir, encrecord.MirrorName), []byte("not one either\n"), 0o600)
	err := CheckRecordUsable(out)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	assure.Refused(t, 1, msg, assure.Code(1), assure.Names("damaged", "mailarchive repair-record", out, "nothing was changed"), assure.Forbid("vault:"),
		assure.NoSideEffect(func() bool { return errors.Is(err, encmark.ErrRecordUnusable) }))
	if n := strings.Count(msg, "record of encrypted archives"); n != 1 {
		t.Fatalf("the record is named %d times: %s", n, msg)
	}
}

// covers: MA-850, R37
// Whether a remembered key would be forgotten is told apart from nothing to forget
// (Tier C); a recovery key typed back wrong is worded without the vault's words
// (B14).
func TestKeyWordsSayWhatIsTrue(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, _ := made(t, s)
	if ok, err := KeyRemembered(out, s); !ok || err != nil {
		t.Fatalf("positive twin: a remembered key: %v %v", ok, err)
	}
	if _, err := ForgetKey(out, s); err != nil {
		t.Fatal(err)
	}
	ok, err := KeyRemembered(out, s)
	assure.Reached(t, !ok && err == nil, "nothing remembered after forgetting")
	for _, e := range []error{vault.ErrRecoveryCheck, vault.ErrRecoveryFormat, errors.New("that is not the recovery key shown")} {
		w := RecoveryKeyRetry(e)
		if strings.Contains(w, "vault") || !strings.HasSuffix(w, ".") {
			t.Fatalf("%v is worded %q", e, w)
		}
	}
}
