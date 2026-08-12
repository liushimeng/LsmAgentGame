package wwplayer

// 2026-08-10 §20260810-08 — 信息账本二期 L2-12：KnowledgeDigestBlock 渲染。
// 设计文档：docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260810-08.md

import (
	"strings"
	"testing"

	"LsmWebGame/agent/wwtypes"
)

func TestKnowledgeDigestBlock_L2_12_NilOrEmpty(t *testing.T) {
	if got := KnowledgeDigestBlock(nil); got != "" {
		t.Fatalf("L2-12 nil ctx 应返回空串: %q", got)
	}
	if got := KnowledgeDigestBlock(&wwtypes.GameContext{}); got != "" {
		t.Fatalf("L2-12 空 digest 应返回空串: %q", got)
	}
	ctx := &wwtypes.GameContext{KnowledgeDigest: &wwtypes.KnowledgeDigest{}}
	if got := KnowledgeDigestBlock(ctx); got != "" {
		t.Fatalf("L2-12 0 entries 应返回空串: %q", got)
	}
}

func TestKnowledgeDigestBlock_L2_12_PrivateWarning(t *testing.T) {
	ctx := &wwtypes.GameContext{
		Round: 3,
		KnowledgeDigest: &wwtypes.KnowledgeDigest{
			Seat: 0, TotalKnown: 2, TotalInRoom: 9,
			Entries: []wwtypes.KnowledgeDigestEntry{
				{Source: "wolf_pack", Count: 2, Highlights: []string{"今晚关注 5号"}},
			},
		},
	}
	got := KnowledgeDigestBlock(ctx)
	if !strings.Contains(got, "🗂") || !strings.Contains(got, "私密来源") {
		t.Fatalf("L2-12 缺少私密警告: %s", got)
	}
	if !strings.Contains(got, "狼队密语") {
		t.Fatalf("L2-12 缺少狼队密语条目: %s", got)
	}
}
