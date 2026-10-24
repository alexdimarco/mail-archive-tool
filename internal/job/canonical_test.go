package job

import (
	"reflect"
	"testing"
)

// covers: MA-585
// FromCanonicalArgs reconstructs a Job from a run's recorded canonical export flags
// and FAILS CLOSED on anything outside the exact subset — the untrusted record can
// carry -since/-outlook/-manifest/-graph or stray tokens, and the Desktop must not
// reconstruct a partial or surprising job from them.
func TestFromCanonicalArgs(t *testing.T) {
	// A full happy case round-trips exactly the modelled fields.
	j, ok := FromCanonicalArgs([]string{"-out", "/a", "-mode", "incremental", "-auto",
		"-input", "/in/x", "-input", "/in/y", "-copy-first", "-follow-links", "/in/x", "-raw"})
	if !ok {
		t.Fatal("full canonical args should reconstruct")
	}
	want := Job{Out: "/a", Mode: "incremental", Auto: true,
		Inputs: []string{"/in/x", "/in/y"}, CopyFirst: true, FollowLinks: []string{"/in/x"}, KeepRaw: true}
	if !reflect.DeepEqual(j, want) {
		t.Errorf("reconstructed %+v, want %+v", j, want)
	}
	if j.ThunderbirdSync != nil {
		t.Error("the mapper must not set ThunderbirdSync; the caller adds the toggle")
	}

	// An -input job (no -auto) reconstructs too.
	if _, ok := FromCanonicalArgs([]string{"-out", "/a", "-mode", "full", "-input", "/in"}); !ok {
		t.Error("an -input job should reconstruct")
	}

	refuse := map[string][]string{
		"missing -out":             {"-mode", "incremental", "-auto"},
		"missing -mode":            {"-out", "/a", "-auto"},
		"no inputs and no -auto":   {"-out", "/a", "-mode", "incremental"},
		"unknown -mode":            {"-out", "/a", "-mode", "sideways", "-auto"},
		"unknown flag -since":      {"-out", "/a", "-mode", "incremental", "-auto", "-since", "7d"},
		"unknown flag -outlook":    {"-out", "/a", "-mode", "incremental", "-outlook"},
		"unknown flag -manifest":   {"-out", "/a", "-mode", "incremental", "-auto", "-manifest", "/m"},
		"bare value token":         {"-out", "/a", "-mode", "incremental", "-auto", "stray"},
		"-out missing its value":   {"-out"},
		"-input missing its value": {"-out", "/a", "-mode", "incremental", "-input"},
		"empty -out":               {"-out", "", "-mode", "incremental", "-auto"},
		"control char in -input":   {"-out", "/a", "-mode", "incremental", "-input", "/in\x00x"},
		"nil":                      nil,
	}
	for name, args := range refuse {
		if _, ok := FromCanonicalArgs(args); ok {
			t.Errorf("%s: should FAIL CLOSED, but reconstructed", name)
		}
	}
}
