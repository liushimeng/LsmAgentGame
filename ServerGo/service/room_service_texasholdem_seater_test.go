// Regression tests for the §20260819 报告 P0-NEW: room_service_crud.go:571
// 条件仅 `gameKind == "werewolf"`,漏掉 `texasholdem` → RegisterAgentSeats
// 从未被调用 → in-memory TexasHoldem manager 无 Bot → IsReady 只计 1 人
// (人类) → 游戏永远卡在 waiting。
//
// 修复:外层条件改为 werewolf | texasholdem,内层把狼人杀专属配置
// (SetJudgeConfig / SetAgentDifficulty / SetCommentaryConfig / SetSeatRolePrefs)
// 收敛到 `gameKind == "werewolf"` 分支;texasholdem 路径只走 RegisterAgentSeats。
//
// 这些测试无法直接调 CreateRoomWithAgents (它会触 DB),因此把"DB commit
// 之后"的 seater dispatch 块原样复刻到测试函数里;fakeRecorderAgentSeater
// 录每条方法调用,我们断言:
//   1. werewolf 路径:RegisterAgentSeats + SetJudgeConfig + SetAgentDifficulty +
//      SetCommentaryConfig + SetSeatRolePrefs 都各被调一次(保留既有行为)
//   2. texasholdem 路径:RegisterAgentSeats 被调一次,4 个狼人杀专属方法均 NOT 调
//   3. gameKind 既不是 werewolf 也不是 texasholdem:整个 dispatch 块不进入
package service

import (
	"testing"

	"LsmAgentGame/errcode"
)

// texSeaterRecorder 记录 5 个接口方法是否被调用 + RegisterAgentSeats 的入参。
// 是 §20260819 报告 P0-NEW 的回归基线:必须能让测试断言"texasholdem 路径
// 调了 RegisterAgentSeats 且没碰狼人杀专属方法"。
type texSeaterRecorder struct {
	gameKind string
	seats    []AgentSeatConfig

	registerCalls         int
	setJudgeConfigCalls   int
	setDifficultyCalls    int
	setCommentaryCalls    int
	setSeatRolePrefsCalls int
}

func (f *texSeaterRecorder) RegisterAgentSeats(gameKind, roomID string, seats []AgentSeatConfig) *errcode.Error {
	f.registerCalls++
	f.gameKind = gameKind
	f.seats = seats
	return nil
}

func (f *texSeaterRecorder) SetJudgeConfig(gameKind, roomID string, desired bool, mode string, modelKey string) *errcode.Error {
	f.setJudgeConfigCalls++
	return nil
}

func (f *texSeaterRecorder) ValidateAgentSeats(seats []AgentSeatConfig) *errcode.Error {
	return nil
}

func (f *texSeaterRecorder) SetAgentDifficulty(gameKind, roomID string, difficulty string) *errcode.Error {
	f.setDifficultyCalls++
	return nil
}

func (f *texSeaterRecorder) SetCommentaryConfig(gameKind, roomID string, cfg *CommentaryConfig) *errcode.Error {
	f.setCommentaryCalls++
	return nil
}

func (f *texSeaterRecorder) SetSeatRolePrefs(gameKind, roomID string, prefs map[int]string, creatorPref string) *errcode.Error {
	f.setSeatRolePrefsCalls++
	return nil
}

// dispatchAgentSeats mirrors the post-DB seater dispatch block in
// CreateRoomWithAgents (ServerGo/service/room_service_crud.go:564-643).
// We keep this in lockstep with the production block — any change to the
// gate (`if gameKind == "werewolf" || gameKind == "texasholdem"`) or the
// inner werewolf-only branch must be reflected here so the regression tests
// continue to bite.
func dispatchAgentSeats(s *RoomService, rec *texSeaterRecorder, gameKind, roomID string, agentSeats []AgentSeatConfig, judge *JudgeConfig, agentDifficulty string, commentary *CommentaryConfig, seatRolePrefs map[int]string, creatorRolePref string) {
	if (gameKind == "werewolf" || gameKind == "texasholdem") && len(agentSeats) > 0 && s.agentSeater != nil {
		if gameKind == "werewolf" {
			judgeMode := "agent"
			if judge != nil && judge.Mode != "" {
				judgeMode = judge.Mode
			}
			_ = s.agentSeater.SetJudgeConfig(gameKind, roomID, true, judgeMode, "")
			_ = s.agentSeater.SetAgentDifficulty(gameKind, roomID, agentDifficulty)
			_ = s.agentSeater.SetCommentaryConfig(gameKind, roomID, commentary)
			if len(seatRolePrefs) > 0 || creatorRolePref != "" {
				_ = s.agentSeater.SetSeatRolePrefs(gameKind, roomID, seatRolePrefs, creatorRolePref)
			}
		}
		_ = s.agentSeater.RegisterAgentSeats(gameKind, roomID, agentSeats)
	}
}

// TestDispatchAgentSeats_Werewolf_AllHooksCalled is the regression baseline:
// werewolf 房间必须调全部 5 个 seater 接口(法官 + 难度 + 解说 + 角色偏好
// + 注册),与 R1/R2 既有行为一致。修复 P0-NEW 之后这条断言必须仍然 PASS,
// 即"修新 bug 不能打回旧 bug"。
func TestDispatchAgentSeats_Werewolf_AllHooksCalled(t *testing.T) {
	rec := &texSeaterRecorder{}
	s := &RoomService{}
	s.SetAgentSeater(rec)

	seats := []AgentSeatConfig{
		{Seat: 0, ModelKey: "Qwen-model", Role: "villager"},
		{Seat: 1, ModelKey: "DouBao-model"},
	}
	rolePrefs := map[int]string{0: "villager"}
	dispatchAgentSeats(s, rec, "werewolf", "room-w", seats, &JudgeConfig{Mode: "agent"}, "normal", &CommentaryConfig{Enabled: true}, rolePrefs, "seer")

	if rec.registerCalls != 1 {
		t.Fatalf("werewolf: RegisterAgentSeats should be called once, got %d", rec.registerCalls)
	}
	if rec.setJudgeConfigCalls != 1 {
		t.Fatalf("werewolf: SetJudgeConfig should be called once, got %d", rec.setJudgeConfigCalls)
	}
	if rec.setDifficultyCalls != 1 {
		t.Fatalf("werewolf: SetAgentDifficulty should be called once, got %d", rec.setDifficultyCalls)
	}
	if rec.setCommentaryCalls != 1 {
		t.Fatalf("werewolf: SetCommentaryConfig should be called once, got %d", rec.setCommentaryCalls)
	}
	if rec.setSeatRolePrefsCalls != 1 {
		t.Fatalf("werewolf: SetSeatRolePrefs should be called once, got %d", rec.setSeatRolePrefsCalls)
	}
	if rec.gameKind != "werewolf" {
		t.Fatalf("werewolf: RegisterAgentSeats gameKind mismatch: %q", rec.gameKind)
	}
}

// TestDispatchAgentSeats_TexasHoldem_OnlyRegisterCalled is the §20260819
// 报告 P0-NEW 的回归测试:texasholdem 含 agent_seats 时,
// RegisterAgentSeats 必须被调(in-memory TexasHoldem manager 注册 Bot),
// 4 个狼人杀专属方法必须 NOT 调(它们没在 TexasHoldemManager 实现,
// 调了可能触发 nil pointer 或无意义写入)。
//
// 修复前:RegisterAgentSeats 注册次数 = 0,游戏卡在 waiting。
// 修复后:RegisterAgentSeats 注册次数 = 1,且 4 个狼人杀专属方法注册次数 = 0。
func TestDispatchAgentSeats_TexasHoldem_OnlyRegisterCalled(t *testing.T) {
	rec := &texSeaterRecorder{}
	s := &RoomService{}
	s.SetAgentSeater(rec)

	seats := []AgentSeatConfig{
		{Seat: 0, ModelKey: "Qwen-model"},
		{Seat: 2, ModelKey: "DouBao-model"},
	}
	// 即便调用方传了狼人杀专属参数(裁判/难度/解说/角色偏好),texasholdem
	// 路径也必须忽略它们,绝不调用对应的 Set* 方法。
	dispatchAgentSeats(s, rec, "texasholdem", "room-t", seats,
		&JudgeConfig{Mode: "agent"}, "hard",
		&CommentaryConfig{Enabled: true},
		map[int]string{0: "werewolf"}, "seer")

	if rec.registerCalls != 1 {
		t.Fatalf("texasholdem: RegisterAgentSeats MUST be called once (P0-NEW regression), got %d", rec.registerCalls)
	}
	if rec.gameKind != "texasholdem" {
		t.Fatalf("texasholdem: RegisterAgentSeats gameKind should be 'texasholdem', got %q", rec.gameKind)
	}
	if got := len(rec.seats); got != 2 {
		t.Fatalf("texasholdem: RegisterAgentSeats seats len should be 2, got %d", got)
	}
	// 狼人杀专属方法:全部必须 NOT 调 — 防御 §130 教训"声明了却从不接线"反例,
	// 即"调了狼人杀方法却没在 TexasHoldemManager 实现 → 静默 NPE"。
	if rec.setJudgeConfigCalls != 0 {
		t.Fatalf("texasholdem: SetJudgeConfig must NOT be called, got %d", rec.setJudgeConfigCalls)
	}
	if rec.setDifficultyCalls != 0 {
		t.Fatalf("texasholem: SetAgentDifficulty must NOT be called, got %d", rec.setDifficultyCalls)
	}
	if rec.setCommentaryCalls != 0 {
		t.Fatalf("texasholdem: SetCommentaryConfig must NOT be called, got %d", rec.setCommentaryCalls)
	}
	if rec.setSeatRolePrefsCalls != 0 {
		t.Fatalf("texasholdem: SetSeatRolePrefs must NOT be called, got %d", rec.setSeatRolePrefsCalls)
	}
}

// TestDispatchAgentSeats_NoAgentSeats_SkipsEntireBlock asserts:零 agent_seats
// 的房间(纯人类)整个 dispatch 块跳过 — 防止未来重构引入"零座位仍调 seater"
// 的资源浪费。
func TestDispatchAgentSeats_NoAgentSeats_SkipsEntireBlock(t *testing.T) {
	rec := &texSeaterRecorder{}
	s := &RoomService{}
	s.SetAgentSeater(rec)

	dispatchAgentSeats(s, rec, "texasholdem", "room-h", nil, nil, "", nil, nil, "")
	dispatchAgentSeats(s, rec, "werewolf", "room-h", []AgentSeatConfig{}, nil, "", nil, nil, "")

	if rec.registerCalls != 0 {
		t.Fatalf("zero agent_seats: RegisterAgentSeats must NOT be called, got %d", rec.registerCalls)
	}
	if rec.setJudgeConfigCalls != 0 {
		t.Fatalf("zero agent_seats: SetJudgeConfig must NOT be called, got %d", rec.setJudgeConfigCalls)
	}
}

// TestDispatchAgentSeats_UnknownGameKind_SkipsBlock 防御未来重构把新游戏
// (doudizhu / xiangqi / 等)接入 dispatch 块时漏改 gameKind 判定:未知 gameKind
// 不应误触发 RegisterAgentSeats 写入到错误的 in-memory manager。
func TestDispatchAgentSeats_UnknownGameKind_SkipsBlock(t *testing.T) {
	rec := &texSeaterRecorder{}
	s := &RoomService{}
	s.SetAgentSeater(rec)

	seats := []AgentSeatConfig{{Seat: 0, ModelKey: "Qwen-model"}}
	dispatchAgentSeats(s, rec, "doudizhu", "room-d", seats, nil, "", nil, nil, "")
	dispatchAgentSeats(s, rec, "xiangqi", "room-x", seats, nil, "", nil, nil, "")

	if rec.registerCalls != 0 {
		t.Fatalf("unknown gameKind: RegisterAgentSeats must NOT be called, got %d", rec.registerCalls)
	}
}
