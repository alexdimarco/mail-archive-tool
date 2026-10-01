// Package graphconfig holds the Microsoft 365 Graph capture configuration the
// setup wizard writes and the `graph` command reads: the non-secret values
// (tenant, application id, auth mode) in a 0600 JSON file, and the client secret
// in a separate SecretStore (Windows Credential Manager, or a 0600 file). The
// wizard surface lives in internal/server; this package is the storage seam both
// it and the capture command share (design-graph-setup-wizard).
package graphconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/util"
)

// ConfigVersion is the on-disk schema version (additive-only, like the manifest).
const ConfigVersion = 1

// Config is the non-secret Graph capture configuration. The client secret is
// NEVER stored here — it lives in the SecretStore — so this file is safe at 0600
// without being a credential.
type Config struct {
	Version    int    `json:"version"`
	Tenant     string `json:"tenant,omitempty"`
	ClientID   string `json:"clientId,omitempty"`
	Auth       string `json:"auth,omitempty"` // "device" | "app"
	TokenCache string `json:"tokenCache,omitempty"`
	Mailbox    string `json:"mailbox,omitempty"`
}

// DefaultConfigPath is where the wizard writes and `graph` reads the config by
// default: under the OS config dir (never inside an archive -out, which may be a
// synced folder).
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine the OS config directory: %w", err)
	}
	return filepath.Join(dir, "mailarchive", "graph-config.json"), nil
}

// Load reads the config at path. A missing file is reported as a typed not-found
// (errors.Is(err, os.ErrNotExist)) so a caller treats "no config yet" as "use the
// flags", never a crash.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no Graph config at %s: %w", path, os.ErrNotExist)
		}
		return nil, fmt.Errorf("graph config %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("graph config %s is not valid JSON: %w", path, err)
	}
	return &c, nil
}

// Save writes the config to path atomically at 0600 (temp + fsync + rename), so a
// crash never leaves a torn file. The secret is not part of Config, so this file
// is never a credential.
func Save(path string, c *Config) error {
	c.Version = ConfigVersion
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600(path, data)
}

// AccountKey derives a stable, collision-resistant key for one app registration,
// so two registrations on one host do not share a secret slot. It carries no
// sensitive value (tenant and client id are public identifiers).
func AccountKey(tenant, clientID string) string {
	return "graph:" + util.ShortHash(tenant) + "-" + util.ShortHash(clientID)
}

// SecretStore persists one client secret per account key. Implementations: the
// Windows Credential Manager (secretstore_windows.go) and a 0600 file
// (secretstore_file.go). Get reports a typed not-found for an unset account.
type SecretStore interface {
	Set(account, secret string) error
	Get(account string) (string, error)
	Delete(account string) error
	Name() string
}
