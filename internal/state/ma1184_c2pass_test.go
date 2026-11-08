package state

// The C2 adversarial pass's tests on the new records (docs/review-thunderbird-completeness-
// c2-adversarial.md, ADV-C2-09, -10, -11 and the store key of ADV-C2-01), brought in from the
// pass's clone with their positive twins first.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/util"
)

// covers: MA-1184
// ADV-C2-10: a record of a later format (version 2) is never read as this one: ReadTBStreak
// refuses it with ErrTBStreakNewer, so the run never writes it back as version 1. Positive twin:
// a version-1 record (and one with no version) reads.
func TestADV_C2_10_LaterVersionIsNotTakenAsThis(t *testing.T) {
	out := t.TempDir()
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(out, TBStreakName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{`"version":1,`, ``} {
		write(`{` + v + `"serial":7,"accounts":[{"store":"k","account":"imap.x.example","runs":40}]}`)
		if s, err := ReadTBStreak(out); err != nil || len(s.Accounts) != 1 || s.Accounts[0].Runs != 40 {
			t.Fatalf("positive twin %q: %+v %v", v, s, err)
		}
	}
	write(`{"version":2,"serial":7,"accounts":[{"store":"k","account":"imap.x.example","runs":40,"first_behind":"2026-10-01T00:00:00Z","last_behind":"2026-11-01T00:00:00Z","new_field":true}]}`)
	s, err := ReadTBStreak(out)
	if !errors.Is(err, ErrTBStreakNewer) || len(s.Accounts) != 0 || !strings.Contains(err.Error(), "format 2") {
		t.Errorf("a version-2 record: %+v %v", s, err)
	}
}

// covers: MA-1184
// ADV-C2-01's store key in the streak record: two stores with one folder name keep a streak each,
// keyed by their store keys; an entry with no key (written before the key existed, on this
// unreleased branch only) is dropped when read, never matched by its name. A record other users
// can read is refused with util.ErrReadableByOthers, so the run can name it (ADV-C2-05). Positive
// twin: keyed entries read back.
func TestTheStreakIsKeyedByTheStore(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	s := TBStreak{}.Behind("k1", "imap.same.example", 5, t0).Behind("k2", "imap.same.example", 7, t0)
	s = s.Behind("k1", "imap.same.example", 6, t0.Add(time.Hour)).End("k2")
	a, ok := s.Account("k1")
	if !ok || a.Runs != 2 || len(s.Accounts) != 1 || s.Serial != 2 {
		t.Fatalf("positive twin: two stores of one name: %+v", s)
	}
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, TBStreakName), []byte(`{"version":1,"accounts":[{"account":"imap.same.example","runs":9},{"store":"k1","account":"imap.same.example","runs":2}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTBStreak(out)
	if err != nil || len(got.Accounts) != 1 || got.Accounts[0].Store != "k1" || got.Accounts[0].Runs != 2 {
		t.Errorf("an entry with no store key was kept: %+v %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(out, TBStreakName), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadTBStreak(out); !errors.Is(err, util.ErrReadableByOthers) {
			t.Errorf("a record others can read: %v", err)
		}
	}
}

// covers: MA-1180
// ADV-C2-11: CleanFolderName against what a server can put in a name: the design's list (C0, C1,
// DEL, the bidi controls, the cap) and, since the C2 pass, every Unicode format character (the
// zero-width ones U+200B to U+200D and U+2060, the byte-order mark, the soft hyphen, a tag
// character) and the line and paragraph separators U+2028 and U+2029. Invalid UTF-8 is never kept;
// a planted record is cleaned again when read. Positive twin: ordinary names, accents and an
// emoji's own code point stay.
func TestADV_C2_11_FolderNameCleaning(t *testing.T) {
	for _, keep := range []string{"[Gmail]/Sent Mail", "Café Ünïcode", "日本語", "😀"} {
		if got := CleanFolderName(keep); got != keep {
			t.Fatalf("positive twin: CleanFolderName(%q) = %q", keep, got)
		}
	}
	ins := []string{"a\x00b", "a\x1bb", "a\x7fb"}
	for _, r := range []rune{0x202e, 0x2067, 0x200f, 0x061c, 0x0085, 0x2028, 0x2029, 0x200b, 0x200c, 0x200d, 0x2060, 0xfeff, 0x00ad, 0xe0041, 0x2061} {
		ins = append(ins, "a"+string(r)+"b")
	}
	for _, in := range ins {
		if got := CleanFolderName(in); got != "ab" {
			t.Errorf("CleanFolderName(%+q) = %+q, want ab", in, got)
		}
	}
	long := strings.Repeat("é", 10_000)
	if got := CleanFolderName(long); len([]rune(got)) != TBNameMax+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("a 10,000-character name: %d characters", len([]rune(got)))
	}
	if got := CleanFolderName("a\xffb"); strings.Contains(got, "\xff") {
		t.Errorf("invalid UTF-8 kept: %q", got)
	}
	out := t.TempDir()
	planted, _ := json.Marshal(map[string]any{"version": 1, "status": "ok", "thunderbird": []map[string]any{{"account": "x" + string(rune(0x202e)) + "\x1b]0;t\x07" + string(rune(0x2028)) + "", "verdict": "caught_up\x1b[0m", "on_disk": -5,
		"store_id": "id\x1b", "no_identity": -2, "split_mailboxes": -1, "default_not_kept_more": -3,
		"folders": []map[string]any{{"folder": strings.Repeat("f", 5000) + string(rune(0xfeff)), "listed": -1}}}}})
	if err := os.WriteFile(filepath.Join(out, LastRunName), planted, 0o600); err != nil {
		t.Fatal(err)
	}
	lr, st, err := ReadLastRun(out)
	if err != nil || st != LastRunPresent || len(lr.Thunderbird) != 1 {
		t.Fatalf("%v %v %+v", err, st, lr.Thunderbird)
	}
	a := lr.Thunderbird[0]
	if a.Account != "x]0;t" || a.Verdict != "caught_up[0m" || a.OnDisk != 0 || len([]rune(a.Folders[0].Folder)) != TBNameMax+1 || a.Folders[0].Listed != 0 {
		t.Errorf("planted record read back as %+v", a)
	}
	if a.StoreID != "id" || a.NoIdentity != 0 || a.SplitMailboxes != 0 || a.DefaultNotKeptMore != 0 {
		t.Errorf("the C2 pass's fields read back as %q %d %d %d", a.StoreID, a.NoIdentity, a.SplitMailboxes, a.DefaultNotKeptMore)
	}
}
