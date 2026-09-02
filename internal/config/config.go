// SPDX-License-Identifier: GPL-3.0-or-later

// Package config is smartview's TOML settings file. It imports nothing of
// smartview's own: main loads and validates it, internal/ui edits it.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Start-view values: which top-level screen smartview opens on.
const (
	StartDrives = "drives"
	StartFleet  = "fleet"
)

// Config is the persisted settings. Field order matches the file.
type Config struct {
	Theme               string   `toml:"theme"`
	RefreshInterval     Duration `toml:"refresh_interval"`
	StandbyAware        bool     `toml:"standby_aware"`
	ShowUnavailableTabs bool     `toml:"show_unavailable_tabs"`
	StartView           string   `toml:"start_view"`
}

// Duration is a time.Duration that round-trips as a TOML string ("30s"): TOML
// has no duration type.
type Duration time.Duration

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Default returns the built-in settings, which reproduce smartview's behaviour
// from before it had a config file.
func Default() Config {
	return Config{
		Theme:           "dark",
		RefreshInterval: Duration(30 * time.Second),
		StartView:       StartDrives,
	}
}

// UnmarshalText decodes a Go duration string ("30s", "1m").
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalText renders the duration the way UnmarshalText reads it.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.Duration().String()), nil }

// Load reads path. It starts from Default and decodes over it, so a key the
// file omits keeps its default instead of becoming a zero value.
func Load(path string) (Config, error) {
	c := Default()
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return Default(), fmt.Errorf("%s: %w", path, err)
	}
	// Every unknown key at once: reporting one typo per run makes fixing a
	// stale config a guessing game.
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return Default(), fmt.Errorf("%s: unknown setting%s: %s",
			path, plural(len(un)), strings.Join(keys, ", "))
	}
	return c, nil
}

// plural returns the "s" that makes a count read correctly.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Interval bounds. The floor is not cosmetic: poll.go hands the interval
// straight to time.NewTicker, which panics on a non-positive duration, so a
// "0s" in a config file would kill the poll goroutine on its first tick. The
// ceiling only catches a plainly mistyped unit.
const (
	minInterval = time.Second
	maxInterval = 24 * time.Hour
)

// ErrUnknownTheme reports a theme name the UI does not define. Callers match
// it to list the choices: which themes exist is internal/ui's knowledge, not
// this package's — the same split as smart.ErrNoSmartctl.
var ErrUnknownTheme = errors.New("unknown theme")

// Validate checks every setting. knownTheme is injected because the theme
// registry lives in internal/ui, which config must not import; main passes
// ui.HasTheme.
func (c Config) Validate(knownTheme func(string) bool) error {
	if !knownTheme(c.Theme) {
		return fmt.Errorf("%w %q", ErrUnknownTheme, c.Theme)
	}
	if d := c.RefreshInterval.Duration(); d < minInterval || d > maxInterval {
		return fmt.Errorf("refresh_interval %s out of range (%s to %s)",
			d, minInterval, maxInterval)
	}
	if c.StartView != StartDrives && c.StartView != StartFleet {
		return fmt.Errorf("unknown start_view %q (want %q or %q)",
			c.StartView, StartDrives, StartFleet)
	}
	return nil
}

// Overrides are the settings a command-line flag supplied. A nil field means
// the flag was absent, which is what keeps a flag left at its default from
// shadowing the config file.
type Overrides struct {
	Theme           *string
	RefreshInterval *time.Duration
}

// With returns c with every set override applied: flag beats file beats
// default.
func (c Config) With(o Overrides) Config {
	if o.Theme != nil {
		c.Theme = *o.Theme
	}
	if o.RefreshInterval != nil {
		c.RefreshInterval = Duration(*o.RefreshInterval)
	}
	return c
}
