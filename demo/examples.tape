# Example filters made from the data (Ctrl+T). From the repository root, run:
#
#   vhs demo/examples.tape
#
# It needs vhs, ttyd, ffmpeg and PowerShell 7 (pwsh).

Output demo/examples.gif
Set Height 640
Source demo/setup.tape

Type "jitto demo.json"
Sleep 500ms
Enter
Sleep 1.5s

Type ".orders"
Sleep 1s

# Examples for an array of orders, with their keys and values.
Ctrl+T
Sleep 2s

# Ctrl+N tries each one in the input and previews it.
Ctrl+N
Sleep 1.2s
Ctrl+N
Sleep 2s
Ctrl+N
Sleep 600ms
Ctrl+N
Sleep 600ms
Ctrl+N
Sleep 2s

# Tab keeps it, and Enter prints the result.
Tab
Sleep 1.2s
Enter
Sleep 2.5s
