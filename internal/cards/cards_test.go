package cards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The testdata files are verbatim copies of the PPLALE-web datasets. Encoding
// them back must reproduce the committed bytes, otherwise every generated pull
// request would rewrite the whole file.
func TestRoundTripMatchesUpstreamBytes(t *testing.T) {
	for _, kind := range AllKinds() {
		t.Run(string(kind), func(t *testing.T) {
			ds := datasetFor(t, kind)
			original := readFixture(t, ds)

			list, err := Decode(ds, original)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(list) == 0 {
				t.Fatal("fixture decoded to zero cards")
			}

			encoded, err := Encode(ds, list)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if string(encoded) != string(original) {
				t.Errorf("re-encoded %s differs from upstream bytes\n%s", ds.JSONPath, firstDiff(string(original), string(encoded)))
			}
		})
	}
}

func TestUpstreamFixturesPassValidation(t *testing.T) {
	for _, kind := range AllKinds() {
		t.Run(string(kind), func(t *testing.T) {
			ds := datasetFor(t, kind)
			list, err := Decode(ds, readFixture(t, ds))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if err := ValidateDataset(ds, list); err != nil {
				t.Errorf("upstream data fails our validation: %v", err)
			}
		})
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	ds := datasetFor(t, KindYojo)
	in := `{"yojo":[{"id":"y_0","name":"x","type":"yojo","fruit":"all","description":"","imageUrl":"/images/yojo/x.webp","cost":0,"hp":0,"attack":0,"rarity":"SSR"}]}`
	if _, err := Decode(ds, []byte(in)); err == nil {
		t.Fatal("expected an error for an unknown field, got nil")
	}
}

func TestDecodeRejectsWrongRootKey(t *testing.T) {
	ds := datasetFor(t, KindYojo)
	if _, err := Decode(ds, []byte(`{"sweet":[]}`)); err == nil {
		t.Fatal("expected an error for a mismatched root key, got nil")
	}
}

func TestEncodeAppendedCard(t *testing.T) {
	ds := datasetFor(t, KindTokenYojo)
	list, err := Decode(ds, readFixture(t, ds))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	added := Card{
		ID:          NextID(ds, list),
		Name:        "美女うゆち",
		Type:        TypeYojo,
		Fruit:       FruitStrawberry,
		Description: "",
		ImageURL:    ds.ImageURL("bijo_uyuchi.webp"),
		Cost:        3,
		HP:          3,
		Attack:      3,
		Effect:      ptr("テスト効果"),
		Role:        ptr(RoleNone),
	}
	if added.ID != "yt_2" {
		t.Fatalf("NextID = %q, want yt_2", added.ID)
	}

	encoded, err := Encode(ds, append(list, added))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := `    {
      "id": "yt_2",
      "name": "美女うゆち",
      "type": "yojo",
      "fruit": "strawberry",
      "description": "",
      "imageUrl": "/images/yojo/bijo_uyuchi.webp",
      "cost": 3,
      "hp": 3,
      "attack": 3,
      "effect": "テスト効果",
      "role": ""
    }
  ]
}
`
	if !strings.HasSuffix(string(encoded), want) {
		t.Errorf("appended card not rendered as expected, got tail:\n%s", tail(string(encoded), 16))
	}

	reparsed, err := Decode(ds, encoded)
	if err != nil {
		t.Fatalf("re-Decode: %v", err)
	}
	if len(reparsed) != len(list)+1 {
		t.Fatalf("re-decoded %d cards, want %d", len(reparsed), len(list)+1)
	}
	if err := ValidateDataset(ds, reparsed); err != nil {
		t.Errorf("appended dataset fails validation: %v", err)
	}
}

func TestEncodeEmptyDataset(t *testing.T) {
	ds := datasetFor(t, KindSweet)
	out, err := Encode(ds, nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(out) != "{\n  \"sweet\": []\n}\n" {
		t.Errorf("Encode(empty) = %q", out)
	}
}

func TestParseID(t *testing.T) {
	ds := datasetFor(t, KindYojo)
	tokenDS := datasetFor(t, KindTokenYojo)

	valid := map[string]int{"y_0": 0, "y_7": 7, "y_151": 151}
	for id, want := range valid {
		got, err := ParseID(ds, id)
		if err != nil || got != want {
			t.Errorf("ParseID(%q) = (%d, %v), want (%d, nil)", id, got, err, want)
		}
	}
	for _, id := range []string{"", "y_", "y_-1", "y_01", "y_1a", "s_1", "yt_1", "Y_1"} {
		if _, err := ParseID(ds, id); err == nil {
			t.Errorf("ParseID(%q) accepted an invalid ID", id)
		}
	}
	// "y_" is a prefix of no token id; token ids must not be read as yojo ids.
	if _, err := ParseID(tokenDS, "y_1"); err == nil {
		t.Error("ParseID(tokenYojo, \"y_1\") accepted a yojo ID")
	}
}

func TestNextIDIgnoresGapsAndMalformedIDs(t *testing.T) {
	ds := datasetFor(t, KindYojo)
	list := []Card{{ID: "y_0"}, {ID: "y_5"}, {ID: "broken"}, {ID: "y_3"}}
	if got := NextID(ds, list); got != "y_6" {
		t.Errorf("NextID = %q, want y_6", got)
	}
	if got := NextID(ds, nil); got != "y_0" {
		t.Errorf("NextID(empty) = %q, want y_0", got)
	}
}

func TestValidate(t *testing.T) {
	yojo := datasetFor(t, KindYojo)
	sweet := datasetFor(t, KindSweet)

	base := func() Card {
		return Card{
			ID: "y_9", Name: "テスト", Type: TypeYojo, Fruit: FruitMelon,
			ImageURL: "/images/yojo/test.webp", Cost: 1, HP: 1, Attack: 1,
			Role: ptr(RoleNone),
		}
	}

	if err := Validate(yojo, base()); err != nil {
		t.Fatalf("valid card rejected: %v", err)
	}

	cases := []struct {
		name  string
		ds    Dataset
		mut   func(*Card)
		field string
	}{
		{"missing name", yojo, func(c *Card) { c.Name = "  " }, "name"},
		{"bad id prefix", yojo, func(c *Card) { c.ID = "s_1" }, "id"},
		{"bad fruit", yojo, func(c *Card) { c.Fruit = "banana" }, "fruit"},
		{"negative cost", yojo, func(c *Card) { c.Cost = -1 }, "cost"},
		{"negative hp", yojo, func(c *Card) { c.HP = -1 }, "hp"},
		{"negative attack", yojo, func(c *Card) { c.Attack = -1 }, "attack"},
		{"png image breaks upstream ci", yojo, func(c *Card) { c.ImageURL = "/images/yojo/test.png" }, "imageUrl"},
		{"empty image", yojo, func(c *Card) { c.ImageURL = "" }, "imageUrl"},
		{"image in wrong directory", yojo, func(c *Card) { c.ImageURL = "/images/sweet/test.webp" }, "imageUrl"},
		{"path traversal in image", yojo, func(c *Card) { c.ImageURL = "/images/yojo/../../etc/passwd.webp" }, "imageUrl"},
		{"unknown role", yojo, func(c *Card) { c.Role = ptr(CardRole("boss")) }, "role"},
		{"unknown version", yojo, func(c *Card) { c.Version = ptr(CardVersion("alpha")) }, "version"},
		{"type mismatch with dataset", yojo, func(c *Card) { c.Type = TypeSweet }, "type"},
		{"role on non yojo", sweet, func(c *Card) {
			*c = Card{ID: "s_1", Name: "n", Type: TypeSweet, Fruit: FruitAll, ImageURL: "/images/sweet/a.webp", SweetType: ptr(SweetCake), Role: ptr(RoleManager)}
		}, "role"},
		{"sweet missing the sweetType field", sweet, func(c *Card) {
			*c = Card{ID: "s_1", Name: "n", Type: TypeSweet, Fruit: FruitAll, ImageURL: "/images/sweet/a.webp"}
		}, "sweetType"},
		{"sweetType on yojo", yojo, func(c *Card) { c.SweetType = ptr(SweetCake) }, "sweetType"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mut(&c)
			err := Validate(tc.ds, c)
			if err == nil {
				t.Fatalf("expected a %s error, got nil", tc.field)
			}
			var found bool
			for _, ve := range err.(ValidationErrors) {
				if ve.Field == tc.field {
					found = true
				}
			}
			if !found {
				t.Errorf("expected an error on field %q, got %v", tc.field, err)
			}
		})
	}
}

func TestValidateDatasetRejectsDuplicateID(t *testing.T) {
	ds := datasetFor(t, KindYojo)
	card := Card{ID: "y_0", Name: "n", Type: TypeYojo, Fruit: FruitAll, ImageURL: "/images/yojo/a.webp"}
	err := ValidateDataset(ds, []Card{card, card})
	if err == nil || !strings.Contains(err.Error(), "重複") {
		t.Fatalf("expected a duplicate ID error, got %v", err)
	}
}

func TestDatasetPaths(t *testing.T) {
	token := datasetFor(t, KindTokenYojo)
	if got := token.ImagePath("a.webp"); got != "public/images/yojo/a.webp" {
		t.Errorf("token ImagePath = %q, want the shared yojo directory", got)
	}
	if got := token.OGImagePath("a.png"); got != "public/og-cards/yojo/a.png" {
		t.Errorf("token OGImagePath = %q, want the shared yojo directory", got)
	}
	if got := token.ImageURL("a.webp"); got != "/images/yojo/a.webp" {
		t.Errorf("token ImageURL = %q", got)
	}
}

func TestRepoImagePath(t *testing.T) {
	// This round-trips through ImagePath/ImageURL on purpose: the way a raw
	// imageUrl field gets turned back into a repository path must stay in
	// sync with how ImagePath builds it in the first place.
	ds := datasetFor(t, KindYojo)
	fileName := "111イチゴかがり.webp"
	if got, want := RepoImagePath(ds.ImageURL(fileName)), ds.ImagePath(fileName); got != want {
		t.Errorf("RepoImagePath(ds.ImageURL(%q)) = %q, want %q", fileName, got, want)
	}
}

func datasetFor(t *testing.T, kind Kind) Dataset {
	t.Helper()
	ds, err := DatasetFor(kind)
	if err != nil {
		t.Fatalf("DatasetFor(%q): %v", kind, err)
	}
	return ds
}

func readFixture(t *testing.T, ds Dataset) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filepath.Base(ds.JSONPath)))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return "line " + itoa(i+1) + "\n  want: " + al[i] + "\n  got:  " + bl[i]
		}
	}
	return "line counts differ: want " + itoa(len(al)) + ", got " + itoa(len(bl))
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestApplyDatasetDefaultsMatchesFileConventions(t *testing.T) {
	for kind, want := range map[Kind]func(Card) bool{
		KindYojo:      func(c Card) bool { return c.Role != nil && *c.Role == RoleNone && c.SweetType == nil },
		KindTokenYojo: func(c Card) bool { return c.Role != nil && c.Version == nil },
		KindSweet:     func(c Card) bool { return c.SweetType != nil && c.Role == nil },
		KindPlayable:  func(c Card) bool { return c.Version != nil && *c.Version == VersionNormal && c.Role == nil },
	} {
		ds := datasetFor(t, kind)
		var c Card
		ApplyDatasetDefaults(ds, &c)
		if !want(c) {
			t.Errorf("%s defaults = %+v", kind, c)
		}
		if c.Effect == nil {
			t.Errorf("%s: effect field should be present", kind)
		}
	}

	// A value the submitter chose must survive.
	ds := datasetFor(t, KindYojo)
	c := Card{Role: ptr(RoleManager)}
	ApplyDatasetDefaults(ds, &c)
	if *c.Role != RoleManager {
		t.Errorf("role = %q, want the submitted value kept", *c.Role)
	}
}
