package cards

import (
	"fmt"
	"regexp"
	"strings"
)

// PatchTaxonomySources updates the PPLALE-web source files that define and
// display classifications. The caller passes fresh files from the base branch
// and commits the returned versions in the same PR as the card data.
func PatchTaxonomySources(sources map[string][]byte, changes TaxonomyChanges) (map[string][]byte, error) {
	out := make(map[string][]byte)
	for path, content := range sources {
		out[path] = append([]byte(nil), content...)
	}

	if changes.Fruit != nil {
		option := *changes.Fruit
		var err error
		out[UpstreamTypesPath], err = patchUnion(out[UpstreamTypesPath], "FruitType", option.Value)
		if err != nil {
			return nil, err
		}
		out[UpstreamSchemaPath], err = patchEnum(out[UpstreamSchemaPath], "fruitTypeSchema", option.Value)
		if err != nil {
			return nil, err
		}
		out[UpstreamLocalePath], err = patchLabels(out[UpstreamLocalePath], "fruitLabels", option)
		if err != nil {
			return nil, err
		}
		out[UpstreamFilterBarPath], err = patchFruitArray(out[UpstreamFilterBarPath], option.Value)
		if err != nil {
			return nil, err
		}
		out[UpstreamFruitSelectionPath], err = patchFruitArray(out[UpstreamFruitSelectionPath], option.Value)
		if err != nil {
			return nil, err
		}
		out[UpstreamTwoPickPath], err = patchLegacyFruit(out[UpstreamTwoPickPath], option.Value)
		if err != nil {
			return nil, err
		}
	}

	if changes.SweetType != nil {
		option := *changes.SweetType
		var err error
		out[UpstreamTypesPath], err = patchUnion(out[UpstreamTypesPath], "SweetType", option.Value)
		if err != nil {
			return nil, err
		}
		out[UpstreamSchemaPath], err = patchEnum(out[UpstreamSchemaPath], "sweetTypeSchema", option.Value)
		if err != nil {
			return nil, err
		}
		out[UpstreamLocalePath], err = patchLabels(out[UpstreamLocalePath], "sweetTypeLabels", option)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func TaxonomySourcePaths(changes TaxonomyChanges) []string {
	if changes.Fruit == nil && changes.SweetType == nil {
		return nil
	}
	paths := []string{UpstreamTypesPath, UpstreamSchemaPath, UpstreamLocalePath}
	if changes.Fruit != nil {
		paths = append(paths, UpstreamFilterBarPath, UpstreamFruitSelectionPath, UpstreamTwoPickPath)
	}
	return paths
}

func patchUnion(source []byte, typeName, value string) ([]byte, error) {
	text := string(source)
	re := regexp.MustCompile(`(?s)(export type ` + regexp.QuoteMeta(typeName) + `\s*=\s*)(.*?)(;)`)
	match := re.FindStringSubmatchIndex(text)
	if match == nil {
		return nil, fmt.Errorf("%s の型定義を更新できません", typeName)
	}
	body := text[match[4]:match[5]]
	if quotedValueExists(body, value) {
		return source, nil
	}
	addition := " | '" + escapeTS(value) + "'"
	if strings.Contains(body, "\n") {
		addition = "\n  | '" + escapeTS(value) + "'"
	}
	return []byte(text[:match[5]] + addition + text[match[5]:]), nil
}

func patchEnum(source []byte, constName, value string) ([]byte, error) {
	text := string(source)
	re := regexp.MustCompile(`(?s)(` + regexp.QuoteMeta(constName) + `\s*=\s*z\.enum\(\s*\[)(.*?)(\])`)
	match := re.FindStringSubmatchIndex(text)
	if match == nil {
		return nil, fmt.Errorf("%s のスキーマを更新できません", constName)
	}
	body := text[match[4]:match[5]]
	if quotedValueExists(body, value) {
		return source, nil
	}
	addition := ", '" + escapeTS(value) + "'"
	if strings.Contains(body, "\n") {
		addition = "  '" + escapeTS(value) + "',\n"
		if !strings.HasSuffix(body, "\n") {
			addition = "\n" + addition
		}
	}
	return []byte(text[:match[5]] + addition + text[match[5]:]), nil
}

func patchLabels(source []byte, constName string, option TaxonomyOption) ([]byte, error) {
	text := string(source)
	var err error
	text, err = insertLocaleLabel(text, constName, "ja", option.Value, option.LabelJA)
	if err != nil {
		return nil, err
	}
	text, err = insertLocaleLabel(text, constName, "en", option.Value, option.LabelEN)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func insertLocaleLabel(source, constName, locale, value, label string) (string, error) {
	start := strings.Index(source, "const "+constName)
	if start < 0 {
		return "", fmt.Errorf("%s が見つかりません", constName)
	}
	localeAt := strings.Index(source[start:], locale+": {")
	if localeAt < 0 {
		return "", fmt.Errorf("%s の %s 表示名を更新できません", constName, locale)
	}
	open := start + localeAt + len(locale+": ")
	close, err := matchingBrace(source, open)
	if err != nil {
		return "", err
	}
	body := source[open+1 : close]
	if quotedKeyExists(body, value) {
		return source, nil
	}
	entry := "'" + escapeTS(value) + "': '" + escapeTS(label) + "'"
	if strings.Contains(body, "\n") {
		// The two spaces before the closing brace are already part of source[:close].
		addition := "  " + entry + ",\n  "
		return source[:close] + addition + source[close:], nil
	}
	separator := ", "
	if strings.TrimSpace(body) == "" {
		separator = ""
	}
	return source[:close] + separator + entry + source[close:], nil
}

func patchFruitArray(source []byte, value string) ([]byte, error) {
	text := string(source)
	re := regexp.MustCompile(`(?s)\(\[('strawberry'.*?)\]\s+as\s+FruitType\[\]\)`)
	match := re.FindStringSubmatchIndex(text)
	if match == nil {
		return nil, fmt.Errorf("フルーツ選択肢を更新できません")
	}
	body := text[match[2]:match[3]]
	if quotedValueExists(body, value) {
		return source, nil
	}
	return []byte(text[:match[3]] + ", '" + escapeTS(value) + "'" + text[match[3]:]), nil
}

func patchLegacyFruit(source []byte, value string) ([]byte, error) {
	text := string(source)
	start := strings.Index(text, "const legacyFruitCodes")
	if start < 0 {
		return nil, fmt.Errorf("2Pick のフルーツ定義を更新できません")
	}
	openRel := strings.Index(text[start:], "{")
	if openRel < 0 {
		return nil, fmt.Errorf("2Pick のフルーツ定義を更新できません")
	}
	open := start + openRel
	close, err := matchingBrace(text, open)
	if err != nil {
		return nil, err
	}
	body := text[open+1 : close]
	if quotedKeyExists(body, value) || regexp.MustCompile(`(?m)\b`+regexp.QuoteMeta(value)+`\s*:`).MatchString(body) {
		return source, nil
	}
	entry := "  " + value + ": '" + escapeTS(value) + "',\n"
	return []byte(text[:close] + entry + text[close:]), nil
}

func quotedValueExists(source, value string) bool {
	return strings.Contains(source, "'"+escapeTS(value)+"'") || strings.Contains(source, `"`+value+`"`)
}

func quotedKeyExists(source, value string) bool {
	return strings.Contains(source, "'"+escapeTS(value)+"':") || strings.Contains(source, `"`+value+`":`)
}

func escapeTS(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return value
}
