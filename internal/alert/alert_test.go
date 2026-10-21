package alert

import (
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

// covers: MA-700
// A stalling mail server (accepts the connection, then never speaks) must not hang the
// caller: the SMTP conversation runs under a connection deadline, so it returns an
// error instead of blocking the scheduled backup or the test handler forever.
func TestMA700_SMTPTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		time.Sleep(3 * time.Second) // hold the connection open, silent (no 220 greeting)
		c.Close()
	}()

	oldDial, oldSend := smtpDialTimeout, smtpSendTimeout
	smtpDialTimeout, smtpSendTimeout = 250*time.Millisecond, 250*time.Millisecond
	defer func() { smtpDialTimeout, smtpSendTimeout = oldDial, oldSend }()

	done := make(chan error, 1)
	go func() {
		done <- smtpTransport(ln.Addr().String(), "127.0.0.1", SecurityStartTLS, nil, "f@x.org", []string{"t@x.org"}, []byte("test"))
	}()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("a stalling mail server should produce a timeout error, not success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("smtpTransport hung past its deadline — the SMTP timeout is not enforced")
	}
}

// covers: MA-697
// The built-in email alert engine: provider presets, the event gate (enabled + which
// results), recipient parsing, validation, a CRLF-injection-proof message, and that
// Send routes to the transport with the right address/recipients/auth — and that an
// invalid config fails loudly instead of silently reaching the transport.
func TestMA697_AlertEngine(t *testing.T) {
	if h, p, s := Preset(ProviderOffice365); h != "smtp.office365.com" || p != 587 || s != SecurityStartTLS {
		t.Fatalf("office365 preset wrong: %s %d %s", h, p, s)
	}
	if h, _, _ := Preset(ProviderGmail); h != "smtp.gmail.com" {
		t.Fatalf("gmail host %s", h)
	}

	// The event gate.
	c := Config{Enabled: true, OnFailure: true}
	if !c.ShouldSend(ResultFailed) {
		t.Fatal("a failure must send when OnFailure is on")
	}
	if c.ShouldSend(ResultOK) {
		t.Fatal("a success must not send when OnSuccess is off")
	}
	if (Config{Enabled: false, OnFailure: true}).ShouldSend(ResultFailed) {
		t.Fatal("a disabled alert must not send")
	}

	if got := (Config{To: "a@x.org, b@x.org; c@x.org"}).Recipients(); len(got) != 3 {
		t.Fatalf("recipients split wrong: %v", got)
	}

	// Validation names the missing field and passes a healthy config.
	if (Config{Host: "", Port: 587, From: "f@x", To: "t@x"}).Validate() == nil {
		t.Fatal("an empty host must be refused")
	}
	if (Config{Host: "h", Port: 587, From: "f@x", To: ""}).Validate() == nil {
		t.Fatal("an empty To must be refused")
	}
	good := Config{Host: "smtp.office365.com", Port: 587, Security: SecurityStartTLS, From: "backup@sa.org", To: "tickets@sa.org", Username: "backup@sa.org"}
	if err := good.Validate(); err != nil {
		t.Fatalf("a healthy config must validate: %v", err)
	}

	// The message: result-specific subject, the To header, and no header injection from
	// a crafted archive path.
	msg := string(good.Message(Event{Result: ResultFailed, Archive: "/vol/StAugustine", Error: "the backup drive was not mounted", When: time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)}))
	if !strings.Contains(msg, "Subject: MailArchive backup FAILED: /vol/StAugustine") {
		t.Fatalf("failure subject missing:\n%s", msg)
	}
	if !strings.Contains(msg, "tickets@sa.org") {
		t.Fatal("the To header is missing")
	}
	inj := (Config{Host: "h", Port: 587, From: "f@x", To: "t@x"}).Message(Event{Result: ResultFailed, Archive: "a\r\nBcc: evil@x.org"})
	headers, _, _ := strings.Cut(string(inj), "\r\n\r\n")
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Fatalf("CRLF injection: a crafted archive path added a header line %q", line)
		}
	}

	// Send routes through the transport with the right address, host, recipients and
	// auth (username set -> auth present).
	var gotAddr, gotHost string
	var gotTo []string
	var gotAuth bool
	old := transport
	defer func() { transport = old }()
	transport = func(addr, host, security string, auth smtp.Auth, from string, to []string, m []byte) error {
		gotAddr, gotHost, gotTo, gotAuth = addr, host, to, auth != nil
		return nil
	}
	if err := Send(good, "app-password", Event{Result: ResultFailed, Archive: "/vol/StAugustine"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotAddr != "smtp.office365.com:587" || gotHost != "smtp.office365.com" || len(gotTo) != 1 || !gotAuth {
		t.Fatalf("send routed wrong: addr=%s host=%s to=%v auth=%v", gotAddr, gotHost, gotTo, gotAuth)
	}

	// A no-username config sends with no auth (an open internal relay).
	gotAuth = true
	if err := Send(Config{Host: "relay.sa.local", Port: 25, Security: SecurityNone, From: "backup@sa.org", To: "tickets@sa.org"}, "", Event{Result: ResultFailed}); err != nil {
		t.Fatalf("no-auth send: %v", err)
	}
	if gotAuth {
		t.Fatal("a config with no username must send without auth")
	}

	// An invalid config fails loudly and never reaches the transport.
	called := false
	transport = func(string, string, string, smtp.Auth, string, []string, []byte) error { called = true; return nil }
	if Send(Config{Enabled: true, OnFailure: true, Host: ""}, "", Event{Result: ResultFailed}) == nil {
		t.Fatal("an invalid config must fail")
	}
	if called {
		t.Fatal("an invalid config must not reach the transport")
	}
}
