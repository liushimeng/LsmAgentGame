package util

import (
	"strings"
	"unicode"
)

// SanitizeModelKey normalizes an LLM model key at every trust boundary
// (admin CRUD, registry seed/load, agent-seat validation, config pools).
//
// Background (R187-2): a provider row whose `model` column was pasted into
// the admin UI with a zero-width non-joiner (U+200C) between "Tencent" and
// "-model" became unaddressable — Registry.IsAvailable("Tencent-model")
// (plain ASCII) returned false and room creation with that model_key was
// rejected even though the key *looked* identical in every UI/log line.
//
// Normalization is deliberately minimal and predictable:
//   - strings.TrimSpace on both ends
//   - drop every Unicode format-category rune (unicode.Cf): U+200B ZWSP,
//     U+200C ZWNJ, U+200D ZWJ, U+2060 WORD JOINER, U+FEFF BOM, U+00AD SOFT
//     HYPHEN, plus any future Cf rune.
//
// Interior whitespace is NOT collapsed — model keys are not expected to
// contain spaces at all; keeping them visible makes typos fail loudly
// instead of silently mutating the key.
func SanitizeModelKey(s string) string {
	s = strings.TrimSpace(s)
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1 // drop
		}
		return r
	}, s)
}
