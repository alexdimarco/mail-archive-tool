package health

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-635, R18, S59
// §4.6.2's Windows install-folder branch for exe_missing: a known-name program
// missing from the running program's own directory is restored with the
// installer's Repair (and an antivirus removal allowed first), never a schedule
// re-install; every other missing case keeps the re-install remedy. The flag's
// COMPUTATION is Windows-only (exeIdentity + schedule.SelfResolver; the exact
// Repair path and msiexec /fa form are lab item L-W9); this pins the rendering
// the flag drives, on either host.
func TestMA635_ExeMissingInstallFolderBranch(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	missing := func(installDir bool) string {
		in := healthyInput(now)
		in.GOOS = "windows"
		in.Desc.Exe = `C:\Program Files\MailArchive\mailarchivew.exe`
		in.ExeExists = false
		in.ExeMissingInInstallDir = installDir
		r := Assess(in, now)
		for i, c := range r.Codes {
			if c == "exe_missing" {
				return r.Reasons[i]
			}
		}
		t.Fatalf("no exe_missing reason:\n%v", r.Reasons)
		return ""
	}

	// The install-folder branch: the installer's Repair, allow-after-antivirus,
	// and NOT a schedule -install the kind guard would refuse.
	msg := missing(true)
	for _, want := range []string{"missing from the install folder", "installer's Repair", "antivirus"} {
		if !strings.Contains(msg, want) {
			t.Errorf("install-folder exe_missing is missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "-install") {
		t.Errorf("the install-folder branch must not offer a schedule -install:\n%s", msg)
	}

	// Positive twin: every other missing case keeps the ordinary re-install remedy.
	msg = missing(false)
	if !strings.Contains(msg, "no longer exists") {
		t.Errorf("the ordinary missing case lost its wording:\n%s", msg)
	}
	if strings.Contains(msg, "install folder") {
		t.Errorf("the ordinary missing case must not use the install-folder branch:\n%s", msg)
	}
}
