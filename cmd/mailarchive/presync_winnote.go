package main

// winScheduledStartNote is the sentence the Windows install and preview notes add to
// presync.SyncNotice (presyncInstallNote in presync_windows.go): the scheduled start is
// minimized (MA-923; lab L-A10 saw no window on screen; a window that shows anyway is
// minimized by the backstop). Untagged so every platform's tests read it. "Minimized",
// never "minimized to the taskbar" (the Windows texts pass, 2026-10-31, finding 2: a
// Thunderbird set to hide to the tray is not on the taskbar, lab case E).
const winScheduledStartNote = "On Windows, each scheduled run starts Thunderbird minimized while it syncs; if its window appears anyway, it is minimized."
