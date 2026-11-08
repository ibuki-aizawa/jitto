# 文字コード

[English](encoding.md) | **日本語**

UTF-8 以外の入力の読み方と、PowerShell から日本語を含む JSON や Web の応答を渡す方法をまとめています。そのほかの使い方は [README](../README.ja.md) を参照してください。

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

### Web の応答を渡す

`Invoke-WebRequest` や `Invoke-RestMethod` の結果を、そのままパイプで渡すことはできません。PowerShell は外部コマンドに、JSON ではなく、画面に表示するときのテキストを渡すためです。長い値は途中で切られます。このとき Windows 版の jitto は、正しい渡し方を案内して終了します。

代わりに、JSON のテキストを渡します。

| 環境 | コマンド |
|---|---|
| PowerShell 7 | `(Invoke-WebRequest URL).Content \| jitto` |
| PowerShell 5.1 | 先に `$OutputEncoding = [Text.Encoding]::UTF8` を設定してから `(Invoke-WebRequest URL -UseBasicParsing).Content \| jitto` |

- `.Content` は、受け取った応答の本文そのままです。
- `Invoke-RestMethod` は JSON を PowerShell のオブジェクトに変換します。`Invoke-RestMethod URL | ConvertTo-Json -Depth 100 | jitto` で JSON に戻せますが、元の JSON と変わることがあります。`-Depth` を付けないと 3 段目より深いオブジェクトは文字列になり、PowerShell 7 では要素が 1 つの配列がその要素だけになります。なるべく `.Content` を使ってください。
- PowerShell 5.1 では `-UseBasicParsing` を付けます。付けないと、`Invoke-WebRequest` が「オブジェクト参照がオブジェクト インスタンスに設定されていません。」のようなエラーで失敗することがあります。
- PowerShell 5.1 は、サーバーが文字コードを示さないとき（`Content-Type: application/json` だけのときなど）、応答の日本語を文字化けさせます。その場合は、受け取ったバイト列がそのまま残るよう、ファイルに保存してから開きます。

  ```powershell
  Invoke-WebRequest URL -UseBasicParsing -OutFile data.json
  jitto data.json
  ```

`curl.exe` も使えます（`curl.exe -s URL | jitto`）。PowerShell 7 は、その出力をそのまま jitto に渡します。PowerShell 5.1 では、先に `$OutputEncoding = [Console]::OutputEncoding = [Text.Encoding]::UTF8` を設定します。また 5.1 の `curl` は `Invoke-WebRequest` の別名なので、`curl.exe` と書きます。

### 結果を受け取る

PowerShell 5.1 で、UTF-8 の結果を `>` やパイプで受けると、ファイルでは日本語が文字化けします（画面の表示は正常です）。受けるときは、先にコンソールの文字コードも UTF-8 に設定します。

```powershell
$OutputEncoding = [Console]::OutputEncoding = [Text.Encoding]::UTF8
Get-Content -Encoding UTF8 data.json | jitto | Set-Content -Encoding UTF8 result.json
```
