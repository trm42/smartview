# Overview tab on /dev/sda (Seagate 22 TB) - identity + seeded ATA temperature trend.
Output "/tmp/smartview-capture.gif"   # throwaway; the PNG is what we want
Source ".claude/skills/capturing-screenshots/tapes/_common.tape"

Hide
Type "./smartview --fixtures internal/smart/testdata"
Enter
Sleep 6s                    # first full tview draw; less than this renders blank
Down@300ms 4
Type "1"
Sleep 1500ms
Show
Sleep 2s                    # Screenshot needs recorded frames to copy from
Screenshot "docs/images/overview.png"
Sleep 1s
Hide
Type "q"
Sleep 500ms
