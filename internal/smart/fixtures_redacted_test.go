// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Placeholders the README's "Contributing drive data" redaction uses.
const (
	redactedID       = 305419896
	redactedFarmWWN  = "0x5000c50012345678"
	redactedHexValue = "0x0"
)

// TestFixturesRedacted fails when a fixture carries a drive identifier that
// isn't one of the README's placeholders.
func TestFixturesRedacted(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("glob fixtures: %v (%d found)", err, len(paths))
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		checkRedacted(t, filepath.Base(p), "", v)
	}
}

func checkRedacted(t *testing.T, file, path string, v any) {
	t.Helper()
	switch v := v.(type) {
	case []any:
		for _, e := range v {
			checkRedacted(t, file, path+"[]", e)
		}
	case map[string]any:
		for k, e := range v {
			at := path + "." + k
			bad := false
			switch k {
			case "serial_number":
				s, _ := e.(string)
				bad = !strings.HasPrefix(s, "EXAMPLE")
			case "world_wide_name":
				bad = e != redactedFarmWWN
			case "wwn":
				bad = numberAt(e, "id") != redactedID
			case "eui64":
				bad = numberAt(e, "ext_id") != redactedID
			case "logical_unit_id", "nguid", "sas_address", "attached_sas_address":
				bad = e != redactedHexValue && e != json.Number("0")
			default:
				checkRedacted(t, file, at, e)
			}
			if bad {
				t.Errorf("%s: %s = %v is not a redaction placeholder (see README \"Contributing drive data\")", file, at, e)
			}
		}
	}
}

// numberAt returns obj[key] as an int64, or -1 when absent or not a number.
func numberAt(obj any, key string) int64 {
	m, _ := obj.(map[string]any)
	n, _ := m[key].(json.Number)
	i, err := n.Int64()
	if err != nil {
		return -1
	}
	return i
}
