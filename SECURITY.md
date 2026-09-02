# Security Policy

## Reporting a vulnerability

Report privately through GitHub's [private vulnerability
reporting](https://github.com/trm42/smartview/security/advisories/new) — the
**Security** tab, *Report a vulnerability*. Please don't open a public issue for
a security bug.

Useful in a report: the smartview version (`smartview --version`), the OS, the
smartmontools version (`smartctl --version`), and — if a specific drive is
involved — the `smartctl -j -x` output that triggers it, with the serial number
removed. A fixture reproducing it under `--fixtures` (see the dev-mode notes in
the README) is the fastest possible report, since it needs no hardware to
confirm.

This is a small project maintained in spare time: expect a best-effort reply
rather than a guaranteed turnaround. Fixes land in the next release; only the
latest release is patched.

## Threat model

smartview is a local TUI. It reads drive data by executing `smartctl` and
parsing its JSON, it is routinely run under `sudo` (full attribute access needs
it), and **the data it renders is controlled by the drive** — model, serial,
firmware and log free-text are writable with vendor tooling and forgeable by a
hostile USB enclosure. That is the boundary worth attacking.

In scope:

- Anything letting drive-reported data escape being data: shell or argument
  injection into the `smartctl` invocation, path traversal through a device
  name, or a crash exploitable beyond a panic.
- A bypass of the markup escaping (`esc()` in `internal/ui/format.go`).
  Drive-controlled strings are rendered into widgets that interpret tview
  markup, so unescaped output can forge colour tags and spoof the health
  display — a wrong "healthy" is a real finding here, not a cosmetic one.
- Anything that makes running under `sudo` more dangerous than running
  `smartctl` under `sudo` already is.
- Parsing bugs in the config file (`internal/config`) with a security
  consequence.

Out of scope:

- Vulnerabilities in smartmontools itself — report those
  [upstream](https://www.smartmontools.org/); smartview only calls `smartctl`.
- Anything requiring an attacker who is already root on the machine, or who can
  replace the `smartctl` binary on `PATH`. smartview trusts the `smartctl` it
  runs, by design.
- SCSI/SAS handling. It is deliberately unsupported and untested (see the README
  roadmap), so bugs there are out of scope rather than pending.
- Release artifacts are checksummed (`checksums.txt`, SHA-256) but not signed.
  That is a known gap, not a vulnerability report.
