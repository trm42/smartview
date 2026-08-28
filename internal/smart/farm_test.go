// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"encoding/json"
	"testing"
)

func TestCollectByHead(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }

	if got := collectByHead(map[string]json.RawMessage{}, "x_head_"); got != nil {
		t.Errorf("no keys = %v, want nil", got)
	}

	single := map[string]json.RawMessage{"x_head_0": raw("5")}
	if got := collectByHead(single, "x_head_"); len(got) != 1 || got[0] != 5 {
		t.Errorf("single = %v, want [5]", got)
	}

	// Gaps fill with zero; a malformed value is skipped (also zero).
	gappy := map[string]json.RawMessage{
		"x_head_0": raw("5"),
		"x_head_1": raw(`"bad"`),
		"x_head_3": raw("9"),
		"other_2":  raw("99"),
	}
	got := collectByHead(gappy, "x_head_")
	want := []int{5, 0, 0, 9}
	if len(got) != len(want) {
		t.Fatalf("gappy len = %d (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("gappy[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

// A head index is a JSON key suffix, i.e. unbounded input that sizes a slice.
// Oversized, negative and unparseable indices must be dropped rather than
// allocating (or panicking in makeslice) on the poll goroutine.
func TestCollectByHeadRejectsOutOfRangeIndex(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }

	crafted := map[string]json.RawMessage{
		"x_head_0":                    raw("5"),
		"x_head_1000000000":           raw("7"), // would allocate ~8 GB
		"x_head_99999999999999999999": raw("7"), // overflows Atoi
		"x_head_-1":                   raw("7"), // negative index
		"x_head_256":                  raw("7"), // exactly at the ceiling
	}
	got := collectByHead(crafted, "x_head_")
	if len(got) != 1 || got[0] != 5 {
		t.Fatalf("crafted = %v (len %d), want [5]", got, len(got))
	}

	// The last in-range index still sizes the slice.
	edge := map[string]json.RawMessage{"x_head_255": raw("3")}
	if got := collectByHead(edge, "x_head_"); len(got) != maxFARMHeads || got[maxFARMHeads-1] != 3 {
		t.Errorf("edge len = %d, want %d", len(got), maxFARMHeads)
	}

	// Only out-of-range keys is the same as no keys at all.
	if got := collectByHead(map[string]json.RawMessage{"x_head_9999": raw("1")}, "x_head_"); got != nil {
		t.Errorf("all out of range = %v, want nil", got)
	}
}
