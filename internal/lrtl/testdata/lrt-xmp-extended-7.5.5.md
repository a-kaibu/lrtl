# JPEGの標準／拡張XMP統合（LRT 7.5.5）

## 観測と変更

インストール済みLRT 7.5.5の完全なファイル単位XMP読込を、独自の `XMPSourceOracle.java` で観測しました。main・GUI・ライセンス状態の差し替えはありません。独自生成JPEGへ標準APP1と拡張APP1を挿入した30条件を `lrt-xmp-extended-7.5.5.json` に保存しています。生成器は `scripts/probes/xmp_extended_fixture.py` です。製品のコード・画像は含みません。

従来のGo版はリンク先の拡張XMLだけを返し、標準XMLにしかない項目とリンク項目を失っていました。現在は標準パケット群を統合した後、読み取った拡張パケットを重ね、標準側にしかない項目を保持します。編集結果は外部サイドカーへ保存し、元画像は変更しません。

| 観測した規則 | 対応 |
| --- | --- |
| 標準・拡張に別々の項目 | 両方保持 |
| 同じ項目 | 拡張側の空でない値を優先。構造体・配列は項目全体を置換 |
| 空の拡張XML／空の値 | 標準側を保持 |
| リンクが複数の標準パケットに存在 | 最初のリンクを使ってチャンクを読む。最終XMLのリンク値自体は後の標準パケットで置換され得る |
| 拡張チャンクの後に標準パケット | 全標準パケットの統合後に拡張値を適用 |
| GUIDの大小文字 | リンクとチャンクは大小文字を区別。両方小文字なら読める |
| リンク前の拡張チャンク | 取り込まない |
| 分割・逆順チャンク | オフセットで組み立て |
| 同じチャンク／パケットの再送 | 既に組み立てた範囲の同一バイトを許可。異なる内容の重なりはエラー |
| リンクなし／標準パケットなし | 拡張パケットをメタデータとして採用しない |

## 意図的に残すエラーの差

30条件のうち20条件で実機XMLの意味内容またはメタデータなしの結果に一致します。残る10条件では、実機が標準値へフォールバックするか検査なしで採用する入力に対して、Go版は従来の整合性検査を残し、書込前のエラーとします。

- 拡張XML不正、チャンク欠落、GUIDのMD5不一致。
- 空／不正リンク、参照先の拡張パケットなし、無関係なGUIDのみ。
- リンクとチャンクで大小文字が異なるため参照先を解決できない場合。
- リンクより前にしか拡張チャンクがなく、参照先を解決できない場合。

各条件を明示したテストがあり、これらを実機との一致として数えていません。正常な先行画像と後続の解決不能な画像を同じ連番で初期化し、どのサイドカーも書かないことを10条件で確認します。

標準XML群と拡張チャンク群はそれぞれ合計32 MiBまでに制限します。拡張パケットの宣言サイズ・範囲・サイズの一貫性・完全性・MD5・XMLを検査します。この制限と検査は製品の許容範囲を完全に再現するものではありません。

## 検証

- 純Go全体テスト：成功（internal/lrtl 128.711秒）。
- 保存済み30入力を実機で再読込し、XML・日時を含む観測全体を比較。以前の標準パケット20条件、ソース選択76条件も再比較。
- 関連テストのrace検査を2回反復。Go 1.23でも3回反復。`go vet ./...`、CGOなしビルドも成功。
- `FuzzXMPExtendedJPEG`：30秒指定、463,149回実行、成功。
- チャンク単体テスト：正順・逆順・再送・一致する包含範囲・矛盾する重なり・欠落・隙間を検査。
- CLI経路：通常リンク・3分割・2つのリンクの3条件で各3枚を初期化／動画化。両側の項目と露出2を保持し、元画像無変更・再初期化無変更、AVIの主要ヘッダー・フレーム・インデックス一致を検査。現在時刻コメントは比較から除外。
- ビルドした実行ファイル：同じ3条件×3フレーム、64×32のAVIを生成。初期化前後と、実機の観測XMLからリンクを除いて作った対照入力のAVIをFFmpegでRGB24へ復号し、すべて一致。各3フレームの画素SHA-256は `03774bb0050b7430ab27181d20e2d09fdf491a21bb1285d54a451324cbda91f3`。

最後の動画比較は、実機が選んだメタデータをGoのレンダラーへ渡したときの一致です。Adobe現像／LRTの動画出力自体との画素一致を示すものではありません。TIFF複数IFD、EXIF内サムネイル由来XMPを含む全メタデータディレクトリの順序、完全な実機インポート・キャッシュ処理は引き続き未完です。

## 再実行

```powershell
python scripts/probes/xmp_extended_fixture.py work/new-xmp-extended-inputs
javac -encoding UTF-8 -d work/xmp-extended-classes scripts/probes/MetadataPasteOracle.java scripts/probes/XMPSourceOracle.java
java --enable-native-access=ALL-UNNAMED -cp 'work/xmp-extended-classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar' lrtlprobe.XMPSourceOracle work/new-xmp-extended.json work/new-xmp-extended-inputs
go test ./internal/lrtl -run TestXMPExtended -count=1
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run TestXMPExtendedInstalledReader -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzXMPExtendedJPEG$' -fuzztime 30s
```
