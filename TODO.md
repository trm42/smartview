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
      (reallocated sectors + MR head resistance). Parser fixture-tested (`TestParseFARM`);
      **not yet validated against a real Seagate SATA drive under sudo on Linux.**
- [x] **Richer identity & diagnostics**: interface/link speed, sector sizes, form factor,
      SATA/ATA version, TRIM (Overview); NVMe workload (read/write commands, controller
      busy, warn/crit temp time); self-test durations and SATA PHY event counters (Logs).
- [x] **Capacity fallback** to `nvme_total_capacity` when `user_capacity` is absent.
- [x] **Unit-test sweep** of pure logic (health/severity, formatting, parsing, capability
      helpers) across `internal/smart` and `internal/ui`.
- [x] **Fleet view** (`c`): a full-screen comparison of every drive at once, with a
      switchable focus metric (Temperature / Health & errors / Endurance & wear /
      Age & usage) that the rows sort by. Backed by cross-protocol accessors in
      `internal/smart/metrics.go` (power-on/cycles, life used, spare, temperature range,
      data written with its provenance, error counters), each fixture-tested. Absent
      readings render as `—` rather than a misleading zero, and an attribute-241 write
      total is marked `~` as a vendor-defined estimate.

## Next up

- [ ] **Validate the ATA path on real Linux SATA hardware.** The dev Mac has only the
      internal NVMe, so the ATA attribute table, pre-fail row colouring, and the
      SCT-history-seeded temperature sparkline are only exercised by fixtures.
- [ ] **Validate the Seagate FARM path on real hardware** (Seagate SATA drive, Linux,
      `sudo`). The renderer and parser are fixture-only so far; confirm the live
      `-l farm -j` fetch, the Seagate-ATA gate, and the per-head charts on actual data.

## Later

- [x] **Self-tests** (`smartctl -t short|long`): a capability-gated **Tests** tab triggers
      short/long tests, shows live progress with a cancel (`smartctl -X`) affordance, and
      flips back to the selector when idle. Conveyance/selective are deliberately excluded.
      ATA + NVMe data paths are fixture/unit-tested; **live trigger needs validation on
      real self-test-capable hardware under `sudo`** (the dev Mac's Apple NVMe reports no
      self-test support, so the tab is hidden there).
- [ ] **Alerts / thresholds.** Optional notification or log when an attribute crosses
      into Caution/Failing; persist temperature history to disk for longer trends.
- [ ] **Config file** for refresh interval, default device, and colour theme.
- [x] **Packaging.** Tag-driven GoReleaser pipeline (`.goreleaser.yaml` +
      `.github/workflows/release.yml`): GitHub Release archives + checksums, a
      Homebrew formula (`trm42/homebrew-tap`), and Linux `.deb`/`.rpm`
      (Depends: smartmontools). Build matrix is **darwin/arm64 + linux/amd64
      only** — darwin/amd64 and linux/arm64 are intentionally excluded.

## Out of scope

- **SCSI/SAS support.** We have no SCSI/SAS hardware to develop or test against,
  so smartview deliberately targets only SATA/ATA and NVMe. Not planned.

## Nice to have

- [ ] Sortable / filterable attribute table.
- [ ] Mouse-driven tab switching (tview mouse is already enabled).
- [ ] `--json` / headless mode for scripting and CI health checks.
