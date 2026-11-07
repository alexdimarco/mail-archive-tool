package tbengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbrestore"
)

// Start is what the person said at Restore (design §6.2 notes, §7.3 RT-8): both are
// checked here, on the server, never only in a page.
type Start struct {
	// NotEncryptedOK: the "I understand the restored mail will not be encrypted" tick
	// (an encrypted archive needs it, else W-TICK).
	NotEncryptedOK bool
	// ConfirmAgain: the second question, owed when the plan asks twice.
	ConfirmAgain bool
	// Progress is told how far the write is, at every message (optional).
	Progress func(Progress)
	// Checking is told once when the post-write check begins, every message then read
	// back (optional; R6-10): the command line says "Checking what was written…".
	Checking func()
}

// Progress is how far a run is. Folder is "" for an encrypted archive: counts only
// (§6.2 note, RT-8).
type Progress struct {
	Done, Total int
	Folder      string
}

// Result is what a run did.
type Result struct {
	Write *tbrestore.WriteResult
	// Restored is how many messages Thunderbird gets; NotRestored how many chosen ones it
	// does not (those the plan knew of, and originals found damaged while restoring).
	Restored, NotRestored int
	// Damaged are originals found damaged or unreadable while restoring; DamagedNames the
	// first of them, by the archive's file (none for an encrypted archive).
	Damaged      int
	DamagedNames []string
	// DeletedInTB counts restored messages Thunderbird had marked deleted before they
	// were backed up (X-Mozilla-Status 0x0008, §4.6).
	DeletedInTB int
	// Earlier is what Start did with each earlier restore into the profile.
	Earlier []tbrestore.Settled
	// Words are the result as the person reads it: the write's own words, the counts
	// left out, W-STORE when it applied, then each earlier restore not cleaned up (N-g).
	Words string
}

// Done reports a restore whose folder is in Thunderbird.
func (r *Result) Done() bool { return r.Write != nil && r.Write.Outcome == tbrestore.OutDone }

// Stopped reports a run ended by Cancel, Ctrl-C or Shut down.
func (r *Result) Stopped() bool { return r.Write != nil && r.Write.Outcome == tbrestore.OutStopped }

// ExitCode is the command line's verdict (design §10 as folded; X1): 0 every chosen
// message restored, 3 partial, 1 not restored. A stop is the caller's 130 or 143.
func (r *Result) ExitCode() int {
	switch {
	case r.Done() && r.NotRestored > 0:
		return 3
	case r.Done():
		return 0
	}
	return 1
}

// maxDamagedNames bounds the damaged originals a result names.
const maxDamagedNames = 10

// Run does the plan (design §5.3, §5.4, §7.1, §8 as folded). It takes, in this order,
// the archive's lock (with the verb restore-thunderbird for an unencrypted archive, no
// verb for an encrypted one), the MailArchive restore lock, re-checks the plan, takes
// Thunderbird's profile lock (which checks Thunderbird's process list again), opens Local
// Folders by the identity the plan pinned, settles every earlier restore into the
// profile (Start always goes on, N-g), and writes; and it releases them in the reverse
// order on every path — a result, an error, Cancel, Shut down (ctx) and a panic, which it
// does not recover (the job runner does) but unwinds through its deferred releases.
// An encrypted archive is read only through a read lease taken here, so a Lock after
// this does not stop it (Q6); nothing plaintext is written outside Local Folders.
//
// A refusal before anything is written is the error (a *tbrestore.Refusal); once the
// write has begun the result says how it ended.
func Run(ctx context.Context, p *Plan, s Start) (*Result, error) {
	if err := p.Confirmed(s); err != nil {
		return nil, err
	}
	if elevatedProbe() {
		return nil, refusal("W-ELEVATED", elevatedWords, nil)
	}
	if ctx.Err() != nil {
		return nil, refusal("W-STOPPED", wStopEarly, ctx.Err())
	}
	var sealed app.SealedOriginals
	if p.Encrypted {
		l, err := keyring.LeaseOriginals(p.keys)
		if err != nil {
			return nil, lockedRefusal(err)
		}
		defer l.Destroy()
		sealed = l
	}
	verb := app.RestoreThunderbirdVerb
	if p.Encrypted {
		verb = "" // a verb beside an encrypted archive would say readable mail was copied out
	}
	lp := filepath.Join(p.Out, lockfile.Name)
	if p.ReadOnly {
		// R6-13: a read-only path, read without writing the archive's lock. Settled
		// 2026-11-07 [R7-02]: the path being read-only does not make the folder so (a bind
		// mount, a container, a share another computer writes), so the lock is held through
		// the read-only handle for the whole run, not probed for an instant: a backup by any
		// other route is refused until the restore ends. The read-only check is repeated
		// under it (the re-check). An archive with no lock file (no backup has ever run
		// there) has none to hold, as the plan said.
		if !readOnlyVolume(p.Out) {
			return nil, tbrestore.Changed("the archive's drive (" + p.Out + ") is no longer read-only")
		}
		rol, err := lockfile.HoldReadOnly(lp)
		switch {
		case errors.Is(err, lockfile.ErrHeld):
			return nil, refusal("W-BUSY", wBusy, err)
		case errors.Is(err, os.ErrNotExist) && !p.roLock:
		case errors.Is(err, os.ErrNotExist):
			return nil, tbrestore.Changed("the archive's lock file (" + lp + ") is gone")
		case err != nil:
			return nil, refusal("W-ARCHLOCK", fill(wArchLock, "OUT", p.Out), err)
		default:
			defer rol.Release()
		}
	} else {
		alock, err := lockfile.AcquireAs(lp, verb)
		if err != nil {
			if errors.Is(err, lockfile.ErrHeld) {
				return nil, refusal("W-BUSY", wBusy, err)
			}
			return nil, refusal("W-ARCHLOCK", fill(wArchLock, "OUT", p.Out), err) // R6-13: words, never the raw error
		}
		defer alock.Release()
	}
	rlock, err := tbrestore.AcquireRestoreLock(p.Profile.Dir)
	if err != nil {
		return nil, err
	}
	defer rlock.Release()
	if err := p.recheck(); err != nil {
		return nil, err
	}
	plock, err := tbrestore.TakeProfileLock(p.Profile.Dir)
	if err != nil {
		return nil, err
	}
	defer plock.Release()
	// R6-05: the lock taken is the pinned profile's own, not a decoy's at its path.
	if !plock.Binds(p.Profile.Dir, p.Profile.Identity) {
		return nil, tbrestore.Changed("the Thunderbird profile (" + p.Profile.Dir + ") is not the folder it was")
	}
	// R6-06: every other profile that uses this Local Folders is held out too.
	for _, o := range p.Sharing {
		ol, err := tbrestore.TakeProfileLock(o.Dir)
		if err != nil {
			return nil, err
		}
		defer ol.Release()
		if !ol.Binds(o.Dir, o.Identity) {
			return nil, tbrestore.Changed("the Thunderbird profile (" + o.Dir + ") is not the folder it was")
		}
	}
	root, err := tbrestore.OpenLocalRoot(p.Local)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f := fsFor(root)
	log, err := restoresLog()
	if err != nil {
		return nil, err
	}
	res := &Result{}
	if res.Earlier, err = tbrestore.Settle(tbrestore.SettleInput{Log: log, Local: p.Local, FS: f, ProfileDir: p.Profile.Dir}); err != nil {
		return nil, err
	}
	var man *state.Manifest
	if sealed != nil {
		man, err = sealed.Load()
		if err != nil {
			return nil, lockedRefusal(err)
		}
	} else if man, err = state.Load(filepath.Join(p.Out, ".mailarchive-manifest.json")); err != nil {
		return nil, err
	}
	id, err := tbrestore.NewID()
	if err != nil {
		return nil, err
	}
	folder := ""
	in := tbrestore.WriteInput{
		ID: id, ProfileDir: p.Profile.Dir, Local: p.Local, Archive: p.Out, Encrypted: p.Encrypted,
		Layout: p.Layout, Top: p.Top, Log: log,
		// R6-01: an encrypted archive's files must be shown readable by this account
		// alone, unless the person confirmed a second time that this drive cannot.
		MustBePrivate: p.Encrypted && len(p.Readers) == 0,
		Checking:      s.Checking,
		Messages: func(i int, emit func([]byte) error) error {
			fo := p.Layout.Folders[i]
			if !p.Encrypted {
				folder = strings.Join(fo.Names, "/")
			}
			for _, key := range p.byFolder[namesKey(fo.Names)] {
				rec, ok := man.Get(key)
				if !ok {
					res.Damaged++
					continue
				}
				orig, skip, err := app.FaithfulOriginal(p.Out, rec, sealed)
				if err != nil {
					return err // the key gone mid-run: a hard stop, never a skip
				}
				if skip != nil {
					res.Damaged++
					if !p.Encrypted && len(res.DamagedNames) < maxDamagedNames {
						res.DamagedNames = append(res.DamagedNames, skip.Path)
					}
					continue
				}
				if deletedInThunderbird(orig.Data) {
					res.DeletedInTB++
				}
				if err := emit(orig.Data); err != nil {
					return err
				}
			}
			return nil
		},
		Progress: func(done int) {
			if s.Progress != nil {
				s.Progress(Progress{Done: done, Total: p.Messages, Folder: folder})
			}
		},
	}
	res.Write = tbrestore.Write(ctx, f, in)
	res.Restored = res.Write.Restored
	if !res.Done() {
		res.DeletedInTB = 0
	}
	res.NotRestored = p.notRestored + res.Damaged
	res.Words = p.resultWords(res)
	return res, nil
}

// Confirmed is Start's own first check, on the server (design §6.2 notes, §7.3 RT-8): an
// encrypted plan needs the "will not be encrypted" tick (W-TICK), and a plan that asks twice
// the second yes (W-NOTAGAIN); nil when s says both as the plan needs them. Run makes it
// first; MailArchive Desktop also makes it before it spends the plan's one-use token, so a
// missing tick leaves the plan waiting for it (slice R7). It reads and writes nothing.
func (p *Plan) Confirmed(s Start) error {
	if p.Encrypted && !s.NotEncryptedOK {
		return refusal("W-TICK", wTick, nil)
	}
	if p.AskTwice() && !s.ConfirmAgain {
		if len(p.Readers) == 0 {
			return refusal("W-NOTAGAIN", fill(wNotAgain, "REASONS", strings.Join(p.Leaves, "; ")), nil)
		}
		return refusal("W-NOTAGAIN", fill(wNotAgainReaders, "REASONS", strings.Join(p.AskReasons(), "; ")), nil)
	}
	return nil
}

// recheck is §5.3 as folded, under the archive lock and the restore lock and before
// Thunderbird's lock: Thunderbird not running (A and B), the profile entry the same
// folder with the same identity, Local Folders the same, the name still free on both
// counts, the space still enough, the archive's record unchanged, the Store-install and
// network checks and the ask-twice reasons as planned. Any difference is W-CHANGED;
// nothing is written.
func (p *Plan) recheck() error {
	if tbrestore.CheckRunning(p.Profile.Dir).Running() {
		return refusal("W-RUNNING", tbrestore.RunningWords(), nil)
	}
	now, err := p.profileNow()
	if tbrestore.RefusalCode(err) == "W-PROFCHANGED" {
		return err // the list of profiles changed: choose the profile again (§3.1 step 6 note)
	}
	if err != nil || now.Dir != p.Profile.Dir || !now.Identity.Same(p.Profile.Identity) {
		return tbrestore.Changed("the Thunderbird profile (" + p.Profile.Dir + ") is not the folder it was")
	}
	prefs, err := tbrestore.ReadPrefs(p.Profile.Dir)
	if err != nil {
		return tbrestore.Changed("this profile's settings file (prefs.js) cannot be read")
	}
	lf, err := tbrestore.FindLocalFolders(p.Profile.Dir, prefs)
	if err != nil || lf.Dir != p.Local.Dir || !lf.Identity.Same(p.Local.Identity) {
		return tbrestore.Changed("Thunderbird's Local Folders (" + p.Local.Dir + ") is not the folder it was")
	}
	// R6-11: the store kind and the inside-an-encrypted-archive test, as the plan made them.
	if tbrestore.CheckStoreKind(prefs, lf) != nil {
		return tbrestore.Changed("Thunderbird's Local Folders (" + p.Local.Dir + ") now keeps each message in its own file (the 'maildir' setting)")
	}
	if app.RefuseKeyless("restore into it", "", p.Local.Dir) != nil {
		return tbrestore.Changed("Thunderbird's Local Folders (" + p.Local.Dir + ") is now inside an encrypted archive")
	}
	// R6-06: the profiles that share this Local Folders are the ones the plan named.
	if sh, err := sharingProfiles(p.choice, p.Profile, p.Local); err != nil {
		return err
	} else if !sameProfiles(sh, p.Sharing) {
		return tbrestore.Changed("which other Thunderbird profiles use this Local Folders")
	}
	if readOnlyVolume(p.Out) != p.ReadOnly {
		return tbrestore.Changed("whether the archive's drive (" + p.Out + ") is read-only")
	}
	entries, err := os.ReadDir(p.Local.Dir)
	if err != nil {
		return tbrestore.Changed("Thunderbird's Local Folders (" + p.Local.Dir + ") cannot be read")
	}
	if top, _, err := tbrestore.FreeTopName(p.Layout.Top, entries); err != nil || top != p.Top {
		return tbrestore.Changed("the name '" + p.Top + "' is now taken in Local Folders")
	}
	if free, err := freeBytes(p.Local.Dir); err != nil || int64(free) < p.KeepFree {
		return tbrestore.Changed("there is no longer enough free space for it in " + p.Local.Dir)
	}
	if st, err := stampOf(p.Out, p.Encrypted); err != nil || st != p.stamp {
		return tbrestore.Changed("the archive changed (a backup may have run)")
	}
	if storeWarning() != p.Store {
		return tbrestore.Changed("whether a Thunderbird from the Microsoft Store is on this computer")
	}
	if networkWarning(lf) != p.Network {
		return tbrestore.Changed("whether Thunderbird's Local Folders is on a network drive")
	}
	if p.Encrypted {
		for _, r := range leaveEnv.LeaveReasons(p.Local.Dir) {
			if !contains(p.leaveCodes, r.Code) {
				return tbrestore.Changed(leaveWords(r.Code, r.Service, p.Local.Dir))
			}
		}
		for _, r := range readerReasons(lf) {
			if !contains(p.Readers, r) {
				return tbrestore.Changed(r)
			}
		}
	}
	return nil
}

// sameProfiles reports two lists of the same profile folders, by path and identity.
func sameProfiles(a, b []tbrestore.Profile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Dir != b[i].Dir || !a[i].Identity.Same(b[i].Identity) {
			return false
		}
	}
	return true
}

// profileNow is the profile the plan chose, as profiles.ini (or the typed folder) says
// now.
func (p *Plan) profileNow() (tbrestore.Profile, error) {
	c := p.choice
	switch {
	case c.Dir != "":
		return tbrestore.CheckProfile(c.Dir)
	case c.ListID != "":
		bases := c.Bases
		if bases == nil {
			bases = profileBases()
		}
		list, err := tbrestore.ReadProfiles(bases)
		if err != nil {
			return tbrestore.Profile{}, err
		}
		return list.Resolve(c.ListID, c.Position)
	}
	return tbrestore.CheckProfile(p.Profile.Dir)
}

// resultWords are the result as the person reads it (§7.5 Done and its folds, N-g).
func (p *Plan) resultWords(r *Result) string {
	w := r.Write.Words
	if r.Done() {
		if r.NotRestored > 0 {
			w += " " + plural(r.NotRestored, "chosen message") + " did not go back"
			if r.Damaged > 0 {
				w += fmt.Sprintf(" (%s whose original turned out damaged or unreadable", plural(r.Damaged, "message"))
				if len(r.DamagedNames) > 0 {
					w += ": " + strings.Join(r.DamagedNames, ", ")
				}
				w += "; run Maintenance → Check integrity)"
			}
			w += "; the plan said why for the rest."
		}
		if r.DeletedInTB > 0 {
			w += " " + fill(wDeletedInTB, "N", commas(r.DeletedInTB))
		}
		if p.m365 {
			w += " " + wM365Unread
		}
		if p.Store != "" {
			w += " " + p.Store
		}
	}
	for _, s := range r.Earlier {
		if s.Words == "" {
			continue
		}
		w += " " + s.Words
		if s.Case == "0" {
			w += wWentAhead
		}
	}
	return w
}

// deletedInThunderbird reports a message whose own X-Mozilla-Status header has the
// deleted bit (0x0008): Thunderbird keeps it hidden (§4.6). Only the header block is
// read, which is already in memory.
func deletedInThunderbird(raw []byte) bool {
	head := raw
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		head = raw[:i]
	}
	if i := bytes.Index(head, []byte("\r\n\r\n")); i >= 0 {
		head = head[:i]
	}
	for _, line := range bytes.Split(head, []byte("\n")) {
		k, v, ok := bytes.Cut(bytes.TrimRight(line, "\r"), []byte(":"))
		if !ok || !strings.EqualFold(string(k), "X-Mozilla-Status") {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(string(v)), 16, 32)
		return err == nil && n&0x0008 != 0
	}
	return false
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
