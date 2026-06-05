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
go run .                       # run (add --interval 10s to change refresh cadence)
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
a pty (`sleep`, `send` keys, `send "q"` to quit), exactly as above.

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
  log). When adding a view, gate it on data presence rather than always showing it.
- **Health/severity** lives in `smart/health.go`. ATA pre-fail vs old-age comes
  from the authoritative `flags.prefailure` bit, not attribute-name heuristics.
- Protocol branching is via `Report.IsNVMe()` / `IsATA()`; NVMe and ATA render
  different attribute tables and gauges.
- **Temperature sparkline**: ATA seeds it instantly from
  `ata_sct_temperature_history`; NVMe has no such log, so it accumulates a
  runtime ring buffer (`App.history`, capped at `maxHistory`) across polls — it
  only appears after ≥2 samples.

## Tests

`internal/smart/smart_test.go` parses real captured fixtures in
`internal/smart/testdata/` (WD NVMe, Seagate HDD, Samsung SSD, sparse Apple
NVMe). Capture new ones with `smartctl -j -x <dev> > internal/smart/testdata/<name>.json`.
There are no UI tests; verify the UI by running it.

## Roadmap

See TODO.md. Notably unimplemented: self-tests (`smartctl -t`) and a
sudo/permission banner. The ATA path is only fixture-tested — it needs
validation on real Linux SATA hardware.

**SCSI/SAS is intentionally out of scope.** We have no SCSI/SAS gear to develop
or test against, so smartview deliberately targets only SATA/ATA and NVMe. Don't
add SCSI/SAS support speculatively — it can't be verified here.
