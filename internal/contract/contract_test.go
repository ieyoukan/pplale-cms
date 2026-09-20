// Package contract verifies, against a real PPLALE-web checkout, that the
// assumptions baked into this repository still hold. Everything here is read
// mechanically from PPLALE-web's own sources rather than transcribed by hand,
// so a change upstream fails a test instead of silently breaking a pull
// request the CMS opens.
//
// Run with `mise run contract-check`. The tests skip when PPLALE-web is not
// checked out next to this repository, so CI without it still passes.
package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/ieyoukan/pplale-cms/internal/cards"
	"github.com/ieyoukan/pplale-cms/internal/imageconv"
)

// upstreamRoot locates the PPLALE-web checkout, or skips the test.
func upstreamRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("PPLALE_WEB_PATH")
	if root == "" {
		root = filepath.Join("..", "..", "..", "PPLALE-web")
	}
	if _, err := os.Stat(filepath.Join(root, "src", "lib", "schema.ts")); err != nil {
		// CONTRACT_STRICT=1 のとき、チェックできないことを「通った」と誤認しない。
		if os.Getenv("CONTRACT_STRICT") == "1" {
			t.Fatalf("PPLALE-web が見つかりません (%s)。PPLALE_WEB_PATH で指定してください", root)
		}
		t.Skipf("PPLALE-web が見つかりません (%s)。PPLALE_WEB_PATH で指定してください", root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	return abs
}

func read(t *testing.T, root string, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatalf("read %v: %v", parts, err)
	}
	return string(data)
}

// TestEnumsMatchUpstreamSchema compares every z.enum in PPLALE-web's
// src/lib/schema.ts with the value sets this repository validates against.
func TestEnumsMatchUpstreamSchema(t *testing.T) {
	root := upstreamRoot(t)
	schema := read(t, root, "src", "lib", "schema.ts")

	cases := []struct {
		constName string
		ours      []string
	}{
		{"cardTypeSchema", enumStrings(cards.AllCardTypes())},
		{"fruitTypeSchema", enumStrings(cards.AllFruits())},
		{"cardRoleSchema", enumStrings(cards.AllRoles())},
		{"sweetTypeSchema", enumStrings(cards.AllSweetTypes())},
		{"cardVersionSchema", enumStrings(cards.AllVersions())},
	}

	for _, tc := range cases {
		t.Run(tc.constName, func(t *testing.T) {
			theirs := parseZodEnum(t, schema, tc.constName)
			if !equalSets(theirs, tc.ours) {
				t.Errorf("PPLALE-web の %s と値が一致しません\n  upstream: %v\n  ours:     %v\n"+
					"internal/cards/card.go と .claude/skills/pplale-web-data-contract/SKILL.md を更新してください",
					tc.constName, theirs, tc.ours)
			}
		})
	}
}

// TestImageParametersMatchUpstreamScripts reads the resize parameters straight
// out of PPLALE-web's conversion scripts. A silent change there would make
// every image the CMS produces differ from the ones generated locally.
func TestImageParametersMatchUpstreamScripts(t *testing.T) {
	root := upstreamRoot(t)

	optimize := read(t, root, "scripts", "optimize-images.mjs")
	if got := parseConstInt(t, optimize, "CARD_WIDTH"); got != imageconv.CardWidth {
		t.Errorf("CARD_WIDTH: upstream %d, ours %d", got, imageconv.CardWidth)
	}
	if got := parseConstInt(t, optimize, "CARD_QUALITY"); got != imageconv.Quality {
		t.Errorf("CARD_QUALITY: upstream %d, ours %d", got, imageconv.Quality)
	}

	og := read(t, root, "scripts", "generate-og-images.mjs")
	if got := parseConstInt(t, og, "MAX_WIDTH"); got != imageconv.OGWidth {
		t.Errorf("OGP MAX_WIDTH: upstream %d, ours %d", got, imageconv.OGWidth)
	}
}

// TestLiveDatasetsRoundTrip decodes the real data files and writes them back.
// This is the strongest check available: it catches a new field, a changed key
// order and any value the CMS would reject, all at once.
func TestLiveDatasetsRoundTrip(t *testing.T) {
	root := upstreamRoot(t)

	for _, kind := range cards.AllKinds() {
		t.Run(string(kind), func(t *testing.T) {
			ds, err := cards.DatasetFor(kind)
			if err != nil {
				t.Fatalf("DatasetFor: %v", err)
			}
			original := read(t, root, filepath.FromSlash(ds.JSONPath))

			list, err := cards.Decode(ds, []byte(original))
			if err != nil {
				t.Fatalf("PPLALE-web の %s を読み込めません（スキーマが変わった可能性があります）: %v", ds.JSONPath, err)
			}
			if err := cards.ValidateDataset(ds, list); err != nil {
				t.Errorf("PPLALE-web の %s が現在の検証ルールを通りません: %v", ds.JSONPath, err)
			}

			encoded, err := cards.Encode(ds, list)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if string(encoded) != original {
				t.Errorf("%s を書き戻すと現物と一致しません。CMS が出す PR に余計な差分が入ります", ds.JSONPath)
			}
		})
	}
}

// TestImagesReferencedByLiveDataExist mirrors upstream's cards:check: every
// imageUrl must point at a WebP that is actually committed, and the OGP PNG
// mirror must exist too (upstream CI does not check the latter).
func TestImagesReferencedByLiveDataExist(t *testing.T) {
	root := upstreamRoot(t)

	for _, kind := range cards.AllKinds() {
		ds, err := cards.DatasetFor(kind)
		if err != nil {
			t.Fatalf("DatasetFor: %v", err)
		}
		list, err := cards.Decode(ds, []byte(read(t, root, filepath.FromSlash(ds.JSONPath))))
		if err != nil {
			t.Fatalf("Decode %s: %v", ds.JSONPath, err)
		}

		var missingOG int
		for _, c := range list {
			webp := filepath.Join(root, "public", filepath.FromSlash(c.ImageURL))
			if _, err := os.Stat(webp); err != nil {
				t.Errorf("%s: 画像が存在しません: %s", c.ID, c.ImageURL)
				continue
			}
			base := filepath.Base(c.ImageURL)
			png := filepath.Join(root, filepath.FromSlash(ds.OGImagePath(base[:len(base)-len(".webp")]+".png")))
			if _, err := os.Stat(png); err != nil {
				missingOG++
			}
		}
		if missingOG > 0 {
			// 情報として出す。上流CIが検知しないため既存分が欠けていることがある。
			t.Logf("%s: OGP用PNGが %d 件欠けています（upstream で npm run cards:og-images が必要）", ds.JSONPath, missingOG)
		}
	}
}

// TestFixturesMatchLiveData keeps internal/cards/testdata honest. When it
// fails the fixtures are stale: run `mise run sync-testdata`.
func TestFixturesMatchLiveData(t *testing.T) {
	root := upstreamRoot(t)

	for _, kind := range cards.AllKinds() {
		ds, err := cards.DatasetFor(kind)
		if err != nil {
			t.Fatalf("DatasetFor: %v", err)
		}
		name := filepath.Base(ds.JSONPath)
		fixture, err := os.ReadFile(filepath.Join("..", "cards", "testdata", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		if string(fixture) != read(t, root, filepath.FromSlash(ds.JSONPath)) {
			t.Errorf("internal/cards/testdata/%s が PPLALE-web の現物と異なります。`mise run sync-testdata` を実行してください", name)
		}
	}
}

func parseZodEnum(t *testing.T, source, constName string) []string {
	t.Helper()
	// `export const fooSchema = z.enum([...]);` の中身を取り出す
	pattern := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(constName) + `\s*=\s*z\.enum\(\s*\[(.*?)\]`)
	match := pattern.FindStringSubmatch(source)
	if match == nil {
		t.Fatalf("PPLALE-web の schema.ts に %s が見つかりません（定義の書き方が変わった可能性があります）", constName)
	}
	values := regexp.MustCompile(`'([^']*)'|"([^"]*)"`).FindAllStringSubmatch(match[1], -1)
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, v[1]+v[2])
	}
	return out
}

func parseConstInt(t *testing.T, source, name string) int {
	t.Helper()
	match := regexp.MustCompile(`const\s+` + regexp.QuoteMeta(name) + `\s*=\s*(\d+)`).FindStringSubmatch(source)
	if match == nil {
		t.Fatalf("%s の定義が見つかりません（スクリプトの書き方が変わった可能性があります）", name)
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("%s の値を解析できません: %v", name, err)
	}
	return n
}

func enumStrings[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

func equalSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		seen[v]--
		if seen[v] < 0 {
			return false
		}
	}
	return true
}
