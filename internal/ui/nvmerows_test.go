// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// nvmeRowMap decodes a health log from JSON and indexes its rendered rows by label.
func nvmeRowMap(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var r smart.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.NVMeHealth == nil {
		t.Fatal("no NVMe health log")
	}
	rows := map[string]string{}
	for _, kv := range nvmeRows(r.NVMeHealth) {
		rows[kv.k] = kv.v
	}
	return rows
}

func nvmeFixtureRows(t *testing.T, name string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "smart", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return nvmeRowMap(t, data)
}

// optionalNVMeRows pairs each optional health-log key with the row it renders
// and that row's text for a reported zero.
var optionalNVMeRows = []struct{ key, label, zero string }{
	{"host_reads", "Read commands", "0"},
	{"host_writes", "Write commands", "0"},
	{"controller_busy_time", "Controller busy", "0 min"},
	{"warning_temp_time", "Warn temp time", "0 min"},
	{"critical_comp_time", "Crit temp time", "0 min"},
}

// TestNVMeOptionalRowsFollowKeyPresence: a row exists exactly when its key
// does, so an absent reading is not a zero and a reported zero is not hidden.
func TestNVMeOptionalRowsFollowKeyPresence(t *testing.T) {
	for _, c := range optionalNVMeRows {
		absent := nvmeRowMap(t, []byte(`{"nvme_smart_health_information_log":{}}`))
		if v, ok := absent[c.label]; ok {
			t.Errorf("%s absent: row %q = %q, want no row", c.key, c.label, v)
		}
		zero := nvmeRowMap(t, []byte(`{"nvme_smart_health_information_log":{"`+c.key+`":0}}`))
		if v, ok := zero[c.label]; !ok || v != c.zero {
			t.Errorf("%s reported as 0: row %q = %q (present %v), want %q", c.key, c.label, v, ok, c.zero)
		}
	}
}

// TestAppleNVMeRowsMatchItsHealthLog: the sparse fixture omits both thermal
// timers and reports a zero controller busy time.
func TestAppleNVMeRowsMatchItsHealthLog(t *testing.T) {
	rows := nvmeFixtureRows(t, "smart-apple-nvme.json")
	for _, label := range []string{"Warn temp time", "Crit temp time"} {
		if v, ok := rows[label]; ok {
			t.Errorf("row %q = %q for a key the drive does not report", label, v)
		}
	}
	if v, ok := rows["Controller busy"]; !ok || v != "0 min" {
		t.Errorf("Controller busy = %q (present %v), want the reported 0 min", v, ok)
	}
	if v := rows["Read commands"]; v != "1547972371" {
		t.Errorf("Read commands = %q, want 1547972371", v)
	}
}

// TestWDNVMeKeepsReportedZeroTimers: genuine zeros still render.
func TestWDNVMeKeepsReportedZeroTimers(t *testing.T) {
	rows := nvmeFixtureRows(t, "smart-nvme.json")
	for _, label := range []string{"Warn temp time", "Crit temp time"} {
		if v, ok := rows[label]; !ok || v != "0 min" {
			t.Errorf("row %q = %q (present %v), want 0 min", label, v, ok)
		}
	}
	if v := rows["Controller busy"]; v != "~19 h" {
		t.Errorf("Controller busy = %q, want ~19 h", v)
	}
}
