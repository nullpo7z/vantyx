# HANDOFF_FIXES — Chrome UI 検証で見つかった問題と修正状況

作成: 2026-08-29(Windows 側セッション、Chrome 実操作による検証)
対象: 本番 https://10.10.10.70(admin でログイン、ブラウザは 10.10.70.31 = Windows 開発機)

このファイルは **Linux 側の Claude / 開発者がすぐに修正・デプロイに着手できるように** 書いています。
Windows 側ではコードを触らない方針なので、以降の修正・ビルド・デプロイはすべて Linux 側で行ってください。

---

## 0. まず読むこと(状態の要約)

> **2026-08-29 Linux 側での対応結果**: A-1〜A-5、E-1、E-2、E-3/E-5、E-4、E-6、E-8、E-9、E-10(方針 a・管理 API 範囲)、E-11、E-12、E-13、E-14、E-15、E-16、F-1 を各 1 コミットで修正し、6 回に分けて本番へデプロイ済み(各項目の「✅ 修正済み(コミット ID)」を参照)。テストユーザー 3 名・テスト録画は本番から削除済み。その後の判断に基づき B-1(remux + 既存録画の一括変換)と E-7 も修正・デプロイ済み(7 回目)。E-10 の接続・録画閲覧範囲は「admin でもグループ配属が必要」で確定(変更なし)。**本ファイルの全項目が対応済み**。

| 区分 | 件数 | 状態 |
|---|---|---|
| A. 修正済み(ワークツリーに変更あり・**未デプロイ**) | 5 | ビルド/テスト通過。`scripts/deploy.sh` で反映するだけ |
| B. 未修正(要実装) | 1 | fragmented MP4 の総再生時間 |
| C. 環境要因(コード修正不要) | 1 | x11spi-tf への SSH 到達不可 |
| D. 検証で正常確認済み | 多数 | §5 参照 |

**注意**: A の 5 ファイルは Windows 側セッションが NAS 共有(`X:\vscode\vantyx` = `/mnt/nas/data/vscode/vantyx`)上で直接編集したものです。
git status に差分として現れます。内容は §1 の通りで、サーバー上の一時コピーで
`docker build --target frontend` 成功、`gofmt -l` 空、`go vet ./internal/httpapi/` OK、
`go test ./internal/httpapi/... ./internal/recording/...` 全パスを確認済みです。
本番 `/opt/vantyx` にはまだ反映されていません(scp が権限でブロックされたため)。

---

## 0.4 再検証結果サマリー(2026-08-29 夜、Windows 側 Chrome、本番 7 回目デプロイ後)

| 項目 | 結果 |
|---|---|
| A-1 / B-1 | ✅ Range 206 + Accept-Ranges、開いた直後に duration 1077 秒、シーク即時反映 |
| A-2 / A-3 / A-4 / A-5 | ✅ UTC 先頭 / ズーム中も再生継続 / SFTP 日時整形 / To=当日 |
| E-1 / E-2 / E-3(E-5)/ E-4 / E-6 / E-7 | ✅ |
| E-9 | ✅ 「アクセスが許可されていません」を表示 |
| E-10 | ✅ 一覧・Server management 全件、POST/DELETE 可 — ⚠️ PUT だけ 403(**R-3**) |
| E-11 | ✅ Delete ボタン → 確認 → 削除、監査 `user_delete`(テストユーザー 2 名を UI で削除済み) |
| E-12 / E-13 | ✅ 監査 `session_invitation_created/revoked`、録画削除 + `recording_deleted` |
| F-1 | ✅ Participants ボタン → モーダル(ただし文言が **R-1**) |
| E-8 | ⚠️ ナビに Settings は出るがクリックで遷移しない(**R-4**) |
| E-14 | ⚠️ hidden クラスは正しく付くが CSS 上書きで表示される(**R-2**) |
| E-15 | ❌ キック後も通常の切断画面(**R-5**) |
| E-16 | 未再検証(参加側タブが閉じられたため。F-2 の受け入れ条件で確認すること) |
| 後片付け | テストユーザー `uitest-admin2` / `uitest-viewer`、テスト録画 `uitest-retest`、SFTP テストファイル、エクスポート、テストターゲット `uitest-admin2-target` はすべて削除済み |

**次回 Linux 側で対応する項目**: R-1, R-2(= E-14 / ユーザー要望「権限の無いボタンを出さない」), R-3, R-4, R-5(= E-15), F-2(Allow rejoin ボタン廃止・再招待で自動解除)。
→ ✅ **すべて対応済み(2026-08-29、コミット d6d9d6e / dc76fff / 878ce9d / f036c76 / 2ba142c / 7e3f01d、8 回目デプロイ)**。各項目の詳細は §0.5 の「✅ 修正済み」を参照。Windows 側での再検証ポイント: (a) viewer 画面に Participants / Invitations / End session が出ず Leave のみ、オーナー画面に Leave が出ない(R-2)、(b) 2 人目の admin で Edit → Update 成功(R-3)、(c) 一般ユーザーで Settings に遷移しタイムゾーンを変更できる(R-4)、(d) Remove 直後に参加者側が「削除されました」+ ホームに戻る のみ(R-5)、(e) Remove → 同ユーザーへ Named user で Issue → 参加者ホームにバナー → Join 成功(F-2/E-16)。

## 0.5 再検証(2026-08-29 夜、Windows 側 Chrome)で見つかった退行 — 要修正

### R-1. 端末ヘッダーのボタン文言が翻訳キーのまま表示される — `web/src/locales/en.js:537-538`, `web/src/locales/ja.js:513-514`, `web/src/terminal_page.js:90, 93, 1527`
- ✅ 修正済み(コミット d6d9d6e、2026-08-29 デプロイ済み)— 原因分析どおり。`terminal:` ブロックに `actionParticipants` / `actionLeave` を追加(en/ja)。
- **症状**: 本番の端末画面(オーナー)でヘッダーに `terminal.actionParticipants (0)` / `terminal.actionLeave` と表示される(英語 UI)。
- **原因**: `actionParticipants` / `actionLeave` が `sessions:` ブロック(`fieldName`…`actionInvite` の並び、en.js 520-539)に追加されているが、`terminal_page.js` は `t('terminal.actionParticipants')` / `t('terminal.actionLeave')` で参照している。
- **修正案**: 両キーを `terminal:` ブロックへ移動(または複製)。ja.js も同様。

### R-2. オーナーの端末画面にも「Leave」ボタンが表示される(E-14 の修正が効いていない) — `web/src/terminal_page.js:93`, `web/src/*.css`(`.vantyx-page-btn`)
- ✅ 修正済み(コミット dc76fff、2026-08-29 デプロイ済み)— 原因分析どおり CSS の詳細度(`#app header .vantyx-page-btn` = id+要素+class が `.hidden` に勝つ)。`style.css` に `#app header .vantyx-page-btn.hidden { display: none !important; }` を追加。これで viewer 画面の Participants / Invitations / End session、オーナー画面の Leave が消える(E-14 と「権限の無いボタンを出さない」要望の両方)。
- **症状**: `#term-leave` は `class="vantyx-page-btn hidden"` だが computed `display: flex` で表示されている(オーナー画面で End session と Leave が両方見える)。
- **原因**: `.vantyx-page-btn { display: flex }` が Tailwind の `.hidden { display: none }` より後に(または高い詳細度で)定義されており、`hidden` クラスが効かない。`#term-participants-manage` も同じクラス構成(初期 `hidden`)なので、viewer 側で Participants ボタンが見えてしまう可能性が高い(要確認)。
- **修正案**: `.vantyx-page-btn.hidden { display: none !important }` を追加するか、表示切替を `hidden` クラスではなく `el.hidden = true`(`[hidden]{display:none!important}`)や `style.display` で行う。F-1 / E-14 の viewer 表示も併せて再確認。
- **viewer 側でも再現(2026-08-29 再検証)**: `uitest-viewer` の参加画面で `#term-participants-manage`(class に hidden あり)/ `#term-close`(End session、class に hidden あり)/ `#term-invite-manage` がすべて computed `display:flex` で表示されている。つまり E-14 のクラス切替ロジック自体は正しく動いており、**原因は CSS の上書きのみ**。この 1 箇所を直せば R-2 と E-14 の両方が解決する。

### R-3. 追加 admin によるターゲット**更新**(PUT)だけが 403 のまま(E-10 の取りこぼし) — `internal/httpapi/targets_handler.go`(update ハンドラ)
- ✅ 修正済み(コミット 878ce9d、2026-08-29 デプロイ済み)— 原因: update ハンドラが対象ターゲットの読み込みに `getSessionAndTargetWithAccess`(ユーザー単位 ACL)を使っていた(E-10 で外したのは移動先グループのチェックのみ)。admin 専用ハンドラなので `TargetStore.Get` で直接読み込むよう変更。旧方針を固定していた `TestApp_UpdateTarget_Forbidden` を反転し、`admin_visibility_test.go` に PUT ケースを追加。
- **症状**: `uitest-admin2`(グループ未配属の admin)で `POST /api/targets`(home/proxmox に作成)→ 201、`DELETE /api/targets/{id}` → 204 は通るが、`PUT /api/targets/{id}` → **403「アクセスが許可されていません」**。Server management の Edit → Update が 2 人目の admin では失敗する。
- **原因(推定)**: E-10 で create/delete からは撤廃した「操作する admin が対象グループのメンバーであること」の再チェック(または `userCanAccessTarget` 系の ACL チェック)が update ハンドラに残っている。
- **修正案**: update ハンドラの admin パスでもメンバーシップ再チェックを撤廃し、`admin_visibility_test.go` に PUT のケースを追加。
- **再検証手順**: `uitest-admin2` でログイン → Server management → proxmox → 任意ターゲットの Edit → 名前変更 → Update が成功すること。

### R-4. 一般ユーザーに Settings リンクは表示されるが、クリックしても遷移しない(E-8 の取りこぼし) — `web/src/nav.js`(`wireNav` / クリック配線), `web/src/app.js`
- ✅ 修正済み(コミット f036c76、2026-08-29 デプロイ済み)— 原因分析どおり、`nav.js` の `navSettings` クリックハンドラが `me.role !== 'admin'` で return していた。サインイン済みなら遷移させ、監査転送セクションはページ側の出し分けに委ねる。
- **症状**: `uitest-viewer`(role=user)でナビに「Settings」(`#nav-settings`, hidden なし)が表示されるが、クリックしてもホームのまま(main の h2 = root、`#settings-timezone` 不在)。admin では遷移する。
- **原因(推定)**: E-8 で `isAdminOnlyNav` からの除外と `showAuthenticatedNav` の表示は直したが、クリックハンドラの登録(または `renderSettingsPage` 呼び出し前のロール判定)が admin 限定のまま。
- **修正案**: nav-settings のクリック配線を全ユーザーに対して行い、`renderSettingsPage` 内で監査転送セクションのみ `meData.role === 'admin'` で出し分ける。回帰確認: 一般ユーザーで Settings → Timezone select が表示され、変更が `/api/me` に反映されること。

### R-5. キックされた参加者の画面が依然「The session is still running on the backend.」+ Reconnect(E-15 が本番で効いていない) — `internal/httpapi/*`(キックハンドラの `alsoNotify` 配信)/ `web/src/terminal_page.js:1334-1343`
- ✅ 修正済み(コミット 2ba142c、2026-08-29 デプロイ済み)— 調査結果: (1) 端末キックハンドラは E-15 で本人を `alsoNotify` に渡していた(漏れなし)、(2) `myUserId` はページ読込時に `/api/me` で解決済み、(3) **順序が問題**: `DetachUser`(WS 切断)が SSE 配信より先に実行され、`onclose` が先に走って通常の切断画面になっていた。対策は SSE の競合に依存しない方式に変更: SSH/Telnet ブリッジの `DetachUser` がクローズ直前に本人のソケットへ `session_ended: kicked` テキストフレームを送る(同一ソケット上のため必ず `onclose` より先に観測される)。フロントはこのフレームを「セッションから削除されました」+ ホームに戻る の専用画面にマッピングし、`onclose` 側の分岐を抑止(`sawSessionEnded`)。端末キックハンドラも SSE 配信→切断の順に変更。受け入れ条件(Remove 直後に専用画面のみ、Reconnect なし)を満たす。
- **症状(ユーザー報告、2026-08-29 再検証)**: オーナーが Participants モーダルから Remove しても、参加者側は E-15 で追加した「You were removed from this session」画面にならず、通常の切断画面(Reconnect / Back to home)のまま。
- **調査ポイント**: (1) 端末のキックハンドラで本人を `alsoNotify` に渡しているか(VNC/RDP だけ直して端末を漏らしていないか)、(2) `participant_left` の `extra.reason === 'kicked'` と `payload.user_id === myUserId` の一致(`myUserId` が `/api/me` 解決前だと不一致)、(3) `DetachUser` で WS を閉じる前にイベント配信が完了しているか(順序: 通知 → detach)。ブラウザ側は `onclose` が先に走ると `disconnectedWrap` を出してしまうので、`showKickedSessionEnded` を close 後でも上書きできるようにする(kicked フラグを立てて `onclose` 側で分岐)。
- **受け入れ条件**: Remove 直後に参加者側が「セッションから削除されました」+ ホームに戻る のみを表示し、Reconnect を出さない。

### F-2. 【仕様変更要望】「Allow rejoin」ボタンは不要。Remove 後に同じユーザーへ招待を作り直したら自動で再参加許可にする — `web/src/participants_dialog.js`, `internal/httpapi/*`(`allow-rejoin` エンドポイント), `internal/sharing/service.go`(招待作成時の `Unkick`)
- ✅ 対応済み(コミット 7e3f01d、2026-08-29 デプロイ済み)— `POST …/participants/{user_id}/allow-rejoin`(terminal/VNC/RDP)と API クライアント、モーダルの「再参加を許可」ボタンを削除。モーダルには「退出させたユーザー」の一覧のみ残し、「戻したい場合は指名招待を新たに発行」と案内。解除経路は E-16 の「指名招待の新規発行時に `Room.Unkick`」のみ(タグ/グループ/共有リンク招待では解除しない — 現状維持)。受け入れテスト `sharing_reinvite_test.go`: Remove → 指名で Issue → ブロック解除(監査 `session_participant_unkicked`)→ 参加者が新しい招待で Join 成功(招待は成功時に消費)→ allow-rejoin エンドポイントが存在しないこと。
- **要望(ユーザー、2026-08-29)**: 「Allow rejoin ボタンを押すのではなく、remove した後にもう一度当該ユーザーの招待を作成したら自動で Allow rejoin フラグを立てる形式にしてほしい」。
- **対応**: E-16 で実装済みの「指名招待の新規発行時に `Room.Unkick`」を唯一の解除経路にする。Participants モーダルの「Removed users / Allow rejoin」UI と `POST …/participants/{user_id}/allow-rejoin` は削除(または非表示)。モーダルには「退出させたユーザー(再招待で復帰可)」の表示だけ残してもよい。タグ/グループ/共有リンク招待でも当該ユーザーが宛先に含まれるなら解除するかは要判断(現状: 指名招待のみ解除)。
- **受け入れ条件**: Remove → 同ユーザーへ Named user で Issue → 参加者側のホームにバナーが出て Join できる(招待が消費されずに失敗しないこと)。

### R-2 補足(ユーザー要望)
- 「自分に使用する権限の無いボタン(Participants など)は表示しない」= viewer 画面で Participants / Invitations / End session を出さないこと。R-2 の CSS 修正で満たされるはずだが、修正後に viewer でログインして確認すること。

## 1. A: 修正済み・未デプロイ(5 件)

### A-1. RDP/VNC 録画(MP4)がブラウザでシークできない — `internal/httpapi/recordings.go`
- ✅ 修正済み(コミット 51f243a、2026-08-29 デプロイ済み)
- **症状**: 録画プレイヤーで `<video>` の `seekable` が `[0,0]`、`duration` が再生に伴い伸びる(18 分の録画で開いた直後 31 秒表示)。タイムライン操作不能。
- **原因**: `format=mp4` 分岐が `io.Copy(w, serveFile)` で流すだけ → `Content-Length` なし、`Range` 非対応(`Range: bytes=0-0` に 200 で全量応答、`Accept-Ranges` なし)。
- **修正**: `http.ServeContent(w, r, filepath.Base(mediaPath), fi.ModTime(), serveFile)` に変更(`serveFile.Stat()` で ModTime 取得、失敗時 `writeInternalError`)。`Content-Type: video/mp4` と `setAttachmentDisposition` は従来通り。
- **確認方法**: 録画ページ → RDP 録画 → Play → タイムラインをドラッグして任意位置へ移動できること。DevTools で `document.querySelector('video').seekable.end(0) > 0`。

### A-2. タイムゾーン一覧に「UTC」がない — `web/src/timezone.js`
- ✅ 修正済み(コミット 97e62e1、2026-08-29 デプロイ済み)
- **症状**: 設定ページのタイムゾーン select に `UTC` が存在しない(Chrome の `Intl.supportedValuesOf('timeZone')` は地域/都市名のみ返す)。
- **修正**: `SUPPORTED_TIMEZONES` で `zones.includes('UTC') ? zones : ['UTC', ...zones]`。
- **確認方法**: 設定 → タイムゾーンの先頭付近に `UTC` があり、選択すると録画一覧の時刻が UTC 表示になる。

### A-3. 録画プレイヤーのズーム(−/＋/Fit)で毎回再生が止まる — `web/src/recordings_page.js`
- ✅ 修正済み(コミット be33761、2026-08-29 デプロイ済み)
- **症状**: ズーム操作のたびに asciinema プレイヤーが再生成され、ポスター(▶)状態に戻り再クリックが必要。
- **修正**: `playing` フラグを `player.addEventListener('play'|'playing'|'pause'|'ended')` で追跡し、`applyZoom` で再生中なら `createPlayer(startAt, /*autoPlay*/ true)`。`createPlayer(startAt, autoPlay=false)` にシグネチャ変更(`autoPlay` → `opts.autoPlay = true`)。
- **確認方法**: SSH 録画を再生中に「＋」→ 同じ位置から再生が継続する。停止中に押した場合は停止のまま。
- **備考**: asciinema-player は `^3.15.1`。v3 の `addEventListener` API 前提。古いビルドでも try/catch で無害。

### A-4. ファイル(SFTP)一覧の更新日時が生の UTC ISO 文字列 — `web/src/files_page.js`
- ✅ 修正済み(コミット 574d9d4、2026-08-29 デプロイ済み)
- **症状**: `2026-08-16T18:16:22Z` のまま表示。他画面(録画・監査ログ)はユーザーのタイムゾーン/ロケールで整形されており不統一。
- **修正**: `import { formatDateTime } from './datetime.js'` を追加し、`const modTime = formatDateTime(e.mod_time, undefined, e.mod_time || '—')`。
- **確認方法**: ホーム → Files → 日時が `08/17/2026, 03:16:22 AM`(Asia/Tokyo)形式になる。

### A-5. 録画フィルタの既定 From/To が UTC 日付で「To = 昨日」になる — `web/src/recordings_page.js`, `web/src/datetime.js`
- ✅ 修正済み(コミット bae1a7a、2026-08-29 デプロイ済み)。サーバー側 `parseTimeRange`(`internal/httpapi/time_range.go`)は from/to を UTC 暦日として解釈し、`to` は翌 UTC 日 0 時を排他上限にするため、UTC より東のゾーンで「当日」を送っても現在時刻は必ず範囲内。境界日の取りこぼしなし(変更不要)。
- **症状**: JST 08:40 に開くと To が `2026/08/28`(UTC 日付)。
- **修正**: `datetime.js` に `formatDateInputValue(d)`(`Intl.DateTimeFormat('en-CA', {timeZone})` → `YYYY-MM-DD`)を追加し、`defaultDateRange()` で使用。
- **確認方法**: 録画一覧の To が当日(ユーザーのタイムゾーン)になる。
- **注意**: サーバー側のフィルタ解釈(UTC 日付比較か)は未確認。境界日の取りこぼしがないか `internal/httpapi/recordings.go` の from/to 解釈を一度確認すること。

### デプロイ手順(Linux 側)
```
cd /mnt/nas/data/vscode/vantyx
git status            # 上記 5 ファイルが modified のはず
docker build --target frontend -t vantyx-frontend-verify . && docker image rm vantyx-frontend-verify
scripts/deploy.sh     # rsync(compose/.env 除外)+ サーバー側ビルド
```

---

## 2. B: 未修正(要実装)

### B-1. RDP/VNC 録画 MP4 に確定した総再生時間がない(fragmented MP4)
- ✅ 対応済み(コミット f4c7bc9、2026-08-29 デプロイ済み)— 録画停止後(ffmpeg 終了を待ってから)に `ffmpeg -c copy -movflags +faststart` で一時ファイルへ非同期 remux し、「非 fragmented かつサイズ>0」を検証できた場合のみ差し替え(失敗時は元ファイルを残し `recording_remux_failed` を監査、成功時 `recording_remux_ok`)。エクスポート用のガバナー枠を使うためライブ録画の開始を妨げない。起動 30 秒後に既存録画の一括変換(`startRecordingRemuxBackfill`)も実行。**本番実績**: 起動時バックフィルで対象 2 件中 1 件(a164b1c7…、18 分の RDP 録画)を変換、`ffprobe` で総時間 1077 秒を確認。残り 1 件(0155172a…)は RDP 録画修正前の行で実体ファイルが存在せず対象外。`recording.IsFragmentedMP4`(top-level `moof` 検出)と ffmpeg 往復テストを追加(ffmpeg のない builder では skip)。
- **症状**: `empty_moov` + `frag_keyframe` で書かれた MP4 は moov に duration を持たず、ブラウザは読み込んだ分だけ `duration` を伸ばす(A-1 の Range 対応後もタイムラインの総時間は「読み込んだところまで」になる)。
- **関係コード**: `internal/recording/video_recorder.go`(ffmpeg 引数 `-movflags +frag_keyframe+empty_moov`)、`internal/httpapi/video_recording.go` の `stopVideoRecordingHandle` / `finishVideoRecording`、`internal/recording/vnc_capture.go`。
- **提案**: 録画停止後(SIGINT で ffmpeg が終了した後)に `ffmpeg -i in.mp4 -c copy -movflags +faststart out.mp4` で remux して差し替える(非同期でよい。失敗時は元ファイルを残す)。既存の録画は Downloads の export ジョブ経由か一括バッチで変換可能。
- **優先度**: 中(再生・シークは A-1 で可能になる。総時間表示だけの問題)。

---

## 3. C: 環境要因(コード修正不要・ユーザー対応)

### C-1. ターゲット `x11spi-tf-1`(10.10.70.31:22, SSH)へ接続不可
- 監査ログ: `terminal_bridge_end_error: dial tcp 10.10.70.31:22: i/o timeout`
- サーバー(10.10.10.70)から `10.10.70.31:3389` は open、`:22` は timeout。他ターゲット(10.10.10.11/12, 10.10.110.105 の :22)は open。
- 10.10.70.31 は Windows 開発機自身。Windows 側 OpenSSH Server の受信規則(サブネット制限/プロファイル)を確認すること。Vantyx 側は「接続が閉じられました」を正しく表示しており挙動は正常。

---

## 4. 検証できなかったもの(理由)

| 項目 | 理由 |
|---|---|
| RDP 実接続・全画面トグル・RDP 録画生成 | 唯一の RDP ターゲットが操作中の Windows 機自身(接続するとデスクトップセッションを奪う) |
| VNC 接続・録画(末尾切れ修正の実機確認) | VNC ターゲット未登録 |
| FTP / TFTP 転送 UI | 該当ターゲット未登録 |
| ファイルのダウンロード(ブラウザ保存) | 自動操作ではダウンロード実行不可(要ユーザー操作) |
| ログイン/ログアウト/パスワード変更 | パスワード入力は自動操作しない方針 |
| 共同セッション(招待リンクでの参加・watch) | 第 2 ユーザーのログインが必要 |

---

## 5. 正常確認済み(2026-08-29 実操作)

- ホーム: グループツリー展開、サーバー一覧、Connect ダイアログ(保存済み認証情報)
- 録画ページ: サーバー一覧/録画一覧の列固定レイアウト(ボタン縦揃え)、RDP MP4 再生(1920×1080)、SSH cast 再生、ズーム −/＋/Fit の描画
- タイムゾーン: `America/New_York` → 一覧表示が `08/25/2026, 08:26:33 PM` に変化、`/api/me` に永続化、Auto に復帰
- ブラウザ端末(malkuth): 接続、コマンド実行、vim 起動/編集/`:wq`、`exit` 後に「セッション終了」オーバーレイのみ表示(接続フォーム・エラー文の重複なし)
- その録画の再生: `r` イベント(180x52→180x49→180x47)記録、vim 終了後の行が正しく分離して描画
- SFTP Files: ルート一覧、ディレクトリ移動、空フォルダ表示
- Sessions / Downloads / Server management / User management / Credentials / Audit log / Settings 各ページ表示
- 言語切替 English ⇄ 日本語

---

## 6. 追加テスト(2 回目のパス)で見つかった軽微な問題(未修正・要対応)

### E-1. アカウントページでナビの選択状態が前ページのまま残る — `web/src/account_page.js`(または該当ページの render)
- ✅ 修正済み(コミット 1682265、2026-08-29 デプロイ済み)— `renderUserInfo` 先頭で `setActiveNav('account')`(該当キーなし → 全解除)。
- **症状**: ナビ右端の「admin」ボタンでアカウントページを開くと、直前に見ていた「Recordings」がアクティブ(下線)のまま。
- **修正案**: アカウントページの render 先頭で `setActiveNav('account')`(該当キーがなければ `setActiveNav(null)` 相当で全解除)を呼ぶ。他ページは `setActiveNav('settings')` 等を呼んでいる(`web/src/settings_page.js:11` 参照)。

### E-2. モーダルダイアログが Escape キーで閉じない — `web/src/ui_dialog.js` / 各ダイアログ実装
- ✅ 修正済み(コミット f4ecc0b、2026-08-29 デプロイ済み)— `uiConfirm`/`uiAlert` は Escape = ×/外側クリックと同じ結果(capture で先取り)。ページ側モーダル(`#…-modal` コンテナ、および招待/参加者ダイアログのような動的 wrap)は document の 1 つのリスナーが最前面のものを判定し、その close/cancel ボタンを click することで各モーダル固有の後処理を経由して閉じる。
- **症状**: 「Edit tags」「SSH public keys」など複数のモーダルで Escape を押しても閉じない(× / Cancel は動作)。
- **修正案**: 共通のモーダル生成箇所で `keydown` (Escape) を拾って close を呼ぶ。確認ダイアログ(uiConfirm)も同様に Escape=Cancel にする。

### E-3. 監査ログ「File transfers」タブに、コマンドログ用の注記が表示される — `web/src/audit_page.js`
- ✅ 修正済み(コミット 843e0e0、2026-08-29 デプロイ済み、E-5 と同一修正)— 注記 `<p id="audit-cmd-note">` を `setActiveTab` で Command log タブのときだけ表示。
- **症状**: File transfers タブの下部に "Note: command logs prefer PTY display lines (including Tab completion) at Enter time…" が出る(Command log タブ専用の文言)。
- **修正案**: 注記要素をタブ切替時に Command log タブでのみ表示する。

### E-4. ファイル操作の成功が監査ログに残らない(削除・ダウンロード)/ アップロード成功に user_id がない — `internal/httpapi/files.go`, `internal/httpapi/file_transfers_jobs.go`
- ✅ 修正済み(コミット c7feb6c、2026-08-29 デプロイ済み)— `files_remove_ok` / `files_download_ok`(user_id, target_id, path[, size])を追加、`files_upload_ok` に `user_id` を追加。回帰テスト: `files_audit_test.go`。
- **症状**: SFTP で `/tmp/vantyx_sftp_upload_test.txt` をアップロード→削除したところ、監査ログ(`/api/audit`)には `files_upload_ok` のみ。削除成功イベントは無し(`files_remove_failed` など失敗系しか `audit()` していない: `files.go:419` 付近の `handleDeleteFile`、`files_open_failed` のみのダウンロード `files.go:288` 付近)。
- また `files_upload_ok`(`file_transfers_jobs.go:246`)の fields は `path / target_id / transfer_id` のみで `user_id` が無く、監査ログ画面の User 列が「—」になる(File transfers 履歴側には admin が出る)。
- **修正案**: `handleDeleteFile` 成功時に `files_remove_ok`(user_id, target_id, path)、ダウンロード成功時に `files_download_ok` を audit する。`files_upload_ok` に `user_id` を追加。監査ログ画面の Kind=Files フィルタ(`files_` プレフィックス)にそのまま乗る。
- **優先度**: 中〜高(ファイル削除はセキュリティ監査上残すべき操作)。

### E-5. (E-3 の補足)監査ログの注記はすべてのタブで表示されている — `web/src/audit_page.js:357`
- ✅ 修正済み(コミット 843e0e0、E-3 と同一)。
- `t('audit.cmdNote')` の `<p>` がタブ切替と無関係に常時描画されている(Audit log / File transfers タブでも表示)。Command log タブのみで表示するか、タブパネル内に移動する。

### E-6. Downloads(録画エクスポート)一覧のセッションラベルが生の UTC 文字列 — `web/src/recording_exports_page.js`
- ✅ 修正済み(コミット f344023、2026-08-29 デプロイ済み)— ラベル/タイトル生成の `recording_started_at` を `formatDateTime` で整形。
- **症状**: Session 列の 2 行目「verify-browser-ui · vim / exit overlay check · **2026-08-28T23:40:04Z** · browser」。Queued/Updated 列は整形されているのにラベル内の started_at だけ未整形。
- **修正案**: ラベル生成箇所で `formatDateTime(job.started_at ...)` を使う(同ファイル 45 行付近の helper を流用)。

### E-7. Sessions ページの「File transfers」欄に完了したアップロードが出ない(観察のみ)
- ✅ 修正済み(コミット 401533f、2026-08-29 デプロイ済み)— 仕様ではなくバグ。`file_transfer_manager.js` が完了/失敗/取消ジョブを完了 8 秒後(`TERMINAL_DISPLAY_MS`)にローカルの一覧から削除し、再読込時も 8 秒より古いスナップショットを墓標化して取り込まなかったため、「実行中 + 直近完了 10 件」を出す設計の Sessions ページが空になっていた。完了ジョブはサーバーが返す間(サーバー側で直近分に限定済み)保持し、8 秒の窓は画面下部のコンパクトなバーの表示のみに適用するよう分離。
- 52 B のアップロード完了直後に Sessions ページを開いても "No active or recent file transfers"。監査ログの File transfers タブには Completed 100% で記録されている。仕様(完了済みは表示しない/一定時間で消える)なら問題なし。意図と違うなら `web/src/file_transfer_manager.js` の表示条件を確認。

### 正常確認済み(2 回目のパス)
- テーマ切替(ダーク⇄ライト)、アカウントページ表示
- サーバー管理: グループ追加 → サーバー追加(ホスト鍵 Re-fetch → 「Trust and register」)→ 「Select an identity」バリデーション → 手動認証で登録 → 名前編集(Update)→ サーバー削除 → グループ削除(タグ/メンバーも同時削除)。テストデータ `zz-uitest` / `uitest-yesod` は削除済み
- タグ編集ダイアログ、メンバー追加ダイアログ(「No more users to add」)表示
- ユーザー管理: Add user / Edit user(タグ)/ SSH public keys の各ダイアログ表示(ユーザー作成はパスワード入力を伴うため未実施)
- 資格情報: Generate key(ED25519)→ 秘密鍵の一度きり表示 → 削除、Add identity ダイアログ表示
- 監査ログ: Kind=Terminal / Kind=Files フィルタ + Apply、Command log で `vim` 検索(本日の `vim /tmp/vantyx_verify.txt` がヒット)、File transfers タブで Apply(アップロード履歴 Completed 100% 52 B)
- 録画エクスポート: cast → MP4(H.264、208 KB、`ftypisom`)/ GIF(agg、95 KB、`GIF89a`)とも数秒で Ready。Downloads ページで一覧表示 → Delete(確認ダイアログ)→ 空に戻ることを確認。ファイル実体は fetch で取得して検証(ブラウザ保存は未実施)
- セッション: Connect(セッション名付き)→ 端末で入力 → **Back でデタッチ(タブが閉じる)** → Sessions ページに「uitest-detach-attach」が残る → Reconnect で新タブに再アタッチ(以前の出力 `DETACH_TEST_1` が復元・入力継続可)→ End session(確認ダイアログ)→ 一覧から消える
- 共同セッション招待: Invitations ダイアログ(Named user / By tag / By group / Shareable link、有効期限 15m/1h/4h)。Shareable link(Single use)を Issue → URL 生成・Existing invitations に Pending で表示(Show link / Reissue / Delete)→ Delete(確認)で削除。※参加テストは第 2 ユーザーが必要なため未実施
- SFTP: `/tmp` へテストファイルをアップロード(52 B、一覧に即反映)→ 削除(確認ダイアログ)→ 一覧から消える
- ホーム: 「Active sessions (0)」ダイアログ(再接続可能セッション無しの案内 + Show all sessions リンク)
- API リファレンス(`/docs`): OpenAPI 0.7.0 / OAS 3.1 の Swagger UI 表示(自己署名証明書に関する注記付き)
- 録画フィルタ: Channel=rdp で 0 件 / 全体で 4 件。※「← Back to servers」→別サーバーの View recordings に移っても直前の Channel 選択が select に残る(値どおりに動作するので不具合ではないが、サーバー切替時にリセットした方が親切)

---

## 8. 権限テスト(複数ユーザー)— 2026-08-29

テストユーザー(ユーザー作成・ログインはユーザー本人が実施、設定・検証は Claude が実施):

| ユーザー | Role | 付与したアクセス | 期待 |
|---|---|---|---|
| `uitest-tagged` | user | タグ `home_proxmox` | home/proxmox(malkuth, yesod)のみ |
| `uitest-member` | user | `home/lan` のメンバー | home/lan(x11spi-tf ×2)のみ |
| `uitest-admin` | admin | なし | admin と同等 |

基準(admin): targets 5 / groups 4(home, home/lan, home/proxmox, home/server)/ recordings 8。

### 8.1 `uitest-tagged`(user, タグ home_proxmox)— すべて期待どおり
- ナビ: Home / Sessions / Recordings / Downloads のみ(Server/User management, Credentials, Audit log, Settings, API reference は非表示。`web/src/nav.js:48 isAdminOnlyNav`)
- ホーム/録画ページのツリー: `home/proxmox` のみ(lan / server は非表示)。ターゲット一覧 malkuth, yesod(Files / Connect / Active sessions)
- API: `/api/targets` → 2 件(malkuth, yesod)、`/api/groups` → home/proxmox のみ、`/api/recordings` → 自分の分のみ
- 403(管理者のみ): `/api/users`, `/api/audit`, `/api/ssh-keys`, `/api/credential-identities`, `/api/groups/home%2Flan/members`, `PUT /api/users/uitest-tagged/tags`(自己タグ昇格), `POST /api/targets`, `DELETE /api/targets/malkuth`, `POST /api/groups`, `POST /api/users`, `POST /api/targets/probe-host-key`, `/api/recordings?user_id=admin`
- 403(アクセス不可): `/api/targets/vscode/files`, `/api/targets/x11spi-tf-1/files`。`/api/targets/malkuth/files?path=/tmp` は 200
- 他ユーザーの録画ファイル `/api/recordings/<admin の id>/file` → 404(存在を漏らさない)
- 直リンク `/files?target_id=vscode` → 「アクセスが許可されていません」表示。`/terminal?target_id=vscode` → Connect すると WS が閉じられ接続不可(→ E-9 参照)
- 許可ターゲット malkuth への実接続 → コマンド実行 → exit まで正常。録画は自分の 1 件のみ一覧に出る。User フィルタ自体が非管理者には表示されない
- 自分の設定: `PUT /api/me/timezone` 200、アカウントページ(Change password ボタン)表示可

### 8.2 `uitest-member`(user, `home/lan` メンバー)— すべて期待どおり
- ナビ: 一般ユーザー用 4 項目のみ。ツリー: `home/lan` のみ(proxmox / server 非表示)
- API: `/api/targets` → x11spi-tf, x11spi-tf-1 の 2 件、`/api/groups` → home/lan のみ、`/api/recordings` → 0 件
- 403: `/api/users`, `/api/audit`, `/api/targets/malkuth/files`, `/api/targets/vscode/files`, `PUT /api/users/uitest-member/tags`, `GET/POST /api/groups/home%2Flan/members`(メンバー本人でもメンバー管理は不可), `/api/recordings?user_id=admin`
- `/api/recordings?target_id=malkuth` → 200 で空(権限外ターゲット指定でも他人の録画は漏れない)
- 備考: `/api/targets/x11spi-tf-1/files`(許可ターゲットだが C-1 の理由でホスト到達不可)は fetch が "Failed to fetch" で終わる(サーバーが接続を切る/長時間応答なし)。到達不可時も JSON エラー(502/504 相当)で返す方が UI で扱いやすい(軽微)

### 8.3 `uitest-admin`(後から作成した admin)
- 管理 API はすべて 200: `/api/users`, `/api/audit`, `/api/ssh-keys`, `/api/groups/home%2Flan/members`, 他ユーザーのタグ変更 `PUT /api/users/uitest-tagged/tags`, `/api/recordings?user_id=<他人>`
- **しかしターゲット/グループの可視性はメンバーシップ基準のまま**: ログイン直後は `/api/targets` 0 件、`/api/groups` 空、`/api/targets/malkuth/files` → 403、ホーム/録画/Server management いずれも「No groups」。組み込み admin が全部見えるのは、グループ作成時に自動でメンバー登録されているため(→ E-10)
- `POST /api/groups/home%2Fproxmox/members {user_id: uitest-admin}` は 204 で通り、その後 proxmox 配下(malkuth, yesod)が見えるようになった(admin は自己追加でアクセス範囲を広げられる)
- `DELETE /api/users/<id>` → **405**。User management 画面にも Delete ボタンが無い(→ E-11)

### E-10. 追加 admin が既存グループ/ターゲットを管理できない — `internal/httpapi/groups_handler.go` / targets 一覧のアクセス判定
- ✅ 修正済み(コミット 32e0068、2026-08-29 デプロイ済み)— 方針 (a) を採用: admin ロールは `GET /api/groups` / `GET /api/targets` で全グループ・全ターゲットを取得(`AccessGroupStore.AllGroupIDs` / `TargetStore.AllIDs` を追加)。admin 専用のターゲット作成/更新(グループ移動)/削除ハンドラにあった「操作する admin 自身が対象グループのメンバーであること」の再チェックを撤廃。一般ユーザーは従来どおり。**接続・録画閲覧の範囲は「admin でもグループ配属(メンバーシップ/タグ)が必要」で確定**(2026-08-29 ユーザー判断)。管理 API のみ admin 全許可。コード変更なし。回帰テスト: `admin_visibility_test.go` + 旧方針を固定していた 3 テストを反転。
- **症状**: role=admin でも `/api/groups` `/api/targets` はメンバーシップ/タグで絞られるため、後から作った admin は Server management で既存グループが見えず、編集・削除・サーバー追加ができない(API を直接叩いてメンバー自己追加すれば見える)。組み込み `admin` はグループ作成時の自動メンバー登録で全件見えているだけ。
- **修正案(要方針決定)**: (a) admin ロールは一覧/管理 API でアクセス制御をバイパスして全グループ・全ターゲットを返す(接続/録画閲覧も admin は全許可にするか、管理のみ全許可にするかを決める)。(b) 現行方針を維持するなら、admin 作成時に既存全グループへ自動メンバー登録し、グループ作成時も全 admin を自動追加する。少なくとも Server management は admin に全件表示すべき。
- **優先度**: 高(運用上、2 人目の管理者が実質何も管理できない)。

### E-11. ユーザー削除ができない — `internal/httpapi/users_handler.go`, `web/src/users_page.js`
- ✅ 修正済み(コミット b0a2fcc、2026-08-29 デプロイ済み)— `DELETE /api/users/{id}`(admin のみ。自分自身は 400、最後の admin は 409)。ライブの端末/VNC セッション停止 → users 行削除(user_groups / user_tags / sessions / user_ssh_keys / file_transfer_jobs は ON DELETE CASCADE)→ 本人が発行・宛先の招待を取消 → 監査 `user_delete`。User management に Delete ボタン(確認ダイアログ)。テストユーザー `uitest-tagged` / `uitest-member` / `uitest-admin` は本番から削除済み(各 204)。回帰テスト: `users_delete_test.go`。
- **症状**: `DELETE /api/users/{id}` → 405、UI にも Delete が無い。退職者や今回のテストユーザー(`uitest-tagged` / `uitest-member` / `uitest-admin`)を消す手段が無い。
- **修正案**: `DELETE /api/users/{id}`(admin のみ、自分自身と最後の admin は拒否、関連: グループメンバー・タグ・公開鍵・セッション・招待の cascade、監査 `user_delete`)+ User management 行に Delete ボタン(確認ダイアログ)。実装後、上記 3 テストユーザーを削除すること。

### 8.4 セッション共有・参加(オーナー `uitest-admin` / 参加者 `uitest-tagged`、別ブラウザでユーザー本人が参加操作)— 機能はすべて正常
- 招待候補リスト: 対象ターゲットにアクセス権のあるユーザーのみ表示(admin, uitest-tagged。lan のみの uitest-member は非表示)✅
- 指名招待(viewer, 1h)発行 → 参加者側が参加 → 参加者側は「View-only (uitest-admin holds control)」、オーナー側の Participants に `uitest-tagged / Viewer / Remove` ✅
- オーナーの出力が参加者へリアルタイム配信 ✅、参加者の入力は View-only でブロック ✅
- 参加者 Request control → オーナー側にオレンジのバナー「uitest-tagged is requesting control」+ Review → Grant ダイアログ(Deny / Grant)✅
- Grant 後: オーナー側は「View-only (uitest-tagged holds control)」+ Request control ボタン、参加者は Writer。オーナーのキー入力はブロックされ、参加者の `echo VIEWER_HAS_CONTROL` がオーナー画面に表示 ✅
- 参加者 Release control → オーナーに操作権が戻る(赤バナー「You hold the write token」)✅
- Remove(確認ダイアログ)→ 参加者一覧が「Only the owner is in this room」に ✅
- 監査ログ: `session_write_request_created` → `session_write_token_granted` → `session_write_token_released` → `session_participant_kicked` が user_id / session_id 付きで記録 ✅

### 8.4b 参加側を Claude が操作(参加者 `uitest-tagged` = Chrome、オーナー `admin` = 別ブラウザでユーザー本人)— 機能は正常
- 招待受信: ホーム上部にバナー「admin invited you to a shared session (target: malkuth)」+ Join。`/api/invitations/incoming` に mode=viewer / expires_at 付きで 1 件 ✅
- Join → 新タブ `/terminal?session_id=…&mode=viewer&target_id=malkuth` が開き、青バナー「View-only (admin holds control)」+ Request control ✅。キー入力は端末に送られない ✅
- Request control → バナー「View-only — waiting for approval」+ Sent ✅ → オーナーが Grant → 赤バナー「You hold the write token」+ Release control に切替、`echo …; whoami` の入力・出力が動作 ✅
- Release control → 確認「Hand control back to the owner?」→ OK → 「View-only (admin holds control)」に戻る ✅
- 参加者(viewer)による API 悪用の拒否: `DELETE /api/terminal/sessions/{id}`(End session)、`GET/POST …/invitations`、`DELETE …/participants/admin`(オーナーのキック)→ すべて **404「セッションが見つからないかアクセスが許可されていません」** ✅(存在を漏らさない)

### E-15. キックされた参加者の画面が「The session is still running on the backend.」+ Reconnect になる — `web/src/terminal_page.js`(disconnectedWrap の分岐)
- ✅ 修正済み(コミット 7c1e221、2026-08-29 デプロイ済み)— 原因は補足の (a): `publishSharingEvent` / `publishSharingEventForSession` の配信先が「オーナー + ルームの**現在の**参加者」で、キック時点で本人は既に `RemoveParticipant` により参加者リストから外れているため、`participant_left reason=kicked` が本人に届いていなかった(フロントの `showKickedSessionEnded` 実装自体は正しかった)。両関数に追加受信者(`alsoNotify`)を持たせ、端末/VNC/RDP のキックハンドラで本人を明示指定。フロント側: キック画面に「ホームに戻る」ボタンを追加、viewer モードで再接続をサーバーに拒否された場合はオーナー用の SSH 資格情報フォームではなく専用の「この共有セッションに参加できません」画面(+ ホームに戻る)を表示。
- **症状**: オーナーが Remove すると、参加者側は通常の切断画面(「The session is still running on the backend.」+ Reconnect / Back to home)が表示され、キックされたことが分からない。Request control ボタンやオーナー専用ボタンも残る。
- **修正案**: サーバーからのキック通知(close code / `session_ended` 系フレームに reason=kicked)を UI で判別し、「セッションから削除されました」専用メッセージ + Back to home のみを表示する。
- **Reconnect を押した場合**: WS はサーバーに拒否され(再参加不可、`/api/invitations/incoming` も空 = 招待は消費済み)、セキュリティ上は正しい。しかし UI はオーナー用の「Enter the target SSH credentials」フォーム(SSH username / password 入力)+「WebSocket connection failed.」にフォールバックする。viewer モード(`mode=viewer` の URL)では資格情報フォームを出さず、「セッションに参加できません(削除済み/招待の期限切れ)」+ Back to home にする。

### E-16. 一度 Remove(キック)した参加者は、オーナーが新しい招待を発行しても再参加できない — `internal/sharing/registry.go:165-188, 211-224`, `internal/sharing/service.go:68-77`
- ✅ 修正済み(コミット ba4f948、2026-08-29 デプロイ済み)— 原因分析どおり。`Room.Unkick` を追加し、オーナーが同じユーザー宛の**指名招待**を新規発行した時点でブロック解除(端末/VNC/RDP 共通、監査 `session_participant_unkicked`)。リンク/タグ/グループ招待では kicked を維持。`JoinRoom` は `IsKicked` を ACL 再チェック・`RecordUse` より前に評価するため、キック済みユーザーの試行で招待が消費されなくなった。回帰テスト: `registry_test.go` `TestRoom_UnkickAllowsRejoin`、`service_test.go` `TestService_JoinRoom_KickedUserDoesNotConsumeInvitation`。案 3(Removed users / Allow rejoin の UI)は F-1 のモーダル内で対応。
- **症状(ユーザー報告・再現済み)**: オーナーが Remove した後、同じユーザー宛に招待を作り直しても、参加時に「このセッションから削除されたため再参加できません」(`ErrUserKicked`)となり参加できない。セッションを作り直さない限り解除手段が無い。
- **原因**: `Room.RemoveParticipant` が `r.kicked[userID]` に記録し、`AddViewer` / `Service.JoinRoom` が `IsKicked` で恒久的に拒否する。kicked を解除する API/UI が存在しない。
- **付随バグ**: `JoinRoom` は `Store.RecordUse`(招待の消費)を **`IsKicked` チェックより前**に実行するため(`service.go:68` → `72`)、キック済みユーザーが新しい single-use 招待で参加しようとすると、拒否されるのに招待だけ消費されて無効になる。
- **修正案**:
  1. オーナーが同じユーザー宛の指名招待を新たに発行した時点で `room.Unkick(inviteeUserID)`(明示的な再招待 = 再入室許可とみなす)。共有リンク/タグ/グループ招待では kicked を維持(キックの意図を尊重)。
  2. `JoinRoom` で `IsKicked` を `RecordUse` より前に評価し、招待を消費しない。
  3. オーナー UI: Participants から消えた後も「Removed users」として表示し、「Allow rejoin」ボタンで解除できるようにする(任意)。
- **テスト**: `registry_test.go:143 TestRoom_KickedUserCannotRejoin` は現行仕様を固定しているので、再招待で解除されるケースを追加する。
- **優先度**: 中(運用上、誤ってキックすると復旧にセッション再作成が必要)。

### F-1. 【機能要望】Participants 一覧を常時表示ではなく、Invitations と同じボタン → ポップアップ表示にする — `web/src/terminal_page.js:1455-1508 renderParticipantsPanel`, `web/src/invite_dialog.js`
- ✅ 対応済み(コミット 918ba7f、2026-08-29 デプロイ済み)— ヘッダーに「参加者 (N)」ボタン(Invitations の隣、オーナーのみ)を追加し、`participants_dialog.js` のモーダルで一覧(Remove 付き)を表示。常設パネル `#sharing-participants-panel` は廃止(旧バンドルの残骸があれば除去)。N は viewer 数で、参加/退出(SSE)や Remove 後に更新され、モーダルが開いていれば再描画。同モーダル内に「退出させたユーザー」+「再参加を許可」を実装(E-16 案 3): `GET …/participants` に `kicked` を追加、`POST …/participants/{user_id}/allow-rejoin`(terminal/VNC/RDP、オーナーのみ、監査 `session_participant_unkicked`)。回帰テスト: `sharing_allow_rejoin_test.go`。
- **要望(ユーザー)**: 現状オーナー画面ではヘッダー直下(`#sharing-status-banner` の後)に `#sharing-participants-panel`(テーブル: Username / Status / Remove)が常時挿入され、参加者が多いと端末領域が圧迫される。Invitations ボタンと同様に、ヘッダーの **「Participants」ボタン → モーダル** で表示してほしい。
- **実装案**:
  1. ヘッダーに `Participants (N)` ボタンを追加(`locales` に `terminal.actionParticipants` は既に存在: en.js:537、`sharing.participantsCount` も en.js:658 にある)。N は `cachedParticipants` の viewer 数、`refreshParticipants` のたびに更新。
  2. `renderParticipantsPanel` の中身(テーブル + Remove ボタン + `kickParticipant`)を `invite_dialog.js` と同じモーダル形式の `participants_dialog.js` に移し、SSE(`participant_joined` / `participant_left` / `session_change`)受信時にモーダルが開いていれば再描画する。
  3. 常設パネル `#sharing-participants-panel` は廃止。代わりに「N 名参加中」の短いインジケータをバナー右端に出す程度に留める(任意)。
  4. E-16 の「Removed users / Allow rejoin」を実装するなら同じモーダル内に置く。
- **確認方法**: 2 ユーザー以上で参加 → 端末領域の高さが参加者数で変わらないこと、モーダルから Remove できること、参加/退出でカウントが更新されること。

### E-15 補足(コード確認結果)
- `terminal_page.js:1334-1343` には `participant_left` で `payload.extra.reason === 'kicked'` かつ `payload.user_id === myUserId` のとき `showKickedSessionEnded()`(「You were removed from this session」)を出す実装が**既にある**。実機では発火せず通常の切断画面になったので、(a) サーバー側がキック時に `participant_left` を `extra.reason='kicked'` 付きで**キックされた本人にも**送っていない、(b) `DetachUser` で WS が先に閉じられ SSE/フレームが届く前に `onclose` の切断画面が出る、(c) `myUserId` 未解決、のいずれかを疑う。`internal/sharing/service.go:82 KickParticipant` → `ctrl.DetachUser` の順序と、イベント配信先(オーナーのみ/全員)を確認すること。

### E-14. 参加者(viewer)画面にオーナー専用ボタン(Invitations / End session)が表示される — `web/src/terminal_page.js:88, 507-513`
- ✅ 修正済み(コミット e70c5d5、2026-08-29 デプロイ済み)— viewer モードでは End session を非表示にし「退出」(`terminal.actionLeave`)ボタンを表示(タブを閉じるだけ。オーナーのセッションは継続)。Invitations/Participants は既存の同期処理で viewer には非表示。
- **症状**: 参加者側にも Invitations / End session ボタンが出る。Invitations はクリックしても何も起きない(`sharingMode === 'viewer'` で return)。End session は確認ダイアログが出た後 API が 404 で失敗する(サーバー側は正しく拒否)。
- **修正案**: `sharingMode === 'viewer'` のとき両ボタンを非表示にし、代わりに「Leave session(退出)」を出す。

### E-12. 招待の発行・参加・削除が監査ログに残らない — `internal/httpapi/vnc_sessions.go` / `internal/sharing/service.go` 周辺
- ✅ 修正済み(コミット d1e9c33、2026-08-29 デプロイ済み)— 原因分析の補正: **端末セッション**の招待は発行(`session_invitation_created`)・取消(`_revoked`)・リンク再発行(`_link_regenerated`)・参加(`_consumed` + `session_participant_joined`)とも当時から監査されていた(`sharing.go`)。`/api/audit` は既定 200 件で、`http_request` イベントが大量に混ざるため検索時に押し出されていたのが「見当たらない」原因。実際に欠けていたのは **VNC/RDP 共通の招待作成パス**(`sharing_invite_request.go`: 指名/リンク/タグ/グループの全分岐)で、ここに `session_invitation_created`(user_id, session_id, target_id, kind, inv_id, mode, is_link, invitee[, tag|group])を追加。VNC/RDP の取消・再発行・キック(`sharing_kind.go`)と参加(`sharing_join_common.go`)は既存の監査あり。回帰テスト: `sharing_audit_test.go`。
- **症状**: 上記テストで `session_write_request_created` 以降は記録されるが、招待作成(指名/共有リンク)・参加者の参加(JoinRoom)・招待削除・共有リンクの Reissue に対応する監査イベントが見当たらない(`/api/audit` を `invit|join|session_` で検索)。誰がいつ誰を招待し、誰が参加したかが後から追えない。
- **修正案**: `session_invitation_created`(inviter, invitee/tag/group/link, role, expires)、`session_invitation_deleted`、`session_participant_joined`(user_id, session_id, invitation_id)を audit する。
- **優先度**: 中〜高(共同セッションはセキュリティ監査対象)。

### E-13. 録画を削除できない — `internal/httpapi/recordings.go`, `web/src/recordings_page.js`
- ✅ 修正済み(コミット bec197c、2026-08-29 デプロイ済み)— `DELETE /api/recordings/{id}`(admin のみ)。DB 行 → 実体ファイル(`VANTYX_RECORDINGS_DIR` 配下であることを read パスと同じ規則で検証、旧命名のサニタイズ済みファイル名にもフォールバック)→ 派生エクスポート(実行中はキャンセル)を削除し、監査 `recording_deleted`。録画一覧に Delete ボタン(admin のみ・確認ダイアログ)。テスト録画は本番から削除済み: `verify-browser-ui` ×2、`uitest-detach-attach`(各 204)。`uitest-tagged-session` / `uitest-sharing` は DB に存在しなかった(残存録画は admin の 6 件のみ、うち `test` 1 件はユーザー作成分のため残置)。回帰テスト: `recordings_delete_test.go`(格納ディレクトリ外のパスは削除しないケース含む)。
- **症状**: `DELETE /api/recordings/{id}` → 405、録画一覧にも Delete が無い。テスト録画(`verify-browser-ui`, `uitest-detach-attach`, `uitest-tagged-session`, `uitest-sharing`)を消す手段が無い。保持期限の運用もできない。
- **修正案**: admin 用 `DELETE /api/recordings/{id}`(cast/mp4 実体 + 派生エクスポートも削除、監査 `recording_deleted`)+ 一覧の Delete ボタン。必要なら保持日数による自動削除。

### 8.5 テスト後の後片付け状態
- 削除済み: `home/lan` の `uitest-member` メンバー登録、`uitest-tagged` のタグ、`uitest-admin` の `home/proxmox` メンバー登録(すべて API で 204/200)。テストユーザー 3 人はアクセス権ゼロの状態で残存
- **残存(削除手段なし → E-11 / E-13 実装後に削除すること)**: ユーザー `uitest-tagged` / `uitest-member` / `uitest-admin`、録画 `verify-browser-ui` `uitest-detach-attach`(admin)、`uitest-tagged-session`(uitest-tagged)、`uitest-sharing`(uitest-admin)

### E-8. 一般ユーザーがタイムゾーンを設定できない — `web/src/nav.js:48-57`, `index.html`
- ✅ 修正済み(コミット 5f13504、2026-08-29 デプロイ済み)— Settings を全ユーザーに表示(`isAdminOnlyNav` から除外、`showAuthenticatedNav` で表示)。監査転送セクションだけ `meData.role === 'admin'` で出し分け(マークアップと配線の両方)。
- **症状**: Settings(言語・タイムゾーン・監査転送)が admin-only ナビのため、一般ユーザーはタイムゾーン設定 UI に到達できない(API `PUT /api/me/timezone` 自体は 200 で使える)。言語はナビの select で変更可。
- **修正案**: Settings を全ユーザーに表示し、監査転送セクションだけ admin 条件で出し分ける。または Account ページに Timezone select を移す。

### E-9. 権限外ターゲットへの端末接続失敗が「ネットワーク/認証情報を確認」と表示される — `web/src/terminal_page.js`, `internal/httpapi/terminal.go`
- ✅ 修正済み(コミット ea11c49、2026-08-29 デプロイ済み)— WebSocket ハンドシェイクでは 403 をブラウザが読めないため、権限なしの場合はアップグレードを完了してから `error: <common.forbidden のローカライズ文言>`(日本語なら「アクセスが許可されていません」)のテキストフレームを送って閉じる(`getSessionAndTargetForWS`)。監査 `terminal_ws_forbidden`(user_id, target_id)を記録(従来は未記録)。通常の HTTP リクエストは従来どおり 403。フロントは受信した `error:` フレーム本文をそのまま表示するため専用マッピング不要。回帰テスト: `TestHandleSSHWebSocket_ForbiddenTarget_RealWSGetsErrorFrame`。
- **症状**: `/terminal?target_id=<権限なし>` で Connect すると "The connection was closed. Check the target, network and saved credentials." と出て、権限不足と分からない。
- **修正案**: WS ハンドシェイク時に 403 相当のエラーフレーム(例 `error: forbidden`)を返し、UI 側で専用メッセージ(`アクセスが許可されていません`)にマッピングする。監査ログに `terminal_ws_forbidden`(user_id, target_id)を記録することも確認する(未確認)。

## 7. 未実施(理由)— 再掲

RDP 実接続/全画面、VNC、FTP/TFTP、ファイルのブラウザ保存、ログイン/ログアウト/パスワード変更/ユーザー作成、招待リンクでの参加。詳細は §4。
