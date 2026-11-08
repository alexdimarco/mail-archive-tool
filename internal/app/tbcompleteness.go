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
			Account:    filepath.Base(store),
			Matched:    a != nil,
			WholeStore: t.facts.Whole,
			Gmail:      t.facts.Gmail,
			Now:        now,
		}
		if a == nil {
			in.Unmatched = tbcomplete.Unmatched{NoProfile: !t.hasProfile, NoImapMailPart: !hasImapMailPart(store), NoMsf: t.noMsf()}
		} else {
			in.Type = a.Type
		}
		if a != nil && a.Type == "imap" && t.facts.Whole {
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
					walked = append(walked, tbcomplete.Mailbox{Rel: mb.Rel, Messages: mb.Messages, IDs: mb.IDs, NoID: mb.NoID})
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
func tbRecords(results []tbcomplete.Result, prev []state.TBAccount, now time.Time) []state.TBAccount {
	before := map[string]state.TBAccount{}
	for _, p := range prev {
		before[p.Account] = p
	}
	var out []state.TBAccount
	for _, r := range results {
		if len(out) >= state.TBAccountsMax {
			break
		}
		a := state.TBAccount{
			Account: state.CleanFolderName(r.Account), Verdict: r.Verdict.String(), Reason: r.Reason.String(),
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
		p, had := before[a.Account]
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
		a.Folders, a.FoldersMore = tbFolders(folders, state.TBFolderMax)
		notKept := byListed(r.NotKept)
		a.NotKept, a.NotKeptMore = tbFolders(notKept, state.TBNotKeptMax)
		if len(notKept) > 0 {
			ids := make([]string, 0, len(notKept))
			for _, f := range notKept {
				ids = append(ids, tbcomplete.FolderIdentity(f.Path))
			}
			sort.Strings(ids)
			a.NotKeptHash = fingerprint(strings.Join(ids, "\x00"))
			a.NotKeptSince = since(a.NotKeptHash, p.NotKeptHash, p.NotKeptSince)
		}
		a.DefaultNotKept, _ = tbFolders(byListed(r.DefaultNotKept), state.TBNotKeptMax)
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
func updateTBStreak(opts Options, results []tbcomplete.Result, encrypted, ok bool, now time.Time, logger *log.Logger) {
	if opts.Key != nil || encrypted || ArchiveEncrypted(opts.Out) {
		_ = removeTBStreak(opts.Out)
		return
	}
	if !ok || len(results) == 0 {
		return
	}
	prev, err := readTBStreak(opts.Out)
	if err != nil {
		return // an unreadable record is left as it is, never reset
	}
	next, moved := prev, false
	for _, r := range results {
		name := state.CleanFolderName(r.Account)
		_, had := next.Account(name)
		switch r.Verdict {
		case tbcomplete.Behind:
			next, moved = next.Behind(name, r.OnDisk, now), true
		case tbcomplete.CaughtUp, tbcomplete.Limited:
			if had {
				next, moved = next.End(name), true
			}
		}
	}
	if !moved {
		return
	}
	if werr := writeTBStreak(opts.Out, next.Named()); werr != nil && logger != nil {
		logger.Printf("warning: could not record how long Thunderbird has been downloading in %s (%v)", filepath.Join(opts.Out, state.TBStreakName), werr)
	}
}
