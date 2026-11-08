package tbcomplete

// MA-1235's All Mail cases, left for slice C1a by the lead's call of 2026-11-08 [B1 split]
// (docs/notes-b1-split-for-gmail-dedup.md): which folder of a store is All Mail is read from
// folderCache.json by this reader, never by thunderbird.GmailForStore. All Mail is the one folder
// carrying the All Mail flag 0x8000 (foldcheck M5); the Archive flag 0x4000 alone never stands in
// for it; none, or more than one, is no All Mail.

import (
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-1235, R25
// All Mail is found by 0x8000 only, read from a folderCache.json fixture in a real profile.
// Positive twin first: Gmail's All Mail, which Thunderbird gives both 0x8000 and 0x4000
// (nsImapMailFolder.cpp, ESR 153 lines 1161-1164), is the one. Then: All Mail hidden and an
// "Archives" label with 0x4000 only (Thunderbird's own archive folder) is none; two folders with
// 0x8000 is several, never a pick; and a folder that could be it but cannot be read (its flags
// broken, two keys for it, or only an old-path key while it is still in the store) is can't tell.
// Each answer but One gives no folder, and the zero value is can't tell.
func TestAllMailIsTheOneFolderWithTheAllMailFlag(t *testing.T) {
	type keyFn func(f ...string) string
	inbox := func(j keyFn) pair { return pair{j("INBOX.msf"), folder(FlagInbox, 10, 10)} }
	all := func(j keyFn) pair {
		return pair{j("[Gmail].sbd", "All Mail.msf"), folder(FlagAllMail|FlagArchive, 100, 100)}
	}
	read := func(t *testing.T, pairs func(j, old keyFn) []pair) Store {
		t.Helper()
		p, store := newProfile(t, "imap.gmail.com")
		write(t, filepath.Join(store, "[Gmail].sbd", "All Mail"), "From x\n") // its mailbox file is here
		j := func(f ...string) string { return filepath.Join(append([]string{store}, f...)...) }
		old := func(f ...string) string {
			return filepath.Join(append([]string{string(filepath.Separator) + "old", "ImapMail", "imap.gmail.com"}, f...)...)
		}
		writeCache(t, p, pairs(j, old)...)
		return readStore(t, p, store)
	}

	s := read(t, func(j, _ keyFn) []pair { return []pair{inbox(j), all(j)} })
	if s.AllMail.Kind != AllMailOne || s.AllMail.Entry == nil || s.AllMail.Entry.Path != "[Gmail]/All Mail" || !s.AllMail.Entry.Has(FlagAllMail) {
		t.Fatalf("the positive twin: %+v", s.AllMail)
	}
	if (AllMail{}).Kind != AllMailCantTell {
		t.Fatal("the zero value is not can't tell")
	}

	caseTwin := AllMailCantTell // one folder with two keys, where names compare case-blind
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		caseTwin = AllMailSeveral // two folders, each with 0x8000
	}
	for name, tc := range map[string]struct {
		pairs func(j, old keyFn) []pair
		want  AllMailKind
	}{
		"All Mail hidden, an Archives label with 0x4000 only": {func(j, _ keyFn) []pair {
			return []pair{inbox(j), {j("Archives.msf"), folder(FlagArchive, 100, 100)}, {j("[Gmail].sbd", "Sent Mail.msf"), folder(FlagSentMail, 30, 30)}}
		}, AllMailNone},
		"no folder with either flag": {func(j, _ keyFn) []pair { return []pair{inbox(j)} }, AllMailNone},
		"two folders with 0x8000": {func(j, _ keyFn) []pair {
			return []pair{inbox(j), all(j), {j("Other.msf"), folder(FlagAllMail, 5, 5)}}
		}, AllMailSeveral},
		"a folder whose flags broke": {func(j, _ keyFn) []pair {
			return []pair{inbox(j), all(j), {j("Odd.msf"), folder(0, 5, 5).with("flags", "x")}}
		}, AllMailCantTell},
		"All Mail with two keys that differ by case": {func(j, _ keyFn) []pair {
			return []pair{inbox(j), all(j), {j("[Gmail].sbd", "all mail.msf"), folder(FlagAllMail, 5, 5)}}
		}, caseTwin},
		"All Mail by an old key only, still in the store": {func(j, old keyFn) []pair {
			return []pair{inbox(j), {old("[Gmail].sbd", "All Mail.msf"), folder(FlagAllMail|FlagArchive, 100, 100)}}
		}, AllMailCantTell},
	} {
		t.Run(name, func(t *testing.T) {
			s := read(t, tc.pairs)
			if s.AllMail.Kind != tc.want || s.AllMail.Entry != nil {
				t.Fatalf("All Mail %+v; want %v and no folder", s.AllMail, tc.want)
			}
		})
	}
}
