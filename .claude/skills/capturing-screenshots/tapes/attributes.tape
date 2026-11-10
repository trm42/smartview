# Attributes tab on /dev/sda - severity-sorted ATA attribute table.
Output "/tmp/smartview-capture.gif"   # throwaway; the PNG is what we want
Source ".claude/skills/capturing-screenshots/tapes/_common.tape"

Hide
Type "./smartview --theme neon --fixtures /tmp/smartview-capture-fixtures"
Enter
Sleep 6s                    # first full tview draw; less than this renders blank
Down@300ms 7
Type "2"
Sleep 1500ms
Show
Sleep 2s                    # Screenshot needs recorded frames to copy from
Screenshot "docs/images/attributes.png"
Sleep 1s
Hide
Type "q"
Sleep 500ms
