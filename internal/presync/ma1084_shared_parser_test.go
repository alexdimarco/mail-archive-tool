package presync

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// legacyParseProfilesINI is presync's own profiles.ini parser as it shipped before
// slice R2 of docs/design-restore-thunderbird.md moved presync onto the shared parser
// in internal/thunderbird, copied here unchanged as the oracle the move is held to.
func legacyParseProfilesINI(data []byte, appSupport string) []string {
	if len(data) > profilesINIMaxBytes {
		data = data[:profilesINIMaxBytes]
	}
	var profiles []string
	add := func(p string, rel bool) {
		if p == "" {
			return
		}
		if rel {
			p = filepath.Join(appSupport, filepath.FromSlash(p))
		}
		if cp, err := filepath.EvalSymlinks(p); err == nil {
			profiles = append(profiles, cp)
		} else {
			profiles = append(profiles, filepath.Clean(p))
		}
	}

	var curPath string
	var curRel, inSection bool
	flush := func() {
		if inSection {
			add(curPath, curRel)
		}
		curPath, curRel = "", false
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripControl(raw))
		if strings.HasPrefix(line, "[") {
			flush()
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Path":
			curPath = v
		case "IsRelative":
			curRel = v == "1"
		case "Default":
			if v != "" && v != "0" && v != "1" {
				add(v, true)
			}
		}
	}
	flush()
	return profiles
}

// covers: MA-1084, MA-578, R40
// Presync on the shared parser gives the same profile paths as before, in the same
// order, on every input (slice R2, RT-23: presync's behaviour must not change, and it
// does not use the parser's new outputs): real layouts on disk (relative, absolute,
// an [Install…] default, a link resolved), the odd shapes (keys before any section, an
// unclosed header, CRLF, repeated keys, Default= flags, an [Install…] with a Path, the
// cap cutting a section in two), and 2,000 generated files.
func TestPresyncGetsTheSamePathsFromTheSharedParser(t *testing.T) {
	appSupport := t.TempDir()
	rel := filepath.Join(appSupport, "Profiles", "rel.default")
	mkdirR2(t, rel)
	inst := filepath.Join(appSupport, "Profiles", "inst.default")
	mkdirR2(t, inst)
	absDir := filepath.Join(t.TempDir(), "abs.default")
	mkdirR2(t, absDir)

	cases := []string{
		"[Install123]\nDefault=Profiles/inst.default\nLocked=1\n\n[Profile0]\nIsRelative=1\nPath=Profiles/rel.default\nDefault=1\n\n[Profile1]\nIsRelative=0\nPath=" + absDir + "\n",
		"Path=before/any/section\n[Profile0]\nPath=Profiles/rel.default\nIsRelative=1\n",
		"[Profile0\nIsRelative=1\nPath=Profiles/rel.default\n[Profile1]\r\nPath=" + absDir + "\r\nIsRelative=0\r\n",
		"[Install1]\nDefault=a\nDefault=0\nDefault=\nDefault=1\nDefault=b\nPath=Profiles/rel.default\nIsRelative=1\n",
		"[Profile0]\nPath=x\nPath=Profiles/rel.default\nIsRelative=0\nIsRelative=1\n[Profile1]\n[Profile2]\nPath=\n",
		"[Profile0]\n  Path  =  Profiles/rel\x07.default  \n IsRelative = 1 \n=novalue\nnoequals\n",
		"[Profile0]\nIsRelative=1\nPath=Profiles/rel.default\n" + strings.Repeat("x", profilesINIMaxBytes) + "\n[Profile1]\nPath=" + absDir + "\n",
		strings.Repeat("[P]\nPath=Profiles/rel.default\nIsRelative=1\n", profilesINIMaxBytes/40),
		"",
	}
	rng := rand.New(rand.NewSource(1084))
	parts := []string{"[", "]", "Profile0", "Install9", "General", "\n", "\r\n", "=", " ", "\t", "\x07", "\x00",
		"Path", "IsRelative", "Default", "Name", "0", "1", "Profiles/rel.default", absDir, "a/b", "..", "‎", "é"}
	for i := 0; i < 2000; i++ {
		var b strings.Builder
		for n := rng.Intn(60); n > 0; n-- {
			b.WriteString(parts[rng.Intn(len(parts))])
		}
		cases = append(cases, b.String())
	}
	for i, ini := range cases {
		want := legacyParseProfilesINI([]byte(ini), appSupport)
		got := parseProfilesINI([]byte(ini), appSupport)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d (%q): shared parser gives %q, presync's own gave %q", i, clipINI(ini), got, want)
		}
	}
	// The real layout case gives what MA-578 pins: all three profiles.
	if got := parseProfilesINI([]byte(cases[0]), appSupport); len(got) != 3 {
		t.Errorf("the real layout gave %d profiles, want 3: %q", len(got), got)
	}
}

func mkdirR2(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func clipINI(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
