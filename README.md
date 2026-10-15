# LumaFrame (`lrtl`)

LRTimelapseと同じ作業を段階的に実現することを目指す、独立したオープンソースのタイムラプスツールです。Lightroom互換XMPのキーフレーム編集やHoly Grail露出補正に加え、現像済みJPEG/PNG/TIFFとRAW連番からMJPEG AVI、H.264/H.265 MP4、ProRes MOVを生成できます。

**現像済み画像は追加ソフトなしでレンダーできます。RAWをセンサー情報から現像するには、外部のDNGLabが必要です。** `render --raw-develop --dnglab PATH`はDNGLabの16-bit sRGB TIFF出力を動画にします。`--raw-preview`はカメラ内蔵プレビューを使う旧来の軽量経路です。DNGLabでRAW画像を現像して動画にできますが、Camera Raw/Lightroomの全設定やXMP現像値を再現する機能ではありません。

本プロジェクトはLRTimelapseおよびAdobeと無関係です。独自実装であり、それらのコード、画像、同梱ツールを含みません。

## 実装状況

下表はローカルにインストールされたLRTimelapse 7.5.5とlrtlの現行コードを照合したものです。LRT側の機能とライセンス別の提供範囲は、公式のLRTimelapse 7機能表を2026-10-15に確認しました。「未実装」は今後の目標です。LRTimelapseのLightroom/Camera Raw連携は外部ソフトを使うワークフローです。

| 機能領域 | LRTimelapseの機能 | lrtlの状況 | 状態 |
|---|---|---|---|
| シーケンス規模 | 評価版は400枚まで、Private/Proはサイズ制限なし | 画像列の枚数上限なし。AVIは約4 GBまで。MP4はFFmpeg対応 | 一部 |
| ワークフロー | Basic、Visual、Long-Term、Lightroom/ACR連携、内部編集/Export | CLIで個別機能を実行。ワークフロー状態や内蔵現像画面なし | 一部 |
| キーフレーム | 輝度推移からキーフレーム数の目安を提示し、スライダーで調整するWizard。手動設定と等間隔配置も可能 | 手動マーク、JPEG/PNG/TIFF、`--raw-preview`の内蔵プレビュー、または`--raw-develop`で現像したRAWの輝度曲線から候補を選ぶ`keyframes`を実装。`--apply-xmp-exposure`と`--apply-xmp-crop`で候補解析へ現在のXMP設定を反映できる。デコード不要の`--evenly-spaced`も可能。LRT互換の2*/3* HGマーカーを`scan`で表示し、通常の4*/5*マークとの重複を防ぐ。候補選定は独自ヒューリスティック | 一部 |
| Holy Grail | Mモードで変化させた露出やISOを補正して昼夜遷移をつなぐ | JPEG/TIFF/CR2/CR3/DNG/RW2のEXIF露出設定から補正を計算。EXIFがManualを示す場合に2*/3*ペアを提案・記録。`--stretch`/`--rotate`とLRTの探索範囲・L1評価・LOWESS平滑化を使う`--auto-shape`に対応。RAW曲線の生成と独立補正レイヤーは未再現。補正を`crs:Exposure2012`へ書く方式 | 一部 |
| 構図・クロップ | Composition作成、キーフレーム間のクロップアニメーション | 5つ星crop markとCropAngle/Top/Left/Bottom/Rightの数値補間に対応。`render --apply-xmp-crop`で回転を含む矩形を画素へ適用し、元と同じ解像度に再サンプル。プレビュー上の構図作成・編集UIとCamera RawのGeometry補正は未対応 | 一部 |
| レンズ情報 | マニュアルレンズのLens Tagger | 未実装 | 未実装 |
| XMP・現像値 | Lightroom/Camera Raw等とのXMP連携、現像パラメーターの遷移 | 外部XMPを読み書きし未知のRDF値を保持。`set`で許可した131項目を編集し、数値設定をキーフレーム間で補間。色相角は最短回転で補間。レンダー時にExposure2012と回転を含むクロップ矩形を任意適用。他の現像値の画素反映、Camera Raw相当RAW現像、ローカルマスクは未対応 | 一部 |
| Auto-Transition | キーフレーム間を自動補間。Look/Preset/Tone Curve/PointColorsのXMP均等化も可能 | 131個の数値項目を補間。ポイントトーンカーブとLook/Preset/PointColorsは先頭フレームから後続へコピー。Tone Curveのコピー元はLRTの説明で確認済み。その他3項目のコピー元はlrtl側の互換判断。マスク内のカーブとローカル補正遷移は未対応 | 一部 |
| 視覚プレビュー | 露出や遷移を画像・グラフで確認 | `preview`で輝度曲線、XMP Exposure2012、キーフレームを静的SVGに表示。RAW内蔵プレビュー/DNGLab現像と、任意でXMP Exposure/cropを解析に適用。画像サムネイル、インタラクティブ操作、LRTの現像結果プレビューは未対応 | 一部 |
| Deflicker | 参照領域と平滑化曲線に基づく視覚補正、複数パス、Lightroomへ渡す調整値 | `render --deflicker`で画素に補正し、`deflicker --write`でJPEG/PNG/TIFFまたは`--raw-preview`／`--raw-develop`の輝度解析結果を`crs:Exposure2012`へ記録可能。`--apply-xmp-exposure`/`--apply-xmp-crop`で既存現像設定を解析に含められる。独自の近似方式。LRTのVisual Preview、Camera Rawとの数値一致、カーブUIは未対応 | 一部 |
| 長期撮影補正 | 明るさ・コントラスト・色相の変化を検出して外れ画像を除外 | 未実装 | 未実装 |
| 動画生成 | 現像済みフレームを動画にレンダー | JPEG/PNG/TIFFとRAW現像画像をMJPEG AVI、外部FFmpegによるH.264/H.265 MP4またはProRes 422/422 HQ MOVに出力。固定サイズの画像列、整数1–240 fps、`--long-edge`によるアスペクト比維持の縮小 | 一部 |
| RAW・DNG | RAW処理、DNG変換 | `--raw-develop`で外部DNGLabにより主要RAW形式を現像し、その16-bit sRGB TIFF出力からレンダー可能。`--raw-preview`によるカメラ内プレビュー経路も維持。DNGLabは別途インストールが必要。Camera Raw/Lightroom設定の完全な適用やDNG変換は未実装 | 一部 |
| 解像度・中間画像 | 720p〜8K/Originalなどライセンス別解像度、Pro向け非圧縮TIFF中間画像 | `--long-edge`で長辺の上限を指定して縮小可能。拡大や16:9等のプリセット、非圧縮TIFF中間画像は未対応 | 一部 |
| コーデック・色・HDR | H.264/MP4、Pro向けH.265/ProRes/DNxHR、HDRワークフロー | 純GoのMJPEG AVIとFFmpegのlibx264/libx265/prores_ks経由のH.264/H.265 MP4、ProRes 422/422 HQ MOV。Rec.2020、HDR、音声なし。入力画素はRGB 8-bitなのでProRes 10-bit出力も16-bit RAW精度を保持しない | 一部 |
| オーバーレイ・字幕 | 透かし、撮影時刻表示、SRT字幕出力 | `render --timestamp-overlay`でEXIF/XMPの撮影日時を各動画フレームへ焼き込み。表示形式を指定可能。任意テキストの透かしとSRTファイル出力は未実装 | 一部 |
| Motion Blur Plus | 書き出し動画への低/中/高モーションブラー | 未実装 | 未実装 |
| 取り込み | メモリーカードからのコピー、プレビュー、リネーム | 未実装 | 未実装 |
| DNG変換 | 取り込み時のロスレス/ロッシーDNG変換（Pro） | 未実装 | 未実装 |
| バッチ・並列処理 | バッチプレビュー/補正、マルチスレッド処理 | 未実装 | 未実装 |
| GUI | シーケンス一覧、カーブ表示、プレビュー編集 | CLIのみ | 未実装 |
| ライセンス制限 | 評価版/Private/Proで枚数、コーデック、解像度、商用利用条件が異なる | MIT OSS。機能別ライセンスゲートなし | 対象外 |

### LRTimelapse 7のライセンス別機能

公式機能表にある主な差分です。評価版は400枚までで、Private/Proはシーケンス枚数に上限がありません。

| 機能 | Evaluation | Private | Pro |
|---|---|---|---|
| Keyframes/Holy Grail/Composition/Lens Tagger | あり | あり | あり |
| Visual Previews/Visual Deflicker/Multipass | あり（400枚まで） | あり | あり |
| LRTImporter | あり（400枚まで） | あり | あり |
| H.264 720p/1080p | あり | あり | あり |
| 3K/4Kレンダー | なし | あり | あり |
| 5K/6K/8K/Originalレンダー | なし | なし | あり |
| 非圧縮TIFF中間画像 | なし | なし | あり |
| ProRes/H.265/DNxHR/True HDR | なし | なし | あり |
| 透かし/撮影時刻/SRT字幕 | なし | なし | あり |
| Motion Blur Plus | なし | なし | あり |
| 取り込み時のDNG変換 | なし | なし | あり |
| 4スレッド超の並列処理 | なし | あり | あり |
| プレビュー/Deflickerのバッチ操作 | なし | なし | あり |
| 1ユーザーあたり同時インストール | ― | 2台 | 2台 |

### 今できること

1. `.xmp`サイドカーをスキャンし、キーフレームと数値設定を確認する。
2. JPEG/PNG/TIFF連番、RAW内蔵プレビュー、またはDNGLabで現像したRAWの明るさ曲線からキーフレーム候補を表示し、任意でXMPにマークする。
3. キーフレームを手動で設定し、許可済みの数値設定をプレビューまたはXMPに書き込む。
4. 現像済みJPEG/PNG/TIFF、対応RAW内蔵プレビュー、またはDNGLabで現像したRAWをMJPEG AVI、H.264/H.265 MP4、ProRes MOVにまとめ、必要に応じて`--long-edge`で縮小する。
5. レンダー時に簡易Deflickerを画素へ適用するか、JPEG/PNG/TIFFまたは選択したRAW入力の解析結果をXMP露出値として保存する。
6. 対応画像のEXIF露出時間・ISO・絞り値からHoly Grail補正を算出し、Camera RawのXMP露出値として出力する。
7. 輝度曲線、XMP露出補正、キーフレーム位置をSVGプレビューで確認する。RAW解析時は内蔵プレビューまたはDNGLab現像を選び、必要に応じてXMP Exposure/cropを反映する。

動画生成の最短経路は、Lightroom等でJPEG/PNG/TIFFを書き出してから`lrtl render`を実行する方法です。RAW連番を直接レンダーするには外部DNGLabを使う`--raw-develop`を指定します。DNGLabのRAW対応はカメラ形式ごとに異なり、DNGLabはalpha版です。`--raw-preview`は内蔵プレビューを使う別経路です。`--apply-xmp-exposure`を指定すると、サイドカーの`crs:Exposure2012`をレンダー入力画像に近似適用します。DNGLabの現像パラメーターはCamera Rawと一致せず、サイドカーにある他の現像値もRAW現像には使いません。Deflickerはレンダー時に出力画素へ適用できます。これらのレンダー操作は元画像やXMPを変更しません。別コマンドの`lrtl deflicker --write`は、解析した補正をXMPに保存します。全フレームの幅と高さは同じである必要があります。MJPEG AVIには約4 GBのサイズ上限があります。MP4/MOV出力は外部FFmpegを必要とし、入力画像をGoで8-bit RGBへデコードしてから選択したエンコーダーへ渡します。`.mp4`の既定はH.264、`.mov`の既定はProRes 422です。`--codec h265`でH.265、`--codec prores-hq`でProRes 422 HQを選べます。FFmpegにそれぞれ`libx264`、`libx265`、`prores_ks`エンコーダーが必要です。

`preview`は画像列の線形sRGB輝度を測り、最初のフレームからのEV差、XMPの`Exposure2012`差、キーフレーム評価をSVGグラフに出します。JPEG/PNG/TIFFのほか、`--raw-preview`で対応RAW内のプレビュー、または`--raw-develop`でDNGLab現像画像を解析できます。`--apply-xmp-exposure`と`--apply-xmp-crop`を指定すると、輝度解析にその値を適用します。これは静的な数値グラフで、Lightroom/Camera Rawの現像画像やLRTimelapseの操作可能なVisual Previewは表示しません。

解析コマンドの`--apply-xmp-exposure`と`--apply-xmp-crop`はlrtl側の画素近似です。既にLightroom等でその補正を焼き込んだJPEG/PNGでは二重適用になるため、主にRAWプレビューやDNGLab現像画像の解析に使ってください。

## インストール

ビルドにはGo 1.23以降が必要です。Go側の依存ライブラリはPure Goです。MJPEG AVI出力にはFFmpegやネイティブDLLは不要です。H.264/H.265 MP4とProRes MOV出力には、対応エンコーダーを含む外部FFmpegが必要です（`--ffmpeg`で実行ファイルを指定できます）。RAWの現像には外部DNGLabが必要です（PATHに置くか`--dnglab`で指定）。FFmpegとDNGLabはlrtlに同梱しません。

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
lrtl mark "D:\Timelapse\Developed\IMG_0005.jpg" --kind default
lrtl mark "D:\Timelapse\Developed\IMG_0012.jpg" --kind crop
lrtl unmark "D:\Timelapse\Developed\IMG_0005.jpg"

# 数値のCamera Raw設定をキーフレームのXMPに書き込む（既存XMPは既定でバックアップ）
lrtl set "D:\Timelapse\Developed\IMG_0005.jpg" crs:Exposure2012 -1.0
lrtl set "D:\Timelapse\Developed\IMG_0012.jpg" crs:Exposure2012 0.5

# キーフレーム間の数値設定をプレビューし、明示して書き込む
lrtl interpolate "D:\Timelapse\Developed"
lrtl interpolate "D:\Timelapse\Developed" --write

# EXIFの撮影露出設定を逆補正し、XMPへの変更案を表示
lrtl holy-grail "D:\Timelapse\Sequence"
lrtl holy-grail "D:\Timelapse\Sequence" --write

# 補正曲線の伸縮・回転を調整
lrtl holy-grail "D:\Timelapse\Sequence" --stretch 0.4 --rotate -0.2

# LRTの探索範囲とL1評価を使った曲線パラメーター提案
lrtl holy-grail "D:\Timelapse\Sequence" --auto-shape

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
lrtl keyframes "D:\Timelapse\RAW" --count 5 --evenly-spaced --write

# 対応RAWの埋め込みプレビューの輝度曲線から候補を選ぶ
lrtl keyframes "D:\Timelapse\RAW" --raw-preview --count 5

# DNGLabでRAWを現像し、その輝度曲線から候補を選ぶ
lrtl keyframes "D:\Timelapse\RAW" --raw-develop --dnglab "C:\Tools\dnglab.exe" --apply-xmp-exposure --apply-xmp-crop --count 5

# 現像済みJPEG/PNG/TIFFから24 fpsのMJPEG AVIを作成（FFmpeg不要）
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.avi"

# 外部FFmpeg/libx264で24 fpsのH.264 MP4を作成
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.mp4" --fps 24 --crf 18 --preset medium

# H.265 MP4またはProRes 422 / ProRes 422 HQ MOVを書き出す
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview-h265.mp4" --codec h265 --crf 20 --preset medium
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\master.mov" --codec prores
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\master-hq.mov" --codec prores-hq

# 4K相当の長辺3840px以下へ縮小してレンダー（アスペクト比維持、拡大なし）
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview-4k.mp4" --long-edge 3840

# 輝度・XMP露出・キーフレームをSVGグラフで確認
lrtl preview "D:\Timelapse\Developed" --output "D:\Timelapse\brightness.svg"

# 対応RAWの埋め込みプレビューからMJPEG AVIを作成（RAW現像ではない）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-preview.avi" --raw-preview

# 対応RAWの埋め込みプレビューからH.264 MP4を作成（FFmpegが必要、RAW現像ではない）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-preview.mp4" --raw-preview

# DNGLabでRAWを現像してH.264 MP4を作成（FFmpegとDNGLabが必要）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-developed.mp4" --raw-develop --dnglab "C:\Tools\dnglab.exe"

# DNGLabでRAWを現像してMJPEG AVIを作成（DNGLabが必要、FFmpeg不要）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-developed.avi" --raw-develop --dnglab "C:\Tools\dnglab.exe"

# DNGLab現像画像から輝度グラフと簡易Deflicker補正を作成
lrtl preview "D:\Timelapse\RAW" --output "D:\Timelapse\raw-brightness.svg" --raw-develop --dnglab "C:\Tools\dnglab.exe" --apply-xmp-exposure --apply-xmp-crop
lrtl deflicker "D:\Timelapse\RAW" --raw-develop --dnglab "C:\Tools\dnglab.exe" --apply-xmp-exposure --apply-xmp-crop

# XMPサイドカーのExposure2012を適用してレンダー
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\adjusted.avi" --apply-xmp-exposure

# XMPサイドカーの回転を含むクロップアニメーションを適用
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\cropped.avi" --apply-xmp-crop

# EXIF/XMP撮影日時を各フレーム右下へ重ねて動画に焼き込む
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\dated.mp4" --timestamp-overlay

# FPSと変換時のJPEG品質を指定
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.avi" --fps 30 --quality 95

# 指定領域で明るさを測り、近傍6フレームの変動を3パスで抑える
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\smooth.avi" `
  --deflicker --reference 200,100,600,300 --smooth 6 --passes 3

# 同じ解析を露出補正値としてXMPへプレビューし、書き込む
lrtl deflicker "D:\Timelapse\Developed" --reference 200,100,600,300 --smooth 6 --passes 3
lrtl deflicker "D:\Timelapse\Developed" --reference 200,100,600,300 --smooth 6 --passes 3 --write

# lrtlが記録したDeflicker補正だけをXMPから戻す
lrtl deflicker "D:\Timelapse\Developed" --reset
lrtl deflicker "D:\Timelapse\Developed" --reset --write

# 露出変化を残さず、一定の明るさへ寄せる（定常光のシーン向け）
lrtl render "D:\Timelapse\Studio" --output "D:\Timelapse\constant.avi" --constant
```

`render`は出力ファイルが既にある場合は停止します。置き換えるときは`--overwrite`を指定します。`--long-edge N`は元の長辺がNより大きいときだけ縮小し、縦横比を保って長辺をNピクセル以下にします。入力より大きくはしません。AVIでは画素処理なしのJPEG入力を再圧縮せず格納し、それ以外の入力は指定品質のJPEGに変換します。TIFFの16-bit階調は8-bit出力時に失われます。MP4ではJPEG/PNG/TIFFをGoでデコードし、H.264エンコーダーにRGBフレームとして渡すため中間JPEG圧縮をしません。`--crf`はH.264品質（0–51、低いほど高品質）、`--preset`は速度と圧縮率の選択です。MP4の画素は8-bit YUV 4:2:0に変換され、奇数幅/高さは右端/下端をパディングします。`--reference`の形式は`x,y,width,height`で、指定しない場合は画面全体を測定します。`--smooth`は平滑化の半径（フレーム数）、`--passes`は補正反復数です。動く被写体や雲など輝度が変わる要素を避けた領域を指定してください。`--constant`は長期的な明るさの変化も抑えるため、照明条件が一定のシーンに向いています。

`--apply-xmp-exposure`は各画像に対応する外部XMPサイドカーから数値の`crs:Exposure2012`を読み、線形sRGB画素に露出倍率を適用します。±32 stopsを超える値や不正な数値はエラーにします。Camera Rawの全現像パイプラインと一致する機能ではなく、露出スライダーだけの近似適用です。JPEGはこの指定で再エンコードされます。

`--apply-xmp-crop`は各サイドカーの`crs:HasCrop`、`CropTop/Left/Bottom/Right`、`CropAngle`を読み、回転した矩形クロップを適用して入力と同じ解像度に再サンプルします。クロップのないフレームはそのままです。空または範囲外の矩形はエラーにします。Camera RawのGeometry/遠近補正は再現しません。Lightroom等でキーフレームのクロップ値を保存し、`interpolate --write`で間を埋めてからこのオプションでレンダーできます。

`--timestamp-overlay`は、各サイドカーの`exif:DateTimeOriginal`、`xmp:CreateDate`、`xmp:ModifyDate`の順で日時を探し、なければ画像内EXIFの撮影/作成/更新日時を使って右下に焼き込みます。全フレームの日時が必要です。指定がない場合は`2006-01-02 15:04:05`形式を使います。形式を変えるには`--timestamp-format`へGoの日時レイアウトを渡してください。埋め込みGoフォントを使うため、日時表示は外部フォント不要です。画像ごとの透かしテキストやSRT字幕ファイルは未実装です。

`deflicker`はJPEG/PNG/TIFF、`--raw-preview`の内蔵画像、または`--raw-develop`でDNGLabが現像した画像の輝度を線形sRGBへ変換して測定し、指定領域の中央値に平滑化または一定輝度の目標曲線を合わせます。`--apply-xmp-exposure`と`--apply-xmp-crop`を指定すると、解析前にサイドカーの値を画素へ近似適用します。既定はプレビューだけです。`--write`は求めた補正量を各フレームの`crs:Exposure2012`へ加算し、前回lrtlが加えた量を`ol:DeflickerCorrection2012`に記録します。再実行ではlrtl管理分を置き換え、`--reset --write`ではその分だけを戻します。Holy Grail補正やユーザーが後から加えたExposure値は差し引かずに残します。Camera Rawの現像パイプラインやLRTimelapseのVisual Deflickerと数値が一致する機能ではなく、画像列から露出差を推定する近似です。

`set`は`NumericCRSProperties`にある数値のCamera RawプロパティだけをXMPサイドカーに書き込みます。131個の値を明示的に許可し、HSL色別スライダー、旧式のClarity/Fill Light/Highlight Recovery/増分WB、HDR最大値、カラーグレーディング、色別色相境界、パラメトリックトーン、ノイズ低減、シャープ化、粒子、周辺光量、手動レンズ/パースペクティブ補正などを含みます。名前はAdobeの公開XMP資料、インストール済みのXMPテンプレート、LRT 7.5.5のプロパティ登録情報から選びました。これはLRTがAuto-Transitionで扱う全項目を確定するものではありません。HueAdjustmentの色別バンドシフトは線形値として、カラーグレーディング/SplitToningの色相角とDefringeの色相境界は0〜360度の最短回転として補間します。`interpolate`ではグローバルポイントトーンカーブの`ToneCurve`、`ToneCurveName`、`ToneCurveName2012`、`ToneCurvePV2012`とRGBチャンネル別カーブを先頭画像から全フレームへコピーします。LRT開発者の説明でもポイントカーブはアニメーションせず、先頭画像からコピーされます。LRT 7.1はLook/Preset/PointColorsも均等化するとしていますが、コピー元は明記していません。lrtlはこれらのトップレベルXMP値も先頭画像からコピーする互換判断を採り、LRTとの完全一致は未確認です。マスク内のカーブやローカル補正遷移は未対応です。プロパティ名には`crs:`を付けても省いても構いません。`mark`でキーフレームを設定し、`set`でキーフレームの値を入力し、`interpolate --write`で間のフレームを補間してから、JPEG/PNG/TIFFでは`render --apply-xmp-exposure`でExposure2012を、`render --apply-xmp-crop`で回転を含むクロップを画素へ適用できます。既定では既存XMPをバックアップし、`--dry-run`で書き込み内容を確認、`--no-backup`でバックアップを省略できます。これはLightroom/Camera Rawの現像エンジンを置き換える機能ではなく、XMPへ補間した値のうち現在レンダーに画素反映できるのはExposure2012とクロップだけです。`set`はCamera Rawスライダーごとの許容範囲を検証しません。

`keyframes`は追加する内側の候補数を指定できます。既定は5候補で、未マークの内側フレームが5未満ならその数まで減らします。通常モードはJPEG/PNG/TIFFの明るさ曲線を平滑化し、直線遷移から外れるフレームを優先します。対応RAWは`--raw-preview`で埋め込みプレビュー、または`--raw-develop`でDNGLab現像画像を解析します。`--apply-xmp-exposure`/`--apply-xmp-crop`を指定すると、候補解析へ既存設定を反映します。`--evenly-spaced`は輝度を見ずに等間隔の候補を選び、画像デコードを省略します。どちらのモードも最初と最後の画像は候補にしません。既存の4/5つ星キーフレームと2/3つ星のHoly Grailマーカーは固定アンカーとして扱い、候補から除外します。`--write`でも既存評価やマーカーを上書きしません。

`unmark --force-rating`はlrtl以外が設定した4つ星/5つ星評価にも使えます。その評価を削除するため、評価がキーフレームマーカーとして使われている場合に限ってください。

`holy-grail`は各フレームのEXIFから露出設定値`log2(露出時間 × ISO / F値²)`を求め、先頭フレームとの差を打ち消す補正量として`crs:Exposure2012`に加えます。対応形式はJPEG、TIFF、CR2、CR3、DNG、RW2です。全フレームに有効な露出時間とISOが必要です。F値がない場合は`--ignore-aperture`を指定し、絞りが変化しなかったことを確認してください。`--stretch`はEXIF露出値を最小・最大で正規化した曲線のスケール（-1.5〜+1.5）、`--rotate`はシーケンスに加える直線傾き（-4〜+4）です。`--auto-shape`はLRTで確認したStretch/Rotateの探索刻み・L1評価と、インストール済みアプリから逆解析した三次カーネル重み付き局所線形回帰（LOWESS）を使って値を提案します。EXIF由来の入力曲線などHoly Grailの上流処理は独自の近似です。いずれも指定しない場合は従来のEXIF露出差補正を使います。EXIFが撮影モードをManualと報告する場合、設定変更位置に2*/3*評価のペアを提案します。撮影モードが不明または自動の場合はペアを作らず、`--mark-jumps`で強制できます。設定変更が連続して隣接ペアを作れない場合や、既存評価と衝突する場合は書き込み前にエラーにします。`--no-mark-jumps`でペア付けを無効化できます。補正の再実行では前回分を置き換え、lrtl管理下の元評価も記録するため`--reset --write`で復元できます。既定はプレビューで、書き込み時は既存サイドカーをバックアップします。

LRTimelapse側では2*/3*が内部HGキーフレームで、4*/5*の編集キーフレームとは別レイヤーとして使われます。lrtlはこの評価マーカーを互換目的で扱いますが、補正値は現時点で通常の`crs:Exposure2012`に書き込むため、LRTの独立した背景補正レイヤーと同じ動作ではありません。LRTのWizardにはRotate（-4〜+4）、Stretch（-1.5〜+1.5）、`holyGrailReduceDeflicker`を使う調整・探索があります。lrtlの`--stretch`と`--rotate`はEXIF曲線に調整を加え、`--auto-shape`は確認できた2段階L1探索とLOWESS平滑化を使います。Holy Grailの上流曲線生成、Lightroom編集値との合成、独立補正レイヤーは未実装です。逆解析で確認した式と探索範囲は[リバースエンジニアリングノート](docs/reverse-engineering.md)に記録しています。

この計算は記録されたカメラ設定による露出差を補う近似です。LRTimelapse公式ワークフローのHoly Grail Wizardも撮影中に変更したシャッター速度/ISOの補正曲線を作ります。lrtlの曲線回転・伸縮はEXIFから作った曲線への追加調整です。`--auto-shape`は確認できた評価式とLOWESS平滑化を使いますが、入力曲線やLRTの独立補正レイヤーは再現していません。`preview`は静的な輝度グラフを出しますが、Lightroomの現像画像プレビューは未実装です。センサー感度の誤差、レンズ透過率、自然な明るさの推移も解析しません。RAW現像やXMPの画素反映をする機能ではありません。

## 今後の実装順

| 段階 | 目標 | 状況 |
|---|---|---|
| 1 | XMPサイドカー、キーフレーム、数値設定の補間 | 手動設定、輝度ベース候補提案、限定的な数値補間を実装 |
| 2 | 画像連番から最初の動画を生成 | JPEG/PNG/TIFFと一部RAWの埋め込みプレビューからMJPEG AVI、外部FFmpeg経由でH.264/H.265 MP4とProRes MOVに対応。`--long-edge`で縮小可能。完全なRAW現像は未実装 |
| 3 | 画像解析、プレビュー、Deflicker | 静的SVGの輝度/露出グラフ、単一参照領域・全体露出補正のレンダー時またはXMP適用、回転を含むXMPクロップを実装。LRT形式の画像付きVisual Preview、インタラクティブ曲線UI、XMPロスレス適用、Geometry補正、マスク別補正は未実装 |
| 4 | RAW現像、Holy Grail、XMP現像値の画素への反映 | EXIF設定ベースのHoly Grail露出補正、現像済みラスターへのExposure2012近似適用、一部RAW内蔵プレビュー、外部DNGLabによるRAW現像を実装。Camera Raw現像の完全再現と他のXMP値のRAW画素への反映は未実装 |
| 5 | ProRes/H.265等の追加コーデック、オーバーレイ、取り込み、GUI | H.265/ProResと日時オーバーレイを実装。DNxHR、モーションブラー、取り込み、GUIは未着手 |

## 機能調査の出典

公式ページの機能・ワークフローを参考に、同等機能の範囲を整理しました。

- [LRTimelapse 7の機能一覧](https://lrtimelapse.com/features/)
- [LRTimelapse 7 Basic Tutorial: Keyframes Wizardと内部編集](https://lrtimelapse.com/tutorial/basic/complete/)
- [Visual Workflow](https://lrtimelapse.com/workflow/visual-workflow/)
- [Holy Grailの2*/3*内部キーフレームに関するLRT開発者の説明](https://forum.lrtimelapse.com/Thread-handling-different-exposure-times-with-keyframes)
- [Lightroomを使わない内部ワークフロー](https://lrtimelapse.com/workflow/internal-workflow/)
- [Export and Render](https://lrtimelapse.com/workflow/export-and-render/)
- [Long Term / Construction](https://lrtimelapse.com/workflow/long-term-construction/)
- [LRTimelapse Import](https://lrtimelapse.com/workflow/lrtimport/)
- [LRTimelapse 7.2 release notes: Simple Deflicker](https://lrtimelapse.com/updates/lrtimelapse-7-2/)
- [LRTimelapse 7.1 release notes: XMP equalization on Auto-Transition](https://lrtimelapse.com/news/lrtimelapse-7-1-released/)
- [Adobe Camera Raw XMP namespace reference](https://github.com/adobe/xmp-docs/blob/master/XMPNamespaces/crs.md)
- [Adobe Community SDK guide to Lightroom crop coordinates and CropAngle](https://community.adobe.com/questions-675/sdk-computing-the-corners-of-a-crop-rectangle-962108)
- [AVI/MJPEG writer for Go](https://github.com/icza/mjpeg)
- [FFmpeg documentation: image formats and H.264 encoding](https://ffmpeg.org/ffmpeg.html)
- [Pure-Go TIFF decoder/encoder used by lrtl](https://pkg.go.dev/golang.org/x/image@v0.29.0/tiff)
- [Pure-Go EXIF reader used for Holy Grail exposure analysis](https://github.com/evanoberholster/imagemeta)
- [ExifTool's public CR3 UUID preview layout notes](https://github.com/exiftool/exiftool/blob/master/lib/Image/ExifTool/QuickTime.pm#L761)
- [libopenraw's Panasonic RW2 format notes](https://libopenraw.freedesktop.org/formats/rw2/)
- [ExifTool's Panasonic RAW embedded JPEG tag definition](https://github.com/exiftool/exiftool/blob/master/lib/Image/ExifTool/PanasonicRaw.pm#L193)
- [libopenraw's Olympus ORF format notes](https://libopenraw.freedesktop.org/formats/orf/)
- [libopenraw's Fujifilm RAF header and JPEG offsets](https://libopenraw.freedesktop.org/formats/raf/)

## 開発

```powershell
go test ./...
go vet ./...
go build -o lrtl.exe ./cmd/lrtl
```

互換性の観察と既知の制約は[リバースエンジニアリングノート](docs/reverse-engineering.md)を参照してください。

## ライセンス

MIT。詳細は[LICENSE](LICENSE)を参照してください。
