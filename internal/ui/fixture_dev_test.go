//go:build dev

// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestFixtureStandbyDriveRendersAsleep drives real Scan/Info through the fixture source into the poll path.
func TestFixtureStandbyDriveRendersAsleep(t *testing.T) {
	if err := smart.UseFixtures("../smart/testdata"); err != nil {
		t.Fatal(err)
	}
	a, _ := newSimApp(t, 120, 40)

	devices, err := smart.Scan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	a.devices = devices

	results := map[string]pollResult{}
	for _, d := range devices {
		rep, err := smart.Info(t.Context(), d, smart.WakeDrive)
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		if rep.InStandby() {
			results[d.Name] = pollResult{standby: true}
			continue
		}
		results[d.Name] = pollResult{rep: rep}
	}
	a.applyResults(results)
	a.populateList()

	var asleep, awake int
	for _, d := range devices {
		_, sec := a.listRow(d)
		if strings.Contains(sec, standbyGlyph) {
			asleep++
			if !strings.Contains(sec, "asleep") {
				t.Errorf("%s carries the glyph but does not say asleep: %q", d.Name, sec)
			}
		} else {
			awake++
		}
	}
	if asleep != 1 {
		t.Errorf("%d drives render as spun down, want exactly 1 (smart-sdd-standby.json)", asleep)
	}
	if awake < 5 {
		t.Errorf("only %d drives render awake; the standby check is over-matching", awake)
	}
}

// TestFixtureFarmPairsBySerial pins which fixture drives get a FARM log, so the
// screenshot tapes' tab numbers stay put as fixtures are added.
func TestFixtureFarmPairsBySerial(t *testing.T) {
	if err := smart.UseFixtures("../smart/testdata"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{ // device -> its FARM log's page-1 serial
		"/dev/sda": "EXAMPLE1", "/dev/sdf": "EXAMPLE1", "/dev/sdc": "EXAMPLE1", "/dev/sdi": "EXAMPLE-ST4000",
	}
	devices, err := smart.Scan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devices {
		farm, err := smart.FarmLog(t.Context(), d, smart.WakeDrive)
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		var got string
		if farm != nil {
			got = farm.DriveInfo.Serial
		}
		if got != want[d.Name] {
			t.Errorf("%s: FARM log serial %q, want %q", d.Name, got, want[d.Name])
		}
	}
}
