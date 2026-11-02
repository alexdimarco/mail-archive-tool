package schedule

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"unicode/utf16"
)

// The scheduler half of a delete's discovery and of its step 2 (docs/design-archive-delete.md
// §5.1, §5.3 step 2; MA-973): what an entry this tool installed backs up, read back from the
// host where the host keeps it, and the Windows side-files an absent entry may leave.

// InstalledOut is the -out the entry name backs up, read back from where this host keeps it:
// on Windows the task's wrapper (CmdWrapper's run line) or, without one, the -out in the
// Arguments of the XML definition written beside it; elsewhere but macOS the managed crontab
// block. known is false when nothing on this host says: macOS cannot read a launchd entry
// back (UJ-6), a GUI task runs a -job file whose -out the side-files do not hold, and the
// Desktop's scheduled capture reads its folder from the settings at run time.
func InstalledOut(name string) (out string, known bool) { return installedOutOn(name, runtime.GOOS) }

func installedOutOn(name, goos string) (string, bool) {
	switch goos {
	case "darwin":
		return "", false
	case "windows":
		wrap := DefaultWrapperPath(name)
		if b, err := readSideFile(wrap); err == nil {
			if o := wrapperOutOf(string(b)); o != "" {
				return o, true
			}
		}
		if b, err := readSideFile(SchtasksXMLPath(wrap)); err == nil {
			if o := xmlArgumentsOut(b); o != "" {
				return o, true
			}
		}
		return "", false
	default:
		if o := cronBlockOut(readCrontab(), CronMarker(name)); o != "" {
			return o, true
		}
		return "", false
	}
}

// sideFileCap bounds a side-file read: a wrapper or a task definition is a few KiB.
const sideFileCap = 256 << 10

// readSideFile reads a regular side-file, never through a link, at most sideFileCap bytes.
func readSideFile(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > sideFileCap {
		return nil, fs.ErrInvalid
	}
	return os.ReadFile(path)
}

// xmlArgumentsOut is the -out in the Arguments of a task definition SchtasksXML wrote
// (UTF-16LE with a byte-order mark, or UTF-8), split as the receiving program splits its
// command line (the Microsoft C-runtime rules winQuote quotes for); "" when it has none.
func xmlArgumentsOut(b []byte) string {
	var doc struct {
		Actions struct {
			Exec struct {
				Arguments string `xml:"Arguments"`
			} `xml:"Exec"`
		} `xml:"Actions"`
	}
	text := decodeUTF16(b)
	// The declaration says UTF-16, which encoding/xml does not read; the text is decoded.
	if i := strings.Index(text, "?>"); strings.HasPrefix(strings.TrimSpace(text), "<?xml") && i >= 0 {
		text = text[i+2:]
	}
	if err := xml.Unmarshal([]byte(text), &doc); err != nil {
		return ""
	}
	return argOut(SplitWinArgs(doc.Actions.Exec.Arguments))
}

// decodeUTF16 decodes UTF-16LE text with a byte-order mark; anything else is taken as UTF-8.
func decodeUTF16(b []byte) string {
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xFE {
		return string(b)
	}
	b = b[2:]
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

// argOut is the value of -out (or -out=V, either dash count) in args, "" when none.
func argOut(args []string) string {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		f := strings.TrimLeft(a, "-")
		if f == "out" && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(f, "out="); ok {
			return v
		}
	}
	return ""
}

// SplitWinArgs splits a command line by the Microsoft C-runtime rules (CommandLineToArgv's,
// the inverse of winQuote): white space separates arguments outside quotes; 2n backslashes
// before a quote are n backslashes and the quote toggles quoting, 2n+1 are n and a literal
// quote; backslashes not before a quote are literal.
func SplitWinArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, have := false, false
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
				i--
			}
			have = true
		case c == '"':
			inQuote = !inQuote
			have = true
		case (c == ' ' || c == '\t') && !inQuote:
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteByte(c)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

// SchtasksSaysMissing reports, on Windows, that `schtasks /Query /TN name` failed saying the
// task does not exist (schtasksSaysMissing: the system's own words, so on a Windows in
// another language it is false). Elsewhere it is false.
func SchtasksSaysMissing(name string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	out, err := schtasksRun(SchtasksQueryArgv(name)...)
	return err != nil && schtasksSaysMissing(out)
}

// SideFilesExist reports, on Windows, that the entry's <name>.cmd or <name>.xml is still
// beside the wrapper folder (DefaultWrapperPath); elsewhere there are none.
func SideFilesExist(name string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	wrap := DefaultWrapperPath(name)
	for _, p := range []string{wrap, SchtasksXMLPath(wrap)} {
		if _, err := os.Lstat(p); err == nil || !errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}
	return false
}

// RemoveSideFiles removes the entry's Windows side-files (removeSchtasksSideFiles), either
// already gone being no error; elsewhere it does nothing.
func RemoveSideFiles(name string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	return removeSchtasksSideFiles(Spec{Name: name, WrapperPath: DefaultWrapperPath(name)})
}

// RemoveLiveEntryNamed is RemoveLiveEntry for the entry name: the host's entry (and, on
// Windows, its side-files), never the archive's descriptor.
func RemoveLiveEntryNamed(name string) error { return RemoveLiveEntryNamedOn(name, runtime.GOOS) }

// RemoveLiveEntryNamedOn is RemoveLiveEntryNamed on the scheduler goos selects ("" is this
// computer's): a test passes "linux" to remove through cron on any computer.
func RemoveLiveEntryNamedOn(name, goos string) error {
	if goos == "" {
		goos = runtime.GOOS
	}
	return removeLiveEntryOn(Spec{Name: name, WrapperPath: DefaultWrapperPath(name)}, goos)
}

// RemoveByHand says how a person removes the entry name with goos's own tools, for a removal
// MailArchive could not make (a delete's step 2, Remove from list; the 1.5.3 friction review's
// H1: a stop there must say how to go on).
func RemoveByHand(name, goos string) string {
	switch goos {
	case "windows":
		return fmt.Sprintf("in Task Scheduler, or with: schtasks /Delete /TN %s /F", winQuote(name))
	case "darwin":
		return fmt.Sprintf("with: launchctl unload %s, then delete that file", shellJoin([]string{launchdPlistPath(name)}))
	}
	return fmt.Sprintf("with crontab -e: delete the lines under %q", CronMarker(name))
}

// SchedulerName is goos's scheduler as a person knows it.
func SchedulerName(goos string) string {
	switch goos {
	case "windows":
		return "Task Scheduler"
	case "darwin":
		return "launchd"
	}
	return "cron"
}
