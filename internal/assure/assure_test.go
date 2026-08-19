package assure

import (
	"fmt"
	"strings"
	"testing"
)

// fakeTB captures a Fatalf without aborting, so the helpers' failure paths are
// themselves testable (a real *testing.T would Goexit).
type fakeTB struct {
	failed bool
	msg    string
}

func (f *fakeTB) Helper() {}
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = true
	f.msg = fmt.Sprintf(format, args...)
}

func TestReachedPassesOnNonEmpty(t *testing.T) {
	got := Reached(t, []int{1, 2}, "slice")
	if len(got) != 2 {
		t.Fatalf("Reached should return its value unchanged, got %v", got)
	}
	Reached(t, "x", "string")
	Reached(t, 7, "int")
}

func TestReachedFailsOnEmptyOrZero(t *testing.T) {
	cases := map[string]func(tb TB){
		"empty slice":  func(tb TB) { Reached(tb, []int{}, "s") },
		"nil slice":    func(tb TB) { Reached(tb, []int(nil), "s") },
		"empty map":    func(tb TB) { Reached(tb, map[string]int{}, "m") },
		"empty string": func(tb TB) { Reached(tb, "", "str") },
		"zero int":     func(tb TB) { Reached(tb, 0, "n") },
		"nil pointer":  func(tb TB) { Reached(tb, (*int)(nil), "p") },
	}
	for name, fn := range cases {
		f := &fakeTB{}
		fn(f)
		if !f.failed {
			t.Errorf("%s: expected Reached to fail", name)
		} else if !strings.Contains(f.msg, "NOT REACHED") {
			t.Errorf("%s: message = %q, want NOT REACHED", name, f.msg)
		}
	}
}

func TestRefusedHappyPath(t *testing.T) {
	Refused(t, 2, "error: -out is required (remediation: pass -out DIR)", Names("-out", "required"))
}

func TestRefusedFailurePaths(t *testing.T) {
	cases := map[string]func(tb TB){
		"wrong code":   func(tb TB) { Refused(tb, 1, "typed refusal") },
		"missing name": func(tb TB) { Refused(tb, 2, "something failed", Names("-out")) },
		"leaked panic": func(tb TB) { Refused(tb, 2, "panic: nil deref") },
		"side effect":  func(tb TB) { Refused(tb, 2, "refused", NoSideEffect(func() bool { return false })) },
	}
	for name, fn := range cases {
		f := &fakeTB{}
		fn(f)
		if !f.failed {
			t.Errorf("%s: expected Refused to fail", name)
		}
	}
}

func TestRefusedCustomCode(t *testing.T) {
	Refused(t, 3, "capability denied", Code(3), Names("denied"))
	f := &fakeTB{}
	Refused(f, 2, "denied", Code(3))
	if !f.failed {
		t.Error("expected failure when rc != declared Code")
	}
}
