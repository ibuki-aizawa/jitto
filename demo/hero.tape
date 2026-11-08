# The still image at the top of the README. From the repository root, run:
#
#   vhs demo/hero.tape
#
# It needs vhs, ttyd, ffmpeg and PowerShell 7 (pwsh).

Set Height 520
Source demo/setup.tape

Hide
Type "jitto demo.json"
Enter
Sleep 2s
Type ".orders"
Sleep 1s
Ctrl+T
Sleep 2s
Ctrl+N
Ctrl+N
Sleep 2s
Show
Sleep 500ms
Screenshot demo/hero.png
Hide
Ctrl+C
