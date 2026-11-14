package reach

import "testing"

// covers: MA-1333, R37
// The checks assureblock's meta-test finds the packages with read calls, not text (the B3
// adversarial pass, round 3's H4). Positive twins first: a TestMain that calls testhome.Run (or
// Check, or under another import name) passes, and code that calls os.UserHomeDir, os.UserCacheDir
// or the config-folder function, or reads APPDATA, LOCALAPPDATA, HOME or an XDG folder variable
// through os.Getenv or os.LookupEnv, under any name it imports os as, is a root. Fail-closed twins:
// a testhome.Run named only in a comment or a string, called outside TestMain, or on something
// else named testhome does not pass; a comment or a string naming a lookup, or a Getenv of another
// variable, is not a root.
func TestTheThrowAwayHomeCheckReadsCallsNotText(t *testing.T) {
	const head = "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n\n\t\"mail-archive-tool/internal/testhome\"\n)\n\n"
	for _, c := range []struct {
		name string
		src  string
		want bool
	}{
		{"a call in TestMain", head + "func TestMain(m *testing.M) { os.Exit(testhome.Run(\"/x\", m)) }\n", true},
		{"Check in TestMain", head + "func TestMain(m *testing.M) { os.Exit(testhome.Check(\"/x\", m.Run())) }\n", true},
		{"another import name", "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n\n\th \"mail-archive-tool/internal/testhome\"\n)\n\nfunc TestMain(m *testing.M) { os.Exit(h.Run(\"/x\", m)) }\n", true},
		{"a comment only", head + "// TestMain calls testhome.Run(root, m).\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n\nvar _ = testhome.Contained\n", false},
		{"a string only", head + "func TestMain(m *testing.M) { _ = \"testhome.Run(\"; os.Exit(m.Run()) }\n\nvar _ = testhome.Contained\n", false},
		{"outside TestMain", head + "func TestMain(m *testing.M) { os.Exit(m.Run()) }\n\nfunc helper(m *testing.M) int { return testhome.Run(\"/x\", m) }\n", false},
		{"a look-alike", "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\ntype th struct{}\n\nfunc (th) Run(string, *testing.M) int { return 0 }\n\nvar testhome th\n\nfunc TestMain(m *testing.M) { os.Exit(testhome.Run(\"/x\", m)) }\n", false},
		{"source that does not parse", head + "func TestMain(m *testing.M) { testhome.Run(\n", false},
	} {
		if got := CallsGuard([]byte(c.src), false); got != c.want {
			t.Errorf("%s: CallsGuard %v, want %v", c.name, got, c.want)
		}
	}
	own := "package testhome\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) { os.Exit(Run(\"/x\", m)) }\n"
	if !CallsGuard([]byte(own), true) || CallsGuard([]byte(own), false) {
		t.Error("an unqualified Run counts in this package's own tests only")
	}
	for _, c := range []struct {
		name string
		src  string
		want bool
	}{
		{"UserHomeDir", "package p\n\nimport \"os\"\n\nfunc f() { os.UserHomeDir() }\n", true},
		{"UserCacheDir", "package p\n\nimport \"os\"\n\nfunc f() { os.UserCacheDir() }\n", true},
		{"UserConfigDir", "package p\n\nimport \"os\"\n\nfunc f() { os.UserConfigDir() }\n", true},
		{"Getenv APPDATA", "package p\n\nimport \"os\"\n\nfunc f() string { return os.Getenv(\"APPDATA\") }\n", true},
		{"Getenv LOCALAPPDATA", "package p\n\nimport \"os\"\n\nfunc f() string { return os.Getenv(\"LOCALAPPDATA\") }\n", true},
		{"LookupEnv HOME", "package p\n\nimport \"os\"\n\nfunc f() { os.LookupEnv(\"HOME\") }\n", true},
		{"an XDG variable", "package p\n\nimport \"os\"\n\nfunc f() string { return os.Getenv(\"XDG_CONFIG_HOME\") }\n", true},
		{"another import name", "package p\n\nimport sys \"os\"\n\nfunc f() { sys.UserHomeDir() }\n", true},
		{"a comment", "package p\n\nimport \"os\"\n\n// os.UserHomeDir is not called here.\nfunc f() { os.Exit(0) }\n", false},
		{"a string", "package p\n\nimport \"os\"\n\nvar s = \"os.UserHomeDir()\"\n\nfunc f() { os.Exit(0) }\n", false},
		{"another variable", "package p\n\nimport \"os\"\n\nfunc f() string { return os.Getenv(\"PATH\") }\n", false},
		{"no os import", "package p\n\ntype o struct{}\n\nfunc (o) UserHomeDir() {}\n\nvar os o\n\nfunc f() { os.UserHomeDir() }\n", false},
	} {
		if got := ComputesUserFolder([]byte(c.src)); got != c.want {
			t.Errorf("%s: ComputesUserFolder %v, want %v", c.name, got, c.want)
		}
	}
}
