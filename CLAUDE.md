# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

smartview is a cross-platform (macOS + Linux) terminal UI for monitoring drive
health via smartmontools. It shells out to `smartctl -j` and renders the parsed
JSON in a tview-based dashboard. Licensed GPL-3.0-or-later (every `.go` file
carries an SPDX header; keep it on new files).

## Commands

```sh
go build -o smartview .        # build
go run .                       # run (refresh defaults to 30s; --interval 10s sets the
                               # starting cadence, runtime +/- keys adjust it live)
go test ./...                  # all tests
go test ./internal/smart/ -run TestParseNVMe   # single test
go vet ./... && gofmt -l .     # vet + format check (gofmt -l should print nothing)
```

Runtime needs `smartctl` (smartmontools ≥ 7.0) on PATH; full attribute access
often requires `sudo`. Driving the TUI for verification (no tmux in this env):
build, then use `expect` to spawn under a pty, `sleep`, `send` keys, and `send
"q"` to quit.

### Fixture dev mode (eyeballing UI without real drives)

```sh
go build -tags dev -o smartview .                  # dev build with fixture support
./smartview --fixtures internal/smart/testdata     # render captured fixtures
```

This is the canonical way to verify UI changes for hardware not on hand.
`--fixtures DIR` loads every `*.json` in DIR as drive data — the committed
`internal/smart/testdata/` fixtures cover ATA, NVMe, sparse Apple NVMe, and a
Seagate FARM log. Fixture mode **bypasses the smartctl preflight** entirely, so
smartmontools need not be installed. Drive the resulting TUI with `expect` under
a pty (`sleep`, `send` keys, `send "q"` to quit), exactly as above. Two
fixtures (`smart-sda-errors.json`, `smart-nvme-errors.json`) are hand-crafted
copies with populated error-log tables and unique device names — the only ones
that exercise the Logs tab's decoded-error-entry renderer, since every captured
fixture has an empty error log.

`--fixtures` is only honored by a `-tags dev` build: a release build (plain
`go build`) still accepts the flag but rejects it at startup with a rebuild
hint (`rebuild with: go build -tags dev`).

## Architecture

Two packages with a hard one-way boundary: **`internal/smart` is the data layer
and has no tview dependency**; **`internal/ui` is the presentation layer**.
`main.go` wires flags + a smartctl preflight and starts the UI.

### Data flow

`ui.App.pollLoop` (poll.go) runs a ticker on a background goroutine →
`smart.Info()` shells out to `smartctl -j -x <device>` → the parsed `*smart.Report`
is applied to UI state **only inside `app.QueueUpdateDraw`**. tview is not
goroutine-safe; all widget mutation must happen there. App state maps
(`reports`, `history`) are therefore touched only on the event-loop goroutine and
need no mutex.

### Non-obvious invariants (read before changing the data layer)

- **smartctl's exit status is a bitmask, not success/failure.** It is routinely
  non-zero (e.g. 4) on a perfectly healthy drive. `smart.run()` returns stdout
  *alongside* the error; `Info`/`Scan` parse the JSON regardless of exit code.
  Never gate parsing on the exit code. Real failures surface via
  `smartctl.messages` (`Report.FatalMessage`).
- **The JSON schema is sparse and drive-dependent.** Only `device`, `smartctl`,
  and `smart_status` are reliably present (Apple internal SSDs omit capacity,
  logs, etc.). Every other field in `types.go` is a pointer or slice so absent
  sections decode to nil. Nil-check before dereferencing; never assume a section
  exists. New fixtures should preserve this — `testdata/smart-apple-nvme.json`
  is the deliberate graceful-degradation guard.
- **Device names are round-tripped verbatim** from `smartctl --scan-open` into
  `Info`. macOS uses `IOService:/...` paths, Linux uses `/dev/...`. Never
  construct or normalize a device name.

### UI specifics

- **Capability-driven tabs** (detail.go `visibleTabs`): a tab only appears when
  its source data exists (the Logs tab hides for drives with no error/self-test
  log; the Tests tab hides unless `Report.SupportsSelfTest()`). When adding a
  view, gate it on data presence rather than always showing it.
- **The Tests tab is the only view that fires smartctl actions.** Most tabs are
  pure renderers (`tabView.refresh`). Two take input: the Attributes tab handles
  `s`/`f` (`attributes.go`) but these only toggle local view state (sort/filter)
  with no smartctl call; the Tests tab is the one that drives self-test
  start/cancel through `selfTestActions` callbacks the App wires in `build()`.
  The App owns the smartctl calls, the confirm/error modals (`pushModal`/
  `popModal`, guarded by `inModal` in `onKey`), and the post-action refresh.
  Self-tests are short/long only — `smart.RunSelfTest` rejects other types.
- **Runtime poll-interval control.** The `+`/`-` keys (`onKey`) walk the
  `intervalPresets` ladder via `nextInterval` (by value, so an off-ladder
  `--interval` snaps to a neighbour on the first press). `setInterval` updates
  `a.interval` and signals `intervalCh`, which `poll.go`'s loop drains to call
  `ticker.Reset` — the cadence changes live without restarting the poll loop.
  `--interval` only sets the starting value (default 30s).
- **Scroll arrows are a shared affordance** (`scroll.go`): every tab that can
  overflow shows the same cyan ▲/▼ off-screen cue via `drawScrollArrows`. The
  `scrollView` container (FARM, Overview's whole layout uses widget composition)
  draws them directly; widgets that scroll natively wrap in `scrollTextView`,
  `scrollTable` or `scrollList`, which embed the tview widget and override `Draw`
  to overlay the arrows from the widget's own scroll metrics. Make a new
  scrollable tab body one of these wrappers rather than a bare TextView/Table/
  List, and keep it focusable so the focused-content keys reach it.
- **Escape drive-controlled strings before rendering** (`esc()`/`tview.Escape`
  in `format.go`). SMART identity and log free-text — model/family/serial/
  firmware, form factor, SATA/link-speed strings, the smartctl fatal message,
  self-test/error-log strings, SATA PHY-counter and ATA attribute names, raw
  attribute values, FARM recording type — is device-controlled (writable via
  vendor tooling, or forged by a hostile USB enclosure). It is written into
  widgets that interpret tview markup (`SetDynamicColors(true)` TextViews **and
  all table cells**), so pass it through `esc()` at the sink or a malicious drive
  can inject colour tags and spoof the health display. Escape only the data, not
  the surrounding intentional tags; keyword/severity tests run on the original,
  the escaped copy is what gets wrapped (see `colorResult`). Purely numeric/
  enumerated/formatted values don't need it.
- **Health/severity** lives in `smart/health.go`. ATA pre-fail vs old-age comes
  from the authoritative `flags.prefailure` bit, not attribute-name heuristics.
- **Colour theming** (`theme.go`). All colour flows through a package-level
  `var activeTheme Theme` of semantic roles (`Accent`, `Muted`, `OK`/`Caution`/
  `Failing`, `Inverse`, `SelectionBg/Fg`, `BannerBg`, `BarHealthy`,
  `ScrollArrow`, `ListSecondary` — the drive-list secondary line device ·
  capacity · temp); `setTheme` swaps it (read/written only on the event-loop
  goroutine, so no mutex — same as `App.reports`). Never write a raw `[aqua]`/
  `[gray]`/`tcell.ColorRed` literal: use the tag helpers (`accentTag()`,
  `mutedTag()`, `okTag()`, `severityTag()`, `fgbgTag(fg,bg)`) for markup or read
  `activeTheme.X` for a `tcell.Color`. The `dark` theme reproduces the original
  palette byte-for-byte (pinned by `theme_test.go`) so the default is unchanged;
  `electric` (an "elite BBS" palette in blue/cyan/white/gray — bright azure-cyan
  borders, white body text, amber caution + red failing for legibility),
  `phosphor` (the classic monochrome green-CRT palette — *pure green only*, no
  amber/red; severity reads via green brightness + the `●` glyph + bold),
  `amber` (a Hercules monochrome amber-monitor palette — warm amber accent/text
  with a warm amber→orange→red severity ramp), and `mono` are the alternates.
  `--theme NAME` selects at startup, the `T` key cycles live
  (`cycleTheme`→`repaintAll`, which forces a detail rebuild so widgets that baked
  colour in at build time get re-coloured — the one-shot root-warning banner is
  the easy miss, hence `refreshBanner`). Known limits: `mono` drops all our colour
  (severity survives via the `●` glyph + bold), and tview's built-in List
  selection inverse is outside the theme and stays a tview default.
- Protocol branching is via `Report.IsNVMe()` / `IsATA()`; NVMe and ATA render
  different attribute tables and gauges.
- **Temperature sparkline**: ATA seeds it instantly from
  `ata_sct_temperature_history`; NVMe has no such log, so it accumulates a
  runtime ring buffer (`App.history`, capped at `maxHistory`) across polls — it
  only appears after ≥2 samples.
- **Padding/gutters (TUI UX).** Every text/table/list box gets a uniform
  horizontal gutter via `SetBorderPadding(0, 0, uiGutter, uiGutter)` (`uiGutter`
  in `format.go`); never bake a left margin into format strings. Vertical
  padding stays 0 for density. Nest a line under an in-box header with
  `nestIndent` (2 spaces), not a custom amount. Two things are intentionally
  exempt and must keep their spaces: table cell padding (`" "+val+" "`) and the
  tab-bar highlight pills. Graphical widgets (gauges, sparkline, bar charts) opt
  out of the gutter to stay full-width. Record new TUI spacing/UX conventions in
  this bullet so they don't drift.

## Tests

`internal/smart/smart_test.go` parses real captured fixtures in
`internal/smart/testdata/` (WD NVMe, Seagate HDD, Samsung SSD, sparse Apple
NVMe) plus two hand-crafted error-log variants (`smart-{sda,nvme}-errors.json`)
that populate the otherwise-empty error tables. Capture new ones with
`smartctl -j -x <dev> > internal/smart/testdata/<name>.json`. There are no UI
tests; verify the UI by running it.

## Roadmap

See TODO.md. Self-tests (`smartctl -t`, the Tests tab) and the sudo/permission
banner have landed, and the ATA and Seagate FARM paths have been validated on
real Linux SATA hardware.

**SCSI/SAS is intentionally out of scope.** We have no SCSI/SAS gear to develop
or test against, so smartview deliberately targets only SATA/ATA and NVMe. Don't
add SCSI/SAS support speculatively — it can't be verified here.
