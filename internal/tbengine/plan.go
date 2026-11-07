// Package tbengine is Restore to Thunderbird's engine (docs/design-restore-thunderbird.md
// §10, §13 slice R6): Plan, which writes nothing and says what a restore would do, and
// Run, which does exactly that plan under its locks. The command line's
// `restore-thunderbird` and MailArchive Desktop (slice R7) both call it, as delete's two
// surfaces call one engine.
//
// It sits above internal/app (the archive's read side and its lock) and
// internal/tbrestore (the Thunderbird side and the writer): tbrestore may not import app,
// because app's backup skip imports tbrestore (design §13, the R3 builder's note). An
// encrypted archive is read through internal/keyring's read lease, never ExtractSealed.
package tbengine

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbrestore"
	"mail-archive-tool/internal/thunderbird"
	"mail-archive-tool/internal/util"
)

// Seams: the OS probes a test stands in for (CLAUDE.md: mocks only for external probes).
var (
	elevatedProbe  = elevated
	storeWarning   = tbrestore.StoreWarning
	networkWarning = tbrestore.NetworkWarning
	freeBytes      = util.FreeBytes
	leaveEnv       = app.NewCopyExportEnv("", "", "")
	// fsFor is the writer's file system on the Local Folders root; a test wraps it to
	// inject a fault or a crash at any step.
	fsFor = tbrestore.NewRootFS
	// fsTypeOf is the Local Folders volume's file system as its identity read it; a test
	// stands in a FAT32 drive, and lowers fat32Limit, since neither can be made here.
	fsTypeOf   = func(lf tbrestore.LocalFolders) string { return lf.Identity.FSType }
	fat32Limit = int64(fat32Max)
)

// The reason codes app.LeaveReasons gives.
const (
	leavesSynced    = app.LeavesSynced
	leavesMayICloud = app.LeavesMayICloud
	leavesRemovable = app.LeavesRemovable
	leavesShare     = app.LeavesShare
)

// The space a restore keeps (design §6.3 as folded, RT-22): what it writes is the
// originals plus framing per message; free space must be at least that × 1.30 + 512 MiB.
const (
	framingPerMessage = 120
	marginFixed       = 512 << 20
	// fat32Max is where a FAT32 Local Folders refuses a folder's file (§4.7 RT-20).
	fat32Max = 4<<30 - 1<<20
)

// Required is the free space a restore of need bytes asks for.
func Required(need int64) int64 { return need*13/10 + marginFixed }

// ProfileChoice is how the person named the Thunderbird profile: exactly one of a folder
// (-profile FOLDER, typed), a name (-profile-name NAME), or a position in a list the
// server sent (Desktop); none asks the engine to choose the default (§3.1 step 4).
type ProfileChoice struct {
	Dir      string
	Name     string
	ListID   string
	Position int
	// Bases are the folders profiles.ini is read from (nil: this computer's,
	// tbrestore.ProfileBases). Tests pass their own fake profiles' folders.
	Bases []string
}

// Request is what the person chose.
type Request struct {
	// Out is the archive folder.
	Out string
	// Keys is an encrypted archive's open (nil for a plain archive): the plan and the run
	// read it through read leases of their own.
	Keys    *keyring.Unlocked
	Profile ProfileChoice
	// Folders are tree node ids (Desktop); FolderPaths are folders as the tree spells
	// them ("Inbox/Receipts", the command line's -folder), with Store when two parts of
	// the archive have one of that name. None of either: the whole archive.
	Folders     []string
	FolderPaths []string
	Store       string
	// Name replaces the archive folder's name after "Restored — " (-name).
	Name string
	// Inputs are the archive's job inputs: whether one lies in the profile decides
	// W-RECAPTURE and the default profile (ArchiveInputs gives them).
	Inputs []string
}

// Line is one line of the plan: its words and the id of what it says ("W-RECAPTURE",
// "DEST", …), so the order can be checked.
type Line struct {
	Code  string
	Words string
}

// Plan is what a restore will do, decided before anything is written (design §6.4).
type Plan struct {
	Out       string
	Encrypted bool
	Profile   tbrestore.Profile
	Local     tbrestore.LocalFolders
	Layout    *tbrestore.Layout
	// Top is the free parent name: "Restored — NAME" or "… (n)".
	Top       string
	Selection *app.RestoreSelection
	// Messages are the restorable messages the layout writes (LongPath folders aside).
	Messages int
	// NeedBytes is what the restore writes; KeepFree what it asks to be free.
	NeedBytes, KeepFree int64
	FreeBytes           uint64
	// Leaves are why the Local Folders may leave this computer (an encrypted archive's
	// ask-twice reasons, worded).
	Leaves []string
	// Earlier is what Start will do with each earlier restore into the profile, predicted.
	Earlier []tbrestore.Settled
	// Store and Network are W-STORE and W-NETWORK as planned ("" when they do not apply).
	Store, Network string
	Lines          []Line

	keys        *keyring.Unlocked
	choice      ProfileChoice
	byFolder    map[string][]string // record keys per layout folder (namesKey)
	m365        bool
	stamp       manifestStamp
	leaveCodes  []string
	notRestored int // chosen records the plan already knows will not go back
}

// AskTwice reports whether Start needs ConfirmAgain (design §6.2 note, O2).
func (p *Plan) AskTwice() bool { return len(p.Leaves) > 0 }

// Text is the plan as the person reads it, one line each.
func (p *Plan) Text() string {
	var b strings.Builder
	for _, l := range p.Lines {
		b.WriteString(l.Words)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// Codes are the plan's line ids, in order.
func (p *Plan) Codes() []string {
	out := make([]string, len(p.Lines))
	for i, l := range p.Lines {
		out[i] = l.Code
	}
	return out
}

// MakePlan plans a restore and writes nothing anywhere (design §6.4, §8 "the plan
// predicts, Start decides"): every refusal of §7.5 that can be known before writing is
// given here, as a *tbrestore.Refusal, and earlier restores into the profile are settled
// read-only (tbrestore.Settle with ReadOnly).
func MakePlan(req Request) (*Plan, error) {
	if elevatedProbe() {
		return nil, refusal("W-ELEVATED", elevatedWords, nil)
	}
	out, err := filepath.Abs(req.Out)
	if err != nil || strings.TrimSpace(req.Out) == "" {
		return nil, refusal("W-NOARCHIVE", fill(wNoArchive, "OUT", req.Out, "WHY", "no folder was named"), err)
	}
	out = filepath.Clean(out)
	if fi, err := os.Stat(out); err != nil || !fi.IsDir() {
		return nil, refusal("W-NOARCHIVE", fill(wNoArchive, "OUT", out, "WHY", "it is not a folder"), err)
	}
	enc := req.Keys != nil
	if enc {
		if filepath.Clean(req.Keys.Out()) != out {
			return nil, fmt.Errorf("tbengine: the key given is %s's, not %s's", req.Keys.Out(), out)
		}
	} else if kerr := app.RefuseKeyless("restore from it", out); kerr != nil {
		if app.UnlockableAt(out) {
			return nil, refusal("W-LOCKED", wLocked, kerr)
		}
		return nil, kerr
	}
	if lockfile.Held(filepath.Join(out, lockfile.Name)) {
		return nil, refusal("W-BUSY", wBusy, lockfile.ErrHeld)
	}
	p := &Plan{Out: out, Encrypted: enc, keys: req.Keys, choice: req.Profile}

	// The Thunderbird side (§3.1–§3.3, §5.1).
	if p.Profile, err = resolveProfile(req.Profile, req.Inputs); err != nil {
		return nil, err
	}
	if tbrestore.RestoreLockHeld(p.Profile.Dir) {
		return nil, tbrestore.RestoringRefusal()
	}
	if tbrestore.CheckRunning(p.Profile.Dir).Running() {
		return nil, refusal("W-RUNNING", tbrestore.RunningWords(), nil)
	}
	prefs, err := tbrestore.ReadPrefs(p.Profile.Dir)
	if err != nil {
		return nil, err
	}
	if p.Local, err = tbrestore.FindLocalFolders(p.Profile.Dir, prefs); err != nil {
		return nil, err
	}
	if err := tbrestore.CheckArchiveOverlap(p.Local, out); err != nil {
		return nil, err
	}
	if kerr := app.RefuseKeyless("restore into it", "", p.Local.Dir); kerr != nil {
		return nil, refusal("W-INSIDEENC", fill(wInsideEnc, "PATH", p.Local.Dir), kerr)
	}
	if err := tbrestore.CheckStoreKind(prefs, p.Local); err != nil {
		return nil, err
	}
	p.Store, p.Network = storeWarning(), networkWarning(p.Local)

	// Earlier restores into the profile, predicted (W-LOGBAD refuses here).
	log, err := restoresLog()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(p.Local.Dir)
	if err != nil {
		return nil, refusal("W-LOCALGONE", "Thunderbird's Local Folders for this profile ("+p.Local.Dir+") could not be opened, so nothing was restored. Open Thunderbird once with this profile and close it, then click Check again.", err)
	}
	p.Earlier, err = tbrestore.Settle(tbrestore.SettleInput{Log: log, Local: p.Local, FS: tbrestore.NewRootFS(root), ProfileDir: p.Profile.Dir, ReadOnly: true})
	root.Close()
	if err != nil {
		return nil, err
	}

	// The archive side (§6.1, §6.5).
	if p.stamp, err = stampOf(out, enc); err != nil {
		return nil, refusal("W-NOARCHIVE", fill(wNoArchive, "OUT", out, "WHY", "its record cannot be read"), err)
	}
	man, err := loadManifest(out, req.Keys)
	if err != nil {
		return nil, err
	}
	tree, err := app.NewArchiveTree(man)
	if err != nil {
		return nil, err
	}
	ids := req.Folders
	if len(req.FolderPaths) > 0 {
		if ids, err = folderIDs(tree, req.FolderPaths, req.Store); err != nil {
			return nil, err
		}
	}
	sel, err := app.SelectForRestore(out, tree, ids, enc)
	if err != nil {
		return nil, selectRefusal(err)
	}
	p.Selection = sel
	if sel.Restorable == 0 {
		if sel.Records > 0 && sel.PST == sel.Records {
			return nil, refusal("W-PST", wPST, nil)
		}
		return nil, refusal("W-NORAW", wNoRaw, nil)
	}
	for _, s := range tree.Stores() {
		if s.Mailbox != "" {
			p.m365 = true
		}
	}

	// The layout, every name final (§4.4, §4.5), and the free top name (§3.5).
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = filepath.Base(out)
	}
	var ins []tbrestore.FolderIn
	p.byFolder = map[string][]string{}
	for _, f := range sel.Folders {
		ins = append(ins, tbrestore.FolderIn{Names: f.Names, Messages: len(f.Keys), Bytes: f.Bytes})
		p.byFolder[namesKey(f.Names)] = f.Keys
	}
	if p.Layout, err = tbrestore.PlanLayout(name, ins, p.Local.Dir, runtime.GOOS); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p.Local.Dir)
	if err != nil {
		return nil, refusal("W-LOCALGONE", "Thunderbird's Local Folders for this profile ("+p.Local.Dir+") could not be read, so nothing was restored. Open Thunderbird once with this profile and close it, then click Check again.", err)
	}
	if p.Top, _, err = tbrestore.FreeTopName(p.Layout.Top, entries); err != nil {
		return nil, err
	}
	var longNames []string
	for _, f := range p.Layout.Folders {
		if f.LongPath {
			longNames = append(longNames, strings.Join(f.Names, "/"))
			p.notRestored += f.Messages
			continue
		}
		p.Messages += f.Messages
		size := f.Bytes + int64(f.Messages)*framingPerMessage
		p.NeedBytes += size
		if isFAT32(fsTypeOf(p.Local)) && size >= fat32Limit {
			return nil, refusal("W-FAT32", fill(wFAT32, "FOLDER", strings.Join(f.Names, "/"), "SIZE", thunderbird.HumanBytes(size)), nil)
		}
	}
	p.notRestored += sel.Records - sel.Restorable

	// Space (§6.3 as folded).
	p.KeepFree = Required(p.NeedBytes)
	free, ferr := freeBytes(p.Local.Dir)
	if ferr != nil {
		return nil, refusal("W-SPACE", fill(wSpaceUnknown, "DRIVE", p.Local.Dir, "WHY", ferr.Error()), ferr)
	}
	p.FreeBytes = free
	if int64(free) < p.KeepFree {
		return nil, refusal("W-SPACE", fill(wSpace, "DRIVE", p.Local.Dir, "X", thunderbird.HumanBytes(p.KeepFree), "Y", thunderbird.HumanBytes(int64(free))), nil)
	}

	// Ask twice (an encrypted archive) when the Local Folders may leave this computer.
	if enc {
		for _, r := range leaveEnv.LeaveReasons(p.Local.Dir) {
			p.leaveCodes = append(p.leaveCodes, r.Code)
			p.Leaves = append(p.Leaves, leaveWords(r.Code, r.Service, p.Local.Dir))
		}
	}
	p.Lines = p.lines(req.Inputs, longNames)
	return p, nil
}

// lines are the plan in the order the person reads it (§6.4 as folded): W-RECAPTURE
// first when the archive backs up the profile (RT-10), then W-STORE (RT-11: the first
// warning; the plan's first line when there is no W-RECAPTURE), the destination, what
// goes back, what does not, earlier restores, the space, Thunderbird, the warnings that
// apply, the scheduled-backup line, the check, and W-WAIT always.
func (p *Plan) lines(inputs, longNames []string) []Line {
	var ls []Line
	add := func(code, words string) {
		if words != "" {
			ls = append(ls, Line{code, words})
		}
	}
	sel := p.Selection
	if tbrestore.BacksUp(inputs, p.Profile.Dir) {
		add("W-RECAPTURE", wRecapture)
	}
	add("W-STORE", p.Store)
	who := "the Thunderbird profile"
	if p.Profile.Name != "" {
		who += " '" + p.Profile.Name + "'"
	}
	add("DEST", fmt.Sprintf("Restore into %s (%s): its Local Folders, %s, gets the new folder '%s'.", who, p.Profile.Dir, p.Local.Dir, p.Top))
	folders := len(p.Layout.Restored())
	add("WHAT", fmt.Sprintf("%s in %s, about %s, go back as the archive kept them.", plural(p.Messages, "message"), plural(folders, "folder"), thunderbird.HumanBytes(p.NeedBytes)))
	if sel.PST > 0 {
		add("W-PST", fmt.Sprintf("%s came from Outlook data files, which keep no original messages, so %s cannot go back into Thunderbird.", plural(sel.PST, "message"), verb(sel.PST, "it", "they")))
	}
	if sel.NoOriginal > 0 {
		add("W-NORAW", fmt.Sprintf("%s %s no original message kept in the archive, so %s cannot go back into Thunderbird. Turn on keeping originals in Settings so that future backups can be restored; if the originals should be there, run Maintenance → Check integrity.", plural(sel.NoOriginal, "message"), verb(sel.NoOriginal, "has", "have"), verb(sel.NoOriginal, "it", "they")))
	}
	if sel.TooBig > 0 {
		add("W-TOOBIG", fmt.Sprintf("%s %s larger than 2 GB, so %s not restored.", plural(sel.TooBig, "message"), verb(sel.TooBig, "is", "are"), verb(sel.TooBig, "it is", "they are")))
	}
	if len(longNames) > 0 {
		add("W-LONGPATH", fill("N folders are nested too deep for Windows to hold under this profile, so they are not restored: FOLDERS.", "N", itoa(len(longNames)), "FOLDERS", strings.Join(longNames, ", ")))
	}
	if sel.NoFixity > 0 {
		add("NOFIXITY", fmt.Sprintf("%s %s no recorded checksum in the archive, so %s restored as stored.", plural(sel.NoFixity, "message"), verb(sel.NoFixity, "has", "have"), verb(sel.NoFixity, "it is", "they are")))
	}
	add("DAMAGED-LATER", wDamagedLater)
	if p.Layout.Changed > 0 {
		add("NAMES", fmt.Sprintf("%s had to be changed to be safe as file names; Thunderbird shows the changed names.", plural(p.Layout.Changed, "folder name")))
	}
	for _, s := range p.Earlier {
		if s.Words == "" {
			continue
		}
		w := s.Words
		if s.Case == "0" {
			w += wWillGoAhead
		}
		add("EARLIER", w)
	}
	add("SPACE", fmt.Sprintf("Space: about %s is written to %s, which has %s free. Thunderbird will use more space as it indexes the restored mail; about %s is kept free for that.", thunderbird.HumanBytes(p.NeedBytes), p.Local.Dir, thunderbird.HumanBytes(int64(p.FreeBytes)), thunderbird.HumanBytes(p.KeepFree-p.NeedBytes)))
	add("CLOSED", wClosed)
	add("W-NETWORK", p.Network)
	if p.Encrypted {
		add("NOT-ENCRYPTED", wNotEncrypted)
		for _, l := range p.Leaves {
			add("W-LEAVES", "It may leave this computer: "+l+". You are asked a second time before it starts.")
		}
	} else if svc, ok := util.UnderCloudSync(p.Local.Dir); ok {
		add("W-CLOUD", fill(wCloud, "SERVICE", svc))
	}
	add("W-OLDTB", tbrestore.OldThunderbirdWarning(p.Profile.Dir))
	if allMailWithOthers(sel.Folders) {
		add("ALLMAIL", wAllMail)
	}
	if sel.KeepsTimeline {
		if sel.Deleted == 0 {
			add("DELETED", "None of these messages were deleted from the mailbox after they were backed up.")
		} else {
			add("DELETED", fmt.Sprintf("%s of these messages were deleted from the mailbox after they were backed up; they are restored too.", commas(sel.Deleted)))
		}
	}
	add("SCHEDULED", wScheduled)
	add("CHECKS", wChecks)
	add("W-WAIT", tbrestore.WaitWords())
	return ls
}

// selectRefusal words the read side's refusal: over a cap is W-TOOMANY (§6.5 RT-7, L1),
// naming the count, or the depth over the level cap.
func selectRefusal(err error) error {
	var tm *app.RestoreTooMany
	if errors.As(err, &tm) {
		if tm.Depth > 0 {
			return refusal("W-TOOMANY", fill(wTooDeep, "N", itoa(tm.Depth)), err)
		}
		return refusal("W-TOOMANY", fill(wTooMany, "N", commas(tm.Folders)), err)
	}
	return err
}

// allMailWithOthers: Gmail's All Mail is chosen beside other folders (§6.5, Q4).
func allMailWithOthers(fs []app.RestoreFolder) bool {
	all, other := false, false
	for _, f := range fs {
		if len(f.Keys) == 0 {
			continue
		}
		if isAllMail(f.Names) {
			all = true
		} else {
			other = true
		}
	}
	return all && other
}

func isAllMail(names []string) bool {
	for i := 0; i+1 < len(names); i++ {
		g := strings.ToLower(names[i])
		if (g == "[gmail]" || g == "[google mail]") && strings.EqualFold(names[i+1], "All Mail") {
			return true
		}
	}
	return false
}

func isFAT32(fsType string) bool {
	switch strings.ToLower(fsType) {
	case "fat32", "fat", "msdos", "vfat":
		return true
	}
	return false
}

// resolveProfile is §3.1: a typed folder through the profile check, a name, a list
// position, or the default.
func resolveProfile(c ProfileChoice, inputs []string) (tbrestore.Profile, error) {
	if c.Dir != "" {
		return tbrestore.CheckProfile(c.Dir)
	}
	bases := c.Bases
	if bases == nil {
		bases = tbrestore.ProfileBases()
	}
	list, err := tbrestore.ReadProfiles(bases)
	if err != nil {
		return tbrestore.Profile{}, err
	}
	if c.ListID != "" {
		return list.Resolve(c.ListID, c.Position)
	}
	if len(list.Profiles) == 0 {
		return tbrestore.Profile{}, tbrestore.NoProfileRefusal()
	}
	if c.Name != "" {
		var hit []tbrestore.Profile
		for _, p := range list.Profiles {
			if p.Name == c.Name {
				hit = append(hit, p)
			}
		}
		switch {
		case len(hit) == 0:
			return tbrestore.Profile{}, refusal("W-NOPROFNAME", fill(wNoProfName, "NAME", c.Name), nil)
		case len(hit) > 1:
			return tbrestore.Profile{}, refusal("W-PROFNAMES", fill(wProfNames, "NAME", c.Name), nil)
		case !hit[0].Usable:
			return tbrestore.Profile{}, refusal("W-PROFMODE", hit[0].Unusable, nil)
		}
		return hit[0], nil
	}
	source := ""
	for _, p := range list.Profiles {
		if source == "" && tbrestore.BacksUp(inputs, p.Dir) {
			source = p.Dir
		}
	}
	pos, ok := list.Default(func(dir string) bool { return tbrestore.BacksUp(inputs, dir) }, source)
	if !ok {
		if len(list.Usable()) == 0 {
			return tbrestore.Profile{}, refusal("W-PROFMODE", list.Profiles[0].Unusable, nil)
		}
		var names []string
		for _, i := range list.Usable() {
			names = append(names, "'"+list.Profiles[i].Name+"'")
		}
		return tbrestore.Profile{}, refusal("W-CHOOSE", fill(wChooseProf, "NAMES", strings.Join(names, ", ")), nil)
	}
	return list.Profiles[pos], nil
}

// folderIDs maps folders as the tree spells them to node ids (-folder, -store).
func folderIDs(tree *app.ArchiveTree, paths []string, store string) ([]string, error) {
	labels := map[string]string{}
	for _, s := range tree.Stores() {
		labels[s.Token] = s.Label()
	}
	var ids []string
	for _, want := range paths {
		want = strings.Trim(strings.TrimSpace(want), "/")
		var hits []string
		var stores []string
		for _, n := range tree.Tree() {
			if n.Store {
				continue
			}
			tok, folder, ok := tree.Locate(n.ID)
			if !ok || folder != want {
				continue
			}
			if store != "" && store != tok && store != labels[tok] {
				continue
			}
			hits = append(hits, n.ID)
			stores = append(stores, "'"+labels[tok]+"'")
		}
		switch {
		case len(hits) == 0:
			return nil, refusal("W-NOFOLDER", fill(wNoFolder, "FOLDER", want), nil)
		case len(hits) > 1:
			return nil, refusal("W-TWOSTORES", fill(wTwoStores, "FOLDER", want, "STORES", strings.Join(stores, ", ")), nil)
		}
		ids = append(ids, hits[0])
	}
	return ids, nil
}

// loadManifest reads the archive's record: a plain archive's manifest, or an encrypted
// one's through a read lease of its own (destroyed before it returns).
func loadManifest(out string, keys *keyring.Unlocked) (*state.Manifest, error) {
	if keys == nil {
		mpath := filepath.Join(out, ".mailarchive-manifest.json")
		if _, err := os.Stat(mpath); errors.Is(err, fs.ErrNotExist) {
			return nil, refusal("W-NOARCHIVE", fill(wNoArchive, "OUT", out, "WHY", "it holds no archive's record"), err)
		}
		return state.Load(mpath)
	}
	l, err := keyring.LeaseOriginals(keys)
	if err != nil {
		return nil, lockedRefusal(err)
	}
	defer l.Destroy()
	m, err := l.Load()
	if err != nil {
		return nil, lockedRefusal(err)
	}
	return m, nil
}

// lockedRefusal is W-LOCKED for an archive locked before the restore could read it.
func lockedRefusal(err error) error {
	if errors.Is(err, keyring.ErrLocked) || errors.Is(err, app.ErrEncryptedNeedsKey) {
		return refusal("W-LOCKED", wLocked, err)
	}
	return err
}

// manifestStamp pins the archive's record between the plan and the start (§5.3, as
// Delete's plan does): size, modification time and SHA-256 of the manifest (a plain
// archive) or of the record pointer a backup rewrites (an encrypted archive).
type manifestStamp struct {
	size int64
	mod  time.Time
	sum  [32]byte
}

func stampOf(out string, enc bool) (manifestStamp, error) {
	name := ".mailarchive-manifest.json"
	if enc {
		name = container.PointerName
	}
	p := filepath.Join(out, name)
	fi, err := os.Lstat(p)
	if err != nil {
		return manifestStamp{}, err
	}
	if !fi.Mode().IsRegular() {
		return manifestStamp{}, fmt.Errorf("%s is not an ordinary file", p)
	}
	f, err := os.Open(p)
	if err != nil {
		return manifestStamp{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return manifestStamp{}, err
	}
	s := manifestStamp{size: fi.Size(), mod: fi.ModTime()}
	copy(s.sum[:], h.Sum(nil))
	return s, nil
}

func restoresLog() (*tbrestore.Log, error) {
	p, err := tbrestore.LogPath()
	if err != nil {
		return nil, err
	}
	return tbrestore.NewLog(p), nil
}

// namesKey is a folder path's map key; each part is length-prefixed.
func namesKey(names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString(itoa(len(n)))
		b.WriteByte(':')
		b.WriteString(n)
	}
	return b.String()
}

func itoa(n int) string { return fmt.Sprint(n) }

// commas is n with thousands separators ("4,310").
func commas(n int) string {
	s := itoa(n)
	if n < 0 {
		return s
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// verb is one when n is 1, else many.
func verb(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return commas(n) + " " + noun + "s"
}
