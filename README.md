# LumaFrame (`lrtl`)

LRTimelapseと同じ作業を段階的に実現することを目指す、独立したオープンソースのタイムラプスツールです。現在はLightroom互換XMPのキーフレーム編集に加え、現像済みJPEG/PNG連番からMJPEG AVIを生成できます。

**現状は初期開発版です。RAW現像やXMPに保存されたCamera Raw設定の適用には対応していません。** RAW連番を読み込んでキーフレームから現像し、完成動画を書き出す一連の処理はまだできません。最初の動画生成経路として、Lightroomなどで現像・書き出し済みのJPEG/PNGをAVIにまとめます。

本プロジェクトはLRTimelapseおよびAdobeと無関係です。独自実装であり、それらのコード、画像、同梱ツールを含みません。

## 実装状況

下表はLRTimelapse 7の公式機能・ワークフローを調査し、lrtlの現行コードと照合したものです。「未実装」は今後の目標を含みます。LRTimelapseではライセンスや外部のLightroom/Camera Raw構成により利用可能な機能が異なります。

| 機能領域 | LRTimelapseの機能 | lrtlの状況 | 状態 |
|---|---|---|---|
| シーケンス | 画像列の読み込み、撮影順の扱い、プレビュー | 対応拡張子の画像を1フォルダーから列挙し、ファイル名を自然順で並べる。画像プレビューなし | 一部 |
| キーフレーム | 明るさを解析する自動Keyframes Wizard、手動キーフレーム | XMPの4つ星/5つ星マーカーを手動設定・解除。自動選定なし | 一部 |
| Holy Grail | 露出変化やISOを補正して昼夜遷移をつなぐ | 未実装 | 未実装 |
| XMP・現像値 | Lightroom/Camera Raw等とのXMP連携、現像パラメーターの遷移 | 外部XMPを読み書きし未知のRDF値を保持。明示的に許可した数値のグローバル設定を線形補間。XMP値を画素に適用する現像機能なし | 一部 |
| Auto-Transition | キーフレーム間を自動補間 | 数値のグローバルCamera Raw項目を補間。ローカル補正、カーブ、マスク、色相環などの補間なし | 一部 |
| 視覚プレビュー | 露出や遷移を画像・グラフで確認 | 未実装 | 未実装 |
| Deflicker | 視覚/複数パスのちらつき補正、領域選択 | 未実装 | 未実装 |
| 長期撮影補正 | 明るさ・コントラスト・色相の変化を検出して外れ画像を除外 | 未実装 | 未実装 |
| 動画生成 | 現像済みフレームを動画にレンダー | JPEG/PNGをMJPEG AVIに変換。固定サイズの画像列、整数1–240 fps。PNGはJPEGに変換し、JPEGは入力データをそのまま格納 | 一部 |
| RAW・DNG | RAW処理、DNG変換 | 未実装。JPEG/PNG以外をrenderに渡すとエラー | 未実装 |
| 書き出し形式 | ライセンスに応じた解像度、H.264/MP4、H.265、ProRes、DNxHR、HDR、TIFF中間画像 | MJPEG AVIのみ。音声、HDR、リサイズ、クロップ、モーションブラーなし | 一部 |
| オーバーレイ | 透かし、撮影時刻、字幕/SRT | 未実装 | 未実装 |
| 取り込み | カードからのコピー、プレビュー、リネーム、DNG変換 | 未実装 | 未実装 |
| バッチ・並列処理 | バッチプレビュー/補正、マルチスレッド処理 | 未実装 | 未実装 |
| UI・構図・レンズ情報 | GUI、構図、レンズタグ付け等 | CLIのみ | 未実装 |

### 今できること

1. `.xmp`サイドカーをスキャンし、キーフレームと数値設定を確認する。
2. キーフレームを手動で設定し、許可済みの数値設定をプレビューまたはXMPに書き込む。
3. 現像済みJPEG/PNG連番をMJPEG AVIにまとめる。

動画生成の最短経路は、Lightroom等で画像を書き出してから`lrtl render`を実行する方法です。`lrtl render`はXMPを読まず、画像の現像もしません。全フレームの幅と高さは同じである必要があります。AVIコンテナのサイズには約4 GBの上限があります。

## インストール

ビルドにはGo 1.23以降が必要です。依存ライブラリはPure Goで、実行時にFFmpegやネイティブDLLは必要ありません。

```powershell
go install github.com/a-kaibu/lrtl/cmd/lrtl@latest
```

またはソースからビルドします。

```powershell
go build -o lrtl.exe ./cmd/lrtl
```

## 使い方

```powershell
# 画像列、サイドカー、キーフレームを確認
lrtl scan "D:\Timelapse\Sequence"

# キーフレームの設定・解除（既存XMPは既定でバックアップ）
lrtl mark "D:\Timelapse\Sequence\IMG_0005.CR3" --kind default
lrtl mark "D:\Timelapse\Sequence\IMG_0012.CR3" --kind crop
lrtl unmark "D:\Timelapse\Sequence\IMG_0005.CR3"

# キーフレーム間の数値設定をプレビューし、明示して書き込む
lrtl interpolate "D:\Timelapse\Sequence"
lrtl interpolate "D:\Timelapse\Sequence" --write

# 現像済みJPEG/PNGから24 fpsのMJPEG AVIを作成
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.avi"

# FPSとPNG入力のJPEG品質を指定
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.avi" --fps 30 --quality 95
```

`render`は出力ファイルが既にある場合は停止します。置き換えるときは`--overwrite`を指定します。JPEG入力は再圧縮せずAVIに入れます。PNG入力は指定品質のJPEGに変換します。

`unmark --force-rating`はlrtl以外が設定した4つ星/5つ星評価にも使えます。その評価を削除するため、評価がキーフレームマーカーとして使われている場合に限ってください。

## 今後の実装順

| 段階 | 目標 | 状況 |
|---|---|---|
| 1 | XMPサイドカー、キーフレーム、数値設定の補間 | 基本実装済み。補間対象は限定的 |
| 2 | 画像連番から最初の動画を生成 | JPEG/PNGからMJPEG AVIに対応 |
| 3 | 画像解析、プレビュー、Deflicker | 未着手 |
| 4 | RAW現像、Holy Grail、XMP現像値の画素への反映 | 未着手 |
| 5 | H.264等の追加出力、クロップ/オーバーレイ、取り込み、GUI | 未着手 |

## 機能調査の出典

公式ページの機能・ワークフローを参考に、同等機能の範囲を整理しました。

- [LRTimelapse 7の機能一覧](https://lrtimelapse.com/features/)
- [Visual Workflow](https://lrtimelapse.com/workflow/visual-workflow/)
- [Lightroomを使わない内部ワークフロー](https://lrtimelapse.com/workflow/internal-workflow/)
- [Export and Render](https://lrtimelapse.com/workflow/export-and-render/)
- [Long Term / Construction](https://lrtimelapse.com/workflow/long-term-construction/)
- [LRTimelapse Import](https://lrtimelapse.com/workflow/lrtimport/)
- [AVI/MJPEG writer for Go](https://github.com/icza/mjpeg)

## 開発

```powershell
go test ./...
go vet ./...
go build -o lrtl.exe ./cmd/lrtl
```

互換性の観察と既知の制約は[リバースエンジニアリングノート](docs/reverse-engineering.md)を参照してください。

## ライセンス

MIT。詳細は[LICENSE](LICENSE)を参照してください。
