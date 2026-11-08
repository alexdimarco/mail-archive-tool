package tbcomplete

// This file is slice C1b: the verdict core (docs/design-thunderbird-completeness.md §3, §4.1 steps
// 3 to 6, §4.2, §4.3 and §4.4, with their folds). It is pure: it reads no file and walks no disk.
// Everything it decides from is in Input, which C2 fills: the account (C0c's match), Gmail
// (thunderbird.GmailForStore), the profile's Panorama signals (ReadProfile), the store's view of
// folderCache.json (ReadCache, Cache.Store), Thunderbird's limits for the account, and what the
// walk counted on disk. The words and surfaces are C3's; this file gives verdicts, reasons and
// numbers only.

import (
	"math"
	"math/big"
	"time"

	"mail-archive-tool/internal/thunderbird"
)

// Verdict is one account's answer (§4.2). Its zero value is Unknown, so a verdict nobody set is
// never caught up.
type Verdict int

const (
	// Unknown: MailArchive cannot tell; Result.Reason says why (§4.4's fold table).
	Unknown Verdict = iota
	// CaughtUp: every kept folder checked, and missing within the tolerance. Not the same as
	// "may say complete" (Result.MaySayComplete).
	CaughtUp
	// Behind: missing above the tolerance (for a Gmail floor, X below the floor less its tolerance).
	Behind
	// Limited: behind (or never checked, with background download off) while a limit of
	// Thunderbird's own is on, so the gap may be by design (§4.1 step 4, TC-9).
	Limited
	// NotApplicable: a matched pop3, none (Local Folders) or rss account, or a plain mail folder
	// (§2.2's M6 and M6-residual folds). Only these.
	NotApplicable
)

// String is the design's identifier, as `status -json` carries it.
func (v Verdict) String() string {
	switch v {
	case CaughtUp:
		return "caught_up"
	case Behind:
		return "behind"
	case Limited:
		return "limited"
	case NotApplicable:
		return "not_applicable"
	}
	return "unknown"
}

// Reason is why a verdict is Unknown (§7's `status -json` reason codes, from §4.4's fold). Its zero
// value, NoReason, is the reason of every other verdict.
type Reason int

const (
	NoReason Reason = iota
	// ReasonCacheUnreadable: folderCache.json missing, refused or malformed (TC-15); also the
	// other §4.4 rows with the same "could not read Thunderbird's list of folders" words: no key
	// for the store, keys that cannot be told (C1A-06, C1A-08), no countable kept folder, and
	// messages on disk while every folder lists 0 (TC-2).
	ReasonCacheUnreadable
	// ReasonNoMatch: no matched account (C0c), M6's cases, and a Gmail answer that cannot be told.
	ReasonNoMatch
	// ReasonDuplicateKey: a key twice in the file, or two keys for one folder (TC-3, M2).
	ReasonDuplicateKey
	// ReasonInvalidEntry: a kept entry that fails validation, flags that cannot be read (M3), a
	// refused key (L2's cautious reading, kept by the C1a pass's notes), a walked folder's entry.
	ReasonInvalidEntry
	// ReasonPanorama: panorama.sqlite, or mail.panorama.enabled, or either cannot be told (TC-3).
	ReasonPanorama
	// ReasonAccountType: a matched account of type ews, another type, or none (TC-5).
	ReasonAccountType
	// ReasonNeverChecked: a kept folder listing mail never checked (lastSyncTimeInSec ≤ 0, absent,
	// or later than now), or a kept folder the server has not listed (no ImapBox, C1A-07).
	ReasonNeverChecked
	// ReasonGmailFloor: Gmail whose All Mail cannot set Y, with X at or above the floor less its
	// tolerance (TC-1, O3).
	ReasonGmailFloor
	// ReasonExpungedUnsized: deleted copies (expungedBytes > 0) on a kept folder with nothing to
	// divide by (TC-16).
	ReasonExpungedUnsized
	// ReasonPartialInput: the walk did not cover the whole store (TC-11).
	ReasonPartialInput
	// ReasonOldPathOnly: a folder with only an old-path key while still in the store (M2).
	ReasonOldPathOnly
	// ReasonNotCountedYet: a kept folder whose one failure is Thunderbird's own totalMsgs -1, "not
	// counted by Thunderbird yet", never invalid_entry's words (C1a pass C1A-01). The design words
	// it but names no code; not_counted_yet is the builder's.
	ReasonNotCountedYet
	// ReasonSplitDifferently: a kept mailbox the archive's reader split into more pieces than
	// Thunderbird's own separators (the C2 pass's ADV-C2-03), with no certain gap. The code is
	// the C2 pass fix's.
	ReasonSplitDifferently
	// ReasonOtherReader: a Thunderbird store this run read with another reader (the Evolution
	// one, ADV-C2-16), which counts nothing for the check. The code is the C2 pass fix's.
	ReasonOtherReader
	// ReasonWalkFailed: the walk of the store ended with an error, so its count is not whole.
	// The code is the C2 pass fix's.
	ReasonWalkFailed
)

var reasonCodes = [...]string{"", "cache_unreadable", "no_match", "duplicate_key", "invalid_entry", "panorama",
	"account_type", "never_checked", "gmail_floor", "expunged_unsized", "partial_input", "old_path_only", "not_counted_yet",
	"split_differently", "other_reader", "walk_failed"}

// String is the reason's code, "" for NoReason or a reason nobody defined.
func (r Reason) String() string {
	if r < 0 || int(r) >= len(reasonCodes) {
		return ""
	}
	return reasonCodes[r]
}

// AllMailUse is what a Gmail account's All Mail did for Y (§3's TC-1 and M5 folds); for a floor it
// also picks O3's last clause in C3's words.
type AllMailUse int

const (
	AllMailNotGmail     AllMailUse = iota // not a Gmail account
	AllMailSetsY                          // the one folder with 0x8000, kept, checked, not below the largest other folder
	AllMailNotShown                       // no folder with 0x8000, several, or one that cannot be read (M5): "…is not shown to Thunderbird"
	AllMailNotKept                        // not kept on this computer: "…is not kept on this computer"
	AllMailNotChecked                     // never checked, checked "in the future", or not listed by the server (no ImapBox): "…has not been checked by Thunderbird yet"
	AllMailBelowLargest                   // lists fewer than the largest other kept folder less that folder's tolerance (N4)
)

// Limits are Thunderbird's own limits for the account, from prefs.js (§4.1 step 4 and TC-9). The
// zero value is Thunderbird's defaults: every age, no size limit, background download on.
type Limits struct {
	MaxAgeDays           int  // autosync_max_age_days: > 0 keeps only that many days
	SizeLimit            bool // limit_offline_message_size
	MaxSizeKB            int  // max_size, for the words
	NoBackgroundDownload bool // autosync_offline_stores false
}

func (l Limits) on() bool { return l.MaxAgeDays > 0 || l.SizeLimit || l.NoBackgroundDownload }

// Unmatched are the M6 facts for a store with no matched account: it is not_applicable (a plain
// mail folder) only when all three are true. The zero value is "can't tell".
type Unmatched struct {
	NoProfile      bool // FindProfileDir found no profile above the store
	NoImapMailPart bool // no part of the store's path is ImapMail
	NoMsf          bool // the store holds no .msf file, in any letter case (M6 residual)
}

// OnDisk is what the walk counted (C2), expunged copies carrying 0x0008 already left out (§2.4).
type OnDisk struct {
	// Folders: messages per folder, keyed by Entry.Path exactly as the reader gives it. A kept
	// folder missing here has no mailbox file: 0. Not used for Gmail's X.
	Folders map[string]int64
	// Distinct: Gmail's X before the estimate: distinct messages by identity key (the Message-ID,
	// else the content, the C2 pass's rule) across the account's mailbox files that have a cache
	// key (L3: never one without).
	Distinct int64
	// NoKey: Gmail's counted messages with no identity key, left out of Distinct (none is
	// expected); any keeps the account from saying complete.
	NoKey int64
	// SplitMailboxes: keyed mailboxes the reader split into pieces that do not begin at a
	// separator Thunderbird writes (ADV-C2-03).
	SplitMailboxes int
	// UnkeyedMailboxes, UnkeyedMessages: walked mailboxes with no cache key and their messages
	// (L3). Counted in the result; never added to X.
	UnkeyedMailboxes int
	UnkeyedMessages  int64
}

// Input is everything the verdict decides from, for one store. Its zero value is never caught up
// and never not_applicable.
type Input struct {
	Account     string            // the store folder's name (§4.1 step 1 fold), for the words
	StoreID     string            // the store's identity by its full path (the caller's hash), carried to the Result
	Matched     bool              // C0c's match found the store's account
	OtherReader bool              // the run read the store with another reader, which counts nothing (ADV-C2-16)
	WalkFailed  bool              // the store's walk ended with an error
	Type        string            // that account's type ("imap", "pop3", …), when Matched
	Unmatched   Unmatched         // the M6 facts, when not Matched
	WholeStore  bool              // the walk covered the whole store (TC-11)
	Gmail       thunderbird.Gmail // GmailForStore's answer
	Profile     Profile           // ReadProfile's Panorama signals
	CacheErr    error             // ReadCache's refusal; nil when the cache was read
	Store       Store             // Cache.Store's view, when CacheErr is nil
	Limits      Limits
	OnDisk      OnDisk
	Now         time.Time // for the "as of" and "in the future"
}

// FolderCount is one folder's listed count and the walk's count on disk (raw, before the estimate).
type FolderCount struct {
	Path   string // as the reader gives it, uncleaned (C3 cleans and caps it, TC-7)
	Listed int64
	OnDisk int64
}

// Result is one account's verdict and the numbers its words and record need.
type Result struct {
	Account string
	StoreID string // Input.StoreID: the streak and the record's carry-forward key on it (ADV-C2-01)
	Verdict Verdict
	Reason  Reason // when Unknown

	Gmail   bool       // the Gmail rule applied (§3)
	Floor   bool       // Listed is "at least" (§3's TC-1 fold): never caught up
	AllMail AllMailUse // for Gmail

	OnDisk    int64 // X, after the expunged estimate, clamped at 0 (for Gmail, the distinct count)
	Listed    int64 // Y, or the floor
	Missing   int64 // per §4.3's fold: per-folder shortfalls clamped at 0, then summed (Gmail: Y − X, ≥ 0)
	Tolerance int64 // Tolerance(Listed)

	ExpungedEstimate int64 // what the estimate took off (TC-16)
	DeletedCopies    bool  // some kept folder has expungedBytes > 0: no "complete" words (M7)

	AsOf  int64 // the oldest lastSyncTimeInSec of the kept folders that list mail (of the kept folders, when none does); 0 when none
	Stale bool  // AsOf more than StaleAfter old, or a kept folder's time later than now (TC-12, M9)

	CapNote     int64 // TC-22: All Mail lists exactly this cap size (1,000, 2,000, 5,000 or 10,000); 0 when not
	ListsNoMail bool  // caught up by the "lists no mail" exception (§4.1 step 3's TC-2 fold)
	Limits      Limits

	Folders        []FolderCount // every kept folder (the record's per-folder X and Y; for Gmail never "behind")
	NotKept        []FolderCount // not kept, listing mail: the thunderbird_not_kept WARN, which blocks complete (L4)
	DefaultNotKept []FolderCount // Trash and Junk not kept (Thunderbird's default): a note only
	LabelsNotKept  int           // Gmail labels not kept while All Mail sets Y: one note only (TC-8)

	SkippedEntries   int // not-kept or ignored entries that failed validation: skipped and counted
	CantTellFolders  int // M2's folders with only an old-path key still in the store
	IgnoredOldKeys   int
	UnkeyedMailboxes int // L3
	UnkeyedMessages  int64
	NoIdentity       int64 // Gmail messages with no identity key, left out of X: no complete words
	SplitMailboxes   int   // kept mailboxes split differently from Thunderbird's separators
}

// StaleAfter is §2.3's 3 days, the same as state.SyncStaleAfter.
const StaleAfter = 72 * time.Hour

// capSizes are the per-folder limits Gmail's IMAP settings may offer (TC-22, recollection,
// unverified: lab row MA-1201).
var capSizes = [...]int64{1000, 2000, 5000, 10000}

// Tolerance is owner ruling O2's max(min(20, ⌊Y/10⌋), ⌊0.2% × Y⌋), applied once per account
// (§4.3's TC-4 fold). 0.2% is 1/500, so ⌊0.2% × Y⌋ is ⌊Y/500⌋ exactly.
func Tolerance(y int64) int64 {
	return max(min(20, y/10), y/500)
}

// ExpungedEstimate is TC-16's ⌈expungedBytes ÷ (folderSize ÷ listed)⌉, computed exactly as
// ⌈expungedBytes × listed ÷ folderSize⌉ and capped at math.MaxInt64. ok is false when there is
// nothing to divide by: listed ≤ 0 or folderSize ≤ 0.
func ExpungedEstimate(expungedBytes, folderSize, listed int64) (int64, bool) {
	if listed <= 0 || folderSize <= 0 {
		return 0, false
	}
	n := new(big.Int).Mul(big.NewInt(expungedBytes), big.NewInt(listed))
	n.Add(n, big.NewInt(folderSize-1))
	n.Quo(n, big.NewInt(folderSize))
	if !n.IsInt64() {
		return math.MaxInt64, true
	}
	return n.Int64(), true
}

// MaySayComplete is §4.2's full test for "this account lets the run say complete" (the M7, M9 and
// L4 folds; `status -json`'s `complete`): not_applicable, or caught up with nothing missing, not
// stale, no TC-22 cap note, no deleted copies, and no not-kept folder that gets the WARN. Spam and
// Trash not kept and Gmail labels not kept are notes and do not block it (§0, the lead's call).
// The C2 pass adds: no message without an identity key, and no mailbox split differently.
func (r Result) MaySayComplete() bool {
	if r.Verdict == NotApplicable {
		return true
	}
	return r.Verdict == CaughtUp && r.Missing == 0 && !r.Stale && r.CapNote == 0 && !r.DeletedCopies && len(r.NotKept) == 0 &&
		r.NoIdentity == 0 && r.SplitMailboxes == 0
}

// Lead is the run's lead, set by its weakest account (§4.2's fold). Its zero value is LeadFinished,
// never LeadComplete.
type Lead int

const (
	LeadFinished Lead = iota // "Backup finished."
	LeadSoFar                // "Backed up what Thunderbird has so far."
	LeadComplete             // "Export complete." (an encrypted archive's Run now note: "Backup complete:")
)

// String is the lead's name in a log; the words are C3's.
func (l Lead) String() string {
	switch l {
	case LeadSoFar:
		return "so_far"
	case LeadComplete:
		return "complete"
	}
	return "finished"
}

// RunLead is the lead of a run whose Thunderbird stores gave rs: LeadSoFar if any is behind or
// limited; otherwise LeadFinished if any may not say complete; otherwise LeadComplete. A run with no
// Thunderbird store keeps today's words, LeadComplete (MF1).
func RunLead(rs []Result) Lead {
	lead := LeadComplete
	for _, r := range rs {
		switch {
		case r.Verdict == Behind || r.Verdict == Limited:
			return LeadSoFar
		case !r.MaySayComplete():
			lead = LeadFinished
		}
	}
	return lead
}

// Assess is one store's verdict. The order is the design's: the kind of account (§2.2), the whole
// store (TC-11), the Gmail answer, Panorama before anything read from the cache (§17 item 5), the
// cache and its keys, each folder's entry (§4.1 step 3), the deleted copies (TC-16), then X, Y,
// missing and the tolerance (§4.1 step 5), and the limits last (TC-9: before the never-checked rule
// for background download).
func Assess(in Input) Result {
	r := Result{Account: in.Account, StoreID: in.StoreID, Limits: in.Limits,
		UnkeyedMailboxes: in.OnDisk.UnkeyedMailboxes, UnkeyedMessages: in.OnDisk.UnkeyedMessages,
		SplitMailboxes: in.OnDisk.SplitMailboxes}
	unknown := func(why Reason) Result {
		r.Verdict, r.Reason = Unknown, why
		return r
	}

	// The kind of account (§2.2's TC-5, M6 and M6-residual folds): by the matched account's type.
	switch {
	case in.Matched && (in.Type == "pop3" || in.Type == "none" || in.Type == "rss"):
		r.Verdict = NotApplicable
		return r
	case in.Matched && in.Type != "imap":
		return unknown(ReasonAccountType)
	case !in.Matched && in.Unmatched.NoProfile && in.Unmatched.NoImapMailPart && in.Unmatched.NoMsf:
		r.Verdict = NotApplicable
		return r
	case !in.Matched:
		return unknown(ReasonNoMatch)
	}
	if in.WalkFailed {
		return unknown(ReasonWalkFailed)
	}
	if !in.WholeStore {
		return unknown(ReasonPartialInput)
	}
	if in.OtherReader {
		return unknown(ReasonOtherReader)
	}
	switch in.Gmail {
	case thunderbird.GmailYes:
		r.Gmail = true
		r.NoIdentity = in.OnDisk.NoKey
	case thunderbird.GmailNo:
	default:
		return unknown(ReasonNoMatch) // GmailForStore cannot tell only without an account it can read
	}
	if in.Profile.PanoramaFile != No || in.Profile.PanoramaPref != No {
		return unknown(ReasonPanorama)
	}
	if in.CacheErr != nil {
		return unknown(ReasonCacheUnreadable)
	}
	s := in.Store
	r.IgnoredOldKeys, r.CantTellFolders = s.IgnoredOldKeys, len(s.OldPathOnly)
	switch {
	case s.Mode != Exact && s.Mode != Tail: // NoKeys, CantTellKeys
		return unknown(ReasonCacheUnreadable)
	case len(s.Duplicates) > 0 || len(s.DuplicateKeys) > 0:
		return unknown(ReasonDuplicateKey)
	case len(s.Refused) > 0:
		return unknown(ReasonInvalidEntry)
	}

	// Each folder's entry (§4.1 step 3, M3, C1A-01).
	var kept, notKept []Entry
	notCounted := false
	for _, e := range s.Folders {
		_, walked := in.OnDisk.Folders[e.Path]
		switch {
		case !e.FlagsKnown():
			return unknown(ReasonInvalidEntry) // M3: kept, since nobody can tell
		case e.Has(FlagVirtual) || e.Has(FlagImapNoselect):
			if !e.Valid() {
				r.SkippedEntries++
			}
		case e.Has(FlagOffline) || walked:
			// A walked folder's entry counts as a kept one's: a not-kept folder the walk read
			// must validate too (§4.1 step 2's TC-15 fold).
			switch {
			case e.Valid() && e.Has(FlagOffline):
				kept = append(kept, e)
			case e.Valid():
				notKept = append(notKept, e)
			case e.NotCountedYet():
				notCounted = true
			default:
				return unknown(ReasonInvalidEntry)
			}
		case e.Valid():
			notKept = append(notKept, e)
		default:
			r.SkippedEntries++
		}
	}
	if notCounted {
		return unknown(ReasonNotCountedYet)
	}

	// The deleted copies' estimate (TC-16, M7).
	est := map[string]int64{}
	for _, e := range kept {
		if e.ExpungedBytes.Value <= 0 {
			continue
		}
		r.DeletedCopies = true
		size := int64(0)
		if e.FolderSize.State == OK {
			size = e.FolderSize.Value
		}
		listed, _ := e.Listed()
		n, ok := ExpungedEstimate(e.ExpungedBytes.Value, size, listed)
		if !ok {
			return unknown(ReasonExpungedUnsized)
		}
		est[e.Path] = n
	}

	// Which kept folders can be counted. checked: the server has listed it (ImapBox, C1A-07) and
	// Thunderbird has checked it, not in the future (TC-12, caught_up's condition 7).
	now := in.Now.Unix()
	checked := func(e Entry) bool {
		t := e.LastSyncTimeInSec
		return e.Has(FlagImapBox) && t.State == OK && t.Value > 0 && t.Value <= now
	}
	var am *Entry // Gmail's All Mail, when it is one of the kept folders
	if r.Gmail {
		r.AllMail = AllMailNotShown
		if s.AllMail.Kind == AllMailOne && s.AllMail.Entry != nil {
			r.AllMail = AllMailNotKept
			for i := range kept {
				if kept[i].Path == s.AllMail.Entry.Path {
					am, r.AllMail = &kept[i], AllMailNotChecked
				}
			}
		}
	}
	var certain []Entry
	unsure, allChecked := 0, true
	var oldest, oldestAny int64
	for _, e := range kept {
		listed, _ := e.Listed()
		raw := max(0, in.OnDisk.Folders[e.Path])
		r.Folders = append(r.Folders, FolderCount{Path: e.Path, Listed: listed, OnDisk: raw})
		if t := e.LastSyncTimeInSec; t.State == OK && t.Value > 0 {
			if t.Value > now {
				r.Stale = true
			}
			if listed > 0 && (oldest == 0 || t.Value < oldest) {
				oldest = t.Value
			}
			if oldestAny == 0 || t.Value < oldestAny {
				oldestAny = t.Value
			}
		}
		if checked(e) {
			certain = append(certain, e)
			continue
		}
		allChecked = false
		if am != nil && e.Path == am.Path {
			continue // All Mail not checked: the floor's case (§3's TC-1 fold), not the never-checked rule
		}
		if listed > 0 || !e.Has(FlagImapBox) {
			unsure++
		}
	}
	r.AsOf = oldest
	if r.AsOf == 0 {
		r.AsOf = oldestAny
	}
	if r.AsOf > 0 && now-r.AsOf > int64(StaleAfter/time.Second) {
		r.Stale = true
	}

	// X, Y, missing and the tolerance (§3, §4.1 step 5, §4.3).
	if r.Gmail {
		var largest, spamTrash, estSum int64
		for _, e := range certain {
			listed, _ := e.Listed()
			switch {
			case am != nil && e.Path == am.Path:
			case e.Has(FlagJunk) || e.Has(FlagTrash):
				spamTrash += listed
			default:
				largest = max(largest, listed)
			}
		}
		for _, n := range est {
			estSum += n
		}
		r.ExpungedEstimate = estSum
		r.OnDisk = max(0, in.OnDisk.Distinct-estSum)
		var amListed int64
		if am != nil {
			amListed, _ = am.Listed()
			for _, c := range capSizes {
				if amListed == c {
					r.CapNote = c
				}
			}
			if checked(*am) {
				r.AllMail = AllMailBelowLargest
				if amListed >= largest-Tolerance(largest) {
					r.AllMail = AllMailSetsY
				}
				largest = max(largest, amListed)
			}
		}
		r.Listed = largest + spamTrash
		r.Floor = r.AllMail != AllMailSetsY
	} else {
		for _, e := range certain {
			listed, _ := e.Listed()
			disk := max(0, in.OnDisk.Folders[e.Path]-est[e.Path])
			r.ExpungedEstimate += est[e.Path]
			r.Listed += listed
			r.OnDisk += disk
			r.Missing += max(0, listed-disk)
		}
	}
	if r.Gmail {
		r.Missing = max(0, r.Listed-r.OnDisk)
	}
	r.Tolerance = Tolerance(r.Listed)

	// Folders not kept (§6, TC-8, §0's lead's call).
	for _, e := range notKept {
		listed, _ := e.Listed()
		fc := FolderCount{Path: e.Path, Listed: listed, OnDisk: max(0, in.OnDisk.Folders[e.Path])}
		switch {
		case e.Has(FlagTrash) || e.Has(FlagJunk):
			r.DefaultNotKept = append(r.DefaultNotKept, fc)
		case listed == 0:
		case r.AllMail == AllMailSetsY:
			r.LabelsNotKept++ // its mail is backed up through All Mail
		default:
			r.NotKept = append(r.NotKept, fc)
		}
	}

	// The verdict.
	switch {
	case r.Floor && r.OnDisk < r.Listed-r.Tolerance:
		r.Verdict = Behind
	case r.Floor:
		r.Verdict, r.Reason = Unknown, ReasonGmailFloor
	case r.Missing > r.Tolerance:
		r.Verdict = Behind
	case r.SplitMailboxes > 0:
		// A mailbox split differently from Thunderbird's separators (ADV-C2-03): a certain gap
		// above still says behind, as M2's can't-tell folders do; otherwise it can't be told.
		r.Verdict, r.Reason = Unknown, ReasonSplitDifferently
	case r.CantTellFolders > 0:
		r.Verdict, r.Reason = Unknown, ReasonOldPathOnly
	case unsure > 0:
		r.Verdict, r.Reason = Unknown, ReasonNeverChecked
	case r.Listed > 0:
		r.Verdict = CaughtUp
	case !allChecked:
		r.Verdict, r.Reason = Unknown, ReasonNeverChecked
	case listsNoMail(s.Folders, in.OnDisk):
		r.Verdict, r.ListsNoMail = CaughtUp, true
	default:
		r.Verdict, r.Reason = Unknown, ReasonCacheUnreadable
	}

	// Thunderbird's own limits (§4.1 step 4, TC-9).
	switch {
	case r.Verdict == Behind && in.Limits.on():
		r.Verdict = Limited
	case r.Verdict == Unknown && in.Limits.NoBackgroundDownload &&
		(r.Reason == ReasonNeverChecked || r.Reason == ReasonGmailFloor && r.AllMail == AllMailNotChecked):
		r.Verdict, r.Reason = Limited, NoReason
	}
	return r
}

// listsNoMail is the rest of the "lists no mail" exception (§4.1 step 3's TC-2 fold), once every
// kept folder is known checked and Y is 0: the store lists at least one folder, every folder lists
// 0, and the walk found no message.
func listsNoMail(folders []Entry, disk OnDisk) bool {
	if len(folders) == 0 || disk.Distinct > 0 || disk.UnkeyedMessages > 0 {
		return false
	}
	for _, e := range folders {
		if n, ok := e.Listed(); !ok || n != 0 {
			return false
		}
	}
	for _, n := range disk.Folders {
		if n > 0 {
			return false
		}
	}
	return true
}
