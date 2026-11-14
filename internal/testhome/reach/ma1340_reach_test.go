package reach

import "testing"

// covers: MA-1340, R37
// The B3 adversarial pass, round 4's J5: the throw-away-home checks accept only a guard that runs
// before the tests, and find the per-user folder lookups the os package's functions miss.
// CallsGuard counts a testhome.Run that is a statement of TestMain's own body (the call itself, an
// assignment of it, or an argument of the statement's call, as os.Exit(testhome.Run(root, m))),
// reached before any statement of the body that runs the tests or ends TestMain (a branch of its
// own, a child helper's, may); never one in dead code, a branch, a loop or a closure, nor a Check
// alone, which reports only after the tests ran.
// ComputesUserFolder also sees os/user's lookups, os.ExpandEnv and os.Expand of a per-user
// variable, os.Getenv of a constant or a list naming one, syscall.Getenv, and Windows' known-folder
// and profile lookups. Positive twins first: the shapes every package's TestMain uses pass, and
// os.UserHomeDir is still a root; fail-closed twins: each careless shape is refused, and a lookup
// of another variable is no root.
func TestTheThrowAwayHomeCheckNeedsAGuardThatRunsFirst(t *testing.T) {
	const head = "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n\n\t\"mail-archive-tool/internal/testhome\"\n)\n\n"
	for _, c := range []struct {
		name string
		src  string
		want bool
	}{
		{"an assignment, as every package's", head + "func TestMain(m *testing.M) {\n\tos.Setenv(\"HOME\", \"/x\")\n\tcode := testhome.Run(\"/x\", m)\n\tos.Exit(code)\n}\n", true},
		{"inside os.Exit", head + "func TestMain(m *testing.M) { os.Exit(testhome.Run(\"/x\", m)) }\n", true},
		{"a var declaration", head + "func TestMain(m *testing.M) {\n\tvar code = testhome.Run(\"/x\", m)\n\tos.Exit(code)\n}\n", true},
		{"after a branch that exits without the tests", head + "func TestMain(m *testing.M) {\n\tif len(os.Args) > 9 {\n\t\tos.Exit(1)\n\t}\n\tos.Exit(testhome.Run(\"/x\", m))\n}\n", true},
		{"after a child helper's own branch", head + "func TestMain(m *testing.M) {\n\tif os.Getenv(\"LEADER\") == \"1\" {\n\t\tos.Exit(m.Run())\n\t}\n\tos.Exit(testhome.Run(\"/x\", m))\n}\n", true},
		{"in dead code", head + "func TestMain(m *testing.M) {\n\tif false {\n\t\tos.Exit(testhome.Run(\"/x\", m))\n\t}\n\tos.Exit(m.Run())\n}\n", false},
		{"in a closure never called", head + "func TestMain(m *testing.M) {\n\t_ = func() int { return testhome.Run(\"/x\", m) }\n\tos.Exit(m.Run())\n}\n", false},
		{"in a loop", head + "func TestMain(m *testing.M) {\n\tfor range os.Args[:0] {\n\t\tos.Exit(testhome.Run(\"/x\", m))\n\t}\n\tos.Exit(m.Run())\n}\n", false},
		{"Check alone, after the tests", head + "func TestMain(m *testing.M) {\n\tc := m.Run()\n\tos.Exit(testhome.Check(\"/x\", c))\n}\n", false},
		{"after the tests ran", head + "func TestMain(m *testing.M) {\n\tc := m.Run()\n\tos.Exit(testhome.Run(\"/x\", m) + c)\n}\n", false},
		{"after TestMain ended", head + "func TestMain(m *testing.M) {\n\tos.Exit(0)\n\tos.Exit(testhome.Run(\"/x\", m))\n}\n", false},
		{"after a return", head + "func TestMain(m *testing.M) {\n\treturn\n\tos.Exit(testhome.Run(\"/x\", m))\n}\n", false},
	} {
		if got := CallsGuard([]byte(c.src), false); got != c.want {
			t.Errorf("%s: CallsGuard %v, want %v", c.name, got, c.want)
		}
	}
	for _, c := range []struct {
		name string
		src  string
		want bool
	}{
		{"UserHomeDir", "package p\n\nimport \"os\"\n\nfunc f() { os.UserHomeDir() }\n", true},
		{"os/user Current", "package p\n\nimport \"os/user\"\n\nfunc f() string { u, _ := user.Current(); return u.HomeDir }\n", true},
		{"os/user Lookup, another name", "package p\n\nimport u \"os/user\"\n\nfunc f() { u.Lookup(\"someone\") }\n", true},
		{"ExpandEnv of HOME", "package p\n\nimport \"os\"\n\nfunc f() string { return os.ExpandEnv(\"$HOME/.config\") }\n", true},
		{"ExpandEnv of APPDATA in braces", "package p\n\nimport \"os\"\n\nfunc f() string { return os.ExpandEnv(\"${APPDATA}/x\") }\n", true},
		{"Expand of XDG_CONFIG_HOME", "package p\n\nimport \"os\"\n\nfunc f() string { return os.Expand(\"$XDG_CONFIG_HOME/x\", os.Getenv) }\n", true},
		{"Getenv of a constant", "package p\n\nimport \"os\"\n\nconst home = \"HOME\"\n\nfunc f() string { return os.Getenv(home) }\n", true},
		{"LookupEnv over a list", "package p\n\nimport \"os\"\n\nvar keys = []string{\"PATH\", \"HOME\"}\n\nfunc f() { for _, k := range keys { os.LookupEnv(k) } }\n", true},
		{"syscall.Getenv", "package p\n\nimport \"syscall\"\n\nfunc f() string { v, _ := syscall.Getenv(\"HOME\"); return v }\n", true},
		{"KnownFolderPath", "package p\n\nimport \"golang.org/x/sys/windows\"\n\nfunc f() string { p, _ := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0); return p }\n", true},
		{"a token's KnownFolderPath", "package p\n\nimport \"golang.org/x/sys/windows\"\n\nfunc f() string { p, _ := windows.Token(0).KnownFolderPath(windows.FOLDERID_LocalAppData, 0); return p }\n", true},
		{"a token's profile folder", "package p\n\nimport \"golang.org/x/sys/windows\"\n\nfunc f() string { p, _ := windows.Token(0).GetUserProfileDirectory(); return p }\n", true},
		{"ExpandEnv of another variable", "package p\n\nimport \"os\"\n\nfunc f() string { return os.ExpandEnv(\"$GOPATH/bin\") }\n", false},
		{"Getenv over a list of other variables", "package p\n\nimport \"os\"\n\nfunc f() { for _, v := range []string{\"SystemRoot\", \"ProgramData\"} { os.Getenv(v) } }\n", false},
		{"os/user's group lookup", "package p\n\nimport \"os/user\"\n\nfunc f() { user.LookupGroup(\"staff\") }\n", false},
		{"another package's KnownFolderPath", "package p\n\ntype w struct{}\n\nfunc (w) KnownFolderPath() {}\n\nvar windows w\n\nfunc f() { windows.KnownFolderPath() }\n", false},
	} {
		if got := ComputesUserFolder([]byte(c.src)); got != c.want {
			t.Errorf("%s: ComputesUserFolder %v, want %v", c.name, got, c.want)
		}
	}
}
