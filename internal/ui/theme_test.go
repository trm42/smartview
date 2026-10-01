// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestDarkThemePinned pins every dark role in hex.
func TestDarkThemePinned(t *testing.T) {
	want := map[string]tcell.Color{
		"Background":    tcell.NewHexColor(0x0b0d10),
		"Accent":        tcell.NewHexColor(0x00ffff),
		"Muted":         tcell.NewHexColor(0x808080),
		"OK":            tcell.NewHexColor(0x008000),
		"Caution":       tcell.NewHexColor(0xffff00),
		"Failing":       tcell.NewHexColor(0xff0000),
		"Neutral":       tcell.NewHexColor(0xffffff),
		"Inverse":       tcell.NewHexColor(0x000000),
		"SelectionBg":   tcell.NewHexColor(0x16202a),
		"SelectionFg":   tcell.NewHexColor(0xffffff),
		"BannerBg":      tcell.NewHexColor(0xffff00),
		"BarHealthy":    tcell.NewHexColor(0x008080),
		"ScrollArrow":   tcell.NewHexColor(0xffffff),
		"ListSecondary": tcell.NewHexColor(0x808080),
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
// every role in RGB, or the terminal scheme can redefine a named colour.
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

// inheritingThemes take colours from the terminal, so every ratio test skips them.
var inheritingThemes = map[string]bool{"mono": true, "terminal": true}

// TestThemesComplete asserts every theme has a unique name and, unless it
// inherits from the terminal, assigns every role.
func TestThemesComplete(t *testing.T) {
	if len(themes) != len(themeList) {
		t.Errorf("themeList has %d palettes but %d distinct names", len(themeList), len(themes))
	}
	for name, th := range themes {
		if name == "" {
			t.Error("a theme has an empty Name")
		}
		if inheritingThemes[name] {
			continue
		}
		for role, c := range themeRoles(th) {
			if c == tcell.ColorDefault {
				t.Errorf("theme %q role %s is ColorDefault (only %v may degrade)",
					name, role, slices.Sorted(maps.Keys(inheritingThemes)))
			}
		}
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
	// A named colour must render as its NAME and round-trip to the same palette index.
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

func TestStepThemeNameWraps(t *testing.T) {
	// Walk the whole cycle and confirm it returns to the start.
	start := themeCycle[0]
	cur := start
	for range themeCycle {
		cur = stepThemeName(cur, 1)
	}
	if cur != start {
		t.Errorf("cycling %d times from %q landed on %q, want %q", len(themeCycle), start, cur, start)
	}
	// Each step advances to the next distinct theme.
	if got := stepThemeName("dark", 1); got == "dark" || !HasTheme(got) {
		t.Errorf("stepThemeName(\"dark\", 1) = %q, want a different registered theme", got)
	}
	// Stepping back undoes a step forward, from every theme.
	for _, name := range themeCycle {
		if got := stepThemeName(stepThemeName(name, 1), -1); got != name {
			t.Errorf("forward then back from %q landed on %q", name, got)
		}
	}
	// Backwards off the head wraps to the tail.
	last := themeCycle[len(themeCycle)-1]
	if got := stepThemeName(start, -1); got != last {
		t.Errorf("stepThemeName(%q, -1) = %q, want %q", start, got, last)
	}
	// An unknown current theme restarts the cycle, whichever way it steps.
	for _, delta := range []int{1, -1} {
		if got := stepThemeName("bogus", delta); got != start {
			t.Errorf("stepThemeName(\"bogus\", %d) = %q, want %q", delta, got, start)
		}
	}
}

// TestSetThemeUpdatesDash confirms dash tracks the active theme's muted colour.
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

// TestListSecondaryIsNotOK: the drive-list metadata line renders on every
// drive, so it must not carry the healthy colour.
func TestListSecondaryIsNotOK(t *testing.T) {
	for name, th := range themes {
		if th.ListSecondary == tcell.ColorDefault {
			continue // inheriting palette
		}
		if th.ListSecondary == th.OK {
			t.Errorf("theme %q: ListSecondary equals OK (%v); a failing drive's "+
				"metadata line would render in the healthy colour", name, th.OK)
		}
	}
}

// TestSeverityRampEscalates: phosphor, the monochrome palette that encodes
// severity by intensity, must look hotter as it worsens, never fainter.
func TestSeverityRampEscalates(t *testing.T) {
	lum := func(c tcell.Color) float64 {
		h := c.TrueColor().Hex()
		r, g, b := float64((h>>16)&0xff), float64((h>>8)&0xff), float64(h&0xff)
		return 0.2126*r + 0.7152*g + 0.0722*b
	}
	th := phosphor
	ok, caution, failing := lum(th.OK), lum(th.Caution), lum(th.Failing)
	if !(ok < caution && caution < failing) {
		t.Errorf("phosphor severity does not escalate: OK %.0f, Caution %.0f, Failing %.0f",
			ok, caution, failing)
	}
	// Escalate by intensity, not by fading toward white: green leads, red/blue stay low.
	for _, c := range []tcell.Color{th.OK, th.Caution, th.Failing} {
		h := c.TrueColor().Hex()
		r, g, b := (h>>16)&0xff, (h>>8)&0xff, h&0xff
		if r > g/2 || b > g/2 {
			t.Errorf("phosphor colour #%06x is washing out: r=%d b=%d against g=%d",
				h, r, b, g)
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
			continue // inheriting palette
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

// foregroundRoles are the roles drawn as text or glyphs on Background.
var foregroundRoles = []string{"Accent", "Muted", "OK", "Caution", "Failing", "Neutral",
	"ListSecondary", "BarHealthy", "ScrollArrow"}

// TestForegroundsAreLegibleOnTheirBackground holds every foreground role to 3:1 on the ground.
func TestForegroundsAreLegibleOnTheirBackground(t *testing.T) {
	const minRatio = 3.0
	for name, th := range themes {
		roles := themeRoles(th)
		for _, role := range foregroundRoles {
			// ColorDefault resolves only in the terminal; nothing to measure.
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
			continue // inheriting palette
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
// own foreground on SelectionBg, so severity must clear 3:1 there too.
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
			continue // inheriting palette
		}
		l := relLuminance(th.Background)
		if l > darkGroundMax && l < lightGroundMin {
			t.Errorf("theme %q: Background luminance %.3f is mid-tone (want <= %.2f or >= %.2f); "+
				"pick a ground that is plainly ink or plainly paper", name, l, darkGroundMax, lightGroundMin)
		}
	}
}

// TestLightGroundsClearAHigherFloor: on a light ground dark ink loses to glare, so the floor is 4:1.
func TestLightGroundsClearAHigherFloor(t *testing.T) {
	const minRatio = 4.0
	light := 0
	for name, th := range themes {
		if th.Background == tcell.ColorDefault || relLuminance(th.Background) < lightGroundMin {
			continue
		}
		light++
		roles := themeRoles(th)
		for _, role := range foregroundRoles {
			if got := contrastRatio(roles[role], th.Background); got < minRatio {
				t.Errorf("theme %q: %s on its light Background has contrast %.2f, want >= %.1f",
					name, role, got, minRatio)
			}
		}
	}
	if light != 6 {
		t.Errorf("found %d light-ground themes, want 6 (daylight, parchment, sorbet, "+
			"marigold, seafoam, sky); a new one must be tuned to this floor too", light)
	}
}

// TestTruecolorTermFollowsTheEnvironment runs in a child process: terminfo's entry cache is process-global.
func TestTruecolorTermFollowsTheEnvironment(t *testing.T) {
	if os.Getenv("SMARTVIEW_TRUECOLOR_CHILD") == "1" {
		fmt.Printf("verdict:%v\n", truecolorTerm("xterm-256color"))
		return
	}
	for _, tc := range []struct{ colorterm, want string }{
		{"", "verdict:false"}, // the SSH case: TERM crosses over, COLORTERM does not
		{"truecolor", "verdict:true"},
	} {
		cmd := exec.Command(os.Args[0], "-test.run=TestTruecolorTermFollowsTheEnvironment")
		// Cmd.Env keeps the last of any duplicate key, so these win.
		cmd.Env = append(os.Environ(), "SMARTVIEW_TRUECOLOR_CHILD=1",
			"COLORTERM="+tc.colorterm, "TCELL_TRUECOLOR=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child with COLORTERM=%q: %v\n%s", tc.colorterm, err, out)
		}
		if !strings.Contains(string(out), tc.want) {
			t.Errorf("xterm-256color with COLORTERM=%q: want %s, got:\n%s",
				tc.colorterm, tc.want, out)
		}
	}
}
