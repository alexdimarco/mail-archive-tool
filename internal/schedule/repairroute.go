package schedule

// Route is the per-kind repair route §4.6.3/§4.6.5(e) decides for an archive's
// recorded schedule: which surface's Repair rebuilds it, or that none is offered.
// One function returns it so every Repair button and every schedule remedy
// (exe_missing, exe_moved, exe_former_name, not_installed, moved_archive) is
// rendered from one source — no surface names a button that is not on its page or
// a command that would refuse.
type Route int

const (
	RouteNone    Route = iota // no single repair; the two explicit installs
	RouteWizard               // run-job: Repair in the wizard (or the card, where shown)
	RouteCard                 // capture: Repair on the Weekly backup card
	RouteCommand              // export: the scheduleCommand line
)

// RepairRoute returns the route for repairing archive out's recorded schedule
// (§4.6.5(e)): a corroborated run-job (and capture not) is the wizard's (or the
// card's where shown); a corroborated capture (and run-job not) is the card's; an
// export descriptor is the command line; anything else — both kinds corroborated,
// or an uncorroborated run-job/capture — offers no single repair. The kind comes
// from owner-side evidence, never a forged Job; the export fallback reads the
// descriptor's own kind only to name a route.
func RepairRoute(d Descriptor, out string, ev KindEvidence) Route {
	runjob, capture := ev.Has(KindRunJob), ev.Has(KindCapture)
	switch {
	case runjob && !capture:
		return RouteWizard
	case capture && !runjob:
		return RouteCard
	case !runjob && !capture && KindOf(d.Job) == KindExport:
		return RouteCommand
	default:
		return RouteNone
	}
}
