// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// fixtureBytes reads one captured smartctl envelope from the smart package's testdata.
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "smart", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// fixtureReport parses one captured smartctl envelope.
func fixtureReport(t *testing.T, name string) *smart.Report {
	t.Helper()
	var rep smart.Report
	if err := json.Unmarshal(fixtureBytes(t, name), &rep); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return &rep
}
