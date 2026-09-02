// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"math"
	"sort"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestDarkThemePinned pins every dark-theme role as an explicit hex value.
// The palette used to name tcell colours, which are sent to the terminal as
// palette indices: markup resolved them to RGB and every style path did not,
// so a role rendered two different colours on any terminal whose scheme is not
// the default. These are the RGB values those names already resolved to, with
// three deliberate departures from the pre-theming original, each noted below.
func TestDarkThemePinned(t *testing.T) {
	want := map[string]tcell.Color{
		// Was ColorBlack. A ground one step off #000 gives the palette
		// somewhere to build layers down from, and stops the app pasting a
		// black rectangle into a terminal that is not itself black.
		"Background": tcell.NewHexColor(0x0b0d10),
		"Accent":     tcell.NewHexColor(0x00ffff), // was ColorAqua
		"Muted":      tcell.NewHexColor(0x808080), // was ColorGray
		"OK":         tcell.NewHexColor(0x008000), // was ColorGreen
		"Caution":    tcell.NewHexColor(0xffff00), // was ColorYellow
		"Failing":    tcell.NewHexColor(0xff0000), // was ColorRed
		"Neutral":    tcell.NewHexColor(0xffffff), // was ColorWhite
		"Inverse":    tcell.NewHexColor(0x000000), // was ColorBlack
		// Was ColorDarkSlateGray (#2f4f4f), bright enough to drop Failing to
		// 2.23:1 and OK to 1.74:1 on the selected row.
		"SelectionBg":   tcell.NewHexColor(0x16202a),
		"SelectionFg":   tcell.NewHexColor(0xffffff), // was ColorWhite
		"BannerBg":      tcell.NewHexColor(0xffff00), // was ColorYellow
		"BarHealthy":    tcell.NewHexColor(0x008080), // was ColorTeal
		"ScrollArrow":   tcell.NewHexColor(0xffffff), // was ColorWhite
		"ListSecondary": tcell.NewHexColor(0x808080), // was ColorGreen, == OK
	}
	got := themeRoles(dark)
	if len(want) != len(got) {
		t.Fatalf("dark has %d roles but %d are pinned; pin the new one here", len(got), len(want))
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

// TestPaintedThemesAreHexOnly: a palette that paints its own ground must spell
// every role in RGB. A named colour resolves through the user's terminal
// scheme, so pairing one with a painted ground lets the two disagree — the
// defect dark carried. inheritingThemes are the deliberate exception: they take
// the terminal's ground, so resolving through its scheme is what makes them
// agree rather than what breaks them.
func TestPaintedThemesAreHexOnly(t *testing.T) {
	for name, th := range themes {
		if inheritingThemes[name] {
			continue
		}
		for role, c := range themeRoles(th) {
			if c != c.TrueColor() {
				t.Errorf("theme %q role %s is the named colour %v; a painted palette "+
					"must use tcell.NewHexColor so the terminal scheme cannot redefine it",
					name, role, c)
			}
		}
	}
}

// themeRoles lists a theme's colour roles by name, so the palette tests below
// cover a new role automatically.
func themeRoles(th Theme) map[string]tcell.Color {
	return map[string]tcell.Color{
		"Background":    th.Background,
		"Accent":        th.Accent,
		"Muted":         th.Muted,
		"OK":            th.OK,
		"Caution":       th.Caution,
		"Failing":       th.Failing,
		"Neutral":       th.Neutral,
		"Inverse":       th.Inverse,
		"SelectionBg":   th.SelectionBg,
		"SelectionFg":   th.SelectionFg,
		"BannerBg":      th.BannerBg,
		"BarHealthy":    th.BarHealthy,
		"ScrollArrow":   th.ScrollArrow,
		"ListSecondary": th.ListSecondary,
	}
}

// inheritingThemes are the palettes that take colours from the terminal rather
// than painting them: mono drops colour entirely, terminal keeps the ground and
// body colour and adds the severity vocabulary back as named colours. Neither
// can be measured for contrast, so every ratio test below skips them.
var inheritingThemes = map[string]bool{"mono": true, "terminal": true}

// inheritingNames lists them for an error message, in a stable order.
func inheritingNames() []string {
	out := make([]string, 0, len(inheritingThemes))
	for n := range inheritingThemes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TestThemesComplete asserts every theme names itself and assigns every role.
// ColorDefault is allowed only in mono, which defers to the terminal wholesale:
// a palette that paints its own ground has to paint every foreground too.
func TestThemesComplete(t *testing.T) {
	for name, th := range themes {
		if th.Name == "" {
			t.Errorf("theme %q has empty Name", name)
		}
		if th.Name != name {
			t.Errorf("theme registered as %q has Name %q", name, th.Name)
		}
		if inheritingThemes[name] {
			continue // these defer to the terminal on purpose
		}
		for role, c := range themeRoles(th) {
			if c == tcell.ColorDefault {
				t.Errorf("theme %q role %s is ColorDefault (only %v may degrade)",
					name, role, inheritingNames())
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
	// An RGB colour must render as #rrggbb that GetColor parses back identically.
	rgbTok := tag(tcell.NewHexColor(0x00ffff))
	if rgbTok == "-" || rgbTok[0] != '#' || len(rgbTok) != 7 {
		t.Fatalf("tag(#00ffff) = %q, want a #rrggbb token", rgbTok)
	}
	if back := tcell.GetColor(rgbTok); back.TrueColor() != tcell.NewHexColor(0x00ffff) {
		t.Errorf("GetColor(%q) = %v, want #00ffff", rgbTok, back)
	}
	// A named colour must render as its NAME and survive the round trip as the
	// same palette index — resolving it to RGB is what broke the dark theme.
	for c := range namedTags {
		tok := tag(c)
		if tok == "-" || tok[0] == '#' {
			t.Errorf("tag(%v) = %q, want a colour name", c, tok)
			continue
		}
		if back := tcell.GetColor(tok); back != c {
			t.Errorf("GetColor(%q) = %v, want the same palette colour %v", tok, back, c)
		}
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

// TestMonoInheritsTheTerminal pins mono's contract: every role, the ground
// included, defers to the terminal.
func TestMonoInheritsTheTerminal(t *testing.T) {
	for role, c := range themeRoles(mono) {
		if c != tcell.ColorDefault {
			t.Errorf("mono role %s is %v, want ColorDefault", role, c)
		}
	}
}

// TestTerminalInheritsGroundAndInk pins the terminal palette's contract: it
// takes the ground, the body colour and the selection from the terminal, and
// every colour it does supply is a NAMED one, so it resolves through the same
// scheme as the ground it sits on.
func TestTerminalInheritsGroundAndInk(t *testing.T) {
	inherited := []string{"Background", "Neutral", "SelectionBg", "SelectionFg"}
	roles := themeRoles(terminal)
	for _, role := range inherited {
		if roles[role] != tcell.ColorDefault {
			t.Errorf("terminal role %s is %v, want ColorDefault: it must come from the terminal",
				role, roles[role])
		}
	}
	for role, c := range roles {
		if c == tcell.ColorDefault {
			continue
		}
		if _, ok := namedTags[c]; !ok {
			t.Errorf("terminal role %s is %v, which tag() renders as RGB; use a colour "+
				"from namedTags so markup and style agree on the terminal's scheme", role, c)
		}
	}
	// Reverse video is the only highlight available without a known ground.
	setTheme(terminal)
	defer setTheme(dark)
	if _, _, attrs := selectedRowStyle(tcell.ColorDefault).Decompose(); attrs&tcell.AttrReverse == 0 {
		t.Error("terminal's selected row is not reverse video; with no SelectionBg " +
			"nothing else marks it")
	}
}

// TestForegroundsAreLegibleOnTheirBackground: every role that renders as text
// or glyphs sits on Background, so a palette that picks one carelessly is
// unreadable. 3:1 is the floor TestInverseIsLegibleOnItsFields already uses.
func TestForegroundsAreLegibleOnTheirBackground(t *testing.T) {
	const minRatio = 3.0
	on := []string{"Accent", "Muted", "OK", "Caution", "Failing", "Neutral",
		"ListSecondary", "BarHealthy", "ScrollArrow"}
	for name, th := range themes {
		roles := themeRoles(th)
		for _, role := range on {
			// A ColorDefault on either side resolves only in the terminal, so
			// there is no ratio to measure.
			if roles[role] == tcell.ColorDefault || th.Background == tcell.ColorDefault {
				continue
			}
			if got := contrastRatio(roles[role], th.Background); got < minRatio {
				t.Errorf("theme %q: %s on Background has contrast %.2f, want >= %.1f",
					name, role, got, minRatio)
			}
		}
	}
}

// TestSelectionIsVisibleOnBackground: the selected row is marked by its
// background alone, so the band must lift off the ground — subtly, since it is
// meant to tint a row rather than repaint it — while SelectionFg, the pin for
// rows whose own foreground is ColorDefault, stays plainly readable on it.
func TestSelectionIsVisibleOnBackground(t *testing.T) {
	const minBand, maxBand, minPin = 1.15, 3.0, 4.5
	for name, th := range themes {
		if th.Background == tcell.ColorDefault {
			continue // mono drops all colour by design
		}
		band := contrastRatio(th.SelectionBg, th.Background)
		if band < minBand {
			t.Errorf("theme %q: SelectionBg is %.2f against Background, want >= %.2f; "+
				"the selected row would be invisible", name, band, minBand)
		}
		if band > maxBand {
			t.Errorf("theme %q: SelectionBg is %.2f against Background, want <= %.1f; "+
				"the band repaints the row instead of tinting it", name, band, maxBand)
		}
		if got := contrastRatio(th.SelectionFg, th.SelectionBg); got < minPin {
			t.Errorf("theme %q: SelectionFg on SelectionBg has contrast %.2f, want >= %.1f",
				name, got, minPin)
		}
	}
}

// TestSeverityIsLegibleOnTheSelectionBand: the selected row keeps each cell's
// own foreground and repaints the ground with SelectionBg, so the severity
// colours are drawn on the band whenever the cursor is on a drive — which is
// exactly the row the user cares about. Nothing pinned this, and four palettes
// dropped a severity below the 3:1 floor every other pairing is held to; dark
// put both ends of its ramp there (Failing 2.23, OK 1.74).
func TestSeverityIsLegibleOnTheSelectionBand(t *testing.T) {
	const minRatio = 3.0
	for name, th := range themes {
		if inheritingThemes[name] {
			continue // no band to measure: the selection is reverse video
		}
		for _, r := range []struct {
			role string
			c    tcell.Color
		}{{"OK", th.OK}, {"Caution", th.Caution}, {"Failing", th.Failing}} {
			if got := contrastRatio(r.c, th.SelectionBg); got < minRatio {
				t.Errorf("theme %q: %s on SelectionBg has contrast %.2f, want >= %.1f; "+
					"selecting a drive must not dim its own health colour",
					name, r.role, got, minRatio)
			}
		}
	}
}

// TestMutedIsDimmerThanNeutral: Muted is the recessive voice — dashes, raw
// values, unfocused borders — so it has to read as quieter than body text
// while staying legible on the ground.
func TestMutedIsDimmerThanNeutral(t *testing.T) {
	const minGap = 1.8
	for name, th := range themes {
		if th.Neutral == tcell.ColorDefault || th.Background == tcell.ColorDefault {
			continue // one side resolves only in the terminal; no ratio to measure
		}
		neutral := contrastRatio(th.Neutral, th.Background)
		muted := contrastRatio(th.Muted, th.Background)
		if neutral < muted*minGap {
			t.Errorf("theme %q: Neutral is %.2f and Muted %.2f against Background; "+
				"Muted must be at least %.1fx quieter to read as recessive",
				name, neutral, muted, minGap)
		}
	}
}

// darkGroundMax and lightGroundMin bound the two bands a ground may sit in.
// The gap between them is the point: TestGroundsAreDecisivelyDarkOrLight keeps
// palettes out of it, so none can be mid-tone and dodge the light-ground floor.
const darkGroundMax, lightGroundMin = 0.15, 0.5

// TestGroundsAreDecisivelyDarkOrLight: the higher contrast floor below is keyed
// off the ground's luminance, so a ground that commits to neither ink nor paper
// would silently take the lower one.
func TestGroundsAreDecisivelyDarkOrLight(t *testing.T) {
	for name, th := range themes {
		if th.Background == tcell.ColorDefault {
			continue // mono inherits the terminal's, whatever it is
		}
		l := relLuminance(th.Background)
		if l > darkGroundMax && l < lightGroundMin {
			t.Errorf("theme %q: Background luminance %.3f is mid-tone (want <= %.2f or >= %.2f); "+
				"pick a ground that is plainly ink or plainly paper", name, l, darkGroundMax, lightGroundMin)
		}
	}
}

// TestLightGroundsClearAHigherFloor: on a light ground the 3:1 UI floor is not
// enough. Dark ink on a bright field loses to glare, and a light palette has no
// terminal default to fall back on — every role is one the palette itself
// picked.
func TestLightGroundsClearAHigherFloor(t *testing.T) {
	const minRatio = 4.0
	on := []string{"Accent", "Muted", "OK", "Caution", "Failing", "Neutral",
		"ListSecondary", "BarHealthy", "ScrollArrow"}
	light := 0
	for name, th := range themes {
		if th.Background == tcell.ColorDefault || relLuminance(th.Background) < lightGroundMin {
			continue
		}
		light++
		roles := themeRoles(th)
		for _, role := range on {
			if got := contrastRatio(roles[role], th.Background); got < minRatio {
				t.Errorf("theme %q: %s on its light Background has contrast %.2f, want >= %.1f",
					name, role, got, minRatio)
			}
		}
	}
	if light != 2 {
		t.Errorf("found %d light-ground themes, want 2 (daylight, parchment); "+
			"a new one must be tuned to this floor too", light)
	}
}
