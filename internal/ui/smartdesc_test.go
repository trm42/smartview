// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"path/filepath"
	"testing"
)

// Every row the Attributes tab draws for a committed fixture has a footer explanation.
func TestAttributeRowsAreExplained(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "smart", "testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		name := filepath.Base(p)
		r := fixtureReport(t, name)
		if r.ATAAttributes != nil {
			for _, a := range r.ATAAttributes.Table {
				if ataDesc[a.ID] == "" {
					t.Errorf("%s: attribute %d (%s) has no ataDesc entry", name, a.ID, a.Name)
				}
			}
		}
		if r.NVMeHealth != nil {
			for _, row := range nvmeRows(r) {
				if nvmeDesc[row.k] == "" {
					t.Errorf("%s: NVMe row %q has no nvmeDesc entry", name, row.k)
				}
			}
		}
	}
}
