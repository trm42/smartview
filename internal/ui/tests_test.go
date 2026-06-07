// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
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
	// No leading margin: the uniform gutter comes from the box's border padding.
	if _, sec := v.list.GetItemText(1); sec != "~2 h" {
		t.Errorf("long duration secondary = %q, want \"~2 h\"", sec)
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

// visibleBarText strips tview color markup so the bar's printed characters
// (the centered percent label) can be asserted independent of per-cell colors.
func visibleBarText(bar string) string {
	var b strings.Builder
	depth := 0
	for _, r := range bar {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func TestProgressBar(t *testing.T) {
	// 0%: nothing filled (no green segments) but the label is present.
	if got := progressBar(0); strings.Count(got, "[black:green]") != 0 {
		t.Errorf("0%% bar should have no filled cells: %q", got)
	}
	if got := visibleBarText(progressBar(0)); got != "0%" {
		t.Errorf("0%% bar label = %q, want \"0%%\"", got)
	}
	// 50%: half (12 of 24) cells filled, label centered inside.
	if got := progressBar(50); strings.Count(got, "[black:green]") != 12 {
		t.Errorf("50%% bar filled cells = %d, want 12", strings.Count(got, "[black:green]"))
	}
	if got := visibleBarText(progressBar(50)); got != "50%" {
		t.Errorf("50%% bar label = %q, want \"50%%\"", got)
	}
	// 100% (and clamped 150%): all 24 cells filled.
	if got := progressBar(100); strings.Count(got, "[black:green]") != barWidth {
		t.Errorf("100%% bar filled cells = %d, want %d", strings.Count(got, "[black:green]"), barWidth)
	}
	if got := visibleBarText(progressBar(100)); got != "100%" {
		t.Errorf("100%% bar label = %q, want \"100%%\"", got)
	}
	if got := progressBar(150); strings.Count(got, "[black:green]") != barWidth {
		t.Errorf("clamped 150%% bar filled cells = %d, want %d", strings.Count(got, "[black:green]"), barWidth)
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
