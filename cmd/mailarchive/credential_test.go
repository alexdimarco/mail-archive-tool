package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// fakeM365 stands in for Microsoft 365 — the device-code and token endpoints,
// /me, and one Inbox message addressed both as /me (delegated) and as
// /users/alice@contoso.org (app-only) — so `graph` runs in-process with no
// network. It counts the requests it answers: a refused run reaches it not at all.
type fakeM365 struct{ hits atomic.Int64 }

func newFakeM365(t *testing.T) *fakeM365 {
	t.Helper()
	f := &fakeM365{}
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, s)
	}
	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"device_code":"DEV","user_code":"WXYZ-1234","verification_uri":"https://microsoft.com/devicelogin","expires_in":900,"interval":1}`)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"AT1","refresh_token":"RT1","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
	})
	for _, base := range []string{"/me", "/users/alice@contoso.org"} {
		mux.HandleFunc(base+"/mailFolders", func(w http.ResponseWriter, r *http.Request) {
			j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`)
		})
		mux.HandleFunc(base+"/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
			j(w, `{"value":[{"id":"M1","internetMessageId":"<m1@x>","subject":"subj-M1","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z"}]}`)
		})
		mux.HandleFunc(base+"/messages/", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "From: a@example.com\r\nSubject: subj-M1\r\nMessage-ID: <m1@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody\r\n")
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	prev := graphFake
	graphFake.base, graphFake.token, graphFake.deviceAuth = srv.URL, srv.URL+"/token", srv.URL+"/devicecode"
	t.Cleanup(func() { graphFake = prev })
	return f
}

// graphInProcess runs `mailarchive graph args...` in this process (against the
// fake) and returns the exit code and message main would give.
func graphInProcess(args ...string) (int, string) {
	return refusalOf(runGraph(args))
}

// refusalOf maps a verb's error to the exit code and stderr line main gives it.
func refusalOf(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code, ""
	}
	return 1, "mailarchive: " + err.Error()
}

// primeCache writes a signed-in, unexpired token cache for alice at path.
func primeCache(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func absent(path string) bool {
	_, err := os.Lstat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// capturedPages counts the message pages a capture wrote under out.
func capturedPages(out string) int {
	n := 0
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") && d.Name() != "index.html" {
			n++
		}
		return nil
	})
	return n
}

// covers: MA-375, R12, R17, S45
// `graph` refuses a -token-cache or -client-secret-file inside -out (P10): a
// credential kept in the archive travels with every copy and sync of it. The
// refusal is exit 1 naming the credential, the archive and the default cache
// path, and nothing happens — no sign-in, no token cache, no capture, not one
// request to Microsoft 365 — whether the file sits in -out directly, below a
// parent not created yet, or behind a symlinked parent [CC16]. A cache outside
// -out signs in and captures (positive twin).
func TestGraphRefusesCredentialInsideArchive(t *testing.T) {
	fake := newFakeM365(t)
	device := func(out, cache string) []string {
		return []string{"-auth", "device", "-out", out, "-tenant", "t", "-client-id", "c", "-token-cache", cache}
	}

	out := t.TempDir()
	cache := filepath.Join(t.TempDir(), "graph-token.json")
	if code, msg := graphInProcess(device(out, cache)...); code != 0 {
		t.Fatalf("positive twin (token cache outside -out): exit %d %s", code, msg)
	}
	assure.Reached(t, capturedPages(out), "pages the positive twin captured")
	if absent(cache) {
		t.Errorf("the positive twin saved no sign-in at %s", cache)
	}

	def, err := defaultTokenCachePath("t", "c", nil)
	if err != nil {
		t.Fatal(err)
	}
	inside := t.TempDir()
	secret := filepath.Join(inside, "graph.secret")
	if err := os.WriteFile(secret, []byte("s3cret-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	type refusal struct {
		name, cred string
		args       []string
		names      []string
	}
	cases := []refusal{
		{"token cache in -out", filepath.Join(inside, "graph.token"), nil, []string{def}},
		{"token cache whose parent is not created yet", filepath.Join(inside, "not", "yet", "graph.token"), nil, []string{def}},
		{"client secret file in -out", secret,
			[]string{"-auth", "app", "-out", inside, "-tenant", "t", "-client-id", "c", "-mailbox", "alice@contoso.org", "-client-secret-file", secret},
			[]string{"-client-secret-file"}},
	}
	link := filepath.Join(t.TempDir(), "archive-link")
	if err := os.Symlink(inside, link); err != nil {
		t.Logf("symlinks unavailable here, the symlinked-parent case is not shown: %v", err)
	} else {
		cases = append(cases, refusal{"token cache behind a symlinked parent", filepath.Join(link, "graph.token"), nil, []string{def}})
	}
	for _, c := range cases {
		args := c.args
		if args == nil {
			args = device(inside, c.cred)
		}
		before := fake.hits.Load()
		code, msg := graphInProcess(args...)
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names(append([]string{c.cred, inside, "is inside the archive folder", "Why:", "Do this:"}, c.names...)...),
			assure.Forbid("s3cret-value"),
			assure.NoSideEffect(func() bool {
				return fake.hits.Load() == before &&
					(c.cred == secret || absent(c.cred)) &&
					absent(filepath.Join(inside, ".mailarchive-manifest.json")) &&
					absent(filepath.Join(inside, state.LastRunName))
			}))
	}
}

// covers: MA-375, R12, R17, S45
// With -unattended — every scheduled job carries it — a credential inside -out
// does not stop the backup (refusing at run time would silently stop the backups
// of existing installs), whether it is a device job's token cache or an app-only
// job's client-secret file: the capture runs, the run log carries the warning,
// the last-run record carries it, and `status` WARNs credential_in_archive
// [CC16]. The secret itself never reaches the log.
func TestGraphUnattendedCredentialInsideArchiveWarns(t *testing.T) {
	newFakeM365(t)
	for _, c := range []struct {
		name  string   // the case
		file  string   // the credential's name inside -out
		names string   // how the warning names it
		auth  []string // the auth flags, ending with the credential's own flag
	}{
		{"device token cache", "graph.token", "the token cache ", []string{"-auth", "device", "-token-cache"}},
		{"app-only client secret file", "graph.secret", "the client secret file ", []string{"-auth", "app", "-mailbox", "alice@contoso.org", "-client-secret-file"}},
	} {
		out := t.TempDir()
		cred := filepath.Join(out, c.file)
		if c.file == "graph.token" {
			primeCache(t, cred)
		} else if err := os.WriteFile(cred, []byte("s3cret-value"), 0o600); err != nil {
			t.Fatal(err)
		}
		logPath := filepath.Join(t.TempDir(), "graph.log")
		args := append([]string{"-unattended", "-out", out, "-tenant", "t", "-client-id", "c", "-log", logPath}, append(c.auth, cred)...)
		code, msg := graphInProcess(args...)
		if code != 0 {
			t.Fatalf("%s: an unattended run must keep running with its credential inside the archive: exit %d %s", c.name, code, msg)
		}
		assure.Reached(t, capturedPages(out), c.name+": pages the unattended capture wrote")
		logged, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		if want := "warning: " + c.names + cred + " is inside the archive folder " + out; !strings.Contains(string(logged), want) {
			t.Errorf("%s: the run log lacks %q:\n%s", c.name, want, logged)
		}
		if strings.Contains(string(logged), "s3cret-value") {
			t.Errorf("%s: the run log carries the client secret itself", c.name)
		}
		lr, _, _ := state.ReadLastRun(out)
		if len(lr.Warnings) != 1 || lr.Warnings[0].Code != "credential_in_archive" || !strings.Contains(lr.Warnings[0].Message, cred) {
			t.Errorf("%s: the last-run record carries warnings %+v, want one credential_in_archive naming %s", c.name, lr.Warnings, cred)
		}
		code, stdout, stderr := runCLIOut(t, "status", "-out", out, "-json")
		if code != 0 {
			t.Fatalf("%s: status: exit %d %s", c.name, code, stderr)
		}
		var doc struct {
			Posture     string   `json:"posture"`
			ReasonCodes []string `json:"reason_codes"`
		}
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
			t.Fatalf("%s: status -json: %v\n%s", c.name, err, stdout)
		}
		found := false
		for _, rc := range doc.ReasonCodes {
			found = found || rc == "credential_in_archive"
		}
		if !found || doc.Posture == "GREEN" {
			t.Errorf("%s: status is %s with reason codes %v, want a WARN carrying credential_in_archive", c.name, doc.Posture, doc.ReasonCodes)
		}
	}
}

// covers: MA-375, R12, R17, S45
// MailArchive's own OS config folder is exempt when the archive contains it (an
// -out that is the home folder): interactive `graph` with its defaults signs in,
// saves the sign-in at the default path inside that folder and captures [CC16].
// The exemption is for an archive that contains the folder, not one that is it.
func TestGraphWithDefaultsProceedsWhenOutIsTheHomeFolder(t *testing.T) {
	newFakeM365(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	cfgDir, err := graphconfig.DefaultConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if !util.Within(util.ResolveExisting(home), util.ResolveExisting(cfgDir)) {
		t.Fatalf("the OS config folder %s is not inside the fake home %s", cfgDir, home)
	}

	if code, msg := graphInProcess("-auth", "device", "-out", home, "-tenant", "t", "-client-id", "c"); code != 0 {
		t.Fatalf("graph with -out = the home folder and default settings: exit %d %s", code, msg)
	}
	assure.Reached(t, capturedPages(home), "pages captured into the home folder")
	def, err := defaultTokenCachePath("t", "c", nil)
	if err != nil {
		t.Fatal(err)
	}
	if absent(def) {
		t.Errorf("no sign-in was saved at the default %s", def)
	}

	code, msg := graphInProcess("-auth", "device", "-out", cfgDir, "-tenant", "t2", "-client-id", "c2")
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("is inside the archive folder", cfgDir, "outside"))
}

// covers: MA-375, R12, R14, S45
// `schedule` refuses a graph job whose -token-cache or -client-secret-file is
// inside -out, naming both paths, and installs nothing (the operator is present
// at install time); a cache outside -out previews (positive twin). The refusal
// comes from parseJob, the validator `schedule -install` runs before it
// installs anything, so the preview and -install refuse alike — and no test
// ever reaches a real scheduler.
func TestScheduleRefusesCredentialInsideArchive(t *testing.T) {
	out := t.TempDir()
	device := []string{"graph", "-auth", "device", "-out", out, "-tenant", "contoso.com", "-client-id", "APPID"}
	outside := filepath.Join(t.TempDir(), "graph-token.json")
	if runtime.GOOS == "windows" {
		// On Windows the device sign-in lives in the OS vault (Credential Manager), not
		// the -token-cache file, and a subprocess reads the real vault. Stand one in
		// (standInVault overrides the in-process deviceTokenStore the check resolves) and
		// validate in-process, as TestScheduleDeviceJobChecksTheVault does.
		vault := standInVault(t, "contoso.com", "APPID")
		if err := vault.StoreToken([]byte(`{"upn":"a@contoso.com","token":{"refresh_token":"RT1"}}`)); err != nil {
			t.Fatal(err)
		}
		if _, err := parseJob(append(append([]string{}, device...), "-token-cache", outside)); err != nil {
			t.Fatalf("positive twin (token cache outside -out) was refused: %v", err)
		}
	} else {
		primeCache(t, outside)
		if code, _, stderr := runCLIOut(t, append(append([]string{"schedule", "--"}, device...), "-token-cache", outside)...); code != 0 {
			t.Fatalf("positive twin (token cache outside -out): exit %d %s", code, stderr)
		}
	}

	noDescriptor := func() bool { return absent(filepath.Join(out, ".mailarchive-schedule.json")) }
	inside := filepath.Join(out, "graph-token.json")
	primeCache(t, inside)
	code, _, stderr := runCLIOut(t, append(append([]string{"schedule", "--"}, device...), "-token-cache", inside)...)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names(inside, out, "is inside the archive folder", "-token-cache"), assure.NoSideEffect(noDescriptor))
	code, msg := parseJobRefusal(append(append([]string{}, device...), "-token-cache", inside))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(inside, out, "is inside the archive folder"))

	secret := filepath.Join(out, "graph.secret")
	if err := os.WriteFile(secret, []byte("s3cret-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := []string{"graph", "-out", out, "-tenant", "t", "-client-id", "c", "-mailbox", "m@x", "-client-secret-file", secret}
	code, _, stderr = runCLIOut(t, append([]string{"schedule", "--"}, app...)...)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names(secret, out, "is inside the archive folder", "-client-secret-file"),
		assure.Forbid("s3cret-value"), assure.NoSideEffect(noDescriptor))
	code, msg = parseJobRefusal(app)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(secret, out), assure.Forbid("s3cret-value"))
}

// parseJobRefusal validates a scheduled job the way `schedule` does and returns
// the exit code and message main would give its error.
func parseJobRefusal(args []string) (int, string) {
	_, err := parseJob(args)
	return refusalOf(err)
}
