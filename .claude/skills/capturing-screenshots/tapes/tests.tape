# Tests tab on /dev/sda - self-test selector.
Output "/tmp/smartview-capture.gif"   # throwaway; the PNG is what we want
Source ".claude/skills/capturing-screenshots/tapes/_common.tape"

Hide
Type "./smartview --fixtures internal/smart/testdata"
Enter
Sleep 6s                    # first full tview draw; less than this renders blank
Down@300ms 4
Type "5"
Sleep 1500ms
Show
Sleep 2s                    # Screenshot needs recorded frames to copy from
Screenshot "docs/images/tests.png"
Sleep 1s
Hide
Type "q"
Sleep 500ms
