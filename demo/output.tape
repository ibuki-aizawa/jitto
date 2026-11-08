# Raw strings and pipes. From the repository root, run:
#
#   vhs demo/output.tape
#
# It needs vhs, ttyd, ffmpeg and PowerShell 7 (pwsh).

Output demo/output.gif
Set Height 640
Source demo/setup.tape

# The confirmed result goes to the next command in the pipe.
Type `jitto demo.json | ForEach-Object { "mailto:$_" }`
Sleep 500ms
Enter
Sleep 1.5s

Type ".customers[].email"
Sleep 1.5s

# Alt+R switches to raw strings, like jq -r.
Alt+r
Sleep 2s

# The emails go through the pipe; the expression stays on screen.
Enter
Sleep 3s
