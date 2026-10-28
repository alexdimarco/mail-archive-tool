package desktop

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// oldCardWords are the names of the old backup card and its removed button (design-desktop-
// settings-card section 12 A5, X11): the card is "Scheduled backup", and how often, the time and
// the sync are changed in Settings.
var oldCardWords = []string{"weekly backup card", "install / update", "set up on this card"}

// oldCardWordsIn names every string literal in the Go source src that names the old card or
// button.
func oldCardWordsIn(name string, src []byte) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	if err != nil {
		return nil, err
	}
	var found []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		for _, w := range oldCardWords {
			if strings.Contains(strings.ToLower(s), w) {
				found = append(found, name+": "+w)
			}
		}
		return true
	})
	return found, nil
}

// covers: MA-868, R37
// Structural: no Desktop text, tooltip, health remedy or README line names the "Weekly backup
// card", "Install / update" or "set up on this card" (X11). The checker reads every string in the
// Desktop's and the health report's source (templates, scripts, replies and remedies) and the
// README. Positive twin first: a planted source naming the old card is caught, as is each phrase.
func TestNoTextNamesTheOldBackupCard(t *testing.T) {
	for _, w := range []string{"click Repair on this archive's Weekly backup card", "then click Install / update", "set up on this card or in the wizard"} {
		found, err := oldCardWordsIn("planted.go", []byte("package p\nvar s = "+strconv.Quote(w)+"\n"))
		if err != nil || len(found) == 0 {
			t.Fatalf("the positive twin: the checker does not catch %q (%v)", w, err)
		}
	}
	if found, _ := oldCardWordsIn("planted.go", []byte("package p\n// the Weekly backup card, in a comment\nvar s = \"Scheduled backup card\"\n")); len(found) != 0 {
		t.Fatalf("the checker flags words that are not text: %v", found)
	}

	var all []string
	for _, dir := range []string{".", filepath.Join("..", "health")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no source in %s: %v", dir, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			found, err := oldCardWordsIn(f, src)
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, found...)
		}
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(strings.NewReplacer("*", "", "`", "").Replace(string(readme))) // "**Weekly backup** card" too
	for _, w := range oldCardWords {
		if strings.Contains(text, w) {
			all = append(all, "README.md: "+w)
		}
	}
	for _, f := range all {
		t.Errorf("names the old backup card or button: %s", f)
	}
}
