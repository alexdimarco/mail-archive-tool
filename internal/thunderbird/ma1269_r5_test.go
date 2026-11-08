package thunderbird

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// printerLines are the lines Thunderbird writes into prefs.js after a person prints to the Windows
// network printer \\PRINTSRV\HP LaserJet 4 (the C0c pass round 5's R5-02): GetAdjustedPrinterName
// (nsPrintSettingsService.cpp) turns only the spaces into "_" for the setting's name, and StrEscape
// (Preferences.cpp's pref_savePrefs) writes each backslash of the name, and of the value, as \\.
const printerLines = `user_pref("print.printer_\\\\PRINTSRV\\HP_LaserJet_4.print_bgcolor", false);` + "\n" +
	`user_pref("print_printer", "\\\\PRINTSRV\\HP LaserJet 4");` + "\n"

// covers: MA-1269, R9, S10
// A setting name with the escapes Thunderbird writes in a name, \\ and \", is read, decoded as in
// a value (the C0c pass round 5's R5-02). Positive twin first, from the review: a prefs.js as
// Thunderbird leaves it after printing to a Windows network printer, LF and CRLF, gives server1
// (off), and EnableOffline turns it on with only server1's line changed. A name ending in \\ just
// before its closing quote ends at that quote. A server key holding a quote is matched by its
// decoded name, and the line EnableOffline writes is escaped as Thunderbird writes it, so it reads
// back as on. Every other backslash in a name (\n, \r, \x, \u, \t, \', a backslash at the end)
// stays refused, with the name's own words.
func TestNamesWithTheEscapesThunderbirdWritesAreRead(t *testing.T) {
	for _, eol := range []string{"\n", "\r\n"} {
		t.Run(map[string]string{"\n": "the printer, LF", "\r\n": "the printer, CRLF"}[eol], func(t *testing.T) {
			prefs := strings.ReplaceAll(tbHeader("\n")+printerLines+live("server1")+
				imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "")+
				`user_pref("print.printer_x\\", true);`+"\n"+off1+"\n", "\n", eol)
			p, store := tbProfile(t, prefs, "")
			acct, err := AccountToEnable(p, store)
			if err != nil || acct.ServerKey != "server1" || acct.OfflineDownload {
				t.Fatalf("AccountToEnable: %+v, %v", acct, err)
			}
			c, err := EnableOffline(p, "server1")
			if err != nil || !c.Changed || !accountOn(t, p, store) {
				t.Fatalf("EnableOffline: %+v, %v", c, err)
			}
			got, _ := os.ReadFile(filepath.Join(p, "prefs.js"))
			if want := strings.Replace(prefs, off1, on1, 1); string(got) != want {
				t.Fatalf("prefs.js is not the file with only server1's line changed:\n%q\nwant\n%q", got, want)
			}
			vals := parsePrefsText(prefs).values()
			if v, ok := vals[`print.printer_\\PRINTSRV\HP_LaserJet_4.print_bgcolor`]; !ok || v != "false" {
				t.Errorf("the printer's name is not read decoded: %q", vals)
			}
			if v, ok := vals[`print.printer_x\`]; !ok || v != "true" {
				t.Errorf(`a name ending in \\ is not read as ending at its quote: %q`, vals)
			}
		})
	}

	t.Run("a server key holding a quote", func(t *testing.T) {
		prefs := `user_pref("mail.accountmanager.accounts", "account1");` + "\n" +
			`user_pref("mail.account.account1.server", "a\"b");` + "\n" +
			`user_pref("mail.server.a\"b.hostname", "imap.gmail.com");` + "\n" +
			`user_pref("mail.server.a\"b.type", "imap");` + "\n" +
			`user_pref("mail.server.a\"b.directory-rel", "[ProfD]ImapMail/imap.gmail.com");` + "\n" +
			`user_pref("mail.server.a\"b.offline_download", false);` + "\n"
		p, store := tbProfile(t, prefs, "")
		acct, err := AccountToEnable(p, store)
		if err != nil || acct.ServerKey != `a"b` || acct.OfflineDownload {
			t.Fatalf("AccountToEnable: %+v, %v", acct, err)
		}
		c, err := EnableOffline(p, acct.ServerKey)
		if err != nil || !c.Changed || !accountOn(t, p, store) {
			t.Fatalf("EnableOffline: %+v, %v", c, err)
		}
		got, _ := os.ReadFile(filepath.Join(p, "prefs.js"))
		if want := strings.Replace(prefs, `a\"b.offline_download", false`, `a\"b.offline_download", true`, 1); string(got) != want {
			t.Fatalf("prefs.js:\n%s\nwant\n%s", got, want)
		}
	})

	bs := string(rune(0x5c))
	for name, line := range map[string]string{
		`\n in a name`:            `user_pref("mail.z` + bs + `nz", 1);`,
		`\r in a name`:            `user_pref("mail.z` + bs + `rz", 1);`,
		`\x in a name`:            `user_pref("mail.z` + bs + `x7a", 1);`,
		`\u in a name (R3-02)`:    `user_pref("mail.server.server1.offline` + bs + `u005fdownload", true);`,
		`\t in a name`:            `user_pref("mail.z` + bs + `tz", 1);`,
		`\' in a name`:            `user_pref("mail.z` + bs + `'z", 1);`,
		`a backslash before a \"`: `user_pref("mail.z` + bs + bs + bs + `z", 1);`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, file := range []string{"prefs.js", "user.js"} {
				p, store := uncertainProfile(t, file, line+"\n", false)
				shapeRefused(t, p, store, file, whatNameBS)
			}
		})
	}
}

// covers: MA-1269, R9
// No accepted name ends its string at a different place than libpref's parser (lib.rs's
// get_string_token, lines 879-985 of the copy at .claude/mozsrc-r4, which reads a name exactly as
// it reads a value: \\ is a backslash, \" a quote, each one character after the backslash and
// never the closing quote) or reads to a different name (the round-5 review's Q1 shape list, for
// names). Each accepted line gives the one statement lib.rs gives, its name and value as lib.rs
// decodes them; each refused line is uncertain. Mozilla's parser was not run: the expected names
// are worked out by hand from that reading.
func TestEscapedNamesAgreeWithLibpref(t *testing.T) {
	bs := string(rune(0x5c))
	q := `"`
	cases := []struct {
		name, line string
		ok         bool
		key, val   string // as lib.rs decodes them; val as written for a bare value
	}{
		{`\\ inside`, `user_pref("a` + bs + bs + `b", 1);`, true, `a` + bs + `b`, "1"},
		{`\" inside`, `user_pref("a` + bs + q + `b", 1);`, true, `a"b`, "1"},
		{`\\ just before the closing quote`, `user_pref("a` + bs + bs + `", true);`, true, `a` + bs, "true"},
		{`\\\\ before the closing quote`, `user_pref("a` + bs + bs + bs + bs + `", true);`, true, `a` + bs + bs, "true"},
		{`\\\" inside`, `user_pref("a` + bs + bs + bs + q + `b", 1);`, true, `a` + bs + `"b`, "1"},
		{`only \\`, `user_pref("` + bs + bs + `", 1);`, true, bs, "1"},
		{`only \"`, `user_pref("` + bs + q + `", 1);`, true, `"`, "1"},
		{"a statement's text in a name", `user_pref("a` + bs + q + `, true); user_pref(` + bs + q + `x` + bs + q + `", false);`, true, `a", true); user_pref("x"`, "false"},
		{`a name ending in \\ then a value with \\`, `user_pref("a` + bs + bs + `", "` + bs + bs + `");`, true, `a` + bs, bs},
		{`the printer`, `user_pref("print.printer_` + bs + bs + bs + bs + `S` + bs + bs + `P.x", false);`, true, `print.printer_` + bs + bs + `S` + bs + `P.x`, "false"},
		// Refused: the name does not end on its line, or holds an escape Thunderbird never writes in one.
		{`\" then no closing quote`, `user_pref("a` + bs + q + `, 1);`, false, "", ""},
		{`\\\" then no closing quote`, `user_pref("a` + bs + bs + bs + q + `, 1);`, false, "", ""},
		{"a backslash ends the line", `user_pref("a` + bs, false, "", ""},
		{`\n`, `user_pref("a` + bs + `n", 1);`, false, "", ""},
		{`\x41`, `user_pref("a` + bs + `x41", 1);`, false, "", ""},
		{`\u0041`, `user_pref("a` + bs + `u0041", 1);`, false, "", ""},
		{"an empty name", `user_pref("", 1);`, false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pt := parsePrefsText(tc.line + "\n")
			if got := pt.uncertainLine < 0; got != tc.ok {
				t.Fatalf("reads with certainty %v, want %v (%s)", got, tc.ok, pt.uncertainWhat)
			}
			if !tc.ok {
				return
			}
			if len(pt.stmts) != 1 {
				t.Fatalf("%d statements, want 1: %+v", len(pt.stmts), pt.stmts)
			}
			s := pt.stmts[0]
			v := s.value
			if strings.HasPrefix(v, `"`) {
				v = unquote(v)
			}
			if s.key != tc.key || v != tc.val {
				t.Fatalf("read %q = %q, want %q = %q (as lib.rs reads it)", s.key, v, tc.key, tc.val)
			}
		})
	}
}

// covers: MA-1270, R9, S10
// A setting MailArchive reads, with a value of a type Thunderbird never writes for it, means
// MailArchive cannot tell what Thunderbird applies (the C0c pass round 5's R5-01): Thunderbird drops
// a user value whose type differs from the setting's built-in default (Preferences.cpp's
// Pref::SetUserValue), so the last statement does not always win. Each case, in prefs.js or
// user.js, with the account off or on, is a HandEditedError from AccountToEnable, CheckOffline and
// EnableOffline, both files byte for byte and no backup. The review's three cases are here: (a) the
// default turned off, then a number for it; (b) the same with a string in user.js; (c) the account
// list as true, with a made-up mail.account.true.server, which made a removed account's leftover
// folder read as owned and on. Positive twin first: a file as Thunderbird writes it (each of these
// settings of its own type) is read and turned on; held as guards, a setting MailArchive does not
// read may hold any type, and a dotted name under mail.server.<key> is not one of these.
func TestASettingOfTheWrongTypeIsNotRead(t *testing.T) {
	p, store := tbProfile(t, tbWritten("\n")+`user_pref("mail.server.default.offline_download", true);`+"\n"+
		`user_pref("mail.server.server1.port", "993");`+"\n"+
		`user_pref("mail.server.server1.spamActionTargetAccount.type", 1);`+"\n"+
		`user_pref("mail.server.default.check_time", true);`+"\n", "")
	if acct, err := AccountToEnable(p, store); err != nil || acct.ServerKey != "server1" || acct.OfflineDownload {
		t.Fatalf("the positive twin: %+v, %v", acct, err)
	}
	if c, err := EnableOffline(p, "server1"); err != nil || !c.Changed || !accountOn(t, p, store) {
		t.Fatalf("the positive twin: %+v, %v", c, err)
	}

	type shape struct{ body, what string }
	cases := map[string]shape{
		"(a) the default off, then a number":        {`user_pref("mail.server.default.offline_download", false);` + "\n" + `user_pref("mail.server.default.offline_download", 0);` + "\n", whatNotBool},
		"(b) the default as a string":               {`user_pref("mail.server.default.offline_download", "yes");` + "\n", whatNotBool},
		"(c) the account list as true":              {`user_pref("mail.account.true.server", "server1");` + "\n" + `user_pref("mail.accountmanager.accounts", true);` + "\n", whatNotText},
		"the server's own setting as a number":      {`user_pref("` + offKey + `", 1);` + "\n", whatNotBool},
		"the server's own setting as a string":      {`user_pref("` + offKey + `", "true");` + "\n", whatNotBool},
		"another server's setting as a string":      {`user_pref("mail.server.server9.offline_download", "false");` + "\n", whatNotBool},
		"the account list as a number":              {`user_pref("mail.accountmanager.accounts", 1);` + "\n", whatNotText},
		"an account's server as a number":           {`user_pref("mail.account.account1.server", 1);` + "\n", whatNotText},
		"an account's server as false":              {`user_pref("mail.account.account7.server", false);` + "\n", whatNotText},
		"the hostname as a number":                  {`user_pref("mail.server.server1.hostname", 0);` + "\n", whatNotText},
		"the type as true":                          {`user_pref("mail.server.server1.type", true);` + "\n", whatNotText},
		"the directory as a number":                 {`user_pref("mail.server.server1.directory", 1);` + "\n", whatNotText},
		"directory-rel as false":                    {`user_pref("mail.server.server1.directory-rel", false);` + "\n", whatNotText},
		"the default's directory-rel as a number":   {`user_pref("mail.server.default.directory-rel", 2);` + "\n", whatNotText},
		"a string first, then the right type after": {`user_pref("` + offKey + `", "x");` + "\n" + off1 + "\n", whatNotBool},
		"a number first, then the right type after": {off1 + "\n" + `user_pref("mail.server.server1.hostname", 5);` + "\n" + `user_pref("mail.server.server1.hostname", "imap.gmail.com");` + "\n", whatNotText},
	}
	for name, tc := range cases {
		for _, file := range []string{"prefs.js", "user.js"} {
			for _, on := range []bool{false, true} {
				t.Run(name+", "+file+map[bool]string{false: ", account off", true: ", account on"}[on], func(t *testing.T) {
					p, store := uncertainProfile(t, file, tc.body, on)
					shapeRefused(t, p, store, file, tc.what)
				})
			}
		}
	}

	// The review's case (c) as it wrote it: a removed account's leftover folder, given a "live"
	// server only through the retyped list, is never given an account.
	t.Run("(c) the leftover folder is not owned", func(t *testing.T) {
		prefs := live("server1") + imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") + off1 + "\n" +
			`user_pref("mail.account.true.server", "server2");` + "\n" +
			imapServer("server2", "old.example", "[ProfD]ImapMail/old.example", "") +
			`user_pref("mail.server.server2.offline_download", true);` + "\n" +
			`user_pref("mail.accountmanager.accounts", true);` + "\n"
		p := profileWith(t, prefs, "imap.gmail.com", "old.example")
		acct, err := FindAccountForStore(p, filepath.Join(p, "ImapMail", "old.example"))
		var h *HandEditedError
		if acct != nil || !errors.As(err, &h) || h.What != whatNotText {
			t.Fatalf("the leftover folder: %+v, %v; want no account and a HandEditedError", acct, err)
		}
	})
}

// covers: MA-1271, R12
// Thunderbird never writes user.js, so a line there MailArchive cannot read is never said to be
// "changed outside Thunderbird" (the lead's words on the round-5 review's N1): the command line says
// "user.js has a line MailArchive cannot read with certainty (user.js is a file people or add-ons
// write by hand)", then the line and what is on it; the wizard says it is a file people or add-ons
// write by hand, without naming it. prefs.js keeps its words. Positive twin first: the same line in
// prefs.js gives "Thunderbird never writes such a line, so the file was changed outside
// Thunderbird". Both still give the way out and change nothing.
func TestUserJSIsNeverSaidToBeChangedOutsideThunderbird(t *testing.T) {
	line := "/* set by IT */\n"
	p, store := uncertainProfile(t, "prefs.js", line, false)
	_, err := AccountToEnable(p, store)
	if err == nil || !strings.Contains(err.Error(), "prefs.js line 7 "+whatComment+". Thunderbird never writes such a line, so the file was changed outside Thunderbird (by hand or by another program)") {
		t.Fatalf("the positive twin, prefs.js: %v", err)
	}
	if plain, _ := PlainWords(err); !strings.HasPrefix(plain, "Thunderbird's settings file was changed outside Thunderbird, so MailArchive could not be sure what it says and did not change any Thunderbird setting.") {
		t.Fatalf("the positive twin, the wizard: %s", plain)
	}

	p, store = uncertainProfile(t, "user.js", "// mine\n"+line, false)
	_, err = AccountToEnable(p, store)
	want := "-enable-offline changed nothing, and nothing was exported: user.js has a line MailArchive cannot read with certainty (user.js is a file people or add-ons write by hand). Its line 2 " +
		whatComment + ", so MailArchive cannot be sure what Thunderbird applies for " + offKey + ". In Thunderbird, open Account Settings"
	refusedNaming(t, err, func() bool { return absent(filepath.Join(p, "prefs.js.mailarchive-bak")) }, want, "Then run the same command again without -enable-offline.")
	if strings.Contains(err.Error(), "changed outside Thunderbird") {
		t.Errorf("user.js said to be changed outside Thunderbird: %s", err)
	}
	plain, _ := PlainWords(err)
	if !strings.HasPrefix(plain, "One of Thunderbird's settings files has a line MailArchive cannot read with certainty. It is a file that people or add-ons write by hand, not Thunderbird, so MailArchive could not be sure what it says and did not change any Thunderbird setting.\n\n"+stepsIntro) ||
		strings.Contains(plain, "changed outside Thunderbird") || strings.Contains(plain, "user.js") {
		t.Errorf("the wizard's words for user.js:\n%s", plain)
	}
}
