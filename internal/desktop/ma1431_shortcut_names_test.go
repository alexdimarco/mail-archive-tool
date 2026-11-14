package desktop

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1431, MA-614, R50
// The owner's ruling on the S1 friction review's F7 (2026-11-13): what opens the choice is
// named MailArchive, not MailArchive Desktop. Positive twin first: the MSI's shortcuts that
// run `start` are found (the Desktop, Start-menu and Startup ones). Each of them is named
// "MailArchive"; no shortcut is named "MailArchive Desktop" (the product, its folders and
// the Uninstall entry keep that name, DC6); the Linux menu entry, which runs `start`, is
// named MailArchive; the guide's way back on Windows and the README and release notes'
// Windows first steps name the MailArchive shortcut. An upgrade leaves no shortcut of the
// old name: the shortcuts are plain (not advertised) shortcuts of the product's own
// component and the MajorUpgrade removes the earlier product, with its shortcuts, before
// the new files are installed (afterInstallInitialize). The upgrade itself on Windows is the
// lab L-W1 (design-menubar §12.3), pending.
func TestMA1431_WhatOpensTheChoiceIsNamedMailArchive(t *testing.T) {
	type shortcut struct{ id, name, args, advertise, component string }
	var shortcuts []shortcut
	upgradeSchedule := ""
	component := ""
	dec := xml.NewDecoder(strings.NewReader(packagingFile(t, "Product.wxs")))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Product.wxs: %v", err)
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
		case "Component":
			component = attrs["Id"]
		case "MajorUpgrade":
			upgradeSchedule = attrs["Schedule"]
		case "Shortcut":
			shortcuts = append(shortcuts, shortcut{attrs["Id"], attrs["Name"], attrs["Arguments"], attrs["Advertise"], component})
		}
	}
	var start []shortcut
	for _, s := range shortcuts {
		if s.args == "start" || strings.HasPrefix(s.args, "start ") {
			start = append(start, s)
		}
	}
	if len(start) != 3 {
		t.Fatalf("twin: %d shortcuts run start, want the Desktop, Start-menu and Startup ones: %+v", len(start), shortcuts)
	}
	for _, s := range start {
		if s.name != "MailArchive" {
			t.Errorf("%s runs %q (the choice) but is named %q, want MailArchive (F7)", s.id, s.args, s.name)
		}
		if s.advertise != "no" || s.component != "CmpShortcutsAndDetection" {
			t.Errorf("%s is not a plain shortcut of the product's component (advertise %q, component %q)", s.id, s.advertise, s.component)
		}
	}
	for _, s := range shortcuts {
		if s.name == "MailArchive Desktop" {
			t.Errorf("%s is still named MailArchive Desktop", s.id)
		}
	}
	if upgradeSchedule != "afterInstallInitialize" {
		t.Errorf("MajorUpgrade is scheduled %q: the earlier product (and its old-named shortcuts) must go before the new ones are made", upgradeSchedule)
	}

	read := func(rel ...string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, rel...)...))
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(string(b)), " ")
	}
	entry := read("packaging", "linux", "mailarchive.desktop")
	if !strings.Contains(entry, "Exec=mailarchive start") || !strings.Contains(entry, "Name=MailArchive ") {
		t.Errorf("the Linux entry runs start but is not named MailArchive: %s", entry)
	}
	if strings.Contains(entry, "Name=MailArchive Desktop") {
		t.Errorf("the Linux entry is still named MailArchive Desktop")
	}
	if !strings.Contains(helpDataFor("windows", true).ComeBack, "open MailArchive from the Start menu") {
		t.Errorf("the guide's way back on Windows: %q", helpDataFor("windows", true).ComeBack)
	}
	if !strings.Contains(read("README.md"), "Then open **MailArchive** from the Start menu") {
		t.Errorf("README's Windows first run does not name the MailArchive shortcut")
	}
	if !strings.Contains(read("packaging", "release-notes.md"), "Open **MailArchive** from the Start menu to begin") {
		t.Errorf("the release notes' Windows first steps do not name the MailArchive shortcut")
	}
}
