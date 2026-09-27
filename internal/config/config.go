// SPDX-License-Identifier: GPL-3.0-or-later

// Package config is smartview's TOML settings file; it imports nothing of smartview's own.
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

// Default returns the built-in settings.
func Default() Config {
	return Config{
		Theme:           "dark",
		RefreshInterval: Duration(30 * time.Second),
		StartView:       StartDrives,
	}
}

// Duration is a time.Duration that round-trips as a TOML string ("30s"): TOML
// has no duration type.
type Duration time.Duration

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// UnmarshalText decodes a Go duration string ("30s", "1m").
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Load reads path, decoding over Default so an omitted key keeps its default.
func Load(path string) (Config, error) {
	c := Default()
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return Default(), fmt.Errorf("%s: %w", path, err)
	}
	// Report every unknown key at once, not one typo per run.
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

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// minInterval guards time.NewTicker, which panics on a non-positive duration;
// maxInterval catches a mistyped unit.
const (
	minInterval = time.Second
	maxInterval = 24 * time.Hour
)

// ErrUnknownTheme reports a theme the UI does not define; the caller attaches the list of choices.
var ErrUnknownTheme = errors.New("unknown theme")

// Validate checks every setting; knownTheme is injected because the theme registry lives in internal/ui.
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

// Overrides are the settings a flag supplied; nil means absent, so a flag at
// its default cannot shadow the file.
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
