package thunderbird

// The refusals of the offline preparation (the wizard's Yes, prepare and the command line's
// -enable-offline), each with two sets of words (the C0c friction review's W0 to W8, MA-1260):
// Error is the command line's, which says nothing was changed or exported, may keep the
// technical detail after its plain sentence, and ends with the way out; Plain is the wizard's,
// with no prefs.js, no server key and no raw error (the wizard adds its own W0 tail).

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// The account's offline setting in Thunderbird, by version (completeness design §6, the M4
// re-read and foldcheck2 L5: the labels change at 160). Lab row MA-1191 confirms them on real
// installs.
const (
	labelBefore160 = `"Keep messages in all folders for this account on this computer"`
	labelFrom160   = `"Save copies of all folders and messages on this computer for offline use"`
	pageBefore160  = `"Synchronization & Storage"`
	groupFrom160   = `"Offline message storage"`
)

const (
	// cliNothing opens every command-line refusal: -enable-offline refuses before the export.
	cliNothing = "-enable-offline changed nothing, and nothing was exported: "
	// cliAgain ends it: once the setting is made sure of by hand, the run needs no -enable-offline.
	cliAgain = " Then run the same command again without -enable-offline."
	// leftoverWords: a removed account's leftover folder is a likely cause (the lead's addition).
	leftoverWords = "If you removed an account from Thunderbird, its leftover mail folder can cause this; if so, you can ignore this message."
	// stepsIntro opens the wizard's numbered steps.
	stepsIntro = "To make sure Thunderbird keeps all of this mail on this computer:\n"
)

// Place is which of Thunderbird's labels the words give: those of the version that last opened
// the profile, or both when it cannot be read.
type Place int

const (
	PlaceBoth      Place = iota // the version cannot be read
	PlaceBefore160              // "Synchronization & Storage", "Keep messages in all folders…"
	PlaceFrom160                // "Offline message storage", "Save copies of all folders…"
)

// placeFor is the Place for the Thunderbird that last opened the profile.
func placeFor(profileDir string) Place {
	major, ok := lastVersionMajor(profileDir)
	switch {
	case !ok:
		return PlaceBoth
	case major < 160:
		return PlaceBefore160
	default:
		return PlaceFrom160
	}
}

// steps are the wizard's numbered steps for who ("each account on imap.gmail.com").
func (p Place) steps(who string) string {
	var two, three string
	switch p {
	case PlaceBefore160:
		two = "open its " + pageBefore160 + " page."
		three = "Make sure " + labelBefore160 + " is ticked."
	case PlaceFrom160:
		two = "find " + groupFrom160 + "."
		three = "Make sure " + labelFrom160 + " is ticked."
	default:
		two = "open its " + pageBefore160 + " page (in Thunderbird 160 and later, find " + groupFrom160 + ")."
		three = "Make sure " + labelBefore160 + " is ticked (in Thunderbird 160 and later it is called " + labelFrom160 + ")."
	}
	return "1. In Thunderbird, open Account Settings.\n2. For " + who + ", " + two + "\n3. " + three
}

// remedy is the command line's sentence tail for who: "open Account Settings and, for who, make
// sure LABEL is ticked (where)."
func (p Place) remedy(who string) string {
	s := "open Account Settings and, for " + who + ", make sure "
	switch p {
	case PlaceBefore160:
		return s + labelBefore160 + " is ticked (on its " + pageBefore160 + " page)."
	case PlaceFrom160:
		return s + labelFrom160 + " is ticked (under " + groupFrom160 + ")."
	default:
		return s + labelBefore160 + " is ticked (on its " + pageBefore160 + " page; in Thunderbird 160 and later, " + labelFrom160 + " under " + groupFrom160 + ")."
	}
}

// OfflineSettingLabel is the offline checkbox's label for the Thunderbird that last opened the
// profile (compatibility.ini's LastVersion): the older label below 160, the newer from 160,
// and both when the version cannot be read (§6 fold L5).
func OfflineSettingLabel(profileDir string) string {
	switch placeFor(profileDir) {
	case PlaceBefore160:
		return labelBefore160
	case PlaceFrom160:
		return labelFrom160
	default:
		return labelBefore160 + " (in Thunderbird 160 and later: " + labelFrom160 + ")"
	}
}

// lastVersionMajor is the major number of compatibility.ini's LastVersion
// ("153.6.0_20251212000000/…"), read as compatibilityLastVersion reads it; ok is false when it
// cannot be read.
func lastVersionMajor(profileDir string) (int, bool) {
	v, ok := compatibilityLastVersion(profileDir)
	if !ok {
		return 0, false
	}
	if i := strings.IndexAny(v, "_/"); i >= 0 {
		v = v[:i]
	}
	head, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(head)
	return n, err == nil
}

// onHost is "<what> on host", or "<what>" alone when host is not known.
func onHost(what, host string) string {
	if host = StripControl(host); host != "" {
		return what + " on " + host
	}
	return what
}

// NoAccountKind is why no account could be told for a store.
type NoAccountKind int

const (
	NoAccountUnreadable NoAccountKind = iota // prefs.js or user.js could not be read
	NoAccountAmbiguous                       // more than one live account could own the store
	NoAccountNone                            // no live account uses the store (often a removed one)
	NoAccountNoList                          // prefs.js lists no account
)

// NoAccountError is what the person is told when MailArchive cannot tell which account a store
// belongs to, so it changes nothing (the C0c pass's OUT-01, MA-1204; words MA-1260). Host is the
// store folder's name, Why FindAccountForStore's technical reason (naming prefs.js).
type NoAccountError struct {
	Kind  NoAccountKind
	Host  string
	Place Place
	Why   error
}

func (e *NoAccountError) Unwrap() error { return e.Why }

// Error is the command line's words (W5).
func (e *NoAccountError) Error() string {
	h := StripControl(e.Host)
	switch e.Kind {
	case NoAccountAmbiguous:
		return cliNothing + "MailArchive could not tell which Thunderbird account the mail in " + h + " belongs to (" + e.Why.Error() + "). " +
			leftoverWords + " In Thunderbird, " + e.Place.remedy(onHost("each account", h)) + cliAgain
	case NoAccountNone:
		return cliNothing + "MailArchive could not find a Thunderbird account for the mail in " + h + " (" + e.Why.Error() + "). " +
			leftoverWords + " If the account is still in Thunderbird, " + e.Place.remedy("that account") + cliAgain
	case NoAccountNoList:
		return cliNothing + "MailArchive could not tell which Thunderbird account the mail in " + h + " belongs to (" + e.Why.Error() + "). " +
			"In Thunderbird, " + e.Place.remedy(onHost("the account", h)) + cliAgain
	default:
		return cliNothing + "MailArchive could not read Thunderbird's settings (" + e.Why.Error() + "). " +
			"In Thunderbird, " + e.Place.remedy(onHost("the account", h)) + cliAgain
	}
}

// Plain is the wizard's words (W1 to W3).
func (e *NoAccountError) Plain() string {
	h := StripControl(e.Host)
	switch e.Kind {
	case NoAccountAmbiguous:
		return "MailArchive could not tell which of your Thunderbird accounts on " + h + " this mail belongs to, so it did not change any Thunderbird setting.\n\n" +
			stepsIntro + e.Place.steps(onHost("each account", h)) +
			"\n\nWhy: more than one account in Thunderbird uses " + h + ", and MailArchive does not guess. " + leftoverWords
	case NoAccountNone:
		return "MailArchive could not find a Thunderbird account for the mail in " + h + ", so it did not change any Thunderbird setting.\n\n" +
			"Why: no account in Thunderbird uses this mail now. " + leftoverWords +
			"\n\nIf the account is still in Thunderbird:\n" + e.Place.steps("that account")
	case NoAccountNoList:
		return "MailArchive could not tell which of your Thunderbird accounts on " + h + " this mail belongs to, so it did not change any Thunderbird setting.\n\n" +
			stepsIntro + e.Place.steps(onHost("the account", h)) +
			"\n\nWhy: Thunderbird's settings list no accounts."
	default:
		return "MailArchive could not read Thunderbird's settings, so it did not change any of them.\n\n" +
			stepsIntro + e.Place.steps(onHost("the account", h))
	}
}

// handWhy is why a hand-edited settings file stops the change.
type handWhy int

const (
	whyUnread    handWhy = iota // a line MailArchive cannot read as settings names the setting
	whyShared                   // the setting's statement shares its line with something else
	whyOpenAtEnd                // a comment left open at the end would hide an added line
)

// HandEditedError is the refusal when prefs.js or user.js was edited by hand where the setting
// is, so MailArchive cannot be sure what Thunderbird reads there, or would not rewrite only that
// setting: it changes nothing (the C0c pass round 2's R2-01 and R2-02, MA-1207).
type HandEditedError struct {
	File  string // "prefs.js" or "user.js"
	Line  int    // the line (from 1); 0 for the end of the file
	Key   string // the setting
	Why   handWhy
	Host  string // the account's server, for the words
	Place Place
}

// Error is the command line's words.
func (e *HandEditedError) Error() string {
	var what string
	switch e.Why {
	case whyShared:
		what = fmt.Sprintf("%s line %d sets %s together with something else on that line (another setting or a comment), and MailArchive rewrites only a line that holds that one setting alone.", e.File, e.Line, e.Key)
	case whyOpenAtEnd:
		what = fmt.Sprintf("a comment left open at the end of %s would hide a line added there for %s, so MailArchive does not add one.", e.File, e.Key)
	default:
		what = fmt.Sprintf("%s line %d names %s in a way MailArchive cannot read as a setting, so it cannot be sure what Thunderbird reads there.", e.File, e.Line, e.Key)
	}
	return cliNothing + what + " In Thunderbird, " + e.Place.remedy(onHost("the account", e.Host)) + cliAgain
}

// Plain is the wizard's words.
func (e *HandEditedError) Plain() string {
	return "Thunderbird's settings file has been edited by hand where this account's setting is, so MailArchive could not be sure what it says and did not change any Thunderbird setting.\n\n" +
		stepsIntro + e.Place.steps(onHost("the account", e.Host))
}

// UserJSError is EnableOffline's refusal when the profile's user.js sets the account's
// offline setting to something other than on: Thunderbird applies user.js over prefs.js at
// every start, so a change to prefs.js would be undone, and nothing is changed (the C0c
// pass's CON-06, MA-1205). Its words name user.js and the server, say to leave it when it was
// set on purpose, and give the file last (W4, W6).
type UserJSError struct {
	Path string // the profile's user.js
	Key  string // the setting, mail.server.<key>.offline_download
	Host string // the account's server
}

// Error is the command line's words (W6).
func (e *UserJSError) Error() string {
	server := ""
	if h := StripControl(e.Host); h != "" {
		server = " (" + h + ")"
	}
	return cliNothing + "user.js sets " + e.Key + server + " to something other than true, and Thunderbird applies user.js over prefs.js at every start, so a change would be undone. " +
		"If you or your IT set this on purpose, leave it and run again without -enable-offline. " +
		"Otherwise close Thunderbird, delete that line from user.js (or change its false to true), and run again. The file: " + e.Path
}

// Plain is the wizard's words (W4).
func (e *UserJSError) Plain() string {
	mail := "this account's mail"
	if h := StripControl(e.Host); h != "" {
		mail = "the mail of " + h
	}
	return "Thunderbird is set, in its file user.js, not to keep " + mail + " on this computer, and it applies that file every time it starts. So MailArchive changed nothing: a change would be undone.\n\n" +
		"If you or your IT set this on purpose, leave it. Otherwise: close Thunderbird, open user.js in a plain text editor (Notepad on Windows), and delete the line that contains " + e.Key + " (or change its false to true).\n\n" +
		"The file: " + e.Path
}

// PlainWords is err's words for the wizard when err is one of these refusals.
func PlainWords(err error) (string, bool) {
	var p interface{ Plain() string }
	if errors.As(err, &p) {
		return p.Plain(), true
	}
	return "", false
}

// SettingSteps are the wizard's numbered steps to make sure, by hand, that Thunderbird keeps the
// mail of the account on host on this computer, labelled for the profile's version.
func SettingSteps(profileDir, host string) string {
	return stepsIntro + placeFor(profileDir).steps(onHost("the account", host))
}

// AccountToEnable is the account whose offline download the wizard's Yes, prepare and the
// command line's -enable-offline would turn on for store. When no account can be told, the
// error is a NoAccountError; when a setting the match reads cannot be told, a HandEditedError;
// when user.js keeps the setting off, a UserJSError. Either way nothing has been, or may be,
// changed.
func AccountToEnable(profileDir, store string) (*Account, error) {
	acct, err := FindAccountForStore(profileDir, store)
	if err != nil {
		var h *HandEditedError
		if errors.As(err, &h) {
			return nil, h
		}
		e := &NoAccountError{Kind: NoAccountUnreadable, Host: StripControl(filepath.Base(store)), Place: placeFor(profileDir), Why: err}
		var m *matchError
		if errors.As(err, &m) {
			e.Kind = m.kind
		}
		return nil, e
	}
	if acct.OfflineInUserJS && !acct.OfflineDownload {
		return nil, &UserJSError{Path: filepath.Join(profileDir, "user.js"), Key: "mail.server." + acct.ServerKey + ".offline_download", Host: acct.Hostname}
	}
	return acct, nil
}
