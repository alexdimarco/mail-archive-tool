package main

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/server"
)

// The handlers `serve` and `setup` serve, built in one place each so the
// structural test (MA-378) probes exactly what the binary serves. Every one is
// behind the local-surface guard (design-local-confidentiality §4.C1, P6): the
// Host check on every route and method, and the policy headers on every response.

// serveHandler is the archive reader as `serve` serves it, on the listener at.
// A serve bound to a non-loopback address also accepts its own IP literals and
// the -allow-host names (serveExtraHosts); a loopback serve, only the loopback
// names and, when bound to a loopback address other than 127.0.0.1 or ::1, that
// address's own literal (the URL it prints; see the open decision on design P6).
// Every request, whatever its route, needs the session s: the cookie a one-time
// link gives, under /s/<P>/ (P7). handoff answers `serve -link`'s POST /_handoff
// (design §4.C3).
func serveHandler(src server.Source, at net.Addr, allowHosts []string, s *localweb.Session, handoff http.Handler) http.Handler {
	surf := localweb.Surface{Name: "serve", Listener: at, ExtraHosts: serveExtraHosts(at, allowHosts), Session: s, Handoff: handoff}
	return localweb.Wrap(surf, server.Serve(src))
}

// setupHandler is the Microsoft 365 setup wizard as `setup` serves it, on the
// listener at, behind the session s.
func setupHandler(cfgPath string, store graphconfig.SecretStore, at net.Addr, s *localweb.Session) http.Handler {
	return localweb.Wrap(localweb.Surface{Name: "setup", Listener: at, Session: s}, server.SetupHandler(cfgPath, store))
}

// serveExtraHosts is what the guard accepts, with any port, beyond 127.0.0.1,
// localhost and [::1] for a serve listening at at (P6): nothing on a loopback
// bind (but its own IP literal when that is not 127.0.0.1 or ::1); on a non-loopback one the bound IP literal — or, bound to every
// interface (0.0.0.0, [::] or no host), the IP literals of this machine's
// interfaces as they are now, at start-up — plus the -allow-host names. IP
// literals are safe against DNS rebinding: a rebinding page's Host carries the
// attacker's name.
func serveExtraHosts(at net.Addr, allowHosts []string) []string {
	tcp, ok := at.(*net.TCPAddr)
	if !ok {
		return nil
	}
	if tcp.IP.IsLoopback() {
		// 127.0.0.1 and ::1 are already accepted. Any other loopback literal
		// (127.0.0.2) is what servedURL prints, so the surface accepts it; the
		// -allow-host names still widen nothing on a loopback bind.
		if ip := ipLiteral(tcp.IP); ip != "127.0.0.1" && ip != "[::1]" {
			return []string{ip}
		}
		return nil
	}
	var hosts []string
	if tcp.IP == nil || tcp.IP.IsUnspecified() {
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
				hosts = append(hosts, ipLiteral(ipn.IP))
			}
		}
	} else {
		hosts = append(hosts, ipLiteral(tcp.IP))
	}
	return append(hosts, allowHosts...)
}

// ipLiteral writes ip as a Host header names it (an IPv6 address bracketed).
func ipLiteral(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return "[" + ip.String() + "]"
}

// allowHostNames checks and normalizes the -allow-host values: each must be a
// host name or IP address with no port, scheme or path — the Host check accepts
// it with any port (R12: refused naming the flag and the value).
func allowHostNames(names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		h, ok := localweb.ExtraHost(n)
		if !ok {
			return nil, fmt.Errorf("-allow-host %q is not a host name or IP address: give it without a port, scheme or path (it is accepted with any port)", n)
		}
		out = append(out, h)
	}
	return out, nil
}

// servedURL is the address `serve`/`setup` print for the listener ln that addr
// was bound as: addr itself, with the real port when addr asked for any (":0"),
// and 127.0.0.1 in place of an every-interface host (0.0.0.0, [::] or none),
// whose own name the Host check does not accept.
func servedURL(addr string, ln net.Addr) string {
	host, port, err := net.SplitHostPort(addr)
	tcp, ok := ln.(*net.TCPAddr)
	if err != nil || !ok {
		return "http://" + addr + "/"
	}
	if port == "0" {
		port = strconv.Itoa(tcp.Port)
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}
