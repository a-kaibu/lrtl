# 撮影日時キャッシュとinitの日時更新（LRT 7.5.5）

## 動作変更

既定の `init --frame-state lrt-7.5.5` で、画像から取得した日時を実機のCSV読込方式で変換し、根の次の4項目へ反映します。

- `exif:DateTimeOriginal`
- `xmp:CreateDate`
- `xmp:ModifyDate`
- `photoshop:DateCreated`

**EXIF日時がある画像では、既存XMPの日時が置き換わります。サブ秒が小数2桁に切り捨てられ、タイムゾーンが除かれる場合があります。** 従来どおり既存日時を保持して初期化するには `--frame-state preserve` を指定します。画像自体は変更せず、保存先はバックアップ付きサイドカーです。

## 実機観測

独自の合成データと観測器で、インストール済みLRT 7.5.5の以下の処理を呼び出しました。GUI・main・ライセンス状態の置き換えや製品メソッドの差し替えはありません。

| 観測 | 入力・件数 | 検査範囲 |
| --- | --- | --- |
| `CaptureDateCacheOracle.java` | 主日時10種×サブ秒11種×初期値4種＝440条件 | 完了したCSVメタデータ読込と日時文字列キャッシュ |
| `CaptureDateImageOracle.java` | EXIFを持つJPEG/PNG/TIFF/DNG 264画像＋画像内XMP 45画像＝309画像。それぞれ初期値あり／なし | インストール済みExifToolのDateTimeOriginal/CreateDate/SubSecDateTimeOriginal列→実機CSV読込 |
| `CaptureDateSaveOracle.java` | RAW/FLAT×HasSettings 2種×日時5種＝20条件 | 保存の履歴処理前に行う日時4項目の書込 |

最後の観測は、履歴が未生成mainインスタンスへ到達した時点で停止します。完全な保存完了とはしていません。時刻依存の `MetadataDate` を観測XMLから取り除き、実機で書かれたかどうかを別途記録しています。Goとの保存比較は上記4項目に限ります。

画像・XMLは独自の合成データです。生成器は `capture_date_image_fixture.py`。既存の独自XMP入力を再利用する45画像を含み、製品コード・撮影データ・実機バイナリは収録しません。

## 読込規則

| 条件 | 実機とGoの対応 |
| --- | --- |
| 主日時あり | 既存キャッシュ値を主日時で置換 |
| 長さ22以上で19文字目の次に小数点があるSubSecDateTimeOriginal | 先頭22 UTF-16単位を採用。最後が`+`／`-`／`Z`ならその文字を`0`へ置換 |
| サブ秒1桁でオフセットなし（全長21） | サブ秒列を採用せず主日時へ戻る |
| `.1+09:00`など | `.10`となる。`.100`も`.10`となる |
| 長い日時文字列 | 先頭22 UTF-16単位へ切り詰め。丸めない |
| コロン区切りの日付 | 固定位置の区切りを変換して`YYYY-MM-DDTHH:mm:ss`形式へ |
| 主日時欠損・既存キャッシュあり | 既存値を保持。初期値は根の`xmp:CreateDate`で、タイムゾーンや精度もそのまま保持 |
| 主日時欠損・既存キャッシュなし | 画像のCreateDateをフォールバックに使う |
| 画像のEXIF日時なし | 対応する画像内XMPのDateTimeOriginal/CreateDateも参照 |

ExifToolの名前付きタグ取得は、Adobe SDKのDateTimeDigitized→CreateDate別名処理とは異なります。画像内の元の項目名で取得し、キャッシュ初期値は従来のSDK式別名処理を適用したXMPから取ります。ここを混同しないよう、別名だけの画像も実機入力に含めています。

一般的な撮影日時読込・`timeshift`はこのキャッシュの切り詰めを使いません。今回の動作変更は既定initのフレーム準備です。日時文字列キャッシュは暦の妥当性検査ではなく、実機と同様に不正な文字列をそのまま保持する場合があります。

## 実装と残る差

元のサブ秒の桁数・末尾ゼロを失わないよう、EXIFをtime.Timeへ変換する前のASCII文字列として読みます。JPEG APP1、PNG eXIf（CRC検査付き）、TIFF系のIFDを純Goで読み、文字列長256、参照深さ32、既存のディレクトリ数・範囲上限を適用します。TIFF読込をReaderAtへ共通化し、PNGの向き読込と日付読込は同じサイズ制限・CRC検査を使います。

画像日時も既存CreateDateも取得できないときは、initは既存日時を保持し、実機キャッシュの`n/a`を4項目へ強制保存しません。壁時計のMetadataDate・履歴更新、完全なフレーム再初期化／キャッシュ再読込条件、CR3/RAF等のコンテナー固有EXIF、複数の画像内メタデータからのExifTool全優先順位、破損データの全許容挙動は未完・未確認です。今回の一致をアプリ全体の保存・日時処理の一致とはしていません。

## テスト

- 440キャッシュ条件、309画像の初期値有無、20保存条件を保存参照と比較。実機・インストール済みExifToolでも再実行。
- 純Go全体テスト、race検査、Go 1.23での反復、`go vet ./...`、CGOなしビルド。
- `FuzzCaptureDateImage` 10,000回：画像を変更しないこととキャッシュ長を検査。
- initのプレビュー無変更、日時4項目、既存CreateDateの優先、preserve指定、未知のメタデータ保持、原画像保持、正確なバックアップ、再実行無変更を検査。
- 不正な後続EXIFで保存前中止。日時文字列長、参照深さ、PNG CRC、JPEGセグメント境界も検査。
- ビルドした実行ファイル：5枚の64×32 JPEG/PNGで初期化・LRT形式SRT・時刻入りAVIを生成。更新後の日時4項目が実機観測と一致し、時刻表示が更新前から変わり、実機日時を入力した対照動画と復号RGB24が全画素一致。5フレームのSHA-256は `91f14c19a78b554f250eab08d4896cb8c37dc05ea9fc2660a6cf1a3f342c4db6`。

動画の対照は実機が採用した日時をGoレンダラーへ渡したものです。LRT/Adobeの動画出力自体との画素一致は示しません。

## 再実行

```powershell
python scripts/probes/capture_date_image_fixture.py work/new-capture-dates 'C:/Program Files/LRTimelapse 7/exiftool.exe'
go test ./internal/lrtl -run TestCaptureDate -count=1
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
$env:LRTL_EXIFTOOL='C:/Program Files/LRTimelapse 7/exiftool.exe'
go test ./internal/lrtl -run TestCaptureDateInstalledReplay -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzCaptureDateImage$' -fuzztime 10000x -fuzzminimizetime 1x
```
