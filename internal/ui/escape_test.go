// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// TestEscNeutralisesColorTags is a regression guard for the markup-injection
// finding: drive-controlled free text must be escaped so a hostile drive cannot
// inject tview colour tags and spoof the health display.
func TestEscNeutralisesColorTags(t *testing.T) {
	got := esc("[red]PWNED[-]")
	if strings.Contains(got, "[red]") {
		t.Fatalf("esc left a live color tag intact: %q", got)
	}
	if !strings.Contains(got, "PWNED") {
		t.Fatalf("esc dropped the literal text: %q", got)
	}
	if got != tview.Escape("[red]PWNED[-]") {
		t.Fatalf("esc = %q, want tview.Escape output", got)
	}
}

// TestIdentityTextEscapesDriveFields confirms the Overview identity panel escapes
// the drive-controlled identity fields rather than emitting them as live markup.
func TestIdentityTextEscapesDriveFields(t *testing.T) {
	const hostile = "[green]HEALTHY[-]"
	r := &smart.Report{
		ModelName:       hostile,
		SerialNumber:    hostile,
		FirmwareVersion: hostile,
	}
	r.SmartStatus.Passed = true // a genuinely healthy drive

	// Both column layouts, so neither formatting path can leak a live tag.
	out := identityText(r, 40) + identityText(r, 120)
	// The escaped form must appear...
	if !strings.Contains(out, tview.Escape(hostile)) {
		t.Fatalf("identityText did not escape the hostile field; got:\n%s", out)
	}
	// ...and the only live [green] tags must be the real verdict styling, not the
	// three injected copies. The verdict adds exactly one "[green::b]" tag; an
	// unescaped field would add bare "[green]" occurrences.
	if n := strings.Count(out, "[green]"); n != 0 {
		t.Fatalf("identityText emitted %d live [green] tags from drive data; got:\n%s", n, out)
	}
}

// TestEscFoldsControlCharacters guards the second way a drive-controlled field
// can forge structure: every caller writes the escaped value into a line of its
// own, so a newline inside a model name would add free-standing lines to the
// identity panel that look like real key/value rows (a fake "SMART
// self-assessment: PASSED" under the genuine FAILED verdict). Whether a raw
// control character survives smartctl's JSON encoder is unverified — this is
// defence in depth, and it also keeps tabs and stray C1 bytes out of the column
// arithmetic.
func TestEscFoldsControlCharacters(t *testing.T) {
	got := esc("Disk\nSMART self-assessment: PASSED")
	if strings.ContainsAny(got, "\n\r\t") {
		t.Fatalf("esc passed a control character through: %q", got)
	}
	if !strings.Contains(got, "SMART self-assessment: PASSED") {
		t.Fatalf("esc dropped the literal text: %q", got)
	}
	for _, r := range []rune{0x00, 0x07, 0x0b, 0x1b, 0x7f, 0x85, 0x9b} {
		if out := esc("a" + string(r) + "b"); out != "a b" {
			t.Errorf("esc(%q) = %q, want %q", r, out, "a b")
		}
	}
	// Printable text — multi-byte runes included — is untouched.
	for _, in := range []string{"Samsung SSD 990 PRO 2TB", "温度 37°C", ""} {
		if got := esc(in); got != in {
			t.Errorf("esc(%q) = %q, want it unchanged", in, got)
		}
	}
}
