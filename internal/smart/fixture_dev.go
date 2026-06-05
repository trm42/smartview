// SPDX-License-Identifier: GPL-3.0-or-later

//go:build dev

package smart

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// This file is the dev-only fixture data source. Built with `-tags dev`, it
// lets the live TUI render captured smartctl JSON from a directory instead of
// shelling out to a real smartctl. The release counterpart (fixture_stub.go,
// //go:build !dev) disables all of this. UseFixtures populates the package-level
// index eagerly so a bad/empty dir fails fast at startup; the guards in
// Scan/Info/FarmLog then delegate to fixtureScan/fixtureInfo/fixtureFarm.

var (
	// fixtureDir is the active fixture directory. Empty means inactive, which
	// is what fixtureActive keys off of.
	fixtureDir string
	// fixtureReports holds the parsed full reports in deterministic (sorted by
	// filename) order so fixtureScan is stable across runs.
	fixtureReports []*Report
	// fixtureByName indexes those reports by Device.Name (round-tripped
	// verbatim, never normalized).
	fixtureByName map[string]*Report
	// fixtureFarms holds standalone FARM logs paired with their drive serial,
	// used to attach FARM data to a matching FARM-capable report.
	fixtureFarms []fixtureFarmEntry
)

// fixtureFarmEntry is a standalone FARM fixture and the serial number reported
// in its page-1 drive information, used to match it to a Report.
type fixtureFarmEntry struct {
	serial string
	farm   *FARM
}

// UseFixtures activates the fixture source backed by dir. It stats dir, reads
// every *.json once and classifies each file: a standalone FARM log (top-level
// seagate_farm_log present but with no report fields) is stored as a FARM log;
// any other file decodes into a Report via the same json.Unmarshal path as Info
// and is keyed by Device.Name. Validation is eager — a missing, non-directory,
// or report-less directory returns a non-nil error so startup fails fast.
func UseFixtures(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("fixture dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("fixture dir %q is not a directory", dir)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return fmt.Errorf("scan fixture dir %q: %w", dir, err)
	}
	sort.Strings(files)

	byName := make(map[string]*Report)
	var reports []*Report
	var farms []fixtureFarmEntry

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read fixture %s: %w", f, err)
		}

		// Probe the file to classify it. A standalone FARM log carries the
		// seagate_farm_log key but lacks the fields a full report always has
		// (smart_status, model_name, the attribute table). Note that the FARM
		// log fixture DOES carry a device block with a protocol, so absence of
		// Device.Protocol is not a reliable discriminator — the report fields
		// are.
		var probe struct {
			SmartStatus   *json.RawMessage `json:"smart_status"`
			ModelName     string           `json:"model_name"`
			ATAAttributes *json.RawMessage `json:"ata_smart_attributes"`
			FARM          json.RawMessage  `json:"seagate_farm_log"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			return fmt.Errorf("parse fixture %s: %w", f, err)
		}

		farmOnly := len(probe.FARM) > 0 &&
			probe.SmartStatus == nil &&
			probe.ModelName == "" &&
			probe.ATAAttributes == nil
		if farmOnly {
			var wrapper struct {
				FARM *FARM `json:"seagate_farm_log"`
			}
			if err := json.Unmarshal(data, &wrapper); err != nil {
				return fmt.Errorf("parse FARM fixture %s: %w", f, err)
			}
			var serialWrapper struct {
				FARM struct {
					DriveInfo struct {
						Serial string `json:"serial_number"`
					} `json:"page_1_drive_information"`
				} `json:"seagate_farm_log"`
			}
			if err := json.Unmarshal(data, &serialWrapper); err != nil {
				return fmt.Errorf("parse FARM fixture %s: %w", f, err)
			}
			farms = append(farms, fixtureFarmEntry{
				serial: serialWrapper.FARM.DriveInfo.Serial,
				farm:   wrapper.FARM,
			})
			continue
		}

		// Full report: decode via the same path as Info and key by Device.Name.
		var rep Report
		if err := json.Unmarshal(data, &rep); err != nil {
			return fmt.Errorf("parse report fixture %s: %w", f, err)
		}
		byName[rep.Device.Name] = &rep
		reports = append(reports, &rep)
	}

	if len(reports) == 0 {
		return fmt.Errorf("no fixture reports found in %q (need at least one *.json full report)", dir)
	}

	fixtureDir = dir
	fixtureReports = reports
	fixtureByName = byName
	fixtureFarms = farms
	return nil
}

// fixtureActive reports whether the fixture source is in use.
func fixtureActive() bool { return fixtureDir != "" }

// fixtureScan returns each indexed report's Device verbatim, standing in for
// `smartctl --scan-open`. Device names are not constructed or normalized.
func fixtureScan() ([]Device, error) {
	devices := make([]Device, 0, len(fixtureReports))
	for _, r := range fixtureReports {
		devices = append(devices, r.Device)
	}
	return devices, nil
}

// fixtureInfo returns the report whose Device.Name matches name, standing in
// for `smartctl -j -x <name>`. An unknown name is an error.
func fixtureInfo(name string) (*Report, error) {
	if r, ok := fixtureByName[name]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("no fixture report for device %q", name)
}

// fixtureFarm returns the FARM log for the named device, mirroring FarmLog: it
// yields (nil, nil) for any drive that does not SupportsFARM or for which no
// supported FARM fixture matches. When several FARM fixtures exist it matches
// on serial number; with a single FARM fixture it attaches to the sole
// FARM-capable device.
func fixtureFarm(name string) (*FARM, error) {
	rep, ok := fixtureByName[name]
	if !ok {
		return nil, fmt.Errorf("no fixture report for device %q", name)
	}
	if !rep.SupportsFARM() || len(fixtureFarms) == 0 {
		return nil, nil
	}

	if len(fixtureFarms) > 1 {
		for _, fe := range fixtureFarms {
			if fe.serial != "" && fe.serial == rep.SerialNumber {
				return supportedFarm(fe.farm), nil
			}
		}
		return nil, nil
	}

	// Single FARM fixture: attach it to the sole FARM-capable device.
	return supportedFarm(fixtureFarms[0].farm), nil
}

// supportedFarm returns f only when it is present and reports Supported, matching
// FarmLog's (nil, nil)-on-unsupported contract.
func supportedFarm(f *FARM) *FARM {
	if f == nil || !f.Supported {
		return nil
	}
	return f
}
