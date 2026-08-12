// R186 修复 — failure/cooldown/quarantine 状态机白盒测试 (2026-07-22)
//
// 背景:13 人局 Agent 房间 "已禁用 · N 连失败" 频发的根因之一是
// run.go:746-762 的失败计数器 mutation 与 ResetConsecutiveFailures /
// recordTranscript / SetQuarantined 等持锁路径形成 data race,Go race
// detector 会 flag。同时存在一个语义 bug:transient (timeout/network)
// 错误不滑动 lastFailureTime,导致一个 bot 持续 timeout 5 分钟后再撞
// 403 时 cooldown 早已过期,单次 403 立即计数 (cf=1)。
//
// 本测试用包内白盒测试 (package agent) 直接驱动 Agent.RecordFailure,
// 验证修复后的 4 项关键不变式。
package wwplayer

import (
	"sync"
	"testing"
	"time"
)

// TestR186_001_TransientFailureSlidesCooldownWindow 验证 R186 改 A:
// transient 错误只 bump lastFailureTime 滑动 cooldown 窗口,不递增
// consecutiveFailures。但持续 timeout 后撞 403 应被冷却吸收(而非
// 立即计数)。
func TestR186_001_TransientFailureSlidesCooldownWindow(t *testing.T) {
	a := &Agent{}

	// T0: 第一次 5xx 失败 → cf=1, lft=T0
	t0 := time.Now()
	cf, inCD := a.RecordFailure(t0, false, 60*time.Second)
	if cf != 1 || inCD {
		t.Fatalf("首次非 transient 失败: 期望 cf=1 inCD=false, got cf=%d inCD=%v", cf, inCD)
	}

	// T0+10s: transient timeout → 不递增,但 lft 被 bump
	t1 := t0.Add(10 * time.Second)
	cf, inCD = a.RecordFailure(t1, true, 60*time.Second)
	if cf != 1 || !inCD {
		t.Fatalf("transient 错误: 期望 cf=1 inCD=true (冷却被吸收), got cf=%d inCD=%v", cf, inCD)
	}

	// T0+20s: 再一次 transient timeout
	t2 := t0.Add(20 * time.Second)
	cf, inCD = a.RecordFailure(t2, true, 60*time.Second)
	if cf != 1 || !inCD {
		t.Fatalf("连续 transient: 期望 cf=1 inCD=true, got cf=%d inCD=%v", cf, inCD)
	}

	// T0+30s: 撞 403 (non-transient) → 距上次失败 t2=20s,仍在 cooldown → inCD=true,cf 不变
	t3 := t0.Add(30 * time.Second)
	cf, inCD = a.RecordFailure(t3, false, 60*time.Second)
	if cf != 1 || !inCD {
		t.Fatalf("transient 后非 transient (30s 内): 期望 cf=1 inCD=true, got cf=%d inCD=%v", cf, inCD)
	}

	// T0+90s: 撞 403 (non-transient) → 距上次失败 t3=90s,超出 cooldown → cf=2
	t4 := t0.Add(90 * time.Second)
	cf, inCD = a.RecordFailure(t4, false, 60*time.Second)
	if cf != 2 || inCD {
		t.Fatalf("超出 cooldown 后: 期望 cf=2 inCD=false, got cf=%d inCD=%v", cf, inCD)
	}
}

// TestR186_002_NonTransientCooldownHold 验证原 cooldown 行为不变:
// 非 transient 错误在 cooldown 窗口内既不递增也不更新 lft。
func TestR186_002_NonTransientCooldownHold(t *testing.T) {
	a := &Agent{}

	t0 := time.Now()
	a.RecordFailure(t0, false, 60*time.Second) // cf=1, lft=t0

	// 30s 后再次非 transient 失败 → 在 cooldown 内 → cf=1
	cf, inCD := a.RecordFailure(t0.Add(30*time.Second), false, 60*time.Second)
	if cf != 1 || !inCD {
		t.Fatalf("cooldown 内非 transient: 期望 cf=1 inCD=true, got cf=%d inCD=%v", cf, inCD)
	}

	// 又一次 30s 后 → 距上次失败 30s (lft 没变)→ 仍在 cooldown
	cf, inCD = a.RecordFailure(t0.Add(59*time.Second), false, 60*time.Second)
	if cf != 1 || !inCD {
		t.Fatalf("cooldown 内 (59s): 期望 cf=1 inCD=true, got cf=%d inCD=%v", cf, inCD)
	}

	// 60s 整 → 严格小于 60s 才算 inCD,边界处递增 + lft 跳到 t=60s
	cf, inCD = a.RecordFailure(t0.Add(60*time.Second), false, 60*time.Second)
	if cf != 2 || inCD {
		t.Fatalf("cooldown 边界 (60s 整,严格不小于): 期望 cf=2 inCD=false, got cf=%d inCD=%v", cf, inCD)
	}

	// 61s (距新 lft=60s 仅 1s)→ 重新进入 cooldown,cf 不变
	cf, inCD = a.RecordFailure(t0.Add(61*time.Second), false, 60*time.Second)
	if cf != 2 || !inCD {
		t.Fatalf("刚递增后再 1s: 期望 cf=2 inCD=true (新 cooldown 立即生效), got cf=%d inCD=%v", cf, inCD)
	}

	// 真正超过 60s 距新 lft → cf=3
	cf, inCD = a.RecordFailure(t0.Add(125*time.Second), false, 60*time.Second)
	if cf != 3 || inCD {
		t.Fatalf("新 cooldown 外: 期望 cf=3 inCD=false, got cf=%d inCD=%v", cf, inCD)
	}
}

// TestR186_003_RecordFailureRaceSafe 验证 RecordFailure /
// FailureSnapshot / ConsecutiveFailures 与 ResetConsecutiveFailures
// 之间不会产生 data race (Go race detector flag)。本测试只验证
// 接口形态正确 + 串行调用返回一致值;真正的并发 race 由
// `go test -race` 在 CI 兜住。
func TestR186_003_RecordFailureRaceSafe(t *testing.T) {
	a := &Agent{}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now := time.Now().Add(time.Duration(i) * time.Millisecond)
			a.RecordFailure(now, i%2 == 0, 60*time.Second)
			a.ConsecutiveFailures()
			a.FailureSnapshot()
		}(i)
	}
	wg.Wait()

	cf, q := a.FailureSnapshot()
	if cf < 0 {
		t.Errorf("consecutiveFailures 不应为负, got %d", cf)
	}
	if q {
		t.Errorf("本测试不应触发 quarantine")
	}
}

// TestR186_004_FailureSnapshotAtomic 验证 FailureSnapshot 返回的值
// 与单独 ConsecutiveFailures + IsQuarantined 在串行下一致(防止
// 后续重构打破"原子读"语义)。
func TestR186_004_FailureSnapshotAtomic(t *testing.T) {
	a := &Agent{}
	a.RecordFailure(time.Now(), false, 60*time.Second)

	cfSnap, qSnap := a.FailureSnapshot()
	cfDirect := a.ConsecutiveFailures()
	qDirect := a.IsQuarantined()

	if cfSnap != cfDirect {
		t.Errorf("FailureSnapshot cf=%d 与 ConsecutiveFailures=%d 不一致", cfSnap, cfDirect)
	}
	if qSnap != qDirect {
		t.Errorf("FailureSnapshot quarantined=%v 与 IsQuarantined=%v 不一致", qSnap, qDirect)
	}
}
