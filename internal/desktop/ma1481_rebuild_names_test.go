package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
)

// qzServer is one Thunderbird IMAP account on imap.qzg.example for qzPrefs: its number, the name
// Thunderbird shows, and its folder (inside the profile as directory-rel, else as directory).
type qzServer struct {
	n         int
	name, rel string
	dir       string
}

// qzPrefs writes the profile's prefs.js with the servers, and the compatibility.ini that makes the
// folder a profile Thunderbird has opened.
func qzPrefs(t *testing.T, profile string, servers ...qzServer) {
	t.Helper()
	compat := "[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\nLastOSABI=Darwin_aarch64-gcc3\n"
	if err := os.WriteFile(filepath.Join(profile, "compatibility.ini"), []byte(compat), 0o600); err != nil {
		t.Fatal(err)
	}
	str := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	var b strings.Builder
	var ids []string
	for _, s := range servers {
		key := fmt.Sprintf("server%d", s.n)
		ids = append(ids, fmt.Sprintf("account%d", s.n))
		fmt.Fprintf(&b, "user_pref(%q, %s);\n", "mail.account.account"+fmt.Sprint(s.n)+".server", str(key))
		fmt.Fprintf(&b, "user_pref(%q, %s);\n", "mail.server."+key+".hostname", str("imap.qzg.example"))
		fmt.Fprintf(&b, "user_pref(%q, %s);\n", "mail.server."+key+".type", str("imap"))
		fmt.Fprintf(&b, "user_pref(%q, %s);\n", "mail.server."+key+".name", str(s.name))
		if s.rel != "" {
			fmt.Fprintf(&b, "user_pref(%q, %s);\n", "mail.server."+key+".directory-rel", str("[ProfD]"+s.rel))
		}
		if s.dir != "" {
			fmt.Fprintf(&b, "user_pref(%q, %s);\n", "mail.server."+key+".directory", str(s.dir))
		}
	}
	fmt.Fprintf(&b, "user_pref(%q, %s);\n", "mail.accountmanager.accounts", str(strings.Join(ids, ",")))
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1473, MA-1481, R14
// The Desktop's schedule rebuild asks the run's own rule with the archive record's names (the 1.7.5
// re-verify, round 3's RV3-03): it keeps a gone input the run skips and refuses one the run stops
// for, also where the server-name rule alone would say the opposite. Three accounts on one mail
// server; the archive holds the first two, the third has always kept its mail off the profile; the
// record is a stopped backup's, which names the folders (RV3-01). The second account's folder deleted
// by hand (Thunderbird still names it): the run skips it (Q4), the server-name rule alone would stop
// (one same-server account outside). The second account moved to another drive: the run stops, the
// server-name rule alone would skip (two outside). Positive twin first: every input there rebuilds.
func TestTheRebuildAsksWithTheRecordsNames(t *testing.T) {
	_, _, profile := goneHome(t)
	a := filepath.Join(profile, "ImapMail", "imap.qzg.example")
	b := filepath.Join(profile, "ImapMail", "imap.qzg-1.example")
	third := filepath.Join(t.TempDir(), "QZUSB", "QZGmail3")
	for _, d := range []string{a, b, third} {
		mailIn(t, d)
	}
	sa := qzServer{n: 1, name: "qza@qzg.example", rel: "ImapMail/imap.qzg.example"}
	sb := qzServer{n: 2, name: "qzb@qzg.example", rel: "ImapMail/imap.qzg-1.example"}
	sc := qzServer{n: 3, name: "qzc@qzg.example", dir: third}
	qzPrefs(t, profile, sa, sb, sc)
	out := archiveOf(t, true, a, b)
	if j, ok := reconstructLocalJob(out); !ok || len(j.Inputs) != 2 {
		t.Fatalf("positive twin: every input there rebuilds: %v %+v", ok, j)
	}
	// The record a stopped backup writes: failed, with the folders' names carried (RV3-01).
	lr, _, _ := state.ReadLastRun(out)
	lr.Status, lr.Error, lr.Thunderbird = state.RunFailed, "qz stopped", nil
	lr.ThunderbirdNames = []state.TBName{{StoreID: autoplace.StoreID(profile, a), Name: sa.name}, {StoreID: autoplace.StoreID(profile, b), Name: sb.name}}
	if err := state.WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	inputs := []string{a, b}
	runSkips := func() bool {
		_, skipped, err := app.DiscoverInputsFor(inputs, true, app.RecordedNamesAt(out))
		return err == nil && len(skipped) == 1 && skipped[0] == b
	}

	// The second account's folder deleted by hand: the run skips it, and the rebuild keeps it.
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	if !runSkips() {
		t.Fatal("setup: the run skips a folder deleted by hand")
	}
	if j, ok := reconstructLocalJob(out); !ok || len(j.Inputs) != 2 || j.Inputs[1] != b {
		t.Errorf("the rebuild refuses a gone mailbox the run skips (deleted by hand): %v %+v", ok, j)
	}

	// The second account moved to another drive: the run stops, and the rebuild refuses.
	away := filepath.Join(t.TempDir(), "QZUSB", "QZGmail2")
	mailIn(t, away)
	sb.rel, sb.dir = "", away
	qzPrefs(t, profile, sa, sb, sc)
	if runSkips() {
		t.Fatal("setup: the run stops for a moved account")
	}
	if j, ok := reconstructLocalJob(out); ok {
		t.Errorf("the rebuild keeps a gone mailbox the run stops for (moved): %+v", j)
	}
}
