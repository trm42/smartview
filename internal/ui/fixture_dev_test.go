//go:build dev

// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestFixtureStandbyDriveRendersAsleep drives the whole stack the way the
// --fixtures build does: real Scan/Info through the fixture source, into the
// real poll path. A pty capture cannot answer this, because terminals emit
// incremental diffs and the stale text of an earlier frame survives in the
// byte stream.
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
