// SPDX-License-Identifier: GPL-3.0-or-later

// Command smartview is a cross-platform terminal UI for monitoring drive health
// via smartmontools (smartctl). It runs on macOS and Linux.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/trm42/smartview/internal/smart"
	"github.com/trm42/smartview/internal/ui"
)

// version is the build version. Override it at link time for release builds:
//
//	go build -ldflags "-X main.version=v1.2.3"
//
// Left at "dev" it falls back to module/VCS info embedded by the Go toolchain,
// so `go install` and VCS-stamped builds still report something useful.
var version = "dev"

func main() {
	interval := flag.Duration("interval", 5*time.Second, "auto-refresh interval")
	fixtures := flag.String("fixtures", "", "load drive data from JSON fixtures in DIR instead of smartctl (requires -tags dev build)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("smartview", buildVersion())
		return
	}

	if *fixtures != "" {
		if err := smart.UseFixtures(*fixtures); err != nil {
			fmt.Fprintln(os.Stderr, "smartview:", err)
			os.Exit(1)
		}
	} else if !smart.Available() {
		fmt.Fprintln(os.Stderr, "smartview: smartctl not found on PATH.")
		fmt.Fprintln(os.Stderr, "Install smartmontools:", installHint())
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := ui.New(*interval)
	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "smartview:", err)
		os.Exit(1)
	}
}

// buildVersion resolves the version to report. It prefers the link-time value,
// then the module version, then the VCS revision (with a -dirty suffix for an
// uncommitted tree) recorded in the build info.
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
