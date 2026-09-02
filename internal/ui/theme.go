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

// namedTags maps the palette-index colours a terminal-inheriting palette is
// built from to the markup names tview parses back to the same index. Without
// this, tag() would resolve them to RGB while every style path kept the index,
// and the two would disagree on any terminal whose scheme is not the default —
// which is the bug the dark theme carried until it was spelled in hex.
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

// tag renders a colour as a tview markup token (no brackets); ColorDefault or
// an invalid colour yields "-" so a themeless role degrades to the default.
// A palette-index colour renders as its name rather than as RGB: tview parses
// the name back to the same index, so markup matches what the style paths send.
func tag(c tcell.Color) string {
	if c == tcell.ColorDefault {
		return "-"
	}
	if n, ok := namedTags[c]; ok {
		return n
	}
	h := c.TrueColor().Hex() // TrueColor() for robust RGB on named/palette colours
	if h < 0 {
		return "-"
	}
	return fmt.Sprintf("#%06x", h)
}

func accentTag() string { return "[" + tag(activeTheme.Accent) + "]" }

// unavailableTabTag styles a tab pill the current drive has no data for. Muted
// plus the dim attribute: Muted is the right semantic role, but it collapses to
// the terminal default under mono, where dim still separates the pill — the
// same trick the ● glyph and bold play for severity there.
func unavailableTabTag() string { return "[" + tag(activeTheme.Muted) + "::d]" }

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
	// A palette that inherits the terminal's ground has no band to paint: an
	// explicit SelectionBg would be a guess about a colour it does not know.
	// Reverse video is the attribute answer, and it is the only one that marks
	// the row on a terminal of any palette.
	if activeTheme.SelectionBg == tcell.ColorDefault {
		return tcell.StyleDefault.Reverse(true).Bold(true)
	}
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

// dark reproduces the original palette, spelled in hex. It used to name tcell
// colours (ColorAqua, ColorGray, ...), and a named colour is sent to the
// terminal as a palette index: markup went out as RGB (tag() resolves through
// TrueColor()) while every style path — styleList, cell colours, borders,
// tview.Print — kept the index, so the same role rendered two colours on any
// terminal whose scheme differs from the default. These are the values
// TrueColor() already yielded, so nothing changes on a default terminal and
// nothing changes on any other one either.
//
// The one deliberate departure from the original is Background: #0b0d10 rather
// than pure black, so the palette has a ground to build layers *down* from and
// does not paste a black rectangle into a terminal that is not itself black.
var dark = Theme{
	Name:       "dark",
	Background: tcell.NewHexColor(0x0b0d10), // near-black, one step off #000
	Accent:     tcell.NewHexColor(0x00ffff), // aqua
	Muted:      tcell.NewHexColor(0x808080), // gray
	OK:         tcell.NewHexColor(0x008000), // green
	Caution:    tcell.NewHexColor(0xffff00), // yellow
	Failing:    tcell.NewHexColor(0xff0000), // red
	// Explicit, not ColorDefault: the ground is painted, so inherited text
	// lands black on black in a light terminal.
	Neutral: tcell.NewHexColor(0xffffff), // white
	Inverse: tcell.NewHexColor(0x000000), // black
	// Was DarkSlateGray (#2f4f4f), which is bright enough to swallow the
	// severity colours drawn on it — red fell to 2.23:1 and green to 1.74:1 on
	// the one row the cursor is always sitting on.
	SelectionBg: tcell.NewHexColor(0x16202a),
	SelectionFg: tcell.NewHexColor(0xffffff), // white
	BannerBg:    tcell.NewHexColor(0xffff00), // yellow
	BarHealthy:  tcell.NewHexColor(0x008080), // teal
	ScrollArrow: tcell.NewHexColor(0xffffff), // white
	// Was green (== OK), which painted a failing drive's metadata line the
	// healthy colour.
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

// terminal is the coloured counterpart to mono: it keeps the terminal's own
// ground and body colour and adds back the severity vocabulary. It is the one
// palette deliberately built from NAMED colours — they resolve through the
// user's own scheme, which is the point rather than the bug here: ground and
// foreground then come from the same source and cannot disagree. The contrast
// invariants the other palettes are held to are unmeasurable for it, exactly
// as for mono, and it opts out of them by construction.
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
	// No band: selectedRowStyle draws reverse video instead, which is the only
	// highlight that works without knowing the ground.
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
	SelectionBg:   tcell.NewHexColor(0x123610), // dark green selected-row bg (recessed: 0x0f4f0f left OK at 2.78:1)
	SelectionFg:   tcell.NewHexColor(0xd6ffd6), // pale green text on selection
	BannerBg:      tcell.NewHexColor(0x4dff4d), // bright-green root-warning banner (was amber); dark Inverse text reads on it
	BarHealthy:    tcell.NewHexColor(0x33ff33), // FARM healthy bar: neon green
	ScrollArrow:   tcell.NewHexColor(0x33ff33), // neon green arrows
	ListSecondary: tcell.NewHexColor(0x1f9f1f), // dim green secondary line
}

// amber is the Hercules amber-monitor palette with an amber→orange→red
// severity ramp. All-hex.
var amber = Theme{
	Name:       "amber",
	Background: tcell.NewHexColor(0x140a00), // brown-black monitor ground
	Accent:     tcell.NewHexColor(0xffb000), // bright amber: borders, headers, active tab, key hints
	Muted:      tcell.NewHexColor(0x8a5a10), // dim brown-amber: dashes, unfocused border, raw values
	// Below Caution, not above it: gold at 13:1 made every healthy drive the
	// brightest thing in the list, and gold reads as a warning.
	OK:            tcell.NewHexColor(0xcf9426), // dark gold (healthy)
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
	SelectionBg:   tcell.NewHexColor(0x2b1733), // deep violet selected-row bg, dark enough not to out-shout a failing row
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
	Muted:   tcell.NewHexColor(0x7b88a3),
	OK:      tcell.NewHexColor(0xa3be8c), // aurora green
	Caution: tcell.NewHexColor(0xebcb8b), // aurora yellow
	Failing: tcell.NewHexColor(0xbf616a), // aurora red
	Neutral: tcell.NewHexColor(0xd8dee9), // snow-storm body text
	Inverse: tcell.NewHexColor(0x2e3440), // polar night: text drawn on Accent / BannerBg
	// Below nord0 rather than above it: nord2 (#434c5e) is light enough to
	// drop aurora red to 2.11:1, the dimmest severity-on-selection in the set.
	SelectionBg:   tcell.NewHexColor(0x21252d),
	SelectionFg:   tcell.NewHexColor(0xeceff4), // brightest snow on selection
	BannerBg:      tcell.NewHexColor(0xd08770), // aurora orange banner (stands out from frost)
	BarHealthy:    tcell.NewHexColor(0x8fbcbb), // frost teal FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x88c0d0), // frost cyan arrows
	ListSecondary: tcell.NewHexColor(0x7b8ca6), // muted slate secondary line
}

// gruvbox is the warm retro-earth scheme. Chrome takes gruvbox blue rather
// than its signature gold, which would read as a caution on every border.
var gruvbox = Theme{
	Name:       "gruvbox",
	Background: tcell.NewHexColor(0x282828), // dark0
	Accent:     tcell.NewHexColor(0x83a598), // gruvbox blue: borders, headers, active tab, key hints
	Muted:      tcell.NewHexColor(0x928374), // gray: dashes, unfocused border, raw values
	OK:         tcell.NewHexColor(0xb8bb26), // bright green
	Caution:    tcell.NewHexColor(0xfabd2f), // bright yellow
	Failing:    tcell.NewHexColor(0xfb4934), // bright red
	Neutral:    tcell.NewHexColor(0xebdbb2), // light cream body text
	Inverse:    tcell.NewHexColor(0x282828), // dark0: text drawn on Accent / BannerBg
	// Below dark0 rather than dark2: dark2 (#504945) dropped bright red to
	// 2.56:1. Off-palette, like the blue chrome choice above it.
	SelectionBg:   tcell.NewHexColor(0x1a1a1a),
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

// The four palettes below take a strongly coloured ground rather than a
// tinted near-black. Blue, violet and red carry little of the WCAG luminance
// weight, so a ground can be plainly a colour and still sit inside the dark
// band TestGroundsAreDecisivelyDarkOrLight requires; a green one cannot, which
// is why there is no dark green here.

// cobalt is CGA blue promoted from accent to ground, with ice-cyan chrome.
// Failing is a rose rather than a pure red, which falls apart against blue.
var cobalt = Theme{
	Name:          "cobalt",
	Background:    tcell.NewHexColor(0x05146b), // royal blue ground
	Accent:        tcell.NewHexColor(0x7fd7ff), // ice cyan: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x8f9fd6), // periwinkle gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x4ade80), // green
	Caution:       tcell.NewHexColor(0xffc233), // amber
	Failing:       tcell.NewHexColor(0xff6b81), // rose red
	Neutral:       tcell.NewHexColor(0xecf1ff), // cool white body text
	Inverse:       tcell.NewHexColor(0x00103a), // deep navy: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x12277f), // one step up from the ground
	SelectionFg:   tcell.NewHexColor(0xeaf1ff), // cool white on selection
	BannerBg:      tcell.NewHexColor(0xffc233), // amber banner (stands out from the blue)
	BarHealthy:    tcell.NewHexColor(0x7fd7ff), // ice cyan FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x7fd7ff), // ice cyan arrows
	ListSecondary: tcell.NewHexColor(0x9aa9de), // periwinkle secondary line
}

// ultraviolet is a deep violet ground with cyan chrome and a magenta banner —
// the warning colour has to leave the violet family or it reads as chrome.
var ultraviolet = Theme{
	Name:          "ultraviolet",
	Background:    tcell.NewHexColor(0x2b0b52), // deep violet ground
	Accent:        tcell.NewHexColor(0x67e8f9), // cyan: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0xa78bc9), // dusty lilac: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x4ade80), // green
	Caution:       tcell.NewHexColor(0xfbbf24), // amber
	Failing:       tcell.NewHexColor(0xff5c8a), // hot pink-red
	Neutral:       tcell.NewHexColor(0xf3e9ff), // pale violet-white body text
	Inverse:       tcell.NewHexColor(0x1a0433), // near-black violet: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x3a1070), // one step up from the ground
	SelectionFg:   tcell.NewHexColor(0xfbeaff), // pale violet-white on selection
	BannerBg:      tcell.NewHexColor(0xf472b6), // magenta banner
	BarHealthy:    tcell.NewHexColor(0x22d3ee), // cyan FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x67e8f9), // cyan arrows
	ListSecondary: tcell.NewHexColor(0xb39ddb), // lilac secondary line
}

// deepsea is a petrol-teal ground with aqua chrome and a warm severity ramp,
// so the ramp never shares a hue with the ground it is drawn on.
var deepsea = Theme{
	Name:          "deepsea",
	Background:    tcell.NewHexColor(0x012b3a), // petrol teal ground
	Accent:        tcell.NewHexColor(0x35d6c0), // aqua: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x7d9fad), // sea gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x7ee081), // spring green
	Caution:       tcell.NewHexColor(0xffc861), // sand
	Failing:       tcell.NewHexColor(0xff7a6b), // coral
	Neutral:       tcell.NewHexColor(0xe6f6fb), // pale ice body text
	Inverse:       tcell.NewHexColor(0x00212c), // near-black teal: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x053e50), // one step up from the ground
	SelectionFg:   tcell.NewHexColor(0xdff4fb), // pale ice on selection
	BannerBg:      tcell.NewHexColor(0xffb02e), // orange banner (stands out from the teal)
	BarHealthy:    tcell.NewHexColor(0x35d6c0), // aqua FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0x35d6c0), // aqua arrows
	ListSecondary: tcell.NewHexColor(0x8fb6c4), // sea gray secondary line
}

// oxblood is a wine ground with gold chrome. Failing moves to rose: a red
// severity on a red ground reads as part of the furniture.
var oxblood = Theme{
	Name:          "oxblood",
	Background:    tcell.NewHexColor(0x300711), // wine ground
	Accent:        tcell.NewHexColor(0xffc857), // gold: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0xb3808c), // dusty rose: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x7fd18a), // sage green
	Caution:       tcell.NewHexColor(0xffa23a), // orange
	Failing:       tcell.NewHexColor(0xff5470), // rose red, off the ground's own hue
	Neutral:       tcell.NewHexColor(0xffeef0), // warm white body text
	Inverse:       tcell.NewHexColor(0x250509), // near-black wine: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0x48101f), // one step up from the ground
	SelectionFg:   tcell.NewHexColor(0xffe6ea), // warm white on selection
	BannerBg:      tcell.NewHexColor(0xffa23a), // orange banner
	BarHealthy:    tcell.NewHexColor(0xffc857), // gold FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0xffc857), // gold arrows
	ListSecondary: tcell.NewHexColor(0xc39aa4), // dusty rose secondary line
}

// The four light palettes below tint the paper rather than the ink. Every
// foreground is held to the 4:1 light floor, which is why the chrome is deep
// even where the ground is loud: on paper the colour lives in the ground.

// sorbet is blush paper with magenta chrome.
var sorbet = Theme{
	Name:          "sorbet",
	Background:    tcell.NewHexColor(0xffe4ec), // blush paper
	Accent:        tcell.NewHexColor(0xb4126b), // magenta: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x8a5f70), // mauve gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x1a7f37), // dark green
	Caution:       tcell.NewHexColor(0xa15c00), // burnt amber
	Failing:       tcell.NewHexColor(0xb3123c), // crimson: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x2b1a22), // warm ink body text
	Inverse:       tcell.NewHexColor(0xfff5f8), // near-white: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0xf7bcd0), // deeper blush selected-row band
	SelectionFg:   tcell.NewHexColor(0x3a1020), // dark wine ink on selection
	BannerBg:      tcell.NewHexColor(0xa15c00), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x177238), // green FARM healthy bar
	ScrollArrow:   tcell.NewHexColor(0xb4126b), // magenta arrows
	ListSecondary: tcell.NewHexColor(0x75505f), // mauve secondary line, darker than Muted: it carries data
}

// marigold is warm gold paper with deep teal chrome — the one cool role on the
// page, so focus does not compete with the ground.
var marigold = Theme{
	Name:          "marigold",
	Background:    tcell.NewHexColor(0xffeec2), // gold paper
	Accent:        tcell.NewHexColor(0x0f5f6b), // deep teal: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x7a6a45), // khaki: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x2f6f2f), // forest green
	Caution:       tcell.NewHexColor(0x9a4f00), // burnt ochre
	Failing:       tcell.NewHexColor(0xa81020), // brick red: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x33291a), // warm ink body text
	Inverse:       tcell.NewHexColor(0xfffaf0), // cream: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0xf3d489), // deeper gold selected-row band
	SelectionFg:   tcell.NewHexColor(0x3a2c10), // dark warm ink on selection
	BannerBg:      tcell.NewHexColor(0x9a4f00), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x2f7d3a), // green FARM healthy bar, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x0f5f6b), // teal arrows
	ListSecondary: tcell.NewHexColor(0x6a5c3c), // khaki secondary line, darker than Muted: it carries data
}

// seafoam is mint paper with emerald chrome; OK stays a darker green than the
// ground so healthy still reads as a mark rather than as the page.
var seafoam = Theme{
	Name:          "seafoam",
	Background:    tcell.NewHexColor(0xd9f5e8), // mint paper
	Accent:        tcell.NewHexColor(0x0b6b4a), // emerald: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x5d7a70), // sage gray: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x10693a), // deep green
	Caution:       tcell.NewHexColor(0x97530a), // burnt ochre
	Failing:       tcell.NewHexColor(0xb01030), // crimson: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x16261f), // cool ink body text
	Inverse:       tcell.NewHexColor(0xf2fffa), // near-white: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0xa9e3cc), // deeper mint selected-row band
	SelectionFg:   tcell.NewHexColor(0x123328), // dark green ink on selection
	BannerBg:      tcell.NewHexColor(0x97530a), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x10784a), // green FARM healthy bar, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x0b6b4a), // emerald arrows
	ListSecondary: tcell.NewHexColor(0x4c6b5f), // sage secondary line, darker than Muted: it carries data
}

// sky is azure paper with indigo chrome.
var sky = Theme{
	Name:          "sky",
	Background:    tcell.NewHexColor(0xdbeafe), // azure paper
	Accent:        tcell.NewHexColor(0x1046a0), // indigo: borders, headers, active tab, key hints
	Muted:         tcell.NewHexColor(0x5a6b84), // slate: dashes, unfocused border, raw values
	OK:            tcell.NewHexColor(0x14713a), // dark green
	Caution:       tcell.NewHexColor(0x9a5300), // burnt amber
	Failing:       tcell.NewHexColor(0xb3122f), // crimson: darkest and most saturated of the ramp
	Neutral:       tcell.NewHexColor(0x16202e), // cool ink body text
	Inverse:       tcell.NewHexColor(0xf5faff), // near-white: text drawn on Accent / BannerBg
	SelectionBg:   tcell.NewHexColor(0xb6d4fb), // deeper azure selected-row band
	SelectionFg:   tcell.NewHexColor(0x10243d), // deep navy ink on selection
	BannerBg:      tcell.NewHexColor(0x9a5300), // the root warning is a caution, so it takes Caution
	BarHealthy:    tcell.NewHexColor(0x1a7f46), // green FARM healthy bar, one step lighter than OK
	ScrollArrow:   tcell.NewHexColor(0x1046a0), // indigo arrows
	ListSecondary: tcell.NewHexColor(0x4b5f7a), // slate secondary line, darker than Muted: it carries data
}

// themes is the registry of built-in palettes; themeCycle gives the stable
// order for the cycle key and ThemeNames.
var themes = map[string]Theme{
	"dark":        dark,
	"mono":        mono,
	"terminal":    terminal,
	"electric":    electric,
	"phosphor":    phosphor,
	"amber":       amber,
	"cga":         cga,
	"neon":        neon,
	"nord":        nord,
	"gruvbox":     gruvbox,
	"beacon":      beacon,
	"daylight":    daylight,
	"parchment":   parchment,
	"cobalt":      cobalt,
	"ultraviolet": ultraviolet,
	"deepsea":     deepsea,
	"oxblood":     oxblood,
	"sorbet":      sorbet,
	"marigold":    marigold,
	"seafoam":     seafoam,
	"sky":         sky,
}

// themeCycle groups the palettes by family — retro hardware, then the modern
// dark schemes, then the coloured grounds, then the light set — so cycling
// walks related looks together.
var themeCycle = []string{
	"dark", "electric", "phosphor", "amber", "cga",
	"neon", "nord", "gruvbox", "beacon",
	"cobalt", "ultraviolet", "deepsea", "oxblood",
	"daylight", "parchment", "sorbet", "marigold", "seafoam", "sky",
	"terminal", "mono",
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
