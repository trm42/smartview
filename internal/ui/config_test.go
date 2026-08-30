// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"testing"
	"time"

	"github.com/trm42/smartview/internal/config"
)

// TestNewAppliesTheConfig pins that every setting reaches the App, not just
// the two that had flags before. A setting that loads but never lands is the
// failure mode a config file invites.
func TestNewAppliesTheConfig(t *testing.T) {
	t.Cleanup(func() { setTheme(themes["dark"]) })
	cfg := config.Config{
		Theme:               "phosphor",
		RefreshInterval:     config.Duration(5 * time.Second),
		StandbyAware:        true,
		ShowUnavailableTabs: true,
		StartView:           config.StartFleet,
	}
	a := New(cfg, func(config.Config) error { return nil })

	if a.themeName != "phosphor" {
		t.Errorf("themeName = %q, want phosphor", a.themeName)
	}
	if activeTheme.Name != "phosphor" {
		t.Errorf("activeTheme = %q, want phosphor: New must install the theme before build", activeTheme.Name)
	}
	if a.interval != 5*time.Second {
		t.Errorf("interval = %s, want 5s", a.interval)
	}
	if !a.standbyAware.Load() {
		t.Error("standbyAware = false, want true")
	}
	if !a.detail.showAllTabs {
		t.Error("detail.showAllTabs = false, want true")
	}
	if a.startView != config.StartFleet {
		t.Errorf("startView = %q, want %q", a.startView, config.StartFleet)
	}
}

// TestNewWithDefaultsMatchesTodaysBehaviour: the default config must build the
// same App the old New(30s, "dark") did.
func TestNewWithDefaultsMatchesTodaysBehaviour(t *testing.T) {
	t.Cleanup(func() { setTheme(themes["dark"]) })
	a := New(config.Default(), func(config.Config) error { return nil })
	if a.themeName != "dark" || a.interval != 30*time.Second {
		t.Errorf("theme/interval = %q/%s, want dark/30s", a.themeName, a.interval)
	}
	if a.standbyAware.Load() || a.detail.showAllTabs {
		t.Error("the new behaviours must default off")
	}
}

// TestStartViewOpensTheFleet pins that start_view is consulted, not merely
// stored. Run itself shells out to smartctl, so the step is exercised through
// the method Run calls.
func TestStartViewOpensTheFleet(t *testing.T) {
	t.Cleanup(func() { setTheme(themes["dark"]) })
	cfg := config.Default()
	cfg.StartView = config.StartFleet
	a := New(cfg, func(config.Config) error { return nil })

	a.applyStartView()

	if !a.fleetMode {
		t.Error("fleetMode = false; start_view = fleet must open the comparison")
	}
	if name, _ := a.bodyPages.GetFrontPage(); name != pageFleet {
		t.Errorf("front page = %q, want %q", name, pageFleet)
	}
}

func TestStartViewDefaultsToTheDriveList(t *testing.T) {
	t.Cleanup(func() { setTheme(themes["dark"]) })
	a := New(config.Default(), func(config.Config) error { return nil })
	a.applyStartView()
	if a.fleetMode {
		t.Error("fleetMode = true; the default start_view must be the drive list")
	}
}
