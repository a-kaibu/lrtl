# RAFの画像寸法とネイティブ保存キャッシュ（7.5.5）

## 対応した処理

`init` の既定 `--frame-state lrt-7.5.5` で、Fujifilm RAFの寸法を読み、`lrt:Width`／`lrt:Height`へ保存します。円形マスク補間の画像寸法フォールバックにも同じ読込を使います。`.RAF`のような大文字拡張子も対象です。既存XMPに補間用の寸法がある場合の優先順位は維持します。

これはメタデータの寸法です。`render --raw-preview`が使う埋め込みJPEGの画素寸法を変更しません。例えば64×32のプレビューを持つRAFでも、RAWクロップが6000×4000なら保存メタデータは6000×4000です。`--frame-state preserve`では従来どおり寸法を追加・更新しません。

| 条件 | 寸法キャッシュの規則 |
| --- | --- |
| JPEGプレビューの寸法が取得できる | プレビューの幅・高さを基準にする |
| RAFディレクトリの`0x111`に2個の16-bit整数がある | 高さ・幅の順を読み、クロップ寸法を優先する |
| `0x100`のセンサー全体寸法だけがある | プレビュー寸法を維持する |
| 複数のディレクトリ／同名タグ | 最初のクロップ値を優先する |
| 最初のクロップが空 | プレビュー寸法へ戻り、後の重複値を採用しない |
| クロップが整数1個だけ | 本体のImageSize文字列はペアにならず、CSVキャッシュの両軸は0 |
| クロップの幅・高さの一方が0 | キャッシュに0を保持し、保存では正の軸だけ更新する |
| JPEGの欠損・空・寸法取得不能 | クロップがあっても寸法キャッシュは0 |
| RAFディレクトリの項目数が256以上 | そのディレクトリを採用しない |

RAFのディレクトリポインターはヘッダーの92／120バイト位置を参照し、JPEG開始位置以降のヘッダーフィールドは読みません。宣言されたディレクトリ長は境界として使わず、ファイル実体の範囲を検査します。クロップに余分な整数や末尾の1バイトがあっても、先頭の2整数を使います。プレビュー位置のbit 15が立つ条件では、測定対象のExifToolがファイルを拒否し、寸法は取得できません。

## 実機との比較

`scripts/probes/raf_dimensions_fixture.py`で生成した、独自の合成RAF **66条件** を `lrt-raf-dimensions-7.5.5.json` に固定しています。JPEGは既存RW2比較で使った独自の単色画像です。実カメラ画像や製品リソースを含みません。

通常・縦長・最大値・ゼロ・欠損・重複・部分値、255／256項目の境界、2ディレクトリ、宣言長の不一致、JPEG位置、JPEG内EXIF、両バイト順のFujiIFDとの競合を含みます。

独自の `RawDimensionsOracle.java` がインストール済みExifToolの `ImageSize` を取得し、その文字列を実際のLRT CSVメタデータ読込とカメラ情報保存へ渡します。66条件のキャッシュと保存パケットがGoと一致します。履歴処理の未初期化main参照で停止する保存前半までの比較で、アプリケーション全体の保存完了を示しません。main・ライセンス・製品メソッドを置換していません。

bit 15が立つ2入力ではExifToolの終了値が1になります。その場合だけ明示的な `--allow-exiftool-errors` で、全入力の出力行があることを検査して観測を続けます。終了値も参照データへ記録します。`-`を渡したCSVキャッシュ／保存前半の観測であり、ExifToolが失敗した場合の製品ワークフロー全体の処理は未検証です。

## 回帰テストと動画

- 純Go全体テスト成功（internal/lrtl 165.507秒）。Go 1.23のRAF関連テスト3回反復、`go vet`、CGOなしビルドも成功。
- RAF／既存RAW寸法／プレビュー／円形マスクの関連race検査2回が成功（240.099秒）。
- インストール済み部品でRAF 66・既存TIFF系80・RW2系84、計230条件の寸法／保存前半を再比較し成功（11.294秒）。
- `FuzzRAFDimensions`は30秒指定で8,651入力が成功。寸法の範囲、CSVキャッシュ、JPEG抽出の整合性、入力データの不変を検査。
- 通常テストでは凍結参照とGoを比較し、本体・ExifTool・Javaを必要としません。
- RAF画像寸法→カメラ情報→XMP保存、および円形マスクの寸法取得を検査します。
- CLIのプレビュー、保存、正確なバックアップ、再実行、原画像の保持、`preserve`を検査します。
- 破損ヘッダー・範囲外のJPEG／ディレクトリ・切れた項目・範囲外データの6種類を拒否し、後続画像が不正でも連番を保存しないことを検査します。これらはGoの保存前検査です。本体の警告・回復動作との一致は主張しません。
- RAFプレビューからのAVIペイロードを、抽出したJPEGからの対照と比較します。
- CGOなしの実行ファイルで3フレームを初期化し、保存寸法6000×4000／4000×6000／64×32、未知のXMP・原画像・バックアップ・プレビュー・再実行の保持を確認しました。露出値を適用したAVIをFFmpegでRGB24へ復号し、JPEG対照と全画素一致。3×64×32フレームのSHA-256は `3ade8b4df12b804cc382992b16379dd011e1af98bb42ad6e14e538b44b366ad4`。

この動画比較はGoのRAFプレビューとJPEG入力の比較です。RAWデモザイクやCamera Raw／LRT現像との画素一致は示しません。

```powershell
go test ./...
go test -race ./internal/lrtl -run '^Test(RAFDimensions|RawDimensions|RW2Dimensions|RawPreview|Radial)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzRAFDimensions$' -fuzztime 30s -parallel 4
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
$env:LRTL_ICC_EXIFTOOL='C:/Program Files/LRTimelapse 7/exiftool.exe'
go test ./internal/lrtl -run '^Test(RAFDimensionsInstalled|RawDimensionsInstalled|RW2DimensionsInstalled)' -count=1
python scripts/probes/raf_dimensions_fixture.py work/new-raf-dimension-inputs
```

## 残る範囲

合成ヘッダーで観測した寸法と保存前半が対象です。全カメラモデル、M-RAW内の複数画像、独自maker noteによる寸法の上書き、壊れたJPEGの回復、RAFのカメラ露出・日時・WB情報の全取得、RAW現像の完全一致は未対応または未検証です。[CR3の寸法](lrt-cr3-dimensions-7.5.5.md)は後続実装で対応しました。Phase One独自の寸法選択も残っています。
