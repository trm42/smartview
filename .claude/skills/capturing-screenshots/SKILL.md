---
name: capturing-screenshots
description: Use when smartview's docs/images PNGs are stale, a UI change needs re-shooting, or a view has no image yet
---

# Capturing smartview screenshots

Renders `docs/images/*.png` from committed fixtures with [vhs](https://github.com/charmbracelet/vhs).
No real drives, no `smartctl`, no `sudo`. Needs `vhs`, `ttyd` and ImageMagick.

## Run it

```sh
.claude/skills/capturing-screenshots/capture.sh            # all seven
.claude/skills/capturing-screenshots/capture.sh farm logs  # just these
```

Each view takes ~25 s. The script builds `-tags dev` first — a release build
accepts `--fixtures` but rejects it at startup.

Then **read every PNG back**. A blank or half-drawn frame writes a perfectly
valid 1280x720 file, so the dimension check the script prints proves nothing on
its own. You are checking three things: the right tab is highlighted, the right
drive is selected in the list, and nothing is clipped at the right edge of the
tab bar or status bar.

## What each tape captures

Tab numbers are **capability-driven** (`visibleTabs` in `internal/ui/detail.go`) —
they are not stable across drives. Every tape therefore picks its drive first,
by counting `Down` from the top of the list, then presses the number that is
correct *for that drive*.

The list is sorted by fixture filename. The tapes therefore run on a **pinned
set** (`PINNED` in `capture.sh`, copied to `/tmp/smartview-capture-fixtures`),
so a contributed fixture never shifts them. Adding a file to `PINNED` can: re-check
this table and every `Down@300ms N` line when you do. FARM-only files
(`*-farm*.json`) are not drives and take no row.

| ↓ | Device | Fixture |
|---|---|---|
| 0 | `/dev/sdg` | `smart-860qvo.json` |
| 1 | `/dev/nvme2` | `smart-990pro.json` |
| 2 | Apple internal | `smart-apple-nvme.json` |
| 3 | `/dev/nvme3` | `smart-hfs512.json` |
| 4 | `/dev/nvme9` | `smart-nvme-errors.json` |
| 5 | `/dev/nvme0` | `smart-nvme.json` |
| 6 | `/dev/sdf` | `smart-sda-errors.json` |
| 7 | `/dev/sda` | `smart-sda.json` |
| 8 | `/dev/sdb` | `smart-sdb.json` |
| 9 | `/dev/sdc` | `smart-sdc-failing.json` |
| 10 | `/dev/sdd` | `smart-sdd-standby.json` |
| 11 | `/dev/sde` | `smart-sde-nodata.json` |
| 12 | `/dev/nvme1` | `smart-sn730.json` |
| 13 | `/dev/sdi` | `smart-st4000dm004.json` |
| 14 | `/dev/sdh` | `smart-wuh721414.json` |

FARM logs pair with their drive by serial (`fixtureFarm`), and a hand-crafted
copy serialled `<serial>-TAG` shares its source's log, so `/dev/sda`, `/dev/sdf`
and `/dev/sdc` all carry a FARM tab — which is what keeps Logs at `6` on
`/dev/sdf`. `TestFixtureFarmPairsBySerial` pins that pairing.

| Image | Drive | Key | Why that drive |
|---|---|---|---|
| overview | `/dev/sda` | `1` | ATA seeds the temperature sparkline instantly; NVMe would show an empty panel |
| attributes | `/dev/sda` | `2` | full 22-row ATA table |
| statistics | `/dev/sda` | `3` | Device Statistics pages |
| farm | `/dev/sda` | `4` | a Seagate whose FARM log pairs with it by serial |
| tests | `/dev/sda` | `5` | supports self-tests |
| logs | `/dev/sdf` | `6` | decoded ATA error-log entries on a drive that only grades Caution (`/dev/sdc` has them too, but is failing) |
| fleet | any | `c` `2` | Health & errors shows Failing/Caution/Healthy and the `—` vs `0` distinction |

## Changing the look

Every tape launches `./smartview --theme neon`: the README images are shot in
the **neon** theme, always. Keep the flag on every tape's command line — the
flag also beats whatever theme the capturing user's own `config.toml` sets.

Geometry lives in `tapes/_common.tape`, sourced by all seven so they cannot
drift apart. Edit it and re-render **all** of them; never change one tape's
geometry alone.

`FontSize` is the largest size at which the widest status bar fits: the
Attributes tab adds `s sort` / `f filter`, and the bar ends in
`<theme> · <interval>`. When changing it, check that trailing summary is not
clipped off the right edge of `attributes.png`.

## Gotchas

| Symptom | Cause |
|---|---|
| PNG is blank, or shows the shell prompt with the command un-run | `Sleep` after `Enter` too short. 6 s is the tested floor for the first full tview draw |
| `Screenshot` silently writes nothing | vhs needs recorded frames to copy from: `Show`, then `Sleep 2s`, then `Screenshot` |
| Characters or the `Enter` dropped | `Set TypingSpeed` below the default drops input into the pty. Leave it unset |
| `Invalid command: private` from vhs | an unquoted path. Every path in a tape is quoted |
| Yellow root banner in every shot | the capture runs unprivileged, exactly as a first-time user does. Deliberate — leave it |

## After capturing

New or renamed images need wiring into the README — use the
`updating-readme-images` skill. This skill stops at writing PNGs; it does not
edit the README and does not commit.
