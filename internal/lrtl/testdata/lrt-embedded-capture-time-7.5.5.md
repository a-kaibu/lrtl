# 画像内XMPの撮影日時とDateTimeDigitized別名（LRT 7.5.5）

## 動作変更

従来は外部サイドカーのない画像を撮影日時のXMP読込から除外しており、画像内XMPしかない入力は日時順取り込み・字幕・動画の時刻表示で失敗することがありました。`timeshift` も空のXMLから始めるため、画像内の日時や現像設定を引き継げませんでした。

現在は撮影日時の取得と日時シフトで共通のXMP読込を使い、JPEG標準／拡張XMP、TIFF/DNG等の対応済み画像内XMPも入力にできます。既存サイドカーを優先し、選んだXMPに日時がなければ従来のEXIFフォールバックを使います。ファイル更新日時は撮影日時の代わりにしません。

| 処理 | 対応 |
| --- | --- |
| 撮影日時の読込 | 根の `exif:DateTimeOriginal` → `xmp:CreateDate` → `xmp:ModifyDate` の既存優先順を維持 |
| 入れ子のプリセット等にある日時 | 撮影日時として採用せず、日時シフトでも変更しない |
| 修飾された単純値 | `xml:lang` 等を持つ日時も読み、日時シフト時に修飾子を保持 |
| `timeshift` | 選択されたXMP全体を引き継ぎ、根の対象日時だけを変更。現像値・未知のRDF・プリセット内日時を保持 |
| 長期撮影フィルター | 同じ根の値と別名を参照。既存の欠損日時／不正日時の扱いを保持 |
| 途中の不正日時 | 全体の保存・出力公開前にエラー。既存SRT／動画・元画像を保持 |

## 実機で確認した別名

独自生成のJPEG／TIFF／DNGを、既存の独自観測器 `XMPSourceOracle.java` からインストール済みLRT 7.5.5の完全なファイル単位XMP読込へ渡しました。GUI・main・ライセンス状態の差し替えはありません。`lrt-embedded-capture-time-7.5.5.json` は3形式×15種類＝45条件です。画像・XMLは合成データです。

全45条件のXML意味内容がGoの読込と一致します。特に実機では、根の単純値 `exif:DateTimeDigitized` を `xmp:CreateDate` へ正規化し、両方ある場合は明示された `CreateDate` を優先して別名側を取り除きます。GoもXMLパース時にこの処理を行うため、**サイドカー保存時のプロパティ名や、矛盾する両値の保持方法が変わります**。属性・要素・修飾値、`xmp` 接頭辞が別URIへ割り当てられた入力も比較しています。入れ子の同名項目は正規化しません。

対象はこの撮影日時の単純値別名です。構造体／配列の別名、XMP全別名API、他の名前空間の一般的な正規化を再現したものではありません。

実機比較で証明したのは、ファイル単位のXML読込と上記の別名です。撮影日時の優先順・厳密な日時パーサーは既存のlrtlの方針です。LRT全体のCSVキャッシュ、フレーム初期化時の日時選択、撮影日時の保存副作用、壁時計のMetadataDateや履歴、Adobe/LRTの動画画素の一致は今回の比較対象に含みません。

## 検証

- 修正前の画像内日時読込失敗を回帰テストで再現し、修正後の成功を確認。
- 純Go全体テスト：成功（internal/lrtl 116.693秒）。
- 実機45条件の再読込と、保存済みXML・日時・入力バイトの完全比較：成功（3.973秒）。
- 関連機能のrace検査、Go 1.23反復、`go vet ./...`、CGOなしビルド：成功。
- `FuzzEmbeddedCaptureTime`：30秒指定、709,578回、成功。別名の正規化とXML再読込による根の日時の安定性を検査。
- 日時シフト：JPEG/TIFF/DNGでプレビュー無変更、未知のメタデータ・現像値・入れ子日時・修飾子保持、原画像保持、2回目の既存サイドカーの正確なバックアップを検査。
- 共通入力：外部サイドカー優先、不正サイドカーでの中止、長期フィルターで入れ子日時を使わないこと、既存の不正日時フィルター動作を検査。
- CLI経路：画像内日時だけのJPEGを日時順で取り込み／リネーム。通常SRTとLRTプロファイル `--every 1`、時刻入りAVIを外部XMPの対照と比較。後続の不正日時でSRT／動画の既存出力を保持することも検査。
- ビルドした実行ファイル：3枚の64×32 JPEGで上記取り込み・2種類のSRT・時刻入りAVI・1時間の日時シフトを実行。時刻入りAVIの復号RGB24は外部XMP対照と全画素一致。3フレームのSHA-256は `296bf9a5a7f82e843f82c7fbe51d5c7748b09ad8f197b9a34edb033626637166`。日時シフト前後の通常動画の画素と元画像も保持。

最後の動画比較はGoレンダラー内の入力経路の一貫性を検査しています。LRTやAdobeの動画出力自体との画素一致は示しません。

## 再実行

```powershell
python scripts/probes/embedded_capture_time_fixture.py work/new-embedded-capture-inputs
javac -encoding UTF-8 -d work/capture-classes scripts/probes/MetadataPasteOracle.java scripts/probes/XMPSourceOracle.java
java --enable-native-access=ALL-UNNAMED -cp 'work/capture-classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar' lrtlprobe.XMPSourceOracle work/new-embedded-capture.json work/new-embedded-capture-inputs
go test ./internal/lrtl -run TestEmbeddedCaptureTime -count=1
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run TestEmbeddedCaptureTimeInstalledReader -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzEmbeddedCaptureTime$' -fuzztime 30s -fuzzminimizetime 1x
```
