package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1268, R12
// The command line's words from the C0c friction review's final look after round 3 (Q3 plural,
// Q5, R10; Q3 singular and Q4 are pinned in MA-1263's test). R10: -enable-offline on a store with
// no Thunderbird profile says it did nothing for that path and why, and that the export goes
// ahead; the exit code stays 0 (the lead's call). Q3: two changes before a stop are "Those
// changes are kept". Q5: a missing ";" says it is often that, and a name with a backslash reads
// "has a setting name with a backslash (an escape code) in it". Positive twin first: a healthy
// profile is turned on with exit 0 and none of these words.
func TestTheFinalLookWordsOnTheCommandLine(t *testing.T) {
	_, store := offlineProfile(t, setOffOwnFolder)
	asStore(t, store)
	if code, msg, logText := enableOfflineRun(t, store); code != 0 || strings.Contains(logText, "did nothing") {
		t.Fatalf("the positive twin: exit %d, %s\n%s", code, msg, logText)
	}

	t.Run("R10: no profile", func(t *testing.T) {
		loose := filepath.Join(t.TempDir(), "copied", "ImapMail", "imap.gmail.com")
		if err := os.MkdirAll(loose, 0o755); err != nil {
			t.Fatal(err)
		}
		asStore(t, loose)
		code, msg, logText := enableOfflineRun(t, loose)
		if code != 0 {
			t.Fatalf("exit %d (the lead kept R10's exit 0): %s", code, msg)
		}
		want := "-enable-offline did nothing for " + loose + ": MailArchive could not find the Thunderbird profile this mail belongs to (the mail may have been copied out of Thunderbird), so it changed no Thunderbird setting. The export goes ahead with the mail as it is."
		if !strings.Contains(assure.Reached(t, logText, "the log"), want) || strings.Contains(logText, "skipping prep") {
			t.Fatalf("the log does not give R10's words:\n%s", logText)
		}
	})

	t.Run("Q3: two changes, then a stop", func(t *testing.T) {
		first, storeA := offlineProfile(t, setOffOwnFolder)
		second, storeB := offlineProfile(t, setOffOwnFolder)
		third, storeC := offlineProfile(t, setOffOwnFolder)
		for _, s := range []string{storeA, storeB, storeC} {
			asStore(t, s)
		}
		restore := tbRunning
		t.Cleanup(func() { tbRunning = restore })
		on := func(p string) bool {
			b, _ := os.ReadFile(filepath.Join(p, "prefs.js"))
			return strings.Contains(string(b), `server1.offline_download", true`)
		}
		tbRunning = func(p string) bool { return p == third && on(first) && on(second) }
		before := snapshot(t, third)
		code, msg, _ := enableOfflineRun(t, storeA, storeB, storeC)
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names("in the Thunderbird profile "+first+"; and for imap.gmail.com (folder imap.gmail.com, server1), in the Thunderbird profile "+second+", by changing prefs.js.",
				"Those changes are kept: they are what -enable-offline is for, and running the same command again leaves them as they are (the log above says where the old prefs.js was backed up).",
				"Why it stopped: Thunderbird looks like it is running for the profile "+third),
			assure.NoSideEffect(func() bool { return sameFiles(t, third, before) }))
	})

	for name, tc := range map[string]struct{ line, words string }{
		"Q5: a missing semicolon": {`user_pref("mail.zz", 1)`,
			`prefs.js line 7 holds text that is not a whole user_pref("name", value); statement (often a missing ; at its end). Thunderbird never writes such a line`},
		"Q5: a name with a backslash": {`user_pref("mail.z\x7a", 1);`,
			// MA-1269: \\ and \" are names Thunderbird writes; \x is not.
			`prefs.js line 7 has a setting name with an escape code Thunderbird never writes in a name (a backslash not followed by \ or "). Thunderbird never writes such a line`},
	} {
		t.Run(name, func(t *testing.T) {
			profile, store := offlineProfile(t, setOffOwnFolder+tc.line+"\n")
			asStore(t, store)
			before := snapshot(t, profile)
			code, msg, _ := enableOfflineRun(t, store)
			assure.Refused(t, code, msg, assure.Code(1), assure.Names(tc.words),
				assure.NoSideEffect(func() bool { return sameFiles(t, profile, before) }))
			if strings.Contains(msg, "holds a setting name holding") {
				t.Errorf("the stutter is back: %s", msg)
			}
		})
	}
}
