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

// Dev-only fixture source: captured smartctl JSON stands in for shelling out.

var (
	// fixtureReports is sorted by filename so fixtureScan is stable.
	fixtureReports []*Report
	fixtureByName  map[string]*Report
	fixtureFarms   []fixtureFarmEntry
)

// fixtureFarmEntry pairs a standalone FARM fixture with its page-1 serial.
type fixtureFarmEntry struct {
	serial string
	farm   *FARM
}

// UseFixtures loads every *.json in dir as a full Report or standalone FARM log, failing
// eagerly on bad input.
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

		// A standalone FARM log carries a device block too, so Device.Protocol can't discriminate.
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
			var wrapper farmWrapper
			if err := json.Unmarshal(data, &wrapper); err != nil {
				return fmt.Errorf("parse FARM fixture %s: %w", f, err)
			}
			fe := fixtureFarmEntry{farm: wrapper.FARM}
			if wrapper.FARM != nil {
				fe.serial = wrapper.FARM.DriveInfo.Serial
			}
			farms = append(farms, fe)
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

	fixtureReports = reports
	fixtureByName = byName
	fixtureFarms = farms
	return nil
}

// fixtureActive reports whether the fixture source is in use.
func fixtureActive() bool { return fixtureByName != nil }

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

// fixtureFarm mirrors FarmLog's (nil, nil)-on-unsupported contract; a lone FARM
// fixture attaches to any FARM-capable device, several match by serial.
func fixtureFarm(name string) (*FARM, error) {
	rep, ok := fixtureByName[name]
	if !ok {
		return nil, fmt.Errorf("no fixture report for device %q", name)
	}
	if !rep.SupportsFARM() || len(fixtureFarms) == 0 {
		return nil, nil
	}

	if len(fixtureFarms) == 1 {
		return supportedFarm(fixtureFarms[0].farm), nil
	}
	for _, fe := range fixtureFarms {
		if fe.serial != "" && fe.serial == rep.SerialNumber {
			return supportedFarm(fe.farm), nil
		}
	}
	return nil, nil
}
