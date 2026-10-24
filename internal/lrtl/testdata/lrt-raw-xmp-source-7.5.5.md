# RAW形式別のXMP採用元（LRT 7.5.5）

## 既定初期化の修正

`init --frame-state lrt-7.5.5`が初期化の入力として採用する画像内XMPを、JPEG／DNG／TIFFに限定しました。以前はGoが読めるNEF・CR2・ARW・ORF・RW2・RAF・CR3等の画像内XMPも使っていましたが、今回の本体ファイル読込観測では採用されませんでした。

| 形式 | サイドカーがある場合 | サイドカーがない場合 |
| --- | --- | --- |
| JPEG／DNG | 画像より新しい場合だけサイドカー。それ以外は画像 | 画像内XMP |
| TIFF | 更新日時によらずサイドカー | 画像内XMP |
| 観測したその他のRAW／PNG | 更新日時によらずサイドカー | この本体ローダーはパケットを返さない |

画像とサイドカーの比較はJavaのミリ秒時刻です。拡張子の大文字も比較し、JPEGヘッダーを持つNEF名のファイルでも画像内パケットを採用しないことを確認しました。

既定初期化では、その他のRAWにサイドカーがなければ新しいメタデータから始めます。埋め込み現像値や独自項目はその初期化の入力には使いません。画像のEXIF寸法・撮影情報を別途読む処理とは独立した規則です。

通常のGo読込と `--frame-state preserve` は、対応済み形式の埋め込みXMPを読む従来の動作を維持します。これはGo側の読込拡張です。`render`、コピー元の設定読込、日時等の各処理を一律に本体ローダーの対象へ制限する変更ではありません。初期化で埋め込み設定を使いたい場合は、最初の保存前に`preserve`を指定してください。元画像はどちらのプロファイルでも変更しません。

## 完了した本体ファイル読込の観測

独自の `scripts/probes/raw_xmp_source_fixture.py` が、独自のTIFF／ORF／RW2／RAF／CR3構造、独自のJPEG／PNGとXMPから合成入力を作ります。実カメラ画像や製品のコード・リソースを含みません。

**29拡張子、両バイト順や大文字等を含む60入力形式 × 7サイドカー条件 = 420条件** を観測しました。サイドカー条件は欠損、古い／同時刻／新しい正常XML、古い／同時刻／新しい不正XMLです。

TIFF系の拡張子はNEF、NRW、ARW、SR2、PEF、ERF、ORF、DCR、RW2、RWL、RAW、IIQ、3FR、FFF、DCS、KDC、MEF、MOS、SRW、SRF、CR2、DNG、TIF、TIFF。それにJPG、JPEG、PNG、RAF、CR3を加えています。大文字6種と、JPEGをNEF名で読む条件も含みます。各入力には画像内XMPがあり、単にXMPがないためにnullとなる入力だけを並べた比較ではありません。

独自の `XMPSourceOracle.java` が、実際の `nNa.A(File)` を呼び、結果を本体のSDKでシリアライズします。**420条件すべてでファイル読込メソッドが完了**しています。main・ライセンス・製品メソッドを置換せず、履歴処理の停止位置から結果を推測する観測でもありません。後続の全キャッシュ再読込や保存完了は、この観測の対象外です。

入力、実際のファイル時刻、サイドカー内容、返されたXMPを `lrt-raw-xmp-source-7.5.5.json.gz` に固定しました。通常テストに本体・Java・Pythonは不要です。

## 一致範囲と厳密エラーの差

252条件の読込結果がGoと一致します。残る168条件は、選択された不正サイドカーを本体がログへ記録してnullを返す条件です。Goは従来どおり解析エラーを返し、後続フレームで不正XMLを見つけた場合も保存前に全ファイルを保持します。

JPEG／DNGで時刻規則により選択されない不正サイドカーは読みません。RAWで無視する画像内XMPの解析エラーを、初期化の入力エラーとして発生させることもありません。

## 検証

- 純Go全体テスト `GOMAXPROCS=4 go test -p 1 ./...` が成功（internal/lrtl 151.231秒）。
- 420条件の本体参照、168条件の厳密エラー差、読込前後の全ファイル不変。
- NEF／CR2／ARW／ORF／RAF／CR3の6形式、各3フレームを既定／preserveで初期化。画像内の露出と独自プロパティの採用有無、原画像、プレビュー、再実行を検査。
- 各形式・プロファイルで、AVIペイロードを明示した露出0／1のJPEG対照と比較。プロファイル間で実際に画素が異なることも検査。
- NEF／RAF／CR3の後続フレームに不正サイドカーがある場合、書込前に全ファイルを保持して失敗。
- 関連race検査2回（16.435秒）、Go 1.23で3回反復（11.691秒）、`go vet`、CGOなしビルドが成功。
- 本体ファイル読込の新規420・既存76・IIQ 6条件を再実行し、凍結した結果全体が一致（10.674秒）。
- `FuzzRawXMPSourcePolicy`は30秒指定で33,857入力が成功。拡張子・プロファイル・任意画像データからの採用、XML読戻し、入力不変を検査。
- CGOなしの実行ファイルでNEF／RAF／CR3、各3フレーム×2プロファイルを確認。AVI復号RGB24は、明示した露出のJPEG対照と全画素一致。原画像、プレビュー、再実行も保持。

各形式で得た64×32の3フレームのSHA-256:

| プロファイル | 復号RGB24 |
| --- | --- |
| 既定（画像内の露出1を不採用） | `6bb0a959695b1b8d272f9fceaf3de8f0ea5a95e5b0b5eb632e61961df4c24138` |
| preserve（画像内の露出1を採用） | `081d4a4502b633118112b6ca1194052412dc142fd43c9415e40f5e55633808f1` |

動画比較はGoのRAWプレビューとJPEG対照です。LRT／AdobeのRAW現像画素との一致を示しません。

```powershell
go test -p 1 ./...
go test -race ./internal/lrtl -run '^Test(RawXMPSource|XMPSource|IIQSource)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzRawXMPSourcePolicy$' -fuzztime 30s -parallel 4
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^Test(RawXMPSourceInstalled|XMPSourceInstalled|IIQSourceInstalled)' -count=1
python scripts/probes/raw_xmp_source_fixture.py work/new-raw-source-inputs
```

## 残る範囲

測定したヘッダーと同名stemサイドカーに対するファイル読込が対象です。全カメラモデル、全壊れたコンテナーの回復、全ての拡張子と実内容の不一致、別名サイドカーや複数候補の本体全選択、ファイル読込後の全キャッシュ・履歴・保存ワークフローの再現は未完です。Goが埋め込みXMPを読めるコンテナーの追加とは別の規則です。
