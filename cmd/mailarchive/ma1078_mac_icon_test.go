package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// plistDict reads the top-level <dict> of an XML property list as key -> string value
// (<string> values; other value kinds are recorded as their element name).
func plistDict(t *testing.T, src []byte) map[string]string {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(src))
	dec.Strict = false // the DOCTYPE line
	out := map[string]string{}
	depth, key, inKey, inString := 0, "", false, false
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Info.plist does not parse: %v", err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			depth++
			text.Reset()
			inKey = depth == 3 && e.Name.Local == "key"
			inString = depth == 3 && e.Name.Local == "string"
			if depth == 3 && !inKey && !inString && key != "" {
				out[key] = "<" + e.Name.Local + ">"
				key = ""
			}
		case xml.CharData:
			text.Write(e)
		case xml.EndElement:
			if inKey {
				key, inKey = strings.TrimSpace(text.String()), false
			} else if inString && key != "" {
				out[key], key, inString = text.String(), "", false
			}
			depth--
		}
	}
	return assure.Reached(t, out, "Info.plist keys")
}

// covers: MA-1078
// The Mac app shows MailArchive's icon and sits in Finder's Productivity category:
// packaging/macos/Info.plist.in (a valid property list once @VERSION@ is filled in)
// names CFBundleIconFile "mailarchive" and LSApplicationCategoryType
// "public.app-category.productivity", keeping CFBundleExecutable mailarchive-gui;
// build-app.sh, run for real, zips "Mail Archive.app" with
// Contents/Resources/mailarchive.icns byte for byte packaging/icon/mailarchive.icns
// (MA-1077 checks what that file holds), the Info.plist inside the zip carries the two
// keys, and Contents/MacOS/mailarchive-gui is still a link to mailarchive (MA-614).
//
// Prove-fail (recorded in the commit): without the two keys, or without the copy in
// build-app.sh, each red.
func TestMA1078_TheMacAppHasTheIconAndCategory(t *testing.T) {
	root := moduleRoot(t)
	in, err := os.ReadFile(filepath.Join(root, "packaging", "macos", "Info.plist.in"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"CFBundleIconFile":          "mailarchive",
		"LSApplicationCategoryType": "public.app-category.productivity",
		"CFBundleExecutable":        "mailarchive-gui",
	}
	check := func(where string, plist []byte) {
		got := plistDict(t, plist)
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: %s is %q, want %q", where, k, got[k], v)
			}
		}
	}
	check("Info.plist.in", bytes.ReplaceAll(in, []byte("@VERSION@"), []byte("1.0.0")))

	icns, err := os.ReadFile(filepath.Join(root, "packaging", "icon", "mailarchive.icns"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("build-app.sh runs under bash on the release runner")
	}
	if _, err := exec.LookPath("zip"); err != nil {
		t.Skip("zip is not installed here; build-app.sh needs it (the release runner and macOS have it)")
	}
	out := t.TempDir()
	cmd := exec.Command("bash", filepath.Join(root, "packaging", "macos", "build-app.sh"), "v0.0.0")
	cmd.Env = append(os.Environ(), "OUT="+out, "RELEASE_TAG=")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-app.sh: %v\n%s", err, b)
	}
	zr, err := zip.OpenReader(filepath.Join(out, "MailArchive-macos.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	read := func(name string) []byte {
		f := files[name]
		if f == nil {
			t.Errorf("MailArchive-macos.zip has no %s", name)
			return nil
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		b, _ := io.ReadAll(rc)
		return b
	}
	app := "Mail Archive.app/Contents/"
	if got := read(app + "Resources/mailarchive.icns"); got != nil && !bytes.Equal(got, icns) {
		t.Errorf("the app's Resources/mailarchive.icns is not packaging/icon/mailarchive.icns (%d bytes, want %d)", len(got), len(icns))
	}
	if plist := read(app + "Info.plist"); plist != nil {
		check("the app's Info.plist", plist)
	}
	if f := files[app+"MacOS/mailarchive-gui"]; f == nil || f.Mode()&os.ModeSymlink == 0 {
		t.Error("the app's MacOS/mailarchive-gui is not a symbolic link")
	}
}
