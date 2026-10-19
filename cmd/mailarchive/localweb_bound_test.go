package main

import (
	"net"
	"net/url"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/localweb/localwebtest"
)

// covers: MA-371, R23, S43
// A `serve` bound to a loopback address other than 127.0.0.1 (127.0.0.2) prints a
// URL its own Host check accepts: the bound IP literal is accepted with any port,
// like a non-loopback bind's own literal, while another loopback literal
// (127.0.0.3) and the -allow-host names stay refused. Positive twin: 127.0.0.1.
// (Adversarial pass AO5.)
func TestLoopbackServeAcceptsTheAddressItPrints(t *testing.T) {
	out, ix := readerFixture(t)
	at := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 8099}
	printed, err := url.Parse(servedURL("127.0.0.2:8099", at))
	if err != nil {
		t.Fatal(err)
	}
	h := serveHandler(out, ix, at, []string{"nas.example"}, localweb.NewSession(), nil)
	probed := localwebtest.Probe{
		Surface: "serve",
		Refused: []string{"127.0.0.3:8099", "nas.example:8099", "attacker.example:8099"},
		Passed:  []string{"127.0.0.1:8099", "localhost:8099", printed.Host, "127.0.0.2:9000"},
		Forbid:  []string{"Acme invoice", out},
	}.Run(t, h)
	assure.Reached(t, probed, "serve routes probed")
}

// covers: MA-371, R23, S43
// OPEN DECISION (re-verification of AO5; escalated, not decided here): design P6,
// S43 and MA-371 state the loopback set as exactly 127.0.0.1, localhost and [::1],
// and the IP literals "only for a non-loopback serve". AO5's fix accepts the bound
// literal on a loopback bind such as 127.0.0.2 so the printed URL works. This
// encodes the design as written: on 127.0.0.2 the Host 127.0.0.2 is refused. The
// owner either amends P6/S43/MA-371 to the fix (delete this test) or rules that
// such a bind is refused at start (then this and the AO5 tombstone change).
func TestLoopbackBindOtherThan127_0_0_1KeepsTheDesignsExactSet(t *testing.T) {
	t.Skip("open decision: design P6/S43/MA-371 say exactly 127.0.0.1, localhost and [::1] on a loopback serve; AO5's fix also accepts the bound literal (escalated)")
	out, ix := readerFixture(t)
	at := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 8099}
	h := serveHandler(out, ix, at, nil, localweb.NewSession(), nil)
	probed := localwebtest.Probe{
		Surface: "serve",
		Refused: []string{"127.0.0.2:8099", "127.0.0.3:8099", "attacker.example:8099"},
		Passed:  []string{"127.0.0.1:8099", "localhost:8099"},
		Forbid:  []string{"Acme invoice", out},
	}.Run(t, h)
	assure.Reached(t, probed, "serve routes probed")
}
