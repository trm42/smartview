// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"smartview/internal/smart"
)

// buildAttributes renders the Attributes tab: the ATA SMART attribute table for
// ATA drives, or the NVMe health-log key/value table for NVMe drives.
func buildAttributes(r *smart.Report) tview.Primitive {
	if r.IsNVMe() && r.NVMeHealth != nil {
		return buildNVMeHealthTable(r.NVMeHealth)
	}
	if r.ATAAttributes != nil {
		return buildATATable(r.ATAAttributes.Table)
	}
	return centeredNote("No SMART attributes reported by this drive.")
}

// buildATATable renders the classic vendor attribute table, colouring each row
// by its computed severity.
func buildATATable(attrs []smart.ATAAttribute) tview.Primitive {
	t := tview.NewTable().SetBorders(false).SetFixed(1, 0)
	t.SetBorder(true).SetTitle(" SMART attributes ")

	headers := []string{"ID", "Attribute", "Flag", "Value", "Worst", "Thresh", "When failed", "Raw"}
	for c, h := range headers {
		t.SetCell(0, c, headerCell(h))
	}
	for i, a := range attrs {
		sev := a.Severity()
		color := severityColor(sev)
		kind := "old-age"
		if a.Flags.Prefailure {
			kind = "pre-fail"
		}
		whenFailed := a.WhenFailed
		if whenFailed == "" {
			whenFailed = "-"
		}
		cells := []string{
			fmt.Sprintf("%d", a.ID),
			a.Name,
			kind,
			fmt.Sprintf("%d", a.Value),
			fmt.Sprintf("%d", a.Worst),
			fmt.Sprintf("%d", a.Thresh),
			whenFailed,
			a.Raw.String,
		}
		for c, v := range cells {
			cell := tview.NewTableCell(" " + v + " ").SetTextColor(color)
			if c == 1 {
				cell.SetExpansion(1)
			}
			if c >= 3 && c <= 5 {
				cell.SetAlign(tview.AlignRight)
			}
			t.SetCell(i+1, c, cell)
		}
	}
	t.SetSelectable(true, false)
	t.Select(1, 0)
	return t
}

// buildNVMeHealthTable renders the NVMe SMART/health log as a key/value table.
func buildNVMeHealthTable(h *smart.NVMeHealth) tview.Primitive {
	t := tview.NewTable().SetBorders(false).SetFixed(1, 0)
	t.SetBorder(true).SetTitle(" NVMe health log ")
	t.SetCell(0, 0, headerCell("Field"))
	t.SetCell(0, 1, headerCell("Value"))

	type kv struct {
		k   string
		v   string
		sev smart.Severity
	}
	var rows []kv
	add := func(k, v string, sev smart.Severity) { rows = append(rows, kv{k, v, sev}) }

	warnSev := smart.SeverityOK
	if h.CriticalWarning != 0 {
		warnSev = smart.SeverityFailing
	}
	add("Critical warning", fmt.Sprintf("0x%02x", h.CriticalWarning), warnSev)
	if h.PercentageUsed != nil {
		add("Percentage used", fmt.Sprintf("%d%%", *h.PercentageUsed), pctUsedSev(*h.PercentageUsed))
	}
	if h.AvailableSpare != nil {
		sev := smart.SeverityOK
		if h.AvailableSpareThreshold != nil && *h.AvailableSpare <= *h.AvailableSpareThreshold {
			sev = smart.SeverityFailing
		}
		add("Available spare", fmt.Sprintf("%d%%", *h.AvailableSpare), sev)
	}
	if h.AvailableSpareThreshold != nil {
		add("Spare threshold", fmt.Sprintf("%d%%", *h.AvailableSpareThreshold), smart.SeverityOK)
	}
	mediaSev := smart.SeverityOK
	if h.MediaErrors > 0 {
		mediaSev = smart.SeverityCaution
	}
	add("Media errors", fmt.Sprintf("%d", h.MediaErrors), mediaSev)
	add("Error log entries", fmt.Sprintf("%d", h.NumErrLogEntries), smart.SeverityOK)
	add("Power-on hours", fmt.Sprintf("%d", h.PowerOnHours), smart.SeverityOK)
	add("Power cycles", fmt.Sprintf("%d", h.PowerCycles), smart.SeverityOK)
	add("Unsafe shutdowns", fmt.Sprintf("%d", h.UnsafeShutdowns), smart.SeverityOK)
	add("Data read", humanBytes(h.DataUnitsRead*512*1000), smart.SeverityOK)
	add("Data written", humanBytes(h.DataUnitsWritten*512*1000), smart.SeverityOK)
	if len(h.TemperatureSensors) > 0 {
		s := ""
		for i, t := range h.TemperatureSensors {
			if i > 0 {
				s += ", "
			}
			s += fmt.Sprintf("%d°C", t)
		}
		add("Sensors", s, smart.SeverityOK)
	}

	for i, r := range rows {
		t.SetCell(i+1, 0, tview.NewTableCell(" "+r.k+" ").SetTextColor(tcell.ColorWhite))
		t.SetCell(i+1, 1, tview.NewTableCell(" "+r.v+" ").SetTextColor(severityColor(r.sev)))
	}
	t.SetSelectable(true, false)
	t.Select(1, 0)
	return t
}

// pctUsedSev grades NVMe endurance consumption.
func pctUsedSev(v int) smart.Severity {
	switch {
	case v >= 100:
		return smart.SeverityCaution
	case v >= 90:
		return smart.SeverityCaution
	default:
		return smart.SeverityOK
	}
}

// headerCell builds a non-selectable bold header cell.
func headerCell(s string) *tview.TableCell {
	return tview.NewTableCell(" " + s + " ").
		SetTextColor(tcell.ColorAqua).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false)
}

// centeredNote is a placeholder primitive for empty/unsupported sections.
func centeredNote(msg string) tview.Primitive {
	tv := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetText("\n" + msg)
	tv.SetBorder(true)
	return tv
}
