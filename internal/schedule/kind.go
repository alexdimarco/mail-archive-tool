package schedule

// Kind is a scheduled job's kind, named from its leading arguments against one
// fixed set (§4.6.5a). It is the single vocabulary the mechanism decides, guards
// (refuseCollision) and repairs by, and every surface that names a kind to the
// owner — the Weekly backup card, the wizard, and status's `scheduled:` line —
// renders it in these words. The kind is a string compared with a fixed set; no
// process is ever started to determine it.
type Kind string

const (
	KindVerify  Kind = "verify"
	KindRunJob  Kind = "run-job"
	KindCapture Kind = "capture"
	KindExport  Kind = "export"
	// KindNone is an absent descriptor; KindUnknown a present but unreadable one.
	// Both are decided by the descriptor's reader, not by KindOf.
	KindNone    Kind = "none"
	KindUnknown Kind = "unknown"
)

// KindOf names a job's kind from its leading arguments, never treating them as a
// path (§4.6.5a): verify; run-job (run-job, or the legacy -job); capture
// (desktop -capture, or the legacy -capture); and export for any other non-empty
// job. An empty job falls to export (the catch-all); it does not occur for a
// descriptor this program wrote.
func KindOf(job []string) Kind {
	switch {
	case len(job) > 0 && job[0] == "verify":
		return KindVerify
	case len(job) > 0 && (job[0] == "run-job" || job[0] == "-job"):
		return KindRunJob
	case len(job) >= 2 && job[0] == "desktop" && job[1] == "-capture":
		return KindCapture
	case len(job) > 0 && job[0] == "-capture":
		return KindCapture
	default:
		return KindExport
	}
}
