// Package alert sends a backup-result email directly over SMTP — MailArchive's
// built-in failure alert (archive-manager / St Augustine's request). It is pure Go
// (net/smtp + crypto/tls), so it starts no outside program and the MA-612 closed-set
// rule is untouched: MailArchive composes and transmits the message itself.
//
// The SMTP password is NOT held here; the caller reads it from the OS secret store
// and passes it to Send, so it never lands in the settings file or a log.
package alert

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Provider presets.
const (
	ProviderOffice365 = "office365"
	ProviderGmail     = "gmail"
	ProviderSMTP      = "smtp"
)

// Security modes.
const (
	SecurityStartTLS = "starttls" // upgrade a plain :587 connection to TLS (Office 365, Gmail, most relays)
	SecurityTLS      = "tls"      // implicit TLS from the first byte (:465)
	SecurityNone     = "none"     // no encryption — only sensible for a trusted internal relay
)

// Result values an Event carries.
const (
	ResultOK     = "ok"
	ResultFailed = "failed"
	ResultPaused = "paused" // a backup auto-paused after a long source-loss streak
	// ResultRecovered is the first successful backup after a failing streak: one email says
	// backups are working again (design-desktop-settings-card 3.7, R3C-10). It is sent under
	// the failure option, since it closes a streak that option told the owner about.
	ResultRecovered = "recovered"
	// ResultSyncStale is the Thunderbird-not-synced email (design-presync-attended §5.5):
	// a plaintext archive's scheduled backups ran for 3 days without syncing Thunderbird
	// first. Sent once per streak, under the failure option.
	ResultSyncStale = "sync-stale"
)

// SyncStale is what the Thunderbird-not-synced email says beyond the archive and the
// computer: when the streak began, how many backups in a row, and the phrase and remedy,
// both taken from the closed maps by the caller (C-37), never from a record's own text.
type SyncStale struct {
	Since  time.Time
	Missed int
	Phrase string
	Remedy string
}

// Config is the built-in email-alert configuration (stored in desktop settings; the
// password lives in the OS secret store, not here).
type Config struct {
	Enabled   bool
	OnFailure bool
	OnSuccess bool
	OnPause   bool
	Provider  string // office365 | gmail | smtp
	Host      string
	Port      int
	Security  string // starttls | tls | none
	From      string
	To        string // one or more recipients, comma- or space-separated (the ticket address)
	Username  string // SMTP auth user; "" means no authentication (an open internal relay)
}

// Event is one completed run's outcome.
type Event struct {
	Result   string // ResultOK | ResultFailed | ResultPaused
	Archive  string
	Error    string // the failure reason, for a failed run
	Messages int
	Host     string // the machine the backup ran on
	When     time.Time
	Sync     *SyncStale // ResultSyncStale only
}

// Preset fills the host, port and security for a known provider; the SMTP provider
// (and an unknown one) keeps whatever the caller set. Returned so a UI can prefill.
func Preset(provider string) (host string, port int, security string) {
	switch provider {
	case ProviderOffice365:
		return "smtp.office365.com", 587, SecurityStartTLS
	case ProviderGmail:
		return "smtp.gmail.com", 587, SecurityStartTLS
	}
	return "", 587, SecurityStartTLS
}

// ShouldSend reports whether a run with this result triggers an alert under cfg.
func (c Config) ShouldSend(result string) bool {
	if !c.Enabled {
		return false
	}
	switch result {
	case ResultFailed, ResultRecovered, ResultSyncStale:
		return c.OnFailure
	case ResultOK:
		return c.OnSuccess
	case ResultPaused:
		return c.OnPause
	}
	return false
}

// Recipients splits To on commas/semicolons/whitespace into individual addresses.
func (c Config) Recipients() []string {
	fields := strings.FieldsFunc(c.To, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Validate reports the first reason the config cannot send, or nil. It is what a
// "send test email" button and a save check against, so the message names the field.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("the mail server (SMTP host) is empty")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("the mail server port %d is not valid", c.Port)
	}
	if strings.TrimSpace(c.From) == "" {
		return fmt.Errorf("the From address is empty")
	}
	if len(c.Recipients()) == 0 {
		return fmt.Errorf("there is no To address to send the alert to")
	}
	return nil
}

// subjectAndBody builds the alert's subject and plain-text body. Kept pure so a test
// pins the wording and a crafted archive path / error cannot change the message shape.
func (c Config) subjectAndBody(ev Event) (subject, body string) {
	name := header(ev.Archive) // an archive path never contains CR/LF; strip any so neither the subject header nor the body line is malformed
	if ev.Result == ResultSyncStale {
		return syncStaleSubjectAndBody(name, ev)
	}
	switch ev.Result {
	case ResultFailed:
		subject = "MailArchive backup FAILED: " + name
	case ResultPaused:
		subject = "MailArchive backup PAUSED after repeated failures: " + name
	case ResultRecovered:
		subject = "MailArchive backups are working again: " + name
	default:
		subject = "MailArchive backup ok: " + name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "MailArchive backup result: %s\r\n", strings.ToUpper(resultWord(ev.Result)))
	fmt.Fprintf(&b, "Archive: %s\r\n", name)
	if ev.Host != "" {
		fmt.Fprintf(&b, "Computer: %s\r\n", ev.Host)
	}
	if !ev.When.IsZero() {
		fmt.Fprintf(&b, "When: %s\r\n", ev.When.Format("2006-01-02 15:04:05 MST"))
	}
	if ev.Result == ResultOK && ev.Messages > 0 {
		fmt.Fprintf(&b, "Messages archived this run: %d\r\n", ev.Messages)
	}
	if ev.Error != "" {
		fmt.Fprintf(&b, "\r\nReason:\r\n%s\r\n", ev.Error)
	}
	b.WriteString("\r\nThis message was sent by MailArchive's built-in backup alerts.\r\n")
	return subject, b.String()
}

// syncStaleSubjectAndBody is the Thunderbird-not-synced email (design-presync-attended
// §5.5, C-29; the design's draft words, for the friction review). The phrase and remedy
// come from the caller's closed maps; every field is stripped of line breaks all the same.
func syncStaleSubjectAndBody(name string, ev Event) (subject, body string) {
	var s SyncStale
	if ev.Sync != nil {
		s = *ev.Sync
	}
	host := header(ev.Host)
	if strings.TrimSpace(host) == "" {
		host = "this computer"
	}
	since := "recently"
	if !s.Since.IsZero() {
		since = s.Since.Local().Format("Jan 2, 2006")
	}
	subject = "MailArchive: Thunderbird not synced for 3 days — " + name
	var b strings.Builder
	fmt.Fprintf(&b, "The scheduled backups of %s on %s still ran, but Thunderbird has not been synced before them since %s (%d backups in a row). ", name, host, since, s.Missed)
	b.WriteString("Mail that had only reached the mail server, not this computer, may be missing from the archive.\r\n\r\n")
	fmt.Fprintf(&b, "Most recently: %s.\r\n\r\n", header(s.Phrase))
	// The reader may be on a phone, away from the computer (friction review, §14 gate 3):
	// say where the remedy is done, and where the buttons it names are.
	fmt.Fprintf(&b, "Do this on that computer: %s\r\n", header(s.Remedy))
	b.WriteString("The buttons and cards named above are in MailArchive Desktop.\r\n\r\n")
	b.WriteString("You will get this email once, until Thunderbird syncs again.\r\n")
	b.WriteString("\r\nThis message was sent by MailArchive's built-in backup alerts.\r\n")
	return subject, b.String()
}

func resultWord(r string) string {
	switch r {
	case ResultFailed:
		return "failed"
	case ResultPaused:
		return "paused"
	case ResultRecovered:
		return "working again"
	default:
		return "ok"
	}
}

// Message is the full RFC 5322 message (headers + body) for the event. Header values
// are sanitised so a newline in a field cannot inject extra headers (CRLF injection).
func (c Config) Message(ev Event) []byte {
	subject, body := c.subjectAndBody(ev)
	var m strings.Builder
	fmt.Fprintf(&m, "From: %s\r\n", header(c.From))
	fmt.Fprintf(&m, "To: %s\r\n", header(strings.Join(c.Recipients(), ", ")))
	fmt.Fprintf(&m, "Subject: %s\r\n", header(subject))
	m.WriteString("MIME-Version: 1.0\r\n")
	m.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	m.WriteString("\r\n")
	m.WriteString(body)
	return []byte(m.String())
}

// header strips CR and LF from a header value so observed data (an archive path, an
// error message) placed in a header cannot add headers of its own.
func header(v string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
}

// transport sends a prepared message to addr. The default dials real SMTP; a test
// replaces it to capture the call without a server.
var transport = smtpTransport

// Send composes the alert for ev and transmits it with the SMTP password (read by the
// caller from the OS secret store). It validates first, so a misconfigured alert fails
// with a clear reason instead of a silent drop.
func Send(c Config, password string, ev Event) error {
	if err := c.Validate(); err != nil {
		return err
	}
	var auth smtp.Auth
	if strings.TrimSpace(c.Username) != "" {
		auth = smtp.PlainAuth("", c.Username, password, c.Host)
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	return transport(addr, c.Host, c.Security, auth, c.From, c.Recipients(), c.Message(ev))
}

// SMTP time bounds (MA-700): a stalling or malicious mail server must never hang the
// caller — the scheduled backup (fireAlert runs AFTER the backup is written to disk) or
// the dashboard's test handler. net/smtp and tls.Dial impose NO timeout of their own,
// so these are enforced here. They are vars so a test can shrink them: the dial bounds
// the connect/handshake, the deadline bounds the whole conversation after connecting.
var (
	smtpDialTimeout = 20 * time.Second
	smtpSendTimeout = 60 * time.Second
)

// smtpTransport delivers msg over a time-bounded SMTP conversation. For implicit TLS
// the link is encrypted from the first byte (:465); otherwise it upgrades to STARTTLS
// when the server offers it. Either way net/smtp refuses PlainAuth on a cleartext link,
// so the password never goes out unencrypted. Every step runs under a connection
// deadline, so a server that connects then stalls cannot hang us indefinitely.
func smtpTransport(addr, host, security string, auth smtp.Auth, from string, to []string, msg []byte) error {
	var conn net.Conn
	var err error
	if security == SecurityTLS {
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: smtpDialTimeout}, "tcp", addr, &tls.Config{ServerName: host})
	} else {
		conn, err = net.DialTimeout("tcp", addr, smtpDialTimeout)
	}
	if err != nil {
		return fmt.Errorf("could not connect to the mail server %s: %w", addr, err)
	}
	defer conn.Close()
	// Bound the whole SMTP conversation (reading the 220 greeting and every later reply),
	// so a tarpit, a dead relay or a blackholing firewall cannot hang the caller.
	_ = conn.SetDeadline(time.Now().Add(smtpSendTimeout))

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if security != SecurityTLS {
		// Upgrade to STARTTLS when the server offers it (Office 365, Gmail, most relays on
		// :587). When it does not, net/smtp's PlainAuth refuses to send over cleartext, so
		// an authenticated send fails rather than leaking the password.
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return err
			}
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
