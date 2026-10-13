// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"encoding/json"
	"slices"
	"testing"
)

// TestSelfTestEstimates pins that only the polling times a drive reports are
// listed: the Samsung omits conveyance, NVMe has none.
func TestSelfTestEstimates(t *testing.T) {
	tests := []struct {
		fixture string
		want    []SelfTestEstimate
	}{
		{"smart-sda.json", []SelfTestEstimate{
			{"short", 1}, {"extended", 1804}, {"conveyance", 2},
		}},
		{"smart-sdb.json", []SelfTestEstimate{
			{"short", 2}, {"extended", 265},
		}},
		{"smart-nvme.json", nil},
	}
	for _, tt := range tests {
		if got := parseFixture(t, tt.fixture).SelfTestEstimates(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: estimates = %v, want %v", tt.fixture, got, tt.want)
		}
	}
}

// TestSelfTestEstimatesKeepReportedZero: key presence, not a positive value,
// is what marks a polling time as reported; the countdown still rejects zero.
func TestSelfTestEstimatesKeepReportedZero(t *testing.T) {
	var r Report
	doc := `{"device":{"protocol":"ATA"},"ata_smart_data":{"self_test":{"polling_minutes":{"short":0,"extended":-1,"conveyance":5}}}}`
	if err := json.Unmarshal([]byte(doc), &r); err != nil {
		t.Fatal(err)
	}
	// A negative time is a firmware "unknown", not a runtime.
	want := []SelfTestEstimate{{"short", 0}, {"conveyance", 5}}
	if got := r.SelfTestEstimates(); !slices.Equal(got, want) {
		t.Errorf("estimates = %v, want %v", got, want)
	}
	if d, ok := r.SelfTestDuration(SelfTestShort); ok {
		t.Errorf("zero short duration offered for the countdown: %v", d)
	}
	if d, ok := r.SelfTestDuration(SelfTestLong); ok {
		t.Errorf("negative extended duration offered for the countdown: %v", d)
	}
}
