// Package virtual_city — distribution_test.go: 资金流向聚合单测(2026-09-19 §P2 v2)。
//
// 契约: lag_docs/虚拟城市/已实现/07-P2财富可视化/.../§13.2.3。
// 覆盖目标:
//   - ComputeFlowStat 正确聚合 6 个节点 + 12 类边;
//   - 玩家间互转(CatTrade 等)被跳过;
//   - 空 Ledger → 空 FlowStat(空态兜底,不 panic);
//   - TotalIn / TotalOut 流入流出合计正确;
package virtual_city

import (
	"testing"
)

// TestComputeFlowStat_EmptyLedger 空账目 → 空 FlowStat(空态兜底)。
func TestComputeFlowStat_EmptyLedger(t *testing.T) {
	w := &World{Month: 1, Ledger: &Ledger{}}
	fs := ComputeFlowStat(w, 1)
	if fs == nil {
		t.Fatal("ComputeFlowStat returned nil for empty world")
	}
	if len(fs.Links) != 0 {
		t.Errorf("empty ledger should have no links, got %d", len(fs.Links))
	}
	if fs.TotalInCNY != 0 || fs.TotalOutCNY != 0 {
		t.Errorf("empty ledger totals should be 0, got in=%d out=%d", fs.TotalInCNY, fs.TotalOutCNY)
	}
	if fs.Period != "M1" {
		t.Errorf("Period = %q, want M1", fs.Period)
	}
}

// TestComputeFlowStat_NilWorld nil world → 空 FlowStat。
func TestComputeFlowStat_NilWorld(t *testing.T) {
	fs := ComputeFlowStat(nil, 1)
	if fs == nil {
		t.Fatal("nil world should return empty FlowStat, not nil")
	}
	if len(fs.Links) != 0 || len(fs.Nodes) != 0 {
		t.Errorf("nil world should produce empty flow stat")
	}
}

// TestComputeFlowStat_BasicAggregation 基本聚合:工资 + 消费 + 税 → 3 条边。
func TestComputeFlowStat_BasicAggregation(t *testing.T) {
	w := &World{Month: 5, Ledger: &Ledger{}}
	// 工资:bank → seat:0
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 5, From: EntityBank, To: "seat:0",
		AmountCNY: 10000, Category: CatSalary,
	})
	// 消费:seat:0 → firms
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 5, From: "seat:0", To: EntityFirms,
		AmountCNY: 3000, Category: CatLiving,
	})
	// 个税:seat:0 → gov
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 5, From: "seat:0", To: EntityGov,
		AmountCNY: 1000, Category: CatTax,
	})

	fs := ComputeFlowStat(w, 5)
	if len(fs.Links) != 3 {
		t.Errorf("expected 3 links, got %d (%v)", len(fs.Links), fs.Links)
	}
	if fs.TotalInCNY != 10000 {
		t.Errorf("TotalInCNY = %d, want 10000", fs.TotalInCNY)
	}
	if fs.TotalOutCNY != 4000 { // 3000 + 1000
		t.Errorf("TotalOutCNY = %d, want 4000", fs.TotalOutCNY)
	}

	// 节点应该包含 bank / player / firms / gov 各 1
	wantNodeIDs := map[string]bool{
		FlowNodeBank: false, FlowNodePlayer: false,
		FlowNodeFirms: false, FlowNodeGov: false,
	}
	for _, n := range fs.Nodes {
		if _, ok := wantNodeIDs[n.ID]; ok {
			wantNodeIDs[n.ID] = true
		}
	}
	for id, seen := range wantNodeIDs {
		if !seen {
			t.Errorf("expected node %q in result, but missing", id)
		}
	}
}

// TestComputeFlowStat_SkipPlayerToPlayer 玩家间互转被跳过(走 P2-1 挂单簿)。
func TestComputeFlowStat_SkipPlayerToPlayer(t *testing.T) {
	w := &World{Month: 3, Ledger: &Ledger{}}
	// seat:0 → seat:1 CatTrade(玩家间交易)→ 应跳过
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 3, From: "seat:0", To: "seat:1",
		AmountCNY: 5000, Category: CatTrade,
	})
	// 同时有一笔工资,确保不因跳过而崩溃
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 3, From: EntityBank, To: "seat:0",
		AmountCNY: 8000, Category: CatSalary,
	})
	fs := ComputeFlowStat(w, 3)
	// 只应有 1 条边:bank → player(工资)
	if len(fs.Links) != 1 {
		t.Errorf("expected 1 link (trade skipped), got %d: %+v", len(fs.Links), fs.Links)
	}
	if fs.TotalInCNY != 8000 {
		t.Errorf("TotalInCNY = %d, want 8000 (trade should not affect)", fs.TotalInCNY)
	}
}

// TestComputeFlowStat_MonthFilter 只聚合指定月份;其他月份条目被过滤。
func TestComputeFlowStat_MonthFilter(t *testing.T) {
	w := &World{Month: 5, Ledger: &Ledger{}}
	// 4 月条目(应被过滤)
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 4, From: EntityBank, To: "seat:0",
		AmountCNY: 10000, Category: CatSalary,
	})
	// 5 月条目(应聚合)
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 5, From: EntityBank, To: "seat:0",
		AmountCNY: 6000, Category: CatSalary,
	})
	fs := ComputeFlowStat(w, 5)
	if fs.TotalInCNY != 6000 {
		t.Errorf("month filter failed: TotalInCNY = %d, want 6000", fs.TotalInCNY)
	}
}

// TestComputeFlowStat_EdgeAggregation 同 from→to 多笔累加。
func TestComputeFlowStat_EdgeAggregation(t *testing.T) {
	w := &World{Month: 1, Ledger: &Ledger{}}
	for i := 0; i < 5; i++ {
		w.Ledger.Entries = append(w.Ledger.Entries, Entry{
			Month: 1, From: "seat:0", To: EntityFirms,
			AmountCNY: 200, Category: CatLiving,
		})
	}
	fs := ComputeFlowStat(w, 1)
	if len(fs.Links) != 1 {
		t.Fatalf("expected 1 aggregated edge, got %d", len(fs.Links))
	}
	if fs.Links[0].AmountCNY != 1000 {
		t.Errorf("edge amount = %d, want 1000 (5 × 200)", fs.Links[0].AmountCNY)
	}
}

// TestRecordFlowStat_StoresCache RecordFlowStat 写入 World.LastFlowStat 缓存。
func TestRecordFlowStat_StoresCache(t *testing.T) {
	w := &World{Month: 7, Ledger: &Ledger{}}
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 7, From: EntityBank, To: "seat:0",
		AmountCNY: 5000, Category: CatSalary,
	})
	fs := w.RecordFlowStat()
	if fs == nil {
		t.Fatal("RecordFlowStat returned nil")
	}
	if w.LastFlowStat == nil {
		t.Fatal("LastFlowStat cache not set")
	}
	if w.LastFlowStat.Period != "M7" {
		t.Errorf("LastFlowStat.Period = %q, want M7", w.LastFlowStat.Period)
	}
}

// TestComputeFlowStat_PctConsistency Pct = amount / maxEdge ∈ (0, 1]。
func TestComputeFlowStat_PctConsistency(t *testing.T) {
	w := &World{Month: 1, Ledger: &Ledger{}}
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 1, From: EntityBank, To: "seat:0",
		AmountCNY: 10000, Category: CatSalary,
	})
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 1, From: "seat:0", To: EntityFirms,
		AmountCNY: 3000, Category: CatLiving,
	})
	fs := ComputeFlowStat(w, 1)
	for _, l := range fs.Links {
		if l.Pct < 0 || l.Pct > 1 {
			t.Errorf("link %s→%s Pct=%f out of [0,1]", l.From, l.To, l.Pct)
		}
	}
	// 最大边应为 salary = 1.0
	for _, l := range fs.Links {
		if l.From == FlowNodeBank && l.To == FlowNodePlayer {
			if l.Pct != 1.0 {
				t.Errorf("max edge Pct = %f, want 1.0", l.Pct)
			}
		}
	}
}