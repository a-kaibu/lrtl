# Phase One IIQの寸法・XMP・プレビュー（LRT 7.5.5）

## 今回の対応

TIFF構造のIIQで、`init`の寸法保存と円形マスク補間にExif寸法の優先規則を追加しました。標準TIFFのJPEG位置・長さを持つIIQを `--raw-preview` で解析・動画化できます。既存の連番探索、取り込み、RAW現像バックエンドへの入力にも接続します。大文字の`.IIQ`にも対応します。

| 条件 | 観測した寸法選択 |
| --- | --- |
| ImageWidth／Heightがあり、ExifImageWidth／Heightが両方非ゼロ | IIQではExifのペアを優先 |
| 同じ内容を`.tif`として読む | 通常のTIFF寸法を使う |
| Makeが`Phase One`だが拡張子は`.tif` | IIQへの切替条件にはならない |
| Exif寸法が片方欠損、または片方0 | 元のTIFF寸法を維持 |
| TIFF寸法が欠け、Exif寸法だけがある | ImageSizeが作れず、寸法は保存しない |
| 複数のExif寸法ペア | 格納順で後のExif値を採用 |
| 有効なExifペアが32-bit符号付き整数範囲外 | 本体CSVと同じく保存前エラー |
| 範囲外Exif値があるがペアにならない | 未使用の値で通常寸法を上書きしない |
| IIQ内のBigTIFF | 今回の観測では寸法を採用しない |

両バイト順、通常／本画像SubIFDとの競合、ゼロ・欠損、70000以上の寸法、整数境界、Make、プレビューを含む **52条件** をインストール済みExifToolのImageSizeと本体CSV読込・保存前半で比較しました。50条件の結果が一致し、2条件は両方で整数範囲エラーです。`lrt-iiq-dimensions-7.5.5.json`に独自の入力と観測値を保存しています。

## XMPの採用元

完全な本体ファイル読込 `nNa.A(File)` を、両バイト順×埋め込みのみ／古いサイドカー／新しいサイドカーの **6条件** で観測しました。今回のIIQでは埋め込みXMPを採用せず、サイドカーがあれば更新日時によらずそちらを採用します。結果は `lrt-iiq-xmp-source-7.5.5.json` に固定しています。

既定の `init --frame-state lrt-7.5.5` はこの規則に従います。サイドカーがなければ、画像内の現像値を初期化の入力には使いません。

一方、通常のGo読込と `--frame-state preserve` は、サイドカーがなければIIQ内のTIFF tag 700のXMPも読めるようにしました。`render`やコピー元XMPの読込でも使えます。これはGo側の読込拡張です。両バイト順の埋め込みXMPを、インストール済みExifToolの抽出結果とも比較しています。

## 検査方法

独自の `scripts/probes/iiq_dimensions_fixture.py` がTIFFメタデータ、独自の単色JPEG、独自のXMPから合成入力を生成します。実カメラ画像や製品のコード・リソースを含みません。通常テストに本体・Java・ExifToolは不要です。

寸法の比較は、既存の `RawDimensionsOracle.java` が本体CSV読込と保存を呼び、履歴処理の未初期化main参照で停止する前までを観測します。製品ワークフロー全体の保存完了は示しません。XMP採用元の6条件は `XMPSourceOracle.java` によるファイル読込完了までの観測です。main・ライセンス・製品メソッドの置換は行っていません。

- 寸法→カメラキャッシュ→XMP保存、円形マスク用の寸法取得。
- IIQのプレビュー抽出、埋め込みXMP読込、大文字拡張子、既定／preserveの初期化。
- CLIプレビュー・正確なバックアップ・再実行・原画像・未知のXMPの保持。
- 後続IIQの範囲外IFDや寸法範囲外で、連番の全ファイルを保持して保存前エラー。
- IIQプレビューと抽出JPEGの対照でAVIペイロード一致。
- 純Go全体テストが成功（internal/lrtl 147.332秒）。初回は他の検査との並列実行中、既存RAW現像テストの補助プロセス起動がWindowsのリソース不足で失敗しました。他の検査終了後に`GOMAXPROCS=4`、`go test -p 1 ./...`を単独実行して全体成功を確認しています。
- 関連race検査2回（26.607秒）、Go 1.23でIIQ／採用元テスト3回反復（6.870秒）、`go vet`、CGOなしビルドが成功。
- IIQ 52条件と既存RAW寸法306条件、IIQのXMP採用元6条件と既存採用元76条件、埋め込みXMP2条件のインストール済み部品との再比較が成功（27.729秒）。
- `FuzzIIQDimensions`は30秒指定で6,373入力が成功。寸法・CSVキャッシュ、プレビュー抽出、原画像の保持を検査。
- CGOなしの実行ファイルで両バイト順を含む3フレームを処理。保存寸法6000×4000、プレビュー寸法64×32、サイドカーの露出と未知のXMP・バックアップ・原画像・再実行を確認。AVI復号RGB24はJPEG対照と全画素一致。SHA-256: `3ade8b4df12b804cc382992b16379dd011e1af98bb42ad6e14e538b44b366ad4`。

この動画比較はGoのIIQプレビューとJPEG入力の比較です。Phase One／Camera Raw／LRTのRAW現像画素との一致は示しません。

```powershell
go test ./...
go test -race ./internal/lrtl -run '^Test(IIQ|XMPSource|RawDimensions|CR3Dimensions|RAFDimensions|RW2Dimensions)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzIIQDimensions$' -fuzztime 30s -parallel 4
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
$env:LRTL_ICC_EXIFTOOL='C:/Program Files/LRTimelapse 7/exiftool.exe'
go test ./internal/lrtl -run '^Test(IIQ.*Installed|XMPSourceInstalled|CR3DimensionsInstalled|RAFDimensionsInstalled|RawDimensionsInstalled|RW2DimensionsInstalled)' -count=1
python scripts/probes/iiq_dimensions_fixture.py work/new-iiq-dimension-inputs
```

## 残る範囲

標準TIFF構造で測定した寸法・XMP・JPEGプレビューが対象です。Phase One独自maker note内の寸法・プレビュー、全カメラモデル、EIP（ZIPパッケージ）、各種壊れたTIFFの本体回復動作は未対応または未検証です。既存の外部RAW現像バックエンドを置き換えず、RAWデモザイク・色管理の完全一致も追加していません。
