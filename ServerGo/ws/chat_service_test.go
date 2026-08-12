package ws

import (
	"strings"
	"testing"
)

// TestTruncateChatText verifies the varchar(1024) cap helper used by the bot
// chat paths (SendFromBot / SendInterjectFromBot / SendFromJudge). Without it,
// LLM output that mystery-mask rewrites beyond the column width triggers
// MySQL Error 1406 and the utterance is silently dropped.
func TestTruncateChatText(t *testing.T) {
	t.Run("short text unchanged", func(t *testing.T) {
		in := "你好，世界"
		if got := truncateChatText(in); got != in {
			t.Fatalf("short text mutated: got %q", got)
		}
	})

	t.Run("exact boundary unchanged", func(t *testing.T) {
		in := strings.Repeat("a", maxChatTextLen)
		if got := truncateChatText(in); got != in {
			t.Fatalf("boundary text mutated: len %d", len(got))
		}
	})

	t.Run("oversize truncated to rune boundary", func(t *testing.T) {
		// 6000 Chinese chars, mirroring the R179 mystery-mask output that blew
		// the varchar column.
		in := strings.Repeat("狼", 6000)
		got := truncateChatText(in)
		if len([]rune(got)) != maxChatTextLen {
			t.Fatalf("truncated len = %d, want %d", len([]rune(got)), maxChatTextLen)
		}
		if got != strings.Repeat("狼", maxChatTextLen) {
			t.Fatalf("truncated content mismatch")
		}
	})

	t.Run("multibyte rune safe", func(t *testing.T) {
		// Mixed ASCII + CJK: ensure we cut on rune boundaries, never mid-rune.
		in := strings.Repeat("a中", maxChatTextLen) // 2 runes each pair
		got := truncateChatText(in)
		if len([]rune(got)) != maxChatTextLen {
			t.Fatalf("truncated len = %d, want %d", len([]rune(got)), maxChatTextLen)
		}
	})
}
