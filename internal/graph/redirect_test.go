package graph

import "testing"

// covers: MA-527, R17, R29
func TestClientTimeoutBoundsTokenFetchNotValue(t *testing.T) {
	t.Skip("MA-527: Client.Timeout (F15) is not set on the JSON clients; lands in EW-E1")
}

// covers: MA-528, R17, R29
func TestRedirectsAndBodiesAreBounded(t *testing.T) {
	t.Skip("MA-528: CheckRedirect is not set on either client; lands in EW-E1")
}
