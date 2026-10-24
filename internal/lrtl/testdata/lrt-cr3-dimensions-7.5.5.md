# Canon CR3寸法キャッシュと保存（LRT 7.5.5）

## 実装した範囲

`init`の既定保存でCR3の寸法を読み、`lrt:Width`／`lrt:Height`へ反映します。円形マスク補間でXMPに寸法がない場合にも同じ読込を使います。`.CR3`の大文字拡張子に対応し、`--frame-state preserve`は寸法を更新しません。

`ftyp`のmajor brandが`crx `のISO BMFFコンテナーを対象に、箱の宣言境界をたどります。Movie内のCanonメタデータUUID `85c0b687-820f-11e0-8111-f4ce462b6a48` のCMT1／CMT2にあるClassic TIFFと、Trackの`tkhd`にある寸法を読みます。画素データや未知のUUID内部を寸法タグとして走査しません。

| 観測した条件 | 寸法の選択 |
| --- | --- |
| 複数のCMT1／CMT2やトラック寸法 | 格納順で最初の値を軸ごとに使う |
| 別ブロックに幅・高さが分かれている | それぞれの最初の値を組み合わせる |
| 本画像フラグ付きSubIFD／次IFD | 先の幅・高さを上書きしない |
| ExifImageWidth／Heightのみ、または競合 | CR2の寸法上書き規則を適用しない |
| Track Header v0／v1 | 16.16値の整数部分を使い、0の軸は採用しない |
| TIFFの寸法0 | 0のままキャッシュし、保存では正の軸だけを更新する |
| 幅または高さが欠損 | ImageSizeペアが得られず、両軸のキャッシュは0 |
| 32-bit符号付き整数の上限を超える寸法 | 本体のCSV読込と同じくエラー |
| BigTIFFのCMT、トップレベルのCanon UUID、未知のUUID | 今回の観測では寸法を採用しない |
| 長さ0のCanon UUID箱 | 今回の観測では中の寸法を採用しない |

この処理はメタデータの寸法を供給します。`render --raw-preview`のJPEG画素寸法は変更しません。フレームごとの保存と補間用XMP寸法の優先順位も維持します。

## 比較の根拠

`scripts/probes/cr3_dimensions_fixture.py`が、独自のISO BMFF箱・両バイト順のTIFF・独自の単色JPEGから **76種類** のCR3を生成します。CMTの順序、SubIFD、次ページ、Exifとの競合、軸別欠損、ゼロ、70000以上の寸法、整数境界、16-bit値、BigTIFF、64-bit箱サイズ、Track v0／v1、プレビューのみ／CMT併存を含みます。

独自の `RawDimensionsOracle.java` がインストール済みExifToolの `ImageSize` を取得し、その文字列を本体のCSVメタデータ読込と保存処理へ渡します。74条件の寸法キャッシュ／保存前半がGoと一致し、残る2条件は両方で整数範囲エラーになります。凍結参照は `lrt-cr3-dimensions-7.5.5.json` です。通常テストにJava・本体・ExifToolは不要です。

この観測は履歴処理の未初期化main参照で停止する保存前半までです。製品ワークフロー全体の保存完了は示しません。main・ライセンス・製品メソッドを置換していません。実カメラ画像や製品リソースも含みません。

## メタデータ依存ライブラリのpanic修正

今回のBigTIFF入りCR3で、既存のimagemeta依存が範囲外アクセスによりpanicすることを確認しました。共通のメタデータ読込境界で依存の実行時エラーを読込エラーへ変換し、失敗した部分的なメタデータは使いません。各呼出元の既存のエラー処理／フォールバックを維持します。

初期化、日時読込、向き取得、カメラ露出読込と高精度露出読込について、この入力がプロセスを落とさずエラーまたは既存のフォールバックになることをテストしています。変換するのは`runtime.Error`だけで、その他のpanicは再送出します。メモリー不足等の回復不能なランタイム障害の回復は保証しません。

## 回帰テストと動画

- 純Go全体テスト `go test ./...` が成功（internal/lrtl 148.746秒）。
- 関連race検査2回（10.885秒）、Go 1.23でCR3関連テスト3回反復（1.900秒）、`go vet`とCGOなしビルドが成功。
- インストール済み部品でCR3 76・TIFF系80・RW2系84・RAF 66、計306条件を再比較して成功（14.585秒）。
- `FuzzCR3Dimensions`は30秒指定で25,245入力が成功。箱・TIFFの境界、寸法範囲とCSVキャッシュ、入力データの不変を検査。
- 76条件の画像寸法→カメラ情報→XMP保存を比較し、正の寸法は円形マスクの取得結果も検査します。
- CLIプレビュー・保存プロファイル・元内容のバックアップ・再実行・原画像の保持を検査します。
- 不正な箱ヘッダー、親を超える長さ、不正な64-bit長、短いUUID、TIFF範囲外参照、寸法の個数不正、整数範囲外を保存前に拒否し、後続画像が不正でも連番を変更しないことを検査します。破損した箱全般に対する本体の回復動作との一致は未検証です。
- `mdat`を含む不透明なペイロードから寸法を誤採用しないこと、箱数上限を検査します。
- CLIのCR3プレビュー→AVIペイロードを抽出JPEG対照と比較します。
- CGOなしの実行ファイルで3フレームを処理し、保存寸法6000×4000、元画像・未知のXMP・バックアップ・プレビュー・再実行の保持を確認しました。露出を適用したAVIの復号RGB24はJPEG対照と全画素一致。64×32の3フレームのSHA-256は `3ade8b4df12b804cc382992b16379dd011e1af98bb42ad6e14e538b44b366ad4`。

動画比較はGoのCR3プレビューとJPEG入力の比較です。RAWデモザイクやCamera Raw／LRT現像との画素一致は示しません。

```powershell
go test ./...
go test -race ./internal/lrtl -run '^Test(CR3Dimensions|RawDimensions|RAFDimensions|RW2Dimensions|FrameMetadata|CaptureTime|EXIFOrientation|HolyGrailMetadata)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzCR3Dimensions$' -fuzztime 30s -parallel 4
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
$env:LRTL_ICC_EXIFTOOL='C:/Program Files/LRTimelapse 7/exiftool.exe'
go test ./internal/lrtl -run '^Test(CR3DimensionsInstalled|RAFDimensionsInstalled|RawDimensionsInstalled|RW2DimensionsInstalled)' -count=1
python scripts/probes/cr3_dimensions_fixture.py work/new-cr3-dimension-inputs
```

## 残る範囲

[IIQのExif寸法優先](lrt-iiq-dimensions-7.5.5.md)は後続実装で対応しました。独自maker noteの全取得は引き続き対象外です。

合成入力で測定したメタデータと保存前半が対象です。全カメラモデル、Canonの独自maker noteによる別の寸法、他のmajor brand、画像項目プロパティや別種のトラック、全破損入力の回復、製品ワークフロー全体のキャッシュ優先順位は未対応または未検証です。Phase Oneの独自寸法選択も残っています。CR3のRAW現像や色管理の完全一致を追加したものではありません。
