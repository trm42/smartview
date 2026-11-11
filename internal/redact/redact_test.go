// SPDX-License-Identifier: GPL-3.0-or-later

package redact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureDir = "../smart/testdata"

type fixture struct {
	name   string
	data   []byte
	serial string // the report's own serial_number, or FARM page 1's
	farm   bool
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(fixtureDir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("glob fixtures: %v (%d found)", err, len(paths))
	}
	var out []fixture
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		serials, err := Serials(data)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		f := fixture{name: filepath.Base(p), data: data, farm: strings.Contains(string(data), `"seagate_farm_log"`)}
		if len(serials) > 0 {
			f.serial = serials[0]
		}
		out = append(out, f)
	}
	return out
}

// Every committed fixture carries only placeholders (see README "Contributing drive data").
func TestFixturesRedacted(t *testing.T) {
	for _, f := range loadFixtures(t) {
		findings, err := Check(f.data)
		if err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		for _, x := range findings {
			t.Errorf("%s: %s is not a placeholder; run go run ./cmd/redact-fixture on it", f.name, x)
		}
	}
}

// Redacting a committed fixture again changes nothing, so the tool keeps smartctl's
// layout and every number literal byte for byte.
func TestRedactIsLossless(t *testing.T) {
	for _, f := range loadFixtures(t) {
		serial := f.serial
		if serial == "" {
			serial = SerialPrefix
		}
		got, err := Redact(f.data, serial)
		if err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		if string(got) != string(f.data) {
			t.Errorf("%s: redacting a redacted fixture changed it", f.name)
		}
	}
}

// Each drive has its own placeholder serial and device name, and each FARM log
// pairs with exactly one report: the dev loader matches them by serial and keys
// reports by device name.
func TestFixtureSerialsPair(t *testing.T) {
	reports := map[string]string{}
	devices := map[string]string{}
	var farms []fixture
	for _, f := range loadFixtures(t) {
		farmOnly := f.farm && !strings.Contains(string(f.data), `"model_name"`)
		if name, _ := DeviceName(f.data); !farmOnly && name != "" {
			if prev := devices[name]; prev != "" {
				t.Errorf("%s and %s share device.name %q; pass redact-fixture an unused -device", prev, f.name, name)
			}
			devices[name] = f.name
		}
		switch {
		case f.serial == "":
		case farmOnly:
			farms = append(farms, f)
		case reports[f.serial] != "":
			t.Errorf("%s and %s share serial %q; give each drive its own placeholder", reports[f.serial], f.name, f.serial)
		default:
			reports[f.serial] = f.name
		}
	}
	seen := map[string]string{}
	for _, f := range farms {
		if prev := seen[f.serial]; prev != "" {
			t.Errorf("FARM logs %s and %s share serial %q", prev, f.name, f.serial)
		}
		seen[f.serial] = f.name
		if reports[f.serial] == "" {
			t.Errorf("FARM log %s: serial %q matches no report; use the drive report's placeholder", f.name, f.serial)
		}
	}
}

func TestRedactScrubsCopiesAndKeepsNumbers(t *testing.T) {
	in := `{
  "smartctl": {
    "argv": [
      "-j",
      "/dev/disk/by-id/ata-ST4000DM004-2CV104_ZFN0ABCD"
    ]
  },
  "device": {
    "name": "/dev/disk/by-id/wwn-0x5000c500deadbeef",
    "info_name": "/dev/disk/by-id/nvme-eui.002538b9a1b2c3d4"
  },
  "serial_number": "ZFN0ABCD",
  "wwn": {
    "naa": 5,
    "oui": 3152,
    "id": 3735928559
  },
  "nvme_namespaces": [
    {
      "eui64": {
        "oui": 9528,
        "ext_id": 794113893332
      },
      "nguid": "0x002538b9a1b2c3d4"
    }
  ],
  "raw": 11703790176571446
}
`
	got, err := Redact([]byte(in), "EXAMPLE-ST4000")
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"ZFN0ABCD", "deadbeef", "a1b2c3d4", "3735928559", "794113893332"} {
		if strings.Contains(strings.ToLower(string(got)), strings.ToLower(leak)) {
			t.Errorf("%s survived redaction:\n%s", leak, got)
		}
	}
	if !strings.Contains(string(got), "11703790176571446") {
		t.Errorf("a number above 2^53 was rewritten:\n%s", got)
	}
	if findings, _ := Check(got); len(findings) > 0 {
		t.Errorf("redacted output still fails Check: %v", findings)
	}
	if findings, _ := Check([]byte(in)); len(findings) < 6 {
		t.Errorf("Check found %d leaks in the raw input, want at least 6: %v", len(findings), findings)
	}
}

// A hand-crafted variant is serialled "<serial>-TAG", so its placeholder contains the serial it replaces.
func TestRedactPlaceholderMayContainTheOldSerial(t *testing.T) {
	in := `{
  "smartctl": {
    "argv": [
      "/dev/disk/by-id/ata-ST4000DM004_EXAMPLE-ST4000"
    ]
  },
  "serial_number": "EXAMPLE-ST4000"
}
`
	got, err := Redact([]byte(in), "EXAMPLE-ST4000-ERRORS")
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.ReplaceAll(in, "EXAMPLE-ST4000", "EXAMPLE-ST4000-ERRORS"); string(got) != want {
		t.Errorf("Redact =\n%s\nwant\n%s", got, want)
	}
}

func TestRedactRejectsARealLookingSerial(t *testing.T) {
	if _, err := Redact([]byte(`{}`), "ZFN0ABCD"); err == nil {
		t.Error("a placeholder without the EXAMPLE prefix was accepted")
	}
}

func TestRenameRewritesTheDeviceEverywhere(t *testing.T) {
	in := `{
  "smartctl": {
    "argv": [
      "-j",
      "-x",
      "/dev/sda"
    ]
  },
  "device": {
    "name": "/dev/sda",
    "info_name": "/dev/sda [SAT]"
  },
  "note": "/dev/sdab"
}
`
	got, err := Rename([]byte(in), "/dev/sdq")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(`"/dev/sda"`, `"/dev/sdq"`, `"/dev/sda [SAT]"`, `"/dev/sdq [SAT]"`).Replace(in)
	if string(got) != want {
		t.Errorf("Rename =\n%s\nwant\n%s", got, want)
	}
}
