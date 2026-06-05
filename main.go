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
	"syscall"
	"time"

	"github.com/trm42/smartview/internal/smart"
	"github.com/trm42/smartview/internal/ui"
)

func main() {
	interval := flag.Duration("interval", 5*time.Second, "auto-refresh interval")
	fixtures := flag.String("fixtures", "", "load drive data from JSON fixtures in DIR instead of smartctl (requires -tags dev build)")
	flag.Parse()

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

// installHint returns the platform-appropriate install command.
func installHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install smartmontools"
	default:
		return "apt install smartmontools  (or your distro's package manager)"
	}
}
