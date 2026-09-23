package cards

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	UpstreamTypesPath          = "src/types/card.ts"
	UpstreamSchemaPath         = "src/lib/schema.ts"
	UpstreamLocalePath         = "src/i18n/LocaleProvider.tsx"
	UpstreamFilterBarPath      = "src/components/card/CardListFilterBar.tsx"
	UpstreamFruitSelectionPath = "src/app/(app)/deck/2pick/components/FruitVersionSelection.tsx"
	UpstreamTwoPickPath        = "src/app/(app)/deck/2pick/page.tsx"
)

// TaxonomyOption is one value shown in a card form and understood by
// PPLALE-web. Value is the stable source-data key; labels are what people see.
type TaxonomyOption struct {
	Value   string
	LabelJA string
	LabelEN string
}

// TaxonomyChanges carries new classifications alongside a draft. Keeping the
// definition with the card lets one pull request update both the game schema
// and the card that first uses the value.
type TaxonomyChanges struct {
	Fruit     *TaxonomyOption `json:"fruit,omitempty"`
	SweetType *TaxonomyOption `json:"sweetType,omitempty"`
}

// Taxonomy is the live set read from PPLALE-web's schema and label tables.
type Taxonomy struct {
	Fruits     []TaxonomyOption
	SweetTypes []TaxonomyOption
}

type TaxonomyReader interface {
	FileContent(ctx context.Context, path string) ([]byte, error)
}

var optionValuePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// NewTaxonomyOption turns an English display name into the stable key, so a
// non-technical submitter never has to invent an identifier separately.
func NewTaxonomyOption(labelJA, labelEN string) (TaxonomyOption, error) {
	labelJA = strings.TrimSpace(labelJA)
	labelEN = strings.TrimSpace(labelEN)
	if labelJA == "" || labelEN == "" {
		return TaxonomyOption{}, fmt.Errorf("日本語名と英語名を入力してください")
	}
	if len([]rune(labelJA)) > 40 || len([]rune(labelEN)) > 40 {
		return TaxonomyOption{}, fmt.Errorf("名前は40文字以内で入力してください")
	}
	for _, label := range []string{labelJA, labelEN} {
		for _, r := range label {
			if unicode.IsControl(r) {
				return TaxonomyOption{}, fmt.Errorf("名前に改行や制御文字は使用できません")
			}
		}
	}

	var b strings.Builder
	underscore := false
	for _, r := range strings.ToLower(labelEN) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			underscore = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if b.Len() > 0 && !underscore {
				b.WriteByte('_')
				underscore = true
			}
		}
	}
	value := strings.Trim(b.String(), "_")
	if len(value) > 32 {
		value = strings.TrimRight(value[:32], "_")
	}
	if !optionValuePattern.MatchString(value) {
		return TaxonomyOption{}, fmt.Errorf("英語名は半角英字から始まる名前にしてください")
	}
	return TaxonomyOption{Value: value, LabelJA: labelJA, LabelEN: labelEN}, nil
}

func (t Taxonomy) HasFruit(value string) bool {
	return hasOption(t.Fruits, value)
}

func (t Taxonomy) HasSweetType(value string) bool {
	return hasOption(t.SweetTypes, value)
}

func hasOption(options []TaxonomyOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func (t *Taxonomy) AddFruit(option TaxonomyOption) error {
	return addOption(&t.Fruits, option)
}

func (t *Taxonomy) AddSweetType(option TaxonomyOption) error {
	return addOption(&t.SweetTypes, option)
}

func addOption(options *[]TaxonomyOption, addition TaxonomyOption) error {
	for _, existing := range *options {
		if existing.Value != addition.Value {
			continue
		}
		if existing.LabelJA == addition.LabelJA && existing.LabelEN == addition.LabelEN {
			return nil
		}
		return fmt.Errorf("分類キー %q は別の表示名ですでに使われています", addition.Value)
	}
	*options = append(*options, addition)
	return nil
}

// DefaultTaxonomy is used only when the live label source is temporarily
// unavailable. Publishing a new classification always requires live sources.
func DefaultTaxonomy() Taxonomy {
	return Taxonomy{
		Fruits: []TaxonomyOption{
			{Value: "all", LabelJA: "全種", LabelEN: "All"},
			{Value: "strawberry", LabelJA: "いちご", LabelEN: "Strawberry"},
			{Value: "grape", LabelJA: "ぶどう", LabelEN: "Grape"},
			{Value: "melon", LabelJA: "メロン", LabelEN: "Melon"},
			{Value: "orange", LabelJA: "オレンジ", LabelEN: "Orange"},
		},
		SweetTypes: []TaxonomyOption{
			{Value: "", LabelJA: "分類なし", LabelEN: "Uncategorized"},
			{Value: "animal_soda", LabelJA: "動物さんソーダ", LabelEN: "Animal soda"},
			{Value: "cafe", LabelJA: "カフェ", LabelEN: "Café"},
			{Value: "float", LabelJA: "フロート", LabelEN: "Float"},
			{Value: "doughnut", LabelJA: "ドーナツ", LabelEN: "Doughnut"},
			{Value: "cake", LabelJA: "ケーキ", LabelEN: "Cake"},
			{Value: "back_menu", LabelJA: "裏メニュー", LabelEN: "Back menu"},
			{Value: "chai", LabelJA: "チャイ", LabelEN: "Chai"},
			{Value: "ice_cream", LabelJA: "アイスクリーム", LabelEN: "Ice cream"},
			{Value: "pplale_soda", LabelJA: "ぷぷりえソーダ", LabelEN: "PPLALE soda"},
			{Value: "pplale_yaki", LabelJA: "ぷぷりえ焼き", LabelEN: "PPLALE-yaki"},
			{Value: "currency", LabelJA: "通貨", LabelEN: "Currency"},
		},
	}
}

func LoadTaxonomy(ctx context.Context, reader TaxonomyReader) (Taxonomy, error) {
	schema, err := reader.FileContent(ctx, UpstreamSchemaPath)
	if err != nil {
		return Taxonomy{}, fmt.Errorf("分類定義を取得できませんでした: %w", err)
	}
	locale, err := reader.FileContent(ctx, UpstreamLocalePath)
	if err != nil {
		return Taxonomy{}, fmt.Errorf("分類の表示名を取得できませんでした: %w", err)
	}
	return ParseTaxonomy(schema, locale)
}

func ParseTaxonomy(schema, locale []byte) (Taxonomy, error) {
	fruits, err := parseZodEnum(string(schema), "fruitTypeSchema")
	if err != nil {
		return Taxonomy{}, err
	}
	sweets, err := parseZodEnum(string(schema), "sweetTypeSchema")
	if err != nil {
		return Taxonomy{}, err
	}
	fruitJA, err := parseLocaleLabels(string(locale), "fruitLabels", "ja")
	if err != nil {
		return Taxonomy{}, err
	}
	fruitEN, err := parseLocaleLabels(string(locale), "fruitLabels", "en")
	if err != nil {
		return Taxonomy{}, err
	}
	sweetJA, err := parseLocaleLabels(string(locale), "sweetTypeLabels", "ja")
	if err != nil {
		return Taxonomy{}, err
	}
	sweetEN, err := parseLocaleLabels(string(locale), "sweetTypeLabels", "en")
	if err != nil {
		return Taxonomy{}, err
	}
	taxonomy := Taxonomy{
		Fruits:     joinOptions(fruits, fruitJA, fruitEN),
		SweetTypes: joinOptions(sweets, sweetJA, sweetEN),
	}
	for i := range taxonomy.SweetTypes {
		if taxonomy.SweetTypes[i].Value == "" {
			taxonomy.SweetTypes[i].LabelJA = "分類なし"
			taxonomy.SweetTypes[i].LabelEN = "Uncategorized"
		}
	}
	return taxonomy, nil
}

func joinOptions(values []string, ja, en map[string]string) []TaxonomyOption {
	out := make([]TaxonomyOption, 0, len(values))
	for _, value := range values {
		labelJA, labelEN := ja[value], en[value]
		if labelJA == "" && value != "" {
			labelJA = value
		}
		if labelEN == "" && value != "" {
			labelEN = value
		}
		out = append(out, TaxonomyOption{Value: value, LabelJA: labelJA, LabelEN: labelEN})
	}
	return out
}

func parseZodEnum(source, name string) ([]string, error) {
	pattern := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(name) + `\s*=\s*z\.enum\(\s*\[(.*?)\]`)
	match := pattern.FindStringSubmatch(source)
	if match == nil {
		return nil, fmt.Errorf("%s が見つかりません", name)
	}
	quoted := regexp.MustCompile(`'((?:\\.|[^'])*)'|"((?:\\.|[^"])*)"`).FindAllStringSubmatch(match[1], -1)
	out := make([]string, 0, len(quoted))
	for _, value := range quoted {
		out = append(out, unescapeTS(value[1]+value[2]))
	}
	return out, nil
}

func parseLocaleLabels(source, constName, locale string) (map[string]string, error) {
	start := strings.Index(source, "const "+constName)
	if start < 0 {
		return nil, fmt.Errorf("%s が見つかりません", constName)
	}
	blockStart := strings.Index(source[start:], locale+": {")
	if blockStart < 0 {
		return nil, fmt.Errorf("%s の %s 表示名が見つかりません", constName, locale)
	}
	blockStart += start + len(locale+": ")
	blockEnd, err := matchingBrace(source, blockStart)
	if err != nil {
		return nil, err
	}
	entryPattern := regexp.MustCompile(`(?:'((?:\\.|[^'])*)'|([a-zA-Z_][a-zA-Z0-9_]*))\s*:\s*'((?:\\.|[^'])*)'`)
	out := map[string]string{}
	for _, match := range entryPattern.FindAllStringSubmatch(source[blockStart+1:blockEnd], -1) {
		out[unescapeTS(match[1]+match[2])] = unescapeTS(match[3])
	}
	return out, nil
}

func matchingBrace(source string, open int) (int, error) {
	if open < 0 || open >= len(source) || source[open] != '{' {
		return 0, fmt.Errorf("分類定義の開始位置が不正です")
	}
	depth := 0
	inString := byte(0)
	escaped := false
	for i := open; i < len(source); i++ {
		c := source[i]
		if inString != 0 {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == inString {
				inString = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			inString = c
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("分類定義の終端が見つかりません")
}

func unescapeTS(value string) string {
	value = strings.ReplaceAll(value, `\'`, `'`)
	value = strings.ReplaceAll(value, `\\`, `\`)
	return value
}
