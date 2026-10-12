package source

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// covers: MA-349, R10
// MS2-AO6. Multiparts with no boundary are joined into the body in linear time, not by
// copying the accumulated body once per part, and the joined text is what it always was.
func TestBoundarylessMultipartsAreJoinedInLinearTime(t *testing.T) {
	useFidelity(t)
	child := func(text string) string { return "--bd\r\nContent-Type: multipart/mixed\r\n\r\n" + text + "\r\n" }

	// The semantics, on a few: text parts and boundary-less containers in order, pieces
	// separated by a blank line; the first plain part keeps the body field to itself.
	small := hostile(`multipart/mixed; boundary="bd"`,
		child("alpha")+child("beta")+"--bd\r\nContent-Type: text/plain\r\n\r\nplain\r\n"+child("gamma")+"--bd--\r\n")
	if got := parseFidelity(small).PlainBody; got != "alpha\n\nbeta\n\ngamma" {
		t.Errorf("the joined body is %q, want %q (a plain part after an unsplit one keeps the first-of-each rule)", got, "alpha\n\nbeta\n\ngamma")
	}

	const n, size = 9000, 3200
	var b strings.Builder
	piece := strings.Repeat("x", size-5)
	for i := 0; i < n; i++ {
		b.WriteString(child(fmt.Sprintf("%05d", i) + piece))
	}
	b.WriteString("--bd--\r\n")
	data := hostile(`multipart/mixed; boundary="bd"`, b.String())
	start := time.Now()
	m := parseFidelity(data)
	took := time.Since(start)
	if want := n*(size) + (n-1)*2; len(m.PlainBody) != want {
		t.Errorf("the joined body is %d bytes, want %d", len(m.PlainBody), want)
	}
	if took > 1500*time.Millisecond {
		t.Errorf("%d boundary-less parts of %d bytes took %v to join; want linear time (copying the body once per part took seconds)", n, size, took)
	}
}
