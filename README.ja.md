# jitto

[![CI](https://github.com/ibuki-aizawa/jitto/actions/workflows/ci.yml/badge.svg)](https://github.com/ibuki-aizawa/jitto/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ibuki-aizawa/jitto)](https://github.com/ibuki-aizawa/jitto/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/ibuki-aizawa/jitto)](go.mod)
[![License](https://img.shields.io/github/license/ibuki-aizawa/jitto)](LICENSE)

[English](README.md) | **日本語**

Windows 向けのインタラクティブ jq です。jq 式を入力すると、そのそばから結果が表示されます。

名前は、JSON を「じっと」見つめるところから付けました。

![jitto のデモ](demo/demo.gif)

## 特徴

- **結果をその場で表示**: 1 文字入力するたびに、結果のペインを更新します。結果は jq と同じ配色で色分けします。
- **データからキーを補完**: 式を実際の JSON に適用して、その位置に現れるキーを候補に出します。`.[] | ...` や `map(...)` の中でも使えます。Ctrl+N / Ctrl+P で候補を入力欄に書き込むと、その式の結果もすぐ表示します。
- **直前の結果を残す**: 式が書きかけやエラーの間も、直前の正常な結果を残したままエラーを表示します。
- **スクリプトに使える**: Enter で確定すると、結果を stdout、式を stderr に出力します。結果は次のコマンドにパイプで渡せて、式はそのままスクリプトに転用できます。
- **Windows で使いやすく**: Windows Terminal、PowerShell、cmd で動きます。UTF-8 のほか、BOM 付きの UTF-16 と Shift_JIS も読めます。
- **シングルバイナリ**: jq のインストールは不要です。jq 式の評価には [gojq](https://github.com/itchyny/gojq) を使っています。

## クイックスタート

```powershell
# ファイルを開く
jitto data.json

# 標準入力から読む
curl https://example.com/data.json | jitto

# 確定した結果を次のコマンドへ渡す
jitto data.json | Set-Content result.json

# Shift_JIS のファイルを開く
jitto --encoding sjis data.json
```

式を入力して Enter を押すと結果を出力し、Ctrl+C を押すと何も出力せずに終了します。

## インストール

winget での配布は準備中です。

### GitHub Releases からダウンロードする

1. [Releases](https://github.com/ibuki-aizawa/jitto/releases/latest) から `jitto-windows-amd64.exe` をダウンロードします。
2. 必要に応じて `jitto.exe` に名前を変え、PATH の通ったフォルダーに置きます。

実行ファイルにはコード署名がありません。エクスプローラーから初めて開くときに「不明な発行元」と表示されることがあります。続けるには「実行」を選んでください。ファイルが壊れていないか確かめたいときは、リリースに添付した `SHA256SUMS` と照合します。

```powershell
Get-FileHash .\jitto-windows-amd64.exe
```

### ソースからビルドする

Go 1.27 以降が必要です。

```powershell
git clone https://github.com/ibuki-aizawa/jitto.git
cd jitto
go build -o jitto.exe .
```

## 使い方

```
jitto [--encoding NAME] FILE
COMMAND | jitto [--encoding NAME]
```

`FILE` を指定しないときは、標準入力から JSON を読みます。`jitto --version`（`-V`）でバージョン、`jitto --help`（`-h`）でヘルプを表示します。

上部の入力欄に jq 式を入力すると、下部のペインに結果を表示します。空の式は `.` として扱います。

Enter で確定すると、結果を stdout、式を stderr に出力して終了します。パイプやリダイレクトには結果だけが流れ、式は画面に残ります。式がエラーの間は、Enter を押しても確定しません。

### 色

結果は jq と同じ配色で表示します。キーは青の太字、文字列は緑、`null` は灰色、括弧は太字です。Enter で確定した結果も、コンソールに出すときは同じ色で表示し、パイプやファイルには色なしの JSON を出します。環境変数 `NO_COLOR` を設定すると色を付けません。`CLICOLOR_FORCE=1` を設定すると、パイプに出すときも色を付けます。

### キーの補完

`.` や `.items[0].na` のようにキーを書きかけると、その位置で使えるキーを、書きかけのキーの下に一覧で表示します。候補は式を実際のデータに適用して集めるので、`.[] | .na` や `map(.na` では、すべての要素のキーが候補に出ます。

- 候補は書きかけの部分で絞り込みます。大文字と小文字は区別しません。
- Ctrl+N / Ctrl+P を押すと、Vim の補完と同じく、候補を順に入力欄へ書き込み、結果のペインにもその式の結果を表示します。最後の候補の次（先頭の候補の前）では、書きかけていた元の値に戻ります。
- そのまま文字を入力するか Tab / Ctrl+Y を押すと、書き込んだ候補で続けられます。候補を選ばずに Tab / Ctrl+Y を押すと、先頭の候補を入力します。
- Esc を押すと、書きかけていた元の値に戻して一覧を閉じます。
- `名前` や `a-b` のように引用符が必要なキーは、`."名前"` の形で入力されます。

### キー操作

| キー | 動作 |
|---|---|
| Enter | 結果を出力して終了 |
| Ctrl+C | 何も出力せずに終了 |
| ↑ ↓ | 結果を 1 行スクロール |
| PgUp PgDn | 結果を 1 ページスクロール |
| Shift+← Shift+→ | 結果を横にスクロール |
| Ctrl+N Ctrl+P | キーの候補を順に入力欄へ書き込み、その結果を表示 |
| Tab Ctrl+Y | 書き込んだ候補で確定（選んでいなければ先頭の候補を入力） |
| Esc | 候補の一覧を閉じる（選択中なら元の値に戻す。次に編集するまで閉じたまま） |
| Ctrl+W（Alt+Backspace） | カーソルの前の単語を削除 |
| Alt+D（Alt+Delete） | カーソルの後ろの単語を削除 |
| Ctrl+← Ctrl+→（Alt+B Alt+F） | 単語単位でカーソルを移動 |
| ← → など | 入力欄の編集 |

マウスのホイールでも結果をスクロールできます。

単語は、キーに使う文字（英数字、`_`、日本語などの文字）が続く部分と、記号が続く部分で区切ります。たとえば `.items[0].name` で Ctrl+W を押すと `name` だけが消えて `.items[0].` が残り、キーの候補を選び直せます。

## 日本語と文字コード

入力は UTF-8 として読みます。先頭に BOM があるときは、BOM に従って UTF-8 か UTF-16 として読みます。

Shift_JIS の入力は、`--encoding sjis` を付けて読みます（`--encoding=sjis` とも書けます）。名前は `shift_jis`、`sjis`、`cp932`、`windows-31j` のどれでもよく、大文字と小文字は区別しません。Windows で使われる Shift_JIS（コードページ 932）として読むので、①や髙などの機種依存文字も読めます。先頭に UTF-16 の BOM があるときは、`--encoding` よりも BOM を優先します。UTF-8 の BOM は読み飛ばし、残りを Shift_JIS として読みます（PowerShell 5.1 は、Shift_JIS のパイプにも UTF-8 の BOM を付けることがあるためです）。

結果は、入力の文字コードにかかわらず UTF-8 で出力します。

### PowerShell のパイプ

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

cmd の `type data.json | jitto` は変換が起こらないので、そのまま使えます（Shift_JIS なら `--encoding sjis` を付けます）。PowerShell 5.1 の既定の `$OutputEncoding` は US-ASCII なので、設定しないと日本語は `??` になります。5.1 の `>` で保存したファイルは BOM 付きの UTF-16 になりますが、jitto はこれもファイル引数で読み込めます。

### 結果を受け取る

結果は UTF-8 で出力します。PowerShell 5.1 で結果を `>` やパイプで受けると、ファイルでは文字化けします（画面の表示は正常です）。受けるときは、先にコンソールの文字コードも UTF-8 に設定します。

```powershell
$OutputEncoding = [Console]::OutputEncoding = [Text.Encoding]::UTF8
Get-Content -Encoding UTF8 data.json | jitto | Set-Content -Encoding UTF8 result.json
```

## 動作の詳細

- 複数の JSON（NDJSON など）を読み込むと、jq と同じくドキュメントごとに式を適用します。
- キーの候補を集めるときは、調べる値を 1 万個、候補を 1000 個までとし、1 秒で打ち切ります。
- プレビューは 1 MiB までで、評価は 5 秒で打ち切ります。打ち切られた式でも、Enter を押すと上限なしで評価して、すべての結果を出力します。時間がかかるときは、式を編集すると評価を中止でき、Ctrl+C で終了できます。
- 画面を stderr に描画するため、stderr はリダイレクトできません。

### 終了コード

| コード | 意味 |
|---|---|
| 0 | 確定して出力した |
| 1 | ファイルを読めない、JSON が不正、stderr がリダイレクトされているなど |
| 2 | 使い方の誤り |
| 130 | Ctrl+C で中止した |

## 制約

jq と異なる点の多くは gojq の仕様によるものです。

- オブジェクトのキーはソートして出力され、入力の順序は保たれません。
- `keys_unsorted`、`$__loc__`、`debug`、`stderr`、`input_filename`、`input`、`inputs` は使えません。
- `-r` などのコマンドラインオプションはありません。
- 入力として読めるのは、UTF-8、BOM 付きの UTF-16、Shift_JIS（`--encoding sjis`）です。BOM のない UTF-16 や EUC-JP などは、UTF-8 に変換してください。
- Shift_JIS の入力を `--encoding` なしで読むと、日本語が `�` に置き換わります。このときは、画面下のヘルプの位置に `--encoding sjis` を勧める注意を表示します。「表」や「ソ」のように 2 バイト目が `\` と同じ値の文字があると、JSON のエラーで終了することもあり、そのときはエラーのメッセージに同じ案内を添えます。
- 10000 段を超えてネストした結果は表示できません。
- jq と同じく、日本語などのキーは `."名前"` のように引用符で囲みます。`.名前` と入力すると、エラーと一緒に正しい書き方を表示します。

キーの補完には、次の制約があります。

- 候補に出るのはオブジェクトのキーだけです。配列の添字（`[0]`、`[]`）は候補に出ません。
- 引用符の中で書きかけのキー（`."名` など）には、候補を出しません。`.` まで入力した時点で、引用符付きの候補が出ます。
- `if` を `end` まで、`def` を `;` まで書き終えていない間（たとえば `if . then .na`）は、候補を出しません。閉じたあとの部分（`if . then . end | .na`）では、候補が出ます。

## ライセンス

[MIT ライセンス](LICENSE)です。

jitto の実行ファイルには、Go と複数のオープンソースのライブラリが含まれています。それらのライセンスは、各リリースに添付する `THIRD_PARTY_NOTICES.txt` にまとめています。
