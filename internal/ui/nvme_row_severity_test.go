// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"slices"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestNVMeRowsGradeLikeTheGauges: the Attributes rows for spare and endurance
// carry the grade the Overview gauges give the same reading.
func TestNVMeRowsGradeLikeTheGauges(t *testing.T) {
	ptr := func(n int) *int { return &n }
	cases := []struct {
		name             string
		spare, thr, used *int
		wantSpare        smart.Severity
		wantUsed         smart.Severity
	}{
		{"healthy", ptr(100), ptr(10), ptr(3), smart.SeverityOK, smart.SeverityOK},
		{"spare near threshold, endurance spent", ptr(15), ptr(10), ptr(100), smart.SeverityCaution, smart.SeverityFailing},
		{"spare at threshold, endurance worn", ptr(10), ptr(10), ptr(95), smart.SeverityFailing, smart.SeverityCaution},
		// No threshold to grade against: the spare row stays ungraded.
		{"no threshold reported", ptr(5), nil, ptr(120), smart.SeverityOK, smart.SeverityFailing},
		{"no threshold reported, spare exhausted", ptr(0), nil, ptr(3), smart.SeverityOK, smart.SeverityOK},
	}
	for _, c := range cases {
		h := &smart.NVMeHealth{AvailableSpare: c.spare, AvailableSpareThreshold: c.thr, PercentageUsed: c.used}
		r := &smart.Report{NVMeHealth: h}
		rows := nvmeRows(h)
		sevOf := func(key string) smart.Severity {
			i := slices.IndexFunc(rows, func(kv attrKV) bool { return kv.k == key })
			if i < 0 {
				t.Fatalf("%s: no %q row", c.name, key)
			}
			return rows[i].sev
		}

		if got := sevOf("Available spare"); got != c.wantSpare {
			t.Errorf("%s: Available spare row = %v, want %v", c.name, got, c.wantSpare)
		}
		if c.thr != nil {
			pct, thr, _ := r.SparePercent()
			if gauge := spareSeverityPct(pct, thr); gauge != c.wantSpare {
				t.Errorf("%s: spare gauge grade = %v, row = %v", c.name, gauge, c.wantSpare)
			}
		}

		used, _ := r.LifeUsedPercent()
		gauge := lifeUsedSeverity(used)
		if gauge != c.wantUsed {
			t.Fatalf("%s: life gauge grade = %v, want %v", c.name, gauge, c.wantUsed)
		}
		if got := sevOf("Percentage used"); got != gauge {
			t.Errorf("%s: Percentage used row = %v, gauge = %v", c.name, got, gauge)
		}
	}
}
