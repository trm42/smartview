// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"testing"

	"smartview/internal/smart"
)

func TestLeadingInt(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"9201 (189 58 0)", 9201, true},
		{"37 (0 21 0 0 0)", 37, true},
		{"  42", 42, true},
		{"", 0, false},
		{"n/a", 0, false},
	}
	for _, c := range cases {
		got, ok := leadingInt(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("leadingInt(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

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
		{mk(4, "15"), "15"}, // undecoded -> raw passthrough
		{mk(5, ""), "—"},
	}
	for _, c := range cases {
		if got := decodeReading(c.a); got != c.want {
			t.Errorf("decodeReading(id %d, %q) = %q, want %q", c.a.ID, c.a.Raw.String, got, c.want)
		}
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
