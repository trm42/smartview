---
name: updating-readme-images
description: Use after smartview screenshots are recaptured or added, or when README image links may be stale, orphaned, or wrongly described
---

# Wiring screenshots into the README

Recapturing overwrites existing files in place, so a straight refresh needs no
README edit at all. The work is the delta and the drift: images with no home,
links with no file, and alt text that no longer matches the picture.

## 1. Inventory

```sh
.claude/skills/updating-readme-images/check-images.sh
```

Reports three failures: `DANGLING` (README links a file that does not exist),
`ORPHAN` (an image nothing references), and `MIXED` (the set has drifted to more
than one geometry — recapture, don't crop).

## 2. Place each orphan

An image goes **immediately after the paragraph that describes the view**, not
in a gallery. The README's existing structure is the map:

| View | Section |
|---|---|
| overview | the intro, above `## Features` — this one is the hero image |
| attributes, statistics, farm, logs, tests | its own paragraph under `### The tabs` |
| fleet | `### Fleet comparison` |

## 3. Write the alt text from the image, not from the tab name

House style is `smartview <view> tab — <what is actually visible>`, em dash,
lower-case after it:

```markdown
![smartview Logs tab — SMART error log and self-test history](docs/images/logs.png)
```

**Open the PNG and describe what you see.** The alt text is the one part of this
that cannot be checked mechanically, and it is the part that silently goes stale
— a shot recaptured on a different fixture, or a panel that has since been
renamed, keeps its old sentence and starts lying.

## 4. Re-run the checker

It must print `OK` before you are done. Then confirm the new lines render as
images, not as literal markdown, by viewing the README.

## Scope

This skill edits `README.md` only. It does not recapture images (use
`capturing-screenshots`), does not touch `CLAUDE.md`, and does not commit.
