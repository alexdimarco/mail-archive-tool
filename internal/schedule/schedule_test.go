package schedule

import (
	"strings"
	"testing"
	"time"
)

func sampleSpec(iv Interval, at string) Spec {
	return Spec{
		Name:     DefaultName,
		Interval: iv,
		At:       at,
		Exe:      "/usr/local/bin/mailarchive",
		Args:     []string{"-out", "/data/backup", "-mode", "incremental", "-auto", "-log", "/data/backup/mailarchive-backup.log"},
		Log:      "/data/backup/mailarchive-backup.log",
	}
}

// covers: MA-45, R14, S14
// The cron line carries the interval's schedule fields and the executable +
// job flags including the job's own -log; nothing is shell-redirected, so a
// crash reaches cron's mail; the block prepends the marker.
func TestCronLineGeneration(t *testing.T) {
	daily, err := CronLine(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(daily, "0 2 * * * ") {
		t.Errorf("daily schedule fields wrong: %q", daily)
	}
	for _, want := range []string{"/usr/local/bin/mailarchive", "-out /data/backup", "-mode incremental", "-auto", "-log /data/backup/mailarchive-backup.log"} {
		if !strings.Contains(daily, want) {
			t.Errorf("cron line missing %q: %q", want, daily)
		}
	}
	if strings.Contains(daily, ">>") || strings.Contains(daily, "2>&1") {
		t.Errorf("cron line must not shell-redirect (the job logs itself; crashes go to cron mail): %q", daily)
	}

	hourly, err := CronLine(sampleSpec(Hourly, "02:15"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hourly, "15 * * * * ") {
		t.Errorf("hourly schedule fields wrong: %q", hourly)
	}

	weekly, err := CronLine(sampleSpec(Weekly, "03:30"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(weekly, "30 3 * * 0 ") {
		t.Errorf("weekly schedule fields wrong: %q", weekly)
	}

	block, err := CronBlock(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(block, CronMarker(DefaultName)+"\n") {
		t.Errorf("block missing leading marker: %q", block)
	}

	if _, err := CronLine(sampleSpec(Daily, "25:00")); err == nil {
		t.Error("expected error for out-of-range hour")
	}
}

// covers: MA-46, R14, S14
// The launchd plist names the label, lists the executable + args as
// ProgramArguments, and encodes the cadence as StartCalendarInterval (Minute
// always; Hour for daily/weekly; Weekday only for weekly).
func TestLaunchdPlistGeneration(t *testing.T) {
	daily, err := LaunchdPlist(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>" + LaunchdLabel(DefaultName) + "</string>",
		"<key>ProgramArguments</key>",
		"<string>/usr/local/bin/mailarchive</string>",
		"<string>-out</string>",
		"<string>/data/backup</string>",
		"<key>StartCalendarInterval</key>",
		"<key>Minute</key>",
		"<key>Hour</key>",
		"<key>StandardErrorPath</key>",
		"<string>/data/backup/mailarchive-backup.stderr.log</string>",
	} {
		if !strings.Contains(daily, want) {
			t.Errorf("plist missing %q", want)
		}
	}
	if strings.Contains(daily, "<key>Weekday</key>") {
		t.Error("daily plist must not set Weekday")
	}

	weekly, err := LaunchdPlist(sampleSpec(Weekly, "03:30"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(weekly, "<key>Weekday</key>") {
		t.Error("weekly plist must set Weekday")
	}

	hourly, err := LaunchdPlist(sampleSpec(Hourly, "00:15"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hourly, "<key>Hour</key>") {
		t.Error("hourly plist must not pin an Hour")
	}
	if !strings.Contains(hourly, "<key>Minute</key>") {
		t.Error("hourly plist must set Minute")
	}
}

// covers: MA-47, R14, S14
// The schtasks command registers the task from its XML definition — /Create /TN
// /XML /F — then confirms it with /Query /TN; the schedule class/time and /TR
// run string are gone (the definition carries them). The install argv Install
// hands schtasks is exactly that /Create pair plus the /Query, and the delete
// command reverses it by name.
func TestSchtasksCommandGeneration(t *testing.T) {
	spec := sampleSpec(Daily, "02:00")
	spec.WrapperPath = `C:\Users\Alex\AppData\Local\mailarchive\mailarchive-backup.cmd`
	xmlPath := SchtasksXMLPath(spec.WrapperPath)

	cmd, err := SchtasksCreateCmd(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`/Create`, `/TN "mailarchive-backup"`, `/XML `, `/F`, `/Query /TN "mailarchive-backup"`, xmlPath} {
		if !strings.Contains(cmd, want) {
			t.Errorf("schtasks command missing %q: %q", want, cmd)
		}
	}
	for _, gone := range []string{`/TR `, `/SC `, `/ST `, `/D SUN`} {
		if strings.Contains(cmd, gone) {
			t.Errorf("schtasks command still carries the retired %q: %q", gone, cmd)
		}
	}

	argv, err := SchtasksCreateArgv(spec)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/Create", "/TN", "mailarchive-backup", "/XML", xmlPath, "/F"}; !equalStrings(argv, want) {
		t.Errorf("create argv = %q, want %q", argv, want)
	}
	if q := SchtasksQueryArgv(spec.Name); !equalStrings(q, []string{"/Query", "/TN", "mailarchive-backup"}) {
		t.Errorf("query argv = %q", q)
	}

	del := SchtasksDeleteCmd(DefaultName)
	if !strings.Contains(del, "/Delete") || !strings.Contains(del, `/TN "mailarchive-backup"`) {
		t.Errorf("delete command wrong: %q", del)
	}
}

// covers: MA-48, R14, S14
// UpsertCronBlock is idempotent: applying the same spec's block twice leaves
// exactly one marker and one command line, and preserves unrelated crontab lines.
func TestUpsertCronBlockIdempotent(t *testing.T) {
	existing := "0 5 * * * /usr/bin/other-job\n"
	block, err := CronBlock(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	marker := CronMarker(DefaultName)

	once := UpsertCronBlock(existing, marker, block)
	twice := UpsertCronBlock(once, marker, block)

	if once != twice {
		t.Errorf("upsert not idempotent:\n once=%q\n twice=%q", once, twice)
	}
	if n := strings.Count(twice, marker); n != 1 {
		t.Errorf("marker appears %d times, want 1:\n%s", n, twice)
	}
	if !strings.Contains(twice, "/usr/bin/other-job") {
		t.Error("upsert dropped an unrelated crontab line")
	}
	if !strings.Contains(twice, "0 2 * * * ") {
		t.Error("upsert did not contain our scheduled command")
	}
}

// covers: MA-49, R14, S14
// RemoveCronBlock cleanly reverses an install: after upsert-then-remove the
// crontab is byte-for-byte the original, and unrelated lines survive.
func TestRemoveCronBlockReverses(t *testing.T) {
	existing := "0 5 * * * /usr/bin/other-job\n"
	block, err := CronBlock(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	marker := CronMarker(DefaultName)

	installed := UpsertCronBlock(existing, marker, block)
	if !strings.Contains(installed, marker) {
		t.Fatal("precondition: block not installed")
	}
	removed := RemoveCronBlock(installed, marker)
	if removed != existing {
		t.Errorf("remove did not restore original:\n got=%q\n want=%q", removed, existing)
	}
	if strings.Contains(removed, marker) {
		t.Error("marker survived removal")
	}
	// Removing from an empty crontab is a clean no-op.
	if got := RemoveCronBlock("", marker); got != "" {
		t.Errorf("remove from empty crontab = %q, want empty", got)
	}
}

// covers: MA-65, R14, S14
// Reconciled to the XML install: the path-with-spaces guarantee is now carried
// by XML escaping of <Command>/<Arguments> plus the wrapper's own token quoting.
// A wrapper path with a space, "&" and a non-ASCII rune is escaped in <Command>
// (the raw byte stream carries &amp;, never a bare &) and round-trips through
// xml.Unmarshal to the identical path; with no wrapper the exe lands in
// <Command> and each argument is quoted in <Arguments> so it splits back
// (C-runtime rules) to the exact args; and the wrapper body still quotes every
// token.
func TestSchtasksQuotesPathsWithSpaces(t *testing.T) {
	now := time.Date(2026, 9, 3, 1, 0, 0, 0, time.Local)

	// With a wrapper: the wrapper path (space, "&", non-ASCII) is the <Command>.
	wrapped := Spec{
		Name:        DefaultName,
		Interval:    Daily,
		At:          "02:00",
		Exe:         `C:\Program Files\MailArchive\mailarchive.exe`,
		Args:        []string{"-out", `C:\Users\Alex\OneDrive - Company\Mail Archive`, "-mode", "incremental"},
		Wrapper:     true,
		WrapperPath: `C:\Users\Alex\R&D (2026)\föö\mailarchive-backup.cmd`,
	}
	doc, err := SchtasksXML(wrapped, now)
	if err != nil {
		t.Fatal(err)
	}
	body, err := decodeUTF16LE(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "&amp;") || strings.Contains(body, `R&D`) {
		t.Errorf("the & in the wrapper path was not XML-escaped:\n%s", body)
	}
	got := parseTaskXML(t, doc)
	if got.Actions.Exec.Command != wrapped.WrapperPath {
		t.Errorf("wrapper <Command> did not round-trip:\n got=%q\n want=%q", got.Actions.Exec.Command, wrapped.WrapperPath)
	}
	if got.Actions.Exec.Arguments != "" {
		t.Errorf("a wrapper install carries no <Arguments>: %q", got.Actions.Exec.Arguments)
	}

	// Without a wrapper: exe in <Command>, each argument quoted in <Arguments> so
	// it splits back to the exact program + arguments.
	direct := wrapped
	direct.Wrapper = false
	direct.Args = []string{
		"-out", `C:\Users\Alex\OneDrive - Company\Mail Archive`,
		"-input", `C:\Users\Alex\Documents\Outlook Files\alex.pst`,
		"-auto",
	}
	doc, err = SchtasksXML(direct, now)
	if err != nil {
		t.Fatal(err)
	}
	got = parseTaskXML(t, doc)
	if got.Actions.Exec.Command != direct.Exe {
		t.Errorf("direct <Command> = %q, want the exe %q", got.Actions.Exec.Command, direct.Exe)
	}
	if split := splitWindowsCommandLine(got.Actions.Exec.Arguments); !equalStrings(split, direct.Args) {
		t.Errorf("<Arguments> does not round-trip:\n args=%q\n got=%q\n want=%q", got.Actions.Exec.Arguments, split, direct.Args)
	}

	// The wrapper body still quotes every token (paths with a space and "&"
	// survive cmd.exe as one argument each).
	w := CmdWrapper(wrapped)
	wl := strings.Split(strings.TrimSpace(w), "\r\n")
	run := wl[len(wl)-1]
	for _, want := range []string{
		`"C:\Program Files\MailArchive\mailarchive.exe"`,
		`"-out"`,
		`"C:\Users\Alex\OneDrive - Company\Mail Archive"`,
		`"-mode"`,
		`"incremental"`,
	} {
		if !strings.Contains(run, want) {
			t.Errorf("wrapper run line lacks %s:\n%s", want, run)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// splitWindowsCommandLine applies the Microsoft C-runtime argv rules (the ones
// a Go program's os.Args follow on Windows): whitespace splits outside quotes, a
// double quote toggles quoting, 2n backslashes before a quote yield n literal
// backslashes, and 2n+1 yield n backslashes plus a literal quote. Backslashes
// not followed by a quote are literal (so ordinary paths pass through).
func splitWindowsCommandLine(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote, inArg := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(s) && s[i] == '\\' {
				n++
				i++
			}
			if i < len(s) && s[i] == '"' {
				cur.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					cur.WriteByte('"')
				} else {
					inQuote = !inQuote
				}
			} else {
				cur.WriteString(strings.Repeat(`\`, n))
				i-- // re-process the non-backslash character
			}
			inArg = true
		case c == '"':
			inQuote = !inQuote
			inArg = true
		case (c == ' ' || c == '\t') && !inQuote:
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}

// covers: MA-65, R14, S14
// Round-trip law for the Windows quoting helpers: for any token — spaces, an
// embedded quote, a trailing backslash (the classic `"C:\dir\"` trap), runs of
// backslashes before a quote, empty — splitting winQuote(x) under the C-runtime
// rules yields exactly [x], and nesting winQuote(winQuote(x)) unwraps twice.
func TestWindowsQuoteRoundTrip(t *testing.T) {
	cases := []string{
		`C:\Program Files\MailArchive\mailarchive.exe`,
		`C:\Users\Alex\OneDrive - Company\Mail Archive`,
		`C:\Mail Archive\`,  // trailing backslash before the closing quote
		`C:\odd\\dir\\`,     // runs of backslashes, trailing
		`say "hi"`,          // embedded quotes
		`back\"slash-quote`, // backslash immediately before a quote
		`tab	separated`,     // a tab is whitespace too
		"",                  // empty argument must survive as an empty token
		`plain-token`,
	}
	for _, c := range cases {
		once := winQuote(c)
		if got := splitWindowsCommandLine(once); !equalStrings(got, []string{c}) {
			t.Errorf("winQuote(%q) = %q splits to %q, want [%q]", c, once, got, c)
		}
		twice := winQuote(once)
		inner := splitWindowsCommandLine(twice)
		if len(inner) != 1 || !equalStrings(splitWindowsCommandLine(inner[0]), []string{c}) {
			t.Errorf("nested winQuote(%q) = %q does not unwrap twice: %q", c, twice, inner)
		}
		if a := winArg(c); !equalStrings(splitWindowsCommandLine(a), []string{c}) {
			t.Errorf("winArg(%q) = %q does not round-trip", c, a)
		}
	}
}
