// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "testing"

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
		got, ok := LeadingInt(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("LeadingInt(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestPowerMetrics pins the one pair of readings every drive type reports, so a
// fleet comparison of age has no gaps regardless of protocol.
func TestPowerMetrics(t *testing.T) {
	cases := []struct {
		fixture string
		hours   int
		cycles  int
	}{
		{"smart-sda.json", 9438, 15},
		{"smart-sdb.json", 48893, 206},
		{"smart-nvme.json", 9361, 14},
		{"smart-apple-nvme.json", 787, 238},
	}
	for _, c := range cases {
		r := parseFixture(t, c.fixture)
		if h, ok := r.PowerOnHours(); !ok || h != c.hours {
			t.Errorf("%s: PowerOnHours = (%d,%v), want (%d,true)", c.fixture, h, ok, c.hours)
		}
		if n, ok := r.PowerCycles(); !ok || n != c.cycles {
			t.Errorf("%s: PowerCycles = (%d,%v), want (%d,true)", c.fixture, n, ok, c.cycles)
		}
	}
}

// TestTempRange covers the ATA-only lifetime range and the NVMe absence: NVMe
// drives report no extremes at all, so the caller must fall back to an observed
// series rather than showing a fabricated range.
func TestTempRange(t *testing.T) {
	for _, c := range []struct {
		fixture  string
		min, max int
		ok       bool
	}{
		{"smart-sda.json", 23, 43, true},
		{"smart-sdb.json", 22, 50, true},
		{"smart-nvme.json", 0, 0, false},
		{"smart-apple-nvme.json", 0, 0, false},
	} {
		min, max, ok := parseFixture(t, c.fixture).TempRange()
		if ok != c.ok || min != c.min || max != c.max {
			t.Errorf("%s: TempRange = (%d,%d,%v), want (%d,%d,%v)",
				c.fixture, min, max, ok, c.min, c.max, c.ok)
		}
	}
}

// TestLifeUsedPercent checks the endurance fallback chain, including the Apple
// drive that reports endurance_used alongside the standard field, and the HDD
// that has no endurance indicator at all.
func TestLifeUsedPercent(t *testing.T) {
	for _, c := range []struct {
		fixture string
		pct     int
		ok      bool
	}{
		{"smart-nvme.json", 0, true},
		{"smart-apple-nvme.json", 3, true},
		{"smart-sda.json", 0, false}, // spinning disk: no endurance indicator
		{"smart-sdb.json", 0, false}, // SATA SSD without the Device Statistics log
	} {
		pct, ok := parseFixture(t, c.fixture).LifeUsedPercent()
		if ok != c.ok || pct != c.pct {
			t.Errorf("%s: LifeUsedPercent = (%d,%v), want (%d,%v)", c.fixture, pct, ok, c.pct, c.ok)
		}
	}
}

// TestSparePercent covers the NVMe health log and the Apple spare_available
// fallback. The Apple drive's threshold of 99 is deliberate: it is a real
// captured value and the near-threshold case worth keeping visible.
func TestSparePercent(t *testing.T) {
	for _, c := range []struct {
		fixture  string
		pct, thr int
		ok       bool
	}{
		{"smart-nvme.json", 100, 10, true},
		{"smart-apple-nvme.json", 100, 99, true},
		{"smart-sda.json", 0, 0, false},
	} {
		pct, thr, ok := parseFixture(t, c.fixture).SparePercent()
		if ok != c.ok || pct != c.pct || thr != c.thr {
			t.Errorf("%s: SparePercent = (%d,%d,%v), want (%d,%d,%v)",
				c.fixture, pct, thr, ok, c.pct, c.thr, c.ok)
		}
	}
}

// TestDataWritten is the important one: it pins which source each drive falls
// through to, because only the attribute source is approximate and the UI marks
// it. The Seagate has the Device Statistics log and must NOT be read from
// attribute 241 even though it also carries one; the Samsung has no log and so
// is the approximate case.
func TestDataWritten(t *testing.T) {
	for _, c := range []struct {
		fixture string
		bytes   int64
		source  WriteSource
		approx  bool
	}{
		{"smart-sda.json", 30003609491 * 512, WriteSourceDeviceStats, false},
		{"smart-sdb.json", 15656703192 * 512, WriteSourceAttribute, true},
		{"smart-nvme.json", 14879302 * 512 * 1000, WriteSourceNVMe, false},
		{"smart-apple-nvme.json", 120469636 * 512 * 1000, WriteSourceNVMe, false},
	} {
		w, ok := parseFixture(t, c.fixture).DataWritten()
		if !ok {
			t.Errorf("%s: DataWritten reported absent", c.fixture)
			continue
		}
		if w.Bytes != c.bytes || w.Source != c.source || w.Approximate() != c.approx {
			t.Errorf("%s: DataWritten = (%d, source %d, approx %v), want (%d, source %d, approx %v)",
				c.fixture, w.Bytes, w.Source, w.Approximate(), c.bytes, c.source, c.approx)
		}
	}
}

// TestErrorCounts checks that absent counters stay nil rather than reading as a
// reassuring zero — the distinction the whole ErrorCounts type exists for.
func TestErrorCounts(t *testing.T) {
	want := func(t *testing.T, label string, got *int64, v int64) {
		t.Helper()
		if got == nil {
			t.Errorf("%s: counter absent, want %d", label, v)
		} else if *got != v {
			t.Errorf("%s = %d, want %d", label, *got, v)
		}
	}
	absent := func(t *testing.T, label string, got *int64) {
		t.Helper()
		if got != nil {
			t.Errorf("%s = %d, want absent", label, *got)
		}
	}

	ata := parseFixture(t, "smart-sda.json").ErrorCounts()
	want(t, "sda Reallocated", ata.Reallocated, 0)
	want(t, "sda Pending", ata.Pending, 0)
	want(t, "sda CRCErrors", ata.CRCErrors, 0)
	want(t, "sda ErrorLogEntries", ata.ErrorLogEntries, 0)
	absent(t, "sda MediaErrors", ata.MediaErrors)
	absent(t, "sda UnsafeShutdowns", ata.UnsafeShutdowns)
	if sev := ata.Worst(); sev != SeverityOK {
		t.Errorf("sda ErrorCounts.Worst = %v, want OK", sev)
	}

	// The Samsung reports neither a pending-sector attribute nor the pending
	// defects log, so that counter must stay absent.
	ssd := parseFixture(t, "smart-sdb.json").ErrorCounts()
	want(t, "sdb Reallocated", ssd.Reallocated, 0)
	absent(t, "sdb Pending", ssd.Pending)
	absent(t, "sdb Uncorrectable", ssd.Uncorrectable)

	nvme := parseFixture(t, "smart-nvme.json").ErrorCounts()
	want(t, "nvme MediaErrors", nvme.MediaErrors, 0)
	want(t, "nvme UnsafeShutdowns", nvme.UnsafeShutdowns, 6)
	want(t, "nvme ErrorLogEntries", nvme.ErrorLogEntries, 0)
	absent(t, "nvme Reallocated", nvme.Reallocated)
	absent(t, "nvme CRCErrors", nvme.CRCErrors)

	// The hand-crafted error fixture populates the ATA error log table.
	errs := parseFixture(t, "smart-sda-errors.json").ErrorCounts()
	if errs.ErrorLogEntries == nil || *errs.ErrorLogEntries == 0 {
		t.Error("smart-sda-errors.json: expected a nonzero ATA error-log count")
	}
}

// TestMetricsOnSparseReport guards the graceful-degradation contract: a report
// with nothing but the three reliably-present sections must not panic and must
// report every optional metric absent.
func TestMetricsOnSparseReport(t *testing.T) {
	r := &Report{Device: Device{Name: "/dev/sdz", Protocol: "ATA"}}
	if _, ok := r.PowerOnHours(); ok {
		t.Error("PowerOnHours should be absent")
	}
	if _, ok := r.PowerCycles(); ok {
		t.Error("PowerCycles should be absent")
	}
	if _, ok := r.LifeUsedPercent(); ok {
		t.Error("LifeUsedPercent should be absent")
	}
	if _, _, ok := r.SparePercent(); ok {
		t.Error("SparePercent should be absent")
	}
	if _, _, ok := r.TempRange(); ok {
		t.Error("TempRange should be absent")
	}
	if _, ok := r.DataWritten(); ok {
		t.Error("DataWritten should be absent")
	}
	if e := r.ErrorCounts(); e.Reallocated != nil || e.MediaErrors != nil || e.Worst() != SeverityOK {
		t.Error("ErrorCounts should be entirely absent")
	}
}
