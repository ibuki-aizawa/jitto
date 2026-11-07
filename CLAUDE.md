# jitto

Windows 向けのインタラクティブ jq ツール。jq 式を入力すると、結果がリアルタイムに表示される。

## 方針

- Go で実装し、シングルバイナリで動作させる。外部の jq には依存しない。
- winget（portable 形式）で配布する。バイナリは GitHub Releases で公開する。
- Windows（Windows Terminal / PowerShell / cmd）での動作を最優先する。macOS / Linux でも動けば望ましい。

## 技術スタック

- jq エンジン: gojq（github.com/itchyny/gojq）をライブラリとして使用
- TUI: Bubble Tea（github.com/charmbracelet/bubbletea）、bubbles（textinput, viewport）、lipgloss

## MVP の機能

- ファイル引数（`jitto data.json`）または標準入力（`curl ... | jitto`）から JSON を読み込む
- 上部の入力欄に jq 式を入力すると、下部のペインに結果をリアルタイム表示する
- 式がエラーのときは、直前の正常な結果を残したままエラーメッセージを表示する
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

## 進め方

- 実装に入る前に、まず計画を提示して確認を取る。
- 変更は小さな単位で行い、ロジック部分（jq 評価など）にはテストを書く。
- 依存パッケージを追加するときは、理由を説明してから追加する。
