// SPDX-License-Identifier: GPL-3.0-or-later

// Command smartview is a cross-platform terminal UI for monitoring drive health
// via smartmontools (smartctl). It runs on macOS and Linux.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/trm42/smartview/internal/config"
	"github.com/trm42/smartview/internal/smart"
	"github.com/trm42/smartview/internal/ui"
)

// version is overridden at link time (-ldflags "-X main.version=v1.2.3");
// left at "dev" it falls back to embedded module/VCS info.
var version = "dev"

func main() {
	interval := flag.Duration("interval", 30*time.Second, "auto-refresh interval")
	fixtures := flag.String("fixtures", "", "load drive data from JSON fixtures in DIR instead of smartctl (requires -tags dev build)")
	theme := flag.String("theme", "dark", "colour theme: "+ui.ThemeNames())
	cfgPath := flag.String("config", "", "settings file (default: "+defaultPathHint()+")")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("smartview", buildVersion())
		return
	}

	// Before the preflight, so a config typo is reported at once rather than
	// after a 5s smartctl timeout.
	cfg, err := loadConfig(*cfgPath, interval, theme)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smartview:", err)
		os.Exit(1)
	}

	if *fixtures != "" {
		if err := smart.UseFixtures(*fixtures); err != nil {
			fmt.Fprintln(os.Stderr, "smartview:", err)
			os.Exit(1)
		}
	}

	// Installed before the preflight, so Ctrl-C works while it runs.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := preflight(ctx); err != nil {
		exitPreflight(err)
	}

	app := ui.New(cfg, func(c config.Config) error { return saveConfig(*cfgPath, c) })
	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "smartview:", err)
		os.Exit(1)
	}
}

// loadConfig resolves the settings: built-in defaults, then the config file,
// then any flag the user actually typed. A flag left at its default must not
// shadow the file, which is what flag.Visit distinguishes.
func loadConfig(path string, interval *time.Duration, theme *string) (config.Config, error) {
	cfg := config.Default()
	switch resolved, err := configPath(path); {
	case err != nil:
		// No user config directory: run on defaults rather than refuse to start.
	case path != "":
		// A path the user named must exist; a missing default one need not.
		if cfg, err = config.Load(resolved); err != nil {
			return cfg, err
		}
	default:
		if cfg, err = config.LoadIfPresent(resolved); err != nil {
			return cfg, err
		}
	}

	var o config.Overrides
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "interval":
			o.RefreshInterval = interval
		case "theme":
			o.Theme = theme
		}
	})
	cfg = cfg.With(o)

	// One validator for both sources, so a bad flag and a bad file read alike.
	// Which themes exist is this program's knowledge, so the list is attached
	// here and only to the error that wants it.
	err := cfg.Validate(ui.HasTheme)
	if errors.Is(err, config.ErrUnknownTheme) {
		return cfg, fmt.Errorf("%w (choices: %s)", err, ui.ThemeNames())
	}
	return cfg, err
}

// configPath returns the file to read: the one the user named, else the
// platform default.
func configPath(named string) (string, error) {
	if named != "" {
		return named, nil
	}
	return config.Path()
}

// saveConfig writes the settings back to whichever file they came from.
func saveConfig(named string, c config.Config) error {
	path, err := configPath(named)
	if err != nil {
		return err
	}
	return config.Save(path, c)
}

// defaultPathHint names the default config file for -h. It degrades to a bare
// description rather than failing: -h must always print.
func defaultPathHint() string {
	if p, err := config.Path(); err == nil {
		return p
	}
	return "none; no user config directory"
}

// preflightTimeout bounds the startup check. It runs `smartctl -j -V`, which
// touches no device, so this is a guard against a wedged binary rather than a
// slow one.
const preflightTimeout = 5 * time.Second

// exitPreflight reports a failed startup check and exits. An interrupt is a
// quiet abort rather than an error, and a deadline names the timeout instead of
// printing the context that carried it.
func exitPreflight(err error) {
	switch {
	case errors.Is(err, context.Canceled):
		os.Exit(130) // interrupted before the UI came up
	case errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintf(os.Stderr, "smartview: smartctl did not respond within %s\n", preflightTimeout)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "smartview:", err)
	// Which package manager to name is this program's knowledge, not the data
	// layer's, so the hint is attached here.
	switch {
	case errors.Is(err, smart.ErrNoSmartctl):
		fmt.Fprintln(os.Stderr, "Install smartmontools:", installHint())
	case errors.Is(err, smart.ErrOldSmartctl):
		fmt.Fprintln(os.Stderr, "Upgrade smartmontools:", installHint())
	}
	os.Exit(1)
}

// preflight runs smart.Preflight under a deadline; a no-op in fixture mode.
func preflight(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, preflightTimeout)
	defer cancel()
	return smart.Preflight(ctx)
}

// buildVersion prefers the link-time value, then the module version, then the
// VCS revision (-dirty when the tree was uncommitted).
func buildVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev, suffix string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				suffix = "-dirty"
			}
		}
	}
	if rev != "" {
		if len(rev) > 12 {
			rev = rev[:12]
		}
		return rev + suffix
	}
	return version
}

// installHint returns the platform-appropriate install command.
func installHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install smartmontools"
	default:
		return "apt install smartmontools  (or your distro's package manager)"
	}
}
