package thunderbird

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Gmail is the answer to "is this Thunderbird store a Gmail account's?" (slice B1, MA-1235). Its
// zero value is GmailCantTell, so an answer nobody set never applies the Gmail rule.
type Gmail int

const (
	// GmailCantTell: no account owns the store, or prefs.js or user.js cannot be read with
	// certainty. Both designs read it as "do not apply the Gmail rule": the completeness check
	// says `unknown`, gmail-dedup archives the store per folder, with one note.
	GmailCantTell Gmail = iota
	// GmailNo: the store's account is not one Thunderbird treats as Gmail.
	GmailNo
	// GmailYes: the store's account has is_gmail true, so Thunderbird keeps one copy of each
	// message across its folders (nsImapMailFolder.cpp, the single-copy rule under
	// GetIsGMailServer).
	GmailYes
)

// String is the answer in a log's words.
func (g Gmail) String() string {
	switch g {
	case GmailYes:
		return "yes"
	case GmailNo:
		return "no"
	default:
		return "can't tell"
	}
}

// GmailForStore answers, for the Thunderbird store folder storeDir in the profile profileDir,
// whether the account that owns it is a Gmail account, the one shared answer of the completeness
// check and gmail-dedup (completeness design §3's "Gmail detection" fold; gmail-dedup design §6's
// [GD-9] fold and §13's B1). It is decided only by that account's is_gmail, as Thunderbird applies
// it:
//
//   - the account is FindAccountForStore's, by the store's own folder setting (MA-1195): a live
//     account only, the hostname only when one server has it, never a guess. No account, or more
//     than one, is GmailCantTell;
//   - prefs.js and user.js are read once, through C0c's reader (util.ReadRegularFile, 16 MB, an
//     allow-list of the lines Thunderbird writes, user.js over prefs.js). A file that cannot be
//     read, or read with certainty, anywhere in it, is GmailCantTell; so is an is_gmail that is not
//     true or false (wrongType, MA-1270), since Thunderbird drops such a value;
//   - an account whose type is not imap is GmailNo whatever its is_gmail: Thunderbird keeps one copy
//     across folders only for IMAP (nsImapMailFolder.cpp), so the Gmail rule never applies to POP;
//   - mail.server.<key>.is_gmail true is GmailYes, false GmailNo. With no line of its own the
//     account takes mail.server.default.is_gmail, as Thunderbird's GetBoolValue falls back to the
//     default branch, and with neither the built-in default, false (mailnews.js): a profile that
//     has never connected to Gmail has no is_gmail yet, and is GmailNo.
//
// The host name (imap.gmail.com) and folder flags never decide it. Which folder is All Mail (the
// one with the All Mail flag 0x8000 in folderCache.json) is not read here: completeness slice C1a's
// folderCache.json reader reads it (lead's call, 2026-11-08 [B1 split]).
//
// The error is nil for a yes or a no. For GmailCantTell it says why, naming prefs.js or user.js
// and the store folder; it unwraps to FindAccountForStore's error (a *HandEditedError when a file
// cannot be read with certainty). Its words are a log's: the surfaces that show the answer give
// their own.
func GmailForStore(profileDir, storeDir string) (Gmail, error) {
	acct, s, err := findAccount(profileDir, storeDir)
	if err != nil {
		return GmailCantTell, &gmailUnknown{store: StripControl(filepath.Base(storeDir)), why: err}
	}
	if acct.Type != "imap" {
		return GmailNo, nil // Thunderbird's single-copy rule is IMAP's (lead's call, 2026-11-08)
	}
	v, own := s.vals["mail.server."+acct.ServerKey+".is_gmail"]
	if !own {
		v = s.vals["mail.server.default.is_gmail"]
	}
	if v == "true" {
		return GmailYes, nil
	}
	return GmailNo, nil
}

// gmailUnknown is why GmailForStore cannot tell.
type gmailUnknown struct {
	store string
	why   error
}

func (e *gmailUnknown) Unwrap() error { return e.why }

func (e *gmailUnknown) Error() string {
	why := e.why.Error()
	var h *HandEditedError
	if errors.As(e.why, &h) {
		// Not h.Error(): those are -enable-offline's words.
		why = fmt.Sprintf("%s line %d %s", h.File, h.Line, h.What)
	}
	return fmt.Sprintf("MailArchive cannot tell whether the Thunderbird account of the mail in %s is a Gmail account (%s), so the Gmail rule is not applied to it", e.store, why)
}
