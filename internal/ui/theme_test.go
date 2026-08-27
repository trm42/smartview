// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestDarkThemeUnchanged pins every role of the default (dark) theme to the
// colour the UI hard-coded before theming existed, so the default behaviour
// can't silently drift.
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
		"ListSecondary": tcell.ColorGreen,
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

// TestThemesComplete asserts every registered theme names itself and assigns
// every role. ColorDefault is allowed only for the whole mono theme (its point
// is to drop colour) and for the Neutral role anywhere (healthy text renders in
// the terminal default, exactly as the original dark palette did); every other
// role of a coloured theme must be a real colour so nothing silently falls back.
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
