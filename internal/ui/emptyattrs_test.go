// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// emptyAttrEnvelopes are the two shapes an ATA drive reports no attributes in.
var emptyAttrEnvelopes = map[string]string{
	"empty table": `{"device":{"name":"/dev/sdx","protocol":"ATA"},"smart_status":{"passed":true},` +
		`"ata_smart_attributes":{"revision":0,"table":[]}}`,
	"no table": `{"device":{"name":"/dev/sdx","protocol":"ATA"},"smart_status":{"passed":true},` +
		`"ata_smart_attributes":{"revision":0}}`,
}

func decodeReport(t *testing.T, src string) *smart.Report {
	t.Helper()
	var r smart.Report
	if err := json.Unmarshal([]byte(src), &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return &r
}

// TestEmptyAttributeTableIsNotData pins that an attribute section with no rows is absent data, not a clean bill.
func TestEmptyAttributeTableIsNotData(t *testing.T) {
	for name, src := range emptyAttrEnvelopes {
		t.Run(name, func(t *testing.T) {
			r := decodeReport(t, src)

			if hasAttributes(r) {
				t.Error("hasAttributes = true for a table with no rows")
			}
			for _, tb := range visibleTabs(r, false) {
				if tb.id == "attributes" {
					t.Error("Attributes tab is offered with no attributes to show")
				}
			}
			if ev := verdictEvidence(r); strings.Contains(ev, "attribute") {
				t.Errorf("verdict evidence %q cites attributes the drive did not report", ev)
			}
		})
	}
}

// TestEmptyAttributeTableShowsPlaceholder covers show_unavailable_tabs: the tab stays in the strip, muted.
func TestEmptyAttributeTableShowsPlaceholder(t *testing.T) {
	for name, src := range emptyAttrEnvelopes {
		t.Run(name, func(t *testing.T) {
			d := newDetail()
			d.showAllTabs = true
			d.update(decodeReport(t, src), nil)

			if len(d.tabs) != len(allTabs) {
				t.Fatalf("showAll gave %d tabs, want %d", len(d.tabs), len(allTabs))
			}
			if d.tabs[1].id != "attributes" || d.tabs[1].available {
				t.Fatalf("tab 1 = %+v, want an unavailable attributes tab", d.tabs[1])
			}
			if _, ok := d.views["attributes"].(staticView); !ok {
				t.Errorf("attributes view is %T, want the placeholder", d.views["attributes"])
			}
			if d.selectTabID("attributes") {
				t.Error("the unavailable Attributes tab was selectable")
			}
		})
	}
}

// TestEmptyAttributeRowsNeverClaimHealthy pins that "all healthy" is only the concerning filter's answer.
func TestEmptyAttributeRowsNeverClaimHealthy(t *testing.T) {
	healthy := []smart.ATAAttribute{{ID: 9, Value: 90, Worst: 90}}
	const (
		noMatch    = " No attributes match this filter "
		allHealthy = " No attributes match — all healthy "
	)
	cases := []struct {
		name   string
		filter filterMode
		want   string
	}{
		{"no pre-fail rows", filterPrefail, noMatch},
		{"nothing concerning", filterConcerning, allHealthy},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := newAttributesView(healthy)
			v.filter = c.filter
			v.renderRows()

			if got := v.table.GetCell(1, 0).Text; got != c.want {
				t.Errorf("row = %q, want %q", got, c.want)
			}
		})
	}
}

// TestEmptyRefreshKeepsRows: an empty poll leaves the rows for the tab swap to replace.
func TestEmptyRefreshKeepsRows(t *testing.T) {
	v := newAttributesView([]smart.ATAAttribute{{ID: 9, Value: 90, Worst: 90}})
	v.refresh(decodeReport(t, emptyAttrEnvelopes["empty table"]), nil)
	if len(v.attrs) != 1 {
		t.Errorf("attrs = %d rows after an empty refresh, want the previous 1", len(v.attrs))
	}
}
