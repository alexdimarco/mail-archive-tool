package wizard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"mail-archive-tool/internal/presync"
)

// covers: MA-861, R37, X16
// The macOS Automation note lives in one place, presync.MacAutomationNote, which the wizard's sync
// question and MailArchive Desktop's Settings card both show (design-desktop-settings-card section
// 7): on macOS the question carries it, elsewhere it does not, and the wizard's source holds no
// copy of its own words that could drift. Positive twin first: the constant says what macOS asks
// and where to answer it. (Amended by design-presync-attended slice AS4, T10, C-52: the note no
// longer sends the person to a Run now, which never closes Thunderbird; MA-915 pins that.)
func TestMA861_TheMacNoteIsTheOneSharedConstant(t *testing.T) {
	for _, want := range []string{"macOS may ask for permission", "Privacy & Security > Automation", "allow the entry listed there"} {
		if !strings.Contains(presync.MacAutomationNote, want) {
			t.Fatalf("the positive twin: presync.MacAutomationNote lacks %q", want)
		}
	}
	q := presyncQuestionText()
	if has := strings.Contains(q, presync.MacAutomationNote); has != (runtime.GOOS == "darwin") {
		t.Errorf("on %s the wizard's question carries the Mac note: %v", runtime.GOOS, has)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "wizard.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, "macOS may ask for permission") {
				t.Errorf("wizard.go keeps its own copy of the Mac note: %q", s)
			}
		}
		return true
	})
}
