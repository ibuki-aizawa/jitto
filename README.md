# jitto

[![CI](https://github.com/ibuki-aizawa/jitto/actions/workflows/ci.yml/badge.svg)](https://github.com/ibuki-aizawa/jitto/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ibuki-aizawa/jitto)](https://github.com/ibuki-aizawa/jitto/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/ibuki-aizawa/jitto)](go.mod)
[![License](https://img.shields.io/github/license/ibuki-aizawa/jitto)](LICENSE)

**English** | [日本語](README.ja.md)

An interactive jq for Windows. Type a jq expression and watch the result update as you type.

The name comes from the Japanese word *jitto* (じっと), "to stare intently" — as in staring at your JSON until it makes sense.

![jitto demo](demo/demo.gif)

> 日本語の説明は [README.ja.md](README.ja.md) にあります。Shift_JIS の入力や PowerShell での使い方も、日本語で詳しく説明しています。

## Features

- **Live results**: The result pane updates on every keystroke, colored like jq.
- **Key completion from your data**: jitto runs the expression on your JSON and suggests the keys that actually appear there, even inside `.[] | ...` or `map(...)`. Ctrl+N / Ctrl+P write each candidate into the input and preview its result.
- **Keeps the last good result**: While the expression is incomplete or has an error, the previous result stays on screen with the error message.
- **Fits into scripts**: Enter prints the result to stdout and the expression to stderr. Pipe the result to the next command, and copy the expression into your script.
- **Made for Windows**: Works in Windows Terminal, PowerShell and cmd. Reads UTF-8, UTF-16 with a BOM and Shift_JIS.
- **A single binary**: No jq to install. jitto evaluates expressions with [gojq](https://github.com/itchyny/gojq).

## Quick start

```powershell
# Open a file
jitto data.json

# Read from standard input
curl https://example.com/data.json | jitto

# Pass the confirmed result to the next command
jitto data.json | Set-Content result.json
```

Type an expression, then press Enter to print the result, or Ctrl+C to quit without output.

## Install

winget support is coming soon.

### From GitHub Releases

1. Download `jitto-windows-amd64.exe` from [Releases](https://github.com/ibuki-aizawa/jitto/releases/latest).
2. Rename it to `jitto.exe` if you like, and put it in a folder on your PATH.

The executable is not code-signed. When you open it from Explorer for the first time, Windows may show "Unknown publisher"; choose "Run" to continue. To check the download, compare it with `SHA256SUMS` attached to the release:

```powershell
Get-FileHash .\jitto-windows-amd64.exe
```

### From source

Requires Go 1.27 or later.

```powershell
git clone https://github.com/ibuki-aizawa/jitto.git
cd jitto
go build -o jitto.exe .
```

## Usage

```
jitto [--encoding NAME] FILE
COMMAND | jitto [--encoding NAME]
```

Without `FILE`, jitto reads JSON from standard input. `jitto --version` (`-V`) shows the version and `jitto --help` (`-h`) shows the help.

Type a jq expression in the input at the top; the result appears in the pane below. An empty expression means `.`.

Press Enter to confirm. jitto prints the result to stdout and the expression to stderr, and exits. Only the result flows into a pipe or redirect, and the expression stays on screen. While the expression has an error, Enter does nothing.

### Colors

The result is colored like jq: keys in bold blue, strings in green, `null` in gray, and brackets in bold. On Enter, the result printed to the console has the same colors, and a pipe or file gets plain JSON. Set `NO_COLOR` to turn colors off, or `CLICOLOR_FORCE=1` to keep them in a pipe.

### Key completion

When you start a key, as in `.` or `.items[0].na`, jitto lists the keys available at that point just below what you typed. It finds them by running the expression on your data, so `.[] | .na` and `map(.na` suggest the keys of every element.

- The list is filtered by what you have typed, ignoring case.
- Ctrl+N / Ctrl+P write the candidates into the input one by one, like completion in Vim, and the result pane shows the result of that expression. After the last candidate (or before the first), you are back to what you typed.
- Keep typing, or press Tab / Ctrl+Y, to continue with the candidate. Without a selection, Tab / Ctrl+Y inserts the first candidate.
- Esc restores what you typed and closes the list.
- Keys that need quotes, such as `a-b`, are inserted as `."a-b"`.

### Keys

| Key | Action |
|---|---|
| Enter | Print the result and exit |
| Ctrl+C | Exit without output |
| ↑ ↓ | Scroll the result by one line |
| PgUp PgDn | Scroll the result by one page |
| Shift+← Shift+→ | Scroll the result sideways |
| Ctrl+N Ctrl+P | Write the next / previous candidate into the input and preview it |
| Tab Ctrl+Y | Accept the candidate (or insert the first one) |
| Esc | Close the candidates (restoring what you typed; they stay closed until you edit) |
| Ctrl+W (Alt+Backspace) | Delete the word before the cursor |
| Alt+D (Alt+Delete) | Delete the word after the cursor |
| Ctrl+← Ctrl+→ (Alt+B Alt+F) | Move by word |
| ← → etc. | Edit the input |

The mouse wheel also scrolls the result.

A word is a run of key characters (letters, digits, `_`) or a run of symbols. In `.items[0].name`, Ctrl+W deletes only `name` and leaves `.items[0].`, so you can pick another key.

## Encodings

jitto reads UTF-8 by default. With a BOM at the start, it reads UTF-8 or UTF-16 as the BOM says.

For Shift_JIS, add `--encoding sjis` (or `--encoding=sjis`). The names `shift_jis`, `sjis`, `cp932` and `windows-31j` all work, in any case. jitto reads it as the Windows code page 932. A UTF-16 BOM wins over `--encoding`; a UTF-8 BOM is skipped, because PowerShell 5.1 can add one even to Shift_JIS.

The output is always UTF-8.

### PowerShell

PowerShell converts text twice in a pipe: when `Get-Content` reads the file (as `-Encoding`), and when it passes the text to an external command (as `$OutputEncoding`). If either is wrong, non-ASCII characters are replaced with `?` before they reach jitto, and jitto cannot detect it.

Passing the file as an argument avoids both conversions. To use a pipe:

| Shell | UTF-8 file | Shift_JIS file |
|---|---|---|
| PowerShell 7 | `Get-Content data.json \| jitto` | `Get-Content -Encoding 932 data.json \| jitto` |
| PowerShell 5.1 | `$OutputEncoding = [Text.Encoding]::UTF8`, then `Get-Content -Encoding UTF8 data.json \| jitto` | `$OutputEncoding = [Text.Encoding]::GetEncoding(932)`, then `Get-Content data.json \| jitto --encoding sjis` |

In cmd, `type data.json | jitto` passes the bytes unchanged. Files saved with `>` in PowerShell 5.1 are UTF-16 with a BOM, which jitto reads as a file argument.

To receive the UTF-8 output in PowerShell 5.1 with `>` or a pipe, set the console encoding first:

```powershell
$OutputEncoding = [Console]::OutputEncoding = [Text.Encoding]::UTF8
Get-Content -Encoding UTF8 data.json | jitto | Set-Content -Encoding UTF8 result.json
```

## Details

- With multiple JSON documents (such as NDJSON), the expression applies to each document, as in jq.
- Key completion examines up to 10,000 values, lists up to 1,000 keys, and stops after 1 second.
- The preview shows up to 1 MiB, and evaluation stops after 5 seconds. Enter evaluates without these limits and prints the whole result. If it takes long, edit the expression to cancel, or press Ctrl+C to quit.
- jitto draws the screen on stderr, so stderr cannot be redirected.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Confirmed and printed the result |
| 1 | Cannot read the file, invalid JSON, stderr redirected, etc. |
| 2 | Invalid usage |
| 130 | Canceled with Ctrl+C |

## Limitations

Most differences from jq come from gojq.

- Object keys are printed in sorted order; the input order is not kept.
- These are not available: `keys_unsorted`, `$__loc__`, `debug`, `stderr`, `input_filename`, `input`, `inputs`.
- There are no command-line options such as `-r`.
- Input must be UTF-8, UTF-16 with a BOM, or Shift_JIS (`--encoding sjis`). Convert other encodings, such as UTF-16 without a BOM, to UTF-8 first.
- Reading Shift_JIS without `--encoding` turns non-ASCII text into `�`. jitto then shows a notice suggesting `--encoding sjis`. Some characters can also make the JSON invalid; the error message includes the same hint.
- Results nested deeper than 10,000 levels cannot be shown.
- Non-identifier keys need quotes, as in jq: `."名前"`. Typing `.名前` shows the correct form with the error.

Key completion:

- Only object keys are suggested, not array indexes such as `[0]` or `[]`.
- Inside quotes (`."na`), no candidates are shown. After `.`, candidates with quotes appear.
- Inside an unfinished `if` (until `end`) or `def` (until `;`), as in `if . then .na`, no candidates are shown. After it is closed, as in `if . then . end | .na`, they are.

## License

[MIT](LICENSE)

The jitto executable includes Go and several open-source libraries. Their licenses are in `THIRD_PARTY_NOTICES.txt`, attached to each release.
