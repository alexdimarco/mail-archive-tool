package wizard

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/state"
)

// perMailboxBuilt says a person may reach One archive for each mailbox. Until step 4 of the design's
// build order it was false: the wizard's part of a set was built (step 3b: makeSet, from the parent
// folder to the schedule's end note), but MailArchive Desktop's Make a new archive… ran this same
// flow through AddArchive, which registered one folder, so the shipped flow never asked the question
// after All of them (MA-1490). Step 4 gave the Desktop AddArchives and its notes (MA-1499), so it is
// true: the double-click wizard and Make a new archive… ask the question (never Make a new encrypted
// archive…, whose flow passes keyed), and auto-detect's one-archive path asks MA-1507's question.
const perMailboxBuilt = true

// The question after All of them (§3, revision 2's C8 and C20; MA-1490): fixed words with none of
// & < > \ (MA-1286), shown whole in Windows' scrolling prompt box since 1.7.4.
const (
	eachQuestion = "Keep these mailboxes together in one archive, or give each one its own archive?\n\n" +
		"One archive: one place to search all your mail. A mailbox you add to Thunderbird later joins it at its next backup.\n\n" +
		"One archive for each mailbox: each can be searched, paused or deleted on its own, and a problem with one does not stop the others. A mailbox you add to Thunderbird later is not backed up until you run this again, choose One archive for each mailbox, and choose the same folder."
	// eachOne is today's result, preselected on Windows and macOS.
	eachOne  = "One archive for all of them"
	eachMany = "One archive for each mailbox"
)

// errNoEachPick ends the step when the question keeps answering with no row: nothing is exported.
var errNoEachPick = errors.New("The question did not say whether to make one archive or one for each mailbox, so nothing was exported. Try again, and click one line before you click OK.")

// askEach asks the question after All of them (MA-1490), through the listDialog seam with One
// archive preselected and an empty pick refused where the dialog can (Windows, macOS). The answer is
// read by the one row rule the mailbox list and the schedule question use (rowChoice); one that
// names no row (Linux's list can be answered with nothing selected) shows MA-1465's note and the
// question again, up to pickTries times, then ends with errNoEachPick. Cancel ends the flow with
// the dialog's error, as Cancel on the list does. goos decides only how the answer is read.
func askEach(goos string) (each bool, err error) {
	rows := []string{eachOne, eachMany}
	choices := [][]string{{eachOne}, {eachMany}}
	transcoded := answerMayBeTranscoded(goos)
	for try := 1; ; try++ {
		answer, err := listDialog(eachQuestion, rows, zenity.Title(appTitle), zenity.DefaultItems(eachOne), zenity.DisallowEmpty())
		if err != nil {
			return false, err
		}
		if i, ok := rowChoice(answer, choices, transcoded); ok {
			return i == 1, nil
		}
		if try >= pickTries {
			return false, errNoEachPick
		}
		// Closing the note with its close button asks again too, as OK does (MA-1209's rule).
		if err := noticeClosed(messageDialog(pickAgain, zenity.Title(appTitle))); err != nil {
			return false, err
		}
	}
}

// One archive for each mailbox (docs/design-per-mailbox-archives.md, revision 2.2; S90): after
// "All of them" the wizard may make one ordinary archive per mailbox, each in its own folder inside
// one parent folder. This file holds the pure parts: which of the list's rows get an archive (§5,
// MA-1491) and what each archive's folder is named (§4, MA-1492).

// setRow is one mailbox of the list, in the list's order, as a set treats it (§5).
type setRow struct {
	path string // the mailbox's store, as auto-detect found it
	// name is its archive folder's name (§4): made safe, and told apart from the other rows that
	// get an archive in the list's order. Rows that get no archive keep their name before telling
	// apart (they are only named in the plan's "Not made" line).
	name string
	// shown is the name the list showed for it (its row as it starts, without the details in
	// brackets): what the download question and the plan's line about it call the account (MA-1502).
	shown string
	make  bool // it gets an archive (§5)
	once  bool // saved once and never scheduled: a folder no account uses now, or an older copy
	// empty: not made, since it is empty and nothing will arrive (an empty feeds account, folder no
	// account uses now, or older copy).
	empty bool
	// unnamed: not made, since every number to setNumberMax was taken by mailboxes named alike.
	unnamed bool
	row     pickRow
}

// setNameMax is how many characters an archive folder's name has before any " (2)" number (§4,
// revision 2.1's F-09): at most 65 with it.
const setNameMax = 60

// setNumberMax is the highest " (N)" a name takes; with setNameMax that keeps a name at 65
// characters (" (99)" is five).
const setNumberMax = 99

// setFallbackName is the name of a mailbox whose own name cannot be a folder's (§4 "Made safe").
const setFallbackName = "Mailbox"

// The suffixes a name takes for its kind (§4 "The name").
const (
	suffixOutlook   = " (Outlook)"
	suffixEvolution = " (Evolution)"
	suffixUnused    = " (not in Thunderbird now)"
	suffixOlder     = " (older copy)"
)

// setRows is each row of the list l, in the list's order, as a set treats it (§5, MA-1491): every
// row gets an archive except an empty one (counted in full, size 0) that is a feeds account, a folder
// no account uses now, or an older copy. Local Folders always gets one, even empty; an account whose
// folder has nothing downloaded yet gets one, as does a row whose size was not counted. A folder no
// account uses now and an older copy are saved once. The names of the rows that get an archive are
// told apart in the list's order (setToldApart, with goos's answer rule).
func setRows(l listed, goos string) []setRow {
	out := make([]setRow, 0, len(l.order))
	var made []int
	for _, i := range l.order {
		r, id := l.rows[i], l.idents[i]
		s := setRow{path: r.path, row: r, name: setFolderName(r, id), shown: rowHead(r, id)}
		s.once = r.unused || id.older
		emptyKind := r.acct == "rss" || r.unused || id.older
		s.empty = emptyKind && r.sized && !r.floor && r.size == 0
		s.make = !s.empty
		if s.make {
			made = append(made, len(out))
		}
		out = append(out, s)
	}
	names := make([]string, len(made))
	for j, k := range made {
		names[j] = out[k].name
	}
	for j, n := range setToldApart(names, goos) {
		if n == "" {
			// A hundred mailboxes named alike: no number is left for this one.
			out[made[j]].make, out[made[j]].unnamed = false, true
			continue
		}
		out[made[j]].name = n
	}
	return out
}

// listName is the name the list showed for the row's mailbox, or its folder's name where none is
// known (a row made by hand).
func (r setRow) listName() string {
	if r.shown != "" {
		return r.shown
	}
	return r.name
}

// setMakes counts the rows of a set that get an archive.
func setMakes(rows []setRow) int {
	n := 0
	for _, r := range rows {
		if r.make {
			n++
		}
	}
	return n
}

// setFolderName is the name of the archive folder for the mailbox of row r, said as id (§4): the
// row's identity text without its details and lead word, with the suffix of its kind, made safe
// (setSafeName). An Outlook data file is said by its file name without .pst or .ost.
func setFolderName(r pickRow, id pickIdent) string {
	base, suffix := id.text, ""
	switch {
	case r.unused:
		suffix = suffixUnused
	case id.older:
		suffix = suffixOlder
	case r.group == groupOutlook:
		suffix = suffixOutlook
		if !id.own {
			if ext := filepath.Ext(base); strings.EqualFold(ext, ".pst") || strings.EqualFold(ext, ".ost") {
				base = strings.TrimSuffix(base, ext)
			}
			if strings.TrimSpace(base) == "" {
				base = "File with no name"
			}
		}
	case r.evolution:
		suffix = suffixEvolution
	}
	return setSafeName(base, suffix)
}

// setSafeName makes a folder name of base and suffix (§4 "Made safe", C16, C17, revision 2.1's
// F-09), in a fixed order: base is cut to setNameMax less the suffix's length, on a character
// boundary; the suffix is added; the copy export's cleaning tidies the result (app.SuggestFolderName:
// control characters dropped, / \ : * ? " < > | made spaces, white space tidied, a leading or trailing
// dot dropped). The result must pass the copy export's folder-name check (app.FolderNameWhy, which
// holds util.SanitizeSegment's) and must not be a device name Windows reserves (setReservedName);
// one that fails, or is empty, "untitled" or "New folder", is setFallbackName. Text from prefs.js is
// used only so: never joined with a separator here, never resolved. A name that would end in an
// ending a file manager treats as something other than a folder gets " (mail)" added
// (setPackageEnding; the adversarial pass's OUT-02, the lead's call).
func setSafeName(base, suffix string) string {
	if name := setSafeNameOnce(base, suffix); !setPackageEnding(name) {
		return name
	}
	return setSafeNameOnce(base, suffix+setPackageSuffix)
}

// setPackageSuffix is added to a folder name that would end in a package or shell-folder ending, so
// the folder still reads as the address and opens as a folder (OUT-02, the lead's call: this keeps the
// owner's ruling 3, folders named by the address).
const setPackageSuffix = " (mail)"

// setPackageEndings are the endings macOS's Finder shows as an app, a document package or a bundle
// rather than a folder (OUT-02, the lead's list).
var setPackageEndings = []string{".app", ".bundle", ".pkg", ".rtfd", ".framework", ".plugin", ".kext"}

// setShellFolderRe is the ".{GUID}" ending by which Windows' File Explorer opens a folder as a shell
// folder (the Recycle Bin, Control Panel …) instead of what it holds.
var setShellFolderRe = regexp.MustCompile(`\.\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}$`)

// setPackageEnding reports whether name ends, in any letter case, in one of setPackageEndings or in a
// ".{GUID}".
func setPackageEnding(name string) bool {
	lower := strings.ToLower(name)
	for _, e := range setPackageEndings {
		if strings.HasSuffix(lower, e) {
			return true
		}
	}
	return setShellFolderRe.MatchString(name)
}

// setSafeNameOnce is setSafeName before its package-ending rule.
func setSafeNameOnce(base, suffix string) string {
	// The row's words are cleaned already (state.CleanFolderName: invisible and bidirectional
	// controls removed); cleaned again here, so no caller can hand a name that is not.
	base = state.CleanFolderName(base)
	keep := setNameMax - utf8.RuneCountInString(suffix)
	if keep < 0 {
		keep = 0
	}
	name := ""
	// Cleaning can lengthen a name (Unicode composition that decomposes): cut again until it fits.
	for try := 0; try < 4; try++ {
		if utf8.RuneCountInString(base) > keep {
			base = string([]rune(base)[:keep])
		}
		name = app.SuggestFolderName(base + suffix)
		over := utf8.RuneCountInString(name) - setNameMax
		if over <= 0 {
			break
		}
		keep -= over
		if keep < 0 {
			keep = 0
		}
	}
	switch {
	case utf8.RuneCountInString(name) > setNameMax,
		name == "",
		strings.EqualFold(name, "untitled"),
		strings.EqualFold(name, "New folder"),
		app.FolderNameWhy(name) != "",
		setReservedName(name):
		return setFallbackName
	}
	return name
}

// setReservedDevices are the device names Windows reserves (§4 "Made safe", C17): util's list and
// the ones it does not hold, the superscript ports and the console's two.
var setReservedDevices = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	"COM¹": true, "COM²": true, "COM³": true, "LPT¹": true, "LPT²": true, "LPT³": true,
	"CONIN$": true, "CONOUT$": true,
}

// setReservedName reports whether name is a device name Windows reserves, with or without an
// extension, compared without regard to case (the part before its first dot, its trailing spaces
// dropped as Windows drops them).
func setReservedName(name string) bool {
	head, _, _ := strings.Cut(name, ".")
	return setReservedDevices[strings.ToUpper(strings.TrimRight(head, " "))]
}

// setToldApart tells names apart in their order (§4 "Told apart", C18): a name the same as an
// earlier one after NFKC and case folding (state.TBTextKey), or, where answers may be transcoded
// (Linux and the BSDs), in its ASCII form too, gets " (2)", " (3)" and so on: Windows and macOS
// folders ignore case.
func setToldApart(names []string, goos string) []string {
	used := setNamesUsed{transcoded: answerMayBeTranscoded(goos), keys: map[string]bool{}, ascii: map[string]bool{}}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = used.free(n, nil)
		used.add(out[i])
	}
	return out
}

// setNamesUsed is the names a set has used, as setToldApart compares them.
type setNamesUsed struct {
	transcoded bool
	keys       map[string]bool
	ascii      map[string]bool
}

func (u *setNamesUsed) add(name string) {
	u.keys[state.TBTextKey(name)] = true
	u.ascii[asciiForm(name)] = true
}

func (u *setNamesUsed) taken(name string) bool {
	return u.keys[state.TBTextKey(name)] || u.transcoded && u.ascii[asciiForm(name)]
}

// free is name, or name with the first number from 2 that is not taken here nor by also (which
// reports a name taken elsewhere, the parent folder's own); "" when every number to setNumberMax is.
func (u *setNamesUsed) free(name string, also func(string) bool) string {
	ok := func(n string) bool { return !u.taken(n) && (also == nil || !also(n)) }
	if ok(name) {
		return name
	}
	for n := 2; n <= setNumberMax; n++ {
		if c := fmt.Sprintf("%s (%d)", name, n); ok(c) {
			return c
		}
	}
	return ""
}
