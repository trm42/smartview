# smartview

A vibe-coded cross-platform terminal UI for monitoring drive health via
[smartmontools](https://www.smartmontools.org/). Runs on **macOS** and **Linux**,
showing SMART status, attributes, and wear indicators for SATA and NVMe drives in
a live, auto-refreshing dashboard.

Pick a drive on the left, then page through its tabs on the right — at a glance
you can tell whether a disk is healthy, how hot it's running, how worn an SSD is,
and whether anything has ever failed.

![smartview overview tab — drive list, identity panel, and temperature trend](docs/images/overview.png)

## Features

- **Live dashboard** — lists every drive with a colour-coded health glyph
  (🟢 OK / 🟡 warning / 🔴 failing), protocol, capacity, and temperature;
  auto-refreshes on an interval and can be refreshed on demand.
- **Per-drive drill-down** — a tabbed detail pane whose tabs appear only when the
  drive actually reports that data (see below). Includes drive identity, NVMe wear
  gauges (life used / spare), and a temperature-trend sparkline.
- **SMART attribute table** — the full ATA attribute table or the NVMe health log,
  sorted by severity and coloured green / yellow / red, with a plain-language
  explanation of the selected attribute at the bottom.
- **Seagate FARM view** — for drives that expose Field Accessible Reliability
  Metrics, a richer reliability panel: error statistics, environment (temperature
  range, 12 V rail), workload counters, and per-head reallocated-sector and
  head-resistance charts.
- **Self-tests & error logs** — view the SMART error log and self-test history
  (short / extended / conveyance) with pass/fail status and estimated durations,
  and — on drives that support it — start short or extended self-tests right from
  the interactive **Tests** tab, with a live progress bar and cancel.
- **Failing/pre-fail highlighting** — uses smartmontools' authoritative
  `prefailure` flag plus `when_failed` / threshold checks, not name heuristics.
- **Graceful degradation** — the SMART JSON is sparse and drive-dependent (e.g.
  Apple internal SSDs report very little). Missing values render as `—` and whole
  tabs hide when a drive doesn't report that section.

### The tabs

Tabs are capability-driven — only the ones backed by real data for the selected
drive are shown.

**Overview** — drive identity (model, serial, firmware, capacity, interface,
power-on hours) and a live temperature-trend sparkline. ATA drives seed the
sparkline instantly from the on-disk temperature history; NVMe drives build it up
across polls.

**Attributes** — the per-drive SMART attributes, sorted by severity so anything
worrying floats to the top. Select a row to read what it means. Press `s` to
cycle the sort order and `f` to cycle the filter (e.g. only flagged attributes).

![smartview attributes tab — severity-sorted SMART attribute table](docs/images/attributes.png)

**FARM** — Seagate Field Accessible Reliability Metrics: unrecoverable read/write
counts, reallocated and candidate sectors, command timeouts, the drive's
temperature range and 12 V rail, workload (read/write command counts), and
per-head reallocated-sector and head-resistance charts.

![smartview FARM tab — Seagate reliability metrics and per-head charts](docs/images/farm.png)

**Logs** — the SMART error log and the self-test history (short / extended /
conveyance offline tests) with their results and estimated run times.

![smartview Logs tab — SMART error log and self-test history](docs/images/logs.png)

**Tests** — shown only on drives that support self-tests. Select a **Short** or
**Long (extended)** test and press `Enter` to start it (usually requires root);
a live progress bar with a centered percent tracks the running test, and `x`
cancels it. Results land in the **Logs** tab when the test completes.

![smartview Tests tab — start a short or extended self-test](docs/images/tests.png)

## Requirements

- **Go 1.26+** (to build)
- **smartmontools 7.0+** providing `smartctl` with JSON output (`-j`)
  - macOS: `brew install smartmontools`
  - Debian/Ubuntu: `apt install smartmontools`
  - Fedora: `dnf install smartmontools`
  - Arch: `pacman -S smartmontools`

Full attribute access generally requires elevated privileges, so you may need to
run smartview with `sudo`.

## Install

```sh
git clone <repo-url> smartview
cd smartview
go build -o smartview .
```

Or run directly:

```sh
go run .
```

## Usage

```sh
smartview                 # auto-refresh every 30s (default)
smartview --interval 10s  # custom starting refresh interval
sudo smartview            # if attributes require root
```

The refresh interval can also be changed at runtime with the `+` / `-` keys (see
below) — `--interval` only sets the starting cadence.

### Keys

| Key                       | Action                                                        |
| ------------------------- | ------------------------------------------------------------- |
| `↑` / `↓` (or `j` / `k`)  | Select a drive (list focus) or scroll content (detail focus)  |
| `PgUp` / `PgDn`, `g` / `G`| Page / jump to top / bottom of scrollable content             |
| `←` / `→`                 | Move between panes and step through detail tabs (no wrap)     |
| `Tab`                     | Toggle focus between the drive list and the detail pane       |
| `1`–`9`                   | Switch detail tab by number                                   |
| `t`                       | Jump straight to the **Tests** tab                            |
| `r`                       | Refresh now                                                   |
| `+` / `-`                 | Slower / faster refresh (2s → 5s → 10s → 30s → 1m → 5m ladder)|
| `s` / `f` (Attributes)    | Cycle the attribute sort / filter                             |
| `Enter` / `x` (Tests)     | Start the selected self-test / cancel the running test        |
| `q` or `Esc`              | Quit                                                          |

Mouse is also supported (click drives and tabs, scroll with the wheel).

### Dev / fixture mode

To eyeball the UI without real drives — or to render hardware you don't have on
hand — build with the `dev` tag and point `--fixtures` at a directory of captured
`smartctl -j -x` JSON:

```sh
go build -tags dev -o smartview .
./smartview --fixtures internal/smart/testdata   # render the committed fixtures
```

The committed `internal/smart/testdata/` fixtures cover ATA, NVMe, a sparse Apple
NVMe, and a Seagate FARM log. Fixture mode bypasses the `smartctl` preflight, so
smartmontools need not be installed. A plain release build (`go build`) still
accepts `--fixtures` but rejects it at startup with a rebuild hint.

## How it works

smartview shells out to `smartctl -j` rather than reading SMART data directly. This
keeps it cross-platform and lets smartmontools handle all device, transport, and
drive-database quirks (SATA / NVMe / SCSI bridges). One subtlety: `smartctl`'s exit
status is a bitmask that can be non-zero on a perfectly healthy drive, so smartview
parses the JSON regardless of exit code and surfaces real problems via
`smartctl.messages`.

```
internal/smart/   data layer — Scan/Info wrappers, typed JSON, health assessment
internal/ui/      tview UI — device pane, capability-driven tabs, poll loop
main.go           flags + smartctl preflight
```

The data layer is fully decoupled from the UI and has no tview dependency.

## Notes

* The content has been made with my server's disk's SMART data so other outputs may 
require additional visualisation work or tweaking. Please make PR's if it can improve
the app for your disks. 

## Development

```sh
go test ./...     # parser/health tests against fixtures in internal/smart/testdata
go vet ./...
```

Tests run against real captured `smartctl` output, including a sparse Apple NVMe
fixture that guards the graceful-degradation behaviour.

## Roadmap

See [TODO.md](TODO.md). Self-test triggering (`smartctl -t`) and a
permission/sudo banner have landed, and the ATA and Seagate FARM paths have been
validated on real Linux SATA hardware.

**SCSI/SAS drives are not supported, on purpose.** smartview targets SATA/ATA and
NVMe only — we don't have SCSI/SAS hardware to develop or test against, so that
support is deliberately out of scope rather than merely pending.

## Built with

- [rivo/tview](https://github.com/rivo/tview) + [gdamore/tcell](https://github.com/gdamore/tcell) — terminal UI
- [navidys/tvxwidgets](https://github.com/navidys/tvxwidgets) — gauges and sparklines
- [smartmontools](https://www.smartmontools.org/) — the SMART data source

## License

smartview is free software licensed under the **GNU General Public License
v3.0** or later. See [LICENSE](LICENSE) for the full text.

    smartview — terminal UI for monitoring drive health via smartmontools
    Copyright (C) 2026  smartview contributors

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU General Public License for more details.
