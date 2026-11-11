# Key and value completion from the data. From the repository root, run:
#
#   vhs demo/completion.tape
#
# It needs vhs, ttyd, ffmpeg and PowerShell 7 (pwsh).

Output demo/completion.gif
Set Height 640
Source demo/setup.tape

Type "jitto demo.json"
Sleep 500ms
Enter
Sleep 1.5s

# Keys of the document. Ctrl+N writes each one into the input and previews it.
Type "."
Sleep 1.2s
Ctrl+N
Sleep 1.2s
Ctrl+N
Sleep 1.2s
Ctrl+N
Sleep 1s

# Narrow the candidates and complete with Tab.
Ctrl+N
Type "o"
Sleep 800ms
Tab
Sleep 800ms

# The keys of every order.
Type "[] | select(."
Sleep 1.2s
Type "st"
Sleep 600ms
Tab
Sleep 800ms

# Values after ==. The expression fails while it is incomplete; the last good
# result stays.
Type ` == "`
Sleep 1.5s
Type "s"
Sleep 600ms
Tab
Sleep 800ms
Type ")"
Sleep 1.2s

Type " | ."
Sleep 1.2s
Type "to"
Sleep 600ms
Tab
Sleep 1.5s

# The result goes to stdout and the expression to stderr.
Enter
Sleep 2.5s
