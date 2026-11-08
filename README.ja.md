# jitto

[![CI](https://github.com/ibuki-aizawa/jitto/actions/workflows/ci.yml/badge.svg)](https://github.com/ibuki-aizawa/jitto/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ibuki-aizawa/jitto)](https://github.com/ibuki-aizawa/jitto/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/ibuki-aizawa/jitto)](go.mod)
[![License](https://img.shields.io/github/license/ibuki-aizawa/jitto)](LICENSE)

[English](README.md) | **日本語**

**実際のデータから補完する、インタラクティブな jq。**

入力するそばから結果を表示し、`map()` の中でも本当にあるキーを候補に出します。できあがった式は、そのままスクリプトに持ち帰れます。

名前は、JSON を「じっと」見つめるところから付けました。

![jitto のデモ](demo/demo.gif)

## 特徴

- **結果をその場で表示**: 1 文字入力するたびに、結果のペインを更新します。結果は jq と同じ配色で色分けします。
- **データからキーを補完**: 式を実際の JSON に適用して、その位置に現れるキーを候補に出します。`.[] | ...` や `map(...)` の中でも使えます。Ctrl+N / Ctrl+P で候補を入力欄に書き込むと、その式の結果もすぐ表示します。
- **直前の結果を残す**: 式が書きかけやエラーの間も、直前の正常な結果を残したままエラーを表示します。
- **スクリプトに使える**: Enter で確定すると、結果を stdout、式を stderr に出力します。結果は次のコマンドにパイプで渡せて、式はそのままスクリプトに転用できます。
- **jq の -r と同じ出力**: `-r` を付けて起動するか、いつでも Alt+R で切り替えると、文字列を引用符なしで出力します。切り替えると、結果もすぐに変わります。
- **Windows を第一に、ほかの OS でも**: Windows Terminal、PowerShell、cmd で動き、UTF-8 のほか、BOM 付きの UTF-16 と Shift_JIS も読めます。macOS と Linux でも動きます（`go install` でインストールします）。もっとも念入りに確かめているのは Windows です。
- **シングルバイナリ**: jq のインストールは不要です。jq 式の評価には [gojq](https://github.com/itchyny/gojq) を使っています。

## クイックスタート

```powershell
# ファイルを開く
jitto data.json

# 標準入力から読む
curl.exe https://example.com/data.json | jitto

# 確定した結果を次のコマンドへ渡す
jitto data.json | Set-Content result.json

# jq -r と同じく、文字列を引用符なしで出力する
jitto -r data.json

# Shift_JIS のファイルを開く
jitto --encoding sjis data.json
```

式を入力して Enter を押すと結果を出力し、Ctrl+C を押すと何も出力せずに終了します。

## インストール

winget での配布は準備中です。

### GitHub Releases からダウンロードする

1. [Releases](https://github.com/ibuki-aizawa/jitto/releases/latest) から、PC に合ったファイルをダウンロードします。
   - `jitto-windows-amd64.exe`: Intel や AMD のプロセッサーを使った、たいていの PC 向け
   - `jitto-windows-arm64.exe`: Snapdragon などの Arm のプロセッサーを使った PC 向け

   どちらか分からないときは、「設定」>「システム」>「バージョン情報」の「システムの種類」で確かめられます。
2. 必要に応じて `jitto.exe` に名前を変え、PATH の通ったフォルダーに置きます。

実行ファイルにはコード署名がありません。エクスプローラーから初めて開くときに「不明な発行元」と表示されることがあります。続けるには「実行」を選んでください。ファイルが壊れていないか確かめたいときは、リリースに添付した `SHA256SUMS` と照合します。

```powershell
Get-FileHash .\jitto-windows-*.exe
```

### go install でインストールする

Go 1.27 以降があれば、次のコマンドでインストールできます。

```powershell
go install github.com/ibuki-aizawa/jitto@latest
```

`jitto.exe` は `%USERPROFILE%\go\bin`（`$GOBIN` を設定していればそのフォルダー）に置かれます。このフォルダーは、Go のインストーラーが PATH に追加します。バージョンを指定するときは、`@latest` の代わりに `@v0.3.0` のように書きます。macOS や Linux でも使えます。

### ソースからビルドする

Go 1.27 以降が必要です。

```powershell
git clone https://github.com/ibuki-aizawa/jitto.git
cd jitto
go build -o jitto.exe .
```

## 使い方

```
jitto [-r] [--encoding NAME] FILE
COMMAND | jitto [-r] [--encoding NAME]
```

`FILE` を指定しないときは、標準入力から JSON を読みます。`jitto --version`（`-V`）でバージョン、`jitto --help`（`-h`）でヘルプを表示します。

上部の入力欄に jq 式を入力すると、下部のペインに結果を表示します。空の式は `.` として扱います。

Enter で確定すると、結果を stdout、式を stderr に出力して終了します。パイプやリダイレクトには結果だけが流れ、式は画面に残ります。式がエラーの間は、Enter を押しても確定しません。

### 色

結果は jq と同じ配色で表示します。キーは青の太字、文字列は緑、`null` は灰色、括弧は太字です。Enter で確定した結果も、コンソールに出すときは同じ色で表示し、パイプやファイルには色なしの JSON を出します。環境変数 `NO_COLOR` を設定すると色を付けません。`CLICOLOR_FORCE=1` を設定すると、パイプに出すときも色を付けます。

### 文字列をそのまま出力する（-r）

`-r`（`--raw-output`）を付けると、jq と同じく、結果の文字列を引用符なしでそのまま出力します。たとえば `"one\ntwo"` は、`one` と `two` の 2 行になります。変わるのは結果のトップレベルの文字列だけで、配列、オブジェクト、数値などは JSON のままです。

Alt+R を押すと、いつでも `-r` のオンとオフを切り替えられます。切り替えると式を評価し直し、`-r` がオンの間はプロンプトが `-r> ` になります。Enter を押すと、画面に表示中のモードで出力します。

`-r` の文字列には色を付けません。結果のペインでは、タブを空白に展開し、ほかの制御文字は `^[`（ESC）などの形で表示します。文字列の内容で画面が崩れないようにするためです。stdout には、文字列をそのまま出力します。

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
| Alt+R | `-r`（文字列をそのまま出力）のオンとオフを切り替え |
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

macOS で Alt+R を使うには、ターミナルが Option キーを Meta として送るように設定します。ターミナル.app ではプロファイルの「キーボード」で「Option キーをメタキーとして使用」をオンにし、iTerm2 では Option キーを「Esc+」に設定します。

単語は、キーに使う文字（英数字、`_`、日本語などの文字）が続く部分と、記号が続く部分で区切ります。たとえば `.items[0].name` で Ctrl+W を押すと `name` だけが消えて `.items[0].` が残り、キーの候補を選び直せます。

## 日本語と文字コード

入力は UTF-8 と、BOM 付きの UTF-16 を読めます。Shift_JIS の入力は、`--encoding sjis` を付けて読みます。結果は常に UTF-8 で出力します。

PowerShell のパイプで日本語を含む JSON を渡すには、設定が必要です。その方法と、文字コードごとの詳しい扱いは [docs/encoding.ja.md](docs/encoding.ja.md) を参照してください。

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
- jq のコマンドラインオプションのうち、使えるのは `-r` だけです。`-c` や `-j` などはありません。
- 入力として読めるのは、UTF-8、BOM 付きの UTF-16、Shift_JIS（`--encoding sjis`）です。詳しくは [docs/encoding.ja.md](docs/encoding.ja.md) を参照してください。
- 10000 段を超えてネストした結果は表示できません。
- jq と同じく、日本語などのキーは `."名前"` のように引用符で囲みます。`.名前` と入力すると、エラーと一緒に正しい書き方を表示します。

キーの補完には、次の制約があります。

- 候補に出るのはオブジェクトのキーだけです。配列の添字（`[0]`、`[]`）は候補に出ません。
- 引用符の中で書きかけのキー（`."名` など）には、候補を出しません。`.` まで入力した時点で、引用符付きの候補が出ます。
- `if` を `end` まで、`def` を `;` まで書き終えていない間（たとえば `if . then .na`）は、候補を出しません。閉じたあとの部分（`if . then . end | .na`）では、候補が出ます。

## ライセンス

[MIT ライセンス](LICENSE)です。

jitto の実行ファイルには、Go と複数のオープンソースのライブラリが含まれています。それらのライセンスは、各リリースに添付する `THIRD_PARTY_NOTICES.txt` にまとめています。
