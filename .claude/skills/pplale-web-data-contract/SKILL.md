---
name: pplale-web-data-contract
description: PPLALE-web（本体サイト）のカードデータ構造・検証ルール・PR作成時の規約。pplale-cmsでカードの追加・編集・画像アップロードを扱うとき、またはPPLALE-webへPRを出すコードを書く/レビューするときに必ず参照する。
---

# PPLALE-web データ契約

pplale-cms は PPLALE-web のデータに**直接pushしない**。すべて PR 経由で反映する
（リポジトリ: `ieyoukan/PPLALE-web`, デフォルトブランチ `main`）。

このドキュメントは PPLALE-web 側の実装（`src/types/card.ts`, `src/lib/schema.ts`,
`scripts/check-cards.mjs`, `scripts/optimize-images.mjs`）から抽出した契約。
PPLALE-web 側でこれらのファイルが変更されたら、このドキュメントも追従して更新すること。

## 1. 対象ファイル

PR で変更してよいのは以下のみ（日本語版データのみが CMS の対象。英語版データの生成方法・注意点は §1a）:

- `src/data/yojo.json` (`{ "yojo": CardInfo[] }`)
- `src/data/sweet.json` (`{ "sweet": CardInfo[] }`)
- `src/data/playable.json` (`{ "playable": CardInfo[] }`)
- `src/data/tokenYojo.json` (`{ "tokenYojo": CardInfo[] }`) — デッキ構築に含まれず、ゲーム中の効果で生成されるカード専用（例: 少女うゆち → 美女うゆち）。
- `public/images/{yojo,sweet,playable}/*.webp`

`src/data/en/*.json` や `src/types/card.ts` / `src/lib/schema.ts` 自体、その他アプリコードは
CMS からの自動PRでは変更しない。

PR には上記データ/画像に加えて、**§4a の OGP 用 PNG も必ず含める**（CI では検知されないため）。

## 1a. 英語版データについて（CMS は直接触らないが依存関係あり）

`src/data/en/*.json` は「手動翻訳」ではなく `scripts/import-english-cards.mjs`
(`npm run cards:import-en`) による**自動再生成**。ローカルの `assets/` 配下に置かれた
外部 CSV + ZIP 画像（Git 管理外、翻訳チームが別途用意）を読み込み、日本語版データの
**配列順（index）**で英語 CSV の行と対応付けて丸ごと作り直す。

重要な制約:

- CSV の行数は日本語版カード件数と**完全一致**している必要があり、不一致だとスクリプトが
  例外を投げて止まる（例: `幼女 CSV は N 件必要ですが M 件でした`）
- つまり CMS が `yojo.json` 等に新規カードを追加すると、**次に誰かが `cards:import-en` を
  実行したタイミングで**、対応する英語 CSV 行がまだ用意されていなければビルドスクリプトが
  失敗する
- CMS 側でこれを解決する術はない（外部 CSV/画像アセットは CMS の管轄外）。
  PR の説明文に「英語版データの追従が必要」であることを明記し、翻訳チーム側のフォロー
  待ちであることが分かるようにする程度に留める
- ただし PPLALE-web 側 `src/data/catalog.ts` に ID ベースのフォールバックを実装済み
  （en 側に該当 ID が無いカードは ja 側のデータでそのまま表示される）。そのため
  `cards:import-en` が未実行でも英語サイトでカードが消えたり参照エラーになったりはしない。
  「翻訳が追いつくまで日本語表示になる」という体験は許容範囲という前提

## 2. カードスキーマ（CardInfo）

PPLALE-web 側 `src/lib/schema.ts` の zod 定義が正。CMS は PR 作成前に同等のバリデーションを
かけること（CI 側の `cards:check` は画像参照整合性のみ見ており、フィールド形状までは見ていない）。

```ts
type CardType = 'yojo' | 'sweet' | 'playable';
type FruitType = 'all' | 'strawberry' | 'grape' | 'melon' | 'orange';
type CardRole = '' | 'assistant_manager' | 'manager';
type SweetType =
  | '' | 'animal_soda' | 'cafe' | 'float' | 'doughnut' | 'cake'
  | 'back_menu' | 'chai' | 'ice_cream' | 'pplale_soda' | 'pplale_yaki' | 'currency';
type CardVersion = 'normal' | 'beta';

interface CardInfo {
  id: string;          // 必須。命名規則は §3
  name: string;        // 必須
  type: CardType;       // 必須
  fruit: FruitType;     // 必須
  cost: number;         // 整数, 0以上
  hp: number;           // 整数, 0以上
  attack: number;       // 整数, 0以上
  description: string;  // 空文字許容
  imageUrl: string;     // 必須。§4 参照
  role?: CardRole;
  sweetType?: SweetType;  // お菓子カードではフィールド自体を必須とする（値は "" 可）
  effect?: string;
  version?: CardVersion;
}
```

- `role` は yojo カードのみで使う想定。`sweetType` は sweet カードのみ。
- オプションフィールドは「そのファイルの既存カードが持っているものは揃える」のが実態。
  実データでは yojo/tokenYojo が `"role": ""`、sweet が `"sweetType"`（`""` のカードが9枚実在する）、
  playable が `"version"` を必ず持つ。省略すると diff が不揃いになるため、新規カードでも同じ形にする。
- キー順も既存ファイルに合わせる:
  `id, name, type, version?, fruit, description, imageUrl, cost, hp, attack, effect?, role?, sweetType?`
  （2スペースインデント・末尾改行あり。pplale-cms 側は実データとのバイト一致をテストで保証している）

## 3. ID 採番規則

| type / file        | prefix | 例          |
|---------------------|--------|-------------|
| yojo.json            | `y_`   | `y_0`       |
| sweet.json           | `s_`   | `s_0`       |
| playable.json        | `p_`   | `p_0`       |
| tokenYojo.json       | `yt_`  | `yt_0`      |

新規カードは対象ファイル内の最大連番 +1 を採番する（欠番があっても詰めない）。
他ファイルとの重複チェックは不要（prefix が異なるため衝突しない）が、
**同一ファイル内での重複だけは必ず確認する**。

## 4. 画像の扱い

- 配置先: `public/images/{yojo,sweet,playable}/<ファイル名>.webp`
  （`imageUrl` はこのパスをそのまま `/images/...` 形式で指す）
- `tokenYojo` の画像も `public/images/yojo/` を共有する
- **PNG のまま PR に入れてはいけない**。PPLALE-web の CI (`cards:check`) が
  `.png` 拡張子の `imageUrl` を検出したら即エラーで PR が落ちる
- 変換仕様（PPLALE-web の `scripts/optimize-images.mjs` と同一にする）:
  - 幅 800px にリサイズ（アップスケールしない。元画像がそれ未満ならそのまま）
  - WebP, quality 80
  - `sharp` ライブラリを使用
- ファイル名は既存踏襲でよい（日本語ファイル名の例あり: `111イチゴかがり.webp`）。
  ただし新規はカード制作者が入力した英数字スラッグなど、パス安全な名前を推奨
  （既存の日本語ファイル名との一貫性は必須ではない）

## 4a. OGP 用 PNG（見落とし注意・CI では検知されない）

デッキ共有時の OGP 画像 (`src/app/api/og/[userId]/[deckId]/route.tsx`, `@vercel/og`) は
`public/og-cards/{yojo,sweet,playable}/<同名>.png` を参照する。内部で使う SVG レンダラー
(resvg) が WebP をデコードできないため、`public/images/...` の WebP とは別に PNG ミラーが必要。

- 生成ロジック: `scripts/generate-og-images.mjs` (`npm run cards:og-images`)
  - `public/images/{yojo,sweet,playable}/*.webp` → 幅 240px にリサイズ → 同名で
    `public/og-cards/{yojo,sweet,playable}/*.png` に出力
- **CI (`mise run check`) はこの PNG の存在を検証しない。** 忘れても PR は通ってしまい、
  該当カードを含むデッキの OGP 画像が壊れたまま本番に乗る。
- そのため CMS は新規/更新画像ごとに、WebP 本体と **同じ PR 内で** この 240px PNG も
  `public/og-cards/...` に生成して含めること（CMS 側で同じ resize ロジックを実装するか、
  PPLALE-web の `generate-og-images.mjs` と同一パラメータで変換する）
- `tokenYojo` の画像は `public/og-cards/yojo/` を共有する（本体画像と同じ扱い）

## 5. PR 作成規約

- カード制作者は画面上で複数カードを「下書き」として溜めてから、まとめて1つの PR として
  送信する（`internal/publish.Publisher.PublishBatch`）。1PRに含まれるカードは同一提出者
  （同一 Discord ID）のものに限る。異なる `kind`（yojo/sweet/playable/tokenYojo）が
  混在してもよく、その場合は該当する各 `src/data/*.json` をまとめて1コミットで更新する
- ブランチ名:
  - バッチ内カードが1件のとき: `cms/<type>-<id>` 例: `cms/yojo-y_42`
  - 2件以上のとき: `cms/batch-<ランダム8桁hex>` 例: `cms/batch-a1b2c3d4`
- コミットメッセージ: 変更内容 + 提出者の Discord ユーザーID（監査用）
  例（1件）: `feat: 幼女カード「〇〇」追加 (submitted by discord:123456789012345678)`
  例（複数件）: `feat: カードを3件まとめて追加・更新 (submitted by discord:123456789012345678)`
- PR 本文に最低限含める項目:
  - バッチに含まれる全カードの一覧（ID・名前・type・追加/更新の別）
  - 変更ファイル一覧
  - 提出者の Discord ユーザー名 / ID
  - 画像変換前後のファイルサイズ（カードごと、任意）
  - 新規カードを1件でも含む場合は英語版データ追従が必要な旨の注意書き
- 画像は下書き作成（アップロード）時点で WebP/OGP-PNG に変換して pplale-cms 側の DB に
  一時保存する。バッチ送信時に変換をやり直すことはない（decode 済みなのでズレようがない）
- **直接 `main` に push しない。必ず PR を作成し、人間のレビュー・マージを挟む。**

## 6. 認証・権限

- GitHub 側は fine-grained PAT を使用し、**対象リポジトリを `PPLALE-web` のみに限定**、
  権限は `contents:write` + `pull-requests:write` のみ
- PAT は pplale-cms 側（k8s Secret）にのみ保管し、クライアント（Discordログイン後のブラウザ）には
  絶対に渡さない。PR 作成はすべてサーバーサイド（バックエンド）で実行する
- Discord ログイン済みユーザーであっても、PR 作成前に許可リスト（管理者 or 承認済みカード制作者）との
  照合をサーバー側で必ず行う

## 7. 変更時の同期

PPLALE-web 側で以下が変わったら、この skill を更新すること:

- `src/types/card.ts` / `src/lib/schema.ts` のフィールド追加・変更
- `scripts/check-cards.mjs` の検証ロジック
- `scripts/optimize-images.mjs` の画像変換パラメータ（幅・quality）
- `mise.toml` の CI タスク構成
