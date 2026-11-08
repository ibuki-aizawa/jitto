# Settings and setup shared by the other tapes, which read this file with Source.
# Each tape sets its own Output and Height first.

Set Shell pwsh
Set FontFamily "Cascadia Mono"
Set FontSize 18
Set Width 1200
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 90ms

# Build jitto and keep the prompt short, out of view.
Hide
Type "go build -o jitto.exe . ; Set-Alias jitto (Resolve-Path .\jitto.exe) ; cd demo ; function prompt { 'PS> ' } ; Clear-Host"
Enter
Sleep 5s
Show
