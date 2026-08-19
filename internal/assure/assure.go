// Package assure provides "assertions that cannot lie" for tests — the Go port
// of assurance-kit's python/assure/assure.py (reached()/refused()). It exists to
// kill the residue classes an audit finds: selectors that quietly match nothing
// (so downstream assertions never run), and bare "err != nil" refusal checks
// that accept a crash as if it were a clean, typed refusal.
//
// See ../assurance-kit/METHODOLOGY.md §3.1 and CLAUDE.md.
package assure

import (
	"reflect"
	"strings"
)

// TB is the minimal subset of *testing.T these helpers need. Taking an
// interface (rather than testing.TB, which cannot be implemented outside the
// testing package) lets the helpers' own failure paths be unit-tested.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
}

// Reached fails the test if v is nil, an empty collection/string, or a zero
// value — the "my selector found nothing and the assertions downstream silently
// never ran" class. It returns v unchanged for in-place chaining:
//
//	for _, f := range assure.Reached(t, files, "discovered files") { ... }
func Reached[T any](t TB, v T, what string) T {
	t.Helper()
	if isEmpty(reflect.ValueOf(v)) {
		t.Fatalf("NOT REACHED (%s is nil/empty/zero) — the test exercised nothing", what)
	}
	return v
}

func isEmpty(rv reflect.Value) bool {
	if !rv.IsValid() {
		return true
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Map, reflect.Array, reflect.String, reflect.Chan:
		return rv.Len() == 0
	case reflect.Ptr, reflect.Interface:
		return rv.IsNil() || isEmpty(rv.Elem())
	default:
		return rv.IsZero()
	}
}

// RefuseOption configures Refused.
type RefuseOption func(*refuseCfg)

type refuseCfg struct {
	code         int
	names        []string
	forbid       []string
	noSideEffect func() bool
}

// Code sets the exact typed exit/return code a clean refusal must carry
// (default 2 — see docs/ux-contract.md X1).
func Code(c int) RefuseOption { return func(o *refuseCfg) { o.code = c } }

// Names requires each needle to appear in the refusal message (the offending
// file/policy key/flag/remediation must be NAMED).
func Names(n ...string) RefuseOption { return func(o *refuseCfg) { o.names = append(o.names, n...) } }

// Forbid requires each needle to be ABSENT from the message (default: "panic",
// "goroutine" — the Go analog of a leaked Python Traceback).
func Forbid(f ...string) RefuseOption {
	return func(o *refuseCfg) { o.forbid = append(o.forbid, f...) }
}

// NoSideEffect requires fn to return true — proving the refused operation left
// no partial state behind (fail-closed, not fail-dirty).
func NoSideEffect(fn func() bool) RefuseOption { return func(o *refuseCfg) { o.noSideEffect = fn } }

// Refused asserts the whole refusal contract in one call: the exact typed code,
// that the message NAMES the given needles, that it leaks no crash text, and
// (optionally) that no side effect survived. Prove the positive twin on a
// healthy fixture first, so you know the refusal fired for the right reason.
func Refused(t TB, rc int, message string, opts ...RefuseOption) {
	t.Helper()
	cfg := refuseCfg{code: 2, forbid: []string{"panic", "goroutine"}}
	for _, op := range opts {
		op(&cfg)
	}

	if rc != cfg.code {
		t.Fatalf("expected typed refusal exit %d, got %d — a crash is not a refusal (message: %q)",
			cfg.code, rc, clip(message))
	}
	for _, n := range cfg.names {
		if !strings.Contains(message, n) {
			t.Fatalf("refusal must name %q (the offending file/policy/remediation); got %q", n, clip(message))
		}
	}
	for _, bad := range cfg.forbid {
		if bad != "" && strings.Contains(message, bad) {
			t.Fatalf("refusal leaked %q — a crash, not a clean refusal: %q", bad, clip(message))
		}
	}
	if cfg.noSideEffect != nil && !cfg.noSideEffect() {
		t.Fatalf("refusal mutated state — fail-dirty, not fail-closed")
	}
}

func clip(s string) string {
	const max = 300
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
