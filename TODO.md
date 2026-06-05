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

## Next up

- [ ] **Validate the ATA path on real Linux SATA hardware.** The dev Mac has only the
      internal NVMe, so the ATA attribute table, pre-fail row colouring, and the
      SCT-history-seeded temperature sparkline are only exercised by fixtures.
- [ ] **Validate the Seagate FARM path on real hardware** (Seagate SATA drive, Linux,
      `sudo`). The renderer and parser are fixture-only so far; confirm the live
      `-l farm -j` fetch, the Seagate-ATA gate, and the per-head charts on actual data.
- [ ] **Capacity fallback.** Apple omits all capacity fields; for other NVMe drives
      that report `nvme_total_capacity`, fall back to it when `user_capacity` is absent.

## Later

- [ ] **Self-tests** (`smartctl -t short|long|conveyance`): trigger from the UI, show
      progress, and refresh the self-test log. Needs elevated privileges + a progress model.
- [ ] **Alerts / thresholds.** Optional notification or log when an attribute crosses
      into Caution/Failing; persist temperature history to disk for longer trends.
- [ ] **Config file** for refresh interval, default device, and colour theme.
- [ ] **Packaging.** Homebrew formula (macOS) and a release binary / distro package (Linux).

## Out of scope

- **SCSI/SAS support.** We have no SCSI/SAS hardware to develop or test against,
  so smartview deliberately targets only SATA/ATA and NVMe. Not planned.

## Nice to have

- [ ] Sortable / filterable attribute table.
- [ ] Mouse-driven tab switching (tview mouse is already enabled).
- [ ] `--json` / headless mode for scripting and CI health checks.
