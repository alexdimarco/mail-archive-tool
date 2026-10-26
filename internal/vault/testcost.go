package vault

import "testing"

// testFloor is the cheap Argon2id cost tests run at: the cost of the frozen
// known answer in TestKeyEncryptionKeyIsTheFrozenArgon2idOutput.
var testFloor = Params{MemoryKiB: 64, Time: 2, Threads: 1}

// inTestBinary reports whether this program is a go test binary. A var only so
// this package's tests can prove UseTestCost refuses outside one.
var inTestBinary = testing.Testing

// UseTestCost lowers the Argon2id floor to a cheap cost for the rest of this
// test binary, so a package whose tests make many keys stays inside go test's
// time limit (each key at the shipped floor costs 256 MiB and about a second).
// Call it once from TestMain, before any test runs. It panics outside a test
// binary, so the shipped program can never lower the floor. TestShippedKDFFloor
// pins the shipped floor and TestFullCostVaultRoundTrip exercises it end to end.
func UseTestCost() {
	if !inTestBinary() {
		panic("vault: UseTestCost called outside a test binary")
	}
	floor = testFloor
}
