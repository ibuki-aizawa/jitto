# Encodings

**English** | [日本語](encoding.ja.md)

How jitto reads text that is not plain UTF-8, and how to pass Japanese text to it from PowerShell. For everything else, see the [README](../README.md).

## Input

jitto reads UTF-8 by default. With a BOM at the start, it reads UTF-8 or UTF-16 as the BOM says.

For Shift_JIS, add `--encoding sjis` (or `--encoding=sjis`). The names `shift_jis`, `sjis`, `cp932` and `windows-31j` all work, in any case. jitto reads it as the Windows code page 932, so vendor characters such as ① and 髙 work too.

- A UTF-16 BOM wins over `--encoding`.
- A UTF-8 BOM is skipped, and the rest is read as Shift_JIS, because PowerShell 5.1 can add one even to Shift_JIS.

Other encodings, such as UTF-16 without a BOM and EUC-JP, cannot be read. Convert them to UTF-8 first.

### Shift_JIS without --encoding

Read as UTF-8, Shift_JIS text turns into `�`. jitto then shows a notice suggesting `--encoding sjis` where the help usually is.

Characters whose second byte is the same as `\`, such as 表 and ソ, can also make the JSON invalid. jitto then exits with an error, and the error message gives the same hint.

## Output

The output is always UTF-8, whatever the input encoding.

## PowerShell

### Pipes

PowerShell converts text twice in a pipe:

1. When `Get-Content` reads the file, as the encoding given by `-Encoding`.
2. When it passes the text to an external command, as `$OutputEncoding`.

If either is wrong, Japanese text is replaced with `??` or `�` before it reaches jitto. jitto cannot undo this, and shows no notice.

Passing the file as an argument avoids both conversions:

```powershell
jitto data.json                   # UTF-8 file
jitto --encoding sjis data.json   # Shift_JIS file
```

To use a pipe:

| Shell | UTF-8 file | Shift_JIS file |
|---|---|---|
| PowerShell 7 | `Get-Content data.json \| jitto` | `Get-Content -Encoding 932 data.json \| jitto` |
| PowerShell 5.1 | `$OutputEncoding = [Text.Encoding]::UTF8`, then `Get-Content -Encoding UTF8 data.json \| jitto` | `$OutputEncoding = [Text.Encoding]::GetEncoding(932)`, then `Get-Content data.json \| jitto --encoding sjis` |

- In cmd, `type data.json | jitto` passes the bytes unchanged. For Shift_JIS, add `--encoding sjis`.
- The default `$OutputEncoding` of PowerShell 5.1 is US-ASCII, so without the setting above, Japanese text becomes `??`.
- Files saved with `>` in PowerShell 5.1 are UTF-16 with a BOM. jitto reads them as a file argument.

### Receiving the output

When PowerShell 5.1 receives the UTF-8 output with `>` or a pipe, Japanese text in the file is garbled (the screen is fine). Set the console encoding to UTF-8 first:

```powershell
$OutputEncoding = [Console]::OutputEncoding = [Text.Encoding]::UTF8
Get-Content -Encoding UTF8 data.json | jitto | Set-Content -Encoding UTF8 result.json
```
