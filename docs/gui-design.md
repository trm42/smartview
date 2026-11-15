# smartview GUI: design and plan

A native-looking desktop front end for smartview on macOS and Linux, sharing
the existing data layer with the terminal UI. This is a plan, not an
implementation; nothing here changes the TUI.

## Goals

- Native windowing, menus, dialogs and widgets on both targets: a Mac user
  gets a Mac app, a KDE or GNOME user gets a desktop app, not a web page or a
  skinned canvas.
- One codebase, one toolkit, Go throughout. `internal/smart` and
  `internal/config` are reused unchanged.
- Feature parity with the TUI's six tabs, the fleet comparison and the
  self-test actions. The TUI stays the primary interface and keeps shipping.

Non-goals: Windows (smartmontools runs there, but the data layer's device
naming and privilege model are untested on it), SCSI/SAS (see TODO.md),
and a redesign of the data model.

## Toolkit choice

Candidates, judged on "native as possible" first and Go fit second:

| Toolkit | Widgets | Mac look | Linux look | Go binding | Cost |
|---|---|---|---|---|---|
| **Qt 6 Widgets** | platform style | QMacStyle, system menu bar, dark mode | follows desktop (Breeze on KDE, Adwaita via platform theme on GNOME, Fusion fallback) | [miqt](https://github.com/mappu/miqt), MIT, cgo | Qt toolchain, ~10 min first build, bundle Qt |
| GTK 4 + libadwaita | GTK's own | foreign (GNOME widgets on a Mac) | native on GNOME only | [gotk4](https://github.com/diamondburned/gotk4), cgo | long generated-binding builds |
| Wails v3 | HTML/CSS in a webview | native window and menus, web content | same | pure Go + JS | UI is a web page, not widgets |
| Fyne / Gio | custom drawn | Material-ish | same everywhere | pure Go | nothing native beyond the window |
| AppKit + GTK, two front ends | native | native (DarwinKit) | native (gotk4-adwaita) | both cgo | two UIs to write and keep in sync; DarwinKit does not recommend itself for complex apps |

**Decision: Qt 6 Widgets through miqt.** It is the only single toolkit that
draws with the platform's own style on both targets: on macOS the widgets are
rendered by `QMacStyle`, the menu bar goes into the system bar, `⌘,` opens
Preferences and light/dark follow the system; on Linux Qt picks up the
desktop's platform theme, so the app matches Plasma natively and GNOME
through `adwaita-qt`/QGnomePlatform where the distro ships them, falling back
to Fusion. Native file dialogs, message boxes, system tray and notifications
come with it.

What it costs, stated up front:

- **cgo.** The TUI's pure-Go, `CGO_ENABLED=0` static binary stays as it is;
  the GUI is a second build that needs a Qt C++ toolchain and either bundles
  Qt or depends on the distro's Qt packages. miqt binds Qt 6.4+ (and 5.15),
  so the floor is a distro with Qt 6.4: Ubuntu 24.04, Debian 12, Fedora
  current. Older distros get the bundled AppImage.
- **Licensing.** Qt is LGPLv3 and smartview is GPL-3.0-or-later, so dynamic
  linking is fine and static linking is allowed under the GPL as well. The
  bindings are MIT.
- **Binding maturity.** miqt describes itself as possibly immature: wrapper
  pointers do not compare equal, Qt containers come back as Go types, and Qt
  objects must be touched from the main thread via its helper. None of these
  blocks a widgets app of this size, but they shape the architecture below.
- **GNOME is the weakest look.** Without a Qt platform theme installed, a Qt
  app on GNOME renders in Fusion. That is a tidy, neutral style, not a wrong
  one, and the packaging section recommends the theme plugin as a dependency.

## Architecture

```
internal/config  <- unchanged
internal/smart   <- unchanged (the data layer)
internal/view    <- NEW: toolkit-neutral presentation and polling
internal/ui      -> view, smart, config   (tview, the TUI)
internal/gui     -> view, smart, config   (Qt via miqt)
main             -> ui        (smartview, pure Go)
cmd/smartview-gui -> gui      (cgo)
```

### `internal/view`: the shared layer

Most of what `internal/ui` knows is not about tview. Extracting it is the
first milestone and leaves the TUI behaviourally identical, which its existing
tests prove. It carries:

- **State and polling.** `reports`, `history`, `asleep`, `unreadable`,
  `lastRead` and the rules `applyResults` enforces (a standby or no-health
  envelope never replaces a good report; the temperature ring buffer capped
  at `maxHistory`). The poll loop becomes a `Poller` that runs the fetchers
  on its goroutine and delivers each result through a callback; each front
  end marshals that onto its own main thread (tview's `QueueUpdateDraw`,
  miqt's main-thread dispatch). `SkipStandby` and the live interval ladder
  (`intervalPresets`, `nextInterval`) move with it.
- **Readings and wording.** The pure formatters (`humanBytes`,
  `humanDuration`, `capacityString`, `kindLabel`, `tempSeverity`), the glyph
  vocabulary (`severityGlyph`, `reportGlyph`, `reportVerdict`), the
  attribute explanations in `smartdesc.go`, and the self-test estimate
  wording. Anything that emits tview markup (`esc`, the `*Tag` helpers,
  `sevVerdict`) stays in `ui`.
- **Structure.** `allTabs` and its availability predicates, the attribute
  sort and filter orders, the fleet sections (columns, `cells`, `rank`,
  `available`, `legend`) with their values delivered as typed cells rather
  than strings, and the chart maths (`downsample` by bucket maximum,
  data-minimum baseline, `bucketMax`) with the glyph fill left in `ui`.

The rule for what moves: if a test for it would run without a tcell
simulation screen, it belongs in `view`.

### `internal/gui`

One `QApplication`, one `QMainWindow`. Every widget mutation happens on the
Qt main thread; the poller's callback hops there, exactly as the TUI's rule
that all tview mutation happens inside `QueueUpdateDraw`. Device strings are
plain text in Qt (no markup to escape), but `stripControl` still applies
before anything drive-controlled reaches a label.

Fixture mode is kept: `smartview-gui --fixtures DIR` on a `-tags dev` build
loads the same captured JSON through `smart.UseFixtures`, so the GUI is
verifiable with no drives attached and the screenshots for the docs come
from the same fixtures the TUI's do.

## Window design

### Main window

```
┌ smartview ──────────────────────────────────────────────────────────────┐
│ File  View  Drive  Help                      (macOS: system menu bar)   │
│ [⟳ Refresh] [Fleet] [interval ▾]                        ▲ 1  ◌ 1        │
├───────────────┬─────────────────────────────────────────────────────────┤
│ ● WD Blue SN  │ Overview | Attributes | Statistics | FARM | Logs | Tests│
│   nvme0  1 TB │                                                         │
│ ▲ ST4000VN    │   Model      ST4000VN006-3CW104                         │
│   sda  4 TB   │   Serial     ...                                        │
│ ◌ ST4000VN    │   Firmware   ...                                        │
│   sdb  4 TB   │   Capacity   4.00 TB (512 B logical / 4096 B physical)  │
│ ? Apple SSD   │                                                         │
│   disk0       │   Temperature  ▁▂▂▃▃▅▅▆▆▇▇  38 °C                       │
├───────────────┴─────────────────────────────────────────────────────────┤
│ Read 12 s ago · next in 18 s                      smartctl 7.4 · root ✓ │
└─────────────────────────────────────────────────────────────────────────┘
```

- **Sidebar**: a `QListView` on a model backed by `view`'s device list,
  inside a `QSplitter` so the width is draggable and remembered. Each row is
  the health glyph, model (plus device when `duplicateModels` says the model
  is repeated), and a secondary line of device, capacity and temperature,
  drawn by a delegate so the glyph keeps its severity colour and shape under
  selection. Selection is tracked by device name, as the fleet already does.
- **Detail**: a `QTabWidget`, one page per entry in `allTabs`. Default
  behaviour matches the TUI: a tab without data is removed; with
  `show_unavailable_tabs` it is present but disabled, so its position is
  stable across drives. On macOS the tab bar is the native document style.
- **Toolbar**: Refresh, Fleet toggle, and a refresh-interval `QComboBox`
  over the `intervalPresets` ladder. The right end carries the `▲ N` / `◌ N`
  / `⊘ N` counts that the TUI's list title carries, since they are the one
  answer to "is anything wrong?" that does not require reading every row.
- **Status bar**: last-read age and next-poll countdown on the left,
  smartctl version and the privilege state on the right. The TUI's root
  banner becomes a `QMessageBox`-style info bar under the toolbar, dismissed
  per session, with a button that invokes the privilege flow below.
- **Menus** use the platform's conventions: the app menu with About and
  Preferences (`⌘,`) on macOS, `Edit ▸ Preferences` on Linux; `Drive` holds
  Refresh (`⌘R`), Start short test, Start long test, Abort test; `View`
  holds the tabs (`⌘1`–`⌘6`), Fleet (`⌘⇧F`), Show unavailable tabs; `Help`
  holds the attribute glossary and a link to the README.

### Tabs

- **Overview**: a two-column `QFormLayout` for identity, the wear gauges as
  `QProgressBar`s (life used drains, spare fills, both graded through
  `PctUsedSeverity`/`SpareSeverity` exactly as the TUI's bars are), and the
  temperature history as a custom `QWidget` painting with `QPainter`. The
  chart keeps every rule from `chart.go`: it scales to the data minimum,
  never to zero; downsamples by bucket maximum; fills under the series;
  takes the healthy colour in band and a severity colour only out of it.
- **Attributes**: a `QTableView` on a sortable model. The header gives the
  sort (the TUI's `s`) and a search field above the table gives the filter
  (`f`). Row severity is a background tint plus the glyph in the first
  column, never colour alone. Selecting a row shows its explanation from
  `smartdesc` in a pane below, as the TUI does.
- **Statistics**: a `QTreeView`, one top-level node per Device Statistics
  page, which replaces the TUI's wrapped text with a collapsible structure.
- **FARM**: four `QGroupBox`es in a `QGridLayout` that reflows to one column
  below the detail pane's `farmColumnMin`, and two per-head bar charts on the
  same custom chart widget. Bars share a bucket (maximum) before any falls
  off the edge, with the caption saying how many share.
- **Logs**: two tables, error log above self-test history. A passed test
  renders in body text, not green, and the decoded error entries keep their
  expandable detail as a child row.
- **Tests**: Short / Long radio buttons, estimated durations from
  `SelfTestEstimates`, a Start button that confirms in a native
  `QMessageBox`, and a `QProgressBar` with Abort while a test runs. Errors
  arrive as a modal `QMessageBox` on the main thread, so the TUI's
  `pendingNotices` queue has no equivalent.

### Fleet comparison

A second page in a `QStackedWidget`, toggled from the toolbar and `⌘⇧F`,
with `Esc` returning to the drive view. A `QTableView` with one column group
per `fleetSection`; the focus metric is a segmented control above the table
and the header click flips between metric and device order (`s`). Absent
readings stay `—`, approximate write totals keep the `~` and the legend's
caveat. The native header sort indicator replaces the TUI's legend line
about ordering, and columns scroll horizontally rather than being dropped,
since a window can grow where a terminal cannot.

### Preferences

A dialog from `view`'s settings list: refresh interval, standby-aware
polling (with the *ATA drives only* caveat as the field's help text), show
unavailable tabs, start view. It writes through the same `save func` main
supplies, so `config.Save`'s commented template and temp+rename stay the one
writer. The TUI's `theme` key is ignored by the GUI, which follows the system
appearance; window geometry and splitter position go to `QSettings`, not the
TOML, so `internal/config` needs no new keys.

## Appearance and severity

The GUI does not port the 21 TUI palettes. It takes the platform's light or
dark appearance and its accent colour, and supplies only the three severity
colours, tuned per appearance so caution stays legible on paper (burnt amber,
not yellow) and failing stays hotter than caution on ink. The TUI's
load-bearing rules carry over: severity is encoded by shape and area before
colour (`●`/`▲`/`■`, and a filled chip for a failing verdict); colour marks
exceptions, not membership, so a healthy fleet is not a wall of green; `—`
means not reported, never zero. A colour-vision-safe option in Preferences
swaps in the `beacon` ramp.

## Privileges

Full SMART access usually needs root, and a GUI must not run as root. The
plan has two steps so the first release does not wait on the second.

1. **Degrade as the TUI does.** Run unprivileged, show the info bar, render
   what smartctl returns (`?` No data where the open failed).
2. **A privileged helper.** `smartview --serve` runs the data layer behind a
   line-delimited JSON protocol on stdio: `scan`, `info`, `farm`,
   `selftest`, `abort`, each mirroring one `smart` function with its
   `PowerPolicy`. The GUI launches it once per session through the platform's
   own authorisation: `pkexec` with a polkit action installed by the Linux
   packages, and on macOS an administrator prompt via `osascript` for the
   first release, with a `SMAppService` helper as the proper follow-up. The
   same serve mode is the `--json` headless mode TODO.md already lists, so
   it pays for itself twice. Device names cross the protocol verbatim.

The helper runs only `smartctl` with arguments the data layer builds, as
today. Nothing in the protocol takes a free-form command line.

## Packaging and CI

- **macOS**: an `.app` bundle assembled by `macdeployqt`, shipped as a
  `.dmg` and as a Homebrew cask beside the TUI's. Signing and notarisation
  need a Developer ID; without one the cask has to document the Gatekeeper
  override, which is an open question below.
- **Linux**: an AppImage built with `linuxdeploy` and its Qt plugin for any
  distro, plus `.deb`/`.rpm` that depend on the distro's `qt6-base` and
  recommend the GNOME platform theme. Flatpak is deliberately not planned:
  its sandbox blocks the raw device access and `pkexec` path the helper
  needs.
- **GoReleaser** keeps building the TUI as it does. The GUI is a separate
  `builds` entry with `CGO_ENABLED=1` built on a native runner per OS
  (macOS runner for darwin/arm64, Ubuntu 24.04 for linux/amd64) with Qt from
  `aqtinstall`, and its artifacts attached to the same release. The dev
  Dockerfile from `miqt-docker` is the fallback for local cross-builds.
- **CI** gates the GUI on `go vet -tags dev ./...`, the `view` and `gui`
  unit tests, and a smoke run of the fixture build under
  `QT_QPA_PLATFORM=offscreen` that opens the window, walks every tab and
  the fleet view, and exits. The existing four required checks are untouched;
  a GUI check becomes required only once it has reported on `pull_request`
  for a while, per the repository ruleset note in CLAUDE.md.

## Testing

- `view` inherits the pure tests from `ui` that move with their code, and
  keeps the fixture-backed pattern: every reading and label is asserted
  against the captured drive JSON in `internal/smart/testdata`.
- `gui` is tested in two patterns, mirroring the TUI's: a model test that
  feeds `view` state into the Qt item models and asserts rows, roles and
  sort order with no window; and an offscreen test that drives the real
  window through `QTest`-style key and click injection for the deadlock,
  focus and tab-availability cases `layout_test.go` covers today.
- Screenshots for the docs come from `QWidget.grab` on the fixture build,
  the GUI analogue of the `capturing-screenshots` skill's expect script.

## Phases

| # | Milestone | Done when |
|---|---|---|
| 0 | **Spike** | A miqt window lists the fixture drives on a Mac and an Ubuntu 24.04 box, built from one `go build`, and the Qt bundle size and build time are measured. Go/no-go on miqt; the fallback is the two-front-end option. |
| 1 | **Extract `internal/view`** | The TUI is behaviourally unchanged, every existing test passes, and `internal/ui` no longer owns polling state or protocol-neutral formatting. |
| 2 | **GUI MVP** | Main window, sidebar, Overview, Attributes, Statistics, Logs; system appearance; fixture mode; `--interval` and the toolbar interval control. |
| 3 | **Parity** | FARM, Tests, Fleet, Preferences, menus and shortcuts, the info bar. |
| 4 | **Helper and headless** | `--serve` and `--json`, polkit action, macOS admin prompt. |
| 5 | **Release** | `.app`/`.dmg`, AppImage, `.deb`/`.rpm`, CI smoke test, README section and screenshots. |

Phase 1 is worth doing even if the spike says no: it tightens the TUI's own
layering and is the smallest possible first PR.

## Open questions

- **Apple Developer ID.** Notarisation is the difference between a
  double-clickable `.app` and one that needs a right-click override. Decide
  before phase 5.
- **Qt floor.** miqt's 6.4 floor excludes Ubuntu 22.04's packaged Qt 6.2;
  either the AppImage is the only route there or the floor is accepted.
- **GNOME theme dependency.** Recommend `qgnomeplatform-qt6` and
  `adwaita-qt6` in the Linux packages, or depend on them? Recommend, since
  Fusion is acceptable and a hard dependency on a theme plugin is unusual.
- **One binary or two.** `smartview --gui` would share a binary but force
  cgo and a Qt dependency on every TUI user; the plan keeps two binaries.
- **Standby validation.** The GUI inherits the TODO.md item: `-n standby`
  has still not been exercised on a real parked SATA drive.
