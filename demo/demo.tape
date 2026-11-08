# The demo GIF in the README. From the repository root, run:
#
#   vhs demo/demo.tape
#
# It needs vhs, ttyd, ffmpeg and PowerShell 7 (pwsh).

Output demo/demo.gif

Set Shell pwsh
Set FontFamily "Cascadia Mono"
Set FontSize 18
Set Width 1200
Set Height 640
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 90ms

# Build jitto and keep the prompt short, out of view.
Hide
Type "go build -o jitto.exe . ; Set-Alias jitto (Resolve-Path .\jitto.exe) ; cd demo ; function prompt { 'PS> ' } ; Clear-Host"
Enter
Sleep 5s
Show

Type "jitto demo.json"
Sleep 500ms
Enter
Sleep 2s

# Keys of the document. Ctrl+N writes each one into the input and previews it.
Type "."
Sleep 1.5s
Ctrl+N
Sleep 1.5s
Ctrl+N
Sleep 1.5s
Ctrl+N
Sleep 1.5s
Ctrl+N
Sleep 1s

# Narrow the candidates and complete with Tab.
Type "o"
Sleep 800ms
Tab
Sleep 1s

# The keys of every order.
Type "[] | select(."
Sleep 1.5s
Type "st"
Sleep 600ms
Tab
Sleep 800ms

# The expression fails while it is incomplete; the last good result stays.
Type ` == "shipped")`
Sleep 1.5s

Type " | ."
Sleep 1.5s
Type "to"
Sleep 600ms
Tab
Sleep 2s

# Examples made from the result. Ctrl+N tries each one, Tab keeps it.
Ctrl+T
Sleep 2s
Ctrl+N
Sleep 1.5s
Ctrl+N
Sleep 1.5s
Ctrl+P
Sleep 1s
Tab
Sleep 1.5s

# The result goes to stdout and the expression to stderr.
Enter
Sleep 3s
