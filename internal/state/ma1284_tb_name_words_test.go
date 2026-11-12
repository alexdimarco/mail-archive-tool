package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The account's name in the words (the owner's ruling of 2026-11-08 on the C3 friction review's
// M1): the name Thunderbird shows for the account (TBAccount.Name, usually the email address) when
// the run read one, cleaned and capped as folder names are (TC-7); otherwise "the Thunderbird
// account kept in the folder NAME". A plaintext archive's record and history keep it beside the
// store folder (the lead's call of 2026-11-08), so every surface says it.

// covers: MA-1284
// An account with Thunderbird's own name is said by that name, in the headline, the possessive
// fix-it words and the card; the same account read back from a record (no name) is said by its
// folder, in words that still read as a sentence. Positive twin first: the folder words.
func TestTheAccountIsSaidByThunderbirdsName(t *testing.T) {
	a := TBAccount{Account: "imap.gmail.com-1", Verdict: "behind", Gmail: true, OnDisk: 60, Listed: 2500}
	say := TBSay{Scheduled: true, Now: tbNow}
	if h := TBHeadline(a, say); !strings.HasPrefix(h, "Thunderbird has downloaded about 60 of 2,500 messages in the Thunderbird account kept in the folder imap.gmail.com-1 so far.") {
		t.Fatalf("positive twin: named by its folder: %q", h)
	}
	a.Name = "sam.work@gmail.com"
	wantLine(t, "the headline", TBHeadline(a, say),
		"Thunderbird has downloaded about 60 of 2,500 messages in sam.work@gmail.com so far. Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.")
	wantLine(t, "the card", TBCardBanner([]TBAccount{a}),
		"Thunderbird is still downloading sam.work@gmail.com: about 60 of 2,500 messages are on this computer so far — see Status & health.")
	l := TBAccount{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "limited", OnDisk: 170, Listed: 2300, Limit: &TBLimit{MaxAgeDays: 30}, ThunderbirdVersion: 153}
	if got := TBLimitLines(l); len(got) != 1 || !strings.Contains(got[0], `then Account Settings; on sam@fastmail.com's "Synchronization & Storage" page, choose`) || strings.Contains(got[0], "imap.fastmail.com") {
		t.Errorf("the fix-it words point at the name Account Settings shows: %q", got)
	}
	// No surface word says the folder once the name is known.
	for _, s := range append(TBAccountLines(l, say), TBCardBanner([]TBAccount{l}), TBHeadline(TBAccount{Account: "imap.x.example", Name: "Work mail", Verdict: "unknown", Reason: "partial_input"}, say)) {
		if strings.Contains(s, "kept in the folder") || strings.Contains(s, "imap.") {
			t.Errorf("a named account said by its folder: %q", s)
		}
	}
	wantLine(t, "a name that is not an address", TBHeadline(TBAccount{Account: "imap.x.example", Name: "Work mail", Verdict: "unknown", Reason: "partial_input"}, say),
		"Only part of Work mail was backed up, so MailArchive did not compare it with Thunderbird's count.")
}

// covers: MA-1284
// The name is text the person, or a server through the account wizard, chose (TC-7): cleaned and
// capped at 80 characters plus "…" as folder names are; a name of nothing visible is no name, so the
// account is said by its folder, and with no folder either, "this account". Positive twin first:
// an ordinary name as it is.
func TestTheNameIsCleanedAndCapped(t *testing.T) {
	a := TBAccount{Account: "imap.example.com", Name: "sam@example.com", Verdict: "caught_up", Complete: true, OnDisk: 2, Listed: 2}
	wantLine(t, "positive twin", TBHeadline(a, TBSay{}), "Thunderbird has downloaded everything it lists for sam@example.com (2 messages).")
	a.Name = "sam‮@evil\x1b[31m.example​" + strings.Repeat("y", 10*1024)
	h := TBHeadline(a, TBSay{})
	for _, bad := range []string{"‮", "\x1b", "​", strings.Repeat("y", 81)} {
		if strings.Contains(h, bad) {
			t.Errorf("the name holds %q: %q", bad, h)
		}
	}
	if want := "for " + CleanFolderName(a.Name) + " (2 messages)."; !strings.HasSuffix(h, want) || !strings.HasSuffix(CleanFolderName(a.Name), "…") {
		t.Errorf("the cleaned, capped name: %q, want suffix %q", h, want)
	}
	a.Name = "​⁦ \x00"
	wantLine(t, "a name of nothing visible", TBHeadline(a, TBSay{}), "Thunderbird has downloaded everything it lists for the Thunderbird account kept in the folder imap.example.com (2 messages).")
	a.Account = "​"
	wantLine(t, "no name and no folder", TBHeadline(a, TBSay{}), "Thunderbird has downloaded everything it lists for this account (2 messages).")
	if s := TBNotKeptLine(TBAccount{Account: "​", NotKept: []TBFolder{{Folder: "INBOX", Listed: 1}}}); !strings.HasPrefix(s, "Thunderbird is not set to keep this account's mail on this computer") {
		t.Errorf("the possessive of this account: %q", s)
	}
}

// writeRaw puts v, as JSON, at name in out: a record as an older version, or anyone who can write
// the archive folder, left it.
func writeRaw(t *testing.T, out, name string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// readRaw is the JSON at name in out, decoded as it is on disk.
func readRaw(t *testing.T, out, name string) any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, name))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// covers: MA-1284
// The name is written beside a plaintext archive (the lead's call of 2026-11-08: every surface says
// it, not only those that see the live run): each last-run `thunderbird` entry and each run-history
// `tb` entry gains `name` beside `account`, which stays the store folder and the key. So what reads
// a record (status, Status & health, the card, Recent backups) says the address. An older record or
// entry with no `name` reads as before, by its folder. Positive twin first: the older record.
func TestTheNameIsWrittenBesideAPlaintextArchive(t *testing.T) {
	out := t.TempDir()
	writeRaw(t, out, LastRunName, map[string]any{"version": 1, "status": RunOK,
		"thunderbird": []any{map[string]any{"account": "imap.gmail.com", "verdict": "behind", "on_disk": 1, "listed": 9}}})
	writeRaw(t, out, RunHistoryName, []any{map[string]any{"when": "2026-11-08T09:00:00Z", "status": RunOK,
		"tb": []any{map[string]any{"account": "imap.gmail.com", "verdict": "behind", "x": 1, "y": 9}}}})
	lr, _, err := ReadLastRun(out)
	if err != nil || len(lr.Thunderbird) != 1 || !strings.Contains(TBHeadline(lr.Thunderbird[0], TBSay{Scheduled: true}), "in the Thunderbird account kept in the folder imap.gmail.com so far") {
		t.Fatalf("positive twin: an older record, by its folder: %+v %v", lr.Thunderbird, err)
	}
	h, err := ReadRunHistory(out)
	if s, _ := TBCompactSentence((*h[0].TB)[0]); err != nil || s != "Thunderbird has downloaded about 1 of 9 messages in the Thunderbird account kept in the folder imap.gmail.com so far" {
		t.Fatalf("positive twin: an older history entry, by its folder: %q %v", s, err)
	}

	out = t.TempDir()
	a := TBAccount{Account: "imap.gmail.com", Name: "qzname@gmail.example", Verdict: "behind", OnDisk: 1, Listed: 9}
	lr = LastRun{Status: RunOK, Finished: tbNow, Thunderbird: []TBAccount{a}}
	if err := WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	if err := AppendRunHistory(out, RunHistoryEntryFrom(lr)); err != nil {
		t.Fatal(err)
	}
	rec := readRaw(t, out, LastRunName).(map[string]any)["thunderbird"].([]any)[0].(map[string]any)
	if rec["account"] != "imap.gmail.com" || rec["name"] != "qzname@gmail.example" {
		t.Errorf("the record's entry: %v", rec)
	}
	hist := readRaw(t, out, RunHistoryName).([]any)[0].(map[string]any)["tb"].([]any)[0].(map[string]any)
	if hist["account"] != "imap.gmail.com" || hist["name"] != "qzname@gmail.example" {
		t.Errorf("the history's entry: %v", hist)
	}
	lr, _, _ = ReadLastRun(out)
	if len(lr.Thunderbird) != 1 || lr.Thunderbird[0].Account != "imap.gmail.com" {
		t.Fatalf("read back: %+v", lr.Thunderbird)
	}
	wantLine(t, "the record's words", TBHeadline(lr.Thunderbird[0], TBSay{Scheduled: true}),
		"Thunderbird has downloaded about 1 of 9 messages in qzname@gmail.example so far. Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.")
	wantLine(t, "the card", TBCardBanner(lr.Thunderbird),
		"Thunderbird is still downloading qzname@gmail.example: about 1 of 9 messages are on this computer so far — see Status & health.")
	h, _ = ReadRunHistory(out)
	if c := (*h[0].TB)[0]; c.Account != "imap.gmail.com" {
		t.Errorf("the history's key: %+v", c)
	}
	if s, behind := TBCompactSentence((*h[0].TB)[0]); s != "Thunderbird has downloaded about 1 of 9 messages in qzname@gmail.example so far" || !behind {
		t.Errorf("Recent backups' words: %q %v", s, behind)
	}
}

// covers: MA-1284, MA-1198
// A record's name is text anyone who can write the archive folder can write: cleaned and capped
// again on read (control and format characters gone, 80 characters plus "…"), and a name of
// nothing visible is no name, so the account is said by its folder. Two accounts with one name in
// a record are both said by their folders, as on screen. Positive twin first: an ordinary name as
// it is.
func TestAPlantedNameIsCleanedOnRead(t *testing.T) {
	plant := func(names ...string) (lr LastRun, tb []TBCompact) {
		t.Helper()
		out := t.TempDir()
		var accts, compact []any
		for i, n := range names {
			folder := fmt.Sprintf("imap.example.com-%d", i)
			accts = append(accts, map[string]any{"account": folder, "name": n, "verdict": "behind", "on_disk": 1, "listed": 9})
			compact = append(compact, map[string]any{"account": folder, "name": n, "verdict": "behind", "x": 1, "y": 9})
		}
		writeRaw(t, out, LastRunName, map[string]any{"version": 1, "status": RunOK, "thunderbird": accts})
		writeRaw(t, out, RunHistoryName, []any{map[string]any{"when": "2026-11-08T09:00:00Z", "status": RunOK, "tb": compact}})
		lr, _, err := ReadLastRun(out)
		if err != nil {
			t.Fatal(err)
		}
		h, err := ReadRunHistory(out)
		if err != nil || len(h) != 1 || h[0].TB == nil {
			t.Fatalf("the history: %+v %v", h, err)
		}
		return lr, *h[0].TB
	}
	lr, tb := plant("sam@example.com")
	if lr.Thunderbird[0].Name != "sam@example.com" || tb[0].Name != "sam@example.com" {
		t.Fatalf("positive twin: an ordinary name: %+v %+v", lr.Thunderbird, tb)
	}
	hostile := "sam‮@evil\x1b[31m.example​<b>x</b>\x07" + strings.Repeat("y", 10*1024)
	lr, tb = plant(hostile)
	s, _ := TBCompactSentence(tb[0])
	for what, got := range map[string]string{"the record": lr.Thunderbird[0].Name, "the history": tb[0].Name,
		"the words": TBHeadline(lr.Thunderbird[0], TBSay{}), "the card": TBCardBanner(lr.Thunderbird), "Recent backups": s} {
		for _, bad := range []string{"‮", "\x1b", "​", "\x07", strings.Repeat("y", 81)} {
			if strings.Contains(got, bad) {
				t.Errorf("%s holds %q: %q", what, bad, got)
			}
		}
		if !strings.Contains(got, CleanFolderName(hostile)) || !strings.HasSuffix(CleanFolderName(hostile), "…") {
			t.Errorf("%s: %q, want the cleaned, capped name", what, got)
		}
	}
	lr, tb = plant("​⁦ \x00")
	s, _ = TBCompactSentence(tb[0])
	if lr.Thunderbird[0].Name != "" || tb[0].Name != "" || !strings.Contains(TBHeadline(lr.Thunderbird[0], TBSay{}), "kept in the folder imap.example.com-0 so far") ||
		!strings.Contains(s, "kept in the folder imap.example.com-0 so far") {
		t.Errorf("a name of nothing visible: %+v %+v %q", lr.Thunderbird, tb, s)
	}
	for _, same := range [][2]string{{"Gmail", "gmail"}, {"Sam​ Mail", "Sam Mail"}} {
		lr, tb = plant(same[0], same[1], "sam@example.com")
		lines := TBLines(lr.Thunderbird, 0, TBSay{Scheduled: true}) // three behind: P2's one remedy line last
		s0, _ := TBCompactSentence(tb[0])
		s1, _ := TBCompactSentence(tb[1])
		s2, _ := TBCompactSentence(tb[2])
		if len(lines) != 4 || !strings.Contains(lines[0], "kept in the folder imap.example.com-0 ") || !strings.Contains(lines[1], "kept in the folder imap.example.com-1 ") ||
			!strings.Contains(lines[2], " sam@example.com ") || !strings.Contains(s0, "folder imap.example.com-0 ") || !strings.Contains(s1, "folder imap.example.com-1 ") || !strings.Contains(s2, " sam@example.com ") {
			t.Errorf("two accounts named %q and %q: %q / %q %q %q", same[0], same[1], lines, s0, s1, s2)
		}
	}
}

// covers: MA-1284
// The history keeps fitting its reader with a name in every entry: at its bounds (RunHistoryMax
// entries of TBAccountsMax accounts, each folder and name 80 characters of '<', 6 bytes each in
// JSON) the file stays within ReadRunHistory's bound and reads back whole, so a run never loses the
// history to a name. Positive twin first: one entry reads back with its names.
func TestTheHistoryWithNamesFitsItsReader(t *testing.T) {
	out := t.TempDir()
	lt := strings.Repeat("<", TBNameMax-2)
	var accts []TBAccount
	for i := 0; i < TBAccountsMax; i++ {
		accts = append(accts, TBAccount{Account: fmt.Sprintf("%s%02d", lt, i), Name: fmt.Sprintf("%s%02d", lt, i), Verdict: "caught_up", OnDisk: 1 << 40, Listed: 1 << 40, Floor: true})
	}
	e := RunHistoryEntryFrom(LastRun{Status: RunOK, Exported: 1 << 30, Filled: 1 << 30, Mode: "incremental", Started: tbNow, Finished: tbNow.Add(time.Hour), Thunderbird: accts})
	if err := AppendRunHistory(out, e); err != nil {
		t.Fatal(err)
	}
	if h, err := ReadRunHistory(out); err != nil || len(h) != 1 || len(*h[0].TB) != TBAccountsMax || (*h[0].TB)[0].Name != lt+"00" {
		t.Fatalf("positive twin: one entry: %v", err)
	}
	for i := 1; i < RunHistoryMax+2; i++ {
		e.When = e.When.Add(time.Hour)
		if err := AppendRunHistory(out, e); err != nil {
			t.Fatal(err)
		}
	}
	fi, err := os.Stat(filepath.Join(out, RunHistoryName))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("worst case: %d entries of %d accounts, %d bytes (bound %d)", RunHistoryMax, TBAccountsMax, fi.Size(), maxRunHistorySize)
	h, err := ReadRunHistory(out)
	if err != nil || len(h) != RunHistoryMax || fi.Size() > maxRunHistorySize {
		t.Errorf("the worst-case history: %d entries, %d bytes, %v", len(h), fi.Size(), err)
	}
}

// covers: MA-1284, MA-1186
// An encrypted archive writes no name, as it writes nothing of completeness (O1): its last-run
// record and its history entry are their allow-lists, and an archive that was plaintext loses the
// name of its older entries when its first encrypted entry is added (F19). Positive twin first: the
// same record into a plaintext archive holds the name.
func TestAnEncryptedArchiveWritesNoName(t *testing.T) {
	a := TBAccount{Account: "imap.gmail.com", Name: "qzname@gmail.example", Verdict: "behind", OnDisk: 1, Listed: 9}
	lr := LastRun{Status: RunOK, Finished: tbNow, Thunderbird: []TBAccount{a}}
	out := t.TempDir()
	if err := WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	if err := AppendRunHistory(out, RunHistoryEntryFrom(lr)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{LastRunName, RunHistoryName} {
		if data, _ := os.ReadFile(filepath.Join(out, f)); !strings.Contains(string(data), "qzname") {
			t.Fatalf("positive twin: the plaintext %s holds no name: %s", f, data)
		}
	}
	lr.Encrypted = true
	if err := WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	if err := AppendRunHistory(out, RunHistoryEntryFrom(lr)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{LastRunName, RunHistoryName} {
		data, err := os.ReadFile(filepath.Join(out, f))
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"qzname", "imap.gmail.com", "thunderbird", `"tb"`} {
			if strings.Contains(string(data), bad) {
				t.Errorf("the encrypted archive's %s holds %q: %s", f, bad, data)
			}
		}
	}
}
