package publish

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/ieyoukan/pplale-cms/internal/cards"
	"github.com/ieyoukan/pplale-cms/internal/ghapp"
	"github.com/ieyoukan/pplale-cms/internal/imageconv"
)

type fakeRepo struct {
	files    map[string][]byte
	fetchErr error
	created  []ghapp.PullRequestInput
}

func (f *fakeRepo) FileContent(_ context.Context, path string) ([]byte, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	data, ok := f.files[path]
	if ok {
		return data, nil
	}
	if path == cards.UpstreamSchemaPath {
		return []byte(taxonomySchemaFixture), nil
	}
	if path == cards.UpstreamLocalePath {
		return []byte(taxonomyLocaleFixture), nil
	}
	fixtures := map[string]string{
		cards.UpstreamTypesPath: `export type FruitType = 'all' | 'strawberry' | 'grape' | 'melon' | 'orange';
export type SweetType = '' | 'animal_soda' | 'cake';`,
		cards.UpstreamFilterBarPath:      `const fruits = (['strawberry', 'grape', 'melon', 'orange'] as FruitType[]);`,
		cards.UpstreamFruitSelectionPath: `const fruits = (['strawberry', 'grape', 'melon', 'orange'] as FruitType[]);`,
		cards.UpstreamTwoPickPath: `const legacyFruitCodes: Record<string, FruitType> = {
  strawberry: 'strawberry', grape: 'grape', melon: 'melon', orange: 'orange',
};`,
	}
	if fixture, ok := fixtures[path]; ok {
		return []byte(fixture), nil
	}
	return nil, errors.New("not found: " + path)
}

func (f *fakeRepo) CreatePullRequest(_ context.Context, in ghapp.PullRequestInput) (ghapp.PullRequest, error) {
	f.created = append(f.created, in)
	return ghapp.PullRequest{Number: 7, HTMLURL: "https://github.com/ieyoukan/PPLALE-web/pull/7"}, nil
}

const yojoFixture = `{
  "yojo": [
    {
      "id": "y_0",
      "name": "かがり",
      "type": "yojo",
      "fruit": "strawberry",
      "description": "",
      "imageUrl": "/images/yojo/kagari.webp",
      "cost": 1,
      "hp": 1,
      "attack": 1,
      "effect": "既存の効果",
      "role": ""
    },
    {
      "id": "y_4",
      "name": "とここ",
      "type": "yojo",
      "fruit": "strawberry",
      "description": "",
      "imageUrl": "/images/yojo/tokoko.webp",
      "cost": 1,
      "hp": 1,
      "attack": 1,
      "effect": "既存の効果",
      "role": ""
    }
  ]
}
`

const taxonomySchemaFixture = `
export const fruitTypeSchema = z.enum(['all', 'strawberry', 'grape', 'melon', 'orange']);
export const sweetTypeSchema = z.enum(['', 'animal_soda', 'cake']);
`

const taxonomyLocaleFixture = `
const fruitLabels = {
  ja: { all: 'すべて', strawberry: 'いちご', grape: 'ぶどう', melon: 'めろん', orange: 'おれんじ' },
  en: { all: 'All', strawberry: 'Strawberry', grape: 'Grape', melon: 'Melon', orange: 'Orange' },
};
const sweetTypeLabels = {
  ja: { '': '', animal_soda: '動物さんソーダ', cake: 'ケーキ' },
  en: { '': '', animal_soda: 'Animal soda', cake: 'Cake' },
};
`

var submitter = Submitter{DiscordID: "123456789012345678", DiscordName: "creator#1"}

func newPublisher() (*Publisher, *fakeRepo) {
	repo := &fakeRepo{files: map[string][]byte{
		"src/data/yojo.json":     []byte(yojoFixture),
		cards.UpstreamSchemaPath: []byte(taxonomySchemaFixture),
		cards.UpstreamLocalePath: []byte(taxonomyLocaleFixture),
	}}
	return &Publisher{Repo: repo}, repo
}

func newItem() Item {
	img, err := imageconv.Convert(testPNG())
	if err != nil {
		panic(err)
	}
	return Item{
		Kind: cards.KindYojo,
		Card: cards.Card{
			Name:  "あたらしい子",
			Fruit: cards.FruitMelon,
			Cost:  2, HP: 3, Attack: 1,
			Effect: strPtr("テスト効果"),
			Role:   rolePtr(cards.RoleNone),
		},
		ImageSlug: "atarashii_ko",
		Image:     &img,
	}
}

func TestPublishBatchSingleNewCard(t *testing.T) {
	p, repo := newPublisher()

	got, err := p.PublishBatch(context.Background(), []Item{newItem()}, submitter)
	if err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}

	// IDs come from live upstream data and never reuse the y_1..y_3 gap.
	if len(got.Cards) != 1 || got.Cards[0].CardID != "y_5" {
		t.Fatalf("Cards = %+v, want a single y_5", got.Cards)
	}
	// A single-card batch keeps the readable cms/<kind>-<id> branch name.
	if got.Branch != "cms/yojo-y_5" {
		t.Errorf("Branch = %q", got.Branch)
	}
	if got.PullRequest.Number != 7 {
		t.Errorf("PullRequest = %+v", got.PullRequest)
	}

	if len(repo.created) != 1 {
		t.Fatalf("created %d pull requests, want 1", len(repo.created))
	}
	in := repo.created[0]

	wantFiles := map[string]bool{
		"src/data/yojo.json":                    false,
		"public/images/yojo/atarashii_ko.webp":  false,
		"public/og-cards/yojo/atarashii_ko.png": false,
	}
	for _, f := range in.Files {
		if _, ok := wantFiles[f.Path]; !ok {
			t.Errorf("unexpected file in pull request: %s", f.Path)
			continue
		}
		wantFiles[f.Path] = true
		if len(f.Content) == 0 {
			t.Errorf("file %s has empty content", f.Path)
		}
	}
	for path, seen := range wantFiles {
		if !seen {
			t.Errorf("pull request is missing %s", path)
		}
	}

	if !strings.Contains(in.CommitMessage, "discord:123456789012345678") {
		t.Errorf("commit message lacks the audit trail: %q", in.CommitMessage)
	}
	if !strings.Contains(in.Body, "creator#1") || !strings.Contains(in.Body, "英語版データ") {
		t.Errorf("body missing submitter or english follow-up note:\n%s", in.Body)
	}

	updated := fileContent(t, in.Files, "src/data/yojo.json")
	ds, _ := cards.DatasetFor(cards.KindYojo)
	list, err := cards.Decode(ds, updated)
	if err != nil {
		t.Fatalf("decode updated dataset: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("dataset has %d cards, want 3", len(list))
	}
	added := list[2]
	if added.ID != "y_5" || added.Name != "あたらしい子" {
		t.Errorf("appended card = %+v", added)
	}
	if added.ImageURL != "/images/yojo/atarashii_ko.webp" {
		t.Errorf("imageUrl = %q", added.ImageURL)
	}
	if added.Type != cards.TypeYojo {
		t.Errorf("type = %q, want it forced to the dataset type", added.Type)
	}
	// 既存カードの行は書き換わってはいけない（PRの差分を最小に保つ）。
	if !bytes.HasPrefix(updated, []byte(yojoFixture[:strings.Index(yojoFixture, `"id": "y_4"`)])) {
		t.Error("existing cards were rewritten")
	}
}

func TestPublishBatchAddsNewFruitClassificationWithCard(t *testing.T) {
	p, repo := newPublisher()
	item := newItem()
	option, err := cards.NewTaxonomyOption("りんご", "Apple")
	if err != nil {
		t.Fatal(err)
	}
	item.Card.Fruit = cards.FruitType(option.Value)
	item.Taxonomy.Fruit = &option

	got, err := p.PublishBatch(context.Background(), []Item{item}, submitter)
	if err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}

	written := map[string]string{}
	for _, file := range repo.created[0].Files {
		written[file.Path] = string(file.Content)
	}
	for _, path := range cards.TaxonomySourcePaths(item.Taxonomy) {
		if _, ok := written[path]; !ok {
			t.Errorf("classification source %s was not included; files=%v", path, got.Files)
		}
	}
	if !strings.Contains(written[cards.UpstreamSchemaPath], "'apple'") {
		t.Error("fruit schema does not contain apple")
	}
	if !strings.Contains(written[cards.UpstreamLocalePath], "'りんご'") || !strings.Contains(written[cards.UpstreamLocalePath], "'Apple'") {
		t.Error("fruit labels were not added in both languages")
	}
	if !strings.Contains(written[cards.UpstreamFruitSelectionPath], "'apple'") {
		t.Error("2Pick fruit selection does not contain apple")
	}
	if !strings.Contains(written["src/data/yojo.json"], `"fruit": "apple"`) {
		t.Error("card data does not use the new fruit")
	}
}

func TestPublishBatchEditKeepsExistingImage(t *testing.T) {
	p, repo := newPublisher()

	item := newItem()
	item.Card.ID = "y_0"
	item.Card.Name = "かがり(改)"
	item.Image = nil
	item.ImageSlug = ""

	got, err := p.PublishBatch(context.Background(), []Item{item}, submitter)
	if err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}
	if got.Branch != "cms/yojo-y_0" {
		t.Errorf("Branch = %q", got.Branch)
	}
	if len(got.Files) != 1 || got.Files[0] != "src/data/yojo.json" {
		t.Errorf("Files = %v, want only the dataset", got.Files)
	}
	if !got.Cards[0].IsEdit {
		t.Error("IsEdit = false, want true")
	}

	in := repo.created[0]
	if !strings.Contains(in.Title, "更新") {
		t.Errorf("title = %q, want an update title", in.Title)
	}
	if strings.Contains(in.Body, "英語版データ") {
		t.Error("edit body should not ask for english follow-up")
	}

	ds, _ := cards.DatasetFor(cards.KindYojo)
	list, _ := cards.Decode(ds, fileContent(t, in.Files, "src/data/yojo.json"))
	if len(list) != 2 {
		t.Fatalf("edit changed the card count: %d", len(list))
	}
	if list[0].Name != "かがり(改)" || list[0].ImageURL != "/images/yojo/kagari.webp" {
		t.Errorf("edited card = %+v", list[0])
	}
}

func TestPublishBatchMultipleCardsSameDatasetShareTheRead(t *testing.T) {
	p, repo := newPublisher()

	first := newItem()
	first.Card.Name = "一人目"
	second := newItem()
	second.Card.Name = "二人目"
	second.ImageSlug = "futarime"

	got, err := p.PublishBatch(context.Background(), []Item{first, second}, submitter)
	if err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}

	// Two new cards in the same batch must not collide on the same ID.
	if len(got.Cards) != 2 || got.Cards[0].CardID != "y_5" || got.Cards[1].CardID != "y_6" {
		t.Fatalf("Cards = %+v, want y_5 then y_6", got.Cards)
	}
	if !strings.HasPrefix(got.Branch, "cms/batch-") {
		t.Errorf("Branch = %q, want a cms/batch-* name for a multi-card batch", got.Branch)
	}

	in := repo.created[0]
	// Exactly one dataset write even though two cards targeted it.
	datasetWrites := 0
	for _, f := range in.Files {
		if f.Path == "src/data/yojo.json" {
			datasetWrites++
		}
	}
	if datasetWrites != 1 {
		t.Errorf("src/data/yojo.json was written %d times, want 1", datasetWrites)
	}

	ds, _ := cards.DatasetFor(cards.KindYojo)
	list, _ := cards.Decode(ds, fileContent(t, in.Files, "src/data/yojo.json"))
	if len(list) != 4 {
		t.Fatalf("dataset has %d cards, want 4 (2 existing + 2 new)", len(list))
	}
	if list[2].Name != "一人目" || list[3].Name != "二人目" {
		t.Errorf("appended order = %q, %q", list[2].Name, list[3].Name)
	}

	if !strings.Contains(in.Title, "2件") {
		t.Errorf("title = %q, want it to mention the batch size", in.Title)
	}
	if !strings.Contains(in.Body, "一人目") || !strings.Contains(in.Body, "二人目") {
		t.Errorf("body does not list both cards:\n%s", in.Body)
	}
}

func TestPublishBatchAcrossDifferentDatasets(t *testing.T) {
	repo := &fakeRepo{files: map[string][]byte{
		"src/data/yojo.json":  []byte(yojoFixture),
		"src/data/sweet.json": []byte("{\n  \"sweet\": []\n}\n"),
	}}
	p := &Publisher{Repo: repo}

	yojoItem := newItem()
	sweetImg, err := imageconv.Convert(testPNG())
	if err != nil {
		t.Fatalf("imageconv.Convert: %v", err)
	}
	sweetItem := Item{
		Kind: cards.KindSweet,
		Card: cards.Card{
			Name: "あたらしいお菓子", Fruit: cards.FruitAll,
			SweetType: sweetPtr(cards.SweetCake),
		},
		ImageSlug: "new_sweet",
		Image:     &sweetImg,
	}

	got, err := p.PublishBatch(context.Background(), []Item{yojoItem, sweetItem}, submitter)
	if err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}
	if len(got.Cards) != 2 {
		t.Fatalf("Cards = %+v, want 2", got.Cards)
	}
	if got.Cards[0].Kind != cards.KindYojo || got.Cards[1].Kind != cards.KindSweet {
		t.Errorf("Cards kinds = %v", got.Cards)
	}

	in := repo.created[0]
	for _, want := range []string{"src/data/yojo.json", "src/data/sweet.json",
		"public/images/yojo/atarashii_ko.webp", "public/images/sweet/new_sweet.webp"} {
		if fileContentOrNil(in.Files, want) == nil {
			t.Errorf("pull request is missing %s", want)
		}
	}
}

func TestPublishBatchRejectsEmpty(t *testing.T) {
	p, repo := newPublisher()
	if _, err := p.PublishBatch(context.Background(), nil, submitter); err == nil {
		t.Fatal("expected an error for an empty batch")
	}
	if len(repo.created) != 0 {
		t.Error("a pull request was opened for an empty batch")
	}
}

func TestPublishBatchRejectsUnknownIDOnEdit(t *testing.T) {
	p, repo := newPublisher()
	item := newItem()
	item.Card.ID = "y_999"
	item.Image = nil

	if _, err := p.PublishBatch(context.Background(), []Item{item}, submitter); err == nil {
		t.Fatal("expected an error for an unknown card ID")
	}
	if len(repo.created) != 0 {
		t.Error("a pull request was opened for an invalid batch")
	}
}

func TestPublishBatchRequiresImageForNewCard(t *testing.T) {
	p, repo := newPublisher()
	item := newItem()
	item.Image = nil

	if _, err := p.PublishBatch(context.Background(), []Item{item}, submitter); err == nil {
		t.Fatal("expected an error when a new card has no image")
	}
	if len(repo.created) != 0 {
		t.Error("a pull request was opened without an image")
	}
}

func TestPublishBatchRequiresSubmitter(t *testing.T) {
	p, _ := newPublisher()
	if _, err := p.PublishBatch(context.Background(), []Item{newItem()}, Submitter{}); err == nil {
		t.Fatal("expected an error when the submitter is unknown")
	}
}

func TestPublishBatchRejectsInvalidCardBeforeOpeningPR(t *testing.T) {
	p, repo := newPublisher()
	item := newItem()
	item.Card.Fruit = "banana"

	_, err := p.PublishBatch(context.Background(), []Item{item}, submitter)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !strings.Contains(err.Error(), "fruit") {
		t.Errorf("err = %v, want a fruit validation error", err)
	}
	if len(repo.created) != 0 {
		t.Error("a pull request was opened for an invalid card")
	}
}

// One invalid card in a batch must not let the valid ones through partway.
func TestPublishBatchRejectsWholeBatchOnOneBadCard(t *testing.T) {
	p, repo := newPublisher()
	good := newItem()
	good.Card.Name = "いいカード"
	bad := newItem()
	bad.Card.Name = "だめなカード"
	bad.Card.Cost = -1

	if _, err := p.PublishBatch(context.Background(), []Item{good, bad}, submitter); err == nil {
		t.Fatal("expected a validation error")
	}
	if len(repo.created) != 0 {
		t.Error("a pull request was opened despite one invalid card")
	}
}

func TestPublishBatchPropagatesFetchFailure(t *testing.T) {
	repo := &fakeRepo{fetchErr: errors.New("boom")}
	p := &Publisher{Repo: repo}

	if _, err := p.PublishBatch(context.Background(), []Item{newItem()}, submitter); err == nil {
		t.Fatal("expected the upstream fetch error to surface")
	}
}

func TestSanitizeSlug(t *testing.T) {
	valid := map[string]string{
		"atarashii_ko":     "atarashii_ko",
		"card-01":          "card-01",
		"card-01.webp":     "card-01",
		"  spaced  ":       "spaced",
		"Mixed_Case-9.png": "Mixed_Case-9",
	}
	for in, want := range valid {
		got, err := SanitizeSlug(in)
		if err != nil || got != want {
			t.Errorf("SanitizeSlug(%q) = (%q, %v), want (%q, nil)", in, got, err, want)
		}
	}

	// A slug becomes both a repository path and a public URL, so anything that
	// could escape the image directory must be refused outright.
	invalid := []string{
		"", "   ", "../../etc/passwd", "a/b", "a\\b", "イチゴかがり", "-leading",
		"has space", "semi;colon", "null\x00byte", strings.Repeat("a", 65),
	}
	for _, in := range invalid {
		if got, err := SanitizeSlug(in); err == nil {
			t.Errorf("SanitizeSlug(%q) = %q, want an error", in, got)
		}
	}
}

func TestPublishBatchRejectsTraversalSlug(t *testing.T) {
	p, repo := newPublisher()
	item := newItem()
	item.ImageSlug = "../../../public/index"

	if _, err := p.PublishBatch(context.Background(), []Item{item}, submitter); err == nil {
		t.Fatal("expected a slug validation error")
	}
	if len(repo.created) != 0 {
		t.Error("a pull request was opened with a traversing image path")
	}
}

func TestPublishBatchSweetRequiresSweetTypeField(t *testing.T) {
	repo := &fakeRepo{files: map[string][]byte{
		"src/data/sweet.json": []byte("{\n  \"sweet\": []\n}\n"),
	}}
	p := &Publisher{Repo: repo}

	item := newItem()
	item.Kind = cards.KindSweet
	item.Card.Role = nil
	item.ImageSlug = "new_sweet"

	if _, err := p.PublishBatch(context.Background(), []Item{item}, submitter); err == nil {
		t.Fatal("expected an error when sweetType is missing")
	}

	item.Card.SweetType = sweetPtr(cards.SweetCake)
	got, err := p.PublishBatch(context.Background(), []Item{item}, submitter)
	if err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}
	if got.Cards[0].CardID != "s_0" {
		t.Errorf("CardID = %q, want s_0 for an empty dataset", got.Cards[0].CardID)
	}
	if got.Files[1] != "public/images/sweet/new_sweet.webp" {
		t.Errorf("image path = %q, want the sweet directory", got.Files[1])
	}
}

func TestPublishBatchTokenYojoSharesYojoImageDirectory(t *testing.T) {
	repo := &fakeRepo{files: map[string][]byte{
		"src/data/tokenYojo.json": []byte("{\n  \"tokenYojo\": []\n}\n"),
	}}
	p := &Publisher{Repo: repo}

	item := newItem()
	item.Kind = cards.KindTokenYojo
	item.ImageSlug = "token_card"

	got, err := p.PublishBatch(context.Background(), []Item{item}, submitter)
	if err != nil {
		t.Fatalf("PublishBatch: %v", err)
	}
	if got.Cards[0].CardID != "yt_0" {
		t.Errorf("CardID = %q, want yt_0", got.Cards[0].CardID)
	}
	want := []string{"src/data/tokenYojo.json", "public/images/yojo/token_card.webp", "public/og-cards/yojo/token_card.png"}
	for i, path := range want {
		if got.Files[i] != path {
			t.Errorf("Files[%d] = %q, want %q", i, got.Files[i], path)
		}
	}
}

func fileContent(t *testing.T, files []ghapp.File, path string) []byte {
	t.Helper()
	if content := fileContentOrNil(files, path); content != nil {
		return content
	}
	t.Fatalf("file %s not found", path)
	return nil
}

func fileContentOrNil(files []ghapp.File, path string) []byte {
	for _, f := range files {
		if f.Path == path {
			return f.Content
		}
	}
	return nil
}

func testPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func strPtr(s string) *string                     { return &s }
func rolePtr(r cards.CardRole) *cards.CardRole    { return &r }
func sweetPtr(s cards.SweetType) *cards.SweetType { return &s }
