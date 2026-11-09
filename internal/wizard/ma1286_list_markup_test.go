package wizard

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// The C3 account-name pass's N-01 (docs/review-thunderbird-completeness-c3-name-adversarial.md):
// on Linux and the BSDs, zenity's list dialog sets its text with
// gtk_label_set_markup(g_strcompress(text)) (GNOME zenity src/tree.c, read 2026-11-08 and again
// 2026-11-09) and has no --no-markup, and ncruces/zenity v0.10.12 passes the text as is
// (list_unix.go:8). So `&` or `<` in a name, a folder or a path broke the whole label, and a
// backslash then `n` became a line break. These tests model those two steps: g_strcompress as GLib
// documents it, and Pango's markup as the XML GMarkup parses (entities, no bare `&` or `<`).

// gStrcompress is GLib's g_strcompress: a backslash then b, f, n, r, t or v is that control
// character, a backslash then up to three octal digits is that byte, a backslash then any other
// character is that character, and a lone backslash at the end ends the text.
func gStrcompress(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i == len(s) {
			break
		}
		switch c := s[i]; {
		case c >= '0' && c <= '7':
			v, n := 0, 0
			for ; i < len(s) && n < 3 && s[i] >= '0' && s[i] <= '7'; i, n = i+1, n+1 {
				v = v*8 + int(s[i]-'0')
			}
			i--
			b.WriteByte(byte(v))
		case strings.IndexByte("bfnrtv", c) >= 0:
			b.WriteByte("\b\f\n\r\t\v"[strings.IndexByte("bfnrtv", c)])
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// pangoText is the text a Pango markup string shows, or an error where GTK would refuse the whole
// label's markup. Any tag is reported too: no text of ours means one.
func pangoText(markup string) (string, error) {
	d := xml.NewDecoder(strings.NewReader("<markup>" + markup + "</markup>"))
	d.Strict = true
	var b strings.Builder
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return b.String(), nil
		}
		if err != nil {
			return "", err
		}
		switch v := tok.(type) {
		case xml.CharData:
			b.Write(v)
		case xml.StartElement:
			if v.Name.Local != "markup" {
				return "", errors.New("a tag: <" + v.Name.Local + ">")
			}
		}
	}
}

// shownByZenityList is what a person sees in zenity's list dialog on Linux for text.
func shownByZenityList(text string) (string, error) { return pangoText(gStrcompress(text)) }

// covers: MA-1286
// N-01: the launch health view's text, built from a real record that holds names and folders a
// person or a mail server chose ("AT&T Mail", "Sam <sam@work.com>", a name with a backslash then
// n, a folder "Bills & Receipts", an archive folder with "&" in its path), is shown on Linux exactly
// as it is, with no markup and no backslash sequence; on macOS and Windows (no markup there) it is
// passed unchanged. Positive twin first: the text as it was passed before is broken markup on
// Linux, so the model above sees the fault.
func TestTheHealthListShowsNamesAsWrittenOnLinux(t *testing.T) {
	out := filepath.Join(t.TempDir(), "Sam & Pat's mail")
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lr := state.LastRun{Status: state.RunOK, Started: now.Add(-time.Minute), Finished: now, Thunderbird: []state.TBAccount{
		{Account: "imap.att.example", Name: "AT&T Mail", Verdict: "behind", OnDisk: 3, Listed: 40,
			Folders: []state.TBFolder{{Folder: "Bills & Receipts", Listed: 30, OnDisk: 1}}},
		{Account: "imap.work.example", Name: "Sam <sam@work.com>", Verdict: "behind", OnDisk: 5, Listed: 50},
		{Account: "imap.n.example", Name: `sam\nBackup complete.\t"quoted"`, Verdict: "behind", OnDisk: 7, Listed: 70},
	}}
	if err := state.WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	in := health.Gather(out, "")
	rep := health.Assess(in, time.Now())
	text := "Backup health — " + rep.Posture + "\n\n" + strings.Join(health.Summary(in, rep), "\n")
	for _, want := range []string{"AT&T Mail", "Sam <sam@work.com>", `sam\nBackup complete.\t"quoted"`, "Bills & Receipts", "Sam & Pat's mail"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the health text lacks %q (the fixture does not reach it):\n%s", want, text)
		}
	}
	if shown, err := shownByZenityList(text); err == nil && shown == text {
		t.Fatalf("positive twin: the raw text shows as written on Linux, so the model cannot see N-01")
	}
	shown, err := shownByZenityList(listTextFor("linux", text))
	if err != nil || shown != text {
		t.Errorf("Linux shows the health text changed (%v):\n got %q\nwant %q", err, shown, text)
	}
	for _, goos := range []string{"freebsd", "openbsd", "netbsd"} {
		if shown, err := shownByZenityList(listTextFor(goos, text)); err != nil || shown != text {
			t.Errorf("%s: %v %q", goos, err, shown)
		}
	}
	for _, goos := range []string{"darwin", "windows"} {
		if got := listTextFor(goos, text); got != text {
			t.Errorf("%s: the text is changed where no markup is read: %q", goos, got)
		}
	}
}

// covers: MA-1286
// Every zenity list dialog in the product passes its text through listText, so no text that
// reaches one, now or later, is read as markup on Linux. Positive twin first: the scan finds the
// list dialogs it guards.
func TestEveryListDialogTextIsMarkupSafe(t *testing.T) {
	call := regexp.MustCompile(`zenity\.List(Multiple)?(Items)?\(\s*`)
	roots := []string{"..", filepath.Join("..", "..", "cmd")}
	found := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			src := string(data)
			for _, m := range call.FindAllStringIndex(src, -1) {
				found++
				if !strings.HasPrefix(src[m[1]:], "listText(") {
					line := strings.Count(src[:m[0]], "\n") + 1
					t.Errorf("%s:%d: a zenity list dialog's text is not passed through listText", p, line)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if found < 10 {
		t.Fatalf("positive twin: the scan found %d list dialogs", found)
	}
}
