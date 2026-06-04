# smartview

A cross-platform terminal UI for monitoring drive health via
[smartmontools](https://www.smartmontools.org/). Runs on **macOS** and **Linux**,
showing SMART status, attributes, and wear indicators for SATA and NVMe drives in
a live, auto-refreshing dashboard.

## Features

- **Live dashboard** — lists every drive with a colour-coded health glyph,
  protocol, and temperature; auto-refreshes on an interval.
- **Per-drive drill-down** — tabbed detail view with drive identity, NVMe wear
  gauges (life used / spare), and a temperature trend sparkline.
- **Attribute table** — the full ATA SMART attribute table or the NVMe health log,
  with rows coloured by severity (green / yellow / red).
- **Failing/pre-fail highlighting** — uses smartmontools' authoritative
  `prefailure` flag plus `when_failed` / threshold checks, not name heuristics.
- **Graceful degradation** — the SMART JSON is sparse and drive-dependent (e.g.
  Apple internal SSDs report very little). Missing values render as `—` and whole
  tabs (like *Logs*) hide when a drive doesn't report that section.

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
smartview                 # auto-refresh every 5s (default)
smartview --interval 10s  # custom refresh interval
sudo smartview            # if attributes require root
```

### Keys

| Key        | Action                          |
| ---------- | ------------------------------- |
| `↑` / `↓`  | Select a drive                  |
| `Tab`      | Move focus between panes        |
| `1`–`3`    | Switch detail tab               |
| `r`        | Refresh now                     |
| `q`        | Quit                            |

Mouse is also supported (click drives and tabs).

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

## Development

```sh
go test ./...     # parser/health tests against fixtures in internal/smart/testdata
go vet ./...
```

Tests run against real captured `smartctl` output, including a sparse Apple NVMe
fixture that guards the graceful-degradation behaviour.

## Roadmap

See [TODO.md](TODO.md). Highlights: validation on Linux SATA hardware, a
permission/sudo banner, SCSI/SAS support, and self-test triggering (`smartctl -t`).

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
