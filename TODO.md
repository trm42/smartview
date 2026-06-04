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

## Next up

- [ ] **Validate the ATA path on real Linux SATA hardware.** The dev Mac has only the
      internal NVMe, so the ATA attribute table, pre-fail row colouring, and the
      SCT-history-seeded temperature sparkline are only exercised by fixtures.
- [ ] **Permission UX.** Detect `error`-severity `smartctl.messages` (e.g. access
      denied) and surface a clear "run with sudo" banner instead of an empty/cached view.
- [ ] **SCSI/SAS support.** Currently only ATA and NVMe are modelled; add `scsi_*`
      fields and a detail view for SCSI drives.
- [ ] **Capacity fallback.** Apple omits all capacity fields; for other NVMe drives
      that report `nvme_total_capacity`, fall back to it when `user_capacity` is absent.

## Later

- [ ] **Self-tests** (`smartctl -t short|long|conveyance`): trigger from the UI, show
      progress, and refresh the self-test log. Needs elevated privileges + a progress model.
- [ ] **Alerts / thresholds.** Optional notification or log when an attribute crosses
      into Caution/Failing; persist temperature history to disk for longer trends.
- [ ] **Config file** for refresh interval, default device, and colour theme.
- [ ] **Packaging.** Homebrew formula (macOS) and a release binary / distro package (Linux).

## Nice to have

- [ ] Sortable / filterable attribute table.
- [ ] Mouse-driven tab switching (tview mouse is already enabled).
- [ ] `--json` / headless mode for scripting and CI health checks.
