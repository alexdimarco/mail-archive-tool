package source

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// Open encoders from the MF-S1 adversarial pass (docs/review-mime-fidelity-s1-adversarial.md).
// Each is a confirmed gap whose fix moves the frozen replay's identity for some population
// of messages or hosts, which only an owner ruling under the design's drift policy (section 5,
// MC28) may do. The bodies are real: remove the t.Skip when the ruling lands and the test is
// the permanent guard. Run once with the skip removed, each is red for the stated reason.

// covers: MA-330, R2, R3, R27
// MS1-AO1. The frozen replay's identity must not depend on the host: the same bytes give
// the same Identity() and Fingerprint() whatever time.Local is. A Date whose zone is an
// alphabetic abbreviation (EST, PST, CET) is read by time.Parse through the host's zone
// database (vendored net/mail ParseDate, layout zone MST), and on a host that does not
// know the abbreviation it is read as UTC, so the instant, the fingerprint, the no-
// Message-ID sha: identity and the UTC-formatted stem move with the host. Pre-existing in
// v0.7.1 (the same stdlib parser); S1 freezes it. Any fix moves identity for one host
// population (mapping an abbreviation to one fixed offset, or to UTC) and needs an owner ruling.
func TestFrozenReplayIdentityDoesNotDependOnTheHostZone(t *testing.T) {
	t.Skip("MS1-AO1: identity of an alphabetic-zone Date depends on the host's time zone; fixing it moves identity for one host population, so it needs an owner ruling under MC28 (invariants R2, R3, MA-330)")
	raw := []byte("Message-ID: <est@x.example>\r\nDate: Mon, 02 Jan 2006 15:04:05 EST\r\nFrom: a@x.example\r\nTo: b@x.example\r\nSubject: Quarterly\r\n\r\nbody\r\n")
	under := func(zone string) (id, fp string) {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Skipf("no zone database for %s: %v", zone, err)
		}
		old := time.Local
		time.Local = loc
		defer func() { time.Local = old }()
		m := ParseRFC822V1(raw)
		return m.Identity(), m.Fingerprint()
	}
	idUTC, fpUTC := under("UTC")
	idNY, fpNY := under("America/New_York")
	if idUTC != idNY || fpUTC != fpNY {
		t.Errorf("one message, two identities by host zone: UTC %q/%s, America/New_York %q/%s", idUTC, fpUTC, idNY, fpNY)
	}
}

// covers: MA-331, R10, R27
// MS1-AO2 (header half). The frozen replay must not stall a run on one hostile message
// (R10: damaged input costs the smallest unit and never hangs the run): go-message's
// consumeComment builds the comment with `comment += p.s[:1]`, so a Message-ID header with
// an unclosed comment of n bytes costs O(n^2) time and memory; safeParseMessage runs the
// replay and the content parser, so S1 doubles the cost v0.7.1 already had. recover()
// cannot end a slow parse and the replay has no deadline. Any bound on the frozen copy (a
// semantic-preserving builder patch) is a drift decision under MC28 and needs an owner
// ruling (R27 and MA-331 are why it is not simply made). The nested-multipart half is
// TestFrozenReplayIsBoundedOnNestedMultipart; a fix that turns only this one green leaves
// that one skipped, so the ruling must cover both.
func TestFrozenReplayIsBoundedOnAHostileHeader(t *testing.T) {
	t.Skip("MS1-AO2: the frozen replay is quadratic on an unclosed Message-ID comment and on nested multipart; bounding it is a drift decision under MC28 and needs an owner ruling (invariants R10, R27, MA-331)")
	raw := []byte("Message-ID: <a@b> (" + strings.Repeat("x", 200_000) + "\r\nFrom: a@x.example\r\n\r\nbody\r\n")
	done := make(chan struct{})
	go func() {
		replayV1(raw)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the frozen replay of a 200 KB header took over a second (quadratic)")
	}
}

// nestedMultipart is a message whose body is multipart/mixed nested depth levels deep, each
// level one part of the one above, with a plain-text leaf.
func nestedMultipart(depth int) []byte {
	var b strings.Builder
	b.WriteString("From: a@x.example\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b0\r\n\r\n")
	for i := 0; i < depth; i++ {
		b.WriteString("--b" + strconv.Itoa(i) + "\r\nContent-Type: multipart/mixed; boundary=b" + strconv.Itoa(i+1) + "\r\n\r\n")
	}
	b.WriteString("--b" + strconv.Itoa(depth) + "\r\nContent-Type: text/plain\r\n\r\nleaf\r\n--b" + strconv.Itoa(depth) + "--\r\n")
	for i := depth - 1; i >= 0; i-- {
		b.WriteString("--b" + strconv.Itoa(i) + "--\r\n")
	}
	return []byte(b.String())
}

// covers: MA-331, R10, R27
// MS1-AC1 (and MS1-AO2's multipart half). The same stall through the body: nested multipart
// costs O(depth^2) in the vendored reader and textproto copies (replay of depth 500, 1000
// and 2000 measured 19, 57 and 209 ms, about 3.6x per doubling; depth 4000, 281 KB, 1.5 s),
// with no depth cap and no deadline, in v0.7.1 as well. A depth cap on the frozen copy moves
// the identity of a message that crosses it, so it is a drift decision under MC28 and needs
// an owner ruling; this encoder keeps the multipart shape from being forgotten when a ruling
// covers only the Message-ID comment.
func TestFrozenReplayIsBoundedOnNestedMultipart(t *testing.T) {
	// The positive twin runs always: the shape is a real nested message the replay reads
	// in full when it is shallow, so the skipped case below is red for depth alone.
	if m, panicked := replayV1(nestedMultipart(20)); panicked || m == nil || !strings.Contains(m.PlainBody, "leaf") {
		t.Fatalf("the shallow nested fixture: panicked %t, message %+v, want its leaf text read", panicked, m)
	}
	t.Skip("MS1-AO2/AC1: the frozen replay is quadratic on nested multipart; a depth cap on the frozen copy moves identity for a message that crosses it and needs an owner ruling under MC28 (invariants R10, R27, MA-331)")
	raw := nestedMultipart(6000)
	done := make(chan struct{})
	go func() {
		replayV1(raw)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("the frozen replay of a %d KB message nested 6000 multiparts deep took over a second (quadratic)", len(raw)/1024)
	}
}
