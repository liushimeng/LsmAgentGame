// Package wwplayer — agent_config.go: 配置注入方法(难度/记忆/狼队友)及工具函数。
// 从 agent.go 拆分出来,单文件 ≤ 1800 行硬约束(CLAUDE.md §4)。
package wwplayer

import (
	"context"
	"time"
)

// SetMemoryMD 注入持久化记忆(MEMORY.md)。2026-07-20 §131 新增。
// 在 NewWithRoom 之后由 StartAgentsLocked 同步调用一次(2s timeout DB 读,
// 失败仅 log 不阻塞启动);之后整个房间生命周期只读(run.go 注入路径)。
// 空串 = 不注入。
func (a *Agent) SetMemoryMD(md string) {
	a.MemoryMD = md
}

// SetMemoryInjectRunes 设置长期记忆注入的 rune 预算(0 = 用默认常量)。
//
// 2026-08-12 §20260812-04 U4 新增。由 StartAgentsLocked 在 SetMemoryMD 之后
// 按房间难度档位调用,接线 difficulty.MemoryInjectRunes(easy 1500 /
// normal 4000 / hard 6000)—— 该字段此前 4 处赋值 0 处读取。
func (a *Agent) SetMemoryInjectRunes(n int) {
	if n < 0 {
		n = 0
	}
	a.memoryInjectRunes = n
}

// SetDifficultyRoundCap 设置难度档位对内层循环轮次的收紧上限(0 = 不收紧)。
//
// 2026-08-13 §20260813-04 U3 新增。由 StartAgentsLocked 紧邻
// SetMemoryInjectRunes 调用 —— 两者同源于 difficulty.ProfileFor,
// 且都是「difficulty.go 4 处赋值 + agent 侧 0 处读取」的 §130 实例
// (MemoryInjectRunes 在 §20260812-04 U4 已修,本字段是漏掉的那一个)。
//
// 只收紧不放宽:见 maxInnerRoundsFor 的 cap < base 守卫。
func (a *Agent) SetDifficultyRoundCap(n int) {
	a.Lock()
	defer a.Unlock()
	if n < 0 {
		n = 0
	}
	a.difficultyRoundCap = n
}

// DifficultyRoundCap 返回难度轮次收紧上限(0 = 不收紧)。
func (a *Agent) DifficultyRoundCap() int {
	a.Lock()
	defer a.Unlock()
	return a.difficultyRoundCap
}

// SetWolfTeammateSeats 注入"开局互知所有狼队友身份"提示(v20260830-01)。
// 在 NewWithRoom 之后由 StartAgentsLocked 同步调用一次:
//   - len(wolfTeammateSeats)==0 → no-op(等价于禁用本设计的注入路径)
//   - Faction != "wolf"     → no-op(本设计仅作用于狼人)
//   - Agent / Memory 为 nil → no-op(测试构造期防御)
//
// 替换 m.messages[0] 的 identity 文本但保留后续对话,避免 LLM 上下文污染。
// 锁安全:Memory.ReplaceIdentity 内部持 m.mu,本方法不持 r.mu;
// StartAgentsLocked 在持 r.mu 调用本方法是安全的(只动单条 user message,
// 无 LLM / DB / WS IO),符合 §92a 自死锁约束。
func (a *Agent) SetWolfTeammateSeats(wolfTeammateSeats []int) {
	if a == nil || a.Memory == nil {
		return
	}
	if len(wolfTeammateSeats) == 0 {
		return
	}
	if a.Faction != "wolf" {
		return
	}
	a.Memory.ReplaceIdentity(a.Role, a.Faction, a.Win, a.Seat, wolfTeammateSeats)
	// v20260830-01：同步保存到 Agent 结构体,供 wolf_whisper/wolfpack工具挂载判断。
	a.WolfTeammateSeats = wolfTeammateSeats
}
func sleepUntil(ctx context.Context, t time.Time) error {
	d := time.Until(t)
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
// getModelContextBudget 根据模型名称返回建议的字节预算。
// 2026-08-10 §20260810-14 新增:按模型上下文窗口大小动态设置预算。
//
// 预算策略:按模型上下文窗口的 60% 设置(留 40% 给 system + tools + max_tokens 输出)。
// 实测数据:
//   - DouBao: ~128K-256K context → 400 "exceed max message tokens" 在 ~810KB 时触发
//   - Kimi: ~256K context → 类似限制
//   - DeepSeek: ~64K-128K context → 更紧凑
//
// 返回值单位:字节(bytes)。0 表示使用默认值(DefaultMaxPromptBytes)。
func getModelContextBudget(modelKey string) int {
	// 模型上下文窗口估算(基于实测数据和文档)
	// 这些值是保守估计,宁可压缩过早也不要溢出
	modelContextWindows := map[string]int{
		// DouBao: 实测 ~810KB 请求体触发 400,保守设 400KB 预算
		"DouBao-model": 400 * 1024,
		// Kimi: 类似 DouBao,保守设 400KB 预算
		"Kimi-model": 400 * 1024,
		// DeepSeek: 实测上下文窗口较小,保守设 300KB 预算
		"DeepSeek-model": 300 * 1024,
		// GLM: 类似 DeepSeek,保守设 300KB 预算
		"GLM-model": 300 * 1024,
		// MeiTuan: 较大上下文窗口,可设宽松
		"MeiTuan-model": 600 * 1024,
		// MinMax: 中等上下文窗口
		"MinMax-model": 500 * 1024,
		// Qwen: 较大上下文窗口
		"Qwen-model": 600 * 1024,
		// Xiaomi: 中等上下文窗口
		"Xiaomi-model": 500 * 1024,
	}

	if budget, ok := modelContextWindows[modelKey]; ok {
		return budget
	}
	// 未知模型使用默认值
	return DefaultMaxPromptBytes
}
