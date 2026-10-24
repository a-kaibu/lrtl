# 選択画像を連番から除外する

`remove-sequence`は、選択画像を隣の`<元フォルダー名>_REMOVED`へ移動します。元画像を完全削除する操作ではありません。WindowsとLinuxに対応し、既定は変更予定の表示です。

```powershell
# 低評価の画像を確認する
lrtl remove-sequence C:\photos\sequence --ratings -1
# sequence_REMOVEDへ移動する
lrtl remove-sequence C:\photos\sequence --ratings -1 --write
# 別の選択も同じ除外先へ追加できる
lrtl remove-sequence C:\photos\sequence --filename-contains test-shot --write
```

`--selection FILTER.json`、`--filename-contains TEXT`、`--ratings LIST`は積集合、`--invert`はその反転です。選択条件がない場合は全画像が対象です。プレビューの出力先・対象一覧を確認してください。

既定では、同名JPEG、両方の外部XMP候補、通常／Visual Previewとプロキシの画像別キャッシュも対象です。同名JPEGが選択外でも追加される場合があります。追加JPEGとキャッシュを省く場合は`--include-lrt-companions=false`を使います。元連番の未選択画像にも必要なXMP・キャッシュは、元にも残します。対象の詳細は[画像の移動](move-sequence.md)を参照してください。

## 繰り返し実行と同名ファイル

`_REMOVED`が既に存在するときは、そのフォルダーへ追記します。既存ファイルの内容・更新日時・属性を変えません。

- 同名画像がある場合は、内容が同じでもエラーにします。
- 同名XMP／キャッシュは、内容のSHA-256が完全一致する場合だけ既存ファイルを再利用します。XMPを意味的に結合する処理は行いません。
- 同名XMP／キャッシュの内容が異なる場合、大小文字だけが異なる既存名がある場合、途中の親がファイルやシンボリックリンクの場合は中断します。
- シーケンス全体の設定、履歴、バックアップは元フォルダーに残します。

任意の既存フォルダーへの移動でも、`move-sequence --merge`で同じ追記処理を使えます。

```powershell
lrtl move-sequence C:\photos\sequence --output D:\photos\archive --ratings 4,5 --merge --write
```

## 失敗からの復元

[移動処理の検証・退避・復元](move-sequence.md#途中失敗と復元)を共有します。追記では検証済みコピーを1ファイルずつ公開し、途中で失敗した場合はその実行で追加したファイルだけを戻してから原本を復元します。既存フォルダー全体を一度で切り替えるトランザクションではありません。

追加したファイルを他のアプリが変更した場合や、追加分の取り消しにも失敗した場合は、そのファイルを無理に削除しません。復元できた原本と、残った移動先・一時コピー・ジャーナルを保持してエラーにします。ジャーナルの`existing_destination`は既存先への追記、各項目の`ReuseDestination`は既存の同内容ファイルを再利用したことを示します。手動復旧時に、再利用した既存ファイルを削除しないでください。

処理中の外部編集に対する完全な隔離、電源断の耐久性、強制終了後の自動復旧、同名画像の上書き、完全削除は未対応です。原本の内容・時刻・パーミッションと、実行前から存在する出力ファイルを確認するテストを追加しています。

## 本体との比較範囲

インストール済みLRT 7.5.5の選択行を除去する処理を調べ、元フォルダー名に`_REMOVED`を付けた兄弟フォルダーへ、通常の選択行移動処理を呼ぶことを確認しました。同名JPEGと画像別キャッシュの対象は、本体の24条件で比較済みの[付随ファイル判定](sequence-companions.md)を使います。

移動・ハッシュ検証・復元は独自実装です。本体が実画像を移動した全結果との一致、同名衝突時のUI選択や上書き処理を再現したという検証ではありません。

テストには、繰り返し除外、共有データ再利用、同名画像／異なるXMP／大小文字衝突、途中の新規ファイル、出力先の置換、共有XMPの外部編集、追記・取り消し・後片付けの失敗、ディレクトリの取り消し、シンボリックリンク境界、CLI評価選択、除外前後のAVIフレーム一致を含みます。追記の失敗箇所・ファイル内容・既存メタデータの有無を変えるfuzz testも追加しています。

```powershell
go test ./internal/lrtl -run 'Test(RemoveSequence|MoveSequence)' -count=2
go test -race ./internal/lrtl -run 'Test(RemoveSequence|MoveSequence|CopySequence|SequenceCompanions)' -count=2
go test ./internal/lrtl -run '^$' -fuzz '^FuzzMoveSequenceMergeRollback$' -fuzztime=30s
```

2026-10-24にWindows／Ubuntu 24.04で関連テストを2回実行し、関連race検査と30秒のfuzz（1,676ケース）も通過しました。Linuxではキャッシュ親のシンボリックリンク境界の検査もスキップされず通過しています。Go 1.23の関連テスト・CGO無効のCLIビルド・`go vet ./...`も通過しました。

Go 1.26.9／CGO無効の全体回帰テスト`go test -p 1 ./...`も通過しました（`internal/lrtl`は153.955秒）。

Linuxの`/tmp`から別ファイルシステムの`/dev/shm`への既存先追記も2回通過し、同内容XMPの再利用と既存ファイルの保持を確認しています。

ビルドしたCLIでは、日本語パスのテスト連番から3回に分けて17ファイルを除外し、既存共有データの再利用、原本と既存出力の内容・更新日時の保持、一時フォルダーが残らないことを確認しました。除外前の選択範囲と除外先のPNGからAVIを作り、FFmpegで復号したRGB 12,288サンプルが一致しています（SHA-256: `9477caeae9ec9ede4e525afc542667644f6f42f65d0f1cbf182ec626ee48169e`）。テスト素材での確認であり、実カメラRAWの現像全体の検証ではありません。
