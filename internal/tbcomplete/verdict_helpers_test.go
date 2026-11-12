package tbcomplete

// Test helpers for slice C1b (the verdict): a store read from a folderCache.json written in full
// through the C1a reader, and an Input with every fact a healthy, checked, whole IMAP store has, so
// a test changes only the fact it is about.

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/thunderbird"
)

// vStore is the store the verdict tests read, by Linux's path rules.
const vStore = "/p/ImapMail/h"

// vSync is the lastSyncTimeInSec folder() writes; vNow is an hour after it.
const vSync = 1762500000

var vNow = time.Unix(vSync, 0).Add(time.Hour)

// at is the key and value of the folder name ("[Gmail]/All Mail" is "[Gmail].sbd/All Mail.msf")
// under vStore.
func at(name string, f fields) pair {
	parts := strings.Split(name, "/")
	for i := range parts[:len(parts)-1] {
		parts[i] += ".sbd"
	}
	return pair{vStore + "/" + strings.Join(parts, "/") + ".msf", f}
}

// vstore reads a cache holding the account root and pairs, as the store vStore sees it.
func vstore(t *testing.T, pairs ...pair) Store {
	t.Helper()
	all := append([]pair{{vStore, root()}}, pairs...)
	s := parseStore(t, cacheJSON(t, all...), vStore, "linux")
	if s.Mode != Exact {
		t.Fatalf("the fixture's store is not exact: %+v", s)
	}
	return s
}

// notKept is folder() without the Offline flag.
func notKept(flags uint32, total int) fields {
	return folder(flags, total, total).with("flags", flags|FlagImapBox)
}

// healthy is the Input of a whole, matched, checked IMAP store that is not Gmail, read from s, with
// the walk's counts disk.
func healthy(s Store, disk map[string]int64) Input {
	return Input{
		Account: "h", Matched: true, Type: "imap", WholeStore: true, Gmail: thunderbird.GmailNo,
		Profile: Profile{PanoramaFile: No, PanoramaPref: No},
		Store:   s, OnDisk: OnDisk{Folders: disk}, Now: vNow,
	}
}

// gmail is healthy for a Gmail store whose distinct count on disk is distinct.
func gmail(s Store, distinct int64) Input {
	in := healthy(s, nil)
	in.Gmail = thunderbird.GmailYes
	in.OnDisk.Distinct = distinct
	return in
}

// gmailStore is a small Gmail store: All Mail (both of the flags Thunderbird gives it) listing
// allMail, INBOX listing inbox and a label listing label, all kept and checked; more are added.
func gmailStore(t *testing.T, allMail, inbox, label int, more ...pair) Store {
	t.Helper()
	return vstore(t, append([]pair{
		at("INBOX", folder(FlagInbox, inbox, inbox)),
		at("[Gmail]", folder(FlagImapNoselect, 0, 0)),
		at("[Gmail]/All Mail", folder(FlagAllMail|FlagArchive, allMail, allMail)),
		at("Label", folder(0, label, label)),
	}, more...)...)
}

// want fails the test unless r has the verdict and, for unknown, the reason.
func want(t *testing.T, what string, r Result, v Verdict, why Reason) {
	t.Helper()
	if r.Verdict != v || r.Reason != why {
		t.Fatalf("%s: got %v (%v), want %v (%v); %+v", what, r.Verdict, r.Reason, v, why, r)
	}
}
