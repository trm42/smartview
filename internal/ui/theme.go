// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Theme is smartview's colour palette, expressed as semantic roles rather than
// raw colours so the same widgets render under several built-in palettes. The
// source of truth is the tcell.Color for each role (needed for SetBorderColor /
// SetTextColor / tcell.Style); the tview markup tags are derived on demand by
// the tag helpers below rather than stored, so they can't drift from the colour.
//
// The active palette lives in the package-level activeTheme, read by every
// colour helper. Like App.reports/history it is touched only on the tview
// event-loop goroutine (all widget mutation happens inside QueueUpdateDraw), so
// it needs no mutex.
type Theme struct {
	Name        string
	Accent      tcell.Color // focused border, key hints, spinner, active tab, table header
	Muted       tcell.Color // dash, "… N more", raw values, scanning glyph, unfocused border
	OK          tcell.Color // SeverityOK
	Caution     tcell.Color // SeverityCaution
	Failing     tcell.Color // SeverityFailing
	Neutral     tcell.Color // healthy attribute-row text
	Inverse     tcell.Color // text drawn ON Accent / BannerBg
	SelectionBg tcell.Color // selected table-row background
	SelectionFg tcell.Color // foreground pin for neutral selected rows
	BannerBg    tcell.Color // root-warning banner background
	BarHealthy  tcell.Color // FARM per-head healthy bar
	ScrollArrow tcell.Color // scroll ▲/▼ arrows
}

// tag renders a colour as a tview markup token (without the surrounding
// brackets). ColorDefault or an invalid colour yields "-", tview's reset token,
// so a themeless role degrades to the terminal default rather than a bogus hex.
func tag(c tcell.Color) string {
	if c == tcell.ColorDefault {
		return "-"
	}
	h := c.TrueColor().Hex() // TrueColor() for robust RGB on named/palette colours
	if h < 0 {
		return "-"
	}
	return fmt.Sprintf("#%06x", h)
}

func accentTag() string  { return "[" + tag(activeTheme.Accent) + "]" }
func mutedTag() string   { return "[" + tag(activeTheme.Muted) + "]" }
func okTag() string      { return "[" + tag(activeTheme.OK) + "]" }
func cautionTag() string { return "[" + tag(activeTheme.Caution) + "]" }
func failingTag() string { return "[" + tag(activeTheme.Failing) + "]" }

// fgbgTag builds a compound "[fg:bg]" token for text drawn on a coloured field.
func fgbgTag(fg, bg tcell.Color) string { return "[" + tag(fg) + ":" + tag(bg) + "]" }

// activeTheme is the live palette every colour helper reads. setTheme installs a
// new one; it is initialised to dark (the original hard-coded colours) in init.
var activeTheme Theme

// dash is rendered wherever a drive does not report a value. It carries the
// theme's muted colour, so it is recomputed in setTheme rather than being a
// const — its call sites keep using it as a plain value.
var dash string

// setTheme installs t as the active palette and recomputes the theme-derived
// package values (currently dash). Runs on the UI goroutine only.
func setTheme(t Theme) {
	activeTheme = t
	dash = mutedTag() + "—[-]"
}

func init() { setTheme(dark) }

// dark reproduces smartview's original hard-coded palette exactly, so the
// default behaviour is unchanged. theme_test.go pins each role to its legacy
// colour to guard against silent drift.
var dark = Theme{
	Name:        "dark",
	Accent:      tcell.ColorAqua,
	Muted:       tcell.ColorGray,
	OK:          tcell.ColorGreen,
	Caution:     tcell.ColorYellow,
	Failing:     tcell.ColorRed,
	Neutral:     tcell.ColorDefault,
	Inverse:     tcell.ColorBlack,
	SelectionBg: tcell.ColorDarkSlateGray,
	SelectionFg: tcell.ColorWhite,
	BannerBg:    tcell.ColorYellow,
	BarHealthy:  tcell.ColorTeal,
	ScrollArrow: tcell.ColorWhite,
}

// light is tuned for a white/bright terminal: severity colours are darkened and
// saturated so they stay legible on white, the neutral text is explicit black
// (not the terminal default, which a dark-on-light scheme already supplies but
// we pin for predictability), and the selection uses a light-grey background
// with black text.
var light = Theme{
	Name:        "light",
	Accent:      tcell.ColorTeal,
	Muted:       tcell.ColorGray,
	OK:          tcell.ColorDarkGreen,
	Caution:     tcell.ColorOlive,
	Failing:     tcell.ColorDarkRed,
	Neutral:     tcell.ColorBlack,
	Inverse:     tcell.ColorWhite,
	SelectionBg: tcell.ColorLightGray,
	SelectionFg: tcell.ColorBlack,
	BannerBg:    tcell.ColorDarkRed,
	BarHealthy:  tcell.ColorTeal,
	ScrollArrow: tcell.ColorBlack,
}

// mono is a no-colour degrade for high-contrast or colour-averse terminals:
// every role is ColorDefault, so tag() emits "-" everywhere and nothing is
// tinted. Severity survives only through the ● glyph and bold weight; the
// selected row stands out by bold alone — an accepted limitation of no-colour
// mode.
var mono = Theme{
	Name:        "mono",
	Accent:      tcell.ColorDefault,
	Muted:       tcell.ColorDefault,
	OK:          tcell.ColorDefault,
	Caution:     tcell.ColorDefault,
	Failing:     tcell.ColorDefault,
	Neutral:     tcell.ColorDefault,
	Inverse:     tcell.ColorDefault,
	SelectionBg: tcell.ColorDefault,
	SelectionFg: tcell.ColorDefault,
	BannerBg:    tcell.ColorDefault,
	BarHealthy:  tcell.ColorDefault,
	ScrollArrow: tcell.ColorDefault,
}

// solarized maps the roles onto the Solarized Dark palette.
var solarized = Theme{
	Name:        "solarized",
	Accent:      tcell.NewHexColor(0x2aa198), // cyan
	Muted:       tcell.NewHexColor(0x586e75), // base01
	OK:          tcell.NewHexColor(0x859900), // green
	Caution:     tcell.NewHexColor(0xb58900), // yellow
	Failing:     tcell.NewHexColor(0xdc322f), // red
	Neutral:     tcell.NewHexColor(0x839496), // base0
	Inverse:     tcell.NewHexColor(0x002b36), // base03
	SelectionBg: tcell.NewHexColor(0x073642), // base02
	SelectionFg: tcell.NewHexColor(0x93a1a1), // base1
	BannerBg:    tcell.NewHexColor(0xb58900), // yellow
	BarHealthy:  tcell.NewHexColor(0x2aa198), // cyan
	ScrollArrow: tcell.NewHexColor(0x93a1a1), // base1
}

// themes is the registry of built-in palettes by name. themeCycle gives the
// stable order for the runtime cycle key and the ThemeNames listing, since a map
// does not preserve insertion order.
var themes = map[string]Theme{
	"dark":      dark,
	"light":     light,
	"mono":      mono,
	"solarized": solarized,
}

var themeCycle = []string{"dark", "light", "mono", "solarized"}

// HasTheme reports whether name is a known built-in theme.
func HasTheme(name string) bool {
	_, ok := themes[name]
	return ok
}

// ThemeNames lists the built-in theme names in cycle order, for help/error text.
func ThemeNames() string {
	return strings.Join(themeCycle, ", ")
}

// nextThemeName returns the theme after cur in themeCycle, wrapping at the end.
// An unknown cur (shouldn't happen) starts the cycle from the beginning.
func nextThemeName(cur string) string {
	for i, n := range themeCycle {
		if n == cur {
			return themeCycle[(i+1)%len(themeCycle)]
		}
	}
	return themeCycle[0]
}
