//go:build balance_gate

// Package virtual_city — montecarlo_gate_test.go: 批次72 T11(T-MC7)
// 1000 局门禁(build tag balance_gate,默认测试集不编译)。
//
// 运行:go test -tags balance_gate ./game/virtual_city/ -run TestMCGate1000 -count=1
//
// 裁决纪律(方案 §10.3 / 任务书):
//   - G12 / I1 / I2 / I4 / I8 任一 failure ⇒ t.Errorf(框架自身正确性,
//     与经济数值无关,不允许带病合入);
//   - 经济阈值门禁首跑允许以「首跑基线」归档(t.Logf,数值裁决归批次 73),
//     失败时输出五元组失败样本(seed/month/seat/cash/net_worth)。
package virtual_city

import (
	"testing"
	"time"
)

// mcGatePerStrategy T11 每策略局数(200×5 = 1000)。
const mcGatePerStrategy = 200

func TestMCGate1000(t *testing.T) {
	total := mcGatePerStrategy * len(mcStrategyOrder)
	start := time.Now()
	stats := mcRunBatch(t, total)
	elapsed := time.Since(start)
	t.Logf("T11 gate:%d 局(%d×5 策略)总耗时 %v(单局均值 %.1fms)",
		total, mcGatePerStrategy, elapsed, float64(elapsed.Milliseconds())/float64(total))

	if len(stats) != total {
		t.Fatalf("批量结果数 = %d,期望 %d", len(stats), total)
	}

	// 框架自身正确性:I1/I2/I4/I8 全月全局全策略 0 failure。
	fatalIDs := []string{InvariantI1, InvariantI2, InvariantI4, InvariantI8}
	for i := range stats {
		for _, id := range fatalIDs {
			if n := stats[i].InvariantFailsByID[id]; n > 0 {
				t.Errorf("不变量 %s 违例:seed=%d strategy=%s n=%d(框架自身正确性问题)",
					id, stats[i].Seed, stats[i].Strategy, n)
			}
		}
	}

	gates := MCEvaluateGates(stats)
	for _, g := range gates {
		if g.Passed {
			t.Logf("[gate1000] %-3s %-14s PASS  actual=%.4f threshold=%.4f %s",
				g.ID, g.Name, g.Actual, g.Threshold, g.Detail)
			continue
		}
		if g.ID == GateG12 {
			// 不变量类:硬失败(同上面的逐 id 检查,此处兜底聚合口径)。
			t.Errorf("[gate1000] G12 不变量门禁失败:%+v", g)
			continue
		}
		// 经济阈值:首跑基线归档,数值裁决归批次 73。
		t.Logf("[gate1000] %-3s %-14s FAIL  actual=%.4f threshold=%.4f %s",
			g.ID, g.Name, g.Actual, g.Threshold, g.Detail)
	}

	// 失败样本输出:每条未通过门禁关联的出局五元组(最多 20 条)。
	dumped := 0
	for i := range stats {
		for _, fs := range stats[i].EliminationSamples {
			if dumped >= 20 {
				return
			}
			t.Logf("失败样本:seed=%d strategy=%s month=%d seat=%d cash=%d net_worth=%d",
				fs.Seed, stats[i].Strategy, fs.Month, fs.Seat, fs.Cash, fs.NetWorth)
			dumped++
		}
	}
}
