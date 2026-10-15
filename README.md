# LumaFrame (`lrtl`)

LRTimelapseと同じ作業を段階的に実現することを目指す、独立したオープンソースのタイムラプスツールです。現在はLightroom互換XMPのキーフレーム編集とHoly Grail露出補正に加え、現像済みJPEG/PNG/TIFF連番からMJPEG AVIを生成できます。

**現状は初期開発版です。RAW現像やCamera Raw設定全体の適用には対応していません。** `render --apply-xmp-exposure`で現像済みJPEG/PNG/TIFFに`Exposure2012`だけを近似適用できます。RAW連番を読み込んでキーフレームから現像し、完成動画を書き出す一連の処理はまだできません。

本プロジェクトはLRTimelapseおよびAdobeと無関係です。独自実装であり、それらのコード、画像、同梱ツールを含みません。

## 実装状況

下表はLRTimelapse 7の公式機能・ワークフローを調査し、lrtlの現行コードと照合したものです。「未実装」は今後の目標を含みます。LRTimelapseではライセンスや外部のLightroom/Camera Raw構成により利用可能な機能が異なります。

| 機能領域 | LRTimelapseの機能 | lrtlの状況 | 状態 |
|---|---|---|---|
| シーケンス規模 | 評価版は400枚まで、Private/Proはサイズ制限なし | 画像列の枚数上限なし。出力AVIは約4 GBまで | 一部 |
| ワークフロー | Basic、Visual、Long-Term、Lightroom/ACR連携、内部編集/Export | CLIで個別機能を実行。ワークフロー状態や内蔵現像画面なし | 一部 |
| キーフレーム | 明るさを解析する自動Keyframes Wizard、手動キーフレーム | 手動マークに加え、JPEG/PNG/TIFFの明るさ曲線から内側の候補を選ぶ`keyframes`コマンドを実装。LRT互換の2*/3* HGマーカーも`scan`で表示し、通常の4*/5*マークとの重複を防ぐ。候補選定は独自ヒューリスティック | 一部 |
| Holy Grail | Mモードで変化させた露出やISOを補正して昼夜遷移をつなぐ | JPEG/TIFF/CR2/CR3/DNG/RW2のEXIF露出設定から補正を計算。EXIFがManualを示す場合に2*/3*ペアを提案・記録。`--stretch`と`--rotate`でLRTの曲線変形を近似。LRT Wizardの画像計測、探索、補正レイヤーは未再現。補正を`crs:Exposure2012`へ書く方式 | 一部 |
| 構図・クロップ | Composition作成、キーフレーム間のクロップアニメーション | 5つ星のcrop keyframeマークのみ。構図作成とクロップ値の補間は未実装 | 一部 |
| レンズ情報 | マニュアルレンズのLens Tagger | 未実装 | 未実装 |
| XMP・現像値 | Lightroom/Camera Raw等とのXMP連携、現像パラメーターの遷移 | 外部XMPを読み書きし未知のRDF値を保持。明示的に許可した数値のグローバル設定を線形補間。レンダー時に`Exposure2012`のみ任意適用。他のCamera Raw値、RAW現像、ローカルマスクは未対応 | 一部 |
| Auto-Transition | キーフレーム間を自動補間 | 数値のグローバルCamera Raw項目を補間。ローカル補正、カーブ、マスク、色相環などの補間なし | 一部 |
| 視覚プレビュー | 露出や遷移を画像・グラフで確認 | 未実装 | 未実装 |
| Deflicker | 参照領域と平滑化曲線に基づく視覚補正、複数パス | `render --deflicker`がJPEG/PNG/TIFFを解析し、任意の参照矩形内の中央値から全画面への露出補正を計算。平滑化幅・パス数・一定輝度モードを指定可能。独自の近似方式で、LRTのVisual PreviewやXMPへのロスレス記録は未対応 | 一部 |
| 長期撮影補正 | 明るさ・コントラスト・色相の変化を検出して外れ画像を除外 | 未実装 | 未実装 |
| 動画生成 | 現像済みフレームを動画にレンダー | JPEG/PNG/TIFFをMJPEG AVIに変換。固定サイズの画像列、整数1–240 fps。JPEGは画素処理なしなら入力データを格納し、画素処理時とPNG/TIFFはJPEGに変換 | 一部 |
| RAW・DNG | RAW処理、DNG変換 | 未実装。JPEG/PNG/TIFF以外をrenderに渡すとエラー | 未実装 |
| 解像度・中間画像 | 720p〜8K/Originalなどライセンス別解像度、Pro向け非圧縮TIFF中間画像 | 入力と同じ解像度のみ。中間画像の書き出し工程なし | 一部 |
| コーデック・色・HDR | H.264/MP4、Pro向けH.265/ProRes/DNxHR、HDRワークフロー | MJPEG AVIのみ。Rec.2020、HDR、音声なし | 一部 |
| オーバーレイ・字幕 | 透かし、撮影時刻表示、SRT字幕出力 | 未実装 | 未実装 |
| Motion Blur Plus | 書き出し動画への低/中/高モーションブラー | 未実装 | 未実装 |
| 取り込み | メモリーカードからのコピー、プレビュー、リネーム | 未実装 | 未実装 |
| DNG変換 | 取り込み時のロスレス/ロッシーDNG変換（Pro） | 未実装 | 未実装 |
| バッチ・並列処理 | バッチプレビュー/補正、マルチスレッド処理 | 未実装 | 未実装 |
| GUI | シーケンス一覧、カーブ表示、プレビュー編集 | CLIのみ | 未実装 |
| ライセンス制限 | 評価版/Private/Proで枚数、コーデック、解像度、商用利用条件が異なる | MIT OSS。機能別ライセンスゲートなし | 対象外 |

### 今できること

1. `.xmp`サイドカーをスキャンし、キーフレームと数値設定を確認する。
2. JPEG/PNG/TIFF連番の明るさ曲線からキーフレーム候補を表示し、任意でXMPにマークする。
3. キーフレームを手動で設定し、許可済みの数値設定をプレビューまたはXMPに書き込む。
4. 現像済みJPEG/PNG/TIFF連番をMJPEG AVIにまとめる。
5. レンダー時に、参照領域と平滑化曲線を使った簡易Deflickerを適用する。
6. 対応画像のEXIF露出時間・ISO・絞り値からHoly Grail補正を算出し、Camera RawのXMP露出値として出力する。

動画生成の最短経路は、Lightroom等でJPEG/PNG/TIFFを書き出してから`lrtl render`を実行する方法です。既定ではXMPを読まず、画像の現像もしません。`--apply-xmp-exposure`を指定すると、サイドカーの`crs:Exposure2012`を線形sRGB上で近似適用し、Deflickerもその後の明るさに対して解析します。ほかの現像設定やRAWデータには対応しません。DeflickerとXMP露出補正は出力画素に適用するため、元画像やXMPは変更しません。全フレームの幅と高さは同じである必要があります。AVIコンテナのサイズには約4 GBの上限があります。

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

# EXIFの撮影露出設定を逆補正し、XMPへの変更案を表示
lrtl holy-grail "D:\Timelapse\Sequence"
lrtl holy-grail "D:\Timelapse\Sequence" --write

# 補正曲線の伸縮・回転を調整
lrtl holy-grail "D:\Timelapse\Sequence" --stretch 0.4 --rotate -0.2

# 絞りが固定で、EXIFのFNumberが欠落している素材を処理
lrtl holy-grail "D:\Timelapse\Sequence" --ignore-aperture

# 露出設定変更の2*/3*マーカーを付けずに補正をプレビュー
lrtl holy-grail "D:\Timelapse\Sequence" --no-mark-jumps

# EXIFに撮影モードが記録されていない素材で2*/3*マーカーを強制
lrtl holy-grail "D:\Timelapse\Sequence" --mark-jumps

# lrtlが最後に書き込んだHoly Grail補正と2*/3*マーカーを取り消す
lrtl holy-grail "D:\Timelapse\Sequence" --reset --write

# JPEG/PNG/TIFFの輝度曲線から5つの候補を提案（既定はプレビューのみ）
lrtl keyframes "D:\Timelapse\Developed" --count 5
lrtl keyframes "D:\Timelapse\Developed" --count 5 --write

# 現像済みJPEG/PNG/TIFFから24 fpsのMJPEG AVIを作成
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.avi"

# XMPサイドカーのExposure2012を適用してレンダー
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\adjusted.avi" --apply-xmp-exposure

# FPSと変換時のJPEG品質を指定
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.avi" --fps 30 --quality 95

# 指定領域で明るさを測り、近傍6フレームの変動を3パスで抑える
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\smooth.avi" `
  --deflicker --reference 200,100,600,300 --smooth 6 --passes 3

# 露出変化を残さず、一定の明るさへ寄せる（定常光のシーン向け）
lrtl render "D:\Timelapse\Studio" --output "D:\Timelapse\constant.avi" --constant
```

`render`は出力ファイルが既にある場合は停止します。置き換えるときは`--overwrite`を指定します。DeflickerなしのJPEG入力は再圧縮せずAVIに入れます。PNG/TIFF入力とDeflickerを使う入力は指定品質のJPEGに変換します。TIFFの16-bit階調は8-bitのMJPEG出力時に失われます。`--reference`の形式は`x,y,width,height`で、指定しない場合は画面全体を測定します。`--smooth`は平滑化の半径（フレーム数）、`--passes`は補正反復数です。動く被写体や雲など輝度が変わる要素を避けた領域を指定してください。`--constant`は長期的な明るさの変化も抑えるため、照明条件が一定のシーンに向いています。

`--apply-xmp-exposure`は各画像に対応する外部XMPサイドカーから数値の`crs:Exposure2012`を読み、線形sRGB画素に露出倍率を適用します。±32 stopsを超える値や不正な数値はエラーにします。Camera Rawの全現像パイプラインと一致する機能ではなく、露出スライダーだけの近似適用です。JPEGはこの指定で再エンコードされます。

`keyframes`は全フレームの明るさを解析し、内側の候補数を指定できます。両端の画像は曲線を測る基準点として使い、キーフレームにはしません。既定は5候補で、内側のフレームが5未満ならその数まで減らします。明るさ曲線を平滑化したうえで、線形遷移から外れる度合いが大きいフレームを優先します。既存の手動キーフレームを自動で解除しません。`--write`を付けた場合も候補だけをマークし、ほかの評価やマーカーは保持します。入力はJPEG/PNG/TIFFに対応します。

`unmark --force-rating`はlrtl以外が設定した4つ星/5つ星評価にも使えます。その評価を削除するため、評価がキーフレームマーカーとして使われている場合に限ってください。

`holy-grail`は各フレームのEXIFから露出設定値`log2(露出時間 × ISO / F値²)`を求め、先頭フレームとの差を打ち消す補正量として`crs:Exposure2012`に加えます。対応形式はJPEG、TIFF、CR2、CR3、DNG、RW2です。全フレームに有効な露出時間とISOが必要です。F値がない場合は`--ignore-aperture`を指定し、絞りが変化しなかったことを確認してください。`--stretch`はEXIF露出値を最小・最大で正規化した曲線のスケール（-1.5〜+1.5）、`--rotate`はシーケンスに加える直線傾き（-4〜+4）です。LRTの曲線変形を近似し、指定しない場合は従来のEXIF露出差補正を使います。LRTの画像輝度を使った平滑化やWizardの自動最適化は行いません。EXIFが撮影モードをManualと報告する場合、設定変更位置に2*/3*評価のペアを提案します。撮影モードが不明または自動の場合はペアを作らず、`--mark-jumps`で強制できます。設定変更が連続して隣接ペアを作れない場合や、既存評価と衝突する場合は書き込み前にエラーにします。`--no-mark-jumps`でペア付けを無効化できます。補正の再実行では前回分を置き換え、lrtl管理下の元評価も記録するため`--reset --write`で復元できます。既定はプレビューで、書き込み時は既存サイドカーをバックアップします。

LRTimelapse側では2*/3*が内部HGキーフレームで、4*/5*の編集キーフレームとは別レイヤーとして使われます。lrtlはこの評価マーカーを互換目的で扱いますが、補正値は現時点で通常の`crs:Exposure2012`に書き込むため、LRTの独立した背景補正レイヤーと同じ動作ではありません。LRTのWizardにはRotate（-4〜+4）、Stretch（-1.5〜+1.5）、`holyGrailReduceDeflicker`を使う調整・探索があります。lrtlの`--stretch`と`--rotate`は、EXIF露出曲線を最小値・最大値で正規化して伸縮し、フレーム番号に沿った直線傾きを加えます。LRT Wizardの画像計測、曲線平滑化、最適値探索、Lightroom編集値との合成は未実装です。逆解析で確認した式と探索範囲は[リバースエンジニアリングノート](docs/reverse-engineering.md)に記録しています。

この計算は記録されたカメラ設定による露出差を補う近似です。LRTimelapse公式ワークフローのHoly Grail Wizardも撮影中に変更したシャッター速度/ISOの補正曲線を作ります。lrtlの曲線回転・伸縮はEXIFから作った曲線への追加調整で、LRTの画像プレビューやWizardによる最適化を再現しません。センサー感度の誤差、レンズ透過率、自然な明るさの推移も解析しません。RAW現像やXMPの画素反映をする機能ではありません。

## 今後の実装順

| 段階 | 目標 | 状況 |
|---|---|---|
| 1 | XMPサイドカー、キーフレーム、数値設定の補間 | 手動設定、輝度ベース候補提案、限定的な数値補間を実装 |
| 2 | 画像連番から最初の動画を生成 | JPEG/PNG/TIFFからMJPEG AVIに対応 |
| 3 | 画像解析、プレビュー、Deflicker | レンダー時の単一参照領域・全体露出補正を実装。LRT形式のVisual Preview、曲線UI、XMPロスレス適用、マスク別補正は未実装 |
| 4 | RAW現像、Holy Grail、XMP現像値の画素への反映 | EXIF設定ベースのHoly Grail露出補正と、現像済みラスターへのExposure2012近似適用を実装。RAW現像とCamera Raw現像の完全再現は未着手 |
| 5 | H.264等の追加出力、クロップ/オーバーレイ、取り込み、GUI | 未着手 |

## 機能調査の出典

公式ページの機能・ワークフローを参考に、同等機能の範囲を整理しました。

- [LRTimelapse 7の機能一覧](https://lrtimelapse.com/features/)
- [Visual Workflow](https://lrtimelapse.com/workflow/visual-workflow/)
- [Holy Grailの2*/3*内部キーフレームに関するLRT開発者の説明](https://forum.lrtimelapse.com/Thread-handling-different-exposure-times-with-keyframes)
- [Lightroomを使わない内部ワークフロー](https://lrtimelapse.com/workflow/internal-workflow/)
- [Export and Render](https://lrtimelapse.com/workflow/export-and-render/)
- [Long Term / Construction](https://lrtimelapse.com/workflow/long-term-construction/)
- [LRTimelapse Import](https://lrtimelapse.com/workflow/lrtimport/)
- [LRTimelapse 7.2 release notes: Simple Deflicker](https://lrtimelapse.com/updates/lrtimelapse-7-2/)
- [AVI/MJPEG writer for Go](https://github.com/icza/mjpeg)
- [Pure-Go TIFF decoder/encoder used by lrtl](https://pkg.go.dev/golang.org/x/image@v0.29.0/tiff)
- [Pure-Go EXIF reader used for Holy Grail exposure analysis](https://github.com/evanoberholster/imagemeta)

## 開発

```powershell
go test ./...
go vet ./...
go build -o lrtl.exe ./cmd/lrtl
```

互換性の観察と既知の制約は[リバースエンジニアリングノート](docs/reverse-engineering.md)を参照してください。

## ライセンス

MIT。詳細は[LICENSE](LICENSE)を参照してください。
