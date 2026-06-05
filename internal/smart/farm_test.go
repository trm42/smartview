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
