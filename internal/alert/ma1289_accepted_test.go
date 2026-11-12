package alert

import (
	"bufio"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// scriptedSMTP is a loopback SMTP server on 127.0.0.1 (no mail leaves the computer) that answers
// the end of DATA with afterData and then, on QUIT, says goodbye (221) or drops the connection
// with no reply.
func scriptedSMTP(t *testing.T, afterData string, goodbye bool) Config {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				r, w := bufio.NewReader(c), bufio.NewWriter(c)
				say := func(s string) { w.WriteString(s + "\r\n"); w.Flush() }
				say("220 loopback ESMTP")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						say("250 loopback")
					case cmd == "DATA":
						say("354 go ahead")
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
						}
						say(afterData)
					case cmd == "QUIT":
						if goodbye {
							say("221 bye")
						}
						return
					default:
						say("250 ok")
					}
				}
			}(c)
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	return Config{Enabled: true, OnFailure: true, Host: host, Port: p, Security: SecurityNone, From: "backup@example.org", To: "owner@example.org"}
}

// covers: MA-1289
// SPAM-01 (the C5 adversarial pass, the lead's call): once the mail server has accepted the message
// (its reply after the end of DATA), it was sent. A failure after that, a goodbye (QUIT) that gets
// no reply and a connection that closes, is reported as ErrAfterAccepted, which the shared sender
// (internal/backupalert) logs and counts as sent; a refusal of the message itself is still a send
// failure, never ErrAfterAccepted. Positive twin first: a server that accepts and says goodbye.
func TestAMessageTheServerAcceptedWasSent(t *testing.T) {
	ev := Event{Result: ResultTBBehind, Archive: "/m", TB: &TBBehind{Accounts: 1, Lines: []string{"a"}, Remedy: []string{"b"}}}
	if err := Send(scriptedSMTP(t, "250 kept", true), "", ev); err != nil {
		t.Fatalf("positive twin: accepted with a goodbye: %v", err)
	}
	err := Send(scriptedSMTP(t, "250 kept", false), "", ev)
	if !errors.Is(err, ErrAfterAccepted) {
		t.Errorf("accepted, then the goodbye was lost: %v, want ErrAfterAccepted", err)
	}
	refused := Send(scriptedSMTP(t, "554 refused", true), "", ev)
	if refused == nil || errors.Is(refused, ErrAfterAccepted) {
		t.Errorf("a message the server refused: %v, want a send failure", refused)
	}
}
