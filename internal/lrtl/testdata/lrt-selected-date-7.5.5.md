# 選択列コピーの日時保存（LRT 7.5.5）

## 対応範囲

`copy-settings --properties NAMES` の既定 `--save-state lrt-7.5.5` で、対象XMPの根の `xmp:CreateDate` を撮影日時キャッシュとして維持します。コピー元の日付は取り込みません。登録列のコピー・保存準備後、対象キャッシュが空でなく、最終 `crs:HasSettings` が大文字小文字を無視して `true` の場合、次を反映します。

- `exif:DateTimeOriginal`、`xmp:CreateDate`、`xmp:ModifyDate`、`photoshop:DateCreated` を対象キャッシュへ揃える。
- 実際に変更を保存する場合だけ、`xmp:MetadataDate` を実時計へ更新する。書式は既定initと同じ秒単位・compact offset。
- `xml:lang` 等の既存単純値の修飾子は保持する。

`HasSettings` を選択列に含めると、コピー後の設定で判定します。空・欠損の対象日時、無効なHasSettingsでは撮影日時4項目とMetadataDateを変更しません。`--save-state preserve` はこの保存副作用を適用しません。

プレビューでは時計を呼びません。変更不要な再実行も保存せず、時計・バックアップを増やしません。保存計画の全出力でMetadataDateの構造を確認してから、複製パケットへ時計を反映します。後続フレームが複合値エラーの場合、先行サイドカーもバックアップも書きません。OSの書込失敗時に保存済みファイルを一括ロールバックする機能ではありません。

## 実機観測

独自の `scripts/probes/SelectedDateOracle.java` を使用しました。製品のメソッド、mainインスタンス、ライセンス状態を置換していません。SDKで対象CreateDateを読み、専用の公開setterへ渡してロード済みフレームの日時キャッシュを与えます。汎用登録列ローダーはこの先頭文字列スロットを読み込まないため使用しません。対象と異なる日付を持つコピー元から、指定した登録キャッシュセルだけをコピーし、対象日時キャッシュが変わらないことをgetterで確認します。

640条件は次の直積です。

| 条件 | 値 |
| --- | --- |
| 対象日時5種 | 欠損、空、n/a、端数秒・オフセット付き文字列、修飾子付き文字列 |
| 対象HasSettings 2種 | False、True |
| コピー元HasSettings 2種 | False、True |
| 選択4種 | Exposure2012、HasSettings、両方、WhiteBalance |
| MetadataDate 4種 | 欠損、単純値、修飾子付き単純値、構造体 |
| Quality 2種 | RAW、FLAT |

保存処理は、履歴処理が未生成mainインスタンスへ到達する地点まで観測します。592件はその地点へ達し、48件はそれより前に構造体MetadataDateの値設定でエラーになります。時計の分岐を決定的に比較するため、公開フォーマッター設定には固定マーカーを与えます。既定時計の書式・実時計観測は [MetadataDateの観測](lrt-metadata-date-7.5.5.md) を参照してください。

Goは592件について全ルートの意味内容を比較し、48件についてエラーを確認します。履歴名前空間は観測XMLから除いています。エラー文面や全保存完了の一致は主張しません。

## 検証結果

- 純Go全体テスト成功（internal/lrtl 129.959秒、追試119.285秒）。関連race検査2回、Go 1.23で3回反復、go vet、CGOなしビルドも成功。
- インストール済みJARから640条件を再生成し、保存参照と完全比較。
- `FuzzSelectedDateCopy`は30秒指定で262,900回成功。
- ビルドした実行ファイルで2枚の64×32 JPEGを処理。対象日時4項目、実時計範囲、修飾子、プレビュー・再実行・画像・コピー元・正確なバックアップの保持を確認。露出・日時字幕入りAVIの復号RGB24は、同じ条件を明示したGoレンダラーの対照入力と全画素一致。SHA-256：`a97b85567a3175dba7d5e7a47840de7b1ca180ffc776f15ffd43affc0e094af8`。これはLRT/Adobeの動画出力との画素一致を示すものではありません。

## テスト項目・再実行

- 固定640条件の比較とインストール済みJARによる再生成比較。
- CLIのプレビュー無変更、実時計範囲、対象日時の維持、正確なバックアップ、コピー元・画像の保持、再実行無変更、後続フレームの複合値エラー時の全ファイル保持。
- 時計を使う前の全出力検証、出力準備による入力パケット不変、各対象の時計更新、無効日時・無効設定・空計画で時計を使わないこと。
- `FuzzSelectedDateCopy`：失敗したコピーの入力不変、対象日時キャッシュ保持、元パケットを変えない保存準備、固定時計、コピー元不変。

```powershell
go test ./...
go test -race ./internal/lrtl -run 'Test(SelectedDate|CopySettings|RegisteredSave|MetadataDate)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzSelectedDateCopy$' -fuzztime 30s -parallel 4
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestSelectedDateInstalledReplay$' -count=1
```

## 残る範囲

`--properties` を省略した全体貼付けの日時再初期化は未接続です。画像から日時を再取得する全経路、キャッシュのロード経路全体、履歴・InstanceID・製品の保存完了は未再現です。汎用WritePacketや他の編集コマンドへ時計更新を広げたものではありません。
