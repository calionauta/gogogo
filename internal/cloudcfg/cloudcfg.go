// SCOPE:core — Local cloud-connection settings (sync.json, 0600).
//
// The desktop reads this file at boot when NATS_LEAFNODE_URL is unset, so a
// user can connect the app to a server from the account page without editing
// environment variables. It lives next to the database (DATA_DIR) and never in
// the web database: it is a credential, not app data.
package cloudcfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the file under DATA_DIR.
const FileName = "sync.json"

const (
	fileMode = 0o600 // holds the device token
	dirMode  = 0o750
)

// Config is the saved connection.
type Config struct {
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

// Path returns the settings file for a data dir.
func Path(dataDir string) string { return filepath.Join(dataDir, FileName) }

// Load reads the file. A missing file returns (nil, nil): "not connected" is
// not an error.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("cloudcfg: read %s: %w", path, err)
	}
	var c Config
	if perr := json.Unmarshal(raw, &c); perr != nil {
		return nil, fmt.Errorf("cloudcfg: parse %s: %w", path, perr)
	}
	if verr := Validate(c.URL); verr != nil {
		return nil, verr
	}
	return &c, nil
}

// Save validates and writes the file 0600 (token inside).
func Save(path string, c Config) error {
	clean, err := Normalize(c.URL)
	if err != nil {
		return err
	}
	c.URL = clean
	if derr := os.MkdirAll(filepath.Dir(path), dirMode); derr != nil {
		return fmt.Errorf("cloudcfg: create dir: %w", derr)
	}
	raw, merr := json.MarshalIndent(c, "", "  ")
	if merr != nil {
		return fmt.Errorf("cloudcfg: marshal: %w", merr)
	}
	if err := os.WriteFile(path, raw, fileMode); err != nil {
		return fmt.Errorf("cloudcfg: write %s: %w", path, err)
	}
	return nil
}

// Clear removes the connection file.
func Clear(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cloudcfg: clear: %w", err)
	}
	return nil
}

// Validate accepts only the schemes a NATS leaf node can dial, with a host and
// without embedded credentials (the token is a separate field, so a pasted URL
// never hides a secret in a log line).
func Validate(raw string) error {
	_, err := Normalize(raw)
	return err
}

// Normalize returns the canonical URL or an error.
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("cloudcfg: server URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("cloudcfg: unreadable server URL")
	}
	switch u.Scheme {
	case "nats", "tls", "ws", "wss":
	default:
		return "", errors.New("cloudcfg: server URL must be nats://, tls://, ws:// or wss://")
	}
	if u.Host == "" {
		return "", errors.New("cloudcfg: server URL needs a host")
	}
	if u.User != nil {
		return "", errors.New("cloudcfg: put the token in the token field, not in the URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("cloudcfg: server URL must not carry a query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// HostAllowed enforces an optional operator allowlist (CLOUD_SYNC_ALLOW).
// An empty list permits any valid URL; otherwise the normalized host (with
// port when the allow entry names one) must match exactly.
func HostAllowed(raw string, allow []string) error {
	if len(allow) == 0 {
		return nil
	}
	clean, err := Normalize(raw)
	if err != nil {
		return err
	}
	u, err := url.Parse(clean)
	if err != nil {
		return err
	}
	for _, entry := range allow {
		want := strings.TrimSpace(entry)
		if want == "" {
			continue
		}
		if strings.EqualFold(want, u.Host) || (!strings.Contains(want, ":") && strings.EqualFold(want, u.Hostname())) {
			return nil
		}
	}
	return fmt.Errorf("cloudcfg: host %q is not in CLOUD_SYNC_ALLOW", u.Host)
}

// LeafURL is the URL the embedded NATS dials: the token, when present, rides as
// userinfo (NATS token auth).
func LeafURL(c Config) (string, error) {
	clean, err := Normalize(c.URL)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(c.Token) == "" {
		return clean, nil
	}
	u, err := url.Parse(clean)
	if err != nil {
		return "", err
	}
	u.User = url.User(c.Token)
	return u.String(), nil
}
