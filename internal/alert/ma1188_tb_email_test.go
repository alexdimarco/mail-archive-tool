package alert

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// loopbackSMTP is a minimal SMTP server on 127.0.0.1 for one message: it answers every command
// and keeps the DATA it was given. No mail leaves the computer.
func loopbackSMTP(t *testing.T) (addr string, got <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ch := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		r, w := bufio.NewReader(c), bufio.NewWriter(c)
		say := func(s string) { w.WriteString(s + "\r\n"); w.Flush() }
		say("220 loopback ESMTP")
		var data strings.Builder
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
					data.WriteString(l)
				}
				ch <- data.String()
				say("250 kept")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	return ln.Addr().String(), ch
}

// covers: MA-1188, MA-1184
// The still-downloading and stopped emails (slice C5), sent through the real SMTP transport to a
// loopback server: the alert settings decide whether they go (on, with the failure option, as the
// Thunderbird-not-synced email); the subject and body are the words below, every line stripped of
// line breaks so a crafted folder name cannot add a header. Positive twin first: the
// still-downloading email.
func TestTheThunderbirdDownloadEmails(t *testing.T) {
	on := Config{Enabled: true, OnFailure: true}
	if !on.ShouldSend(ResultTBBehind) {
		t.Fatal("positive twin: alerts on with the failure option do not send it")
	}
	for _, off := range []Config{{Enabled: false, OnFailure: true}, {Enabled: true, OnFailure: false, OnSuccess: true, OnPause: true}} {
		if off.ShouldSend(ResultTBBehind) {
			t.Errorf("%+v sends it", off)
		}
	}

	addr, got := loopbackSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Enabled: true, OnFailure: true, Host: host, Port: p, Security: SecurityNone, From: "backup@example.org", To: "owner@example.org"}
	when := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)
	still := Event{Result: ResultTBBehind, Archive: "/Volumes/Backup/Mail", Host: "studio", When: when, TB: &TBBehind{
		Lines:  []string{"Thunderbird has been downloading the Thunderbird account kept in the folder imap.gmail.com for 3 days and has about 120,000 of 238,080 messages so far."},
		Remedy: "leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.",
	}}
	if err := Send(c, "", still); err != nil {
		t.Fatalf("positive twin: send: %v", err)
	}
	msg := <-got
	_, body, _ := strings.Cut(msg, "\r\n\r\n")
	if !strings.Contains(msg, "Subject: MailArchive: Thunderbird is still downloading mail after 3 days — /Volumes/Backup/Mail\r\n") {
		t.Errorf("the still-downloading subject:\n%s", msg)
	}
	wantStill := "The scheduled backups of /Volumes/Backup/Mail on studio are working, but Thunderbird on that computer is still downloading the mail of the account below, so the backups have only what it has so far.\r\n\r\n" +
		"Thunderbird has been downloading the Thunderbird account kept in the folder imap.gmail.com for 3 days and has about 120,000 of 238,080 messages so far.\r\n\r\n" +
		"Do this on that computer: leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.\r\n\r\n" +
		"You will get this email once while this mail is still downloading, and one more only if Thunderbird then stops.\r\n\r\n" +
		"This message was sent by MailArchive's built-in backup alerts.\r\n"
	if body != wantStill {
		t.Errorf("the still-downloading body:\n%q\nwant\n%q", body, wantStill)
	}

	stopped := Event{Result: ResultTBBehind, Archive: "/Volumes/Backup/Mail", When: when, TB: &TBBehind{Stopped: true,
		Lines:  []string{"Thunderbird has not downloaded any more of the Thunderbird account kept in the folder imap.work.example since Nov 4 (about 1,670 of 56,611 messages are on that computer).", "Thunderbird has been downloading the Thunderbird account kept in the folder imap.gmail.com for 3 days and has about 120,000 of 238,080 messages so far."},
		Remedy: "open Thunderbird and leave it open with the computer idle for a while.",
	}}
	subject, body := c.subjectAndBody(stopped)
	if subject != "MailArchive: Thunderbird has stopped downloading mail — /Volumes/Backup/Mail" {
		t.Errorf("the stopped subject: %q", subject)
	}
	if !strings.HasPrefix(body, "The scheduled backups of /Volumes/Backup/Mail on this computer are working, but Thunderbird on that computer has stopped downloading mail of the accounts below, so the backups cannot get the rest.\r\n\r\n") ||
		!strings.Contains(body, "\r\n\r\nYou will get this email once for each download that stops, not after every backup.\r\n") {
		t.Errorf("the stopped body:\n%s", body)
	}

	// No header from a crafted line; no account line may break the body's shape.
	evil := Event{Result: ResultTBBehind, Archive: "/a", TB: &TBBehind{Lines: []string{"x\r\nBcc: evil@example.org"}, Remedy: "y\r\nBcc: evil@example.org"}}
	headers, rest, _ := strings.Cut(string(c.Message(evil)), "\r\n\r\n")
	for _, line := range strings.Split(headers+"\r\n"+rest, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Errorf("a crafted line made its own line %q", line)
		}
	}
}
