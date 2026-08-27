// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

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

	// Latest report and the width the panel was last formatted for. The panel
	// runs its fields in one or two columns depending on how much room it has,
	// and the true width is only known at draw time — the same lazy relayout
	// farm.go uses for its stat boxes. -1 forces a rebuild.
	rep       *smart.Report
	gauges    tview.Primitive
	chart     tview.Primitive
	lastWidth int
}

// newOverviewView builds the Overview tab. tempHistory is the runtime-accumulated
// series used for NVMe drives, which lack an on-device temperature log.
func newOverviewView(r *smart.Report, tempHistory []float64) *overviewView {
	id := newScrollTextView()
	id.SetDynamicColors(true).SetScrollable(true)
	id.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(" Drive ")
	v := &overviewView{
		Flex:      tview.NewFlex().SetDirection(tview.FlexRow),
		identity:  id,
		lastWidth: -1,
	}
	v.refresh(r, tempHistory)
	return v
}

// refresh rebuilds the panel contents for a new report. The identity panel is
// the one element that can outgrow its slot (a rich ATA drive lists many rows),
// so it is a focusable, scrollable TextView with off-screen arrows; the gauges
// and sparkline are small and fixed. Its scroll offset is preserved across polls.
func (v *overviewView) refresh(r *smart.Report, tempHistory []float64) {
	v.rep = r
	v.gauges = buildGauges(r)
	v.chart = buildTempSparkline(r, tempHistory)
	v.lastWidth = -1 // data changed: reformat and relayout at the current width
}

// relayout rebuilds the tab for a panel width of w. The identity box is sized to
// its content and the temperature chart takes what is left, rather than the
// panel stretching to full height with its text pinned to the top: on a 120x40
// terminal the old layout used 22 of 36 rows and left fourteen blank above a
// fixed ten-row chart.
func (v *overviewView) relayout(w, h int) {
	row, col := v.identity.GetScrollOffset()
	text := identityText(v.rep, w)
	v.identity.SetText(text)
	v.identity.ScrollTo(row, col)

	v.Clear()
	mid := tview.NewFlex() // horizontal: identity | gauges
	mid.AddItem(v.identity, 0, 2, true)
	if v.gauges != nil {
		mid.AddItem(v.gauges, gaugeColumnWidth, 0, false)
	}

	if v.chart == nil {
		v.AddItem(mid, 0, 1, true)
		return
	}
	// Two borders around the text, and at least the gauges' own height so the
	// box never clips them.
	panelH := strings.Count(text, "\n") + 1 + 2
	if v.gauges != nil {
		panelH = max(panelH, gaugeColumnHeight)
	}
	// Leave the chart its minimum; below that the panel keeps the space and the
	// chart scrolls into view with the rest of the tab.
	if h > 0 && panelH > h-chartMinRows {
		panelH = max(h-chartMinRows, 1)
	}
	v.AddItem(mid, panelH, 0, true)
	v.AddItem(v.chart, 0, 1, false)
}

// gaugeColumnWidth is the width of the NVMe wear gauges beside the panel, and
// gaugeColumnHeight the room two of them need.
const (
	gaugeColumnWidth  = 26
	gaugeColumnHeight = 8
)

// chartMinRows is the least the temperature chart may be squeezed to: a border,
// two plot rows, the axis and its caption.
const chartMinRows = 7

// Draw reformats the identity panel when the available width changed (or a
// refresh invalidated it), then defers to the Flex. Column count depends on the
// live width, which is only known here.
func (v *overviewView) Draw(screen tcell.Screen) {
	if _, _, w, h := v.GetInnerRect(); w != v.lastWidth && v.rep != nil {
		// The panel's own width, once the gauges have taken their column.
		panelW := w - 2 - 2*uiGutter
		if v.gauges != nil {
			panelW -= gaugeColumnWidth
		}
		v.relayout(panelW, h)
		v.lastWidth = w
	}
	v.Flex.Draw(screen)
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

// identityField is one labelled value in the drive panel.
type identityField struct{ k, v string }

// identitySection is a named group of fields; the name is rendered as a heading
// so a two-column layout still reads as groups rather than a jumble.
type identitySection struct {
	title  string
	fields []identityField
}

// identityText renders the drive identity and wear panel for a panel width of
// cols cells. Every field is gated on presence, so a sparse drive (an Apple
// internal NVMe with no geometry or logs) still degrades gracefully.
//
// The panel used to run one field per row down its full width, which left a rich
// ATA drive using 22 of 36 rows and a wide terminal mostly empty. It now packs
// into two columns when there is room for them.
func identityText(r *smart.Report, cols int) string {
	var b strings.Builder
	writeVerdict(&b, r)
	for _, sec := range identitySections(r) {
		if len(sec.fields) == 0 {
			continue
		}
		b.WriteByte('\n')
		fmt.Fprintf(&b, "%s%s[-]\n", accentTag(), sec.title)
		writeFields(&b, sec.fields, cols)
	}
	return b.String()
}

// identityColumnWidth is the width one key/value column needs: a 14-cell key
// plus room for a value. Below two of these the panel runs a single column.
const identityColumnWidth = 40

// writeFields lays out one section's fields in as many columns as fit. A field
// whose value is too long for a column (a sector-size line runs to 45 cells)
// takes a full row of its own after the paired ones, rather than overrunning
// its neighbour.
func writeFields(b *strings.Builder, fields []identityField, cols int) {
	line := func(f identityField, width int) {
		if width <= 0 {
			fmt.Fprintf(b, "[::b]%-14s[-:-:-] %s", f.k, f.v)
			return
		}
		fmt.Fprintf(b, "[::b]%-14s[-:-:-] %-*s", f.k, width, f.v)
	}
	if cols < 2*identityColumnWidth {
		for _, f := range fields {
			line(f, 0)
			b.WriteByte('\n')
		}
		return
	}

	valueWidth := identityColumnWidth - 15
	var narrow, wide []identityField
	for _, f := range fields {
		if tview.TaggedStringWidth(f.v) > valueWidth {
			wide = append(wide, f)
			continue
		}
		narrow = append(narrow, f)
	}
	rows := (len(narrow) + 1) / 2
	for row := range rows {
		for col := range 2 {
			i := col*rows + row
			if i >= len(narrow) {
				break
			}
			line(narrow[i], valueWidth)
		}
		b.WriteByte('\n')
	}
	for _, f := range wide {
		line(f, 0)
		b.WriteByte('\n')
	}
}

// writeVerdict renders the drive-level health as a word plus the evidence behind
// it. The word alone said nothing about why, and — before Overall() learned to
// read the error logs — could disagree with the Logs tab outright.
func writeVerdict(b *strings.Builder, r *smart.Report) {
	sev := r.Overall()
	fmt.Fprintf(b, "[%s::b]%s[-:-:-]  %s%s[-]\n",
		severityTag(sev), verdictWord(sev), mutedTag(), verdictEvidence(r))

	// Keep the raw SMART pass/fail only when it adds signal — i.e. on a failure.
	if !r.SmartStatus.Passed {
		fmt.Fprintf(b, "%sSMART self-assessment: FAILED[-]\n", failingTag())
	}
	// A per-drive smartctl message (a permission or open failure; known-benign
	// ones like the Apple-NVMe log limitation are filtered by FatalMessage) is a
	// data-availability caveat, not a health verdict, so it reads as a notice.
	if msg, ok := r.FatalMessage(); ok {
		fmt.Fprintf(b, "%s⚠ %s[-]\n", cautionTag(), esc(msg))
	}
}

// verdictEvidence summarises what the verdict was derived from, so the Overview
// answers "why" without a trip to the Attributes and Logs tabs.
func verdictEvidence(r *smart.Report) string {
	var parts []string
	if r.ATAAttributes != nil {
		bad := 0
		for i := range r.ATAAttributes.Table {
			if r.ATAAttributes.Table[i].Severity() != smart.SeverityOK {
				bad++
			}
		}
		if bad == 0 {
			parts = append(parts, fmt.Sprintf("%d attributes in range", len(r.ATAAttributes.Table)))
		} else {
			parts = append(parts, fmt.Sprintf("%s of %d attributes need attention",
				plural(bad, "attribute", ""), len(r.ATAAttributes.Table)))
		}
	}
	if e := r.ErrorCounts(); e.ErrorLogEntries != nil {
		switch n := int(*e.ErrorLogEntries); {
		case n == 0:
			parts = append(parts, "error log empty")
		case r.IsNVMe():
			// NVMe entries accumulate for benign reasons and do not grade the
			// drive (see logSeverity), so the phrasing states a count rather than
			// reading as a fault beside a Healthy verdict.
			parts = append(parts, fmt.Sprintf("%d error-log entries", n))
		default:
			parts = append(parts, fmt.Sprintf("%s logged", plural(n, "error", "errors")))
		}
	}
	if r.ATAPendingDefects != nil && r.ATAPendingDefects.Count > 0 {
		parts = append(parts, fmt.Sprintf("%d pending sectors", r.ATAPendingDefects.Count))
	}
	return strings.Join(parts, " · ")
}

// identitySections groups the panel's fields. Grouping is what lets the panel
// run two columns without reading as a jumble.
func identitySections(r *smart.Report) []identitySection {
	// Free-text fields are drive-controlled, so escape markup (see esc).
	id := identitySection{title: "Identity"}
	add := func(sec *identitySection, k, v string) {
		sec.fields = append(sec.fields, identityField{k, v})
	}
	// The full device name, verbatim: every other surface trims it for display
	// (see shortDevice), so this is where the untrimmed name lives.
	add(&id, "Device", esc(r.Device.Name))
	add(&id, "Model", orDash(esc(r.ModelName)))
	if r.ModelFamily != "" {
		add(&id, "Family", esc(r.ModelFamily))
	}
	add(&id, "Type", driveKind(r))
	add(&id, "Serial", orDash(esc(r.SerialNumber)))
	add(&id, "Firmware", orDash(esc(r.FirmwareVersion)))
	if r.WWN != nil {
		add(&id, "WWN", wwnString(r.WWN))
	}
	if r.NVMeVersion != nil && r.NVMeVersion.String != "" {
		add(&id, "NVMe ver", esc(r.NVMeVersion.String))
	}
	if r.NVMeNumberOfNamespaces != nil {
		add(&id, "Namespaces", fmt.Sprintf("%d", *r.NVMeNumberOfNamespaces))
	}
	if r.NVMeControllerID != nil {
		add(&id, "Controller", fmt.Sprintf("%d", *r.NVMeControllerID))
	}
	if r.NVMePCIVendor != nil {
		add(&id, "PCI vendor", fmt.Sprintf("0x%04x", r.NVMePCIVendor.ID))
	}

	geom := identitySection{title: "Capacity & geometry"}
	add(&geom, "Capacity", capacityString(r))
	if r.LogicalBlockSize != nil {
		add(&geom, "Sector size", sectorSizeString(r))
	}
	if r.FormFactor != nil && r.FormFactor.Name != "" {
		add(&geom, "Form factor", esc(r.FormFactor.Name))
	}
	if s := interfaceString(r.InterfaceSpeed); s != "" {
		add(&geom, "Interface", s)
	}
	if r.SATAVersion != nil && r.SATAVersion.String != "" {
		add(&geom, "SATA", esc(r.SATAVersion.String))
	}
	if r.Trim != nil {
		add(&geom, "TRIM", yesNo(r.Trim.Supported))
	}

	wear := identitySection{title: "Wear & usage"}
	add(&wear, "Temp", tempCell(r))
	if hours, ok := r.PowerOnHours(); ok {
		add(&wear, "Power-on", humanDuration(hours))
	} else {
		add(&wear, "Power-on", dash)
	}
	if n, ok := r.PowerCycles(); ok {
		add(&wear, "Power cycles", fmt.Sprintf("%d", n))
	}
	if h := r.NVMeHealth; h != nil {
		// Life used and spare are shown by the gauges beside this panel when the
		// drive reports the standard fields, so only the fallback sources need a
		// row of their own — printing both was the panel stating what the widget
		// next to it already showed.
		if h.PercentageUsed == nil {
			if pct, ok := r.LifeUsedPercent(); ok {
				add(&wear, "Life used", fmt.Sprintf("%d%%", pct))
			}
		}
		if h.AvailableSpare == nil {
			if pct, _, ok := r.SparePercent(); ok {
				add(&wear, "Spare avail", fmt.Sprintf("%d%%", pct))
			}
		}
		add(&wear, "Media errors", fmt.Sprintf("%d", h.MediaErrors))
		add(&wear, "Unsafe shutdn", fmt.Sprintf("%d", h.UnsafeShutdowns))
	}
	return []identitySection{id, geom, wear}
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
//
// It uses our own rangeChart rather than tvxwidgets.Sparkline: that widget
// scales against zero, so this drive's real 35-40°C history drew as a solid
// block of █ carrying no information at all (see chart.go).
func buildTempSparkline(r *smart.Report, runtime []float64) tview.Primitive {
	data := temperatureSeries(r, runtime)
	if len(data) < 2 {
		return nil
	}
	now := int(data[len(data)-1])
	lo, hi, _ := dataRange(data)

	c := newRangeChart().
		setSeries(data, "°C", fmt.Sprintf("%d samples · oldest left, now right", len(data))).
		// Colour by the current temperature rather than the drive-wide verdict:
		// a hot-but-otherwise-OK drive should not show a calm green line.
		setColor(severityColor(tempSeverity(now)))
	c.SetBorder(true)
	c.SetBorderPadding(0, 0, uiGutter, uiGutter)
	c.SetTitle(fmt.Sprintf(" Temperature — now %d°C · range %.0f–%.0f°C ", now, lo, hi))
	return c
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
