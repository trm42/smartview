// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"encoding/json"
	"testing"
	"time"
)

// decode is a small helper for the inline self-test JSON snippets below.
func decode(t *testing.T, raw string) *Report {
	t.Helper()
	var r Report
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &r
}

func TestSupportsSelfTest(t *testing.T) {
	// ATA drive advertising the capability bit.
	if !parseFixture(t, "smart-sda.json").SupportsSelfTest() {
		t.Error("ATA drive with self_tests_supported should support self-tests")
	}
	// NVMe drive that exposes a self-test log (WD).
	if !parseFixture(t, "smart-nvme.json").SupportsSelfTest() {
		t.Error("NVMe drive with a self-test log should support self-tests")
	}
	// Apple internal NVMe: optional-admin self_test=false, no log → unsupported.
	if parseFixture(t, "smart-apple-nvme.json").SupportsSelfTest() {
		t.Error("Apple internal NVMe should not advertise self-test support")
	}
	// ATA drive that explicitly does not support self-tests.
	r := decode(t, `{"device":{"protocol":"ATA"},
		"ata_smart_data":{"capabilities":{"self_tests_supported":false}}}`)
	if r.SupportsSelfTest() {
		t.Error("self_tests_supported=false should report unsupported")
	}
}

func TestSelfTestProgressATA(t *testing.T) {
	r := decode(t, `{"device":{"protocol":"ATA"},
		"ata_smart_data":{"self_test":{"status":{
			"value":241,"string":"Self-test routine in progress","remaining_percent":90}}}}`)
	label, pct, running := r.SelfTestProgress()
	if !running {
		t.Fatal("expected a running self-test")
	}
	if pct != 10 {
		t.Errorf("percent = %d, want 10 (100-90)", pct)
	}
	if label == "" {
		t.Error("expected a status label")
	}

	// A completed status (no remaining_percent) is not "running".
	done := decode(t, `{"device":{"protocol":"ATA"},
		"ata_smart_data":{"self_test":{"status":{"value":0,"string":"completed without error","passed":true}}}}`)
	if _, _, running := done.SelfTestProgress(); running {
		t.Error("completed self-test should not report running")
	}
}

func TestSelfTestProgressNVMe(t *testing.T) {
	r := decode(t, `{"device":{"protocol":"NVMe"},
		"nvme_self_test_log":{
			"current_self_test_operation":{"value":2,"string":"Extended self-test in progress"},
			"current_self_test_completion_percent":42}}`)
	label, pct, running := r.SelfTestProgress()
	if !running {
		t.Fatal("expected a running NVMe self-test")
	}
	if pct != 42 {
		t.Errorf("percent = %d, want 42", pct)
	}
	if label == "" {
		t.Error("expected a status label")
	}

	// value 0 == no operation in progress.
	idle := decode(t, `{"device":{"protocol":"NVMe"},
		"nvme_self_test_log":{"current_self_test_operation":{"value":0,"string":"No self-test in progress"}}}`)
	if _, _, running := idle.SelfTestProgress(); running {
		t.Error("idle NVMe self-test log should not report running")
	}
}

func TestSelfTestDuration(t *testing.T) {
	r := parseFixture(t, "smart-sda.json") // short:1 extended:1804 (minutes)
	if d, ok := r.SelfTestDuration("short"); !ok || d != 1*time.Minute {
		t.Errorf("short duration = %v, %v", d, ok)
	}
	if d, ok := r.SelfTestDuration("long"); !ok || d != 1804*time.Minute {
		t.Errorf("long duration = %v, %v", d, ok)
	}
	if _, ok := r.SelfTestDuration("conveyance"); ok {
		t.Error("conveyance must not be offered as a duration")
	}
	// NVMe has no polling minutes.
	if _, ok := parseFixture(t, "smart-nvme.json").SelfTestDuration("short"); ok {
		t.Error("NVMe should not report a self-test duration")
	}
}

func TestRunSelfTestRejectsBadType(t *testing.T) {
	for _, bad := range []string{"conveyance", "selective", "offline", ""} {
		if err := RunSelfTest(t.Context(), "/dev/sda", bad); err == nil {
			t.Errorf("RunSelfTest(%q) = nil, want error", bad)
		}
	}
}
