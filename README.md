# LumaFrame (`lrtl`)

LRTimelapseと同じ作業を段階的に実現することを目指す、独立したオープンソースのタイムラプスツールです。Lightroom互換XMPのキーフレーム編集やHoly Grail露出補正に加え、現像済みJPEG/PNG/TIFFとRAW連番からMJPEG AVI、H.264/H.265 MP4、ProRes MOVを生成できます。

**現像済み画像は追加ソフトなしでレンダーできます。RAWをセンサー情報から現像するには、外部のDNGLabが必要です。** `render --raw-develop --dnglab PATH`はDNGLabの16-bit sRGB TIFF出力を動画にします。`--raw-preview`はカメラ内蔵プレビューを使う旧来の軽量経路です。DNGLabでRAW画像を現像して動画にできますが、Camera Raw/Lightroomの全設定やXMP現像値を再現する機能ではありません。

本プロジェクトはLRTimelapseおよびAdobeと無関係です。独自実装であり、それらのコード、画像、同梱ツールを含みません。

## 実装状況

下表はローカルにインストールされたLRTimelapse 7.5.5とlrtlの現行コードを照合したものです。LRT側の主要機能とライセンス別の提供範囲は[公式機能表](https://lrtimelapse.com/features/)、[基本チュートリアル](https://lrtimelapse.com/tutorial/basic/complete/)、[内部ワークフロー](https://lrtimelapse.com/workflow/internal-workflow/)と、インストール済みアプリの各メニューを2026-10-15に確認しました。「未実装」は今後の目標です。LRTimelapseのLightroom/Camera Raw連携は外部ソフトを使うワークフローです。

状態の「一部」は該当機能の一部を実装済みですが、LRTと同等ではないことを示します。「未実装」は対応コマンドや処理がありません。「対象外」はLRTのライセンス区分にある制限をOSS版では再現しない項目です。

| 機能領域 | LRTimelapseの機能 | lrtlの状況 | 状態 |
|---|---|---|---|
| シーケンス規模 | 評価版は400枚まで、Private/Proはサイズ制限なし | 画像列の枚数上限なし。AVIは約4 GBまで。MP4はFFmpeg対応 | 一部 |
| ワークフロー | Basic、Visual、Long-Term、Lightroom/ACR連携、内部編集/Export | CLIで個別機能を実行。ワークフロー状態や内蔵現像画面なし | 一部 |
| キーフレーム | 輝度推移からキーフレーム数の目安を提示し、スライダーで調整するWizard。4/5つ星を選択でき、手動設定と等間隔配置も可能 | 手動マーク、JPEG/PNG/TIFF、`--raw-preview`内蔵プレビュー、または`--raw-develop`現像RAWを使う`keyframes`を実装。既定数量はLRT 7.5.5から復元した輝度レンジ・連番長・2*/3*マーカーの式。候補位置も同版の整数間隔・端点選択ルールに対応。`--rating 4|5`でLRT Wizardと同じ評価を選び、既定は4つ星。既存マーカーは既定で保持し、選択中の評価だけ数量推定から差し引く。`--replace-existing`では同じ評価を連番から削除し、他の2〜5つ星との衝突を前方9枚以内の空きフレームへ移す。`--evenly-spaced`は輝度解析なしで利用可能。`--luminance-deviation`はlrtl独自配置法。`--apply-xmp-basic`（または`--apply-xmp-exposure`）と`--apply-xmp-crop`を解析に反映できる。選択行のUI、LRT内部の現像輝度値との一致は未対応 | 一部 |
| Holy Grail | Mモードで変化させた露出やISOを補正して昼夜遷移をつなぐ | JPEG/TIFF/CR2/CR3/DNG/RW2のEXIF露出設定から補正を計算。EXIFがManualを示す場合に2*/3*ペアを提案・記録。`--stretch`/`--rotate`とLRTの探索範囲・L1評価・LOWESS平滑化を使う`--auto-shape`に対応。RAW曲線の生成と独立補正レイヤーは未再現。補正を`crs:Exposure2012`へ書く方式 | 一部 |
| 構図・クロップ | Composition作成、キーフレーム間のクロップアニメーション | 5つ星crop markとCropAngle/Top/Left/Bottom/Rightの数値補間に対応。`render --apply-xmp-crop`で回転を含む矩形を画素へ適用し、元と同じ解像度に再サンプル。プレビュー上の構図作成・編集UIとCamera RawのGeometry補正は未対応 | 一部 |
| レンズ情報 | マニュアルレンズのLens Tagger | 未実装 | 未実装 |
| 連番管理・選択 | ファイル名/キーフレーム/Visual Previewによる選択、フィルター、反転、連番内の移動・コピー・除去・削除、フォルダー作成とリネーム | `scan`で自然順に一覧表示し、キーフレーム評価を確認可能。`render --filename-contains TEXT`でファイル名の部分一致（大文字小文字を区別しない）による絞り込みに対応。選択UI、キーフレーム/Visual Preview選択、反転、コピー/移動/除去/削除、フォルダー作成、リネームは未対応 | 一部 |
| メタデータ管理 | EXIF再読込、メタデータの初期化/消去、連番全体のコピー/貼付、撮影日時シフト、レンズタグ付け | EXIF読込、XMP数値値の個別編集、`timeshift`によるXMP撮影日時の連番一括シフトに対応。日時シフトはXMPサイドカーのみ変更し、埋め込みEXIFは変更しない。メタデータ一括操作やLens Taggerは未対応 | 一部 |
| XMP・現像値 | Lightroom/Camera Raw等とのXMP連携、現像パラメーターの遷移 | 外部XMPを読み書きし未知のRDF値を保持。`set`で許可した131項目を編集し、全体設定とLRTの位置登録スロットにあるローカル補正・基本マスク形状をキーフレーム間で補間。sRGB入力のレンダーでは`--apply-xmp-basic`で全体8項目、Texture/Clarity/Dehaze/Sharpening/NoiseReduction、4領域パラメトリックカーブ、HSL 8色域の色相/彩度/輝度、Temperature/Tint、全体ColorGrade 4領域の色調、明示されたマスター/RGBポイントカーブに加え、線形/円形グラデーションと現行形式の輝度範囲コンポーネントのAdd/Intersect合成（`MaskGroupBasedCorrections`/`Mask/Aggregate`、MaskBlendMode=0/1、MaskInverted/Flipped対応）に対するLocalExposure/Contrast/Highlights/Shadows/Whites/Blacks/Saturation/LocalTexture/LocalClarity2012/LocalDehaze/LocalSharpness/Temperature/Tint/LocalToningHue/LocalToningSaturation/LocalColorGradeとローカルMainCurve/RedCurve/GreenCurve/BlueCurveを独自近似で画素反映し、クロップも任意適用。ローカルカーブはマスク濃度でLUT出力を混ぜる近似で、LocalCurveRefineSaturation、ExtendedMainCurveなどのHDR曲線、キーフレーム間の曲線補間は未対応。色範囲Type=1はColorAmountを数値スプラインで遷移し、PointModelsはフレームごとに保持する。レンダーではSampleType=0のPointModelsをCIELAB/ΔE76の幅推定で近似選択（Adobeと画素一致未確認）。深度範囲、ブラシ描線/AI、他の合成モード、Camera Raw相当RAW現像も未対応 | 一部 |
| Auto-Transition | キーフレーム間を自動補間。Look/Preset/Tone Curve/PointColorsのXMP均等化も可能 | LRT 7.5.5の実行時レジストリに登録された127個の全体数値項目と、固定配列スロットのローカル補正・マスク数値項目を3次ベジェスプラインで遷移する。Linear 1–4、Circular 1–2、Brush 1–2、MaskGroup 1–4/8–11と各CorrectionMasks[1]を位置で対応させ、Sync IDは必須にしない。`--smoothness 0..100`はLRTの設定を再現し既定値は50。Look/Preset/PointColorsとポイントカーブ、MaskGroup 1–4/8–11のMainCurve/RedCurve/GreenCurve/BlueCurve構造を先頭フレームから均等化する。ローカルカーブは構造をコピーするだけでキーフレーム間の曲線補間はしない。`LumRange`文字列は最初の非空値を同じ配列位置の全フレーム（キーフレームを含む）へコピーし、色範囲Type=1はColorAmountを補間する。`PointModels`はLRTの遷移登録に見つからないため変更しない。円形グラデーションとMaskGroup 8/9のAngleはLRTと同じ後処理（−45°未満なら+90°、45°超なら−90°）を、補間値と既存キーフレーム値に適用する。Depthの画像依存データ、ブラシ描線、登録スロット外のマスクは未対応。非キーフレームに欠けた登録位置のマスク構造は、最寄りの実体を持つXMPからRDF配列位置で複製して作成する。登録位置外やSync ID形式の欠損構造は未対応。RDF配列として格納されない独自XMPではSync IDを使って対応する。登録値ごとの範囲クランプとInteger型四捨五入を適用する。許可リストにある旧式4項目（ChromaticAberrationB/R、Defringe、Exposure）はLRTの遷移登録にないため補間しない。先頭フレームのcrs:IsMergedHDRがtrueならExposure2012を-10..10、通常は-5..5に制限する。アスペクト比を使う円形グラデーションの専用形状補正、Camera Rawのマスク描画との一致は未対応 | 一部 |
| スナップショット | シーケンス状態のスナップショット作成、読み込みと管理 | `.lrtl/snapshots`にXMPサイドカーのZIPスナップショットを作成/一覧表示し、復元案をプレビュー可能。元の連番パスを確認し、別パスには`--allow-moved`で復元。`--write`で適用し、既存サイドカーは既定でバックアップ。画像本体は含めず、後から増えたサイドカーは削除しない | 一部 |
| 視覚プレビュー | 露出や遷移を画像・グラフで確認 | `preview`で輝度曲線、XMP Exposure2012、キーフレームを静的SVGに表示。RAW内蔵プレビュー/DNGLab現像と、任意でXMPの基本現像値（グラデーションと現行形式の輝度範囲マスクによるローカル露出・トーン・色・WBを含む）、露出またはクロップを解析に適用。画像サムネイル、インタラクティブ操作、LRTの現像結果プレビューは未対応 | 一部 |
| Deflicker | 参照領域と平滑化曲線に基づく視覚補正、複数パス、Lightroomへ渡す調整値 | `render --deflicker`で画素に補正し、`deflicker --write`でJPEG/PNG/TIFFまたは`--raw-preview`／`--raw-develop`の輝度解析結果を`crs:Exposure2012`へ記録可能。`--smoother gaussian|lowess`で平滑化を選択。`deflicker`解析は`--apply-xmp-basic`（グラデーションと現行形式の輝度範囲マスクによるローカル露出・トーン・色・WBを含む）または露出のみの`--apply-xmp-exposure`、`--apply-xmp-crop`を測定前に適用できる。LRTで観察したtricube重み付き局所線形回帰に近いLOWESSも選べるが、測光・反復補正は独自近似。LRTのVisual Preview、Camera Rawとの数値一致、カーブUIは未対応 | 一部 |
| 長期撮影補正 | Luminance・Contrast・Hueで長期連番の画像をフィルターし、明るさ・コントラスト・色の大きく異なる画像を除外 | `long-term`で画像を分析し、各指標の範囲から選択JSONを作成。`render --selection`で選択フレームのみ動画化でき、`--apply-xmp-basic`（グラデーションと現行形式の輝度範囲ローカル露出/彩度マスクを含む）/`--apply-xmp-exposure`/`--apply-xmp-crop`で測定前にXMP値を反映できる。既定指標は独自定義。`--metric-profile lrt-7.5.5`は64×64 bilinear縮小、Gaussian scale 4のぼかし、平均グレースケール輝度/標準偏差/平均HSB色相を使う。LRTの時間フィルター曲線、構図UI、現像プレビューは未対応 | 一部 |
| 動画生成 | 現像済みフレームを動画にレンダー | JPEG/PNG/TIFFとRAW現像画像をMJPEG AVI、外部FFmpegによるH.264/H.265 MP4またはProRes 422/422 HQ MOVに出力。EXIF向き、XMPの全体8項目/HSL色別補正/簡易Temperature・Tint/ポイントカーブ/グラデーションと現行形式の輝度範囲マスクによるローカル露出・トーン・色・WB/クロップ、任意アスペクト比への中央クロップ、Deflicker、撮影日時焼き込みに対応。整数1–240 fps、`--output-height`で出力高を指定し、`--long-edge`で長辺上限まで縮小。全フレームはEXIF向き適用後の解像度が同じ必要 | 一部 |
| RAW・DNG | RAW処理、DNG変換 | `--raw-develop`で外部DNGLabにより主要RAW形式を現像し、その16-bit sRGB TIFF出力からレンダー可能。`import --convert-dng`で対応RAWを外部DNGLab経由のlossless/uncompressed DNGへ変換可能。`--raw-preview`によるカメラ内プレビュー経路も維持。DNGLabは別途インストールが必要。Camera Raw/Lightroom設定の完全な適用、ロッシーDNGとサイズ縮小は未実装 | 一部 |
| 解像度・中間画像 | 720p〜8K/Originalなどライセンス別解像度、Pro向け非圧縮TIFF中間画像 | `--aspect W:H`で1:16〜16:1の任意比率へ中央クロップし、`--output-height H`で出力高を指定。`--long-edge N`は長辺上限まで縮小。LRTのプリセット/UIや非圧縮TIFF中間画像は未対応 | 一部 |
| コーデック・色・HDR | H.264/MP4、Pro向けH.265/ProRes/DNxHR、HDRワークフロー | 純GoのMJPEG AVIとFFmpegのlibx264/libx265/prores_ks/dnxhd経由のH.264/H.265 MP4、ProRes 422/422 HQとDNxHR LB/SQ/HQ/HQX/444 MOV。MP4/MOVは`--audio FILE`でAAC音声を追加可能。Rec.2020/HDRは未対応。入力画素はRGB 8-bitなのでProRes/DNxHR 10-bit出力も16-bit RAW精度を保持しない | 一部 |
| オーバーレイ・字幕 | 透かし、撮影時刻表示、SRT字幕出力 | `render --watermark TEXT`で任意テキストを各フレームに焼き込み、位置を5種類から選択可能。`render --timestamp-overlay`で撮影日時も表示し、`subtitles`で同じ日時情報からSRT字幕を出力。埋め込みフォントを使用 | 一部 |
| Motion Blur Plus | 書き出し動画への低/中/高モーションブラー | `render --motion-blur N`でLRTの7種類のフレーム数と、確認できた`tmix`重み`(i/N)^1.5`（3桁丸め）を適用。過去N枚を合成し、先頭は最初のフレームを複製して窓を満たす。8-bit RGB処理のためLRTの10-bit YUVフィルター出力との画素一致は未確認 | 一部 |
| 取り込み | フォルダー/カードまたは動画ファイルからの取り込み、サブフォルダー作成、日時順整列、JPG省略、日時リネーム | `import SOURCE --output DIRECTORY`で写真ファイルと対応XMPをコピー。任意で再帰検索、撮影日時順、JPEG/JPG除外、撮影日時へのリネーム、dry-runに対応。`--convert-dng`で対応RAWをlossless/uncompressed DNGへ変換し、他の画像とXMPを取り込み。`import-video VIDEO --output DIRECTORY`でFFmpegから動画の全フレームまたは指定fpsのフレームをJPG/PNG連番へ抽出。画像プレビュー、ロッシーDNG、DNGのサイズ縮小、動画の自動分割は未実装 | 一部 |
| DNG変換 | 取り込み時のロスレス/ロッシーDNG変換、サイズ縮小（Pro） | `import --convert-dng`でDNGLabが対応するRAWをlosslessまたはuncompressed DNGへ変換。ロッシー圧縮とPro相当のサイズ縮小は未対応 | 一部 |
| バッチ・並列処理 | バッチプレビュー/補正、マルチスレッド処理 | `batch-render ROOT --output DIRECTORY`で複数の画像連番を動画化。既定でルート直下、`--recursive`で全階層から連番フォルダーを検索し、`--workers`で並列数を指定。`--`以降で通常の`render`オプションを共通指定できる。バッチプレビューとXMP一括補正は未実装 | 一部 |
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
2. JPEG/PNG/TIFF連番、RAW内蔵プレビュー、またはDNGLabで現像したRAWからLRT互換の数量と等間隔キーフレーム位置を提案し、任意でXMPにマークする。
3. キーフレームを手動で設定し、許可済みの数値設定をプレビューまたはXMPに書き込む。
4. 現像済みJPEG/PNG/TIFF、対応RAW内蔵プレビュー、またはDNGLabで現像したRAWをMJPEG AVI、H.264/H.265 MP4、ProRes MOVにまとめる。必要なら`--aspect 16:9 --output-height 1080`のように出力比率と高さを指定するか、`--long-edge`で縮小する。XMP値未焼き込みのsRGB入力には`--apply-xmp-basic`で基本8項目、Texture/Clarity/Dehaze/Sharpening/NoiseReduction、4領域パラメトリックカーブ、8色域のHSL補正、Temperature/Tintの簡易ホワイトバランス、ColorGrade近似、明示ポイントカーブを近似適用できる。
5. レンダー時に簡易Deflickerを画素へ適用するか、JPEG/PNG/TIFFまたは選択したRAW入力の解析結果をXMP露出値として保存する。
6. 対応画像のEXIF露出時間・ISO・絞り値からHoly Grail補正を算出し、Camera RawのXMP露出値として出力する。
7. 輝度曲線、XMP露出補正、キーフレーム位置をSVGプレビューで確認する。RAW解析時は内蔵プレビューまたはDNGLab現像を選び、必要に応じてXMP Exposure/cropを反映する。
8. XMPサイドカーをZIPスナップショットに保存し、一覧表示とプレビュー後の復元を行う。
9. フレームのEXIF/XMP撮影日時を動画タイミングに合わせたSRT字幕に出力する。
10. 長期連番を輝度・コントラスト・色相で分析し、選択した画像だけを動画にする。
11. カード/フォルダーから写真とXMPを取り込み、必要に応じて日時順へ並べたりJPEGを除外したりする。対応RAWは外部DNGLabでDNGに変換できる。
12. 動画から全フレームまたは指定fpsのJPG/PNG連番を抽出し、通常の画像列としてレンダーする。
13. XMPサイドカー上の連番撮影日時を一括で前後にずらす。
14. 複数の連番フォルダーを並列に動画化し、レンダー失敗を連番ごとに報告する。

動画生成の最短経路は、Lightroom等でJPEG/PNG/TIFFを書き出してから`lrtl render`を実行する方法です。キーフレーム間のXMP値も反映する場合は、XMP現像値をまだ焼き込んでいない画像に対して`interpolate --write`後に`render --apply-xmp-basic`を使います。このオプションはExposure2012、Contrast2012、Highlights2012、Shadows2012、Whites2012、Blacks2012、Vibrance、Saturationの8項目にTexture/Clarity/Dehaze/Sharpening/NoiseReductionと4領域のパラメトリックカーブに加え、8色域のHue/Saturation/Luminance調整、Temperature/Tint、XMPに明示されたマスター/RGBポイントカーブを表示済みsRGB画像へ独自近似で適用します。HSLは色域中心の間を線形補間し、色相をスライダー値あたり0.3度、彩度を0〜2倍、輝度をHSL明度の±0.25以内として近似します。ホワイトバランスは推定色温度からRGBゲインを求める近似で、カメラのセンサーデータやプロファイルを使わず、Lightroom/Camera Rawと画素値が一致する現像機能ではありません。線形/円形グラデーションと現行形式の輝度範囲マスクは、単一またはAdd/Intersect合成の対応範囲でローカル補正値を独自近似で適用します。ブラシ/AI、Depth範囲、旧形式の範囲マスク、プリセット名だけのカーブ、ExtendedMainCurveなどのHDRローカル曲線は未対応です。ローカルMainCurve/RedCurve/GreenCurve/BlueCurveは[共有されたXMP実例](https://community.adobe.com/feature-requests-676/p-implement-copy-and-paste-global-curve-to-masking-local-curve-and-vice-versa-665754)にある点列をsRGB LUT化し、マスク濃度で混ぜる近似です。LocalCurveRefineSaturationは未対応です。LocalTemperature/Tintは値を推定RGBゲインへ写像し、各ローカル効果をマスク濃度で混ぜます。既にLightroom/Camera Rawで同じ現像値を焼き込んで書き出したJPEG等には再適用せず、通常の`render`を使ってください。RAW連番を直接レンダーするには外部DNGLabを使う`--raw-develop`を指定します。DNGLabのRAW対応はカメラ形式ごとに異なり、DNGLabはalpha版です。`--raw-preview`は内蔵プレビューを使う別経路です。`--apply-xmp-exposure`を指定すると、サイドカーの`crs:Exposure2012`だけをレンダー入力画像に近似適用します。`--apply-xmp-basic`には同じ露出値が含まれるため、両方は同時に指定できません。DNGLabの現像パラメーターはCamera Rawと一致せず、サイドカーにある他の現像値もRAW現像には使いません。Deflickerはレンダー時に出力画素へ適用できます。これらのレンダー操作は元画像やXMPを変更しません。別コマンドの`lrtl deflicker --write`は、解析した補正をXMPに保存します。全フレームの幅と高さは同じである必要があります。MJPEG AVIには約4 GBのサイズ上限があります。MP4/MOV出力は外部FFmpegを必要とし、入力画像をGoで8-bit RGBへデコードしてから選択したエンコーダーへ渡します。`.mp4`の既定はH.264、`.mov`の既定はProRes 422です。`--codec h265`でH.265、`--codec prores-hq`でProRes 422 HQ、`--codec dnxhr-lb|dnxhr-sq|dnxhr-hq|dnxhr-hqx|dnxhr-444`でDNxHR各プロファイルを選べます。FFmpegに`libx264`、`libx265`、`prores_ks`または`dnxhd`エンコーダーが必要です。DNxHR HQX/444とProResは10-bit形式へ符号化しますが、lrtlの現行フレーム経路はRGB 8-bitなので、入力にない階調やHDR情報は増えません。

`--apply-xmp-basic`は`Dehaze`とマスク補正内の`LocalDehaze`も近似適用します。正の値は中間輝度を基点にコントラストを最大20%高め、画像短辺に応じた広半径の輝度高周波成分を最大0.6倍加えます。負の値は画素を線形輝度0.78のベールへ最大18%混ぜ、彩度は正の値で最大15%上げ、負の値で最大15%下げます。これは連番間で急変しにくいsRGB近似で、Camera Rawの大気光推定やRAW処理、画素一致は再現しません。AdobeはDehazeが霞を減らす/加える調整で、局所マスクにも使えると説明しています（[全体調整](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/image-tone-color.html)、[ローカル調整](https://helpx.adobe.com/uk/lightroom-classic/desktop/process-and-develop-photos/apply-local-adjustments.html)）。

`--apply-xmp-basic`は`Sharpness`と`SharpenRadius`/`SharpenDetail`/`SharpenEdgeMasking`も読みます。短辺1080pxを基準に半径をスケールし、sRGB線形輝度の広域差を加えてエッジを強調します。Detailは強調量、EdgeMaskingは高コントラスト部へ絞るしきい値として近似し、`LocalSharpness`はマスク濃度に合わせて同じ処理を適用します。LocalSharpnessの負値はぼかし方向に働きます。Camera Rawと画素一致する処理ではなく、RAWノイズ除去やデモザイク前のシャープ化も行いません。Adobeが説明するAmount/Radius/Detail/Maskingの役割は[公式資料](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/retouch-photos.html)を参照してください。

`--apply-xmp-basic`は`ParametricShadows/Darks/Lights/Highlights`と各`Parametric*Split`も読みます。3つの分割位置を単調に整え、線形sRGB輝度上で4領域のsmoothstep重みと最大0.15の加算値を用いて近似カーブを適用します。これはAdobeが説明する領域別調整と分割コントロールの独自演算で、LRT/Camera Rawとの画素一致は未確認です（[Adobeのトーンカーブ説明](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/image-tone-color.html)）。`--apply-xmp-basic`は`Texture`/`LocalTexture`と`Clarity2012`/`LocalClarity2012`も近似適用します。sRGB輝度から細い/広い半径の高周波成分を抽出し、Textureは細部、Clarityは中間調に重みを付けて輝度へ加算し、ローカル値はマスク濃度で混ぜます。半径は画像の短辺に合わせて制限し、色相を保つようRGB各チャンネルを同じ比率で拡縮します。独自の8-bit sRGB処理でCamera Rawとの画素一致は未確認です。[AdobeはTextureを色や階調を変えずに細部へ作用する調整、Clarityを局所コントラスト調整として説明しています](https://helpx.adobe.com/uk/lightroom-classic/desktop/process-and-develop-photos/apply-local-adjustments.html)。

`--apply-xmp-basic`はLuminanceSmoothing/LuminanceNoiseReductionDetail/LuminanceNoiseReductionContrastとColorNoiseReduction/ColorNoiseReductionDetail/ColorNoiseReductionSmoothnessを近似適用します。各値は0〜100で、3×3の範囲内から輝度差・色差が近い画素を重み付き平均し、輝度ノイズと色ノイズを別々に弱めます。Luminance Detailは輝度境界の保護、Contrastは平滑化後に局所コントラストを残す量、Color Detail/Smoothnessは色差許容値として使います。固定近傍と固定スライダー応答を使う独自sRGBフィルターで、AdobeのRAWノイズ除去やAI Denoiseとの一致は未確認です。Adobeの各スライダーの役割は[公式資料](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/retouch-photos.html)を参照してください。

`--apply-xmp-basic`は`ColorGradeShadow/Midtone/Highlight/GlobalHue/Sat/Lum`、`ColorGradeBlending`、`ColorGradeBalance`と、対応するローカル補正内の`LocalColorGrade*`も読みます。線形輝度0.15/0.5/0.85を中心とする正規化Gaussian重みで3領域を分け、Blendingを重なり幅、Balanceを影/ハイライトの寄せ方として使います。各領域の色相/彩度はHSL色付け、輝度は線形輝度スケールで近似し、LocalColorGradeはマスク濃度で混ぜます。Adobeは3領域の色相/彩度とBlending/Balanceの役割を説明していますが、この重み付けと画素演算は独自近似です（[Adobe Color Gradingガイド](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/image-tone-color.html)）。Camera Rawとの画素一致は未確認です。

`--apply-xmp-basic`は現行Camera Raw XMPの輝度範囲マスクも限定対応します。`Mask/RangeMask`コンポーネント内の`CorrectionRangeMask Type=2`と4要素の`LumRange`を読み、向き適用後・クロップ前の各画素で線形sRGB輝度に基づく滑らかな範囲選択を評価します。`MaskBlendMode`、`MaskInverted`、範囲側の`Invert`を合成します。選択とAdd/Intersectは独自近似で、Adobeとの画素一致は未確認です。Color Range Type=1はSampleType=0の`PointModels`内6値を読み、後半3値をsRGBサンプル色としてΔE76距離と`ColorAmount`から選択を推定します。座標と末尾識別値の意味、Adobeの選択演算は未解析のため近似です。Type=1以外、Depth範囲、旧形式の`CorrectionRangeMask`、ブラシ/AIマスクは未対応です。

`preview`は画像列の線形sRGB輝度を測り、最初のフレームからのEV差、XMPの`Exposure2012`差、キーフレーム評価をSVGグラフに出します。JPEG/PNG/TIFFのほか、`--raw-preview`で対応RAW内のプレビュー、または`--raw-develop`でDNGLab現像画像を解析できます。`--apply-xmp-basic`（`--apply-xmp-exposure`とは排他的）と`--apply-xmp-crop`を指定すると、輝度解析にその値を適用します。これは静的な数値グラフで、Lightroom/Camera Rawの現像画像やLRTimelapseの操作可能なVisual Previewは表示しません。

`long-term`は画像を最大64×64点のグリッドでサンプリングし、線形sRGB輝度の中央値、輝度log2の90–10パーセンタイル差（EV）、彩度で重み付けした円形平均色相（0–360度）を記録します。`--luminance-range`、`--contrast-range`、`--hue-range`はそれぞれ`MIN,MAX`で指定し、指定した全範囲を満たすフレームをJSONマニフェストに含めます。色相は`MIN>MAX`で0度をまたぐ範囲を表せます。無彩色など平均色相が定まらない画像は色相範囲指定時に除外します。範囲を何も指定しなければ全画像を含めます。`render --selection`は元フォルダー、ファイル名、サイズ、更新時刻を照合してから含まれる画像をレンダーし、画像やXMPを変更しません。`render --filename-contains TEXT`はさらにファイル名の大文字小文字を無視した部分一致で絞り込みます。LRTimelapse 7にも「Select by Filename」がありますが、lrtlはCLI絞り込みのみで、ファイル一覧上の選択UIはありません。LRTimelapseも長期用にLuminance/Contrast/Hueフィルターを提供します。既定のlrtl指標は独自実装です。`--metric-profile lrt-7.5.5`を選ぶと、インストール済み7.5.5で確認した64×64 bilinear縮小、scale 4のGaussian blur、平均グレースケール輝度、グレースケール標準偏差、平均HSB色相による測定を使います。数値計算は別実装で、LRTの選択構図、時間方向のフィルター/定数補正、曲線UIは再現しません。

解析コマンドの`--apply-xmp-basic`、`--apply-xmp-exposure`、`--apply-xmp-crop`はlrtl側の画素近似です。`--apply-xmp-basic`は露出、トーン、色、Temperature/Tintをまとめて含み、`--apply-xmp-exposure`とは同時に使えません。既にLightroom等でその補正を焼き込んだJPEG/PNGでは二重適用になるため、主にRAWプレビューやDNGLab現像画像の解析に使ってください。

`batch-render`はROOT自身と直下の各サブフォルダーを調べ、対応画像を含むフォルダーを個別の連番としてレンダーします。`--recursive`を指定すると全階層を検索します。ROOTが連番の場合は`ROOT名.mp4`のように出力し、配下の連番は相対フォルダーを保って出力します。`--format`は`avi`/`mp4`/`mov`（既定`mp4`）、`--workers`は同時処理数（既定1、最大64）です。`--`の後に指定した通常の`render`オプションは全ジョブへ渡されます。各ジョブの成功・失敗をまとめて表示し、失敗した連番があっても他のレンダーを続け、どれかが失敗した場合は終了コード1を返します。これは動画レンダーのバッチ機能で、プレビュー生成やXMP補正の一括適用はまだありません。

## インストール

ビルドにはGo 1.23以降が必要です。Go側の依存ライブラリはPure Goです。MJPEG AVI出力にはFFmpegやネイティブDLLは不要です。H.264/H.265 MP4とProRes MOV出力、動画からのフレーム抽出には外部FFmpegが必要です（`--ffmpeg`で実行ファイルを指定できます）。RAW現像とDNG変換には外部DNGLabが必要です（PATHに置くか`--dnglab`で指定）。FFmpegとDNGLabはlrtlに同梱しません。

```powershell
go install github.com/a-kaibu/lrtl/cmd/lrtl@latest
```

またはソースからビルドします。

```powershell
go build -o lrtl.exe ./cmd/lrtl
```

## クイックスタート: 画像列から動画を作る

Lightroomなどで現像・書き出し済みのJPEG/PNG/TIFFを1つのフォルダーに置き、ファイル名がフレーム順になるようにします。画像サイズは全フレームで揃えてください。

```powershell
# 画像列と並び順を確認
.\lrtl.exe scan "D:\Timelapse\Sequence"

# 依存ソフト不要のMJPEG AVIを作成（既定は24 fps）
.\lrtl.exe render "D:\Timelapse\Sequence" --output "D:\Timelapse\Timelapse.avi"
```

H.264 MP4にする場合はFFmpegをPATHに追加し、出力先を`.mp4`にします。

```powershell
.\lrtl.exe render "D:\Timelapse\Sequence" --output "D:\Timelapse\Timelapse.mp4" --fps 24
```

AVI出力と画像列の処理はPure Goです。MP4/MOVには外部FFmpegが必要です。RAWを直接現像する場合は別途DNGLabも必要です。XMPの値を画像に焼き込む現像処理は、レンダー済み画像へ重ねて適用すると二重補正になるため、未適用の画像だけで`--apply-xmp-basic`を使ってください。

## 使い方

`import`は指定フォルダーの対応画像と、見つかった同名XMPサイドカーを出力フォルダーへコピーします。`--recursive`でサブフォルダーも検索し、元フォルダー名を出力名に付けて衝突を避けます。`--sort capture-time`はXMP/EXIF撮影日時順に並ぶ連番を付け、`--rename-by-date`は日時をファイル名に使います。同じ秒の撮影には連番を追加します。`--skip-jpeg`はJPEG/JPGを除外し、`--dry-run`はコピー予定だけを表示します。撮影日時順・日時リネームには全画像の撮影日時が必要です。`--convert-dng`を指定するとDNGLabが対応するRAWファイルを`.dng`へ変換し、隣接XMPサイドカーも新しい画像名に合わせてコピーします。JPEG等と既存DNGはそのままコピーします。圧縮方式は`--dng-compression lossless|uncompressed`で指定でき、既定はlosslessです。ロッシー圧縮とサイズ縮小は未対応です。DNGLabはPATHまたは`--dnglab PATH`で指定してください。

`import-video`はFFmpegで動画をJPGまたはPNGの連番へデコードします。既定ではデコード可能な全フレームを保存し、`--fps N`を指定するとその間隔でフレームをサンプリングします。JPEGの圧縮品質は`--quality 1`（高画質）〜`31`（低画質）で指定できます。抽出先は新しいフォルダーでなければならず、既存フォルダーを置き換えません。LRTimelapseの動画取り込みもJPG連番を生成しますが、lrtlではプレビュー生成、シーンごとの自動分割、動画フレームのDNG変換は未実装です。

`timeshift`は`--offset 2h30m`、`--offset=-45s`、`--offset 1d`のような固定時間差で連番の撮影日時をずらします。既定はプレビューで、`--write`を付けるとXMPサイドカーへ保存し、既存ファイルはバックアップします。対象は`DateTimeOriginal`、`DateTimeDigitized`、`CreateDate`で、`ModifyDate`と画像に埋め込まれたEXIF値は変更しません。サイドカーに撮影日時がなく、画像EXIFで読める場合は、ずらした`DateTimeOriginal`をサイドカーへ追加します。

```powershell
# 画像列、サイドカー、キーフレームを確認
lrtl scan "D:\Timelapse\Sequence"

# カメラカードからRAWだけを撮影日時順で取り込む
lrtl import "E:\DCIM" --output "D:\Timelapse\Sequence" --recursive --sort capture-time --skip-jpeg

# RAWをlossless DNGに変換し、XMPも一緒に取り込む（DNGLabが必要）
lrtl import "E:\DCIM" --output "D:\Timelapse\DNGSequence" --recursive --skip-jpeg --convert-dng

# 非圧縮DNGに変換。DNGLabをPATHに置かない場合は場所を指定
lrtl import "E:\DCIM" --output "D:\Timelapse\DNGUncompressed" --convert-dng --dng-compression uncompressed --dnglab "C:\Tools\dnglab.exe"

# 取り込み名も撮影日時へ変更し、実行前に内容を確認
lrtl import "E:\DCIM" --output "D:\Timelapse\SequenceByDate" --recursive --skip-jpeg --rename-by-date --dry-run

# 撮影日時を1時間戻す（プレビュー後、--writeでXMPに保存）
lrtl timeshift "D:\Timelapse\Sequence" --offset=-1h
lrtl timeshift "D:\Timelapse\Sequence" --offset=-1h --write

# 動画の全フレームをJPG連番へ展開（FFmpegが必要）
lrtl import-video "D:\Clips\source.mp4" --output "D:\Timelapse\ExtractedFrames"

# 2 fpsでPNGフレームを抽出
lrtl import-video "D:\Clips\source.mp4" --output "D:\Timelapse\SampledFrames" --fps 2 --format png

# 抽出した画像列をタイムラプス動画にする
lrtl render "D:\Timelapse\ExtractedFrames" --output "D:\Timelapse\result.mp4" --fps 24

# XMPサイドカーのスナップショットを作成・一覧表示
lrtl snapshot create "D:\Timelapse\Sequence" --name before-edit
lrtl snapshot list "D:\Timelapse\Sequence"

# 復元内容を確認し、明示して適用（既存XMPは既定でバックアップ）
lrtl snapshot restore "D:\Timelapse\Sequence" "D:\Timelapse\Sequence\.lrtl\snapshots\before-edit.zip"
lrtl snapshot restore "D:\Timelapse\Sequence" "D:\Timelapse\Sequence\.lrtl\snapshots\before-edit.zip" --write

# キーフレームの設定・解除（既存XMPは既定でバックアップ）
lrtl mark "D:\Timelapse\Developed\IMG_0005.jpg" --kind default
lrtl mark "D:\Timelapse\Developed\IMG_0012.jpg" --kind crop
lrtl unmark "D:\Timelapse\Developed\IMG_0005.jpg"

# 数値のCamera Raw設定をキーフレームのXMPに書き込む（既存XMPは既定でバックアップ）
lrtl set "D:\Timelapse\Developed\IMG_0005.jpg" crs:Exposure2012 -1.0
lrtl set "D:\Timelapse\Developed\IMG_0012.jpg" crs:Exposure2012 0.5

# キーフレーム間の数値設定をプレビューし、明示して書き込む
lrtl interpolate "D:\Timelapse\Developed"
lrtl interpolate "D:\Timelapse\Developed" --smoothness 50 --write

# 補間したCamera Raw基本設定を画素へ近似適用し、動画まで出力
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\transition.mp4" --apply-xmp-basic --apply-xmp-crop

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

# LRTから確認した数量式とフレーム配置で候補を提案（既定はプレビューのみ）
lrtl keyframes "D:\Timelapse\Developed"
# 数量を5に固定（最大20、端点を含む）
lrtl keyframes "D:\Timelapse\Developed" --count 5
lrtl keyframes "D:\Timelapse\Developed" --count 5 --write
# LRT Wizardと同じ5つ星キーフレームを提案
lrtl keyframes "D:\Timelapse\Developed" --rating 5 --count 5 --write
# LRT Wizard同様、既存4つ星を置き換えて衝突位置を調整
lrtl keyframes "D:\Timelapse\Developed" --rating 4 --count 5 --replace-existing --write
# RAW画像をデコードせず、数量5でLRT互換配置を使う
lrtl keyframes "D:\Timelapse\RAW" --count 5 --evenly-spaced --write

# 対応RAWの埋め込みプレビューの輝度曲線から候補を選ぶ
lrtl keyframes "D:\Timelapse\RAW" --raw-preview --count 5

# DNGLabでRAWを現像し、その輝度曲線から候補を選ぶ
lrtl keyframes "D:\Timelapse\RAW" --raw-develop --dnglab "C:\Tools\dnglab.exe" --apply-xmp-basic --apply-xmp-crop --count 5

# 長期連番を測定し、輝度・コントラスト・色相の範囲から選択する
lrtl long-term "D:\Timelapse\Developed" --output "D:\Timelapse\selection.json" `
  --luminance-range 0.01,0.9 --contrast-range 0.5,12 --hue-range 330,40

# LRTimelapse 7.5.5の長期測定手順に合わせた指標をJSONへ記録
lrtl long-term "D:\Timelapse\Developed" --output "D:\Timelapse\lrt-metrics.json" --metric-profile lrt-7.5.5

# 現像済みJPEG/PNG/TIFFから24 fpsのMJPEG AVIを作成（FFmpeg不要）
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.avi"

# 連番をフォルダーから探して並列レンダー（MP4、最大2ジョブ）
lrtl batch-render "D:\Sequences" --output "D:\Exports" --format mp4 --workers 2 --recursive -- --fps 24 --crf 18 --preset medium

# 長期フィルターで選択した画像だけをレンダー
lrtl render "D:\Timelapse\Developed" --selection "D:\Timelapse\selection.json" --output "D:\Timelapse\filtered.avi"

# 外部FFmpeg/libx264で24 fpsのH.264 MP4を作成
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.mp4" --fps 24 --crf 18 --preset medium

# 5枚の隣接フレームをLRT互換の重みで合成し、動きを滑らかにする
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\smooth.mp4" --motion-blur 5

# H.265 MP4またはProRes 422 / ProRes 422 HQ MOVを書き出す
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview-h265.mp4" --codec h265 --crf 20 --preset medium
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\master.mov" --codec prores
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\master-hq.mov" --codec prores-hq
# DNxHR HQX編集マスター、またはDNxHR 444
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\master-hqx.mov" --codec dnxhr-hqx
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\master-444.mov" --codec dnxhr-444

# 4K相当の長辺3840px以下へ縮小してレンダー（アスペクト比維持、拡大なし）
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview-4k.mp4" --long-edge 3840

# 16:9に中央クロップし、1920x1080でレンダー
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview-1080p.mp4" --aspect 16:9 --output-height 1080

# MP4へ音声ファイルを追加し、動画の長さに合わせて切り詰め/無音で補う
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview-music.mp4" --audio "D:\Audio\music.wav"

# 輝度・XMP露出・キーフレームをSVGグラフで確認
lrtl preview "D:\Timelapse\Developed" --output "D:\Timelapse\brightness.svg"

# 対応RAWの埋め込みプレビューからMJPEG AVIを作成（RAW現像ではない）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-preview.avi" --raw-preview

# EXIFの向きを適用して動画化（回転・鏡像を反映）
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\oriented.mp4" --auto-orient

# 対応RAWの埋め込みプレビューからH.264 MP4を作成（FFmpegが必要、RAW現像ではない）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-preview.mp4" --raw-preview

# DNGLabでRAWを現像してH.264 MP4を作成（FFmpegとDNGLabが必要）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-developed.mp4" --raw-develop --dnglab "C:\Tools\dnglab.exe"

# DNGLabでRAWを現像してMJPEG AVIを作成（DNGLabが必要、FFmpeg不要）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-developed.avi" --raw-develop --dnglab "C:\Tools\dnglab.exe"

# DNGLab現像画像から輝度グラフと簡易Deflicker補正を作成
lrtl preview "D:\Timelapse\RAW" --output "D:\Timelapse\raw-brightness.svg" --raw-develop --dnglab "C:\Tools\dnglab.exe" --apply-xmp-basic --apply-xmp-crop
lrtl deflicker "D:\Timelapse\RAW" --raw-develop --dnglab "C:\Tools\dnglab.exe" --apply-xmp-basic --apply-xmp-crop

# XMPサイドカーのExposure2012を適用してレンダー
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\adjusted.avi" --apply-xmp-exposure

# XMPの基本調整と簡易WBを近似適用してレンダー
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\xmp-basic.mp4" --apply-xmp-basic

# XMPサイドカーの回転を含むクロップアニメーションを適用
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\cropped.avi" --apply-xmp-crop

# EXIF/XMP撮影日時を各フレーム右下へ重ねて動画に焼き込む
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\dated.mp4" --timestamp-overlay

# 任意の透かし文字を左下へ焼き込む（5種類の位置から選択）
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\watermarked.mp4" --watermark "Kota Timelapse" --watermark-position bottom-left

# ファイル名にLEFTを含むフレームだけをレンダー（大文字小文字を区別しない）
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\left-camera.mp4" --filename-contains LEFT

# 各フレームの撮影日時を動画と同じFPSのSRT字幕として出力
lrtl subtitles "D:\Timelapse\Developed" --output "D:\Timelapse\dated.srt" --fps 24

# FPSと変換時のJPEG品質を指定
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.avi" --fps 30 --quality 95

# 指定領域で明るさを測り、近傍6フレームの変動を3パスで抑える
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\smooth.avi" `
  --deflicker --reference 200,100,600,300 --smooth 6 --deflicker-smoother lowess --passes 3

# 同じ解析を露出補正値としてXMPへプレビューし、書き込む
lrtl deflicker "D:\Timelapse\Developed" --reference 200,100,600,300 --smooth 6 --passes 3
lrtl deflicker "D:\Timelapse\Developed" --reference 200,100,600,300 --smooth 6 --smoother lowess --passes 3
lrtl deflicker "D:\Timelapse\Developed" --reference 200,100,600,300 --smooth 6 --passes 3 --write

# lrtlが記録したDeflicker補正だけをXMPから戻す
lrtl deflicker "D:\Timelapse\Developed" --reset
lrtl deflicker "D:\Timelapse\Developed" --reset --write

# 露出変化を残さず、一定の明るさへ寄せる（定常光のシーン向け）
lrtl render "D:\Timelapse\Studio" --output "D:\Timelapse\constant.avi" --constant
```

`render`は出力ファイルが既にある場合は停止します。置き換えるときは`--overwrite`を指定します。`--aspect W:H`はXMPクロップの後に画像中央を切り出し、指定比率に近い整数画素の画像を作ります。受け付ける比率は1:16〜16:1です。これはlrtl独自の中央クロップで、LRTのCompositionやクロップ選択UIとの数値一致は確認していません。`--output-height H`は縦横比を維持したまま出力高をHピクセルに設定し、拡大も行います。`--long-edge N`は元の長辺がNより大きいときだけ縮小し、縦横比を保って長辺をNピクセル以下にします。`--output-height`と`--long-edge`は同時に指定できません。両方とも16〜32768ピクセルを受け付けます。`--audio FILE`はMP4/MOVへ音声ファイルの最初の音声トラックをAAC 192 kbit/sで追加します。FFmpegが必要です。音声の開始時刻を0にし、動画より短い場合は末尾を無音で補い、長い場合は動画の長さで切ります。MJPEG AVIへの音声追加、複数トラック選択、音量調整は未対応です。AVIでは画素処理なしのJPEG入力を再圧縮せず格納し、それ以外の入力は指定品質のJPEGに変換します。TIFFの16-bit階調は8-bit出力時に失われます。MP4ではJPEG/PNG/TIFFをGoでデコードし、H.264エンコーダーにRGBフレームとして渡すため中間JPEG圧縮をしません。`--crf`はH.264品質（0–51、低いほど高品質）、`--preset`は速度と圧縮率の選択です。MP4の画素は8-bit YUV 4:2:0に変換され、奇数幅/高さは右端/下端をパディングします。`--reference`の形式は`x,y,width,height`で、指定しない場合は画面全体を測定します。`--smooth`は平滑化の半径（フレーム数）、`--passes`は補正反復数です。動く被写体や雲など輝度が変わる要素を避けた領域を指定してください。`--constant`は長期的な明るさの変化も抑えるため、照明条件が一定のシーンに向いています。

`--apply-xmp-exposure`は各画像に対応する外部XMPサイドカーから数値の`crs:Exposure2012`を読み、線形sRGB画素に露出倍率を適用します。±32 stopsを超える値や不正な数値はエラーにします。Camera Rawの全現像パイプラインと一致する機能ではなく、露出スライダーだけの近似適用です。JPEGはこの指定で再エンコードされます。

`--apply-xmp-basic`はExposure2012に加えてContrast2012、Highlights2012、Shadows2012、Whites2012、Blacks2012、Texture、Clarity2012、Dehaze、Sharpness/SharpenRadius/SharpenDetail/SharpenEdgeMasking/LuminanceSmoothing/LuminanceNoiseReductionDetail/LuminanceNoiseReductionContrast/ColorNoiseReduction/ColorNoiseReductionDetail/ColorNoiseReductionSmoothness、ParametricShadows/Darks/Lights/Highlights（-100〜100）と各分割位置（10〜70/20〜80/30〜90）、Vibrance、Saturation、Temperature、Tintを画像画素に近似適用します。Contrast2012/Highlights2012/Shadows2012/Whites2012/Blacks2012/Vibrance/Saturation/Texture/Clarity2012/Dehaze/Parametric*は-100〜100、LuminanceSmoothing/LuminanceNoiseReductionDetail/LuminanceNoiseReductionContrast/ColorNoiseReduction/ColorNoiseReductionDetail/ColorNoiseReductionSmoothnessは0〜100、Sharpnessは0〜150、SharpenRadiusは0.5〜3、SharpenDetail/SharpenEdgeMaskingは0〜100、Exposure2012は±32 stops、Temperatureは2000〜50000、Tintは-150〜150の範囲が必要です。`ToneCurvePV2012`とRGB別の明示ポイントカーブも読み、順序付きの整数点間をPCHIP補間した8-bit LUTとして適用します。表示済みsRGBの輝度選択マスク、LUT、彩度倍率による独立実装であり、Camera Rawのトーン応答や色管理を再現しません。Temperature/Tintは6500 Kを基準とする推定RGBゲインであり、センサーデータを使うCamera Rawのホワイトバランスとは一致しません。プリセット名だけのカーブは未対応です。パラメトリック領域調整と分割値は線形輝度上で独自近似します。ローカル補正は、線形/円形グラデーションの単一またはAdd/Intersect合成マスクに対するLocalExposure/Contrast/Highlights/Shadows/Whites/Blacks/Saturation/LocalTexture/LocalClarity2012/LocalDehaze/LocalSharpness/Temperature/Tint/LocalToningHue/LocalToningSaturationを近似適用します。CorrectionMasks内のMask/Aggregateは子要素へ展開し、MaskBlendMode=0をAdd、1をIntersectとして合成します。 XMPのLocalContrast2012/Highlights/Shadows/Whites/Blacks/Saturation/Texture/Clarity2012/Dehaze/Sharpness/Temperature/Tintは正規化小数を-100〜+100のスライダー値に換算し、LocalExposure2012はEV値のまま扱います。LocalToningSaturationは0〜1を0〜100へ換算します。Addはcoverage union式を使う独自近似で、Camera Rawとの数値一致は未確認です。LocalToningHue/LocalToningSaturationは元画素のHSL明度を保った色味をサチュレーション値とマスク濃度に応じて混ぜる独自近似です。単一マスク要素の反転は現行MaskInvertedと旧形式Flippedに対応し、両方有効なら二重反転として処理します。マスクはクロップ前の向き適用済み座標で評価します。Lightroom/Camera Rawで現像値を焼き込み済みの画像では同じ値を二重適用しないでください。`--apply-xmp-exposure`とは同時指定できません。JPEG/PNG/TIFFは再エンコードし、元画像とXMPは変更しません。

`--apply-xmp-crop`は各サイドカーの`crs:HasCrop`、`CropTop/Left/Bottom/Right`、`CropAngle`を読み、回転した矩形クロップを適用して入力と同じ解像度に再サンプルします。クロップのないフレームはそのままです。空または範囲外の矩形はエラーにします。Camera RawのGeometry/遠近補正は再現しません。Lightroom等でキーフレームのクロップ値を保存し、`interpolate --write`で間を埋めてからこのオプションでレンダーできます。

`--auto-orient`は入力画像、RAW内蔵プレビュー、またはDNGLab出力のEXIF Orientationを読み、回転と鏡像を画素へ適用してからXMPクロップ、色調補正、Deflicker、リサイズを行います。向き情報がないフレームはそのままです。向きを適用した後の全フレームは同じ幅と高さである必要があります。この処理は入力ファイルとメタデータを変更しません。

`--motion-blur N`は出力前に時間方向へ隣接フレームを合成します。選べる窓幅はLRTのRenderPresetで確認した2、3、4、5、10、15、20枚で、0は無効です。インストール済みLRTのレンダー引数生成処理から、`tmix=frames=N:weights='...'`と各重み`(i/N)^1.5`（`i=1..N`、小数3桁）を確認しました。重みは古いフレームから現在フレームへ向かって増え、FFmpegの既定スケールで合計1に正規化されます。各出力フレームは現在と過去N-1枚から作り、先頭では最初のフレームを複製して窓を満たします。XMP/露出補正、クロップ、リサイズを先に行い、指定時刻の文字焼き込みは合成後に行います。lrtlはPure Goの8-bit RGBで同じ重みと時間窓を計算します。LRTのレンダー経路は10-bit YUV444でフィルター処理するため、数値上の重みと窓の一致は確認済みですが、画素値の完全一致は未確認です。一時フレームバッファーの推定量が768 MiBを超える場合は処理を開始しません。大きなフレームでは`--long-edge`または窓幅を下げてください。

`--timestamp-overlay`と`subtitles`は、各サイドカーの`exif:DateTimeOriginal`、`xmp:CreateDate`、`xmp:ModifyDate`の順で日時を探し、なければ画像内EXIFの撮影/作成/更新日時を使います。全フレームの日時が必要です。`subtitles`は各画像の撮影日時を、そのフレームが動画上に表示される区間のSRT字幕として書き出します。`--fps`は動画のフレームレートと合わせてください。既定の表示形式は`2006-01-02 15:04:05`で、`--timestamp-format`へGoの日時レイアウトを渡して変更できます。`render --timestamp-overlay`は日時を右下に焼き込みます。`render --watermark TEXT`は埋め込みGoフォントで固定テキストを重ね、`--watermark-position top-left|top-right|bottom-left|bottom-right|center`で配置を選べます（既定は左下）。両方を指定すると日時と透かしを同じフレームに表示します。

`deflicker`はJPEG/PNG/TIFF、`--raw-preview`の内蔵画像、または`--raw-develop`でDNGLabが現像した画像の輝度を線形sRGBへ変換して測定し、指定領域の中央値に平滑化または一定輝度の目標曲線を合わせます。既定の`--smoother gaussian`はGaussian平滑化を使い、`--smoother lowess`はtricube重み付き局所線形回帰を使います。LOWESSの近傍数は`--smooth`の半径から`2×半径+1`へ換算します。LRT 7.5.5のVisual Deflickerも局所線形回帰型スムーザーを呼びますが、lrtlの測光、近傍数、補正パスは独自実装です。`--apply-xmp-basic`（または`--apply-xmp-exposure`）と`--apply-xmp-crop`を指定すると、解析前にサイドカーの値を画素へ近似適用します。既定はプレビューだけです。`--write`は求めた補正量を各フレームの`crs:Exposure2012`へ加算し、前回lrtlが加えた量を`ol:DeflickerCorrection2012`に記録します。再実行ではlrtl管理分を置き換え、`--reset --write`ではその分だけを戻します。Holy Grail補正やユーザーが後から加えたExposure値は差し引かずに残します。Camera Rawの現像パイプラインやLRTimelapseのVisual Deflickerと数値が一致する機能ではなく、画像列から露出差を推定する近似です。

`set`は`NumericCRSProperties`にある数値のCamera Rawプロパティ131個を明示的に許可し、HSL色別スライダー、旧式のClarity/Fill Light/Highlight Recovery/増分WB、HDR最大値、カラーグレーディング、色別色相境界、パラメトリックトーン、ノイズ低減、シャープ化、粒子、周辺光量、手動レンズ/パースペクティブ補正などを含みます。名前はAdobeの公開XMP資料、インストール済みXMPテンプレート、LRT 7.5.5の登録情報から選びましたが、LRTのAuto-Transition全項目を網羅すると確認したものではありません。`interpolate`は全体および登録済みローカル数値項目をLRT 7.5.5の3次ベジェスプラインで補間し、`--smoothness 0..100`でLRTの設定値を指定できます（既定50）。登録数値は各プロパティのLRT範囲でクランプし、Integer型はJava互換の四捨五入（.5はゼロから遠ざける）で出力します。先頭フレームのIsMergedHDRがtrueならExposure2012の範囲を-10..10へ広げ、通常は-5..5です。LRT登録外の旧式4項目とColorGradeBalance/ColorGradeHighlightHue/Sat/ColorGradeShadowHue/Satは補間しません。全体プロパティはXMP上の数値を補間し、色相の最短回転はしません。ローカル補正はRDF Seq内の固定位置で照合し、Linear 1–4、Circular 1–2、Brush 1–2、MaskGroup 1–4/8–11を対象に各CorrectionMasks[1]の登録値を補間します。Angleは円形グラデーションとMaskGroup 8/9だけLRTの後処理どおり−45°未満なら90°加算、45°超なら90°減算し、キーフレームにも適用します。`LumRange`は登録上String型のため同じ配列位置の最初の非空値を全フレームへ均等化します。RDF配列がない独自XMPでは共通`CorrectionSyncID`/`MaskSyncID`にフォールバックします。色範囲Type=1ではColorAmountを数値補間し、`PointModels`はLRTの遷移登録に見つからないため各フレームの値を保持します。Depthの画像依存データ、ブラシ描線、登録位置外のマスクは未対応です。MaskGroup 1–4/8–11のMainCurve/RedCurve/GreenCurve/BlueCurveは先頭フレームから同じ位置の対象フレームへコピーしますが、曲線の数値補間は未対応です。非キーフレームで欠けた登録位置のMaskGroup構造は最寄りの実体を持つXMPから複製して作成しますが、キーフレームや登録位置外/Sync ID形式の欠損構造は生成しません。Look/Preset/PointColorsとグローバルポイントカーブは先頭フレームからコピーしますが、前者3項目のコピー元はLRT資料で確認できていません。数値一致はLRT 7.5.5との比較前です。`mark`でキーフレームを設定し、`set`でキーフレームの値を入力し、`interpolate --smoothness 50 --write`（LRTの既定スムージング）で間のフレームを補間してから、JPEG/PNG/TIFFでは`render --apply-xmp-basic`で基本8項目、Texture/Clarity/Dehaze/Sharpening/NoiseReduction、4領域パラメトリックカーブ、8色域HSL、簡易Temperature/Tint、明示ポイントカーブを、`render --apply-xmp-crop`で回転を含むクロップを画素へ適用できます。既定では既存XMPをバックアップし、`--dry-run`で書き込み内容を確認、`--no-backup`でバックアップを省略できます。Camera Rawの現像エンジンは含まず、レンダー処理は独自近似です。`set`はCamera Rawスライダーごとの許容範囲を検証しません。

`keyframes`はJPEG/PNG/TIFFの測定輝度レンジからLRT 7.5.5で観察した数量式を既定数量（最大20）に使い、同版の整数間隔で候補位置を選びます。数量は最大20で、LRTのヘルパーと同じく1個なら先頭、2個以上なら先頭と末尾を含む配置です。JPEG/PNG/TIFF、対応RAWの`--raw-preview`、または`--raw-develop`の現像画像から数量を求めます。`--rating 4|5`で書き込む星評価を指定でき、既定はLRT Wizardと同じ4つ星です。既定数量からは既存の選択中と同じ評価だけを差し引きます。`--apply-xmp-basic`（`--apply-xmp-exposure`と排他的）/`--apply-xmp-crop`は測定輝度へ反映します。`--evenly-spaced`は既定数量5で測定と画像デコードを省略し、同じ配置ルールを使います。既定動作では候補が既存4*/5*/2*/3*マークに重なるとそのマークを残して候補から外します。`--replace-existing`を指定すると選択中と同じ星評価を先に削除し、別の2〜5つ星が候補位置にある場合は最大9枚前の空きフレームへ移します。`--write`と併用すると各XMPをバックアップ付きで更新します。明るさの曲がりが大きい位置を優先する以前のlrtl独自方式は`--luminance-deviation`で選べます。LRTの選択行UIと内部の現像輝度値との一致は未対応です。

`unmark --force-rating`はlrtl以外が設定した4つ星/5つ星評価にも使えます。その評価を削除するため、評価がキーフレームマーカーとして使われている場合に限ってください。

`snapshot create`は画像をコピーせず、連番に紐づくXMPサイドカーを`.lrtl/snapshots/<name>.zip`へ保存します。名前を省略するとUTC時刻から自動生成します。`snapshot list`はこのフォルダーのアーカイブを検証して一覧表示します。`snapshot restore`は既定で書き込み内容をプレビューし、`--write`を付けるとアーカイブ内のサイドカーだけを復元します。異なる場所へ移動した連番には`--allow-moved`が必要です。置き換える既存XMPは`.lrtl-snapshot-*.bak`としてバックアップします。スナップショット作成後に追加されたXMPサイドカーは削除しません。移動や削除を含むLRTのシーケンス状態スナップショットとは異なる、XMP限定の復元です。

`holy-grail`は各フレームのEXIFから露出設定値`log2(露出時間 × ISO / F値²)`を求め、先頭フレームとの差を打ち消す補正量として`crs:Exposure2012`に加えます。対応形式はJPEG、TIFF、CR2、CR3、DNG、RW2です。全フレームに有効な露出時間とISOが必要です。F値がない場合は`--ignore-aperture`を指定し、絞りが変化しなかったことを確認してください。`--stretch`はEXIF露出値を最小・最大で正規化した曲線のスケール（-1.5〜+1.5）、`--rotate`はシーケンスに加える直線傾き（-4〜+4）です。`--auto-shape`はLRTで確認したStretch/Rotateの探索刻み・L1評価と、インストール済みアプリから逆解析した三次カーネル重み付き局所線形回帰（LOWESS）を使って値を提案します。EXIF由来の入力曲線などHoly Grailの上流処理は独自の近似です。いずれも指定しない場合は従来のEXIF露出差補正を使います。EXIFが撮影モードをManualと報告する場合、設定変更位置に2*/3*評価のペアを提案します。撮影モードが不明または自動の場合はペアを作らず、`--mark-jumps`で強制できます。設定変更が連続して隣接ペアを作れない場合や、既存評価と衝突する場合は書き込み前にエラーにします。`--no-mark-jumps`でペア付けを無効化できます。補正の再実行では前回分を置き換え、lrtl管理下の元評価も記録するため`--reset --write`で復元できます。既定はプレビューで、書き込み時は既存サイドカーをバックアップします。

LRTimelapse側では2*/3*が内部HGキーフレームで、4*/5*の編集キーフレームとは別レイヤーとして使われます。lrtlはこの評価マーカーを互換目的で扱いますが、補正値は現時点で通常の`crs:Exposure2012`に書き込むため、LRTの独立した背景補正レイヤーと同じ動作ではありません。LRTのWizardにはRotate（-4〜+4）、Stretch（-1.5〜+1.5）、`holyGrailReduceDeflicker`を使う調整・探索があります。lrtlの`--stretch`と`--rotate`はEXIF曲線に調整を加え、`--auto-shape`は確認できた2段階L1探索とLOWESS平滑化を使います。Holy Grailの上流曲線生成、Lightroom編集値との合成、独立補正レイヤーは未実装です。逆解析で確認した式と探索範囲は[リバースエンジニアリングノート](docs/reverse-engineering.md)に記録しています。

この計算は記録されたカメラ設定による露出差を補う近似です。LRTimelapse公式ワークフローのHoly Grail Wizardも撮影中に変更したシャッター速度/ISOの補正曲線を作ります。lrtlの曲線回転・伸縮はEXIFから作った曲線への追加調整です。`--auto-shape`は確認できた評価式とLOWESS平滑化を使いますが、入力曲線やLRTの独立補正レイヤーは再現していません。`preview`は静的な輝度グラフを出しますが、Lightroomの現像画像プレビューは未実装です。センサー感度の誤差、レンズ透過率、自然な明るさの推移も解析しません。RAW現像やXMPの画素反映をする機能ではありません。

## 今後の実装順

| 段階 | 目標 | 状況 |
|---|---|---|
| 1 | XMPサイドカー、キーフレーム、数値設定の補間 | 手動設定、輝度ベース候補提案、限定的な数値補間を実装 |
| 2 | 画像連番から最初の動画を生成 | JPEG/PNG/TIFFと一部RAWの埋め込みプレビューからMJPEG AVI、外部FFmpeg経由でH.264/H.265 MP4、ProResとDNxHR MOVに対応。`--long-edge`で縮小可能。完全なRAW現像は未実装 |
| 3 | 画像解析、プレビュー、Deflicker | 静的SVGの輝度/露出グラフ、単一参照領域・全体露出補正のレンダー時またはXMP適用、回転を含むXMPクロップを実装。LRT形式の画像付きVisual Preview、インタラクティブ曲線UI、XMPロスレス適用、Geometry補正、Add/Intersect以外の複合マスク、色/深度/旧形式の範囲マスク、ブラシ/AIの画素現像は未実装 |
| 4 | RAW現像、Holy Grail、XMP現像値の画素への反映 | EXIF設定ベースのHoly Grail露出補正、現像済みラスターへのExposure2012近似適用、一部RAW内蔵プレビュー、外部DNGLabによるRAW現像を実装。Camera Raw現像の完全再現と他のXMP値のRAW画素への反映は未実装 |
| 5 | ProRes/H.265等の追加コーデック、オーバーレイ、取り込み、GUI | H.265/ProRes/DNxHR、日時オーバーレイ、写真フォルダーからの画像/XMP取り込み、対応RAWのlossless/uncompressed DNG変換、動画からJPG/PNGフレーム抽出、Goによる時間方向フレーム平均を実装。動画プレビュー・自動分割、ロッシーDNG・サイズ縮小、LRT互換のモーションブラー重み、GUIは未着手 |

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
- [LRTimelapse 7.5.4 release notes: Import from Video](https://lrtimelapse.com/updates/lrtimelapse-7-5-4/)
- [LRTimelapse 7.2 release notes: Simple Deflicker](https://lrtimelapse.com/updates/lrtimelapse-7-2/)
- [LRTimelapse 7.1 release notes: XMP equalization on Auto-Transition](https://lrtimelapse.com/news/lrtimelapse-7-1-released/)
- [LRT developer guidance on animating initialized masks between keyframes](https://forum.lrtimelapse.com/Thread-masks-on-keyframes-tip-on-adjusting-exposure-of-the-moon)
- [LRTimelapse discussion with an XMP example of intersected masks](https://forum.lrtimelapse.com/Thread-issue-with-luminance-range-mask-intersected-with-linear-gradient-mask?pid=57463)
- [Adobe Camera Raw XMP sample with HSL color adjustments](https://github.com/AdobeDocs/cis-photoshop-api-docs/blob/main/sample-code/lr-sample-app/crs.xml)
- [Adobe Camera Raw XMP namespace reference](https://github.com/adobe/xmp-docs/blob/master/XMPNamespaces/crs.md)
- [Adobe Lightroom Classic local-adjustment controls](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/apply-local-adjustments.html)
- [Adobe Community XMP sample showing fractional local correction values](https://community.adobe.com/questions-675/syncing-range-masks-in-lr-classic-7-3-933460)
- [Adobe Lightroom Classic masking guide](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/masking.html)
- [Adobe Community SDK guide to Lightroom crop coordinates and CropAngle](https://community.adobe.com/questions-675/sdk-computing-the-corners-of-a-crop-rectangle-962108)
- [AVI/MJPEG writer for Go](https://github.com/icza/mjpeg)
- [FFmpeg documentation: image sequence extraction](https://ffmpeg.org/ffmpeg.html)
- [FFmpeg DNxHR profiles](https://ffmpeg.org/doxygen/trunk/profiles_8c.html) and [DNxHD/DNxHR encoder pixel formats](https://ffmpeg.org/doxygen/8.1/dnxhdenc_8c_source.html)
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
