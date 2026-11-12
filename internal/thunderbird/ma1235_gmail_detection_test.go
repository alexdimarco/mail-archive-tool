package thunderbird

// Slice B1: the one shared Gmail detection (docs/design-gmail-dedup.md §6, its "[dedup foldcheck
// S7]" note and §13's B1; docs/design-thunderbird-completeness.md §3's "Gmail detection" fold).
// A store is Gmail only when the account that owns it, found by FindAccountForStore, has
// mail.server.<key>.is_gmail true as Thunderbird applies it. Rows: MA-1235 owns the function (the
// read, the account, the three answers); MA-1175 owns "is_gmail only, never the host or a folder
// flag"; MA-1195 owns the account match it reuses; MA-1270 owns the typed reading it extends.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// gmailLine is the is_gmail line Thunderbird writes for server key (IMAP's capability check,
// nsImapIncomingServer.cpp's SetIsGMailServer), with value as written (true, false, or another
// type for the refusals).
func gmailLine(key, value string) string {
	return `user_pref("mail.server.` + key + `.is_gmail", ` + value + `);` + "\n"
}

// isGmail is GmailForStore's answer for store, failing the test unless it is a yes or a no with
// no error.
func isGmail(t *testing.T, profile, store string) bool {
	t.Helper()
	g, err := GmailForStore(profile, store)
	if err != nil || (g != GmailYes && g != GmailNo) {
		t.Fatalf("GmailForStore(%s) = %v, %v; want a yes or a no", filepath.Base(store), g, err)
	}
	return g == GmailYes
}

// cantTell asserts GmailForStore answers "can't tell" for store, with an error that names each
// of names (prefs.js and the store folder among them), and that it changed nothing in the profile:
// prefs.js and user.js are byte for byte as they were, and no backup was written. It returns the
// error.
func cantTell(t *testing.T, profile, store string, names ...string) error {
	t.Helper()
	before := snapshot(profile)
	g, err := GmailForStore(profile, store)
	if g != GmailCantTell {
		t.Fatalf("GmailForStore(%s) = %v, want can't tell (%v)", filepath.Base(store), g, err)
	}
	rc, msg := 0, ""
	if err != nil {
		rc, msg = 1, err.Error()
	}
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(names...),
		assure.Forbid("panic", "goroutine", "-enable-offline"),
		assure.NoSideEffect(func() bool { return snapshot(profile) == before }))
	return err
}

// snapshot is the profile's prefs.js, user.js and backup as they are, for "changed nothing".
func snapshot(profile string) string {
	var b strings.Builder
	for _, name := range []string{"prefs.js", "user.js", "prefs.js.mailarchive-bak"} {
		data, err := os.ReadFile(filepath.Join(profile, name))
		b.WriteString(name + ":")
		if err != nil {
			b.WriteString("absent")
		} else {
			b.Write(data)
		}
		b.WriteString("\x00")
	}
	return b.String()
}

// withFolderCache writes a folderCache.json into profile giving the folder "All Mail" of store
// Thunderbird's All Mail flag (0x8000, nsMsgFolderFlags.idl) and the Archive flag (0x4000), as
// Thunderbird gives Gmail's All Mail.
func withFolderCache(t *testing.T, profile, store string) {
	t.Helper()
	data, err := json.Marshal(map[string]map[string]int{
		filepath.Join(store, "All Mail"): {"flags": 0x8000 | 0x4000 | 0x08000000, "totalMsgs": 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(profile, "folderCache.json"), string(data))
}

// covers: MA-1235, MA-1175, R25
// Gmail is told by the matched account's is_gmail only, as Thunderbird applies it (the lead's
// call for B1; completeness design §3's "Gmail detection" fold; dedup design §6's [GD-9] fold).
// Positive twin first: one account on imap.gmail.com with is_gmail true is Gmail. Then: false is
// not; no is_gmail line is not (Thunderbird's built-in default, mailnews.js
// pref("mail.server.default.is_gmail", false), ESR 153 line 578, Release 159 line 582; a profile
// that never connected has none); with no line of its own the account takes
// mail.server.default.is_gmail when prefs.js sets it, as Thunderbird's GetBoolValue falls back to
// the default branch (nsMsgIncomingServer.cpp, ESR 153 lines 449-459); the host is never used (an
// account on another host with is_gmail true is Gmail, one on imap.gmail.com without it is not);
// and a folder with the All Mail flag, in a folderCache.json beside, never makes an account Gmail.
func TestGmailIsToldByTheAccountsIsGmailOnly(t *testing.T) {
	p, store := tbProfile(t, onePrefs+gmailLine("server1", "true"), "")
	if !isGmail(t, p, store) {
		t.Fatal("the positive twin: an account with is_gmail true is not Gmail")
	}

	for name, tc := range map[string]struct {
		lines string
		want  bool
	}{
		"is_gmail false": {gmailLine("server1", "false"), false},
		"no is_gmail line (Thunderbird's default)":       {"", false},
		"no line of its own, the default true":           {gmailLine("default", "true"), true},
		"no line of its own, the default false":          {gmailLine("default", "false"), false},
		"its own false over the default true":            {gmailLine("default", "true") + gmailLine("server1", "false"), false},
		"its own true over the default false":            {gmailLine("default", "false") + gmailLine("server1", "true"), true},
		"the last of two lines wins (false, then true)":  {gmailLine("server1", "false") + gmailLine("server1", "true"), true},
		"the last of two lines wins (true, then false)":  {gmailLine("server1", "true") + gmailLine("server1", "false"), false},
		"another server's is_gmail true does not count":  {gmailLine("server9", "true"), false},
		"a dotted name holding is_gmail does not count":  {`user_pref("mail.server.server1.x.is_gmail", true);` + "\n", false},
		"a commented-out is_gmail true does not count":   {`// user_pref("mail.server.server1.is_gmail", true);` + "\n", false},
		"is_gmail of an account with the same prefix":    {gmailLine("server10", "true"), false},
		"is_gmail true written with the account's lines": {gmailLine("server1", "true"), true},
	} {
		t.Run(name, func(t *testing.T) {
			p, store := tbProfile(t, onePrefs+tc.lines, "")
			if got := isGmail(t, p, store); got != tc.want {
				t.Fatalf("Gmail = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("the host is never used", func(t *testing.T) {
		prefs := live("server1") + imapServer("server1", "mail.example.org", "[ProfD]ImapMail/mail.example.org", "") + gmailLine("server1", "true")
		p := profileWith(t, prefs, "mail.example.org")
		if !isGmail(t, p, filepath.Join(p, "ImapMail", "mail.example.org")) {
			t.Fatal("an account on another host with is_gmail true is not Gmail")
		}
		p, store := tbProfile(t, onePrefs, "")
		if isGmail(t, p, store) {
			t.Fatal("an account on imap.gmail.com with no is_gmail is Gmail: the host was used")
		}
	})

	t.Run("the All Mail flag alone is not Gmail", func(t *testing.T) {
		prefs := live("server1") + imapServer("server1", "mail.example.org", "[ProfD]ImapMail/mail.example.org", "")
		p := profileWith(t, prefs, "mail.example.org")
		store := filepath.Join(p, "ImapMail", "mail.example.org")
		withFolderCache(t, p, store)
		if isGmail(t, p, store) {
			t.Fatal("a folder with the All Mail flag made a non-Gmail account Gmail")
		}
		p, store = tbProfile(t, onePrefs+gmailLine("server1", "false"), "")
		withFolderCache(t, p, store)
		if isGmail(t, p, store) {
			t.Fatal("a folder with the All Mail flag overrode is_gmail false")
		}
	})
}

// covers: MA-1235, R25
// user.js decides over prefs.js, as Thunderbird applies it at every start (the C0c pass's CON-06):
// user.js's is_gmail false over prefs.js's true is not Gmail, and true over false is. Positive twin
// first: user.js setting something else leaves prefs.js's true as it is.
func TestGmailReadsUserJSOverPrefsJS(t *testing.T) {
	p, store := tbProfile(t, onePrefs+gmailLine("server1", "true"), `user_pref("browser.zz", true);`+"\n")
	if !isGmail(t, p, store) {
		t.Fatal("the positive twin: an unrelated user.js changed is_gmail true")
	}
	p, store = tbProfile(t, onePrefs+gmailLine("server1", "true"), gmailLine("server1", "false"))
	if isGmail(t, p, store) {
		t.Fatal("user.js's false over prefs.js's true is Gmail")
	}
	p, store = tbProfile(t, onePrefs+gmailLine("server1", "false"), gmailLine("server1", "true"))
	if !isGmail(t, p, store) {
		t.Fatal("user.js's true over prefs.js's false is not Gmail")
	}
	p, store = tbProfile(t, onePrefs, gmailLine("default", "true"))
	if !isGmail(t, p, store) {
		t.Fatal("user.js's default true, with no line of the account's own, is not Gmail")
	}
}

// covers: MA-1235, MA-1270, R25
// A file MailArchive cannot read with certainty, or an is_gmail of a type Thunderbird never writes,
// is "can't tell", never a yes or a no (the lead's call for B1). Thunderbird drops a user value
// whose type differs from the setting's default (MA-1270's rule; is_gmail's default is the boolean
// false, mailnews.js ESR 153 line 578), so is_gmail joins the settings wrongType checks: a string or
// a number for it, the account's own or the default, in prefs.js or user.js, is a HandEditedError
// with MA-1270's words. Because the reader is shared, AccountToEnable now refuses such a file too
// (Thunderbird never writes one, so no file it wrote is refused). And a line Thunderbird never
// writes anywhere in either file, with is_gmail true read beside it, is "can't tell". Positive twin
// first: is_gmail true and the default false, each a boolean, read as a yes.
func TestGmailOfTheWrongTypeOrAnUncertainFileIsCantTell(t *testing.T) {
	p, store := tbProfile(t, onePrefs+gmailLine("default", "false")+gmailLine("server1", "true"), "")
	if !isGmail(t, p, store) {
		t.Fatal("the positive twin: booleans read as a yes")
	}

	for name, line := range map[string]string{
		"its own as a string":         gmailLine("server1", `"true"`),
		"its own as a number":         gmailLine("server1", "1"),
		"the default as a string":     gmailLine("default", `"false"`),
		"the default as a number":     gmailLine("default", "0"),
		"another server's as text":    gmailLine("server9", `"yes"`),
		"a string after a true":       gmailLine("server1", "true") + gmailLine("server1", `"false"`),
		"a number before a true":      gmailLine("server1", "0") + gmailLine("server1", "true"),
		"an empty string for its own": gmailLine("server1", `""`),
	} {
		for _, file := range []string{"prefs.js", "user.js"} {
			t.Run(name+", "+file, func(t *testing.T) {
				prefs, user := onePrefs+gmailLine("server1", "true"), line
				if file == "prefs.js" {
					prefs, user = onePrefs+line, ""
				}
				p, store := tbProfile(t, prefs, user)
				err := cantTell(t, p, store, file, "imap.gmail.com", whatNotBool)
				var h *HandEditedError
				if !errors.As(err, &h) || h.File != file || h.What != whatNotBool {
					t.Fatalf("the reason is not MA-1270's HandEditedError for %s: %v", file, err)
				}
				if _, err := AccountToEnable(p, store); !errors.As(err, &h) || h.What != whatNotBool {
					t.Fatalf("AccountToEnable read a file with a wrong-typed is_gmail: %v", err)
				}
			})
		}
	}

	for name, tc := range uncertainFiles() {
		t.Run("uncertain: "+name, func(t *testing.T) {
			prefs, user := onePrefs+gmailLine("server1", "true"), tc.body
			if tc.file == "prefs.js" {
				prefs, user = onePrefs+gmailLine("server1", "true")+tc.body, ""
			}
			p, store := tbProfile(t, prefs, user)
			err := cantTell(t, p, store, tc.file, "imap.gmail.com", tc.what)
			var h *HandEditedError
			if !errors.As(err, &h) || h.File != tc.file {
				t.Fatalf("the reason is not a HandEditedError for %s: %v", tc.file, err)
			}
		})
	}
}

// covers: MA-1235, MA-1175, MA-1195, R9
// Two accounts on imap.gmail.com, only one with is_gmail true: each store gets its own account's
// answer, by its folder setting (MA-1195), never the host's. Positive twin first: one account,
// is_gmail true, is Gmail. Then server2 (ImapMail/imap.gmail.com-1) is Gmail and server1
// (ImapMail/imap.gmail.com) is not, and the same with the two swapped.
func TestGmailGivesEachStoreItsOwnAccount(t *testing.T) {
	p, store := tbProfile(t, onePrefs+gmailLine("server1", "true"), "")
	if !isGmail(t, p, store) {
		t.Fatal("the positive twin: one Gmail account is not Gmail")
	}
	two := live("server1", "server2") +
		imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") +
		imapServer("server2", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", "")
	for _, gmail := range []string{"server1", "server2"} {
		t.Run(gmail+" is the Gmail one", func(t *testing.T) {
			other := map[string]string{"server1": "server2", "server2": "server1"}[gmail]
			p := profileWith(t, two+gmailLine(gmail, "true")+gmailLine(other, "false"), "imap.gmail.com", "imap.gmail.com-1")
			got := map[string]bool{
				"server1": isGmail(t, p, filepath.Join(p, "ImapMail", "imap.gmail.com")),
				"server2": isGmail(t, p, filepath.Join(p, "ImapMail", "imap.gmail.com-1")),
			}
			if !got[gmail] || got[other] {
				t.Fatalf("Gmail by server = %v, want only %s", got, gmail)
			}
		})
	}
	t.Run("the other one with no is_gmail line", func(t *testing.T) {
		p := profileWith(t, two+gmailLine("server2", "true"), "imap.gmail.com", "imap.gmail.com-1")
		if isGmail(t, p, filepath.Join(p, "ImapMail", "imap.gmail.com")) || !isGmail(t, p, filepath.Join(p, "ImapMail", "imap.gmail.com-1")) {
			t.Fatal("the account with no is_gmail took the other's")
		}
	})
}

// covers: MA-1235, MA-1195, R9
// No account, no answer: when FindAccountForStore gives no account, Gmail is "can't tell", which
// both designs read as "do not apply the Gmail rule" (completeness `unknown`; gmail-dedup "per
// folder, with one note"). Positive twin first: the live account's own folder is Gmail. Then a
// removed account's leftover folder, whose leftover mail.server lines say is_gmail true, is never
// Gmail; a store no account uses; two live accounts that could own one store; and a prefs.js that
// lists no account.
func TestGmailWithNoAccountIsCantTell(t *testing.T) {
	ghost := accountsLine("account3", "server3") +
		imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") + gmailLine("server1", "true") + // removed
		imapServer("server3", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", "") + gmailLine("server3", "true") // live
	p := profileWith(t, ghost, "imap.gmail.com", "imap.gmail.com-1")
	if !isGmail(t, p, filepath.Join(p, "ImapMail", "imap.gmail.com-1")) {
		t.Fatal("the positive twin: the live account's own folder is not Gmail")
	}

	t.Run("a removed account's leftover folder", func(t *testing.T) {
		cantTell(t, p, filepath.Join(p, "ImapMail", "imap.gmail.com"), "prefs.js", "imap.gmail.com", "no account")
	})
	t.Run("a store no account uses", func(t *testing.T) {
		p := profileWith(t, onePrefs+gmailLine("server1", "true"), "imap.gmail.com", "old.example")
		cantTell(t, p, filepath.Join(p, "ImapMail", "old.example"), "prefs.js", "old.example", "no account")
	})
	t.Run("two live accounts could own the store", func(t *testing.T) {
		prefs := live("server1", "server2") + imapServer("server1", "imap.gmail.com", "", "") + gmailLine("server1", "true") +
			imapServer("server2", "imap.gmail.com", "", "") + gmailLine("server2", "true")
		p := profileWith(t, prefs, "imap.gmail.com")
		cantTell(t, p, filepath.Join(p, "ImapMail", "imap.gmail.com"), "prefs.js", "imap.gmail.com", "more than one account")
	})
	t.Run("prefs.js lists no account", func(t *testing.T) {
		p := profileWith(t, imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "")+gmailLine("server1", "true"), "imap.gmail.com")
		cantTell(t, p, filepath.Join(p, "ImapMail", "imap.gmail.com"), "prefs.js", "imap.gmail.com", "lists no account")
	})
}

// covers: MA-1235, R25
// prefs.js is read as a planted file may be, through C0c's opener (util.ReadRegularFile, 16 MB):
// a link at prefs.js or at user.js is refused, never followed, and its target is untouched; a
// prefs.js over its bound is refused, never read whole; no prefs.js at all is "can't tell". Each is
// "can't tell", never a yes from the file behind the link. Positive twin first: the same prefs.js,
// in place, is Gmail.
func TestGmailReadsPrefsJsAsAPlantedFileMayBe(t *testing.T) {
	gmailPrefs := onePrefs + gmailLine("server1", "true")
	p, store := tbProfile(t, gmailPrefs, "")
	if !isGmail(t, p, store) {
		t.Fatal("the positive twin: a healthy Gmail prefs.js is not Gmail")
	}

	linkTo := func(t *testing.T, name string) (string, string, func() bool) {
		t.Helper()
		target := filepath.Join(t.TempDir(), name)
		writeAt(t, target, gmailPrefs)
		p := t.TempDir()
		store := filepath.Join(p, "ImapMail", "imap.gmail.com")
		if err := os.MkdirAll(store, 0o755); err != nil {
			t.Fatal(err)
		}
		if name == "user.js" {
			writeAt(t, filepath.Join(p, "prefs.js"), onePrefs)
		}
		if err := os.Symlink(target, filepath.Join(p, name)); err != nil {
			t.Skip("no links here:", err)
		}
		return p, store, bytesStill(target, []byte(gmailPrefs))
	}
	for _, name := range []string{"prefs.js", "user.js"} {
		t.Run("a link at "+name, func(t *testing.T) {
			p, store, untouched := linkTo(t, name)
			cantTell(t, p, store, name, "a link", "imap.gmail.com")
			if !untouched() {
				t.Fatal("the link's target changed")
			}
		})
	}
	t.Run("prefs.js over its bound", func(t *testing.T) {
		p := t.TempDir()
		store := filepath.Join(p, "ImapMail", "imap.gmail.com")
		if err := os.MkdirAll(store, 0o755); err != nil {
			t.Fatal(err)
		}
		overCap(t, filepath.Join(p, "prefs.js"))
		cantTell(t, p, store, "prefs.js", "bound", "imap.gmail.com")
	})
	t.Run("no prefs.js", func(t *testing.T) {
		p := t.TempDir()
		store := filepath.Join(p, "ImapMail", "imap.gmail.com")
		if err := os.MkdirAll(store, 0o755); err != nil {
			t.Fatal(err)
		}
		cantTell(t, p, store, "prefs.js", "imap.gmail.com")
	})
}

// covers: MA-1235
// The answer's zero value is "can't tell", so an answer nobody set never applies the Gmail rule,
// and each answer has words for a log. Positive twin first: a yes reads "yes".
func TestGmailAnswersZeroValueIsCantTell(t *testing.T) {
	if GmailYes.String() != "yes" || GmailNo.String() != "no" {
		t.Fatalf("the answers' words: %q, %q", GmailYes, GmailNo)
	}
	var g Gmail
	if g != GmailCantTell || g.String() != "can't tell" {
		t.Fatalf("the zero value is %q, want can't tell", g)
	}
}
