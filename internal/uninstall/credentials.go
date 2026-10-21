package uninstall

import (
	"fmt"

	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graphconfig"
)

// ClearCredentials clears BOTH credentials of the Graph registration (tenant, clientID):
// the device sign-in TOKEN and the client SECRET — a separate credential in a separate
// store that the first design draft missed (U-C4). The caller reads tenant+clientID from
// graph-config.json FIRST and passes them here, so the derived credential keys survive,
// and deletes graph-config.json only AFTER this returns.
//
// Fail-closed: a real (non-"already absent") error from either clear is returned, not
// swallowed, so uninstall never reports success over a surviving credential. An
// already-absent credential is not an error (both stores map not-found to nil). It does
// NOT delete graph-config.json.
func ClearCredentials(tenant, clientID string, tokenStore graph.TokenStore, tokenCachePath string, secretStore graphconfig.SecretStore) error {
	if err := graph.ClearSignIn(graph.Config{TokenStore: tokenStore, TokenCachePath: tokenCachePath}); err != nil {
		return fmt.Errorf("the saved Microsoft sign-in could not be cleared (it may still be present): %w", err)
	}
	if secretStore != nil {
		if err := secretStore.Delete(graphconfig.AccountKey(tenant, clientID)); err != nil {
			return fmt.Errorf("the stored app secret could not be cleared (it may still be present): %w", err)
		}
	}
	return nil
}
