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
	if !ok {
		return nil, errors.New("not found: " + path)
	}
	return data, nil
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

func newPublisher() (*Publisher, *fakeRepo) {
	repo := &fakeRepo{files: map[string][]byte{"src/data/yojo.json": []byte(yojoFixture)}}
	return &Publisher{Repo: repo}, repo
}

func newSubmission() Submission {
	return Submission{
		Kind: cards.KindYojo,
		Card: cards.Card{
			Name:  "あたらしい子",
			Fruit: cards.FruitMelon,
			Cost:  2, HP: 3, Attack: 1,
			Effect: strPtr("テスト効果"),
			Role:   rolePtr(cards.RoleNone),
		},
		ImageSlug:   "atarashii_ko",
		ImageSource: testPNG(),
		Submitter:   Submitter{DiscordID: "123456789012345678", DiscordName: "creator#1"},
	}
}

func TestPublishNewCard(t *testing.T) {
	p, repo := newPublisher()

	got, err := p.Publish(context.Background(), newSubmission())
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// IDs come from live upstream data and never reuse the y_1..y_3 gap.
	if got.CardID != "y_5" {
		t.Errorf("CardID = %q, want y_5", got.CardID)
	}
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
		"src/data/yojo.json":                   false,
		"public/images/yojo/atarashii_ko.webp": false,
		// OGP用PNGが同一PRに含まれないと本番のOGP画像が壊れる。
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

func TestPublishEditKeepsExistingImage(t *testing.T) {
	p, repo := newPublisher()

	sub := newSubmission()
	sub.Card.ID = "y_0"
	sub.Card.Name = "かがり(改)"
	sub.ImageSource = nil
	sub.ImageSlug = ""

	got, err := p.Publish(context.Background(), sub)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got.Branch != "cms/yojo-y_0" {
		t.Errorf("Branch = %q", got.Branch)
	}
	if len(got.Files) != 1 || got.Files[0] != "src/data/yojo.json" {
		t.Errorf("Files = %v, want only the dataset", got.Files)
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

func TestPublishRejectsUnknownIDOnEdit(t *testing.T) {
	p, repo := newPublisher()
	sub := newSubmission()
	sub.Card.ID = "y_999"
	sub.ImageSource = nil

	if _, err := p.Publish(context.Background(), sub); err == nil {
		t.Fatal("expected an error for an unknown card ID")
	}
	if len(repo.created) != 0 {
		t.Error("a pull request was opened for an invalid submission")
	}
}

func TestPublishRequiresImageForNewCard(t *testing.T) {
	p, repo := newPublisher()
	sub := newSubmission()
	sub.ImageSource = nil

	if _, err := p.Publish(context.Background(), sub); err == nil {
		t.Fatal("expected an error when a new card has no image")
	}
	if len(repo.created) != 0 {
		t.Error("a pull request was opened without an image")
	}
}

func TestPublishRequiresSubmitter(t *testing.T) {
	p, _ := newPublisher()
	sub := newSubmission()
	sub.Submitter.DiscordID = ""

	if _, err := p.Publish(context.Background(), sub); err == nil {
		t.Fatal("expected an error when the submitter is unknown")
	}
}

func TestPublishRejectsInvalidCardBeforeOpeningPR(t *testing.T) {
	p, repo := newPublisher()
	sub := newSubmission()
	sub.Card.Fruit = "banana"

	_, err := p.Publish(context.Background(), sub)
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

func TestPublishPropagatesFetchFailure(t *testing.T) {
	repo := &fakeRepo{fetchErr: errors.New("boom")}
	p := &Publisher{Repo: repo}

	if _, err := p.Publish(context.Background(), newSubmission()); err == nil {
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

func TestPublishRejectsTraversalSlug(t *testing.T) {
	p, repo := newPublisher()
	sub := newSubmission()
	sub.ImageSlug = "../../../public/index"

	if _, err := p.Publish(context.Background(), sub); err == nil {
		t.Fatal("expected a slug validation error")
	}
	if len(repo.created) != 0 {
		t.Error("a pull request was opened with a traversing image path")
	}
}

func TestPublishSweetRequiresSweetTypeField(t *testing.T) {
	repo := &fakeRepo{files: map[string][]byte{
		"src/data/sweet.json": []byte("{\n  \"sweet\": []\n}\n"),
	}}
	p := &Publisher{Repo: repo}

	sub := newSubmission()
	sub.Kind = cards.KindSweet
	sub.Card.Role = nil
	sub.ImageSlug = "new_sweet"

	if _, err := p.Publish(context.Background(), sub); err == nil {
		t.Fatal("expected an error when sweetType is missing")
	}

	sub.Card.SweetType = sweetPtr(cards.SweetCake)
	got, err := p.Publish(context.Background(), sub)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got.CardID != "s_0" {
		t.Errorf("CardID = %q, want s_0 for an empty dataset", got.CardID)
	}
	if got.Files[1] != "public/images/sweet/new_sweet.webp" {
		t.Errorf("image path = %q, want the sweet directory", got.Files[1])
	}
}

func TestPublishTokenYojoSharesYojoImageDirectory(t *testing.T) {
	repo := &fakeRepo{files: map[string][]byte{
		"src/data/tokenYojo.json": []byte("{\n  \"tokenYojo\": []\n}\n"),
	}}
	p := &Publisher{Repo: repo}

	sub := newSubmission()
	sub.Kind = cards.KindTokenYojo
	sub.ImageSlug = "token_card"

	got, err := p.Publish(context.Background(), sub)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got.CardID != "yt_0" {
		t.Errorf("CardID = %q, want yt_0", got.CardID)
	}
	want := []string{"src/data/tokenYojo.json", "public/images/yojo/token_card.webp", "public/og-cards/yojo/token_card.png"}
	for i, path := range want {
		if got.Files[i] != path {
			t.Errorf("Files[%d] = %q, want %q", i, got.Files[i], path)
		}
	}
}

func fileContent(t *testing.T, files any, path string) []byte {
	t.Helper()
	switch v := files.(type) {
	case []ghapp.File:
		for _, f := range v {
			if f.Path == path {
				return f.Content
			}
		}
	}
	t.Fatalf("file %s not found", path)
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
