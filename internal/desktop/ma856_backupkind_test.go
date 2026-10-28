package desktop

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-856, R37
// The backup kind is read from the archive's own record (design-desktop-settings-card 3.1):
// local mail files are Local; Microsoft 365, mixed and empty are Microsoft 365, as is a folder
// that holds no record yet (a new archive); a record that can't be read — the folder gone, as
// when its drive is unplugged, or a damaged record — is Unknown, never Microsoft 365 (C-8).
func TestBackupKindIsReadFromTheArchiveRecord(t *testing.T) {
	local := plainMboxArchive(t)
	m365 := emptyArchive(t)
	setStores(t, m365, "mailbox:a@contoso.org")
	mixed := plainMboxArchive(t)
	addStore(t, mixed, "mailbox:a@contoso.org")
	damaged := t.TempDir()
	if err := os.WriteFile(filepath.Join(damaged, manifestName), []byte("{not a record"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, out, want string }{
		{"local", local, backupLocal},
		{"m365", m365, backupM365},
		{"mixed", mixed, backupM365},
		{"empty", emptyArchive(t), backupM365},
		{"a new folder with no record", t.TempDir(), backupM365},
		{"the folder gone (its drive unplugged)", filepath.Join(t.TempDir(), "gone"), backupUnknown},
		{"a damaged record", damaged, backupUnknown},
		{"no location", "", backupUnknown},
	} {
		if got := backupKind(c.out); got != c.want {
			t.Errorf("%s: backupKind = %q, want %q", c.name, got, c.want)
		}
	}
}

// covers: MA-856, R37
// The kind is never the tenant: a local archive on a computer with a Microsoft 365 tenant saved
// (where no capture is counted for it) installs a run-job of its mail files, never a capture.
// Before, localOnly() read the tenant and sent it the capture way. Positive twin first: with no
// tenant it installs the run-job.
func TestALocalArchiveIsJudgedByItsRecordNotTheTenant(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) || len(g.installs) != 1 || g.installs[0].Args[0] != "-job" {
		t.Fatalf("the positive twin, no tenant: %s %+v", body, g.installs)
	}
	os.Remove(g.jobPath)

	cfgPath := filepath.Join(t.TempDir(), "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "app"}); err != nil {
		t.Fatal(err)
	}
	gt := newA3Rig(t, out, "darwin", func(c *Config) { c.ConfigPath = cfgPath })
	body := gt.post(`{"interval":"daily","time":"04:00"}`)
	if !strings.Contains(body, `"ok":true`) || len(gt.installs) != 1 {
		t.Fatalf("a local archive with a tenant saved: %s", body)
	}
	if args := strings.Join(gt.installs[0].Args, " "); gt.installs[0].Args[0] != "-job" || strings.Contains(args, "-capture") {
		t.Errorf("a local archive was given a Microsoft 365 capture because a tenant is saved: %s", args)
	}
}

// covers: MA-856, R37
// An archive whose record can't be read is Unknown (C-8): its card shows the location only and
// the line saying why, with no schedule inputs, and an install is refused in plain words before
// anything is written — scheduler, descriptor, job file and settings file. Positive twin first:
// the same archive with its record readable installs.
func TestAnUnreadableRecordIsUnknownAndRefusedBeforeAnyWrite(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin: %s", body)
	}
	os.Remove(g.jobPath)
	g.installs, g.fake.tasks = nil, map[string]bool{}

	// The drive is unplugged: the archive folder is gone.
	away := out + "-away"
	if err := os.Rename(out, away); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Rename(away, out) })
	page := g.page()
	if !shows(page, unknownKindLine) || !strings.Contains(page, `id="backupUnknown"`) {
		t.Errorf("the card does not say the record can't be read")
	}
	for _, bad := range []string{`id="schedInterval"`, `id="schedInstall"`, `id="schedRepair"`, `id="runBackupBtn"`} {
		if strings.Contains(page, bad) {
			t.Errorf("an Unknown archive's card renders %s", bad)
		}
	}
	u := g.snapshot()
	body := g.post(`{"interval":"daily","time":"04:00"}`)
	assure.Refused(t, refusedCode(body), body, assure.Names(unknownKindRefusal), assure.NoSideEffect(u.holds))
	if fileBytes(g.jobPath) != nil {
		t.Error("the refused install left a job file")
	}

	// A damaged record is Unknown too.
	if err := os.Rename(away, out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, manifestName), []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	u = g.snapshot()
	body = g.post(`{"interval":"daily","time":"04:00"}`)
	assure.Refused(t, refusedCode(body), body, assure.Names(unknownKindRefusal), assure.NoSideEffect(u.holds))
}

// covers: MA-856, R37
// Microsoft 365, mixed and empty archives go the capture way, as a new folder with no record does,
// where no run-job is installed: the install is handed a capture. Archive now is unchanged: it
// still follows the tenant (localOnly), offered on a local archive where a tenant is saved.
func TestMicrosoft365MixedAndEmptyArchivesGoTheCaptureWay(t *testing.T) {
	isolateHome(t)
	cfgPath := filepath.Join(t.TempDir(), "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "app"}); err != nil {
		t.Fatal(err)
	}
	m365 := emptyArchive(t)
	setStores(t, m365, "mailbox:a@contoso.org")
	mixed := plainMboxArchive(t)
	addStore(t, mixed, "mailbox:a@contoso.org")
	for name, out := range map[string]string{"m365": m365, "mixed": mixed, "empty": emptyArchive(t), "no record yet": t.TempDir()} {
		g := newA3Rig(t, out, "darwin", func(c *Config) { c.ConfigPath = cfgPath })
		body := g.post(`{"interval":"weekly","time":"03:00"}`)
		if !strings.Contains(body, `"ok":true`) || len(g.installs) != 1 || !strings.Contains(strings.Join(g.installs[0].Args, " "), "-capture") {
			t.Errorf("%s: not the capture way: %s %+v", name, body, g.installs)
		}
	}

	local := plainMboxArchive(t)
	with := Config{Out: local, ConfigPath: cfgPath, SettingsPath: filepath.Join(t.TempDir(), "a.json")}
	if line := with.archiveNowOffLine(); line != "" {
		t.Errorf("Archive now changed: a local archive with a tenant saved no longer offers it: %q", line)
	}
	without := Config{Out: local, SettingsPath: filepath.Join(t.TempDir(), "b.json")}
	if without.archiveNowOffLine() == "" {
		t.Error("Archive now changed: a local archive with no tenant now offers it")
	}
}

// covers: MA-856, R37
// The kind guard is called unchanged on every install (part A changes neither refuseCollision
// nor CheckKind): every Desktop route installs through one installer whose default is
// schedule.Install, no Desktop code calls the guard or the host installer itself, and Install
// still runs refuseCollision, which builds the owner-side evidence and calls CheckKind, before
// the scheduler step.
func TestEveryDesktopInstallGoesThroughTheUnchangedGuard(t *testing.T) {
	d := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	if reflect.ValueOf(d.installer()).Pointer() != reflect.ValueOf(schedule.Install).Pointer() {
		t.Fatal("the Desktop's installer is not schedule.Install")
	}
	// The code itself, comments aside: every use of schedule.<name> in the package's files.
	uses := map[string][]string{}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "schedule" {
					uses[sel.Sel.Name] = append(uses[sel.Sel.Name], f)
				}
			}
			return true
		})
	}
	if got := uses["Install"]; len(got) != 1 || got[0] != "schedule.go" {
		t.Errorf("schedule.Install is reached outside the one installer: %v", got)
	}
	if got := uses["CheckKind"]; len(got) != 0 {
		t.Errorf("Desktop code calls the guard's CheckKind itself: %v", got)
	}
	if got := uses["CaptureCorroborator"]; len(got) != 1 || got[0] != "kindcorroborate.go" {
		t.Errorf("the capture corroborator is set outside its one place: %v", got)
	}
	src, err := os.ReadFile(filepath.Join("..", "schedule", "schedule.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(src), "\r\n", "\n") // a Windows checkout's line ends
	inst := s[strings.Index(s, "func installOn("):]
	if i, j := strings.Index(inst, "refuseCollision(s, goos)"), strings.Index(inst, "switch goos"); i < 0 || j < 0 || i > j {
		t.Error("Install no longer runs refuseCollision before the scheduler step")
	}
	rc := s[strings.Index(s, "func refuseCollision("):]
	if !strings.Contains(rc[:strings.Index(rc, "\n}\n")], "CheckKind(BuildKindEvidence(s.Out), KindOf(s.Args), recordedKind(s.Out), s.Out, s.Name)") {
		t.Error("refuseCollision no longer calls CheckKind on the owner-side evidence")
	}
}
