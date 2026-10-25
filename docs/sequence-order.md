# 動画・中間画像・字幕の並べ替え

`render`、`export`、`preview-images`、`subtitles`に`--sort`を指定できます。画像の選択・ファイル名フィルターを適用した後、時間方向の処理や出力マニフェストを作る前に並べ替えます。元の画像名・XMPは変更しません。既定は従来どおり`name`です。

| 値 | 比較する情報 | 同値の場合 |
| --- | --- | --- |
| `name` | lrtl従来のファイル名自然順 | 従来の名前比較規則 |
| `capture-time` | XMP／画像の撮影日時を解釈した時刻。タイムゾーンのオフセットも反映 | 入力順を保持 |
| `lrt-name` | LRT 7.5.5方式のファイル名自然順 | 置換後のUTF-16文字数、それも同じなら入力順 |
| `lrt-ascii` | 本体のASCII設定が使う、大文字小文字を区別しない文字順。Unicode名も比較する | 入力順を保持 |
| `lrt-date` | グローバル`xmp:CreateDate`の文字列順。日時の解析やタイムゾーン変換はしない | 文字列が完全に同じなら`lrt-ascii`の名前順 |
| `lrt-settings` | `.lrt/lrtsequence.json`の`filenameSort`設定。`true`なら`lrt-name`、`false`なら`lrt-date` | 選択された本体方式に従う |

例えば`frame2.png`、`frame02.png`、`frame10.png`の順序は、`name`では02→2→10、`lrt-name`では2→02→10、`lrt-ascii`では02→10→2です。

```powershell
lrtl render C:\photos\sequence --sort lrt-date --output C:\photos\movie.avi --apply-xmp-basic
lrtl export C:\photos\sequence --sort lrt-date --output C:\photos\tiff --apply-xmp-basic
lrtl preview-images C:\photos\sequence --sort lrt-date --output C:\photos\preview
lrtl subtitles C:\photos\sequence --sort lrt-date --output C:\photos\movie.srt
lrtl render C:\photos\sequence --sort lrt-settings --output C:\photos\movie.avi

# 撮影時刻をタイムゾーン込みで比べる場合
lrtl render C:\photos\sequence --sort capture-time --output C:\photos\chronological.avi

# バッチ処理にも渡せる
lrtl batch-render C:\photos --output C:\movies --format avi -- --sort lrt-date
lrtl batch-preview-images C:\photos --output C:\previews -- --sort lrt-name
```

字幕も動画と同じ`--sort`・`--selection`・`--filename-contains`を指定してください。再生速度・Motion Blur等は従来どおり、それぞれの対応フラグで合わせます。TIFF/JPEGの`frames.json`は並べ替え後の画像と対応します。JPEGプレビューの`sourceIndex`は元の自然順における番号を保持し、`--write-preview-ev`も対応する元画像のXMPへ書き戻します。

## 本体方式の規則と制限

`lrt-name`は`_`を空白、`@`を`!`へ置き換えてから数字列とその他の列に分けます。数字同士は符号付き64-bit整数として比べ、その他は大文字小文字を区別しない文字列として比べます。数字列に対する本体の桁あふれ例外は、Goでは出力前のエラーとして扱います。数字の先頭ゼロは整数比較では無視しますが、最後の文字数比較に影響します。

`lrt-date`は外部／埋め込みXMPを通常の採用規則で読み、ルートの`xmp:CreateDate`だけを使います。URI・修飾子付きの単純値にも対応し、入れ子の別メタデータの日時は採用しません。日時が全画像で欠ける場合は名前の文字順です。一部だけ欠ける場合は、比較の推移性が崩れ得る本体のペアごとの名前フォールバックを使用せず、エラーにします。不正な日時らしい文字列も、本体方式では文字列として並びます。`capture-time`は撮影日時の欠損・不正値をエラーにし、ファイル更新日時で補いません。

日時順は`2026-01-01T02:00:00+09:00`と`2026-01-01T01:00:00Z`などで、実時刻の前後関係と逆になる場合があります。また、`CreateDate`が大文字小文字だけ異なる場合は、その比較が同値になっても名前順へは切り替えません。本体の比較処理に合わせた動作です。

`lrt-settings`は各連番の`.lrt/lrtsequence.json`から`filenameSort`だけを読み、保存済みの名前順／日時順を出力に使います。設定ファイルや連番データは変更しません。`sortFoldersAscii`はバッチ時のフォルダー列挙設定なので、このフレーム順指定には影響しません。ファイル読込直後の本体日時キャッシュ生成全体、未知のUnicode版での全比較、すべてのファイル名集合でのJava／Goのソート実装の一致を保証するものではありません。UTF-8として不正なファイル名は本体方式でエラーにします。

## 比較とテスト

インストール済みLRT 7.5.5の実際の比較処理を、[独立した観測スクリプト](../scripts/probes/SequenceOrderOracle.java)で呼びました。64種類の名前の全4,096組について自然順・文字順を比較し、日時順8条件も[観測値](../internal/lrtl/testdata/lrt-sequence-order-7.5.5.json)へ保存しています。先頭ゼロ、64-bit境界と桁あふれ、置換文字、ギリシャ文字、トルコ語I、日本語、補助平面文字、ASCII／Unicode数字、空白を含みます。

Goの回帰テストはその全観測値に加え、各順序と`lrt-settings`の手動順序を、AVIフレーム・TIFF/JPEG・マニフェスト・SRTと比較します。バッチ処理、元インデックス、測光結果の正しいXMPへの保存とバックアップ、UTCオフセットと同時刻の安定順、修飾子付き日時、入れ子の日時の無視、選択後の読込、異常時の出力中止と元ファイル保持も検査します。並べ替え単体のfuzz testもあります。

```powershell
go test ./internal/lrtl -run '^TestSequenceOrder' -count=2
go test -race ./internal/lrtl -run 'Test(SequenceOrder|LRTReference)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzSequenceOrderComparator$' -fuzztime=30s
```

本体比較を再実行する場合は、Java/JavacをPATHへ追加し、`LRTL_NATIVE_JAR`をインストール済みJARへ設定して`TestSequenceOrderInstalledComparators`を実行します。画像やユーザー設定の編集・ライセンス処理の置き換えは行いません。本体クラスの静的初期化に伴って、本体のログが出力される場合があります。通常のテストと本番CLIにJava依存はありません。
