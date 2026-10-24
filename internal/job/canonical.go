package job

import "mail-archive-tool/internal/util"

// FromCanonicalArgs reconstructs a Job from the canonical export flags a run records
// in state.LastRun.Job (app.canonicalJob is the forward direction). It is the ONE
// mapper the Desktop, the wizard and the CLI schedule path use to turn a recorded
// run into a schedulable job, and it is deliberately STRICT: it accepts ONLY the
// exact canonical subset — -out, -mode (incremental|full), -auto, repeated -input,
// -copy-first, repeated -follow-links, -raw — and FAILS CLOSED (ok=false) on a
// missing/empty -out, a flag missing its value, an unknown -mode, a value carrying a
// control character, or ANY other token. So a record carrying -since, -outlook,
// -manifest, -log, -graph or -verify (anything outside the subset) is refused rather
// than reconstructed into a partial or surprising job, and the caller falls back
// (e.g. the wizard pointer).
//
// The record is UNTRUSTED input — it lives in the archive directory, writable by
// anyone who can write there — so nothing from it is trusted beyond this whitelist.
// It sets no ThunderbirdSync (the caller adds the pre-sync toggle), and the caller
// pins -out to the archive it is actually scheduling rather than the record's value.
func FromCanonicalArgs(args []string) (Job, bool) {
	var j Job
	var sawOut, sawMode bool
	// value returns the token after position i as a flag value, refusing an absent,
	// empty or control-bearing value (the record is untrusted).
	value := func(i int) (string, bool) {
		if i+1 >= len(args) {
			return "", false
		}
		v := args[i+1]
		if v == "" || util.StripControl(v) != v {
			return "", false
		}
		return v, true
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-out":
			v, ok := value(i)
			if !ok {
				return Job{}, false
			}
			j.Out, sawOut = v, true
			i++
		case "-mode":
			if i+1 >= len(args) || (args[i+1] != "incremental" && args[i+1] != "full") {
				return Job{}, false
			}
			j.Mode, sawMode = args[i+1], true
			i++
		case "-auto":
			j.Auto = true
		case "-input":
			v, ok := value(i)
			if !ok {
				return Job{}, false
			}
			j.Inputs = append(j.Inputs, v)
			i++
		case "-copy-first":
			j.CopyFirst = true
		case "-follow-links":
			v, ok := value(i)
			if !ok {
				return Job{}, false
			}
			j.FollowLinks = append(j.FollowLinks, v)
			i++
		case "-raw":
			j.KeepRaw = true
		default:
			// Any token outside the canonical subset (a flag we do not model, or a
			// bare value) means the record is not a plain export we can reconstruct.
			return Job{}, false
		}
	}
	// canonicalJob always writes -out and -mode and needs at least one input or -auto.
	if !sawOut || !sawMode || (len(j.Inputs) == 0 && !j.Auto) {
		return Job{}, false
	}
	return j, true
}
