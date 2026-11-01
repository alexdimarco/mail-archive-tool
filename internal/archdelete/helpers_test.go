package archdelete

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// rec is a fixture record: its recorded Path, AlsoFiles and date.
type rec struct {
	Path string   `json:"path"`
	Also []string `json:"also_files,omitempty"`
	Date string   `json:"date,omitempty"`
}

// writeManifest writes a manifest of the given format version: records keyed as
// that version keyed them, and a Stores map only when stores is not nil.
func writeManifest(t *testing.T, dir string, version int, stores map[string]string, recs []rec) {
	t.Helper()
	entries := map[string]map[string]any{}
	for i, r := range recs {
		key := fmt.Sprintf("Inbox\x00id%d", i)
		if version >= 3 {
			tok := strings.SplitN(r.Path, "/", 2)[0]
			key = fmt.Sprintf("%s\x00Inbox\x00id%d", tok, i)
		}
		e := map[string]any{"path": r.Path, "folder": "Inbox", "exported_at": "2024-03-01T09:00:00Z"}
		if len(r.Also) > 0 {
			e["also_files"] = r.Also
		}
		if r.Date != "" {
			e["date"] = r.Date
		}
		entries[key] = e
	}
	// "version" first, as Save writes it (the encrypted-marker probe reads the first key).
	ents, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf(`{"version":%d,"entries":%s`, version, ents)
	if stores != nil {
		st, err := json.Marshal(stores)
		if err != nil {
			t.Fatal(err)
		}
		data += `,"stores":` + string(st)
	}
	writeFile(t, filepath.Join(dir, manifestName), data+"}")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tree is every file and folder under dir with its content's digest ("dir" for a
// folder, "link→target" for a link): two equal trees are byte-identical.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[rel] = "link→" + target
		case d.IsDir():
			out[rel] = "dir"
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			s := sha256.Sum256(b)
			out[rel] = hex.EncodeToString(s[:])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// testDeps are the seams a plan reads, with this computer's protected folders
// (isolated by TestMain).
func testDeps() Deps {
	return Deps{Protected: DefaultProtected(nil)}
}

func removeRels(p *DeletePlan) []string {
	var out []string
	for _, it := range p.Remove {
		out = append(out, it.Rel)
	}
	return out
}

func unrecordedRels(p *DeletePlan) []string {
	var out []string
	for _, it := range p.Unrecorded {
		out = append(out, it.Rel)
	}
	return out
}

func sorted(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// setDiff says how got and want differ as sets, or "" when they are the same.
func setDiff(what string, got, want []string) string {
	g, w := sorted(got), sorted(want)
	if strings.Join(g, "\n") != strings.Join(w, "\n") {
		return fmt.Sprintf("%s:\n got  %q\n want %q", what, g, w)
	}
	return ""
}

// mustPlan plans dir, failing the test on a refusal.
func mustPlan(t *testing.T, dir string, deps Deps) *DeletePlan {
	t.Helper()
	p, err := Plan(dir, deps)
	if err != nil {
		t.Fatalf("Plan(%s) refused: %v", dir, err)
	}
	return p
}

// refusalOf plans dir and returns the refusal's exit code and message (0, "" when
// it was planned).
func refusalOf(t *testing.T, dir string, deps Deps) (int, string, Case) {
	t.Helper()
	_, err := Plan(dir, deps)
	if err == nil {
		return 0, "", ""
	}
	r := AsRefusal(err)
	if r == nil {
		t.Fatalf("Plan(%s) failed with an untyped error: %v", dir, err)
	}
	return r.ExitCode(), r.Message, r.Case
}
