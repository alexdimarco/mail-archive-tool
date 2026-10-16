package source

import (
	"strings"
	"testing"
)

// covers: MA-348, MA-349, R1, R4
// F9 of the MF-S2 friction review (cell IN-2). The name of a part kept as received (.undecoded) or
// cut short (.partial) is made by this version, and a bidirectional control in the sender's name
// would reverse the very suffix that tells a reader what the file is: "evil<RLO>gnp.exe.undecoded"
// displays as "evildedocednu.exe.png"-style text, an .exe that looks like an image with the warning
// reversed away. The controls are stripped from these names, whose suffix is the tool's. (The names of
// ordinary attachments keep the archive writer's open F75 gap, ruling Q15: not changed here.)
func TestAKeptPartsNameCarriesNoBidiControlThatReversesItsSuffix(t *testing.T) {
	useFidelity(t)
	rlo := "%E2%80%AE" // U+202E RIGHT-TO-LEFT OVERRIDE, as RFC 2231 percent-encoded UTF-8
	part := func(name, cte, body string) string {
		return "--bd\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename*=utf-8''" + name +
			"\r\nContent-Transfer-Encoding: " + cte + "\r\n\r\n" + body + "\r\n"
	}
	data := hostile(`multipart/mixed; boundary="bd"`, stopsV1+
		part("evil"+rlo+"gnp.exe", "x-uuencode", "begin 644 a\r\n#86%B\r\n`\r\nend")+
		part("cut"+rlo+"gnp.exe", "base64", "QUJDREVGR0hJSktMTU5P\r\nT1BRUlN!UVVW")+
		"--bd--\r\n")
	m := safeParseMessage(data)
	requireNoStub(t, "a bidi-named part", m)
	var suffixes []string
	for _, a := range m.Attachments {
		if a.GapKind == "" {
			continue
		}
		for _, r := range a.Filename {
			if (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
				t.Errorf("the name %q of a kept part carries the bidi control %U", a.Filename, r)
			}
		}
		for _, s := range []string{".undecoded", ".partial"} {
			if strings.HasSuffix(a.Filename, s) {
				suffixes = append(suffixes, s)
			}
		}
	}
	if len(suffixes) != 2 {
		t.Fatalf("want one .undecoded and one .partial part, got the suffixes %v (attachments %v, notes %v)", suffixes, len(m.Attachments), noteCodes(m))
	}
	// The positive twin: the part is still named after the sender's file.
	found := 0
	for _, a := range m.Attachments {
		if strings.Contains(a.Filename, "gnp.exe") {
			found++
		}
	}
	if found != 2 {
		t.Errorf("the kept parts lost the sender's names: %d of 2 keep gnp.exe", found)
	}
}
