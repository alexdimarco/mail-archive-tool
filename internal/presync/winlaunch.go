package presync

import "strings"

// The Windows start of Thunderbird as a pure description (design-presync-attended
// §2.7, MA-923): what CreateProcessW is asked for, built here and started by
// winStart (quit_windows.go). Untagged, so the Linux test gate checks it.
//
// Lab L-A10 (2026-10-31, Windows 11 on ARM, amd64 emulated, Thunderbird
// 153.6.0esr) showed that Thunderbird honours a STARTUPINFO show state of
// SW_SHOWMINNOACTIVE: started that way its window was minimized from the first
// 100 ms sample in every trial and never took the keyboard, even after more than
// 200 s without input, when the earlier start (exec.Cmd, which can ask Windows
// only for SW_HIDE, a state Thunderbird ignores, lab L-W4) showed the window and
// took the foreground every time. So every start, scheduled or attended, asks for
// "minimized, do not activate"; the backstop minimize (minimizeMain) stays for a
// window that shows anyway.

const (
	// wCreateBreakawayFromJob is CREATE_BREAKAWAY_FROM_JOB: Thunderbird runs outside
	// the job object of whatever started this program, so a Task Scheduler task's
	// End, a logoff or a terminal's teardown cannot hard-kill it mid-write (PC-W6).
	wCreateBreakawayFromJob = 0x01000000
	// wStartfUseShowWindow is STARTF_USESHOWWINDOW: Windows applies the STARTUPINFO
	// show state to the program's first window.
	wStartfUseShowWindow = 0x00000001
)

// winLaunch is one Thunderbird start, field by field as CreateProcessW takes it.
type winLaunch struct {
	app     string // lpApplicationName: the resolved thunderbird.exe, never searched for
	cmdLine string // lpCommandLine: every argument quoted as Go's exec quotes it
	dir     string // lpCurrentDirectory: Thunderbird's install folder ("" keeps ours)
	flags   uint32 // dwCreationFlags
	siFlags uint32 // STARTUPINFO.dwFlags
	show    uint16 // STARTUPINFO.wShowWindow
	inherit bool   // bInheritHandles: always false, Thunderbird gets none of our handles
}

// buildLaunchCmd builds the Thunderbird start (design §2, §6; design-presync-attended
// §2.7, MA-923): `thunderbird.exe -profile <profile>`, minimized and not activated
// (STARTF_USESHOWWINDOW with SW_SHOWMINNOACTIVE), OUT of the starting program's job
// object (CREATE_BREAKAWAY_FROM_JOB, PC-W6; breakaway forfeits nothing the session
// uses, since the process is re-found by the scan+`-profile` identity, §3), with no
// handle inherited, in Thunderbird's own install folder (C-65, so a Thunderbird left
// open for hours never pins the folder Mail Archive was started in). The command
// line is the one exec.Command built before (winCommandLine), so the `-profile`
// identity reads the same; the profile is always one quoted argument, whatever
// spaces, quotes or trailing backslash its path has. A pure builder, so every field
// is golden-tested.
func buildLaunchCmd(exe, profile string) winLaunch {
	return winLaunch{
		app:     exe,
		cmdLine: winCommandLine(exe, "-profile", profile),
		dir:     winInstallDir(exe),
		flags:   wCreateBreakawayFromJob,
		siFlags: wStartfUseShowWindow,
		show:    swShowMinNoActive,
	}
}

// winCommandLine joins arguments into one Windows command line exactly as Go's
// os/exec does (syscall's makeCmdLine): each argument escaped by winEscapeArg,
// separated by one space.
func winCommandLine(args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = winEscapeArg(a)
	}
	return strings.Join(quoted, " ")
}

// winEscapeArg quotes one argument by the rules Windows programs use to split a
// command line (CommandLineToArgvW), byte for byte as Go's syscall.EscapeArg does
// (TestWinEscapeArgMatchesGo checks it on Windows): an empty argument is `""`; an
// argument with no quote, backslash, space or tab is left as it is; otherwise each
// quote gets a backslash, and the backslashes just before it are doubled; an
// argument with a space or tab is wrapped in quotes, with the backslashes at its
// end doubled so they do not escape the closing quote.
func winEscapeArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, "\"\\ \t") {
		return s
	}
	wrap := strings.ContainsAny(s, " \t")
	var b strings.Builder
	if wrap {
		b.WriteByte('"')
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes+1))
			slashes = 0
		default:
			slashes = 0
		}
		b.WriteByte(s[i])
	}
	if wrap {
		b.WriteString(strings.Repeat(`\`, slashes))
		b.WriteByte('"')
	}
	return b.String()
}
