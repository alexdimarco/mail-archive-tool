package main

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mail-archive-tool/internal/instance"
)

// probeFormer says what answers at MailArchive's former address (design §4.C4
// step 2, CC22, CC23, ruling OQ-9): nothing — "" —, an older MailArchive Desktop
// (instance.FormerPreUpgrade: it answers / with its dashboard and has no
// /_handoff route), or any other listener (instance.FormerProgram), which is
// unverified and is reported, never silently. A listener that accepts and then
// stays silent or resets is a program too. It only reads: nothing it finds is
// trusted, opened or followed.
func probeFormer(addr string, timeout time.Duration) string {
	c := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get("http://" + addr + "/")
	if err != nil {
		if refusedConnection(err) {
			return ""
		}
		return instance.FormerProgram
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !looksLikeOldDashboard(string(body)) {
		return instance.FormerProgram
	}
	// An instance of this version answers /_handoff (400, 403 or 405 to a request
	// it cannot verify); only the older one has no such route and says 404.
	r, err := c.Post("http://"+addr+"/_handoff", "application/json", strings.NewReader("{}"))
	if err != nil {
		return instance.FormerProgram
	}
	io.Copy(io.Discard, io.LimitReader(r.Body, 4<<10))
	r.Body.Close()
	if r.StatusCode == http.StatusNotFound {
		return instance.FormerPreUpgrade
	}
	return instance.FormerProgram
}

// looksLikeOldDashboard is what an older MailArchive Desktop's root page shows.
func looksLikeOldDashboard(body string) bool {
	return strings.Contains(body, "<title>MailArchive Desktop</title>") && strings.Contains(body, `name="csrf"`)
}

// refusedConnection reports a connection that nothing was listening for.
func refusedConnection(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "actively refused") || strings.Contains(msg, "no connection could be made")
}

// parseProcNetTCP finds the socket inode of the listener on port in the text of
// /proc/net/tcp or /proc/net/tcp6, listening on loopback or every interface; ""
// when there is none. Linux lists each socket as "sl local_address rem_address st
// … uid timeout inode", addresses and ports in hex, state 0A for LISTEN.
func parseProcNetTCP(data string, port int) string {
	for i, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 10 || f[3] != "0A" {
			continue
		}
		ip, p, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		if n, err := strconv.ParseInt(p, 16, 32); err != nil || int(n) != port {
			continue
		}
		switch ip {
		case "0100007F", "00000000", "00000000000000000000000001000000", "00000000000000000000000000000000":
			return f[9]
		}
	}
	return ""
}
