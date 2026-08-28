// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Theme is smartview's colour palette as semantic roles. The tcell.Color is
// the source of truth; markup tags are derived on demand by the tag helpers
// so they can't drift. activeTheme is touched only on the event-loop
// goroutine (like App.reports), so no mutex.
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

	// ListSecondary is the drive-list secondary line. Must never equal OK:
	// it renders on every drive, failing ones included.
	ListSecondary tcell.Color
}

// tag renders a colour as a tview markup token (no brackets); ColorDefault or
// an invalid colour yields "-" so a themeless role degrades to the default.
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

// activeTabTag is the bold Inverse-on-Accent pill for the active tab, falling
// back to black-on-white under mono where the pill would otherwise vanish.
func activeTabTag() string {
	fg, bg := activeTheme.Inverse, activeTheme.Accent
	if bg == tcell.ColorDefault {
		fg, bg = tcell.ColorBlack, tcell.ColorWhite
	}
	return "[" + tag(fg) + ":" + tag(bg) + ":b]"
}

// activeTheme is the live palette every colour helper reads.
var activeTheme Theme

// dash marks an unreported value; recomputed in setTheme since it carries the
// muted colour.
var dash string

// setTheme installs the palette and recomputes theme-derived values. UI
// goroutine only.
func setTheme(t Theme) {
	activeTheme = t
	dash = mutedTag() + "—[-]"
}

func init() { setTheme(dark) }

// dark reproduces the original hard-coded palette; theme_test.go pins each role.
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
	// Was ColorGreen (== OK), which painted a failing drive's metadata line
	// the healthy colour.
	ListSecondary: tcell.ColorGray,
}

// mono is the no-colour degrade: every role is ColorDefault. Severity
// survives only via the ● glyph and bold — an accepted limitation.
var mono = Theme{
	Name:          "mono",
	Accent:        tcell.ColorDefault,
	Muted:         tcell.ColorDefault,
	OK:            tcell.ColorDefault,
	Caution:       tcell.ColorDefault,
	Failing:       tcell.ColorDefault,
	Neutral:       tcell.ColorDefault,
	Inverse:       tcell.ColorDefault,
	SelectionBg:   tcell.ColorDefault,
	SelectionFg:   tcell.ColorDefault,
	BannerBg:      tcell.ColorDefault,
	BarHealthy:    tcell.ColorDefault,
	ScrollArrow:   tcell.ColorDefault,
	ListSecondary: tcell.ColorDefault,
}

// electric is an "elite BBS" palette: azure-cyan and white with amber caution
// and red failing. All-hex so it renders identically across terminals.
var electric = Theme{
	Name:          "electric",
	Accent:        tcell.NewHexColor(0x00b7ff), // bright azure-cyan: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x5f7184), // dark slate gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x3ddc84), // healthy green (universal "good" cue)
	Caution:       tcell.NewHexColor(0xffb000), // amber
	Failing:       tcell.NewHexColor(0xff3b30), // red
	Neutral:       tcell.NewHexColor(0xe6f1ff), // bright white: healthy attribute-row text
	Inverse:       tcell.NewHexColor(0x001830), // dark navy: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x0f3a63), // deep blue selected-row bg
	SelectionFg:   tcell.NewHexColor(0xeaf4ff), // bright white on selection
	BannerBg:      tcell.NewHexColor(0xffb000), // amber root-warning banner (stands out from blue)
	BarHealthy:    tcell.NewHexColor(0x00b7ff), // cyan FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x00b7ff), // cyan arrows
	ListSecondary: tcell.NewHexColor(0x6f9fc0), // muted blue-cyan secondary line
}

// phosphor is the green-CRT palette: pure green only, severity read through
// brightness plus the ● glyph and bold. All-hex.
var phosphor = Theme{
	Name:   "phosphor",
	Accent: tcell.NewHexColor(0x33ff33), // pure neon CRT green: borders, headers, active tab, key hints
	Muted:  tcell.NewHexColor(0x1f8f1f), // dim green: dashes, unfocused border, raw values
	// The severity ramp must escalate by getting brighter, not paler
	// (theme_test.go pins Failing hotter than OK).
	OK:            tcell.NewHexColor(0x2a9d2a), // steady green
	Caution:       tcell.NewHexColor(0x38d938), // brighter
	Failing:       tcell.NewHexColor(0x6bff6b), // brightest — severity by intensity + ● + bold
	Neutral:       tcell.NewHexColor(0x2ad42a), // standard green body text
	Inverse:       tcell.NewHexColor(0x001a00), // near-black green: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x0f4f0f), // dark green selected-row bg
	SelectionFg:   tcell.NewHexColor(0xd6ffd6), // pale green text on selection
	BannerBg:      tcell.NewHexColor(0x4dff4d), // bright-green root-warning banner (was amber); dark Inverse text reads on it
	BarHealthy:    tcell.NewHexColor(0x33ff33), // FARM healthy bar: neon green
	ScrollArrow:   tcell.NewHexColor(0x33ff33), // neon green arrows
	ListSecondary: tcell.NewHexColor(0x1f9f1f), // dim green secondary line
}

// amber is the Hercules amber-monitor palette with an amber→orange→red
// severity ramp. All-hex.
var amber = Theme{
	Name:          "amber",
	Accent:        tcell.NewHexColor(0xffb000), // bright amber: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x8a5a10), // dim brown-amber: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0xffcc33), // gold-amber (healthy)
	Caution:       tcell.NewHexColor(0xff7f00), // orange
	Failing:       tcell.NewHexColor(0xff2d00), // red-orange
	Neutral:       tcell.NewHexColor(0xf0a830), // warm amber body text
	Inverse:       tcell.NewHexColor(0x1a0a00), // near-black brown: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x4a2600), // dark brown selected-row bg
	SelectionFg:   tcell.NewHexColor(0xffe0b0), // pale amber on selection
	BannerBg:      tcell.NewHexColor(0xff5000), // vivid orange-red banner (stands out from amber)
	BarHealthy:    tcell.NewHexColor(0xffb000), // amber FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0xffb000), // amber arrows
	ListSecondary: tcell.NewHexColor(0xb87818), // dim amber secondary line
}

// cga draws every role from the authentic IBM CGA 16, nothing interpolated.
var cga = Theme{
	Name:          "cga",
	Accent:        tcell.NewHexColor(0x55ffff), // light cyan: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0xaaaaaa), // light gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x55ff55), // light green
	Caution:       tcell.NewHexColor(0xffff55), // yellow
	Failing:       tcell.NewHexColor(0xff5555), // light red
	Neutral:       tcell.NewHexColor(0xffffff), // white body text
	Inverse:       tcell.NewHexColor(0x000000), // black: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x0000aa), // blue selected-row bg
	SelectionFg:   tcell.NewHexColor(0xffffff), // white on selection
	BannerBg:      tcell.NewHexColor(0xff55ff), // light magenta banner (stands out from cyan)
	BarHealthy:    tcell.NewHexColor(0x00aaaa), // cyan FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0xffffff), // white arrows
	ListSecondary: tcell.NewHexColor(0xaaaaaa), // light gray secondary line
}

// neon is the cyberpunk palette: electric blue chrome, magenta banner and
// bars, white text.
var neon = Theme{
	Name:          "neon",
	Accent:        tcell.NewHexColor(0x22d3ff), // electric blue: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x6b7a99), // desaturated blue-gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x39ff9e), // neon mint
	Caution:       tcell.NewHexColor(0xffcc33), // neon amber
	Failing:       tcell.NewHexColor(0xff2f5f), // neon crimson
	Neutral:       tcell.NewHexColor(0xeef2ff), // near-white body text
	Inverse:       tcell.NewHexColor(0x0a0a12), // near-black: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x3a0d43), // deep violet selected-row bg
	SelectionFg:   tcell.NewHexColor(0xffe6fb), // pale pink on selection
	BannerBg:      tcell.NewHexColor(0xff2fb8), // hot magenta banner (stands out from blue)
	BarHealthy:    tcell.NewHexColor(0xff2fb8), // magenta FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x22d3ff), // electric blue arrows
	ListSecondary: tcell.NewHexColor(0xc084d8), // mauve secondary line
}

// nord is the arctic blue-gray scheme: frost for chrome, aurora for severity.
var nord = Theme{
	Name:          "nord",
	Accent:        tcell.NewHexColor(0x88c0d0), // frost cyan: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x616e88), // polar slate: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0xa3be8c), // aurora green
	Caution:       tcell.NewHexColor(0xebcb8b), // aurora yellow
	Failing:       tcell.NewHexColor(0xbf616a), // aurora red
	Neutral:       tcell.NewHexColor(0xd8dee9), // snow-storm body text
	Inverse:       tcell.NewHexColor(0x2e3440), // polar night: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x434c5e), // polar night selected-row bg
	SelectionFg:   tcell.NewHexColor(0xeceff4), // brightest snow on selection
	BannerBg:      tcell.NewHexColor(0xd08770), // aurora orange banner (stands out from frost)
	BarHealthy:    tcell.NewHexColor(0x8fbcbb), // frost teal FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x88c0d0), // frost cyan arrows
	ListSecondary: tcell.NewHexColor(0x7b8ca6), // muted slate secondary line
}

// gruvbox is the warm retro-earth scheme. Chrome takes gruvbox blue rather
// than its signature gold, which would read as a caution on every border.
var gruvbox = Theme{
	Name:          "gruvbox",
	Accent:        tcell.NewHexColor(0x83a598), // gruvbox blue: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x928374), // gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0xb8bb26), // bright green
	Caution:       tcell.NewHexColor(0xfabd2f), // bright yellow
	Failing:       tcell.NewHexColor(0xfb4934), // bright red
	Neutral:       tcell.NewHexColor(0xebdbb2), // light cream body text
	Inverse:       tcell.NewHexColor(0x282828), // dark0: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x504945), // dark2 selected-row bg
	SelectionFg:   tcell.NewHexColor(0xfbf1c7), // light0 on selection
	BannerBg:      tcell.NewHexColor(0xfe8019), // bright orange banner (stands out from blue)
	BarHealthy:    tcell.NewHexColor(0x8ec07c), // aqua FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x83a598), // gruvbox blue arrows
	ListSecondary: tcell.NewHexColor(0xa89984), // dim cream secondary line
}

// beacon is the colour-vision-deficient-safe palette: a blue → yellow → rose
// severity ramp (Paul Tol's high-contrast set) that stays separable under all
// three CVD types, with neutral chrome so no hue competes with it.
var beacon = Theme{
	Name:          "beacon",
	Accent:        tcell.NewHexColor(0xd6dee8), // cool near-white: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x6b7785), // slate: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x6cb4ee), // blue
	Caution:       tcell.NewHexColor(0xeecc66), // yellow
	Failing:       tcell.NewHexColor(0xee7788), // rose
	Neutral:       tcell.NewHexColor(0xe4e9ef), // cool white body text
	Inverse:       tcell.NewHexColor(0x10151b), // near-black: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x2a3542), // dark slate selected-row bg
	SelectionFg:   tcell.NewHexColor(0xf0f4f8), // near-white on selection
	BannerBg:      tcell.NewHexColor(0xeecc66), // yellow banner
	BarHealthy:    tcell.NewHexColor(0x6cb4ee), // blue FARM healthy bar (matches OK)
	ScrollArrow:   tcell.NewHexColor(0xd6dee8), // near-white arrows
	ListSecondary: tcell.NewHexColor(0x8b97a5), // muted slate secondary line
}

// daylight is the cool light palette. Yellow is invisible on paper, so the
// ramp is re-tuned for a light field: dark amber caution, darkened green OK.
// Like parchment it assumes a light terminal background — Theme has no
// background role, so the terminal supplies it.
var daylight = Theme{
	Name:          "daylight",
	Accent:        tcell.NewHexColor(0x0b6fb4), // deep azure: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x7a8894), // slate: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x1a7f37), // dark green (legible on paper)
	Caution:       tcell.NewHexColor(0x9a6700), // dark amber
	Failing:       tcell.NewHexColor(0xcf222e), // red
	Neutral:       tcell.NewHexColor(0x1f2328), // ink body text
	Inverse:       tcell.NewHexColor(0xffffff), // white: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0xdbe9f6), // pale blue selected-row bg
	SelectionFg:   tcell.NewHexColor(0x0b3a5c), // deep blue on selection
	BannerBg:      tcell.NewHexColor(0xb35900), // dark orange banner (stands out from azure)
	BarHealthy:    tcell.NewHexColor(0x2da44e), // green FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x0b6fb4), // azure arrows
	ListSecondary: tcell.NewHexColor(0x6a737d), // gray secondary line
}

// parchment is the warm light palette: cool teal chrome against a warm
// severity ramp. Assumes a light terminal background, as daylight does.
var parchment = Theme{
	Name:          "parchment",
	Accent:        tcell.NewHexColor(0x1a6b62), // deep teal: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x93887a), // warm gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x4a7a2c), // olive green
	Caution:       tcell.NewHexColor(0xa86a00), // ochre
	Failing:       tcell.NewHexColor(0xb3261e), // brick red
	Neutral:       tcell.NewHexColor(0x3a352d), // warm ink body text
	Inverse:       tcell.NewHexColor(0xfbf7ee), // cream: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0xe3d9c4), // warm sand selected-row bg
	SelectionFg:   tcell.NewHexColor(0x2c281f), // dark warm ink on selection
	BannerBg:      tcell.NewHexColor(0xa86a00), // ochre banner (stands out from teal)
	BarHealthy:    tcell.NewHexColor(0x5a8f34), // olive FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x1a6b62), // teal arrows
	ListSecondary: tcell.NewHexColor(0x8c8172), // dim warm gray secondary line
}

// themes is the registry of built-in palettes; themeCycle gives the stable
// order for the cycle key and ThemeNames.
var themes = map[string]Theme{
	"dark":      dark,
	"mono":      mono,
	"electric":  electric,
	"phosphor":  phosphor,
	"amber":     amber,
	"cga":       cga,
	"neon":      neon,
	"nord":      nord,
	"gruvbox":   gruvbox,
	"beacon":    beacon,
	"daylight":  daylight,
	"parchment": parchment,
}

// themeCycle groups the palettes by family — retro hardware, then the modern
// dark schemes, then the light pair — so cycling walks related looks together.
var themeCycle = []string{
	"dark", "electric", "phosphor", "amber", "cga",
	"neon", "nord", "gruvbox", "beacon",
	"daylight", "parchment", "mono",
}

// HasTheme reports whether name is a known built-in theme.
func HasTheme(name string) bool {
	_, ok := themes[name]
	return ok
}

// ThemeNames lists the built-in theme names in cycle order, for help/error text.
func ThemeNames() string {
	return strings.Join(themeCycle, ", ")
}

// nextThemeName returns the theme after cur in themeCycle, wrapping; an
// unknown cur starts from the beginning.
func nextThemeName(cur string) string {
	for i, n := range themeCycle {
		if n == cur {
			return themeCycle[(i+1)%len(themeCycle)]
		}
	}
	return themeCycle[0]
}
