package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// linuxEntryAsset is the release asset that adds MailArchive to a Linux app menu.
const linuxEntryAsset = "mailarchive-linux-desktop-entry"

// desktopEntry parses a freedesktop .desktop file's [Desktop Entry] group, failing on
// a line that is not a comment, a group header or key=value, and on a repeated key.
func desktopEntry(t *testing.T, src string) map[string]string {
	t.Helper()
	keys := map[string]string{}
	group := ""
	for i, ln := range strings.Split(strings.TrimRight(src, "\n"), "\n") {
		switch {
		case ln == "" || strings.HasPrefix(ln, "#"):
		case strings.HasPrefix(ln, "[") && strings.HasSuffix(ln, "]"):
			if group == "" && ln != "[Desktop Entry]" {
				t.Errorf(".desktop: the first group is %s, want [Desktop Entry]", ln)
			}
			group = ln
		default:
			k, v, ok := strings.Cut(ln, "=")
			if !ok || group == "" || !regexp.MustCompile(`^[A-Za-z0-9-]+(\[[^\]]+\])?$`).MatchString(k) {
				t.Errorf(".desktop line %d is not key=value in a group: %q", i+1, ln)
				continue
			}
			if group != "[Desktop Entry]" {
				continue
			}
			if _, dup := keys[k]; dup {
				t.Errorf(".desktop: %s is set twice", k)
			}
			keys[k] = v
		}
	}
	return assure.Reached(t, keys, ".desktop keys")
}

// covers: MA-1080
// Linux gets MailArchive's icon in one new release asset, and its two programs stay as
// they are: build-release.sh still builds the bare mailarchive-linux-amd64 and -arm64
// with the same two lines, and calls packaging/linux/build-desktop-entry.sh "$DIST"
// after them and before the checksums, so SHA256SUMS lists the new file. That script,
// run for real, writes mailarchive-linux-desktop-entry.tar.gz holding exactly the
// folder mailarchive-linux-desktop-entry/ with mailarchive.png (packaging/icon's
// square 256 px PNG), mailarchive.svg (its square drawing), mailarchive.desktop and
// READ ME.txt. The .desktop is a valid [Desktop Entry]: Type=Application,
// Name=MailArchive Desktop, Exec=mailarchive desktop, Icon=mailarchive,
// Terminal=false, Categories ending in ";". READ ME.txt has exactly three install
// lines, which put the program at ~/.local/bin/mailarchive, the PNG at
// ~/.local/share/icons/hicolor/256x256/apps/mailarchive.png (the name Icon finds) and
// the .desktop in ~/.local/share/applications/ with Exec naming the program by its full
// path (friction proposal P1, 2026-11-04); on Linux the three lines are run for real
// for a home folder with a space in its name.
//
// Prove-fail (recorded in the commit): build-release.sh without the call, or the
// script without the SVG, each red.
func TestMA1080_LinuxDesktopEntryAsset(t *testing.T) {
	root := moduleRoot(t)
	br := readRepo(t, "packaging/build-release.sh")
	for _, want := range []string{
		`GOOS=linux   GOARCH=amd64 go build -o "$DIST/mailarchive-linux-amd64" $ONE`,
		`GOOS=linux   GOARCH=arm64 go build -o "$DIST/mailarchive-linux-arm64" $ONE`,
	} {
		if !strings.Contains(br, want) {
			t.Errorf("build-release.sh no longer builds the bare Linux program with %q", want)
		}
	}
	call := `bash "$ROOT/packaging/linux/build-desktop-entry.sh" "$DIST"`
	if !inOrder(br, `"$DIST/mailarchive-linux-arm64" $ONE`, call, `sha256sum $(ls`) {
		t.Errorf("build-release.sh must run %s after the Linux programs and before the checksums", call)
	}

	if runtime.GOOS == "windows" {
		t.Skip("build-desktop-entry.sh runs under bash on the release runner")
	}
	dist := t.TempDir()
	cmd := exec.Command("bash", filepath.Join(root, "packaging", "linux", "build-desktop-entry.sh"), dist)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-desktop-entry.sh: %v\n%s", err, b)
	}
	ents, _ := os.ReadDir(dist)
	if len(ents) != 1 || ents[0].Name() != linuxEntryAsset+".tar.gz" {
		t.Fatalf("build-desktop-entry.sh wrote %v, want only %s.tar.gz", ents, linuxEntryAsset)
	}
	f, err := os.Open(filepath.Join(dist, linuxEntryAsset+".tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	members := map[string][]byte{}
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		name := strings.TrimPrefix(h.Name, "./")
		names = append(names, name)
		members[name] = b
		if h.Typeflag == tar.TypeReg && h.Mode&0o777 != 0o644 {
			t.Errorf("%s has mode %o, want 644", name, h.Mode&0o777)
		}
	}
	sort.Strings(names)
	dir := linuxEntryAsset + "/"
	want := []string{dir, dir + "READ ME.txt", dir + "mailarchive.desktop", dir + "mailarchive.png", dir + "mailarchive.svg"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Errorf("the tarball holds %q, want exactly %q", names, want)
	}

	if !bytes.Equal(members[dir+"mailarchive.png"], readRepoBytes(t, "packaging/icon/png/square-256.png")) {
		t.Error("mailarchive.png is not packaging/icon/png/square-256.png")
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(members[dir+"mailarchive.png"])); err != nil || cfg.Width != 256 || cfg.Height != 256 {
		t.Errorf("mailarchive.png is not a 256 px PNG (%v)", err)
	}
	if !bytes.Equal(members[dir+"mailarchive.svg"], readRepoBytes(t, "packaging/icon/mailarchive-square.svg")) {
		t.Error("mailarchive.svg is not packaging/icon/mailarchive-square.svg")
	}

	keys := desktopEntry(t, string(members[dir+"mailarchive.desktop"]))
	for k, v := range map[string]string{
		"Type": "Application", "Name": "MailArchive Desktop", "Exec": "mailarchive desktop",
		"Icon": "mailarchive", "Terminal": "false",
	} {
		if keys[k] != v {
			t.Errorf(".desktop: %s=%q, want %q", k, keys[k], v)
		}
	}
	if c := keys["Categories"]; c == "" || !strings.HasSuffix(c, ";") {
		t.Errorf(".desktop: Categories=%q must be set and end in ;", c)
	}

	// The install lines are the READ ME's lines that start with install or sed. Since
	// proposal P1 of the friction review (settled 2026-11-04), the third line writes the
	// menu entry with the program's full path, $HOME expanded by the shell when it runs
	// and quoted for a home folder with a space, because a .desktop file expands neither
	// ~ nor $HOME and GLib drops an entry whose program is not on the session's PATH.
	var installs []string
	for _, ln := range strings.Split(string(members[dir+"READ ME.txt"]), "\n") {
		if s := strings.TrimSpace(ln); strings.HasPrefix(s, "install ") || strings.HasPrefix(s, "sed ") {
			installs = append(installs, s)
		}
	}
	wantInstalls := []string{
		"install -D -m 755 mailarchive-linux-amd64 ~/.local/bin/mailarchive",
		"install -D -m 644 mailarchive.png ~/.local/share/icons/hicolor/256x256/apps/mailarchive.png",
		`sed "s|^Exec=mailarchive |Exec=\"$HOME/.local/bin/mailarchive\" |" mailarchive.desktop | install -D -m 644 /dev/stdin ~/.local/share/applications/mailarchive.desktop`,
	}
	if strings.Join(installs, "\n") != strings.Join(wantInstalls, "\n") {
		t.Errorf("READ ME.txt's install lines are\n%s\nwant\n%s", strings.Join(installs, "\n"), strings.Join(wantInstalls, "\n"))
	}
	if runtime.GOOS == "linux" {
		runInstallLines(t, members, dir, installs)
	}

	// The friction review of 2026-11-04 (docs/review-linux-desktop-entry-friction.md):
	// GNOME leaves the entry out of the menu while ~/.local/bin is not on the session's
	// PATH, so the READ ME says what to do when it is MISSING, gives a way to start it
	// that needs no PATH, and sends the person to Uninstall MailArchive (which stops the
	// scheduled backups) before the program file is deleted.
	prose := strings.Join(strings.Fields(string(members[dir+"READ ME.txt"])), " ")
	for _, want := range []string{
		"If MailArchive Desktop is not in your app menu yet",
		"sign out and back in once",
		"~/.local/bin/mailarchive desktop",
		"Uninstall MailArchive",
	} {
		if !strings.Contains(prose, want) {
			t.Errorf("READ ME.txt does not say %q", want)
		}
	}
	if u, del := strings.Index(prose, "Uninstall MailArchive"), strings.Index(prose, "delete the three files"); u < 0 || del < 0 || u > del {
		t.Error("READ ME.txt must name Uninstall MailArchive before it says to delete the three files")
	}
}

// runInstallLines runs the READ ME's three lines for real with bash, in a copy of the
// unpacked folder (with a stand-in program file), for a home folder whose name has a
// space, and checks what they made: the program (755), the icon (644, the tarball's
// PNG) and the menu entry (644), which is the shipped entry with only its Exec line
// changed to the program's full path, quoted. Linux only: the lines use GNU install.
func runInstallLines(t *testing.T, members map[string][]byte, dir string, lines []string) {
	t.Helper()
	folder := t.TempDir()
	for name, b := range members {
		if strings.HasSuffix(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(folder, strings.TrimPrefix(name, dir)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prog := []byte("#!/bin/sh\necho stand-in\n")
	if err := os.WriteFile(filepath.Join(folder, "mailarchive-linux-amd64"), prog, 0o644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "pat smith")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, ln := range assure.Reached(t, lines, "install lines") {
		cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", ln)
		cmd.Dir = folder
		cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("READ ME line %q: %v\n%s", ln, err, b)
		}
	}
	for rel, want := range map[string]struct {
		mode os.FileMode
		body []byte
	}{
		".local/bin/mailarchive":                                  {0o755, prog},
		".local/share/icons/hicolor/256x256/apps/mailarchive.png": {0o644, members[dir+"mailarchive.png"]},
	} {
		p := filepath.Join(home, rel)
		fi, err := os.Stat(p)
		if err != nil {
			t.Errorf("the three lines did not make ~/%s: %v", rel, err)
			continue
		}
		if fi.Mode().Perm() != want.mode {
			t.Errorf("~/%s has mode %o, want %o", rel, fi.Mode().Perm(), want.mode)
		}
		if got, _ := os.ReadFile(p); !bytes.Equal(got, want.body) {
			t.Errorf("~/%s is not the file the line installs", rel)
		}
	}
	entry := filepath.Join(home, ".local/share/applications/mailarchive.desktop")
	fi, err := os.Stat(entry)
	if err != nil {
		t.Fatalf("the third line did not make the menu entry: %v", err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("the installed menu entry has mode %o, want 644", fi.Mode().Perm())
	}
	got, _ := os.ReadFile(entry)
	shipped := string(members[dir+"mailarchive.desktop"])
	wantExec := `Exec="` + home + `/.local/bin/mailarchive" desktop`
	if want := strings.Replace(shipped, "\nExec=mailarchive desktop\n", "\n"+wantExec+"\n", 1); want == shipped || string(got) != want {
		t.Errorf("the installed menu entry is\n%s\nwant the shipped entry with only %s", got, wantExec)
	}
	if keys := desktopEntry(t, string(got)); keys["Exec"] != strings.TrimPrefix(wantExec, "Exec=") {
		t.Errorf("installed .desktop: Exec=%q", keys["Exec"])
	}
}

// readRepoBytes reads a file under the module root.
func readRepoBytes(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return assure.Reached(t, b, rel)
}
