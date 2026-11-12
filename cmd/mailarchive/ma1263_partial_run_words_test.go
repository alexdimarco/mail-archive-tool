package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1263, MA-1268, R12, S10
// When a later store stops -enable-offline after an earlier store was changed, the words say
// what was changed instead of "changed nothing" (the C0c pass round 3's wording note, the lead's
// pick of the simpler true option). Every store is still checked before any change (MA-1208), so
// this happens only when something changes between the check and the later store: Thunderbird
// started for the second profile after the first was changed (Thunderbird's running state is
// injected here, the one fault no temp folder can stage on every OS), or the second profile's
// write failing (its folder made read-only after the check; Unix, not as root). Either way the run
// exits 1, the words never say "changed nothing", they name the first profile's change and why
// the run stopped, and the second profile is left byte for byte. Positive twin first: two healthy
// profiles are both turned on, exit 0, and a refusal before any change still says "changed
// nothing".
func TestALaterStopSaysWhatWasChanged(t *testing.T) {
	first, storeA := offlineProfile(t, setOffOwnFolder)
	second, storeB := offlineProfile(t, setOffOwnFolder)
	asStore(t, storeA)
	asStore(t, storeB)
	if code, msg, logText := enableOfflineRun(t, storeA, storeB); code != 0 {
		t.Fatalf("the positive twin: exit %d, %s\n%s", code, msg, logText)
	}

	turnedOn := func(p string) bool {
		b, _ := os.ReadFile(filepath.Join(p, "prefs.js"))
		return strings.Contains(string(b), `server1.offline_download", true`)
	}
	for name, stage := range map[string]func(t *testing.T, first, second string){
		"Thunderbird started for the second profile": func(t *testing.T, first, second string) {
			restore := tbRunning
			t.Cleanup(func() { tbRunning = restore })
			tbRunning = func(p string) bool { return p == second && turnedOn(first) }
		},
		"the second profile's write fails": func(t *testing.T, first, second string) {
			if runtime.GOOS == "windows" || os.Geteuid() == 0 {
				t.Skip("a read-only folder stops a write only on Unix, and not for root")
			}
			restore := tbRunning
			t.Cleanup(func() { tbRunning = restore })
			tbRunning = func(p string) bool {
				if p == second && turnedOn(first) {
					_ = os.Chmod(second, 0o500)
				}
				return false
			}
			t.Cleanup(func() { _ = os.Chmod(second, 0o755) })
		},
	} {
		t.Run(name, func(t *testing.T) {
			first, storeA := offlineProfile(t, setOffOwnFolder)
			second, storeB := offlineProfile(t, setOffOwnFolder)
			asStore(t, storeA)
			asStore(t, storeB)
			beforeB := snapshot(t, second)
			stage(t, first, second)
			code, msg, logText := enableOfflineRun(t, storeA, storeB)
			_ = os.Chmod(second, 0o755)
			assure.Refused(t, code, msg, assure.Code(1),
				assure.Names("-enable-offline stopped, and nothing was exported",
					// The friction final look's Q3 (2026-11-08): what was turned on, and that it is kept.
					"Before it stopped, it turned on offline download for imap.gmail.com (folder imap.gmail.com, server1), in the Thunderbird profile "+first+", by changing prefs.js.",
					"That change is kept: it is what -enable-offline is for, and running the same command again leaves it as it is (the log above says where the old prefs.js was backed up).",
					"Why it stopped: "),
				assure.NoSideEffect(func() bool { return sameFiles(t, second, beforeB) && turnedOn(first) }))
			if strings.Contains(msg, "changed nothing") {
				t.Errorf("the words say \"changed nothing\" after a change:\n%s", msg)
			}
			if !strings.Contains(logText, "Enabled offline download") {
				t.Errorf("the log does not name the first change:\n%s", logText)
			}
		})
	}

	// A refusal before any change keeps "changed nothing".
	first, storeA = offlineProfile(t, setOffOwnFolder)
	second, storeB = offlineProfile(t, setOffOwnFolder)
	asStore(t, storeA)
	asStore(t, storeB)
	restore := tbRunning
	defer func() { tbRunning = restore }()
	tbRunning = func(p string) bool { return p == second }
	beforeA := snapshot(t, first)
	code, msg, _ := enableOfflineRun(t, storeA, storeB)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-enable-offline changed nothing, and nothing was exported"),
		assure.NoSideEffect(func() bool { return sameFiles(t, first, beforeA) }))

	// A failed write on the first store, after the check, claims no change.
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	only, store := offlineProfile(t, setOffOwnFolder)
	asStore(t, store)
	tbRunning = func(p string) bool { _ = os.Chmod(only, 0o500); return false }
	t.Cleanup(func() { _ = os.Chmod(only, 0o755) })
	code, msg, logText := enableOfflineRun(t, store)
	_ = os.Chmod(only, 0o755)
	// The friction final look's Q4: what failed, and the way on.
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(
		"-enable-offline changed nothing, and nothing was exported: MailArchive could not write prefs.js or its backup in the Thunderbird profile "+only+" (",
		"). Check that the folder is not read-only and that the disk has room, then run the same command again."),
		assure.NoSideEffect(func() bool { return !turnedOn(only) && !strings.Contains(logText, "Enabled offline download") }))
	if strings.Contains(msg, "Before it stopped") {
		t.Errorf("a failed write on the first store claims a change:\n%s", msg)
	}
}
