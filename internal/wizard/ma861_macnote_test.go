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
// and what to do before relying on the schedule.
func TestMA861_TheMacNoteIsTheOneSharedConstant(t *testing.T) {
	for _, want := range []string{"macOS asks once", "Run-now button", "choose Allow"} {
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
			if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, "macOS asks once") {
				t.Errorf("wizard.go keeps its own copy of the Mac note: %q", s)
			}
		}
		return true
	})
}
