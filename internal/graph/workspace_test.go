package graph

import "testing"

// covers: MA-400, R17, R29, S52
func TestFolderTreeContract(t *testing.T) {
	t.Skip("MA-400: FolderTree and FolderCount are not built; lands in EW-E1")
}

// covers: MA-401, R17, R29, S52, S20
func TestMessagePageContract(t *testing.T) {
	t.Skip("MA-401: MessagePage is not built; lands in EW-E1")
}

// covers: MA-401, R17, R29, S52, S20
func TestWorkspaceNextLinkPin(t *testing.T) {
	t.Skip("MA-401: the host/path pin on the workspace loops is not switched on; lands in EW-E1")
}

// covers: MA-403, R17, R9, R29, S52
func TestWorkspaceClientIsDeviceMeOnly(t *testing.T) {
	t.Skip("MA-403: the workspace read paths are not built; lands in EW-E1")
}
