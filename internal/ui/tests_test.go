// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"testing"
	"time"

	"github.com/trm42/smartview/internal/smart"
)

// idleATAReport is an ATA drive that supports self-tests with no test running.
func idleATAReport() *smart.Report {
	return &smart.Report{
		Device: smart.Device{Protocol: "ATA"},
		ATASmartData: &smart.ATASmartData{
			Capabilities: &struct {
				SelfTestsSupported bool `json:"self_tests_supported"`
			}{SelfTestsSupported: true},
			SelfTest: &struct {
				Status         *smart.ATASelfTestStatus `json:"status"`
				PollingMinutes *smart.SelfTestPolling   `json:"polling_minutes"`
			}{
				PollingMinutes: &smart.SelfTestPolling{Short: 2, Extended: 120},
			},
		},
	}
}

func TestTestsViewIdle(t *testing.T) {
	v := newTestsView(idleATAReport(), selfTestActions{})
	if v.mode != modeIdle {
		t.Fatalf("mode = %v, want idle", v.mode)
	}
	// Exactly two choices — short and long, never conveyance/selective.
	if got := v.list.GetItemCount(); got != 2 {
		t.Fatalf("idle list items = %d, want 2 (short, long only)", got)
	}
	if main, _ := v.list.GetItemText(0); main != "Short test" {
		t.Errorf("first item = %q, want \"Short test\"", main)
	}
	if main, _ := v.list.GetItemText(1); main != "Long (extended) test" {
		t.Errorf("second item = %q", main)
	}
	// Estimated durations come from the ATA polling minutes (extended:120 → 2 h).
	if _, sec := v.list.GetItemText(1); sec != "  ~2 h" {
		t.Errorf("long duration secondary = %q, want \"  ~2 h\"", sec)
	}
}

func TestTestsViewRunning(t *testing.T) {
	pct := 30
	r := &smart.Report{
		Device: smart.Device{Protocol: "NVMe"},
		NVMeSelfTestLog: &smart.NVMeSelfTestLog{
			CurrentSelfTestOperation: &smart.StringValue{Value: 2, String: "Extended self-test in progress"},
			CurrentCompletionPercent: &pct,
		},
	}
	v := newTestsView(r, selfTestActions{})
	if v.mode != modeRunning {
		t.Fatalf("mode = %v, want running", v.mode)
	}
	if got := v.info.GetText(true); got == "" {
		t.Error("running view should render progress text")
	}
}

func TestProgressBar(t *testing.T) {
	if got := progressBar(0); got != "░░░░░░░░░░░░░░░░░░░░░░░░" {
		t.Errorf("0%% bar = %q", got)
	}
	if got := []rune(progressBar(50)); len(got) != 24 {
		t.Errorf("bar width = %d, want 24", len(got))
	}
	if got := progressBar(150); got != "████████████████████████" {
		t.Errorf("clamped 150%% bar = %q", got)
	}
}

func TestFormatTestDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{2 * time.Minute, "2 min"},
		{90 * time.Minute, "1 h 30 min"},
		{120 * time.Minute, "2 h"},
	}
	for _, c := range cases {
		if got := formatTestDuration(c.d); got != c.want {
			t.Errorf("formatTestDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
