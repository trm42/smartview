# TODO

Tracking work for **smartview**, a cross-platform (macOS + Linux) terminal UI
for drive health via smartmontools.

## Done

- [x] Data layer (`internal/smart`): `Scan`/`Info` over `smartctl -j`, optional/
      pointer structs for the sparse drive-dependent schema, parse-despite-exit-code.
- [x] Health assessment using authoritative `flags.prefailure` + `when_failed`/`thresh`.
- [x] tview UI: device-selector pane, capability-driven tabs (Overview / Attributes /
      Logs), NVMe wear gauges, temperature sparkline, auto-refresh poll loop.
- [x] Fixture-based parser tests incl. the sparse Apple NVMe (graceful degradation).
- [x] smartctl preflight with platform-specific install hint.
- [x] Root/permission banner (`euid != 0`) + per-device `error`-severity message surfacing.
- [x] **Seagate FARM** tab: separate `smartctl -l farm -j` fetch (Seagate-ATA gated),
      curated drive/error/environment/workload stats, and per-head bar charts
      (reallocated sectors + MR head resistance). Parser fixture-tested (`TestParseFARM`)
      and validated live against a real Seagate SATA drive under `sudo` on Linux.
- [x] **Richer identity & diagnostics**: interface/link speed, sector sizes, form factor,
      SATA/ATA version, TRIM (Overview); NVMe workload (read/write commands, controller
      busy, warn/crit temp time); self-test durations and SATA PHY event counters (Logs).
- [x] **Capacity fallback** to `nvme_total_capacity` when `user_capacity` is absent.
- [x] **Unit-test sweep** of pure logic (health/severity, formatting, parsing, capability
      helpers) across `internal/smart` and `internal/ui`.
- [x] **Headless UI tests**: `layout_test.go` runs the whole `App` on a tcell simulation
      screen at both sides of the 100-column breakpoint (layout choice, focus, no
      deadlock in the draw hook), `keys_test.go` parses the package with `go/ast` and
      fails when a bound key is missing from the `?` modal, and `fleet_test.go` asserts
      on the first frame the fleet view draws.
- [x] **Fleet view** (`c`): a full-screen comparison of every drive at once, with a
      switchable focus metric (Temperature / Health & errors / Endurance & wear /
      Age & usage) that the rows sort by. Backed by cross-protocol accessors in
      `internal/smart/metrics.go` (power-on/cycles, life used, spare, temperature range,
      data written with its provenance, error counters), each fixture-tested. Absent
      readings render as `—` rather than a misleading zero, and an attribute-241 write
      total is marked `~` as a vendor-defined estimate.
- [x] **Sortable / filterable attribute table**: `s` cycles the sort order and `f` the
      filter, both local view state with no smartctl call.
- [x] **Colour themes**: `dark`, `electric`, `phosphor`, `amber`, `mono`; `--theme` picks
      the starting palette and `T` cycles them live.
- [x] **Validated on real Linux SATA hardware**: the ATA path (attribute table, pre-fail
      row colouring, SCT-history-seeded temperature sparkline) and the Seagate FARM path
      (live `-l farm -j` fetch, the Seagate-ATA gate, the per-head charts).

## Next up

- [ ] **Validate standby-aware polling on real spun-down SATA hardware.** The
      argv is pinned by stub-script tests and the rendering by a fixture, but
      the live path — smartctl actually declining to wake a parked disk and
      exiting 129 — has never run against a real drive. The dev Mac cannot
      exercise it: its only drive is an Apple NVMe, where `-n` is ignored by
      design. Needs the Linux SATA box, as FARM and self-tests did.
- [x] **Validate the live self-test trigger**: started and tracked to completion on real
      self-test-capable hardware under `sudo` on the Linux SATA box.

## Later

- [x] **Self-tests** (`smartctl -t short|long`): a capability-gated **Tests** tab triggers
      short/long tests, shows live progress with a cancel (`smartctl -X`) affordance, and
      flips back to the selector when idle. Conveyance/selective are deliberately excluded.
      ATA + NVMe data paths are fixture/unit-tested; **live trigger needs validation on
      real self-test-capable hardware under `sudo`** (the dev Mac's Apple NVMe reports no
      self-test support, so the tab is hidden there).
- [ ] **Alerts / thresholds.** Optional notification or log when an attribute crosses
      into Caution/Failing; persist temperature history to disk for longer trends.
- [x] **Config file / settings.** TOML at `os.UserConfigDir()/smartview/config.toml`
      (`--config PATH` overrides), five settings — `theme`, `refresh_interval`,
      `standby_aware`, `show_unavailable_tabs`, `start_view`. Precedence is
      flag > file > default via `flag.Visit`; a bad file or unknown key refuses
      startup. An in-app **Settings** modal (`S`) is the only writer, so `T` and
      `+`/`-` stay session-only. "Default device" was deliberately **not**
      implemented — drive selection stays per-session — and is not planned.
- [x] **Packaging.** Tag-driven GoReleaser pipeline (`.goreleaser.yaml` +
      `.github/workflows/release.yml`): GitHub Release archives + checksums, a
      Homebrew cask (`trm42/homebrew-tap`), and Linux `.deb`/`.rpm`
      (Depends: smartmontools). Build matrix is **darwin/arm64 + linux/amd64
      only** — darwin/amd64 and linux/arm64 are intentionally excluded.

## Out of scope

- **SCSI/SAS support.** We have no SCSI/SAS hardware to develop or test against,
  so smartview deliberately targets only SATA/ATA and NVMe. Not planned.

## Nice to have

- [x] Wire `smart.Preflight` into startup. It is now the single startup gate:
      smartctl on PATH *and* at least smartmontools 7.0, under a 5s deadline and
      the signal context, so Ctrl-C works while it runs. A missing binary comes
      back as `smart.ErrNoSmartctl` and one below the floor as
      `smart.ErrOldSmartctl` — including a build too old to answer `-j -V` at
      all, which is what every real pre-7.0 smartctl does — and `main` matches
      both to add the platform's install/upgrade hint. Fixture mode bypasses it,
      as before.
- [ ] `--json` / headless mode for scripting and CI health checks.
