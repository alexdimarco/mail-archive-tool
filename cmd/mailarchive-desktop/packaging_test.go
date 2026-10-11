package main

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/instance"
)

func packagingFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "packaging", "mailarchive-desktop", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// covers: MA-376, R23
// The packaging half of the upgrade story (CC22; the Windows lab drill that runs it
// is MA-377, still pending): the files the MSI and the RMM scripts are built from
// say what the code now does. These are static checks of those files, not the drill.
// Install-RMM.ps1 stops running mailarchive-desktop instances before msiexec;
// Product.wxs declares util:CloseApplication for mailarchive-desktop.exe with
// TerminateProcess, a Start Menu "Copy MailArchive link" shortcut (-copy-link) and
// the Startup shortcut's -no-browser, and Build-MSI.ps1 passes WixUtilExtension to
// both WiX tools; the runbook, DEPLOYMENT.txt and the payload readme no longer give
// 8097 or 8099 as an address to open, and verify the install by the shortcut and
// the locked page; Cleanup-CurrentUser.ps1 ends the user's instance and removes the
// lock and instance files under the names the code writes.
func TestPackagingFilesMatchTheNewLaunch(t *testing.T) {
	// Install-RMM.ps1: the stop comes before msiexec.
	install := packagingFile(t, "Install-RMM.ps1")
	stop, msi := strings.Index(install, "Get-Process mailarchive-desktop"), strings.Index(install, "Start-Process msiexec.exe")
	if stop < 0 || msi < 0 || stop > msi || !strings.Contains(install, "Stop-Process") {
		t.Errorf("Install-RMM.ps1 must stop mailarchive-desktop before msiexec (stop at %d, msiexec at %d)", stop, msi)
	}

	// Product.wxs: parse it, never grep an XML file.
	wxs := packagingFile(t, "Product.wxs")
	if strings.Contains(wxs, ":8097") || strings.Contains(wxs, ":8099") {
		t.Errorf("Product.wxs still names the fixed ports 8097/8099")
	}
	var closeApp map[string]string
	shortcuts := map[string]map[string]string{}
	dec := xml.NewDecoder(strings.NewReader(wxs))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Product.wxs is not well-formed XML: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		attrs := map[string]string{}
		for _, a := range se.Attr {
			attrs[a.Name.Local] = a.Value
		}
		switch se.Name.Local {
		case "CloseApplication":
			if se.Name.Space == "http://schemas.microsoft.com/wix/UtilExtension" {
				closeApp = attrs
			}
		case "Shortcut":
			shortcuts[attrs["Id"]] = attrs
		}
	}
	if closeApp == nil || closeApp["Target"] != "mailarchive-desktop.exe" || closeApp["TerminateProcess"] == "" {
		t.Errorf("Product.wxs lacks util:CloseApplication for mailarchive-desktop.exe with TerminateProcess: %v", closeApp)
	}
	if s := shortcuts["CopyLinkShortcut"]; s["Name"] != "Copy MailArchive link" || s["Arguments"] != "-copy-link" || s["Directory"] != "ApplicationProgramsFolder" {
		t.Errorf("the Start Menu shortcut \"Copy MailArchive link\" is missing or wrong: %v", s)
	}
	if shortcuts["StartupShortcut"]["Arguments"] != "-no-browser" {
		t.Errorf("the Startup shortcut lost -no-browser: %v", shortcuts["StartupShortcut"])
	}

	// Build-MSI.ps1: both WiX tools get the Util extension (CloseApplication needs it).
	build := packagingFile(t, "Build-MSI.ps1")
	if strings.Count(build, "-ext WixUtilExtension") < 2 {
		t.Errorf("Build-MSI.ps1 must pass -ext WixUtilExtension to candle and to light")
	}

	// The runbook and the shipped texts: no fixed address to open; verify by the shortcut.
	for _, name := range []string{"README-RMM.md", "DEPLOYMENT.txt", "README-payload.txt"} {
		text := packagingFile(t, name)
		for _, bad := range []string{"http://127.0.0.1:8097", "http://127.0.0.1:8099"} {
			if strings.Contains(text, bad) {
				t.Errorf("%s still gives %s as an address", name, bad)
			}
		}
	}
	rmm := packagingFile(t, "README-RMM.md")
	for _, want := range []string{"locked page", "from its shortcut", "Copy MailArchive link", "-copy-link", "MA-377", "util:CloseApplication", "Install-RMM.ps1", "%LOCALAPPDATA%\\MailArchiveDesktop"} {
		if !strings.Contains(rmm, want) {
			t.Errorf("README-RMM.md lacks %q", want)
		}
	}
	for _, name := range []string{"DEPLOYMENT.txt", "README-payload.txt"} {
		text := strings.Join(strings.Fields(packagingFile(t, name)), " ")
		for _, want := range []string{"private", "link"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}
	if d := packagingFile(t, "DEPLOYMENT.txt"); !strings.Contains(d, "%LOCALAPPDATA%\\MailArchiveDesktop") || !strings.Contains(d, "FORMER") {
		t.Errorf("DEPLOYMENT.txt does not name the per-account state folder and the former address")
	}
	bundle := packagingFile(t, "BUNDLE-CONTENTS.txt")
	if !strings.Contains(bundle, "ends running instances") || !strings.Contains(bundle, "lock + instance files") {
		t.Errorf("BUNDLE-CONTENTS.txt does not say what Install-RMM.ps1 and Cleanup-CurrentUser.ps1 now do")
	}

	// Cleanup-CurrentUser.ps1: it ends the user's instance and removes the state the
	// code writes, under the names the code writes them.
	cleanup := packagingFile(t, "Cleanup-CurrentUser.ps1")
	if !strings.Contains(cleanup, "Get-Process mailarchive-desktop") || !strings.Contains(cleanup, "Stop-Process") {
		t.Errorf("Cleanup-CurrentUser.ps1 does not end the user's running instance")
	}
	var patterns []string
	for _, p := range []string{"desktop*.lock", "desktop-instance*.json", "serve-*.json"} {
		if !strings.Contains(cleanup, "'"+p+"'") {
			t.Errorf("Cleanup-CurrentUser.ps1 does not remove %s", p)
		}
		patterns = append(patterns, p)
	}
	written := []string{instance.ServeFileName("/some/archive")}
	for _, session := range []string{"", "2"} {
		lock, file := instance.Names(session)
		written = append(written, lock, file)
	}
	for _, name := range written {
		matched := false
		for _, p := range patterns {
			if ok, _ := filepath.Match(p, name); ok {
				matched = true
			}
		}
		if !matched {
			t.Errorf("the state file %s the code writes is matched by no pattern Cleanup-CurrentUser.ps1 removes", name)
		}
	}
}
