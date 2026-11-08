package app

// The Thunderbird completeness check, wired into the backup run (docs/design-thunderbird-
// completeness.md, slice C2). It changes nothing a backup archives: the walk only counts as it
// reads (source.MailboxCounts), and the verdict is taken after every store has been walked, from
// the counts, Thunderbird's own folder list (folderCache.json, read once per profile per run,
// TC-21) and the account's settings. A plaintext run that ends ok records it (the last-run record
// and the run history) and keeps each account's behind-streak in its own small record (M10); an
// encrypted archive's run keeps it in memory only, for the screen (O1). The words are C3's.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
	"mail-archive-tool/internal/thunderbird"
	"mail-archive-tool/internal/util"
)

// Seams, so a test sees each read the check makes (the functions are the real ones).
var (
	findTBProfile  = thunderbird.FindProfileDir
	readStoreFacts = thunderbird.ReadStoreFacts
	readTBCache    = tbcomplete.ReadCache
	readTBProfile  = tbcomplete.ReadProfile
	readTBStreak   = state.ReadTBStreak
	writeTBStreak  = state.WriteTBStreak
	removeTBStreak = state.RemoveTBStreak
	// tbCheckOff turns the check off for a test that compares a run with and without it (the
	// archive's bytes must be the same either way).
	tbCheckOff = false
)

// tbStore is one local mbox or maildir source of the run, as the check sees it.
type tbStore struct {
	input      string // the input path as given, made absolute: the store is told by it, never by -copy-first's copy (TC-11)
	isDir      bool
	profile    string
	hasProfile bool
	facts      thunderbird.StoreFacts
	counts     *source.MailboxCounts // nil: the walk counts nothing for it
	// otherReader: the run opened the store with another reader than the mbox one (the
	// Evolution reader, which a maildir store with folders named "cur" and ".x" passes for,
	// ADV-C2-16), so the walk counted nothing; walkFailed: its walk ended with an error. Either
	// way the store is told unknown, never left out (the C2 pass's LOW item 3).
	otherReader bool
	walkFailed  bool
}

// planTBOther is planTB for a local store the run opened with another reader than the mbox
// one: kept (as unknown, other_reader) only when it is under a Thunderbird profile, so a real
// Evolution store is never told anything. The walk counts nothing for it.
func planTBOther(input string) *tbStore {
	t := planTB(input)
	if !t.hasProfile {
		return nil
	}
	t.counts, t.otherReader = nil, true
	return t
}

// storeID is a store's identity by its full path, profile and store, each made absolute and
// compared as this OS compares names (tbcomplete.FolderIdentity over the slash form), hashed: the
// streak and the record's carry-forward key on it, so two stores with one folder name are never
// one account (ADV-C2-01). A hash, so no path is written beside the archive.
func storeID(profile, store string) string {
	norm := func(p string) string {
		if p == "" {
			return ""
		}
		return tbcomplete.FolderIdentity(filepath.ToSlash(filepath.Clean(absOrSame(p))))
	}
	return fingerprint("tbstore\x00" + norm(profile) + "\x00" + norm(store))
}

// planTB is what the check needs of a local source before its walk: its profile and account,
// and so whether the walk counts. Only a whole store of a matched imap account is counted; an
// account of another type (Local Folders, POP3, Feeds, Exchange) never is, and never reaches the
// folder cache (the C1a pass round 2's R2-02 note: IMAP is decided by the account's type, never
// by the folder's path); a store with no profile above it has its ".msf" entries noted (the M6
// residual).
func planTB(input string) *tbStore {
	t := &tbStore{input: absOrSame(input)}
	if fi, err := os.Stat(t.input); err == nil {
		t.isDir = fi.IsDir()
	}
	t.profile, t.hasProfile = findTBProfile(t.input)
	if t.hasProfile {
		t.facts, _ = readStoreFacts(t.profile, t.input) // no account: Assess says unknown (no_match)
	}
	a := t.facts.Account
	switch {
	case a != nil && a.Type == "imap" && t.facts.Whole:
		t.counts = &source.MailboxCounts{Count: true, Distinct: t.facts.Gmail == thunderbird.GmailYes}
	case a == nil && !t.hasProfile && t.isDir:
		t.counts = &source.MailboxCounts{}
	}
	return t
}

// storeDir is the folder the check takes as the store: the matched account's, else the input
// folder, else the folder that holds the input file.
func (t *tbStore) storeDir() string {
	switch {
	case t.facts.Account != nil:
		return t.facts.Store
	case t.isDir:
		return t.input
	}
	return filepath.Dir(t.input)
}

// noMsf is the M6 residual's "the store holds no .msf summary file": seen by the walk's own
// listing for a folder, and for one mailbox file by its folder's names (never opened).
func (t *tbStore) noMsf() bool {
	if t.isDir {
		return t.counts != nil && !t.counts.Msf
	}
	entries, err := os.ReadDir(filepath.Dir(t.input))
	if err != nil {
		return false // cannot tell: never not_applicable
	}
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".msf") {
			return false
		}
	}
	return true
}

// hasImapMailPart reports whether a part of p is named ImapMail, compared as the OS compares
// names (case-blind on Windows and macOS).
func hasImapMailPart(p string) bool {
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator }) {
		if part == "ImapMail" || ((runtime.GOOS == "windows" || runtime.GOOS == "darwin") && strings.EqualFold(part, "ImapMail")) {
			return true
		}
	}
	return false
}

// assessThunderbird gives each Thunderbird store of the run its verdict, after the walk of every
// store (TC-21): the folder cache and the profile's own signals are read once per profile, in
// memory for this run only. A store that is not_applicable (Local Folders, POP3, Feeds, a plain
// mail folder) gives no result: it has no completeness to tell. An account walked whole and also
// as a part, in one run, is told once, by its whole walk.
func assessThunderbird(stores []*tbStore, now time.Time) []tbcomplete.Result {
	if tbCheckOff || len(stores) == 0 {
		return nil
	}
	type cacheRead struct {
		c   *tbcomplete.Cache
		err error
	}
	caches := map[string]cacheRead{}
	profiles := map[string]tbcomplete.Profile{}
	wholes := map[string]bool{}
	for _, t := range stores {
		if t.facts.Account != nil && t.facts.Whole {
			wholes[t.facts.Store] = true
		}
	}
	var out []tbcomplete.Result
	told := map[string]bool{}
	for _, t := range stores {
		a, store := t.facts.Account, t.storeDir()
		if a != nil {
			if told[store] || (!t.facts.Whole && wholes[store]) {
				continue
			}
			told[store] = true
		}
		in := tbcomplete.Input{
			Account:     filepath.Base(store),
			StoreID:     storeID(t.profile, store),
			Matched:     a != nil,
			WholeStore:  t.facts.Whole,
			OtherReader: t.otherReader,
			WalkFailed:  t.walkFailed,
			Gmail:       t.facts.Gmail,
			Now:         now,
		}
		if a == nil {
			in.Unmatched = tbcomplete.Unmatched{NoProfile: !t.hasProfile, NoImapMailPart: !hasImapMailPart(store), NoMsf: t.noMsf()}
		} else {
			in.Type = a.Type
		}
		if a != nil && a.Type == "imap" && t.facts.Whole && t.counts != nil && !t.walkFailed {
			if _, ok := profiles[t.profile]; !ok {
				profiles[t.profile] = readTBProfile(t.profile)
			}
			in.Profile = profiles[t.profile]
			cr, ok := caches[t.profile]
			if !ok {
				c, err := readTBCache(t.profile)
				cr = cacheRead{c, err}
				caches[t.profile] = cr
			}
			in.CacheErr = cr.err
			if cr.err == nil {
				view, err := cr.c.StoreAmong(store, t.facts.OtherStores)
				if err != nil {
					in.CacheErr = err
				}
				in.Store = view
				var walked []tbcomplete.Mailbox
				for _, mb := range t.counts.Mailboxes {
					walked = append(walked, tbcomplete.Mailbox{Rel: mb.Rel, Messages: mb.Messages, Keys: mb.Keys, NoKey: mb.NoKey, Unseparated: mb.Unseparated})
				}
				in.OnDisk = view.OnDiskFor(walked, t.facts.Gmail == thunderbird.GmailYes)
			}
			l := t.facts.Limits
			in.Limits = tbcomplete.Limits{MaxAgeDays: l.MaxAgeDays, SizeLimit: l.SizeLimit, MaxSizeKB: l.MaxSizeKB, NoBackgroundDownload: l.NoBackgroundDownload}
		}
		r := tbcomplete.Assess(in)
		if r.Verdict == tbcomplete.NotApplicable {
			continue
		}
		out = append(out, r)
	}
	return out
}

// allMailNames are AllMailUse's names in the record.
var allMailNames = map[tbcomplete.AllMailUse]string{
	tbcomplete.AllMailSetsY: "sets_y", tbcomplete.AllMailNotShown: "not_shown", tbcomplete.AllMailNotKept: "not_kept",
	tbcomplete.AllMailNotChecked: "not_checked", tbcomplete.AllMailBelowLargest: "below_largest",
}

// fingerprint is a TC-19 hash: the first 16 bytes of SHA-256, in hex.
func fingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

// limitsOn reports whether one of Thunderbird's own limits is on (tbcomplete's rule).
func limitsOn(l tbcomplete.Limits) bool {
	return l.MaxAgeDays > 0 || l.SizeLimit || l.NoBackgroundDownload
}

// tbRecords is the run's results as a plaintext last-run record keeps them (§7, §9.1), each
// TC-19 fingerprint carried forward from prev (the previous run's record, read before this run
// wrote its "running" one) while it is unchanged, so its first-seen time stays.
//
// The carry-forward is by the store's identity (StoreID), never by its folder name, so two
// stores with one name never trade first-seen times (ADV-C2-01); a previous entry with no store
// identity carries nothing (the chosen-setting WARN shows once more). The folders of all three
// lists across every account stay within state.TBFoldersTotalMax: each account gets its share,
// not-kept folders first (they carry a WARN), then kept, then Trash and Junk (ADV-C2-08).
func tbRecords(results []tbcomplete.Result, prev []state.TBAccount, now time.Time) []state.TBAccount {
	before := map[string]state.TBAccount{}
	for _, p := range prev {
		if p.StoreID != "" {
			before[p.StoreID] = p
		}
	}
	n := min(len(results), state.TBAccountsMax)
	share := state.TBFoldersTotalMax
	if n > 0 {
		share = max(1, state.TBFoldersTotalMax/n)
	}
	var out []state.TBAccount
	for _, r := range results {
		if len(out) >= state.TBAccountsMax {
			break
		}
		a := state.TBAccount{
			Account: state.CleanFolderName(r.Account), StoreID: r.StoreID, Verdict: r.Verdict.String(), Reason: r.Reason.String(),
			NoIdentity: r.NoIdentity, SplitMailboxes: r.SplitMailboxes,
			Complete: r.MaySayComplete(), Gmail: r.Gmail, Floor: r.Floor, AllMail: allMailNames[r.AllMail],
			OnDisk: r.OnDisk, Listed: r.Listed, Missing: r.Missing, Tolerance: r.Tolerance,
			ExpungedEstimate: r.ExpungedEstimate, DeletedCopies: r.DeletedCopies, Stale: r.Stale,
			CapNote: r.CapNote, ListsNoMail: r.ListsNoMail, LabelsNotKept: r.LabelsNotKept,
			CantTellFolders: r.CantTellFolders, SkippedEntries: r.SkippedEntries, IgnoredOldKeys: r.IgnoredOldKeys,
			UnkeyedMailboxes: r.UnkeyedMailboxes, UnkeyedMessages: r.UnkeyedMessages,
		}
		if r.AsOf > 0 {
			a.AsOf = time.Unix(r.AsOf, 0).UTC()
		}
		p, had := before[a.StoreID]
		had = had && a.StoreID != ""
		since := func(hash, prevHash string, prevSince time.Time) time.Time {
			if had && hash == prevHash && !prevSince.IsZero() {
				return prevSince
			}
			return now
		}
		if l := r.Limits; limitsOn(l) {
			a.Limit = &state.TBLimit{NoBackgroundDownload: l.NoBackgroundDownload}
			if l.MaxAgeDays > 0 {
				a.Limit.MaxAgeDays = l.MaxAgeDays
			}
			if l.SizeLimit {
				a.Limit.MaxSizeKB = l.MaxSizeKB
			}
			a.LimitHash = fingerprint(fmt.Sprintf("age=%d size=%v kb=%d background=%v", max(0, l.MaxAgeDays), l.SizeLimit, l.MaxSizeKB, !l.NoBackgroundDownload))
			a.LimitSince = since(a.LimitHash, p.LimitHash, p.LimitSince)
		}
		folders := append([]tbcomplete.FolderCount(nil), r.Folders...)
		sort.SliceStable(folders, func(i, j int) bool {
			si, sj := folders[i].Listed-folders[i].OnDisk, folders[j].Listed-folders[j].OnDisk
			if si != sj {
				return si > sj
			}
			return folders[i].Path < folders[j].Path
		})
		notKept := byListed(r.NotKept)
		left := share
		a.NotKept, a.NotKeptMore = tbFolders(notKept, min(state.TBNotKeptMax, left))
		left -= len(a.NotKept)
		a.Folders, a.FoldersMore = tbFolders(folders, min(state.TBFolderMax, left))
		left -= len(a.Folders)
		a.DefaultNotKept, a.DefaultNotKeptMore = tbFolders(byListed(r.DefaultNotKept), min(state.TBNotKeptMax, left))
		if len(notKept) > 0 {
			ids := make([]string, 0, len(notKept))
			for _, f := range notKept {
				ids = append(ids, tbcomplete.FolderIdentity(f.Path))
			}
			sort.Strings(ids)
			a.NotKeptHash = fingerprint(strings.Join(ids, "\x00"))
			a.NotKeptSince = since(a.NotKeptHash, p.NotKeptHash, p.NotKeptSince)
		}
		out = append(out, a)
	}
	return out
}

// byListed is fs sorted by listed count, largest first, then by name.
func byListed(fs []tbcomplete.FolderCount) []tbcomplete.FolderCount {
	out := append([]tbcomplete.FolderCount(nil), fs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Listed != out[j].Listed {
			return out[i].Listed > out[j].Listed
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// tbFolders is the first limit of fs as the record keeps them, names cleaned and capped (TC-7),
// and how many more there were.
func tbFolders(fs []tbcomplete.FolderCount, limit int) ([]state.TBFolder, int) {
	var out []state.TBFolder
	for i, f := range fs {
		if i == limit {
			return out, len(fs) - limit
		}
		out = append(out, state.TBFolder{Folder: state.CleanFolderName(f.Path), Listed: f.Listed, OnDisk: f.OnDisk})
	}
	return out, 0
}

// previousThunderbird is the completeness record of the archive's last run, read before this
// run writes its "running" record over it (TC-19's read-before-the-start-write); nil for an
// encrypted archive, which keeps none.
func previousThunderbird(opts Options) []state.TBAccount {
	if opts.Key != nil || encryptedAt(opts.Out) {
		return nil
	}
	prev, st, _ := state.ReadLastRun(opts.Out)
	if st != state.LastRunPresent {
		return nil
	}
	return prev.Thunderbird
}

// updateTBStreak folds a finished run's verdicts into the archive's behind-streak record (M10;
// §5's rules): only a behind run starts or extends an account's streak; a caught_up run ends it
// with no email and a limited run ends it silently; an unknown run, and an account the run did
// not reach, move nothing. Plaintext only, by code: a keyed run, or a run into an encrypted
// archive, writes nothing and removes a stray record (O1). Only a run that ended ok moves it.
// Best effort: a read or write problem never fails the run and never falsely moves the streak.
//
// Each account's streak is keyed by its store's identity (Result.StoreID, ADV-C2-01), never by
// its folder name. A record it cannot use is never silent (the C2 pass's ADV-C2-05, -10): the
// run logs a line and returns a WARN for the last-run record (code tbstreak_unreadable), which
// status shows. Then:
//   - a record other users can read (util.ErrReadableByOthers) is not trusted (other users may
//     also have written it): the streak starts again from this run and the record is rewritten
//     owner-only, so the next run reads it. Fresh is the fail-safe reading for R42: a fresh
//     streak can only make the "still downloading" notice later, never claim more than was seen;
//   - a record a later MailArchive wrote (state.ErrTBStreakNewer) is never downgraded: nothing
//     moves and nothing is written;
//   - any other refusal (a link, a pipe, too large, a read error) leaves the record as it is.
func updateTBStreak(opts Options, results []tbcomplete.Result, encrypted, ok bool, now time.Time, logger *log.Logger) *state.RunWarning {
	if opts.Key != nil || encrypted || ArchiveEncrypted(opts.Out) {
		_ = removeTBStreak(opts.Out)
		return nil
	}
	if !ok || len(results) == 0 {
		return nil
	}
	path := filepath.Join(opts.Out, state.TBStreakName)
	logf := func(format string, args ...any) {
		if logger != nil {
			logger.Printf(format, args...)
		}
	}
	var warn *state.RunWarning
	prev, err := readTBStreak(opts.Out)
	switch {
	case err == nil:
	case errors.Is(err, util.ErrReadableByOthers):
		logf("warning: MailArchive's record of how long Thunderbird has been downloading (%s) could be read by other users, so it was not trusted; it starts again from this backup and is now readable only by you", path)
		warn = &state.RunWarning{Code: tbStreakUnreadable, Message: fmt.Sprintf("MailArchive's record of how long Thunderbird has been downloading (%s) could be read by other users, so it started again from the last backup. Nothing is needed unless this shows after every backup: then the archive is on a drive that cannot keep files private (such as exFAT), and the \"still downloading\" notice cannot count days there.", path)}
		prev = state.TBStreak{}
	case errors.Is(err, state.ErrTBStreakNewer):
		logf("warning: %v; it was left as it is, so how long Thunderbird has been downloading is not counted by this version", err)
		return &state.RunWarning{Code: tbStreakUnreadable, Message: fmt.Sprintf("A newer MailArchive keeps its record of how long Thunderbird has been downloading in %s, so this version leaves it as it is and does not count. Use the newer MailArchive for this archive.", path)}
	default:
		logf("warning: could not read MailArchive's record of how long Thunderbird has been downloading (%v); it was left as it is", err)
		return &state.RunWarning{Code: tbStreakUnreadable, Message: fmt.Sprintf("MailArchive could not read its record of how long Thunderbird has been downloading (%s), so it was left as it is and not counted. Remove the file if it is not a regular file of yours; the next backup starts it again.", path)}
	}
	next, moved := prev, warn != nil
	for _, r := range results {
		if r.StoreID == "" {
			continue // a store with no identity is never matched to a streak
		}
		_, had := next.Account(r.StoreID)
		switch r.Verdict {
		case tbcomplete.Behind:
			next, moved = next.Behind(r.StoreID, state.CleanFolderName(r.Account), r.OnDisk, now), true
		case tbcomplete.CaughtUp, tbcomplete.Limited:
			if had {
				next, moved = next.End(r.StoreID), true
			}
		}
	}
	if !moved {
		return warn
	}
	if werr := writeTBStreak(opts.Out, next.Named()); werr != nil {
		logf("warning: could not record how long Thunderbird has been downloading in %s (%v)", path, werr)
	}
	return warn
}

// tbStreakUnreadable is the WARN code of a behind-streak record the run could not use.
const tbStreakUnreadable = "tbstreak_unreadable"
