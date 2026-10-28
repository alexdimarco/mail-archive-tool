package app

import (
	"fmt"
	"sort"
	"strings"

	"mail-archive-tool/internal/container"
)

// The program copies in the note (program design §6, C8, R5, R16; PG3): rendered from
// the landed set — each copy by system and version, which can be checked and against
// which checksum file and release page, this computer's own copy marked "cannot be
// checked" — with how to use one and the offline warning. It names no file but the
// checksum files that cover the copies (R16).

// EncryptedNoteFor is the note beside an encrypted archive whose sealed program set is
// ps (nil or with keep unset: no program section).
func EncryptedNoteFor(ps *container.ProgramSet) string {
	n := EncryptedNote()
	if s := programSection(ps); s != "" {
		n += "\n" + s
	}
	return n
}

// systemName names a release platform or a self-copy's <os>-<arch> in words.
// ProgramCopiesLabel is the choice to keep copies of MailArchive, as the Desktop's box
// and -encrypt's question put it: what this build will keep, never a release's promise
// from a development build (P3-8 friction review).
func ProgramCopiesLabel(release bool) string {
	if release {
		return "Keep a copy of MailArchive for Mac, Windows and Linux in the archive so it can be opened years from now (about 134 MB each time a new version updates it)"
	}
	return "Keep a copy of this program in the archive so it can be opened years from now. This is a development build of MailArchive: it keeps only a copy of itself, for this kind of computer — not the copies for Mac, Windows and Linux a release keeps"
}

// SystemName is a platform's name as the person reads it ("Mac", "Linux (ARM)"), for a
// front end that tells of a kept copy.
func SystemName(platform string) string { return systemName(platform) }

func systemName(platform string) string {
	switch platform {
	case "macos":
		return "Mac"
	case "windows-amd64":
		return "Windows (x86-64)"
	case "linux-amd64":
		return "Linux (x86-64)"
	case "linux-arm64":
		return "Linux (ARM)"
	}
	goos, goarch, _ := strings.Cut(platform, "-")
	name := map[string]string{"darwin": "Mac", "windows": "Windows", "linux": "Linux"}[goos]
	if name == "" {
		name = goos
	}
	return name + " (" + goarch + ")"
}

func programSection(ps *container.ProgramSet) string {
	if ps == nil || ps.Keep == nil || !*ps.Keep {
		return ""
	}
	var release, self []string
	sums := map[string]bool{}
	// Which run steps the kept copies need, each with the file names it is for, so the
	// steps name the very file to type (P3-8 friction, B22).
	kinds := map[string][]string{}
	for _, f := range ps.Files {
		if f.MissingRuns > 0 {
			continue // not on the drive now: never named as kept (NOTE-03)
		}
		goos, _, _ := strings.Cut(f.Platform, "-")
		switch {
		case f.Platform == container.PlatformChecksums:
			sums[f.Version] = true
		case f.Origin == container.OriginSelf:
			v := "a development build"
			if f.Version != "" {
				v = f.Version
			}
			self = append(self, fmt.Sprintf("  - for %s: this computer's own program, %s — it cannot be checked: no\n    release page or checksum file vouches for it, so nobody can tell whether it was\n    changed since it was put here", systemName(f.Platform), v))
			switch {
			case strings.HasSuffix(f.Name, ".zip"):
				kinds["macapp"] = append(kinds["macapp"], f.Name)
			case goos == "windows":
				kinds["windows"] = append(kinds["windows"], f.Name)
			case goos == "darwin":
				kinds["macbare"] = append(kinds["macbare"], f.Name)
			default:
				kinds["bare"] = append(kinds["bare"], f.Name)
			}
		default:
			release = append(release, fmt.Sprintf("  - for %s: MailArchive %s", systemName(f.Platform), f.Version))
			switch f.Platform {
			case "macos":
				kinds["macapp"] = append(kinds["macapp"], f.Name)
			case "windows-amd64":
				kinds["windowszip"] = append(kinds["windowszip"], f.Name)
			default:
				kinds["bare"] = append(kinds["bare"], f.Name)
			}
		}
	}
	sort.Strings(release)
	sort.Strings(self)
	var b strings.Builder
	b.WriteString("Copies of MailArchive are kept with this archive, in its \"program\" folder, so it\ncan be opened years from now:\n")
	if len(release)+len(self) == 0 {
		b.WriteString("  none yet — the next backup that can download them adds them.\n")
	}
	for _, l := range append(release, self...) {
		b.WriteString(l + "\n")
	}
	if len(sums) > 0 {
		var vs []string
		for v := range sums {
			vs = append(vs, v)
		}
		sort.Strings(vs)
		fmt.Fprintf(&b, "Each release copy's checksum is in its version's checksum file in that folder\n(SHA256SUMS-%s) and on the release page:\n  %s\nThe file in the folder could have been changed with the copy; the release page\ncannot.\n", strings.Join(vs, ", SHA256SUMS-"), releasesPage)
	}
	b.WriteString("Copy a program out of the archive's folder before you open or unzip it. Then:\n")
	if len(kinds["macapp"]) > 0 {
		b.WriteString("  - a Mac copy (a .zip): unzip it and open the app inside. If the Mac says the app\n" +
			"    cannot be opened, or that it is from the internet: Control-click the app, choose\n" +
			"    Open, then Open again (or System Settings > Privacy & Security > Open Anyway).\n")
	}
	if len(kinds["windowszip"]) > 0 {
		b.WriteString("  - the Windows copy (a .zip): unzip it, then double-click mailarchivew.exe to open\n" +
			"    MailArchive Desktop (mailarchive.exe beside it is the same program, for a\n" +
			"    terminal). If Windows SmartScreen warns, choose More info, then Run anyway.\n")
	}
	for _, name := range kinds["windows"] {
		fmt.Fprintf(&b, "  - this computer's own Windows program runs at a terminal. Copy %s to your\n"+
			"    Documents folder, open Command Prompt, and type these two lines, pressing Enter\n"+
			"    after each (after -out, type this folder's path in quotes):\n"+
			"      cd %%USERPROFILE%%\\Documents\n"+
			"      %s serve -out \"<this folder>\"\n"+
			"    If Windows SmartScreen warns, choose More info, then Run anyway.\n", name, name)
	}
	for _, name := range kinds["macbare"] {
		fmt.Fprintf(&b, "  - this computer's own Mac program runs at a terminal. Copy %s to your\n"+
			"    home folder, open Terminal (in Applications > Utilities), and type these lines,\n"+
			"    pressing Return after each (the third may say \"No such xattr\": that is fine;\n"+
			"    for the last, type the line up to -out and a space, then drag this folder onto\n"+
			"    the Terminal window, and press Return):\n"+
			"      cd ~\n"+
			"      chmod +x %s\n"+
			"      xattr -d com.apple.quarantine %s\n"+
			"      ./%s serve -out <this folder>\n", name, name, name, name)
	}
	if bare := kinds["bare"]; len(bare) > 0 {
		sort.Strings(bare)
		name, others := bare[0], ""
		if len(bare) > 1 {
			others = fmt.Sprintf(" (on a computer\n    with another processor, use %s instead in each line)", strings.Join(bare[1:], " or "))
		}
		fmt.Fprintf(&b, "  - a Linux copy runs at a terminal. Copy %s to your home folder, open a\n"+
			"    terminal, and type these lines, pressing Enter after each%s; after -out,\n"+
			"    type this folder's path in quotes:\n"+
			"      cd ~\n"+
			"      chmod +x %s\n"+
			"      ./%s serve -out \"<this folder>\"\n", name, others, name, name)
	}
	if len(kinds["windows"])+len(kinds["macbare"])+len(kinds["bare"]) > 0 {
		b.WriteString("    It asks for the archive's password or recovery key, then prints a link: open\n" +
			"    the link in a web browser to read the mail. Press Ctrl-C at the terminal to stop.\n")
	}
	b.WriteString(programOffline)
	return b.String()
}

// programOffline is the note's warning about checking a copy offline (§6, C8).
const programOffline = `Without the release page a copy cannot be checked: anyone who had this drive could
have swapped it, and changing the archive's password afterwards does not undo what
a swapped program took, because the archive's key itself never changes. Prefer the
official download whenever you can reach it.
`

// programSectionUnread is the note's program section when a newer MailArchive keeps
// the copies in a form this one cannot read (NOTE-06).
const programSectionUnread = `Copies of MailArchive may be kept with this archive, in its "program" folder, by
a newer MailArchive than the one that wrote this note.
` + programOffline
