# 全体貼り付けの日時再読込（LRT 7.5.5）

## 変更

`copy-settings SOURCE DIRECTORY` の既定 `--save-state lrt-7.5.5` は、XML統合後の撮影日時とHasSettingsを再読込してから登録列の保存準備を行います。`--properties` のある選択列コピーとは読込条件が異なります。

| 条件 | 全体貼り付け | 選択列コピー |
| --- | --- | --- |
| 日時入力 | 統合後の根のCreateDate。欠損時だけ根のDateTimeOriginal | コピー前の対象CreateDateキャッシュ |
| 空・欠損 | 選んだ値が空／両方欠損ならn/a。空CreateDateはOriginalへフォールバックしない | 空・欠損なら日時保存をしない |
| 文字列処理 | ネイティブ日時キャッシュの区切り変換・長さ制限・サブ秒2桁化 | 対象値をそのまま維持 |
| HasSettings | 再読込の既定値・Boolean判定を反映 | 選択した列だけコピー |
| 時計更新 | 保存が必要かつ最終HasSettingsが文字列trueなら更新 | 非空日時かつ最終HasSettingsが文字列trueなら更新 |

日時キャッシュの文字列処理は [撮影日時キャッシュ](lrt-capture-date-cache-7.5.5.md) と同じ関数を使います。新たに原画像のEXIFを照会する処理ではありません。Calendarの検証も行わず、n/aや任意文字列も保持します。XML統合の非Camera Raw値の既存優先は維持します。

再読込では、欠損・空のHasSettingsを `True` で補います。Booleanとして偽の値は `true` に直します。`true/t/on/yes`（大文字小文字不問）や32-bit範囲内の符号付き非ゼロ整数は保持します。保存時に日時更新を有効にする条件は文字列trueなので、`1/yes/t/on` などを残した場合は撮影日時4項目とMetadataDateを更新しません。整数の範囲外・余分な符号・前後空白を区別します。Javaが認めるBMPの10進数字も扱います。

有効な日時保存では、DateTimeOriginal/CreateDate/ModifyDate/Photoshop DateCreatedの4項目をキャッシュへ揃え、実際に保存する際だけMetadataDateを実時計へ更新します。修飾子は保持します。プレビュー・変更不要な再実行は時計を更新しません。`--save-state preserve` はXML統合だけを行います。

## 実機観測の範囲

独自の `scripts/probes/FullPasteDateOracle.java` が、現在インストールされている7.5.5の実際の全体貼り付けメソッド `cOM7.A(tQa,tQa,boolean,boolean)` を呼びます。古い別バージョンの解析結果は根拠にしていません。

コンストラクターを省略した観測用controllerへ、製品の本物のtable modelとsequenceを持たせます。製品のグローバルmain/controller、ライセンス、メソッドは差し替えません。対象フレームには架空のNEFパスとファイル名キャッシュを与え、合成XMPを渡します。

実際の呼出しは、XML統合、マスク・既定値の準備、登録キャッシュ読込、撮影日時キャッシュの代入を通過し、未生成の `LRTimelapseMain.N.s` を参照する地点で停止します。各件でその停止理由を検査し、対象日時のgetterとHasSettingsの登録セルを記録します。JVMの同一例外省略で停止理由を失わないよう、再実行では `-XX:-OmitStackTraceInFastThrow` を付けます。

| 観測 | 内容 |
| --- | --- |
| 日時・フラグ直積2,048件 | 対象CreateDate 8種 × 対象Original 4種 × コピー元CreateDate 4種 × コピー元Original 2種 × コピー元HasSettings 8種 |
| HasSettings境界32件 | 符号・ゼロ・非ゼロ・int32境界・範囲外・全角数字・yes/on/t・Unicodeのlong s・空白・余分な符号・小数 |

CreateDateは欠損、空、n/a、EXIF区切り、ISOのサブ秒・オフセット、修飾子付き、UTF-16の位置が異なる絵文字入りを含みます。2,080件の日時キャッシュとHasSettingsキャッシュをGoと比較します。

**この観測は全体貼り付けや保存が完了したという証拠ではありません。** 保存時の4項目と実時計の条件は、別途観測した [日時保存](lrt-selected-date-7.5.5.md)・[MetadataDate](lrt-metadata-date-7.5.5.md) の規則と組み合わせて実装しています。今回の参照で全マスク・全既定値・履歴までの意味内容が一致したとは主張しません。

## テスト

- 実機2,080件の固定参照比較と、インストール済みJARからの再生成比較。
- コピー元不変、既存値優先、修飾子保持、最終フラグ別の日時保存・時計更新条件、日時不明のn/a・Originalへのフォールバック。
- CLIのプレビュー、保存時刻範囲、正確なバックアップ、画像・参照XMPの保持、再実行無変更、preserve指定。
- 後続フレームの構造体MetadataDateで、プレビュー／保存とも全サイドカーとバックアップを保持してエラーを返す。
- 登録保存のfuzzへ2,080件をシード追加。失敗したコピーの原パケット不変、コピー元不変、出力XMLの読み戻し、保存準備の安定性を検査。

純Go全体テスト成功（internal/lrtl 126.518秒、最終追試133.635秒）。関連race検査2回、Go 1.23で3回反復、go vet、CGOなしビルドも成功しています。`FuzzNativeSaveState` は30秒指定で457,372回、境界条件の追加後の追試で272,245回成功しました。

ビルドした実行ファイルで64×32 JPEGを2枚処理し、Falseのコピー元からの有効化、統合後日時の正規化、保存時刻範囲、修飾子、プレビュー・再実行・画像・コピー元・正確なバックアップの保持を確認しました。露出・日時字幕入りAVIの復号RGB24は、同じ条件を明示したGoレンダラーの対照と全画素一致です（SHA-256：`55ef0491fb891def7717ce08f8eb93946cef6250cb8a86cb0fbe6f5eb272fb40`）。この比較はLRT/Adobeの動画自体との画素一致を示しません。

```powershell
go test ./...
go test -race ./internal/lrtl -run 'Test(FullCopyDate|SelectedDate|CopySettings|RegisteredSave|SaveState|NativeSave|MetadataDate)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeSaveState$' -fuzztime 30s -parallel 4
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestFullCopyDateInstalledReplay$' -count=1
```

## 残る差

全体貼り付け後の[マスク準備・旧形式変換](lrt-mask-preparation-7.5.5.md)にも対応しました。全既定値の構成、特殊なレイアウト・全内部キャッシュの再読込、製品GUIのdirtyフラグ・履歴・InstanceID更新と保存完了は未再現です。Goは変更不要な再実行の保存を省略します。複合値などの内容エラーを保存前に検出しますが、OSの書込失敗で保存済みファイルを一括ロールバックする機能ではありません。
