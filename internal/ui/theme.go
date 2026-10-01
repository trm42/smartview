// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// Theme is the palette as semantic roles; activeTheme is touched only on the event-loop goroutine, so no mutex.
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

// namedTags renders palette-index colours by name so tview parses them back to the same index the style paths send.
var namedTags = map[tcell.Color]string{
	tcell.ColorBlack:  "black",
	tcell.ColorRed:    "red",
	tcell.ColorGreen:  "green",
	tcell.ColorYellow: "yellow",
	tcell.ColorTeal:   "teal",
	tcell.ColorAqua:   "aqua",
	tcell.ColorGray:   "gray",
	tcell.ColorWhite:  "white",
}

// tag renders a colour as a markup token: "-" for ColorDefault, the name for a palette index, else #rrggbb.
func tag(c tcell.Color) string {
	if c == tcell.ColorDefault {
		return "-"
	}
	if n, ok := namedTags[c]; ok {
		return n
	}
	h := c.TrueColor().Hex()
	if h < 0 {
		return "-"
	}
	return fmt.Sprintf("#%06x", h)
}

// colorTag is the bracketed markup token for c.
func colorTag(c tcell.Color) string { return "[" + tag(c) + "]" }

func accentTag() string { return colorTag(activeTheme.Accent) }

// unavailableTabTag is Muted plus dim, since Muted collapses to the terminal default under mono.
func unavailableTabTag() string { return "[" + tag(activeTheme.Muted) + "::d]" }

func mutedTag() string   { return colorTag(activeTheme.Muted) }
func okTag() string      { return colorTag(activeTheme.OK) }
func cautionTag() string { return colorTag(activeTheme.Caution) }
func failingTag() string { return colorTag(activeTheme.Failing) }

// fgbgTag builds a compound "[fg:bg]" token for text drawn on a coloured field.
func fgbgTag(fg, bg tcell.Color) string { return "[" + tag(fg) + ":" + tag(bg) + "]" }

// activeTabTag is the bold Inverse-on-Accent pill for the active tab, black-on-white under mono.
func activeTabTag() string {
	fg, bg := activeTheme.Inverse, activeTheme.Accent
	if bg == tcell.ColorDefault {
		fg, bg = tcell.ColorBlack, tcell.ColorWhite
	}
	return strings.TrimSuffix(fgbgTag(fg, bg), "]") + ":b]"
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

// sevText wraps text in a severity's colour.
func sevText(sev smart.Severity, text string) string {
	return "[" + severityTag(sev) + "]" + text + "[-]"
}

// sevBold is sevText in bold, for the health verdict.
func sevBold(sev smart.Severity, text string) string {
	return "[" + severityTag(sev) + "::b]" + text + "[-:-:-]"
}

// selectedRowStyle is the selected-row highlight, keeping the cell's own foreground.
// A palette with no SelectionBg gets reverse video; a ColorDefault fg is pinned to SelectionFg.
func selectedRowStyle(fg tcell.Color) tcell.Style {
	if activeTheme.SelectionBg == tcell.ColorDefault {
		return tcell.StyleDefault.Reverse(true).Bold(true)
	}
	if fg == tcell.ColorDefault {
		fg = activeTheme.SelectionFg
	}
	return tcell.StyleDefault.
		Background(activeTheme.SelectionBg).
		Foreground(fg).
		Attributes(tcell.AttrBold)
}

// styleList applies the theme to a List, pinning the secondary-text colour
// (tview defaults it to a green that leaks into every theme).
func styleList(l *tview.List) {
	bg := activeTheme.Background
	l.SetBackgroundColor(bg)
	// A List does not paint the ground under its rows, so each row style carries it.
	l.SetMainTextStyle(tcell.StyleDefault.Foreground(activeTheme.Neutral).Background(bg))
	l.SetSecondaryTextStyle(tcell.StyleDefault.Foreground(activeTheme.ListSecondary).Background(bg))
	l.SetShortcutStyle(tcell.StyleDefault.Foreground(activeTheme.Accent).Background(bg))
	l.SetSelectedStyle(selectedRowStyle(activeTheme.SelectionFg))
}

// backgrounder is any widget whose ground can be re-set.
type backgrounder interface {
	SetBackgroundColor(tcell.Color) *tview.Box
}

// applyBackground re-grounds widgets; tview bakes the ground in at construction.
func applyBackground(ws ...backgrounder) {
	for _, w := range ws {
		w.SetBackgroundColor(activeTheme.Background)
	}
}

// textColorer is any widget whose default foreground can be re-set.
type textColorer interface {
	SetTextColor(tcell.Color) *tview.TextView
}

// applyTextColor re-pins the default ink tview baked in at construction.
func applyTextColor(ws ...textColorer) {
	for _, w := range ws {
		w.SetTextColor(activeTheme.Neutral)
	}
}

// titler is any widget carrying a box title.
type titler interface {
	SetTitleColor(tcell.Color) *tview.Box
}

// childHaver and pageHaver are the two ways a tview container holds children.
type childHaver interface {
	GetItemCount() int
	GetItem(int) tview.Primitive
}

type pageHaver interface {
	GetPageNames(bool) []string
	GetPage(string) tview.Primitive
}

// rethemeTree re-applies the ground and title colour tview bakes in at
// construction to root and every descendant, hidden pages included; unmounted
// widgets are the caller's job.
func rethemeTree(root tview.Primitive) {
	if root == nil {
		return
	}
	if b, ok := root.(backgrounder); ok {
		b.SetBackgroundColor(activeTheme.Background)
	}
	if t, ok := root.(titler); ok {
		t.SetTitleColor(activeTheme.Neutral)
	}
	switch c := root.(type) {
	case pageHaver:
		for _, name := range c.GetPageNames(false) {
			rethemeTree(c.GetPage(name))
		}
	case childHaver:
		for i := range c.GetItemCount() {
			rethemeTree(c.GetItem(i))
		}
	}
}

// attrTextColor colours row text: neutral when healthy, else the severity colour.
func attrTextColor(s smart.Severity) tcell.Color {
	if s == smart.SeverityOK {
		return activeTheme.Neutral
	}
	return severityColor(s)
}

// truecolorTerm reports whether terminfo gives tcell 24-bit colour for term.
func truecolorTerm(term string) bool {
	ti, err := tcell.LookupTerminfo(term)
	if err != nil {
		return false
	}
	return ti.SetFgRGB != "" || ti.SetBgRGB != "" || ti.SetFgBgRGB != ""
}

// hasTruecolor is the verdict for the running terminal. Memoised: an unknown TERM shells out to infocmp.
var hasTruecolor = sync.OnceValue(func() bool { return truecolorTerm(os.Getenv("TERM")) })

// activeTheme is the live palette every colour helper reads.
var activeTheme Theme

// dash marks an unreported value in the muted colour; recomputed in setTheme.
var dash string

// setTheme installs the palette and recomputes theme-derived values; UI goroutine only.
func setTheme(t Theme) {
	activeTheme = t
	dash = mutedTag() + "—[-]"
	applyTviewStyles(t)
}

// applyTviewStyles maps the palette onto tview's construction-time defaults;
// the only lever on tvxwidgets' gauge, which re-reads them at draw time.
func applyTviewStyles(t Theme) {
	tview.Styles.PrimitiveBackgroundColor = t.Background
	tview.Styles.ContrastBackgroundColor = t.SelectionBg
	tview.Styles.MoreContrastBackgroundColor = t.SelectionBg
	tview.Styles.BorderColor = t.Muted
	tview.Styles.TitleColor = t.Neutral
	tview.Styles.GraphicsColor = t.Muted
	tview.Styles.PrimaryTextColor = t.Neutral
	tview.Styles.SecondaryTextColor = t.Accent
	tview.Styles.TertiaryTextColor = t.ListSecondary
	tview.Styles.InverseTextColor = t.Inverse
	tview.Styles.ContrastSecondaryTextColor = t.SelectionFg
}

func init() { setTheme(dark) }

// dark is the original palette in hex, with the ground one step off pure black.
var dark = Theme{
	Name:       "dark",
	Background: tcell.NewHexColor(0x0b0d10), // near-black, one step off #000
	Accent:     tcell.NewHexColor(0x00ffff), // aqua
	Muted:      tcell.NewHexColor(0x808080), // gray
	OK:         tcell.NewHexColor(0x008000), // green
	Caution:    tcell.NewHexColor(0xffff00), // yellow
	Failing:    tcell.NewHexColor(0xff0000), // red
	// Explicit, not ColorDefault: inherited text lands black on black in a light terminal.
	Neutral:       tcell.NewHexColor(0xffffff), // white
	Inverse:       tcell.NewHexColor(0x000000), // black
	SelectionBg:   tcell.NewHexColor(0x16202a),
	SelectionFg:   tcell.NewHexColor(0xffffff), // white
	BannerBg:      tcell.NewHexColor(0xffff00), // yellow
	BarHealthy:    tcell.NewHexColor(0x008080), // teal
	ScrollArrow:   tcell.NewHexColor(0xffffff), // white
	ListSecondary: tcell.NewHexColor(0x808080), // gray
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

// terminal keeps the terminal's ground and ink and adds severity as NAMED colours so they resolve through the same scheme.
var terminal = Theme{
	Name:       "terminal",
	Background: tcell.ColorDefault, // the terminal's own
	Accent:     tcell.ColorAqua,
	Muted:      tcell.ColorGray,
	OK:         tcell.ColorGreen,
	Caution:    tcell.ColorYellow,
	Failing:    tcell.ColorRed,
	Neutral:    tcell.ColorDefault, // the terminal's own body colour
	Inverse:    tcell.ColorBlack,   // Accent and BannerBg are both light in any scheme
	// No band: selectedRowStyle draws reverse video instead.
	SelectionBg:   tcell.ColorDefault,
	SelectionFg:   tcell.ColorDefault,
	BannerBg:      tcell.ColorYellow, // light in every scheme, so Inverse reads on it
	BarHealthy:    tcell.ColorTeal,
	ScrollArrow:   tcell.ColorAqua,
	ListSecondary: tcell.ColorGray,
}

// electric is an "elite BBS" palette: azure-cyan and white with amber caution
// and red failing. All-hex so it renders identically across terminals.
var electric = Theme{
	Name:          "electric",
	Background:    tcell.NewHexColor(0x050b14), // cold near-black navy
	Accent:        tcell.NewHexColor(0x00b7ff), // bright azure-cyan
	Muted:         tcell.NewHexColor(0x5f7184), // dark slate gray
	OK:            tcell.NewHexColor(0x3ddc84), // healthy green (universal "good" cue)
	Caution:       tcell.NewHexColor(0xffb000), // amber
	Failing:       tcell.NewHexColor(0xff3b30), // red
	Neutral:       tcell.NewHexColor(0xe6f1ff), // bright white
	Inverse:       tcell.NewHexColor(0x001830), // dark navy
	SelectionBg:   tcell.NewHexColor(0x0f3a63), // deep blue
	SelectionFg:   tcell.NewHexColor(0xeaf4ff), // bright white
	BannerBg:      tcell.NewHexColor(0xffb000), // amber (stands out from blue)
	BarHealthy:    tcell.NewHexColor(0x00b7ff), // cyan
	ScrollArrow:   tcell.NewHexColor(0x00b7ff), // cyan
	ListSecondary: tcell.NewHexColor(0x6f9fc0), // muted blue-cyan
}

// phosphor is the green-CRT palette: pure green only, severity read through
// brightness plus the ● glyph and bold. All-hex.
var phosphor = Theme{
	Name:       "phosphor",
	Background: tcell.NewHexColor(0x001000), // green-black CRT ground
	Accent:     tcell.NewHexColor(0x33ff33), // pure neon CRT green
	Muted:      tcell.NewHexColor(0x1f8f1f), // dim green
	// The severity ramp must escalate by getting brighter, not paler
	// (theme_test.go pins Failing hotter than OK).
	OK:            tcell.NewHexColor(0x2a9d2a), // steady green
	Caution:       tcell.NewHexColor(0x38d938), // brighter
	Failing:       tcell.NewHexColor(0x6bff6b), // brightest — severity by intensity + ● + bold
	Neutral:       tcell.NewHexColor(0x2ad42a), // standard green
	Inverse:       tcell.NewHexColor(0x001a00), // near-black green
	SelectionBg:   tcell.NewHexColor(0x123610), // dark green
	SelectionFg:   tcell.NewHexColor(0xd6ffd6), // pale green text
	BannerBg:      tcell.NewHexColor(0x4dff4d), // bright green
	BarHealthy:    tcell.NewHexColor(0x33ff33), // neon green
	ScrollArrow:   tcell.NewHexColor(0x33ff33), // neon green
	ListSecondary: tcell.NewHexColor(0x1f9f1f), // dim green
}

// amber is the Hercules amber-monitor palette with an amber→orange→red
// severity ramp. All-hex.
var amber = Theme{
	Name:       "amber",
	Background: tcell.NewHexColor(0x140a00), // brown-black monitor ground
	Accent:     tcell.NewHexColor(0xffb000), // bright amber
	Muted:      tcell.NewHexColor(0x8a5a10), // dim brown-amber
	// Below Caution, not above it: gold at 13:1 made every healthy drive the
	// brightest thing in the list, and gold reads as a warning.
	OK:            tcell.NewHexColor(0xcf9426), // dark gold (healthy)
	Caution:       tcell.NewHexColor(0xff7f00), // orange
	Failing:       tcell.NewHexColor(0xff2d00), // red-orange
	Neutral:       tcell.NewHexColor(0xf0a830), // warm amber
	Inverse:       tcell.NewHexColor(0x1a0a00), // near-black brown
	SelectionBg:   tcell.NewHexColor(0x4a2600), // dark brown
	SelectionFg:   tcell.NewHexColor(0xffe0b0), // pale amber
	BannerBg:      tcell.NewHexColor(0xff5000), // vivid orange-red banner (stands out from amber)
	BarHealthy:    tcell.NewHexColor(0xffb000), // amber
	ScrollArrow:   tcell.NewHexColor(0xffb000), // amber
	ListSecondary: tcell.NewHexColor(0xb87818), // dim amber
}

// cga draws every role from the authentic IBM CGA 16, nothing interpolated.
var cga = Theme{
	Name:          "cga",
	Background:    tcell.NewHexColor(0x000000), // CGA black
	Accent:        tcell.NewHexColor(0x55ffff), // light cyan
	Muted:         tcell.NewHexColor(0xaaaaaa), // light gray
	OK:            tcell.NewHexColor(0x55ff55), // light green
	Caution:       tcell.NewHexColor(0xffff55), // yellow
	Failing:       tcell.NewHexColor(0xff5555), // light red
	Neutral:       tcell.NewHexColor(0xffffff), // white
	Inverse:       tcell.NewHexColor(0x000000), // black
	SelectionBg:   tcell.NewHexColor(0x0000aa), // blue
	SelectionFg:   tcell.NewHexColor(0xffffff), // white
	BannerBg:      tcell.NewHexColor(0xff55ff), // light magenta banner (stands out from cyan)
	BarHealthy:    tcell.NewHexColor(0x00aaaa), // cyan
	ScrollArrow:   tcell.NewHexColor(0xffffff), // white
	ListSecondary: tcell.NewHexColor(0xaaaaaa), // light gray
}

// neon is the cyberpunk palette: electric blue chrome, magenta banner and
// bars, white text.
var neon = Theme{
	Name:          "neon",
	Background:    tcell.NewHexColor(0x0a0a12), // near-black violet
	Accent:        tcell.NewHexColor(0x22d3ff), // electric blue
	Muted:         tcell.NewHexColor(0x6b7a99), // desaturated blue-gray
	OK:            tcell.NewHexColor(0x39ff9e), // neon mint
	Caution:       tcell.NewHexColor(0xffcc33), // neon amber
	Failing:       tcell.NewHexColor(0xff2f5f), // neon crimson
	Neutral:       tcell.NewHexColor(0xeef2ff), // near-white
	Inverse:       tcell.NewHexColor(0x0a0a12), // near-black
	SelectionBg:   tcell.NewHexColor(0x2b1733), // deep violet, dark enough not to out-shout a failing row
	SelectionFg:   tcell.NewHexColor(0xffe6fb), // pale pink
	BannerBg:      tcell.NewHexColor(0xff2fb8), // hot magenta banner (stands out from blue)
	BarHealthy:    tcell.NewHexColor(0xff2fb8), // magenta
	ScrollArrow:   tcell.NewHexColor(0x22d3ff), // electric blue
	ListSecondary: tcell.NewHexColor(0xc084d8), // mauve
}

// nord is the arctic blue-gray scheme: frost for chrome, aurora for severity.
var nord = Theme{
	Name:       "nord",
	Background: tcell.NewHexColor(0x2e3440), // polar night
	Accent:     tcell.NewHexColor(0x88c0d0), // frost cyan
	// Off-palette slate: Nord's own grays fall under the 3:1 floor on nord0.
	Muted:   tcell.NewHexColor(0x7b88a3),
	OK:      tcell.NewHexColor(0xa3be8c), // aurora green
	Caution: tcell.NewHexColor(0xebcb8b), // aurora yellow
	Failing: tcell.NewHexColor(0xbf616a), // aurora red
	Neutral: tcell.NewHexColor(0xd8dee9), // snow-storm
	Inverse: tcell.NewHexColor(0x2e3440), // polar night
	// Below the ground so severity clears 3:1 on selection.
	SelectionBg:   tcell.NewHexColor(0x21252d),
	SelectionFg:   tcell.NewHexColor(0xeceff4), // brightest snow
	BannerBg:      tcell.NewHexColor(0xd08770), // aurora orange banner (stands out from frost)
	BarHealthy:    tcell.NewHexColor(0x8fbcbb), // frost teal
	ScrollArrow:   tcell.NewHexColor(0x88c0d0), // frost cyan
	ListSecondary: tcell.NewHexColor(0x7b8ca6), // muted slate
}

// gruvbox is the warm retro-earth scheme. Chrome takes gruvbox blue rather
// than its signature gold, which would read as a caution on every border.
var gruvbox = Theme{
	Name:       "gruvbox",
	Background: tcell.NewHexColor(0x282828), // dark0
	Accent:     tcell.NewHexColor(0x83a598), // gruvbox blue
	Muted:      tcell.NewHexColor(0x928374), // gray
	OK:         tcell.NewHexColor(0xb8bb26), // bright green
	Caution:    tcell.NewHexColor(0xfabd2f), // bright yellow
	Failing:    tcell.NewHexColor(0xfb4934), // bright red
	Neutral:    tcell.NewHexColor(0xebdbb2), // light cream
	Inverse:    tcell.NewHexColor(0x282828), // dark0
	// Below the ground so severity clears 3:1 on selection.
	SelectionBg:   tcell.NewHexColor(0x1a1a1a),
	SelectionFg:   tcell.NewHexColor(0xfbf1c7), // light0
	BannerBg:      tcell.NewHexColor(0xfe8019), // bright orange banner (stands out from blue)
	BarHealthy:    tcell.NewHexColor(0x8ec07c), // aqua
	ScrollArrow:   tcell.NewHexColor(0x83a598), // gruvbox blue
	ListSecondary: tcell.NewHexColor(0xa89984), // dim cream
}

// beacon is the colour-vision-deficient-safe palette: a blue → yellow → rose
// severity ramp (Paul Tol's high-contrast set) that stays separable under all
// three CVD types, with neutral chrome so no hue competes with it.
var beacon = Theme{
	Name:          "beacon",
	Background:    tcell.NewHexColor(0x12161c), // near-black slate
	Accent:        tcell.NewHexColor(0xd6dee8), // cool near-white
	Muted:         tcell.NewHexColor(0x6b7785), // slate
	OK:            tcell.NewHexColor(0x6cb4ee), // blue
	Caution:       tcell.NewHexColor(0xeecc66), // yellow
	Failing:       tcell.NewHexColor(0xee7788), // rose
	Neutral:       tcell.NewHexColor(0xe4e9ef), // cool white
	Inverse:       tcell.NewHexColor(0x10151b), // near-black
	SelectionBg:   tcell.NewHexColor(0x2a3542), // dark slate
	SelectionFg:   tcell.NewHexColor(0xf0f4f8), // near-white
	BannerBg:      tcell.NewHexColor(0xeecc66), // yellow banner
	BarHealthy:    tcell.NewHexColor(0x6cb4ee), // blue (matches OK)
	ScrollArrow:   tcell.NewHexColor(0xd6dee8), // near-white
	ListSecondary: tcell.NewHexColor(0x8b97a5), // muted slate
}

// daylight is the cool light palette, tuned against its own paper ground:
// every foreground clears 4:1 on it, and the ramp trades yellow — invisible on
// paper — for a burnt amber that darkens into crimson as it worsens.
var daylight = Theme{
	Name:          "daylight",
	Background:    tcell.NewHexColor(0xfbfbfa), // cool paper
	Accent:        tcell.NewHexColor(0x0a5f9e), // deep azure
	Muted:         tcell.NewHexColor(0x6b7784), // slate
	OK:            tcell.NewHexColor(0x1a7f37), // dark green
	Caution:       tcell.NewHexColor(0xa15c00), // burnt amber
	Failing:       tcell.NewHexColor(0xc1121f), // crimson: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x1f2328), // ink
	Inverse:       tcell.NewHexColor(0xffffff), // white
	SelectionBg:   tcell.NewHexColor(0xc9e0f5), // pale blue
	SelectionFg:   tcell.NewHexColor(0x0b3a5c), // deep blue
	BannerBg:      tcell.NewHexColor(0xa15c00), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x1e8a41), // green, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x0a5f9e), // azure
	ListSecondary: tcell.NewHexColor(0x5b6672), // slate, darker than Muted: it carries data
}

// parchment is the warm light palette: cool teal chrome against a warm ramp,
// tuned against its own paper ground on the same 4:1 basis as daylight.
var parchment = Theme{
	Name:          "parchment",
	Background:    tcell.NewHexColor(0xf4eee1), // warm parchment
	Accent:        tcell.NewHexColor(0x15615a), // deep teal
	Muted:         tcell.NewHexColor(0x746a5b), // warm gray
	OK:            tcell.NewHexColor(0x3f6b25), // olive green
	Caution:       tcell.NewHexColor(0x8f5300), // burnt ochre
	Failing:       tcell.NewHexColor(0xa01f18), // brick red: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x3a352d), // warm ink
	Inverse:       tcell.NewHexColor(0xfbf7ee), // cream
	SelectionBg:   tcell.NewHexColor(0xded0b4), // warm sand
	SelectionFg:   tcell.NewHexColor(0x2c281f), // dark warm ink
	BannerBg:      tcell.NewHexColor(0x8f5300), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x4c7d2a), // olive, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x15615a), // teal
	ListSecondary: tcell.NewHexColor(0x665d50), // warm gray, darker than Muted: it carries data
}

// Coloured grounds: blue, violet and red carry little WCAG luminance, so they still count as dark.

// cobalt is CGA blue promoted from accent to ground, with ice-cyan chrome.
// Failing is a rose rather than a pure red, which falls apart against blue.
var cobalt = Theme{
	Name:          "cobalt",
	Background:    tcell.NewHexColor(0x05146b), // royal blue ground
	Accent:        tcell.NewHexColor(0x7fd7ff), // ice cyan
	Muted:         tcell.NewHexColor(0x8f9fd6), // periwinkle gray
	OK:            tcell.NewHexColor(0x4ade80), // green
	Caution:       tcell.NewHexColor(0xffc233), // amber
	Failing:       tcell.NewHexColor(0xff6b81), // rose red
	Neutral:       tcell.NewHexColor(0xecf1ff), // cool white
	Inverse:       tcell.NewHexColor(0x00103a), // deep navy
	SelectionBg:   tcell.NewHexColor(0x12277f), // one step up from the ground
	SelectionFg:   tcell.NewHexColor(0xeaf1ff), // cool white
	BannerBg:      tcell.NewHexColor(0xffc233), // amber banner (stands out from the blue)
	BarHealthy:    tcell.NewHexColor(0x7fd7ff), // ice cyan
	ScrollArrow:   tcell.NewHexColor(0x7fd7ff), // ice cyan
	ListSecondary: tcell.NewHexColor(0x9aa9de), // periwinkle
}

// ultraviolet is the blacklight palette: a violet ground with orchid chrome
// and orchid bars. Severity leaves the purple family altogether — lime, amber,
// red — since a magenta failing would read as more chrome.
var ultraviolet = Theme{
	Name:          "ultraviolet",
	Background:    tcell.NewHexColor(0x1e0736), // blacklight violet ground
	Accent:        tcell.NewHexColor(0xc77dff), // orchid
	Muted:         tcell.NewHexColor(0x8f77ad), // dusty lilac
	OK:            tcell.NewHexColor(0x7bea5c), // acid lime
	Caution:       tcell.NewHexColor(0xffb703), // amber
	Failing:       tcell.NewHexColor(0xff3b30), // red, the one hue no chrome here uses
	Neutral:       tcell.NewHexColor(0xf0e4ff), // pale lavender
	Inverse:       tcell.NewHexColor(0x14031f), // near-black violet
	SelectionBg:   tcell.NewHexColor(0x33125e), // one step up from the ground
	SelectionFg:   tcell.NewHexColor(0xf7e9ff), // pale lavender
	BannerBg:      tcell.NewHexColor(0xffb703), // amber banner: the root warning is a caution
	BarHealthy:    tcell.NewHexColor(0xc77dff), // orchid
	ScrollArrow:   tcell.NewHexColor(0xc77dff), // orchid
	ListSecondary: tcell.NewHexColor(0xa98cc4), // lilac
}

// deepsea is a petrol-teal ground with aqua chrome and a warm severity ramp,
// so the ramp never shares a hue with the ground it is drawn on.
var deepsea = Theme{
	Name:          "deepsea",
	Background:    tcell.NewHexColor(0x012b3a), // petrol teal ground
	Accent:        tcell.NewHexColor(0x35d6c0), // aqua
	Muted:         tcell.NewHexColor(0x7d9fad), // sea gray
	OK:            tcell.NewHexColor(0x7ee081), // spring green
	Caution:       tcell.NewHexColor(0xffc861), // sand
	Failing:       tcell.NewHexColor(0xff7a6b), // coral
	Neutral:       tcell.NewHexColor(0xe6f6fb), // pale ice
	Inverse:       tcell.NewHexColor(0x00212c), // near-black teal
	SelectionBg:   tcell.NewHexColor(0x053e50), // one step up from the ground
	SelectionFg:   tcell.NewHexColor(0xdff4fb), // pale ice
	BannerBg:      tcell.NewHexColor(0xffb02e), // orange banner (stands out from the teal)
	BarHealthy:    tcell.NewHexColor(0x35d6c0), // aqua
	ScrollArrow:   tcell.NewHexColor(0x35d6c0), // aqua
	ListSecondary: tcell.NewHexColor(0x8fb6c4), // sea gray
}

// oxblood is a wine ground with gold chrome. Failing moves to rose: a red
// severity on a red ground reads as part of the furniture.
var oxblood = Theme{
	Name:          "oxblood",
	Background:    tcell.NewHexColor(0x300711), // wine ground
	Accent:        tcell.NewHexColor(0xffc857), // gold
	Muted:         tcell.NewHexColor(0xb3808c), // dusty rose
	OK:            tcell.NewHexColor(0x7fd18a), // sage green
	Caution:       tcell.NewHexColor(0xffa23a), // orange
	Failing:       tcell.NewHexColor(0xff5470), // rose red, off the ground's own hue
	Neutral:       tcell.NewHexColor(0xffeef0), // warm white
	Inverse:       tcell.NewHexColor(0x250509), // near-black wine
	SelectionBg:   tcell.NewHexColor(0x48101f), // one step up from the ground
	SelectionFg:   tcell.NewHexColor(0xffe6ea), // warm white
	BannerBg:      tcell.NewHexColor(0xffa23a), // orange banner
	BarHealthy:    tcell.NewHexColor(0xffc857), // gold
	ScrollArrow:   tcell.NewHexColor(0xffc857), // gold
	ListSecondary: tcell.NewHexColor(0xc39aa4), // dusty rose
}

// Tinted light palettes: the 4:1 light floor keeps the chrome deep, so the colour lives in the ground.

// sorbet is blush paper with magenta chrome.
var sorbet = Theme{
	Name:          "sorbet",
	Background:    tcell.NewHexColor(0xffe4ec), // blush paper
	Accent:        tcell.NewHexColor(0xb4126b), // magenta
	Muted:         tcell.NewHexColor(0x8a5f70), // mauve gray
	OK:            tcell.NewHexColor(0x1a7f37), // dark green
	Caution:       tcell.NewHexColor(0xa15c00), // burnt amber
	Failing:       tcell.NewHexColor(0xb3123c), // crimson: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x2b1a22), // warm ink
	Inverse:       tcell.NewHexColor(0xfff5f8), // near-white
	SelectionBg:   tcell.NewHexColor(0xf7bcd0), // deeper blush
	SelectionFg:   tcell.NewHexColor(0x3a1020), // dark wine ink
	BannerBg:      tcell.NewHexColor(0xa15c00), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x177238), // green
	ScrollArrow:   tcell.NewHexColor(0xb4126b), // magenta
	ListSecondary: tcell.NewHexColor(0x75505f), // mauve, darker than Muted: it carries data
}

// marigold is warm gold paper with deep teal chrome — the one cool role on the
// page, so focus does not compete with the ground.
var marigold = Theme{
	Name:          "marigold",
	Background:    tcell.NewHexColor(0xffeec2), // gold paper
	Accent:        tcell.NewHexColor(0x0f5f6b), // deep teal
	Muted:         tcell.NewHexColor(0x7a6a45), // khaki
	OK:            tcell.NewHexColor(0x2f6f2f), // forest green
	Caution:       tcell.NewHexColor(0x9a4f00), // burnt ochre
	Failing:       tcell.NewHexColor(0xa81020), // brick red: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x33291a), // warm ink
	Inverse:       tcell.NewHexColor(0xfffaf0), // cream
	SelectionBg:   tcell.NewHexColor(0xf3d489), // deeper gold
	SelectionFg:   tcell.NewHexColor(0x3a2c10), // dark warm ink
	BannerBg:      tcell.NewHexColor(0x9a4f00), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x2f7d3a), // green, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x0f5f6b), // teal
	ListSecondary: tcell.NewHexColor(0x6a5c3c), // khaki, darker than Muted: it carries data
}

// seafoam is mint paper with emerald chrome; OK stays a darker green than the
// ground so healthy still reads as a mark rather than as the page.
var seafoam = Theme{
	Name:          "seafoam",
	Background:    tcell.NewHexColor(0xd9f5e8), // mint paper
	Accent:        tcell.NewHexColor(0x0b6b4a), // emerald
	Muted:         tcell.NewHexColor(0x5d7a70), // sage gray
	OK:            tcell.NewHexColor(0x10693a), // deep green
	Caution:       tcell.NewHexColor(0x97530a), // burnt ochre
	Failing:       tcell.NewHexColor(0xb01030), // crimson: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x16261f), // cool ink
	Inverse:       tcell.NewHexColor(0xf2fffa), // near-white
	SelectionBg:   tcell.NewHexColor(0xa9e3cc), // deeper mint
	SelectionFg:   tcell.NewHexColor(0x123328), // dark green ink
	BannerBg:      tcell.NewHexColor(0x97530a), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x10784a), // green, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x0b6b4a), // emerald
	ListSecondary: tcell.NewHexColor(0x4c6b5f), // sage, darker than Muted: it carries data
}

// sky is azure paper with indigo chrome.
var sky = Theme{
	Name:          "sky",
	Background:    tcell.NewHexColor(0xdbeafe), // azure paper
	Accent:        tcell.NewHexColor(0x1046a0), // indigo
	Muted:         tcell.NewHexColor(0x5a6b84), // slate
	OK:            tcell.NewHexColor(0x14713a), // dark green
	Caution:       tcell.NewHexColor(0x9a5300), // burnt amber
	Failing:       tcell.NewHexColor(0xb3122f), // crimson: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x16202e), // cool ink
	Inverse:       tcell.NewHexColor(0xf5faff), // near-white
	SelectionBg:   tcell.NewHexColor(0xb6d4fb), // deeper azure
	SelectionFg:   tcell.NewHexColor(0x10243d), // deep navy ink
	BannerBg:      tcell.NewHexColor(0x9a5300), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x1a7f46), // green, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x1046a0), // indigo
	ListSecondary: tcell.NewHexColor(0x4b5f7a), // slate, darker than Muted: it carries data
}

// themeList is every built-in palette in cycle order, grouped by family.
var themeList = []Theme{
	dark, electric, phosphor, amber, cga,
	neon, nord, gruvbox, beacon,
	cobalt, ultraviolet, deepsea, oxblood,
	daylight, parchment, sorbet, marigold, seafoam, sky,
	terminal, mono,
}

// themes is the registry by name; themeCycle is the names in cycle order.
var (
	themes     = themesByName(themeList)
	themeCycle = themeNames(themeList)
)

func themesByName(ts []Theme) map[string]Theme {
	m := make(map[string]Theme, len(ts))
	for _, t := range ts {
		m[t.Name] = t
	}
	return m
}

func themeNames(ts []Theme) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
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

// stepThemeName returns the theme delta places from cur in themeCycle, wrapping; an unknown cur starts over.
func stepThemeName(cur string, delta int) string {
	for i, n := range themeCycle {
		if n == cur {
			return themeCycle[(i+delta+len(themeCycle))%len(themeCycle)]
		}
	}
	return themeCycle[0]
}
