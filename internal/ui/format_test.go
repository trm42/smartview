// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/trm42/smartview/internal/smart"
)

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:                 "0 B",
		512:               "512 B",
		999:               "999 B",
		1000:              "1.0 kB",
		1_000_000:         "1.0 MB",
		1_000_000_000:     "1.0 GB",
		1_000_000_000_000: "1.0 TB",
		1_500_000_000_000: "1.5 TB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := map[int]string{
		0:    "0 h",
		23:   "23 h",
		24:   "1 d",
		100:  "4 d",
		8736: "364 d",   // 364 days
		8760: "1 y 0 d", // exactly 365 days
		8784: "1 y 1 d",
		9439: "1 y 28 d",
	}
	for in, want := range cases {
		if got := humanDuration(in); got != want {
			t.Errorf("humanDuration(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanMinutes(t *testing.T) {
	cases := map[int]string{0: "0 min", 1: "1 min", 89: "89 min", 90: "~2 h", 1804: "~30 h"}
	for in, want := range cases {
		if got := humanMinutes(in); got != want {
			t.Errorf("humanMinutes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestMarginBar(t *testing.T) {
	// Full headroom → full bar; the bar carries no number (the now/thr column does).
	full := marginBar(100, 100, 10, smart.SeverityOK)
	if !strings.Contains(full, "["+severityTag(smart.SeverityOK)+"]") {
		t.Errorf("full bar = %q", full)
	}
	if strings.ContainsAny(stripTags(full), "0123456789") {
		t.Errorf("margin bar should carry no unlabelled number: %q", full)
	}
	if strings.Count(full, "█") != pctBarWidth {
		t.Errorf("full bar should be %d blocks: %q", pctBarWidth, full)
	}
	// 200-base attribute (e.g. CRC) keeps the bar within range (no overflow).
	b200 := marginBar(200, 200, 0, smart.SeverityOK)
	if strings.Count(b200, "█") != pctBarWidth {
		t.Errorf("200-base full bar should be a full bar: %q", b200)
	}
	// Value below threshold clamps to an empty bar.
	empty := marginBar(5, 5, 10, smart.SeverityFailing)
	if strings.Count(empty, "█") != 0 || !strings.Contains(empty, "["+severityTag(smart.SeverityFailing)+"]") {
		t.Errorf("below-threshold bar = %q", empty)
	}
}

func TestCapacity(t *testing.T) {
	withUser := &smart.Report{UserCapacity: &smart.Capacity{Bytes: 1_000_000_000_000}}
	if got := capacityString(withUser); got != "1.0 TB" {
		t.Errorf("user capacity = %q", got)
	}
	total := int64(2_000_398_934_016)
	fallback := &smart.Report{NVMeTotalCapacity: &total}
	if b, ok := capacityBytes(fallback); !ok || b != total {
		t.Errorf("nvme fallback = %d,%v", b, ok)
	}
	if got := capacityString(&smart.Report{}); got != dash {
		t.Errorf("no capacity = %q, want dash", got)
	}
}

func TestTempString(t *testing.T) {
	c := 37
	if got := tempString(&smart.Report{Temperature: &smart.Temperature{Current: &c}}); got != "37°C" {
		t.Errorf("temp = %q", got)
	}
	if got := tempString(&smart.Report{}); got != dash {
		t.Errorf("no temp = %q, want dash", got)
	}
}

func TestDriveKind(t *testing.T) {
	rpm := 7200
	zero := 0
	cases := []struct {
		name string
		r    smart.Report
		want string
	}{
		{"nvme", smart.Report{Device: smart.Device{Protocol: "NVMe"}}, "NVMe SSD"},
		{"hdd", smart.Report{Device: smart.Device{Protocol: "ATA"}, RotationRate: &rpm}, "HDD @ 7200 rpm"},
		{"ssd nil rpm", smart.Report{Device: smart.Device{Protocol: "ATA"}}, "SATA SSD"},
		{"ssd zero rpm", smart.Report{Device: smart.Device{Protocol: "ATA"}, RotationRate: &zero}, "SATA SSD"},
		{"other", smart.Report{Device: smart.Device{Protocol: "SCSI"}}, "SCSI"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := driveKind(&c.r); got != c.want {
				t.Errorf("driveKind = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAttrTextColor(t *testing.T) {
	if attrTextColor(smart.SeverityOK) != tcell.ColorDefault {
		t.Error("OK should be neutral (default)")
	}
	if attrTextColor(smart.SeverityCaution) != tcell.ColorYellow {
		t.Error("caution should be yellow")
	}
	if attrTextColor(smart.SeverityFailing) != tcell.ColorRed {
		t.Error("failing should be red")
	}
}

// TestTempMarkupOnlyTintsOutOfBand pins the "colour marks exceptions" rule: an
// in-band reading keeps the caller's colour so a healthy fleet is not a wall of
// green, and only a caution/failing reading is tinted.
func TestTempMarkupOnlyTintsOutOfBand(t *testing.T) {
	if got := tempMarkup(37); got != "37°C" {
		t.Errorf("in-band temp should carry no markup, got %q", got)
	}
	for _, c := range []int{55, 67} {
		got := tempMarkup(c)
		if !strings.Contains(got, "[") || !strings.Contains(got, "°C") {
			t.Errorf("out-of-band temp %d should be tinted, got %q", c, got)
		}
	}
}

// TestPctBarsShareOnePolarity pins the rule that a fuller bar always means
// healthier. The fleet shows endurance beside spare, and rendering "life used"
// directly gave adjacent columns opposite polarity in the same colour.
func TestPctBarsShareOnePolarity(t *testing.T) {
	// A nearly-new drive: 3% used, 100% spare. Both bars should be nearly full.
	life := pctBarUsed(3, smart.SeverityOK)
	spare := pctBar(100, smart.SeverityOK)
	if strings.Count(life, "█") < pctBarWidth-1 {
		t.Errorf("3%% used should render a nearly full bar, got %q", life)
	}
	if strings.Count(spare, "█") != pctBarWidth {
		t.Errorf("100%% spare should render a full bar, got %q", spare)
	}
	// A worn drive drains.
	if worn := pctBarUsed(95, smart.SeverityCaution); strings.Count(worn, "█") > 1 {
		t.Errorf("95%% used should render a nearly empty bar, got %q", worn)
	}
	// The number reported is still the "used" figure, not the remainder.
	if !strings.Contains(pctBarUsed(3, smart.SeverityOK), "3%") {
		t.Errorf("pctBarUsed should print the used percentage, got %q", life)
	}
}

// TestShortDeviceKeepsDistinguishingPart: a trimmed macOS IOService path must
// keep whole trailing components, not a mid-word character cut.
func TestShortDeviceKeepsDistinguishingPart(t *testing.T) {
	const apple = "IOService:/AppleARMPE/arm-io@10F00000/AppleH16GFamilyIO/ans@9600000/" +
		"AppleASCWrapV6/iop-ans-nub/RTBuddy(ANS2)/RTBuddyService/AppleANS3CGv2Controller/NS_01@1"
	got := shortDevice(apple, 30)
	if !strings.HasSuffix(got, "NS_01@1") {
		t.Errorf("shortDevice = %q, want it to end at a whole path component", got)
	}
	if len([]rune(got)) > 30 {
		t.Errorf("shortDevice = %q, %d runes, want <= 30", got, len([]rune(got)))
	}
	// A short name is returned untouched.
	if got := shortDevice("/dev/sda", 30); got != "/dev/sda" {
		t.Errorf("short name should pass through, got %q", got)
	}
	// A narrow budget still yields something identifying rather than an ellipsis.
	if got := shortDevice(apple, 11); !strings.Contains(got, "NS_01@1") {
		t.Errorf("narrow shortDevice = %q, want the namespace component", got)
	}
	// A name with no separators falls back to a tail trim.
	if got := shortDevice(strings.Repeat("x", 50), 10); len([]rune(got)) != 10 {
		t.Errorf("separator-less name = %q, want 10 runes", got)
	}
}

// TestHangingIndentBreaksLongTokens: a macOS IOService path is 150+ characters
// with no spaces, and callers disable tview's wrapping, so an unbreakable token
// has to be split here or it is simply cut at the border.
func TestHangingIndentBreaksLongTokens(t *testing.T) {
	const path = "IOService:/AppleARMPE/arm-io@10F00000/AppleH16GFamilyIO/ans@9600000/" +
		"AppleASCWrapV6/iop-ans-nub/RTBuddy(ANS2)/RTBuddyService/AppleANS3CGv2Controller/NS_01@1"
	line := "Device         " + path
	got := hangingIndent(line, 15, 40)
	lines := strings.Split(got, "\n")
	if len(lines) < 4 {
		t.Fatalf("a 150-character token should break across lines, got %d:\n%s", len(lines), got)
	}
	for _, l := range lines {
		if n := len([]rune(l)); n > 40 {
			t.Errorf("line is %d runes, wider than the pane: %q", n, l)
		}
	}
	if strings.Join(strings.Fields(strings.Join(lines, "")), "") != "Device"+path {
		t.Error("breaking the token lost or reordered part of it")
	}
}
