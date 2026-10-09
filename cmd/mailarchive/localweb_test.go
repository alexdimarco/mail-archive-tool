package main

import "testing"

// covers: MA-371, R19, R23, S43
// `serve` and `setup` check the Host header first on every route and method: a
// rebinding page's name, an empty Host, a trailing-dot or userinfo Host is refused
// with no archive data, while every loopback name passes on any port.
func TestServeAndSetupRefuseForeignHost(t *testing.T) {
	t.Skip("MA-371: no local web surface checks the Host header yet (localweb.Wrap); lands in LC-C1")
}

// covers: MA-378, R23, R19
// Every route `serve` and `setup` register is served only through the guard: the
// production constructors return a localweb.Wrap handler, and a hostile Host is
// refused on every registered pattern, probed by name.
func TestServeAndSetupServeOnlyThroughTheGuard(t *testing.T) {
	t.Skip("MA-378: there is no localweb.Mux route registry or localweb.Wrap guard yet; lands in LC-C1")
}
