//go:build race

package app

// raceHeapAllowance is added to a heap bound under the race detector, whose
// instrumentation makes every allocation larger.
const raceHeapAllowance = 16 << 20
