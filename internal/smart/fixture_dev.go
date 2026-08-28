// SPDX-License-Identifier: GPL-3.0-or-later

//go:build dev

package smart

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Dev-only fixture data source (-tags dev): the TUI renders captured smartctl
// JSON from a directory instead of shelling out. fixture_stub.go is the
// release counterpart.

var (
	// fixtureDir is the active fixture directory; empty means inactive.
	fixtureDir string
	// fixtureReports is sorted by filename so fixtureScan is stable.
	fixtureReports []*Report
	// fixtureByName indexes reports by verbatim Device.Name.
	fixtureByName map[string]*Report
	// fixtureFarms holds standalone FARM logs keyed by drive serial.
	fixtureFarms []fixtureFarmEntry
)

// fixtureFarmEntry pairs a standalone FARM fixture with its page-1 serial.
type fixtureFarmEntry struct {
	serial string
	farm   *FARM
}

// UseFixtures activates the fixture source: every *.json in dir is classified
// as a standalone FARM log or a full Report. Validation is eager so a bad or
// empty directory fails at startup.
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
	slices.Sort(files)

	byName := make(map[string]*Report)
	var reports []*Report
	var farms []fixtureFarmEntry

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read fixture %s: %w", f, err)
		}

		// A standalone FARM log lacks the fields a full report always has;
		// it DOES carry a device block, so Device.Protocol can't discriminate.
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

// fixtureScan stands in for `smartctl --scan-open`, returning each Device verbatim.
func fixtureScan() ([]Device, error) {
	devices := make([]Device, 0, len(fixtureReports))
	for _, r := range fixtureReports {
		devices = append(devices, r.Device)
	}
	return devices, nil
}

// fixtureInfo stands in for `smartctl -j -x <name>`; an unknown name is an error.
func fixtureInfo(name string) (*Report, error) {
	if r, ok := fixtureByName[name]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("no fixture report for device %q", name)
}

// fixtureFarm mirrors FarmLog's (nil, nil)-on-unsupported contract. Several
// FARM fixtures match by serial; a single one attaches to the sole
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

	return supportedFarm(fixtureFarms[0].farm), nil
}

// supportedFarm returns f only when present and Supported.
func supportedFarm(f *FARM) *FARM {
	if f == nil || !f.Supported {
		return nil
	}
	return f
}
