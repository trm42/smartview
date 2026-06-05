// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"

	"smartview/internal/smart"
)

// overviewView renders the Overview tab: a health banner, an identity panel
// beside protocol-specific gauges, and a temperature sparkline. It refreshes in
// place, keeping the banner widget stable so a focused Overview is not disturbed.
type overviewView struct {
	*tview.Flex
	banner *tview.TextView
}

// newOverviewView builds the Overview tab. tempHistory is the runtime-accumulated
// series used for NVMe drives, which lack an on-device temperature log.
func newOverviewView(r *smart.Report, tempHistory []float64) *overviewView {
	v := &overviewView{
		Flex:   tview.NewFlex().SetDirection(tview.FlexRow),
		banner: tview.NewTextView().SetDynamicColors(true),
	}
	v.refresh(r, tempHistory)
	return v
}

// refresh rebuilds the panel contents for a new report. The held banner widget
// is reused (stays first), so focus on the Overview tab is preserved.
func (v *overviewView) refresh(r *smart.Report, tempHistory []float64) {
	sev := r.Overall()
	v.banner.SetText(fmt.Sprintf("  [%s::b]%s[-:-:-]   SMART self-assessment: %s",
		severityTag(sev), sev.String(), passFailText(r)))

	v.Clear()
	v.AddItem(v.banner, 1, 0, false)

	// Surface a per-drive smartctl error message (a permission/open failure, or a
	// log-read limitation common on Apple internal SSDs). It is a data-availability
	// caveat, not a health verdict — the verdict is the banner above — so it is
	// styled as a yellow notice rather than an alarming red one.
	if msg, ok := r.FatalMessage(); ok {
		errLine := tview.NewTextView().SetDynamicColors(true)
		errLine.SetText(fmt.Sprintf("  [yellow]⚠ %s[-]", msg))
		v.AddItem(errLine, 1, 0, false)
	}

	mid := tview.NewFlex() // horizontal: identity | gauges
	mid.AddItem(buildIdentity(r), 0, 2, false)
	if g := buildGauges(r); g != nil {
		mid.AddItem(g, 26, 0, false)
	}
	v.AddItem(mid, 0, 1, false)

	if sl := buildTempSparkline(r, tempHistory); sl != nil {
		v.AddItem(sl, 8, 0, false)
	}
}

// passFailText renders the headline SMART verdict.
func passFailText(r *smart.Report) string {
	if r.SmartStatus.Passed {
		return "[green]PASSED[-]"
	}
	return "[red]FAILED[-]"
}

// buildIdentity is the key/value panel of drive identity and wear summary.
func buildIdentity(r *smart.Report) tview.Primitive {
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetBorder(true).SetTitle(" Drive ")

	var b strings.Builder
	row := func(k, v string) { fmt.Fprintf(&b, "  [::b]%-14s[-:-:-] %s\n", k, v) }

	row("Model", orDash(r.ModelName))
	if r.ModelFamily != "" {
		row("Family", r.ModelFamily)
	}
	row("Type", driveKind(r))
	row("Serial", orDash(r.SerialNumber))
	row("Firmware", orDash(r.FirmwareVersion))
	row("Capacity", capacityString(r))
	row("Temp", tempString(r))
	if r.PowerOnTime != nil {
		row("Power-on", humanDuration(r.PowerOnTime.Hours))
	} else {
		row("Power-on", dash)
	}
	if r.PowerCycleCount != nil {
		row("Power cycles", fmt.Sprintf("%d", *r.PowerCycleCount))
	}

	// Interface and geometry (mostly ATA; each gated on presence).
	if s := interfaceString(r.InterfaceSpeed); s != "" {
		row("Interface", s)
	}
	if r.LogicalBlockSize != nil {
		row("Sector size", sectorSizeString(r))
	}
	if r.FormFactor != nil && r.FormFactor.Name != "" {
		row("Form factor", r.FormFactor.Name)
	}
	if r.SATAVersion != nil && r.SATAVersion.String != "" {
		row("SATA", r.SATAVersion.String)
	}
	if r.Trim != nil {
		row("TRIM", yesNo(r.Trim.Supported))
	}

	if r.NVMeHealth != nil {
		h := r.NVMeHealth
		if h.PercentageUsed != nil {
			row("Life used", fmt.Sprintf("%d%%", *h.PercentageUsed))
		}
		row("Media errors", fmt.Sprintf("%d", h.MediaErrors))
		row("Unsafe shutdn", fmt.Sprintf("%d", h.UnsafeShutdowns))
	}
	tv.SetText(b.String())
	return tv
}

// interfaceString renders the SATA link speed, flagging a negotiated speed below
// the drive's maximum (a degraded link, often a cable issue) in yellow.
func interfaceString(is *smart.InterfaceSpeed) string {
	if is == nil || is.Current == nil || is.Current.String == "" {
		return ""
	}
	cur := is.Current.String
	if is.Max != nil && is.Max.String != "" && is.Max.String != cur {
		return fmt.Sprintf("%s  [yellow](max %s)[-]", cur, is.Max.String)
	}
	return cur
}

// sectorSizeString renders the logical (and physical, when different) block size.
func sectorSizeString(r *smart.Report) string {
	logical := *r.LogicalBlockSize
	physical := logical
	if r.PhysicalBlockSize != nil {
		physical = *r.PhysicalBlockSize
	}
	if physical != logical {
		return fmt.Sprintf("%d B logical / %d B physical", logical, physical)
	}
	return fmt.Sprintf("%d B", logical)
}

// yesNo renders a boolean as a word.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// buildGauges returns NVMe wear gauges, or nil for drives without a usable
// percentage indicator (e.g. ATA, where wear is vendor-specific).
func buildGauges(r *smart.Report) tview.Primitive {
	if r.NVMeHealth == nil {
		return nil
	}
	h := r.NVMeHealth
	col := tview.NewFlex().SetDirection(tview.FlexRow)
	added := false

	if h.PercentageUsed != nil {
		g := tvxwidgets.NewPercentageModeGauge()
		g.SetTitle(" Life used ")
		g.SetBorder(true)
		g.SetMaxValue(100)
		g.SetValue(clampPct(*h.PercentageUsed))
		col.AddItem(g, 3, 0, false)
		added = true
	}
	if h.AvailableSpare != nil {
		g := tvxwidgets.NewPercentageModeGauge()
		g.SetTitle(" Spare avail ")
		g.SetBorder(true)
		g.SetMaxValue(100)
		g.SetValue(clampPct(*h.AvailableSpare))
		col.AddItem(g, 3, 0, false)
		added = true
	}
	if !added {
		return nil
	}
	return col
}

// buildTempSparkline returns a temperature trend widget. ATA drives seed it from
// the on-device SCT history (instant); NVMe drives use the runtime series.
func buildTempSparkline(r *smart.Report, runtime []float64) tview.Primitive {
	data := temperatureSeries(r, runtime)
	if len(data) < 2 {
		return nil
	}
	sl := tvxwidgets.NewSparkline()
	sl.SetBorder(true)
	sl.SetTitle(" Temperature trend ")
	sl.SetDataTitle("°C")
	sl.SetData(data)
	sl.SetLineColor(severityColor(r.Overall()))
	return sl
}

// temperatureSeries picks the best available temperature history for the drive.
func temperatureSeries(r *smart.Report, runtime []float64) []float64 {
	if r.ATATemperatureHistory != nil && len(r.ATATemperatureHistory.Table) > 1 {
		out := make([]float64, 0, len(r.ATATemperatureHistory.Table))
		for _, v := range r.ATATemperatureHistory.Table {
			// SCT logs use a sentinel for "no reading"; skip implausible values.
			if v > -40 && v < 200 {
				out = append(out, float64(v))
			}
		}
		return out
	}
	return runtime
}

// clampPct bounds a percentage into the gauge's 0..100 range.
func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
