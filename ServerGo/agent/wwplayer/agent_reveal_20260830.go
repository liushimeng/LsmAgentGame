// Package wwplayer — agent_reveal_20260830.go: §20260830-01「死亡亮身份」
// Agent 侧开关同步方法。
//
// 单独成文件的原因(§4):agent.go 在本设计前已是 2100+ 行的历史超限文件,
// 新逻辑按规约拆到同 package 新文件,仅 struct 字段声明留在 agent.go。
package wwplayer

// SetRevealRoleOnDeath §20260830-01 §6.2 — 同步本局「死亡亮身份」开关到玩家 Agent。
//
// 调用方:buildAgentContextLocked 每次 wake 幂等调用(值未变即 no-op,零开销);
// 开关在房间创建时一次性写入 GameState,整局不变 → 首次 wake 后字节稳定
// (§20260813-05 U5 provider cache 命中不受影响)。
//
// 值变化时同步重算 systemPromptBytes 冻结快照 —— BuildSystemPrompt 的 §135
// 规则段按本开关双模式输出,快照与 run.go 请求路径同源,保证 invariant I11
// (req.System 字节数 == 冻结快照)不违反。
//
// 锁安全:仅持 a.mu 读写 Agent 自身字段,不触碰 r.mu / Memory / LLM;
// 与 handleEvent 请求路径(a.mu → 无外部锁)无反向依赖,§92a 安全。
// 调用序 r.mu → a.mu 与既有 AvgLLMLatencyMs(room_agent_context.go 持 r.mu 调用)同序。
func (a *Agent) SetRevealRoleOnDeath(enabled bool) {
	if a == nil {
		return
	}
	a.Lock()
	defer a.Unlock()
	if a.revealRoleOnDeath == enabled {
		return
	}
	a.revealRoleOnDeath = enabled
	a.systemPromptBytes = BuildSystemPromptBytes(
		a.SelfPortraitText, a.Personality, a.PersonalityPresetKey, a.DifficultyDirective, enabled)
}

// RevealRoleOnDeath 返回本局「死亡亮身份」开关(§20260830-01)。
// run.go 两处 BuildSystemPrompt 调用点透传,与冻结快照同源。
func (a *Agent) RevealRoleOnDeath() bool {
	if a == nil {
		return false
	}
	a.Lock()
	defer a.Unlock()
	return a.revealRoleOnDeath
}
