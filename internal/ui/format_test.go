// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

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
	if b, ok := fallback.CapacityBytes(); !ok || b != total {
		t.Errorf("nvme fallback = %d,%v", b, ok)
	}
	if got := capacityString(&smart.Report{}); got != dash {
		t.Errorf("no capacity = %q, want dash", got)
	}
}

func TestTempCell(t *testing.T) {
	c := 37
	if got := tempCell(&smart.Report{Temperature: &smart.Temperature{Current: &c}}); got != "37°C" {
		t.Errorf("temp = %q", got)
	}
	if got := tempCell(&smart.Report{}); got != dash {
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

// TestAttrTextColor pins "colour marks exceptions" for attribute rows: a
// healthy row takes the body colour and only a row needing attention is tinted.
// Asserted against the roles, not literals, so it holds in every theme.
func TestAttrTextColor(t *testing.T) {
	cases := []struct {
		sev  smart.Severity
		role string
		want tcell.Color
	}{
		{smart.SeverityOK, "Neutral", activeTheme.Neutral},
		{smart.SeverityCaution, "Caution", activeTheme.Caution},
		{smart.SeverityFailing, "Failing", activeTheme.Failing},
	}
	for _, c := range cases {
		if got := attrTextColor(c.sev); got != c.want {
			t.Errorf("attrTextColor(%v) = %v, want %s (%v)", c.sev, got, c.role, c.want)
		}
	}
	if attrTextColor(smart.SeverityOK) == attrTextColor(smart.SeverityCaution) ||
		attrTextColor(smart.SeverityOK) == attrTextColor(smart.SeverityFailing) {
		t.Error("a healthy row is tinted the same as one needing attention")
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

// TestHangingIndentSplitsOnDisplayColumns pins the fix for a byte-vs-display
// mix-up: valueCol is a display measure, so a key wrapped in zero-width style
// tags must still be cut at its own column, and the value must survive the
// re-wrap verbatim (padding, colour tags and multi-byte runes included).
func TestHangingIndentSplitsOnDisplayColumns(t *testing.T) {
	const valueCol = 15
	// The Overview identity row: 12 bytes of markup around a 14-cell key.
	key := "[::b]" + padRight("Device", 14) + "[-:-:-] "
	if w := tview.TaggedStringWidth(key); w != valueCol {
		t.Fatalf("test premise: key column is %d cells, want %d", w, valueCol)
	}

	t.Run("ioservice path", func(t *testing.T) {
		const path = "IOService:/AppleARMPE/arm-io@10F00000/AppleH16GFamilyIO/ans@9600000/" +
			"AppleASCWrapV6/iop-ans-nub/RTBuddy(ANS2)/RTBuddyService/AppleANS3CGv2Controller/NS_01@1"
		got := hangingIndent(key+path, valueCol, 60)
		lines := strings.Split(got, "\n")
		if len(lines) < 3 {
			t.Fatalf("a 150-character path should wrap, got:\n%s", got)
		}
		// The key stays whole on the first line, and every continuation hangs
		// under the value column — the byte slice used to cut mid-key and the
		// field re-join used to shift wrapped rows left of unwrapped ones.
		if !strings.HasPrefix(lines[0], key) {
			t.Errorf("first line lost the key column: %q", lines[0])
		}
		for _, l := range lines[1:] {
			if n := len(l) - len(strings.TrimLeft(l, " ")); n != valueCol {
				t.Errorf("continuation indent = %d, want %d: %q", n, valueCol, l)
			}
			if w := tview.TaggedStringWidth(l); w > 60 {
				t.Errorf("line is %d cells wide, wider than the pane: %q", w, l)
			}
		}
		if rejoined := strings.ReplaceAll(strings.Join(lines, ""), " ", ""); rejoined != strings.ReplaceAll(key+path, " ", "") {
			t.Errorf("wrapping altered the text:\n%s", got)
		}
	})

	t.Run("colour tags survive", func(t *testing.T) {
		value := "193.6 TB " + cautionTag() + "(378186418521 sectors, checked twice)[-]"
		got := hangingIndent(key+value, valueCol, 50)
		if !strings.Contains(got, cautionTag()) || !strings.Contains(got, "[-]") {
			t.Errorf("a colour tag was mangled by the re-wrap:\n%s", got)
		}
		// The reset tag is zero-width markup, not a word: it must not be moved,
		// duplicated or dropped.
		if n := strings.Count(got, "[-]"); n != 1 {
			t.Errorf("reset tag appears %d times, want 1:\n%s", n, got)
		}
		for _, l := range strings.Split(got, "\n") {
			if w := tview.TaggedStringWidth(l); w > 50 {
				t.Errorf("line is %d cells wide: %q", w, l)
			}
		}
	})

	t.Run("multi-byte runes", func(t *testing.T) {
		value := strings.Repeat("温度", 12) + " 37°C"
		got := hangingIndent(key+value, valueCol, 40)
		if !strings.Contains(got, "37°C") {
			t.Errorf("multi-byte tail was lost:\n%s", got)
		}
		for _, l := range strings.Split(got, "\n") {
			if w := tview.TaggedStringWidth(l); w > 40 {
				t.Errorf("line is %d cells wide (wide runes count double): %q", w, l)
			}
			if strings.ContainsRune(l, '\uFFFD') {
				t.Errorf("a rune was cut in half: %q", l)
			}
		}
	})

	t.Run("no wrap needed is byte-identical", func(t *testing.T) {
		for _, line := range []string{
			key + "/dev/sda",
			key,
			"[::b]Section[-:-:-]",
			"",
			key + "one\n" + key + "two",
		} {
			if got := hangingIndent(line, valueCol, 120); got != line {
				t.Errorf("a line that fits was rewritten:\n%q\n%q", line, got)
			}
		}
	})
}

// TestSplitAtWidth pins the display-column cut splitAtWidth exists for: markup
// is worth no cells, an escaped bracket is worth its own, and a cut may not land
// inside either.
func TestSplitAtWidth(t *testing.T) {
	head, tail := splitAtWidth("[::b]"+padRight("Model", 14)+"[-:-:-] Samsung", 15)
	if tview.TaggedStringWidth(head) != 15 {
		t.Errorf("head = %q, %d cells, want 15", head, tview.TaggedStringWidth(head))
	}
	if tail != "Samsung" {
		t.Errorf("tail = %q, want %q", tail, "Samsung")
	}
	// An escaped tag from a hostile drive is literal text, and the cut must not
	// land inside the "[]" that keeps it inert.
	escaped := tview.Escape("[red]X[-]") // renders as the 9 literal cells "[red]X[-]"
	head, tail = splitAtWidth(escaped+"tail", 4)
	if head+tail != escaped+"tail" {
		t.Errorf("split lost bytes: %q + %q", head, tail)
	}
	if tview.TaggedStringWidth(head)+tview.TaggedStringWidth(tail) != tview.TaggedStringWidth(escaped+"tail") {
		t.Errorf("split severed an escape sequence: %q | %q", head, tail)
	}
	// Narrower than the column: returned whole, with nothing to hang.
	if head, tail = splitAtWidth("short", 40); head != "short" || tail != "" {
		t.Errorf("short split = %q, %q", head, tail)
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
