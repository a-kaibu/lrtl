# JPEG内の複数標準XMPパケット（LRT 7.5.5）

## 実機観測

インストール済み `LRTimelapse.jar` の完全なファイル単位XMP読込を、既存の独自観測器 `scripts/probes/XMPSourceOracle.java` から呼び出しました。GUI・main・ライセンス状態を置き換えていません。独自生成の小さなJPEGへ標準APP1 XMPを1〜3個挿入した20条件を `lrt-xmp-multiple-7.5.5.json` に保存しています。画像とXMLは合成データで、製品コードや撮影データを含みません。

| 入力 | 実機の結果／Goの対応 |
| --- | --- |
| 別々の項目を持つ複数パケット | すべての項目を取り込む |
| 同じ項目が複数に存在 | ファイル内で後のパケットを優先 |
| 同じ構造体・配列 | 後の項目全体で置換。フィールド／要素を追加結合しない |
| 空の単純値・空配列・空構造体 | 前の値を保持 |
| 空でない配列／構造体の中に空要素／フィールド | 置換時に空の子を除去。すべて空なら以前の項目も削除 |
| 型の変更・修飾値 | 後の項目で置換 |
| 正常と壊れたXMLの混在 | 壊れたパケットを読み飛ばし、正常なパケットを使用 |
| 壊れたXMLのみ（2条件） | 実機は空メタデータ。Goは従来どおり厳密なエラー |

20条件のうち18条件で実機のXML意味内容に一致します。残る2条件は意図した厳密性の差をテストで明示しています。後続画像の壊れたXMPで初期化が失敗しても、先行画像のサイドカーを書かないことも確認します。

## 変更範囲

従来は最後の標準APP1だけを採用しており、それ以前のパケットにしかない項目が失われていました。JPEGを読む共通経路で順序付き統合を行うため、`init`・メタデータ編集・レンダリングから同じ結果を使えます。画像自体は変更しません。

標準パケット群のXML合計サイズを32 MiBに制限します。既存の拡張XMPのリンク・チャンク検証も回帰テストしています。ただし標準XMPと拡張XMPの全項目の統合、TIFFの複数IFD、EXIF内サムネイル由来のXMPを含むすべてのメタデータディレクトリの読込順序は今回の実装・実機一致の対象外です。完全な実機インポート／キャッシュ処理の再現ではありません。

## 検証

- 純Goの全体テスト：成功（internal/lrtl 124.586秒）。
- 実機のファイル読込再実行：今回20条件と前回のソース選択76条件が保存参照に一致（6.285秒、初期化／動画テストを含む）。
- race検査：成功。
- `FuzzXMPMultipleJPEG`：30秒指定、76,452回、成功。
- CLI経路：3枚の連番でプレビュー無変更、項目保持、露出2、元画像保持、再初期化無変更、初期化前後のMJPEG AVIの主要ヘッダー・全フレーム・インデックス一致を検査。現在時刻コメントを比較から除外し、race反復でも確認。
- ビルドした実行ファイルでも3枚の連番を初期化・動画化し、元JPEG保持と両パケットの項目保持、初期化前後のAVI一致を確認。
- 異常系：後続画像のXML不正で保存前中止、32 MiB制限、拡張XMPリンクの回帰検査。

AVIの比較はGoレンダラー内の入力メタデータの一貫性を確かめるもので、Adobe現像やLRT動画出力との画素一致を示すものではありません。

## 再実行

```powershell
python scripts/probes/xmp_multiple_fixture.py work/new-xmp-multiple-inputs
javac -encoding UTF-8 -d work/xmp-multiple-classes scripts/probes/MetadataPasteOracle.java scripts/probes/XMPSourceOracle.java
java --enable-native-access=ALL-UNNAMED -cp 'work/xmp-multiple-classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar' lrtlprobe.XMPSourceOracle work/new-xmp-multiple.json work/new-xmp-multiple-inputs
go test ./internal/lrtl -run TestXMPMultiple -count=1
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run TestXMPMultipleInstalledReader -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzXMPMultipleJPEG$' -fuzztime 30s
```

実機再実行テストは保存済みの合成入力・日時を復元し、XMLと日時を含めた観測全体を比較します。生成器から新規作成したファイルは作成日時が異なるため、JSONの日時は保存参照と一致するとは限りません。
