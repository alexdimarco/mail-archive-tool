//go:build !darwin

package instance

import "testing"

// covers: MA-1427, R50
// The Linux sign-in rule over its inputs (the S1 fix round, A3). Positive twin first:
// XDG_SESSION_ID when it is a bounded printable value. When the environment does not
// carry it (an autostart, `su`, a systemd user unit), the kernel's audit session of the
// process, which logind uses as its session id. Neither readable ("", the kernel's
// "unset" 4294967295, a value with the separator or not a number) is "" — cannot tell,
// which the instance answers by showing nothing (MA-1427's fail-closed rule).
func TestMA1427_ASignInThatCannotBeReadIsUnknown(t *testing.T) {
	none := func() string { return "" }
	if s := guiSessionFrom("c2", func() string { return "7" }); s != "c2" {
		t.Errorf("XDG_SESSION_ID c2 gave %q", s)
	}
	if s := guiSessionFrom("", func() string { return "7\n" }); s != "7" {
		t.Errorf("no XDG_SESSION_ID, audit session 7: %q, want 7", s)
	}
	for name, c := range map[string][2]string{
		"nothing":              {"", ""},
		"audit unset":          {"", "4294967295"},
		"separator, no audit":  {"a|b", ""},
		"audit not a number":   {"", "x7"},
		"audit with separator": {"", "1|2"},
	} {
		audit := none
		if c[1] != "" {
			v := c[1]
			audit = func() string { return v }
		}
		if s := guiSessionFrom(c[0], audit); s != "" {
			t.Errorf("%s: %q, want \"\" (cannot tell)", name, s)
		}
	}
}
