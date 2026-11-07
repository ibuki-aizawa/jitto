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
- Enter で確定し、結果を stdout、式を stderr に出力して終了する
- Ctrl+C で何も出力せずに終了する

## 実装上の注意

- 標準入力から JSON を読んだ場合、キー入力は標準入力ではなくコンソールから読む必要がある（Bubble Tea の `tea.WithInputTTY()` を検討）。
- 確定時の結果を stdout でパイプに流せるよう、TUI の描画は stderr に出す（`tea.WithOutput(os.Stderr)` を検討）。
- 大きな JSON でも入力が引っかからないよう、評価はデバウンスする、または前回の評価をキャンセルできるようにする。

## 開発コマンド

- ビルド: `go build -o jitto.exe .`
- テスト: `go test ./...`
- 静的解析: `go vet ./...`
- フォーマット: `gofmt -w .`

## 進め方

- 実装に入る前に、まず計画を提示して確認を取る。
- 変更は小さな単位で行い、ロジック部分（jq 評価など）にはテストを書く。
- 依存パッケージを追加するときは、理由を説明してから追加する。
