package wizard

import (
	"encoding/xml"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// MA-1286 (the C3 account-name pass's N-01, amended by the confirm pass's R2-01,
// docs/review-thunderbird-completeness-c3-name-adversarial-r2.md). On Linux and the BSDs the
// ncruces/zenity module runs the first of qarma, zenity and matedialog it finds
// (internal/zenutil/run_unix.go). zenity and matedialog set a list dialog's text with
// gtk_label_set_markup(g_strcompress(text)) and have no --no-markup there (src/tree.c), so `&` or
// `<` breaks the label and a backslash then n becomes a line break; qarma shows the same text as
// plain text, so any escaping for GTK shows up literally. No one text shows as written on all three.
// So on Linux and the BSDs no list dialog is given text that can vary: the text is shown first in a
// message dialog, which the module opens with --no-markup (msg_unix.go:8; plain text on all three),
// and the list carries a fixed prompt. These tests model zenity's two steps (g_strcompress as GLib
// documents it, and Pango's markup as the XML GMarkup parses) for the positive twins.

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

// shownByZenityList is what a person sees in zenity's (or matedialog's) list dialog on Linux for text.
func shownByZenityList(text string) (string, error) { return pangoText(gStrcompress(text)) }

// listMarkupChars are the characters a list prompt shown on Linux must not hold: `&` and `<` break
// GTK's markup, `>` is kept out with them, and a backslash is read by g_strcompress. `"` and `'`
// are plain characters in Pango's text (only attribute values treat them specially) and in qarma.
const listMarkupChars = `&<>\`

// healthTextFromARealRecord is the launch health view's text, built as healthCheck builds it from a
// real last-run record that holds names and folders a person or a mail server chose ("AT&T Mail",
// "Sam <sam@work.com>", a name with a backslash then n, "Bills & Receipts", an archive folder with
// "&" and "'" in its path), with the Fixity line's quoted verify command.
func healthTextFromARealRecord(t *testing.T) string {
	t.Helper()
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
	in.Surface = "gui"
	in.HasManifest, in.Messages, in.WithFixity = true, 15, 15 // the Fixity line, with its verify command
	rep := health.Assess(in, time.Now())
	text := "Backup health — " + rep.Posture + "\n\n" + strings.Join(health.Summary(in, rep), "\n")
	verify := "mailarchive verify -out " + health.OutArg(out, "")
	for _, want := range []string{"AT&T Mail", "Sam <sam@work.com>", `sam\nBackup complete.\t"quoted"`, "Bills & Receipts", "Sam & Pat's mail", verify} {
		if !strings.Contains(text, want) {
			t.Fatalf("the health text lacks %q (the fixture does not reach it):\n%s", want, text)
		}
	}
	return text
}

// dialogCall is one dialog the wizard opened, as recordDialogs saw it.
type dialogCall struct {
	kind  string // "message" or "list"
	text  string
	items []string
}

// recordDialogs replaces the message and list dialogs with recorders for the test: the list
// answers pick, and the message dialog answers msgErr.
func recordDialogs(t *testing.T, pick string, msgErr error) *[]dialogCall {
	t.Helper()
	var calls []dialogCall
	oldMsg, oldList := messageDialog, listDialog
	t.Cleanup(func() { messageDialog, listDialog = oldMsg, oldList })
	messageDialog = func(text string, _ ...zenity.Option) error {
		calls = append(calls, dialogCall{kind: "message", text: text})
		return msgErr
	}
	listDialog = func(text string, items []string, _ ...zenity.Option) (string, error) {
		calls = append(calls, dialogCall{kind: "list", text: text, items: append([]string(nil), items...)})
		return pick, nil
	}
	return &calls
}

// covers: MA-1286
// R2-01: on Linux and the BSDs the launch health view's text, from a real record, is shown in a
// message dialog exactly as written (no escaping: the module opens it with --no-markup, plain text in
// zenity, matedialog and qarma), and the list of choices then carries only the fixed prompt. On macOS
// and Windows nothing changes: one list whose text is the health text, as written. A dismissed
// message ends the step as a dismissed list did, with no list shown. Positive twin first: the raw
// text is broken in zenity's list dialog, so it can never be a list's text on Linux.
func TestTheHealthTextIsAMessageAndTheListPromptIsFixedOnLinux(t *testing.T) {
	text := healthTextFromARealRecord(t)
	if shown, err := shownByZenityList(text); err == nil && shown == text {
		t.Fatalf("positive twin: the raw text shows as written in zenity's list, so the model cannot see R2-01")
	}
	choices := []string{"Continue to the wizard", "Repair the scheduled backup", rebuildChoice, "Remove the scheduled backup"}
	for _, goos := range []string{"linux", "freebsd", "openbsd", "netbsd"} {
		calls := recordDialogs(t, choices[3], nil)
		pick, err := textThenListOn(goos, text, healthListPrompt, choices, zenity.Title(appTitle), zenity.DefaultItems(choices[0]))
		if err != nil || pick != choices[3] {
			t.Fatalf("%s: textThenList = %q, %v; want the list's pick", goos, pick, err)
		}
		want := []dialogCall{{kind: "message", text: text}, {kind: "list", text: healthListPrompt, items: choices}}
		if !reflect.DeepEqual(*calls, want) {
			t.Errorf("%s: the dialogs shown were\n%#v\nwant\n%#v", goos, *calls, want)
		}
	}
	for _, goos := range []string{"darwin", "windows"} {
		calls := recordDialogs(t, choices[0], nil)
		if pick, err := textThenListOn(goos, text, healthListPrompt, choices); err != nil || pick != choices[0] {
			t.Fatalf("%s: textThenList = %q, %v", goos, pick, err)
		}
		want := []dialogCall{{kind: "list", text: text, items: choices}}
		if !reflect.DeepEqual(*calls, want) {
			t.Errorf("%s: the dialogs shown were\n%#v\nwant\n%#v (unchanged where no markup is read)", goos, *calls, want)
		}
	}
	calls := recordDialogs(t, choices[2], zenity.ErrCanceled)
	if pick, err := textThenListOn("linux", text, healthListPrompt, choices); !errors.Is(err, zenity.ErrCanceled) || pick != "" {
		t.Errorf("a dismissed message: textThenList = %q, %v; want ErrCanceled", pick, err)
	}
	if len(*calls) != 1 || (*calls)[0].kind != "message" {
		t.Errorf("a dismissed message still showed: %#v", *calls)
	}
}

// covers: MA-1286
// The health list's prompt is fixed, plain words: a constant holding none of & < > " ' \, so it
// shows as written in zenity, matedialog and qarma alike, with nothing to escape.
func TestTheHealthListPromptIsPlainFixedWords(t *testing.T) {
	if strings.TrimSpace(healthListPrompt) == "" {
		t.Fatalf("the health list's prompt is empty")
	}
	if i := strings.IndexAny(healthListPrompt, `&<>"'\`); i >= 0 {
		t.Errorf("the health list's prompt holds %q: %q", healthListPrompt[i], healthListPrompt)
	}
	if shown, err := shownByZenityList(healthListPrompt); err != nil || shown != healthListPrompt {
		t.Errorf("zenity shows the prompt as %q (%v)", shown, err)
	}
}

// fixedPromptFuncs are the functions whose result may be a list dialog's text: each returns the
// same words every time on a given platform, and the scan checks those words on Linux and the BSDs.
var fixedPromptFuncs = map[string]func(goos string) string{
	"scheduleQuestionFor": scheduleQuestionFor,
	"presyncQuestionText": presyncQuestionTextFor,
}

// unixGOOS are the platforms where the ncruces/zenity module runs qarma, zenity or matedialog.
var unixGOOS = []string{"linux", "freebsd", "openbsd", "netbsd"}

// listDialogFaults scans parsed Go files (keyed by path) for list dialogs: zenity.List,
// ListItems, ListMultiple and ListMultipleItems, the listDialog seam, and the prompt of
// textThenList / textThenListOn. It reports each one whose text could vary or would not show as
// written on Linux and the BSDs. The bodies of textThenList and textThenListOn are left to the
// tests that drive them (their list is given the varying text on macOS and Windows only).
func listDialogFaults(fset *token.FileSet, files map[string]*ast.File) (found int, faults []string) {
	consts := map[string]map[string]ast.Expr{} // directory → const name → value
	for p, f := range files {
		dir := filepath.Dir(p)
		if consts[dir] == nil {
			consts[dir] = map[string]ast.Expr{}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if gd, ok := n.(*ast.GenDecl); ok && gd.Tok == token.CONST {
				for _, spec := range gd.Specs {
					vs := spec.(*ast.ValueSpec)
					for i, name := range vs.Names {
						if i < len(vs.Values) {
							consts[dir][name.Name] = vs.Values[i]
						}
					}
				}
			}
			return true
		})
	}
	var value func(dir string, e ast.Expr, depth int) ([]string, bool)
	value = func(dir string, e ast.Expr, depth int) ([]string, bool) {
		if depth > 20 {
			return nil, false
		}
		switch v := e.(type) {
		case *ast.ParenExpr:
			return value(dir, v.X, depth+1)
		case *ast.BasicLit:
			if v.Kind != token.STRING {
				return nil, false
			}
			s, err := strconv.Unquote(v.Value)
			return []string{s}, err == nil
		case *ast.Ident:
			if c, ok := consts[dir][v.Name]; ok {
				return value(dir, c, depth+1)
			}
		case *ast.BinaryExpr:
			if v.Op != token.ADD {
				return nil, false
			}
			l, ok1 := value(dir, v.X, depth+1)
			r, ok2 := value(dir, v.Y, depth+1)
			if !ok1 || !ok2 || len(l) != 1 || len(r) != 1 {
				return nil, false
			}
			return []string{l[0] + r[0]}, true
		case *ast.CallExpr:
			id, ok := v.Fun.(*ast.Ident)
			if !ok || fixedPromptFuncs[id.Name] == nil {
				return nil, false
			}
			for _, a := range v.Args { // only the platform may be passed in
				if sel, ok := a.(*ast.SelectorExpr); !ok || sel.Sel.Name != "GOOS" {
					return nil, false
				}
			}
			var out []string
			for _, goos := range unixGOOS {
				out = append(out, fixedPromptFuncs[id.Name](goos))
			}
			return out, true
		}
		return nil, false
	}
	for p, f := range files {
		dir := filepath.Dir(p)
		skip := map[ast.Node]bool{}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && (fd.Name.Name == "textThenList" || fd.Name.Name == "textThenListOn") && fd.Body != nil {
				skip[fd.Body] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if skip[n] {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			arg := -1
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				if x, ok := fn.X.(*ast.Ident); ok && x.Name == "zenity" {
					switch fn.Sel.Name {
					case "List", "ListItems", "ListMultiple", "ListMultipleItems":
						arg = 0
					}
				}
			case *ast.Ident:
				switch fn.Name {
				case "listDialog":
					arg = 0
				case "textThenList":
					arg = 1
				case "textThenListOn":
					arg = 2
				}
			}
			if arg < 0 {
				return true
			}
			found++
			where := fset.Position(call.Pos()).String()
			if arg >= len(call.Args) {
				faults = append(faults, where+": a list dialog with no text argument")
				return true
			}
			vals, ok := value(dir, call.Args[arg], 0)
			if !ok {
				faults = append(faults, where+": a list dialog's text can vary on Linux; show it in a message dialog (textThenList) and give the list a fixed prompt")
				return true
			}
			for _, s := range vals {
				if i := strings.IndexAny(s, listMarkupChars); i >= 0 {
					faults = append(faults, where+": a list dialog's fixed text holds "+strconv.Quote(s[i:i+1])+", which zenity reads as markup on Linux")
				}
			}
			return true
		})
	}
	return found, faults
}

// covers: MA-1286
// R2-01's wiring scan: no list dialog in the product is given text that can vary on Linux and the
// BSDs. Every zenity list call, the listDialog seam and every textThenList prompt (all non-test Go
// under cmd/ and internal/) takes a string literal, a constant, or a fixed per-platform question,
// and none of those holds & < > or \ on Linux and the BSDs, so nothing needs escaping and qarma
// never shows an escape code. Positive twin first: planted calls that pass a name, the health text,
// a variable prompt or "AT&T" are each caught, and a literal and a constant pass.
func TestNoListDialogIsGivenVariableTextOnLinux(t *testing.T) {
	// The planted lines are indented, so no line of this test starts with "func " (assureblock
	// reads a test's body up to the next such line).
	const planted = `package p
	import "github.com/ncruces/zenity"
	const fixed = "Pick" + " one"
	const ampersand = "AT&T"
	func f(name, posture, prompt string) {
	zenity.List("Choose one", nil)
	zenity.List(fixed, nil)
	zenity.List(name, nil)
	zenity.List("Backup health — "+posture, nil)
	zenity.List(ampersand, nil)
	zenity.ListItems(name, "a")
	listDialog(name, nil)
	textThenList(name, prompt, nil)
	textThenList(name, fixed, nil)
}
`
	fset := token.NewFileSet()
	pf, err := parser.ParseFile(fset, "planted/p.go", planted, 0)
	if err != nil {
		t.Fatal(err)
	}
	found, faults := listDialogFaults(fset, map[string]*ast.File{"planted/p.go": pf})
	if found != 9 || len(faults) != 6 {
		t.Fatalf("positive twin: the scan found %d list dialogs and %d faults, want 9 and 6:\n%s", found, len(faults), strings.Join(faults, "\n"))
	}

	files := map[string]*ast.File{}
	fset = token.NewFileSet()
	for _, root := range []string{"..", filepath.Join("..", "..", "cmd")} {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return err
			}
			files[p] = f
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	found, faults = listDialogFaults(fset, files)
	for _, f := range faults {
		t.Error(f)
	}
	if found < 11 {
		t.Fatalf("positive twin: the scan found %d list dialogs in the product, want at least 11", found)
	}
}
