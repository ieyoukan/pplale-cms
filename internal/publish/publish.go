// Package publish turns a reviewed CMS submission into a pull request against
// PPLALE-web. It owns every rule from the data contract that spans more than
// one concern: ID allocation against live upstream data, the WebP plus OGP PNG
// pair, branch naming and the audit trail carried in the commit message.
package publish

import (
	"context"
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

// Submitter is the authenticated Discord identity behind a submission. It is
// recorded in the commit message so a merged card can always be traced back.
type Submitter struct {
	DiscordID   string
	DiscordName string
}

// Submission is one card addition or edit.
type Submission struct {
	Kind cards.Kind
	Card cards.Card
	// ImageSlug is the base file name (without extension) for a new image.
	// Ignored when ImageSource is empty.
	ImageSlug string
	// ImageSource is the raw upload. Empty means "keep the existing image",
	// which is only valid when editing a card that already has one.
	ImageSource []byte
	Submitter   Submitter
}

// Result reports what was opened upstream.
type Result struct {
	PullRequest ghapp.PullRequest
	CardID      string
	Branch      string
	Files       []string
	Image       *imageconv.Result
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

// Publish validates a submission against the current upstream data and opens a
// pull request. The dataset is always re-read from the base branch so that IDs
// are allocated against live data rather than a possibly stale CMS copy.
func (p *Publisher) Publish(ctx context.Context, sub Submission) (Result, error) {
	ds, err := cards.DatasetFor(sub.Kind)
	if err != nil {
		return Result{}, err
	}
	if sub.Submitter.DiscordID == "" {
		return Result{}, errors.New("publish: 提出者の Discord ID が必要です")
	}

	raw, err := p.Repo.FileContent(ctx, ds.JSONPath)
	if err != nil {
		return Result{}, fmt.Errorf("publish: %s の取得に失敗しました: %w", ds.JSONPath, err)
	}
	list, err := cards.Decode(ds, raw)
	if err != nil {
		return Result{}, err
	}

	card := sub.Card
	card.Type = ds.CardType

	index := -1
	if card.ID == "" {
		card.ID = cards.NextID(ds, list)
	} else {
		for i, existing := range list {
			if existing.ID == card.ID {
				index = i
				break
			}
		}
		if index < 0 {
			return Result{}, fmt.Errorf("publish: %s に ID %q のカードが存在しません", ds.JSONPath, card.ID)
		}
	}

	var converted *imageconv.Result
	var files []ghapp.File
	switch {
	case len(sub.ImageSource) > 0:
		slug, err := SanitizeSlug(sub.ImageSlug)
		if err != nil {
			return Result{}, err
		}
		result, err := imageconv.Convert(sub.ImageSource)
		if err != nil {
			return Result{}, err
		}
		converted = &result
		card.ImageURL = ds.ImageURL(slug + ".webp")
		files = append(files,
			ghapp.File{Path: ds.ImagePath(slug + ".webp"), Content: result.WebP},
			// 上流CIはこのPNGを検証しないが、欠けるとOGP画像が壊れる。
			ghapp.File{Path: ds.OGImagePath(slug + ".png"), Content: result.OGPNG},
		)
	case index >= 0:
		card.ImageURL = list[index].ImageURL
	default:
		return Result{}, errors.New("publish: 新規カードには画像が必要です")
	}

	if index >= 0 {
		list[index] = card
	} else {
		list = append(list, card)
	}

	if err := cards.Validate(ds, card); err != nil {
		return Result{}, err
	}
	if err := cards.ValidateDataset(ds, list); err != nil {
		return Result{}, err
	}

	encoded, err := cards.Encode(ds, list)
	if err != nil {
		return Result{}, err
	}
	files = append([]ghapp.File{{Path: ds.JSONPath, Content: encoded}}, files...)

	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}

	action := "追加"
	if index >= 0 {
		action = "更新"
	}
	branch := fmt.Sprintf("cms/%s-%s", ds.Kind, card.ID)
	title := fmt.Sprintf("feat: %sカード「%s」を%s", datasetLabel(ds.Kind), card.Name, action)
	commit := fmt.Sprintf("%s (submitted by discord:%s)", title, sub.Submitter.DiscordID)

	pr, err := p.Repo.CreatePullRequest(ctx, ghapp.PullRequestInput{
		Branch:        branch,
		Title:         title,
		Body:          buildBody(ds, card, sub, paths, converted, index >= 0),
		CommitMessage: commit,
		Files:         files,
	})
	if err != nil {
		return Result{}, err
	}

	return Result{PullRequest: pr, CardID: card.ID, Branch: branch, Files: paths, Image: converted}, nil
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

func buildBody(ds cards.Dataset, card cards.Card, sub Submission, paths []string, img *imageconv.Result, isEdit bool) string {
	var b strings.Builder
	b.WriteString("pplale-cms から自動生成された PR です。\n\n")

	fmt.Fprintf(&b, "## カード\n\n- ID: `%s`\n- 名前: %s\n- データセット: `%s`\n- 画像: `%s`\n\n",
		card.ID, card.Name, ds.JSONPath, card.ImageURL)

	b.WriteString("## 変更ファイル\n\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "- `%s`\n", p)
	}
	b.WriteString("\n")

	if img != nil {
		fmt.Fprintf(&b, "## 画像変換\n\n- 変換前: %s %s\n- WebP (幅%dpx, quality %d): %s\n- OGP用 PNG (幅%dpx): %s\n\n",
			strings.ToUpper(img.SourceType), humanBytes(img.SourceBytes),
			imageconv.CardWidth, imageconv.Quality, humanBytes(len(img.WebP)),
			imageconv.OGWidth, humanBytes(len(img.OGPNG)))
	}

	fmt.Fprintf(&b, "## 提出者\n\n- Discord: %s (`%s`)\n\n", sub.Submitter.DiscordName, sub.Submitter.DiscordID)

	if !isEdit {
		b.WriteString("## 注意\n\n")
		b.WriteString("- 新規カードのため、英語版データ (`src/data/en/*.json`) の追従が必要です。")
		b.WriteString("`npm run cards:import-en` は日本語版と同じ件数の英語 CSV が揃うまで失敗します。\n")
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
