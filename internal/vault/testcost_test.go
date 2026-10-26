package vault

import (
	"fmt"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-776, MA-705, R37
func TestTheCheapTestCostIsRefusedOutsideATestBinary(t *testing.T) {
	orig := floor
	t.Cleanup(func() { floor = orig })

	// Positive twin: inside a test binary it lowers the floor to the cheap cost.
	UseTestCost()
	assure.Reached(t, floor == testFloor && testFloor != orig, "the cheap test cost in effect")
	floor = orig

	// Outside a test binary it panics and leaves the shipped floor in place.
	inTestBinary = func() bool { return false }
	t.Cleanup(func() { inTestBinary = testing.Testing })
	msg := func() (msg string) {
		defer func() { msg = fmt.Sprint(recover()) }()
		UseTestCost()
		return ""
	}()
	code := 0
	if msg != "" && msg != "<nil>" {
		code = 1
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("UseTestCost", "outside a test binary"),
		assure.NoSideEffect(func() bool { return floor == orig }))
}
