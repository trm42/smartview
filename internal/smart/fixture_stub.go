// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !dev

package smart

import "errors"

// UseFixtures rejects fixture activation in release builds.
func UseFixtures(string) error {
	return errors.New("smartview was built without fixture support; rebuild with: go build -tags dev")
}

func fixtureActive() bool { return false }

// Unreachable: guarded by fixtureActive.

func fixtureScan() ([]Device, error)           { return nil, nil }
func fixtureInfo(name string) (*Report, error) { return nil, nil }
func fixtureFarm(name string) (*FARM, error)   { return nil, nil }
