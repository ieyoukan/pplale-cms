package contract

import "testing"

// These guard the readers themselves: if a regex silently stopped matching,
// every contract check above would pass for the wrong reason.
func TestParseZodEnum(t *testing.T) {
	source := `
export const cardTypeSchema = z.enum(['yojo', 'sweet', 'playable']);
export const sweetTypeSchema = z.enum([
  '',
  'animal_soda',
  "cafe",
]);
`
	if got := parseZodEnum(t, source, "cardTypeSchema"); !equalSets(got, []string{"yojo", "sweet", "playable"}) {
		t.Errorf("cardTypeSchema = %v", got)
	}
	if got := parseZodEnum(t, source, "sweetTypeSchema"); !equalSets(got, []string{"", "animal_soda", "cafe"}) {
		t.Errorf("sweetTypeSchema = %v", got)
	}
}

func TestParseConstInt(t *testing.T) {
	source := "const CARD_WIDTH = 800; // comment\nconst CARD_QUALITY = 80;\n"
	if got := parseConstInt(t, source, "CARD_WIDTH"); got != 800 {
		t.Errorf("CARD_WIDTH = %d", got)
	}
	if got := parseConstInt(t, source, "CARD_QUALITY"); got != 80 {
		t.Errorf("CARD_QUALITY = %d", got)
	}
}

func TestEqualSetsDetectsDrift(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{[]string{"a", "b"}, []string{"b", "a"}, true},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
		{[]string{"a", "a"}, []string{"a", "b"}, false},
		{nil, nil, true},
	}
	for _, tc := range cases {
		if got := equalSets(tc.a, tc.b); got != tc.want {
			t.Errorf("equalSets(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
