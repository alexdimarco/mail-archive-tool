package desktop

import "testing"

// The extraction workspace's slice E3 (the desktop pages and endpoints; design
// docs/design-extraction-workspace.md §9 and §10) lands catalog-first: each row
// starts here as a skip encoder and is replaced by its real test as the piece it
// covers is built. What stays here at the end is what a later slice or an owner
// decision closes, each with the row it cites.

// covers: MA-506, MA-403, R17, R9, R29, S52
// The workspace's Graph session, built from the desktop's settings, is the device
// (/me) client even when the saved config says auth=app, and a whole Microsoft 365
// preview and export issues only GET.
func TestWorkspaceSessionIsTheDeviceClientEvenWithAppAuth(t *testing.T) {
	t.Skip("MA-506: the workspace session (app.OpenGraphSession) does not exist yet; lands in EW-E3")
}

// covers: MA-404, R17, R19, S41
// The sign-in-only POST runs in its own slot under one process-wide sign-in mutex,
// serves the device code only from the guarded signin-status POST and persists the
// token after the XC7 confirmation.
func TestSignInRunsInItsOwnSlot(t *testing.T) {
	t.Skip("MA-404: the sign-in slot and its endpoints do not exist yet; lands in EW-E3")
}

// covers: MA-405, R19, R12, S41, S52
// Every workspace and export route is a guarded POST: GET is 405, a missing token, a
// foreign Origin or a non-loopback Host is 403 with the side effect absent, and a
// body over 1 MiB is a 413 worded as a refusal.
func TestWorkspaceRoutesAreGuardedPosts(t *testing.T) {
	t.Skip("MA-405: the workspace and export routes do not exist yet; lands in EW-E3")
}

// covers: MA-407, R19, S52
// The workspace's scripts hold no innerHTML or its kin, the pages no inline handler,
// and mail-derived text comes back as JSON-escaped strings.
func TestWorkspaceScriptsAndPagesAreInert(t *testing.T) {
	t.Skip("MA-407: the workspace pages and scripts do not exist yet; lands in EW-E3")
}

// covers: MA-408, R12, S41, S52
// Every dashboard page renders the sidebar and every entry resolves.
func TestEveryPageRendersTheSidebarAndEveryEntryResolves(t *testing.T) {
	t.Skip("MA-408: the workspace and Export activity pages and the Export sidebar group do not exist yet; lands in EW-E3")
}

// covers: MA-409, R29, S52
// A selection resolves to the folder set and the message set, and the count comes
// from the cached tree with no Graph request.
func TestSelectionResolutionAndCounts(t *testing.T) {
	t.Skip("MA-409: selection, row handles and counts do not exist yet; lands in EW-E3")
}

// covers: MA-410, R29, R12
// A handle the session never listed, an unseen folder id and more than 10,000 picks
// are refused; 10,000 handles fit in 1 MiB.
func TestSelectionHandlesAreAdmittedAndBounded(t *testing.T) {
	t.Skip("MA-410: the seen-set and the selection bounds do not exist yet; lands in EW-E3")
}

// covers: MA-411, R29, R12
// A selection or preview made under signer A is refused after a sign-in as B or a
// clear sign-in.
func TestSelectionIsBoundToTheSigner(t *testing.T) {
	t.Skip("MA-411: the selection's signer binding does not exist yet; lands in EW-E3")
}

// covers: MA-412, R29, S52
// The preview writes nothing to the destination, the archive or the config directory.
func TestPreviewWritesNothing(t *testing.T) {
	t.Skip("MA-412: the preview endpoint does not exist yet; lands in EW-E3")
}

// covers: MA-413, R29, S52
// The run copies exactly the previewed ids, and a stale or superseded preview id is R-k.
func TestRunCopiesExactlyThePreview(t *testing.T) {
	t.Skip("MA-413: Start export does not exist yet; lands in EW-E3")
}

// covers: MA-415, R29, R1, S52
// The preview reads a folder's count before and after listing it and re-lists on a
// change.
func TestPreviewReconcilesAFolderThatChanged(t *testing.T) {
	t.Skip("MA-415: the preview's resolution does not exist yet; lands in EW-E3")
}

// covers: MA-431, R29, R5, S25, S52
// A Microsoft 365 export completes while a capture holds the archive lock, and a
// second start is R-m.
func TestM365ExportRunsBesideALockedArchive(t *testing.T) {
	t.Skip("MA-431: the export slot does not exist yet; lands in EW-E3")
}

// covers: MA-511, R29, S52
// The list endpoint never returns more than 100 rows, progress advances with an ETA,
// and the session's retained heap stays bounded.
func TestListEndpointIsBoundedAndProgressAdvances(t *testing.T) {
	t.Skip("MA-511: the list and progress endpoints do not exist yet; lands in EW-E3")
}

// covers: MA-433, R29, R12, S52
// Browse calls the picker seam, one dialog at a time, and never when unguarded.
func TestBrowseCallsThePickerSeam(t *testing.T) {
	t.Skip("MA-433: the picker seam does not exist yet; lands in EW-E3")
}

// covers: MA-437, R29, S52
// Retry builds a new preview from the run record into the next part of the set.
func TestRetryBuildsFromTheRunRecord(t *testing.T) {
	t.Skip("MA-437: the retry endpoint does not exist yet; lands in EW-E3")
}

// covers: MA-438, R12, S52
// Every Microsoft 365 and destination refusal carries its what, why and remedy, and
// no token, Graph URL or crash text.
func TestRefusalsCarryWhatWhyAndRemedy(t *testing.T) {
	t.Skip("MA-438: the workspace's refusals do not exist yet; lands in EW-E3")
}

// covers: MA-444, R29, R4, R12
// The destination is validated again at start.
func TestDestinationIsCheckedAgainAtStart(t *testing.T) {
	t.Skip("MA-444: Start export does not exist yet; lands in EW-E3")
}

// covers: MA-445, R12
// The remembered destination and options round-trip in the settings file, with the
// per-slice defaults.
func TestExportSettingsRoundTrip(t *testing.T) {
	t.Skip("MA-445: the export settings fields do not exist yet; lands in EW-E3")
}

// covers: MA-446, R12, S41, S52
// The Export activity page shows the running export, the history and the actions.
func TestExportActivityPage(t *testing.T) {
	t.Skip("MA-446: the Export activity page does not exist yet; lands in EW-E3")
}

// covers: MA-448, R29, R12, S52
// Reconciliation runs off the request path, once per launch, and only downgrades.
func TestReconciliationIsOffTheRequestPath(t *testing.T) {
	t.Skip("MA-448: the background reconciler does not exist yet; lands in EW-E3")
}

// covers: MA-449, R17, R29
// A session built inside a handler whose context is then cancelled still refreshes.
func TestSessionOutlivesTheRequestThatBuiltIt(t *testing.T) {
	t.Skip("MA-449: the session on the process-lifetime context does not exist yet; lands in EW-E3")
}

// covers: MA-514, R29, R5
// Cancelling main's root context drains the run and finalizes it within 15 s.
func TestDrainCancelsAndFinalizesTheRun(t *testing.T) {
	t.Skip("MA-514: Drain does not exist yet; lands in EW-E3")
}

// covers: MA-518, R29, R12
// Start refuses with R-g2 when the estimate plus the reserve exceeds the free bytes.
func TestFreeSpacePolicyRefusesAtStart(t *testing.T) {
	t.Skip("MA-518: Start export does not exist yet; lands in EW-E3")
}

// covers: MA-522, R29, R12, S52
// Cursors, the seen-set and the idle release behave as the session lifecycle says.
func TestSessionLifecycle(t *testing.T) {
	t.Skip("MA-522: the session's cursors and seen-set do not exist yet; lands in EW-E3")
}

// covers: MA-524, R12, R5
// An export start racing a settings save never writes back a stale Out.
func TestSettingsWritesAreSerialized(t *testing.T) {
	t.Skip("MA-524: the settings mutex does not exist yet; lands in EW-E3")
}

// covers: MA-526, R12
// With the deployment switch off every workspace and export route is 404 and the
// sidebar has no Export group.
func TestDeploymentSwitchHidesTheWorkspace(t *testing.T) {
	t.Skip("MA-526: the deployment switch does not exist yet; lands in EW-E3")
}

// covers: MA-529, R12, S52
// Browse is refused while an export runs, and CI scans the desktop binary for Windows.
func TestBrowseIsDisabledDuringARunAndCIScansWindows(t *testing.T) {
	t.Skip("MA-529: the picker seam and the CI step do not exist yet; lands in EW-E3")
}

// covers: MA-530, R17, R29, S41, S52
// Clearing or changing the sign-in during a run stops it with R-z, finalized.
func TestSignInChangesStopTheRun(t *testing.T) {
	t.Skip("MA-530: the sign-in changes at the desktop surface do not exist yet; lands in EW-E3")
}
