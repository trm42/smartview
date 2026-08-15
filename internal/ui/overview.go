// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// overviewView renders the Overview tab: an identity panel (which carries the
// SMART health verdict in its top row) beside protocol-specific gauges, and a
// temperature sparkline. It refreshes in place.
type overviewView struct {
	*tview.Flex
	identity *scrollTextView // the drive panel; scrolls (with arrows) when tall
}

// newOverviewView builds the Overview tab. tempHistory is the runtime-accumulated
// series used for NVMe drives, which lack an on-device temperature log.
func newOverviewView(r *smart.Report, tempHistory []float64) *overviewView {
	id := newScrollTextView()
	id.SetDynamicColors(true).SetScrollable(true)
	id.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(" Drive ")
	v := &overviewView{
		Flex:     tview.NewFlex().SetDirection(tview.FlexRow),
		identity: id,
	}
	v.refresh(r, tempHistory)
	return v
}

// refresh rebuilds the panel contents for a new report. The identity panel is
// the one element that can outgrow its slot (a rich ATA drive lists many rows),
// so it is a focusable, scrollable TextView with off-screen arrows; the gauges
// and sparkline are small and fixed. Its scroll offset is preserved across polls.
func (v *overviewView) refresh(r *smart.Report, tempHistory []float64) {
	row, col := v.identity.GetScrollOffset()
	v.identity.SetText(identityText(r))
	v.identity.ScrollTo(row, col)

	v.Clear()
	mid := tview.NewFlex() // horizontal: identity | gauges
	mid.AddItem(v.identity, 0, 2, true)
	if g := buildGauges(r); g != nil {
		mid.AddItem(g, 26, 0, false)
	}
	v.AddItem(mid, 0, 1, true)

	if sl := buildTempSparkline(r, tempHistory); sl != nil {
		v.AddItem(sl, 8, 0, false)
	}
}

// setFocused accents the identity panel's border when the Overview tab holds
// focus (it is the tab's one focusable, scrollable element).
func (v *overviewView) setFocused(focused bool) {
	v.identity.SetBorderColor(borderColor(focused))
}

// verdictWord renders the drive-level health as a single plain verdict, replacing
// the cryptic "OK self-assessment PASSED" doubling with one clear word.
func verdictWord(s smart.Severity) string {
	switch s {
	case smart.SeverityFailing:
		return "Failing"
	case smart.SeverityCaution:
		return "Caution"
	default:
		return "Healthy"
	}
}

// identityText renders the key/value body of the drive identity and wear panel,
// grouped into Identity · Capacity & geometry · Wear & usage blocks separated by
// a blank line. Every field is gated on presence, so a sparse drive (e.g. an
// Apple internal NVMe with no geometry/logs) still degrades gracefully.
func identityText(r *smart.Report) string {
	var b strings.Builder
	row := func(k, v string) { fmt.Fprintf(&b, "[::b]%-14s[-:-:-] %s\n", k, v) }
	gap := func() { b.WriteByte('\n') }

	sev := r.Overall()
	health := fmt.Sprintf("[%s::b]%s[-:-:-]", severityTag(sev), verdictWord(sev))
	// Keep the raw SMART pass/fail only when it adds signal — i.e. on a failure.
	if !r.SmartStatus.Passed {
		health += "  " + failingTag() + "(SMART self-test: FAILED)[-]"
	}
	row("Health", health)

	// Surface a per-drive smartctl error message (a permission/open failure;
	// known-benign messages like the Apple-NVMe log-read limitation are already
	// filtered out by FatalMessage). It is a data-availability caveat, not a
	// health verdict, so it is styled as a yellow notice rather than an alarming
	// red one. The message is long, so it gets its own full-width line rather
	// than the key/value row format.
	if msg, ok := r.FatalMessage(); ok {
		fmt.Fprintf(&b, "%s⚠ %s[-]\n", cautionTag(), esc(msg))
	}

	// Identity. Free-text fields are drive-controlled, so escape markup (see esc).
	gap()
	row("Model", orDash(esc(r.ModelName)))
	if r.ModelFamily != "" {
		row("Family", esc(r.ModelFamily))
	}
	row("Type", driveKind(r))
	row("Serial", orDash(esc(r.SerialNumber)))
	row("Firmware", orDash(esc(r.FirmwareVersion)))
	if r.WWN != nil {
		row("WWN", wwnString(r.WWN))
	}
	if r.NVMeVersion != nil && r.NVMeVersion.String != "" {
		row("NVMe ver", esc(r.NVMeVersion.String))
	}
	if r.NVMeNumberOfNamespaces != nil {
		row("Namespaces", fmt.Sprintf("%d", *r.NVMeNumberOfNamespaces))
	}
	if r.NVMeControllerID != nil {
		row("Controller", fmt.Sprintf("%d", *r.NVMeControllerID))
	}
	if r.NVMePCIVendor != nil {
		row("PCI vendor", fmt.Sprintf("0x%04x", r.NVMePCIVendor.ID))
	}

	// Capacity & geometry (mostly ATA; each gated on presence).
	gap()
	row("Capacity", capacityString(r))
	if r.LogicalBlockSize != nil {
		row("Sector size", sectorSizeString(r))
	}
	if r.FormFactor != nil && r.FormFactor.Name != "" {
		row("Form factor", esc(r.FormFactor.Name))
	}
	if s := interfaceString(r.InterfaceSpeed); s != "" {
		row("Interface", s)
	}
	if r.SATAVersion != nil && r.SATAVersion.String != "" {
		row("SATA", esc(r.SATAVersion.String))
	}
	if r.Trim != nil {
		row("TRIM", yesNo(r.Trim.Supported))
	}

	// Wear & usage.
	gap()
	row("Temp", tempString(r))
	if hours, ok := r.PowerOnHours(); ok {
		row("Power-on", humanDuration(hours))
	} else {
		row("Power-on", dash)
	}
	if n, ok := r.PowerCycles(); ok {
		row("Power cycles", fmt.Sprintf("%d", n))
	}
	if r.NVMeHealth != nil {
		h := r.NVMeHealth
		// LifeUsedPercent resolves the standard percentage_used field and the
		// endurance_used fallback that Apple internal SSDs report instead.
		if pct, ok := r.LifeUsedPercent(); ok {
			row("Life used", fmt.Sprintf("%d%%", pct))
		}
		// The gauge already carries spare when the standard field is present, so
		// only the fallback source needs a row of its own.
		if h.AvailableSpare == nil {
			if pct, _, ok := r.SparePercent(); ok {
				row("Spare avail", fmt.Sprintf("%d%%", pct))
			}
		}
		row("Media errors", fmt.Sprintf("%d", h.MediaErrors))
		row("Unsafe shutdn", fmt.Sprintf("%d", h.UnsafeShutdowns))
	}
	return b.String()
}

// wwnString renders a World Wide Name in smartctl's "LU WWN Device Id" form:
// the NAA nibble, the 24-bit OUI, and the 36-bit vendor id, each in hex.
func wwnString(w *smart.WWN) string {
	return fmt.Sprintf("%x %06x %09x", w.NAA, w.OUI, w.ID)
}

// interfaceString renders the SATA link speed, flagging a negotiated speed below
// the drive's maximum (a degraded link, often a cable issue) in yellow.
func interfaceString(is *smart.InterfaceSpeed) string {
	if is == nil || is.Current == nil || is.Current.String == "" {
		return ""
	}
	cur := is.Current.String
	if is.Max != nil && is.Max.String != "" && is.Max.String != cur {
		return fmt.Sprintf("%s  %s(max %s)[-]", esc(cur), cautionTag(), esc(is.Max.String))
	}
	return esc(cur)
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
		// Colour by the value itself, not the drive-wide verdict: a near-worn
		// drive should read caution/red even when nothing else is wrong.
		g.SetPgBgColor(severityColor(lifeUsedSeverity(*h.PercentageUsed)))
		col.AddItem(g, 3, 0, false)
		added = true
	}
	if h.AvailableSpare != nil {
		g := tvxwidgets.NewPercentageModeGauge()
		g.SetTitle(" Spare avail ")
		g.SetBorder(true)
		g.SetMaxValue(100)
		g.SetValue(clampPct(*h.AvailableSpare))
		g.SetPgBgColor(severityColor(spareSeverity(h)))
		col.AddItem(g, 3, 0, false)
		added = true
	}
	if !added {
		return nil
	}
	return col
}

// lifeUsedSeverity grades NVMe endurance consumption for the "Life used" gauge:
// caution as it nears the rated writes, red once past 100%. The data layer's
// PctUsedSeverity never returns failing, so the >=100 red is added here.
func lifeUsedSeverity(pct int) smart.Severity {
	if pct >= 100 {
		return smart.SeverityFailing
	}
	return smart.PctUsedSeverity(pct)
}

// spareSeverity grades the "Spare avail" gauge: failing once available spare has
// fallen to the drive's threshold, caution as it approaches it.
func spareSeverity(h *smart.NVMeHealth) smart.Severity {
	if h.AvailableSpare == nil || h.AvailableSpareThreshold == nil {
		return smart.SeverityOK
	}
	switch thr := *h.AvailableSpareThreshold; {
	case *h.AvailableSpare <= thr:
		return smart.SeverityFailing
	case *h.AvailableSpare <= thr+10:
		return smart.SeverityCaution
	default:
		return smart.SeverityOK
	}
}

// buildTempSparkline returns a temperature trend widget. ATA drives seed it from
// the on-device SCT history (instant); NVMe drives use the runtime series.
func buildTempSparkline(r *smart.Report, runtime []float64) tview.Primitive {
	data := temperatureSeries(r, runtime)
	if len(data) < 2 {
		return nil
	}
	now := int(data[len(data)-1])
	lo, hi := now, now
	for _, v := range data {
		iv := int(v)
		if iv < lo {
			lo = iv
		}
		if iv > hi {
			hi = iv
		}
	}

	sl := tvxwidgets.NewSparkline()
	sl.SetBorder(true)
	// Title with live values so the trend is readable, and colour by the current
	// temperature rather than the drive-wide verdict — a hot-but-otherwise-OK
	// drive should not show a calm green line.
	sl.SetTitle(fmt.Sprintf(" Temperature trend — now %d°C · min %d · max %d ", now, lo, hi))
	sl.SetDataTitle("°C")
	sl.SetData(data)
	sl.SetLineColor(severityColor(tempSeverity(now)))
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
