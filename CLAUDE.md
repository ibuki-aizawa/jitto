# jitto

Windows 向けのインタラクティブ jq ツール。jq 式を入力すると、結果がリアルタイムに表示される。

## 方針

- Go で実装し、シングルバイナリで動作させる。外部の jq には依存しない。
- winget（portable 形式）で配布する。バイナリは GitHub Releases で公開する。
- Windows（Windows Terminal / PowerShell / cmd）での動作を最優先する。macOS / Linux でも動けば望ましい。

## 技術スタック

- jq エンジン: gojq（github.com/itchyny/gojq）をライブラリとして使用
- TUI: Bubble Tea（github.com/charmbracelet/bubbletea）、bubbles（textinput, viewport）、lipgloss
- 文字コードの変換: golang.org/x/text（Shift_JIS）。UTF-16 は標準ライブラリで変換する

## MVP の機能

- ファイル引数（`jitto data.json`）または標準入力（`curl ... | jitto`）から JSON を読み込む
  - 入力は UTF-8。先頭に BOM があれば UTF-8 / UTF-16 を判別する。`--encoding sjis` で Shift_JIS（CP932）として読む（UTF-16 の BOM があれば BOM を優先。UTF-8 の BOM は PowerShell 5.1 が Shift_JIS にも付けるので、読み飛ばして Shift_JIS として読む）
  - 結果は常に UTF-8 で出力する
- 上部の入力欄に jq 式を入力すると、下部のペインに結果をリアルタイム表示する
- 式がエラーのときは、直前の正常な結果を残したままエラーメッセージを表示する
- カーソルの直前でキーを書きかけているとき（`.`、`.items[0].na`、`map(.na` など）は、その位置に流れてくる値のオブジェクトのキーを候補として表示する
  - 書きかけの部分で、大文字と小文字を区別せずに前方一致で絞り込む
  - 一覧は書きかけのキーの列に表示する（画面に収まらないときは左にずらす）
  - Ctrl+N / Ctrl+P は Vim の補完と同じく、候補を順に入力欄へ書き込み、結果もその式でプレビューする。書きかけていた元の値も巡回の 1 つに含める
  - Tab / Ctrl+Y で確定（選んでいなければ先頭の候補を入力）、Esc で元の値に戻して閉じる。↑↓ は結果のスクロールのまま
  - 識別子にならないキーは `."名前"` の形で入力する
- Enter で確定し、結果を stdout、式を stderr に出力して終了する（式がエラーの間は確定せず、エラーを表示したまま入力を続ける）
- Ctrl+C で何も出力せずに終了する

## 実装上の注意

- 標準入力から JSON を読んだ場合、キー入力は標準入力ではなくコンソールから読む必要がある。
  - Windows では `tea.WithInputTTY()` を使わない。Bubble Tea v1.3.10 は標準入力がパイプのとき、オプションなしでも内部で `CONIN$` を開き、終了時のクローズが読み取り待ちで止まる（`p.Run()` が次のキー入力まで返らない）。
  - 代わりに、パイプの JSON を読み終えたあとで `CONIN$` を開き、`SetStdHandle` と `os.Stdin` を差し替えてから Bubble Tea を起動する。
- 確定時の結果を stdout でパイプに流せるよう、TUI の描画は stderr に出す（`tea.WithOutput(os.Stderr)`）。
- stdout をリダイレクトすると lipgloss の標準レンダラーは色を無効にするので、`lipgloss.NewRenderer(os.Stderr)` を使う。
- 大きな JSON でも入力が引っかからないよう、評価はデバウンスする、または前回の評価をキャンセルできるようにする。

## 開発コマンド

- ビルド: `go build -o jitto.exe .`
- テスト: `go test ./...`
- E2E テスト（Windows のみ）: `go test -tags e2e ./e2e`（`-slow` を付けると、時間のかかるシナリオも実行する）
- 静的解析: `go vet ./...`
- フォーマット: `gofmt -w .`
- デモの GIF の作り直し: `vhs demo/demo.tape`（vhs、ttyd、ffmpeg、pwsh が必要）

## ドキュメント

- README.md は英語で、日本語版は README.ja.md に置く。どちらも同じ構成に保つ。
- 動作やキー操作を変えたら、README.md と README.ja.md の両方を更新する。
- 画面の見た目や補完の操作が変わったら、デモの GIF（demo/demo.gif）も作り直す。デモのデータ（demo/demo.json）は英語だけで書く。

## 進め方

- ユーザーとのやり取りは日本語で行う。
- 実装に入る前に、まず計画を提示して確認を取る。
- 変更は小さな単位で行い、ロジック部分（jq 評価など）にはテストを書く。
- 依存パッケージを追加するときは、理由を説明してから追加する。
