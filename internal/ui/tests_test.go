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
	// The bar is one run of █ in the theme's OK colour followed by one run of ░
	// in the muted colour, then the label. Counting █ gives the filled cells;
	// the label is outside the fill (see TestProgressBarLabelIsOutsideTheFill).
	filled := func(pct int) int { return strings.Count(progressBar(pct), "█") }

	if got := filled(0); got != 0 {
		t.Errorf("0%% bar filled cells = %d, want 0", got)
	}
	if got := visibleBarText(progressBar(0)); !strings.Contains(got, "0%") {
		t.Errorf("0%% bar should contain label, got %q", got)
	}
	if got := filled(50); got != 12 {
		t.Errorf("50%% bar filled cells = %d, want 12", got)
	}
	if got := visibleBarText(progressBar(50)); !strings.Contains(got, "50%") {
		t.Errorf("50%% bar should contain label, got %q", got)
	}
	if got := filled(100); got != barWidth {
		t.Errorf("100%% bar filled cells = %d, want %d", got, barWidth)
	}
	if got := visibleBarText(progressBar(100)); !strings.Contains(got, "100%") {
		t.Errorf("100%% bar should contain label, got %q", got)
	}
	// Out-of-range percentages clamp rather than overflowing the bar.
	if got := filled(150); got != barWidth {
		t.Errorf("clamped 150%% bar filled cells = %d, want %d", got, barWidth)
	}
	if got := filled(-10); got != 0 {
		t.Errorf("clamped -10%% bar filled cells = %d, want 0", got)
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

// TestProgressBarLabelIsOutsideTheFill pins a rendering fix: the percent label
// used to be written into the bar, replacing fill cells, so a run at 60% drew
// as "██████████60%█░░░" — the bar appeared broken exactly where the eye goes.
func TestProgressBarLabelIsOutsideTheFill(t *testing.T) {
	got := progressBar(60)
	plain := stripTags(got)
	bar, label, found := strings.Cut(plain, "  ")
	if !found {
		t.Fatalf("no label after the bar: %q", plain)
	}
	if strings.ContainsAny(bar, "0123456789%") {
		t.Errorf("digits inside the bar fill: %q", bar)
	}
	if len([]rune(bar)) != barWidth {
		t.Errorf("bar is %d cells, want %d: %q", len([]rune(bar)), barWidth, bar)
	}
	if strings.TrimSpace(label) != "60%" {
		t.Errorf("label = %q, want 60%%", label)
	}
	if strings.Count(bar, "█") != barWidth*60/100 {
		t.Errorf("fill does not match 60%%: %q", bar)
	}
}

// stripTags removes tview colour markup so a rendered string can be measured.
func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '[':
			depth++
		case r == ']' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}
