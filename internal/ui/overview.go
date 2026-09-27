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

// overviewView renders the Overview tab: identity panel (with the health
// verdict) beside protocol-specific gauges, plus a temperature sparkline.
type overviewView struct {
	*tview.Flex
	identity *scrollTextView

	// lastWidth -1 forces a relayout at the next Draw.
	rep       *smart.Report
	gauges    tview.Primitive
	chart     tview.Primitive
	lastWidth int
}

// newOverviewView builds the Overview tab; tempHistory is the runtime series
// used for NVMe drives.
func newOverviewView(r *smart.Report, tempHistory []float64) *overviewView {
	id := newScrollTextView()
	id.SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	titledBox(id.Box, " Drive ")
	v := &overviewView{
		Flex:      tview.NewFlex().SetDirection(tview.FlexRow),
		identity:  id,
		lastWidth: -1,
	}
	v.refresh(r, tempHistory)
	return v
}

// refresh rebuilds the contents for a new report; the identity panel's scroll
// offset is preserved across polls.
func (v *overviewView) refresh(r *smart.Report, tempHistory []float64) {
	v.rep = r
	v.gauges = buildGauges(r)
	v.chart = buildTempSparkline(r, tempHistory)
	v.lastWidth = -1
}

// relayout rebuilds the tab for a panel width of w: the identity box is sized
// to its content and the temperature chart takes what is left.
func (v *overviewView) relayout(w, h int) {
	text := hangingIndent(identityText(v.rep, w), identityWrap, w)
	v.identity.setTextKeepingScroll(text)

	v.Clear()
	mid := tview.NewFlex()
	mid.AddItem(v.identity, 0, 2, true)
	if v.gauges != nil {
		mid.AddItem(v.gauges, gaugeColumnWidth, 0, false)
	}

	if v.chart == nil {
		v.AddItem(mid, 0, 1, true)
		return
	}
	// Two borders around the text, and at least the gauges' own height.
	panelH := strings.Count(text, "\n") + 1 + 2
	if v.gauges != nil {
		panelH = max(panelH, gaugeColumnHeight)
	}
	// Leave the chart its minimum.
	if h > 0 && panelH > h-chartMinRows {
		panelH = max(h-chartMinRows, 1)
	}
	v.AddItem(mid, panelH, 0, true)
	v.AddItem(v.chart, 0, 1, false)
}

// gaugeColumnWidth is the NVMe wear gauges' column width; gaugeColumnHeight
// the room two of them need.
const (
	gaugeColumnWidth  = 26
	gaugeColumnHeight = 8
)

// chartMinRows is the least the chart may be squeezed to: border, two plot
// rows, axis, caption.
const chartMinRows = 7

// Draw reformats the identity panel when the width changed or a refresh invalidated it.
func (v *overviewView) Draw(screen tcell.Screen) {
	if _, _, w, h := v.GetInnerRect(); w != v.lastWidth && v.rep != nil {
		panelW := boxInner(w)
		if v.gauges != nil {
			panelW -= gaugeColumnWidth
		}
		v.relayout(panelW, h)
		v.lastWidth = w
	}
	v.Flex.Draw(screen)
}

// setFocused accents the identity panel's border.
func (v *overviewView) setFocused(focused bool) {
	v.identity.SetBorderColor(borderColor(focused))
}

// verdictWord renders the drive-level health as one plain word.
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

// identitySection is a named group of fields, rendered under a heading.
type identitySection struct {
	title  string
	fields []identityField
}

// identityText renders the identity/wear panel for cols cells, packing two columns when there is room.
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

// identityColumnWidth is the width one key/value column needs; below two of
// these the panel runs a single column.
const identityColumnWidth = 40

// identityValueCol is the column values start in: a 14-cell key plus a space.
const identityValueCol = 15

// identityWrap stops at nine cells: WordWrap hard-splits an unbreakable IOService path and the panel is sized from its line count.
var identityWrap = hangingWrap{valueCol: identityValueCol, minValueW: 9}

// writeFields lays out fields in as many columns as fit; a value too long for
// a column takes a full row of its own after the paired ones.
func writeFields(b *strings.Builder, fields []identityField, cols int) {
	line := func(f identityField, width int) {
		fmt.Fprintf(b, "[::b]%-*s[-:-:-] %-*s", identityValueCol-1, f.k, width, f.v)
	}
	if cols < 2*identityColumnWidth {
		for _, f := range fields {
			line(f, 0)
			b.WriteByte('\n')
		}
		return
	}

	valueWidth := identityColumnWidth - identityValueCol
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

// writeVerdict renders the health verdict plus the evidence behind it.
func writeVerdict(b *strings.Builder, r *smart.Report) {
	fmt.Fprintf(b, "%s  %s%s[-]\n",
		reportVerdict(r), mutedTag(), verdictEvidence(r))

	// The raw SMART pass/fail only adds signal on a failure.
	if r.SmartStatus != nil && !r.SmartStatus.Passed {
		fmt.Fprintf(b, "%sSMART self-assessment: FAILED[-]\n", failingTag())
	}
	// A smartctl message is a data-availability caveat, not a verdict.
	if msg, ok := r.FatalMessage(); ok {
		fmt.Fprintf(b, "%s⚠ %s[-]\n", cautionTag(), esc(msg))
	}
}

// verdictEvidence summarises what the verdict was derived from.
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
			parts = append(parts, fmt.Sprintf("%d of %d attributes need attention",
				bad, len(r.ATAAttributes.Table)))
		}
	}
	if e := r.ErrorCounts(); e.ErrorLogEntries != nil {
		switch n := int(*e.ErrorLogEntries); {
		case n == 0:
			parts = append(parts, "error log empty")
		case r.IsNVMe():
			// NVMe entries accumulate benignly, so state a count rather than a fault.
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

// identitySections groups the panel's fields.
func identitySections(r *smart.Report) []identitySection {
	id := identitySection{title: "Identity"}
	add := func(sec *identitySection, k, v string) {
		sec.fields = append(sec.fields, identityField{k, v})
	}
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
	// The untrimmed device name goes last: an IOService path wraps to several lines.
	add(&id, "Device", esc(r.Device.Name))

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
		// The gauges show the standard fields; only fallback sources need a row.
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

// wwnString renders a WWN in smartctl's "LU WWN Device Id" form.
func wwnString(w *smart.WWN) string {
	return fmt.Sprintf("%x %06x %09x", w.NAA, w.OUI, w.ID)
}

// interfaceString renders the SATA link speed, flagging a negotiated speed
// below the maximum (a degraded link, often cabling).
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

// buildGauges returns NVMe wear gauges, nil without a percentage indicator.
func buildGauges(r *smart.Report) tview.Primitive {
	if r.NVMeHealth == nil {
		return nil
	}
	h := r.NVMeHealth
	col := tview.NewFlex().SetDirection(tview.FlexRow)
	// shown and graded differ: "90% left" is coloured by the 10% consumed.
	addGauge := func(title string, shown int, graded smart.Severity) {
		g := tvxwidgets.NewPercentageModeGauge()
		g.SetTitle(title)
		g.SetBorder(true)
		g.SetMaxValue(100)
		g.SetValue(clampPct(shown))
		g.SetPgBgColor(severityColor(graded))
		col.AddItem(g, 3, 0, false)
	}

	if h.PercentageUsed != nil {
		// Remaining endurance, so it fills toward healthy like the fleet bar.
		addGauge(" Life left ", 100-clampPct(*h.PercentageUsed), lifeUsedSeverity(*h.PercentageUsed))
	}
	if h.AvailableSpare != nil {
		pct, thr, _ := r.SparePercent()
		addGauge(" Spare avail ", pct, spareSeverityPct(pct, thr))
	}
	if col.GetItemCount() == 0 {
		return nil
	}
	return col
}

// lifeUsedSeverity grades the "Life used" gauge; the data layer's
// PctUsedSeverity never returns failing, so the >=100 red is added here.
func lifeUsedSeverity(pct int) smart.Severity {
	if pct >= 100 {
		return smart.SeverityFailing
	}
	return smart.PctUsedSeverity(pct)
}

// spareSeverityPct grades spare against the drive's depletion threshold; it
// takes the pair SparePercent resolved, since NVMeHealth may be nil when spare is reported.
func spareSeverityPct(pct, threshold int) smart.Severity {
	switch {
	case pct <= threshold:
		return smart.SeverityFailing
	case pct <= threshold+10:
		return smart.SeverityCaution
	default:
		return smart.SeverityOK
	}
}

// buildTempSparkline returns a temperature trend widget: ATA seeds from the
// SCT history, NVMe from the runtime series.
func buildTempSparkline(r *smart.Report, runtime []float64) tview.Primitive {
	data := temperatureSeries(r, runtime)
	if len(data) < 2 {
		return nil
	}
	now := int(data[len(data)-1])
	lo, hi, _ := dataRange(data)

	// Graded on the current temperature, and only once it leaves the band.
	color := activeTheme.BarHealthy
	if sev := tempSeverity(now); sev != smart.SeverityOK {
		color = severityColor(sev)
	}

	c := newRangeChart().
		setSeries(data, fmt.Sprintf("%d samples · oldest left, now right", len(data))).
		setColor(color)
	c.SetBorder(true)
	c.SetTitle(fmt.Sprintf(" Temperature — now %d°C · range %.0f–%.0f°C ", now, lo, hi))
	return c
}

// temperatureSeries picks the best available temperature history for the drive.
func temperatureSeries(r *smart.Report, runtime []float64) []float64 {
	if r.ATATemperatureHistory != nil && len(r.ATATemperatureHistory.Table) > 1 {
		out := make([]float64, 0, len(r.ATATemperatureHistory.Table))
		for _, v := range r.ATATemperatureHistory.Table {
			// An empty slot is null; skip it and any implausible value.
			if v != nil && *v > -40 && *v < 200 {
				out = append(out, float64(*v))
			}
		}
		return out
	}
	return runtime
}
