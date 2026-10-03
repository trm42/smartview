// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// loadReport decodes one captured smartctl report fixture.
func loadReport(t *testing.T, file string) *smart.Report {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "smart", "testdata", file))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var r smart.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("parse fixture %s: %v", file, err)
	}
	return &r
}

// TestOutcomeClassifiesAFetch: nothing back is a failed read, a parked drive is asleep, and neither is the other.
func TestOutcomeClassifiesAFetch(t *testing.T) {
	if got := outcome(nil); !got.failed || got.standby || got.rep != nil {
		t.Errorf("outcome(nil) = %+v, want only failed set", got)
	}
	if got := outcome(loadReport(t, "smart-sdd-standby.json")); !got.standby || got.failed || got.rep != nil {
		t.Errorf("outcome(standby envelope) = %+v, want only standby set", got)
	}
	if got := outcome(healthyReport("/dev/sdb")); got.failed || got.standby || got.rep == nil {
		t.Errorf("outcome(good report) = %+v, want only the report", got)
	}
}

// TestFailedFetchMarksTheReadingStale: a timeout or empty stdout leaves the same stale reading a failed open does.
func TestFailedFetchMarksTheReadingStale(t *testing.T) {
	for name, width := range layoutWidths {
		t.Run(name, func(t *testing.T) {
			a, d := goneUnreadable(t, width, outcome(nil))
			assertMarkedStale(t, a, d)
			assertReadClears(t, a, d)
		})
	}
}

// TestFailedFetchBeforeAnyReadingStaysScanning: nothing is cached, so there is nothing to call stale.
func TestFailedFetchBeforeAnyReadingStaysScanning(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	a.applyResults(map[string]pollResult{dev: outcome(nil)})
	if a.reports[dev] != nil || a.unreadable[dev] {
		t.Errorf("reports = %v, unreadable = %v; want no report and no mark", a.reports[dev], a.unreadable[dev])
	}
}

// TestFailedFetchReplacesTheStandbyMark: a drive that stopped answering is no longer known to be asleep.
func TestFailedFetchReplacesTheStandbyMark(t *testing.T) {
	a, d := goneUnreadable(t, 120, pollResult{standby: true})
	a.applyPoll(map[string]pollResult{d.Name: outcome(nil)})

	if a.asleep[d.Name] {
		t.Error("asleep is still set after a failed fetch")
	}
	_, sec := a.listRow(d)
	if strings.Contains(sec, standbyGlyph) || !strings.Contains(sec, staleGlyph) {
		t.Errorf("row %q: want the unreadable mark in place of the standby one", sec)
	}
	if got := a.detail.note.GetText(true); !strings.Contains(got, "Last read failed") {
		t.Errorf("detail note %q still describes a spun-down drive", got)
	}
}

// farmCapable is a healthy report from a model the FARM predicate accepts.
func farmCapable(name string) *smart.Report {
	r := healthyReport(name)
	r.ModelName = "ST8000DM004"
	return r
}

// TestFetchAllReportsEveryDrive: a drive that returns nothing still gets a result, and no FARM call.
func TestFetchAllReportsEveryDrive(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const seagate, gone = "/dev/sda", "/dev/sdb"
	a.devices = []smart.Device{{Name: seagate, Type: "sat"}, {Name: gone, Type: "sat"}}

	var farmFor []string
	got := a.fetchAll(context.Background(), false, fetchers{
		info: func(_ context.Context, d smart.Device, _ smart.PowerPolicy) (*smart.Report, error) {
			if d.Name == gone {
				return nil, errors.New("timed out")
			}
			return farmCapable(d.Name), nil
		},
		farm: func(_ context.Context, d smart.Device, _ smart.PowerPolicy) (*smart.FARM, error) {
			farmFor = append(farmFor, d.Name)
			return &smart.FARM{}, nil
		},
	})

	if res, ok := got[gone]; !ok || !res.failed {
		t.Errorf("result for the silent drive = %+v (present %v), want a failed result", res, ok)
	}
	if res := got[seagate]; res.rep == nil || res.rep.FARM == nil {
		t.Errorf("result for the answering drive = %+v, want its report with FARM attached", res)
	}
	if !slices.Equal(farmFor, []string{seagate}) {
		t.Errorf("FARM was fetched for %v, want only %s", farmFor, seagate)
	}
}

// TestFetchAllPassesOnePolicyToBothCalls: either call wakes a parked drive, so both must carry the policy.
func TestFetchAllPassesOnePolicyToBothCalls(t *testing.T) {
	cases := []struct {
		name        string
		aware, wake bool
		want        smart.PowerPolicy
	}{
		{"standby aware", true, false, smart.SkipStandby},
		{"standby aware, forced", true, true, smart.WakeDrive},
		{"not standby aware", false, false, smart.WakeDrive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := newSimApp(t, 120, 40)
			a.standbyAware.Store(c.aware)
			a.devices = []smart.Device{{Name: "/dev/sda", Type: "sat"}}

			var info, farm []smart.PowerPolicy
			a.fetchAll(context.Background(), c.wake, fetchers{
				info: func(_ context.Context, d smart.Device, p smart.PowerPolicy) (*smart.Report, error) {
					info = append(info, p)
					return farmCapable(d.Name), nil
				},
				farm: func(_ context.Context, _ smart.Device, p smart.PowerPolicy) (*smart.FARM, error) {
					farm = append(farm, p)
					return nil, nil
				},
			})

			want := []smart.PowerPolicy{c.want}
			if !slices.Equal(info, want) || !slices.Equal(farm, want) {
				t.Errorf("policies: info %v, farm %v; want %v for both", info, farm, want)
			}
		})
	}
}
