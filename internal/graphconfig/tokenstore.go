package graphconfig

import (
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/util"
)

// accountTokenStore adapts an account-keyed SecretStore to graph.TokenStore, so a
// delegated sign-in token can live in the same vault as the client secret (Windows
// Credential Manager) rather than a plaintext file (design-mailarchive-desktop P4).
type accountTokenStore struct {
	store   SecretStore
	account string
}

func (a accountTokenStore) LoadToken() ([]byte, error) {
	s, err := a.store.Get(a.account)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

func (a accountTokenStore) StoreToken(data []byte) error { return a.store.Set(a.account, string(data)) }
func (a accountTokenStore) ClearToken() error            { return a.store.Delete(a.account) }

// TokenAccountKey is the vault account the delegated sign-in token is stored under,
// distinct from the client-secret account (AccountKey).
func TokenAccountKey(tenant, clientID string) string {
	return "graph-token:" + util.ShortHash(tenant) + "-" + util.ShortHash(clientID)
}

// NewTokenStore wraps a SecretStore as a graph.TokenStore bound to one registration.
func NewTokenStore(store SecretStore, tenant, clientID string) graph.TokenStore {
	return accountTokenStore{store: store, account: TokenAccountKey(tenant, clientID)}
}
