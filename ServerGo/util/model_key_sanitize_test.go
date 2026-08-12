package util

import "testing"

// Invisible runes are written as \u escapes: Go's scanner rejects a literal
// BOM (U+FEFF) anywhere except the very first bytes of a source file, and
// literal zero-width characters in source are exactly the maintainability
// hazard this sanitizer exists to defend against.
const (
	zwsp       = "\u200b" // zero-width space
	zwnj       = "\u200c" // zero-width non-joiner (R187-2 root cause)
	zwj        = "\u200d" // zero-width joiner
	wordJoiner = "\u2060" // word joiner
	bom        = "\ufeff" // byte order mark
	softHyphen = "\u00ad" // soft hyphen
)

func TestSanitizeModelKey_ASCIIPassThrough(t *testing.T) {
	in := "Tencent-model"
	if got := SanitizeModelKey(in); got != in {
		t.Fatalf("ASCII key mutated: got %q want %q", got, in)
	}
}

func TestSanitizeModelKey_StripsZeroWidthAndFormatRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ZWNJ (R187-2 root cause)", "Tencent" + zwnj + "-model", "Tencent-model"},
		{"ZWSP", "Tencent" + zwsp + "-model", "Tencent-model"},
		{"ZWJ", "Tencent" + zwj + "-model", "Tencent-model"},
		{"word joiner", "Tencent" + wordJoiner + "-model", "Tencent-model"},
		{"BOM", bom + "Tencent-model", "Tencent-model"},
		{"soft hyphen", "Tencent" + softHyphen + "-model", "Tencent-model"},
		{"multiple mixed", zwnj + "Tencent" + zwsp + "-m" + zwj + "odel" + bom, "Tencent-model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeModelKey(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestSanitizeModelKey_TrimsEnds(t *testing.T) {
	if got := SanitizeModelKey("  Tencent-model\t\n "); got != "Tencent-model" {
		t.Fatalf("trim failed: got %q", got)
	}
}

func TestSanitizeModelKey_KeepsInteriorWhitespace(t *testing.T) {
	// Deliberately NOT collapsed — interior spaces make a key invalid and
	// should fail loudly downstream rather than being silently rewritten.
	in := "Tencent model"
	if got := SanitizeModelKey(in); got != in {
		t.Fatalf("interior whitespace should be preserved: got %q want %q", got, in)
	}
}

func TestSanitizeModelKey_Idempotent(t *testing.T) {
	in := " " + bom + "Tencent" + zwnj + "-model" + zwsp + " "
	once := SanitizeModelKey(in)
	twice := SanitizeModelKey(once)
	if once != twice {
		t.Fatalf("not idempotent: once=%q twice=%q", once, twice)
	}
	if once != "Tencent-model" {
		t.Fatalf("unexpected result: %q", once)
	}
}

func TestSanitizeModelKey_EmptyAndOnlyFormatRunes(t *testing.T) {
	if got := SanitizeModelKey(""); got != "" {
		t.Fatalf("empty input: got %q", got)
	}
	if got := SanitizeModelKey(zwnj + zwsp + zwj); got != "" {
		t.Fatalf("format-only input should become empty: got %q", got)
	}
}
