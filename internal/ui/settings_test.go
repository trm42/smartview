// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"errors"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/trm42/smartview/internal/config"
	"github.com/trm42/smartview/internal/smart"
)

// recordingApp is a sim App whose saver records instead of writing: no test
// may touch a real config file.
func recordingApp(t *testing.T, cfg config.Config) (*App, *[]config.Config) {
	t.Helper()
	var saved []config.Config
	a, _ := newSimAppCfg(t, 120, 40, cfg)
	a.save = func(c config.Config) error {
		saved = append(saved, c)
		return nil
	}
	return a, &saved
}

// TestCurrentConfigIsDerivedFromLiveState: a cached config would let Save revert a theme chosen with T.
func TestCurrentConfigIsDerivedFromLiveState(t *testing.T) {
	a, _ := recordingApp(t, config.Default())

	a.cycleTheme()
	a.setInterval(5 * time.Second)

	got := a.currentConfig()
	if got.Theme != a.themeName {
		t.Errorf("currentConfig().Theme = %q, want the live %q", got.Theme, a.themeName)
	}
	if got.RefreshInterval.Duration() != 5*time.Second {
		t.Errorf("currentConfig().RefreshInterval = %s, want the live 5s", got.RefreshInterval.Duration())
	}
}

func TestApplySettingsAppliesEverySetting(t *testing.T) {
	a, saved := recordingApp(t, config.Default())

	want := config.Config{
		Theme:               "phosphor",
		RefreshInterval:     config.Duration(2 * time.Second),
		StandbyAware:        true,
		ShowUnavailableTabs: true,
		StartView:           config.StartFleet,
	}
	a.applySettings(want)

	if a.themeName != "phosphor" || activeTheme.Name != "phosphor" {
		t.Errorf("theme = %q/%q, want phosphor applied live", a.themeName, activeTheme.Name)
	}
	if a.interval != 2*time.Second {
		t.Errorf("interval = %s, want 2s", a.interval)
	}
	// setInterval must reach the poll loop, or the cadence lies.
	select {
	case d := <-a.intervalCh:
		if d != 2*time.Second {
			t.Errorf("intervalCh carried %s, want 2s", d)
		}
	default:
		t.Error("applySettings did not signal the poll loop")
	}
	if !a.standbyAware.Load() {
		t.Error("standbyAware not applied")
	}
	if !a.detail.showAllTabs {
		t.Error("showAllTabs not applied")
	}
	if a.startView != config.StartFleet {
		t.Errorf("startView = %q, want fleet", a.startView)
	}
	if len(*saved) != 1 {
		t.Fatalf("saver called %d times, want exactly 1", len(*saved))
	}
	if (*saved)[0] != want {
		t.Errorf("saved %+v, want %+v", (*saved)[0], want)
	}
}

// TestApplySettingsAppliesTabsAlongsideATheme: repaintAll rebuilds from showAllTabs, so the flag must land first.
func TestApplySettingsAppliesTabsAlongsideATheme(t *testing.T) {
	a, _ := recordingApp(t, config.Default())
	r := sparseReport("/dev/sdb")
	a.devices = []smart.Device{r.Device}
	a.reports = map[string]*smart.Report{r.Device.Name: r}
	a.populateList()
	a.showSelected()
	before := a.detail.tabCount()
	if before >= len(allTabs) {
		t.Fatalf("this report is not sparse enough to test: %d tabs", before)
	}

	cfg := a.currentConfig()
	cfg.Theme = "phosphor"
	cfg.ShowUnavailableTabs = true
	a.applySettings(cfg)

	if got := a.detail.tabCount(); got != len(allTabs) {
		t.Errorf("tab strip has %d tabs after the change, want all %d", got, len(allTabs))
	}
}

// TestApplySettingsKeepsFocusOnTheDetail: the rebuild destroys the focused page, so focus must be re-homed.
func TestApplySettingsKeepsFocusOnTheDetail(t *testing.T) {
	a, _ := recordingApp(t, config.Default())
	r := sparseReport("/dev/sdb")
	a.devices = []smart.Device{r.Device}
	a.reports = map[string]*smart.Report{r.Device.Name: r}
	a.populateList()
	a.showSelected()

	a.showSettings()
	a.popModal() // as the Save button does, before applying
	if !a.detail.HasFocus() {
		t.Fatal("popModal did not leave focus on the detail")
	}

	cfg := a.currentConfig()
	cfg.ShowUnavailableTabs = true
	a.applySettings(cfg)

	if !a.detail.HasFocus() {
		t.Error("focus was stranded off-tree by the detail rebuild")
	}
}

// TestApplySettingsSurvivesASaveFailure: the in-memory settings still apply and the failure is reported.
func TestApplySettingsSurvivesASaveFailure(t *testing.T) {
	a, _ := recordingApp(t, config.Default())
	a.save = func(config.Config) error { return errFakeSave }

	cfg := a.currentConfig()
	cfg.StandbyAware = true
	a.applySettings(cfg)

	if !a.standbyAware.Load() {
		t.Error("a failed save discarded the setting")
	}
	if !a.inModal {
		t.Error("a failed save did not report itself")
	}
}

func TestSettingsModalOpensAndCancels(t *testing.T) {
	a, saved := recordingApp(t, config.Default())
	before := a.currentConfig()

	a.showSettings()
	if !a.inModal {
		t.Fatal("showSettings did not enter a modal")
	}
	a.popModal()

	if a.inModal {
		t.Error("still in a modal after popModal")
	}
	if a.currentConfig() != before {
		t.Error("cancelling changed the settings")
	}
	if len(*saved) != 0 {
		t.Error("cancelling wrote the config file")
	}
}

// TestSettingsFormIsThemed: a Form needs styleForm or it is born in tview's palette.
func TestSettingsFormIsThemed(t *testing.T) {
	a, _ := recordingApp(t, config.Default())
	for range themeCycle {
		a.cycleTheme()
		if activeTheme.Background != dark.Background {
			break
		}
	}
	if activeTheme.Background == dark.Background {
		t.Fatal("no theme grounds differently from dark; nothing is under test")
	}

	form := a.settingsForm(a.currentConfig())
	if got := form.GetBackgroundColor(); got != activeTheme.Background {
		t.Errorf("form ground = %v, want %v", got, activeTheme.Background)
	}
	if got := form.GetFormItemCount(); got != 5 {
		t.Errorf("form has %d items, want one per setting (5)", got)
	}
}

var errFakeSave = errors.New("disk on fire")

// TestSettingsKeyOpensTheModal goes through the real input capture to catch a missing binding.
func TestSettingsKeyOpensTheModal(t *testing.T) {
	a, screen := newSimAppCfg(t, 120, 40, config.Default())
	runSim(t, a, screen)
	t.Cleanup(func() { onLoop(t, a, func() any { a.popModal(); return nil }) })

	screen.InjectKey(tcell.KeyRune, 'S', tcell.ModNone)
	waitFor(t, a, "settings modal to open on the S key", func() bool { return a.inModal })
}
