// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"slices"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestNVMeRowsGradeLikeTheGauges: the Attributes rows for spare and endurance
// carry the grade the Overview gauges and the drive verdict give the same reading.
func TestNVMeRowsGradeLikeTheGauges(t *testing.T) {
	ptr := func(n int) *int { return &n }
	cases := []struct {
		name             string
		spare, thr, used *int
		wantSpare        smart.Severity
		wantUsed         smart.Severity
	}{
		{"healthy", ptr(100), ptr(10), ptr(3), smart.SeverityOK, smart.SeverityOK},
		{"spare near threshold, endurance spent", ptr(15), ptr(10), ptr(100), smart.SeverityCaution, smart.SeverityCaution},
		{"spare at threshold, endurance worn", ptr(10), ptr(10), ptr(95), smart.SeverityFailing, smart.SeverityCaution},
		// An unreported threshold grades as 0 everywhere, so row and gauge still agree.
		{"no threshold reported", ptr(5), nil, ptr(120), smart.SeverityCaution, smart.SeverityCaution},
		{"no threshold reported, spare exhausted", ptr(0), nil, ptr(3), smart.SeverityFailing, smart.SeverityOK},
	}
	for _, c := range cases {
		h := &smart.NVMeHealth{AvailableSpare: c.spare, AvailableSpareThreshold: c.thr, PercentageUsed: c.used}
		r := &smart.Report{Device: smart.Device{Protocol: "NVMe"}, SmartStatus: &smart.SmartStatus{Passed: true}, NVMeHealth: h}
		rows := nvmeRows(r)
		sevOf := func(key string) smart.Severity {
			i := slices.IndexFunc(rows, func(kv attrKV) bool { return kv.k == key })
			if i < 0 {
				t.Fatalf("%s: no %q row", c.name, key)
			}
			return rows[i].sev
		}

		pct, thr, _, _ := r.SparePercent()
		if gauge := smart.SpareSeverity(pct, thr); gauge != c.wantSpare {
			t.Errorf("%s: spare gauge grade = %v, want %v", c.name, gauge, c.wantSpare)
		}
		if got := sevOf("Available spare"); got != c.wantSpare {
			t.Errorf("%s: Available spare row = %v, want %v", c.name, got, c.wantSpare)
		}
		used, _, _ := r.LifeUsedPercent()
		if gauge := smart.PctUsedSeverity(used); gauge != c.wantUsed {
			t.Errorf("%s: life gauge grade = %v, want %v", c.name, gauge, c.wantUsed)
		}
		if got := sevOf("Percentage used"); got != c.wantUsed {
			t.Errorf("%s: Percentage used row = %v, want %v", c.name, got, c.wantUsed)
		}
		if got, want := r.Overall(), max(c.wantSpare, c.wantUsed); got != want {
			t.Errorf("%s: Overall = %v, want %v, the worst row", c.name, got, want)
		}
	}
}
