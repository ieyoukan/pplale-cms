package cards

import (
	"strings"
	"testing"
)

const taxonomyTestSchema = `
export const fruitTypeSchema = z.enum(['all', 'strawberry']);
export const sweetTypeSchema = z.enum(['', 'cake']);
`

const taxonomyTestLocale = `
const fruitLabels = {
  ja: { all: 'すべて', strawberry: 'いちご' },
  en: { all: 'All', strawberry: 'Strawberry' },
};
const sweetTypeLabels = {
  ja: { '': '', cake: 'ケーキ' },
  en: { '': '', cake: 'Cake' },
};
`

func TestNewTaxonomyOptionGeneratesStableValue(t *testing.T) {
	option, err := NewTaxonomyOption("アップルパイ", "Apple Pie")
	if err != nil {
		t.Fatal(err)
	}
	if option.Value != "apple_pie" {
		t.Errorf("Value = %q, want apple_pie", option.Value)
	}
	if _, err := NewTaxonomyOption("りんご", "りんご"); err == nil {
		t.Error("a value with no ASCII identifier was accepted")
	}
}

func TestParseTaxonomyUsesFriendlyEmptySweetLabel(t *testing.T) {
	taxonomy, err := ParseTaxonomy([]byte(taxonomyTestSchema), []byte(taxonomyTestLocale))
	if err != nil {
		t.Fatal(err)
	}
	if !taxonomy.HasFruit("strawberry") || !taxonomy.HasSweetType("cake") {
		t.Fatalf("taxonomy = %+v", taxonomy)
	}
	if got := taxonomy.SweetTypes[0].LabelJA; got != "分類なし" {
		t.Errorf("empty sweet label = %q, want 分類なし", got)
	}
}

func TestPatchTaxonomySourcesAddsFruitAndSweetType(t *testing.T) {
	fruit, _ := NewTaxonomyOption("りんご", "Apple")
	sweet, _ := NewTaxonomyOption("タルト", "Tart")
	changes := TaxonomyChanges{Fruit: &fruit, SweetType: &sweet}
	sources := map[string][]byte{
		UpstreamTypesPath:          []byte("export type FruitType = 'all' | 'strawberry';\nexport type SweetType =\n  | ''\n  | 'cake';\n"),
		UpstreamSchemaPath:         []byte(taxonomyTestSchema),
		UpstreamLocalePath:         []byte(taxonomyTestLocale),
		UpstreamFilterBarPath:      []byte("(['strawberry', 'grape', 'melon', 'orange'] as FruitType[])"),
		UpstreamFruitSelectionPath: []byte("(['strawberry', 'grape', 'melon', 'orange'] as FruitType[])"),
		UpstreamTwoPickPath:        []byte("const legacyFruitCodes = {\n  strawberry: 'strawberry',\n};"),
	}

	patched, err := PatchTaxonomySources(sources, changes)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{UpstreamTypesPath, UpstreamSchemaPath, UpstreamLocalePath} {
		if !strings.Contains(string(patched[path]), "apple") || !strings.Contains(string(patched[path]), "tart") {
			t.Errorf("%s does not contain both additions:\n%s", path, patched[path])
		}
	}
	if !strings.Contains(string(patched[UpstreamLocalePath]), "りんご") || !strings.Contains(string(patched[UpstreamLocalePath]), "タルト") {
		t.Error("Japanese labels were not added")
	}
	banana, _ := NewTaxonomyOption("バナナ", "Banana")
	patched, err = PatchTaxonomySources(patched, TaxonomyChanges{Fruit: &banana})
	if err != nil {
		t.Fatalf("adding a second custom fruit: %v", err)
	}
	if !strings.Contains(string(patched[UpstreamFruitSelectionPath]), "'banana'") {
		t.Error("a second custom fruit was not added to the existing array")
	}
}
