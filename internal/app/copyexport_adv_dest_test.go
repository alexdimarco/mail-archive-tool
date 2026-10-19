package app

import (
	"os"
	"strings"
	"testing"
)

// covers: MA-414, MA-444, R29
// EW-AI5 tombstone. servedHere recognised only "localhost", an IP literal Go parses, and
// the computer's bare host name, so this computer's own address in the other spellings a
// share path takes (127.1, a decimal or hex address, 0, 0.0.0.0, an IPv6-literal UNC
// name, the host's fully qualified or dotted name) made a share on this very computer
// look like another one: decideViews then said "outside" for an archive and a
// destination that are one filesystem, and no identity was compared (R-f). Every such
// spelling now says "here", and a share on another computer still does not.
func TestCopyExportServedHereKnowsTheOtherSpellings(t *testing.T) {
	e := newCEEnv(t)
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("no host name here")
	}
	host = strings.ToLower(host)
	short, _, _ := strings.Cut(host, ".")
	here := []string{"localhost", "LOCALHOST", "localhost.localdomain", "127.0.0.1", "[::1]", "::1", host, short,
		"127.1", "127.0.1", "2130706433", "0x7f.1", "0x7f000001", "0177.0.0.1", "0", "0.0.0.0", "::",
		"--1.ipv6-literal.net", "--ffff-7f00-1.ipv6-literal.net", short + ".corp.example.com", short + ".", short + ".local"}
	for _, s := range here {
		if !e.servedHere(s) {
			t.Errorf("servedHere(%q) = false: a share by that name is on this computer", s)
		}
	}
	elsewhere := []string{"", "fileserver", "fileserver.example.com", "192.0.2.77", "8.8.8.8", "2001-db8--1.ipv6-literal.net",
		"x" + short + ".example.com", short + "x", "10.255.255.254", "300.1.1.1", "1.2.3.4.5"}
	for _, s := range elsewhere {
		if e.servedHere(s) {
			t.Errorf("servedHere(%q) = true: that is not this computer", s)
		}
	}

	// The decision the finder saw: a local archive and a share served from 127.1.
	local := ceView{Kind: viewLocal}
	for _, srv := range []string{"127.1", "0.0.0.0", "--1.ipv6-literal.net"} {
		if d := e.decideViews(local, ceView{Kind: viewShare, Server: srv}); d.unconfirm != "here" {
			t.Errorf("a share on %q beside a local archive decided %+v, want the R-f2 \"here\" answer", srv, d)
		}
	}
	if d := e.decideViews(local, ceView{Kind: viewShare, Server: "fileserver.example.com"}); d != (viewDecision{}) {
		t.Errorf("a share on another computer decided %+v, want outside", d)
	}
}
