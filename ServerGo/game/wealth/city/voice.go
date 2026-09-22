// Package city — voice.go: 城市之声 LLM 抽样调度(契约 03 §5,2026-09-21)。
//
// 让背景居民「活着」:每月少量真实 LLM 发声,全城可见,且严格受 LLM 线路池
// (llm.LinePool)约束 —— 每名居民一次极短对话(无工具、无 Memory、小
// max_tokens),经 LinePool.Acquire(ctx 15s);失败/超时丢弃本条(下月再抽),
// 绝不阻塞月结。AgentClassName = LsmAgentGame-City-Human(§24 两步注册;
// 2026-09-22 §CityHuman重构: 五类 AgentClass 合一,背景层发声与焦点层
// 决策共用同一「城市居民」身份,仅调用深度不同)。
package city

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// 城市之声常量。
const (
	// voiceAcquireTimeout 线路 Acquire + 调用共享的 ctx 超时(契约:15s)。
	voiceAcquireTimeout = 15 * time.Second
	// voiceMaxTokens 极短对话上限(无工具,一条短句)。
	voiceMaxTokens = 128
	// voiceTextMaxRunes 产物截断(契约:Text 截 100 字)。
	voiceTextMaxRunes = 100
	// voicePerMonthMax / Min:CityVoicePerMonth clamp 边界(契约 0..32)。
	voicePerMonthMax = 32
)

// VoiceRecord 城市之声单条产物(随 game.event type="city_voice" 广播 +
// Backdrop 环形缓冲随 Snapshot 下发)。
type VoiceRecord struct {
	Month    int    `json:"month"`
	Name     string `json:"name"`  // 已锚定:真实姓名;未锚定:域·城区 代号(如 "A1024·金融CBD")
	Text     string `json:"text"`  // ≤100 字
	ModelKey string `json:"model"` // 服务线路的模型 key
	// 2026-09-21 §档案锚定(契约 §6):锚定后增补;未锚定为空(前端兼容)。
	ResidentID string `json:"resident_id,omitempty"` // 人物卡编号(CardID)
	Occupation string `json:"occupation,omitempty"`  // 职业
}

// LinePoolSource 返回当前 LLM 线路池(nil-safe;Registry.LinePool 满足)。
// 以函数形式注入以支持 Registry.Reload 换池后自动生效。
type LinePoolSource func() *llm.LinePool

// VoiceScheduler 城市之声调度器(每房一个,Start 时构造;无内部状态,
// 仅配置容器)。零值不可用 —— 经 NewVoiceScheduler 构造。
type VoiceScheduler struct {
	Enabled  bool
	PerMonth int
	Pool     LinePoolSource
}

// NewVoiceScheduler 构造调度器(perMonth clamp [0,32];0 = 关闭)。
func NewVoiceScheduler(enabled bool, perMonth int, pool LinePoolSource) *VoiceScheduler {
	if perMonth < 0 {
		perMonth = 0
	}
	if perMonth > voicePerMonthMax {
		perMonth = voicePerMonthMax
	}
	return &VoiceScheduler{Enabled: enabled, PerMonth: perMonth, Pool: pool}
}

// Run 执行本月城市之声:抽样 stressed/失业优先的候选 → 逐条经线路池发起
// 极短 LLM 对话 → 成功的回调 onRecord(串行,本 goroutine)。
// 必须在独立 goroutine 调用(房间月结后异步触发,绝不阻塞月结);
// 任何失败(Acquire 超时/LLM 错误/空响应)静默丢弃本条,不 panic。
func (s *VoiceScheduler) Run(b *Backdrop, month int, onRecord func(VoiceRecord)) {
	if s == nil || !s.Enabled || s.PerMonth <= 0 || b == nil || onRecord == nil {
		return
	}
	if s.Pool == nil {
		return
	}
	pool := s.Pool()
	if pool == nil || pool.Total() == 0 {
		return // 无线路:本月整体跳过(不算失败,下月池恢复再抽)
	}
	for _, idx := range b.PickVoiceCandidates(s.PerMonth) {
		vr, ok := s.speakOne(b, pool, month, idx)
		if ok {
			onRecord(vr)
		}
	}
}

// speakOne 单名居民一次极短对话(经线路池)。
// 2026-09-21 §档案锚定(契约 §6):锚定后 persona 用真实档案(姓名/年龄/
// 职业/域/城区/人格/opening_hook/目标/本月状态);未锚定保持代号语义零变化。
func (s *VoiceScheduler) speakOne(b *Backdrop, pool *llm.LinePool, month, idx int) (VoiceRecord, bool) {
	br, codename, ok := b.voiceBrief(idx)
	if !ok {
		return VoiceRecord{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceAcquireTimeout)
	defer cancel()
	lease, err := pool.Acquire(ctx)
	if err != nil {
		// 全忙/超时:丢弃本条(下月再抽),不重试不阻塞。
		return VoiceRecord{}, false
	}
	defer lease.Release()

	employDesc := "就业中"
	if !br.Employed {
		employDesc = "失业中,正在找活路"
	}
	stress := "心态平稳"
	if br.Stressed {
		stress = "积蓄见底,压力很大"
	}
	// 锚定判定信号:brief.CardID 非空即已锚定(未锚定 brief 恒零值)。
	anchored := br.CardID != ""
	sys := ""
	name := codename
	if anchored {
		if br.Name != "" {
			name = br.Name
		}
		status := employDesc
		if br.Stressed {
			status += "," + stress
		}
		sys = fmt.Sprintf(
			"你是虚拟城市居民「%s」,%d岁,%s(%s,住在%s)。性格:%s。%s 你的 5 年目标:%s。本月状态:%s;储蓄约 %.0f 元(约 %.1f 个月开支)。请始终以这名居民的口吻说话。",
			name, br.Age, br.Occupation, br.DomainName, br.DistrictName,
			br.Personality, br.OpeningHook, br.Goal, status, br.SavingsCNY, br.MonthsRunway,
		)
	} else {
		sys = fmt.Sprintf(
			"你是虚拟城市的一名普通居民,代号 %s,从事 %s 行业,住在%s。本月状态:%s;储蓄约 %.0f 元(约 %.1f 个月开支),%s。请始终以这名居民的口吻说话。",
			codename, br.DomainName, br.DistrictName, employDesc, br.SavingsCNY, br.MonthsRunway, stress,
		)
	}
	req := llm.LLMRequest{
		Model: lease.ModelKey,
		System: []llm.SystemBlock{
			{Type: "text", Text: sys},
		},
		Messages: []llm.Message{
			{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "用一句话说出你本月的感受或打算(不超过 60 字)。"}}},
		},
		MaxTokens:      voiceMaxTokens,
		AgentClassName: string(agentroot.AgentClassCityHuman),
	}
	resp, err := chatViaLease(ctx, lease, req)
	if err != nil {
		logger.L().Debug("city voice llm call failed, dropped",
			zap.Int("month", month), zap.String("name", name), zap.Error(err))
		return VoiceRecord{}, false
	}
	text := strings.TrimSpace(resp.Text())
	if text == "" {
		return VoiceRecord{}, false
	}
	vr := VoiceRecord{
		Month:    month,
		Name:     name,
		Text:     clipRunes(text, voiceTextMaxRunes),
		ModelKey: lease.ModelKey,
	}
	if anchored {
		vr.ResidentID = br.CardID
		vr.Occupation = br.Occupation
	}
	return vr, true
}

// chatViaLease 用租约内的 Provider/APIKey 发起调用(流式优先,§197 字节刷新;
// 无流式能力的 Provider 回退 Chat)。
func chatViaLease(ctx context.Context, lease *llm.LineLease, req llm.LLMRequest) (llm.LLMResponse, error) {
	type streamingProvider interface {
		ChatStreamAccumulate(context.Context, string, llm.LLMRequest, func(llmtypes.StreamEvent) error) (llm.LLMResponse, error)
	}
	if sp, ok := lease.Provider.(streamingProvider); ok {
		return sp.ChatStreamAccumulate(ctx, lease.APIKey, req, nil)
	}
	return lease.Provider.Chat(ctx, lease.APIKey, req)
}

// clipRunes 按 rune 截断。
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
