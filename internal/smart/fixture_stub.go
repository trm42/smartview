// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !dev

package smart

import "errors"

// Release counterpart of fixture_dev.go: fixtureActive is always false, so
// the guards in Scan/Info/FarmLog never fire.

// UseFixtures rejects fixture activation in release builds.
func UseFixtures(string) error {
	return errors.New("smartview was built without fixture support; rebuild with: go build -tags dev")
}

func fixtureActive() bool { return false }

// Unreachable stubs, guarded by fixtureActive.

func fixtureScan() ([]Device, error)           { return nil, nil }
func fixtureInfo(name string) (*Report, error) { return nil, nil }
func fixtureFarm(name string) (*FARM, error)   { return nil, nil }
