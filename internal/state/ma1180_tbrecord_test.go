package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-1180
// The completeness record's names are server text (§6's TC-7 fold, §9.1): CleanFolderName strips
// control characters and the bidirectional controls (U+202A to U+202E, U+2066 to U+2069, U+200E,
// U+200F, U+061C) and caps a name at 80 characters plus "…". A planted record is cleaned again as
// it is read (control characters, a bidi override, a 10 KB name, negative counts, an overlong
// list), and so is a planted history entry. An encrypted archive's record never carries the
// field, whatever the caller hands the writer. Positive twin first: a plain name is unchanged.
func TestTheCompletenessRecordIsCleanedAndBounded(t *testing.T) {
	if got := CleanFolderName("[Gmail]/Sent Mail"); got != "[Gmail]/Sent Mail" {
		t.Fatalf("positive twin: %q", got)
	}
	long := strings.Repeat("é", 10<<10)
	for in, want := range map[string]string{
		"Pro\x1b[31mjects": "Pro[31mjects",
		"evil‮gnp.exe":     "evilgnp.exe",
		"a⁦b⁩c‎‏؜d":        "abcd",
		long:               strings.Repeat("é", 80) + "…",
	} {
		if got := CleanFolderName(in); got != want {
			t.Errorf("CleanFolderName(%.40q) = %.90q, want %.90q", in, got, want)
		}
	}

	out := t.TempDir()
	var folders []TBFolder
	for i := 0; i < TBFolderMax+5; i++ {
		folders = append(folders, TBFolder{Folder: "f‮", Listed: -1, OnDisk: -2})
	}
	planted := LastRun{Status: RunOK, Thunderbird: []TBAccount{{
		Account: "imap.example.com\x07", Verdict: "behind\n", OnDisk: -5, Listed: 9, Missing: -1,
		Folders: folders, NotKept: []TBFolder{{Folder: long, Listed: 3}}, NotKeptMore: -4,
	}}}
	if err := WriteLastRun(out, planted); err != nil {
		t.Fatal(err)
	}
	lr, st, err := ReadLastRun(out)
	if err != nil || st != LastRunPresent || len(lr.Thunderbird) != 1 {
		t.Fatalf("read back: %v %v %+v", err, st, lr.Thunderbird)
	}
	a := lr.Thunderbird[0]
	if a.Account != "imap.example.com" || a.Verdict != "behind" || a.OnDisk != 0 || a.Missing != 0 || a.NotKeptMore != 0 {
		t.Errorf("planted strings or counts survived: %+v", a)
	}
	if len(a.Folders) != TBFolderMax || a.Folders[0].Folder != "f" || a.Folders[0].Listed != 0 || a.Folders[0].OnDisk != 0 {
		t.Errorf("planted folders: %d, first %+v", len(a.Folders), a.Folders[0])
	}
	if a.NotKept[0].Folder != strings.Repeat("é", 80)+"…" {
		t.Errorf("a 10 KB name was not capped")
	}

	tb := []TBCompact{{Account: "a\x1b", Verdict: "x\r", X: -1, Y: -2}}
	data, _ := json.Marshal([]RunHistoryEntry{{When: time.Now(), Status: RunOK, TB: &tb}})
	if err := os.WriteFile(filepath.Join(out, RunHistoryName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := ReadRunHistory(out)
	if err != nil || len(h) != 1 || h[0].TB == nil || (*h[0].TB)[0] != (TBCompact{Account: "a", Verdict: "x", X: 0, Y: 0}) {
		t.Errorf("a planted history entry: %v %+v", err, h)
	}

	enc := t.TempDir()
	if err := WriteLastRun(enc, LastRun{Status: RunOK, Encrypted: true, Thunderbird: planted.Thunderbird}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(enc, LastRunName)); strings.Contains(string(raw), "thunderbird") || strings.Contains(string(raw), "imap.example.com") {
		t.Errorf("an encrypted archive's record carries completeness:\n%s", raw)
	}
	if e := RunHistoryEntryFrom(LastRun{Status: RunOK, Encrypted: true, Thunderbird: planted.Thunderbird}); e.TB != nil {
		t.Errorf("an encrypted archive's history entry carries tb: %+v", e)
	}
}

// covers: MA-1184
// The behind streak's record (§5's M10 fold) follows the presync streak's clock rules: a clock that
// went back restarts the first time and keeps the serial; a time before MinPlausibleTime counts the
// run but moves no time; a damaged record reads as none and a planted one is cleaned; a removed
// record is gone and a second removal is no error. Positive twin first: two behind runs a day apart
// extend one streak with one serial.
func TestTheBehindStreakRecordsClockRules(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	s := TBStreak{}.Behind("h", 10, t0).Behind("h", 12, t0.Add(24*time.Hour))
	a, ok := s.Account("h")
	if !ok || a.Runs != 2 || a.Serial != 1 || s.Serial != 1 || !a.FirstBehind.Equal(t0) || a.GrewX != 12 {
		t.Fatalf("positive twin: %+v %+v", s, a)
	}
	back := s.Behind("h", 13, t0.Add(-time.Hour))
	b, _ := back.Account("h")
	if b.Serial != 1 || back.Serial != 1 || !b.FirstBehind.Equal(t0.Add(-time.Hour)) || b.Runs != 3 {
		t.Errorf("a clock that went back: %+v", b)
	}
	early := s.Behind("h", 20, MinPlausibleTime.Add(-time.Hour))
	e, _ := early.Account("h")
	if e.Runs != 3 || !e.LastBehind.Equal(a.LastBehind) {
		t.Errorf("an implausible time moved the streak's times: %+v", e)
	}
	two := s.Behind("other", 1, t0)
	if o, _ := two.Account("other"); o.Serial != 2 || two.Serial != 2 {
		t.Errorf("a second account's streak: %+v serial %d", o, two.Serial)
	}

	out := t.TempDir()
	if err := WriteTBStreak(out, s.Named()); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTBStreak(out)
	if err != nil || got.RecordID == "" || len(got.Accounts) != 1 {
		t.Fatalf("read back: %v %+v", err, got)
	}
	if err := os.WriteFile(filepath.Join(out, TBStreakName), []byte(`{"record_id":"bad id!","accounts":[{"account":"h\u001b‮","first_x":-3,"runs":-1}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = ReadTBStreak(out)
	if err != nil || got.RecordID != "" || got.Accounts[0].Account != "h" || got.Accounts[0].FirstX != 0 || got.Accounts[0].Runs != 0 {
		t.Errorf("a planted record: %v %+v", err, got)
	}
	if err := os.WriteFile(filepath.Join(out, TBStreakName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadTBStreak(out); err != nil || len(got.Accounts) != 0 {
		t.Errorf("a damaged record: %v %+v", err, got)
	}
	if err := RemoveTBStreak(out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, TBStreakName)); !os.IsNotExist(err) {
		t.Errorf("not removed: %v", err)
	}
	if err := RemoveTBStreak(out); err != nil {
		t.Errorf("a second removal: %v", err)
	}
}
