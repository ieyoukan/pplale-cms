# pplale-cms

PPLALE のカードデータを管理し、[PPLALE-web](https://github.com/ieyoukan/PPLALE-web) へ
**Pull Request として**反映する CMS。Discord ログイン必須。

- 画像は 800px WebP (quality 80) と OGP 用 240px PNG の 2 枚に自動変換され、同じ PR に含まれる
- `main` への直接 push は行わない（コード上も禁止されている）
- GitHub へのアクセスは GitHub App のインストールトークンで行い、ブラウザには一切渡さない

## 構成

| ディレクトリ | 役割 |
|---|---|
| `internal/cards` | PPLALE-web のカードスキーマ、ID 採番、JSON の読み書き（既存ファイルとバイト一致） |
| `internal/imageconv` | 画像変換（CGO 不要、WebP エンコーダは WebAssembly） |
| `internal/ghapp` | GitHub App 認証（JWT → インストールトークン）と PR 作成 |
| `internal/publish` | 提出 → 検証 → 画像変換 → PR 作成のオーケストレーション |
| `internal/auth` | Discord OAuth2 (PKCE)、サーバーサイドセッション、CSRF |
| `internal/store` | 許可リスト・セッション・監査ログ（Postgres / インメモリ） |
| `internal/httpapi` | HTTP エンドポイントと認可ミドルウェア |
| `web` | React + Vite のフロントエンド |
| `helm/pplale-cms` | Helm chart |

カードデータ自体は CMS に複製しない。提出のたびに PPLALE-web の `main` から読み直すため、
ID 採番や重複チェックは常に最新の状態に対して行われる。

## 開発

ツール (Go / Node / Helm) のバージョンは [mise](https://mise.jdx.dev) で固定している。
PPLALE-web と同じ運用。

```bash
mise install          # go, node, helm を揃える
cp .env.example .env  # 秘密情報を記入（mise が自動で読み込む）
mise run install      # go mod download + npm ci

mise run dev          # API サーバー (:8080)
mise run dev-web      # 別ターミナル: Vite (:5173, /api と /auth を :8080 へプロキシ)

mise run check        # fmt / vet / 型生成のズレ / go test / vitest / helm lint
mise run build        # フロントをビルドして bin/server を作る
mise tasks            # タスク一覧
```

## 型と契約のズレを検出する

「二重管理しない」ために、値の定義は必ず1か所に置き、他はそこから生成するか、
機械的に突き合わせて落とす。

### 1. フロントエンドの型 — 生成する

`internal/api` が HTTP の形の唯一の定義。`web/src/types.generated.ts` はそこから
[tygo](https://github.com/gzuidhof/tygo) で生成する。手で編集しない。

```bash
mise run gen-types    # Go の struct → TypeScript
mise run gen-check    # 生成物が古ければ失敗する（mise run check に含まれる）
```

フォームの選択肢 (`fruit` / `role` / `sweetType` / `version`) も `internal/cards` の
enum を `/api/datasets` が返しているだけなので、UI がサーバーの知らない値を出すことはない。

### 2. PPLALE-web との契約 — 突き合わせる

PPLALE-web 側の定義は生成できないので、**向こうのソースを機械的に読んで比較する**。
目視でのレビューには依存しない。

```bash
mise run contract-check      # PPLALE_WEB_PATH で場所指定（既定 ../PPLALE-web）
```

検証内容 (`internal/contract`):

| 見るもの | PPLALE-web 側の出どころ | ズレたときに壊れるもの |
|---|---|---|
| enum 5種の値 | `src/lib/schema.ts` の `z.enum` | 提出したカードが本体で弾かれる |
| 画像の幅・quality | `scripts/optimize-images.mjs` | 生成画像が手動変換と不一致になる |
| OGP画像の幅 | `scripts/generate-og-images.mjs` | OGP画像の解像度が変わる |
| 実データ4ファイル | `src/data/*.json` | PRの差分が汚れる / 未知フィールドの消失 |
| 画像の実在 | `public/images`, `public/og-cards` | リンク切れ・OGP破損 |
| testdata の鮮度 | 同上 | テストが古い前提のまま通る |

PPLALE-web が手元に無いときテストは skip されるが、`contract-check` タスクは
`CONTRACT_STRICT=1` で走るので「チェックできなかった」を成功と誤認しない。
データが更新されたら `mise run sync-testdata` で `internal/cards/testdata` を追従させる。

さらに実行時の保険として、`cards.Decode` は未知フィールドを拒否する。
PPLALE-web に新しいフィールドが増えた場合、CMS は黙って捨てずにその場でエラーになる。

開発用の非機密な既定値 (`BASE_URL`, `ALLOW_INSECURE_COOKIES` など) は `mise.toml` の
`[env]` に入っている。秘密情報は `.env` にのみ置き、commit しない。
`DATABASE_URL` 未設定ならインメモリ（再起動で消える）。

## GitHub App の設定

対象リポジトリを **PPLALE-web のみ**に限定してインストールする。

- Repository permissions
  - Contents: **Read and write**（ブランチとコミットの作成）
  - Pull requests: **Read and write**（PR の作成）
  - それ以外は No access
- Webhook
  - URL: `<BASE_URL>/webhooks/github`
  - Secret: `GITHUB_WEBHOOK_SECRET` と同じ値
  - Subscribe to events: **Pull request** のみ

Webhook は PR がマージ/クローズされたときに CMS 側の提出履歴の状態を更新するためだけに使う。
署名 (`X-Hub-Signature-256`) を検証しないリクエストは受け付けない。

## Discord アプリの設定

- OAuth2 → Redirects に `<BASE_URL>/auth/callback` を登録
- スコープは `identify` のみ（CMS はギルドもメールも読まない）

Discord でログインするには許可リスト
(`/api/users`, 管理画面の「許可リスト」タブ) に登録されている必要がある。
未登録のユーザーは Discord 認証後にセッションを発行せず、ログインを拒否する。
最初の管理者は `BOOTSTRAP_ADMIN_DISCORD_ID` で起動時に登録される。

### ローカル開発で Discord ログインを省略する

`http://localhost` はDiscordのOAuthリダイレクトURIに登録できない/しづらいことが多い。
その場合は `.env` に以下を設定すると `/auth/login` が Discord に飛ばず、
`BOOTSTRAP_ADMIN_DISCORD_ID` として即ログインする。

```bash
DEV_SKIP_AUTH=true
```

`ALLOW_INSECURE_COOKIES=true`（`mise.toml` の既定値）のときしか有効にならない。
`BASE_URL` が `https://` の設定（本番相当）では `config.Load` がエラーで起動を拒否するため、
設定ミスで本番にログインバイパスが紛れ込むことはない。有効時は起動ログと
ログインのたびに `WARN` を出す。`DISCORD_CLIENT_ID`/`SECRET` も不要になる。

## デプロイ

### イメージの自動バージョニング

`main` への push のたびに GitHub Actions (`.github/workflows/docker-publish.yml`) が
コミットメッセージ(`feat:`/`fix:`/...)からセマンティックバージョンを自動採番し、
`vX.Y.Z` の git タグを打ってから `ghcr.io/ieyoukan/pplale-cms:X.Y.Z`(と `:latest`)を
push する。追加のシークレット設定は不要（`GITHUB_TOKEN` の `contents:write` +
`packages:write` のみで動く）。**初回だけ**、GitHub の Package 設定でこのパッケージの
公開範囲を確認しておくこと（明示的に Public にしないと、Helm 側で `imagePullSecrets`
なしの運用の場合 pull できないことがある）。

実際に何をデプロイするかは [ArgoCD Image Updater](https://argocd-image-updater.readthedocs.io/)
に任せる想定。Application 側に以下のようなアノテーションを付けると、新しい
`X.Y.Z` タグが push されるたびに自動で `image.tag` を書き換えて再デプロイする
（`helm/pplale-cms/values.yaml` の `image.tag` がその書き込み先）:

```yaml
annotations:
  argocd-image-updater.argoproj.io/image-list: pplale-cms=ghcr.io/ieyoukan/pplale-cms
  argocd-image-updater.argoproj.io/pplale-cms.update-strategy: semver
  argocd-image-updater.argoproj.io/pplale-cms.allow-tags: regexp:^[0-9]+\.[0-9]+\.[0-9]+$
  argocd-image-updater.argoproj.io/write-back-method: argocd
  argocd-image-updater.argoproj.io/pplale-cms.helm.image-tag: image.tag
```

`helm/pplale-cms/values.yaml` の `image.tag` は空文字（Chart の `appVersion` に
フォールバック）のままにしてある。ArgoCD 管理下ではこの値は Image Updater が
上書きするので実質参照されず、ArgoCD を使わずに手動 `helm install` するときだけの
フォールバックという位置づけ。

```bash
helm dependency update helm/pplale-cms
helm upgrade --install pplale-cms helm/pplale-cms \
  --set baseURL=https://pplale-cms.youkan.uk \
  --set bootstrapAdmin.discordId=123456789012345678 \
  --set secrets.existingSecret=pplale-cms-secrets
```

機密値は `secrets.existingSecret` で渡す（sealed-secrets / external-secrets 等で作成した Secret）。
必要なキー: `DISCORD_CLIENT_ID` `DISCORD_CLIENT_SECRET` `SESSION_KEY` `GITHUB_APP_ID`
`GITHUB_APP_INSTALLATION_ID` `GITHUB_APP_PRIVATE_KEY` `GITHUB_WEBHOOK_SECRET`
（`postgresql.enabled=false` の場合は `DATABASE_URL` も）。

Cloudflare Tunnel を使う場合は `ingress.enabled=false` のままで、cloudflared 側から
`http://<release>-pplale-cms.<namespace>.svc.cluster.local:80` を参照すればよい。

## データ契約

PPLALE-web 側のスキーマ・画像変換パラメータ・CI の検証内容は
`.claude/skills/pplale-web-data-contract/SKILL.md` に整理してある。
PPLALE-web 側で `src/lib/schema.ts` や `scripts/optimize-images.mjs` が変わったら、
その skill と `internal/cards` / `internal/imageconv` を追従させること。

`internal/cards/testdata/` には PPLALE-web の実データをコピーしてあり、
「読み込んで書き戻すとバイト単位で一致する」ことをテストで保証している。
スキーマに未知のフィールドが増えた場合はテストが落ちる（黙って削除されない）。
