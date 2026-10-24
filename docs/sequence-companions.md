# 連番コピーのLRT付随ファイル

```powershell
lrtl copy-sequence C:\photos\source --output C:\photos\selected --ratings 4 --include-lrt-companions
lrtl copy-sequence C:\photos\source --output C:\photos\selected --ratings 4 --include-lrt-companions --write
```

`--include-lrt-companions`で、選択画像と採用中の外部XMPに加え、本体のコピー・移動処理が扱う付随データを新しいフォルダーへコピーします。既定は変更予定の表示で、`--write`で実行します。元の連番は変更しません。フラグを付けない従来のコピー対象も維持します。

## 追加するファイル

| 種類 | 名前の規則 |
| --- | --- |
| 同名JPEG | JPEG/JPG以外の画像に対し、同じ親にある`STEM.jpg` |
| 通常プレビュー | `.lrt/previews/STEM.lrtpreview` |
| Visual Preview | `.lrt/visual/STEM.lrtpreview` |
| プロキシ | `.lrt/proxy/STEM.proxy` |

`STEM`は最後の拡張子を除いたファイル名です。実際に存在するファイルだけを追加し、入れ子のキャッシュ階層、内容と更新日時を保持します。PNG／TIFF等もJPEG以外なので、同名JPEGが追加されます。`.jpeg`自体を選択した場合は別の`.jpg`を追加しません。

Go側のワークフローで設定を失わないよう、追加JPEGに専用の採用中XMPがあればそれもコピーします。共有XMPと共有キャッシュは一度だけコピーし、Windowsで`.JPG`という名前で保存されているJPEGも、実際の名前を保持します。

選択外の同名JPEGも追加されるため、選択フレーム数よりコピー後の画像数が増える場合があります。プレビューは画像・XMP・キャッシュの実数と全ファイル名を表示します。RAWとJPEGの両方があるコピー先で動画を作る場合は、必要に応じて`--filename-contains .nef`等で対象を絞ってください。Goの連番探索でペア画像を自動的に隠す機能は追加していません。

## 対象外とエラー

- `.lrt`全体、連番全体の設定、履歴・バックアップ、選択画像と無関係なキャッシュはコピーしません。全内容を維持したままフォルダーを改名する場合は`rename-sequence`を使えます。
- この機能はキャッシュの持ち運びです。GoレンダラーでLRTのプレビュー／プロキシを再利用する実装は追加していません。
- ファイルとして読めない付随データや、連番の外へ解決されるキャッシュリンクは、コピー開始前にエラーにします。シンボリックリンク自体もコピー対象にしません。
- コピー中に元ファイルのID・サイズ・更新日時・モードが変わった場合はエラーにし、未公開のコピー先を片付けます。既存の出力フォルダーは置き換えません。
- このコマンドはコピー専用です。移動には[move-sequence](move-sequence.md)、隣の`_REMOVED`への除外には[remove-sequence](remove-sequence.md)を使えます。これらも同じ付随ファイル判定を使います。

## 本体との比較範囲

インストール済みLRT 7.5.5の画像転送処理を調べ、画像・XMPに続いて同名JPEGと通常／Visual Preview／プロキシを扱う構造を確認しました。

独自の `scripts/probes/SequenceCompanionsOracle.java` は、実際のbasename・JPEG分類・キャッシュパスのヘルパーを呼びます。24種類のファイル名で実行が完了し、Goの名前・追加JPEGの対象判定・3キャッシュ名と一致します。日本語、大文字の拡張子、複数ドット、隠し名、拡張子なしも含みます。参照は `internal/lrtl/testdata/lrt-sequence-companions-7.5.5.json` です。

この比較は**パス計算のヘルパー**が対象です。本体GUIの画像転送全体の成功、衝突時の確認・上書き、JPEGに含めるXMPの全選択、連番設定の移行を比較したものではありません。Goは既存のサイドカー選択・新規出力・保存前検証を使います。製品メソッドやmain・ライセンスを置換せず、製品のコードやキャッシュデータを配布物へ含めません。

## 検証

- 純Go全体テスト `GOMAXPROCS=4 go test -p 1 ./...` が成功（internal/lrtl 154.137秒）。
- パス計算24条件の凍結参照と実機ヘルパーの再実行が一致（4.664秒）。通常テストは本体・Java不要。
- 通常／付随データ付きコピー、共有XMP・キャッシュ、JPEG分類、追加JPEGの専用XMP、Windowsの実際の`.JPG`名、内容・更新日時・原連番の保持を検査。
- 不正な付随ファイル、外部へのリンク、キャッシュのコピー失敗・コピー中の変更、出力衝突で未完成の出力を公開しないことを検査。
- 関連race検査2回（3.611秒）、Go 1.23で3回反復（2.166秒）、`go vet`、CGOなしビルドが成功。
- Linux用テスト実行ファイルをクロスコンパイルし、WSL Ubuntu 24.04の`/tmp`でコピー／付随ファイルの関連テストを各2回成功。
- `FuzzSequenceCompanionNames`は30秒指定で243,986入力が成功し、生成したキャッシュ名が`.lrt`階層を外れないことを検査。
- Windowsの実行ファイルで3 PNGを選択し、同名JPEG込みで6画像、6 XMP、9キャッシュをコピー。全内容・更新日時、元連番、プレビューを保持し、連番全体の設定を除外することを確認。選択PNGだけのAVI復号RGB24はコピー前後で全画素一致。64×32の3フレームのSHA-256は `32000cb2070c3f94d18bfa2456959c6af1221fdf8253a1401804c1b2282bd855`。

キャッシュのテストデータは独自の不透明なバイト列で、実LRTの有効なプレビュー形式との画素比較ではありません。動画比較はGoの選択画像の処理を確認するものです。

```powershell
go test -p 1 ./...
go test -race ./internal/lrtl -run '^Test(SequenceCompanions|CopySequence)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzSequenceCompanionNames$' -fuzztime 30s -parallel 4
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestSequenceCompanionsInstalledPaths$' -count=1
```
