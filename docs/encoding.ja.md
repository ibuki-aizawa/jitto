# 文字コード

[English](encoding.md) | **日本語**

UTF-8 以外の入力の読み方と、PowerShell から日本語を含む JSON を渡す方法をまとめています。そのほかの使い方は [README](../README.ja.md) を参照してください。

## 入力

入力は UTF-8 として読みます。先頭に BOM があるときは、BOM に従って UTF-8 か UTF-16 として読みます。

Shift_JIS の入力は、`--encoding sjis` を付けて読みます（`--encoding=sjis` とも書けます）。名前は `shift_jis`、`sjis`、`cp932`、`windows-31j` のどれでもよく、大文字と小文字は区別しません。Windows で使われる Shift_JIS（コードページ 932）として読むので、①や髙などの機種依存文字も読めます。

- 先頭に UTF-16 の BOM があるときは、`--encoding` よりも BOM を優先します。
- UTF-8 の BOM は読み飛ばし、残りを Shift_JIS として読みます。PowerShell 5.1 は、Shift_JIS のパイプにも UTF-8 の BOM を付けることがあるためです。

BOM のない UTF-16 や EUC-JP などは読めません。UTF-8 に変換してください。

### --encoding なしで Shift_JIS を読んだとき

Shift_JIS の入力を UTF-8 として読むと、日本語が `�` に置き換わります。このときは、画面下のヘルプの位置に `--encoding sjis` を勧める注意を表示します。

「表」や「ソ」のように 2 バイト目が `\` と同じ値の文字があると、JSON のエラーで終了することもあります。そのときは、エラーのメッセージに同じ案内を添えます。

## 出力

結果は、入力の文字コードにかかわらず UTF-8 で出力します。

## PowerShell

### パイプで渡す

PowerShell のパイプでは、文字コードの変換が 2 回起こります。

1. `Get-Content` がファイルを読むとき（`-Encoding` で指定した文字コードとして読む）
2. 外部コマンドに渡すとき（`$OutputEncoding` の文字コードに変換する）

どちらかが合っていないと、日本語は jitto に届く前に `??` や `�` に置き換わります。この状態は jitto では元に戻せず、注意も表示されません。

入力は、パイプではなくファイル引数で渡すのが確実です。

```powershell
jitto data.json                   # UTF-8 のファイル
jitto --encoding sjis data.json   # Shift_JIS のファイル
```

パイプで渡すときは、次のようにします。

| 環境 | UTF-8 のファイル | Shift_JIS のファイル |
|---|---|---|
| PowerShell 7 | `Get-Content data.json \| jitto` | `Get-Content -Encoding 932 data.json \| jitto` |
| PowerShell 5.1 | 先に `$OutputEncoding = [Text.Encoding]::UTF8` を設定してから `Get-Content -Encoding UTF8 data.json \| jitto` | 先に `$OutputEncoding = [Text.Encoding]::GetEncoding(932)` を設定してから `Get-Content data.json \| jitto --encoding sjis` |

- cmd の `type data.json | jitto` は変換が起こらないので、そのまま使えます。Shift_JIS なら `--encoding sjis` を付けます。
- PowerShell 5.1 の既定の `$OutputEncoding` は US-ASCII なので、設定しないと日本語は `??` になります。
- PowerShell 5.1 の `>` で保存したファイルは BOM 付きの UTF-16 になりますが、jitto はこれもファイル引数で読み込めます。

### 結果を受け取る

PowerShell 5.1 で、UTF-8 の結果を `>` やパイプで受けると、ファイルでは日本語が文字化けします（画面の表示は正常です）。受けるときは、先にコンソールの文字コードも UTF-8 に設定します。

```powershell
$OutputEncoding = [Console]::OutputEncoding = [Text.Encoding]::UTF8
Get-Content -Encoding UTF8 data.json | jitto | Set-Content -Encoding UTF8 result.json
```
