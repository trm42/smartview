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

	out := identityText(r)
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
