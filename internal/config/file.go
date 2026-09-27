// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Path is the default config location under os.UserConfigDir.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate the config directory: %w", err)
	}
	return filepath.Join(dir, "smartview", "config.toml"), nil
}

// LoadIfPresent is [Load], except that a missing file yields the defaults.
func LoadIfPresent(path string) (Config, error) {
	c, err := Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	return c, err
}

// template is what Save writes; toml.Encoder emits no comments. Every
// interpolated value is a bool or from a closed set, so the output is valid TOML.
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

// Save writes c to path atomically (temp file plus rename), creating the
// directory if needed.
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

// writeTemp writes body to a new file in dir and returns its closed name,
// removing it on any failure.
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
	if _, err = f.WriteString(body); err != nil {
		return "", fmt.Errorf("write %s: %w", f.Name(), err)
	}
	return f.Name(), nil
}
