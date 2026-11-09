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

	"mail-archive-tool/internal/schedule"
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
// one: kept (as unknown) when it is under a Thunderbird profile, or, with no profile above it,
// when its folder holds any .msf summary file (fold check 3's N1, the lead's call of 2026-11-08:
// the M6 residual's rule, so a Thunderbird store copied out of its profile is "can't tell", never
// no result). A real Evolution store has no .msf file and is never told anything. The walk counts
// nothing for it.
func planTBOther(input string) *tbStore {
	t := planTB(input)
	if !t.hasProfile && !holdsMsf(t.storeDir()) {
		return nil
	}
	t.counts, t.otherReader = nil, true
	return t
}

// holdsMsf reports whether dir holds an entry named *.msf (any letter case), seen by name in one
// listing, never opened. A folder that cannot be listed shows none (as before N1, a folder under
// no profile is then told nothing): the reader cannot walk it either, so the run fails and its
// failure words win (§11).
func holdsMsf(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".msf") {
			return true
		}
	}
	return false
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
			Name:        t.facts.Name,
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
					walked = append(walked, tbcomplete.Mailbox{Rel: mb.Rel, Messages: mb.Messages, Keys: mb.Keys, NoKey: mb.NoKey,
						Loose: mb.Loose, LooseKeys: mb.LooseKeys, LooseNoKey: mb.LooseNoKey, Unseparated: mb.Unseparated, Glued: mb.Glued,
						Misaligned: mb.Misaligned, Uncounted: mb.Uncounted})
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
//
// Past state.TBAccountsMax accounts the record keeps the weakest first (behind, limited,
// unknown, then the caught-up ones that may not say complete, then the rest), each group in the
// run's order, and tbRecordsMore says how many were left out (the C2 pass round 2's R2-06).
func tbRecords(results []tbcomplete.Result, prev []state.TBAccount, now time.Time) []state.TBAccount {
	before := map[string]state.TBAccount{}
	for _, p := range prev {
		if p.StoreID != "" {
			before[p.StoreID] = p
		}
	}
	if len(results) > state.TBAccountsMax {
		results = append([]tbcomplete.Result(nil), results...)
		sort.SliceStable(results, func(i, j int) bool { return weakness(results[i]) < weakness(results[j]) })
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
			Account: state.CleanFolderName(r.Account), Name: state.CleanFolderName(r.Name), StoreID: r.StoreID, Verdict: r.Verdict.String(), Reason: r.Reason.String(),
			NoIdentity: r.NoIdentity, SplitMailboxes: r.SplitMailboxes,
			Complete: r.MaySayComplete(), Gmail: r.Gmail, Floor: r.Floor, AllMail: allMailNames[r.AllMail],
			OnDisk: r.OnDisk, Listed: r.Listed, Missing: r.Missing, Tolerance: r.Tolerance,
			ExpungedEstimate: r.ExpungedEstimate, DeletedCopies: r.DeletedCopies, Stale: r.Stale,
			CapNote: r.CapNote, ListsNoMail: r.ListsNoMail, LabelsNotKept: r.LabelsNotKept,
			CantTellFolders: r.CantTellFolders, SkippedEntries: r.SkippedEntries, IgnoredOldKeys: r.IgnoredOldKeys,
			UnkeyedMailboxes: r.UnkeyedMailboxes, UnkeyedMessages: r.UnkeyedMessages,
			GluedMailboxes: r.GluedMailboxes, NoStatus: r.NoStatus, MisalignedMailboxes: r.MisalignedMailboxes,
			ThunderbirdVersion: r.Version,
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

// tbRecordsMore is how many of the run's results the record leaves out (tbRecords).
func tbRecordsMore(results []tbcomplete.Result) int {
	return max(0, len(results)-state.TBAccountsMax)
}

// TBAccounts is r's Thunderbird verdicts in the shape the last-run record keeps them, names
// cleaned and capped (TC-7), and how many it leaves out past state.TBAccountsMax: what the
// wizard's end summary, Run now's note and the command line say them from (state.TBLines), so
// a run's words on screen and its record's words in `status` are one producer's (slice C3). For
// the screen only: nothing here is written, and for an encrypted archive it is the only form the
// verdicts take (O1). The run's lead is tbcomplete.RunLead(r.Thunderbird).
//
// Each account carries the name Thunderbird shows for it (state.TBAccount.Name, for the screen
// only; the owner's ruling of 2026-11-08 on the C3 friction review's M1), except when two accounts
// share one name as the words would say it (compared without letter case): then both are said by
// their folders, so two accounts never read as one.
func TBAccounts(r Result) ([]state.TBAccount, int) {
	if len(r.Thunderbird) == 0 {
		return nil, 0
	}
	accts := tbRecords(r.Thunderbird, nil, time.Now().UTC())
	seen := map[string]int{}
	for _, a := range accts {
		if a.Name != "" {
			seen[strings.ToLower(a.Name)]++
		}
	}
	for i := range accts {
		if seen[strings.ToLower(accts[i].Name)] > 1 {
			accts[i].Name = ""
		}
	}
	return accts, tbRecordsMore(r.Thunderbird)
}

// TBScheduled reports whether a schedule other than "only when I click Run now" will run the
// archive at out's later backups, so the words may say "later backups add the rest" (§7's
// TC-17 fold). A descriptor that cannot be read is no schedule: the words then ask for the
// backup to be run again, which is true either way.
func TBScheduled(out string) bool {
	d, err := schedule.ReadDescriptor(out)
	return err == nil && !d.IsManual()
}

// weakness orders results for a record that cannot keep them all: the lower, the weaker (kept
// first). It follows tbcomplete.RunLead's order.
func weakness(r tbcomplete.Result) int {
	switch {
	case r.Verdict == tbcomplete.Behind:
		return 0
	case r.Verdict == tbcomplete.Limited:
		return 1
	case r.Verdict == tbcomplete.Unknown:
		return 2
	case !r.MaySayComplete():
		return 3
	}
	return 4
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
//
// A full record never drops a streak this run found behind (the C2 pass round 3's R3-04): a new
// behind store that finds no room is logged by name and the run gets a WARN for the last-run
// record (code tbstreak_full), which status shows. The WARNs come back in the order they arose.
func updateTBStreak(opts Options, results []tbcomplete.Result, encrypted, ok bool, now time.Time, logger *log.Logger) []state.RunWarning {
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
	var warns []state.RunWarning
	prev, err := readTBStreak(opts.Out)
	switch {
	case err == nil:
	case errors.Is(err, util.ErrReadableByOthers):
		logf("warning: MailArchive's record of how long Thunderbird has been downloading (%s) could be read by other users, so it was not trusted; it starts again from this backup and is now readable only by you", path)
		warns = append(warns, state.RunWarning{Code: tbStreakUnreadable, Message: tbStreakOthersMessage(path)})
		prev = state.TBStreak{}
	case errors.Is(err, state.ErrTBStreakNewer):
		logf("warning: %v; it was left as it is, so how long Thunderbird has been downloading is not counted by this version", err)
		return []state.RunWarning{{Code: tbStreakUnreadable, Message: tbStreakNewerMessage(path)}}
	default:
		logf("warning: could not read MailArchive's record of how long Thunderbird has been downloading (%v); it was left as it is", err)
		return []state.RunWarning{{Code: tbStreakUnreadable, Message: tbStreakOtherMessage(path)}}
	}
	// The run's own count and time, and every store it reached is seen, whatever its verdict
	// (the C2 pass round 2's R2-08): an entry whose store no run has reached for
	// state.TBStreakUnseenFor is pruned below (by time: round 3's R3-03). Streaks already in the
	// record are folded first, so a full record that must make room for a new one drops only an
	// entry this run did not find behind (R3-04).
	next, moved := prev.StartRun(now), len(warns) > 0
	for _, r := range results {
		if r.StoreID != "" {
			next = next.Seen(r.StoreID, now)
		}
	}
	var waiting []string // new behind stores that found no room
	for _, existing := range []bool{true, false} {
		for _, r := range results {
			if r.StoreID == "" {
				continue // a store with no identity is never matched to a streak
			}
			if _, had := next.Account(r.StoreID); had != existing {
				continue
			}
			switch r.Verdict {
			case tbcomplete.Behind:
				var gone state.TBStreakAccount
				var room state.TBRoom
				name := state.CleanFolderName(r.Account)
				next, gone, room = next.BehindEvicting(r.StoreID, name, r.OnDisk, now)
				moved = true
				switch room {
				case state.TBRoomEvicted:
					logf("warning: MailArchive's record of how long Thunderbird has been downloading (%s) is full (%d accounts), so it dropped the record of %s, last reached %d backup(s) ago, to start counting %s", path, state.TBStreakAccountsMax, gone.Account, next.Run-gone.SeenRun, name)
				case state.TBRoomFull:
					logf("warning: MailArchive's record of how long Thunderbird has been downloading (%s) is full: all %d accounts in it were still downloading at this backup, so it could not start counting %s", path, state.TBStreakAccountsMax, name)
					waiting = append(waiting, name)
				}
			case tbcomplete.CaughtUp, tbcomplete.Limited:
				if existing {
					next, moved = next.End(r.StoreID), true
				}
			}
		}
	}
	if len(waiting) > 0 {
		warns = append(warns, state.RunWarning{Code: tbStreakFull, Message: tbStreakFullMessage(path, waiting)})
	}
	next, pruned := next.Prune(now)
	for _, a := range pruned {
		logf("note: MailArchive's record of how long Thunderbird has been downloading (%s) dropped %s: no backup has reached it in the last %d days", path, a.Account, int(state.TBStreakUnseenFor/(24*time.Hour)))
	}
	// With any streak kept, the run's count and time are recorded every run.
	moved = moved || len(pruned) > 0 || len(next.Accounts) > 0
	if !moved {
		return warns
	}
	if werr := writeTBStreak(opts.Out, next.Named()); werr != nil {
		logf("warning: could not record how long Thunderbird has been downloading in %s (%v)", path, werr)
	}
	return warns
}

// The WARN codes of the behind-streak record: one the run could not use, and one too full to
// start a behind store's streak (R3-04).
const (
	tbStreakUnreadable = "tbstreak_unreadable"
	tbStreakFull       = "tbstreak_full"
)

// The tbstreak_unreadable sentences (slice C3's words; `status` and Status & health show the
// recorded one, and may name the file): a record other users could read, so not trusted and
// started again (ADV-C2-05); one a newer MailArchive wrote, left alone (ADV-C2-10); and any other
// refusal (a link, a pipe, too large, a read error), left alone.
func tbStreakOthersMessage(path string) string {
	return "MailArchive did not trust its record of how long Thunderbird has been downloading (" + path + "), because other people on this computer could read it, so it started that count again at the last backup and made the file private. Nothing is needed unless this shows after every backup: then the archive is on a drive that cannot keep files private (such as a USB drive formatted as exFAT), and the \"still downloading\" notice cannot count days there."
}

func tbStreakNewerMessage(path string) string {
	return "A newer version of MailArchive keeps its record of how long Thunderbird has been downloading in " + path + ", so this version left it alone and did not count the last backup. Use the newer MailArchive for this archive."
}

func tbStreakOtherMessage(path string) string {
	return "MailArchive could not read its record of how long Thunderbird has been downloading (" + path + "), so it left the file alone and did not count the last backup. If that file is not one MailArchive made (for example a shortcut, or a folder of that name), remove it; the next backup starts the record again."
}

// tbStreakFullNames is how many of the waiting accounts tbstreak_full's sentence names.
const tbStreakFullNames = 3

// tbStreakFullMessage is tbstreak_full's sentence: the record, how many accounts wait and the
// first of them by name, and what follows (the builder's words; C3 owns the final ones).
func tbStreakFullMessage(path string, waiting []string) string {
	names := waiting
	more := ""
	if len(names) > tbStreakFullNames {
		names, more = names[:tbStreakFullNames], fmt.Sprintf(" and %d more", len(waiting)-tbStreakFullNames)
	}
	return fmt.Sprintf("MailArchive's record of how long Thunderbird has been downloading (%s) is full: all %d accounts in it were still downloading at the last backup, so it did not start counting %d more (%s%s). The \"still downloading\" notice cannot come for those until an account in the record catches up or is no longer backed up.", path, state.TBStreakAccountsMax, len(waiting), strings.Join(names, ", "), more)
}
