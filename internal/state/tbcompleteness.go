package state

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"mail-archive-tool/internal/util"
)

// TBAccount is one Thunderbird account's completeness in a plaintext run's last-run record
// (docs/design-thunderbird-completeness.md §7's records row, §9.1, §2.4's "the last backup's
// numbers, from its record"; slice C2). Additive: absent on an older record, on a run with no
// Thunderbird IMAP store, on a run that did not end ok (§11: the failure words win), and always
// beside an encrypted archive (O1, R44: encryptedLastRun never carries it). Accounts are named
// by their store folder; folder names are cleaned and capped (CleanFolderName) before they are
// written. The words are C3's: nothing here is a sentence.
type TBAccount struct {
	Account string `json:"account"` // the store folder's name (§4.1 step 1 fold)
	// StoreID is a hash of the store's full path (profile and store, compared as the OS compares
	// names): two stores with one folder name are told apart by it (the C2 pass's ADV-C2-01), and
	// TC-19's fingerprints are carried forward by it. A hash, never the path.
	StoreID string `json:"store_id,omitempty"`
	Verdict string `json:"verdict"`          // caught_up | behind | limited | unknown
	Reason  string `json:"reason,omitempty"` // when unknown: §7's reason codes
	// Complete is the full "may say complete" test (§4.2's fold; `-json`'s complete).
	Complete bool `json:"complete"`

	Gmail   bool   `json:"gmail,omitempty"`
	Floor   bool   `json:"floor,omitempty"`    // Listed is "at least" (§3's TC-1 fold)
	AllMail string `json:"all_mail,omitempty"` // for Gmail: what All Mail did for Y

	OnDisk           int64     `json:"on_disk"`
	Listed           int64     `json:"listed"`
	Missing          int64     `json:"missing"`
	Tolerance        int64     `json:"tolerance"`
	ExpungedEstimate int64     `json:"expunged_estimate,omitempty"`
	DeletedCopies    bool      `json:"deleted_copies,omitempty"`
	AsOf             time.Time `json:"as_of,omitzero"` // the oldest count time (TC-12)
	Stale            bool      `json:"stale,omitempty"`
	CapNote          int64     `json:"cap_note,omitempty"`
	ListsNoMail      bool      `json:"lists_no_mail,omitempty"`

	// Limit is the account's limits when one is on (§4.1 step 4, TC-9); LimitHash and
	// LimitSince are TC-19's fingerprint of them and when it was first seen.
	Limit      *TBLimit  `json:"limit,omitempty"`
	LimitHash  string    `json:"limit_hash,omitempty"`
	LimitSince time.Time `json:"limit_since,omitzero"`

	// Folders are the kept folders (§3: X and Y per folder; for Gmail never "behind"), at most
	// TBFolderMax, the largest shortfall first; FoldersMore how many more there were.
	Folders     []TBFolder `json:"folders,omitempty"`
	FoldersMore int        `json:"folders_more,omitempty"`
	// NotKept are the folders not kept that list mail (the thunderbird_not_kept WARN), at most
	// TBNotKeptMax by listed count; NotKeptMore how many more; NotKeptHash and NotKeptSince
	// TC-19's fingerprint of the whole list.
	NotKept      []TBFolder `json:"not_kept,omitempty"`
	NotKeptMore  int        `json:"not_kept_more,omitempty"`
	NotKeptHash  string     `json:"not_kept_hash,omitempty"`
	NotKeptSince time.Time  `json:"not_kept_since,omitzero"`
	// DefaultNotKept are Trash and Junk not kept, Thunderbird's default (a note only);
	// DefaultNotKeptMore how many more there were.
	DefaultNotKept     []TBFolder `json:"default_not_kept,omitempty"`
	DefaultNotKeptMore int        `json:"default_not_kept_more,omitempty"`
	// LabelsNotKept counts Gmail labels not kept while All Mail sets Y (TC-8: one note).
	LabelsNotKept int `json:"labels_not_kept,omitempty"`

	// What the reader and the walk could not use, counted (M2, TC-2, L3).
	CantTellFolders  int   `json:"cant_tell_folders,omitempty"`
	SkippedEntries   int   `json:"skipped_entries,omitempty"`
	IgnoredOldKeys   int   `json:"ignored_old_keys,omitempty"`
	UnkeyedMailboxes int   `json:"unkeyed_mailboxes,omitempty"`
	UnkeyedMessages  int64 `json:"unkeyed_messages,omitempty"`
	// The C2 pass: Gmail messages with no identity key (ADV-C2-02), and kept mailboxes split
	// differently from Thunderbird's separators (ADV-C2-03).
	NoIdentity     int64 `json:"no_identity,omitempty"`
	SplitMailboxes int   `json:"split_mailboxes,omitempty"`
	// The C2 pass round 2: kept mailboxes with a separator glued to the line before it (R2-05:
	// never behind), and pieces at a separator with no X-Mozilla-Status (R2-04: counted only
	// toward "behind").
	GluedMailboxes int   `json:"glued_mailboxes,omitempty"`
	NoStatus       int64 `json:"no_status,omitempty"`
}

// TBLimit is an account's own download limits as the record keeps them.
type TBLimit struct {
	MaxAgeDays           int  `json:"max_age_days,omitempty"`
	MaxSizeKB            int  `json:"max_size_kb,omitempty"` // only when the size limit is on
	NoBackgroundDownload bool `json:"no_background_download,omitempty"`
}

// TBFolder is one folder's listed count and the walk's count on disk.
type TBFolder struct {
	Folder string `json:"folder"`
	Listed int64  `json:"listed"`
	OnDisk int64  `json:"on_disk,omitempty"`
}

// TBCompact is one account in a run-history entry: the verdict and the account's X and Y only
// (§9.1: only the per-account X and Y go into the history).
type TBCompact struct {
	Account string `json:"account"`
	Verdict string `json:"verdict"`
	X       int64  `json:"x"`
	Y       int64  `json:"y"`
}

// The record's bounds: TBFolderMax kept folders, TBNotKeptMax not-kept folders per list (§6's
// "first 10 by listed count, then and N more"), TBAccountsMax accounts, and TBNameMax
// characters of a folder name (§6's TC-7 fold: 80, plus "…"). TBFoldersTotalMax bounds the
// folders of all three lists across all accounts together (the C2 pass's ADV-C2-08): each of n
// accounts gets TBFoldersTotalMax/n of them, so the record's `thunderbird` stays far inside
// ReadLastRun's 4 MiB whatever the names (MA-1180's bound test measures the worst case).
const (
	TBFolderMax       = 200
	TBNotKeptMax      = 10
	TBAccountsMax     = 64
	TBNameMax         = 80
	TBFoldersTotalMax = 1000
)

// CleanFolderName is a folder or account name as the record and every surface may show it
// (§6's TC-7 fold): control characters stripped (util.StripControl), every Unicode format
// character removed (category Cf: the bidirectional controls the fold lists, U+202A to U+202E,
// U+2066 to U+2069, U+200E, U+200F and U+061C, and with them the zero-width ones U+200B to U+200D
// and U+2060, the byte-order mark U+FEFF, the soft hyphen and the tag characters, the C2 pass's
// ADV-C2-11), every other Default_Ignorable_Code_Point removed too (Unicode's
// Other_Default_Ignorable_Code_Point and Variation_Selector: the combining grapheme joiner
// U+034F, the Hangul fillers U+115F, U+1160, U+3164 and U+FFA0, U+17B4 and U+17B5, the variation
// selectors U+180B to U+180F, U+FE00 to U+FE0F and U+E0100 to U+E01EF; the C2 pass round 2's
// R2-07), the line and paragraph separators U+2028 and U+2029 removed, and capped at TBNameMax
// characters plus "…". A folder name is text the mail server controls; a name of only such
// characters becomes "", which looks as empty as it is.
func CleanFolderName(s string) string {
	s = strings.Map(func(r rune) rune {
		if invisible(r) || r == 0x2028 || r == 0x2029 {
			return -1
		}
		return r
	}, util.StripControl(s))
	if utf8.RuneCountInString(s) <= TBNameMax {
		return s
	}
	return string([]rune(s)[:TBNameMax]) + "…"
}

// invisible reports whether r renders as nothing: a format character (Cf) or another
// Default_Ignorable_Code_Point. The Hangul fillers and the variation selector ranges are also
// named outright, so a Unicode table that lacks one still removes it.
func invisible(r rune) bool {
	switch {
	case unicode.Is(unicode.Cf, r), unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r), unicode.Is(unicode.Variation_Selector, r):
		return true
	case r == 0x115F, r == 0x1160, r == 0x3164, r == 0xFFA0:
		return true
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF:
		return true
	}
	return false
}

// saneTB is a record's accounts as read back: anyone who can write the archive folder can write
// the record, so every string is cleaned again, counts are never negative, and the lists keep
// their bounds (as Partial's sane does). cut is how many accounts past TBAccountsMax were left
// out (they add to ThunderbirdMore).
func saneTB(in []TBAccount) (out []TBAccount, cut int) {
	if len(in) > TBAccountsMax {
		in, cut = in[:TBAccountsMax], len(in)-TBAccountsMax
	}
	for i := range in {
		a := &in[i]
		a.Account, a.Verdict, a.Reason, a.AllMail = CleanFolderName(a.Account), util.StripControl(a.Verdict), util.StripControl(a.Reason), util.StripControl(a.AllMail)
		a.LimitHash, a.NotKeptHash, a.StoreID = util.StripControl(a.LimitHash), util.StripControl(a.NotKeptHash), util.StripControl(a.StoreID)
		for _, n := range []*int64{&a.OnDisk, &a.Listed, &a.Missing, &a.Tolerance, &a.ExpungedEstimate, &a.CapNote, &a.UnkeyedMessages, &a.NoIdentity, &a.NoStatus} {
			*n = max(0, *n)
		}
		for _, n := range []*int{&a.FoldersMore, &a.NotKeptMore, &a.DefaultNotKeptMore, &a.LabelsNotKept, &a.CantTellFolders, &a.SkippedEntries, &a.IgnoredOldKeys, &a.UnkeyedMailboxes, &a.SplitMailboxes, &a.GluedMailboxes} {
			*n = max(0, *n)
		}
		a.Folders = saneFolders(a.Folders, TBFolderMax)
		a.NotKept = saneFolders(a.NotKept, TBNotKeptMax)
		a.DefaultNotKept = saneFolders(a.DefaultNotKept, TBNotKeptMax)
	}
	return in, cut
}

func saneFolders(fs []TBFolder, limit int) []TBFolder {
	if len(fs) > limit {
		fs = fs[:limit]
	}
	for i := range fs {
		fs[i].Folder = CleanFolderName(fs[i].Folder)
		fs[i].Listed, fs[i].OnDisk = max(0, fs[i].Listed), max(0, fs[i].OnDisk)
	}
	return fs
}

// saneTBCompact is a history entry's accounts as read back.
func saneTBCompact(in []TBCompact) []TBCompact {
	if len(in) > TBAccountsMax {
		in = in[:TBAccountsMax]
	}
	for i := range in {
		in[i].Account, in[i].Verdict = CleanFolderName(in[i].Account), util.StripControl(in[i].Verdict)
		in[i].X, in[i].Y = max(0, in[i].X), max(0, in[i].Y)
	}
	return in
}

// TBCompactOf is accounts as a history entry keeps them; nil for none.
func TBCompactOf(accounts []TBAccount) *[]TBCompact {
	if len(accounts) == 0 {
		return nil
	}
	out := make([]TBCompact, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, TBCompact{Account: a.Account, Verdict: a.Verdict, X: a.OnDisk, Y: a.Listed})
	}
	return &out
}
