# LumaFrame (`lrtl`)

LRTimelapseと同じ作業を段階的に実現することを目指す、独立したオープンソースのタイムラプスツールです。Lightroom互換XMPのキーフレーム編集やHoly Grail露出補正に加え、現像済みJPEG/PNG/TIFFとRAW連番からMJPEG AVI、H.264/H.265 MP4、ProRes MOVを生成できます。

**RAW連番は、インストール済みのAdobe DNG Converterでデモザイクし、Goで現像して動画にできます。** `render --raw-develop`はConverterを自動検出し、実画素のLinear DNGを16-bit sRGBへ変換します。JPEG/PNG/TIFF連番とMJPEG AVIの処理はPure Go、MP4/MOV出力にはFFmpegも必要です。`--dnglab PATH`で従来のDNGLabバックエンドも選べます。Camera Raw/Lightroomのプロファイル・全設定と画素一致する現像エンジンではありません。

本プロジェクトはLRTimelapseおよびAdobeと無関係です。独自実装であり、それらのコード、画像、同梱ツールを含みません。

## 開発方針

**CLIでLRTimelapseの処理とワークフローに近づけることを優先します。GUIの追加開発は対象外です。** 数値処理は実際のLRT 7.5.5で得た観測値との比較を増やし、対応状況と未確認の範囲をこのREADMEに記録します。

### 今回の互換性改善

- `curve IMAGE_OR_XMP|DIRECTORY`でマスター／RGBのポイントカーブをCLIから確認・編集できます。`--mask 'MaskGroupBasedCorrections[1]'`で既存の局所補正にも対応します。JSON表示、4つ星マーク、直線へ戻す操作、dry-run、バックアップと連番全体の事前検証を備え、全体／局所カーブの編集→均等化→AVI画素までテストしています。

- 登録位置の局所補正・マスク形状にも数値の欠損補完を追加しました。既存の補正／セレクター内の欠けた露出・座標などを補完してから4つ星で補間し、欠けたキーフレーム項目にも保存します。実LRTの局所露出列9ケースと比較し、ID・有効フラグの保持、角度処理、バックアップ、動画画素までテストしています。要素自体がないキーフレームやSync ID独自形式の事前補完は未対応です。

- 数値XMPの欠損補完を補間前に追加しました。値がないフレームを前方へ引き継いでから先頭の欠損を後方から埋めるため、値が欠けた4/5つ星キーフレームも補完後の値で補間できます。実LRTの欠損ヘルパー9ケースと比較済みです。Temperature/Tint/IncrementalTemperature/IncrementalTintは事前補完から除外し、不正な数値は書き込み前にエラーにします。

- 4つ星と5つ星の補間アンカーを分離しました。通常の現像値・局所補正は4つ星、CropTop/Left/Bottom/Right/Angleは5つ星があれば5つ星、なければ4つ星を使います。CLIにDevelop/Cropそれぞれのアンカーを表示します。通常の4つ星がない場合、通常現像は先頭画像の値を使います。
- 端点2つだけの数値補間を、実LRTの倍精度の直線経路へ修正しました。実LRT部品の20ケースと比較し、負の整数丸め・長さ3〜101・精密な小数を回帰テスト化。単一参照の全体値・局所値もfloat32へ丸めずコピーします。構造化XMPと登録MaskGroupカーブのコピー元も、最初の4つ星（なければ先頭）へ修正しました。

- Auto-Transitionの全体スカラー処理を追加しました。登録済みの文字列22項目・真偽値4項目を、参照フレーム・4つ星キーフレーム数・端点の分岐に従ってコピー／欠損補完／均等化します。参照に値がない場合の削除、Custom WBの均等化、As Shot時のWB数値解除も扱います。`interpolate`の既定プレビューで変更・削除対象を確認できます。実LRTのヘルパー12ケースと比較し、呼び出し元の分岐・XMP保持・バックアップも回帰テスト化しました。

- `crop IMAGE_OR_XMP|DIRECTORY`で構図をCLIから確認・編集できます。`--rect L,T,R,B`、`--angle`、`--enable`、`--clear`に対応し、単一画像では`--mark`で5つ星クロップキーフレームも同時に保存できます。`--dry-run`、JSON表示、既存XMPバックアップ、連番全体の事前検証に対応します。

- `deflicker --passes N`と`render --deflicker --passes N`は、補正した画素を毎パス再測定します。XMP基本現像を使う場合はトーン処理も再計算し、16-bitの測光精度を保持します。XMPは計算完了後の最終結果だけを書き込みます。RAW現像結果は一時ファイルに最大512 MiBまでキャッシュし、上限を超えるフレームは再現像します。
- Deflickerのテストに、非線形XMPでの収束、出力AVIの実画素、最終XMPとバックアップ、途中失敗時の保持、CorrectionAmount、16-bit測光、RAWキャッシュの上限と後始末を追加しました。測光・70%の補正応答・現像エンジンは独自実装で、LRT/Camera Rawとの画素一致は未対応です。

- `masks IMAGE [--json]`で補正/マスクのXMPパスと現在値を確認でき、`set IMAGE 'MaskGroupBasedCorrections[1].LocalExposure2012' 0.5`の形式で局所値・形状・Active/反転フラグをCLIから編集できます。既定でバックアップし、`--dry-run`にも対応します。HG/Deflicker/Globalなどの内部補正はこの編集経路では読み取り専用です。

- `init DIRECTORY --write`で新しい連番の補正スロットを準備できます。ModernはMaskGroup 1〜9、LegacyはGradient 1〜7＋Circular 1〜2を生成し、5/6/7番目をHG/Deflicker/Global用として分離します。既存の編集値・キーフレーム・未知のメタデータを保持し、予約位置に別の補正がある場合は全フレームの書き込み前に競合を報告します。

- Auto-Transitionのスプライン部品を実LRT 7.5.5へ入力した27ケース（5/17/101フレーム、3/4アンカー、smoothness 0/50/100）と比較し、単精度の演算順序を修正しました。整数11ケースと4桁丸め13ケースも実LRTのヘルパーと比較しています。全ワークフローやXMP出力全体の一致を示すものではありません。
- LRTの数値ワーカーで確認した後処理を追加しました。アンカー間隔が`max(2, floor(連番長×4%))`未満なら直線にし、同じ値の区間（小数値は4桁のHALF_EVEN丸めで比較）は平坦にします。全体値と登録マスク値に適用し、キーフレームを保持します。
- `interpolate`がサイドカーなしの画像内XMPを読み落とす不具合と、`--smoothness 50 --write`の引数解析を修正しました。画像内キーフレームから補間し、未知のRDF値を保ったサイドカーを作成できます。元画像は変更しません。

- MP4/MOVのFFmpeg入力を`rgb48le`（RGB各16-bit）へ変更しました。16-bit TIFF/PNG/RAWの階調を、向き・クロップ・リサイズ・露出のみのXMP補正・Deflickerの画素ゲイン・Motion Blur・文字焼き込みを通して保持できます。実FFmpegのProRes HQ往復で、合成グレー階調に877種類の10-bit出力値が残ることを確認しました。対応済みXMP基本現像の全体/局所トーン・色・カーブ・空間フィルターも16-bit画素で処理します。XMP調整後の実ProRes HQ往復では693種類のグレー階調を確認しました。HDRやLRTの色管理との一致は未対応です。

- Holy GrailのLOWESSについて、実際のLRTへ合成数値列を入力した53ケースを回帰テスト化。1〜2001フレーム、端点、通常帯域幅とAuto Shapeの1.5設定を比較し、長い連番のフォールバック処理を修正しました。これは平滑化部品の比較で、Holy Grail全体や現像結果の一致を示すものではありません。
- `deflicker --write`は、既存の`#LRT internal use (Deflicker)`スロット、または署名を確認できる旧形式の`GradientBasedCorrections[6]`へ補正を書きます。識別できるスロットがない場合は`Exposure2012`を使います。HGとは別に保存先と補正量を記録し、リセットや旧版の全体露出補正からの移行に対応します。
- `deflicker --apply-xmp-basic --write`または`--apply-xmp-exposure --write`を再実行すると、現像済み入力の残差を既存の補正量へ加算します。XMPを適用しない解析では従来どおり前回分を置換します。LRTのVisual Deflickerと測光・現像結果が一致するという意味ではありません。
- HG/Deflickerの管理スロットは、元から存在する露出オフセット、CorrectionActive/Amountも考慮してレンダーへ一度だけ適用します。`--reset`はユーザーが後から加えた露出変更を残して管理補正だけを取り除きます。

## 実装状況

下表はローカルにインストールされたLRTimelapse 7.5.5とlrtlの現行コードを照合したものです。LRT側の主要機能とライセンス別の提供範囲は[公式機能表](https://lrtimelapse.com/features/)、[基本チュートリアル](https://lrtimelapse.com/tutorial/basic/complete/)、[内部ワークフロー](https://lrtimelapse.com/workflow/internal-workflow/)と、インストール済みアプリの各メニューを2026-10-16に確認しました。「未実装」は今後の目標です。LRTimelapseのLightroom/Camera Raw連携は外部ソフトを使うワークフローです。

状態の「一部」は該当機能の一部を実装済みですが、LRTと同等ではないことを示します。「未実装」は対応コマンドや処理がありません。「対象外」はLRTのライセンス区分にある制限をOSS版では再現しない項目です。

### 到達度の要約

- **単独で完了できる作業:** JPEG/PNG/TIFFの連番を並べ、追加ソフトなしでMJPEG AVIを生成できます。キーフレームと数値XMPの編集・補間、連番プレビュー、Deflicker、クロップ、字幕などもCLIで利用できます。
- **LRT相当を目指して一部実装:** 画像列からの自動キーフレーム提案、Holy Grail補正、XMP値の画像適用、GUIでのフレーム選択・編集、MP4/MOV出力、RAW対応。これらは個々の経路が使えますが、LRTとの操作性・演算結果・対応形式の完全一致は確認できていません。
- **到達していない範囲:** Camera Raw/Lightroomと同等のRAW現像、HDR処理、LRT Visual Previewと同等のプレビュー、未対応のマスク形式やGeometry補正は未実装または近似です。詳しい境界は下表と各コマンドの説明を参照してください。

下表の「一部」はLRTLの基本処理が未完成という意味ではなく、LRTの対応機能全体との比較です。たとえば画像連番からの基本動画生成は達成済みですが、LRTの全コーデック・色深度・HDRとの同等性はありません。

| 機能領域 | LRTimelapseの機能 | lrtlの状況 | 状態 |
|---|---|---|---|
| シーケンス規模 | 評価版は400枚まで、Private/Proはサイズ制限なし | 画像列の枚数上限なし。AVIは約4 GBまで。MP4はFFmpeg対応 | 一部 |
| ワークフロー | Basic、Visual、Long-Term、Lightroom/ACR連携、内部編集/Export | CLIで個別機能を実行。ワークフロー状態や内蔵現像画面なし | 一部 |
| キーフレーム | 輝度推移からキーフレーム数の目安を提示し、スライダーで調整するWizard。4/5つ星を選択でき、手動設定と等間隔配置も可能 | 手動マーク、JPEG/PNG/TIFF、`--raw-preview`内蔵プレビュー、または`--raw-develop`現像RAWを使う`keyframes`を実装。既定数量は、`set IMAGE PreviewEV VALUE`またはGUIで編集できるLRT値`lrt:ExternalExposureDefault / 4`が全フレームで有効なら使い、値が揃わない場合は画像輝度へフォールバックして、LRT 7.5.5から復元した輝度レンジ・連番長・2*/3*マーカーの式で算出。候補位置も同版の整数間隔・端点選択ルールに対応。GUIでは候補一覧から保存対象を選べる。`--rating 4|5`でLRT Wizardと同じ評価を選び、既定は4つ星。既存マーカーは既定で保持し、選択中の評価だけ数量推定から差し引く。`--replace-existing`では同じ評価を連番から削除し、他の2〜5つ星との衝突を前方9枚以内の空きフレームへ移す。`--evenly-spaced`は輝度解析なしで利用可能。`--luminance-deviation`はlrtl独自配置法。`--apply-xmp-basic`（または`--apply-xmp-exposure`）と`--apply-xmp-crop`を解析に反映できる。`preview-ev --write`で復元したヒストグラム測光値をXMPへ生成できる。LRT本体のプレビュー縮小/ROIと同じ画像入力、および数量スライダーは未対応 | 一部 |
| Holy Grail | Mモードで変化させた露出やISOを補正して昼夜遷移をつなぐ | JPEGおよびTIFFベース画像/RAW（依存ライブラリの実RAWサンプルでNEF/ARWも確認）、CR2/CR3/DNG/RW2のEXIF露出設定から独自の補正を計算。`--lrt-input`で参照する画像内XMPはJPEG/TIFF/DNG/CR2/RW2に加えてNEF/NRW/ARW/SR2/PEF/ERF/ORF/DCRからもタグ700を読み、CR3はAdobe XMP UUID boxから読む。FNumberがない場合は`lrt:Aperture`を参照。`--lrt-input`では既存の`lrt:ExternalExposureDefault`を1/4して読み、LRT同様に最大10件のフレーム差平均と2*/3*/4*隣接ペア補正を適用する近似経路も選択可能。`--lrt-reduce-deflicker`で0〜1の平滑化設定を指定できる。両経路で`--stretch`/`--rotate`とL1評価の`--auto-shape`に対応。LRT内部の画像入力生成は未再現。LOWESS単体は実LRTの53ケースで比較済み。HG専用LocalExposureスロットを優先し、識別できない場合は`crs:Exposure2012`へ書き込む | 一部 |
| 構図・クロップ | Composition作成、キーフレーム間のクロップアニメーション | `crop`で単一画像/XMPまたは連番全体の構図をCLIから確認・編集。矩形と角度、HasCropの有効化、解除、単一画像の5つ星crop markをバックアップ付きで保存できる。`interpolate`は数値をキーフレーム間で補間し、`render --apply-xmp-crop`で回転矩形を適用して入力と同じ解像度へ再サンプル。GUIのドラッグ編集も既存機能として残る。Camera RawのGeometry/遠近補正、回転後の自動最大矩形計算は未対応 | 一部 |
| レンズ情報 | マニュアルレンズのLens Tagger | `lens-tag --aperture f/8`とGUIで連番全体へLRT名前空間の`lrt:Aperture`を設定。EXIFのFNumberが欠けている場合、`holy-grail`がこのXMP値を絞りとして使う。標準EXIFや画像内XMPは変更せず、外部XMPサイドカーのみ | 一部 |
| 連番管理・選択 | ファイル名/キーフレーム/Visual Previewによる選択、フィルター、反転、連番内の移動・コピー・除去・削除、フォルダー作成とリネーム | `scan`と`gui`で自然順に連番を一覧表示。GUIはページ送りできる画像サムネイルを表示し、4/5つ星キーフレームの設定・解除と、輝度分析で候補一覧を表示し、選んだ候補だけ保存できる。`render --filename-contains TEXT`で名前の絞り込みに対応。反転選択、画像のコピー/移動/除去/削除、フォルダー作成、リネームは未対応 | 一部 |
| メタデータ管理 | EXIF再読込、メタデータの初期化/消去、連番全体のコピー/貼付、撮影日時シフト、レンズタグ付け | EXIF読込、JPEG画像内XMP（標準/拡張APP1形式）、TIFF/DNG/CR2/RW2/NEF/NRW/ARW/SR2/PEF/ERF/ORF/DCR画像内XMP（tag 700）、RAF内埋め込みJPEGのXMP、およびCR3画像内XMP（Adobe UUID box）の読込、XMP数値値の個別編集、`init`による不足マスク/内部補正スロットの初期化、`timeshift`によるXMP撮影日時の連番一括シフト、`lens-tag`による連番一括絞り設定に対応。画像内XMPを読むのは外部サイドカーがない場合で、編集結果はXMPサイドカーへ保存し画像本体は変更しない。その他のRAWコンテナー内XMP、メタデータ全消去/リセットや全設定のコピー/貼付は未対応 | 一部 |
| XMP・現像値 | Lightroom/Camera Raw等とのXMP連携、現像パラメーターの遷移 | 外部XMPを読み書きし未知のRDF値を保持。JPEGは外部サイドカーがなければ画像内の標準/拡張XMP、TIFF/DNG/CR2/RW2/NEF/NRW/ARW/SR2/PEF/ERF/ORF/DCRはtag 700、RAFは埋め込みJPEG、CR3はISO BMFFのAdobe XMP UUIDから読み取る。編集結果はサイドカーへ保存し画像を変更しない。`set`で許可した131項目とPreview EVを選択フレーム単位で編集し、局所補正/マスクは`masks`で確認した配列パスを`set`へ渡して編集できます。`preview-ev`でRGBヒストグラムからPreview EVを算出してXMPへ書き込める。CLIの`curve`で全体／局所のマスター/R/G/Bポイントを単一画像や連番へ編集し、属性/単純テキスト/RDF Seq表現をバックアップ付きで保存。全体設定とLRTの位置登録スロットにあるローカル補正・基本マスク形状をキーフレーム間で補間。sRGB入力のレンダーでは`--apply-xmp-basic`で全体8項目、Texture/Clarity/Dehaze/Sharpening/NoiseReduction/Vignette、4領域パラメトリックカーブ、HSL 8色域の色相/彩度/輝度、Temperature/Tint、全体ColorGrade 4領域の色調、明示されたマスター/RGBポイントカーブに加え、線形/円形グラデーションと現行形式の輝度範囲コンポーネントのAdd/Intersect合成（`MaskGroupBasedCorrections`/`Mask/Aggregate`、MaskBlendMode=0/1、MaskInverted/Flipped対応）に対するLocalExposure/Contrast/Highlights/Shadows/Whites/Blacks/Saturation/LocalTexture/LocalClarity2012/LocalDehaze/LocalSharpness/Temperature/Tint/LocalToningHue/LocalToningSaturation/LocalColorGradeとローカルMainCurve/RedCurve/GreenCurve/BlueCurveを独自近似で画素反映し、クロップも任意適用。ローカルカーブはマスク濃度で16-bit PCHIPカーブ出力を混ぜる近似で、LocalCurveRefineSaturation、ExtendedMainCurveなどのHDR曲線、キーフレーム間の曲線補間は未対応。色範囲Type=1はColorAmountを数値スプラインで遷移し、PointModelsはフレームごとに保持する。レンダーではSampleType=0のPointModelsをCIELAB/ΔE76の幅推定で近似選択（Adobeと画素一致未確認）。深度範囲、ブラシ描線/AI、他の合成モード、Camera Raw相当RAW現像も未対応 | 一部 |
| Auto-Transition | キーフレーム間を自動補間。Look/Preset/Tone Curve/PointColorsのXMP均等化も可能 | LRT 7.5.5の実行時レジストリに登録された127個の全体数値項目と、固定配列スロットのローカル補正・マスク数値項目を3次ベジェスプラインで遷移する。Linear 1–4、Circular 1–2、Brush 1–2、MaskGroup 1–4/8–11と各CorrectionMasks[1]を位置で対応させ、Sync IDは必須にしない。`--smoothness 0..100`はLRTの設定を再現し既定値は50。Look/Preset/PointColorsとポイントカーブ、MaskGroup 1–4/8–11のMainCurve/RedCurve/GreenCurve/BlueCurve構造を最初の4つ星（なければ先頭）から均等化する。ローカルカーブは構造をコピーするだけでキーフレーム間の曲線補間はしない。`LumRange`文字列は最初の非空値を同じ配列位置の全フレーム（キーフレームを含む）へコピーし、色範囲Type=1はColorAmountを補間する。`PointModels`はLRTの遷移登録に見つからないため変更しない。円形グラデーションとMaskGroup 8/9のAngleはLRTと同じ後処理（−45°未満なら+90°、45°超なら−90°）を、補間値と既存キーフレーム値に適用する。Depthの画像依存データ、ブラシ描線、登録スロット外のマスクは未対応。非キーフレームに欠けた登録位置のマスク構造は、最寄りの実体を持つXMPからRDF配列位置で複製して作成する。登録位置外やSync ID形式の欠損構造は未対応。RDF配列として格納されない独自XMPではSync IDを使って対応する。登録値ごとの範囲クランプとInteger型四捨五入、近接アンカーの直線化・等値区間の平坦化を適用する。許可リストにある旧式4項目（ChromaticAberrationB/R、Defringe、Exposure）はLRTの遷移登録にないため補間しない。先頭フレームのcrs:IsMergedHDRがtrueならExposure2012を-10..10、通常は-5..5に制限する。アスペクト比を使う円形グラデーションの専用形状補正、Camera Rawのマスク描画との一致は未対応 | 一部 |
| スナップショット | シーケンス状態のスナップショット作成、読み込みと管理 | `.lrtl/snapshots`にXMPサイドカーのZIPスナップショットを作成/一覧表示し、復元案をプレビュー可能。元の連番パスを確認し、別パスには`--allow-moved`で復元。`--write`で適用し、既存サイドカーは既定でバックアップ。画像本体は含めず、後から増えたサイドカーは削除しない | 一部 |
| 視覚プレビュー | 露出や遷移を画像・グラフで確認 | `preview`で輝度曲線、XMP Exposure2012、キーフレームを静的SVGに表示。`gui`は全フレームのXMP Exposure2012とキーフレームをグラフ表示し、クリックでフレームを選択できる。「画像輝度を解析」で最大64×64格子から得た線形sRGB輝度の中央値を先頭フレームとの差分EVで重ねる。RAW内蔵プレビュー/Adobe DNG Converterによる現像、XMP基本値・クロップ適用を選択可能。ラスター画像と対応RAWの内蔵プレビューを一覧表示し、選択フレームにXMP基本現像値・ローカル補正・クロップを適用した静止画も表示できる。「連番を再生」で1〜30 fpsを指定したループ再生にも対応し、XMP基本値/クロップとRAW内蔵プレビュー/Adobe DNG Converterによる現像を反映できる。再生画像は最大640 pxへ縮小し、読み込み時間によって指定fpsを下回る場合がある。GUIでは表示中の青いExposure2012点を上下ドラッグして、該当1フレームの値をサイドカーのバックアップ付きで保存する。通常点は最大4000個に間引き、キーフレーム点を追加表示する。曲線全体/隣接フレームへの補間編集、LRT Visual Previewとの速度・画素一致、Adobe現像値との一致は未対応 | 一部 |
| Deflicker | 参照領域と平滑化曲線に基づく視覚補正、複数パス、Lightroomへ渡す調整値 | `render --deflicker`で画素に補正し、`deflicker --write`でJPEG/PNG/TIFFまたは`--raw-preview`／`--raw-develop`の輝度解析結果を識別可能なDeflicker専用LocalExposureスロット（なければ`crs:Exposure2012`）へ記録可能。`--smoother gaussian|lowess`で平滑化を選択。`deflicker`解析は`--apply-xmp-basic`（グラデーションと現行形式の輝度範囲マスクによるローカル露出・トーン・色・WBを含む）または露出のみの`--apply-xmp-exposure`、`--apply-xmp-crop`を測定前に適用できる。LRTで観察したtricube重み付き局所線形回帰に近いLOWESSも選べるが、測光・反復補正は独自近似。LRTのVisual Preview、Camera Rawとの数値一致、カーブUIは未対応 | 一部 |
| 長期撮影補正 | Luminance・Contrast・Hueで長期連番の画像をフィルターし、明るさ・コントラスト・色の大きく異なる画像を除外 | `long-term`で画像を分析し、各指標の範囲から選択JSONを作成。`render --selection`で選択フレームのみ動画化でき、`--apply-xmp-basic`（グラデーションと現行形式の輝度範囲ローカル露出/彩度マスクを含む）/`--apply-xmp-exposure`/`--apply-xmp-crop`で測定前にXMP値を反映できる。既定指標は独自定義。`--metric-profile lrt-7.5.5`は64×64 bilinear縮小、Gaussian scale 4のぼかし、平均グレースケール輝度/標準偏差/平均HSB色相を使う。LRTの時間フィルター曲線、構図UI、現像プレビューは未対応 | 一部 |
| 動画生成 | 現像済みフレームを動画にレンダー | JPEG/PNG/TIFFとRAW現像画像をMJPEG AVI、外部FFmpegによるH.264/H.265 MP4またはProRes 422/422 HQ MOVに出力。EXIF向き、XMPの全体8項目/HSL色別補正/簡易Temperature・Tint/ポイントカーブ/グラデーションと現行形式の輝度範囲マスクによるローカル露出・トーン・色・WB/クロップ、任意アスペクト比への中央クロップ、Deflicker、撮影日時焼き込みに対応。整数1–240 fps、`--output-height`で出力高を指定し、`--long-edge`で長辺上限まで縮小。全フレームはEXIF向き適用後の解像度が同じ必要 | 一部 |
| RAW・DNG | RAW処理、DNG変換 | `--raw-develop`でインストール済みAdobe DNG Converterを自動検出し、未圧縮Linear DNGのセンサー実画素をGoで16-bit sRGBへ現像。レンダー、キーフレーム解析、Preview EV、Deflicker、長期撮影解析、GUIプレビュー/動画生成で利用可能。`--dng-converter PATH`で場所を明示でき、`--dnglab PATH`で従来バックエンドも選択可能。`import --convert-dng`はAdobeによるlossless/uncompressed DNG取り込みに対応。内蔵JPEGを使う`--raw-preview`も維持。Camera Rawの全プロファイル/Opcode/現像値の完全再現、ロッシーDNGとサイズ縮小は未対応 | 一部 |
| 解像度・中間画像 | 720p〜8K/Originalなどライセンス別解像度、Pro向け非圧縮TIFF中間画像 | `--aspect W:H`で1:16〜16:1の任意比率へ中央クロップし、`--output-height H`で出力高を指定。`--long-edge N`は長辺上限まで縮小。LRTのプリセット/UIや非圧縮TIFF中間画像は未対応 | 一部 |
| コーデック・色・HDR | H.264/MP4、Pro向けH.265/ProRes/DNxHR、HDRワークフロー | 純GoのMJPEG AVIとFFmpegのlibx264/libx265/prores_ks/dnxhd経由のH.264/H.265 MP4、ProRes 422/422 HQとDNxHR LB/SQ/HQ/HQX/444 MOV。MP4/MOVは`--audio FILE`でAAC音声を追加可能。Rec.2020/HDRは未対応。FFmpegへRGB 16-bitで渡し、ProRes/DNxHR HQX/444は10-bit出力へ変換。16-bit入力の向き・クロップ・縮小・露出・Motion Blur・字幕を通して対応済みXMP基本現像の全体/局所調整も16-bit画素で処理。HDR・ICC色管理は未対応 | 一部 |
| オーバーレイ・字幕 | 透かし、撮影時刻表示、SRT字幕出力 | `render --watermark TEXT`で任意テキストを各フレームに焼き込み、位置を5種類から選択可能。`render --timestamp-overlay`で撮影日時も表示し、`subtitles`で同じ日時情報からSRT字幕を出力。埋め込みフォントを使用 | 一部 |
| Motion Blur Plus | 書き出し動画への低/中/高モーションブラー | `render --motion-blur N`でLRTの7種類のフレーム数と、確認できた`tmix`重み`(i/N)^1.5`（3桁丸め）を適用。過去N枚を合成し、先頭は最初のフレームを複製して窓を満たす。16-bit RGB処理。LRTの10-bit YUVフィルター出力との画素一致は未確認 | 一部 |
| 取り込み | フォルダー/カードまたは動画ファイルからの取り込み、サブフォルダー作成、日時順整列、JPG省略、日時リネーム | `import SOURCE --output DIRECTORY`で写真ファイルと対応XMPをコピー。任意で再帰検索、撮影日時順、JPEG/JPG除外、撮影日時へのリネーム、dry-runに対応。`--convert-dng`で対応RAWをlossless/uncompressed DNGへ変換し、他の画像とXMPを取り込み。`import-video VIDEO --output DIRECTORY`でFFmpegから動画の全フレームまたは指定fpsのフレームをJPG/PNG連番へ抽出。画像プレビュー、ロッシーDNG、DNGのサイズ縮小、動画の自動分割は未実装 | 一部 |
| DNG変換 | 取り込み時のロスレス/ロッシーDNG変換、サイズ縮小（Pro） | `import --convert-dng`はAdobe DNG Converterを既定で使い、lossless/uncompressed DNGと隣接XMPを取り込む。`--dnglab PATH`でDNGLabを明示選択可能。ロッシー圧縮とPro相当のサイズ縮小は未対応 | 一部 |
| バッチ・並列処理 | バッチプレビュー/補正、マルチスレッド処理 | `batch-render ROOT --output DIRECTORY`で複数の画像連番を動画化。既定でルート直下、`--recursive`で全階層から連番フォルダーを検索し、`--workers`で並列数を指定。`--`以降で通常の`render`オプションを共通指定できる。バッチプレビューとXMP一括補正は未実装 | 一部 |
| GUI | シーケンス一覧、カーブ表示、プレビュー編集 | `lrtl gui`でループバック限定のローカルWeb UIを起動。画像一覧/サムネイル、全フレームのXMP Exposure2012と解析した画像輝度のグラフ表示、クリックによるフレーム選択、選択フレームの131数値プロパティ編集と対応範囲のXMP値を適用する静止画プレビュー、元画像上のドラッグによるクロップ矩形/回転角度の編集と5★保存、4/5つ星マーク、輝度分析で候補一覧を出し選択保存、JPEG/PNG/TIFFまたはRAW内蔵プレビュー/Adobe DNG Converterによる現像を使った候補分析、XMP補間、動画レンダーを操作できる。レンダーは比率/解像度、音声、選択JSON/ファイル名フィルター、撮影日時と透かし、モーションブラー、H.264/H.265品質も指定可能。書き込みとレンダー開始前に確認を求め、XMPの既存バックアップを作成。グラフからの現像値編集、LRT連続画像Visual Preview、Camera Raw現像プレビュー、フォルダー選択ダイアログ、フレーム移動/削除は未対応 | 一部 |
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
2. JPEG/PNG/TIFF連番、RAW内蔵プレビュー、またはAdobe DNG ConverterでデモザイクしたRAWからLRT互換の数量と等間隔キーフレーム位置を提案し、任意でXMPにマークする。
3. キーフレームを手動で設定し、許可済みの数値設定をプレビューまたはXMPに書き込む。
4. 現像済みJPEG/PNG/TIFF、対応RAW内蔵プレビュー、またはAdobe DNG ConverterでデモザイクしたRAWをMJPEG AVI、H.264/H.265 MP4、ProRes MOVにまとめる。必要なら`--aspect 16:9 --output-height 1080`のように出力比率と高さを指定するか、`--long-edge`で縮小する。XMP値未焼き込みのsRGB入力には`--apply-xmp-basic`で基本8項目、Texture/Clarity/Dehaze/Sharpening/NoiseReduction/Vignette、4領域パラメトリックカーブ、8色域のHSL補正、Temperature/Tintの簡易ホワイトバランス、ColorGrade近似、明示ポイントカーブを近似適用できる。
5. レンダー時に簡易Deflickerを画素へ適用するか、JPEG/PNG/TIFFまたは選択したRAW入力の解析結果をXMP露出値として保存する。
6. 対応画像のEXIF露出時間・ISO・絞り値からHoly Grail補正を算出し、Camera RawのXMP露出値として出力する。
7. 輝度曲線、XMP露出補正、キーフレーム位置をSVGプレビューで確認する。RAW解析時は内蔵プレビューまたはAdobe DNG Converterによる現像を選び、必要に応じてXMP Exposure/cropを反映する。
8. XMPサイドカーをZIPスナップショットに保存し、一覧表示とプレビュー後の復元を行う。
9. フレームのEXIF/XMP撮影日時を動画タイミングに合わせたSRT字幕に出力する。
10. 長期連番を輝度・コントラスト・色相で分析し、選択した画像だけを動画にする。
11. カード/フォルダーから写真とXMPを取り込み、必要に応じて日時順へ並べたりJPEGを除外したりする。対応RAWはAdobe DNG ConverterでDNGに変換できる。
12. 動画から全フレームまたは指定fpsのJPG/PNG連番を抽出し、通常の画像列としてレンダーする。
13. XMPサイドカー上の連番撮影日時を一括で前後にずらす。
14. 複数の連番フォルダーを並列に動画化し、レンダー失敗を連番ごとに報告する。

動画生成の最短経路は、Lightroom等でJPEG/PNG/TIFFを書き出してから`lrtl render`を実行する方法です。キーフレーム間のXMP値も反映する場合は、XMP現像値をまだ焼き込んでいない画像に対して`interpolate --write`後に`render --apply-xmp-basic`を使います。このオプションはExposure2012、Contrast2012、Highlights2012、Shadows2012、Whites2012、Blacks2012、Vibrance、Saturationの8項目にTexture/Clarity/Dehaze/Sharpening/NoiseReduction/Vignetteと4領域のパラメトリックカーブに加え、8色域のHue/Saturation/Luminance調整、Temperature/Tint、XMPに明示されたマスター/RGBポイントカーブを表示済みsRGB画像へ独自近似で適用します。HSLは色域中心の間を線形補間し、色相をスライダー値あたり0.3度、彩度を0〜2倍、輝度をHSL明度の±0.25以内として近似します。ホワイトバランスは推定色温度からRGBゲインを求める近似で、カメラのセンサーデータやプロファイルを使わず、Lightroom/Camera Rawと画素値が一致する現像機能ではありません。線形/円形グラデーションと現行形式の輝度範囲マスクは、単一またはAdd/Intersect合成の対応範囲でローカル補正値を独自近似で適用します。ブラシ/AI、Depth範囲、旧形式の範囲マスク、プリセット名だけのカーブ、ExtendedMainCurveなどのHDRローカル曲線は未対応です。ローカルMainCurve/RedCurve/GreenCurve/BlueCurveは[共有されたXMP実例](https://community.adobe.com/feature-requests-676/p-implement-copy-and-paste-global-curve-to-masking-local-curve-and-vice-versa-665754)にある点列を16-bit sRGB画素に対してPCHIP補間し、マスク濃度で混ぜる近似です。LocalCurveRefineSaturationは未対応です。LocalTemperature/Tintは値を推定RGBゲインへ写像し、各ローカル効果をマスク濃度で混ぜます。既にLightroom/Camera Rawで同じ現像値を焼き込んで書き出したJPEG等には再適用せず、通常の`render`を使ってください。RAW連番を直接レンダーするには`--raw-develop`を指定します。既定ではインストール済みAdobe DNG Converterを使い、`--dnglab PATH`を明示した場合だけDNGLabを使います。`--raw-preview`は内蔵プレビューを使う別経路です。`--apply-xmp-exposure`を指定すると、サイドカーの`crs:Exposure2012`だけをレンダー入力画像に近似適用します。`--apply-xmp-basic`には同じ露出値が含まれるため、両方は同時に指定できません。DNGLabの現像パラメーターはCamera Rawと一致せず、サイドカーにある他の現像値もRAW現像には使いません。Deflickerはレンダー時に出力画素へ適用できます。これらのレンダー操作は元画像やXMPを変更しません。別コマンドの`lrtl deflicker --write`は、解析した補正をXMPに保存します。全フレームの幅と高さは同じである必要があります。MJPEG AVIには約4 GBのサイズ上限があります。MP4/MOV出力は外部FFmpegを必要とし、入力画像をGoでデコードし、RGB各16-bitの`rgb48le`として選択したエンコーダーへ渡します。`.mp4`の既定はH.264、`.mov`の既定はProRes 422です。`--codec h265`でH.265、`--codec prores-hq`でProRes 422 HQ、`--codec dnxhr-lb|dnxhr-sq|dnxhr-hq|dnxhr-hqx|dnxhr-444`でDNxHR各プロファイルを選べます。FFmpegに`libx264`、`libx265`、`prores_ks`または`dnxhd`エンコーダーが必要です。DNxHR HQX/444とProResは10-bit形式へ符号化し、16-bit TIFF/PNG/RAWの精度を利用できます。JPEG等の8-bit入力に元からない階調やHDR情報を復元する機能ではありません。

`--apply-xmp-basic`は`Dehaze`とマスク補正内の`LocalDehaze`も近似適用します。正の値は中間輝度を基点にコントラストを最大20%高め、画像短辺に応じた広半径の輝度高周波成分を最大0.6倍加えます。負の値は画素を線形輝度0.78のベールへ最大18%混ぜ、彩度は正の値で最大15%上げ、負の値で最大15%下げます。これは連番間で急変しにくいsRGB近似で、Camera Rawの大気光推定やRAW処理、画素一致は再現しません。AdobeはDehazeが霞を減らす/加える調整で、局所マスクにも使えると説明しています（[全体調整](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/image-tone-color.html)、[ローカル調整](https://helpx.adobe.com/uk/lightroom-classic/desktop/process-and-develop-photos/apply-local-adjustments.html)）。

`--apply-xmp-basic`は`Sharpness`と`SharpenRadius`/`SharpenDetail`/`SharpenEdgeMasking`も読みます。短辺1080pxを基準に半径をスケールし、sRGB線形輝度の広域差を加えてエッジを強調します。Detailは強調量、EdgeMaskingは高コントラスト部へ絞るしきい値として近似し、`LocalSharpness`はマスク濃度に合わせて同じ処理を適用します。LocalSharpnessの負値はぼかし方向に働きます。Camera Rawと画素一致する処理ではなく、RAWノイズ除去やデモザイク前のシャープ化も行いません。Adobeが説明するAmount/Radius/Detail/Maskingの役割は[公式資料](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/retouch-photos.html)を参照してください。

`--apply-xmp-basic`は`ParametricShadows/Darks/Lights/Highlights`と各`Parametric*Split`も読みます。3つの分割位置を単調に整え、線形sRGB輝度上で4領域のsmoothstep重みと最大0.15の加算値を用いて近似カーブを適用します。これはAdobeが説明する領域別調整と分割コントロールの独自演算で、LRT/Camera Rawとの画素一致は未確認です（[Adobeのトーンカーブ説明](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/image-tone-color.html)）。`--apply-xmp-basic`は`Texture`/`LocalTexture`と`Clarity2012`/`LocalClarity2012`も近似適用します。sRGB輝度から細い/広い半径の高周波成分を抽出し、Textureは細部、Clarityは中間調に重みを付けて輝度へ加算し、ローカル値はマスク濃度で混ぜます。半径は画像の短辺に合わせて制限し、色相を保つようRGB各チャンネルを同じ比率で拡縮します。独自の16-bit sRGB処理でCamera Rawとの画素一致は未確認です。[AdobeはTextureを色や階調を変えずに細部へ作用する調整、Clarityを局所コントラスト調整として説明しています](https://helpx.adobe.com/uk/lightroom-classic/desktop/process-and-develop-photos/apply-local-adjustments.html)。

`--apply-xmp-basic`はLuminanceSmoothing/LuminanceNoiseReductionDetail/LuminanceNoiseReductionContrastとColorNoiseReduction/ColorNoiseReductionDetail/ColorNoiseReductionSmoothnessを近似適用します。各値は0〜100で、3×3の範囲内から輝度差・色差が近い画素を重み付き平均し、輝度ノイズと色ノイズを別々に弱めます。Luminance Detailは輝度境界の保護、Contrastは平滑化後に局所コントラストを残す量、Color Detail/Smoothnessは色差許容値として使います。固定近傍と固定スライダー応答を使う独自sRGBフィルターで、AdobeのRAWノイズ除去やAI Denoiseとの一致は未確認です。Adobeの各スライダーの役割は[公式資料](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/retouch-photos.html)を参照してください。

`--apply-xmp-basic`はPostCropVignetteAmount/Midpoint/Roundness/Feather/HighlightContrastも読み、クロップ後の画像サイズを基準に周辺へ最大±1.5 EVを加える近似を行います。Roundnessは楕円率、Midpointは効果開始位置、Featherは境界幅、負のAmount時のHighlightContrastは明部を保つ量として扱います。Camera Rawの適応的なハイライト保護や画素一致は再現しません。Adobeの各コントロールの説明は[公式資料](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/retouch-photos.html)を参照してください。

`--apply-xmp-basic`は`ColorGradeShadow/Midtone/Highlight/GlobalHue/Sat/Lum`、`ColorGradeBlending`、`ColorGradeBalance`と、対応するローカル補正内の`LocalColorGrade*`も読みます。線形輝度0.15/0.5/0.85を中心とする正規化Gaussian重みで3領域を分け、Blendingを重なり幅、Balanceを影/ハイライトの寄せ方として使います。各領域の色相/彩度はHSL色付け、輝度は線形輝度スケールで近似し、LocalColorGradeはマスク濃度で混ぜます。Adobeは3領域の色相/彩度とBlending/Balanceの役割を説明していますが、この重み付けと画素演算は独自近似です（[Adobe Color Gradingガイド](https://helpx.adobe.com/lightroom-classic/desktop/process-and-develop-photos/image-tone-color.html)）。Camera Rawとの画素一致は未確認です。

`--apply-xmp-basic`は現行Camera Raw XMPの輝度範囲マスクも限定対応します。`Mask/RangeMask`コンポーネント内の`CorrectionRangeMask Type=2`と4要素の`LumRange`を読み、向き適用後・クロップ前の各画素で線形sRGB輝度に基づく滑らかな範囲選択を評価します。`MaskBlendMode`、`MaskInverted`、範囲側の`Invert`を合成します。選択とAdd/Intersectは独自近似で、Adobeとの画素一致は未確認です。Color Range Type=1はSampleType=0の`PointModels`内6値を読み、後半3値をsRGBサンプル色としてΔE76距離と`ColorAmount`から選択を推定します。座標と末尾識別値の意味、Adobeの選択演算は未解析のため近似です。Type=1以外、Depth範囲、旧形式の`CorrectionRangeMask`、ブラシ/AIマスクは未対応です。

`preview`は画像列の線形sRGB輝度を測り、最初のフレームからのEV差、XMPの`Exposure2012`差、キーフレーム評価をSVGグラフに出します。JPEG/PNG/TIFFのほか、`--raw-preview`で対応RAW内のプレビュー、または`--raw-develop`でAdobe DNG Converter（または明示したDNGLab）の現像画像を解析できます。`--apply-xmp-basic`（`--apply-xmp-exposure`とは排他的）と`--apply-xmp-crop`を指定すると、輝度解析にその値を適用します。これは静的な数値グラフで、Lightroom/Camera Rawの現像画像やLRTimelapseの操作可能なVisual Previewは表示しません。

`long-term`は画像を最大64×64点のグリッドでサンプリングし、線形sRGB輝度の中央値、輝度log2の90–10パーセンタイル差（EV）、彩度で重み付けした円形平均色相（0–360度）を記録します。`--luminance-range`、`--contrast-range`、`--hue-range`はそれぞれ`MIN,MAX`で指定し、指定した全範囲を満たすフレームをJSONマニフェストに含めます。色相は`MIN>MAX`で0度をまたぐ範囲を表せます。無彩色など平均色相が定まらない画像は色相範囲指定時に除外します。範囲を何も指定しなければ全画像を含めます。`render --selection`は元フォルダー、ファイル名、サイズ、更新時刻を照合してから含まれる画像をレンダーし、画像やXMPを変更しません。`render --filename-contains TEXT`はさらにファイル名の大文字小文字を無視した部分一致で絞り込みます。LRTimelapse 7にも「Select by Filename」がありますが、lrtlはCLI絞り込みのみで、ファイル一覧上の選択UIはありません。LRTimelapseも長期用にLuminance/Contrast/Hueフィルターを提供します。既定のlrtl指標は独自実装です。`--metric-profile lrt-7.5.5`を選ぶと、インストール済み7.5.5で確認した64×64 bilinear縮小、scale 4のGaussian blur、平均グレースケール輝度、グレースケール標準偏差、平均HSB色相による測定を使います。数値計算は別実装で、LRTの選択構図、時間方向のフィルター/定数補正、曲線UIは再現しません。

解析コマンドの`--apply-xmp-basic`、`--apply-xmp-exposure`、`--apply-xmp-crop`はlrtl側の画素近似です。`--apply-xmp-basic`は露出、トーン、色、Temperature/Tintをまとめて含み、`--apply-xmp-exposure`とは同時に使えません。既にLightroom等でその補正を焼き込んだJPEG/PNGでは二重適用になるため、主にRAWプレビューやDNGLab現像画像の解析に使ってください。

`batch-render`はROOT自身と直下の各サブフォルダーを調べ、対応画像を含むフォルダーを個別の連番としてレンダーします。`--recursive`を指定すると全階層を検索します。ROOTが連番の場合は`ROOT名.mp4`のように出力し、配下の連番は相対フォルダーを保って出力します。`--format`は`avi`/`mp4`/`mov`（既定`mp4`）、`--workers`は同時処理数（既定1、最大64）です。`--`の後に指定した通常の`render`オプションは全ジョブへ渡されます。各ジョブの成功・失敗をまとめて表示し、失敗した連番があっても他のレンダーを続け、どれかが失敗した場合は終了コード1を返します。これは動画レンダーのバッチ機能で、プレビュー生成やXMP補正の一括適用はまだありません。

## インストール

ビルドにはGo 1.23以降が必要です。Go側の依存ライブラリはPure Goです。MJPEG AVI出力にはFFmpegやネイティブDLLは不要です。H.264/H.265 MP4とProRes MOV出力、動画からのフレーム抽出には外部FFmpegが必要です（`--ffmpeg`で実行ファイルを指定できます）。RAW現像とDNG変換にはAdobe DNG Converter（または明示選択するDNGLab）が必要です。Adobeは自動検出または`--dng-converter PATH`、DNGLabは`--dnglab PATH`で指定します。FFmpeg、Adobe DNG Converter、DNGLabはlrtlに同梱しません。RAWデコーダー候補の[raw-go](https://github.com/vegidio/raw-go)はLibRawをCGOで静的リンクするためCツールチェーンが必要で、現行のPure Goビルド条件に合わず採用していません。LibRaw自体はLGPL-2.1またはCDDL 1.0で提供されています（[ライセンス説明](https://github.com/LibRaw/LibRaw#licensing)）。

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

AVI出力と画像列の処理はPure Goです。MP4/MOVには外部FFmpegが必要です。RAWを直接現像する場合はインストール済みAdobe DNG Converterを使用します。XMPの値を画像に焼き込む現像処理は、レンダー済み画像へ重ねて適用すると二重補正になるため、未適用の画像だけで`--apply-xmp-basic`を使ってください。

ブラウザー上で連番を見ながら操作する場合は、ローカルUIを起動し、表示されたURLをブラウザーで開きます。UIは`127.0.0.1`にだけ待ち受けます。

```powershell
.\lrtl.exe gui
```

フォルダー欄に連番のパスを入力するとサムネイルが表示されます。選択フレームの数値XMPプロパティを編集でき、「XMPプレビュー」は対応する補正を適用した静止画を表示します。新しいトーンカーブ欄ではRGBマスターまたはRed/Green/Blueチャンネルを選び、点をドラッグして編集、空白をクリックして追加、右クリックで中間点を削除できます。カーブは選択中の1フレームに保存し、実行前に確認して既存XMPのバックアップを作成します。連番グラフの青いExposure2012点を上下にドラッグすると、そのフレームの露出値を編集できます。「連番を再生」は1〜30 fpsでループ再生し、対応XMP基本値・クロップ、RAW内蔵プレビューまたはAdobe DNG Converterによる現像を選択できます。再生画像は最大640 pxに縮小します。フレームごとの4/5つ星設定、輝度からのキーフレーム候補作成、XMP補間、マニュアルレンズの絞り値設定、AVI/MP4/MOVレンダーも操作できます。RAWの輝度解析と静止画プレビューは内蔵プレビューまたはAdobe DNG Converterによる現像を選べます。XMPを書き換える操作とレンダーは実行前に確認します。RAWセンサーデータの現像にはAdobe DNG Converter、MP4/MOVにはFFmpegが必要です。

## 使い方

`import`は指定フォルダーの対応画像と、見つかった同名XMPサイドカーを出力フォルダーへコピーします。`--recursive`でサブフォルダーも検索し、元フォルダー名を出力名に付けて衝突を避けます。`--sort capture-time`はXMP/EXIF撮影日時順に並ぶ連番を付け、`--rename-by-date`は日時をファイル名に使います。同じ秒の撮影には連番を追加します。`--skip-jpeg`はJPEG/JPGを除外し、`--dry-run`はコピー予定だけを表示します。撮影日時順・日時リネームには全画像の撮影日時が必要です。`--convert-dng`を指定するとAdobe DNG Converterが対応するRAWファイルを`.dng`へ変換し、隣接XMPサイドカーも新しい画像名に合わせてコピーします。JPEG等と既存DNGはそのままコピーします。圧縮方式は`--dng-compression lossless|uncompressed`で指定でき、既定はlosslessです。ロッシー圧縮とサイズ縮小は未対応です。Adobe DNG Converterは自動検出し、必要なら`--dng-converter PATH`で指定します。`--dnglab PATH`を指定した場合はDNGLabを使います。

`import-video`はFFmpegで動画をJPGまたはPNGの連番へデコードします。既定ではデコード可能な全フレームを保存し、`--fps N`を指定するとその間隔でフレームをサンプリングします。JPEGの圧縮品質は`--quality 1`（高画質）〜`31`（低画質）で指定できます。抽出先は新しいフォルダーでなければならず、既存フォルダーを置き換えません。LRTimelapseの動画取り込みもJPG連番を生成しますが、lrtlではプレビュー生成、シーンごとの自動分割、動画フレームのDNG変換は未実装です。

`timeshift`は`--offset 2h30m`、`--offset=-45s`、`--offset 1d`のような固定時間差で連番の撮影日時をずらします。既定はプレビューで、`--write`を付けるとXMPサイドカーへ保存し、既存ファイルはバックアップします。対象は`DateTimeOriginal`、`DateTimeDigitized`、`CreateDate`で、`ModifyDate`と画像に埋め込まれたEXIF値は変更しません。XMPの日付値がなく画像EXIFで撮影日時を読める場合は、ずらした`DateTimeOriginal`をサイドカーへ追加します。

XMPを読む処理は、同じ画像に外部`.xmp`サイドカーがあればそれを優先します。サイドカーがないJPEG画像では標準/拡張XMP APP1、TIFF/DNG/CR2/RW2/NEF/NRW/ARW/SR2/PEF/ERF/DCRではTIFF XMLPacket tag 700、ORFではIIRO/IIRS/MMORマジックのTIFF XMLPacket tag 700、RAFではヘッダーのオフセット/長さで指定される埋め込みJPEGの標準/拡張XMP APP1を読みます。CR3ではISO BMFFのAdobe XMP UUID boxを探し、サイズとXMLパケットを検証します。埋め込みXMPを編集した場合も、変更は外部サイドカーへ保存し、元画像を書き換えません。組み立てた埋め込みXMPの上限は32 MiBです。これら以外のRAWコンテナーに埋め込まれたXMPは未対応です。

`lens-tag`はLRTimelapse 7.5.5のLens Taggerで確認したf/1〜f/90の候補から選び、連番の各XMPサイドカーへ`lrt:Aperture`として記録します。既定はプレビューで、`--write`で保存し、既存サイドカーはバックアップします。`--no-backup`は`--write`と併用できます。GUIにも同じ操作があります。これはマニュアルレンズ向けのLRT独自XMP値で、画像内EXIFや埋め込みXMPは変更しません。`holy-grail`はEXIFのFNumberが有効ならそちらを優先し、欠落時には`lrt:Aperture`を使います。

```powershell
# 画像列、サイドカー、キーフレームを確認
lrtl scan "D:\Timelapse\Sequence"

# カメラカードからRAWだけを撮影日時順で取り込む
lrtl import "E:\DCIM" --output "D:\Timelapse\Sequence" --recursive --sort capture-time --skip-jpeg

# RAWをlossless DNGに変換し、XMPも一緒に取り込む（Adobe DNG Converterが必要）
lrtl import "E:\DCIM" --output "D:\Timelapse\DNGSequence" --recursive --skip-jpeg --convert-dng

# 非圧縮DNGに変換。Converterの場所を明示する場合
lrtl import "E:\DCIM" --output "D:\Timelapse\DNGUncompressed" --convert-dng --dng-compression uncompressed --dng-converter "C:\Program Files\Adobe\Adobe DNG Converter\Adobe DNG Converter.exe"

# 取り込み名も撮影日時へ変更し、実行前に内容を確認
lrtl import "E:\DCIM" --output "D:\Timelapse\SequenceByDate" --recursive --skip-jpeg --rename-by-date --dry-run

# 撮影日時を1時間戻す（プレビュー後、--writeでXMPに保存）
lrtl timeshift "D:\Timelapse\Sequence" --offset=-1h
lrtl timeshift "D:\Timelapse\Sequence" --offset=-1h --write

# マニュアルレンズの絞り値を連番全体に設定（確認後、--writeでサイドカーへ保存）
lrtl lens-tag "D:\Timelapse\Sequence" --aperture f/8
lrtl lens-tag "D:\Timelapse\Sequence" --aperture f/8 --write

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

# LRTimelapseがXMPへ保存した入力値からHG補正を計算する（既定はプレビュー）
lrtl holy-grail "D:\Timelapse\Sequence" --lrt-input
lrtl holy-grail "D:\Timelapse\Sequence" --lrt-input --write
# LRT WizardのReduce Deflickerスライダー値も指定する（0〜1）
lrtl holy-grail "D:\Timelapse\Sequence" --lrt-input --lrt-reduce-deflicker 0.35

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

# Adobe DNG ConverterとGoでRAWを現像し、その輝度曲線から候補を選ぶ
lrtl keyframes "D:\Timelapse\RAW" --raw-develop --apply-xmp-basic --apply-xmp-crop --count 5

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

# Adobe DNG ConverterとGoでRAWを現像してH.264 MP4を作成（FFmpegとAdobe DNG Converterが必要）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-developed.mp4" --raw-develop

# Adobe DNG ConverterとGoでRAWを現像してMJPEG AVIを作成（Adobe DNG Converterが必要、FFmpeg不要）
lrtl render "D:\Timelapse\RAW" --output "D:\Timelapse\raw-developed.avi" --raw-develop

# RAW現像画像から輝度グラフと簡易Deflicker補正を作成
lrtl preview "D:\Timelapse\RAW" --output "D:\Timelapse\raw-brightness.svg" --raw-develop --apply-xmp-basic --apply-xmp-crop
lrtl deflicker "D:\Timelapse\RAW" --raw-develop --apply-xmp-basic --apply-xmp-crop

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

`render`は出力ファイルが既にある場合は停止します。置き換えるときは`--overwrite`を指定します。`--aspect W:H`はXMPクロップの後に画像中央を切り出し、指定比率に近い整数画素の画像を作ります。受け付ける比率は1:16〜16:1です。これはlrtl独自の中央クロップで、LRTのCompositionやクロップ選択UIとの数値一致は確認していません。`--output-height H`は縦横比を維持したまま出力高をHピクセルに設定し、拡大も行います。`--long-edge N`は元の長辺がNより大きいときだけ縮小し、縦横比を保って長辺をNピクセル以下にします。`--output-height`と`--long-edge`は同時に指定できません。両方とも16〜32768ピクセルを受け付けます。`--audio FILE`はMP4/MOVへ音声ファイルの最初の音声トラックをAAC 192 kbit/sで追加します。FFmpegが必要です。音声の開始時刻を0にし、動画より短い場合は末尾を無音で補い、長い場合は動画の長さで切ります。MJPEG AVIへの音声追加、複数トラック選択、音量調整は未対応です。AVIでは画素処理なしのJPEG入力を再圧縮せず格納し、それ以外の入力は指定品質のJPEGに変換します。MJPEG AVIの最終JPEG符号化では16-bit階調が8-bitへ量子化されます。MP4/MOVへはRGB各16-bitで渡します。MP4ではJPEG/PNG/TIFFをGoでデコードし、H.264エンコーダーにRGBフレームとして渡すため中間JPEG圧縮をしません。`--crf`はH.264品質（0–51、低いほど高品質）、`--preset`は速度と圧縮率の選択です。MP4の画素は8-bit YUV 4:2:0に変換され、奇数幅/高さは右端/下端をパディングします。`--reference`の形式は`x,y,width,height`で、指定しない場合は画面全体を測定します。`--smooth`は平滑化の半径（フレーム数）、`--passes`は補正反復数です。動く被写体や雲など輝度が変わる要素を避けた領域を指定してください。`--constant`は長期的な明るさの変化も抑えるため、照明条件が一定のシーンに向いています。

`--apply-xmp-exposure`は各画像に対応する外部XMPサイドカーから数値の`crs:Exposure2012`を読み、線形sRGB画素に露出倍率を適用します。±32 stopsを超える値や不正な数値はエラーにします。Camera Rawの全現像パイプラインと一致する機能ではなく、露出スライダーだけの近似適用です。JPEGはこの指定で再エンコードされます。

`--apply-xmp-basic`はExposure2012に加えてContrast2012、Highlights2012、Shadows2012、Whites2012、Blacks2012、Texture、Clarity2012、Dehaze、Sharpness/SharpenRadius/SharpenDetail/SharpenEdgeMasking/LuminanceSmoothing/LuminanceNoiseReductionDetail/LuminanceNoiseReductionContrast/ColorNoiseReduction/ColorNoiseReductionDetail/ColorNoiseReductionSmoothness/PostCropVignetteAmount/PostCropVignetteMidpoint/PostCropVignetteRoundness/PostCropVignetteFeather/PostCropVignetteHighlightContrast、ParametricShadows/Darks/Lights/Highlights（-100〜100）と各分割位置（10〜70/20〜80/30〜90）、Vibrance、Saturation、Temperature、Tintを画像画素に近似適用します。Contrast2012/Highlights2012/Shadows2012/Whites2012/Blacks2012/Vibrance/Saturation/Texture/Clarity2012/Dehaze/Parametric*は-100〜100、LuminanceSmoothing/LuminanceNoiseReductionDetail/LuminanceNoiseReductionContrast/ColorNoiseReduction/ColorNoiseReductionDetail/ColorNoiseReductionSmoothnessは0〜100、PostCropVignetteAmountは±100、PostCropVignetteMidpoint/Feather/HighlightContrastは0〜100、PostCropVignetteRoundnessは±100、Sharpnessは0〜150、SharpenRadiusは0.5〜3、SharpenDetail/SharpenEdgeMaskingは0〜100、Exposure2012は±32 stops、Temperatureは2000〜50000、Tintは-150〜150の範囲が必要です。`ToneCurvePV2012`とRGB別の明示ポイントカーブも読み、順序付きの整数点間をPCHIP補間し、16-bit画素の位置で直接評価します。表示済みsRGBの輝度選択マスク、PCHIPカーブ、彩度倍率による独立実装であり、Camera Rawのトーン応答や色管理を再現しません。Temperature/Tintは6500 Kを基準とする推定RGBゲインであり、センサーデータを使うCamera Rawのホワイトバランスとは一致しません。プリセット名だけのカーブは未対応です。パラメトリック領域調整と分割値は線形輝度上で独自近似します。ローカル補正は、線形/円形グラデーションの単一またはAdd/Intersect合成マスクに対するLocalExposure/Contrast/Highlights/Shadows/Whites/Blacks/Saturation/LocalTexture/LocalClarity2012/LocalDehaze/LocalSharpness/Temperature/Tint/LocalToningHue/LocalToningSaturationを近似適用します。CorrectionMasks内のMask/Aggregateは子要素へ展開し、MaskBlendMode=0をAdd、1をIntersectとして合成します。 XMPのLocalContrast2012/Highlights/Shadows/Whites/Blacks/Saturation/Texture/Clarity2012/Dehaze/Sharpness/Temperature/Tintは正規化小数を-100〜+100のスライダー値に換算し、LocalExposure2012はEV値のまま扱います。LocalToningSaturationは0〜1を0〜100へ換算します。Addはcoverage union式を使う独自近似で、Camera Rawとの数値一致は未確認です。LocalToningHue/LocalToningSaturationは元画素のHSL明度を保った色味をサチュレーション値とマスク濃度に応じて混ぜる独自近似です。単一マスク要素の反転は現行MaskInvertedと旧形式Flippedに対応し、両方有効なら二重反転として処理します。マスクはクロップ前の向き適用済み座標で評価します。Lightroom/Camera Rawで現像値を焼き込み済みの画像では同じ値を二重適用しないでください。`--apply-xmp-exposure`とは同時指定できません。JPEG/PNG/TIFFは再エンコードし、元画像とXMPは変更しません。

`ol:HolyGrailCorrection2012`と`ol:HolyGrailXMPTarget`が示すlrtl管理下のHG補正は、`--apply-xmp-exposure`と`--apply-xmp-basic`で全画面の露出差として加えます。LRTの内部HGレイヤーは意図的に画面外のマスクを使うため、その補正をローカルマスクとして二重適用しません。lrtlの管理マーカーがないXMPは通常どおりCamera Rawのマスクを近似描画します。

`--apply-xmp-crop`は各サイドカーの`crs:HasCrop`、`CropTop/Left/Bottom/Right`、`CropAngle`を読み、回転した矩形クロップを適用して入力と同じ解像度に再サンプルします。クロップのないフレームはそのままです。空または範囲外の矩形はエラーにします。Camera RawのGeometry/遠近補正は再現しません。Lightroom等でキーフレームのクロップ値を保存し、`interpolate --write`で間を埋めてからこのオプションでレンダーできます。

`--auto-orient`は入力画像、RAW内蔵プレビュー、またはDNGLab出力のEXIF Orientationを読み、回転と鏡像を画素へ適用してからXMPクロップ、色調補正、Deflicker、リサイズを行います。向き情報がないフレームはそのままです。向きを適用した後の全フレームは同じ幅と高さである必要があります。この処理は入力ファイルとメタデータを変更しません。

`--motion-blur N`は出力前に時間方向へ隣接フレームを合成します。選べる窓幅はLRTのRenderPresetで確認した2、3、4、5、10、15、20枚で、0は無効です。インストール済みLRTのレンダー引数生成処理から、`tmix=frames=N:weights='...'`と各重み`(i/N)^1.5`（`i=1..N`、小数3桁）を確認しました。重みは古いフレームから現在フレームへ向かって増え、FFmpegの既定スケールで合計1に正規化されます。各出力フレームは現在と過去N-1枚から作り、先頭では最初のフレームを複製して窓を満たします。XMP/露出補正、クロップ、リサイズを先に行い、指定時刻の文字焼き込みは合成後に行います。lrtlはPure Goの16-bit RGBで同じ重みと時間窓を計算します。LRTのレンダー経路は10-bit YUV444でフィルター処理するため、数値上の重みと窓の一致は確認済みですが、画素値の完全一致は未確認です。一時フレームバッファーの推定量が768 MiBを超える場合は処理を開始しません。大きなフレームでは`--long-edge`または窓幅を下げてください。

`--timestamp-overlay`と`subtitles`は、各サイドカーの`exif:DateTimeOriginal`、`xmp:CreateDate`、`xmp:ModifyDate`の順で日時を探し、なければ画像内EXIFの撮影/作成/更新日時を使います。全フレームの日時が必要です。`subtitles`は各画像の撮影日時を、そのフレームが動画上に表示される区間のSRT字幕として書き出します。`--fps`は動画のフレームレートと合わせてください。既定の表示形式は`2006-01-02 15:04:05`で、`--timestamp-format`へGoの日時レイアウトを渡して変更できます。`render --timestamp-overlay`は日時を右下に焼き込みます。`render --watermark TEXT`は埋め込みGoフォントで固定テキストを重ね、`--watermark-position top-left|top-right|bottom-left|bottom-right|center`で配置を選べます（既定は左下）。両方を指定すると日時と透かしを同じフレームに表示します。

`deflicker`はJPEG/PNG/TIFF、`--raw-preview`の内蔵画像、または`--raw-develop`でAdobe DNG ConverterとGoが現像した画像の輝度を線形sRGBへ変換して測定し、指定領域の中央値に平滑化または一定輝度の目標曲線を合わせます。既定の`--smoother gaussian`はGaussian平滑化を使い、`--smoother lowess`はtricube重み付き局所線形回帰を使います。LOWESSの近傍数は`--smooth`の半径から`2×半径+1`へ換算します。LRT 7.5.5のVisual Deflickerも局所線形回帰型スムーザーを呼びますが、lrtlの測光、近傍数、補正応答は独自実装です。`--apply-xmp-basic`（または`--apply-xmp-exposure`）と`--apply-xmp-crop`を指定すると、サイドカーの値を画素へ近似適用します。`--passes 1..8`は候補補正後の画素を再描画・再測定し、追加変更がなくなれば早期終了します。既定はプレビューだけです。

`--write`は識別できるDeflicker専用スロット（なければ`crs:Exposure2012`）へ最終補正を書き、管理量と保存先を`ol:DeflickerCorrection2012`/`ol:DeflickerXMPTarget`に記録します。XMPを適用する再解析は既存管理量に残差を加算し、それ以外は管理量を置き換えます。中間パスはサイドカーを変更せず、最終書き込み時に元のXMPをバックアップします。`--reset --write`は管理分だけを戻し、Holy Grail補正やユーザーの露出変更を残します。Camera Rawの現像パイプラインやLRTimelapseのVisual Deflickerとの数値一致は未対応です。

`set`は`NumericCRSProperties`にある数値のCamera Rawプロパティ131個を明示的に許可し、`set IMAGE PreviewEV VALUE`ではLRTのPreview EVをEV単位で編集でき、XMPの`lrt:ExternalExposureDefault`へ4倍して保存します。GUIの数値編集にもPreview EVを追加しました。HSL色別スライダー、旧式のClarity/Fill Light/Highlight Recovery/増分WB、HDR最大値、カラーグレーディング、色別色相境界、パラメトリックトーン、ノイズ低減、シャープ化、粒子、周辺光量、手動レンズ/パースペクティブ補正などを含みます。名前はAdobeの公開XMP資料、インストール済みXMPテンプレート、LRT 7.5.5の登録情報から選びましたが、LRTのAuto-Transition全項目を網羅すると確認したものではありません。`interpolate`は全体および登録済みローカル数値項目をLRT 7.5.5の3次ベジェスプラインで補間し、`--smoothness 0..100`でLRTの設定値を指定できます（既定50）。登録数値は各プロパティのLRT範囲でクランプし、Integer型はJava互換の四捨五入（.5はゼロから遠ざける）で出力します。先頭フレームのIsMergedHDRがtrueならExposure2012の範囲を-10..10へ広げ、通常は-5..5です。LRT登録外の旧式4項目とColorGradeBalance/ColorGradeHighlightHue/Sat/ColorGradeShadowHue/Satは補間しません。全体プロパティはXMP上の数値を補間し、色相の最短回転はしません。ローカル補正はRDF Seq内の固定位置で照合し、Linear 1–4、Circular 1–2、Brush 1–2、MaskGroup 1–4/8–11を対象に各CorrectionMasks[1]の登録値を補間します。Angleは円形グラデーションとMaskGroup 8/9だけLRTの後処理どおり−45°未満なら90°加算、45°超なら90°減算し、キーフレームにも適用します。`LumRange`は登録上String型のため同じ配列位置の最初の非空値を全フレームへ均等化します。RDF配列がない独自XMPでは共通`CorrectionSyncID`/`MaskSyncID`にフォールバックします。色範囲Type=1ではColorAmountを数値補間し、`PointModels`はLRTの遷移登録に見つからないため各フレームの値を保持します。Depthの画像依存データ、ブラシ描線、登録位置外のマスクは未対応です。MaskGroup 1–4/8–11のMainCurve/RedCurve/GreenCurve/BlueCurveは最初の4つ星（なければ先頭）から同じ位置の対象フレームへコピーしますが、曲線の数値補間は未対応です。非キーフレームで欠けた登録位置のMaskGroup構造は最寄りの実体を持つXMPから複製して作成しますが、キーフレームや登録位置外/Sync ID形式の欠損構造は生成しません。Look/Preset/PointColorsとグローバルポイントカーブは最初の4つ星（なければ先頭）からコピーします。構造均等化の参照フレームは実LRTの保存経路を確認しましたが、PointColors等の全形式の均等化範囲は未確認です。数値一致はLRT 7.5.5との比較前です。`mark`でキーフレームを設定し、`set`でキーフレームの値を入力し、`interpolate --smoothness 50 --write`（LRTの既定スムージング）で間のフレームを補間してから、JPEG/PNG/TIFFでは`render --apply-xmp-basic`で基本8項目、Texture/Clarity/Dehaze/Sharpening/NoiseReduction/Vignette、4領域パラメトリックカーブ、8色域HSL、簡易Temperature/Tint、明示ポイントカーブを、`render --apply-xmp-crop`で回転を含むクロップを画素へ適用できます。既定では既存XMPをバックアップし、`--dry-run`で書き込み内容を確認、`--no-backup`でバックアップを省略できます。Camera Rawの現像エンジンは含まず、レンダー処理は独自近似です。`set`はCamera Rawスライダーごとの許容範囲を検証しません。

`keyframes`は全フレームのXMPに有限な数値`lrt:ExternalExposureDefault`があれば、LRTのフレーム内部値と同じく4で割った値から数量式（最大20）を計算します。`set IMAGE PreviewEV VALUE`またはGUIでPreview EV列を編集すると、この値を入力できます。値が一部でも欠けている場合はJPEG/PNG/TIFF、対応RAWの`--raw-preview`、または`--raw-develop`の現像画像の輝度へフォールバックします。画像解析オプションを明示した場合も画像輝度を使います。同版の整数間隔で候補位置を選びます。数量は最大20で、LRTのヘルパーと同じく1個なら先頭、2個以上なら先頭と末尾を含む配置です。`--rating 4|5`で書き込む星評価を指定でき、既定はLRT Wizardと同じ4つ星です。既定数量からは既存の選択中と同じ評価だけを差し引きます。`--apply-xmp-basic`（`--apply-xmp-exposure`と排他的）/`--apply-xmp-crop`は測定輝度へ反映します。`--evenly-spaced`は既定数量5で測定と画像デコードを省略し、同じ配置ルールを使います。既定動作では候補が既存4*/5*/2*/3*マークに重なるとそのマークを残して候補から外します。`--replace-existing`を指定すると選択中と同じ星評価を先に削除し、別の2〜5つ星が候補位置にある場合は最大9枚前の空きフレームへ移します。`--write`と併用すると各XMPをバックアップ付きで更新します。明るさの曲がりが大きい位置を優先する以前のlrtl独自方式は`--luminance-deviation`で選べます。LRTと同じ数量スライダー、内部の現像輝度値との一致は未対応です。

`unmark --force-rating`はlrtl以外が設定した4つ星/5つ星評価にも使えます。その評価を削除するため、評価がキーフレームマーカーとして使われている場合に限ってください。

`snapshot create`は画像をコピーせず、連番に紐づくXMPサイドカーを`.lrtl/snapshots/<name>.zip`へ保存します。名前を省略するとUTC時刻から自動生成します。`snapshot list`はこのフォルダーのアーカイブを検証して一覧表示します。`snapshot restore`は既定で書き込み内容をプレビューし、`--write`を付けるとアーカイブ内のサイドカーだけを復元します。異なる場所へ移動した連番には`--allow-moved`が必要です。置き換える既存XMPは`.lrtl-snapshot-*.bak`としてバックアップします。スナップショット作成後に追加されたXMPサイドカーは削除しません。移動や削除を含むLRTのシーケンス状態スナップショットとは異なる、XMP限定の復元です。

`holy-grail`の既定はEXIFから露出設定値`log2(露出時間 × ISO / F値²)`を求め、先頭フレームとの差を打ち消す独自補正を計算します。対応形式はJPEG、TIFF、CR2、CR3、DNG、RW2です。全フレームに有効な露出時間とISOが必要です。FNumberがない場合はXMPの`lrt:Aperture`を参照します。マニュアルレンズは先に`lens-tag --aperture f/8 --write`で絞り値を登録してください。EXIF値とXMP値の両方が使えないときは、絞りが固定であることを確認して`--ignore-aperture`を指定します。`--stretch`は露出値を最小・最大で正規化した曲線のスケール（-1.5〜+1.5）、`--rotate`はシーケンスに加える直線傾き（-4〜+4）です。`--auto-shape`はLRTで確認したStretch/Rotateの探索刻み・L1評価を使って値を提案します。

`--lrt-input`は、各フレームのXMPにある数値`lrt:ExternalExposureDefault`を読み、LRTのフレーム内部値と同じく4で割ります。LRTの処理順に沿って、2*/3*/4*評価が隣り合う場所で直近最大10件のフレーム差を重み付き平均で補正し、他のフレームにも直前の補正オフセットを引き継ぎます。その後に`--lrt-reduce-deflicker`（0〜1、既定0）のLOWESS近似、中央寄せ/範囲制限、隣接HGキーフレーム間のカメラ設定差、Stretch/Rotateを適用します。全フレームに同プロパティと有効な撮影露出EXIFが必要です。星評価は新規作成・変更しません。`preview-ev`は復元したRGBヒストグラム式を指定ラスター全体へ適用しますが、LRTのネイティブプレビュー生成・縮小・ROI選択は再現せず、平滑化単体の実測比較だけではLRT Wizard全体との数値一致は保証しません。`--auto-shape`はこのモデル上で探索します。

両モードともHG専用のCamera Raw `LocalExposure2012`スロットがXMPにあればそこへ書き、見つからなければ全体`crs:Exposure2012`を使います。既定はプレビューで、書き込み時は既存サイドカーをバックアップします。EXIFモードでは撮影モードがManualの場合に設定変更位置へ2*/3*評価を提案します。`--lrt-input`では既存のLRT評価をアンカーとして使うため、新しい評価を付けません。補正の再実行では前回分を置き換え、lrtl管理下の元評価も記録するため`--reset --write`で復元できます。

LRTimelapse側では2*/3*が内部HGキーフレームで、4*/5*の編集キーフレームとは別に扱われます。7.5.5のウィザード計算は2*/3*/4*マークを使い、最大10件のフレーム差の重み付き平均を隣接キーフレームで使いながら、フレームごとの曲線を作ります。シーケンス内のフレーム状態への保存処理も一部追跡できました。フレーム状態の3つの値は、MaskGroup形式では`CorrectionName`が`#LRT internal use (HG)`、`(Deflicker)`、`(Global)`の既存スロットへ対応付け、従来形式では`GradientBasedCorrections[5]`〜`[7]`の`crs:LocalExposure2012`へ書き戻します。インストール済みJARの`raw_m_1.xmp`では、従来形式の5〜7番目にある`CorrectionMasks`内の`crs:MaskName`要素が`LRT: don't alter!`という文字列を持ちます。`lrt:ExternalExposureDefault`を1/4にして内部値へ取り込み、`crs:Exposure2012`とは別に保つ経路も確認しました。lrtlは`CorrectionName`が`#LRT internal use (HG)`のMaskGroup、または従来形式の5〜7番目全てで`LRT: don't alter!`署名を確認できた場合に5番目の`LocalExposure2012`へ補正を書き、書き込み先を記録して`--reset`で戻します。いずれの識別条件も満たさないXMPでは`crs:Exposure2012`を使います。LRTのWizardにはRotate（-4〜+4）、Stretch（-1.5〜+1.5）、0〜1の`holyGrailReduceDeflicker`設定があります。lrtlは`--lrt-input`で入力値を読み、10件重み付き差分キュー、隣接フレーム式、曲線の中央寄せ・制限、Stretch/Rotate経路を実装しました。LOWESS単体は実LRTの53ケースで比較していますが、入力値をLRTと同じ画像処理で生成する部分は未対応です。逆解析で確認した式と保存経路は[リバースエンジニアリングノート](docs/reverse-engineering.md)に記録しています。

EXIFモードと`--lrt-input`はどちらも近似で、LRTimelapseと数値一致する機能ではありません。後者はLRTが既にXMPへ記録した入力値を利用し、`preview-ev --write`で同版から復元したヒストグラム計算値を生成できます。LRTのネイティブプレビュー入力とROIは未対応です。Piaの平滑化部品は合成数値列で実測比較済みですが、全ワークフローの比較は未完了です。`preview`は静的な輝度グラフを出しますが、Lightroomの現像画像プレビューは未実装です。センサー感度の誤差、レンズ透過率、自然な明るさの推移も解析しません。RAW現像やXMPの画素反映をする機能ではありません。

## 今後の実装順

| 段階 | 目標 | 状況 |
|---|---|---|
| 1 | XMPサイドカー、キーフレーム、数値設定の補間 | XMPの読み書き、4/5つ星キーフレーム操作、LRT 7.5.5由来の数量/配置規則、127個の全体数値項目と登録ローカル項目のスプライン補間、スナップショット、GUIのExposure2012グラフ、選択フレームのマスター/R/G/Bトーンカーブ点編集を実装。Lightroom/Camera Raw連携画面と連番全体でカーブを同時編集する機能は未実装 |
| 2 | 画像連番から基本タイムラプス動画を生成 | **達成（現像済み画像・対応RAW）**。JPEG/PNG/TIFFからPure GoのMJPEG AVIを出力可能。FFmpegがあればH.264/H.265 MP4、ProRes/DNxHR MOVも出力でき、バッチレンダー、縮小、クロップ、日時表示、音声、Deflicker、モーションブラーに対応。RAWはAdobe DNG ConverterとGoによる実画素現像、内蔵プレビュー、明示したDNGLabから選択可能 |
| 3 | 画像解析、プレビュー、Deflicker | 静的SVG輝度プレビュー、UIのXMP Exposure2012/解析画像輝度グラフ、選択フレームのRGBトーンカーブ編集、静止画プレビューと最大640 pxの連番ループ再生、長期撮影指標、XMP画素反映、回転クロップ、参照領域/全体露出Deflickerを実装。LRT Visual Previewと同等の再生速度/画素再現、Exposure2012の複数フレーム同時編集、フレーム間のトーンカーブ編集、XMPロスレス適用、Geometry補正、未対応マスク形式の画素現像は未実装 |
| 4 | RAW現像、Holy Grail、XMP現像値の画素への反映 | EXIF推定と既存LRT XMP入力を使う近似Holy Grail補正、現像済みラスターへの対応XMP値適用、一部RAW内蔵プレビュー、Adobe DNG ConverterとGoによるRAW現像（DNGLabも任意選択）を実装。Camera Raw現像の完全再現と同等のRAW画素処理は未実装 |
| 5 | 残りのCLIワークフローと取り込み機能 | Go標準`net/http`で動くループバック限定のローカルWeb UIで、サムネイル、キーフレーム、XMP補間、レンダー操作を実装。動画からのフレーム抽出、画像/XMP取り込み、lossless/uncompressed DNG変換、追加コーデックも実装。GUIの追加開発は対象外。CLIの動画自動分割、ロッシーDNG/サイズ縮小、LRTと同じ10-bit YUV画素処理・色管理は未対応（現状は16-bit RGBを経由） |

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
- [ExifTool's Adobe XMP UUID definition for ISO Base Media boxes](https://github.com/exiftool/exiftool/blob/master/lib/Image/ExifTool/QuickTime.pm#L3071)
- [libopenraw's Canon CR2 format notes](https://libopenraw.freedesktop.org/formats/cr2/)
- [libopenraw's Panasonic RW2 format notes](https://libopenraw.freedesktop.org/formats/rw2/)
- [libopenraw's Nikon NEF/NRW format notes](https://libopenraw.freedesktop.org/formats/nef/)
- [libopenraw's Kodak DCR format notes](https://libopenraw.freedesktop.org/formats/dcr/)
- [UK National Archives PRONOM's Kodak DCR TIFF signature](https://www.nationalarchives.gov.uk/PRONOM/Format/proFormatSearch.aspx?id=917&status=detailReport&strPageToDisplay=signatures)
- [libopenraw's Olympus ORF custom TIFF magic values](https://libopenraw.freedesktop.org/formats/orf/)
- [libopenraw's TIFF-based RAW format identification](https://libopenraw.freedesktop.org/api/libopenraw/stable/rawfile_8cpp_source.html)
- [libopenraw's Pentax PEF format notes](https://libopenraw.freedesktop.org/formats/pef/)
- [ExifTool's Panasonic RAW embedded JPEG tag definition](https://github.com/exiftool/exiftool/blob/master/lib/Image/ExifTool/PanasonicRaw.pm#L193)
- [libopenraw's Olympus ORF format notes](https://libopenraw.freedesktop.org/formats/orf/)
- [libopenraw's Fujifilm RAF header and JPEG offsets](https://libopenraw.freedesktop.org/formats/raf/)

## Adobe DNG Converterを使うRAW処理

NEF/NRW、CR2/CR3/CRW、ARW/SR2/SRF、RAF、ORF、RW2、DNGなどに加え、RAW/RWL/GPR/X3F拡張子も連番読み込み・現像・DNG取り込みの対象として認識します。実際の機種対応は[Adobeの対応カメラ一覧](https://helpx.adobe.com/camera-raw/desktop/dng-and-file-formats/camera-raw-plug-supported-cameras.html)とインストール済みConverterの版に依存します。

`--raw-develop`の既定バックエンドはAdobe DNG Converterです。PATHと、Windows標準インストール先またはmacOSのアプリケーションを探します。場所が異なる場合は`--dng-converter PATH`で指定します。従来のDNGLabを使う場合は`--dnglab PATH`を明示してください。2つのバックエンド指定は同時に使えません。

```powershell
# RAWセンサー実画素からAVIを生成（DNG Converterが必要、FFmpeg不要）
lrtl render "D:\Timelapse\RAW" --raw-develop --auto-orient --long-edge 1920 --output "D:\Timelapse\raw.avi"
# MP4へ出力（FFmpegも必要）
lrtl render "D:\Timelapse\RAW" --raw-develop --long-edge 1920 --output "D:\Timelapse\raw.mp4"
# 解析コマンドでも同じ現像経路を使用
lrtl keyframes "D:\Timelapse\RAW" --raw-develop
lrtl preview-ev "D:\Timelapse\RAW" --raw-develop
# AdobeによるDNG変換とXMP取り込み
lrtl import "E:\DCIM" --output "D:\Timelapse\DNG" --convert-dng
```

Converterを`-u -l -p0 -dng1.4`で呼び、一時フォルダーにデモザイク済み・未圧縮Linear DNGを生成します。その実画素から黒/白レベル、AsShotホワイトバランス、CameraCalibration/AnalogBalance、ForwardMatrixまたはColorMatrix、BaselineExposureを使ってGoで16-bit sRGBを生成します。ストリップ/タイル、DefaultCrop/ActiveArea、DefaultScaleに対応します。`--raw-preview`は従来どおりカメラ内蔵プレビューを使う別の入力モードです。元RAWと隣接XMPは現像処理で変更しません。

この現像はAdobeのデモザイクと独自の色変換を組み合わせたものです。デュアル光源の色温度推定、色域外のクリップ、画素端のクロップ丸めは近似です。AdobeプロファイルのHue/Sat/Lookテーブル、トーンカーブ、DNG Opcodeによる補正、Camera Rawの全現像工程は再現しません。追加XMPは`--apply-xmp-basic`/`--apply-xmp-crop`を指定した場合に既存のlrtl近似処理で適用します。対応カメラはインストール済みConverterに依存し、変換失敗時は内蔵JPEGへ自動フォールバックせずエラーを返します。Converterの出力と1枚分の復号画像にはそれぞれ1 GiBの上限を設け、Converter実行は5分でタイムアウトします。

自動テストはAdobeを必要としない既知画素のLinear DNGと代替プロセスを使い、黒/白レベル、ガンマ、WB/色行列、タイル境界、異常データ、CLI/GUIの各経路と一時ファイルの後始末を確認します。実機ではAdobe DNG Converter 18.7でNikon D3S NEFとCanon 40D sRAW CR2を現像し、NEF連番のAVI、CR2のH.264 MP4、CR2→DNG取り込み→AVIを確認しました。完全なCamera Raw画素一致の検証ではありません。仕様は[AdobeのCLI資料](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section/download-1/dng_converter_commandline.pdf)と[DNG仕様](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section_733958301/download-1/DNG_Spec_1_7_1_0.pdf)を参照しました。

追加の実機確認として、依存パッケージ`imagemeta v0.3.1`のテスト素材`Hero8.GPR`もAdobe DNG Converterで4000×3000の実画素から現像し、GUIプレビューを生成できることを確認しました。通常の自動テストはこれらの実RAWをダウンロードせず、合成データのみで動作します。

## Preview EVの生成

`preview-ev DIRECTORY`は連番画像から各フレームのPreview EVを測定し、`lrt:ExternalExposureDefault`への保存値も表示します。復元したLRT 7.5.5の計算に合わせ、RGB各256段階のヒストグラムを作り、各ヒストグラムを半径2の移動平均で平滑化してから全チャンネルの平均強度を求めます。Preview EVは平均値を256で割った値で、XMP保存値はその4倍です。既定では表示のみで、`--write`を付けるとXMPサイドカーを更新します。既存ファイルはバックアップされます。`--raw-preview`と`--raw-develop --dnglab PATH`、`--apply-xmp-basic`、`--apply-xmp-crop`も指定できます。

```powershell
lrtl preview-ev "D:\Timelapse\Developed"
lrtl preview-ev "D:\Timelapse\Developed" --write
```

この測光式は復元したものですが、LRTが利用するネイティブのプレビュー縮小・色処理と選択ROIは再現していません。lrtlは入力ラスター全体を測定します。そのため保存値はLRTの表示プレビューから得る値と異なることがあります。XMP値がない連番では、先に`preview-ev --write`を実行すると、`keyframes`の既定数量計算で使える値を生成できます。

## 連番の初期化

```powershell
# 既定では予定を表示するだけ
lrtl init "D:\Timelapse\RAW"
# 補正スロットをXMPサイドカーへ保存
lrtl init "D:\Timelapse\RAW" --write
# 旧形式を明示して作成
lrtl init "D:\Timelapse\RAW" --mask-format legacy --write
```

既定の`--mask-format auto`は既存のマスク形式を保ち、マスクがないフレームでは連番内で最初に確認できた形式（連番全体にマスクがなければmodern）を使います。`modern`/`legacy`を明示することもできます。形式の変換は行いません。

Modernは`MaskGroupBasedCorrections[1..9]`に4つの編集用線形マスク、HG/Deflicker/Globalの3つの全画面補正、2つの編集用円形マスクを準備します。Legacyは同じ役割を`GradientBasedCorrections[1..7]`と`CircularGradientBasedCorrections[1..2]`に分けます。追加する補正値は中立です。内部レイヤーはLRTの識別名/旧形式の署名に対応し、`holy-grail`と`deflicker`がそれぞれの専用スロットを使えます。既存のSync IDは保持し、追加するスロットのIDは連番で共有します。

既存の編集用マスクは変更せず、内部スロットも既存の露出・Active/Amount・形状を保持して不足項目を補います。予約位置5〜7に無関係なマスクがある場合、内部ロールが別位置にある場合、または既存内部レイヤーと異なる形式を強制した場合は中断し、移動や上書きをしません。全フレームを確認してから書き込みを始めます。書き込み時の既存サイドカーは既定でバックアップし、再実行で変更がなければ書き込みません。サイドカーのない画像の埋め込みXMPも読み、未知の値を引き継いでサイドカーへ保存します。

Camera RawのVersion/ProcessVersion/HasSettingsがない場合に限り、観測した互換初期値14/11/Trueを補います。既存の現像設定の消去、全スライダーのリセット、キーフレームの自動作成、LRTのシーケンス状態ファイルの生成は行いません。製品テンプレートは同梱せず、配列構造をGoで生成します。LRT/Camera Rawでの読み込み往復や全初期化機能の同等性は未検証です。

初期化→キーフレーム→編集→補間→Deflicker→AVI出力をCLIの回帰テストで実行しています。実NEFでも初期化→キーフレーム→Holy Grail→補間→DNG Converter現像→10-bit ProRes出力（640×426、2フレーム）を確認しました。

## CLIで局所補正・マスクを編集

```powershell
# initで準備した配列位置と値を確認（JSON出力も可能）
lrtl masks "D:\Timelapse\RAW\frame001.nef"
lrtl masks "D:\Timelapse\RAW\frame001.nef" --json
# 局所露出とグラデーションの位置を編集
lrtl set "D:\Timelapse\RAW\frame001.nef" 'MaskGroupBasedCorrections[1].LocalExposure2012' -0.5
lrtl set "D:\Timelapse\RAW\frame001.nef" 'MaskGroupBasedCorrections[1].CorrectionMasks[1].FullY' 0.3
# 円形マスクの角度と反転
lrtl set "D:\Timelapse\RAW\frame001.nef" 'MaskGroupBasedCorrections[8].CorrectionMasks[1].Angle' 20
lrtl set "D:\Timelapse\RAW\frame001.nef" 'MaskGroupBasedCorrections[8].CorrectionMasks[1].MaskInverted' true
```

パスは`COLLECTION[N].PROPERTY`または`COLLECTION[N].CorrectionMasks[M].PROPERTY`で、添字は1始まりです。Legacyの`GradientBasedCorrections`/`CircularGradientBasedCorrections`、`PaintBasedCorrections`にも対応します。任意の`crs:`接頭辞を付けられます。対象の補正/マスク要素は既に存在する必要があり、`init`で標準スロットを作成できます。未知の値や構造、Sync ID、キーフレームは保持します。`rdf:Description`でラップされた項目や異なる名前空間接頭辞にも対応します。

数値は保存XMPの単位です。たとえばLocalExposure2012はEV（この編集経路の許容範囲はLRT登録に合わせて−1〜+1）、LocalContrast2012/LocalSaturation等は−1〜+1の正規化値、座標は画像の正規化座標です。LRT 7.5.5の登録範囲と整数型を検証し、範囲外やNaN/Infは保存しません。CorrectionActive、MaskActive、MaskInverted、Flippedは真偽値で設定できます。形状プロパティは線形/円形の対応する項目だけを受け付けます。`--dry-run`で書き込み前の確認、`--no-backup`でバックアップ省略ができます。画像内XMPを読む場合も保存先はサイドカーで、元画像を変更しません。

キーフレーム上の局所値や形状を編集して`interpolate --write`を実行し、`render --apply-xmp-basic`で動画に反映できます。一覧の`transition=true`は数値XMP補間の登録位置を表し、レンダー対応の印ではありません。補間対象の配列位置は実装状況表のAuto-Transition行を参照してください。CorrectionMasksは1番目が補間対象で、2番目以降はフレームごとに保持されます。真偽値は数値補間せず、そのフレームだけの設定です。ブラシ/AI等の画素現像や、複雑なAggregate内部をたどるパス編集、範囲マスクのサンプル点編集は未対応です。

旧/新形式の両方で、初期化→キーフレーム→局所露出/グラデーション位置の編集→補間→AVI出力までをテストし、マスク外の画素を保ちながら局所露出がフレームごとに変化することを確認しています。実NEF3フレームでも同じ局所編集・補間を行い、640×426・10-bit ProResを生成できました。これはlrtlの処理連携の検証で、LRT/Camera Rawとの画素一致は未確認です。

## CLIで構図を編集・アニメーション化

```powershell
# 現在のXMP構図を確認（RAWの現像は不要）
lrtl crop C:\photos\sequence\frame001.nef --json

# 連番の既存の矩形を有効化する。数値・星評価は維持
lrtl crop C:\photos\sequence --enable

# 始点と終点の矩形を設定し、5つ星クロップキーフレームとして保存
lrtl crop C:\photos\sequence\frame001.nef --rect 0,0,0.7,1 --angle 0 --mark
lrtl crop C:\photos\sequence\frame300.nef --rect 0.3,0,1,1 --angle 0 --mark
lrtl interpolate C:\photos\sequence --write
lrtl render C:\photos\sequence --output pan.mp4 --raw-develop --apply-xmp-crop

# 書き込み前の確認、角度だけの変更、全画面への解除
lrtl crop C:\photos\sequence\frame001.nef --angle -5 --dry-run
lrtl crop C:\photos\sequence\frame001.nef --angle -5
lrtl crop C:\photos\sequence --clear
```

`--rect`はXMPのLeft,Top,Right,Bottomに対応する0〜1の正規化座標です。Left < Right、Top < Bottomが必要で、新しく指定する角度はLRT 7.5.5の登録範囲に合わせて−45〜+45度です。矩形や角度を指定すると`HasCrop=True`も保存します。省略した値は維持します。`--enable`は既存の矩形を保って有効化し、`--clear`は矩形を0,0,1,1、角度を0、`HasCrop=False`へ戻します。解除しても星評価やキーフレームマークは残ります。

`--mark`は単一画像/XMPだけに指定でき、HG用2/3つ星の上書きを拒否します。通常の編集は星評価を変更しません。`interpolate`は通常の現像値を4つ星、クロップ値を5つ星（なければ4つ星）で補間します。`HasCrop`を含む全体スカラーはLRTの別処理に合わせ、参照フレームと4つ星の配置でコピー・欠損補完が変わります。5つ星だけの連番では先頭フレームの真偽値へ揃うため、上記の`--enable`で全体を明示的に有効化してから構図を編集してください。`--json`は編集を伴わない確認用で、常に画像ごとの配列を返します。

編集は既定でサイドカーへ保存し、既存XMPをバックアップします。`--dry-run`で保存せずに確認でき、`--no-backup`でバックアップを省略できます。同じ編集の再実行では変更のないXMPを書き直しません。連番では全フレームのパケットと候補を検証した後、各サイドカーを原子的に保存します。画像内XMPからの読み込み、通常のbasename.xmp、画像のないXMP単体にも対応します。元画像は変更しません。

合成画像列の回帰テストでは、端点の構図設定→補間→MJPEG AVIの画素を検査してパンの進行を確認しています。既存の回転クロップ描画を利用する独自実装で、Camera Rawとの画素一致や回転後の自動フィット、遠近補正は未対応です。

## 補間時の全体スカラー

登録済みの全体数値は、4/5つ星のアンカーを選ぶ前に欠損を補完します。途中と末尾の欠損は直前の有効値、先頭の欠損は最初の有効値を使い、ゼロは有効値として扱います。非キーフレームの値も補完元になり、欠けたキーフレーム項目にも保存されます。全フレームで欠けている項目は作りません。WB数値4項目はこの事前補完をせず、従来のWB・数値補間経路で扱います。

局所補正は、登録されたRDF配列位置で既存の補正・`CorrectionMasks[1]`内の欠けた数値を同じ方法で補完します。局所露出、トーン・色の数値、マスク座標などが対象で、IDやActive/反転フラグは補完しません。全体WBの除外ルールはLocalTemperature等へは適用しません。要素そのものがない選択キーフレーム、空のRDF要素、Sync ID独自形式、未登録位置、2番目以降のセレクターへの事前補完は未対応です。非キーフレームに欠けた登録構造を近傍から複製する既存処理は別経路です。RangeMaskの数値はその子要素が既にある場合に補完し、子要素を新設しません。不正な登録数値は書き込み前にエラーにします。

`HasSettings`、`HasCrop`、`UprightPreview`、`ConvertToGrayscale`と、WB名・CameraProfile/Digest・ToneCurveName2012・CompatibleVersion・LookName・LensProfile名/ファイル/Digest/Setup・Uprightの文字列を処理します。数値スプラインとは別の経路で、4つ星が1つ以下、端点を含むアンカー値が同じ、端点2点だけ、3点以上で値が異なる場合を区別します。真偽値を数値として補間することはありません。欠損補完は前方への引き継ぎ後に先頭の欠損を後方から埋めます。文字列ワーカーでは空文字を飛ばしますが、空白だけの値は有効値です。

全体スカラーの参照は最初の4つ星（なければ先頭画像）です。参照に当該値がない場合は、他フレームの同じプロパティを削除する経路があります。4つ星に`WhiteBalance=Custom`があれば全体をCustomへ揃え、参照がAs ShotならTemperature/Tint/IncrementalTemperature/IncrementalTintを解除します。通常の数値キーフレーム保持とは異なり、スカラー処理はキーフレームにも変更を加えます。まず`interpolate DIRECTORY`で変更と`(remove)`表示を確認し、`--write`でバックアップ付き保存します。

実LRT 7.5.5の欠損補完・文字列列ワーカーを呼び出した12ケースを保存し、通常テストではGoだけで比較します。全体スカラー22+4項目の処理であり、局所補正のActive/Flipped等、crd名前空間、LRTの全XMP保存結果や現像画素との完全一致は対象に含めていません。

## CLIでトーンカーブを編集

```powershell
# 全体マスターの確認。--channel red|green|blueでRGB別のカーブ
lrtl curve C:\photos\sequence\frame001.nef --json

# 最初の4つ星参照フレームへカーブを設定
# セミコロンがPowerShellの区切りにならないよう、点列を引用する
lrtl curve C:\photos\sequence\frame001.nef --points '0,0;128,96;255,255' --mark

# 既存の局所補正内の赤チャンネル曲線を編集
lrtl curve C:\photos\sequence\frame001.nef --mask 'MaskGroupBasedCorrections[1]' --channel red --points '0,0;128,144;255,255'

# 参照曲線を連番へ均等化して、RAWから動画を作る
lrtl interpolate C:\photos\sequence --write
lrtl render C:\photos\sequence --output curves.mp4 --raw-develop --apply-xmp-basic

# 連番へ同じ曲線を直接設定、書き込み前の確認、直線へ戻す操作
lrtl curve C:\photos\sequence --channel blue --points '0,0;128,112;255,255' --dry-run
lrtl curve C:\photos\sequence\frame001.nef --linear
```

点列は2〜64個の整数X,Yペアで、各座標は0〜255、Xは厳密な昇順、最初と最後のXは0と255が必要です。Yは非単調でも指定できます。新規カーブはRDF Seqで作り、既存の属性・単純テキスト・対応するRDF Seqはその表現で編集します。チャンネルは`master|red|green|blue`で、局所補正ではMainCurve/RedCurve/GreenCurve/BlueCurveに対応します。局所補正の要素は既に存在する必要があります。HG/Deflicker等の内部補正はこの編集経路では保護されます。

パスだけなら読み取り専用で、`--json`は画像ごとの配列を返します。`found=false`の点列は未設定時の直線表示で、名前だけのプリセットを展開したものではありません。`editable=false`の複雑な構造・点領域や、同名カーブの重複は編集を拒否します。`equalized`は`interpolate`の構造均等化対象かを示します。通常の全体カーブと登録MaskGroupの1〜4/8〜11が対象で、旧形式のRGB曲線やLegacy局所曲線など対象外の曲線は必要に応じてディレクトリーへの直接設定を使います。点のフレーム間アニメーションは実装していません。

`--points`または`--linear`は既定で保存し、元XMPをバックアップします。`--dry-run`、`--no-backup`に対応し、同じ内容は再保存しません。`--mark`は編集と同時に単一画像を4つ星にし、HGの2/3つ星は上書きを拒否します。連番の編集は全パケットの検証後に、各サイドカーを原子的に保存します。元画像、他のチャンネル、未知のメタデータを保持します。

全体／局所の両方で、参照フレームへの編集→均等化→MJPEG AVIの画素を検査する回帰テストがあります。RDF名前空間の別名や衝突、旧形式、画像内XMP、元内容のバックアップ、不正な後続パケットでも検証しています。画素反映は既存の16-bit PCHIP近似で、Camera Rawとの一致やHDR曲線、未対応マスクの描画は未対応です。

## XMP現像の精度

対応済みの全体/ローカル現像処理は、NRGBA64（RGB/アルファ各16-bit）と浮動小数点の計算を使います。全体の露出・WB・トーン・彩度/HSL・Color Grading・ポイント/パラメトリックカーブ、Texture/Clarity/Dehaze、シャープ・ノイズ低減・周辺減光、および対応マスクの局所調整が対象です。空間フィルターの補助層はfloat32、主要演算はfloat64です。ポイントカーブはXMPの0〜255点座標からPCHIP係数を作り、65536段階の入力位置で評価し、フレームごとに巨大なLUTを持ちません。

全体22ケース、ローカル12ケースで細かな入力差・アルファ・元画像の保持を確認し、カーブは全65536段階の恒等性、宣言点の通過、単調性とファズテストを検証します。これはlrtl内での精度を確認するものです。Camera Rawの演算式・カメラプロファイル・ICC色管理・HDRの再現、およびLRTとの画素一致は未達です。Deflicker測光など一部の解析値は従来の統計定義のままで、Preview EVの256ビンは観測したLRT仕様に合わせています。

## 開発

Goの機能追加・修正では、数値の期待値、境界条件、異常入力、既存XMPの保持とバックアップを回帰テストで確認します。数値テストは手計算や独立した参照値を使い、実装の計算をそのまま複製して期待値にはしません。変更後は以下のテスト・静的解析・ビルドを実行します。GitHub ActionsもWindows/Linuxで同じ3項目を実行します。

現在の自動テストはXMP読み書き・補間、キーフレーム、Preview EV、Deflicker、MJPEG AVI生成などを対象としています。平滑化単体はLRT 7.5.5の実測値53ケースと比較済みですが、LRTが実際に出力した一連のXMPとの数値比較、ブラウザーのGUI操作、外部FFmpeg/DNGLabを含む処理全体の検証は未整備です。ビルド成功だけでは動作確認済みとしません。

```powershell
go test ./...
go vet ./...
go build -o lrtl.exe ./cmd/lrtl
```

互換性の観察と既知の制約は[リバースエンジニアリングノート](docs/reverse-engineering.md)を参照してください。

## ライセンス

MIT。詳細は[LICENSE](LICENSE)を参照してください。
