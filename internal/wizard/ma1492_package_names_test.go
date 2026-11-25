package wizard

import (
	"strings"
	"testing"
	"unicode/utf8"

	"mail-archive-tool/internal/app"
)

// covers: MA-1492, S90
// A folder name never ends as an app, a package or a shell folder (the adversarial pass's OUT-02; the
// lead's call: a name ending in a macOS package extension, .app, .bundle, .pkg, .rtfd, .framework,
// .plugin or .kext, or in .{GUID} gets " (mail)" added, so the folder still reads as the address and
// opens as a folder, which keeps the owner's ruling 3). ".app" is a real email domain ending: Finder
// would show "me@mydomain.app" as an app that will not open, and File Explorer opens a
// "Work.{645FF040-…}" folder as the Recycle Bin. Positive twin first: an ordinary address, and endings
// that only look alike (.appx, .apps, a brace that is not a GUID), are kept as they are. Every ending
// in any letter case gets " (mail)"; a name at the 60-character bound is cut to make room, never past
// 60, and still passes the copy export's folder-name check.
func TestAFolderNameNeverEndsAsAnAppOrAShellFolder(t *testing.T) {
	for _, n := range []string{"me@mydomain.com", "me@mydomain.appx", "me@my.apps", "Work.{not-a-guid}", "Local Folders"} {
		if got := setSafeName(n, ""); got != n {
			t.Errorf("positive twin: %q became %q", n, got)
		}
	}
	for n, want := range map[string]string{
		"me@mydomain.app":  "me@mydomain.app (mail)",
		"ME@MYDOMAIN.APP":  "ME@MYDOMAIN.APP (mail)",
		"Notes.rtfd":       "Notes.rtfd (mail)",
		"x.bundle":         "x.bundle (mail)",
		"x.pkg":            "x.pkg (mail)",
		"x.framework":      "x.framework (mail)",
		"x.plugin":         "x.plugin (mail)",
		"x.kext":           "x.kext (mail)",
		"me@mydomain.app.": "me@mydomain.app. (mail)",
		"Work.{645FF040-5081-101B-9F08-00AA002F954E}": "Work.{645FF040-5081-101B-9F08-00AA002F954E} (mail)",
		"work.{645ff040-5081-101b-9f08-00aa002f954e}": "work.{645ff040-5081-101b-9f08-00aa002f954e} (mail)",
	} {
		if got := setSafeName(n, ""); got != want {
			t.Errorf("%q became %q, want %q", n, got, want)
		}
	}
	long := strings.Repeat("q", 52) + "@ab.app" // 59 characters
	got := setSafeName(long, "")
	if !strings.HasSuffix(got, setPackageSuffix) || utf8.RuneCountInString(got) > setNameMax || setPackageEnding(got) || app.FolderNameWhy(got) != "" {
		t.Errorf("a long name ending .app: %q (%d characters)", got, utf8.RuneCountInString(got))
	}
	// Told apart as any other name: a second one gets its number after " (mail)".
	if got := setToldApart([]string{setSafeName("me@mydomain.app", ""), setSafeName("ME@mydomain.app", "")}, "darwin"); got[1] != "ME@mydomain.app (mail) (2)" {
		t.Errorf("told apart: %q", got)
	}
}

// covers: MA-1492, S90
// RV-04 (the re-verify of rounds A and B): OUT-02's list missed ".download", a real domain ending that
// macOS types as a package, so Finder showed a folder named "me@shop.download" as one item, not a
// folder. The builder checked other candidates the same way (`mdls -name kMDItemContentTypeTree` on an
// empty folder with the ending, on this Mac, 2026-11-25) and added each that listed com.apple.package.
// Positive twin first: the candidates mdls typed as a plain folder (public.folder) are kept as they
// are. Then each ending added gets " (mail)", in any letter case, and the name with it is no package
// ending.
func TestEveryEndingMacOSTypesAsAPackageGetsMail(t *testing.T) {
	for _, n := range []string{"me@x.photos", "me@x.music", "me@x.movie", "me@x.zip", "me@x.mov", "me@x.mbox",
		"me@x.component", "me@x.xcodeproj", "me@x.playground", "me@x.docset", "me@x.vst", "me@shop.downloads"} {
		if got := setSafeName(n, ""); got != n {
			t.Errorf("positive twin: %q, a plain folder, became %q", n, got)
		}
	}
	for _, e := range []string{".download", ".band", ".menu", ".help", ".key", ".pages", ".numbers", ".workflow",
		".action", ".scptd", ".wdgt", ".prefpane", ".saver", ".xpc", ".appex", ".mpkg", ".sparsebundle",
		".photoslibrary", ".logicx"} {
		for _, n := range []string{"me@shop" + e, strings.ToUpper("me@shop" + e)} {
			got := setSafeName(n, "")
			if got != n+setPackageSuffix || setPackageEnding(got) {
				t.Errorf("RV-04: %q became %q, want %q", n, got, n+setPackageSuffix)
			}
		}
	}
}
