# Logs tab on /dev/sdf (smart-sda-errors.json) - the only fixture with decoded error entries.
Output "/tmp/smartview-capture.gif"   # throwaway; the PNG is what we want
Source ".claude/skills/capturing-screenshots/tapes/_common.tape"

Hide
Type "./smartview --fixtures internal/smart/testdata"
Enter
Sleep 6s                    # first full tview draw; less than this renders blank
Down@300ms 3
Type "6"
Sleep 1500ms
Show
Sleep 2s                    # Screenshot needs recorded frames to copy from
Screenshot "docs/images/logs.png"
Sleep 1s
Hide
Type "q"
Sleep 500ms
