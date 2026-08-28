# Fleet comparison across all seven fixtures.
Output "/tmp/smartview-capture.gif"   # throwaway; the PNG is what we want
Source ".claude/skills/capturing-screenshots/tapes/_common.tape"

Hide
Type "./smartview --fixtures internal/smart/testdata"
Enter
Sleep 6s                    # first full tview draw; less than this renders blank
Type "c"
Sleep 500ms
Type "2"                    # Health & errors reads better than Temperature: NVMe fixtures report no min/max
Sleep 1500ms
Show
Sleep 2s                    # Screenshot needs recorded frames to copy from
Screenshot "docs/images/fleet.png"
Sleep 1s
Hide
Type "q"
Sleep 500ms
