package instance

import (
	"errors"
	"testing"
)

// covers: MA-998, R12
// Dir's error says that no home folder is set (ErrNoHomeFolder) only when none is: no HOME (no
// XDG_CONFIG_HOME either, on Linux and the other Unixes), or no LOCALAPPDATA on Windows. Any other
// reason the per-account folder cannot be found — a relative XDG_CONFIG_HOME with HOME set — keeps
// its own cause, so the person is not told to set a HOME that is set (the settle pass's LOW-5).
// Positive twin first: with the home folder unset it is said so, on every platform.
func TestNoHomeFolderIsSaidOnlyWhenNoneIsSet(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	for _, c := range []struct {
		name, goos string
		vars       map[string]string
		want       bool
	}{
		// Positive twins: nothing set.
		{"linux, nothing set", "linux", nil, true},
		{"darwin, no HOME", "darwin", nil, true},
		{"windows, no LOCALAPPDATA", "windows", map[string]string{"HOME": "/h", "USERPROFILE": `C:\Users\a`}, true},
		// A cause of its own: kept.
		{"linux, relative XDG_CONFIG_HOME with HOME set", "linux", map[string]string{"HOME": "/home/a", "XDG_CONFIG_HOME": "rel"}, false},
		{"linux, relative XDG_CONFIG_HOME, no HOME", "linux", map[string]string{"XDG_CONFIG_HOME": "rel"}, false},
		{"freebsd, relative XDG_CONFIG_HOME with HOME set", "freebsd", map[string]string{"HOME": "/home/a", "XDG_CONFIG_HOME": "rel"}, false},
		{"darwin, HOME set", "darwin", map[string]string{"HOME": "/Users/a"}, false},
		{"windows, LOCALAPPDATA set", "windows", map[string]string{"LOCALAPPDATA": `C:\L`}, false},
	} {
		if got := homeUnset(c.goos, env(c.vars)); got != c.want {
			t.Errorf("%s: homeUnset = %v, want %v", c.name, got, c.want)
		}
	}

	// The error itself: its text is Dir's as before, and it is still ErrNoAccountFolder.
	cause := errors.New("neither $XDG_CONFIG_HOME nor $HOME are defined")
	err := accountFolderErr(cause, true)
	if !errors.Is(err, ErrNoHomeFolder) || !errors.Is(err, ErrNoAccountFolder) || err.Error() != "cannot determine your per-account folder: neither $XDG_CONFIG_HOME nor $HOME are defined" {
		t.Fatalf("no home: %v (is no-home %v)", err, errors.Is(err, ErrNoHomeFolder))
	}
	rel := accountFolderErr(errors.New("path in $XDG_CONFIG_HOME is relative"), false)
	if errors.Is(rel, ErrNoHomeFolder) || !errors.Is(rel, ErrNoAccountFolder) || rel.Error() != "cannot determine your per-account folder: path in $XDG_CONFIG_HOME is relative" {
		t.Fatalf("a relative XDG_CONFIG_HOME: %v", rel)
	}
}
