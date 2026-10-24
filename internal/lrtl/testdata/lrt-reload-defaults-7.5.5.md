# 全体コピーの現行形式既定値（LRT 7.5.5）

## 変更

既定の `copy-settings SOURCE DIRECTORY` で、マスク準備の後、統合済みXMPへ現行形式の既定値を補ってから登録列の保存を行います。対象画像のファイル名と統合済みQualityを使い、Camera Rawの基本調整・HSL・カラーグレーディング・シャープネス・ノイズ低減・レンズ・変形・グレーミキサー等を独立したGoコードで構成します。製品プリセットのXMLは実装に含めません。

| 条件 | シャープネス／色ノイズ低減 | RawFileNameの既定値 |
| --- | --- | --- |
| JPEG/JPEG拡張子、TIF/TIFF拡張子（大文字小文字不問） | 0／0 | なし |
| その他の拡張子でQualityが厳密にFLAT | 0／0 | なし |
| その他 | 25／25 | 対象画像のbasename |

PNGを無条件にFLAT扱いにはしません。`flat` や末尾空白付きの `FLAT ` もFLATとは異なります。画像を新たにデコード・分類する処理ではなく、本体の再読込で既定値を選ぶ条件です。

TIFF Orientation=1、XMP Rating=0、Version=14、ProcessVersion=11、線形 `ToneCurvePV2012`（0,0 → 255,255）なども補います。マスク以外のルート数はRAW系145、FLAT系144です。今回の現行形式の既定パケットにはLookがないため、Lookを追加する処理はありません。既存Lookは他の入力プロパティと同じ規則で残します。

## 既存値と空値

本体は、新しい既定パケットへ入力XMPを全スキーマ・既存値置換あり・空値削除なしのappendで統合します。

- 非空の入力ルートが既定値より優先します。入力の露出・カーブ・Look・RawFileName等を、既定値で上書きしません。
- ルートが置き換わる場合は、型・配列全体・修飾子を入力から引き継ぎます。入力の構造体の空フィールドは除外します。
- 空の入力ルートはappend時に除外され、該当する既定値が残ります。空白だけの単純値は空とは異なります。
- 既定パケット自体が持つ空文字のLensProfileDigest／Filename／Nameは保持します。

そのため空の参照XMPを全体コピーした場合、露出が単に欠損するのではなく、既定の0へ戻ります。`--save-state preserve` は従来のXML統合だけを行い、これらの既定値を追加しません。選択列コピーにもこの補完を適用しません。

対象画像ごとに既定値が異なるので、共有XMPにNEFとJPEGの異なる結果を同時保存しようとした場合は、保存前に競合を返します。両方がFLAT扱いになるなど、実際の変更内容が同じ場合は一度だけ保存します。

## 実機観測

独自の `scripts/probes/ReloadDefaultsOracle.java` が、実際のフレームの既定パケット生成 `tQa.A(Rsa):Wd` を呼びます。合成ファイル名・Quality・現行形式の実シーケンスを渡し、生成されたパケットをcloneして、別途比較済みのマスクルートだけを観測対象から除いています。main・ライセンス・製品メソッド・プリセット内容を差し替えず、設定を保存しません。

| 観測 | 内容 |
| --- | --- |
| 既定値84件 | 14ファイル名 × Quality 6種（欠損・空・RAW・FLAT・flat・末尾空白） |
| 統合432件 | NEF/JPEG/PNG × RAW/FLAT × 9プロパティ × 8表現 |

統合では本体が実際に使う `ieA.A(Wd,Wd,true,true,false)` を呼びます。露出・シャープネス・空のレンズ名・カーブ・WB・Look・Orientation・Rating・RawFileNameを、属性、空、空白、修飾子、URI、構造体、配列、任意文字列で比較します。84件の全非マスクルート、432件の統合後の全ルートがGoと一致します。

保存参照は `lrt-reload-defaults-7.5.5.json.gz` です。独自の入力と観測結果だけを圧縮しており、通常テストで本体・Javaを呼ぶ必要はありません。Go生成結果の一部は、実機のAdobe製SDKへ読み戻して比較します。

## 検証

- 純Go全体テスト成功（internal/lrtl 138.488秒）。
- 関連race検査2回、Go 1.23で3回反復、go vet、CGOなしビルドが成功。
- `FuzzFullCopyDefaults`：30秒指定で310,681回成功。失敗時のパケット不変、XML読戻し、繰り返し統合の安定性を検査。
- `FuzzNativeSaveState`：対象名NEF/JPEG/PNGも渡すコピー・マスク・既定値・登録保存の経路で、30秒指定292,482回成功。
- CLI：拡張子×Quality×保存プロファイル、対象ファイル名、既存露出の優先、空参照、プレビュー・再実行・原画像・参照XMP・正確なバックアップの保持。
- 共有XMP：画像別の既定値が競合する場合は全ファイルを保持してエラー。同じFLAT結果なら一度だけ保存。
- 実行ファイル：64×32のPNG（RAW/FLAT扱い）・JPEGを3フレーム処理。上表の値とファイル名、バックアップ・再実行等を確認。実機の既定パケットを対照入力としてGoでレンダーし、AVI復号RGB24が全画素一致。RAW扱いPNGの既定効果を0へ変えた対照とは画素が異なり、効果が実際に適用されることも確認。SHA-256：`c8073ce4f6d498e0f70223c7ff3a94bb8eb3833ab0dffc11e871e02562711ec2`。

動画の比較対象は、実機で得たメタデータを与えたGoレンダラーです。LRT/Adobeの現像・動画出力自体との画素一致は示しません。

```powershell
go test ./...
go test -race ./internal/lrtl -run 'Test(FullCopyDefaults|CopySettings|FullCopyDateCLI|FullCopyMasksCLI)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzFullCopyDefaults$' -fuzztime 30s -parallel 4
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestFullCopyDefaults' -count=1
```

## 残る範囲

今回測定・実装したのは現行既定形式です。旧既定形式・他のユーザー設定やバージョン別の既定値の全パターン、全内部キャッシュの再読込、履歴・InstanceIDと保存完了は未再現です。不正な型に対する本体上位層の既定パケットへのフォールバックも含みません。Goは保存前に内容エラーを検出し、変更不要の再実行は保存自体を省略します。
