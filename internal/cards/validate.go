package cards

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ValidationError reports one broken rule, tagged with the offending field so
// the web UI can attach it to the right input.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string { return e.Field + ": " + e.Message }

// ValidationErrors is the full set of rules a card broke.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	parts := make([]string, len(e))
	for i, v := range e {
		parts[i] = v.Error()
	}
	return strings.Join(parts, "; ")
}

func (e ValidationErrors) OrNil() error {
	if len(e) == 0 {
		return nil
	}
	return e
}

// Validate checks a single card against the PPLALE-web schema and against the
// extra constraints the dataset it belongs to imposes.
func Validate(ds Dataset, c Card) error {
	return ValidateWithTaxonomy(ds, c, DefaultTaxonomy())
}

// ValidateWithTaxonomy validates classifications against the live upstream
// set, including a new value being introduced by the same submission.
func ValidateWithTaxonomy(ds Dataset, c Card, taxonomy Taxonomy) error {
	var errs ValidationErrors
	add := func(field, msg string) {
		errs = append(errs, ValidationError{Field: field, Message: msg})
	}

	if _, err := ParseID(ds, c.ID); err != nil {
		add("id", err.Error())
	}
	if strings.TrimSpace(c.Name) == "" {
		add("name", "カード名は必須です")
	}
	if _, ok := cardTypes[string(c.Type)]; !ok {
		add("type", fmt.Sprintf("不正なカードタイプ: %q", c.Type))
	} else if c.Type != ds.CardType {
		add("type", fmt.Sprintf("%s のカードは type=%q である必要があります", ds.Kind, ds.CardType))
	}
	if !taxonomy.HasFruit(string(c.Fruit)) {
		add("fruit", fmt.Sprintf("不正なフルーツタイプ: %q", c.Fruit))
	}
	if c.Cost < 0 {
		add("cost", "コストは0以上の整数です")
	}
	if c.HP < 0 {
		add("hp", "体力は0以上の整数です")
	}
	if c.Attack < 0 {
		add("attack", "攻撃力は0以上の整数です")
	}
	validateImageURL(ds, c.ImageURL, add)

	if c.Role != nil {
		if _, ok := cardRoles[string(*c.Role)]; !ok {
			add("role", fmt.Sprintf("不正な役職: %q", *c.Role))
		}
		if c.Type != TypeYojo {
			add("role", "role は幼女カードのみで使用します")
		}
	}
	if c.SweetType != nil {
		if !taxonomy.HasSweetType(string(*c.SweetType)) {
			add("sweetType", fmt.Sprintf("不正なお菓子タイプ: %q", *c.SweetType))
		}
		if c.Type != TypeSweet {
			add("sweetType", "sweetType はお菓子カードのみで使用します")
		}
	}
	// 既存データには sweetType が "" のお菓子カードが存在するため、値ではなく
	// フィールドの存在のみを必須とする（省略すると既存ファイルと形が揃わない）。
	if c.Type == TypeSweet && c.SweetType == nil {
		add("sweetType", "お菓子カードでは sweetType フィールドが必要です（分類なしの場合は空文字）")
	}
	if c.Version != nil {
		if _, ok := cardVersion[string(*c.Version)]; !ok {
			add("version", fmt.Sprintf("不正なバージョン: %q", *c.Version))
		}
	}
	return errs.OrNil()
}

func validateImageURL(ds Dataset, url string, add func(field, msg string)) {
	switch {
	case url == "":
		add("imageUrl", "画像URLは必須です")
	case strings.HasSuffix(strings.ToLower(url), ".png"):
		// PPLALE-web の cards:check が .png を検出すると CI が落ちる。
		add("imageUrl", "PNG のままでは PPLALE-web の CI が落ちます。WebP に変換してください")
	case !strings.HasSuffix(strings.ToLower(url), ".webp"):
		add("imageUrl", "画像は .webp である必要があります")
	case !strings.HasPrefix(url, "/images/"+ds.ImageDir+"/"):
		add("imageUrl", fmt.Sprintf("画像URLは /images/%s/ 配下である必要があります", ds.ImageDir))
	case strings.Contains(url, ".."):
		add("imageUrl", "画像URLに .. は使用できません")
	}
}

// ValidateDataset validates every card in a dataset and additionally rejects
// duplicate IDs, which the upstream CI does not catch.
func ValidateDataset(ds Dataset, list []Card) error {
	return ValidateDatasetWithTaxonomy(ds, list, DefaultTaxonomy())
}

func ValidateDatasetWithTaxonomy(ds Dataset, list []Card, taxonomy Taxonomy) error {
	var errs []error
	seen := make(map[string]int, len(list))
	for i, c := range list {
		if err := ValidateWithTaxonomy(ds, c, taxonomy); err != nil {
			errs = append(errs, fmt.Errorf("%s[%d] (%s): %w", ds.RootKey, i, c.ID, err))
		}
		if first, dup := seen[c.ID]; dup {
			errs = append(errs, fmt.Errorf("%s: ID %q が重複しています (index %d と %d)", ds.JSONPath, c.ID, first, i))
			continue
		}
		seen[c.ID] = i
	}
	return errors.Join(errs...)
}

// ParseID extracts the numeric part of an ID such as "y_42".
func ParseID(ds Dataset, id string) (int, error) {
	if id == "" {
		return 0, errors.New("IDは必須です")
	}
	rest, ok := strings.CutPrefix(id, ds.IDPrefix)
	if !ok {
		return 0, fmt.Errorf("ID は %q で始まる必要があります: %q", ds.IDPrefix, id)
	}
	n, err := strconv.Atoi(rest)
	if err != nil || rest != strconv.Itoa(n) || n < 0 {
		return 0, fmt.Errorf("ID の連番部分が不正です: %q", id)
	}
	return n, nil
}

// NextID allocates the next ID for a dataset: the highest existing sequence
// number plus one. Gaps are deliberately not reused.
func NextID(ds Dataset, list []Card) string {
	highest := -1
	for _, c := range list {
		if n, err := ParseID(ds, c.ID); err == nil && n > highest {
			highest = n
		}
	}
	return ds.IDPrefix + strconv.Itoa(highest+1)
}

// ApplyDatasetDefaults fills the optional fields a dataset conventionally
// carries so that a newly submitted card has the same shape as its neighbours
// in the file. Values the submitter set are never overwritten.
func ApplyDatasetDefaults(ds Dataset, c *Card) {
	switch ds.Kind {
	case KindYojo, KindTokenYojo:
		if c.Role == nil {
			c.Role = ptr(RoleNone)
		}
	case KindSweet:
		if c.SweetType == nil {
			c.SweetType = ptr(SweetNone)
		}
	case KindPlayable:
		if c.Version == nil {
			c.Version = ptr(VersionNormal)
		}
	}
	if c.Effect == nil {
		c.Effect = ptr("")
	}
}
