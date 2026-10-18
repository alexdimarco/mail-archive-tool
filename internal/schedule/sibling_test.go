package schedule

import (
	"runtime/debug"
	"testing"
)

// covers: MA-628, R33, S59
// SameBuild (§4.6.0) is true only for the same build of the one program: it
// ignores -ldflags (the pair's one legitimate difference) but a different main
// package, Go version, main-module version, dependency, vcs stamp or any other
// setting makes it false, and a nil side is a different build. MA-628 also runs
// SameBuild on the two real Windows binaries build-release.sh produces.
func TestMA628_SameBuild(t *testing.T) {
	base := func() *debug.BuildInfo {
		return &debug.BuildInfo{
			GoVersion: "go1.26.0",
			Path:      "mail-archive-tool/cmd/mailarchive",
			Main:      debug.Module{Path: "mail-archive-tool", Version: "(devel)"},
			Deps:      []*debug.Module{{Path: "github.com/ncruces/zenity", Version: "v0.10.0", Sum: "h1:abc"}},
			Settings: []debug.BuildSetting{
				{Key: "GOARCH", Value: "amd64"},
				{Key: "GOOS", Value: "windows"},
				{Key: "vcs.revision", Value: "deadbeef"},
				{Key: "vcs.modified", Value: "false"},
				{Key: "-ldflags", Value: ""},
			},
		}
	}

	// The pair: identical except -ldflags -H=windowsgui on the no-console build.
	con, gui := base(), base()
	gui.Settings[4] = debug.BuildSetting{Key: "-ldflags", Value: "-H=windowsgui"}
	if !SameBuild(con, gui) {
		t.Error("the pair (differing only in -ldflags) must be the same build (SC7)")
	}

	// Each of these makes it a different build.
	for _, m := range []struct {
		name string
		f    func(*debug.BuildInfo)
	}{
		{"a three-file-era main package", func(b *debug.BuildInfo) { b.Path = "mail-archive-tool/cmd/mailarchive-desktop" }},
		{"a different Go version", func(b *debug.BuildInfo) { b.GoVersion = "go1.27.2" }},
		{"a different main-module version", func(b *debug.BuildInfo) { b.Main.Version = "v1.0.0" }},
		{"a different dependency version", func(b *debug.BuildInfo) { b.Deps[0].Version = "v0.11.0" }},
		{"a different vcs.revision", func(b *debug.BuildInfo) { b.Settings[2].Value = "cafef00d" }},
		{"a dirty vcs.modified", func(b *debug.BuildInfo) { b.Settings[3].Value = "true" }},
		{"a different GOARCH", func(b *debug.BuildInfo) { b.Settings[0].Value = "arm64" }},
	} {
		other := base()
		m.f(other)
		if SameBuild(con, other) {
			t.Errorf("SameBuild must be false for %s", m.name)
		}
	}

	if SameBuild(nil, con) || SameBuild(con, nil) {
		t.Error("a nil side is a different build")
	}
}
