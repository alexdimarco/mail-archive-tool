package desktop

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// This file wires the folder-only archive index (internal/archindex, the
// archive-manager Slice-1 leaf) into the Desktop. The Desktop is the index's sole
// maintainer (operator C2 decision, docs/design-archive-manager.md rev 2 §4/§5.1,
// SD-6): it folds the folders it already knows about into the index and reads the
// list back for the "Your archives" surface (UX-1, §13). Every per-archive fact
// shown here — whether an archive is the current one, whether it is reachable, what
// it was made from — is derived LIVE, never cached in the index, so nothing here can
// drift against the archive itself.

// ArchiveRow is one row of the Desktop's archive list: a known archive folder plus
// the three facts derived live for it. Current follows the active pointer
// (desktop-settings out), NEVER a field in the index — the active-archive invariant.
// Available and Kind come from archindex's SAFE bounded read of the archive.
type ArchiveRow struct {
	Path      string
	Current   bool   // this folder is the archive in use (effectiveOut)
	Available bool   // reachable right now and still a readable archive
	Sealed    bool   // paused (hard read-only) — show Resume, not Switch/Pause
	Kind      string // local | m365 | mixed | empty; "" when unavailable
	// SourceLostSince is a "since DATE" string when this archive's SOURCE (its mailbox or
	// local mail folder) has been unreachable past the surface threshold (§5.7, S3-C6); ""
	// when the source is reachable or the streak has not crossed the threshold.
	SourceLostSince string
	// RunNowOnly is set beside SourceLostSince when the archive's record says its backup runs
	// only when Run now is clicked (MA-949): nothing resumes by itself, so the banner says to
	// click Run now once the source is back.
	RunNowOnly bool
	// SourceLostRepair is set beside SourceLostSince when a Repair or a Run now would now get past
	// the gone mailbox the archive's last run refused on (health.GoneMailboxRepairable, MA-1479):
	// the banner then says to click Repair (or Run now) first, in MA-949's forms.
	SourceLostRepair bool
	// SourceLostMailbox is the missing mailbox the Repair words name (the 1.7.5 friction review's
	// F10), beside SourceLostRepair. SourceLostSet: this is the current archive and its scheduled
	// entry is there (a Repair done, or not yet paused), so the banner says the next backup skips
	// it by itself and Run now backs up at once (F2).
	SourceLostMailbox string
	SourceLostSet     bool
	// SourceLostWords, beside SourceLostSince, are the words for an archive whose last refused run
	// kept why: Thunderbird keeps its account's mail in another folder now, or no longer has the
	// account (health.SourceReasonWordsAt, the words status says; docs/design-per-mailbox-archives.md
	// §11, F-03, MA-1506). Shown in place of the banner's "reconnect the source" forms; "" for any
	// other archive, which keeps today's words.
	SourceLostWords string
	// Problem is, for a folder this computer recorded as an encrypted archive that no
	// longer holds it — emptied, replanted, a link, its key files gone — that case's
	// own words (design-encrypted-archives-phase3 §4.5): shown in place of Switch and
	// Pause, never as a normal archive (the P3-3 plan, 3b).
	Problem string
	// DeleteOK says the row offers Delete…, and DeleteWhy, when it does not, why, in §7's
	// words (design-archive-delete §10: from §4's archive test and §7, never from the row's
	// greyed state; MA-975). Unfinished marks a delete of the folder that did not finish
	// (§5.4): the row shows "Delete not finished: click Delete again" in place of Switch and
	// Pause, and Delete is offered.
	DeleteOK   bool
	DeleteWhy  string
	Unfinished bool
	// UnfinishedText, when not "", is said in place of "Delete not finished: click Delete again":
	// a delete only the folder's delete marker knows (archdelete.MarkerRowText; design-archive-delete
	// §15.4), whose person on this computer may never have deleted anything.
	UnfinishedText string
	// Deleting marks a delete of the folder under way in this process (MA-979): the row says
	// "Being deleted" in place of Switch and Pause and offers neither Delete nor Remove from list.
	Deleting bool
	// UnlistWhy, when not "", is why Remove from list would be refused for the row, in §7's words
	// (archdelete.UnlistRefusal: the archive in use while it is reachable and still an archive,
	// the folder MailArchive was started with, another archive's scheduled capture): the button
	// is drawn disabled with these words as its title, as Delete… is (MA-1016; the 1.5.3 friction
	// review's D3), never offered only to be refused.
	UnlistWhy string
	// ShowUnlistWhy: the row also says UnlistWhy as text, after its buttons ("Remove from list is
	// not offered for it now. WHY"), since a touch screen shows no tooltip (the 1.5.3 re-walk's N13,
	// the operator's delegate's decision). Set for the rows of Your archives, not for one whose
	// delete did not finish (its row already says "Delete not finished").
	ShowUnlistWhy bool
	// TBBanner is the card's one line when the archive's last backup found Thunderbird still
	// downloading an account, or folders not kept on this computer (completeness design §7's
	// archive card row, slice C3): state.TBCardBanner over the plaintext last-run record; "" for
	// none, for a backup that did not end ok, and always for an encrypted archive (O1).
	TBBanner string
	// LastNotDone is set when the archive's last backup did not end ok, from the same read of the
	// last-run record as TBBanner, and LastNotDoneOn is its date ("Nov 24"; with its year when not
	// this year; "" when the record holds no time): the row says "Last backup did not finish (DATE):
	// switch to it to see why." (docs/design-per-mailbox-archives.md §10, R1, revision 2.1's F-10b;
	// MA-1505). Not set for a backup that ended ok, for a record that says a backup is running, for no
	// record or one that cannot be read, for the current archive (its health view says why), and
	// never for an encrypted archive (O1).
	LastNotDone   bool
	LastNotDoneOn string
}

// OffersSwitch reports whether the row draws Switch to this archive: as the row template decides
// it (dashboard.go's row-state), not being deleted, its delete finished, no problem, reachable,
// not paused and not the current archive.
func (r ArchiveRow) OffersSwitch() bool {
	return !r.Deleting && !r.Unfinished && r.Problem == "" && r.Available && !r.Sealed && !r.Current
}

// DeleteName is the folder's name the row's Delete… button carries (data-delete-name): the last
// element of its path as the page is drawn, no folder read, so an unplugged drive cannot slow the page
// (docs/design-1.7.5-fixes.md §3.2, MA-1477). It only enables Delete when the check finishes early;
// what decides is archdelete.NameMatches against the plan's own name, on the page and on the server.
func (r ArchiveRow) DeleteName() string { return filepath.Base(r.Path) }

// KindLabel is the short, plain-words form of Kind for the archive list ("" when
// the archive's kind is unknown, e.g. the folder is unavailable).
func (r ArchiveRow) KindLabel() string {
	switch r.Kind {
	case kindLocal:
		return "local mail files"
	case kindM365:
		return "Microsoft 365"
	case kindMixed:
		return "Microsoft 365 + local files"
	case kindEmpty:
		return "empty"
	case kindEncrypted:
		return "encrypted" // MA-731
	}
	return ""
}

// archivePointers are the archive folders the Desktop already knows about from its
// own config, gathered for a reconcile: the archive in use (effectiveOut), the
// wizard's last-touched breadcrumb (Config.LastArchive), the folder a weekly
// backup is named for (Settings.BackupFor) and each folder the stand-alone wizard's
// made-list names (Config.MadeList, MA-1500). archindex reads none of these itself —
// it is handed the list, staying a leaf. Blank and duplicate pointers are harmless
// (Reconcile skips them).
func (cfg Config) archivePointers() []string { return cfg.pointersWith(cfg.madeLines()) }

// pointersWith is archivePointers with the made-list's lines as already read.
func (cfg Config) pointersWith(made []job.Made) []string {
	var ps []string
	add := func(p string) {
		if p = strings.TrimSpace(p); p != "" {
			ps = append(ps, p)
		}
	}
	add(cfg.effectiveOut())
	if cfg.LastArchive != nil {
		add(cfg.LastArchive())
	}
	add(cfg.settings().BackupFor)
	for _, m := range made {
		add(m.Path)
	}
	return ps
}

// madeListPath is the stand-alone wizard's made-list the Desktop reads (job.MadeListFile), "" when
// MailArchive's settings folder cannot be told.
func madeListPath() string {
	p, _ := job.MadeListFile()
	return p
}

// madeLines are the made-list's lines (docs/design-per-mailbox-archives.md §10, MA-1500), none
// when the Desktop has no made-list or it cannot be read: a list that cannot be read adds no row,
// and is never struck.
func (cfg Config) madeLines() []job.Made {
	if strings.TrimSpace(cfg.MadeList) == "" {
		return nil
	}
	ms, err := job.ReadMade(cfg.MadeList)
	if err != nil {
		return nil
	}
	return ms
}

// strikeFolded removes from the made-list, after a reconcile that read or wrote the list of
// archives idx, each line whose folder is now listed in idx and holds an archive, or is listed and
// gone (§10, C10; INS-07: a folder never listed keeps its line, gone or not, until it is listed or
// the 200-line cap drops it): so a row the person removes later stays removed. A line whose folder is there with no
// archive yet (a copy that failed before its first save) is kept for the wizard's re-run (F-02),
// as is one whose folder is not listed (the list was full) and one whose folder cannot be looked at
// now (goneFolder). A strike that cannot be made now (another program holds the made-list) is
// left for the next page load.
func (cfg Config) strikeFolded(idx archindex.Index, made []job.Made) {
	if len(made) == 0 {
		return
	}
	listed := map[string]bool{}
	for _, e := range idx.Entries {
		listed[filepath.Clean(strings.TrimSpace(e.Path))] = true
	}
	var strike []string
	for _, m := range made {
		// A line whose folder is gone is struck only once that folder was listed (the adversarial
		// pass's INS-07): a folder never listed may be on a drive mounted at a folder that stays
		// when the drive is out, and its line must list it when the drive is back. Listed is listed
		// now, or deleted by MailArchive (its delete removed the row before the line could go).
		switch {
		case listed[m.Path] && archindex.IsArchive(m.Path):
			strike = append(strike, m.Path)
		case goneFolder(m.Path) && (listed[m.Path] || archdelete.DeletedHere(cfg.deletionsLogPath(), m.Path, cfg.goos())):
			strike = append(strike, m.Path)
		}
	}
	_ = job.StrikeMade(cfg.MadeList, strike)
}

// goneFolder reports a made-list folder that is gone for good: nothing is at its path while its
// parent folder is there. A folder whose parent is not there either (a drive unplugged, a share not
// connected) is not gone: its line stays until it can be looked at again.
func goneFolder(path string) bool {
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	fi, err := os.Stat(filepath.Dir(path))
	return err == nil && fi.IsDir()
}

// reconcileArchives folds the Desktop's known pointers into the folder-only index
// and returns it. The Desktop is the index's sole maintainer (SD-6): it writes the
// file back ONLY when the reconcile changed the membership, so rendering the list
// does not churn the file (or fsync) on every page load — Reconcile is monotone, so
// a longer entry list is the one signal that a new archive folder was imported. A
// write failure is returned but never loses the in-memory list the caller renders
// from. IndexPath empty (the informational shell, a test that opts out) means no
// index: an empty one is returned and nothing is written.
func (cfg Config) reconcileArchives(now time.Time) (archindex.Index, error) {
	if strings.TrimSpace(cfg.IndexPath) == "" {
		return archindex.Index{Version: archindex.Version}, nil
	}
	before, err := archindex.ReadFile(cfg.IndexPath)
	if err != nil {
		return archindex.Index{Version: archindex.Version}, err
	}
	made := cfg.madeLines()
	pointers := cfg.pointersWith(made)
	after := archindex.Reconcile(before, pointers, now)
	if len(after.Entries) == len(before.Entries) {
		cfg.strikeFolded(after, made)
		return after, nil
	}
	// It adds a row: read again and written under the list's lock, so a row another program
	// added or removed meanwhile is kept as it left it (archindex.UpdateFile, CON-07).
	werr := archindex.UpdateFile(cfg.IndexPath, func(fresh archindex.Index) (archindex.Index, bool, error) {
		n := len(fresh.Entries)
		after = archindex.Reconcile(fresh, pointers, now)
		return after, len(after.Entries) != n, nil
	})
	// The made-list's lines are struck only against a list that is on disk: a row this reconcile
	// could not write keeps its line, so the next page load adds it (MA-1500).
	if werr == nil {
		cfg.strikeFolded(after, made)
	}
	return after, werr
}

// archiveRows turns an index into the list the Desktop shows. The row whose folder
// is the active pointer (effectiveOut) is the only Current one, so changing out
// moves Current with no index write; a row whose folder is unreachable or no longer
// an archive reads Available=false (shown greyed, never dropped — SD-6) with an
// empty Kind; an available row's Kind is derived live by archindex's safe read
// (a bounded streaming walk inside an os.Root, its store ids read lexically as
// state.LoadFrom reads them; MA-1034), never the Desktop's state.Load-based
// archiveKind, which would resolve an untrusted manifest's store ids on disk (SD-1).
func (cfg Config) archiveRows(idx archindex.Index) []ArchiveRow {
	current := cleanPath(cfg.effectiveOut())
	rows := make([]ArchiveRow, 0, len(idx.Entries))
	// The row's busy check is the plan's: the Desktop's own runs and another program's holding
	// the archive lock (del-s3a conformance gap 1).
	deps := cfg.deleteDeps(cfg.rowBusy, nil)
	rc := archdelete.NewRowsCheck(deps)
	for _, e := range idx.Entries {
		avail := archindex.Available(e)
		kind := ""
		if avail {
			kind = archindex.Kind(e.Path)
		}
		problem := ""
		if kind == archindex.KindEncrypted {
			problem = recordedProblem(e.Path)
		}
		lostSince := sourceLostSince(e.Path)
		goneMailbox, lostWords := "", ""
		if lostSince != "" {
			goneMailbox = goneRepairInput(e.Path)
			lostWords = health.SourceReasonWordsAt(e.Path) // archive-local records only (F-03)
		}
		isCurrent := current != "" && cleanPath(e.Path) == current
		tb, notDone, notDoneOn := rowLastRun(e.Path, avail && kind != "" && kind != archindex.KindEncrypted && problem == "", time.Now())
		if isCurrent || lostSince != "" {
			// The current archive's health view says why (§10); a row whose source-loss words are
			// shown already says why, so it is not sent to Switch to find it (the friction review's F7).
			notDone = false
		}
		del := cfg.deleteRowFacts(e.Path, rc, avail && kind != "" && problem == "")
		unlistWhy := ""
		// The route's own refusal (MA-1016), through the page's memo of the file system.
		if err := rc.UnlistRefusal(e.Path); err != nil {
			unlistWhy = refusalText(err)
		}
		rows = append(rows, ArchiveRow{
			UnlistWhy:       unlistWhy,
			DeleteOK:        del.ok,
			DeleteWhy:       del.why,
			Unfinished:      del.unfinished,
			UnfinishedText:  del.unfinishedText,
			Deleting:        del.deleting,
			Path:            e.Path,
			Current:         isCurrent,
			Available:       avail && problem == "",
			Sealed:          seal.IsSealed(e.Path),
			Kind:            kind,
			SourceLostSince: lostSince,
			RunNowOnly:      lostSince != "" && runNowOnlyRecorded(e.Path),
			// Status's own helper, so the banner and status agree (X10, MA-1479), asked within
			// sourceWait (MA-869, OUT-01), naming the missing mailbox (F10).
			SourceLostRepair:  goneMailbox != "",
			SourceLostMailbox: goneMailbox,
			SourceLostWords:   lostWords,
			Problem:          problem,
			TBBanner:         tb,
			LastNotDone:      notDone,
			LastNotDoneOn:    notDoneOn,
		})
	}
	return rows
}

// recordedProblem is, for a folder this computer recorded as an encrypted archive,
// the words of a case in which it no longer holds that archive (§4.5); "" for an
// intact one, a folder not recorded, and a path repair rebuilt that is not proven
// yet. archindex reads an encrypted folder by its markers alone, so an emptied
// recorded folder would otherwise list as a normal archive.
func recordedProblem(path string) string {
	res, err := encmark.At(path)
	if err != nil || !res.Recorded {
		return ""
	}
	switch res.Case {
	case encmark.None, encmark.Unproven:
		return ""
	}
	return res.Why
}

// runNowOnlyRecorded reports whether the archive's schedule record says its backup runs only when
// Run now is clicked (MA-949). It only picks the source-lost banner's words, so the record is read
// as the source streak is — bounded, a regular file only, never through a link or a pipe — and
// anything it cannot read counts as a scheduled backup.
func runNowOnlyRecorded(out string) bool {
	data, err := util.ReadSecureFile(filepath.Join(out, schedule.DescriptorName), "schedule record", 64<<10)
	if err != nil {
		return false
	}
	var d schedule.Descriptor
	return json.Unmarshal(data, &d) == nil && d.IsManual()
}

// rowLastRun reads the last-run record of the archive at out once for its row (state.ReadLastRun:
// bounded, never through a link), for a plaintext archive only (O1). tb is the card's Thunderbird
// line (completeness design §7, slice C3; until step 4 its own function, tbBanner): from its
// last-run record, whose reader cleans every name again, for a plaintext archive whose last backup
// ended ok; an encrypted archive, or one whose kind cannot be told as plaintext, gets none (O1:
// nothing is written beside it, and nothing planted there is shown). notDone says the last backup
// did not end ok
// (docs/design-per-mailbox-archives.md §10, R1; MA-1505), and on is its date, when it ended, else
// when it started, as "Jan 2" (with its year when it is not now's year; "" when the record holds no
// time). A record that says a backup is running gives neither while the archive lock is held
// (revision 2.1's F-10b: one in progress); with the lock free it is a backup that never finished and
// gives the line, dated when it started (CON-04). Only the record's times reach the page; its status
// and error text never do.
func rowLastRun(out string, plaintext bool, now time.Time) (tb string, notDone bool, on string) {
	if !plaintext || encmark.MustMinimise(out) {
		return "", false, ""
	}
	lr, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent || lr.Encrypted {
		return "", false, ""
	}
	switch lr.Status {
	case state.RunOK:
		return state.TBCardBanner(lr.Thunderbird), false, ""
	case state.RunRunning:
		// A backup in progress holds the archive lock; a record that says "running" while the lock
		// is free is a run that never finished (a crash, a power cut: the adversarial pass's
		// CON-04), so it gets the line. A lock that cannot be looked at says nothing.
		if held, err := lockfile.Probe(filepath.Join(out, lockfile.Name)); held || err != nil {
			return "", false, ""
		}
	}
	when := lr.Finished
	if when.IsZero() {
		when = lr.Started
	}
	switch {
	case when.IsZero():
		return "", true, ""
	case when.Local().Year() == now.Local().Year():
		return "", true, when.Local().Format("Jan 2")
	}
	return "", true, when.Local().Format("Jan 2, 2006")
}

// sourceLostSince returns a "since DATE" string when the archive's SOURCE has been unreachable
// past the surface threshold (§5.7, S3-C6), for the Desktop banner; "" when the source is
// reachable or the streak has not yet crossed the threshold. It reads the archive-local streak
// the scheduled run maintains (state.SourceStreak), best-effort.
func sourceLostSince(out string) string {
	s, err := state.ReadSourceStreak(out)
	if err != nil || !s.SurfaceLoudly() {
		return ""
	}
	if s.FirstUnreachable.IsZero() {
		return "recently"
	}
	return s.FirstUnreachable.Format("Jan 2, 2006")
}

// cleanPath normalizes a folder path for identity comparison, matching archindex's
// own cleaning so a Desktop pointer and an index entry for the same folder compare
// equal.
func cleanPath(p string) string {
	if p = strings.TrimSpace(p); p == "" {
		return ""
	}
	return filepath.Clean(p)
}
