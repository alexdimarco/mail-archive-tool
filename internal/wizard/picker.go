package wizard

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
)

// The list of mailboxes auto-detect found (S85, MA-1462 to MA-1467). Its first row is "All of
// them"; then one row per mailbox, said the way the person knows it, with how much of it is on this
// computer: "sam.walker@gmail.com (14 GB)", never the folder Thunderbird keeps it in
// ("imap.gmail-1.com" says nothing about which address it holds). The words are shown and never
// written: the job file keeps the chosen paths, or auto-discovery (MA-1288's rule).
const (
	// allOfThem is the first row, preselected on Windows and macOS: every mailbox found, and any
	// added later, since auto-discovery is what is remembered.
	allOfThem = "➤ All of them (and any added later)"
	// pickPrompt is the list's text: fixed, plain words with none of & < > \ (MA-1286), and one
	// short line, as Windows' list shows only one.
	pickPrompt = "Which mailbox do you want to export?"
	// pickAgain is the note shown when the list's answer names no row (on Linux the list can be
	// answered with nothing selected).
	pickAgain = "Nothing was chosen. Choose one line in the list, then click OK."
	// notInThunderbird starts the row of a Thunderbird folder no account uses now (MA-1463).
	notInThunderbird = "Not in Thunderbird now: "
	// pickTries is how many times the list is shown before the wizard stops, when its answer
	// keeps naming no row (a dialog program that answers nothing, never a person clicking).
	pickTries = 10
	// sizeBudget is how long counting the sizes of all the mailboxes found may take before the
	// list is shown; a count still going then says "over" what it reached, and the mailboxes not
	// yet counted show no size (MA-1466).
	sizeBudget = 3 * time.Second
	// sizeEntryMax bounds the files and folders one mailbox's count visits.
	sizeEntryMax = 200_000
)

// allOfThemBare is the All row's words without its arrow, which a dialog program might drop: an
// answer of them is the All row too.
var allOfThemBare = strings.TrimPrefix(allOfThem, "➤ ")

// errNoPick ends the wizard when the list keeps answering with no row: nothing is exported.
var errNoPick = errors.New("The list of mailboxes did not say which one was chosen, so nothing was exported. Start MailArchive again and choose one line in the list.")

// pickAutoInputs lets the person choose one of the auto-detected mailboxes, or all of them. "All
// of them" is remembered as auto-discovery, so a scheduled run picks up accounts added later.
//
// Each row says its mailbox as pickList words it. The answer maps back to its row (pickChoice);
// an answer that names no row shows a note and the list again, and is never taken as a path.
func pickAutoInputs(found []string) ([]string, bool, error) {
	if len(found) == 1 {
		return found, false, nil
	}
	rows := gatherPickRows(found, time.Now().Add(sizeBudget), sizeEntryMax)
	labels, paths := pickList(rows, runtime.GOOS)
	items := append([]string{allOfThem}, labels...)
	for try := 1; ; try++ {
		answer, err := listDialog(pickPrompt, items, zenity.Title(appTitle), zenity.DefaultItems(allOfThem), zenity.DisallowEmpty())
		if err != nil {
			return nil, false, err
		}
		if i, all, ok := pickChoice(answer, labels); ok && all {
			return found, true, nil
		} else if ok {
			return []string{paths[i]}, false, nil
		}
		if try >= pickTries {
			return nil, false, errNoPick
		}
		// Closing the note with its close button shows the list again too, as OK does (MA-1209's
		// rule for a one-button notice); any other error stops.
		if err := noticeClosed(messageDialog(pickAgain, zenity.Title(appTitle))); err != nil {
			return nil, false, err
		}
	}
}

// identKind is what a row says its mailbox by, for its lead word (needsLead).
type identKind int

const (
	identName   identKind = iota // the account's name, as its mail program shows it
	identFolder                  // the folder's own name, or another mail program's store name
	identFile                    // the file's own name
)

// leadWord starts a row whose words could read as something else (needsLead).
var leadWord = map[identKind]string{identName: "Account ", identFolder: "Folder ", identFile: "File "}

// pickGroup orders the list: Outlook's files, then Thunderbird's mail accounts, then its Local
// Folders and feeds, then other mail programs' stores, then Thunderbird folders no account uses now.
type pickGroup int

const (
	groupOutlook pickGroup = iota // an Outlook data file (.pst or .ost)
	// groupTBMail: a Thunderbird mail account, or a Thunderbird folder whose account cannot be told
	// for certain (a settings file read without certainty, two accounts that could own it, a limit
	// of a type Thunderbird never writes).
	groupTBMail
	groupTBLocal // a Thunderbird account of type none or rss: Local Folders, feeds, unified folders
	groupOther   // another mail program's store (Evolution), or mail outside any Thunderbird profile
	groupUnused  // a Thunderbird folder no account uses now
)

// pickRow is what the list knows of one mailbox found, read from disk once (gatherPickRow) and then
// worded (pickList). Nothing in it is written anywhere.
type pickRow struct {
	path  string
	group pickGroup
	kind  identKind // what folder says the mailbox by
	// folder is the folder's or file's own name, or, for another mail program's folder, the name its
	// reader gives the store (Evolution's account name, "On This Computer"); as found, not cleaned.
	folder string
	// tb: a folder in a Thunderbird profile (FindProfileDir), which is profile.
	tb      bool
	profile string
	// name is Thunderbird's name for the account that owns exactly this folder (ReadStoreFacts,
	// MA-1284's one strict read), as written, not cleaned; "" when it cannot be told.
	name string
	// unused: no live Thunderbird account uses the folder, as the match says for certain (MA-1463).
	unused bool
	// downloads: the account's mail is downloaded over time from a server (IMAP, Exchange), so an
	// empty folder means nothing is downloaded yet.
	downloads bool
	size      int64 // bytes on this computer (storeSize)
	sized     bool  // size was counted
	floor     bool  // the count stopped early: size is at least
}

// gatherPickRows reads what the list says of each mailbox found, in found's order. The sizes are
// counted until deadline, each count visiting at most entryMax entries.
func gatherPickRows(found []string, deadline time.Time, entryMax int) []pickRow {
	rows := make([]pickRow, 0, len(found))
	for _, p := range found {
		rows = append(rows, gatherPickRow(p, deadline, entryMax))
	}
	return rows
}

// gatherPickRow reads what the list says of the mailbox at p: for a folder in a Thunderbird profile,
// through the same strict read of prefs.js and user.js every backup makes of it (planTB); for an
// Outlook file, its name and size; for any other folder, the store name its reader gives, as before.
func gatherPickRow(p string, deadline time.Time, entryMax int) pickRow {
	r := pickRow{path: p, group: groupOther, kind: identFolder, folder: filepath.Base(filepath.Clean(p))}
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		r.kind = identFile
		if isOutlookFile(p) {
			r.group = groupOutlook
		}
		if li, err := os.Lstat(p); err == nil && li.Mode().IsRegular() {
			r.size, r.sized = li.Size(), true
		}
		return r
	}
	if profile, ok := thunderbird.FindProfileDir(p); ok {
		r.tb, r.profile, r.group = true, profile, groupTBMail
		f, err := thunderbird.ReadStoreFacts(profile, p)
		kind, isMatch := thunderbird.NoAccountKindOf(err)
		switch {
		case f.Account != nil && f.Whole:
			r.name = f.Name
			switch f.Account.Type {
			case "none", "rss":
				r.group = groupTBLocal
			case "imap", "ews", "graph":
				r.downloads = true
			}
		case isMatch && kind == thunderbird.NoAccountNone:
			r.unused, r.group = true, groupUnused
		default:
			r.downloads = thunderbird.IsImapStore(p)
		}
	} else if s, err := source.Open(p); err == nil {
		if name := strings.TrimSpace(s.StoreName()); name != "" {
			r.folder = name
		}
		_ = s.Close()
	}
	r.size, r.floor, r.sized = storeSize(p, deadline, entryMax)
	return r
}

// storeSize counts how much mail the mailbox folder dir keeps on this computer (MA-1466): every
// regular file under it, at any depth, except Thunderbird's summary files (.msf) and its .dat
// settings files, and nothing inside a folder of Windows Search's copies (<mailbox>.mozmsgs, one
// file per message, which Thunderbird keeps when Windows Search may index mail). It follows no link:
// a folder that is itself a link, or that cannot be read, is not counted (ok false). The count stops
// after entryMax entries or at deadline, or when something below cannot be read, and then says at
// least (floor).
func storeSize(dir string, deadline time.Time, entryMax int) (n int64, floor, ok bool) {
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() || !time.Now().Before(deadline) {
		return 0, false, false
	}
	errStop := errors.New("stop counting")
	entries := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			floor = true
			return nil
		}
		if entries++; entries > entryMax || !time.Now().Before(deadline) {
			floor = true
			return errStop
		}
		switch {
		case d.IsDir():
			if p != dir && strings.HasSuffix(strings.ToLower(d.Name()), ".mozmsgs") {
				return filepath.SkipDir
			}
		case d.Type().IsRegular():
			switch strings.ToLower(filepath.Ext(d.Name())) {
			case ".msf", ".dat":
				return nil
			}
			info, err := d.Info()
			if err != nil {
				floor = true
				return nil
			}
			n += info.Size()
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return 0, false, false
	}
	return n, floor, true
}

// pickSizeWords says n bytes as the computer's own file browser does: in MB, GB or TB, with one
// decimal below 10 and whole numbers from 10, and "under 1 MB" below that. 1 GB is 1,000,000,000
// bytes on macOS and Linux (as Finder and GNOME Files count) and 1,073,741,824 on Windows (as File
// Explorer counts).
func pickSizeWords(n int64, goos string) string {
	unit := 1000.0
	if goos == "windows" {
		unit = 1024
	}
	v, u := float64(n)/(unit*unit), "MB"
	if v < 1 {
		return "under 1 MB"
	}
	for _, next := range []string{"GB", "TB"} {
		if math.Round(v) < 1000 {
			break
		}
		v, u = v/unit, next
	}
	if math.Round(v*10) < 100 {
		return fmt.Sprintf("%.1f %s", v, u)
	}
	return fmt.Sprintf("%.0f %s", v, u)
}

// pickSizeOf is a row's size words: "" when its size was not counted, or the count stopped before
// 1 MB; "nothing downloaded yet" for an empty folder of an account whose mail is downloaded over
// time; "empty" for another empty one; "over SIZE" when the count stopped early.
func pickSizeOf(r pickRow, goos string) string {
	switch {
	case !r.sized:
		return ""
	case r.floor && pickSizeWords(r.size, goos) == "under 1 MB":
		return ""
	case r.floor:
		return "over " + pickSizeWords(r.size, goos)
	case r.size == 0 && r.downloads:
		return "nothing downloaded yet"
	case r.size == 0:
		return "empty"
	}
	return pickSizeWords(r.size, goos)
}

// pickIdent is what a row says its mailbox by, before any lead word or detail.
type pickIdent struct {
	text string
	kind identKind
	own  bool   // text is MailArchive's own words ("Folder with no name"), which need no lead word
	hint string // the profile's folder name, when a row of another profile is said the same way
}

// pickIdents is what each row says its mailbox by (MA-1462):
//
//   - a Thunderbird account by the name Thunderbird shows for it, cleaned and capped
//     (CleanFolderName). The name is not used, and the folder's name is, when it spells MailArchive's
//     no-name words or another account of the same profile has it too (MA-1287, TBDistinctNames), or
//     when it reads like the folder of another Thunderbird row, which that row may be said by;
//   - any other mailbox by its folder's or file's own name, or its reader's store name, cleaned,
//     else "Folder with no name" or "File with no name".
//
// Thunderbird rows of two profiles said the same way (one address kept in an old profile and a new
// one) each say their profile's folder name (hint).
func pickIdents(rows []pickRow) []pickIdent {
	names := make([]string, len(rows))
	byProfile := map[string][]int{}
	for i, r := range rows {
		if r.tb && r.name != "" {
			names[i] = state.CleanFolderName(r.name)
			byProfile[r.profile] = append(byProfile[r.profile], i)
		}
	}
	for _, idx := range byProfile {
		accts := make([]state.TBAccount, len(idx))
		for j, i := range idx {
			accts[j].Name = names[i]
		}
		state.TBDistinctNames(accts)
		for j, i := range idx {
			names[i] = accts[j].Name
		}
	}
	folderKey := func(r pickRow) string { return state.TBTextKey(state.CleanFolderName(r.folder)) }
	folders := map[string]int{}
	for _, r := range rows {
		if r.tb {
			folders[folderKey(r)]++
		}
	}
	for i, r := range rows {
		if names[i] == "" {
			continue
		}
		k := state.TBTextKey(names[i])
		others := folders[k]
		if k == folderKey(r) {
			others-- // its own folder: said either way, it reads the same
		}
		if others > 0 {
			names[i] = ""
		}
	}

	out := make([]pickIdent, len(rows))
	for i, r := range rows {
		if names[i] != "" {
			out[i] = pickIdent{text: names[i], kind: identName}
			continue
		}
		out[i] = pickIdent{text: state.CleanFolderName(r.folder), kind: r.kind}
		switch {
		case out[i].text != "":
		case r.kind == identFile:
			out[i].text, out[i].own = "File with no name", true
		default:
			out[i].text, out[i].own = "Folder with no name", true
		}
	}

	profiles := map[string]map[string]bool{}
	for i, r := range rows {
		if r.tb {
			k := state.TBTextKey(out[i].text)
			if profiles[k] == nil {
				profiles[k] = map[string]bool{}
			}
			profiles[k][r.profile] = true
		}
	}
	for i, r := range rows {
		if r.tb && len(profiles[state.TBTextKey(out[i].text)]) > 1 {
			out[i].hint = profileHint(r.profile)
		}
	}
	return out
}

// profileHint is the name a Thunderbird profile's folder is told apart by: the part after its first
// dot ("default-release" for x1y2z3ab.default-release), cleaned, as the folder itself is named.
func profileHint(profile string) string {
	base := filepath.Base(profile)
	if _, rest, ok := strings.Cut(base, "."); ok && strings.TrimSpace(rest) != "" {
		base = rest
	}
	if h := state.CleanFolderName(base); h != "" {
		return h
	}
	return "with no name"
}

// reservedStarts are the words, as TBTextKey keys them, that only MailArchive's own rows start
// with: the All row's, a folder no account uses now, and the lead words.
var reservedStarts = []string{"all of them", "not in thunderbird", "account ", "folder ", "file "}

// needsLead reports whether a row that starts with ident must start with a lead word instead
// (MA-1464): ident does not start with an ordinary letter or digit (A to Z, a to z, 0 to 9), so it
// could start like the All row, read as a dialog program's option ("-x"), or turn the row's
// direction; or it starts with the words of the All row, of a folder no account uses, or of a lead
// word, compared as MA-1287 compares names.
func needsLead(ident string) bool {
	r, _ := utf8.DecodeRuneInString(ident)
	if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9') {
		return true
	}
	k := state.TBTextKey(ident)
	for _, w := range reservedStarts {
		if strings.HasPrefix(k, w) {
			return true
		}
	}
	return false
}

// pickRowText is one row's words before the uniqueness pass: IDENT, with "Not in Thunderbird now: "
// before a folder no account uses, or a lead word before one that needs it, then the details in
// brackets: the profile's folder name when needed, and the size.
func pickRowText(r pickRow, id pickIdent, goos string) string {
	head := id.text
	switch {
	case r.unused:
		head = notInThunderbird + head
	case !id.own && needsLead(head):
		head = leadWord[id.kind] + head
	}
	var detail []string
	if id.hint != "" {
		detail = append(detail, "profile "+id.hint)
	}
	if w := pickSizeOf(r, goos); w != "" {
		detail = append(detail, w)
	}
	if len(detail) == 0 {
		return head
	}
	return head + " (" + strings.Join(detail, ", ") + ")"
}

// pickList words rows for the list (after "All of them") and orders them: by group (pickGroup),
// then A to Z by what each says its mailbox by, ignoring letter case. It gives each row's text and the
// path it stands for, in that order. No two texts are the same after NFKC and case folding
// (TBTextKey, whole texts with no cap), and none is the All row's: a text already taken gets " (2)",
// " (3)" and so on (MA-1464).
func pickList(rows []pickRow, goos string) (labels, paths []string) {
	idents := pickIdents(rows)
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ra, rb := rows[order[a]], rows[order[b]]
		if ra.group != rb.group {
			return ra.group < rb.group
		}
		if ka, kb := state.TBTextKey(idents[order[a]].text), state.TBTextKey(idents[order[b]].text); ka != kb {
			return ka < kb
		}
		return ra.path < rb.path
	})
	used := map[string]bool{state.TBTextKey(allOfThem): true, state.TBTextKey(allOfThemBare): true}
	for _, i := range order {
		text := pickRowText(rows[i], idents[i], goos)
		unique := text
		for n := 2; used[state.TBTextKey(unique)]; n++ {
			unique = fmt.Sprintf("%s (%d)", text, n)
		}
		used[state.TBTextKey(unique)] = true
		labels = append(labels, unique)
		paths = append(paths, rows[i].path)
	}
	return labels, paths
}

// pickChoice maps the list's answer back to its row (MA-1465): all for the All row (its text, or
// its words without the arrow); else the label with exactly that text; else the one whose whole text
// is the same after NFKC and case folding (TBTextKey), of which there is at most one. ok is false for
// any other answer, "" included: it names no row, and is never taken as a path.
func pickChoice(answer string, labels []string) (i int, all, ok bool) {
	k := state.TBTextKey(strings.TrimSpace(answer))
	if answer == allOfThem || k == state.TBTextKey(allOfThem) || k == state.TBTextKey(allOfThemBare) {
		return -1, true, true
	}
	for i, l := range labels {
		if answer == l {
			return i, false, true
		}
	}
	if k == "" {
		return -1, false, false
	}
	for i, l := range labels {
		if state.TBTextKey(l) == k {
			return i, false, true
		}
	}
	return -1, false, false
}
