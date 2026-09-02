# smartview

A vibe-coded terminal dashboard for drive health, built on
[smartmontools](https://www.smartmontools.org/). Runs on macOS and Linux and
covers SATA and NVMe.

Pick a drive on the left, page through its tabs on the right. At a glance you
can tell whether a disk is healthy, how hot it's running, how worn an SSD is,
and whether anything has ever failed.

![smartview overview tab — drive list, identity panel, and temperature trend](docs/images/overview.png)

## Features

- Live drive list with a health glyph (`●` OK, `▲` caution, `■` failing),
  protocol, capacity and temperature.
- The ATA attribute table or NVMe health log, sorted by severity, with a
  plain-language explanation of whichever row you select.
- Seagate FARM metrics, including per-head reallocated-sector and resistance
  charts, on drives that expose them.
- Error logs, self-test history, and a Tests tab that starts a short or
  extended self-test and watches its progress.
- Severity comes from smartmontools' own `prefailure` flag and thresholds, not
  from guessing at attribute names.
- SMART JSON is sparse and varies by drive (Apple internal SSDs report almost
  nothing). Missing values show as `—`, and tabs with no data behind them are
  hidden rather than shown empty.

### Tabs

Only the tabs backed by real data appear, so a tab's number can differ between
drives. Set `show_unavailable_tabs` if you'd rather they stayed put: all six
are drawn, with the empty ones muted and skipped by `←` / `→`.

**Overview** — model, serial, firmware, capacity, interface, power-on hours,
and a temperature sparkline. ATA drives seed it from the on-disk temperature
history; NVMe drives fill it in over successive polls.

**Attributes** — sorted by severity, so anything worrying floats to the top.
`s` cycles the sort, `f` the filter.

![smartview attributes tab — severity-sorted SMART attribute table](docs/images/attributes.png)

**Statistics** — the Device Statistics log, when the drive keeps one: power-on
resets, sectors written and read, head flight hours, load events, uncorrectable
errors, temperature history, transport counters. Raw counters, not the
normalised values from the attribute table.

![smartview Statistics tab — Device Statistics pages](docs/images/statistics.png)

**FARM** — Seagate Field Accessible Reliability Metrics: unrecoverable
read/write counts, reallocated and candidate sectors, command timeouts,
temperature range, the 12 V rail, workload counters, per-head charts.

![smartview FARM tab — Seagate reliability metrics and per-head charts](docs/images/farm.png)

**Logs** — the SMART error log and the self-test history with results and run
times.

![smartview Logs tab — SMART error log and self-test history](docs/images/logs.png)

**Tests** — only on drives that support self-tests. Pick Short or Long, press
`Enter` to start (usually needs root), `x` to cancel. Results turn up in Logs.

![smartview Tests tab — start a short or extended self-test](docs/images/tests.png)

### Fleet comparison

`c` gives you every drive at once. One metric is in focus and the rows sort by
it, so the drive you care about is at the top: Temperature, Health & errors,
Endurance & wear, Age & usage. `←` / `→` or `1`–`4` switch sections, `s` flips
between metric and device order, `Enter` opens the highlighted drive.

ATA and NVMe expose different counters, so a `—` means this drive doesn't
report that reading, never that it's zero. A write total like `~1.5 TB` came
from vendor attribute 241, whose unit is vendor-defined. A drive marked `◌` is
spun down, so its numbers are from the last read.

![smartview fleet comparison — every drive ranked by health](docs/images/fleet.png)

## Requirements

smartmontools 7.0 or newer, for `smartctl` with JSON output:

```sh
brew install smartmontools      # macOS
apt install smartmontools       # Debian/Ubuntu
dnf install smartmontools       # Fedora
pacman -S smartmontools         # Arch
```

Full attribute access usually needs elevated privileges, so expect to run
smartview under `sudo`. Building from source needs Go 1.26+.

## Install

Binaries for macOS (Apple Silicon) and Linux (x86-64) ship with every
[release](https://github.com/trm42/smartview/releases). The Homebrew cask and
the `.deb`/`.rpm` packages pull smartmontools in too.

```sh
brew install trm42/tap/smartview                 # macOS

sudo apt install ./smartview_*_linux_amd64.deb   # Debian/Ubuntu
sudo dnf install ./smartview_*_linux_amd64.rpm   # Fedora/RHEL
```

There are also plain `tar.gz` builds; verify one with
`sha256sum -c checksums.txt --ignore-missing` and put the binary on your
`PATH`. From source:

```sh
go install github.com/trm42/smartview@latest
```

## Usage

```sh
smartview                    # auto-refresh every 30s
smartview --interval 10s     # different starting interval
smartview --theme phosphor   # start in a named theme
smartview --config ~/sv.toml # a specific settings file
smartview --version
sudo smartview               # when attributes need root
```

`--interval` only sets the starting cadence; `+` and `-` change it while
running.

### Configuration

Settings live in a TOML file, and `S` opens an editor for it in the app.

| Platform | Path |
| -------- | ---- |
| Linux    | `~/.config/smartview/config.toml` (honours `$XDG_CONFIG_HOME`) |
| macOS    | `~/Library/Application Support/smartview/config.toml` |

`--config PATH` overrides the location. Having no file is normal, but a file
you name explicitly has to exist. Flags beat the file, the file beats the
defaults, and a bad key stops startup with a message naming it.

```toml
theme = "dark"

# Go duration: "2s", "10s", "30s", "1m", "5m".
refresh_interval = "30s"

# Skip spun-down drives instead of waking them. The last reading is kept and
# marked stale. ATA only — NVMe has no standby state to check.
standby_aware = false

# Draw all six tabs even when a drive has no data for some of them, so tab
# numbers stay put between drives.
show_unavailable_tabs = false

# "drives" or "fleet".
start_view = "drives"
```

In the modal, `↑` / `↓` move between settings and on to Save/Cancel, `⏎` or
`→` changes the focused one, and a `•` marks what you've edited but not saved.
`S` is the only key that writes the file: `T` and `+` / `-` change the session
only, so a stray keypress can't rewrite something you hand-edited.

With `standby_aware`, smartview stops spinning idle disks up just to read them.
A parked drive keeps its last reading, marked `◌` with its age, and `r`
respects that. `R` is the one key that wakes a drive.

### Themes

Twenty-one of them. `--theme` picks the starting one, `T` cycles live.

| Theme | Palette |
| ----- | ------- |
| `dark` | The default: aqua chrome, green → yellow → red severity |
| `electric` | Elite-BBS azure and white, amber caution |
| `phosphor` | Green CRT, pure green — severity reads as brightness |
| `amber` | Hercules amber monitor, amber → orange → red |
| `cga` | The authentic IBM CGA 16, nothing interpolated |
| `neon` | Cyberpunk: electric blue, magenta banner and bars |
| `nord` | Arctic blue-grey, frost and aurora |
| `gruvbox` | Warm retro earth, blue chrome rather than the signature gold |
| `beacon` | Colour-vision-safe: blue → yellow → rose, neutral chrome |
| `cobalt` | Royal-blue ground, ice-cyan chrome |
| `ultraviolet` | Blacklight violet ground, orchid chrome |
| `deepsea` | Petrol-teal ground, sand → coral severity |
| `oxblood` | Wine ground, gold chrome, severity on rose |
| `daylight` | Light, cool paper, azure chrome |
| `parchment` | Light, warm paper, deep teal chrome |
| `sorbet` | Light, blush paper, magenta chrome |
| `marigold` | Light, gold paper, deep teal chrome |
| `seafoam` | Light, mint paper, emerald chrome |
| `sky` | Light, azure paper, indigo chrome |
| `terminal` | Your terminal's own colours, with severity added back |
| `mono` | No colour at all; the glyph and reverse video carry it |

Severity is never colour alone: `●` healthy, `▲` caution, `■` failing, and a
failing verdict is a filled chip rather than tinted text. That's what keeps
`phosphor`, `amber` and `mono` readable.

Over SSH, export `COLORTERM=truecolor` on the far end. `ssh` forwards `TERM`
but not `COLORTERM`, and without it every painted palette gets snapped to the
nearest xterm-256 colour.

### Keys

| Key | Action |
| --- | ------ |
| `↑` / `↓` | Select a drive, or scroll content when the detail pane has focus |
| `j` / `k` | Scroll the focused content line by line |
| `PgUp` / `PgDn`, `Ctrl-B` / `Ctrl-F` | Page the content |
| `g` / `G`, `Home` / `End` | Jump to top / bottom |
| `←` / `→` | Move between panes and step through tabs |
| `Tab` | Toggle focus between the list and the detail pane |
| `1`–`9` | Switch tab by number (a click on a tab does the same) |
| `t` | Jump to the Tests tab |
| `c` | Toggle the fleet comparison |
| `r` / `R` | Refresh now / wake spun-down drives and refresh |
| `+` / `-` | Slower / faster refresh (2s → 5s → 10s → 30s → 1m → 5m) |
| `T` | Cycle the theme |
| `S` | Open Settings |
| `?` | Show every binding |
| `s` / `f` (Attributes) | Cycle the sort / filter |
| `Enter` / `x` (Tests) | Start the selected test / cancel the running one |
| `1`–`4`, `←` / `→` (Fleet) | Switch section |
| `s` / `Enter` (Fleet) | Sort by metric or name / open the drive |
| `Esc` (Fleet) | Back to the per-drive view |
| `q` or `Esc` | Quit |

Below 100 columns the drive list collapses to a one-row rail and the detail
takes the full width, so `↑` / `↓` always step the drive there. `?` is the
authoritative list; a test parses the key handlers and fails if a binding isn't
documented.

The mouse works too: click a tab or a drive, scroll any pane with the wheel.
Chrome with no keys behind it (rail, banner, hint bar) declines clicks rather
than stealing focus.

### Fixture mode

To eyeball the UI without real drives, build with the `dev` tag and point
`--fixtures` at a directory of captured `smartctl -j -x` JSON:

```sh
go build -tags dev -o smartview .
./smartview --fixtures internal/smart/testdata
```

The committed fixtures cover ATA, NVMe, a sparse Apple NVMe and a Seagate FARM
log. Fixture mode skips the `smartctl` preflight, so smartmontools needn't be
installed. A release build accepts `--fixtures` but refuses to run with it.

## How it works

smartview shells out to `smartctl -j` instead of reading SMART data itself,
which leaves every device, transport and drive-database quirk to smartmontools.
One subtlety worth knowing: `smartctl`'s exit status is a bitmask and is
routinely non-zero on a perfectly healthy drive, so smartview parses the JSON
regardless of exit code and reports real failures from `smartctl.messages`.

```
internal/smart/   data layer — Scan/Info wrappers, typed JSON, health assessment
internal/config/  the TOML settings file
internal/ui/      tview UI — drive pane, capability-driven tabs, poll loop
main.go           flags + smartctl preflight
```

The data layer has no tview dependency and doesn't know the UI exists.

## Contributing drive data

smartview is developed against the drives its author owns, so the fastest way
to improve support for yours is to send the JSON smartview reads. A dump from a
drive that renders wrong — a missing tab, a blank value, a wrong verdict, an
unfamiliar vendor — beats a bug report describing it, because it can be
replayed here without the hardware.

Find the device name with `sudo smartctl --scan-open` (don't invent one), then
capture the same report smartview reads on every poll:

```sh
sudo smartctl -j -x /dev/sda > smart-mydrive.json
sudo smartctl -l farm -j /dev/sda > smart-mydrive-farm.json   # Seagate FARM
```

**Redact before sharing.** A report carries the drive's `serial_number` and
`wwn`, and the tracker is public; replace them the way the committed fixtures
do (`EXAMPLE1`, `wwn.id` → `305419896`). If you send a FARM log too, redact its
serial in `seagate_farm_log.page_1_drive_information.serial_number` to the
*same* placeholder, or the two files won't pair up.

Then open an issue with the JSON attached, or a PR dropping it into
`internal/smart/testdata/` as `smart-<something>.json`, where it becomes a
fixture everyone can develop against. Reports are keyed by `device.name`, so
give yours a name no existing fixture uses, and check it renders:

```sh
go build -tags dev -o smartview .
./smartview --fixtures internal/smart/testdata
```

Most wanted is anything the current set doesn't cover: non-Seagate FARM-capable
drives, SSDs with unusual vendor attributes, sparse NVMe schemas, and anything
that shows `—` where your drive clearly has data. SMART data is drive-controlled
and treated as untrusted input, so if a dump breaks rendering in a way that
looks exploitable, send it through the Security tab instead (see
[SECURITY.md](SECURITY.md)).

## Development

```sh
go build -o smartview .
go test -race -cover ./...
go vet ./... && gofmt -l .
GOTOOLCHAIN=go1.26.4 golangci-lint run ./...
```

Pin `GOTOOLCHAIN` for the linter: under Go 1.27.0 a bare `golangci-lint run`
fails inside `crypto/internal/randutil` with `undefined: rand`. CI gates on
more than the above (SPDX headers, `go mod tidy`, the `-tags dev` build, a
darwin/arm64 cross-compile, `govulncheck`); see `.github/workflows/`.

The `internal/smart` tests parse real captured `smartctl` output, including a
sparse Apple NVMe fixture that guards graceful degradation. `internal/ui` is
tested too: `layout_test.go` drives the whole app headlessly on a tcell
simulation screen, and `keys_test.go` fails if a bound key is missing from `?`.

## Security

Report security bugs privately through the repository's Security tab rather
than in a public issue. [SECURITY.md](SECURITY.md) covers what's in scope —
drive-controlled strings are the interesting boundary — and what to include.

## Scope

See [TODO.md](TODO.md) for what's next.

SCSI/SAS drives aren't supported, deliberately. There's no SCSI/SAS hardware
here to develop or test against, so it's out of scope rather than pending.

Everything visible here was built against my own server's disks, so other
drives may need visualisation work — see [Contributing drive
data](#contributing-drive-data) if yours renders badly.

## Built with

- [rivo/tview](https://github.com/rivo/tview) and
  [gdamore/tcell](https://github.com/gdamore/tcell) for the terminal UI
- [navidys/tvxwidgets](https://github.com/navidys/tvxwidgets) for the NVMe
  percentage gauges; the charts and sparkline are drawn by
  `internal/ui/chart.go`, which scales to the data rather than to zero
- [smartmontools](https://www.smartmontools.org/) for the data itself

## License

GNU General Public License v3.0 or later. See [LICENSE](LICENSE).

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
