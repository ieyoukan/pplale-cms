// Package publish turns a batch of reviewed CMS drafts into a single pull
// request against PPLALE-web. It owns every rule from the data contract that
// spans more than one concern: ID allocation against live upstream data, the
// WebP plus OGP PNG pair, branch naming and the audit trail carried in the
// commit message.
package publish

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ieyoukan/pplale-cms/internal/cards"
	"github.com/ieyoukan/pplale-cms/internal/ghapp"
	"github.com/ieyoukan/pplale-cms/internal/imageconv"
)

// RepoClient is the subset of the GitHub client the publisher needs.
type RepoClient interface {
	FileContent(ctx context.Context, path string) ([]byte, error)
	CreatePullRequest(ctx context.Context, in ghapp.PullRequestInput) (ghapp.PullRequest, error)
}

// Submitter is the authenticated Discord identity behind a batch. It is
// recorded in the commit message so a merged card can always be traced back.
// A batch always belongs to a single submitter: each creator submits their
// own queued drafts.
type Submitter struct {
	DiscordID   string
	DiscordName string
}

// Item is one card addition or edit going into the batch. Images are
// converted to WebP/OGP-PNG when a draft is created, not here: PublishBatch
// only ever writes bytes it is handed, so a batch commits instantly no matter
// how many images it carries.
type Item struct {
	Kind cards.Kind
	Card cards.Card
	// Taxonomy carries a new fruit and/or sweet classification first used by
	// this card. The publisher updates PPLALE-web's types, schema and labels in
	// the same commit.
	Taxonomy cards.TaxonomyChanges
	// ImageSlug is the base file name (without extension) for a new image.
	// Ignored when Image is nil.
	ImageSlug string
	// Image is the already-converted pair to write. Nil means "keep the
	// existing image", which is only valid when editing a card that already
	// has one.
	Image *imageconv.Result
}

// CardResult reports what happened to one item in the batch.
type CardResult struct {
	Kind     cards.Kind
	CardID   string
	CardName string
	IsEdit   bool
}

// Result reports what was opened upstream.
type Result struct {
	PullRequest ghapp.PullRequest
	Branch      string
	Files       []string
	Cards       []CardResult
}

// Publisher creates pull requests. It holds no credentials itself; the repo
// client does.
type Publisher struct {
	Repo RepoClient
}

var slugPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// SanitizeSlug validates a user supplied image file name. Anything that could
// escape the image directory or collide with a path separator is rejected
// outright rather than escaped, because the name ends up both in a repository
// path and in a public URL.
func SanitizeSlug(slug string) (string, error) {
	slug = strings.TrimSpace(slug)
	slug = strings.TrimSuffix(strings.TrimSuffix(slug, ".webp"), ".png")
	if slug == "" {
		return "", errors.New("publish: 画像ファイル名が空です")
	}
	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("publish: 画像ファイル名には半角英数字・ハイフン・アンダースコアのみ使用できます: %q", slug)
	}
	return slug, nil
}

// datasetGroup accumulates every item targeting one dataset file, so the file
// is read once, appended to in submission order, and written back once.
type datasetGroup struct {
	ds   cards.Dataset
	list []cards.Card
}

// PublishBatch validates every item against the current upstream data and
// opens a single pull request containing all of them. Datasets are always
// re-read from the base branch so that IDs are allocated against live data
// rather than a possibly stale CMS copy; two items targeting the same dataset
// in one batch share that read and are appended in order, so they never
// collide on an allocated ID.
func (p *Publisher) PublishBatch(ctx context.Context, items []Item, submitter Submitter) (Result, error) {
	if len(items) == 0 {
		return Result{}, errors.New("publish: カードが1件もありません")
	}
	if submitter.DiscordID == "" {
		return Result{}, errors.New("publish: 提出者の Discord ID が必要です")
	}

	taxonomy, err := cards.LoadTaxonomy(ctx, p.Repo)
	if err != nil {
		return Result{}, fmt.Errorf("publish: %w", err)
	}
	var taxonomyChanges []cards.TaxonomyChanges
	for _, item := range items {
		var pending cards.TaxonomyChanges
		if option := item.Taxonomy.Fruit; option != nil {
			if !taxonomy.HasFruit(option.Value) {
				copy := *option
				pending.Fruit = &copy
			}
			if err := taxonomy.AddFruit(*option); err != nil {
				return Result{}, fmt.Errorf("publish: 新しいフルーツ分類: %w", err)
			}
		}
		if option := item.Taxonomy.SweetType; option != nil {
			if !taxonomy.HasSweetType(option.Value) {
				copy := *option
				pending.SweetType = &copy
			}
			if err := taxonomy.AddSweetType(*option); err != nil {
				return Result{}, fmt.Errorf("publish: 新しいお菓子タイプ: %w", err)
			}
		}
		if pending.Fruit != nil || pending.SweetType != nil {
			taxonomyChanges = append(taxonomyChanges, pending)
		}
	}

	groups := make(map[cards.Kind]*datasetGroup)
	var order []cards.Kind
	var datasetFiles []ghapp.File
	var imageFiles []ghapp.File
	var results []CardResult
	var images []imageNote

	for _, item := range items {
		ds, err := cards.DatasetFor(item.Kind)
		if err != nil {
			return Result{}, err
		}
		g, ok := groups[item.Kind]
		if !ok {
			raw, err := p.Repo.FileContent(ctx, ds.JSONPath)
			if err != nil {
				return Result{}, fmt.Errorf("publish: %s の取得に失敗しました: %w", ds.JSONPath, err)
			}
			list, err := cards.Decode(ds, raw)
			if err != nil {
				return Result{}, err
			}
			g = &datasetGroup{ds: ds, list: list}
			groups[item.Kind] = g
			order = append(order, item.Kind)
		}

		card := item.Card
		card.Type = g.ds.CardType

		index := -1
		if card.ID == "" {
			card.ID = cards.NextID(g.ds, g.list)
		} else {
			for i, existing := range g.list {
				if existing.ID == card.ID {
					index = i
					break
				}
			}
			if index < 0 {
				return Result{}, fmt.Errorf("publish: %s に ID %q のカードが存在しません", g.ds.JSONPath, card.ID)
			}
		}

		switch {
		case item.Image != nil:
			slug, err := SanitizeSlug(item.ImageSlug)
			if err != nil {
				return Result{}, err
			}
			card.ImageURL = g.ds.ImageURL(slug + ".webp")
			imageFiles = append(imageFiles,
				ghapp.File{Path: g.ds.ImagePath(slug + ".webp"), Content: item.Image.WebP},
				// 上流CIはこのPNGを検証しないが、欠けるとOGP画像が壊れる。
				ghapp.File{Path: g.ds.OGImagePath(slug + ".png"), Content: item.Image.OGPNG},
			)
			images = append(images, imageNote{CardName: card.Name, Image: *item.Image})
		case index >= 0:
			card.ImageURL = g.list[index].ImageURL
		default:
			return Result{}, fmt.Errorf("publish: 「%s」: 新規カードには画像が必要です", card.Name)
		}

		if err := cards.ValidateWithTaxonomy(g.ds, card, taxonomy); err != nil {
			return Result{}, fmt.Errorf("「%s」: %w", card.Name, err)
		}

		if index >= 0 {
			g.list[index] = card
		} else {
			g.list = append(g.list, card)
		}

		results = append(results, CardResult{Kind: item.Kind, CardID: card.ID, CardName: card.Name, IsEdit: index >= 0})
	}

	for _, kind := range order {
		g := groups[kind]
		if err := cards.ValidateDatasetWithTaxonomy(g.ds, g.list, taxonomy); err != nil {
			return Result{}, err
		}
		encoded, err := cards.Encode(g.ds, g.list)
		if err != nil {
			return Result{}, err
		}
		datasetFiles = append(datasetFiles, ghapp.File{Path: g.ds.JSONPath, Content: encoded})
	}

	var taxonomyFiles []ghapp.File
	if len(taxonomyChanges) > 0 {
		summary := cards.TaxonomyChanges{}
		for _, change := range taxonomyChanges {
			if change.Fruit != nil {
				summary.Fruit = change.Fruit
			}
			if change.SweetType != nil {
				summary.SweetType = change.SweetType
			}
		}
		paths := cards.TaxonomySourcePaths(summary)
		sources := make(map[string][]byte, len(paths))
		for _, path := range paths {
			raw, err := p.Repo.FileContent(ctx, path)
			if err != nil {
				return Result{}, fmt.Errorf("publish: %s の取得に失敗しました: %w", path, err)
			}
			sources[path] = raw
		}
		for _, change := range taxonomyChanges {
			var err error
			sources, err = cards.PatchTaxonomySources(sources, change)
			if err != nil {
				return Result{}, fmt.Errorf("publish: 分類定義の更新に失敗しました: %w", err)
			}
		}
		for _, path := range paths {
			taxonomyFiles = append(taxonomyFiles, ghapp.File{Path: path, Content: sources[path]})
		}
	}

	files := append(datasetFiles, taxonomyFiles...)
	files = append(files, imageFiles...)
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}

	branch, err := branchName(results)
	if err != nil {
		return Result{}, err
	}
	title := buildTitle(results)
	commit := fmt.Sprintf("%s (submitted by discord:%s)", title, submitter.DiscordID)

	pr, err := p.Repo.CreatePullRequest(ctx, ghapp.PullRequestInput{
		Branch:        branch,
		Title:         title,
		Body:          buildBody(results, paths, images, taxonomyChanges, submitter),
		CommitMessage: commit,
		Files:         files,
	})
	if err != nil {
		return Result{}, err
	}

	return Result{PullRequest: pr, Branch: branch, Files: paths, Cards: results}, nil
}

type imageNote struct {
	CardName string
	Image    imageconv.Result
}

// branchName keeps the readable cms/<kind>-<id> form for the common single
// card case; a batch of several cards has no single natural name, so it gets
// a short random suffix instead.
func branchName(results []CardResult) (string, error) {
	if len(results) == 1 {
		return fmt.Sprintf("cms/%s-%s", results[0].Kind, results[0].CardID), nil
	}
	token, err := randomHex(4)
	if err != nil {
		return "", err
	}
	return "cms/batch-" + token, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("publish: ブランチ名の生成に失敗しました: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func buildTitle(results []CardResult) string {
	if len(results) == 1 {
		r := results[0]
		return fmt.Sprintf("feat: %sカード「%s」を%s", datasetLabel(r.Kind), r.CardName, actionLabel(r.IsEdit))
	}
	return fmt.Sprintf("feat: カードを%d件まとめて追加・更新", len(results))
}

func actionLabel(isEdit bool) string {
	if isEdit {
		return "更新"
	}
	return "追加"
}

func datasetLabel(kind cards.Kind) string {
	switch kind {
	case cards.KindYojo:
		return "幼女"
	case cards.KindSweet:
		return "お菓子"
	case cards.KindPlayable:
		return "プレイアブル"
	case cards.KindTokenYojo:
		return "トークン幼女"
	default:
		return string(kind)
	}
}

func buildBody(results []CardResult, paths []string, images []imageNote, taxonomy []cards.TaxonomyChanges, submitter Submitter) string {
	var b strings.Builder
	b.WriteString("pplale-cms から自動生成された PR です。\n\n")

	b.WriteString("## カード\n\n")
	for _, r := range results {
		fmt.Fprintf(&b, "- `%s` %s（%s / %s）\n", r.CardID, r.CardName, datasetLabel(r.Kind), actionLabel(r.IsEdit))
	}
	b.WriteString("\n")

	if len(taxonomy) > 0 {
		b.WriteString("## 新しい分類\n\n")
		for _, change := range taxonomy {
			if option := change.Fruit; option != nil {
				fmt.Fprintf(&b, "- フルーツ: %s / %s (`%s`)\n", option.LabelJA, option.LabelEN, option.Value)
			}
			if option := change.SweetType; option != nil {
				fmt.Fprintf(&b, "- お菓子タイプ: %s / %s (`%s`)\n", option.LabelJA, option.LabelEN, option.Value)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## 変更ファイル\n\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "- `%s`\n", p)
	}
	b.WriteString("\n")

	if len(images) > 0 {
		b.WriteString("## 画像変換\n\n")
		for _, img := range images {
			fmt.Fprintf(&b, "- %s: 変換前 %s %s → WebP (幅%dpx, quality %d) %s / OGP用 PNG (幅%dpx) %s\n",
				img.CardName, strings.ToUpper(img.Image.SourceType), humanBytes(img.Image.SourceBytes),
				imageconv.CardWidth, imageconv.Quality, humanBytes(len(img.Image.WebP)),
				imageconv.OGWidth, humanBytes(len(img.Image.OGPNG)))
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## 提出者\n\n- Discord: %s (`%s`)\n\n", submitter.DiscordName, submitter.DiscordID)

	hasNew := false
	for _, r := range results {
		if !r.IsEdit {
			hasNew = true
			break
		}
	}
	if hasNew {
		b.WriteString("## 注意\n\n")
		b.WriteString("- 新規カードを含みます。英語版データ (`src/data/en/*.json`) にはまだ含まれていません。")
		b.WriteString("`catalog.ts` の ID ベースのフォールバックにより、英語サイトでも日本語データで表示されるため機能的には壊れませんが、")
		b.WriteString("`npm run cards:import-en` 用の CSV が別途用意されるまで英語訳は反映されません。\n")
	}
	return b.String()
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
