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

// §20260817-04 U3 — 私聊目标账号格式化测试。
//
// 验证 lookupSeatAccount 在早期返回分支(无需 DB,纯函数分支):
//   - 空 userID → 返回 ""(WhisperFromBot 调用方在 toUserID="" 时根本不会走到这里,
//     但作为防御性兜底必须有)
//   - 空 roomID → 不调 lookupAccount,直接传 userID(lookupAccount 需要 DB,
//     跳过以避免 panic;后续真实 DB 测试覆盖)
//
// 走 DB 的成功/失败路径(werewolf + 找到 seat → "Bot N号"、非 werewolf → lookupAccount)
// 在集成测试或手工测试中覆盖;这里只覆盖不需要 DB 的纯分支。
func TestLookupSeatAccount_Fallbacks(t *testing.T) {
	t.Run("empty userID returns empty string", func(t *testing.T) {
		s := &ChatService{} // nil db
		if got := s.lookupSeatAccount("room-1", ""); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
}
