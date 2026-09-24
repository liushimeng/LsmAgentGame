// Package vcplayer — prompt_batch20_test.go: 批次20 三块上下文小节渲染
// (文档2 §4.3 / 文档3 A5/B4):有内容注入、无内容不注入。
package vcplayer

import (
	"strings"
	"testing"

	"LsmAgentGame/agent/vctypes"
)

func batch20Ctx() *vctypes.GameContext {
	return &vctypes.GameContext{
		Month: 5, Age: 25,
		Cycle:  vctypes.CycleBrief{Phase: "recovery", LPR: 0.035, CPI: 0.02, MonthsLeft: 10},
		Market: vctypes.MarketBrief{StockIndex: 3.5, GoldPrice: 750, BondRate: 0.032},
		Me:     vctypes.SelfBrief{Cash: 100000, Energy: 5, Network: 3, Cognition: 4, ActionBudget: 3},
	}
}

// TestUserPrompt_Batch20Sections 三小节渲染 + 空字段零注入。
func TestUserPrompt_Batch20Sections(t *testing.T) {
	ctx := batch20Ctx()
	out := UserPrompt(ctx, "")
	for _, junk := range []string{"副业定价市场", "市政选举", "T+1"} {
		if strings.Contains(out, junk) {
			t.Errorf("empty briefs must not inject %q", junk)
		}
	}
	ctx.SideMarketBrief = "你的副业:跑腿配送 高价档,客群份额 40%,上月实收 ¥3250。"
	ctx.ElectionBrief = "本城已启动市长选举:你是现任市长,下届选举在第 49 月。"
	ctx.MicroPriceBrief = "股票现价 买3.50/卖3.50(价差 4bp)。T+1 冻结 600 份。"
	out = UserPrompt(ctx, "")
	if !strings.Contains(out, "■ 副业定价市场") || !strings.Contains(out, "客群份额 40%") {
		t.Errorf("side market section missing: %q", out)
	}
	if !strings.Contains(out, "■ 市政选举") || !strings.Contains(out, "现任市长") {
		t.Errorf("election section missing")
	}
	// 微观行落在「■ 市场行情」小节内(B4 市场小节追加)。
	idx := strings.Index(out, "■ 市场行情")
	micro := strings.Index(out, "T+1 冻结 600 份")
	if idx < 0 || micro < 0 || micro < idx || micro > idx+600 {
		t.Errorf("micro brief must sit inside 市场行情 section (idx=%d micro=%d)", idx, micro)
	}
	// 定价策略提示(拟人化不破坏:无硬编码指令,仅一句话引导)。
	if strings.Contains(out, "你必须") {
		t.Error("batch20 injections must stay advisory, not imperative")
	}
}
