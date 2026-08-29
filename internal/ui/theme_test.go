// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"math"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestDarkThemeUnchanged pins every dark-theme role to its pre-theming colour,
// with one deliberate exception: ListSecondary moved off green (it equalled OK).
func TestDarkThemeUnchanged(t *testing.T) {
	want := map[string]tcell.Color{
		"Accent":        tcell.ColorAqua,
		"Muted":         tcell.ColorGray,
		"OK":            tcell.ColorGreen,
		"Caution":       tcell.ColorYellow,
		"Failing":       tcell.ColorRed,
		"Neutral":       tcell.ColorDefault,
		"Inverse":       tcell.ColorBlack,
		"SelectionBg":   tcell.ColorDarkSlateGray,
		"SelectionFg":   tcell.ColorWhite,
		"BannerBg":      tcell.ColorYellow,
		"BarHealthy":    tcell.ColorTeal,
		"ScrollArrow":   tcell.ColorWhite,
		"ListSecondary": tcell.ColorGray,
	}
	got := map[string]tcell.Color{
		"Accent":        dark.Accent,
		"Muted":         dark.Muted,
		"OK":            dark.OK,
		"Caution":       dark.Caution,
		"Failing":       dark.Failing,
		"Neutral":       dark.Neutral,
		"Inverse":       dark.Inverse,
		"SelectionBg":   dark.SelectionBg,
		"SelectionFg":   dark.SelectionFg,
		"BannerBg":      dark.BannerBg,
		"BarHealthy":    dark.BarHealthy,
		"ScrollArrow":   dark.ScrollArrow,
		"ListSecondary": dark.ListSecondary,
	}
	for role, w := range want {
		if got[role] != w {
			t.Errorf("dark.%s = %v, want %v", role, got[role], w)
		}
	}
	if dark.Name != "dark" {
		t.Errorf("dark.Name = %q, want %q", dark.Name, "dark")
	}
}

// TestThemesComplete asserts every theme names itself and assigns every role.
// ColorDefault is allowed only for mono (all roles) and Neutral (anywhere).
func TestThemesComplete(t *testing.T) {
	roles := func(th Theme) map[string]tcell.Color {
		return map[string]tcell.Color{
			"Accent":        th.Accent,
			"Muted":         th.Muted,
			"OK":            th.OK,
			"Caution":       th.Caution,
			"Failing":       th.Failing,
			"Inverse":       th.Inverse,
			"SelectionBg":   th.SelectionBg,
			"SelectionFg":   th.SelectionFg,
			"BannerBg":      th.BannerBg,
			"BarHealthy":    th.BarHealthy,
			"ScrollArrow":   th.ScrollArrow,
			"ListSecondary": th.ListSecondary,
			// Neutral is intentionally omitted: ColorDefault is a valid value for it.
		}
	}
	for name, th := range themes {
		if th.Name == "" {
			t.Errorf("theme %q has empty Name", name)
		}
		if th.Name != name {
			t.Errorf("theme registered as %q has Name %q", name, th.Name)
		}
		if name == "mono" {
			continue // mono is intentionally all-default
		}
		for role, c := range roles(th) {
			if c == tcell.ColorDefault {
				t.Errorf("theme %q role %s is ColorDefault (only mono may degrade)", name, role)
			}
		}
	}
}

// TestThemeCycleCoverage checks themeCycle and themes agree: every cycle entry
// is a registered theme and vice versa, so the 'T' key visits each exactly once.
func TestThemeCycleCoverage(t *testing.T) {
	if len(themeCycle) != len(themes) {
		t.Fatalf("themeCycle has %d entries, themes has %d", len(themeCycle), len(themes))
	}
	seen := map[string]bool{}
	for _, n := range themeCycle {
		if !HasTheme(n) {
			t.Errorf("themeCycle entry %q is not a registered theme", n)
		}
		if seen[n] {
			t.Errorf("themeCycle entry %q appears more than once", n)
		}
		seen[n] = true
	}
}

func TestTagRoundTrip(t *testing.T) {
	if got := tag(tcell.ColorDefault); got != "-" {
		t.Errorf("tag(ColorDefault) = %q, want %q", got, "-")
	}
	// A real colour must render as #rrggbb that GetColor parses back identically.
	tok := tag(tcell.ColorAqua)
	if tok == "-" || tok[0] != '#' || len(tok) != 7 {
		t.Fatalf("tag(ColorAqua) = %q, want a #rrggbb token", tok)
	}
	if back := tcell.GetColor(tok); back.TrueColor() != tcell.ColorAqua.TrueColor() {
		t.Errorf("GetColor(%q) = %v, want %v", tok, back, tcell.ColorAqua)
	}
}

func TestHasTheme(t *testing.T) {
	if !HasTheme("dark") {
		t.Error(`HasTheme("dark") = false, want true`)
	}
	if HasTheme("bogus") {
		t.Error(`HasTheme("bogus") = true, want false`)
	}
}

func TestNextThemeNameWraps(t *testing.T) {
	// Walk the whole cycle and confirm it returns to the start.
	start := themeCycle[0]
	cur := start
	for range themeCycle {
		cur = nextThemeName(cur)
	}
	if cur != start {
		t.Errorf("cycling %d times from %q landed on %q, want %q", len(themeCycle), start, cur, start)
	}
	// Each step advances to the next distinct theme.
	if got := nextThemeName("dark"); got == "dark" || !HasTheme(got) {
		t.Errorf("nextThemeName(\"dark\") = %q, want a different registered theme", got)
	}
	// An unknown current theme restarts the cycle.
	if got := nextThemeName("bogus"); got != themeCycle[0] {
		t.Errorf("nextThemeName(\"bogus\") = %q, want %q", got, themeCycle[0])
	}
}

// TestSetThemeUpdatesDash confirms dash tracks the active theme's muted colour,
// then restores the dark default so other tests/usage see the normal state.
func TestSetThemeUpdatesDash(t *testing.T) {
	defer setTheme(dark)

	setTheme(dark)
	if want := mutedTag() + "—[-]"; dash != want {
		t.Errorf("dash = %q, want %q", dash, want)
	}
	setTheme(mono)
	if want := "[-]—[-]"; dash != want { // mono muted is ColorDefault → "-"
		t.Errorf("mono dash = %q, want %q", dash, want)
	}
}

// TestListSecondaryIsNotOK guards the reason ListSecondary moved off green: the
// drive-list metadata line renders on every drive regardless of health, so it
// must not carry the colour that means "healthy" in any theme.
func TestListSecondaryIsNotOK(t *testing.T) {
	for name, th := range themes {
		if th.ListSecondary == tcell.ColorDefault {
			continue // mono drops all colour by design
		}
		if th.ListSecondary == th.OK {
			t.Errorf("theme %q: ListSecondary equals OK (%v); a failing drive's "+
				"metadata line would render in the healthy colour", name, th.OK)
		}
	}
}

// TestSeverityRampEscalates: every theme's severity ramp must look hotter as
// it worsens, never fainter.
func TestSeverityRampEscalates(t *testing.T) {
	lum := func(c tcell.Color) float64 {
		h := c.TrueColor().Hex()
		r, g, b := float64((h>>16)&0xff), float64((h>>8)&0xff), float64(h&0xff)
		return 0.2126*r + 0.7152*g + 0.0722*b
	}
	for name, th := range themes {
		if th.OK == tcell.ColorDefault {
			continue // mono drops all colour by design
		}
		if name != "phosphor" {
			// Only a monochrome palette has to encode severity by intensity; the
			// others carry it in hue, where luminance ordering means nothing.
			continue
		}
		ok, caution, failing := lum(th.OK), lum(th.Caution), lum(th.Failing)
		if !(ok < caution && caution < failing) {
			t.Errorf("theme %q severity does not escalate: OK %.0f, Caution %.0f, Failing %.0f",
				name, ok, caution, failing)
		}
		// And it must escalate by intensity, not by fading toward white: the
		// green channel leads and red/blue stay low.
		for _, c := range []tcell.Color{th.OK, th.Caution, th.Failing} {
			h := c.TrueColor().Hex()
			r, g, b := (h>>16)&0xff, (h>>8)&0xff, h&0xff
			if r > g/2 || b > g/2 {
				t.Errorf("theme %q colour #%06x is washing out: r=%d b=%d against g=%d",
					name, h, r, b, g)
			}
		}
	}
}

// relLuminance is the WCAG relative luminance of a colour.
func relLuminance(c tcell.Color) float64 {
	h := c.TrueColor().Hex()
	lin := func(v int32) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin((h>>16)&0xff) + 0.7152*lin((h>>8)&0xff) + 0.0722*lin(h&0xff)
}

// contrastRatio is the WCAG contrast ratio between two colours.
func contrastRatio(a, b tcell.Color) float64 {
	la, lb := relLuminance(a), relLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestInverseIsLegibleOnItsFields: Inverse is the only foreground drawn on
// Accent (the active-tab pill) and on BannerBg (the root warning), so a palette
// that picks it carelessly renders both unreadable. 3:1 is the WCAG floor for
// this kind of large/UI text.
func TestInverseIsLegibleOnItsFields(t *testing.T) {
	const minRatio = 3.0
	for name, th := range themes {
		if th.Inverse == tcell.ColorDefault {
			continue // mono drops all colour by design
		}
		for _, f := range []struct {
			role string
			bg   tcell.Color
		}{{"Accent", th.Accent}, {"BannerBg", th.BannerBg}} {
			if got := contrastRatio(th.Inverse, f.bg); got < minRatio {
				t.Errorf("theme %q: Inverse on %s has contrast %.2f, want >= %.1f",
					name, f.role, got, minRatio)
			}
		}
	}
}

// simulateDeuteranopia approximates how a deuteranope sees a colour (Viénot,
// Brettel & Mollon 1999), for TestBeaconSurvivesColourBlindness.
func simulateDeuteranopia(c tcell.Color) (float64, float64, float64) {
	h := c.TrueColor().Hex()
	lin := func(v int32) float64 { return math.Pow(float64(v)/255, 2.2) }
	r, g, b := lin((h>>16)&0xff), lin((h>>8)&0xff), lin(h&0xff)
	// RGB → LMS.
	l := 17.8824*r + 43.5161*g + 4.11935*b
	s := 0.0299566*r + 0.184309*g + 1.46709*b
	// The M cone is dropped entirely: it is reconstructed from L and S.
	m := 0.494207*l + 1.24827*s
	// LMS → RGB.
	return 0.080944*l - 0.130504*m + 0.116721*s,
		-0.0102485*l + 0.0540194*m - 0.113615*s,
		-0.000365294*l - 0.00412163*m + 0.693513*s
}

// TestBeaconSurvivesColourBlindness is the point of the beacon palette: its
// severity ramp must stay separable for a deuteranope, where dark's green/red
// pair collapses. The threshold is calibrated to reject that pair.
func TestBeaconSurvivesColourBlindness(t *testing.T) {
	const minDist = 0.25
	dist := func(a, b tcell.Color) float64 {
		ar, ag, ab := simulateDeuteranopia(a)
		br, bg, bb := simulateDeuteranopia(b)
		return math.Sqrt((ar-br)*(ar-br) + (ag-bg)*(ag-bg) + (ab-bb)*(ab-bb))
	}
	pairs := []struct {
		a, b string
		ca   tcell.Color
		cb   tcell.Color
	}{
		{"OK", "Caution", beacon.OK, beacon.Caution},
		{"Caution", "Failing", beacon.Caution, beacon.Failing},
		{"OK", "Failing", beacon.OK, beacon.Failing},
	}
	for _, p := range pairs {
		if got := dist(p.ca, p.cb); got < minDist {
			t.Errorf("beacon %s vs %s: simulated deuteranope distance %.3f, want >= %.2f",
				p.a, p.b, got, minDist)
		}
	}
	// Calibration: the default palette's green/red pair is what beacon exists
	// to fix, so it must fall below the threshold this test enforces.
	if got := dist(dark.OK, dark.Failing); got >= minDist {
		t.Errorf("dark OK vs Failing: simulated distance %.3f is above the %.2f "+
			"threshold — the test no longer proves beacon does anything", got, minDist)
	}
}
