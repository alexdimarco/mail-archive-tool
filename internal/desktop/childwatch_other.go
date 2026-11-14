//go:build !linux

package desktop

// newChildStarted never fires here: only Linux hands the choice an activation token
// (activationToken), so choose never waits on it (the S1 fix round 2, B10).
func newChildStarted(stop <-chan struct{}) <-chan struct{} { return make(chan struct{}) }
