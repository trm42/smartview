// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Path is the default config location, under the platform's user config
// directory: ~/.config/smartview/config.toml on Linux (honouring
// $XDG_CONFIG_HOME), ~/Library/Application Support/... on macOS.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate the config directory: %w", err)
	}
	return filepath.Join(dir, "smartview", "config.toml"), nil
}

// LoadIfPresent is [Load], except that a missing file yields the defaults:
// running with no config is the normal case, not a failure. A path the user
// named explicitly goes through Load instead, where the typo is reported.
func LoadIfPresent(path string) (Config, error) {
	c, err := Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	return c, err
}

// template is what Save writes. It is a rendered template rather than
// toml.Encoder output because the encoder emits no comments, so the first save
// from the settings modal would strip the header off a hand-written file.
// Every value interpolated here is validated against a closed set or is a
// bool, so the result cannot be invalid TOML.
const template = `# smartview configuration
#
#   Linux:  ~/.config/smartview/config.toml
#   macOS:  ~/Library/Application Support/smartview/config.toml
#
# Override the path with --config PATH. A command-line flag always wins over
# this file, and this file always wins over the built-in default.

# Colour theme.
theme = %q

# Auto-refresh cadence, as a Go duration: "2s", "10s", "30s", "1m", "5m".
refresh_interval = %q

# Skip drives that are spun down instead of waking them to be read. The last
# reading is kept and marked stale. ATA only: NVMe has no standby to check, so
# this has no effect on an NVMe-only machine.
standby_aware = %t

# Always draw all six detail tabs, muting the ones this drive reports no data
# for, so a tab keeps the same number and position on every drive.
show_unavailable_tabs = %t

# Which screen to open on: "drives" or "fleet".
start_view = %q
`

// Save writes c to path, creating the directory if needed. The write is
// atomic (temp file plus rename) so an interrupted save cannot leave a
// half-written config that the next startup would refuse to parse.
func Save(path string, c Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	body := fmt.Sprintf(template, c.Theme, c.RefreshInterval.Duration().String(),
		c.StandbyAware, c.ShowUnavailableTabs, c.StartView)

	tmp, err := writeTemp(dir, body)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// writeTemp writes body to a new 0600 file in dir and returns its name, closed
// and ready to rename. The file is removed on any failure, close included, so
// a failed save leaves nothing behind.
func writeTemp(dir, body string) (name string, err error) {
	f, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return "", fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", f.Name(), cerr)
		}
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}()
	if err = f.Chmod(0o600); err != nil {
		return "", fmt.Errorf("set the mode on %s: %w", f.Name(), err)
	}
	if _, err = f.WriteString(body); err != nil {
		return "", fmt.Errorf("write %s: %w", f.Name(), err)
	}
	return f.Name(), nil
}
