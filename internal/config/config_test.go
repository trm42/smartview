// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDefaultIsTodaysBehaviour pins the built-in defaults to what smartview did
// before it had a config file: the dark theme, a 30s poll, and both new
// behaviours off.
func TestDefaultIsTodaysBehaviour(t *testing.T) {
	got := Default()
	want := Config{
		Theme:               "dark",
		RefreshInterval:     Duration(30 * time.Second),
		StandbyAware:        false,
		ShowUnavailableTabs: false,
		StartView:           StartDrives,
	}
	if got != want {
		t.Errorf("Default() = %+v, want %+v", got, want)
	}
}

// writeFile writes a config file into a temp dir and returns its path.
func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsEverySetting(t *testing.T) {
	path := writeFile(t, `
theme = "phosphor"
refresh_interval = "45s"
standby_aware = true
show_unavailable_tabs = true
start_view = "fleet"
`)
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Theme:               "phosphor",
		RefreshInterval:     Duration(45 * time.Second),
		StandbyAware:        true,
		ShowUnavailableTabs: true,
		StartView:           StartFleet,
	}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

// TestLoadKeepsDefaultsForOmittedKeys pins per-key precedence: a key the file
// does not mention keeps its default rather than decoding to a zero value.
func TestLoadKeepsDefaultsForOmittedKeys(t *testing.T) {
	got, err := Load(writeFile(t, "theme = \"amber\"\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Default()
	want.Theme = "amber"
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

// TestLoadRejectsUnknownKeys is the refuse-to-start contract: a typo must be
// named, not silently ignored. Assert on the key, not the whole message.
func TestLoadRejectsUnknownKeys(t *testing.T) {
	_, err := Load(writeFile(t, "theme = \"dark\"\nrefresh_intervall = \"10s\"\n"))
	if err == nil {
		t.Fatal("Load accepted an unknown key; a typo would be silently ignored")
	}
	if !strings.Contains(err.Error(), "refresh_intervall") {
		t.Errorf("error does not name the offending key: %v", err)
	}
}

// TestLoadNamesEveryUnknownKey: reporting one typo at a time makes fixing a
// stale config a guessing game.
func TestLoadNamesEveryUnknownKey(t *testing.T) {
	_, err := Load(writeFile(t, "colour = \"dark\"\nspin = true\n"))
	if err == nil {
		t.Fatal("Load accepted unknown keys")
	}
	for _, k := range []string{"colour", "spin"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("error does not name %q: %v", k, err)
		}
	}
}

func TestLoadRejectsMalformedTOML(t *testing.T) {
	path := writeFile(t, "theme = \n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted malformed TOML")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error does not name the file: %v", err)
	}
}

// TestLoadRejectsABadDuration covers the Duration TextUnmarshaler's error path.
func TestLoadRejectsABadDuration(t *testing.T) {
	if _, err := Load(writeFile(t, "refresh_interval = \"soon\"\n")); err == nil {
		t.Fatal("Load accepted a non-duration refresh_interval")
	}
}

// knownTheme stands in for ui.HasTheme, which config must not import.
func knownTheme(name string) bool { return name == "dark" || name == "phosphor" }

func TestValidate(t *testing.T) {
	valid := func(mut func(*Config)) Config {
		c := Default()
		mut(&c)
		return c
	}
	tests := []struct {
		name    string
		cfg     Config
		wantErr string // substring the message must carry; "" means it must pass
	}{
		{"defaults", Default(), ""},
		{"every field set", valid(func(c *Config) {
			c.Theme, c.StartView = "phosphor", StartFleet
			c.RefreshInterval = Duration(2 * time.Second)
		}), ""},
		{"unknown theme", valid(func(c *Config) { c.Theme = "sepia" }), "sepia"},
		{"empty theme", valid(func(c *Config) { c.Theme = "" }), "theme"},
		// A zero interval reaches time.NewTicker, which PANICS. The floor here
		// is the only thing standing between a config typo and a dead poll
		// goroutine.
		{"zero interval", valid(func(c *Config) { c.RefreshInterval = 0 }), "refresh_interval"},
		{"negative interval", valid(func(c *Config) { c.RefreshInterval = Duration(-time.Second) }), "refresh_interval"},
		{"sub-second interval", valid(func(c *Config) { c.RefreshInterval = Duration(500 * time.Millisecond) }), "refresh_interval"},
		{"absurd interval", valid(func(c *Config) { c.RefreshInterval = Duration(48 * time.Hour) }), "refresh_interval"},
		{"unknown start view", valid(func(c *Config) { c.StartView = "fleets" }), "fleets"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate(knownTheme)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error naming %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %v, want it to name %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidateIntervalFloorIsTickerSafe pins the floor against the thing it
// protects: every interval Validate accepts must be a legal time.NewTicker
// argument, which panics on <= 0.
func TestValidateIntervalFloorIsTickerSafe(t *testing.T) {
	c := Default()
	c.RefreshInterval = Duration(minInterval)
	if err := c.Validate(knownTheme); err != nil {
		t.Fatalf("the floor itself must be valid: %v", err)
	}
	tick := time.NewTicker(c.RefreshInterval.Duration()) // panics if <= 0
	tick.Stop()
}

func TestSaveRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	want := Config{
		Theme:               "phosphor",
		RefreshInterval:     Duration(2 * time.Second),
		StandbyAware:        true,
		ShowUnavailableTabs: true,
		StartView:           StartFleet,
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// TestSaveWritesACommentedFile pins the reason Save renders a template instead
// of using toml.Encoder: the encoder emits no comments, so saving from the
// settings modal would strip the header off a hand-written file.
func TestSaveWritesACommentedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# smartview configuration", "--config", "standby_aware"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("saved file is missing %q:\n%s", want, body)
		}
	}
}

func TestSaveCreatesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "smartview", "config.toml")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}
}

func TestLoadIfPresentToleratesAMissingFile(t *testing.T) {
	got, err := LoadIfPresent(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("LoadIfPresent on a missing file: %v", err)
	}
	if got != Default() {
		t.Errorf("LoadIfPresent = %+v, want the defaults", got)
	}
}

// TestLoadRequiresAFileTheUserNamed is the other half of the split: a path the
// user typed must exist, or the typo is silently ignored.
func TestLoadRequiresAFileTheUserNamed(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.toml")); err == nil {
		t.Fatal("Load accepted a missing file")
	}
}

// TestLoadIfPresentStillReportsARealError: only a missing file is tolerated.
func TestLoadIfPresentStillReportsARealError(t *testing.T) {
	if _, err := LoadIfPresent(writeFile(t, "theme = \n")); err == nil {
		t.Fatal("LoadIfPresent accepted malformed TOML")
	}
}

func TestPathIsUnderTheUserConfigDir(t *testing.T) {
	got, err := Path()
	if err != nil {
		t.Skipf("no user config dir on this system: %v", err)
	}
	if want := filepath.Join("smartview", "config.toml"); !strings.HasSuffix(got, want) {
		t.Errorf("Path() = %q, want it to end in %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Path() = %q, want an absolute path", got)
	}
}

// TestWithAppliesOnlySetOverrides pins the precedence rule without needing a
// process: a flag the user did not type must not shadow the file.
func TestWithAppliesOnlySetOverrides(t *testing.T) {
	file := Config{
		Theme:           "phosphor",
		RefreshInterval: Duration(5 * time.Second),
		StandbyAware:    true,
		StartView:       StartFleet,
	}

	t.Run("no overrides leaves the file untouched", func(t *testing.T) {
		if got := file.With(Overrides{}); got != file {
			t.Errorf("With(empty) = %+v, want %+v", got, file)
		}
	})

	t.Run("a set override wins", func(t *testing.T) {
		got := file.With(Overrides{Theme: new("amber")})
		want := file
		want.Theme = "amber"
		if got != want {
			t.Errorf("With(theme) = %+v, want %+v", got, want)
		}
	})

	t.Run("overrides do not disturb the other fields", func(t *testing.T) {
		got := file.With(Overrides{RefreshInterval: new(90 * time.Second)})
		want := file
		want.RefreshInterval = Duration(90 * time.Second)
		if got != want {
			t.Errorf("With(interval) = %+v, want %+v", got, want)
		}
	})
}

// TestValidateFlagsTheThemeErrorForTheCaller lets main attach the theme list
// to a theme error and nothing else — the same split as smart.ErrNoSmartctl,
// where which names to print is the caller's knowledge, not this package's.
func TestValidateFlagsTheThemeErrorForTheCaller(t *testing.T) {
	bad := Default()
	bad.Theme = "sepia"
	if err := bad.Validate(knownTheme); !errors.Is(err, ErrUnknownTheme) {
		t.Errorf("theme error = %v, want it to match ErrUnknownTheme", err)
	}

	slow := Default()
	slow.RefreshInterval = 0
	if err := slow.Validate(knownTheme); errors.Is(err, ErrUnknownTheme) {
		t.Errorf("interval error %v matches ErrUnknownTheme; main would append the theme list to it", err)
	}
}
