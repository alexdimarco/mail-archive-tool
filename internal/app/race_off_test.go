//go:build !race

package app

// raceHeapAllowance is zero without the race detector.
const raceHeapAllowance = 0
