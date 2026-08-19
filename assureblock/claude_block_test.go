// Structural meta-test: the CLAUDE.md discipline block is present, current,
// and fully instantiated in EVERY instruction file this repo carries -- the Go
// twin of python/tests/test_claude_block.py, implementing the reviewed
// instruction-file contract (assurance-kit
// docs/design-instruction-file-generalization.md).
//
// Failure modes: MISSING / UNREADABLE (directory, dangling symlink) /
// OUTSIDE-REPO (symlink escaping the repo) / UNGOVERNED (incl. the
// materialized-symlink shape from core.symlinks=false clones -- common on
// Windows checkouts) / STALE / AHEAD / UNTERMINATED / PLACEHOLDER / DUPLICATED
// / DIVERGED. All sentinel analysis runs on FENCE-STRIPPED text; candidate
// discovery is case-folded; written path-portably (this twin's adopters run
// multi-OS CI).
//
// Vendored from assurance-kit go/claude_block_test.go -- fix there first,
// re-sync out (vendor block_version.go beside it).
package assureblock

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	sentinelRe    = regexp.MustCompile(SentinelRe)
	placeholderRe = regexp.MustCompile(`<[A-Z][A-Z0-9_ +-]*>`)
	fenceLineRe   = regexp.MustCompile("(?m)^```[^\n]*$")
)

func stripFences(text string) string {
	parts := fenceLineRe.Split(text, -1)
	kept := make([]string, 0, len(parts))
	for i, p := range parts {
		if i%2 == 0 {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n")
}

// repoRoot ascends from the package dir to the first ancestor with .git,
// falling back to the first ancestor holding any listed instruction file.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("MISSING: cannot determine working directory: %v", err)
	}
	fallback := ""
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if fallback == "" {
			for _, n := range InstructionFiles {
				if fi, err := os.Stat(filepath.Join(dir, n)); err == nil && !fi.IsDir() {
					fallback = dir
					break
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if fallback != "" {
		return fallback
	}
	t.Fatalf("MISSING: no repo root (.git) or instruction file (%s) found ascending",
		strings.Join(InstructionFiles, ", "))
	return ""
}

// candidates: case-folded scan so agents.md is policed like AGENTS.md.
func candidates(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("cannot read repo root %s: %v", root, err)
	}
	var out []string
	for _, e := range entries {
		for _, n := range InstructionFiles {
			if strings.EqualFold(e.Name(), n) {
				out = append(out, filepath.Join(root, e.Name()))
			}
		}
	}
	sort.Strings(out)
	return out
}

func shapeError(root, path string) string {
	name := filepath.Base(path)
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Sprintf("UNREADABLE: %s cannot be lstat'd: %v", name, err)
	}
	if fi.IsDir() {
		return fmt.Sprintf("UNREADABLE: %s is a DIRECTORY bearing an instruction-file name.", name)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		if _, err := os.Stat(path); err != nil {
			return fmt.Sprintf("UNREADABLE: %s is a dangling symlink; it governs nothing.", name)
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Sprintf("UNREADABLE: %s does not resolve: %v", name, err)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Sprintf("UNREADABLE: repo root does not resolve: %v", err)
	}
	rel, err := filepath.Rel(rootReal, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Sprintf("OUTSIDE-REPO: %s resolves to %s, outside the repo root %s; "+
			"governance must come from committed, in-repo content.", name, resolved, rootReal)
	}
	return ""
}

type governed struct {
	name    string
	text    string // fence-stripped
	version int
	start   int
}

func readable(t *testing.T, root string, cands []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, p := range cands {
		if shapeError(root, p) != "" {
			continue // the presence test owns shape reds
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("UNREADABLE: %s: %v", filepath.Base(p), err)
		}
		out[filepath.Base(p)] = stripFences(string(raw))
	}
	return out
}

func governedFiles(t *testing.T, root string, cands []string) []governed {
	t.Helper()
	var out []governed
	for name, text := range readable(t, root, cands) {
		ms := sentinelRe.FindAllStringSubmatchIndex(text, -1)
		if len(ms) != 1 {
			continue
		}
		v := 0
		fmt.Sscanf(text[ms[0][2]:ms[0][3]], "%d", &v)
		out = append(out, governed{name: name, text: text, version: v, start: ms[0][0]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func TestBlockPresentExactlyOnce(t *testing.T) {
	root := repoRoot(t)
	cands := candidates(t, root)
	if len(cands) == 0 {
		t.Fatalf("MISSING: no instruction file (%s) exists in %s. Paste the block "+
			"from the kit's CLAUDE-block.md.", strings.Join(InstructionFiles, ", "), root)
	}
	for _, p := range cands {
		if err := shapeError(root, p); err != "" {
			t.Fatal(err)
		}
	}
	for name, text := range readable(t, root, cands) {
		n := len(sentinelRe.FindAllString(text, -1))
		if n == 0 {
			raw, _ := os.ReadFile(filepath.Join(root, name))
			hint := ""
			for _, listed := range InstructionFiles {
				if strings.TrimSpace(string(raw)) == listed {
					hint = " (content is only another instruction-file name: a symlink " +
						"MATERIALIZED by a core.symlinks=false clone -- common on Windows; " +
						"paste the block as a real file here)"
				}
			}
			t.Fatalf("UNGOVERNED: %s exists but carries no assurance-kit block sentinel%s. "+
				"An agent reading it operates without the discipline.", name, hint)
		}
		if n != 1 {
			t.Fatalf("DUPLICATED: %d block sentinels in %s; exactly one block may govern a file.", n, name)
		}
	}
}

func TestBlockVersionCurrent(t *testing.T) {
	root := repoRoot(t)
	for _, g := range governedFiles(t, root, candidates(t, root)) {
		if g.version < BlockVersion {
			t.Fatalf("STALE: %s carries block v%d; the vendored kit expects v%d. "+
				"Re-paste the current CLAUDE-block.md (re-fill placeholders, re-delete "+
				"inapplicable tier lines).", g.name, g.version, BlockVersion)
		}
		if g.version > BlockVersion {
			t.Fatalf("AHEAD: %s carries block v%d but the vendored kit is at v%d; "+
				"re-vendor the kit so both sides agree.", g.name, g.version, BlockVersion)
		}
	}
}

func TestBlockTerminatedAndInstantiated(t *testing.T) {
	root := repoRoot(t)
	blocks := map[string]string{}
	for _, g := range governedFiles(t, root, candidates(t, root)) {
		end := strings.Index(g.text[g.start:], EndSentinel)
		if end == -1 {
			t.Fatalf("UNTERMINATED: end sentinel %q not found after the opening sentinel "+
				"in %s; re-paste keeping both sentinel comments.", EndSentinel, g.name)
		}
		block := g.text[g.start : g.start+end]
		if left := placeholderRe.FindAllString(block, -1); len(left) > 0 {
			sort.Strings(left)
			t.Fatalf("PLACEHOLDER: unfilled template tokens survive in %s: %s. Fill them "+
				"with the repo's real values.", g.name, strings.Join(left, ", "))
		}
		blocks[g.name] = block
	}
	if len(blocks) > 1 {
		var names, uniq []string
		seen := map[string]bool{}
		for n, b := range blocks {
			names = append(names, n)
			if !seen[b] {
				seen[b] = true
				uniq = append(uniq, b)
			}
		}
		if len(uniq) != 1 {
			sort.Strings(names)
			t.Fatalf("DIVERGED: %s carry non-identical block text, so different agent "+
				"harnesses read different rules. Keep one canonical block everywhere.",
				strings.Join(names, ", "))
		}
	}
}
