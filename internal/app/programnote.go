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
	kinds := map[string]bool{} // which run steps the kept copies need
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
			self = append(self, fmt.Sprintf("  - for %s: this computer's own program, %s — it cannot be checked", systemName(f.Platform), v))
			switch {
			case strings.HasSuffix(f.Name, ".zip"):
				kinds["macapp"] = true
			case goos == "windows":
				kinds["windows"] = true
			default:
				kinds["bare"] = true
			}
		default:
			release = append(release, fmt.Sprintf("  - for %s: MailArchive %s", systemName(f.Platform), f.Version))
			switch f.Platform {
			case "macos":
				kinds["macapp"] = true
			case "windows-amd64":
				kinds["windowszip"] = true
			default:
				kinds["bare"] = true
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
	if kinds["macapp"] {
		b.WriteString("  - a Mac copy: unzip it and open the app (the Mac may say it is from the internet).\n")
	}
	if kinds["windowszip"] {
		b.WriteString("  - the Windows copy: unzip it and run mailarchive.exe (SmartScreen may warn).\n")
	}
	if kinds["windows"] {
		b.WriteString("  - this computer's own Windows program: run it (SmartScreen may warn).\n")
	}
	if kinds["bare"] {
		b.WriteString("  - a Linux or bare program: copy it to your home folder, then in a terminal run\n      chmod +x <the file>\n      ./<the file> serve -out <this folder>\n")
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
