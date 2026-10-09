package graph

import "testing"

// covers: MA-402, R17, R29
// MIMETo is the one bounded $value download both consumers share: it streams the
// body byte-exact into a writer, stops on idle, on the size cap and on a cancel
// with no partial output, and runs its own header timer of at least the idle
// window; StatusError is {Code, GraphCode, Text} with Error() unchanged.
func TestMIMEToContract(t *testing.T) {
	t.Skip("MA-402: MIMETo, its typed stops and the typed StatusError do not exist yet; lands in FI-SG")
}

// covers: MA-314, R10, R17
// The primitive download bounds, scaled: a trickle inside the idle window
// completes, a mid-body stall and the overall cap stop with typed errors while
// the caller's context stays live, and the size cap holds at its boundary.
func TestMIMEToBounds(t *testing.T) {
	t.Skip("MA-314: MIMETo's idle watchdog, overall cap and size cap do not exist yet; lands in FI-SG")
}
