package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/tbrestore"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1141, R41, S68
// The headless `status` names an earlier restore into Thunderbird that did not finish
// (design-restore-thunderbird §7.2 foldcheck2 note P2), whatever archive it reports on,
// read from restores.log in MailArchive's default settings folder — a read: the log's
// bytes and the Local Folders the same before and after. Positive twin first: no line
// when there is none. Uninstall's plan prints the leftovers it does not remove.
func TestStatusNamesAnUnfinishedRestore(t *testing.T) {
	e := newCLIDel(t)
	archive := e.plainArchive("Kept mail")
	if out, code := statusOf(t, archive); code != 0 || strings.Contains(out, "Restore to Thunderbird") {
		t.Fatalf("positive twin: no restore line:\n%s", out)
	}

	profile := filepath.Join(t.TempDir(), "p.default")
	local := filepath.Join(profile, "Mail", "Local Folders")
	if err := os.MkdirAll(local, 0o700); err != nil {
		t.Fatal(err)
	}
	local, _ = filepath.EvalSymlinks(local)
	profile, _ = filepath.EvalSymlinks(profile)
	prefs := "user_pref(\"mail.server.server1.type\", \"none\");\nuser_pref(\"mail.server.server1.directory\", \"" + strings.ReplaceAll(local, `\`, `\\`) + "\");\n"
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(prefs), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, err := tbrestore.LocalFoldersOf(profile)
	if err != nil {
		t.Fatal(err)
	}
	logPath, err := tbrestore.LogPath()
	if err != nil {
		t.Fatal(err)
	}
	layout, _ := tbrestore.PlanLayout("Old mail", []tbrestore.FolderIn{{Names: []string{"Inbox"}}}, local, "")
	root, err := tbrestore.OpenLocalRoot(lf)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := tbrestore.NewID()
	res := tbrestore.Write(context.Background(), tbrestore.NewRootFS(root), tbrestore.WriteInput{ID: id, ProfileDir: profile, Local: lf,
		Layout: layout, Top: "Restored — Old mail", Log: tbrestore.NewLog(logPath), Messages: func(int, func([]byte) error) error { return nil }})
	root.Close()
	if res.Outcome != tbrestore.OutDone {
		t.Fatal(res.Err)
	}
	// As a crash after "checked" leaves it: no parent file, no closing line.
	data, _ := os.ReadFile(logPath)
	var keep []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if !strings.Contains(l, `"event":"ended"`) {
			keep = append(keep, l)
		}
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(keep, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(local, "Restored — Old mail")); err != nil {
		t.Fatal(err)
	}
	logBefore, _ := os.ReadFile(logPath)
	entries, _ := os.ReadDir(filepath.Join(local, "Restored — Old mail.sbd"))

	out, code := statusOf(t, archive)
	if code != 0 || !strings.Contains(out, "Restore to Thunderbird: An earlier restore into Thunderbird did not finish") || !strings.Contains(out, "Restored — Old mail.sbd") {
		t.Fatalf("status names the unfinished restore:\n%s", out)
	}
	logAfter, _ := os.ReadFile(logPath)
	after, _ := os.ReadDir(filepath.Join(local, "Restored — Old mail.sbd"))
	if !bytes.Equal(logBefore, logAfter) || len(after) != len(entries) {
		t.Fatal("status wrote something")
	}

	var plan bytes.Buffer
	printUninstallPlan(&plan, &uninstall.Report{RestoreLeftovers: []string{"An earlier restore into Thunderbird did not finish. Nothing will clean it up after MailArchive is removed."}}, "")
	if !strings.Contains(plan.String(), "It does NOT remove what an earlier Restore to Thunderbird left (1):") {
		t.Fatalf("uninstall's plan names the leftovers:\n%s", plan.String())
	}
}
