// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !dev

package smart

import "errors"

// This file is the non-dev (release) implementation of the fixture source
// toggle. Because fixtureActive always reports false here, the guards in
// Scan/Info/FarmLog never fire and runtime behavior is identical to a build
// without any fixture support. The real fixture-backed implementation lives in
// a //go:build dev counterpart.

// UseFixtures rejects fixture activation in release builds.
func UseFixtures(string) error {
	return errors.New("smartview was built without fixture support; rebuild with: go build -tags dev")
}

// fixtureActive reports whether the fixture source is in use; always false in
// the release build.
func fixtureActive() bool { return false }

// The following stubs are unreachable in release builds (guarded by
// fixtureActive). They exist only to satisfy the guards' call sites.

func fixtureScan() ([]Device, error)           { return nil, nil }
func fixtureInfo(name string) (*Report, error) { return nil, nil }
func fixtureFarm(name string) (*FARM, error)   { return nil, nil }
