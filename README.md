# LumaFrame (`lrtl`)

LRTimelapseと同じ作業を段階的に実現することを目指す、独立したオープンソースのタイムラプスツールです。Lightroom互換XMPのキーフレーム編集やHoly Grail露出補正に加え、現像済みJPEG/PNG/TIFFとRAW連番からMJPEG AVI、H.264 MP4／H.265 MP4・MOV、ProRes MOVを生成できます。

**RAW連番は、インストール済みのAdobe DNG Converterでデモザイクし、Goで現像して動画にできます。** `render --raw-develop`はConverterを自動検出し、実画素のLinear DNGを16-bit sRGBへ変換します。JPEG/PNG/TIFF連番とMJPEG AVIの処理はPure Go、MP4/MOV出力にはFFmpegも必要です。`--dnglab PATH`で従来のDNGLabバックエンドも選べます。Camera Raw/Lightroomのプロファイル・全設定と画素一致する現像エンジンではありません。

本プロジェクトはLRTimelapseおよびAdobeと無関係です。独自実装であり、それらのコード、画像、同梱ツールを含みません。

## 開発方針

**CLIでLRTimelapseの処理とワークフローに近づけることを優先します。GUIの追加開発は対象外です。** 数値処理は実際のLRT 7.5.5で得た観測値との比較を増やし、対応状況と未確認の範囲をこのREADMEに記録します。

### 今回の互換性改善

- `preview-ev`でLRT 7.5.5のカメラプレビュー縮小を再現しました。画像幅が設定値を超える場合、既定の1024×682へ1パスbilinearで縮小してから測光します。LRT本体の合成画像3条件でPreview EVを1e-5以内に比較し、拡大・縮小を含む6条件の82画素（246色成分）も完全一致を確認しました。`--preview-width-camera N`でLRTの設定幅も指定できます。LRT/Camera Rawの元画像デコード・色管理は未対応です。[JAR解析・オラクル・制限](internal/lrtl/testdata/lrt-preview-resize-7.5.5.md)
- Native Deflickerの画像測光を実LRT 7.5.5のキャッシュ経路6条件と比較しました。LRT復号済み画像では測光値が`1e-10 EV`以内で一致し、JPEG復号差を含む元JPEGでも`0.007 EV`以内です。GoとLRTではJPEGの色差サンプル補間が異なるため、JPEG復号の完全一致は未達です。[オラクル・条件・残る差](internal/lrtl/testdata/lrt-native-deflicker-image-7.5.5.md)
- Linear DNGの`OpcodeList2/3`に、放射状ビネット補正`FixVignetteRadial`（ID 3）を追加しました。r²〜r¹⁰の5係数と正規化中心を読み、Opcodeの順序どおり線形カメラ値へ適用します。仕様式との独立計算、両バイト順、色面、クリップ、RAW→16-bit TIFF→AVIと失敗時の出力取消をテストしています。Camera Rawの画素一致を示すものではありません。
- Linear DNGの`OpcodeList2/3`に`WarpRectilinear2`（ID 14）を追加しました。r⁰〜r¹⁴の係数、多項式の有効半径、逆数モード、色面別の中心を使い、オプションOpcode直後の旧式ワープを省く互換規則も処理します。独立座標式、破損データ、両バイト順、RAW→16-bit TIFF→AVIをテストしています。
- Linear DNGの`OpcodeList2/3`に`WarpFisheye`（ID 2）を追加しました。DNG仕様のarctan写像、共有／色面別の係数、`DefaultScale`の画素比を使い、既存のバイキュービック再サンプルへ接続します。独立した座標計算、異常入力、fuzz、RAW→16-bit TIFF→AVIの画素までテストしています。欠陥画素補間などは引き続き未対応です。
- Linear DNGの`OpcodeList2/3`に`TrimBounds`（ID 6）を追加しました。Opcode順に画像領域を切り詰め、後続補正とDefaultCropを反映します。ActiveArea／DefaultCropとの交差、切り詰め後の座標を使う補正、両バイト順、不正領域をテストしています。
- DNG 1.6/1.7の`ProfileGainTableMap`（52525）と`ProfileGainTableMap2`（52544）をLinear DNG現像へ追加しました。後者がある場合は仕様どおり前者より優先し、8/16-bit整数・float16/float32のゲインとガンマ入力に対応。両バイト順、4形式の独立期待値、優先順位、異常データ、ActiveAreaオフセットを含む現像画素をテストし、パーサーをfuzzしています。AsShotProfileNameまたはsidecarのcrs:CameraProfileから埋め込みCamera Profile IFDを選択し、プロファイル署名でCameraCalibrationの適用を制御します。HDR/オーバーレイ保持は未対応です。
- DNG 1.6/1.7の`RGBTables`（52543）をLinear DNG現像へ追加しました。8/16-bit整数と32-bit floatの3D LUT、5種類のGammaEncoding/ColorPrimaries、三線形補間、色域拡張、Weighted Sum/Sequential合成、Semantic Mask IFDと`MaskSubArea`に対応します。両バイト順、全データ型・ガンマ・原色、合成方式、32-bit float/Deflate/JPEG/タイルマスク、ActiveAreaオフセットを含む現像画素をテストし、DNG全体のfuzz seedにも追加しました。Lossless JPEGマスクと幾何Warpを伴うSemantic Maskは未対応です。
- DNG 1.6の三光源カメラプロファイルを追加しました。ColorMatrix1/2/3・CameraCalibration1/2/3・ForwardMatrix1/2/3を同じ三光源ウェイトで補間し、`ProfileHueSatMapData1/2/3`をRAW画素へ適用します。Adobe DNG SDKの温度・Tint距離、平滑化・小ウェイト抑制を参照し、標準光源とxy形式のカスタム`IlluminantData`を扱います。固定参照ウェイト、補間表、行列、両バイト順の現像画素、別バイト順の選択Camera Profile IFD、欠損・重複・不正タグをテストし、Linear DNGファズ入力にも追加しました。カスタムスペクトル形式の`IlluminantData`、4面以上の色データ、HDR/オーバーレイ保持は未対応です。

- `--apply-xmp-basic`にCamera Rawの`PerspectiveX`／`PerspectiveY`オフセットを追加しました。XMPの`set`で編集でき、Xは正値で右、Yは正値で上へ画像を移動します。係数は公開されたLightroom書き出し計測の+20値を基準に線形近似し、独立座標期待値とAVI画素までテストしています。この2項目はLRT 7.5.5の登録遷移列に含まれないため、`interpolate`では遷移させません。

- 動画・TIFF・JPEGプレビュー・字幕で`--sort`の順序を選べます。`lrt-settings`は各連番に保存されたLRTの名前順／日時順を使います。本体方式の自然順・文字順を4,096組、日時順を8条件で比較しました。動画と字幕の選択・順序を合わせ、プレビューの元インデックスと測光保存先も保持します。[規則・時刻順との違い・テスト](docs/sequence-order.md)を参照してください。

- `holy-grail --lrt-preview camera|visual`で既存のLRTプレビューキャッシュを測光し、XMPへPreview EVを書き込まずにLRT互換の補正計算へ渡せるようにしました。`--lrt-reference`で保存済み参照領域も適用できます。キャッシュと`preview-ev --write`経由の数値一致、保存済みXMP値との同等性、キャッシュ欠落時の非書込み、設定・バッチ経路をテストしています。キャッシュ生成とLRT本体のRAW現像は引き続き対象外です。

- `remove-sequence`で選択画像を隣の`_REMOVED`フォルダーへ除外できます。既存先への追記・同内容XMP／キャッシュの再利用に対応し、`move-sequence --merge`でも同じ処理を使えます。追記途中の衝突・復元失敗・共有XMPの変更検出、繰り返し除外後のAVI画素をテストしています。[対象・衝突・本体との比較範囲](docs/remove-sequence.md)を参照してください。

- `move-sequence`で選択画像・XMP・同名JPEG・画像別キャッシュを新規フォルダーへ移動できます。SHA-256検証、共有データの元への保持、失敗からの復元、読み取り専用ハードリンク、Windows／Linuxとファイルシステムをまたぐ移動、AVI画素一致をテストしています。[使用方法・復元と制限](docs/move-sequence.md)を参照してください。

- `copy-sequence --include-lrt-companions`で同名JPEGと通常／Visual Preview・プロキシの付随データもコピーできます。本体のパス計算24条件と比較し、追加JPEGの採用中XMP、共有キャッシュの重複排除、変更検出・元連番保持、AVI画素一致をテストしています。フラグなしの対象は従来どおりです。[対象ファイル・フレーム数の増加・比較範囲](docs/sequence-companions.md)を参照してください。画像単位の移動は`move-sequence`に対応しました。

- `rename-sequence DIRECTORY --name NEW_NAME`でシーケンスのフォルダーを改名できるようにしました。既定はプレビューで、`--write`時も既存フォルダーを上書きしません。画像・XMP・`.lrt`・バックアップ等をそのまま移し、大小文字だけの変更にも対応します。Windows／Ubuntuの実ファイルシステム、失敗からの復元、AVI画素一致をテストしています。[使用方法・対応OS・制限](docs/rename-sequence.md)を参照してください。

- 既定`init`の画像内XMP採用を、本体ファイル読込420条件に合わせてJPEG／DNG／TIFFに限定しました。NEF・CR2・ARW・ORF・RW2・RAF・CR3等はサイドカーだけを採用します。通常のGo読込と`--frame-state preserve`は従来の埋め込みXMP対応を維持します。6形式の初期化→AVI画素、保存前エラー、再実行も検査しています。[動作変更・採用元・厳密エラーの差](internal/lrtl/testdata/lrt-raw-xmp-source-7.5.5.md)を参照してください。

- Phase One IIQのExif寸法優先を52条件で比較し、初期化・円形マスク補間へ反映しました。標準TIFFのJPEGプレビューを持つIIQを`--raw-preview`で動画化でき、通常読込／`preserve`では埋め込みXMPも読めます。既定初期化は本体のファイル読込6条件に合わせ、IIQのサイドカーだけを採用します。AVIの復号画素・バックアップ・再実行もテストしています。[対応条件・採用元の違い・残る範囲](internal/lrtl/testdata/lrt-iiq-dimensions-7.5.5.md)を参照してください。

- Canon CR3の寸法読込を追加しました。CMT1／CMT2とトラック寸法の順序、CR2とのExif優先順位の違い、整数境界などを実機76条件で比較し、`init`の保存と円形マスク補間に反映します。BigTIFF入りCR3で見つかったメタデータ依存のpanicも修正し、エラー時の保持とAVIのJPEG対照一致をテストしています。[規則・検証・未対応範囲](internal/lrtl/testdata/lrt-cr3-dimensions-7.5.5.md)を参照してください。

- RAF（Fujifilm RAW）の寸法読込を追加しました。埋め込みJPEGとRAWクロップの優先順位、複数ディレクトリ、重複・欠損・ゼロ寸法を実機66条件で比較しています。`init`の寸法保存と円形マスク補間に使い、RAFプレビューからの3フレームAVIも抽出JPEG対照と全画素一致を確認しました。[規則・テスト・未対応範囲](internal/lrtl/testdata/lrt-raf-dimensions-7.5.5.md)を参照してください。

- レーティング再読込を実機3,440条件に合わせ、1→4、厳密な整数解釈、直近の`LRT_`フォルダーでのHG評価解除を既定コピー保存に反映しました。URI／修飾子付き評価もキーフレーム選択・補間で読み取り、mark／unmarkやHG処理では入れ子の別メタデータを保持します。回帰・race・fuzzテストと、補間後のAVI復号画素比較を追加しています。[規則・テスト・未対応範囲](internal/lrtl/testdata/lrt-reload-rating-7.5.5.md)を参照してください。

- 全体の`copy-settings`で、現行形式の既定スライダー・線形カーブ等を補うようにしました。ファイル名とQualityでRAW系／FLAT系を選び、既存値を優先して統合します。実機の既定84条件・統合432条件、空参照・共有XMP・再実行、既定値を適用したAVI画素を検査しています。[既定値・動作変更・観測範囲](internal/lrtl/testdata/lrt-reload-defaults-7.5.5.md)を参照してください。

- 全体の`copy-settings`へマスク再構成を追加しました。現行9項目の準備、ZeroYによる内部3役割の修復、旧形式から現行形式への変換を本体684条件の初回・再実行で比較しています。内部露出を保ったAVI画素比較、共有サイドカー、バックアップ・再実行も検査済みです。本体と同じく配置修復で途中の補正を削除する場合があるため、[具体的な規則・識別子の違い・残る範囲](internal/lrtl/testdata/lrt-mask-preparation-7.5.5.md)を参照してください。

- 全体の`copy-settings`でも日時キャッシュと`HasSettings`を再読込するようにしました。統合後CreateDate／Originalの選択、日時不明時の`n/a`、サブ秒2桁化と保存時の実時計更新を反映します。実際の全体貼り付け経路2,080条件と比較し、選択列コピーとの違い、保存前のエラー検出も検査しています。[対応範囲・観測・残る差](internal/lrtl/testdata/lrt-full-paste-date-7.5.5.md)を参照してください。

- `copy-settings --properties`の既定保存で、対象の撮影日時キャッシュを維持し、条件を満たす場合に撮影日時4項目と実時計の`MetadataDate`を反映します。実機640条件、プレビュー・バックアップ・再実行・後続フレームのエラー時に全ファイルを保持する検査を追加しました。[観測方法・対応範囲・テスト](internal/lrtl/testdata/lrt-selected-date-7.5.5.md)を参照してください。

- 既定`init`の実際の保存で`xmp:MetadataDate`を実時計へ更新するようにしました。実機512条件・64書式を比較し、秒単位・`+0900`形式のオフセット・修飾子保持に対応します。`HasSettings=False`の撮影日時4項目を保持するよう修正し、プレビュー・再実行では時計を変えません。全体・race・fuzz約51.6万回と実行ファイルのAVI比較で検査しています。[書込条件・観測方法・残る差](internal/lrtl/testdata/lrt-metadata-date-7.5.5.md)を参照してください。

- 既定の`init`で、画像の撮影日時を実機のCSVキャッシュ方式で変換し、DateTimeOriginal/CreateDate/ModifyDate/Photoshop DateCreatedの4項目へ反映するようにしました。440キャッシュ条件、309画像の初期値有無、保存処理20条件を比較し、既存CreateDateの優先・サブ秒2桁切り捨て・タイムゾーン除去も検査しています。日時を保持する従来の初期化は`--frame-state preserve`で選べます。[動作変更・観測方法・残る差](internal/lrtl/testdata/lrt-capture-date-cache-7.5.5.md)を参照してください。

- 画像内XMPだけにある撮影日時を、日時順取り込み・字幕・動画の時刻表示・日時シフトで使えるよう修正しました。`DateTimeDigitized`を`CreateDate`へ正規化する実機の別名処理も合わせ、プリセット内の日時を採用・変更しません。JPEG/TIFF/DNGの実機XML45条件、未知のRDF・修飾子・バックアップ保持、日時順取り込み→SRT／AVI、全体・race・fuzzテストで検査しています。[動作変更・比較範囲・残る差](internal/lrtl/testdata/lrt-embedded-capture-time-7.5.5.md)を参照してください。

- TIFF/DNG内の複数IFDのXMPを実機に合わせて統合するようにしました。SubIFDとExifIFDを格納順に辿り、次ページ自身のXMPは採用しません。実機120条件のうち114条件が一致し、壊れた対象XMLの6条件はGo版の保存前エラーとの差を明示しています。実画素TIFFの初期化・AVI復号画素・全体・race・fuzzテストを追加しました。降順タグの画像はメタデータを読めますが、画像デコーダーは未対応です。[規則・テスト・残る差](internal/lrtl/testdata/lrt-xmp-tiff-multiple-7.5.5.md)を参照してください。

- JPEGの標準XMPと拡張XMPを統合し、標準側だけにある項目を保持するようにしました。最初のリンク・チャンク順序・GUID大小文字・再送を実機30条件で観測し、20条件は一致、10条件はGo版の整合性エラーとの差を明示しています。全体・race・fuzzテストと、実行ファイルでの初期化／AVI復号画素比較を追加しました。[読込規則・検証範囲・残る差](internal/lrtl/testdata/lrt-xmp-extended-7.5.5.md)を参照してください。

- JPEG内の複数の標準XMPパケットを順番に統合するようにしました。先のパケットだけにある項目を保持し、重複する項目は後の値で置き換えます。配列・構造体・空値・壊れたXMLを含む20条件の実機観測と、初期化／AVI生成・全体・race・fuzzテストで検査しています。壊れたXMLしかない場合はGo版の厳密なエラーを維持します。[観測方法・対応範囲・残る差](internal/lrtl/testdata/lrt-xmp-multiple-7.5.5.md)を参照してください。

- `init`のJPEG/DNGのXMP採用を実機方式へ変更しました。画像がサイドカーと同時刻または新しければ画像内XMP、新しいサイドカーならそちらを使います。TIFFはサイドカー優先です。実機ファイル読込76条件、未来日時・バックアップ・再実行・動画画素を検査しました。従来の初期化は`--frame-state preserve`で選べます。[動作変更・実測値・残る差](internal/lrtl/testdata/lrt-xmp-source-7.5.5.md)を参照してください。

- RW2／Leica RWL／Panasonic RAWの寸法読込を追加しました。センサー境界、ExifIFD、埋め込みJPEGの選択を実機84条件で比較しています。`.raw`（PanasonicのTIFF構造）と`.rwl`も`--raw-preview`で動画化でき、各3フレームのAVIが抽出JPEGの対照と一致しました。[規則・テスト・未対応範囲](internal/lrtl/testdata/lrt-rw2-dimensions-7.5.5.md)を参照してください。

- TIFF系RAWの寸法読込を実機へ近づけました。先頭IFD・本画像SubIFD・CR2のExif寸法・BigTIFFの選択を80種類の合成入力で比較し、70,000ピクセルを超える寸法も保存できます。円形マスク補間もこの読込と保存済みLRT寸法を使います。RW2／RAF／CR3／IIQ系は後続実装で対応し、独自maker noteの全取得等は残っています。[選択規則・テスト・未対応範囲](internal/lrtl/testdata/lrt-raw-dimensions-7.5.5.md)を参照してください。

- `init`で画像寸法・向き・カメラ設定の保存を追加しました。実機のCSV読込→保存を75条件で比較し、EXIF露出ラベル94例、PNGの向き、バックアップ・元画像保持・破損時の保存前拒否もテストしています。日時更新、RAW寸法の一部、自動撮影モード/HDRの画像判定などは未対応です。[観測方法・対応範囲・再実行手順](internal/lrtl/testdata/lrt-frame-metadata-save-7.5.5.md)を参照してください。

- `copy-settings`／`init`の既定保存を登録861列へ広げました。数値755列すべて、内部HG/Deflicker/Global露出、評価の保存、旧参照タグ整理を実機864条件の初回・再保存で比較しています。内部露出の整形は別途Javaの1,426パターンでも検査します。選択外の数値や評価も変わる場合があり、従来動作は`preserve`で選べます。[動作変更・検証範囲・未対応部分](internal/lrtl/testdata/lrt-registered-save-7.5.5.md)を参照してください。

- `init`にQuality分類とWB・クロップ保存準備を追加しました。最初の4つ星（なければ先頭）を参照して連番のQualityを揃え、DNGはAsShotNeutralタグを読みます。実機426条件と合成DNG24例を比較しました。`lrt:Quality=FLAT`の相対WBを動画へ近似反映する処理も追加しています。従来の初期化範囲は`--frame-state preserve`で選べます。[動作変更・実機観測・対応範囲](internal/lrtl/testdata/lrt-frame-state-7.5.5.md)を参照してください。

- `set`／`set-sequence`のWB編集を実機へ近づけました。有効な色温度・Tintの編集で`WhiteBalance=Custom`へ切り替え、対になる値を補い、非使用のWB数値を解除します。4項目の整数化・範囲制限とキャッシュ更新を実機960条件と比較しました。全体数値編集が入れ子の同名メタデータを書き換える問題も修正しています。[動作変更・観測方法・未対応範囲](internal/lrtl/testdata/lrt-wb-edit-7.5.5.md)を参照してください。

- `copy-settings`の既定保存へ、LRT 7.5.5のWB整理・WB/クロップ9数値の保存・`HasCrop`の再計算を追加しました。WBがなければ`As Shot`を補い、方式と`lrt:Quality`に応じて数値を整理します。丸め前のキャッシュを使うクロップ判定を含む397条件を実機と比較し、クロップ有効化／WB整理後のAVI画素も検査しました。従来のコピー段階だけを使う場合は`--save-state preserve`を指定します。[既定動作の変更・検証範囲・残る差](internal/lrtl/testdata/lrt-save-state-7.5.5.md)を参照してください。

- `copy-settings --properties NAMES`で列を指定して設定をコピーできるようにしました。LRT 7.5.5の登録866列を確認し、実機の対象範囲である861列を扱います。`--list-properties`で一覧を表示し、全体値・Look内の値・局所補正・マスク座標を指定できます。整数の丸めと倍精度数値の小数6桁保存を1,001件の実機部品観測で比較し、親配列の作成条件、保存・元ファイル保持・AVI画素も検査しています。WB/クロップ保存準備は既定プロファイルで対応し、保存全体の再現は未完です。[型変換・対象列・検証範囲](internal/lrtl/testdata/lrt-selected-columns-7.5.5.md)を参照してください。

- `copy-settings SOURCE_IMAGE_OR_XMP DIRECTORY`で、参照画像／XMPの設定を連番へコピーできるようにしました。対象のCamera Raw項目を置き換え、他の名前空間は既存値を優先して欠損項目・配列要素を追加します。欠けた撮影日時・評価もコピーされるため、現像設定だけを扱う操作ではありません。42種類の実機XML参照と再コピー結果、実機パーサへの読み戻し、選択・バックアップ・コピー元保護・AVI画素をテストしています。日時キャッシュ・HasSettingsの再読込に対応しましたが、フレーム再初期化全体は未対応です。列指定は別経路の`--properties`で対応します。[動作・互換性の範囲](internal/lrtl/testdata/lrt-metadata-paste-7.5.5.md)を参照してください。

- `long-term --lrt-filter-preset FILE`で、LRTの長期撮影フィルターの保存プリセットを使えるようにしました。曜日・時刻→輝度→コントラスト→色相の順に、Constantまたは時間方向の基準曲線との差で選択します。924件の実機曲線ヘルパーと36件の実機時刻ワーカーを比較し、選択JSON→AVI、元ファイル保持、途中失敗もテストしました。`--lrt-filter-preset-name`で名前を選び、`--lrt-filter-color auto|on|off`で色系フィルターを制御できます。[計算・日時欠損時の動作・検証範囲](internal/lrtl/testdata/lrt-longterm-filter-7.5.5.md)を参照してください。

- 長期撮影フィルターの`--metric-profile lrt-7.5.5`で、輝度・コントラストの定義と縮小時の画素差を修正しました。元画像の整数平滑化RGBヒストグラム、単一パスのbilinear縮小、透過の丸め、Gaussian係数、JavaのHSB計算を合わせ、112条件の測定値と縮小／ぼかし後の全3,670,016成分を比較しました。選択JSON→AVIと失敗時の既存出力保持も検査済みです。過去の同プロファイルで生成したJSONは再解析すると判定が変わる場合があります。[定義・測定範囲・互換性](internal/lrtl/testdata/lrt-longterm-metrics-7.5.5.md)を参照してください。

- XMP補間で、現行形式の円形マスク（MaskGroup 8・9）の形状遷移を追加しました。画像寸法が既知で全キーフレームの形状が揃う場合、中心・物理半径ベクトル・連続回転を補間して矩形へ戻します。本体120件のヘルパー観測と、通常／2・5つ星混在の360条件×3 RDF表現を比較し、CLI保存・バックアップ・再実行・バッチ並列処理・AVI画素への反映も検査しました。[対応条件・旧形式との差・残る差](internal/lrtl/testdata/lrt-radial-transition-7.5.5.md)を参照してください。

- ネイティブ経路のPNG／TIFFグレー入力を、RGBを経由せず処理するようにしました。Gray8のビット拡張、Gray16の等倍ディザと拡縮時の丸めを純Goで再現します。16-bit全65,536値×ディザ64位置を含む72保存参照と、回転・クロップ・時間フィルターを含む192動画で比較しました。[測定範囲と残る差](internal/lrtl/testdata/lrt-gray-input-8.0.1.md)を参照してください。

- ネイティブ経路のリサイズを、純Goの固定小数点LanczosとRGB8／RGB16／JPEGそれぞれの色変換へ対応させました。保存プリセットでは本体と同じ「偶数端クロップ→拡縮→YUV回転→最終クロップ」の順を使います。本体48グラフ、保存参照144条件、通常CPU／スカラー84比較と、フィルターを含む324動画で検査しました。[測定範囲・再実行方法・残る差](internal/lrtl/testdata/lrt-native-scale-8.0.1.md)を参照してください。

- 偶数寸法のネイティブ経路で、通常／プログレッシブJPEGを純Goで本体に近い復号・YUV入力へ対応させました。YCbCrの4:4:4／4:2:2／4:2:0とグレースケール、リスタートマーカー付き32素材を付属FFmpegと比較し、フィルター・透かし・字幕・フェードを含む180動画も比較しました。Go標準デコーダーのBSD表記を保持し、独立したIDCT丸め・色差拡大を実装しています。[対応条件と残る差](internal/lrtl/testdata/lrt-jpeg-input-8.0.1.md)を参照してください。リサイズ・回転は上記の追加範囲を検査し、完全な色管理は引き続き未達です。

- 偶数寸法のネイティブ経路で、RGB8のPNG／TIFF入力に残っていた初段の色変換差を修正しました。RGB16へ先に拡張せず、本体と同じ14→15-bitの丸めを純Goで再現します。全16,777,216色を付属FFmpegの通常CPU／スカラー経路と比較し、PNG／TIFF・時間処理・補間・透かし・字幕・フェードを含む72動画も比較しました。[測定範囲](internal/lrtl/testdata/lrt-rgb8-input-8.0.1.md)を参照してください。JPEGとリサイズ・回転は上記の追加範囲に対応し、未検証形式や完全な色管理の差は残っています。

- `--watermark-style lrt-7.5.5`で本体のArial透かしに対応しました。固定位置・幅別の文字サイズ・色・影・文字置換と、最終色変換→透かし→字幕→フェードの順を再現します。本体ヘルパー105件・合成グラフ3件と、SDR／広色域／PQの42動画で比較しました。保存設定の透かしも読み込めます。[測定範囲と残る差](internal/lrtl/testdata/lrt-watermark-7.5.5.md)を参照してください。

- `render --output-color lrt-7.5.5`は、最初の選択画像のICC名からBT.2020とPQを自動判定します。JPEG／PNG／TIFFを純Goで読み取り、明示的な入力色・ダイナミックレンジ指定を優先します。付属ExifToolとの比較と、破損メタデータで既存出力を保持するテストを追加しました。[判定条件と検査範囲](internal/lrtl/testdata/lrt-icc-input-7.5.5.md)を参照してください。

- 準備済みBT.2020画像から、BT.709 SDR／BT.2020 SDR／BT.2020 PQの動画を出力できるようになりました。`--output-color lrt-7.5.5 --input-color bt2020 --output-gamut bt2020 --dynamic-range sdr|pq`を使います。保存プリセットの色設定、10-bit選択、HDRメタデータに対応し、本体の162色設定・48形式選択・94実動画で比較。通常SDRのH.264／ProResに不足していた色タグも修正しました。JPEG／PNG／TIFF内のICC名から入力BT.2020とPQを自動判定します。ICC色変換とRAWのHDR現像は未対応です。[対応条件と制約](internal/lrtl/testdata/lrt-wide-output-color-7.5.5.md)を参照してください。

- `render --timestamp-style lrt-7.5.5 --timestamp-every N`で、本体のArial/libass撮影日時字幕を動画へ合成できるようになりました。既存FFmpegの最終色変換→字幕→フェードの順に合わせています。本体が返した合成コマンド3件・字幕66動画で比較し、既存フェードも変換後へ修正しました。偶数寸法の通常SDR MP4/MOV向けで、`--watermark-style lrt-7.5.5`による透かしとの併用にも対応しました。[検査範囲](internal/lrtl/testdata/lrt-timestamp-burn-7.5.5.md)を参照してください。

- `subtitles --profile lrt-7.5.5 --every N`で本体の撮影日時SRT用の時刻・表示間隔に対応しました。本体ヘルパーの1,344件と文字列単位で一致。fpsの別解釈、単精度、6桁切り捨て、元連番を基準にした区間を再現します。標準モードは従来の動画同期を維持します。動画への合成は上記のネイティブ字幕スタイルで対応しました。[測定記録](internal/lrtl/testdata/lrt-timestamp-clock-7.5.5.md)を参照してください。

- 通常SDRのフェードを本体の最終色変換後へ合わせました。初期実装の変換前のGoフェードと参照順序の誤りを、実測した本体の合成コマンドに基づき修正しています。8/10-bit・3種類のfps・42件の保存参照、時間処理との併用、低速補間2経路、30実動画で比較。シャープ処理が一時的に1023を超える値を保持する本体の挙動も修正しています。[測定条件と制約](internal/lrtl/testdata/lrt-native-fade-7.5.5.md)を参照してください。

- 通常SDRのGaussianぼかしを、AVX2/FMA対応CPUでは本体と同じ1回丸めの積和演算に合わせました。最強ノイズ除去＋光跡除去＋シャープ/CASで残っていた1成分・2段階の差が解消しました。192件の単体参照、従来スカラーと通常CPUの両データを検査しています。小画像を多数スレッドで処理した場合の本体側の差は再現対象に含めていません。

- 通常SDRの低速ブレンド／モーション補間もYUV経路へ対応しました。本体と同じfull-range輝度の10→8-bit変換を行い、補間後のBlur・シャープ／CAS・エンコーダー入力を8-bitで維持します。0.0625〜0.9倍速の22経路、移動画像、単独/併用フィルターと132実動画をスカラー参照と比較。字幕・透かし併用や奇数寸法は既存RGB経路です。

- 通常SDRのYUV経路をMotion Blur全7段階と高速補間へ広げました。時間平均のfloat32積算、整数の均等平均、前後トリムを反映し、22経路とH.264／H.265／ProResの132実動画をスカラー参照と比較しています。補間なしの低速複製もYUVのまま処理できます。低速ブレンド／モーション補間のYUV経路も対応しました（上記参照）。標準スタイルの字幕・透かし併用は既存RGB経路です。本体スタイルはYUV経路を維持します。

- 通常SDRで、動画デフリッカー→ノイズ除去→光跡除去→シャープ／CASまでYUVのまま処理する経路を追加しました。最強ノイズ除去のfull-range輝度の8/10-bit変換も修正。12経路のスカラー参照、H.264／H.265／ProResの72実動画で全画素を比較しています。標準スタイルの字幕・透かし併用時は既存RGB経路です。本体スタイルはYUV経路を維持します。AVX2/FMA対応CPUで残っていた最強併用の差は、上記のGaussian演算対応で解消しました。

- 通常SDRのシャープ／CASで、純Goの固定小数点RGB16→YUV10変換を使い、YUVのまま出力できるようにしました。24件の保存済み参照、16-bitの灰色・RGB全入力値と乱数のnative／scalar比較、シャープ・CAS・両方を適用した84実動画ケースで確認しています。偶数寸法かつ字幕・透かしなしの経路で有効です。時間フィルターとの併用条件と残る差は以下を参照してください。

- `render --output-color lrt-7.5.5`で、本体に合わせた最終SDR色変換を使えます。保存プリセットでは既定で有効になります。本体の色設定162通りから通常SDRの経路を確認し、RGB16→YUVの144ケースと、PNG／TIFF→H.264／H.265／ProResの実動画12ケースで復号後の全画素を比較しています。内部フィルターのRGB/YUV変換、ICC、HDR・広色域の完全一致は未対応です。

- `render --interpolation 2`で純Goの低速モーション補償を使えるようにしました。本体が選ぶ双方向のブロック推定・重なり合成・履歴予測を実装し、8-bit／10-bitの720ケース、96,085,440サンプル分のハッシュと一致。ブレンドとは異なる中間画像を生成します。プリセット・字幕・バッチ、時間フィルターの併用、JPEG AVI・実MP4/MOV、失敗時の保持もテストしています。HDRと色管理を含む本体動画全体の一致は未確認です。

- 保存プリセットのfps変換を本体の156入力と比較して反映しました。`24.5`→`25000/1001`、整数近傍の0.001境界、指数・16進表記などを確認し、補間の282ケースも照合。`subtitles --lrt-render-preset FILE`で動画と同じfps・速度・補間・前後トリムを読み込めます。映像と音声で時間計算に使うfpsが異なる本体の挙動も、実MP4の長さ・映像/音声フェードまでテストしています。

- `render --interpolation 1`で純Goのブレンド補間に対応しました。低速ではシーン変化を避けて中間画像を作り、高速では本体と同じ丸めた速度分の過去画像を均等合成します。保存プリセット・バッチ・字幕にも反映。8-bit／10-bitの2,304ケース、96,523,260サンプル分の出力ハッシュと枚数を比較しました。低速のモーション補償も後述のモード2で対応し、色管理の完全一致は未確認です。

- `render --output-deflicker-luma`／`--output-deflicker-chroma`と保存プリセットの動画デフリッカーに対応しました。純Goの10枚パワー平均を、短い連番・末尾を含む288ケース・686,556画素の参照と比較。プリセットでは出力クロップ前に測光し、ノイズ除去・光跡除去・速度変更より前に適用します。実動画、バッチ、無効化、異常時の保持もテストしています。色変換と本体全体の画素一致は未確認です。

- `render --remove-airplanes 1..3`と保存プリセットの光跡除去に対応しました。純Goで3枚／5枚の時間中央値と中・強段階の輝度補正を実装し、102件・323,253画素と比較。速度変更時に末尾の処理画像を保持する動作と前後の切り詰めも100ケースで確認し、字幕へ反映しました。実動画・音声の長さ、処理順、バッチ、失敗時の保持をテストしています。色管理と本体の動画全体の完全一致は未確認です。

- `render --denoise 1..4`と保存プリセットの動画ノイズ除去に対応しました。純Goで時間方向の平均化、CDF 9/7ウェーブレット、最強段階の点状ノイズ前処理を実装。354件・644,058画素の観測値と、最大10段の変換を含む約428万画素の全出力ハッシュを比較しました。速度変更・Motion Blurより前に処理し、プリセット解除、バッチ、実動画、異常時の保持もテストしています。RGB/YUV変換と一部の本体SIMD動作には差があります。

- `render --cas`と保存プリセットの`cas`に対応しました。純Goで本体のCAS 0.90に相当する10-bit輝度・色差フィルターを実装し、同梱FFmpegの24パターン・12,288画素と一致を確認しました。通常シャープ・Motion Blur・字幕・フェードの処理順、プリセット解除、AVIとバッチ、元ファイル保持をテストしています。既存RGB経路のRGB/YUV変換は独自近似です。

- `deflicker`と`batch-deflicker`で`--calculation lrt-7.5.5`を選べるようにしました。本体108ケースで検証した計算コアをヒストグラム測光・内部補正スロットの保存・複数パスへ接続しました。個別/一括の最終XMP一致、日時順ROI、RAW変換経路、既存補正の移行、バックアップ・解除・失敗時の保存中止をテストしています。複数パスの画素現像はGo側で行い、Camera Rawを使う本体全体との一致は未検証です。

- `preview-ev`に`--ignore-blacks`、`--ignore-whites`、`--hard-cut`を追加しました。本体の黒白除外測光を、ソフトな端の重み付けとハードカットの両方で再現します。LRT 7.5.5の96ケース、保存済み参照領域、既存RAWキャッシュ、XMP保存・バックアップ、無効引数をテストしています。

- Preview EVのヒストグラム計算を実LRT 7.5.5の59ケースと比較して修正しました。255の除外、重み付き平滑化の丸め・再正規化、RGB各成分の平均に対応し、従来の一様移動平均との差を解消しました。`preview-ev --lrt-reference`で保存済みの移動する参照領域も使え、日時順でも元フレームへ測光値を保存します。再計算時のPreview EVと`preview-images`の測光マニフェストの数値が従来版から変わります。

- `--lrt-reference`が日時順の連番にも対応しました。`filenameSort=false`では、本体の`xmp:CreateDate`文字列比較と同日時時のファイル名比較を使い、その順序で領域補間・測光・平滑化・複数パスを実行します。実LRTの並べ替え結果、順序が異なる切り出し連番との最終XMP一致、元フレームへの保存・バックアップをテストしています。

- `deflicker`と`batch-deflicker`に`--lrt-reference`を追加しました。`.lrt/lrtsequence.json`の参照領域を読み、LRT 7.5.5と同じ単精度の自然3次スプラインで移動・変形させながら測光します。本体64ケースとの座標一致、対象領域の独立した測光値、複数パス、切り出し画像との最終XMP一致、破損設定時の一括保存中止をテストしています。

- `--lrt-preview camera|visual`で、既存のLRTプレビューキャッシュを画像入力として使えます。`render`、`export`、`preview-images`、`preview-ev`、`preview`、`keyframes`、`long-term`、`deflicker`、`batch-deflicker`、`holy-grail`、`batch-holy-grail`に対応します。LRT 7.5.5のパス生成・JPEGエンコードを実行して形式を確認し、TIFF画素保持、AVIの無再圧縮、測光値の元XMP保存、個別/一括Deflickerの一致をテストしています。キャッシュの鮮度判定やAdobe現像の再生成は行いません。

- `--apply-xmp-basic`で`ConvertToGrayscale`と8色の`GrayMixer`を画像へ反映します。従来は保存・補間だけで、プレビュー・動画がカラーのままになる処理の抜けを修正しました。16-bit階調、色別調整、無彩色保持、露出/WBとの順序、後段のカラーグレーディング、出力TIFF/JPEG/AVIとエラー時の保持をテストしています。Adobe Monochromeプロファイルとの画素一致は未対応です。

- `batch-preview-images`で複数連番のJPEGプレビューを並列生成できます。`--write-lrt-visual-cache`は全連番の出力を検証し、各ソースへLRT visualキャッシュを追加します。個別生成との一致、選択フレームの保存、元ファイルの保持、キャッシュ競合、生成・保存途中の失敗をテストしています。

- `preview-images DIRECTORY --output NEW_DIRECTORY`でJPEG連番プレビューと測光結果を生成できます。既定は長辺1280px・品質90で、対応XMP基本現像・クロップ・向きを適用します。RAW現像、選択フィルター、Deflicker/Motion Blurにも対応します。表示するJPEGを復号してPreview EVと輝度を測り、`--write-preview-ev`で元XMPへ記録できます。既存TIFF/動画処理との画素一致、選択フレームだけの保存、失敗時の保持、実NEF/CR2をテストしています。Camera Raw/LRT Visual Previewとの画素一致や自動キャッシュ再利用は未対応です。
- `preview-images`／`batch-preview-images`の`--write-lrt-visual-cache`で生成JPEGを`.lrt/visual/<元ファイル名>.lrtpreview`へ追加できます。既存キャッシュがある場合は全件を書き込む前に失敗し、途中書き込み失敗時は追加済みファイルと新規ディレクトリをロールバックします。これはlrtlが現像したJPEGの保存で、LRT/Camera Rawの現像画素やキャッシュ鮮度判定を再現しません。既存キャッシュの置換は未対応です。パス対応、JPEG読み戻し、既存ファイル保護、途中失敗時のロールバックをテストしています。

- `import --convert-dng --dng-compression lossy`でロッシーDNGを取り込めます。`--dng-long-edge N`で長辺を制限でき、既存DNGにも明示的なロッシー再変換を適用します。元RAW/DNGとXMPは保持し、縮小結果の寸法も検査します。CLI引数、dry-run、XMP保持、途中失敗時のロールバックをテストし、実NEF/CR2で元解像度・256pxの変換、既存DNGの再縮小、TIFF→AVIまで確認しました。

- `import --split-intervals`で、LRT 7.5.5の「間隔で分割」と同じグループ分けを使えます。間隔変化の許容差と最小連番長を指定し、短い連番も`single_images`へ保持します。本体から取得した240ケース、境界・重複時刻・サブ秒・コピー/XMP・取り込み後AVI・衝突・途中失敗のロールバック・ファズテストで検証しています。動画のシーン分割を本体の不足対応項目としていた記述も、今回確認したJPEG展開の実態に合わせて訂正しました。

- `batch-holy-grail ROOT`でHoly Grail露出補正を一括解析・保存できます。再帰探索・並列解析・EXIF/キャッシュ済みLRT入力・Stretch/Rotate・自動形状・2*/3*マーカー・リセットに対応し、全連番の内容検証後に保存します。個別実行とのXMP/AVI一致、LRT 7.5.5実測値との補正一致、再実行での重複防止、評価の復元、解析エラー時の全体保存中止をテストしています。

- `holy-grail --lrt-holy-grail-settings`と`batch-holy-grail --lrt-holy-grail-settings`は、各連番の`.lrt/lrtsequence.json`から`filenameSort`、`holyGrailReduceDeflicker`、`holyGrailStretch`、`holyGrailRotate`を読みます。単独/バッチの保存結果を照合し、連番ごとに異なる設定、欠損・不正・範囲外設定、フラグ競合、バッチ全体の事前検証をテストしています。LRT本体が入力値を生成する画像処理は別途未対応です。

- `batch-deflicker ROOT`で複数連番のDeflickerを一括解析・保存できます。既定は変更予定の表示で、再帰探索・並列解析・複数パス・Gaussian/LOWESS・参照領域・RAW現像・XMP基本現像・クロップ・リセットに対応します。全連番の解析と最終XMPの内容検証後に保存します。個別実行とXMP/AVIが一致すること、RAW、専用補正スロット、画像を復号しないリセット、エラー時に保存しないことをテストしています。

- RAWプロファイルの`BaselineExposureOffset`を適用します。正負のSRATIONAL値をBaselineExposureへEVで加算し、Hue/Sat補正後・Look/トーンカーブ前の露出段階へ反映します。両バイト順の分数684ケース、露出の相殺で全65,536入力を保持すること、プロファイルとの適用順序、ルートIFDからの継承と主画像による上書き、合成RAW→XMP→TIFF→AVI、異常入力とファズテストで検証しています。

- RAWプロファイルの`ProfileHueSatMapData1/2/3`と`ProfileLookTableData`を適用します。線形ProPhoto RGB上でHSVを補正し、色相の循環・3次元補間・2次元テーブル・彩度ゼロ行の省略・Vのlinear/sRGB符号化に対応します。2光源は既存の温度推定、3光源はDNG SDKの温度/Tint距離・平滑化・小ウェイト抑制から各テーブルを混合します。独立した格子計算、固定参照ウェイト、ForwardMatrixのウェイト、両バイト順、選択プロファイルIFD、カスタムxy照明、不正入力、合成RAW画素、ファズテストで検証しています。カスタムスペクトル照明・HDR・Camera Rawとの画素一致は未対応です。

- RAWの埋め込みDNGプロファイルにある`ProfileToneCurve`を画素へ反映します。線形ProPhoto RGB上で自然3次スプラインと色相を保つRGB補間を使い、最後にsRGBへ変換します。解析解・不等間隔の制御点・RGBの全並び順・恒等カーブの全65,536入力・異常入力・合成RAW→XMP露出→TIFF→AVIをテストしています。外部プロファイルの選択、Camera Rawとの画素一致は未対応です。

- RAWの`OpcodeList2/3`にあるWarpRectilinearへ対応しました。径方向・接線方向の歪み、色面別の係数、DefaultScaleの画素比を反映し、色変換・クロップ前にバイキュービック補間します。座標の独立計算、複数回のワープ、恒等変換での全65,536入力の保持、合成RAW→XMP露出→TIFF→AVI、異常入力による出力の中止をテストしています。Adobeの補間テーブルとのビット単位の一致は未対応です。

- RAWの`OpcodeList2/3`に、16-bit LUT、多項式、行/列ごとの加算・倍率補正を追加しました。既存GainMapと合わせて7種類を記録順に適用します。LUTの全65,536入力の恒等性、0〜8次の多項式、混在する補正順とクリップ、領域・色面・間引き、RAW→XMP露出→TIFF→AVIをテストしています。

- RAW現像でDNGの`OpcodeList2/3`にあるGainMapを適用します。黒レベル補正後の線形値へ、領域・色面・行列の間引き・画素中心の双線形補間・端の延長を反映します。クロップ前のActiveAreaを基準にし、各補正後にクリップします。合成RAW→XMP露出→TIFF→AVIと異常入力をテストしています。未対応の必須Opcodeはエラー、オプション指定された未対応Opcodeはスキップします。

- RAW現像のLinear DNG黒レベル処理を修正しました。ActiveAreaを基準とする繰り返しパターンと行・列の補正値に対応し、各色面の最大黒レベルから正規化します。LUT範囲外の末尾への丸めと、色変換前のセンサー飽和クリップも反映しました。200パターンの独立計算と、合成RAW→XMP露出→TIFF→AVIで検証しています。

- `export DIRECTORY --output NEW_DIRECTORY`で、動画と同じ画像処理から非圧縮RGB 16-bit TIFF連番を書き出せます。RAW現像、XMP基本現像・クロップ、解像度、Deflicker、Motion Blur、文字オーバーレイに対応します。全画像の成功後に新規フォルダーを公開し、元画像との対応を`frames.json`へ記録します。16-bit下位ビット、動画側RGB48との一致、失敗時の後片付けをテストしています。

- `batch-interpolate ROOT`で複数の連番のXMPをCLIから一括補間できます。既定は変更予定の表示だけで、`--recursive`・並列解析・smoothness設定に対応します。`--write`では全連番の内容検証後にバックアップ付きで保存します。個別補間との一致、エラー時の書き込み防止と、一括動画生成までテストしています。

- `holy-grail --lrt-input`で、ExposureTime/FNumberがない画像のAPEX形式（ShutterSpeedValue/ApertureValue）にも対応しました。同梱ExifTool→実LRTパーサーの112ケースで、符号・通常タグとの優先順位・ゼロ値を比較しています。APEXだけの撮影情報からXMP保存・AVI生成まで、実LRTコントローラーの12ケースと比較しました。

- `holy-grail --lrt-input`でEXIFのシャッター・絞りをLRTの入力文字列に整形してから計算します。同梱ExifTool→実LRTパーサーの94ケースと比較し、分数精度も保持します。シャッター・絞り・ISOが変化する実LRTコントローラーの12ケースでは、合成EXIF→CLI保存→17フレームAVI画素まで検証しています。

- `holy-grail --lrt-input`でXMPの`lrt:ShutterSpeed`・`lrt:Iso`・`lrt:Aperture`を優先して読みます。情報が揃えばEXIFなしのPNG列でも補正でき、不足項目はEXIFで補います。実LRTの文字列解析とフレーム間計算を比較し、PNG→CLI保存→AVI生成、異常値での書き込み防止もテストしています。

- Holy Grailの中心合わせ・クリップを実LRTの84ケースと比較し、極端な一定値の列でも実LRTの境界処理に揃えました。`--lrt-input --auto-shape`は探索中のクリップを無効にし、350候補の値をfloat32で生成します。実LRTの数値コントローラーを呼ぶ独立グリッド探索と6ケースで候補・評価値を比較し、CLIのAuto Shape保存と17フレームAVIもテストしています。

- `holy-grail --lrt-input`の数値コントローラーを実LRTの66ケースと比較しました。ISO文字列の前処理を140入力で調べて反映し、Stretch/Rotate/Reduce Deflickerも実LRTと同じfloat32経路に揃えました。実LRTが返した補正値を、合成EXIF→CLI保存→AVI画素まで追跡するテストを追加しています。全カメラメタデータとCamera Raw画素との一致は未検証です。クリップと探索経路は後述の比較範囲を参照してください。

- `holy-grail --lrt-input`の形状調整を実LRTの112ケースで比較し、2/3つ星区間の補正と正規化範囲、float32のRotateトレンドを修正しました。カメラ設定差の27ケースも直接比較し、シャッター比のlog計算を修正しました。同じカメラ設定が続く区間に不要な補正を加えません。Holy Grail全体とCamera Raw現像の完全一致は未検証です。

- `import --split-gap 30s`で、撮影時刻の間隔に応じて写真を複数の連番フォルダーへ分けて取り込めます。時刻順の整列、対応XMPの保持、dry-run、衝突の事前検証と失敗時のロールバックに対応します。LRTの分割ワークフローに対応するCLI機能ですが、指定した時間差を使う独立した判定です。

- `curve IMAGE_OR_XMP|DIRECTORY`でマスター／RGBのポイントカーブをCLIから確認・編集できます。`--mask 'MaskGroupBasedCorrections[1]'`で既存の局所補正にも対応します。JSON表示、4つ星マーク、直線へ戻す操作、dry-run、バックアップと連番全体の事前検証を備え、全体／局所カーブの編集→均等化→AVI画素までテストしています。

- 登録位置の局所補正・マスク形状にも数値の欠損補完を追加しました。既存の補正／セレクター内の欠けた露出・座標などを補完してから4つ星で補間し、欠けたキーフレーム項目にも保存します。実LRTの局所露出列9ケースと比較し、ID保持と登録真偽値の遷移、角度処理、バックアップ、動画画素までテストしています。補正要素が丸ごとない4つ星にも登録数値とCorrectionActiveを補完し、参照元のIDや未知の値は複製しません。本体の欠損列補完・XMP書き戻し16ケースと比較済みです。選択キーフレームの欠けた先頭セレクターも、Linear／Circular／MaskGroupの登録座標とFlippedを本体40ケースと比較して補完します。親補正が既存または同時に補完できる場合が対象です。補正直下の欠けたCorrectionRangeMaskもType/ColorAmount/LumMin/LumMax/LumFeather/LumRangeを本体144ケースと比較して補完します。色域サンプルそのものの復元、セレクター直下の欠けた範囲要素、Sync ID独自形式の事前補完は未対応です。

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
- **到達していない範囲:** Camera Raw/Lightroomと同等のRAW現像、RAWのHDR現像・ICC色管理、LRT Visual Previewと同等のプレビュー、未対応のマスク形式や新規Upright解析は未実装または近似です。保存済みUpright変換は再生できます。詳しい境界は下表と各コマンドの説明を参照してください。

下表の「一部」はLRTLの基本処理が未完成という意味ではなく、LRTの対応機能全体との比較です。たとえば画像連番からの基本動画生成は達成済みですが、LRTの全コーデック・色深度・HDRとの同等性はありません。

| 機能領域 | LRTimelapseの機能 | lrtlの状況 | 状態 |
|---|---|---|---|
| 初期化時のXMP採用 | 画像内XMP／サイドカーから初期値を読込 | 本体ファイル読込420条件を追加比較し、既定初期化はJPEG／DNG／TIFFの画像内XMPと、その他RAWのサイドカーを採用。通常読込／`preserve`の対応形式は従来どおり。不正な選択サイドカーは保存前エラー | 一部 |
| RAWの寸法メタデータ | RAWの本画像寸法を読み、保存・補間に利用 | TIFF系／CR2／BigTIFF、RW2／RWL／Panasonic RAW、RAF、CR3、IIQの観測済み寸法選択に対応。RAFの66条件、CR3の76条件、IIQ／TIFFの52条件を含めて比較。`init`保存と円形マスク補間へ反映。独自maker noteの全上書きは未対応 | 一部 |
| レンダープリセット | 保存した動画出力設定の再利用 | `--lrt-render-preset`で単独プリセットJSON、またはLRT設定JSONのlastRenderSettings／名前で選ぶrenderPresetsを読み込む。fps、解像度、H.264/H.265画質、ProRes/DNxHRプロファイル、Motion Blur、音声、速度、本体の推定枚数に基づくフェード・音声時刻、映像レンジ、保存済みアスペクト比に対応。動画デフリッカー・ノイズ除去・光跡除去・ブレンド／モーション補間も対応。`gamut`／`dynamicRange`でBT.2020 SDR/PQ設定も読める。BT.2020中間画像が必要。ICC名から自動判定し、`--input-color bt2020`で手動指定も可能 | 一部 |
| 選択範囲の手動キーフレーム | 選択画像への4/5つ星マーク設定・解除 | `mark-sequence`／`unmark-sequence`で一括設定・解除。ファイル名・評価・JSON・反転で対象を選択し、元評価の復元、HG保護、保存前検証、バックアップに対応。GUI操作との比較は未実施 | CLI実装済み |
| シーケンス規模 | 評価版は400枚まで、Private/Proはサイズ制限なし | 画像列の枚数上限なし。AVIは約4 GBまで。MP4はFFmpeg対応 | 一部 |
| ワークフロー | Basic、Visual、Long-Term、Lightroom/ACR連携、内部編集/Export | CLIで個別機能を実行。ワークフロー状態や内蔵現像画面なし | 一部 |
| キーフレーム | 輝度推移からキーフレーム数の目安を提示し、スライダーで調整するWizard。4/5つ星を選択でき、手動設定と等間隔配置も可能 | 手動マーク、JPEG/PNG/TIFF、`--raw-preview`内蔵プレビュー、`--lrt-preview camera|visual`既存キャッシュ、または`--raw-develop`現像RAWを使う`keyframes`を実装。既定数量は、`set IMAGE PreviewEV VALUE`またはGUIで編集できるLRT値`lrt:ExternalExposureDefault / 4`が全フレームで有効なら使い、値が揃わない場合はLRT互換Preview EVヒストグラムで画像を測り、LRT 7.5.5から復元した数量式へ渡す。`--preview-width-camera N`でカメラプレビュー幅を指定できる（既定1024）。`--lrt-preview`を明示するとXMPの既存値より指定キャッシュのPreview EVを優先。候補位置も同版の整数間隔・端点選択ルールに対応。GUIでは候補一覧から保存対象を選べる。`--rating 4|5`でLRT Wizardと同じ評価を選び、既定は4つ星。既存マーカーは既定で保持し、選択中の評価だけ数量推定から差し引く。`--replace-existing`では同じ評価を連番から削除し、他の2〜5つ星との衝突を前方9枚以内の空きフレームへ移す。`--evenly-spaced`は測光なしで利用可能。`--luminance-deviation`はlrtl独自配置法。`--apply-xmp-basic`（または`--apply-xmp-exposure`）と`--apply-xmp-crop`を解析に反映できる。`preview-ev --write`で測光値をXMPへ生成できる。RAW/現像画像入力の一致、数量スライダーUIは未対応 | 一部 |
| Holy Grail | Mモードで変化させた露出やISOを補正して昼夜遷移をつなぐ | JPEGおよびTIFFベース画像/RAW（依存ライブラリの実RAWサンプルでNEF/ARWも確認）、CR2/CR3/DNG/RW2のEXIF露出設定から独自の補正を計算。`--lrt-input`で参照する画像内XMPはJPEG/TIFF/DNG/CR2/RW2に加えてNEF/NRW/ARW/SR2/PEF/ERF/ORF/DCRからもタグ700を読み、CR3はAdobe XMP UUID boxから読む。FNumberがない場合は`lrt:Aperture`を参照。`--lrt-input`では既存の`lrt:ExternalExposureDefault`を1/4して読み、LRT同様に最大10件のフレーム差平均と2*/3*/4*隣接ペア補正を適用する近似経路も選択可能。`--lrt-preview camera|visual`では既存LRTキャッシュからPreview EVを直接測定し、`--lrt-reference`で保存領域を使える。個別/一括実行、`--lrt-holy-grail-settings`との併用にも対応。`--lrt-reduce-deflicker`で0〜1の平滑化設定を指定できる。両経路で`--stretch`/`--rotate`とL1評価の`--auto-shape`に対応。LRT内部の画像入力生成は未再現。LOWESS単体は実LRTの53ケースで比較済み。HG専用LocalExposureスロットを優先し、識別できない場合は`crs:Exposure2012`へ書き込む | 一部 |
| 構図・クロップ | Composition作成、キーフレーム間のクロップアニメーション | `crop`で単一画像/XMPまたは連番全体の構図をCLIから確認・編集。矩形と角度、HasCropの有効化、解除、単一画像の5つ星crop markをバックアップ付きで保存できる。`interpolate`は数値をキーフレーム間で補間し、`render --apply-xmp-crop`で回転矩形を適用して入力と同じ解像度へ再サンプル。GUIのドラッグ編集も既存機能として残る。手動PerspectiveRotate/Scale/Vertical/Horizontal/Aspectと、XMPに保存されたPerspectiveX/Y Offsetを基本現像で適用。後者はXMP編集とレンダーに対応しますが、LRT 7.5.5の登録遷移列にはなく、`interpolate`では遷移しません。`PerspectiveUpright`が選ぶ保存済み`UprightTransform_*`行列も再生する。Upright自動解析、行列のないGuided操作、Constrain Crop、回転後の自動最大矩形計算は未対応 | 一部 |
| レンズ情報 | マニュアルレンズのLens Tagger | `lens-tag --aperture f/8`とGUIで連番全体へLRT名前空間の`lrt:Aperture`を設定。EXIFのFNumberが欠けている場合、`holy-grail`がこのXMP値を絞りとして使う。標準EXIFや画像内XMPは変更せず、外部XMPサイドカーのみ | 一部 |
| 連番管理・選択 | ファイル名/キーフレーム/Visual Previewによる選択、フィルター、反転、連番内の移動・コピー・除去・削除、フォルダー作成とリネーム | `scan`と`gui`で自然順に連番を一覧表示。GUIはページ送りできる画像サムネイルを表示し、4/5つ星キーフレームの設定・解除と、輝度分析で候補一覧を表示し、選んだ候補だけ保存できる。`render --filename-contains TEXT`で名前の絞り込みに対応。`select`でファイル名・XMP評価・既存選択JSONの積集合と反転選択を作成し、動画/TIFF/JPEG処理へ渡すJSONを保存できる。`copy-sequence`で選択画像と採用中XMPをファイル名・内容・更新日時を保って新規フォルダーへコピーできる。`--include-lrt-companions`で同名JPEGと画像別のLRTプレビュー／プロキシも追加できる。`rename-sequence`でフォルダー全体を同じ親の中で改名できる。`move-sequence`で画像・両候補XMP・同名JPEG・画像別キャッシュを新規フォルダーへ移動でき、共有XMP／キャッシュは元にも残す。`move-sequence --merge`で既存先に追記でき、`remove-sequence`で隣の`_REMOVED`へ除外できる。同名画像の上書き、完全削除、任意フォルダー作成、個々の画像名変更は未対応 | 一部 |
| メタデータ管理 | EXIF再読込、メタデータの初期化/消去、連番全体のコピー/貼付、撮影日時シフト、レンズタグ付け | EXIF読込、JPEG画像内XMP（標準/拡張APP1形式）、TIFF/DNG/CR2/RW2/NEF/NRW/ARW/SR2/PEF/ERF/ORF/DCR画像内XMP（tag 700）、RAF内埋め込みJPEGのXMP、およびCR3画像内XMP（Adobe UUID box）の読込、XMP数値値の個別編集、`init`による不足マスク/内部補正スロットの初期化とQuality・WB・クロップ保存準備、対応画像の寸法・カメラ設定と撮影日時4項目の更新、`timeshift`によるXMP撮影日時の連番一括シフト、`lens-tag`による連番一括絞り設定に対応。通常の編集は外部サイドカー優先、`init`のJPEG/DNGは更新時刻で採用元を決める。編集結果はXMPサイドカーへ保存し画像本体は変更しない。`copy-settings`で参照画像/XMPの全Camera Raw項目の置換と、他の名前空間の欠損補完に対応（実機XML処理42例と比較）。選択・dry-run・バックアップ・コピー元保護を備える。`--properties`で登録861列の型変換付き指定コピーと欠損キャッシュ処理にも対応。既定で登録861列（数値755列）・WB/クロップ状態・内部補正露出・評価の保存準備に対応（実機864条件で初回/再保存を比較）。その他のRAWコンテナー内XMP、メタデータ全消去/リセット、フレーム再初期化全体、他の保存操作の日時副作用・履歴等は未対応 | 一部 |
| XMP・現像値 | Lightroom/Camera Raw等とのXMP連携、現像パラメーターの遷移 | 外部XMPを読み書きし未知のRDF値を保持。JPEGは外部サイドカーがなければ画像内の標準/拡張XMP、TIFF/DNG/CR2/RW2/NEF/NRW/ARW/SR2/PEF/ERF/ORF/DCRはtag 700、RAFは埋め込みJPEG、CR3はISO BMFFのAdobe XMP UUIDから読み取る。編集結果はサイドカーへ保存し画像を変更しない。`set`で許可した131項目とPreview EVを単一画像へ、`set-sequence`で選択した連番フレームへ一括設定し、WBの4数値は実機の整数範囲と有効な組のCustom切替・対の補完・非使用値の解除に対応（実機コールバック先頭960条件と比較）。全体数値の編集は根のRDFだけを対象にする。局所補正/マスクは`masks`で確認した配列パスを`set`へ渡して編集できます。`preview-ev`でRGBヒストグラムからPreview EVを算出してXMPへ書き込める。CLIの`curve`で全体／局所のマスター/R/G/Bポイントを単一画像や連番へ編集し、属性/単純テキスト/RDF Seq表現をバックアップ付きで保存。全体設定とLRTの位置登録スロットにあるローカル補正・基本マスク形状をキーフレーム間で補間。sRGB入力のレンダーでは`--apply-xmp-basic`で全体8項目、Texture/Clarity/Dehaze/Sharpening/NoiseReduction/Vignette、4領域パラメトリックカーブ、HSL 8色域の色相/彩度/輝度、Temperature/Tint、全体ColorGrade 4領域の色調、ConvertToGrayscaleと8色GrayMixer、明示されたマスター/RGBポイントカーブに加え、線形/円形グラデーションと現行形式の輝度範囲コンポーネントのAdd/Intersect合成（`MaskGroupBasedCorrections`/`Mask/Aggregate`、MaskBlendMode=0/1、MaskInverted/Flipped対応）に対するLocalExposure/Contrast/Highlights/Shadows/Whites/Blacks/Saturation/LocalTexture/LocalClarity2012/LocalDehaze/LocalSharpness/Temperature/Tint/LocalToningHue/LocalToningSaturation/LocalColorGradeとローカルMainCurve/RedCurve/GreenCurve/BlueCurveを独自近似で画素反映し、クロップも任意適用。ローカルカーブはマスク濃度で16-bit PCHIPカーブ出力を混ぜる近似で、LocalCurveRefineSaturation、ExtendedMainCurveなどのHDR曲線、キーフレーム間の曲線補間は未対応。色範囲Type=1はColorAmountを数値スプラインで遷移し、PointModelsはフレームごとに保持する。レンダーではSampleType=0のPointModelsをCIELAB/ΔE76の幅推定で近似選択（Adobeと画素一致未確認）。深度範囲、ブラシ描線/AI、他の合成モード、Camera Raw相当RAW現像も未対応 | 一部 |
| Auto-Transition | キーフレーム間を自動補間。Look/Preset/Tone Curve/PointColorsのXMP均等化も可能 | LRT 7.5.5の実行時レジストリに登録された127個の全体数値項目と、固定配列スロットのローカル補正・マスク数値項目を3次ベジェスプラインで遷移する。Linear 1–4、Circular 1–2、Brush 1–2、MaskGroup 1–4/8–11と各CorrectionMasks[1]を位置で対応させ、Sync IDは必須にしない。`--smoothness 0..100`はLRTの設定を再現し既定値は50。Look/Preset/PointColorsとポイントカーブ、MaskGroup 1–4/8–11のMainCurve/RedCurve/GreenCurve/BlueCurve構造を最初の4つ星（なければ先頭）から均等化する。ローカルカーブは構造をコピーするだけでキーフレーム間の曲線補間はしない。`LumRange`文字列は最初の非空値を同じ配列位置の全フレーム（キーフレームを含む）へコピーし、色範囲Type=1はColorAmountを補間する。`PointModels`はLRTの遷移登録に見つからないため変更しない。円形マスク角度の後処理は本体24ケースで境界・欠損・対象位置を観測し、純粋な旧形式／現行形式の保存値を3種類のRDF表現で比較済み。両形式が混在する場合も、先頭XMPのMaskGroup／Gradientによる形式判定と`--mask-format auto|legacy|modern`で後処理対象を切り替える。混在列の保存結果を本体の全6列・3種類のRDF表現で432件比較済み。円形グラデーションとMaskGroup 8/9のAngleはLRTと同じ後処理（−45°未満なら+90°、45°超なら−90°）を、補間値と既存キーフレーム値に適用する。Depthの画像依存データ、ブラシ描線、登録スロット外のマスクは未対応。非キーフレームに欠けた登録位置のマスク構造は、最寄りの実体を持つXMPからRDF配列位置で複製して作成する。登録位置外やSync ID形式の欠損構造は未対応。RDF配列として格納されない独自XMPではSync IDを使って対応する。登録値ごとの範囲クランプとInteger型四捨五入、近接アンカーの直線化・等値区間の平坦化を適用する。許可リストにある旧式4項目（ChromaticAberrationB/R、Defringe、Exposure）はLRTの遷移登録にないため補間しない。先頭フレームのcrs:IsMergedHDRがtrueならExposure2012を-10..10、通常は-5..5に制限する。現行MaskGroup 8/9・selector 1は、画像寸法と全アンカー形状が揃う場合、物理座標への変換・補間・形状復元に対応。本体120ヘルパー観測と、通常／2・5つ星混在の360条件×3 RDF表現で比較。旧形式の同ヘルパーではパス未登録による入力保持を観測。欠損形状・不明寸法の専用変換とCamera Rawマスク画素の一致は未確認 | 一部 |
| スナップショット | シーケンス状態のスナップショット作成、読み込みと管理 | `.lrtl/snapshots`にXMPサイドカーのZIPスナップショットを作成/一覧表示し、復元案をプレビュー可能。元の連番パスを確認し、別パスには`--allow-moved`で復元。`--write`で適用し、既存サイドカーは既定でバックアップ。画像本体は含めず、後から増えたサイドカーは削除しない | 一部 |
| 視覚プレビュー | 露出や遷移を画像・グラフで確認 | `preview`で輝度曲線、XMP Exposure2012、キーフレームを静的SVGに表示。既存LRTキャッシュを使う場合は`--lrt-preview camera|visual`で指定。`preview-images`と`batch-preview-images`で対応RAW/XMPからJPEG連番と測光マニフェストを生成し、任意で元XMPへPreview EVを保存。両コマンドの`--write-lrt-visual-cache`でlrtlが生成したJPEGを新規LRT visualキャッシュ位置へ追加。`--lrt-preview camera|visual`でLRTの既存JPEGキャッシュを明示的に読み、測光・SVGプレビュー・長期測定・動画・TIFF・個別/一括Deflickerの入力として使える。`gui`は全フレームのXMP Exposure2012とキーフレームをグラフ表示し、クリックでフレームを選択できる。「画像輝度を解析」で最大64×64格子から得た線形sRGB輝度の中央値を先頭フレームとの差分EVで重ねる。RAW内蔵プレビュー/Adobe DNG Converterによる現像、XMP基本値・クロップ適用を選択可能。ラスター画像と対応RAWの内蔵プレビューを一覧表示し、選択フレームにXMP基本現像値・ローカル補正・クロップを適用した静止画も表示できる。「連番を再生」で1〜30 fpsを指定したループ再生にも対応し、XMP基本値/クロップとRAW内蔵プレビュー/Adobe DNG Converterによる現像を反映できる。再生画像は最大640 pxへ縮小し、読み込み時間によって指定fpsを下回る場合がある。GUIでは表示中の青いExposure2012点を上下ドラッグして、該当1フレームの値をサイドカーのバックアップ付きで保存する。通常点は最大4000個に間引き、キーフレーム点を追加表示する。曲線全体/隣接フレームへの補間編集、LRT Visual Previewとの速度・画素一致、Adobe現像値との一致は未対応 | 一部 |
| Deflicker | 参照領域と平滑化曲線に基づく視覚補正、複数パス、Lightroomへ渡す調整値 | `render --deflicker`で画素に補正し、`deflicker --write`でJPEG/PNG/TIFFまたは`--raw-preview`／`--raw-develop`の測光結果をDeflicker専用LocalExposureスロット（なければ`crs:Exposure2012`）へ記録可能。`--smoother gaussian|lowess`で平滑化を選択。`--lrt-reference`で保存済み参照領域を読込・補間して測光し、`batch-deflicker`でも利用可能。`deflicker`解析は`--apply-xmp-basic`（グラデーションと現行形式の輝度範囲マスクによるローカル露出・トーン・色・WBを含む）または露出のみの`--apply-xmp-exposure`、`--apply-xmp-crop`を測定前に適用できる。`--calculation lrt-7.5.5`は本体108ケースと比較したPreview EV・LOWESS・補正計算を使い、`--preview-width-camera N`でLRTカメラプレビューの幅と参照領域の縮尺を指定できる（既定1024）。`--lrt-deflicker-settings`で連番ごとの平滑化・精度・Constant・複数パス・並び順を再利用し、`--lrt-preferences FILE`でLRT全体の黒白除外測光設定も読み込める。XMPへの保存・リセットとAVI画素への反映も回帰テスト済み。複数パスの画像再生成はGoの独自近似。LRTのVisual Preview、Camera Rawとの数値一致、カーブUIは未対応 | 一部 |
| 長期撮影補正 | Luminance・Contrast・Hueで長期連番の画像をフィルターし、明るさ・コントラスト・色の大きく異なる画像を除外 | `long-term`で画像を分析し、各指標の範囲から選択JSONを作成。`--lrt-preview camera|visual`で既存キャッシュを入力にでき、RAWをデコードせずLRT 7.5.5指標も測定できる。`render --selection`で選択フレームのみ動画化でき、`--apply-xmp-basic`（グラデーションと現行形式の輝度範囲ローカル露出/彩度マスクを含む）/`--apply-xmp-exposure`/`--apply-xmp-crop`で測定前にXMP値を反映できる。既定指標は独自定義。`--metric-profile lrt-7.5.5`は元画像の整数平滑化RGBヒストグラム平均÷256、64×64単一パスbilinear縮小とGaussian scale 4の後のグレー標準偏差×2・平均HSB色相を使う。112条件の測定値と全3,670,016成分を本体参照と比較済み。`--lrt-filter-preset`で保存プリセットのConstant／時間方向の基準曲線、曜日・時刻、上下しきい値に対応。後段ほど先行フィルターを通過した画像のみで曲線を作る。本体924曲線ヘルパー・36時刻ワーカーと比較。構図UI、Camera Raw現像プレビュー、メトリック3軸のGUIワーカー全体との最終選択一致は未確認 | 一部 |
| 動画生成 | 現像済みフレームを動画にレンダー | `--sort`で従来の自然順・撮影時刻順・本体方式の自然順／文字順／XMP日時順を選べ、TIFF/JPEGプレビューと字幕にも同じ順序を指定できる。JPEG/PNG/TIFFとRAW現像画像をMJPEG AVI、外部FFmpegによるH.264 MP4／H.265 MP4・MOVまたはProRes 422/422 HQ MOVに出力。EXIF向き、XMPの全体8項目/HSL色別補正/簡易Temperature・Tint/ポイントカーブ/グラデーションと現行形式の輝度範囲マスクによるローカル露出・トーン・色・WB/クロップ、任意アスペクト比への中央クロップ、Deflicker、撮影日時焼き込みに対応。1–240 fpsの整数・小数・分数（23.976/29.97/47.952/59.94はNTSCの/1001指定として扱う）に対応し、`--output-height`で出力高を指定し、`--long-edge`で長辺上限まで縮小。全フレームはEXIF向き適用後の解像度が同じ必要 | 一部 |
| RAW・DNG | RAW処理、DNG変換 | `--raw-develop`でインストール済みAdobe DNG Converterを自動検出し、未圧縮Linear DNGのセンサー実画素をGoで16-bit sRGBへ現像。対応Opcode、埋め込みHue/Sat・Lookテーブル・ProfileToneCurve・RGBTables・BaselineExposureOffset、DNG 1.6/1.7の`ProfileGainTableMap`/`ProfileGainTableMap2`を適用。RGBTablesはSemantic Maskと背景の変換、Weighted Sum/Sequential、8/16-bit整数・32-bit float LUTに対応。1〜3光源のColor/CameraCalibration/ForwardMatrixと三光源Hue/Satテーブルにも対応。レンダー、キーフレーム解析、Preview EV、Deflicker、長期撮影解析、GUIプレビュー/動画生成で利用可能。`--dng-converter PATH`で場所を明示でき、`--dnglab PATH`で従来バックエンドも選択可能。`import --convert-dng`はAdobeによるlossless/uncompressed/lossy DNG取り込みとロッシーDNGの長辺縮小に対応。内蔵JPEGを使う`--raw-preview`も維持。Camera Rawの全プロファイル/Opcode/現像値、埋め込みCamera Profile IFDの選択は一部対応。Lossless JPEGマスク、Semantic Maskと幾何Warpの併用、カスタムスペクトル、4面以上の色データ、HDR/オーバーレイの完全再現は未対応 | 一部 |
| 解像度・中間画像 | 720p〜8K/Originalなどライセンス別解像度、Pro向け非圧縮TIFF中間画像 | `--aspect W:H`で1:16〜16:1の任意比率へ中央クロップし、`--output-height H`で出力高を指定。`--long-edge N`は長辺上限まで縮小。`export`で既存レンダーと同じ画像処理を使う非圧縮RGB 16-bit TIFF連番を新規フォルダーへ保存。RAW現像、対応XMP値の適用、Deflicker、Motion Blurにも対応。LRTのプリセット/UI、Camera Rawとの画素一致は未対応 | 一部 |
| コーデック・色・HDR | H.264/MP4、Pro向けH.265/ProRes/DNxHR、HDRワークフロー | 純GoのMJPEG AVIとFFmpegのlibx264/libx265/prores_ks/dnxhd経由のH.264 MP4／H.265 MP4・MOV、ProRes 422/422 HQとDNxHR LB/SQ/HQ/HQX/444 MOV。MP4/MOVは`--audio FILE`でAAC音声を追加可能。H.264/H.265は通常SDRの`--subsampling 420`で8-bit、`422`／`444`で10-bit出力。ネイティブBT.2020/PQは4:2:0も10-bit。準備済みBT.2020画像からのBT.2020 SDR/PQ出力に対応。FFmpegへRGB16またはネイティブYUV平面で渡し、ProRes/DNxHR HQX/444は10-bit出力へ変換。16-bit入力の向き・クロップ・縮小・露出・Motion Blur・字幕を通して対応済みXMP基本現像の全体/局所調整も16-bit画素で処理。ICC名による入力判定に対応。RAWのHDR現像・ICC色変換は未対応 | 一部 |
| BT.2020・PQ出力 | 広色域・HDRの動画エンコード | `--input-color bt2020 --output-gamut bt2020 --dynamic-range sdr\|pq`と本体プリセットに対応。既存FFmpegで最終変換・メタデータを設定し、162色プラン・48形式選択・94実動画を比較。素材は対象の伝達特性で符号化済みのRGB画像が必要。ICC変換や一般的なHDRトーンマッピングではない | 一部 |
| 最終SDR色変換 | 本体の出力レンジ・色差変換・メタデータ | `--output-color lrt-7.5.5`と保存プリセットの既定で、full-range YUV444 10-bitを経由する最終エンコードに対応。144平面ケース・実動画12ケースを比較。内部フィルター、ICC、HDR・広色域まで含む全体一致は未対応 | 一部 |
| オーバーレイ・字幕 | 透かし、撮影時刻表示、SRT字幕出力 | `render --watermark TEXT`で任意テキストを各フレームに焼き込み、位置を5種類から選択可能。`render --timestamp-overlay`で撮影日時も表示し、`subtitles`で同じ日時情報からSRT字幕を出力。`subtitles --profile lrt-7.5.5 --every N`は本体のSRT時計と元フレーム単位の表示間隔に対応し、1,344件の実測と比較。動画表示は既定のGoフォントに加え、`--timestamp-style lrt-7.5.5`でArial/libass方式へ対応。通常SDR・偶数寸法で66実動画を比較。`--watermark-style lrt-7.5.5`で本体のArial透かし、固定位置、文字サイズ、色・影、字幕・フェードとの合成順に対応し、42動画を追加比較。LRT設定JSONのrenderCopyrightEnabled／renderCopyrightTextも読み込む | 一部 |
| フレーム補間 | 速度変更時のブレンド・動き補償 | `--interpolation 1`でブレンド、`2`で低速の双方向モーション補償。保存プリセット・字幕・バッチに対応。ブレンド2,304ケース、モーション720ケースの平面出力ハッシュ・枚数を比較。通常SDRプロファイルでは低速時の8-bitへの切り替えと後段Blur/Sharpen/CASも同じ深度で処理し、追加の22経路・132実動画を比較。ICC/HDRを含む本体動画全体の完全一致は未確認 | 一部 |
| Motion Blur Plus | 書き出し動画への低/中/高モーションブラー | `render --motion-blur N`でLRTの7種類のフレーム数と`(i/N)^1.5`の3桁重みを適用。先頭Nフレーム除去と字幕の開始位置に対応。通常SDR・偶数寸法・字幕/透かしなしでは10-bit YUVをfloat32で積算し、高速補間や動画デフリッカー・ノイズ/光跡除去と併用可能。22経路・132実動画をスカラー参照と比較。低速ブレンド/モーション補間後は8-bit YUVで合成。字幕/透かしなどは既存RGB経路で、全体一致は未確認 | 一部 |
| 取り込み | フォルダー/カードまたは動画ファイルからの取り込み、サブフォルダー作成、日時順整列、JPG省略、日時リネーム | `import SOURCE --output DIRECTORY`で写真ファイルと対応XMPをコピー。任意で再帰検索、撮影日時順、JPEG/JPG除外、撮影日時へのリネーム、撮影間隔による連番分割、dry-runに対応。`--convert-dng`で対応RAWをlossless/uncompressed/lossy DNGへ変換し、他の画像とXMPを取り込み。`import-video VIDEO --output DIRECTORY`でFFmpegから動画の全フレームまたは指定fpsのフレームをJPG/PNG連番へ抽出。`--split-intervals`で本体の間隔変化・最小連番長による分割に対応。`--dng-long-edge`でロッシーDNGを縮小可能。取り込み画面内の画像プレビューは未実装（既存連番には`preview-images`を使用可能） | 一部 |
| DNG変換 | 取り込み時のロスレス/ロッシーDNG変換、サイズ縮小（Pro） | `import --convert-dng`はAdobe DNG Converterを既定で使い、lossless/uncompressed/lossy DNGと隣接XMPを取り込む。`--dng-long-edge`でロッシーDNGの長辺を制限できる。`--dnglab PATH`でDNGLabを明示選択可能。ロッシーと縮小はAdobeバックエンドのみ。LRTプリセットとの画素一致、JPEG XLは未対応 | 一部 |
| バッチ・並列処理 | バッチプレビュー/補正、マルチスレッド処理 | `batch-render ROOT --output DIRECTORY`で複数の画像連番を動画化。既定でルート自身と直下、`--recursive`で全階層から連番フォルダーを検索し、`--workers`で並列数を指定。`--`以降で通常の`render`オプションを共通指定できる。`batch-interpolate ROOT`は同じ探索範囲でXMPを一括補間し、並列解析・全連番の内容検証・バックアップ付き保存に対応。`batch-deflicker ROOT`は複数パス・RAW/XMP解析・リセットを一括実行し、全連番の内容検証後に保存。`batch-holy-grail ROOT`はEXIF/LRT入力・自動形状・評価マーク・リセットを一括解析し、全連番の内容検証後に保存。`batch-preview-images ROOT --output NEW_DIRECTORY`でJPEG連番・測光結果を並列生成し、任意で元XMPへPreview EVを保存。全連番の生成・XMP内容検証後に出力を公開 | 一部 |
| GUI | シーケンス一覧、カーブ表示、プレビュー編集 | `lrtl gui`でループバック限定のローカルWeb UIを起動。画像一覧/サムネイル、全フレームのXMP Exposure2012と解析した画像輝度のグラフ表示、クリックによるフレーム選択、選択フレームの131数値プロパティ編集と対応範囲のXMP値を適用する静止画プレビュー、元画像上のドラッグによるクロップ矩形/回転角度の編集と5★保存、4/5つ星マーク、輝度分析で候補一覧を出し選択保存、JPEG/PNG/TIFFまたはRAW内蔵プレビュー/Adobe DNG Converterによる現像を使った候補分析、XMP補間、動画レンダーを操作できる。レンダーは比率/解像度、音声、選択JSON/ファイル名フィルター、撮影日時と透かし、モーションブラー、H.264/H.265品質も指定可能。書き込みとレンダー開始前に確認を求め、XMPの既存バックアップを作成。グラフからの現像値編集、LRT連続画像Visual Preview、Camera Raw現像プレビュー、フォルダー選択ダイアログ、フレーム移動/削除は未対応 | 一部 |
| 出力フェード | 動画末尾の映像・音声フェード | `--fade-out`と保存プリセットに対応。通常SDRは最終色変換・字幕の後にFFmpegで暗転し、時間処理・シャープ/CAS・低速補間と併用可能。42件の保存参照と30実動画で比較。標準Go字幕/透かしの描画はRGB近似 | 一部 |
| 出力時シャープ | 動画レンダー時の追加シャープ・CAS | `render --sharpen`／`--cas`と保存済みの両設定に対応。純Goで7×7輝度シャープとCAS 0.90の輝度・色差処理を実装。通常SDR・偶数寸法・字幕/透かしなしでは固定小数点RGB16→YUV10とフィルター結果をYUVのまま出力し、84実動画ケースで全画素一致を確認。その他は既存RGB近似。全時間フィルターを含む本体動画全体の一致は未確認 | 一部 |
| 動画ノイズ除去 | 4段階の時間・空間ノイズ除去 | `render --denoise 1..4`と保存済み`denoise`／`denoiseLevel`に対応。純Goの時間平均とウェーブレット、最強段階の点状ノイズ前処理。速度変更・Motion Blurより前に適用。354件の平面/連番観測と深い変換の全出力ハッシュを比較。RGB/YUV変換は独自近似。本体の小画像SIMD処理には差があり、色管理の完全一致は未確認 | 一部 |
| レンダー時のLuma/Chroma Deflicker | 輝度・色差の時間方向の補正 | `--output-deflicker-luma`／`--output-deflicker-chroma`と保存済み`deflickerLuma`／`deflickerChroma`に対応。純Goの10枚パワー平均、短い連番と末尾の挙動を288ケース・686,556画素で比較。実動画・バッチ・処理順をテスト。RGB/YUV変換と本体全体の出力一致は未確認 | 一部 |
| 飛行機の光跡除去 | 時間中央値による一時的な光跡の抑制 | `render --remove-airplanes 1..3`と保存済み`removeAirplanes`／`removeAirplanesLevel`に対応。弱・中は3枚、強は5枚の中央値。中・強の輝度・ハイライト・色差制限も実装。速度変更・Motion Blur、字幕・音声・フェードの時間計算、バッチをテスト。本体の生成ヘルパーと呼び出し式から得た観測との比較で、色管理・全アプリ出力の一致は未確認 | 一部 |
| 出力時リサイズ | レンダー解像度へのLanczos拡大縮小 | 純GoのLanczos3へ変更し、画像の端へ重みを折り返す。16-bit画素とアルファを保持。同梱FFmpegの16-bitグレー24ケース・16,338画素でRMSEを旧方式の約980から約44へ削減。プリセット読込は本体480件の寸法に一致し、最終領域だけを計算して大きな中間画像を作らない。偶数のネイティブ経路に固定小数点RGB8/RGB16/JPEG処理を追加し、144保存参照と324動画で拡縮・回転・クロップを比較。PNG/TIFFグレーも72保存参照・192動画で追加比較。全形式・全寸法の一致は未確認 | 一部 |
| クロップ位置 | レンダー時の切り出し位置 | `render --crop-position 0..100`で左／上から右／下へ指定。50は従来の中央。保存プリセットの寸法処理と通常の`--aspect`の両方に対応し、本体288件の矩形と比較。AVI・MP4・Motion Blur・バッチ・保持テスト済み。保存JSONの位置フィールドは見つかっておらず自動読込は行わない | 一部 |
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
4. 現像済みJPEG/PNG/TIFF、対応RAW内蔵プレビュー、またはAdobe DNG ConverterでデモザイクしたRAWをMJPEG AVI、H.264 MP4／H.265 MP4・MOV、ProRes MOVにまとめる。必要なら`--aspect 16:9 --output-height 1080`のように出力比率と高さを指定するか、`--long-edge`で縮小する。XMP値未焼き込みのsRGB入力には`--apply-xmp-basic`で基本8項目、Texture/Clarity/Dehaze/Sharpening/NoiseReduction/Vignette、4領域パラメトリックカーブ、8色域のHSL補正、Temperature/Tintの簡易ホワイトバランス、ColorGrade近似、明示ポイントカーブを近似適用できる。
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

`long-term`は画像を最大64×64点のグリッドでサンプリングし、線形sRGB輝度の中央値、輝度log2の90–10パーセンタイル差（EV）、彩度で重み付けした円形平均色相（0–360度）を記録します。`--luminance-range`、`--contrast-range`、`--hue-range`はそれぞれ`MIN,MAX`で指定し、指定した全範囲を満たすフレームをJSONマニフェストに含めます。色相は`MIN>MAX`で0度をまたぐ範囲を表せます。無彩色など平均色相が定まらない画像は色相範囲指定時に除外します。範囲を何も指定しなければ全画像を含めます。`render --selection`は元フォルダー、ファイル名、サイズ、更新時刻を照合してから含まれる画像をレンダーし、画像やXMPを変更しません。`render --filename-contains TEXT`はさらにファイル名の大文字小文字を無視した部分一致で絞り込みます。LRTimelapse 7にも「Select by Filename」がありますが、lrtlはCLI絞り込みのみで、ファイル一覧上の選択UIはありません。LRTimelapseも長期用にLuminance/Contrast/Hueフィルターを提供します。既定のlrtl指標は独自実装です。`--metric-profile lrt-7.5.5`を選ぶと、元画像のRGBヒストグラムを半径2・整数除算で平滑化した平均÷256を輝度に使います。コントラストは64×64単一パスbilinear縮小・scale 4のGaussian blur後のグレー標準偏差×2、色相は同じぼかし画像のJava式HSB色相の算術平均です。112条件の実測と比較しました。新しいJSONは`metric_revision: lrt-7.5.5-histogram-v1`で計算版を記録します。既存JSONの選択フラグはそのまま使えますが、過去の同プロファイルでの測定・判定は再解析すると変わる場合があります。`contrast_stddev_0_to_1`というJSONキーは互換性のため残していますが、この版では標準偏差の2倍です。[測定記録と対応条件](internal/lrtl/testdata/lrt-longterm-metrics-7.5.5.md)を参照してください。`--lrt-filter-preset FILE`を使うと、保存プリセットのConstant／時間方向の基準曲線と曜日・時刻条件で選択できます。詳しくは[プリセット対応の計算と制約](internal/lrtl/testdata/lrt-longterm-filter-7.5.5.md)を参照してください。LRTの選択構図、曲線UI、Camera Rawプレビューは未対応です。

解析コマンドの`--apply-xmp-basic`、`--apply-xmp-exposure`、`--apply-xmp-crop`はlrtl側の画素近似です。`--apply-xmp-basic`は露出、トーン、色、Temperature/Tintをまとめて含み、`--apply-xmp-exposure`とは同時に使えません。既にLightroom等でその補正を焼き込んだJPEG/PNGでは二重適用になるため、主にRAWプレビューやDNGLab現像画像の解析に使ってください。

`batch-render`はROOT自身と直下の各サブフォルダーを調べ、対応画像を含むフォルダーを個別の連番としてレンダーします。`--recursive`を指定すると全階層を検索します。ROOTが連番の場合は`ROOT名.mp4`のように出力し、配下の連番は相対フォルダーを保って出力します。`--format`は`avi`/`mp4`/`mov`（既定`mp4`）、`--workers`は同時処理数（既定1、最大64）です。`--`の後に指定した通常の`render`オプションは全ジョブへ渡されます。各ジョブの成功・失敗をまとめて表示し、失敗した連番があっても他のレンダーを続け、どれかが失敗した場合は終了コード1を返します。XMP補間の一括適用には`batch-interpolate`、Deflicker補正の一括適用には`batch-deflicker`、Holy Grail補正には`batch-holy-grail`を使います。

## 参照フレームの設定を連番へコピー

```powershell
# 変更予定を確認。画像を指定すると有効な外部XMP、なければ埋め込みXMPを読む
lrtl copy-settings "D:\Timelapse\reference.jpg" "D:\Timelapse\Sequence"

# 選択JSONに含まれる画像だけへ、既存XMPをバックアップして保存
lrtl copy-settings "D:\Timelapse\reference.xmp" "D:\Timelapse\Sequence" --selection "D:\Timelapse\selected.json" --write

# コピー後のJPEG/PNG/TIFF連番から基本現像値を反映した動画を生成
lrtl render "D:\Timelapse\Sequence" --output "D:\Timelapse\copied.avi" --apply-xmp-basic
```

`--filename-contains`・`--ratings`・`--invert`も使えます。既定はプレビューのみで、保存には`--write`が必要です。`--no-backup`は`--write`との併用に限ります。コピー元と同じ画像／サイドカー、およびコピー元へのハードリンクは対象から除外します。全対象の入力を検証してから書き込みを開始し、非選択画像と共有するサイドカーの変更を拒否します。保存先はXMPサイドカーで、画像本体は編集しません。保存中のI/Oエラーを含む複数ファイル全体のトランザクションではありません。

対象の`crs`名前空間を削除してから、コピー元の全名前空間をマージします。他の名前空間の既存の単純値は空文字列でも保持し、欠けた撮影日時・評価・カメラ情報・LRT情報はコピー元から補います。構造体・配列・言語別テキストのマージは実機XML処理に合わせています。明示した空のXMPはコピー段階で対象のCamera Raw項目を消去し、既定の保存準備でWB/クロップ状態を補います。`--save-state preserve`ではコピー段階のまま保存します。XMPのない画像はコピー元として拒否します。一部の空フィールドを含む配列では実機も再コピーで要素を追加するため、全形式での冪等性は保証しません。[42例の参照結果・再現手順・残る差](internal/lrtl/testdata/lrt-metadata-paste-7.5.5.md)を参照してください。Camera Rawの全設定を保存できますが、動画へ適用できる項目は既存のlrtl現像機能の範囲です。

### 列を指定したコピー

```powershell
# コピー可能な861列を確認（プロパティ名と型）
lrtl copy-settings --list-properties

# 露出とコントラストだけをコピーする値を確認
lrtl copy-settings "D:\Timelapse\reference.xmp" "D:\Timelapse\Sequence" --properties Exposure2012,Contrast2012

# 曜日等で作成した選択JSONの対象に、局所露出だけを保存
lrtl copy-settings "D:\Timelapse\reference.xmp" "D:\Timelapse\Sequence" --properties 'MaskGroupBasedCorrections[1].LocalExposure2012' --selection "D:\Timelapse\selected.json" --write
```

`--properties`を付けると、コピー段階ではその列だけを実機の登録型で更新します。コピー元にない値は対象の同じプロパティを削除します。既定の保存準備は非選択のWB/クロップ値と状態も更新するため、列コピーだけに限定する場合は`--save-state preserve`を指定してください。撮影日時・評価・非選択のマスクIDは保持します。空文字列は欠損とは別です。プレビューには型変換後の値または`remove`を表示します。文字列・真偽値の表記は保持し、倍精度数値は小数6桁、整数はJavaの読込・丸めに合わせます。スライダー範囲でのクランプはしません。不正な数値表記が0へ変換される動作や、非有限値の文字列保存も実機に合わせています。動画へ適用できる設定と値は既存レンダー機能の範囲です。

プロパティ名は`Exposure2012`／`crs:Exposure2012`、`CameraProfile`／`crs:CameraProfile`のように指定でき、局所補正では既存CLIのドット表記と登録表の`/crs:`表記を使えます。重複した指定はまとめ、実機の列順で処理します。ファイル名・画像寸法・撮影日時・外部露出既定値の先頭5列、未登録の位置、カーブ配列の指定は受け付けません。局所補正の作成は実機の配列準備処理に合わせ、親配列がない二重配列や、空から一度に作れない位置は書き込み前にエラーにします。必要なマスク構造は先に`init --write`または全体コピーで準備してください。

既定の`--save-state lrt-7.5.5`はWB/クロップ保存準備を行います。RAW系のWBを移す場合は`WhiteBalance,Temperature,Tint`など必要な列を一緒に選んでください。`FLAT`では相対WB数値を保持し、レンダーは`lrt:Quality=FLAT`の相対WBをsRGB方式で近似反映します（Adobeとの画素一致は未確認）。`HasCrop`はクロップ5項目から再計算されます。内部HG／Deflicker／Global露出の書き戻しにも対応しました。履歴・撮影情報などその他の付随情報の更新は未対応です。コピー先のマスクと別の形状へ移すためのID／構造対応付けも行いません。[実機部品の1,001件、配列準備9件、残る差と再実行方法](internal/lrtl/testdata/lrt-selected-columns-7.5.5.md)を参照してください。

### 登録列とWB・クロップの保存状態

`copy-settings`は既定で`--save-state lrt-7.5.5`を使います。登録861列を処理し、存在する数値755列を選択外も含め登録型で保存します。WBが欠けていれば`As Shot`を追加します。完全一致する`As Shot`はWB数値4項目を消去し、それ以外ではキャッシュされた`lrt:Quality`が`FLAT`ならTemperature/Tint、それ以外ならIncrementalTemperature/IncrementalTintを消去します。品質文字列から画像形式を推測したり、大文字小文字を揃えたりはしません。

`HasCrop`はCropTop/Left/Bottom/Right/Angleの丸め前の値から厳密比較で決めます。指定されたフラグをそのまま保持する処理ではありません。列コピーでは対象の旧キャッシュを引き継ぎ、選択した数値だけをコピー元で置換します。`Quality`列は、その操作の旧キャッシュが非空なら保存時に元の値へ戻ります。全体コピーではマージ後にキャッシュを読み直します。

```powershell
# 新しい既定動作：WB/クロップ保存準備も行う
lrtl copy-settings reference.xmp SEQUENCE --properties CropLeft --write

# da97960以前のコピー段階だけを使う
lrtl copy-settings reference.xmp SEQUENCE --properties CropLeft --save-state preserve --write
```

プレビューには保存プロファイル、最終的な選択列、Quality/WB/HasCrop/Ratingを表示します。欠損列は直前の名前空間を引き継ぐ実機の削除処理に合わせるため、コピー元で欠損していても対象のCamera Raw値が残る場合があります。HasSettingsがTrueなら非正の評価を0へ、評価1は4へ保存する場合もあります。内部HG/Deflicker/Global露出はコピー先キャッシュから小数最大8桁で書き戻し、旧参照タグ等を整理します。詳しい条件は[登録列の保存比較](internal/lrtl/testdata/lrt-registered-save-7.5.5.md)に記載しています。コピーと保存準備の最終差分だけを反映し、中間の削除・追加によるXML要素順の変更で不要なバックアップを作らないようにしています。小さなクロップ値がゼロへ丸められた後に読み直すと判定が変わる場合があり、すべての入力で再実行時の変更ゼロを保証するものではありません。一般的な再実行・元ファイル保持・バックアップ・AVI画素はテストしています。

入れ子のRDF構造にある同名の値を全体設定として読まないよう、レンダー側のWB・トーン・クロップ参照も修正しました。入れ子メタデータを保持したまま、根の設定だけで動画を生成します。

`init`でも画像からのQuality分類に加えて同じ登録列の保存準備を行います。`init`は読める画像形式の寸法、露出・向き等のカメラ値も保存します。`copy-settings`単独での再分類、フレーム初期化全体、内部キャッシュの全読込経路、全形式・設定別の既定値・特殊レイアウトの復旧、RAW寸法の一部、履歴の完了は未対応です。実機の保存は履歴処理で停止するところまでの比較で、全ワークフローの再現ではありません。裸の`rdf:RDF`形式も扱い、根のメタデータを認識できない入力は書き込み前に拒否します。[実機397条件と対応範囲](internal/lrtl/testdata/lrt-save-state-7.5.5.md)を参照してください。

## インストール

ビルドにはGo 1.23以降が必要です。Go側の依存ライブラリはPure Goです。MJPEG AVI出力にはFFmpegやネイティブDLLは不要です。H.264 MP4／H.265 MP4・MOVとProRes MOV出力、動画からのフレーム抽出には外部FFmpegが必要です（`--ffmpeg`で実行ファイルを指定できます）。RAW現像とDNG変換にはAdobe DNG Converter（または明示選択するDNGLab）が必要です。Adobeは自動検出または`--dng-converter PATH`、DNGLabは`--dnglab PATH`で指定します。FFmpeg、Adobe DNG Converter、DNGLabはlrtlに同梱しません。RAWデコーダー候補の[raw-go](https://github.com/vegidio/raw-go)はLibRawをCGOで静的リンクするためCツールチェーンが必要で、現行のPure Goビルド条件に合わず採用していません。LibRaw自体はLGPL-2.1またはCDDL 1.0で提供されています（[ライセンス説明](https://github.com/LibRaw/LibRaw#licensing)）。

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

`import`は指定フォルダーの対応画像と、見つかった同名XMPサイドカーを出力フォルダーへコピーします。`--recursive`でサブフォルダーも検索し、元フォルダー名を出力名に付けて衝突を避けます。`--sort capture-time`はXMP/EXIF撮影日時順に並ぶ連番を付け、`--rename-by-date`は日時をファイル名に使います。同じ秒の撮影には連番を追加します。`--skip-jpeg`はJPEG/JPGを除外し、`--dry-run`はコピー予定だけを表示します。撮影日時順・日時リネームには全画像の撮影日時が必要です。`--convert-dng`を指定するとAdobe DNG Converterが対応するRAWファイルを`.dng`へ変換し、隣接XMPサイドカーも新しい画像名に合わせてコピーします。JPEG等はそのままコピーします。既存DNGは通常コピーし、ロッシー指定時のみ再変換します。圧縮方式は`--dng-compression lossless|uncompressed|lossy`で指定でき、既定はlosslessです。ロッシーでは`--dng-long-edge N`で長辺を制限できます。Adobe DNG Converterは自動検出し、必要なら`--dng-converter PATH`で指定します。`--dnglab PATH`を指定した場合はDNGLabを使います。

`import-video`はFFmpegで動画をJPGまたはPNGの連番へデコードします。既定ではデコード可能な全フレームを保存し、`--fps N`を指定するとその間隔でフレームをサンプリングします。JPEGの圧縮品質は`--quality 1`（高画質）〜`31`（低画質）で指定できます。抽出先は新しいフォルダーでなければならず、既存フォルダーを置き換えません。インストール済みLRT 7.5.5の動画取り込みもFFmpegで全フレームをJPEGへ展開します。今回確認した呼び出しにはシーン検出やシーン境界での分割がなく、写真の「間隔で分割」は別機能でした。以前は動画のシーン分割を本体の不足対応項目として記載していましたが、その根拠を確認できなかったため訂正しました。写真の間隔分割には`import --split-intervals`を使います。

`timeshift`は`--offset 2h30m`、`--offset=-45s`、`--offset 1d`のような固定時間差で連番の撮影日時をずらします。既定はプレビューで、`--write`を付けるとXMPサイドカーへ保存し、既存ファイルはバックアップします。外部XMPがなければ対応画像内XMPを引き継ぎ、他の現像設定・未知のRDFを保持します。対象は根の`DateTimeOriginal`と`CreateDate`で、単純値の`DateTimeDigitized`は読込時に`CreateDate`へ正規化します（両方あれば明示された`CreateDate`を優先）。プリセット内の同名日時は変更しません。`ModifyDate`と画像に埋め込まれたEXIF値は変更しません。XMPの日付値がなく画像EXIFで撮影日時を読める場合は、ずらした`DateTimeOriginal`をサイドカーへ追加します。

XMPを読む処理は、同じ画像に外部`.xmp`サイドカーがあればそれを優先します。サイドカーがないJPEG画像では標準/拡張XMP APP1、TIFF/DNG/CR2/RW2/NEF/NRW/ARW/SR2/PEF/ERF/DCR/IIQではTIFF XMLPacket tag 700、ORFではIIRO/IIRS/MMORマジックのTIFF XMLPacket tag 700、RAFではヘッダーのオフセット/長さで指定される埋め込みJPEGの標準/拡張XMP APP1を読みます。CR3ではISO BMFFのAdobe XMP UUID boxを探し、サイズとXMLパケットを検証します。既定初期化は本体の採用規則に合わせ、画像内XMPをJPEG／DNG／TIFFに限定し、その他のRAWではサイドカーだけを使います。通常読込／`preserve`では上記の埋め込みXMP対応を維持します。埋め込みXMPを編集した場合も、変更は外部サイドカーへ保存し、元画像を書き換えません。組み立てた埋め込みXMPの上限は32 MiBです。これら以外のRAWコンテナーに埋め込まれたXMPは未対応です。

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
lrtl holy-grail "D:\Timelapse\Sequence" --lrt-holy-grail-settings --write

# XMPへPreview EVを書き込まず、既存のVisual Previewキャッシュから補正入力を測定
lrtl holy-grail "D:\Timelapse\RAW" --lrt-preview visual
# .lrt/lrtsequence.jsonの参照領域で測光し、補正を保存
lrtl holy-grail "D:\Timelapse\RAW" --lrt-preview visual --lrt-reference --write

# 補正曲線の伸縮・回転を調整
lrtl holy-grail "D:\Timelapse\Sequence" --stretch 0.4 --rotate -0.2
# LRT入力では、隣接2/3つ星のHG区間を使う形状調整
lrtl holy-grail "D:\Timelapse\Sequence" --lrt-input --stretch 0.4 --rotate -0.2

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

# RAW本体を開かず、LRTが生成済みのvisualプレビューキャッシュを測光
lrtl keyframes "D:\Timelapse\RAW" --lrt-preview visual

# Adobe DNG ConverterとGoでRAWを現像し、その輝度曲線から候補を選ぶ
lrtl keyframes "D:\Timelapse\RAW" --raw-develop --apply-xmp-basic --apply-xmp-crop --count 5

# 長期連番を測定し、輝度・コントラスト・色相の範囲から選択する
lrtl long-term "D:\Timelapse\Developed" --output "D:\Timelapse\selection.json" `
  --luminance-range 0.01,0.9 --contrast-range 0.5,12 --hue-range 330,40

# LRTimelapse 7.5.5の長期測定手順に合わせた指標をJSONへ記録
lrtl long-term "D:\Timelapse\Developed" --output "D:\Timelapse\lrt-metrics.json" --metric-profile lrt-7.5.5

# LRTが生成済みのvisualキャッシュを使って長期測定（RAWデコード不要）
lrtl long-term "D:\Timelapse\RAW" --output "D:\Timelapse\lrt-metrics.json" --metric-profile lrt-7.5.5 --lrt-preview visual

# 保存済みの長期フィルター設定で選択し、選択結果だけ動画にする
lrtl long-term "D:\Timelapse\Developed" --lrt-filter-preset "D:\Timelapse\lrt-settings.json" `
  --lrt-filter-preset-name default --output "D:\Timelapse\selection.json"
lrtl render "D:\Timelapse\Developed" --selection "D:\Timelapse\selection.json" --output "D:\Timelapse\filtered.avi"

# 現像済みJPEG/PNG/TIFFから24 fpsのMJPEG AVIを作成（FFmpeg不要）
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\preview.avi"

# XMPを反映した非圧縮16-bit TIFF中間画像を新規フォルダーへ書き出す
lrtl export "D:\Timelapse\Developed" --output "D:\Timelapse\TIFF" --apply-xmp-basic --apply-xmp-crop

# 連番をフォルダーから探して並列レンダー（MP4、最大2ジョブ）
lrtl batch-render "D:\Sequences" --output "D:\Exports" --format mp4 --workers 2 --recursive -- --fps 24 --crf 18 --preset medium

# 複数連番のXMP補間を確認し、バックアップ付きで一括保存
lrtl batch-interpolate "D:\Sequences" --recursive --workers 2
lrtl batch-interpolate "D:\Sequences" --recursive --workers 2 --write

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

# 既存のLRT visualキャッシュを入力にして輝度グラフを作成
lrtl preview "D:\Timelapse\RAW" --output "D:\Timelapse\raw-brightness.svg" --lrt-preview visual

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

# 本体と同じArial透かし（位置は固定、ネイティブ字幕との併用も可能）
lrtl render "D:\Timelapse\Developed" --output "D:\Timelapse\native-watermark.mp4" --output-color lrt-7.5.5 --watermark "Kota Timelapse" --watermark-style lrt-7.5.5

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

`--apply-xmp-crop`は各サイドカーの`crs:HasCrop`、`CropTop/Left/Bottom/Right`、`CropAngle`を読み、回転した矩形クロップを適用して入力と同じ解像度に再サンプルします。クロップのないフレームはそのままです。空または範囲外の矩形はエラーにします。このオプションはクロップだけを適用します。手動のPerspectiveRotate/Scale/Vertical/Horizontal/Aspectは`--apply-xmp-basic`で別途近似適用できます。Upright自動/GuidedとAdobeの保存済み変換は再現しません。Lightroom等でキーフレームのクロップ値を保存し、`interpolate --write`で間を埋めてからこのオプションでレンダーできます。

`--auto-orient`は入力画像、RAW内蔵プレビュー、またはDNGLab出力のEXIF Orientationを読み、回転と鏡像を画素へ適用してからXMPクロップ、色調補正、Deflicker、リサイズを行います。向き情報がないフレームはそのままです。向きを適用した後の全フレームは同じ幅と高さである必要があります。この処理は入力ファイルとメタデータを変更しません。

`--motion-blur N`は出力前に時間方向へ隣接フレームを合成します。選べる窓幅はLRTのRenderPresetで確認した2、3、4、5、10、15、20枚で、0は無効です。インストール済みLRTのレンダー引数生成処理から、`tmix=frames=N:weights='...'`と各重み`(i/N)^1.5`（`i=1..N`、小数3桁）を確認しました。重みは古いフレームから現在フレームへ向かって増え、FFmpegの既定スケールで合計1に正規化されます。各出力フレームは現在と過去N-1枚から作り、先頭では最初のフレームを複製して窓を満たします。XMP/露出補正、クロップ、リサイズを先に行い、指定時刻の文字焼き込みは合成後に行います。lrtlはPure Goの16-bit RGBで同じ重みと時間窓を計算します。LRTのレンダー経路は10-bit YUV444でフィルター処理するため、数値上の重みと窓の一致は確認済みですが、画素値の完全一致は未確認です。一時フレームバッファーの推定量が768 MiBを超える場合は処理を開始しません。大きなフレームでは`--long-edge`または窓幅を下げてください。

`--timestamp-overlay`と`subtitles`は、各サイドカーの`exif:DateTimeOriginal`、`xmp:CreateDate`、`xmp:ModifyDate`の順で日時を探し、なければ画像内EXIFの撮影/作成/更新日時を使います。全フレームの日時が必要です。`subtitles`は各画像の撮影日時を、そのフレームが動画上に表示される区間のSRT字幕として書き出します。`--fps`は動画のフレームレートと合わせてください。既定の表示形式は`2006-01-02 15:04:05`で、`--timestamp-format`へGoの日時レイアウトを渡して変更できます。`subtitles --profile lrt-7.5.5 --every N`は、本体のSRT生成と同じ元連番を基準にし、fps・速度を単精度へ変換した時刻を有効数字6桁で切り捨てます。`--every -1`は本体の字幕用fps、正の値は元フレーム間隔です。低速時は本体の剰余計算により隣接フレームも選ばれる場合があります。このモードは動画の補間・前後トリムをSRTへ反映しません。既定の`standard`は従来どおり動画の最終区間へ合わせます。`render --timestamp-overlay`は既定のGoフォントで日時を右下に焼き込みます。`--timestamp-style lrt-7.5.5`は本体のArial/libassスタイルを選び、`--timestamp-every N`で元フレーム単位の表示間隔を指定できます（-1は本体の字幕用fps）。`--output-color lrt-7.5.5`、偶数寸法のMP4/MOV、透かしとの併用時は`--watermark-style lrt-7.5.5`が必要です。既存FFmpegのsubtitles/libassとインストール済みフォントを使います。`render --watermark TEXT`は埋め込みGoフォントで固定テキストを重ね、`--watermark-position top-left|top-right|bottom-left|bottom-right|center`で配置を選べます（既定は左下）。両方を指定すると日時と透かしを同じフレームに表示します。

`deflicker`はJPEG/PNG/TIFF、`--raw-preview`の内蔵画像、または`--raw-develop`でAdobe DNG ConverterとGoが現像した画像の輝度を線形sRGBへ変換して測定し、指定領域の中央値に平滑化または一定輝度の目標曲線を合わせます。既定の`--smoother gaussian`はGaussian平滑化を使い、`--smoother lowess`はtricube重み付き局所線形回帰を使います。LOWESSの近傍数は`--smooth`の半径から`2×半径+1`へ換算します。LRT 7.5.5のVisual Deflickerも局所線形回帰型スムーザーを呼びますが、lrtlの測光、近傍数、補正応答は独自実装です。`--apply-xmp-basic`（または`--apply-xmp-exposure`）と`--apply-xmp-crop`を指定すると、サイドカーの値を画素へ近似適用します。`--passes 1..8`は候補補正後の画素を再描画・再測定し、追加変更がなくなれば早期終了します。既定はプレビューだけです。

`--write`は識別できるDeflicker専用スロット（なければ`crs:Exposure2012`）へ最終補正を書き、管理量と保存先を`ol:DeflickerCorrection2012`/`ol:DeflickerXMPTarget`に記録します。XMPを適用する再解析は既存管理量に残差を加算し、それ以外は管理量を置き換えます。中間パスはサイドカーを変更せず、最終書き込み時に元のXMPをバックアップします。`--reset --write`は管理分だけを戻し、Holy Grail補正やユーザーの露出変更を残します。Camera Rawの現像パイプラインやLRTimelapseのVisual Deflickerとの数値一致は未対応です。

`set`は`NumericCRSProperties`にある数値のCamera Rawプロパティ131個を明示的に許可し、`set IMAGE PreviewEV VALUE`ではLRTのPreview EVをEV単位で編集でき、XMPの`lrt:ExternalExposureDefault`へ4倍して保存します。GUIの数値編集にもPreview EVを追加しました。HSL色別スライダー、旧式のClarity/Fill Light/Highlight Recovery/増分WB、HDR最大値、カラーグレーディング、色別色相境界、パラメトリックトーン、ノイズ低減、シャープ化、粒子、周辺光量、手動レンズ/パースペクティブ補正などを含みます。名前はAdobeの公開XMP資料、インストール済みXMPテンプレート、LRT 7.5.5の登録情報から選びましたが、LRTのAuto-Transition全項目を網羅すると確認したものではありません。`interpolate`は全体および登録済みローカル数値項目をLRT 7.5.5の3次ベジェスプラインで補間し、`--smoothness 0..100`でLRTの設定値を指定できます（既定50）。登録数値は各プロパティのLRT範囲でクランプし、Integer型はJava互換の四捨五入（.5はゼロから遠ざける）で出力します。先頭フレームのIsMergedHDRがtrueならExposure2012の範囲を-10..10へ広げ、通常は-5..5です。LRT登録外の旧式4項目とColorGradeBalance/ColorGradeHighlightHue/Sat/ColorGradeShadowHue/Satは補間しません。全体プロパティはXMP上の数値を補間し、色相の最短回転はしません。ローカル補正はRDF Seq内の固定位置で照合し、Linear 1–4、Circular 1–2、Brush 1–2、MaskGroup 1–4/8–11を対象に各CorrectionMasks[1]の登録値を補間します。Angleは円形グラデーションとMaskGroup 8/9だけLRTの後処理どおり−45°未満なら90°加算、45°超なら90°減算し、キーフレームにも適用します。`LumRange`は登録上String型のため同じ配列位置の最初の非空値を全フレームへ均等化します。RDF配列がない独自XMPでは共通`CorrectionSyncID`/`MaskSyncID`にフォールバックします。色範囲Type=1ではColorAmountを数値補間し、`PointModels`はLRTの遷移登録に見つからないため各フレームの値を保持します。Depthの画像依存データ、ブラシ描線、登録位置外のマスクは未対応です。MaskGroup 1–4/8–11のMainCurve/RedCurve/GreenCurve/BlueCurveは最初の4つ星（なければ先頭）から同じ位置の対象フレームへコピーしますが、曲線の数値補間は未対応です。非キーフレームで欠けた登録位置のMaskGroup構造は最寄りの実体を持つXMPから複製して作成しますが、キーフレームや登録位置外/Sync ID形式の欠損構造は生成しません。Look/Preset/PointColorsとグローバルポイントカーブは最初の4つ星（なければ先頭）からコピーします。構造均等化の参照フレームは実LRTの保存経路を確認しましたが、PointColors等の全形式の均等化範囲は未確認です。数値一致はLRT 7.5.5との比較前です。`mark`でキーフレームを設定し、`set`でキーフレームの値を入力し、`interpolate --smoothness 50 --write`（LRTの既定スムージング）で間のフレームを補間してから、JPEG/PNG/TIFFでは`render --apply-xmp-basic`で基本8項目、Texture/Clarity/Dehaze/Sharpening/NoiseReduction/Vignette、4領域パラメトリックカーブ、8色域HSL、簡易Temperature/Tint、明示ポイントカーブを、`render --apply-xmp-crop`で回転を含むクロップを画素へ適用できます。既定では既存XMPをバックアップし、`--dry-run`で書き込み内容を確認、`--no-backup`でバックアップを省略できます。Camera Rawの現像エンジンは含まず、レンダー処理は独自近似です。`set`はCamera Rawスライダーごとの許容範囲を検証しません。

`keyframes`は全フレームのXMPに有限な数値`lrt:ExternalExposureDefault`があれば、LRTのフレーム内部値と同じく4で割った値から数量式（最大20）を計算します。`set IMAGE PreviewEV VALUE`またはGUIでPreview EV列を編集すると、この値を入力できます。値が一部でも欠けている場合は、JPEG/PNG/TIFF、対応RAWの`--raw-preview`、`--raw-develop`の現像画像、または`--lrt-preview camera|visual`で選んだLRTキャッシュからPreview EVを測定し、同じ数量式へ渡します。Preview EVはLRT 7.5.5のRGBヒストグラム式で計算し、`--preview-width-camera N`でカメラプレビュー幅を指定できます（既定1024）。`--lrt-preview`、画像解析オプション、`--preview-width-camera`を明示した場合は、XMP測光値が全フレームで揃っていても選択画像から測定します。候補位置は同版の整数間隔で選びます。数量は最大20で、LRTのヘルパーと同じく1個なら先頭、2個以上なら先頭と末尾を含む配置です。`--rating 4|5`で書き込む星評価を指定でき、既定はLRT Wizardと同じ4つ星です。既定数量からは既存の選択中と同じ評価だけを差し引きます。`--apply-xmp-basic`（`--apply-xmp-exposure`と排他的）/`--apply-xmp-crop`はPreview EV測定へ反映します。`--evenly-spaced`は既定数量5で測定と画像デコードを省略し、同じ配置ルールを使います。既定動作では候補が既存4*/5*/2*/3*マークに重なるとそのマークを残して候補から外します。`--replace-existing`を指定すると選択中と同じ星評価を先に削除し、別の2〜5つ星が候補位置にある場合は最大9枚前の空きフレームへ移します。`--write`と併用すると各XMPをバックアップ付きで更新します。明るさの曲がりが大きい位置を優先する以前のlrtl独自方式は`--luminance-deviation`で選べます。この配置法の位置選びは線形sRGB輝度を使います。LRTと同じ数量スライダー、ネイティブプレビュー生成・RAW現像入力の一致は未対応です。

`unmark --force-rating`はlrtl以外が設定した4つ星/5つ星評価にも使えます。その評価を削除するため、評価がキーフレームマーカーとして使われている場合に限ってください。

`snapshot create`は画像をコピーせず、連番に紐づくXMPサイドカーを`.lrtl/snapshots/<name>.zip`へ保存します。名前を省略するとUTC時刻から自動生成します。`snapshot list`はこのフォルダーのアーカイブを検証して一覧表示します。`snapshot restore`は既定で書き込み内容をプレビューし、`--write`を付けるとアーカイブ内のサイドカーだけを復元します。異なる場所へ移動した連番には`--allow-moved`が必要です。置き換える既存XMPは`.lrtl-snapshot-*.bak`としてバックアップします。スナップショット作成後に追加されたXMPサイドカーは削除しません。移動や削除を含むLRTのシーケンス状態スナップショットとは異なる、XMP限定の復元です。

`holy-grail`の既定はEXIFから露出設定値`log2(露出時間 × ISO / F値²)`を求め、先頭フレームとの差を打ち消す独自補正を計算します。対応形式はJPEG、TIFF、CR2、CR3、DNG、RW2です。全フレームに有効な露出時間とISOが必要です。FNumberがない場合はXMPの`lrt:Aperture`を参照します。マニュアルレンズは先に`lens-tag --aperture f/8 --write`で絞り値を登録してください。EXIF値とXMP値の両方が使えないときは、絞りが固定であることを確認して`--ignore-aperture`を指定します。`--stretch`は露出値を最小・最大で正規化した曲線のスケール（-1.5〜+1.5）、`--rotate`はシーケンスに加える直線傾き（-4〜+4）です。`--auto-shape`はLRTで確認したStretch/Rotateの探索刻み・L1評価を使って値を提案します。

`--lrt-input`は、各フレームのXMPにある数値`lrt:ExternalExposureDefault`を読み、LRTのフレーム内部値と同じく4で割ります。LRTの処理順に沿って、2*/3*/4*評価が隣り合う場所で直近最大10件のフレーム差を重み付き平均で補正し、他のフレームにも直前の補正オフセットを引き継ぎます。その後に`--lrt-reduce-deflicker`（0〜1、既定0）のLOWESS近似、中央寄せ/範囲制限、隣接HGキーフレーム間のカメラ設定差、Stretch/Rotateを適用します。全フレームに同プロパティと有効なカメラ設定が必要です。`lrt:ShutterSpeed`・`lrt:Iso`・`lrt:Aperture`を優先し、不足項目はEXIFで補います。星評価は新規作成・変更しません。`preview-ev`は復元したRGBヒストグラム式を指定ラスター全体へ適用しますが、LRTのネイティブプレビュー生成・縮小・ROI選択は再現せず、平滑化単体の実測比較だけではLRT Wizard全体との数値一致は保証しません。`--auto-shape`はこのモデル上で探索します。

`--lrt-holy-grail-settings`を指定すると、この入力モードに加えて、LRTが連番単位で保存する`.lrt/lrtsequence.json`の`filenameSort`による名前順/撮影日時順と、Holy GrailのReduce Deflicker/Stretch/Rotate値を読みます。設定値の補正・順序を含む単独実行と、設定の異なる連番を混ぜたバッチ実行をテストしています。この指定時は個別の曲線値、`--auto-shape`、`--reset`を併用できません。LRTのGUIで設定ファイルを保存した連番に使います。

両モードともHG専用のCamera Raw `LocalExposure2012`スロットがXMPにあればそこへ書き、見つからなければ全体`crs:Exposure2012`を使います。既定はプレビューで、書き込み時は既存サイドカーをバックアップします。EXIFモードでは撮影モードがManualの場合に設定変更位置へ2*/3*評価を提案します。`--lrt-input`では既存のLRT評価をアンカーとして使うため、新しい評価を付けません。補正の再実行では前回分を置き換え、lrtl管理下の元評価も記録するため`--reset --write`で復元できます。

LRTimelapse側では2*/3*が内部HGキーフレームで、4*/5*の編集キーフレームとは別に扱われます。7.5.5のウィザード計算は2*/3*/4*マークを使い、最大10件のフレーム差の重み付き平均を隣接キーフレームで使いながら、フレームごとの曲線を作ります。シーケンス内のフレーム状態への保存処理も一部追跡できました。フレーム状態の3つの値は、MaskGroup形式では`CorrectionName`が`#LRT internal use (HG)`、`(Deflicker)`、`(Global)`の既存スロットへ対応付け、従来形式では`GradientBasedCorrections[5]`〜`[7]`の`crs:LocalExposure2012`へ書き戻します。インストール済みJARの`raw_m_1.xmp`では、従来形式の5〜7番目にある`CorrectionMasks`内の`crs:MaskName`要素が`LRT: don't alter!`という文字列を持ちます。`lrt:ExternalExposureDefault`を1/4にして内部値へ取り込み、`crs:Exposure2012`とは別に保つ経路も確認しました。lrtlは`CorrectionName`が`#LRT internal use (HG)`のMaskGroup、または従来形式の5〜7番目全てで`LRT: don't alter!`署名を確認できた場合に5番目の`LocalExposure2012`へ補正を書き、書き込み先を記録して`--reset`で戻します。いずれの識別条件も満たさないXMPでは`crs:Exposure2012`を使います。LRTのWizardにはRotate（-4〜+4）、Stretch（-1.5〜+1.5）、0〜1の`holyGrailReduceDeflicker`設定があります。lrtlは`--lrt-input`で入力値を読み、10件重み付き差分キュー、隣接フレーム式、曲線の中央寄せ・制限、Stretch/Rotate経路を実装しました。LOWESS単体は実LRTの53ケースで比較していますが、入力値をLRTと同じ画像処理で生成する部分は未対応です。逆解析で確認した式と保存経路は[リバースエンジニアリングノート](docs/reverse-engineering.md)に記録しています。

EXIFモードと`--lrt-input`はどちらも近似で、LRTimelapseと数値一致する機能ではありません。後者はLRTが既にXMPへ記録した入力値を利用し、`preview-ev --write`で同版から復元したヒストグラム計算値を生成できます。既存のLRTプレビューと保存済みROIは`preview-ev --lrt-preview camera --lrt-reference --write`で測光できます。プレビュー画像の生成経路全体との一致は未検証です。Piaの平滑化部品は合成数値列で実測比較済みですが、全ワークフローの比較は未完了です。`preview`は静的な輝度グラフを出しますが、Lightroomの現像画像プレビューは未実装です。センサー感度の誤差、レンズ透過率、自然な明るさの推移も解析しません。RAW現像やXMPの画素反映をする機能ではありません。

## MP4/MOVの映像レンジ

`render --video-range auto|limited|full`で、SDRの映像レンジを指定できます。明示したlimited／fullには、FFmpegの`zscale`フィルター（libzimgを含むビルド）が必要です。

| 指定 | 処理 |
|---|---|
| auto（既定） | 従来のFFmpeg変換・コーデック既定値を使う |
| limited | 8-bitはY=16〜235、10-bitはY=64〜940へ変換する |
| full | 8-bitはY=0〜255、10-bitはY=0〜1023へ変換する。ProResには後述の制約がある |

```powershell
lrtl render C:\photos\sequence --output C:\photos\limited.mp4 --video-range limited
lrtl render C:\photos\sequence --output C:\photos\full.mp4 --codec h265 --video-range full
lrtl render C:\photos\sequence --output C:\photos\full.mov --codec dnxhr-hqx --video-range full
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --video-range limited
```

LRTプリセットの`videoRange=1`をfull、それ以外を本体の既定分岐と同じlimitedとして読み込みます。CLIで明示した値を優先します。通常の`standard`色変換ではRGB48からBT.709行列で変換し、`--video-range auto`は従来のFFmpeg変換を使います。

`--output-color lrt-7.5.5`は、本体の通常SDR出力に合わせ、RGB48→full-range YUV444 10-bit→指定レンジ・色差形式の順に変換し、BT.709・レンジ・MOVガンマ情報を渡します。**保存プリセットではこのプロファイルが既定**です。このとき`--video-range auto`もlimitedへ解決します。`--output-color standard`で通常の経路を明示できます。MP4/MOVとlibzimg入りFFmpegが必要です。プリセットの画像フィルターだけをAVIに使う場合も`--output-color standard`を明示してください。バッチでも`--`以降に同じ指定を渡せます。

広色域／PQは、最初の選択画像のICC名で入力を自動判定します。`--input-color bt2020`で解釈を明示することもでき、`--output-gamut bt2020 --dynamic-range sdr|pq`を指定します。PQ指定時の素材もPQ符号化済みである必要があります。ICCを読み取ってsRGB素材を変換する機能ではありません。BT.2020からBT.709 SDRへの変換も本体のBT.2020-10解釈で行い、一般的なPQ→SDRトーンマッピングは行いません。HDRの実メタデータ検査はH.265で行っています。本体由来のH.264/PQなどの色タグの不整合も含め、[色出力の測定記録](internal/lrtl/testdata/lrt-wide-output-color-7.5.5.md)を参照してください。

通常SDRプロファイルでシャープ／CASを指定し、出力寸法が偶数、字幕・透かしなしの場合、Goで本体と同じ固定小数点RGB16→YUV10変換を行い、シャープ→CASの結果をYUVのままエンコーダーへ渡します。RGBへ差分を戻す従来経路で発生した色差クリップを避けます。`standard`、奇数寸法、標準スタイルの字幕・透かし併用は従来のRGB経路です。本体スタイルの字幕・透かしはYUV経路を維持します。

元の実動画比較は等倍・不透明な16-bit PNG／TIFFで、フィルターなし12ケース、シャープ・CAS・両方の84ケースです。RGB8のPNG／TIFFも初段変換と72動画で追加比較しています。ノイズ除去・動画デフリッカー・光跡除去、Motion Blur、高速補間、補間なしの低速複製も、標準スタイルの字幕・透かしがなく偶数寸法の場合、初段からYUVのまま処理します。低速ブレンド／モーション補間では補間時に8-bitへ切り替え、後段も8-bitのYUVのまま処理します。[低速補間の測定記録](internal/lrtl/testdata/lrt-native-slow-color-7.5.5.md)を参照してください。[Motion Blurと再生速度の測定記録](internal/lrtl/testdata/lrt-native-playback-color-7.5.5.md)を参照してください。12経路・72実動画をスカラー参照と比較し、最強併用で以前残っていた1成分・2段階の差は、[AVX2/FMAの演算対応](internal/lrtl/testdata/lrt-gaussian-arithmetic-7.5.5.md)で解消しました。RGB8/RGB16/JPEGの拡縮・回転は[追加測定](internal/lrtl/testdata/lrt-native-scale-8.0.1.md)の範囲で比較済みです。PNG/TIFFグレーは[追加の72条件・192動画](internal/lrtl/testdata/lrt-gray-input-8.0.1.md)で比較済みです。その他の入力形式、全寸法・全フィルターの組合せ、ICCを含む全経路、HDR・広色域の再現は未確認または未対応です。測定条件と再実行方法は[testdataの説明](internal/lrtl/testdata/lrt-sdr-output-color-7.5.5.md)と[Go空間フィルターの説明](internal/lrtl/testdata/lrt-native-spatial-color-7.5.5.md)を参照してください。

ProResでは、実測したFFmpeg 8.0.1でレンジ情報がffprobeへ報告されず、fullの10-bit端点も4〜1019へクリップされました。limitedが一般的な用途に適します。H.264/H.265/DNxHRではfull／limitedのタグと実際の輝度値を確認しています。AVIはfull-range JPEGのためauto／fullを受け付け、limited指定は保存前にエラーにします。

本体のレンジフィルター生成3ケース、全コーデック経路への引数、プリセット／バッチ／CLI上書き、元ファイル保持をテストしています。実際のH.264・H.265・ProRes・DNxHR出力について、黒・白・中間グレーとBT.709の行列情報も検査しています。本体の色変換済み中間画像とGoのRGB入力の完全一致を検証したものではありません。

### H.264／H.265の色差形式・bit深度

`--subsampling 420|422|444`で選択します。既定の420は8-bit、422／444はLRT本体の設定に合わせて10-bit出力です。`4:2:0`／`4:2:2`／`4:4:4`表記も受け付けます。保存したプリセットの`subSampling`も読み込みます。H.265はMP4とMOVの両方へ出力できます。

```powershell
lrtl render C:\photos\sequence --output C:\photos\master.mov --codec h265 --subsampling 444 --video-range limited
```

H.264／H.265だけに適用する設定です。ProRes／DNxHRは各プロファイルの形式を使います。FFmpegが要求した形式に対応するか出力作成前に確認し、未対応なら既存出力を保持してエラーにします（FFmpegの[非対応pixel formatの代替選択](https://ffmpeg.org/ffmpeg.html#Advanced-Video-options)を防ぐため）。`standard`は従来のSDR形式です。ネイティブBT.2020/PQ出力では4:2:0も10-bitを選びます。準備済み画像の94動画を比較していますが、ICCや任意素材の全画素一致は未確認です。

本体ヘルパーの36観測、CLI／プリセット／バッチ、形式非対応時の出力保持をテストしています。外部FFmpegが利用できる環境では18種類の実動画を生成し、bit深度・色差形式・レンジ・16-bit入力から残る輝度階調も検査します。追加で2枚入力のMOV／MP4の表示・デコード時刻を確認します。旧x265で短い入力の時刻が不正になる問題を避けるため、H.265の出力が1〜2枚ならBフレームを無効にします。観測条件は[検証メモ](internal/lrtl/testdata/lrt-h26x-format-7.5.5.md)に記載しています。

## レンダー時の拡大縮小

`--output-height`／`--long-edge`による出力時の拡大縮小は、本体が選ぶLanczos系に合わせて純GoのLanczos3を使います。以前のCatmull-Romから変更しました。[FFmpegの仕様](https://www.ffmpeg.org/ffmpeg-scaler.html)にある既定幅3のsinc系カーネルを、縮小率に合わせて広げ、範囲外の重みを最端画素へ折り返します。色付きの透明画素による縁の混入を避けるため、アルファを乗じた色を合成してから元に戻します。画像全体の浮動小数点中間層を持たず、縦方向の窓に必要な行を再利用します。

上記は従来の画像空間経路の説明です。偶数寸法の`lrt-7.5.5`動画では、[固定小数点スケーラー](internal/lrtl/testdata/lrt-native-scale-8.0.1.md)でRGB8/RGB16/JPEGから直接YUV10を生成します。保存プリセットでGoのXMP画素調整がない場合は、拡縮してからYUV平面を回転し、最終クロップを計算します。144保存参照、84の通常CPU／スカラー比較、324実動画で確認する範囲を測定記録に記載しています。

従来経路では、本体のフィルター6件で`flags=lanczos`を確認し、同梱FFmpeg 8.0.1の16-bitグレー24ケースを参照値として保存しています。定数・傾斜・単一ピーク・段差・市松模様・決定的ノイズを、2種類の縮小・1種類の拡大・横方向だけの縮小で比較しました。16,338画素でRMSEは旧方式の980.479から44.413へ下がりました。固定小数点・色変換を含む完全一致ではなく、特に高周波模様の拡大には差が残ります。この数値は合成グレー画像の比較で、実写動画全体の品質スコアではありません。

16-bit値・アルファ・画像の端・非ゼロ座標・縮小時のエイリアス抑制・元画像保持をテストしています。GUIサムネイルとXMPの幾何変換フィルターは今回の変更対象ではありません。

### 保存プリセットの寸法・クロップ

`--lrt-render-preset`を指定すると、本体で観測した寸法選択を使います。保存した解像度は縦位置画像では短辺の指定として扱われ、幅は4の倍数、高さは2の倍数へ揃います。たとえば32×24から保存解像度16では20×16、32×64から保存解像度32では32×64を出力します。`outputResV=0`は元解像度を基準とし、比率制限と寸法の整列は適用します。

奇数寸法を偶数へクロップし、拡大縮小後に指定比率へ中央クロップします。保存比率に近い標準比率への補正や、数画素の差を選択した枠へ戻す本体の処理も含みます。480件の直接観察（12入力寸法・5解像度・8比率）で、拡大縮小の寸法とクロップ矩形を照合しています。EXIFの全8方向で除去する端が一致すること、通常／Motion Blurの画素、バッチ転送、元画像保持、実MP4の解像度もテストしています。

CLIで`--output-height`または`--long-edge`を明示すると、保存された寸法処理よりCLIを優先します。この場合の`--output-height`は従来どおり実際の高さを指定し、`--long-edge`は長辺の上限です。`--aspect`だけの上書きは本体の寸法処理へ渡します。プリセットを使わないCLIの寸法指定は従来どおりです。

本体が生成するゼロ幅／ゼロ高さのクロップは保存前にエラーにします。プリセット寸法経路の処理上限は、最終出力で1辺32768画素・64メガピクセルです。仮想的な拡大縮小後の寸法がそれより大きくても、出力領域だけを計算するため中間画像の上限では拒否しません。Camera Raw現像を含む全体、RGB/YUVの完全一致は未確認です。アルゴリズムと観察条件は[寸法の観察ノート](internal/lrtl/testdata/lrt-geometry-7.5.5.md)を参照してください。

### 大きな中間画像を作らないリサイズ

プリセットの拡大縮小とクロップをまとめ、最終的に残す範囲だけをLanczos計算します。画素中心・重み・端の処理は拡大縮小後の画像全体を基準に保ち、元画像を先に切り取る方法には変えていません。従来の全体計算後クロップと、RGBA／NRGBAの8・16-bit、グレー、パレット、JPEGのYCbCr、汎用Image、非ゼロ座標、透過画像で画素・アルファの一致をテストしています。

たとえば本体が6144×12288へ拡大して6144×384を切り出す設定でも、約604MBのRGBA16中間画像を作りません。Go 1.26.9・Windowsのリサイズ単体ベンチマークでは、同じ最終領域の総確保量は21,709,096バイト（約22MB）でした。元画像デコード・XMP処理・動画エンコード・出力バッファは含まない測定です。実MP4の6144×384・1フレームと復号画素も確認しています。

中間寸法が10800×7200、25920×17280、4320×69120になる本体の設定も、寸法計画と小領域の計算をテストしています。最終出力に収まる場合は、以前の中間64MP／1辺32768の制限で拒否しません。最終出力自体の上限とMotion Blurの作業メモリ制限は引き続き適用します。本体の4件の追加観察と測定方法は[観察ノート](internal/lrtl/testdata/lrt-region-geometry-7.5.5.md)を参照してください。

### クロップ位置

`render --crop-position N`で、比率のために切り出す部分を動かせます。整数の0〜100で、0は左端／上端、50は中央、100は右端／下端です。横方向または縦方向に余った部分へ同じ割合を使い、画素位置は切り捨てます。既定は50で、位置指定なしの従来出力を維持します。比率による切り出しがない場合は位置を変えても画素は変わりません。

```powershell
lrtl render C:\photos\sequence --output C:\photos\left.avi --aspect 1:1 --crop-position 0
lrtl render C:\photos\sequence --output C:\photos\right.mp4 --lrt-render-preset C:\photos\preset.json --crop-position 100
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --aspect 16:9 --crop-position 33
```

プリセット寸法では本体と同じ拡大縮小後のクロップへ適用し、通常のCLI比率指定では拡大縮小前の比率クロップへ適用します。本体の直接観察288件（6入力寸法・2解像度・3比率・8位置）で矩形が一致しました。16-bit画素・アルファ、非ゼロ座標、位置0／中央／100、通常／Motion Blur、AVI画素、実MP4の復号画素、バッチ転送、不正値での出力保持をテストしています。

これはCLIとGo APIからの明示指定です。調べた保存設定には位置フィールドがなく、本体のバッチ経路は50を渡していたため、保存JSONからの位置読込は追加していません。`--crop-position`は現在`render`と`batch-render`で使えます。GUI・XMPクロップの位置やキーフレームアニメーションは変更しません。[観察ノート](internal/lrtl/testdata/lrt-crop-position-7.5.5.md)に条件と制約を記録しています。

## Motion Blurと動画の開始位置

`render --motion-blur N`は、過去Nフレームの重み付き合成後、先頭のN出力フレームを除去します。LRT 7.5.5の通常レンダーで観測した動作に合わせたもので、以前のlrtlの「全フレームを残す」挙動から変更しました。Nは2／3／4／5／10／15／20、0で無効です。速度変更を先に行うため、除去するのは元画像N枚ではなく、速度変更後のNフレームです。

たとえば20枚を2倍速・`--motion-blur 2`で処理すると、10フレームを合成して先頭2フレームを除去し、8フレームの動画になります。fps=4なら動画・音声は2秒です。通常CLIでは、動画のフレーム数、音声の長さ、フェード、完了表示に同じ実出力枚数を使います。プリセット使用時の小数速度の境界例は後述の時間計算を参照してください。全フレームが除去される設定は保存前にエラーにし、既存出力を保持します。除去する先頭画像も、破損やXMPの検証対象です。

```powershell
lrtl render C:\photos\sequence --output C:\photos\motion.mp4 --speed 2 --motion-blur 2 --fps 4
# SRTにも同じ速度・Motion Blurを指定すると、字幕の開始位置と長さが一致する
lrtl subtitles C:\photos\sequence --output C:\photos\motion.srt --speed 2 --motion-blur 2 --fps 4
```

TIFFの`export`とJPEGの`preview-images`は、元画像との対応を保持するため先頭を除去しません。同じ合成を行い、動画はその後半と対応します。本体から取得した48件のフィルター（全7窓幅＋無効、3速度、2連番長）をLRT同梱FFmpegで実行し、動画の枚数と開始位置をテストしています。RGB16合成と本体のYUV10合成には丸め・色変換の差があるため、画素の完全一致は未確認です。実MP4の映像・音声の長さとフェード、AVIヘッダー・画素、字幕、失敗時の保持も回帰テストに含めています。

## 動画と字幕の再生速度

`render --speed MULTIPLIER`で、出力fpsを保ったまま連番の再生速度を変更できます。`1`が通常、`2`が2倍速、`0.5`が半速です。範囲は0.0625〜16で、AVIは追加ソフトなしで利用できます。LRTプリセットの`speed`も読み込み、CLIで明示した値を優先します。

```powershell
lrtl render C:\photos\sequence --output C:\photos\fast.avi --speed 2
lrtl render C:\photos\sequence --output C:\photos\slow.mp4 --fps 29.97 --speed 0.5
# 字幕も同じfps・速度で生成し、元画像の撮影時刻を対応させる
lrtl subtitles C:\photos\sequence --output C:\photos\slow.srt --fps 29.97 --speed 0.5
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --speed 2
```

本体の通常の速度フィルターに合わせ、フレームを間引き・複製します。2倍速では0始まりの元フレーム1/3/5…、半速では0/0/1/1…を出力します。既定の`--interpolation 0`は中間画素を生成せず、`--interpolation 1`で後述のブレンド補間を選べます。端点と単精度丸めのため、たとえば速度0.33333334で元画像が1枚なら出力は2枚となります。速度変更後にフレームが残らない短い連番は保存前にエラーにします。

XMP現像・Deflickerの解析対象は元の連番です。速度変更をMotion Blurより前、透かし・撮影日時表示・フェードをその後へ適用します。複製した画像の処理結果を再利用し、撮影日時は元画像と対応させます。字幕にも同じ間引き・複製を使います。音声の再生速度は変更しません。通常CLIは実出力枚数に音声とフェードを合わせ、保存プリセットでは次節の本体と同じ時間計算を使います。元画像やXMPは変更しません。

本体から取得した20種類の通常速度フィルターを、FFmpeg 8.0.1で80ケースの短い合成連番へ適用し、Goのフレーム対応と比較しています。RGB48画素、XMP反映、Motion Blur／フェードの順序、撮影日時・字幕、AVI画素とフレーム数、バッチ／プリセット、間引かれる画像の破損時の出力保持、実NTSC MP4もテストしています。LRTの動画出力全体・速度と他の本体フィルターを組み合わせた結果の完全一致は未確認です。速度変更時のAVIは処理済みRGBからJPEGへ再エンコードします。

### 速度変更時のフレーム補間

`render --interpolation MODE`で動画の速度変更に使う補間を選びます。既定の`0`はフレームの複製／間引き、`1`はブレンド補間です。保存プリセットの`interpolation`も読み込み、`--interpolation 0`で明示的に解除できます。`2`は低速で動きを補償します。1倍では補間せず、1倍超では両モードとも本体と同じ均等時間平均を使います。

```powershell
lrtl render C:\photos\sequence --output C:\photos\slow.mp4 --speed 0.5 --interpolation 1
lrtl render C:\photos\sequence --output C:\photos\fast.avi --speed 2.3 --interpolation 1
lrtl subtitles C:\photos\sequence --output C:\photos\slow.srt --speed 0.5 --interpolation 1
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --speed 0.7 --interpolation 1
```

低速では本体と同じ8-bit平面のブレンドとシーン判定を使います。シーン変化の閾値を超える区間は近い画像を選びます。光跡除去後に最低3枚が必要です。最後の入力画像は次の区間を確定するために読み、最後に生成できた補間画像を終端まで保持します。高速では速度を整数へ四捨五入した枚数の過去画像を均等合成してから間引きます。1倍では補間せず、1倍超・1.5倍未満も平均処理を追加しません。Motion Blurはその後の出力フレームへ適用します。

中間fpsの3桁丸め、タイムスタンプの切り捨て、終端時刻の変換により、出力枚数が単純な`N/speed`と違う場合があります。たとえば24 fps・元3枚・0.7倍のブレンド補間では3枚になり、補間なしでは4枚です。保存プリセットの音声・フェードは本体の推定枚数を使うため、この差が末尾にも現れます。`subtitles`へ同じfps・速度・補間・Motion Blur・光跡除去を指定すると、出力枚数と撮影時刻の対応が揃います。補間画像の撮影時刻は左側の元画像を使います。

8-bitの1,728ケースと10-bitの576ケースで、全96,523,260サンプルの出力ハッシュと枚数を比較しています。実H.264／H.265の18ケースは、独立に計算した補間RGBを別途動画化し、復号した全平面を比較します。バッチ・保存設定・字幕・JPEG AVI・破損した後続画像・既存出力の保持もテストしています。中間TIFF・JPEGプレビューには適用しません。時間処理全体のバッファ見積もりが768 MiBを超える場合は出力前にエラーにします。

本体の小画像SIMDシーン判定にはスカラー処理との差があり、ここではスカラーの計算を採用しています。RGB16とYUV10の変換は独自近似です。HDR、完全な色管理と本体全体の出力一致は未確認です。[比較範囲と再実行方法](internal/lrtl/testdata/lrt-interpolation-7.5.5.md)を参照してください。

### 動きを補償したスローモーション

`--interpolation 2`は、低速で物体の動きを推定し、その途中位置へ元画像を移動して合成します。本体の保存プリセットが選ぶ双方向推定に合わせ、16×16pxのブロック、32×32pxの重なり窓、空間・過去2フレームの予測を使います。推定ベクトルは最大32pxの範囲で探索し、シーン切替では近い元画像へ切り替えます。

```powershell
lrtl render C:\photos\sequence --output C:\photos\slow.mp4 --speed 0.5 --interpolation 2
lrtl render C:\photos\sequence --output C:\photos\slow.avi --speed 0.3 --interpolation 2 --motion-blur 2
lrtl subtitles C:\photos\sequence --output C:\photos\slow.srt --lrt-render-preset C:\photos\preset.json
```

出力画像は各辺32px以上、光跡除去後に最低3枚が必要です。処理順は動画デフリッカー→ノイズ除去→光跡除去→補間・速度変更→Motion Blur→Sharpen/CAS→オーバーレイ→フェードです。保存プリセットの`interpolation=2`、バッチ、字幕も同じスケジュールを使います。中間TIFF・JPEGプレビューには適用しません。

本体の指定文字列には`mc_mode=aobmc`／`vsbmc=1`も含まれますが、選ばれている双方向経路は固定ブロックの重なり合成を使います。可変ブロック処理は別の双方向対称推定経路に属します。

横・縦・斜めの移動、加速、移動する矩形、ノイズ、シーン切替、静止を含む720ケースを比較し、96,085,440サンプルの出力ハッシュと枚数が一致しました。このうち468ケースは通常ブレンドと異なる画素です。実H.264／H.265の18ケースでも、独立に計算したRGB参照を別途動画化した結果と復号後の全画素を比較します。返却バッファの所有、3枚の入力キャッシュ・2枚のベクトル場、最大32参照の重なり、破損した未来画像と小さすぎる出力での保存中止をテストしています。

低速の補間内部は本体と同じ8-bit平面です。通常SDRのYUV経路では深度変換から後段フィルターまで[8-bitの参照と比較](internal/lrtl/testdata/lrt-native-slow-color-7.5.5.md)しています。`standard`などのRGB経路ではRGB16/YUV変換を独自近似し、本体の色管理を含む動画全体の完全一致は未確認です。比較対象のCPU・寸法、観測値の出所と再実行方法は[測定記録](internal/lrtl/testdata/lrt-motion-interpolation-7.5.5.md)を参照してください。

### 小数速度の境界とプリセットの時間計算

0.1／0.3／0.7／1.1倍などを含む12速度・3fps・7連番長（最大1001枚）の252ケースを追加し、本体が生成したフィルターを同梱FFmpegで実行しました。166,242出力フレームの対応元を、繰り返し・間引きを失わない区間データとして保存しています。Goの枚数と対応元は全件一致し、CLI画素と字幕にも比較を追加しました。

本体はフェードと音声の長さに、単精度の「元枚数÷速度」を整数化した推定枚数を使います。実際の間引き・複製とは丸め方が違うため、境界で1枚分の差が出ます。`--lrt-render-preset`使用時はこの計算へ合わせ、Motion Blurで除去する枚数も引きます。音声の切り詰め時刻は本体と同じ3桁の小数秒です。CLIでfps・速度・フェード・寸法を上書きしても、プリセットの時間計算は有効です。

たとえば11枚・1.1倍速・24fpsでは、映像は9フレーム／0.375秒ですが、推定は10枚です。プリセット使用時のフェードは10枚分の時刻を使い、音声は0.417秒となり、映像終了後に短い音声の末尾が残ります。この本体フィルター指定を再現するため、音声を映像側の終了条件で途中停止させません。実MP4のフレーム数・映像／音声の長さ・復号した映像と音量をテストしています。

本体の直接観察96ケースで、フェード時刻と音声の指定時間が一致しました。このうち24件では従来の実枚数によるフェードと差があります。プリセットなしのCLIは従来の実枚数基準を維持し、静止画連番にはこの動画用設定を適用しません。完全なLRTエンコーダー出力や色管理の一致を示すものではありません。[観察ノート](internal/lrtl/testdata/lrt-native-timing-7.5.5.md)に条件を記録しています。

## フェードアウト

`render --fade-out SECONDS`で動画の最後を黒へ、MP4/MOVに音声がある場合は同じ時刻から無音へフェードできます。既定の`0`で無効になり、小数秒も指定可能です。動画より長い時間を指定した場合は、動画全体へ短縮します。AVIの映像処理はGoだけで動きます。

```powershell
lrtl render C:\photos\sequence --output C:\photos\fade.avi --fps 24 --fade-out 3
lrtl render C:\photos\sequence --output C:\photos\fade.mp4 --audio C:\photos\music.wav --fade-out 2.5
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --fade-out 3
```

LRTプリセットの`fadeOut=true`／`fadeTime`も読み込み、`--fade-out 0`で明示的に解除できます。開始時刻の小数3桁丸めと、映像・音声の開始／長さの共通化を、本体のフィルター生成16ケースと比較しています。映像はMotion Blur、透かし・時刻表示を適用した後のRGB値を減衰させます。露出補正とは別の処理です。字幕ファイル自体にはフェードを追加しません。

最後のフレームは動画終了時刻より前に表示されるため、完全な黒へ到達するフレームがない場合があります。これは終端時刻へ向けてフェードする方式によるものです。AVIの変更不要な前半JPEGは再圧縮せず、フェードが必要なフレームを再エンコードします。Go側のRGBフェードと、本体の色変換済みYUVフェードの画素完全一致は未検証です。時刻計算、16-bit精度、Motion Blur・透かしとの順序、元画像保持、不正値での既存出力保持、実MP4の映像と音量をテストしています。

## LRTのレンダープリセットを使う

`render --lrt-render-preset FILE.json`で、本体のRenderPreset形式のJSONを再利用できます。LRTの設定JSON全体を渡した場合は`lastRenderSettings`を読み、`--lrt-preset-name NAME`を付けると`renderPresets`から完全一致する名前を選びます。同名が複数ある場合はエラーです。単独のプリセットJSONも受け付けます。

```powershell
lrtl render C:\photos\sequence --output C:\photos\result.mp4 --lrt-render-preset C:\photos\render-preset.json
lrtl render C:\photos\sequence --output C:\photos\result.mov --lrt-render-preset C:\photos\lrt-settings.json --lrt-preset-name "My ProRes Preset"
# 明示したCLI設定を優先する
lrtl render C:\photos\sequence --output C:\photos\result.mp4 --lrt-render-preset C:\photos\render-preset.json --fps 25 --output-height 1080 --motion-blur 0
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --lrt-render-preset C:\photos\render-preset.json
```

`encoder`、`frameRate`、`outputResV`、`outputQuality`を必須とし、省略可能な項目には本体のコンストラクターで観測した既定値を使います。読み込むのは指定したJSONだけで、LRTのグローバル設定を自動探索・変更しません。JSON内の音声が有効な場合、相対パスはそのJSONのフォルダーを基準に解決します。設定JSON全体の`renderConstrainCrop=true`では`renderAR`も使います。動画の拡張子は選んだコーデックに合わせて指定してください。

CLIで明示した`--fps`、`--codec`、`--crf`、`--output-height`／`--long-edge`、`--aspect`、`--motion-blur`、`--audio`、`--speed`、`--fade-out`、`--video-range`、`--sharpen`、`--cas`、`--denoise`、`--remove-airplanes`、`--output-deflicker-luma`、`--output-deflicker-chroma`、`--interpolation`を優先します。H.264の画質0〜5はCRF 22/18/15/10/8/0、H.265は22/20/16/10/8/0へ対応させます。ProResの画質0〜4はProxy/LT/422/HQ/4444、DNxHRはLB/SQ/HQ/HQX/444を選びます。両者の画質5は本体の既定分岐に合わせ、それぞれHQ/HQへ割り当てます。Motion Blurは有効なときだけ保存されたフレーム数を使います。

HDR・広色域が有効なプリセットは、出力を作る前にエラーにします。H.264/H.265のsubSamplingは420／422／444を読み込み、明示した`--subsampling`を優先します。色管理・フィルター結果・本体の出力画素全体との一致を保証するものではありません。

この対応に合わせ、`--codec prores-proxy|prores-lt|prores-4444`も追加しました。FFmpegにはProRes 4444用10-bit YUVを渡し、ffprobeでは4444・12-bitのデコード形式として報告されることを確認しています。完全な12-bit入力精度やアルファチャンネルの保持は実装していません。本体で生成した6種類のJSONと画質ヘルパー値、明示CLIとの画素入力・引数一致、設定と元画像の保持、バッチ転送、不正設定時の出力保持、実MOVのプロファイルをテストしています。

### 出力時シャープ

`render --sharpen`は、リサイズ・速度変更・Motion Blurの後、撮影日時・透かし・フェードの前に輝度のシャープ処理を追加します。AVIでも外部ライブラリやFFmpegを使わず純Goで動作します。XMPの`crs:Sharpness`を使う現像処理とは別の、動画専用オプションです。`export`や`preview-images`の静止画には適用しません。

```powershell
lrtl render C:\photos\sequence --output C:\photos\video.avi --sharpen
lrtl render C:\photos\sequence --output C:\photos\video.mp4 --sharpen
lrtl render C:\photos\sequence --output C:\photos\video.mp4 --lrt-render-preset C:\photos\preset.json --sharpen=false
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --sharpen
```

保存プリセットの`sharpen`を読み込み、CLIの`--sharpen`／`--sharpen=false`を優先します。既定は無効です。LRT 7.5.5の生成フィルター4件と、同梱FFmpeg 8.0.1で処理した32×16の10-bit輝度6パターン（計3072画素）を観察してテストに保存しました。7×7の二項カーネル、端の画素の複製、固定小数点の丸めを比較しています。同梱版の10-bit経路は8-bit経路より効果が弱く、その実測に合わせています。

`--output-color lrt-7.5.5`の偶数寸法・字幕/透かしなしでは、[GoのネイティブSDR経路](internal/lrtl/testdata/lrt-native-spatial-color-7.5.5.md)で変換・シャープ・CASの順に処理し、YUVのまま出力します。その他の経路はRGB48からBT.709の10-bit輝度を近似し、得られた差をRGB各成分へ加えます。クリップしない範囲の色差とRGB16の細部を保持しますが、RGB/YUV変換・時間フィルターを含むLRTの出力動画全体との画素一致は未確認です。処理順、速度によるフレーム複製、16-bit値・アルファの黒背景合成、JPEG保持、AVI画素、プリセット解除、バッチ転送、元画像/XMPと既存出力の保持を回帰テストに含めています。実測値の出所と再照合方法は[観察ノート](internal/lrtl/testdata/lrt-output-sharpen-7.5.5.md)を参照してください。

### Contrast Adaptive Sharpening（CAS）

`render --cas`で、局所コントラストに合わせて強さを変える動画用シャープ処理を追加します。本体が生成する`cas=strength=0.90`に合わせ、強度は0.90固定です。既定は無効で、保存プリセットの`cas=true`も読み込みます。`--cas=false`で明示的に解除できます。

```powershell
lrtl render C:\photos\sequence --output C:\photos\cas.avi --cas
lrtl render C:\photos\sequence --output C:\photos\cas.mp4 --sharpen --cas
lrtl render C:\photos\sequence --output C:\photos\cas.mp4 --lrt-render-preset C:\photos\preset.json --cas=false
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --cas
```

処理順はリサイズ・速度変更・Motion Blur→通常の`--sharpen`→CAS→撮影日時・透かし→フェードです。輝度と色差の全3成分を処理するため、輝度だけの通常シャープとは画素結果が異なります。AVIの処理は純Goで動作し、静止画の`export`／`preview-images`には適用しません。

同梱FFmpeg 8.0.1の10-bit平面で、黒・白・一定値・中央／隅の点・境界・ランプ・ノイズを3強度で比較し、24パターン・12,288画素の計算が一致しました。通常SDRの偶数寸法・字幕/透かしなしは前述のGoネイティブYUV経路を使います。その他のRGB48経路ではBT.709のY/Cb/Crを独自近似し、処理による差分をRGB16へ戻します。全時間フィルター・ICCを含む本体動画全体の一致やHDRには対応していません。[検証条件](internal/lrtl/testdata/lrt-cas-7.5.5.md)を参照してください。

### 動画のノイズ除去

`render --denoise LEVEL`で動画用のノイズ除去を有効にします。`0`が無効、`1`〜`4`が本体の4段階に対応します。保存プリセットの`denoise=true`／`denoiseLevel`を読み込み、明示したCLI値を優先します。`--denoise 0`で解除できます。

```powershell
lrtl render C:\photos\sequence --output C:\photos\clean.avi --denoise 2
lrtl render C:\photos\sequence --output C:\photos\clean.mp4 --denoise 3 --cas
lrtl render C:\photos\sequence --output C:\photos\clean.mp4 --lrt-render-preset C:\photos\preset.json --denoise 0
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --denoise 2
```

| 段階 | 時間方向の窓 | 空間処理 |
|---|---|---|
| 1 | 5枚 | 弱いウェーブレット処理 |
| 2 | 5枚 | ウェーブレット処理 |
| 3 | 7枚 | 同じ空間処理＋広い時間窓 |
| 4 | 13枚 | 点状ノイズの前処理＋強いウェーブレット処理 |

リサイズ・クロップの後、速度変更・Motion Blur・シャープ・CAS・撮影日時・透かし・フェードの前に適用します。先読みするため、後ろの画像も時間方向の補正に使います。動画の枚数と撮影日時の対応は維持し、静止画の`export`／`preview-images`には適用しません。AVIのノイズ除去はFFmpegなしで動作します。XMPのノイズ軽減スライダーやAdobeのAI Denoiseとは別の動画処理です。

時間方向の判定・端の処理、CDF 9/7変換、最強段階の8-bit中間変換を、同梱FFmpeg 8.0.1の354件・644,058出力値と比較しています。最大10段の変換も約428万画素の全出力ハッシュで確認しました。通常CPU実行と`-cpuflags 0`の観測を区別しており、本体SIMD経路の一部小画像では差が残ります。既存RGB経路のRGB48/YUV10変換は独自近似で、本体の色管理・HDRや動画全体の完全な画素一致は未対応です。

ノイズ除去とMotion Blurを合わせたフレームバッファーの推定が768 MiBを超える場合は、出力作成前にエラーにします。`--long-edge`／`--output-height`、ノイズ除去の段階、Motion Blurの窓を下げてください。元画像やXMPは変更しません。[観測値・SIMD差・検証条件](internal/lrtl/testdata/lrt-denoise-7.5.5.md)を記録しています。

### レンダー時の輝度・色差デフリッカー

`--output-deflicker-luma`で輝度、`--output-deflicker-chroma`で色差のフレーム間変化を補正します。保存プリセットの`deflickerLuma`／`deflickerChroma`も読み込み、明示した`--output-deflicker-luma=false`／`--output-deflicker-chroma=false`を優先します。XMP露出を解析・変更する既存の`deflicker`／`render --deflicker`とは別の、動画出力だけの処理です。

```powershell
lrtl render C:\photos\sequence --output C:\photos\smooth.mp4 --output-deflicker-luma --output-deflicker-chroma
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --output-deflicker-chroma
lrtl render C:\photos\sequence --output C:\photos\smooth.mp4 --lrt-render-preset C:\photos\preset.json --output-deflicker-luma=false
```

本体の生成ヘルパーが指定する10枚のパワー平均を純Goで適用します。各フレームと後続9枚の平面平均を使い、フレーム数・撮影時刻は変えません。10枚未満の連番では未使用スロットがゼロのまま計算されるため、一定の明るさでも暗くなる場合があります。末尾は本体フィルターの縮小するキューと平均値スロットの更新を再現します。

保存プリセットでは、全体リサイズ後・出力クロップ前に測光・補正します。通常のCLIでは指定したアスペクト比・出力寸法へ変換した画像を使います。その後、ノイズ除去→光跡除去→速度変更→Motion Blur→Sharpen/CAS→オーバーレイ→フェードの順に処理します。中間TIFF・JPEGプレビューには適用しません。最大10枚のRGB16入力を保持し、併用する時間フィルターを含むバッファ見積もりが768 MiBを超える場合は出力前にエラーにします。

288ケース・686,556画素の平面参照、短い連番と末尾、クロップ位置、処理順、プリセット・バッチ、JPEG AVI、実H.264/H.265の18ケース、失敗時の既存出力保持をテストしています。既存RGB経路のRGB16/YUV10変換は独自近似で、本体全体の画素一致は未確認です。参照の採取範囲と再実行方法は[測定記録](internal/lrtl/testdata/lrt-output-deflicker-7.5.5.md)を参照してください。

### 飛行機の光跡除去

`render --remove-airplanes LEVEL`で、本体の3段階に対応する光跡除去を追加します。`0`が無効、`1`が弱、`2`が中、`3`が強です。保存プリセットの`removeAirplanes`／`removeAirplanesLevel`も読み込み、`--remove-airplanes 0`で解除できます。画素ごとの時間中央値を使うため、一時的に動く物体や光源にも影響します。

```powershell
lrtl render C:\photos\sequence --output C:\photos\trails.avi --remove-airplanes 1
lrtl render C:\photos\sequence --output C:\photos\trails.mp4 --remove-airplanes 3 --speed 0.5 --motion-blur 2
lrtl subtitles C:\photos\sequence --output C:\photos\trails.srt --remove-airplanes 3 --speed 0.5 --motion-blur 2
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --remove-airplanes 2
```

弱・中は3枚、強は5枚の窓から各画素の中央値を取ります。中・強では、広いGaussianぼかしとの差による輝度補正とハイライト持ち上げを行い、本体のLUTに伴う色差制限も再現します。処理順はリサイズ・ノイズ除去→光跡除去→速度変更→Motion Blur→通常シャープ・CAS→撮影日時・透かし・フェードです。AVIの処理は純Goで動きます。

本体の中央値フィルターは端の画像を複製せず、弱・中で2枚分、強で4枚分の出力が減ります。その後の先頭切り詰めにより、通常速度・Motion Blurなしでは元がN枚なら弱・中はN−3枚、強はN−5枚となります。速度を変更すると、本体のfps処理が最後の中央値画像を元の終端時刻まで複製します。たとえば8枚・Motion Blurなしなら、強は通常速度で3枚、0.5倍速では14枚になります。字幕も同じ`--remove-airplanes`・速度・Motion Blurを指定すると、この出力タイムラインに合わせられます。

保存プリセットは、元枚数を使う本体の推定時間へ音声・フェードを合わせます。12枚・強・通常速度・2fpsでは、映像は7枚／3.5秒ですが音声は5秒になり、1秒のフェードは映像終了後の4秒から始まります。プリセットなしのCLIは実際の出力時間へ合わせます。静止画の`export`／`preview-images`にはこのフィルターを適用せず、画像との対応を維持します。

生成ヘルパーの102件・323,253画素、呼び出し側の式を組み合わせた100件の時間計算、実MP4／MOV 18ケースと音声付きMP4を検証しています。既存RGB経路のRGB48/YUV10変換は独自近似で、本体の全アプリ経路との完全な一致は未確認です。合計フレームバッファーの推定が768 MiBを超える場合や、処理後の動画が空になる場合は出力作成前にエラーにします。[観測範囲と再検証方法](internal/lrtl/testdata/lrt-airplane-7.5.5.md)を参照してください。

### 保存プリセットのfps変換と字幕

保存された`frameRate`には、本体のfps文字列変換を使います。値と最も近い整数との差が0.001未満なら整数fps、それ以外は最も近い整数の`1000/1001`倍になります。整数を選ぶ境界は四捨五入です。

| 保存値 | 動画の実効fps |
|---|---|
| `24.0009` | `24` |
| `24.0011` | `24000/1001` |
| `24.5` | `25000/1001` |
| `23.90625` | `24000/1001` |

通常の`--fps 24.5`は従来どおり正確な`49/2`として扱います。保存プリセットと併用しても、明示した`--fps`を優先します。プリセットの数値ラベルは本体で確認した指数・16進表記や`f`／`D`接尾辞も読み取れます。実効fpsは1〜240、ラベルは64文字までです。`30000/1001`などの分数はlrtl拡張として扱います。本体のfps変換ヘルパーは分数を通しますが、後段の小数パーサーは分数を受け付けません。

字幕は保存プリセットからfps・速度・Motion Blur・光跡除去・補間をまとめて読み取れます。設定JSONの`lastRenderSettings`や名前による`renderPresets`選択も動画と同じです。字幕側で明示したオプションを優先します。

```powershell
lrtl render C:\photos\sequence --output C:\photos\result.mp4 --lrt-render-preset C:\photos\preset.json
lrtl subtitles C:\photos\sequence --output C:\photos\result.srt --lrt-render-preset C:\photos\preset.json
lrtl subtitles C:\photos\sequence --output C:\photos\result.srt --lrt-render-preset C:\photos\settings.json --lrt-preset-name "My Preset"
```

本体の保存プリセットでは、音声・フェードの推定長には変換前の小数fpsを使います。たとえば15枚・保存値`3.5`なら、動画は`4000/1001` fpsで約3.754秒、音声は約4.286秒です。156件のfpsヘルパー出力、282件の補間グラフ、5件の本体音声/フェードグラフと比較し、実MP4の映像・音声長とフェードも確認しています。異常な設定や未知のプリセット名で既存SRTを置き換えないこともテストしています。[比較範囲](internal/lrtl/testdata/lrt-rate-label-7.5.5.md)を参照してください。

## 動画と字幕のフレームレート

`render`と`subtitles`の`--fps`は、整数・小数・分数で1〜240 fpsを指定できます。23.976／29.97／47.952／59.94という小数表記は、LRT 7.5.5の9ケースの観測に合わせ、それぞれ24000/1001、30000/1001、48000/1001、60000/1001として扱います。末尾のゼロがある`29.970`なども同じです。分数を明示した場合はその値をそのまま使うため、正確な小数29.97が必要なら`2997/100`を指定できます。`24.5`など他の小数も指定可能です。約分後の分子・分母は32-bitの正整数に収まる必要があります。

```powershell
# 29.97 NTSCのMJPEG AVI。追加ソフト不要
lrtl render C:\photos\sequence --output C:\photos\ntsc.avi --fps 29.97
# 動画と同じフレームレートのSRT字幕
lrtl subtitles C:\photos\sequence --output C:\photos\ntsc.srt --fps 30000/1001
# 音声付きMP4と、複数連番の一括出力（FFmpegが必要）
lrtl render C:\photos\sequence --output C:\photos\ntsc.mp4 --fps 29.97 --audio C:\photos\music.wav
lrtl batch-render C:\photos\sequences --output C:\photos\videos -- --fps 24000/1001
```

AVIはストリームの分子・分母へ、FFmpegには分数のまま渡します。音声の終了時刻と字幕も同じ比率を使い、字幕は絶対フレーム番号から整数演算でミリ秒を求めるため、1フレームごとの丸め誤差を蓄積しません。これはNTSCのdrop-frameタイムコード表示ではなく、通常のSRT再生時刻です。GUIのfps欄は従来の整数指定のままです。

AVIヘッダー・JPEG画素データ・インデックス保持、バッチ転送、長時間字幕の時刻、不正fpsでの既存出力保持をテストしています。FFmpeg／ffprobeがある環境では、実際のAVI・H.264 MP4・ProRes MOVのフレーム数・fps・再生時間と、短い音声を動画長まで延長する処理も検査します。本体の動画エクスポート全体との一致を示すものではありません。

## 今後の実装順

| 段階 | 目標 | 状況 |
|---|---|---|
| 1 | XMPサイドカー、キーフレーム、数値設定の補間 | XMPの読み書き、4/5つ星キーフレーム操作、LRT 7.5.5由来の数量/配置規則、127個の全体数値項目と登録ローカル項目のスプライン補間、スナップショット、GUIのExposure2012グラフ、選択フレームのマスター/R/G/Bトーンカーブ点編集を実装。Lightroom/Camera Raw連携画面と連番全体でカーブを同時編集する機能は未実装 |
| 2 | 画像連番から基本タイムラプス動画を生成 | **達成（現像済み画像・対応RAW）**。JPEG/PNG/TIFFからPure GoのMJPEG AVIを出力可能。FFmpegがあればH.264 MP4／H.265 MP4・MOV、ProRes/DNxHR MOVも出力でき、バッチレンダー、縮小、クロップ、日時表示、音声、Deflicker、モーションブラーに対応。RAWはAdobe DNG ConverterとGoによる実画素現像、内蔵プレビュー、明示したDNGLabから選択可能 |
| 3 | 画像解析、プレビュー、Deflicker | 静的SVG輝度プレビュー、UIのXMP Exposure2012/解析画像輝度グラフ、選択フレームのRGBトーンカーブ編集、静止画プレビューと最大640 pxの連番ループ再生、長期撮影指標、XMP画素反映、回転クロップ、参照領域/全体露出Deflickerを実装。LRT Visual Previewと同等の再生速度/画素再現、Exposure2012の複数フレーム同時編集、フレーム間のトーンカーブ編集、XMPロスレス適用、手動Vertical/Horizontal/Aspectは独自投影モデルで近似実装。Upright自動/Guided、Adobeの保存済み変換、未対応マスク形式の画素現像は未実装 |
| 4 | RAW現像、Holy Grail、XMP現像値の画素への反映 | EXIF推定と既存LRT XMP入力を使う近似Holy Grail補正、現像済みラスターへの対応XMP値適用、一部RAW内蔵プレビュー、Adobe DNG ConverterとGoによるRAW現像（DNGLabも任意選択）を実装。Camera Raw現像の完全再現と同等のRAW画素処理は未実装 |
| 5 | 残りのCLIワークフローと取り込み機能 | Go標準`net/http`で動くループバック限定のローカルWeb UIで、サムネイル、キーフレーム、XMP補間、レンダー操作を実装。動画からのフレーム抽出、画像/XMP取り込み、lossless/uncompressed/lossy DNG変換・ロッシーDNG縮小、追加コーデックも実装。GUIの追加開発は対象外。LRTと同じ10-bit YUV画素処理・色管理は未対応（現状は16-bit RGBを経由） |

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

Converterを`-u -l -p0 -dng1.4`で呼び、一時フォルダーにデモザイク済み・未圧縮Linear DNGを生成します。その実画素から黒/白レベル、AsShotホワイトバランス、CameraCalibration/AnalogBalance、ForwardMatrixまたはColorMatrix、BaselineExposureとプロファイルのBaselineExposureOffsetを使ってGoで16-bit sRGBを生成します。ストリップ/タイル、DefaultCrop/ActiveArea、DefaultScaleに対応します。`--raw-preview`は従来どおりカメラ内蔵プレビューを使う別の入力モードです。元RAWと隣接XMPは現像処理で変更しません。

この現像はAdobeのデモザイクと独自の色変換を組み合わせたものです。デュアル光源の色温度推定、色域外のクリップ、画素端のクロップ丸めは近似です。DNG `OpcodeList2/3`のWarpRectilinear/WarpRectilinear2/WarpFisheye/TrimBounds/FixVignetteRadial/MapTable/MapPolynomial/GainMap/DeltaPerRow/DeltaPerColumn/ScalePerRow/ScalePerColumnと、埋め込みプロファイルのHue/Sat・Lookテーブル・ProfileToneCurve・RGBTablesを適用します。その他のOpcodeやCamera Rawの全現像工程は再現しません。Semantic Maskに対するLossless JPEG復号と幾何Warpの反映は未対応で、該当するRGBTables処理はエラーにします。未対応の必須Opcodeはエラーにし、オプション指定された未対応Opcodeだけスキップします。追加XMPは`--apply-xmp-basic`/`--apply-xmp-crop`を指定した場合に既存のlrtl近似処理で適用します。対応カメラはインストール済みConverterに依存し、変換失敗時は内蔵JPEGへ自動フォールバックせずエラーを返します。Converterの出力と1枚分の復号画像にはそれぞれ1 GiBの上限を設け、Converter実行は5分でタイムアウトします。

自動テストはAdobeを必要としない既知画素のLinear DNGと代替プロセスを使い、黒/白レベル、ガンマ、WB/色行列、タイル境界、異常データ、CLI/GUIの各経路と一時ファイルの後始末を確認します。実機ではAdobe DNG Converter 18.7でNikon D3S NEFとCanon 40D sRAW CR2を現像し、NEF連番のAVI、CR2のH.264 MP4、CR2→DNG取り込み→AVIを確認しました。完全なCamera Raw画素一致の検証ではありません。仕様は[AdobeのCLI資料](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section/download-1/dng_converter_commandline.pdf)と[DNG仕様](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section_733958301/download-1/DNG_Spec_1_7_1_0.pdf)を参照しました。

追加の実機確認として、依存パッケージ`imagemeta v0.3.1`のテスト素材`Hero8.GPR`もAdobe DNG Converterで4000×3000の実画素から現像し、GUIプレビューを生成できることを確認しました。通常の自動テストはこれらの実RAWをダウンロードせず、合成データのみで動作します。

## Preview EVの生成

`preview-ev DIRECTORY`は連番画像から各フレームのPreview EVを測定し、`lrt:ExternalExposureDefault`への保存値も表示します。実LRT 7.5.5で観測した計算に合わせ、RGB各成分の256ビンを[0,255)に配置し、値255を除外します。半径2の重み付き平滑化を上端除外の窓で行い、頻度を丸め、元の成分別総数へ再正規化して再度丸めます。ビン幅は255/256で、各成分の平均強度を個別に求めてRGBの平均を取ります。成分のヒストグラムが空になる場合は、本体と同じ255/256を返します。例えば20×20pxのRGB(128,128,128)は0.4990196228027344です。

Preview EVは平均値を256で割った値で、XMP保存値はその4倍です。既定では表示のみで、`--write`を付けるとXMPサイドカーを更新します。既存ファイルはバックアップされます。`--raw-preview`、`--raw-develop`、`--lrt-preview camera|visual`、`--apply-xmp-basic`、`--apply-xmp-crop`も指定できます。`--lrt-reference`で保存済み参照領域を読み、日時順の領域を元フレームへ対応付けてから測光できます。画像処理を指定した場合は、その処理後の画像上の領域を使います。参照設定が壊れている場合や領域に画素がない場合は全XMPの保存を中止します。

この計算は従来の一様移動平均から修正されています。黒・白・RGB端値・小画像・様々な領域を含む本体59ケースに加え、日時順の領域・XMP保存・バックアップ・破損時の保持をテストしています。同じ復号画素と領域での比較であり、RAW現像・JPEG復号・色管理全体の画素一致を示すものではありません。

```powershell
lrtl preview-ev "D:\Timelapse\Developed"
lrtl preview-ev "D:\Timelapse\Developed" --write
# LRT settingsでカメラプレビュー幅を変更している場合
lrtl preview-ev "D:\Timelapse\Developed" --preview-width-camera 1280
```

Preview EVのヒストグラム、既定の全画面ROI、および幅1024pxを超える画像を1024×682へ縮小する規則は、LRT 7.5.5の合成画像で照合済みです。縮小後の測定値は3条件で本体から1e-5以内、6条件の拡大・縮小で個別画素82点の全246色成分が本体と完全一致します。LRTの`previewWidthCamera`を変えている場合は`--preview-width-camera N`で同じ幅を指定できます（LRT設定ファイルは自動読込しません）。LRTと同じRAW/画像デコードや色管理は未再現のため、同じ画像ファイルでも入力画素が異なる場合があります。`--lrt-reference`では保存済みROIを全画面の代わりに使います。XMP値がない連番では、先に`preview-ev --write`を実行すると、`keyframes`の既定数量計算で使える値を生成できます。

## 連番の初期化

既定の`--frame-state lrt-7.5.5`は、JPEG/DNGで画像とサイドカーの更新時刻をミリ秒単位で比べ、サイドカーが画像より新しい場合だけ採用します。同時刻・古いサイドカーでは画像内XMPを使い、画像内にXMPがなければ空から初期化します。TIFF/PNGのサイドカー優先は維持します。プレビューに採用元を表示し、保存後のXMPは未来日時の画像よりも新しくして再読込を安定させます。[76条件の実機比較と変更範囲](internal/lrtl/testdata/lrt-xmp-source-7.5.5.md)を参照してください。採用したXMPの最初の4つ星（なければ先頭）の画像を参照して連番の`lrt:Quality`を再分類します。JPEG/PNG/TIFFはFLAT、その他のRAWはRAW、DNGはAsShotNeutral（TIFFタグ50728）の有無で分けます。Custom WBに不足する値を補い、登録861列（数値755列）の保存、非使用WB値の解除、HasCropの再計算、内部補正露出・評価・旧参照タグの保存整理も行います。画像から取得できる寸法は`lrt:Width/Height`へ、露出・向きなどは対応タグへ保存し、画像側にないカメラ値は既存のLRT値を引き継ぎます。PNGの向き情報も読みます。RW2/RWL/Panasonic RAWはセンサー境界とExifIFD/JPEGの実機選択に合わせて寸法を求めます。TIFF系RAWの寸法はExifToolの優先IFD・CR2の例外に合わせ、16ビットへ切り詰めずに読みます。[実機80入力の比較](internal/lrtl/testdata/lrt-raw-dimensions-7.5.5.md)を参照してください。日時は保持し、RAW寸法の一部や撮影モード/HDRの画像判定は未対応です。[実機75条件と画像読込の制限](internal/lrtl/testdata/lrt-frame-metadata-save-7.5.5.md)を参照してください。既存のQualityはこの初期化で更新します。`--frame-state preserve`では従来のマスク・ヘッダー初期化範囲を使えます。[実機426条件・合成DNG24例・制限](internal/lrtl/testdata/lrt-frame-state-7.5.5.md)を参照してください。

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

Camera RawのVersion/ProcessVersion/HasSettingsがない場合に限り、観測した互換初期値14/11/Trueを補います。WB保存準備では非使用の数値を解除します。全スライダーのリセット、キーフレームの自動作成、LRTのシーケンス状態ファイルの生成は未対応です。製品テンプレートは同梱せず、配列構造をGoで生成します。LRT/Camera Rawでの読み込み往復や全初期化機能の同等性は未検証です。

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

## 補間時のマスク形式

`interpolate`と`batch-interpolate`は`--mask-format auto|legacy|modern`に対応します。円形マスクの角度を±90度ずらす後処理の対象を、legacyではCircular 1/2、modernではMaskGroup 8/9へ限定します。両形式が混在するXMPで、非対象の角度まで後処理してしまう問題を修正しました。数値の補間・コピー自体は既存の登録項目を処理します。

既定の`auto`は、本体で観測した先頭画像の判定に合わせ、XMPにMaskGroupがあればmodern、なければGradientがあればlegacyを選びます。空の配列でも存在すれば判定に使い、両方あればmodernを優先します。後続画像は形式の判定に使いません。CircularやPaintだけでは自動判別できず、lrtlではmodernを既定とします。本体のグローバル設定は読み書きしないため、本体でlegacyを既定にしている場合や形式を明示したい場合は、以下のように指定できます。

```powershell
lrtl interpolate C:\photos\sequence --mask-format legacy
lrtl interpolate C:\photos\sequence --mask-format legacy --write
lrtl batch-interpolate C:\photos\sequences --mask-format auto --workers 4 --write
```

採用した形式をプレビューに表示します。バッチの`auto`は連番ごとに判定します。形式判定6ケースと角度後処理24ケースを実LRT 7.5.5で観測し、混在XMPの全6列を3種類のRDF表現で432件比較しています。通常／バッチの保存、バックアップ、元画像保持、無変更の再実行、不正オプション時に保存しないこともテストしています。先頭画像の並びは既存のlrtlの自然順、XMP読込は既存の外部／埋め込み優先順位を使います。本体のフォルダー読込順・RAW現像・縦横比を使う円形形状補正全体を再現したものではありません。

## 補間時の全体スカラー

全体数値には、補間ワーカーへ入らず参照値を全体へコピーする経路があります。選択した4つ星が1つ以下の場合、または選択キーフレームと両端の数値がすべて同じ場合が対象です。Crop項目は5つ星があればそちらを使います。この経路では数値のクランプ・整数丸め・float32変換を行わず、参照値のdouble精度を保持します。通常範囲を超える既存値もそのままコピーされます。値が異なるアンカーの補間では従来どおり登録範囲を適用します。実LRTの判定・コピーコンポーネント14ケースと比較し、CLIの保存・バックアップ・再実行・3フレームAVIの画素もテストしています。登録された配列位置のローカル補正・マスク座標にも同じコピー経路を適用します。Sync ID形式、登録外の位置、2番目以降のセレクターはこのコピー経路の対象外です。ローカル露出と座標の判定・コピーを実LRTの28ケースと比較しています。

登録済みの全体数値は、4/5つ星のアンカーを選ぶ前に欠損を補完します。途中と末尾の欠損は直前の有効値、先頭の欠損は最初の有効値を使い、ゼロは有効値として扱います。非キーフレームの値も補完元になり、欠けたキーフレーム項目にも保存されます。全フレームで欠けている項目は作りません。WB数値4項目はこの事前補完をしません。最初の4つ星（なければ先頭画像）に当該WB数値がなければ、他フレームの同じ項目を削除します。Custom指定でも数値は作らず、ゼロ値は欠損扱いしません。この削除はキーフレームも対象です。参照に数値があり4つ星を複数選んだ場合は、列ワーカー段階で欠損補完してから数値アンカーを選びます。この補完元には非キーフレームも含まれます。4つ星が1つ以下の場合は参照値を全体へコピーします。

WB数値の列コンポーネントを実LRT 7.5.5で呼び出した16ケースを固定データとして比較しています。GUI設定を参照しない固定テンションの処理経路を使い、Goの`--smoothness 100`に対応するアンカーと結果を比較します。0/50/100では欠けたキーフレームを手動補完した対照列とも比較し、CLIでの保存・バックアップ・再実行と実際の5フレームAVI生成を検査しています。LRT全体の自動処理やCamera Raw現像画素との完全一致を示すものではありません。

局所補正は、登録されたRDF配列位置で既存の補正・`CorrectionMasks[1]`内の欠けた数値を同じ方法で補完します。局所露出、トーン・色の数値、マスク座標などが対象で、IDは補完しません。CorrectionActiveと登録されたFlippedは後述の真偽値処理で扱います。全体WBの除外ルールはLocalTemperature等へは適用しません。登録された親補正の数値とCorrectionActiveは、要素がない選択キーフレームにも補完します。親補正が既存または同時に補完できる場合、Linear／Circular／MaskGroupの欠けた`CorrectionMasks[1]`や空の配列・先頭要素にも登録形状値とFlippedを補完します。他フレームのID・What・未知の形状はコピーしません。親を作れる値が全くない場合、Sync ID独自形式、未登録位置、Paintの形状、2番目以降のセレクターへの事前補完は未対応です。非キーフレームに欠けた登録構造を近傍から複製する既存処理は別経路です。補正直下の登録`CorrectionRangeMask`は、子要素や親補正がなくてもType/ColorAmount/LumMin/LumMax/LumFeatherを補完し、LumRangeは既存の最初の非空値をコピーして作成できます。PointModelsなどの既存色域サンプルは保持し、参照元からはコピーしません。セレクター直下の欠けた範囲要素は新設しません。不正な登録数値は書き込み前にエラーにします。

`HasSettings`、`HasCrop`、`UprightPreview`、`ConvertToGrayscale`と、WB名・CameraProfile/Digest・ToneCurveName2012・CompatibleVersion・LookName・LensProfile名/ファイル/Digest/Setup・Uprightの文字列を処理します。数値スプラインとは別の経路で、4つ星が1つ以下、端点を含むアンカー値が同じ、端点2点だけ、3点以上で値が異なる場合を区別します。真偽値を数値として補間することはありません。欠損補完は前方への引き継ぎ後に先頭の欠損を後方から埋めます。文字列ワーカーでは空文字を飛ばしますが、空白だけの値は有効値です。

全体スカラーの参照は最初の4つ星（なければ先頭画像）です。参照に当該値がない場合は、他フレームの同じプロパティを削除する経路があります。4つ星に`WhiteBalance=Custom`があれば全体をCustomへ揃え、参照がAs ShotならTemperature/Tint/IncrementalTemperature/IncrementalTintを解除します。通常の数値キーフレーム保持とは異なり、スカラー処理はキーフレームにも変更を加えます。まず`interpolate DIRECTORY`で変更と`(remove)`表示を確認し、`--write`でバックアップ付き保存します。

実LRT 7.5.5の欠損補完・文字列列ワーカーを呼び出した12ケースを保存し、通常テストではGoだけで比較します。全体スカラー22+4項目の処理であり、crd名前空間、LRTの全XMP保存結果や現像画素との完全一致は対象に含めていません。

### ローカル補正の有効・無効と反転

`interpolate`は、登録された16個の補正位置の`CorrectionActive`と、Circular 1–2 / MaskGroup 1–4・8–11の`CorrectionMasks[1].Flipped`の計26項目を扱います。欠損は前方へ引き継いで先頭を後方から補完し、4つ星が1つ以下なら参照値を全体へコピーします。複数の4つ星と両端の値が同じ場合もコピーします。値が異なる場合は既存の真偽値を保持し、数値補間はしません。5つ星はこの処理のアンカーになりません。

全フレームで欠けたフラグは作りません。Sync ID形式、内部予約位置、2番目以降のマスク、線形マスクの`Flipped`、`MaskInverted`はこの遷移処理の対象外です。既存IDを保持し、真偽値だけをコピーするために選択キーフレームの欠損構造は生成しません。不正・構造化された登録真偽値は保存前にエラーにします。実LRTの登録26項目とヘルパー処理24ケースを固定データで照合し、CLIの保存・バックアップ・再実行・有効化/反転がAVI画素に及ぼす結果をテストしています。LRT全体のXMP保存やCamera Raw画素との完全一致は未検証です。

## 16-bit TIFF中間画像を書き出す

```powershell
# JPEG/PNG/TIFFの連番をXMP現像・クロップ付きで書き出す
lrtl export C:\photos\sequence --output C:\photos\tiff-export --apply-xmp-basic --apply-xmp-crop

# RAWを現像して、長辺3840画素のTIFF連番へ
lrtl export C:\photos\raw --output C:\photos\raw-tiff --raw-develop --apply-xmp-basic --long-edge 3840

# 書き出した画素には現像が反映済みなので、そのまま動画にする
lrtl render C:\photos\tiff-export --output C:\photos\timelapse.avi
```

`export`は動画と同じ画像処理を使って、`frame000001.tif`から始まる非圧縮RGB 16-bit TIFF連番を作ります。`frames.json`に順序と元ファイル名を記録します。既定でEXIFの向きを画素へ適用し、出力Orientationは1です。PNGの`eXIf`向き情報も両バイト順で読みます。`--auto-orient=false`で向きの適用を止められます。

`--apply-xmp-basic`（または`--apply-xmp-exposure`）、`--apply-xmp-crop`、`--aspect`、`--long-edge`/`--output-height`、`--selection`、`--filename-contains`を指定できます。`--deflicker`とその平滑化・参照領域設定、`--motion-blur`、透かし・撮影時刻のオーバーレイも`render`と同じです。ラスター入力の処理はPure GoでFFmpeg不要です。RAWは`--raw-develop`とインストール済みAdobe DNG Converter（または`--dnglab PATH`）、内蔵プレビューは`--raw-preview`を使います。

出力先は新規フォルダーに限定し、既存のファイルやフォルダーは置き換えません。同じ親の一時フォルダーへ全画像と対応表を作ってから公開するため、途中の不正画像・異なる解像度・現像失敗では出力先に不完全な連番を残しません。元画像・XMPは変更しません。

入力は既存レンダーと同じくsRGBとして処理し、16-bit入力の精度を保持します。8-bit入力の細部が増えるわけではありません。透明度は黒背景へ合成し、出力にアルファチャンネルはありません。撮影EXIF・XMPサイドカー・ICCプロファイルのコピーは行わず、指定した現像値は画素へ反映します。TIFFへ書き出した後に同じXMP露出を再適用すると補正が重複します。Camera Raw現像・色管理・HDRとの一致は未対応です。

テストではTIFFの圧縮・bit深度・向きタグを直接確認し、16-bit下位ビットと透明度の計算、XMP・Deflicker・Motion Blur・オーバーレイ適用後の動画側RGB48入力との一致を検査します。通常のテストは外部アプリ不要で、RAWコンバーターのコマンド契約はテスト用子プロセスで確認します。別途、実NEF/CR2を使ったインストール済みAdobe DNG Converter→TIFF→AVI生成も確認しています（再実行は`LRTL_RAW_SAMPLE`を指定して`go test ./internal/lrtl -run '^TestExportTIFFRealRAW$' -count=1`）。

## 複数連番のXMPを一括補間

```powershell
# ROOT自身と直下の連番を検証して変更予定を表示
lrtl batch-interpolate C:\photos\sequences

# 全階層を検索し、4連番ずつ解析。全体の内容検証後に保存
lrtl batch-interpolate C:\photos\sequences --recursive --workers 4 --smoothness 50 --write

# 補間したXMP露出を使って一括動画生成
lrtl batch-render C:\photos\sequences --recursive --output C:\photos\movies --format avi -- --apply-xmp-exposure
```

`batch-interpolate`は各フォルダーを独立した連番として、通常の`interpolate`と同じ4/5つ星のアンカー、数値補間、スカラー処理、構造均等化、局所補正を適用します。画像のないフォルダーは処理しません。`--workers`は解析の並列数（既定1、1〜64）、`--smoothness`は0〜100（既定50）です。解析結果と保存順はフォルダー順です。

既定では書き込みません。`--write`でも全連番の読込・補間・XMP構築を先に検証し、1つでも内容エラーがあれば全体を保存せず終了コード1を返します。保存する既存XMPはバックアップされ、新規サイドカーや変更のないファイルにはバックアップを作りません。`--no-backup`も指定できます。保存開始後のディスク・権限等のエラーでは、それまでに保存したファイルは残り、全体のロールバックはしません。

このコマンドはLRTのバッチ作業に対応するCLI機能です。LRTのバッチキューそのものを再現した比較ではなく、数値処理は既存の実測比較済み補間経路を再利用しています。Deflickerの一括計算は次節の`batch-deflicker`に対応します。Holy Grailの一括計算は`batch-holy-grail`に対応します。JPEGプレビューの一括生成は`batch-preview-images`に対応します。

### 複数連番のDeflicker

```powershell
# 解析結果を確認（書き込まない）
lrtl batch-deflicker C:\photos\sequences --recursive --workers 2 --constant --passes 3 --apply-xmp-basic

# 全連番の内容検証後、既存XMPをバックアップして最終補正を保存
lrtl batch-deflicker C:\photos\sequences --recursive --workers 2 --constant --passes 3 --apply-xmp-basic --write

# RAWを現像して測定。参照領域・LOWESSを共通指定
lrtl batch-deflicker C:\photos\sequences --raw-develop --smoother lowess --smooth 5 --reference 20,20,600,400 --write

# lrtlが記録した補正を一括解除（画像の復号・現像は不要）
lrtl batch-deflicker C:\photos\sequences --recursive --reset --write
```

探索範囲は`batch-interpolate`と同じくROOT自身と直下です。`--recursive`は全階層、`--workers`は同時解析する連番数（既定1、1〜64）です。結果と保存順はフォルダー順に固定します。通常の`deflicker`と同じGaussian/LOWESS・参照領域・定輝度・最大8パスの画素フィードバック、RAWプレビュー/現像、XMP露出/基本現像・クロップを使い、各連番の最終補正だけ保存します。専用LocalExposureスロットの識別、CorrectionAmount、既存補正の追跡・リセットも個別コマンドと共通です。

既定は解析結果の表示だけで、`--write`で保存、`--no-backup`でバックアップを省略します。`--reset`は解析オプションと併用できません。どれかの連番の画像・XMP・補正値・最終パケットが不正なら、全連番のXMPとバックアップを変更せず終了コード1を返します。保存中のOSエラーは保存済み枚数を報告して中断します。複数ファイルを一括でロールバックするトランザクションではありません。

Gaussian/LOWESS、並列数1/4、XMP基本現像・専用補正スロット・RAW現像で個別実行との一致を検査し、補正後AVIも比較します。破損画像/XMP・不正な補正記録・参照領域による全体保存の中止、バックアップ、リセットのプレビューと画像復号の省略もテストしています。LRTのバッチキューやCamera Rawとの画素一致を再現した検証ではありません。測光と補正の一致範囲は通常のDeflickerと同じです。

### 複数連番のHoly Grail補正

```powershell
# EXIFから補正予定を確認。既定はROOT自身と直下を探索する
lrtl batch-holy-grail C:\photos\sequences

# 既存のLRT入力を使い、各連番の曲線形状を自動計算して保存する
lrtl batch-holy-grail C:\photos\sequences --recursive --workers 2 --lrt-input --auto-shape --lrt-reduce-deflicker 0.35 --write

# 各連番の保存済み順序・Holy Grail設定を使って保存する
lrtl batch-holy-grail C:\photos\sequences --recursive --workers 2 --lrt-holy-grail-settings --write

# 既存のVisual Previewキャッシュから各連番の補正入力を測定
lrtl batch-holy-grail C:\photos\sequences --recursive --workers 2 --lrt-preview visual --write

# EXIFから露出補正だけを保存し、2*/3*評価を付けない
lrtl batch-holy-grail C:\photos\sequences --recursive --no-mark-jumps --write

# 保存済みのlrtl補正と管理対象のジャンプ評価を解除する
lrtl batch-holy-grail C:\photos\sequences --recursive --reset --write
```

`holy-grail`と同じオプションと検証・計算・保存経路を使います。`--recursive`で全階層、`--workers`で同時解析数（既定1、1〜64）を指定し、結果表示と保存はフォルダー順です。EXIFモード、`--ignore-aperture`、ジャンプ評価の手動指定/無効化、`--lrt-input`、`--lrt-preview camera|visual`と任意の`--lrt-reference`、Stretch/Rotate、`--auto-shape`、`--lrt-reduce-deflicker`に対応します。`--lrt-holy-grail-settings`では連番ごとの`.lrt/lrtsequence.json`から順序と曲線設定を個別に読み込みます。設定の欠損や不正値があれば全連番の保存を始めません。自動形状は連番ごとに独立して計算します。LRT入力の生成条件・数値一致範囲は個別コマンドの説明と同じです。

既定は変更予定の表示で、`--write`でバックアップ付き保存、`--no-backup`でバックアップを省略します。全連番の撮影値・XMP・補正記録・ジャンプ評価・最終パケットを検証し、どれかが不正なら全体を書き込まず終了コード1を返します。ジャンプ評価は既存の明示的な評価を上書きしません。衝突時の扱いも個別コマンドと同じです。保存中のOSエラーは保存済み枚数を報告して中断します。複数ファイルの一括ロールバックには対応しません。

個別実行とのXMP・AVIフレーム/索引の一致、並列数1/4、EXIF/LRT入力の自動形状、専用補正スロット、LRT 7.5.5で観測した補正値、バックアップ、再実行での重複防止、評価の作成・解除をテストしています。破損した撮影値・XMP・LRT入力・補正記録や評価衝突による全体保存の中止、画像EXIFを読めない場合の補正解除も検査します。LRTのバッチキュー自体の再現やCamera Rawとの画素一致を示すものではありません。

## Preview EVの黒白除外測光

```powershell
# 黒側10%、白側25%の範囲をソフトに弱めて測光
lrtl preview-ev C:\photos\sequence --ignore-blacks 10 --ignore-whites 25

# 保存済み参照領域とVisual Previewを使い、ハードカットした値をXMPへ保存
lrtl preview-ev C:\photos\sequence --lrt-preview visual --lrt-reference --ignore-blacks 10 --ignore-whites 25 --hard-cut --write
```

数値は0〜100の整数で、256ビン上の輝度位置の割合を表します。既定のソフトモードは黒端・白端へ向かう直線の重みを正規化し、成分ごとの頻度を合わせたヒストグラムの重み付き平均を求めます。`--hard-cut`では黒側・白側のビン数を本体同様に四捨五入して除外し、残ったRGB各成分の平均を求めます。カット範囲の重複も本体と同じ式で扱い、ハードカットで有効範囲がなくなった場合は0を返します。

これらのフラグを一つでも明示すると除外測光モードになります。`--ignore-blacks 0`、`--ignore-whites 0`、`--hard-cut=false`も明示指定として扱います。通常測光と除外測光は本体の計算経路が異なり、ゼロ指定でも結果が異なる場合があります。特に除外モードの全白画像・空ヒストグラムは0になります。フラグを省略した場合は通常のPreview EV計算です。

`preview-ev`での保存値は従来どおりPreview EVの4倍を元XMPの`lrt:ExternalExposureDefault`へ記録し、`--write`の指定時だけ保存します。RAW現像・既存LRTキャッシュ・参照領域・XMP反映を組み合わせられます。`deflicker`と`batch-deflicker`では`--calculation lrt-7.5.5`を選んだ場合に同じ除外測光フラグを使えます。LRT Visual Deflicker全体との一致は引き続き未検証です。

## LRT方式のDeflicker計算

### 保存済みDeflicker設定

`deflicker`と`batch-deflicker`の`--lrt-deflicker-settings`は、各連番の`.lrt/lrtsequence.json`から平滑化係数・精度・Constant・複数パスの有効状態・パス数・並び順を読み、本体方式の計算を選びます。`--lrt-preferences FILE`を追加すると、LRTの`LRTimelapse.config`から全体の黒白除外・閾値・Hard Cut設定と、平滑化係数・精度・Constant・複数パスの既定値を読み込みます。

```powershell
lrtl deflicker C:\photos\sequence --lrt-deflicker-settings --apply-xmp-basic
lrtl batch-deflicker C:\photos\sequences --lrt-deflicker-settings --lrt-reference --lrt-preview visual --workers 2 --write
lrtl batch-deflicker C:\photos\sequences --lrt-deflicker-settings --lrt-preferences "$env:APPDATA\LRTimelapse\LRTimelapse.config" --write
```

`--lrt-reference`を追加すると保存済み参照領域も使います。設定ファイルやキャッシュは変更しません。明示した`--passes`・`--constant`・`--lrt-smoothing`・`--lrt-accuracy`・`--smooth`・`--smoother`とは併用できません。保存値があればそれを使い、欠けた平滑化係数・精度・Constant・複数パス状態・有効時のパス数は`--lrt-preferences`から補います。`filenameSort`がなければLRTの初期値`false`に従って撮影日時順にします。必要な設定がシーケンスと全体設定の両方にない場合や画像枚数が一致しない場合は、保存前にエラーにします。複数パスが無効なら1パス、有効なら保存された1〜8パスを使います。

黒白除外の`deflickerIgnoreBlacksWhites=1`は、`--lrt-preferences FILE`でLRTの`LRTimelapse.config`から黒白割合・ソフト/ハードカットを読み込むか、`--ignore-blacks`／`--ignore-whites`等を明示して使います。設定値はCLIフラグで個別に上書きできます。値0では全体設定を使い、値2は黒白除外を無効にして全体設定を上書きします（明示した除外フラグとの併用はエラー）。全体設定だけを使う場合も`--calculation lrt-7.5.5`が必要です。本体のJSONシリアライズ6ケース、保存値欠落時の全体設定フォールバックと日時順、個別指定とのXMP一致、連番ごとに異なる設定の並列一括処理、参照領域、異常時のファイル保持をテストしています。Camera Rawや本体の全反復処理との画素一致を示すものではありません。

### 計算値の個別指定

```powershell
# 対応XMP現像を反映しながら3パス計算し、補正案を確認
lrtl deflicker C:\photos\sequence --calculation lrt-7.5.5 --preview-width-camera 1024 --lrt-smoothing 0.5 --lrt-accuracy 1 --passes 3 --apply-xmp-basic

# LRTが生成済みのVisual Previewと参照領域から単一パスを計算・保存
lrtl deflicker C:\photos\sequence --calculation lrt-7.5.5 --lrt-preview visual --lrt-reference --write

# 全連番を同じ計算法で処理。全内容検証後にバックアップ付き保存
lrtl batch-deflicker C:\photos --calculation lrt-7.5.5 --passes 3 --workers 2 --apply-xmp-basic --write
```

既定の`--calculation lrtl`は従来の線形sRGB中央値・Gaussian/LOWESS・70%応答の補正です。`lrt-7.5.5`は通常または黒白除外ヒストグラムからPreview EVを測り、8枚未満は両端の直線、8枚以上はLRT型LOWESS、`--constant`は算術平均をターゲットにします。測光差を対数関数へ渡し、精度別しきい値と既存補正への加算・-1〜1の制限を適用します。全測光差が厳密にゼロの場合に補正をゼロへ戻す分岐も本体どおりです。

`--lrt-smoothing`は0〜2の帯域係数（既定0.5）、`--lrt-accuracy`は-1:低／0:通常／1:高（既定0）です。高精度ではパス番号に応じた減衰を使います。`--ignore-blacks`、`--ignore-whites`、`--hard-cut`で除外測光を指定できます。`--preview-width-camera N`はLRTカメラプレビューの縮小幅を指定し（既定1024）、明示した`--reference`領域も縮小後の画素へ写像します。`--smooth`・`--smoother`は`lrtl`計算専用で、LRT方式との同時指定はエラーです。Native用フラグは`--calculation lrt-7.5.5`を指定した場合に使えます。最小2枚が必要です。

識別できるDeflicker内部スロットがあれば、その既存値へ本体の計算結果を反映します。スロットがない場合は、lrtlが管理する補正分に計算を適用し、ユーザーの`crs:Exposure2012`を基準値として保持します。既存のlrtl補正が別の位置にある場合も移行し、CorrectionAmountを考慮して保存するスロット値を調整します。必要な変更があるのに対象が無効・Amount=0の場合は保存しません。`--reset`はlrtlが記録した変更を解除し、処理開始前の露出や内部補正の基準値を保持します。

複数パスでは、候補XMPをメモリー上で更新し、Go側の対応現像で再測光して最後の値だけを保存します。XMP反映を指定しない入力は既に現像済みの表示画像として扱い、開始時からの追加補正だけを画素へ反映して既存補正の重複を避けます。LRTキャッシュには開始時の現像状態が反映されている必要があります。キャッシュの鮮度判定・Camera Rawでの再生成は行いません。RAW/XMP現像全体・疎なフレーム選択・本体の保存丸めまでの一致は未検証です。単一パスの数値コア、保存先への対応付け、Go内での再測光・保存のテストを区別しています。

## LRTの既存プレビューキャッシュを使う

```powershell
# LRTで生成済みのVisual Previewを使って動画化
lrtl render C:\photos\sequence --output C:\photos\visual.avi --lrt-preview visual

# 通常プレビューのPreview EVを測り、元の連番のXMPへ保存
lrtl preview-ev C:\photos\sequence --lrt-preview camera --write

# Visual Previewの画像からDeflicker補正を計算（--writeで保存）
lrtl deflicker C:\photos\sequence --lrt-preview visual --smoother lowess

# Visual PreviewをTIFF連番に展開
lrtl export C:\photos\sequence --output C:\photos\visual-tiff --lrt-preview visual
```

`camera`は`画像フォルダー/.lrt/previews/拡張子を除いた名前.lrtpreview`、`visual`は`.lrt/visual/拡張子を除いた名前.lrtpreview`を使います。例えば`frame.001.NEF`は`frame.001.lrtpreview`になります。これはインストール済みLRT 7.5.5のパス生成メソッドを呼び出して確認した配置です。同版のプレビューエンコーダーが生成したファイルは通常のJPEGであり、本プロジェクトが用意したグラデーションを同エンコーダーで保存したテスト画像で読込を検査しています。

元画像の連番順・選択・XMPとの対応を維持して、画素だけを選択したキャッシュから読みます。RAW本体のデコードや外部コンバーターは不要です。`render`、`export`、`preview-images`、`preview-ev`、`preview`、`keyframes`、`long-term`、`holy-grail`、`batch-holy-grail`、`deflicker`、`batch-deflicker`で指定できます。`batch-render`と`batch-preview-images`は`--`以降に同じオプションを渡します。`--raw-preview`、`--raw-develop`との併用はエラーです。選択したキャッシュが欠けている場合や壊れている場合もエラーとし、元画像・別の種類のキャッシュへフォールバックしません。

キャッシュには処理が適用済みの場合があるため、`export`と`preview-images`では、この入力モードを選ぶと既定のEXIF向き・XMP基本現像・クロップ適用を無効にします。必要な場合はそれぞれのフラグを明示できますが、適用済みの処理を重ねることになります。無加工のMJPEG AVIはキャッシュJPEGを無再圧縮で格納し、TIFFはGoで復号したRGB画素を16-bitコンテナーに保持します。JPEG由来の階調が16-bit相当に増えるものではありません。

通常の解析と同様に`preview-ev --write`は元画像のXMPへ`lrt:ExternalExposureDefault`を保存します。Deflickerは既定で独自測光・補正経路を使い、`--calculation lrt-7.5.5`で本体比較済みの数値計算を選べます。複数パスではキャッシュを基にしたGo側の補正を再測定します。Camera Rawを再実行する機能ではありません。キャッシュは変更しません。LRTの操作後やXMP編集後にキャッシュが古くなっていても日時等から自動判定しないため、必要なプレビューをLRTで生成してから明示的に選択してください。ICC色管理とVisual Deflicker全体の数値一致は未確認です。

## LRTの保存済み参照領域を使う

```powershell
# 保存された参照領域で測光し、補正案を表示
lrtl deflicker C:\photos\sequence --lrt-reference --smoother lowess

# LRT Visual Previewと保存済み領域を使い、2パスの補正を保存
lrtl deflicker C:\photos\sequence --lrt-preview visual --lrt-reference --passes 2 --write

# 各連番自身の参照領域を使って一括補正
lrtl batch-deflicker C:\photos --lrt-reference --recursive --workers 4 --write
```

`--lrt-reference`は画像フォルダーの`.lrt/lrtsequence.json`から`referenceBoxes`を読みます。各エントリーは`フレーム番号;x1;y1;x2;y2`で、番号は0始まり、座標は0〜1です。単一の領域は全フレームにコピーします。複数領域はLRTの参照領域用の単精度自然3次スプラインで補間し、仮想端点・0〜1クランプも反映します。Auto-Transitionの数値補間とは別の経路です。ピクセル化は原点と幅・高さを別々に切り捨てます。

複数領域を使う場合は`filenameSort`の明示が必要です。`true`では現在のファイル名順、`false`ではLRTの日時順に対応付け、同じ順序で平滑化・反復補正も行います。日時順では元XMP（外部サイドカーがなければ対応画像内XMP）の`xmp:CreateDate`を文字列として大文字小文字を無視して比較します。同じ日時ならファイル名を数値の自然順ではなく辞書順で比較し、比較上同じ項目は元の並びを保持します。タイムゾーンはUTCへ換算しません。この列と比較規則は実LRT 7.5.5で確認しています。

日時が全画像で欠けている場合は、本体の比較規則どおりファイル名の辞書順へフォールバックします。一部だけ欠けている場合、本体のペアごとのフォールバックでは一貫した並びを保証できないため、CLIは保存前にエラーにします。日時はXMP上の文字列を使い、原画像のEXIF日時からその列を生成する処理は未対応です。補正の保存先とFrame.Indexは元画像への対応を保持します。

選択で間引いた連番への対応付けは未対応です。`imageCount`が正数で現在の枚数と異なる場合、領域が空の場合、設定ファイル・座標が壊れている場合、画像上で1ピクセル未満になる場合は保存しません。重複するフレーム番号はファイル内の最後の値を使います。有限の範囲外座標は本体同様に0〜1へ制限します。本体が一部の破損エントリーを無視する場合でも、このCLIは明示的に選んだ設定全体を検証してエラーにします。

固定領域の`--reference`、補正解除の`--reset`とは併用できません。XMP基本現像・クロップを明示した場合は、それらを適用した画像上で正規化座標を使います。`.lrt/lrtsequence.json`は変更しません。`--lrt-reference`単独では参照領域と並び順だけを読み、`--lrt-deflicker-settings`を追加すると平滑化・精度・Constant・複数パス設定も読み込みます。測光と補正は既定の独自処理または`--calculation lrt-7.5.5`を使います。参照領域座標や補正計算の一致は、RAW現像を含むLRT Visual Deflicker全体の一致を示しません。

## 白黒現像とGrayMixer

```powershell
# 元XMPで白黒設定された連番を現像して動画化
lrtl render C:\photos\sequence --output C:\photos\monochrome.avi --apply-xmp-basic

# RAWのXMP白黒設定を反映したJPEGプレビュー
lrtl preview-images C:\photos\raw --output C:\photos\monochrome-preview --raw-develop

# 白黒ミキサーの値は従来のset/interpolateでも編集・補間可能
lrtl set C:\photos\sequence\frame0001.nef GrayMixerBlue -30
```

`crs:ConvertToGrayscale=True`（`1`も可）の場合、8色の`GrayMixerRed/Orange/Yellow/Green/Aqua/Blue/Purple/Magenta`を適用します。各値は-100〜100で、不正な真偽値・非有限値・範囲外は画像生成前にエラーにします。False・0・未指定の場合、GrayMixerだけでカラー画像を変更しません。白黒モードの切り替えは元XMPで指定します。

[Adobeの白黒ミックスの説明](https://helpx.adobe.com/lightroom-classic/help/image-tone-color.html)にある、元の色ごとにグレーの明るさを調整するワークフローに対応する独自実装です。既定の変換は線形sRGBのRec.709輝度を使用します。ミキサー値は既存HSL処理の8色の色相中心間で補間し、±100を最大±2段の露出変化へ写像します。彩度の低い画素ほど効果を弱め、無彩色・黒・白を保持します。露出とWBの後、トーン処理の前に変換し、白黒モードでは全体Saturation/Vibrance/HSLミキサーを省略します。後段のRGBカーブ・カラーグレーディング・局所補正は色を付けられます。

16-bit画素・アルファを保持する共通処理を使うため、`render`、`export`、`preview-images`とXMP基本現像を指定した解析に反映されます。GrayMixerの数値補間は既存のLRT登録値の処理を使います。画像の変換式とAdobe Monochrome/カメラ別白黒プロファイル、Adobeの自動白黒ミックス、Camera Rawの演算順・色管理との一致は未検証です。自動ミックスやプロファイル名だけによる白黒切り替えは行いません。実NEF/CR2からの白黒JPEG生成・画像内容の保持・元RAW保持も検証しました。

## JPEG連番プレビュー

```powershell
# 対応XMP基本現像・クロップを適用し、長辺1280pxのプレビューを作る
lrtl preview-images C:\photos\sequence --output C:\photos\preview

# RAWから生成し、表示するJPEGのPreview EVを元XMPへバックアップ付き保存
lrtl preview-images C:\photos\raw --output C:\photos\raw-preview --raw-develop --write-preview-ev

# 露出だけを適用し、指定高のプレビューを作る（基本現像の既定値も切り替わる）
lrtl preview-images C:\photos\sequence --output C:\photos\exposure-preview --apply-xmp-exposure --output-height 720

# 生成済みプレビューから動画を作る
lrtl render C:\photos\preview --output C:\photos\preview.avi

# 現像したJPEGをLRTのvisualキャッシュ位置にも新規保存
lrtl preview-images C:\photos\sequence --output C:\photos\preview --write-lrt-visual-cache
```

既定はJPEG品質90、長辺上限1280px、EXIF向き・対応XMP基本現像・クロップの適用です。`--long-edge`、`--output-height`、`--aspect`、`--quality 1..100`で変更できます。高さを明示し長辺を指定しない場合は、既定の1280px制限を外します。基本現像・クロップはそれぞれ`--apply-xmp-basic=false`、`--apply-xmp-crop=false`で無効化できます。露出のみの指定は、基本現像を明示しなければその既定値を無効化します。

`export`と同じ画像処理と、RAWプレビュー/現像、選択マニフェスト・ファイル名フィルター、画素Deflicker、Motion Blur、文字焼き込みを使います。全画像が成功してから新規フォルダーを公開し、既存の出力は置き換えません。画像はsRGBの8-bit JPEGです。`frames.json`は元ファイル名・元の連番インデックス・生成JPEG名・出力寸法・Preview EV・`ExternalExposureDefault`相当値（Preview EVの4倍）・線形sRGBの輝度中央値を持ちます。測光は保存するJPEGを復号して行い、圧縮前の画素との差を避けます。既存TIFF/動画処理と同じ画素からJPEGを作ることをテストしています。

元画像とXMPは既定では変更しません。`--write-preview-ev`は生成した画像の測光値を元XMPの`lrt:ExternalExposureDefault`へ保存し、`--no-backup`でバックアップを省略できます。選択フィルターを使った場合は選択画像だけが対象です。XMPを読み取れない場合や画像生成に失敗した場合はXMPを書きません。全プレビュー生成・最終XMPの内容検証後に保存します。保存中のOSエラーでは保存済み枚数を報告し、生成済みプレビューは残します。画像と複数XMPを一括でロールバックする機能ではありません。

これはCLIから再生・測光に使う独立したプレビュー画像を作る機能です。XMP反映済み画像を測った値は、未調整RAWの測光値とは異なります。LRT/Camera Rawの縮小・ROI・現像と同じ画像を作る検証や、自動キャッシュ判定・再利用は未対応です。既存キャッシュの明示的な読込は`--lrt-preview camera|visual`で指定できます。`--write-lrt-visual-cache`は生成JPEGを`.lrt/visual/<元ファイル名>.lrtpreview`へ追加し、既存キャッシュは置き換えません。これはlrtlの画像処理結果を書き込むもので、LRT本体が生成した画素との一致は保証しません。個別・バッチ生成で新規書き出しできます。生成画像には現像値が焼き込まれるので、再生時に元画像のXMPを重ねて適用する必要はありません。

## JPEGプレビューの一括生成

```powershell
# ルートと直下の連番を、新規フォルダーへ並列生成
lrtl batch-preview-images C:\photos --output C:\previews --workers 4

# 全階層のRAWを現像し、測光値を元XMPへバックアップ付き保存
lrtl batch-preview-images C:\photos --output C:\raw-previews --recursive --workers 2 --write-preview-ev -- --raw-develop --long-edge 1280 --quality 94

# 全階層のRAWを現像し、新しいLRT visualキャッシュも保存
lrtl batch-preview-images C:\photos --output C:\lrt-previews --recursive --workers 4 --write-lrt-visual-cache -- --raw-develop --long-edge 1280
```

探索範囲は既定でルート自身と直下のフォルダー、`--recursive`で全階層です。`--workers`は連番の並列処理数（既定1、1〜64）です。`--`以降には`preview-images`の画像処理・RAW・選択オプションを共通指定できます。`--output`、`--write-preview-ev`、`--write-lrt-visual-cache`、`--no-backup`は`--`より前に指定します。キャッシュは全連番で重複・既存ファイルを事前検査してから保存し、途中I/O失敗では今回追加したキャッシュをロールバックします。生成済みプレビュー出力は残ります。

出力先全体が新規フォルダーである必要があります。各連番は`sequence_000001_元フォルダー名`のような番号付きフォルダーに入り、`sequences.json`に入力ルートからの相対パス・出力フォルダー・生成枚数を記録します。親子の連番や、同じ名前のフォルダーも区別します。各フォルダー内のJPEG・`frames.json`は個別生成と同じです。

全連番の生成成功後に出力全体を公開します。`--write-preview-ev`では公開前に全対象XMPの内容を検証し、複数画像が同じXMPを共有する場合も保存を中止します。画像生成や内容検証に失敗した場合は一時出力を削除し、元画像・XMPを変更しません。公開後のXMP保存中にOSエラーが発生した場合は、完全なプレビューとそれまでの保存を残し、保存済み枚数を報告します。一括ロールバックには対応しません。

個別生成とのJPEG・測光値・XMPの一致、並列数1/4、親子フォルダー、同名フォルダー、RAW変換経路、バックアップ、破損画像・XMP、共有XMP、保存途中のエラーをテストしています。Camera Raw/LRT Visual Previewとの画素一致やLRTのバッチキュー自体の再現は未検証です。

## ロッシーDNGとサイズ縮小

```powershell
# 元解像度のロッシーDNGへ変換して取り込む
lrtl import E:\DCIM --output C:\photos\lossy-dng --convert-dng --dng-compression lossy

# 長辺2560px以下のロッシーDNGへ変換。予定だけ確認する場合は --dry-run を付ける
lrtl import E:\DCIM --output C:\photos\proxy-dng --convert-dng --dng-compression lossy --dng-long-edge 2560
```

Adobe DNG Converterの[公式CLI](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section/download-1/dng_converter_commandline.pdf)を使います。`lossy`は`-lossy`、長辺指定は`-side N`へ渡し、DNG 1.4互換で出力します。縮小は`--dng-compression lossy`を明示した場合に指定でき、`--dng-long-edge`は1〜32768（0は元サイズ）の上限です。DNGLabバックエンドはlossless/uncompressedのみで、ロッシー/縮小との併用はエラーです。JPEG XLは選択できません。

既存DNGは通常の取り込みではコピーしますが、`lossy`を指定した場合は再変換します。JPEG等はコピーします。元データは変更せず、隣接XMPを新しいDNG名へコピーします。非可逆圧縮と縮小は画質・解像度を減らすため、既定値は引き続きlosslessです。必要なコピー先を事前検証し、変換失敗・不正DNG・縮小上限を満たさない出力では、この取り込みで作成したファイルと一時出力を戻します。

自動テストは外部プロセスを代替して、RAW/既存DNG・空白やハイフンを含むファイル名・引数順・dry-run・XMP保持・不正オプション・縮小を無視するプロセス・2枚目の変換失敗を検査します。実機のAdobe DNG Converter 18.7ではNikon D3S NEF（4256×2832→256×170）とCanon 40D sRAW CR2（1944×1296→256×171）を検証し、Lossy JPEG圧縮（tag 259 = 34892）、既存DNGの再縮小、XMP露出を含むTIFF現像とAVI生成、元データ保持を確認しました。LRT本体の圧縮プリセットやCamera Rawとの画素一致を再現した比較ではありません。

## 撮影間隔で連番を分けて取り込む

### LRT互換の間隔変化による分割

```powershell
# 本体と同じ既定値：許容差1秒、最小5枚。予定だけ確認
lrtl import E:\DCIM --output C:\photos\imported --split-intervals --dry-run

# ミリ秒単位の許容差と最小連番長を明示してコピー
lrtl import E:\DCIM --output C:\photos\imported --recursive --split-intervals --interval-tolerance 500ms --min-sequence-length 10
```

全画像のXMP/EXIF撮影時刻を読み、時刻順に並べ、各写真から次の写真までの間隔をミリ秒で計算します。最後の写真の間隔は本体と同じく−1とします。直前の間隔との差が許容差を**超える**場合に区切ります。本体の境界規則に従い、既に2枚以上ある連番では境界の写真を直前の連番へ含めます。最小枚数に達しないグループは破棄せず、末尾の`single_images`へまとめます。同時刻や非正の間隔を含むグループも保持します。

`--interval-tolerance`は0〜2,147,483,647 msの整数ミリ秒（既定1秒）、`--min-sequence-length`は1以上（既定5）です。元画像とXMPはコピーし、元の場所から移動・削除しません。通常の取り込みと同じく、`--dry-run`、再帰検索、JPEG省略、日時リネーム、DNG変換と併用できます。`--split-gap`や明示的な`--sort name`とは併用できません。時刻の欠損はエラーで、ファイル更新日時には代替しません。

本体の分割モデルへ合成間隔列を与えて得た240ケースと比較しています。一定/変動間隔、許容差の境界、短い連番の集約、重複・非正の間隔を含みます。ファイル名と保存方法はlrtlの方式で、`sequence_001_日時`と`single_images`を新しいコピー先へ作ります。本体の既存フォルダーからの移動やUI上の名前付けを再現するものではありません。コピーとXMP保持、動画生成、衝突の事前検証、途中失敗で作成済みファイル/フォルダーだけを戻す処理もテストしています。

### 指定時間差で区切る方式

```powershell
# 30秒を超える撮影間隔を連番の区切りにする。まずプレビュー
lrtl import E:\DCIM --output C:\photos\imported --recursive --split-gap 30s --dry-run

# 確認後に画像と対応XMPをコピー
lrtl import E:\DCIM --output C:\photos\imported --recursive --split-gap 30s

# 分かれた連番を一括レンダー（MP4/MOVはFFmpegが必要）
lrtl batch-render C:\photos\imported --output C:\photos\movies --format avi
```

`--split-gap`は正の時間（`500ms`、`30s`、`5m`など）で指定します。XMPまたはEXIFの撮影日時が全画像に必要で、更新時刻には代替しません。撮影時刻順に並べ、隣り合う写真の間隔が指定値を**超える**と新しいフォルダーを作ります。同じ間隔なら分割せず、同時刻の写真は元パスの自然順で保持します。フォルダー名は`sequence_001_20261016_120000`の形式です。通常は全体の順番をファイル名に付け、`--rename-by-date`との併用もできます。`--sort name`との同時指定はエラーです。

取り込み前に全候補の日時と出力先を確認し、既存画像やXMPを置き換えません。画像/XMPの内容はコピー時に保持し、元の写真は移動・削除しません。コピーに失敗した場合は、その実行で作成したファイルと空の連番サブフォルダーを除去します。既存の出力ルートや無関係なファイルは保持します。`--convert-dng`も同じ分割プランを利用できますが、RAW変換には外部コンバーターが必要です。

この機能は、[公式の取り込み・連番分割ワークフロー](https://lrtimelapse.com/workflow/lrtimport/)に対応する独立したCLI処理です。LRT側の境界判定アルゴリズムとの一致や、動画のシーン検出による分割は対象に含みません。

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

## Holy Grailの数値比較の範囲

中心合わせ・クリップのヘルパーは84ケースで比較し、非常に大きな正負の値、一定列、対称/非対称の範囲、クリップ有無、符号付きゼロまで同じdouble結果を確認しています。全ての値が境界より外にある場合は、単純に最小値と最大値の中点へ移動する処理とは異なります。

`--lrt-input --auto-shape`の探索は、実LRTの数値コントローラーを呼び出す独立した350候補のグリッド探索と、17/31/61フレーム・Reduce Deflicker 0/0.35の6ケースで比較しています。選ばれるStretch/Rotateは一致し、評価値と曲線は誤差2×10⁻⁸以下です。探索中はクリップをせず、確定した補正の保存計算ではクリップを適用します。LRTのUIワーカーを実行した比較ではありません。

`holy-grail --lrt-input`は、実LRT 7.5.5の数値コントローラーが返す補正曲線と平滑化曲線を66ケースで比較しています。5/17/20/31フレーム、2/3/4つ星の混在、ISO 100/125の変化、Stretch/RotateとReduce Deflickerの組み合わせが対象です。シャッター速度は1秒、絞りはf/8の合成入力で、誤差は各点で2×10⁻⁸以下です。既存LRTプレビューキャッシュからのPreview EV測定は、XMP保存値経由と同じ補正入力になることをGo側の統合テストで確認しています。これはLRT本体のキャッシュ生成や保存・RAW現像を含む全ワークフローの一致を示しません。

この経路では、実LRTのフレーム間処理と同じISO前処理を行います。たとえば整数ラベル`100`は露出値用の数値変換表で約101.594へ変換され、整数化して101として計算されます。1〜99の整数、主要ISOラベル、端数・整数範囲端など140入力を比較しています。入力EXIFを書き換える機能ではありません。操作値はfloat32に揃え、Reduce Deflicker=0.35・20フレームでは6点の平滑化を使います。

`--lrt-input`では名前空間`http://lrtimelapse.com/`の`ShutterSpeed`・`Iso`・`Aperture`を優先します。`Iso`は大文字小文字を区別します。シャッター/ISO文字列137入力と絞り45入力を実測し、前者と有効な絞り43入力でdouble値を比較しています。たとえば`100`と`100.0`、`5.6`と`5.60`は異なる経路を通ります。残り2入力の不正な絞り表記は、ネイティブのゼロへのフォールバックに合わせずエラーにします。実際のフレーム間ラッパーを通す36ケースでも補正量を比較しています。

3項目が揃えばEXIFなしの画像でも利用でき、不足する項目だけEXIFで補います。`--ignore-aperture`では絞りは不要です。EXIFのExposureTime/FNumberは元の分数精度を保ち、LRTが要求するExifTool表示に合わせて文字列へ整形します。この経路は同梱ExifTool 13.59→実LRTパーサーの94ケースと比較しています。たとえば1/125秒は計算時に1/128秒となり、f/11の表示`11.0`もネイティブの値に変換されます。通常のEXIFモードにはこの整形を適用しません。

通常の露出タグがない場合は、APEX形式のShutterSpeedValue/ApertureValueから秒数・絞りを計算します。通常タグを優先し、FNumber=0の場合はApertureValueを使います。ExifToolと同じく、絶対値100以上のAPEXシャッター値は無効な秒数となり、絞りのAPEX値0は情報なしとしてエラーにします（`--ignore-aperture`では絞り不要）。この経路は符号・タグ順序・優先順位を含む112ケースで比較しています。

シャッター・絞り・ISOが変わる17フレームの数値コントローラー比較では、通常タグの12ケースとAPEXだけの12ケースでCLI保存とAVI画素を検査しています。キャッシュ付きPNG、XMPバックアップ、評価・カメラ設定保持、元画像保持、異常なAPEXを含む連番の書き込み防止も検査します。測定対象外の文字列形式、Bulbやメーカー固有タグの選択、クリップを含むLRTの保存・表示ワークフロー、Camera Raw画素との完全一致は未検証です。

## 手動Geometry補正を動画へ反映

```powershell
lrtl set C:\photos\sequence\frame001.nef PerspectiveRotate 2.5
lrtl set C:\photos\sequence\frame001.nef PerspectiveScale 110
lrtl set C:\photos\sequence\frame001.nef PerspectiveVertical 25
lrtl set C:\photos\sequence\frame001.nef PerspectiveHorizontal -10
lrtl set C:\photos\sequence\frame001.nef PerspectiveAspect 15
lrtl set C:\photos\sequence\frame001.nef PerspectiveX 20
lrtl set C:\photos\sequence\frame001.nef PerspectiveY -10
lrtl mark C:\photos\sequence\frame001.nef
# 4つ星の現像キーフレーム間で補間してから動画へ適用
lrtl interpolate C:\photos\sequence --write
lrtl render C:\photos\sequence --raw-develop --apply-xmp-basic --output C:\photos\geometry.avi
```

`--apply-xmp-basic`は`PerspectiveRotate`（−10〜10度）、`PerspectiveScale`（50〜150%、既定100）、`PerspectiveVertical/Horizontal/Aspect/X/Y`（各−100〜100）と、保存済みのUpright変換を読みます。X/Y OffsetはXの正値で右へ、Yの正値で上へ画像を動かします。フルスケール係数0.8121を各軸の画像幅/高さに掛けて移動量を求めます。この係数は公開されたLightroom書き出し計測のX+20/Y+20から採った線形近似で、他の値をAdobe出力と比較したものではありません。[計測根拠](https://github.com/skymanbp/autoshade/blob/main/src/render/perspective.rs)。`PerspectiveUpright`の整数値1〜5が同じ番号の`UprightTransform_1`〜`_5`を選択し、0は無効です。行列は9個の有限なカンマ区切り値を持つ行優先3×3射影行列として扱い、各軸を画像端の画素座標`0..幅−1`／`0..高さ−1`へ正規化して逆写像します。`UprightTransformCount`があれば選択行列を含む個数であることも検証します。選択値に対応する行列がない場合はエラーにし、画像解析から新しいUpright行列は生成しません。`PerspectiveX/Y`はLRT 7.5.5の遷移登録にないため、`interpolate`は値を変更しません。

保存Upright→手動変形→XMPクロップ→全体トーンの順で処理します。保存Uprightと非中立の手動Geometryを同時に適用する場合、現在は変形ごとに再サンプルします。Rotate/Scale/Aspectは向き適用済み画像の中心基準で変換し、正の回転は画面上で時計回りです。Vertical/Horizontalは独立した逆ピンホールカメラモデルで適用します。出力キャンバスの大きさは変えず、はみ出した領域を切り、入力外は透明（動画では黒）にします。自動フィットやConstrain Cropは行いません。

16-bit画素とアルファを保持し、解析・Deflickerの再測定・Motion Blur・動画の共通処理経路で適用します。`PerspectiveVertical/Horizontal`は60度の画角を持つ逆ピンホールカメラモデルで投影し、スライダー±100をピッチ/ヨー±20度として扱います。`PerspectiveAspect`は横倍率`2^(値/100)`、Rotate/Scaleは既存の中心基準変換です。X/Y Offsetの座標式と符号は公開されたLightroom出力の1点ずつの計測から採った線形近似です。保存UprightはXMPに記録された行列を再生しますが、行列の座標解釈・補間結果をAdobeの出力画像と画素比較したものではありません。その他の手動スライダーも独立実装で、Camera Rawとの数値・画素一致は未検証です。出力キャンバスは同じ寸法で、外側は透明（動画では黒）。Upright自動解析、行列のないGuided操作、Distortion、Constrain Crop、自動フィットは未対応です。合成ランプの独立座標期待値、XMPモード/個数/行列の異常入力、射影分母と画像外サンプル、非ゼロ画像原点、恒等行列の非再サンプル、16-bit/アルファ保持、AVI実画素、入力画像/XMPの保持をテストします。[AdobeのGeometry説明](https://helpx.adobe.com/lightroom/desktop/edit-photos/crop-rotate-geometry.html)ではVertical/Horizontalが遠近、Aspectが画像全体の縦横比を調整する操作として説明されています。

## XMP現像の精度

### RAWの黒レベルとセンサー値

Adobe DNG Converterが出力したLinear DNGを、Go側で黒レベル・白レベル・カメラ色変換・sRGB符号化の順に処理します。`BlackLevelRepeatDim`の繰り返しパターン、色面別BlackLevel、ActiveArea幅/高さの`BlackLevelDeltaH/V`を扱い、DefaultCropより前のActiveAreaを座標基準にします。正規化は各色面の最大黒レベルを使います。LinearizationTableの範囲外は末尾の値へ丸め、センサー値を0〜1へ制限してから色変換とBaselineExposureを適用します。[Adobe DNG仕様](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section_733958301/download-1/DNG_Spec_1_7_1_0.pdf)の第5章に基づく処理です。

1または3色面の200パターンを独立した全領域走査と比較し、両バイト順・有効領域のずれ・クロップ・負の行/列補正・不正メタデータをテストします。合成RAWでは補正後の一様な画素値をXMP露出、TIFF書き出し、AVI画素まで追跡します。Adobe SDKの最大黒レベル計算も参照していますが、SDK/Camera Rawを呼んだ画素比較ではありません。未対応のOpcodeやカメラプロファイル全体、ノイズ処理、HDR、Camera Rawとの完全一致は未対応です。

### RAWのOpcode補正

`OpcodeList2`と`OpcodeList3`の以下の補正を記録順に適用します。黒レベル補正後、色変換とBaselineExposureより前の線形カメラ値が対象です。領域・色面・行列の間引きはActiveArea内の座標で指定し、DefaultCropは補正の原点を変えません。整数RAWの0〜1正規化モデルを使い、補正ごとにクリップします。

| ID | Opcode | 対応内容 |
|---|---|---|
| 1 | WarpRectilinear | 径方向4係数・接線方向2係数。共通の変形中心と全色面共通または色面別の係数を読み、DefaultScaleの画素比を反映して再サンプル |
| 2 | WarpFisheye | arctan写像による径方向4係数。共通または色面別の係数と変形中心を読み、DefaultScaleの画素比を反映して再サンプル |
| 3 | FixVignetteRadial | 正規化した光学中心と径方向の5係数（r²〜r¹⁰）で線形値を補正 |
| 6 | TrimBounds | Opcodeの位置で画像を指定矩形へ切り詰め、後続OpcodeとDefaultCropに反映 |
| 7 | MapTable | 正規化値を16-bit添字へ丸め、1〜65,536要素のLUTを参照。範囲外は末尾を延長 |
| 8 | MapPolynomial | 定数項から順に指定された0〜8次の係数を適用 |
| 9 | GainMap | 画素中心とActiveAreaの幅/高さから位置を求め、双線形補間。範囲外は端を延長し、不足色面は最後の面を使用 |
| 10 / 11 | DeltaPerRow / DeltaPerColumn | 対象の各行/列へテーブル値を加算 |
| 12 / 13 | ScalePerRow / ScalePerColumn | 対象の各行/列へテーブル値を乗算 |
| 14 | WarpRectilinear2 | 径方向0〜14次、接線方向2係数、有効半径、逆数モードを備えたワープ。色面ごとの中心・係数に対応 |

OpcodeのデータはTIFF本体のバイト順と独立してbig-endianで読みます。必須の未対応Opcode、将来バージョンの必須Opcode、不正なサイズ・非有限値はエラーです。行/列のテーブル数は指定領域と間引き後の要素数に一致する必要があります。空領域はそのOpcode時点の画像全体を表し、row/column pitchは1にします。行/列Opcodeでは空領域でもテーブル添字の基準座標と行数/列数は格納された領域のTop/BottomまたはLeft/Rightを使い、`TrimBounds`後も画像座標を保持します。オプション指定された未対応Opcodeはスキップし、対応済みのOpcodeはオプション/プレビュー指定でも適用します。対応する`WarpRectilinear2`がオプション指定の場合、その次に置かれた`WarpRectilinear`または`WarpFisheye`だけを互換用フォールバックとしてスキップします。`TrimBounds`後の配列を縮小し、後続Opcodeの座標原点を保持してDefaultCropと交差します。現在は1リスト1024個・16 MiBまでです。`OpcodeList1`、欠陥画素補間など他のOpcodeは未対応です。画像IFDごとのリストを使い、別のルートプレビューのリストは継承しません。

FixVignetteRadialはActiveAreaを基準に正規化した光学中心と、最も遠い角までの距離から半径を求めます。ゲイン`1 + k0·r² + k1·r⁴ + … + k4·r¹⁰`を線形値へ乗算し、Opcodeの後で0〜1にクリップします。WarpRectilinear2は色面ごとの15径方向係数、2接線係数、有効半径と任意の逆数モードを使い、WarpRectilinear/WarpFisheyeと同じ再サンプル処理へ渡します。WarpRectilinear/WarpRectilinear2/WarpFisheyeは変形先から入力位置を求め、画像外は端へ制限し、係数A=-0.75のバイキュービック補間で読みます。幾何補正・ビネット補正を含む場合はActiveArea全体の線形カメラ値をfloat32で保持して他の補正との記録順を守り、作業バッファーの上限を1 GiBにします（画像ファイルや出力画像のメモリーは別です）。恒等ワープでは再サンプルせず、従来の精度を保ちます。補間重みは直接計算しており、SDKの座標・重みテーブルの量子化とは一致しません。サイズ・有限値・中心範囲・有効半径・演算時の座標を検査しますが、全係数モデルの可逆性を保証する検査は未実装です。

処理は[Adobe DNG仕様の第7章](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section_733958301/download-1/DNG_Spec_1_7_1_0.pdf)とSDKを参照した独立実装です。WarpRectilinear2は明示的なべき乗和による独立座標参照、色面ごとの係数・中心、有効半径、逆数式、旧式フォールバック省略を検査します。TrimBoundsは連続した切り詰めと座標維持、ActiveArea／DefaultCropとの交差を検査します。ビネットゲインも独立したべき乗和で、中心・補正順・クリップ・両バイト順・異常値を検証します。既存のLUT全65,536入力、多項式、混在する補正順、破損データ、合成RAWからXMP露出・TIFF・AVIへの画素をテストし、ファズテストも追加しています。Adobeの画素処理全体や、SDKの中間16-bit量子化・走査最適化を含むビット単位の一致を示すものではありません。

### RAWプロファイルの露出オフセット

埋め込み`BaselineExposureOffset`（tag 51109）は1個のSRATIONALとして読み、BaselineExposure（50730）へEV値で加算します。省略時は0です。合計に対する`2^EV`を既存の線形露出段階で適用し、XMP露出はその後のレンダーパイプラインで処理します。プロファイルの色テーブルやトーンカーブがない場合も適用します。

分母0、非有限値、形式・個数の不一致はエラーです。現像エンジンの範囲として、BaselineExposure・オフセット・合計のそれぞれを−16〜+16 EVに制限します（DNG仕様の値域を表すものではありません）。両バイト順・正負の分母を含む684ケースでEVと画素を独立計算し、全65,536入力での正負露出の相殺、色テーブル・カーブとの順序、IFD継承と上書き、XMP露出→TIFF→AVI、失敗時の出力取り消し・元データの保持をテストしています。分数の専用ファズテストとDNGファイル全体のファズテストもあります。Adobeのシャドウ・露出ランプやCamera Raw全体との画素一致は未対応です。

### RAWプロファイルのHue/Sat・Lookテーブル

`ProfileHueSatMapDims/Data1/Data2`（tag 50937〜50939）と`ProfileLookTableDims/Data`（50981/50982）を読み、埋め込みプロファイルを線形ProPhoto RGB上で適用します。Hue/Sat→BaselineExposure + BaselineExposureOffsetの線形ゲイン→Look→ProfileToneCurve→sRGB変換の順です。トーンカーブがなくてもテーブルを適用します。

テーブルはV→H→Sの順に格納された色相シフト（度）・彩度倍率・明度倍率です。Hは循環し、S/Vの端点を含めて三線形補間します。Vが1の2次元テーブル、V省略/ゼロの旧形式も読みます。彩度ゼロでは明度倍率を1に補正します。彩度ゼロ行の省略形式では最初の彩度行から色相・彩度倍率を補い、明度倍率を1にします。`ProfileHueSatMapEncoding`/`ProfileLookTableEncoding`の0（linear）と1（sRGB）に対応し、sRGBの場合はVを符号化して参照・倍率補正し、復号してRGBへ戻します。浮動小数点の直接演算を使い、SDKの補間テーブル量子化との一致は保証しません。

2光源は、色行列とWBの既存McCamy色温度近似から逆色温度の重みを求め、各テーブル値を線形混合します。既知の校正光源範囲外では端のテーブルを使います。校正光源が未知または同一なら最初のテーブルを使います。3光源ではDNG SDKのxy→温度/Tint推定と、温度/Tint空間での距離の逆数、SmoothStep、小ウェイト抑制を使って3つのウェイトを求め、ColorMatrix・CameraCalibration・ForwardMatrixおよびH/Sテーブルへ同じ重みを適用します。標準校正光源とxy形式のカスタム`IlluminantData`に対応します。スペクトル形式のカスタム`IlluminantData`は未対応としてエラーにします。

寸法・形式・個数・有限値・非負の倍率・符号化を検査し、1テーブルあたり約35万エントリー（数値要素で2^20以下）を上限とします。不正なテーブルや未対応の符号化は出力公開前にエラーにします。H/S/Vの独立した格子式、色相の折り返し、全RGB格子17×17×17の恒等性、無彩色、sRGB V、2光源の混合と順序反転、三光源ウェイトの固定参照値、3テーブル補間、三光源Matrix/ForwardMatrix、両バイト順、別バイト順の埋め込みCamera Profile IFD、カスタムxy照明、欠損・重複・不正タグ、トーンカーブなし、合成RAW画素をテストしています。Linear DNG全体のファズテストにも三光源プロファイルをseedとして追加しています。実カメラのCamera Raw現像との画素比較、外部DCPの選択、HDRは未対応です。

### RAW埋め込みプロファイルと空間ゲインマップ

Camera Profile IFD内の`ProfileName`と`ExtraCameraProfiles`を解析し、`AsShotProfileName`または現像時のsidecar XMP `crs:CameraProfile`に一致する埋め込みプロファイルを選びます。プロファイルIFD独自のバイト順と相対オフセットを使い、Color/Forward Matrix、Hue/Sat・Lookテーブル、Tone Curve、BaselineExposureOffset、GainTableMap/Map2を既存の現像経路へ渡します。`CameraCalibration1/2/3`はCameraCalibrationSignatureとProfileCalibrationSignatureが一致する場合だけ使います。指定名が埋め込まれていない場合は主プロファイルへ戻ります。テストはAsShotProfileNameとsidecar指定、主プロファイル維持、未知名のフォールバック、別バイト順のMap/Map2画素、三光源テーブルとxy照明データを持つ別バイト順プロファイル、破損IFDのエラー、CameraCalibration署名一致/不一致、sidecar指定からConverter経由の画素までを確認し、DNG全体のfuzz seedにも追加しています。

DNG 1.6の`ProfileGainTableMap`（tag 52525）とDNG 1.7の`ProfileGainTableMap2`（tag 52544）を読みます。両方がある場合はMap2を優先します。`MapInputWeights`でRGB・最小値・最大値を1値にまとめ、範囲を0〜1へ制限します。Map2はさらに`Gamma`を適用してからゲイン曲線を参照します。MapPointsN方向は線形補間し、画素位置に対応する隣接空間テーブルは双線形補間します。ゲイン値はfloat32、float16、16-bit整数、8-bit整数（`GainMin`/`GainMax`で復号）を扱います。ゲインはOpcode適用後、BaselineExposure適用後の線形ProPhoto RGBへ乗算し、その後のLook/ProfileToneCurve処理へ渡します。

TIFFのバイト順、テーブル寸法・符号化・長さ、有限の座標・重み・ゲイン、非負のゲインを検証し、テーブルデータは16 MiBまでに制限します。色と位置を手計算した補間、Map2の全4データ型・ガンマ・優先順位、ActiveAreaの原点ずれ、範囲端、両バイト順、不正データ、Linear DNG現像後の画素値をテストし、Map/Map2パーサーをfuzzしています。現在のSDR現像経路ではテーブル適用前に値を0〜1へ制限し、最終画素もSDR範囲へ制限します。埋め込みCamera Profile IFDはAsShotProfileName／sidecar crs:CameraProfileで選択し、別バイト順・ProfileGainTableMap/Map2・現像画素までテストしています。HDR/オーバーレイの保持は未対応です。

### RAWプロファイルのRGBTables

DNG 1.6/1.7の`RGBTables`（tag 52543）を読み、3DカラーLUTをLinear DNGの埋め込みCamera Profileへ適用します。DNG 1.7で選択プロファイルとIFD 0の両方にタグがある場合は、Camera Profile IFD側を使います。各テーブルは2〜32分割、8-bit整数・16-bit整数・32-bit floatに対応し、GammaEncoding（linear/sRGB/1.8/2.2/Rec.2020）、5種類のColorPrimaries、色域拡張を処理します。テーブル値は三線形補間です。

SemanticNameに一致するSemantic Maskで適用範囲を決め、`MaskSubArea`の縮小・切り抜き配置、Weighted Sum/Sequential合成、背景テーブル、未指定背景の恒等変換を扱います。8/16-bit整数と32-bit floatのマスクをストリップまたはタイルから読み、無圧縮・Deflate・lossy JPEGを復号します。ProfileToneCurveの後、最終sRGB変換の前にRGBTablesを適用します。

テストは両バイト順と3つのテーブルデータ形式、5種のガンマ/原色、補間・色域拡張、両合成方式、背景恒等変換、8/16-bit整数・32-bit floatマスク、Predictor、Deflate、lossy JPEG、パディング付きタイル、MaskSubArea、ActiveAreaオフセット、不正データ、Linear DNG全体のfuzz seedを確認します。Semantic MaskのLossless JPEG復号、Semantic Maskと幾何Warpの併用、HDR拡張値は未対応です。Camera Rawとの画素一致を検証したものではありません。

### RAWプロファイルのトーンカーブ

主画像に埋め込まれた`ProfileToneCurve`（tag 50940）を読みます。2〜4096組のFLOAT形式の制御点を受け付け、入力座標が厳密に増加する0〜1の点列を自然3次スプラインで補間します。出力は0〜1へ制限し、制御点の範囲外では端の値を使います。サイズ・形式・座標順・非有限値が不正なら、出力の公開前にエラーを返します。

色画像は既存のWB・色行列処理から線形ProPhoto RGBへ変換し、OpcodeとBaselineExposureのゲインを反映した後にカーブを適用します。RGBの最大・最小値をカーブで変換し、中間値の相対位置を保つことで色相を維持します。最後に線形sRGBへ変換してガンマを符号化します。モノクロ入力にも適用します。XMPの露出や基本現像は、その後の既存レンダーパイプラインで処理します。カーブがなくても、前節の対応テーブルがあれば適用します。カーブもテーブルもない画像では従来の線形現像を使います。

自然スプラインの解析解、不等間隔の制御点、RGBの全6順列と同値、色域外の出力、両バイト順、BaselineExposureとの順序、恒等カーブの全65,536入力（行列の丸めによる差は16-bitで最大1）、合成RAW→Opcode→プロファイルカーブ→XMP露出→TIFF→AVI、失敗時の後始末をテストしています。プロファイルカーブとDNGファイルのファズテストも追加しています。

処理は[DNG仕様](https://helpx.adobe.com/content/dam/help/en/camera-raw/digital-negative/jcr_content/root/content/flex/items/position/position-par/download_section_733958301/download-1/DNG_Spec_1_7_1_0.pdf)と[Adobe DNG SDKの三光源補間実装](https://android.googlesource.com/platform/external/dng_sdk/+/de700ad461e35af50b28b861943a0b0753b10929/source/dng_xy_coord.cpp)を参照した独立実装です。DNG内に埋め込まれていない外部DCP／カメラプロファイルの読込、スペクトル形式のカスタム照明データ、Semantic MaskのLossless JPEG復号、RGBTablesでのHDR拡張値・幾何Warp、Adobe既定のシャドウ・露出ランプは未対応です。BaselineExposure + BaselineExposureOffsetは既存の単純な線形ゲインを使います。AsShotProfileNameまたはsidecarのcrs:CameraProfileで埋め込み追加プロファイルを選べますが、外部DCPの読込・選択には対応しません。SDKの補間テーブルの量子化やCamera Raw全体との画素一致を検証したものではありません。

### XMPの画素処理

`lrt:Quality=FLAT`ではIncrementalTemperature/Tintを選び、非使用のTemperature/Tintを参照しません。相対色温度は`6500 + 値 × 40` Kへ対応づけ、既存のローカルWBと同じsRGB近似で反映します。相対値0では中立経路を保ち、16-bitの細かな差とアルファも検査します。Adobeの相対WB変換式を再現したものではありません。Qualityが未指定または他の値なら従来の絶対WB経路を使います。

対応済みの全体/ローカル現像処理は、NRGBA64（RGB/アルファ各16-bit）と浮動小数点の計算を使います。全体の露出・WB・トーン・彩度/HSL・Color Grading・ポイント/パラメトリックカーブ、Texture/Clarity/Dehaze、シャープ・ノイズ低減・周辺減光、および対応マスクの局所調整が対象です。空間フィルターの補助層はfloat32、主要演算はfloat64です。ポイントカーブはXMPの0〜255点座標からPCHIP係数を作り、65536段階の入力位置で評価し、フレームごとに巨大なLUTを持ちません。

全体22ケース、ローカル12ケースで細かな入力差・アルファ・元画像の保持を確認し、カーブは全65536段階の恒等性、宣言点の通過、単調性とファズテストを検証します。これはlrtl内での精度を確認するものです。Camera Rawの演算式・カメラプロファイル・ICC色管理・HDRの再現、およびLRTとの画素一致は未達です。Deflicker測光など一部の解析値は従来の統計定義のままで、Preview EVの256ビンは観測したLRT仕様に合わせています。

## 連番の選択とコピー

`select`はファイル名、XMP評価、既存の選択JSONでフレームを絞り込みます。条件は積集合として扱い、`--invert`でその結果を元の連番全体に対して反転します。評価がない画像は0、除外評価は-1で、`--ratings`は-1〜5の整数を複数指定できます。外部XMPがある場合はそれを使い、なければ対応画像内のXMPを読みます。ファイル名やJSONだけの選択では画像現像やXMP解析を必要としません。

```powershell
# 4/5つ星を選び、render/export/preview-imagesでも使えるJSONへ保存
lrtl select C:\photos\sequence --ratings "4,5" --output C:\photos\keyframes.json

# 既存選択とファイル名条件を満たすフレーム以外を選ぶ
lrtl select C:\photos\sequence --selection C:\photos\filter.json --filename-contains IMG_ --invert --output C:\photos\other.json

# まず予定を確認し、--writeを追加して新規フォルダーへコピー
lrtl copy-sequence C:\photos\sequence --selection C:\photos\keyframes.json --output C:\photos\selected
lrtl copy-sequence C:\photos\sequence --selection C:\photos\keyframes.json --output C:\photos\selected --write
lrtl render C:\photos\selected --output C:\photos\selected.avi --apply-xmp-basic
```

`select`は`--output`なしなら選択した名前を表示し、JSONの上書きには`--overwrite`が必要です。JSONは選択結果を固定し、画像のサイズ・更新日時が変わると古いJSONの使用を拒否します。現在のXMP評価で選び直す場合は`select`を再実行してください。空の選択も保存・反転できますが、動画処理やコピーは選択0枚をエラーにします。

`copy-sequence`にも同じ選択フラグを直接指定できます。既定は画像と、通常の読込で採用する外部XMPだけをバイト単位でコピーし、更新日時と名前を保持します。複数画像が同じstemのXMPを共有するときは一度だけコピーします。`--include-lrt-companions`では、同名JPEGとその採用中XMP、選択画像に対応する`.lrt/previews`・`.lrt/visual`・`.lrt/proxy`のファイルも追加します。選択外のJPEGが追加され、画像数が増える場合があります。連番全体の設定や履歴・バックアップは含めません。[付随ファイルの詳細](docs/sequence-companions.md)を参照してください。コピー先は元連番の外にある新規フォルダーが必要で、既存出力を置き換えません。全ファイルのコピーが成功してから出力を公開します。元画像・XMPは変更しません。画像単位の移動には`move-sequence`を使います。削除は未対応です。

`move-sequence DIRECTORY --output NEW_DIRECTORY --ratings 4,5`は選択画像を移動する計画を表示し、`--write`で実行します。既定では同名JPEG・画像別キャッシュと両候補XMPも対象にします。未選択画像に必要なXMP／キャッシュは元にも残します。Windows／Linuxの新規出力先に対応し、SHA-256検証と失敗時の復元を行います。[移動対象・途中失敗・復旧の制限](docs/move-sequence.md)を確認してください。

`remove-sequence DIRECTORY --ratings -1`は選択画像を隣の`<元フォルダー名>_REMOVED`へ除外する計画を表示し、`--write`で移動します。既存先へ繰り返し追記でき、同内容のXMP／キャッシュは既存分を再利用します。任意の既存先へ移動する場合は`move-sequence --merge`を使います。同名画像や異なる内容のXMPは上書きしません。[追記と除外・復旧の制限](docs/remove-sequence.md)を参照してください。

動画・TIFF・JPEGプレビュー・字幕の出力順序は`--sort`で指定できます。`capture-time`は撮影時刻を解釈し、`lrt-date`は本体同様に`xmp:CreateDate`の文字列で並べます。`lrt-settings`は連番の`.lrt/lrtsequence.json`に保存された`filenameSort`を読みます。字幕には動画と同じ選択・並べ替えを指定してください。[順序の種類・バッチ・制限](docs/sequence-order.md)を参照してください。

フォルダー全体の改名は`rename-sequence`で行います。`.lrt`やサブフォルダーも含めて同じ親の中で移動し、個々のファイル名と内容を保持します。既定はプレビュー、`--write`で実行します。絶対パスを保存した選択JSONは改名後に再生成してください。[復元処理・対応OS・使用例](docs/rename-sequence.md)を参照してください。

```powershell
lrtl rename-sequence C:\photos\sequence --name "Day 01"
lrtl rename-sequence C:\photos\sequence --name "Day 01" --write
```

選択の積集合・反転、埋め込みXMPと外部XMPの優先順位、空の選択、古いJSON、共有XMP、読み取り専用画像、シンボリックリンク境界、失敗時の保持をテストしています。コピーした選択範囲から作ったAVIのフレームとインデックスが、元連番の同じ選択範囲と一致することも確認しています。本体のコピーUIやファイル属性全体との一致を検証したものではありません。

### 選択範囲のXMP一括設定

`set-sequence DIRECTORY PROPERTY VALUE`は、`set`と同じ全体数値・PreviewEV・既存マスクの局所数値/真偽値を、選択した画像のXMPへ一括設定します。既定は変更予定の表示で、`--write`で保存します。ファイル名、XMP評価、選択JSON、反転の意味は`select`と同じです。

WBの4数値は小数部を0方向へ切り捨て、Temperatureは2000〜50000、Tintは-150〜150、IncrementalTemperature/Tintは-100〜100へ制限します。`lrt:Quality`が完全一致の`FLAT`なら相対2項目、それ以外ならTemperature/Tintが有効な組です。有効な組の編集では`WhiteBalance=Custom`へ切り替え、対になる既存値を整数読込・範囲制限して保存し、非使用の組を消去します。対の値がなければTemperature=5850／Tint=8／相対値=0を使います。プレビューにWB全5項目を表示します。画像からのQuality分類は`init --frame-state lrt-7.5.5`で行います。実機全体のEXIF由来WB初期化、編集によるキーフレーム設定は未対応です。[960条件の比較と対応範囲](internal/lrtl/testdata/lrt-wb-edit-7.5.5.md)を参照してください。

```powershell
lrtl set-sequence C:\photos\sequence Exposure2012 -0.5 --ratings "4,5"
lrtl set-sequence C:\photos\sequence Exposure2012 -0.5 --ratings "4,5" --write
lrtl set-sequence C:\photos\sequence PreviewEV 0.25 --selection C:\photos\filter.json --invert --write
lrtl set-sequence C:\photos\sequence 'MaskGroupBasedCorrections[1].CorrectionMasks[1].FullY' 0.8 --filename-contains IMG_ --write
```

全対象のXMP・値・マスク種別を検証した後、変更があるXMPごとに保存し、既存XMPの元内容をバックアップします。元画像・選択外のXMP・未知のメタデータ・マスクIDを保持し、同じ設定を再実行しても不要な書き込みやバックアップを増やしません。`--no-backup`は`--write`との併用時だけ使えます。PreviewEVは既存の単一画像編集と同じ4倍の保存単位です。全体数値の有限値チェック、局所項目の範囲・型チェック、HG/Deflickerなどの内部補正保護も共有します。

選択画像の変更先XMPが未選択画像の候補にもなる場合は、保存前にエラーにします。未選択画像が現在は専用の`.画像拡張子.xmp`を使っていても、共通stemのXMPを更新すると採用先が切り替わるため、両方の候補を確認します。両方を選択するか、`copy-sequence`で選択範囲を分離してから編集できます。両方が選択されている共有XMPは一度だけ保存します。外部XMPがない画像は埋め込みXMPを引き継いだサイドカーを作成します。

共通のXMP保存処理では、未来日時を持つ既存サイドカーの更新日時も進めます。`.画像拡張子.xmp`と通常のstem `.xmp`が併存するとき、保存後に古い方が採用されて編集が見えなくなる問題を回帰テストで修正しました。

単一画像編集とのXMP一致、負の値・評価、選択JSONと反転、局所項目、埋め込みメタデータ、元内容バックアップ、繰り返し時の保持、後の不正XMPや内部補正に対する保存前エラー、編集後のAVI一致をテストしています。

### 選択範囲のキーフレーム設定・解除

`mark-sequence`で選択した画像を4つ星（`--kind default`）または5つ星（`--kind crop`）に設定できます。`unmark-sequence`でlrtlが付けたマークを解除し、設定前の評価（評価なしも含む）を復元します。両方とも既定は変更予定の表示で、保存には`--write`が必要です。`select`と同じファイル名・評価・JSON・反転の選択フラグを使えます。

```powershell
lrtl mark-sequence C:\photos\sequence --selection C:\photos\filter.json --kind default
lrtl mark-sequence C:\photos\sequence --selection C:\photos\filter.json --kind crop --write
lrtl unmark-sequence C:\photos\sequence --selection C:\photos\filter.json --write
```

4つ星から5つ星へ変更しても最初の評価を保持します。解除時はlrtlのマークがない画像をスキップし、外部で付けた4/5つ星も消す場合だけ`--force-rating`を指定します。2/3つ星や内部Holy Grailマークは解除しません。通常マークの設定対象にHGが含まれる場合、およびlrtlのマークを持つ画像が後からHGへ変更されている場合は、全対象の保存前にエラーにします。

既存XMPのバックアップ、変更がない場合の保存省略、共有XMPによる選択外画像の保護、埋め込みXMPの引き継ぎは`set-sequence`と共通です。保存は事前検証後にXMP単位で行うため、保存中のI/O障害に対する連番全体の原子性は保証しません。評価の復元、種別変更、選択反転、繰り返し、HG保護、後半の不正XMP、共有XMP、元画像保持を回帰テストに含めています。

## 開発

局所補正の新規RDF Resourceへ値を追加するときは子プロパティを使い、`rdf:parseType="Resource"`とプロパティ属性の不正な組み合わせを生成しません。初期化（Modern/Legacy）・欠損補正生成（4形式）・欠損セレクター生成40ケース・欠損RangeMask生成144ケースの計190件のXMPを、インストール済みLRTのAdobe XMPパーサーで読み込めることも確認しました。通常テストはGoだけでRDF構造を検査します。実パーサー検査を再実行する場合はJava/Javacを用意し、以下を実行します（本番CLIにはJava依存を追加しません）。

```powershell
$env:LRTL_NATIVE_JAR='C:\Program Files\LRTimelapse 7\LRTimelapse.jar'
go test ./internal/lrtl -run '^TestGeneratedMaskResourcesAdobeParser$' -count=1 -v
```

この検査はXMPの受理を確認するもので、Camera Rawの画像現像やLRTの保存処理全体との一致を示すものではありません。

Goの機能追加・修正では、数値の期待値、境界条件、異常入力、既存XMPの保持とバックアップを回帰テストで確認します。数値テストは手計算や独立した参照値を使い、実装の計算をそのまま複製して期待値にはしません。変更後は以下のテスト・静的解析・ビルドを実行します。GitHub ActionsもWindows/Linuxで同じ3項目を実行します。

現在の自動テストはXMP読み書き・補間、キーフレーム、Preview EV、Deflicker、MJPEG AVI生成に加え、LRTキャッシュだけを入力にしたSVGプレビュー/長期測定、キャッシュ欠落時の失敗と入力不変も対象としています。平滑化単体はLRT 7.5.5の実測値53ケース、Deflicker計算は108ケースと比較済みです。後者は3種類のXMP保存形式へ適用した324ケースも確認し、CLI保存からAVIの復号画素が変わることとリセットで元に戻ることをテストしています。FFmpegは各測定条件で動画の復号画素・フレーム数・色タグまで比較し、再実行方法を個別の測定記録に載せています。一方、本体が一連の処理で保存したXMP全体との一致、Camera Raw/Lightroom現像との画素一致、外部バックエンドの全入力・全設定の組合せは未検証です。ビルド成功だけでは動作確認済みとしません。

```powershell
go test ./...
go vet ./...
go build -o lrtl.exe ./cmd/lrtl
```

互換性の観察と既知の制約は[リバースエンジニアリングノート](docs/reverse-engineering.md)を参照してください。

## ライセンス

MIT。詳細は[LICENSE](LICENSE)を参照してください。

This product includes DNG technology under license by Adobe.
