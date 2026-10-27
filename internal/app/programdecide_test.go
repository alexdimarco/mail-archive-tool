package app

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/progcache"
)

func relFile(plat, version string) container.ProgramFile {
	return container.ProgramFile{Name: progcache.AssetFor(plat), SHA256: strings.Repeat(version[1:2], 64), Size: 10,
		Origin: container.OriginRelease, Version: version, Platform: plat, Reads: programReads}
}

func sumsFile(version string) container.ProgramFile {
	return container.ProgramFile{Name: "SHA256SUMS-" + version, SHA256: strings.Repeat("e", 64), Size: 1,
		Origin: container.OriginRelease, Version: version, Platform: container.PlatformChecksums, Reads: programReads}
}

func cacheOf(version string, plats ...string) map[string]progcache.Asset {
	m := map[string]progcache.Asset{}
	for _, p := range plats {
		m[progcache.AssetFor(p)] = progcache.Asset{SHA256: strings.Repeat("9", 64), Size: 20}
	}
	return m
}

// view is a release v1.5.0 on a Mac with nothing kept, nothing cached.
func view() programView {
	return programView{keep: true, version: "v1.5.0", release: true, platform: "macos", selfPlatform: "darwin-arm64",
		selfName: "mailarchive-self-darwin-arm64.zip", state: map[string]judged{}, now: "2026-10-27T12:00:00Z"}
}

func withCache(v programView, version string, plats ...string) programView {
	v.cacheVersion, v.cacheAssets = version, cacheOf(version, plats...)
	v.cacheSums = progcache.Asset{SHA256: strings.Repeat("f", 64), Size: 2}
	return v
}

func kept(v programView, files ...container.ProgramFile) programView {
	v.set.Files = append(v.set.Files, files...)
	for _, f := range files {
		if _, ok := v.state[f.Name]; !ok {
			v.state[f.Name] = jOK
		}
	}
	return v
}

// summary is a plan as "+name@version" adds, "-name" removals, "=name" keeps and "!platform" blocks.
func summary(p programPlan) string {
	var s []string
	for _, a := range p.add {
		s = append(s, "+"+a.file.Name+"@"+a.file.Version)
	}
	for _, r := range p.remove {
		s = append(s, "-"+r)
	}
	for _, k := range p.keep {
		s = append(s, "="+k.Name)
	}
	for _, b := range p.blocked {
		s = append(s, "!"+b)
	}
	sort.Strings(s)
	return strings.Join(s, " ")
}

var all = container.ReleasePlatforms

// covers: MA-839, R37
// Deciding the program folder (program design §4.2, C5, C6, R8, R15, R16): each case
// of the design, as the plan it yields.
func TestDecidingTheProgramFolder(t *testing.T) {
	mac, win, lx, arm := progcache.AssetFor("macos"), progcache.AssetFor("windows-amd64"), progcache.AssetFor("linux-amd64"), progcache.AssetFor("linux-arm64")
	v14 := []container.ProgramFile{relFile("macos", "v1.4.0"), relFile("windows-amd64", "v1.4.0"), relFile("linux-amd64", "v1.4.0"), relFile("linux-arm64", "v1.4.0"), sumsFile("v1.4.0")}
	cases := []struct {
		name string
		v    programView
		want string
		warn string
	}{
		{"a release writes its four copies and their checksum file", withCache(view(), "v1.5.0", all...),
			"+" + mac + "@v1.5.0 +" + win + "@v1.5.0 +" + lx + "@v1.5.0 +" + arm + "@v1.5.0 +SHA256SUMS-v1.5.0@v1.5.0", ""},
		{"a development build writes no release copy, only its own", func() programView { v := withCache(view(), "v1.5.0", all...); v.release = false; return v }(),
			"+mailarchive-self-darwin-arm64.zip@", ""},
		{"a development build removes none", func() programView { v := kept(view(), v14...); v.release = false; return v }(),
			"=" + mac + " =" + win + " =" + lx + " =" + arm + " =SHA256SUMS-v1.4.0", ""},
		{"an upgrade replaces every copy and the checksum file", withCache(kept(view(), v14...), "v1.5.0", all...),
			"+" + mac + "@v1.5.0 +" + win + "@v1.5.0 +" + lx + "@v1.5.0 +" + arm + "@v1.5.0 +SHA256SUMS-v1.5.0@v1.5.0 -SHA256SUMS-v1.4.0", ""},
		{"a platform the new release lacks keeps its older copy and checksums", withCache(kept(view(), v14...), "v1.5.0", "macos", "windows-amd64", "linux-amd64"),
			"+" + mac + "@v1.5.0 +" + win + "@v1.5.0 +" + lx + "@v1.5.0 +SHA256SUMS-v1.5.0@v1.5.0 =" + arm + " =SHA256SUMS-v1.4.0", ""},
		// Written again and told — design line 230: "replaces a changed copy and warns" (P3-8, B3).
		{"a changed copy is written again from the cache, and told", func() programView {
			v := withCache(kept(view(), relFile("macos", "v1.5.0")), "v1.5.0", "macos")
			v.state[mac] = jChanged
			return v
		}(), "+" + mac + "@v1.5.0 +SHA256SUMS-v1.5.0@v1.5.0", "was changed; it was replaced with the release's own"},
		{"a changed copy the cache cannot write goes, its entry marked missing", func() programView {
			v := kept(view(), relFile("macos", "v1.5.0"))
			v.state[mac] = jChanged
			return v
		}(), "+mailarchive-self-darwin-arm64.zip@v1.5.0 -" + mac + " =" + mac, "was changed"},
		{"a missing copy the cache cannot write is kept marked missing", func() programView {
			v := kept(view(), relFile("windows-amd64", "v1.4.0"))
			v.state[win] = jAbsent
			return v
		}(), "+mailarchive-self-darwin-arm64.zip@v1.5.0 =" + win, "is missing"},
		{"a folder at a copy's name blocks its platform", func() programView {
			v := withCache(kept(view(), relFile("macos", "v1.4.0")), "v1.5.0", "macos")
			v.state[mac] = jBlocked
			return v
		}(), "+mailarchive-self-darwin-arm64.zip@v1.5.0 =" + mac + " !macos", ""},
		{"kept copies that cannot open the archive bring this computer's own", func() programView {
			f := relFile("macos", "v1.4.0")
			f.Reads = 0
			return kept(view(), f)
		}(), "+mailarchive-self-darwin-arm64.zip@v1.5.0 =" + mac, "cannot open it"},
		{"another computer's self-copy stays until a release copy for its system arrives",
			kept(view(), relFile("macos", "v1.5.0"), container.ProgramFile{Name: "mailarchive-self-linux-arm64", SHA256: strings.Repeat("1", 64), Size: 3, Origin: container.OriginSelf, Platform: "linux-arm64", Reads: programReads}),
			"=" + mac + " =mailarchive-self-linux-arm64", ""},
		{"and goes when one arrives",
			withCache(kept(view(), relFile("macos", "v1.5.0"), container.ProgramFile{Name: "mailarchive-self-linux-arm64", SHA256: strings.Repeat("1", 64), Size: 3, Origin: container.OriginSelf, Platform: "linux-arm64", Reads: programReads}), "v1.5.0", "linux-arm64"),
			"+" + arm + "@v1.5.0 +SHA256SUMS-v1.5.0@v1.5.0 -mailarchive-self-linux-arm64 =" + mac, ""},
		{"a self-copy that cannot open the archive goes, by any computer",
			kept(view(), relFile("macos", "v1.5.0"), container.ProgramFile{Name: "mailarchive-self-windows-amd64.exe", SHA256: strings.Repeat("1", 64), Size: 3, Origin: container.OriginSelf, Platform: "windows-amd64", Reads: 0}),
			"-mailarchive-self-windows-amd64.exe =" + mac, "cannot open it; it was removed"},
		{"this computer's own self-copy is kept while no release copy opens the archive",
			func() programView {
				v := kept(view(), container.ProgramFile{Name: "mailarchive-self-darwin-arm64.zip", SHA256: strings.Repeat("1", 64), Size: 3, Origin: container.OriginSelf, Version: "v1.5.0", Platform: "darwin-arm64", Reads: programReads})
				return v
			}(), "=mailarchive-self-darwin-arm64.zip", ""},
		{"an online-only copy is present, not checked",
			func() programView {
				v := kept(view(), relFile("macos", "v1.5.0"))
				v.state[mac] = jUnchecked
				return v
			}(), "=" + mac, ""},
		{"keep off removes every present copy, leaves a blocked one",
			func() programView {
				v := kept(view(), v14...)
				v.keep = false
				v.state[win], v.state[lx] = jAbsent, jBlocked
				return v
			}(), "-" + mac + " -" + arm + " -SHA256SUMS-v1.4.0 =" + lx + " !linux-amd64", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := decide(c.v)
			warn := strings.Join(p.warnings, " | ")
			want := strings.Fields(c.want)
			sort.Strings(want)
			if got := summary(p); got != strings.Join(want, " ") || (c.warn == "" && warn != "") || !strings.Contains(warn, c.warn) {
				t.Fatalf("plan:\n got  %s\n want %s\nwarnings: %s", got, c.want, warn)
			}
			for _, a := range p.add {
				if !a.self && container.ValidateProgram(container.ProgramSet{Format: container.ProgramFormat, Files: []container.ProgramFile{a.file}}) != nil {
					t.Fatalf("an added entry does not validate: %+v", a.file)
				}
			}
		})
	}
	v := kept(view(), relFile("macos", "v1.5.0"))
	v.state[progcache.AssetFor("macos")] = jUnchecked
	p := decide(v)
	assure.Reached(t, len(p.keep) == 1 && p.keep[0].UncheckedSince == v.now, fmt.Sprintf("unchecked since: %+v", p.keep))
}
