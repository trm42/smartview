// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// Theme is smartview's colour palette as semantic roles. The tcell.Color is
// the source of truth; markup tags are derived on demand by the tag helpers
// so they can't drift. activeTheme is touched only on the event-loop
// goroutine (like App.reports), so no mutex.
type Theme struct {
	Name string
	// Background is the ground every widget paints on; ColorDefault inherits
	// the terminal's own background.
	Background  tcell.Color
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

// borderColor returns the accent colour for a focused pane, muted otherwise.
func borderColor(focused bool) tcell.Color {
	if focused {
		return activeTheme.Accent
	}
	return activeTheme.Muted
}

// severityColor maps a health severity to its display colour.
func severityColor(s smart.Severity) tcell.Color {
	switch s {
	case smart.SeverityFailing:
		return activeTheme.Failing
	case smart.SeverityCaution:
		return activeTheme.Caution
	default:
		return activeTheme.OK
	}
}

// severityTag returns the bare colour token, for callers interpolating into "[%s]".
func severityTag(s smart.Severity) string {
	return tag(severityColor(s))
}

// sevText wraps text in a severity's colour. severityTag returns the bare
// token, unlike accentTag and its siblings, so every caller that just wanted
// coloured text was hand-writing the brackets; this is that.
func sevText(sev smart.Severity, text string) string {
	return fmt.Sprintf("[%s]%s[-]", severityTag(sev), text)
}

// sevBold is sevText in bold, for the health verdict.
func sevBold(sev smart.Severity, text string) string {
	return fmt.Sprintf("[%s::b]%s[-:-:-]", severityTag(sev), text)
}

// selectedRowStyle is the selected-row highlight: an explicit background that
// keeps the cell's own foreground (tview's default inversion makes neutral
// rows vanish).
func selectedRowStyle(fg tcell.Color) tcell.Style {
	// Pin ColorDefault to SelectionFg — the terminal default can be illegible
	// on the highlight.
	if fg == tcell.ColorDefault {
		fg = activeTheme.SelectionFg
	}
	return tcell.StyleDefault.
		Background(activeTheme.SelectionBg).
		Foreground(fg).
		Attributes(tcell.AttrBold)
}

// styleList applies the theme to a List: pins the secondary-text colour
// (tview defaults it to a green that leaks into every theme) and routes
// selection through selectedRowStyle so list and table selections match.
// Re-call after a theme change.
func styleList(l *tview.List) {
	bg := activeTheme.Background
	l.SetBackgroundColor(bg)
	// A List prints its rows without maintaining the background underneath, so
	// the ground has to be pinned in each row style; the SetXTextColor setters
	// reach only the foreground.
	l.SetMainTextStyle(tcell.StyleDefault.Foreground(activeTheme.Neutral).Background(bg))
	l.SetSecondaryTextStyle(tcell.StyleDefault.Foreground(activeTheme.ListSecondary).Background(bg))
	l.SetShortcutStyle(tcell.StyleDefault.Foreground(activeTheme.Accent).Background(bg))
	l.SetSelectedStyle(selectedRowStyle(activeTheme.SelectionFg))
}

// backgrounder is any widget whose ground can be re-set; Box, Flex, Pages,
// Table, List and TextView all satisfy it.
type backgrounder interface {
	SetBackgroundColor(tcell.Color) *tview.Box
}

// applyBackground grounds widgets in the active theme. tview bakes
// Styles.PrimitiveBackgroundColor in at construction, so every widget that
// outlives a theme change has to be told again.
func applyBackground(ws ...backgrounder) {
	for _, w := range ws {
		w.SetBackgroundColor(activeTheme.Background)
	}
}

// childHaver and pageHaver are the two ways a tview container holds children.
// Both are satisfied by promotion, so *detail, *fleetView and the tab views
// are walked as their embedded Flex.
type childHaver interface {
	GetItemCount() int
	GetItem(int) tview.Primitive
}

type pageHaver interface {
	GetPageNames(bool) []string
	GetPage(string) tview.Primitive
}

// groundTree re-grounds root and everything under it. This replaces a
// hand-listed set of widgets, which is the miss CLAUDE.md names for the
// banner: a widget added to the layout was themed only if someone remembered
// to add it to the list too. Hidden pages are included (GetPageNames(false)),
// so the fleet is re-grounded while off-screen.
//
// It reaches only what is *in* the tree: the narrow and wide layouts swap
// which drive selector is mounted, so the other one is passed separately by
// the caller.
func groundTree(root tview.Primitive) {
	if root == nil {
		return
	}
	if b, ok := root.(backgrounder); ok {
		b.SetBackgroundColor(activeTheme.Background)
	}
	switch c := root.(type) {
	case pageHaver:
		for _, name := range c.GetPageNames(false) {
			groundTree(c.GetPage(name))
		}
	case childHaver:
		for i := range c.GetItemCount() {
			groundTree(c.GetItem(i))
		}
	}
}

// attrTextColor colours attribute row text: neutral when healthy, so only
// rows needing attention are tinted. Colour marks exceptions, not membership.
func attrTextColor(s smart.Severity) tcell.Color {
	if s == smart.SeverityOK {
		return activeTheme.Neutral
	}
	return severityColor(s)
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
	applyTviewStyles(t)
}

// applyTviewStyles maps the palette onto tview's package-level defaults, which
// tview reads at widget construction: every widget built after this call is
// born in the theme, and it is the only lever on tvxwidgets' gauge, which
// re-reads the globals at draw time. Widgets that already exist keep the
// colours they were built with (see App.repaintAll).
func applyTviewStyles(t Theme) {
	tview.Styles.PrimitiveBackgroundColor = t.Background
	tview.Styles.ContrastBackgroundColor = t.SelectionBg
	tview.Styles.MoreContrastBackgroundColor = t.SelectionBg
	tview.Styles.BorderColor = t.Muted
	// Neutral, not Accent: a box title is chrome, and accenting every one of
	// them spends the colour that marks focus and exceptions.
	tview.Styles.TitleColor = t.Neutral
	tview.Styles.GraphicsColor = t.Muted
	tview.Styles.PrimaryTextColor = t.Neutral
	tview.Styles.SecondaryTextColor = t.Accent
	tview.Styles.TertiaryTextColor = t.ListSecondary
	tview.Styles.InverseTextColor = t.Inverse
	tview.Styles.ContrastSecondaryTextColor = t.SelectionFg
}

func init() { setTheme(dark) }

// dark reproduces the original hard-coded palette; theme_test.go pins each role.
var dark = Theme{
	Name:       "dark",
	Background: tcell.ColorBlack,
	Accent:     tcell.ColorAqua,
	Muted:      tcell.ColorGray,
	OK:         tcell.ColorGreen,
	Caution:    tcell.ColorYellow,
	Failing:    tcell.ColorRed,
	// Explicit, not ColorDefault: the ground is painted, so inherited text
	// lands black on black in a light terminal.
	Neutral:     tcell.ColorWhite,
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
	Background:    tcell.ColorDefault,
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
	Background:    tcell.NewHexColor(0x050b14), // cold near-black navy
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
	Name:       "phosphor",
	Background: tcell.NewHexColor(0x001000), // green-black CRT ground
	Accent:     tcell.NewHexColor(0x33ff33), // pure neon CRT green: borders, headers, active tab, key hints
	Muted:      tcell.NewHexColor(0x1f8f1f), // dim green: dashes, unfocused border, raw values
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
	Background:    tcell.NewHexColor(0x140a00), // brown-black monitor ground
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
	Background:    tcell.NewHexColor(0x000000), // CGA black
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
	Background:    tcell.NewHexColor(0x0a0a12), // near-black violet
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
	Name:       "nord",
	Background: tcell.NewHexColor(0x2e3440), // polar night
	Accent:     tcell.NewHexColor(0x88c0d0), // frost cyan: borders, headers, active tab, key hints
	// Off-palette slate: Nord's own grays fall under the 3:1 floor on nord0.
	Muted:         tcell.NewHexColor(0x7b88a3),
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
	Background:    tcell.NewHexColor(0x282828), // dark0
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
	Background:    tcell.NewHexColor(0x12161c), // near-black slate
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

// daylight is the cool light palette, tuned against its own paper ground:
// every foreground clears 4:1 on it, and the ramp trades yellow — invisible on
// paper — for a burnt amber that darkens into crimson as it worsens.
var daylight = Theme{
	Name:          "daylight",
	Background:    tcell.NewHexColor(0xfbfbfa), // cool paper
	Accent:        tcell.NewHexColor(0x0a5f9e), // deep azure: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x6b7784), // slate: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x1a7f37), // dark green
	Caution:       tcell.NewHexColor(0xa15c00), // burnt amber
	Failing:       tcell.NewHexColor(0xc1121f), // crimson: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x1f2328), // ink body text
	Inverse:       tcell.NewHexColor(0xffffff), // white: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0xc9e0f5), // pale blue selected-row band
	SelectionFg:   tcell.NewHexColor(0x0b3a5c), // deep blue on selection
	BannerBg:      tcell.NewHexColor(0xa15c00), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x1e8a41), // green FARM healthy bar, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x0a5f9e), // azure arrows
	ListSecondary: tcell.NewHexColor(0x5b6672), // slate secondary line, darker than Muted: it carries data
}

// parchment is the warm light palette: cool teal chrome against a warm ramp,
// tuned against its own paper ground on the same 4:1 basis as daylight.
var parchment = Theme{
	Name:          "parchment",
	Background:    tcell.NewHexColor(0xf4eee1), // warm parchment
	Accent:        tcell.NewHexColor(0x15615a), // deep teal: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x746a5b), // warm gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x3f6b25), // olive green
	Caution:       tcell.NewHexColor(0x8f5300), // burnt ochre
	Failing:       tcell.NewHexColor(0xa01f18), // brick red: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x3a352d), // warm ink body text
	Inverse:       tcell.NewHexColor(0xfbf7ee), // cream: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0xded0b4), // warm sand selected-row band
	SelectionFg:   tcell.NewHexColor(0x2c281f), // dark warm ink on selection
	BannerBg:      tcell.NewHexColor(0x8f5300), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x4c7d2a), // olive FARM healthy bar, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x15615a), // teal arrows
	ListSecondary: tcell.NewHexColor(0x665d50), // warm gray secondary line, darker than Muted: it carries data
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
