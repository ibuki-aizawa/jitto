# jitto

Windows 向けのインタラクティブ jq ツールです。jq 式を入力すると、結果がその場で表示されます。

Go 製のシングルバイナリで、外部の jq は必要ありません。jq 式の評価には [gojq](https://github.com/itchyny/gojq) を使っています。

## インストール

winget での配布は準備中です。現時点ではソースからビルドしてください（Go 1.27 以降が必要です）。

```powershell
git clone https://github.com/ibuki-aizawa/jitto.git
cd jitto
go build -o jitto.exe .
```

## 使い方

```powershell
# ファイルを開く
jitto data.json

# 標準入力から読む
curl https://example.com/data.json | jitto

# 確定した結果を次のコマンドへ渡す
jitto data.json | Set-Content result.json
```

`jitto --version`（`-V`）でバージョン、`jitto --help`（`-h`）でヘルプを表示します。

上部の入力欄に jq 式を入力すると、下部のペインに結果が表示されます。Enter で確定すると、結果を stdout、式を stderr に出力して終了します。結果だけがパイプやリダイレクトに流れ、式は画面に残るので、スクリプトに転用できます。

式がエラーのときは、直前の正常な結果を残したままエラーを表示します。この間は Enter を押しても確定しません。

## キー操作

| キー | 動作 |
|---|---|
| Enter | 結果を出力して終了 |
| Ctrl+C | 何も出力せずに終了 |
| ↑ ↓ | 結果を1行スクロール |
| PgUp PgDn | 結果を1ページスクロール |
| Shift+← Shift+→ | 結果を横にスクロール |
| ← → など | 入力欄の編集 |

## 動作の詳細

- 複数の JSON（NDJSON など）を読み込むと、jq と同じくドキュメントごとに式を適用します。
- 空の式は `.` として扱います。
- プレビューは 1 MiB までで、評価は 5 秒で打ち切ります。打ち切られた式でも、Enter を押すと上限なしで評価して、すべての結果を出力します。時間がかかるときは、式を編集すると評価を中止でき、Ctrl+C で終了できます。

### 終了コード

| コード | 意味 |
|---|---|
| 0 | 確定して出力した |
| 1 | ファイルを読めない、JSON が不正、stderr がリダイレクトされているなど |
| 2 | 使い方の誤り |
| 130 | Ctrl+C で中止した |

画面を stderr に描画するため、stderr はリダイレクトできません。

## 制約

jq と異なる点の多くは gojq の仕様によるものです。

- オブジェクトのキーはソートして出力され、入力の順序は保たれません。
- 次の機能は使えません。
  - `keys_unsorted`、`$__loc__`
  - `debug`、`stderr`、`input_filename`
  - `input`、`inputs`
- `-r` などのコマンドラインオプションはありません。
- UTF-16 のファイルは読めません。UTF-8 に変換してください。UTF-8 の BOM は問題ありません。
- 10000 段を超えてネストした結果は表示できません。

jq と同じく、キーに日本語などを使うときは `."名前"` のように引用符で囲みます。

## PowerShell 5.1 と日本語

Windows PowerShell 5.1 は、外部コマンドとの間で文字コードを変換します。そのため、日本語を含むデータが jitto に届く前後で壊れることがあります。

| 操作 | 結果 |
|---|---|
| `Get-Content -Encoding UTF8 data.json \| jitto` | 日本語が `??` に置き換わる（エラーにならない） |
| `Get-Content data.json \| jitto` | 文字化けして JSON エラーになることがある |
| `jitto data.json > out.txt` | 画面は正常だが、ファイルでは文字化けする |

入力は、パイプではなくファイル引数で渡すのが確実です。出力もパイプで受けるときは、先に文字コードを UTF-8 に設定します。

```powershell
$OutputEncoding = [Console]::OutputEncoding = [Text.Encoding]::UTF8
Get-Content -Encoding UTF8 data.json | jitto | Set-Content -Encoding UTF8 result.json
```

なお、5.1 の `>` で保存したファイルは UTF-16 になるため、jitto では読み込めません。

PowerShell 7 と cmd では、この問題は確認していません。
