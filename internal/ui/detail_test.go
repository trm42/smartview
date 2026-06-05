// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"testing"

	"smartview/internal/smart"
)

func tabIDs(tabs []tab) []string {
	ids := make([]string, len(tabs))
	for i, t := range tabs {
		ids[i] = t.id
	}
	return ids
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestVisibleTabs(t *testing.T) {
	cases := []struct {
		name string
		r    smart.Report
		want []string
	}{
		{"bare", smart.Report{Device: smart.Device{Protocol: "ATA"}}, []string{"overview"}},
		{"ata attrs only", smart.Report{
			Device: smart.Device{Protocol: "ATA"}, ATAAttributes: &smart.ATAAttributes{Table: []smart.ATAAttribute{{ID: 5}}},
		}, []string{"overview", "attributes"}},
		{"nvme", smart.Report{
			Device: smart.Device{Protocol: "NVMe"}, NVMeHealth: &smart.NVMeHealth{},
		}, []string{"overview", "attributes"}},
		{"with logs via self-test timing", smart.Report{
			Device: smart.Device{Protocol: "ATA"}, ATASmartData: &smart.ATASmartData{},
		}, []string{"overview", "logs"}},
		{"with farm", smart.Report{
			Device:        smart.Device{Protocol: "ATA"},
			ATAAttributes: &smart.ATAAttributes{Table: []smart.ATAAttribute{{ID: 5}}},
			FARM:          &smart.FARM{Supported: true},
		}, []string{"overview", "attributes", "farm"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tabIDs(visibleTabs(&c.r)); !eqStrs(got, c.want) {
				t.Errorf("visibleTabs = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSameTabIDs(t *testing.T) {
	a := []tab{{"overview", "Overview"}, {"attributes", "Attributes"}}
	if !sameTabIDs(nil, nil) {
		t.Error("empty/empty should match")
	}
	if !sameTabIDs(a, []tab{{"overview", "x"}, {"attributes", "y"}}) {
		t.Error("same ids should match regardless of title")
	}
	if sameTabIDs(a, []tab{{"attributes", ""}, {"overview", ""}}) {
		t.Error("reordered ids should not match")
	}
	if sameTabIDs(a, a[:1]) {
		t.Error("different lengths should not match")
	}
}

func TestHasLogs(t *testing.T) {
	cases := []struct {
		name string
		r    smart.Report
		want bool
	}{
		{"none", smart.Report{}, false},
		{"ata error log", smart.Report{ATAErrorLog: &smart.ATAErrorLog{}}, true},
		{"nvme self-test", smart.Report{NVMeSelfTestLog: &smart.NVMeSelfTestLog{}}, true},
		{"self-test timing", smart.Report{ATASmartData: &smart.ATASmartData{}}, true},
		{"phy counters", smart.Report{SATAPhyEvents: &smart.SATAPhyEvents{}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasLogs(&c.r); got != c.want {
				t.Errorf("hasLogs = %v, want %v", got, c.want)
			}
		})
	}
}
