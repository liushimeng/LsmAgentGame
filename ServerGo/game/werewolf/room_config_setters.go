package werewolf

// room_config_setters.go — §4 单文件 ≤1800 行治理:room.go 房间级配置 setter
// 整段搬移(2026-08-30 §20260830-01 同批纯代码搬移;零逻辑改动,函数体逐字节保留)。
// 新增的 §20260830-01 SetRevealRoleOnDeath / setRevealRoleOnDeathLocked 落在
// death_reveal.go(本设计新逻辑集中地),与本文件既有 setter 同款 §92a 成对模式。

import "LsmAgentGame/agent/wwplayer"

// SetDeathRevealDelayMin 设置 §20260810-12 D2 死者身份「终局延时揭晓」配置(0/5/15)。
// 由 RoomService.CreateRoomWithAgents 在房间创建时一次性调用;锁内变体,公开
// 入口包锁委托。非法值自动归一化为 0(零回归)。仅影响前端 UI 层,SettlementModal
// 倒计时;§135 RolePubliclyRevealed 单点判定不受影响。
func (r *WerewolfRoom) SetDeathRevealDelayMin(min int) {
	r.setDeathRevealDelayMinLocked(min)
}

// SetAgentDifficulty 设置 §20260811-09 U2 Agent 难度分级配置(easy/normal/hard/hell)。
// 由 RoomService.CreateRoomWithAgents 在房间创建时一次性调用;锁内变体,公开
// 入口包锁委托。非法 / 空值自动归一化为 normal(零回归)。
func (r *WerewolfRoom) SetAgentDifficulty(difficulty string) {
	r.setAgentDifficultyLocked(difficulty)
}

// setAgentDifficultyLocked 锁内变体(§92a)。调用方必须已持 r.mu。
func (r *WerewolfRoom) setAgentDifficultyLocked(difficulty string) {
	r.agentDifficulty = string(NormalizeAgentDifficulty(difficulty))
}

// DifficultyCoinMultiplierX10Locked 返回当前房间难度档位对应的胜方金币倍率(×10)。
// 必须在持有 r.mu 时调用(§92a);由 EmitGameOver 内 settleBots/settleHumans
// 读取作为结算因子。败方扣款不受倍率影响 —— 仅胜方收益被放大。
func (r *WerewolfRoom) DifficultyCoinMultiplierX10Locked() int64 {
	return int64(ProfileFor(AgentDifficulty(r.agentDifficulty)).CoinMultiplierX10)
}

// SetDeathRevealDelayMinLocked 锁内变体(§92a)。调用方必须已持 r.mu。
func (r *WerewolfRoom) setDeathRevealDelayMinLocked(min int) {
	switch min {
	case 0, 5, 15:
		r.deathRevealDelayMin = min
	default:
		r.deathRevealDelayMin = 0
	}
}

// §20260811-04 U2 — 人设倾向参数 setter。
// 由 RoomService.CreateRoomWithAgents 在房间创建时一次性调用。
// mode/presetKey 非枚举值自动归一化为默认(uniform + logical);
// customVec 仅 mode="custom" 时使用,其他模式忽略。
func (r *WerewolfRoom) SetAgentPersonality(mode, presetKey string, customVec *wwplayer.PersonalityVector) {
	r.setAgentPersonalityLocked(mode, presetKey, customVec)
}

// setAgentPersonalityLocked 是 SetAgentPersonality 的锁内变体(§92a)。
func (r *WerewolfRoom) setAgentPersonalityLocked(mode, presetKey string, customVec *wwplayer.PersonalityVector) {
	switch mode {
	case PersonalityModeUniform, PersonalityModeRandom, PersonalityModeCustom:
		r.personalityMode = mode
	default:
		r.personalityMode = PersonalityModeUniform
	}
	switch presetKey {
	case "logical", "emotional", "aggressive", "cautious", "showman":
		r.personalityPresetKey = presetKey
	default:
		r.personalityPresetKey = "logical"
	}
	if r.personalityMode == PersonalityModeCustom && customVec != nil {
		clamped := customVec.Clamp()
		r.personalityCustomVec = &clamped
	} else {
		r.personalityCustomVec = nil
	}
}

// PersonalitySnapshotLocked 返回房间级人设配置(供 view.go / 前端展示)。
// 返回值是 (mode, preset_key, custom_vector 副本);调用方不持有 r.mu。
func (r *WerewolfRoom) PersonalitySnapshotLocked() (string, string, *wwplayer.PersonalityVector) {
	if r == nil {
		return PersonalityModeUniform, "logical", nil
	}
	if r.personalityMode == "" {
		return PersonalityModeUniform, "logical", nil
	}
	if r.personalityMode == PersonalityModeCustom && r.personalityCustomVec != nil {
		vecCopy := *r.personalityCustomVec
		return r.personalityMode, r.personalityPresetKey, &vecCopy
	}
	return r.personalityMode, r.personalityPresetKey, nil
}
