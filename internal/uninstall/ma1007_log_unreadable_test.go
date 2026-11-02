package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// unreadableCode is 1 for uninstall's typed stop on a deletion log it cannot read, 0 otherwise.
func unreadableCode(err error) int {
	var u *LogUnreadableError
	if errors.As(err, &u) {
		return 1
	}
	return 0
}

// covers: MA-1007, R36
// Uninstall stops, removing nothing, when deletions.log cannot be read (fail closed), and its
// advice fits the cause (slice 5's re-verification, residual 5), never "make sure your account
// can read it" for a log it could open: a log larger than MailArchive writes (copy it, delete it),
// a name that is a link (put the file itself there), a log other accounts can read (chmod 600),
// and a log the account cannot open (make sure your account can read it). Positive twin first:
// an owner-only log of a finished delete is planned and nothing refuses.
func TestUninstallSaysWhyItCannotReadTheDeletionLog(t *testing.T) {
	noSchedulesNoSignIn(t)
	cfg := t.TempDir()
	settings := filepath.Join(cfg, "desktop-settings.json")
	writeFile(t, settings, "{}")
	log := logLines(t, cfg)
	if rep, err := Uninstall(cfg, nil, "", true); err != nil || rep.DeletionsLog != log {
		t.Fatalf("positive twin: an owner-only log is planned: %v, %+v", err, rep)
	}

	type fixture struct {
		name   string
		make   func(t *testing.T)
		advice string
	}
	cases := []fixture{
		{"larger than MailArchive writes", func(t *testing.T) {
			logLines(t, cfg)
			if err := os.Truncate(log, 33<<20); err != nil {
				t.Fatal(err)
			}
		}, "larger than MailArchive ever writes, so it is not MailArchive's own log. Copy it somewhere safe if you need it, delete it, then uninstall again"},
	}
	if runtime.GOOS != "windows" {
		cases = append(cases, fixture{"readable by other accounts", func(t *testing.T) {
			logLines(t, cfg)
			if err := os.Chmod(log, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "other accounts on this computer can read it, so MailArchive does not trust it. Run chmod 600 " + log + ", then uninstall again"})
		if os.Geteuid() != 0 {
			cases = append(cases, fixture{"not readable by this account", func(t *testing.T) {
				logLines(t, cfg)
				if err := os.Chmod(log, 0o200); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(log, 0o600) })
			}, "make sure your account can read it, then uninstall again"})
		}
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(log)
			writeFile(t, settings, "{}")
			c.make(t)
			for _, dry := range []bool{true, false} {
				rep, err := Uninstall(cfg, nil, "", dry)
				assure.Refused(t, unreadableCode(err), errText(err), assure.Code(1),
					assure.Names(log, "uninstall stopped before removing anything", c.advice),
					assure.NoSideEffect(func() bool {
						_, lerr := os.Lstat(log)
						return rep == nil && lerr == nil && exists(settings)
					}))
			}
			os.Remove(log)
		})
	}

	// A log that is a link: ConfigTargets never names a link, so Uninstall keeps it and does
	// not read it; the check's own advice, for a log replaced by a link after that listing
	// (the real run's second check), is asked of the check directly.
	real := filepath.Join(t.TempDir(), archdelete.LogName)
	if err := os.WriteFile(real, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, log); err != nil {
		t.Skipf("links cannot be made here: %v", err)
	}
	err := checkNoPendingDelete(log)
	assure.Refused(t, unreadableCode(err), errText(err), assure.Code(1),
		assure.Names(log, "uninstall stopped before removing anything",
			"it is not an ordinary file (a link, a folder or a device), so MailArchive does not read it. Put the log file itself there, or remove what is there, then uninstall again"),
		assure.NoSideEffect(func() bool { fi, lerr := os.Lstat(log); return lerr == nil && fi.Mode()&os.ModeSymlink != 0 && exists(real) }))
}
