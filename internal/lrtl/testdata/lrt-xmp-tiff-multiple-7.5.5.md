# TIFF/DNGの複数IFDにあるXMP（LRT 7.5.5）

## 実機観測

独自生成のTIFF/DNGディレクトリグラフを、インストール済みLRT 7.5.5の完全なファイル単位XMP読込に渡しました。既存の独自観測器 `XMPSourceOracle.java` を使い、GUI・main・ライセンス状態を差し替えていません。製品コードや撮影データは含みません。

`lrt-xmp-tiff-multiple-7.5.5.json` は `.tif`／`.tiff`／`.dng`、little/big endian、20種類のグラフの計120条件です。生成器は `scripts/probes/xmp_tiff_multiple_fixture.py` です。親・子は異なる露出値と独自項目を持ち、採用値だけでなく項目が消えていないかも比較します。

| 観測した規則 | Goの対応 |
| --- | --- |
| 同じ画像内に複数の対象XMP | 全パケットから項目を取り込み、後の空でない項目で置換 |
| タグの走査 | ファイル内の格納順。SubIFD（330）とExifIFD（34665）を見つけた位置ですぐ再帰 |
| 通常の昇順タグ＋SubIFD | 子のXMP→親のXMPとなり、親の重複値を優先 |
| 降順タグ＋SubIFD | 親のXMP→子のXMPとなり、子の重複値を優先 |
| ExifIFD | 親のtag 700→ExifIFDとなり、Exif側の重複値を優先 |
| 次ページへのnextポインター | そのIFD自身のtag 700を採用しない。そのIFDから辿るSubIFDのXMPは取り込む |
| GPS／Interopの直接参照先 | 観測した参照先自身のtag 700を採用しない |
| 循環／同じ子への複数参照 | 同じオフセットを再訪しない |

従来のGo版は幅優先で最初のtag 700だけを返していました。現在はclassic TIFF/DNGの通常読込から上記の順序で統合し、初期化・編集・動画生成が同じメタデータを使います。次ページにしかXMPがない入力では、その値を採用しなくなる動作変更を含みます。外部サイドカーの優先規則は既存の[ソース選択](lrt-xmp-source-7.5.5.md)と同じです。

## 意図的な差と残る範囲

120条件のうち114条件で、実機のXML意味内容またはメタデータなしの結果に一致します。対象となる親XMPが壊れている6条件では、実機が空メタデータを返すのに対し、Go版は保存前エラーを維持します。次ページ自身の壊れたXMPは、実機と同じく採用しません。

合計XMLの宣言サイズを32 MiB、参照の深さを32段に制限し、既存のディレクトリ数・エントリ数・範囲検査も適用します。不正な末尾ディレクトリなど、実機が途中結果を返す可能性があるすべての破損パターンとの一致は未確認です。

今回の走査順はclassic TIFF/DNGに適用します。BigTIFFおよびTIFF構造の他RAWの独自拡張対応は従来の読込経路を保持し、BigTIFFのTIFF/DNGとNEF/CR2/RW2/ARWの回帰テストを追加しました。それらの複数IFD統合が今回の実機比較で証明されたわけではありません。EXIF内サムネイル由来XMPや全メタデータディレクトリ、完全なインポート／キャッシュ処理の再現は未完です。

降順タグの合成入力ではメタデータ読込を実機と比較していますが、実画素を持たせた同じ形式の画像はGoのTIFFデコーダーが `tags are not sorted in ascending order` で拒否しました。この入力の動画生成は未対応で、成功した動画比較には含めていません。

## 検証

- 純Go全体テスト：成功（internal/lrtl 125.880秒）。
- 実機120条件の再読込と保存参照の完全比較。以前のソース選択76条件、JPEG標準20条件、拡張30条件も再比較（計11.983秒）。
- 関連テストのrace検査、Go 1.23での3回反復、`go vet ./...`、CGOなしビルド：成功。
- `FuzzXMPTIFFMultiple`：10,000回指定、最小化1回で実行し成功。時間指定のfuzzも別途実行。ファイル内容不変と、成功時のXMLを検査。
- 安全性：後続画像の壊れたXMPにより初期化が失敗しても先行サイドカーを書かないことを6条件で検査。XML合計サイズ、参照深さ、循環を検査。
- 実画素TIFFのCLIテスト：親と子の項目を保持し、親の露出1を採用。プレビュー無変更、元画像保持、再初期化無変更、初期化前後のAVI主要ヘッダー・全フレーム・インデックス一致を検査。現在時刻コメントは比較から除外。
- ビルドした実行ファイル：SubIFD・ExifIFD・次ページの3種類×3フレーム、64×32のAVIを生成。初期化前後および実機の観測XMLを使った対照入力をFFmpegでRGB24へ復号し、各条件ですべて一致。元画像も保持。

| 実行ファイルの入力 | 3フレームの画素SHA-256 |
| --- | --- |
| ExifIFD（露出2） | `6b96f976f330dae8ff7e1a71846bc861f45ad2389301a581568d487dab8b893b` |
| SubIFD／次ページ（露出1） | `eb1c5734cd7c3f0b1a0e4b30372ade3ca169537cd58ee5c5845201e2b6d324f5` |

動画比較は、実機が採用したメタデータをGoレンダラーへ渡した場合の一致です。Adobe現像やLRT動画出力自体との画素一致は示しません。

## 再実行

```powershell
python scripts/probes/xmp_tiff_multiple_fixture.py work/new-xmp-tiff-inputs
javac -encoding UTF-8 -d work/xmp-tiff-classes scripts/probes/MetadataPasteOracle.java scripts/probes/XMPSourceOracle.java
java --enable-native-access=ALL-UNNAMED -cp 'work/xmp-tiff-classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar' lrtlprobe.XMPSourceOracle work/new-xmp-tiff.json work/new-xmp-tiff-inputs
go test ./internal/lrtl -run TestXMPTIFFMultiple -count=1
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run TestXMPTIFFMultipleInstalledReader -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzXMPTIFFMultiple$' -fuzztime 10000x -fuzzminimizetime 1x
```
