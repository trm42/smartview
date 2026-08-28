// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestLeadingInt lives in internal/smart alongside the function it covers.

func TestDecodeReading(t *testing.T) {
	mk := func(id int, raw string) smart.ATAAttribute {
		return smart.ATAAttribute{ID: id, Raw: smart.ATARaw{String: raw}}
	}
	cases := []struct {
		a    smart.ATAAttribute
		want string
	}{
		{mk(241, "30003645387"), "15.4 TB"}, // LBAs * 512
		{mk(9, "9438"), "1 y 28 d"},         // power-on hours
		{mk(240, "9201 (189 58 0)"), "1 y 18 d"},
		{mk(194, "37 (0 21 0 0 0)"), "37°C"},
		{mk(4, "15"), "15"},     // undecoded -> raw passthrough
		{mk(5, ""), "—"},        // empty -> dash
		{mk(200, "0"), "0"},     // unknown id -> raw passthrough
		{mk(194, "n/a"), "n/a"}, // temp id but non-numeric raw -> raw passthrough
	}
	for _, c := range cases {
		if got := decodeReading(c.a); got != c.want {
			t.Errorf("decodeReading(id %d, %q) = %q, want %q", c.a.ID, c.a.Raw.String, got, c.want)
		}
	}
}

func TestHumanAttrName(t *testing.T) {
	cases := map[string]string{
		"Raw_Read_Error_Rate": "Raw Read Error Rate",
		"":                    "",
		"no_under":            "no under",
	}
	for in, want := range cases {
		if got := humanAttrName(in); got != want {
			t.Errorf("humanAttrName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAttrMargin(t *testing.T) {
	noThresh := attrMargin(smart.ATAAttribute{Value: 100, Thresh: 0})
	if noThresh != 1<<30 {
		t.Errorf("no-threshold margin = %d, want sentinel", noThresh)
	}
	if m := attrMargin(smart.ATAAttribute{Value: 100, Thresh: 10}); m != 90 {
		t.Errorf("margin = %d, want 90", m)
	}
	if m := attrMargin(smart.ATAAttribute{Value: 5, Thresh: 10}); m != -5 {
		t.Errorf("negative margin = %d, want -5", m)
	}
}

func TestMarginCell(t *testing.T) {
	// One encoding per column: no threshold means the not-reported dash, not a
	// dot standing in for a bar.
	none := marginCell(smart.ATAAttribute{Value: 100, Thresh: 0})
	if strings.Contains(none, "█") || strings.Contains(none, "●") {
		t.Errorf("no-threshold margin = %q, want the dash placeholder", none)
	}
	// With a threshold -> a bar, and no trailing number: the raw value-minus-
	// threshold read as a contradiction on a full bar and went negative on a
	// failing row. Those numbers live in the now/thr column instead.
	bar := marginCell(smart.ATAAttribute{Value: 100, Worst: 100, Thresh: 10, Flags: smart.ATAFlags{Prefailure: true}})
	if !strings.Contains(bar, "█") {
		t.Errorf("thresholded margin = %q, want a bar", bar)
	}
	if strings.ContainsAny(stripTags(bar), "0123456789") {
		t.Errorf("margin bar should carry no unlabelled number, got %q", bar)
	}
	// A failing attribute must not print a negative number anywhere.
	failing := marginCell(smart.ATAAttribute{Value: 12, Worst: 12, Thresh: 36, Flags: smart.ATAFlags{Prefailure: true}})
	if strings.Contains(stripTags(failing), "-") {
		t.Errorf("failing margin = %q, want no negative number", failing)
	}
}

// TestAttrStateIsAWord checks smartctl's raw enums are translated once, at the
// sink: they used to reach the table verbatim and truncate to "FAILING_NO…".
func TestAttrStateIsAWord(t *testing.T) {
	cases := []struct {
		a    smart.ATAAttribute
		want string
	}{
		{smart.ATAAttribute{Value: 100, Thresh: 10}, "ok"},
		{smart.ATAAttribute{WhenFailed: "FAILING_NOW"}, "FAILING"},
		{smart.ATAAttribute{WhenFailed: "in_the_past"}, "failed once"},
	}
	for _, c := range cases {
		if got := attrState(c.a); got != c.want {
			t.Errorf("attrState(%+v) = %q, want %q", c.a, got, c.want)
		}
		if strings.Contains(attrState(c.a), "_") {
			t.Errorf("attrState leaked a raw enum: %q", attrState(c.a))
		}
	}
}

// TestAttrLimitsShowsBothNumbers: the pair the state is derived from is stated
// in the footer, which now leads with the numbers so they cannot be clipped.
func TestAttrLimitsShowsBothNumbers(t *testing.T) {
	if got := attrLimits(smart.ATAAttribute{Value: 12, Thresh: 36}); got != "12/36" {
		t.Errorf("attrLimits = %q, want %q", got, "12/36")
	}
	got := attrLimits(smart.ATAAttribute{Value: 100, Thresh: 0})
	if !strings.HasPrefix(got, "100/") || strings.HasSuffix(got, "/0") {
		t.Errorf("no-threshold limits = %q, want the dash rather than a fake 0", got)
	}
}

func TestVisibleRowsSortFilter(t *testing.T) {
	attrs := []smart.ATAAttribute{
		{ID: 5, Value: 100, Worst: 100, Thresh: 10, Flags: smart.ATAFlags{Prefailure: true}},
		{ID: 1, Value: 83, Worst: 64, Thresh: 44, Flags: smart.ATAFlags{Prefailure: true}},
		{ID: 197, Value: 100, Worst: 100, Thresh: 0, WhenFailed: "FAILING_NOW"},
		{ID: 9, Value: 90, Worst: 90, Thresh: 0},
	}
	v := &attributesView{attrs: attrs}

	v.sortBy, v.filter = sortID, filterAll
	if ids := rowIDs(v.visibleRows()); !eqInts(ids, []int{1, 5, 9, 197}) {
		t.Errorf("sortID = %v, want [1 5 9 197]", ids)
	}

	v.sortBy = sortSeverity
	if ids := rowIDs(v.visibleRows()); ids[0] != 197 {
		t.Errorf("sortSeverity first = %d, want 197 (failing)", ids[0])
	}

	v.sortBy, v.filter = sortID, filterPrefail
	if ids := rowIDs(v.visibleRows()); !eqInts(ids, []int{1, 5}) {
		t.Errorf("filterPrefail = %v, want [1 5]", ids)
	}

	v.filter = filterConcerning
	if ids := rowIDs(v.visibleRows()); !eqInts(ids, []int{197}) {
		t.Errorf("filterConcerning = %v, want [197]", ids)
	}
}

func ataReport(attrs []smart.ATAAttribute) *smart.Report {
	return &smart.Report{
		Device:        smart.Device{Protocol: "ATA"},
		ATAAttributes: &smart.ATAAttributes{Table: attrs},
	}
}

func TestAttributesRefreshKeepsSelection(t *testing.T) {
	attrs := []smart.ATAAttribute{
		{ID: 1, Value: 83, Worst: 64, Thresh: 44, Flags: smart.ATAFlags{Prefailure: true}},
		{ID: 5, Value: 100, Worst: 100, Thresh: 10, Flags: smart.ATAFlags{Prefailure: true}},
		{ID: 9, Value: 90, Worst: 90, Thresh: 0, Raw: smart.ATARaw{String: "100"}},
		{ID: 197, Value: 100, Worst: 100, Thresh: 0},
	}
	v := newAttributesView(attrs)
	v.selectByID(9)
	if got := v.selectedID(); got != 9 {
		t.Fatalf("setup: selectedID = %d, want 9", got)
	}

	// A poll with the same IDs but a changed raw value must keep ID 9 selected.
	next := append([]smart.ATAAttribute(nil), attrs...)
	next[2].Raw = smart.ATARaw{String: "200"}
	v.refresh(ataReport(next), nil)
	if got := v.selectedID(); got != 9 {
		t.Errorf("after value refresh, selectedID = %d, want 9", got)
	}

	// A severity flip that reorders the list under sortSeverity must still keep
	// the selected attribute (ID 9), not the row index.
	reordered := append([]smart.ATAAttribute(nil), next...)
	reordered[0].WhenFailed = "FAILING_NOW" // ID 1 jumps to the top
	v.refresh(ataReport(reordered), nil)
	if got := v.selectedID(); got != 9 {
		t.Errorf("after reorder, selectedID = %d, want 9", got)
	}
}

func TestNVMeRefreshKeepsSelection(t *testing.T) {
	used := 5
	h := &smart.NVMeHealth{PercentageUsed: &used, MediaErrors: 0, PowerOnHours: 100}
	v := newNVMeAttributesView(h)
	v.table.Select(3, 0)
	row, _ := v.table.GetSelection()
	if row != 3 {
		t.Fatalf("setup: row = %d, want 3", row)
	}
	h2 := &smart.NVMeHealth{PercentageUsed: &used, MediaErrors: 1, PowerOnHours: 101}
	v.refresh(&smart.Report{Device: smart.Device{Protocol: "NVMe"}, NVMeHealth: h2}, nil)
	if row, _ := v.table.GetSelection(); row != 3 {
		t.Errorf("after refresh, row = %d, want 3", row)
	}
}

func rowIDs(attrs []smart.ATAAttribute) []int {
	ids := make([]int, len(attrs))
	for i, a := range attrs {
		ids[i] = a.ID
	}
	return ids
}

func eqInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestNVMeSensorsCarrySeverity: a hot sensor must grade the Sensors row even
// when the composite temperature sits comfortably in range.
func TestNVMeSensorsCarrySeverity(t *testing.T) {
	h := &smart.NVMeHealth{TemperatureSensors: []int{67, 43}}
	var row attrKV
	found := false
	for _, r := range nvmeRows(h) {
		if r.k == "Sensors" {
			row, found = r, true
		}
	}
	if !found {
		t.Fatal("no Sensors row")
	}
	if row.sev != smart.SeverityFailing {
		t.Errorf("row severity = %v, want Failing (hottest sensor is 67°C)", row.sev)
	}
	if !strings.Contains(row.v, "67°C") || !strings.Contains(row.v, "43°C") {
		t.Errorf("both sensors should still be listed, got %q", row.v)
	}
}
