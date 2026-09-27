// SPDX-License-Identifier: GPL-3.0-or-later

// Command smartview is a terminal UI for monitoring drive health via smartctl.
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
	def := config.Default()
	interval := flag.Duration("interval", def.RefreshInterval.Duration(), "auto-refresh interval")
	fixtures := flag.String("fixtures", "", "load drive data from JSON fixtures in DIR instead of smartctl (requires -tags dev build)")
	theme := flag.String("theme", def.Theme, "colour theme: "+ui.ThemeNames())
	cfgPath := flag.String("config", "", "settings file (default: "+defaultPathHint()+")")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("smartview", buildVersion())
		return
	}

	// Before the preflight so a config typo is not reported after a 5s timeout.
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

// loadConfig layers defaults, the config file, then only flags the user typed
// (flag.Visit), so a defaulted flag cannot shadow the file.
func loadConfig(path string, interval *time.Duration, theme *string) (config.Config, error) {
	cfg := config.Default()
	var err error
	if path != "" {
		cfg, err = config.Load(path)
	} else if p, perr := config.Path(); perr == nil { // No user config dir: run on defaults.
		cfg, err = config.LoadIfPresent(p)
	}
	if err != nil {
		return cfg, err
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

	// The theme list is main's knowledge, so it is attached here.
	err = cfg.Validate(ui.HasTheme)
	if errors.Is(err, config.ErrUnknownTheme) {
		return cfg, fmt.Errorf("%w (choices: %s)", err, ui.ThemeNames())
	}
	return cfg, err
}

// configPath returns the named file, else the platform default.
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

// defaultPathHint names the default config file for -h, which must print even without one.
func defaultPathHint() string {
	if p, err := config.Path(); err == nil {
		return p
	}
	return "none; no user config directory"
}

// preflightTimeout guards against a wedged smartctl; the probe touches no device.
const preflightTimeout = 5 * time.Second

// exitPreflight reports a failed startup check and exits: 130 on interrupt,
// the timeout named on deadline.
func exitPreflight(err error) {
	switch {
	case errors.Is(err, context.Canceled):
		os.Exit(130)
	case errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintf(os.Stderr, "smartview: smartctl did not respond within %s\n", preflightTimeout)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "smartview:", err)
	// Which package manager to name is main's knowledge, not the data layer's.
	switch {
	case errors.Is(err, smart.ErrNoSmartctl):
		fmt.Fprintln(os.Stderr, "Install smartmontools:", installHint())
	case errors.Is(err, smart.ErrOldSmartctl):
		fmt.Fprintln(os.Stderr, "Upgrade smartmontools:", installHint())
	}
	os.Exit(1)
}

// preflight runs smart.Preflight under preflightTimeout.
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
		return rev[:min(len(rev), 12)] + suffix
	}
	return version
}

func installHint() string {
	if runtime.GOOS == "darwin" {
		return "brew install smartmontools"
	}
	return "apt install smartmontools  (or your distro's package manager)"
}
