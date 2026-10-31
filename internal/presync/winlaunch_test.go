package presync

import "testing"

// The Windows start's pure builder (design-presync-attended §2.7, MA-923; lab L-A10
// 2026-10-31), checked on every platform. The CreateProcess call itself, its handles
// and Windows' own reading of the command line are tested in start_windows_test.go.

const (
	tbExe     = `C:\Program Files\Mozilla Thunderbird\thunderbird.exe`
	tbExeLine = `"C:\Program Files\Mozilla Thunderbird\thunderbird.exe"`
)

// covers: MA-923
// Every field of the start, golden: the resolved thunderbird.exe as the program
// (never searched for); minimized and not activated (STARTF_USESHOWWINDOW, and
// nothing else, with SW_SHOWMINNOACTIVE 7); CREATE_BREAKAWAY_FROM_JOB and no other
// creation flag (no CREATE_NO_WINDOW, no new console); no handle inherited;
// Thunderbird's install folder as the working folder (C-65); and the command line
// exactly as exec.Command built it before, so the `-profile` identity reads the same.
func TestWinLaunchGolden(t *testing.T) {
	const prof = `C:\Users\First Last\AppData\Roaming\Thunderbird\Profiles\abc.default`
	got := buildLaunchCmd(tbExe, prof)
	want := winLaunch{
		app:     tbExe,
		cmdLine: tbExeLine + ` -profile "C:\Users\First Last\AppData\Roaming\Thunderbird\Profiles\abc.default"`,
		dir:     `C:\Program Files\Mozilla Thunderbird`,
		flags:   0x01000000, // CREATE_BREAKAWAY_FROM_JOB
		siFlags: 0x00000001, // STARTF_USESHOWWINDOW
		show:    7,          // SW_SHOWMINNOACTIVE
		inherit: false,
	}
	if got != want {
		t.Errorf("buildLaunchCmd =\n %+v\nwant\n %+v", got, want)
	}
	if wCreateBreakawayFromJob != 0x01000000 || wStartfUseShowWindow != 0x00000001 || swShowMinNoActive != 7 {
		t.Errorf("Win32 values %#x/%#x/%d, want CREATE_BREAKAWAY_FROM_JOB 0x1000000, STARTF_USESHOWWINDOW 0x1, SW_SHOWMINNOACTIVE 7",
			wCreateBreakawayFromJob, wStartfUseShowWindow, swShowMinNoActive)
	}
}

// covers: MA-923
// The profile is always ONE argument, whatever its path holds (MA-923; the quoting
// of Go's syscall.EscapeArg): spaces wrap it in quotes; a trailing backslash inside
// quotes is doubled so it cannot escape the closing quote; a quote gets a backslash
// and the backslashes before it are doubled; so a profile path cannot end the
// argument early and add arguments of its own. Positive twin first: a plain path is
// left exactly as it is.
func TestWinLaunchCommandLineQuoting(t *testing.T) {
	for _, c := range []struct{ what, profile, want string }{
		{"a plain path, left as it is", `C:\p`, `C:\p`},
		{"spaces", `C:\Users\First Last\Profiles\x.default`, `"C:\Users\First Last\Profiles\x.default"`},
		{"a trailing backslash, no space", `C:\p\`, `C:\p\`},
		{"a trailing backslash with a space", `C:\Users\First Last\p\`, `"C:\Users\First Last\p\\"`},
		{"two trailing backslashes with a space", `C:\a b\\`, `"C:\a b\\\\"`},
		{"a quote", `C:\a"b`, `C:\a\"b`},
		{"a quote after backslashes", `C:\a\\"b`, `C:\a\\\\\"b`},
		{"a quote, a space and a trailing backslash", `C:\x y\a"b\`, `"C:\x y\a\"b\\"`},
		{"an attempt to add an argument", `C:\p" -foo "x`, `"C:\p\" -foo \"x"`},
		{"a tab", "C:\\a\tb", "\"C:\\a\tb\""},
		{"empty", ``, `""`},
	} {
		l := buildLaunchCmd(tbExe, c.profile)
		if want := tbExeLine + " -profile " + c.want; l.cmdLine != want {
			t.Errorf("%s: command line\n %s\nwant\n %s", c.what, l.cmdLine, want)
		}
	}
	// The program itself is quoted the same way (a folder with a space, the usual case).
	if got := winCommandLine(`C:\Tb\thunderbird.exe`, "-profile", `C:\p`); got != `C:\Tb\thunderbird.exe -profile C:\p` {
		t.Errorf("a program path with no space: %s", got)
	}
}
