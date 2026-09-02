// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// sparseReport has an ATA attribute table and nothing else, so Statistics,
// FARM, Tests and Logs are all unavailable.
func sparseReport(name string) *smart.Report {
	return &smart.Report{
		Device:        smart.Device{Name: name, Type: "sat", Protocol: "ATA"},
		ModelName:     "ACME SpinRight 8TB",
		SmartStatus:   smart.SmartStatus{Passed: true},
		ATAAttributes: &smart.ATAAttributes{},
	}
}

func TestShowAllTabsDrawsEveryTab(t *testing.T) {
	r := sparseReport("/dev/sdb")

	hidden := visibleTabs(r, false)
	shown := visibleTabs(r, true)

	if len(shown) != len(allTabs) {
		t.Errorf("showAll gave %d tabs, want all %d", len(shown), len(allTabs))
	}
	if len(hidden) >= len(shown) {
		t.Fatalf("this report is not sparse enough to test: %d hidden vs %d shown", len(hidden), len(shown))
	}
	// The point of the setting: a tab keeps its number on every drive.
	for i, want := range allTabs {
		if shown[i].id != want.id {
			t.Errorf("tab %d = %q, want %q", i, shown[i].id, want.id)
		}
	}
	if !shown[0].available {
		t.Error("overview must always be available")
	}
}

func TestStepTabSkipsUnavailableTabs(t *testing.T) {
	d := newDetail()
	d.showAllTabs = true
	d.update(sparseReport("/dev/sdb"), nil)

	if d.activeID() != "overview" {
		t.Fatalf("active = %q, want overview", d.activeID())
	}
	if !d.stepTab(1) {
		t.Fatal("stepTab(1) did not move")
	}
	if got := d.activeID(); got != "attributes" {
		t.Errorf("stepTab landed on %q, want attributes (the next *available* tab)", got)
	}
	// Nothing after attributes is available on this drive.
	if d.stepTab(1) {
		t.Errorf("stepTab(1) moved to %q; every later tab is unavailable", d.activeID())
	}
	if !d.stepTab(-1) || d.activeID() != "overview" {
		t.Errorf("stepTab(-1) = %q, want overview", d.activeID())
	}
}

func TestSelectTabDeclinesAnUnavailableTab(t *testing.T) {
	d := newDetail()
	d.showAllTabs = true
	d.update(sparseReport("/dev/sdb"), nil)

	farm := -1
	for i, tb := range d.tabs {
		if tb.id == "farm" {
			farm = i
		}
	}
	if farm < 0 {
		t.Fatal("no farm tab in the always-six strip")
	}
	if d.tabs[farm].available {
		t.Fatal("farm should be unavailable on this report")
	}
	if d.selectTab(farm) {
		t.Error("selectTab accepted an unavailable tab")
	}
	if d.activeID() != "overview" {
		t.Errorf("active moved to %q; the digit must do nothing", d.activeID())
	}
	// selectTabID routes through selectTab, so 't' keeps today's behaviour.
	if d.selectTabID("tests") {
		t.Error("selectTabID accepted an unavailable tab")
	}
}

// TestTabSetRebuildsWhenAvailabilityChanges is the sameTabs trap: with
// show_unavailable_tabs the id list is constant across every drive and every
// poll, so an id-only comparison would refresh in place and never replace an
// unavailable tab's placeholder with the real view.
func TestTabSetRebuildsWhenAvailabilityChanges(t *testing.T) {
	d := newDetail()
	d.showAllTabs = true
	const dev = "/dev/sdb"

	d.update(sparseReport(dev), nil)
	placeholder := d.views["statistics"]

	// The same device, now reporting device statistics.
	richer := sparseReport(dev)
	richer.ATADeviceStatistics = &smart.ATADeviceStatistics{
		Pages: []smart.ATAStatPage{{Number: 1, Name: "General", Table: []smart.ATAStatEntry{
			{Name: "Lifetime Power-On Resets", Value: 42, Flags: smart.ATAStatFlags{Valid: true}},
		}}},
	}
	d.update(richer, nil)

	if !d.tabs[2].available {
		t.Fatal("statistics is still unavailable: either the tab set was not " +
			"rebuilt (sameTabs ignoring availability) or the fixture is wrong")
	}
	if d.views["statistics"] == placeholder {
		t.Error("the statistics placeholder survived the drive gaining the data")
	}
}

func TestSameTabsComparesAvailability(t *testing.T) {
	a := []tab{{id: "overview", title: "Overview", available: true}}
	if !sameTabs(a, a) {
		t.Error("identical slices should match")
	}
	if sameTabs(a, []tab{{id: "overview", title: "Overview", available: false}}) {
		t.Error("availability must be part of the comparison, or the rebuild is skipped")
	}
	if sameTabs(a, nil) {
		t.Error("different lengths should not match")
	}
	// Titles are part of the struct now, so a retitled tab rebuilds too. That
	// is a behaviour change from sameTabIDs and it is the safe direction.
	if sameTabs(a, []tab{{id: "overview", title: "x", available: true}}) {
		t.Error("a different title should not match")
	}
	reordered := []tab{
		{id: "attributes", available: true}, {id: "overview", available: true},
	}
	if sameTabs([]tab{{id: "overview", available: true}, {id: "attributes", available: true}}, reordered) {
		t.Error("reordered ids should not match")
	}
}

func TestUnavailablePillsRenderMuted(t *testing.T) {
	d := newDetail()
	d.showAllTabs = true
	d.update(sparseReport("/dev/sdb"), nil)
	d.bar.lastWidth = 200 // wide enough that no title is dropped
	d.bar.layout()

	got := d.bar.GetText(false)
	if !strings.Contains(got, unavailableTabTag()) {
		t.Errorf("no muted pill in the strip: %q", got)
	}
	// Mutation guard: with every tab available the muted tag must be absent.
	d2 := newDetail()
	d2.showAllTabs = false
	d2.update(sparseReport("/dev/sdb"), nil)
	d2.bar.lastWidth = 200
	d2.bar.layout()
	if strings.Contains(d2.bar.GetText(false), unavailableTabTag()) {
		t.Error("a strip of available tabs carries the muted tag")
	}
}

// TestOpenTabOnAnUnavailablePillLeavesFocus: a click behaves like the digit.
func TestOpenTabOnAnUnavailablePillLeavesFocus(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	a.detail.showAllTabs = true
	a.detail.update(sparseReport("/dev/sdb"), nil)
	a.app.SetFocus(a.list)

	farm := -1
	for i, tb := range a.detail.tabs {
		if tb.id == "farm" {
			farm = i
		}
	}
	a.openTab(farm)

	if !a.list.HasFocus() {
		t.Error("clicking an unavailable pill moved focus off the list")
	}
	if a.detail.activeID() != "overview" {
		t.Errorf("active = %q, want overview", a.detail.activeID())
	}
}
