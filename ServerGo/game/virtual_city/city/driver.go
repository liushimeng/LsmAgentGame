// Package city — driver.go: 居民驱动层(2026-09-22 §17-CityHuman 全民驱动,
// 契约: lag_docs/虚拟城市/已实现/17-CityHuman全民驱动/
// 02-架构设计-居民驱动层与LLM线路池-v1.md §2/§3/§5/§8)。
//
// 目标:虚拟城市中的**每个居民都是 LsmAgentGame-City-Human** —— 拥有自己的
// 系统提示词(档案人格 persona)、Context 内容(月度状态)与通用 Tools 定义
// (set_intent + speak);由驱动层统一驱动运行:上层是**线程池**(worker pool),
// 底层是 **LLM 线路池**(并发 = Σ线路数,全局共享)。
//
// 成本算术:10 万居民 × 每月 1 次 × 3~8s/次,即使 64 条线路也需 1.3~3 小时/月。
// 驱动层以 per_month 预算 + 跨月轮转(cursor)保证「每个居民都会被驱动到,
// 且城市规模与线路数解耦」;12 名 UI 展示居民同样经驱动层抽样。
//
// 锁纪律(§92a/契约 §8):Driver 全程**不持有 Backdrop.mu 跨 LLM 调用** ——
// brief 锁内快照、ApplyIntent 锁内短临界区(与 voice.go 同款);worker 数仅
// 经全局 LinePool;
// LLM 调用流式优先(chatViaLease → ChatStreamAccumulate),15s 租约超时 +
// 单轮 256 max tokens,无长循环续命需求(§197)。
//
// 失败语义与 voice.go 一致:单条失败静默丢弃(Debug 日志),不重试、不阻塞
// 月结、不影响其他 worker。
package city

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// 驱动层常量(契约 02 §2/§7)。
const (
	// driverMaxTokens 单居民轮次 LLM 上限(单轮解析,无二轮循环)。
	driverMaxTokens = 256
	// driverSpeakMaxRunes speak 产物截断(契约 §3.3:text ≤60 字)。
	driverSpeakMaxRunes = 60
	// driverNeighborCap contextText 附近居民条数上限(契约 §3.2:≤3)。
	driverNeighborCap = 3
	// driverDefaultAcquireTimeoutMS 线路租约等待缺省(契约 §2:15000ms)。
	driverDefaultAcquireTimeoutMS = 15000
	// driverDefaultWorkers / clamp 边界(契约 §7:workers 缺省 4,clamp [1,16])。
	driverDefaultWorkers = 4
	driverMinWorkers     = 1
	driverMaxWorkers     = 16
	// driverDefaultPerMonth / clamp 边界(契约 §7:per_month 缺省 8,clamp [0,64];
	// 2026-09-24 重构:0 = 缺省 8,删除「仅抽样层」语义,所有居民都被驱动)。
	driverDefaultPerMonth = 8
	driverMaxPerMonth     = 64
	driverMinPerMonth     = 0
)

// DriverConfig 驱动层配置(config [wealth] 段注入)。
type DriverConfig struct {
	Enabled          bool // city_driver_enabled,缺省 true
	Workers          int  // city_driver_workers,线程池大小,缺省 4,clamp [1,16]
	PerMonth         int  // city_driver_per_month,每月驱动居民数,缺省 8,clamp [0,64]
	AcquireTimeoutMS int  // 线路租约等待,缺省 15000
}

// DriverSnapshot 驱动层快照(随 game.state.city.driver 下发;omitempty ——
// driver 未启用时前端不渲染该块)。
type DriverSnapshot struct {
	Enabled    bool `json:"enabled"`
	Workers    int  `json:"workers"`
	PerMonth   int  `json:"per_month"`
	DrivenLast int  `json:"driven_last"` // 最近一个月实际完成轮次数
}

// ResidentDriver 居民驱动层:线程池 + 线路池 + 跨月轮转游标。每房一个,
// Start 时构造(契约 §5;与 VoiceScheduler 互斥)。
type ResidentDriver struct {
	cfg  DriverConfig
	pool LinePoolSource // 与 voice.go 同名类型复用(池 Reload 后指向新池)

	// cursor 跨月公平轮转游标(原子读写;PickDriverCandidates 消费并推进)。
	cursor uint64

	// mu 只保护 lastDriven(与 Snapshot 读竞争);绝不嵌套 Backdrop.mu。
	mu         sync.Mutex
	lastDriven int

	// ambiance 单城区气味/声响来源(room 注入,锁外调用;nil = 无氛围段)。
	ambiance func() map[string]AmbianceTags
}

// NewResidentDriver 构造驱动层(cfg 归一:Workers 0→4 且 clamp [1,16];
// PerMonth clamp [0,64],0=缺省 8;AcquireTimeoutMS 0→15000)。
func NewResidentDriver(cfg DriverConfig, pool LinePoolSource) *ResidentDriver {
	if cfg.Workers <= 0 {
		cfg.Workers = driverDefaultWorkers
	}
	if cfg.Workers < driverMinWorkers {
		cfg.Workers = driverMinWorkers
	}
	if cfg.Workers > driverMaxWorkers {
		cfg.Workers = driverMaxWorkers
	}
	if cfg.PerMonth < driverMinPerMonth {
		cfg.PerMonth = driverMinPerMonth
	}
	if cfg.PerMonth > driverMaxPerMonth {
		cfg.PerMonth = driverMaxPerMonth
	}
	if cfg.AcquireTimeoutMS <= 0 {
		cfg.AcquireTimeoutMS = driverDefaultAcquireTimeoutMS
	}
	return &ResidentDriver{cfg: cfg, pool: pool}
}

// SetAmbianceSource 注入城区氛围来源(room 层 cityAmbianceLocked 的锁外
// 薄包装;nil 安全)。worker 在 Backdrop.mu 之外调用 —— 绝不在任何锁内触发。
func (d *ResidentDriver) SetAmbianceSource(fn func() map[string]AmbianceTags) {
	if d == nil {
		return
	}
	d.ambiance = fn
}

// Snapshot 返回驱动层快照(city.Snapshot.Driver 下发;mu 只圈 lastDriven)。
func (d *ResidentDriver) Snapshot() DriverSnapshot {
	if d == nil {
		return DriverSnapshot{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return DriverSnapshot{
		Enabled:    d.cfg.Enabled,
		Workers:    d.cfg.Workers,
		PerMonth:   d.cfg.PerMonth,
		DrivenLast: d.lastDriven,
	}
}

// RunMonth 驱动一个月的居民轮次(月结后异步调用,绝不阻塞月结)。
// picks = b.PickDriverCandidates(cfg.PerMonth, &d.cursor) → 任务 chan →
// cfg.Workers 个 worker(线程池)→ runOne → WaitGroup;完成计数写入
// lastDriven(锁内,Snapshot 下发)。
func (d *ResidentDriver) RunMonth(b *Backdrop, month int, onVoice func(VoiceRecord)) {
	if d == nil || b == nil || onVoice == nil {
		return
	}
	if !d.cfg.Enabled || d.cfg.PerMonth <= driverMinPerMonth {
		return
	}
	if d.pool == nil {
		return
	}
	pool := d.pool()
	if pool == nil || pool.Total() == 0 {
		return // 无线路:本月整体跳过(不算失败,下月池恢复再抽;与 voice.go 同款)
	}
	picks := b.PickDriverCandidates(d.cfg.PerMonth, &d.cursor)
	d.setLastDriven(0)
	if len(picks) == 0 {
		return
	}
	workers := d.cfg.Workers
	if workers > len(picks) {
		workers = len(picks)
	}
	tasks := make(chan int, len(picks))
	for _, idx := range picks {
		tasks <- idx
	}
	close(tasks)
	var done atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range tasks {
				if d.runOne(b, pool, month, idx, onVoice) {
					done.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	d.setLastDriven(int(done.Load()))
}

// setLastDriven 记录本月完成轮次数(mu 只圈该字段)。
func (d *ResidentDriver) setLastDriven(n int) {
	d.mu.Lock()
	d.lastDriven = n
	d.mu.Unlock()
}

// runOne 居民轮次 runOne(单居民单月;契约 §2.1 五步)。返回是否完成一次
// LLM 轮次(Acquire 失败/LLM 错误 → false,静默丢弃)。
//
//	1. brief := b.DriverBrief(idx)         锁内快照:档案人格 + 月度状态 + 邻居
//	2. ctx 超时 → pool.Acquire(ctx)        ErrAllLinesBusy → 丢弃本条
//	3. req := LLMRequest{persona / context / commonToolDefs / 256 tokens}
//	4. resp := chatViaLease(...)           复用 voice.go 的租约调用路径(流式优先)
//	5. 解析 resp tool_use 块(单轮,不发 tool_result 二轮):
//	   set_intent → b.ApplyIntent / speak → VoiceRecord → onVoice 回调
func (d *ResidentDriver) runOne(b *Backdrop, pool *llm.LinePool, month, idx int, onVoice func(VoiceRecord)) bool {
	brief, ok := b.DriverBrief(idx)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(d.cfg.AcquireTimeoutMS)*time.Millisecond)
	defer cancel()
	lease, err := pool.Acquire(ctx)
	if err != nil {
		// 全忙/超时:丢弃本条(下月轮转再抽),不重试不阻塞。
		logger.L().Debug("city driver line acquire busy, dropped",
			zap.Int("month", month), zap.Int("resident", idx), zap.Error(err))
		return false
	}
	defer lease.Release()

	req := llm.LLMRequest{
		Model:          lease.ModelKey,
		AgentClassName: string(agentroot.AgentClassCityHuman),
		System:         personaBlocks(brief),
		Messages: []llm.Message{
			{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: contextText(brief, month, d.ambianceFor(brief.DistrictID))}}},
		},
		Tools:     commonToolDefs(),
		MaxTokens: driverMaxTokens,
	}
	resp, err := chatViaLease(ctx, lease, req)
	if err != nil {
		logger.L().Debug("city driver llm call failed, dropped",
			zap.Int("month", month), zap.String("name", brief.displayName()), zap.Error(err))
		return false
	}
	d.applyToolUses(b, idx, month, brief, lease.ModelKey, resp, onVoice)
	return true
}

// applyToolUses 解析响应中的 tool_use 块(单轮解析,不发 tool_result 二轮;
// 契约 §2.1 第 5 步):set_intent 写意图位(下个 TickMonth 消费后清除);
// speak 产出 VoiceRecord(城市之声事件 + 环形缓冲,复用 emitCityVoiceEvent
// 现有管线)。
func (d *ResidentDriver) applyToolUses(b *Backdrop, idx, month int, brief DriverBrief, modelKey string, resp llm.LLMResponse, onVoice func(VoiceRecord)) {
	for _, cb := range resp.Content {
		if cb.Type != "tool_use" {
			continue
		}
		switch cb.Name {
		case "speak":
			text := toolString(cb.Input, "text")
			if text == "" {
				continue
			}
			vr := VoiceRecord{
				Month:    month,
				Name:     brief.displayName(),
				Text:     clipRunes(text, driverSpeakMaxRunes),
				ModelKey: modelKey,
			}
			if brief.anchored() {
				vr.ResidentID = brief.CardID
				vr.Occupation = brief.Occupation
			}
			onVoice(vr)
		case "set_intent":
			b.ApplyIntent(idx, toolString(cb.Input, "intent"), toolString(cb.Input, "target_district"))
		}
	}
}

// ambianceFor 取指定城区的当月氛围(未注入来源 / 城区未命中 → 零值)。
// 调用点在 Backdrop.mu 之外(runOne 已拿到 brief 快照),来源内部自行持锁。
func (d *ResidentDriver) ambianceFor(districtID string) AmbianceTags {
	if d == nil || d.ambiance == nil || districtID == "" {
		return AmbianceTags{}
	}
	if m := d.ambiance(); m != nil {
		return m[districtID]
	}
	return AmbianceTags{}
}

// personaBlocks System 提示词 = 档案人格(契约 §3.1)。锚定后用真实档案
// (姓名/年龄/职业/域/城区/人格/opening_hook/目标/婚姻/健康档);锚定前
// 降级代号语义(与 voice.go 现行降级链一致)。
func personaBlocks(brief DriverBrief) []llm.SystemBlock {
	var sys string
	if brief.anchored() {
		sys = fmt.Sprintf(
			"你是虚拟城市居民「%s」,%d岁,%s(%s,住在%s)。性格:%s。%s 你的 5 年目标:%s。婚姻%s,健康档 %s。请始终以这名居民的身份思考与说话。",
			brief.displayName(), brief.Age, brief.Occupation, brief.DomainName, brief.DistrictName,
			brief.Personality, brief.OpeningHook, brief.Goal, brief.MaritalOr("单身"), brief.HealthGradeOr("A"),
		)
	} else {
		sys = fmt.Sprintf(
			"你是虚拟城市的一名普通居民,代号 %s,从事 %s 行业,住在%s。请始终以这名居民的身份思考与说话。",
			brief.Codename, brief.DomainName, brief.DistrictName)
	}
	return []llm.SystemBlock{{Type: "text", Text: sys}}
}

// contextText User Context = 月度状态(契约 §3.2):收入/支出/储蓄/可支撑
// 月数/压力位 + 所在城区与氛围 + 附近居民 ≤3 + 行动指引。
func contextText(brief DriverBrief, month int, amb AmbianceTags) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "■ 本月状态(第 %d 月)\n", month)
	employ := "就业中"
	if !brief.Employed {
		employ = "失业中"
	}
	stress := ""
	if brief.Stressed {
		stress = "(积蓄见底,压力偏高)"
	}
	fmt.Fprintf(&sb, "收入 %.0f 元/月(%s);支出 %.0f 元;储蓄 %.0f 元(约 %.1f 个月开支)%s。\n",
		brief.Income, employ, brief.Expense, brief.SavingsCNY, brief.MonthsRunway, stress)
	smells, sounds := "平静", "安静"
	if len(amb.Smells) > 0 {
		smells = strings.Join(amb.Smells, "、")
	}
	if len(amb.Sounds) > 0 {
		sounds = strings.Join(amb.Sounds, "、")
	}
	fmt.Fprintf(&sb, "所在城区:%s。城区氛围:%s/%s。\n", brief.DistrictName, smells, sounds)
	if len(brief.Neighbors) > 0 {
		names := make([]string, 0, len(brief.Neighbors))
		for _, n := range brief.Neighbors {
			names = append(names, fmt.Sprintf("%s(%s)", n.Name, n.Occupation))
		}
		fmt.Fprintf(&sb, "附近居民:%s。\n", strings.Join(names, "、"))
	}
	sb.WriteString("请调用 set_intent 设定你本月的打算;如果想对街坊说句话,再调用 speak。")
	return sb.String()
}

// commonToolDefs 通用工具定义(契约 §3.3;Anthropic wire 四键约束 §14.1 ——
// InputSchema 恒为 JSON Schema object)。
func commonToolDefs() []llmtypes.ToolDef {
	return []llmtypes.ToolDef{
		{
			Name:        "set_intent",
			Description: "设定你本月的打算(下月结算时生效一次,随后自动清除)。move_out 必须带 target_district(目标城区 id)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent": map[string]any{
						"type": "string",
						"enum": []string{"job_seeking", "frugal", "consume", "socialize", "move_out"},
						"description": "job_seeking=努力找工作(提高再就业概率);frugal=省吃俭用(降低本月支出);" +
							"consume=消费一把(提高本月支出);socialize=走亲访友(缓解压力);move_out=搬去 target_district 城区",
					},
					"target_district": map[string]any{
						"type":        "string",
						"description": "目标城区 id(仅 move_out 需要)",
					},
				},
				"required": []string{"intent"},
			},
		},
		{
			Name:        "speak",
			Description: "对同城区街坊说一句话(不超过 60 字),会出现在城市之声里,全城可闻。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "想说的一句话(1..60 字,以这名居民的身份与口吻)",
					},
				},
				"required": []string{"text"},
			},
		},
	}
}

// toolString 从 tool_use.Input 安全取 string 字段(缺省/类型漂移 → "")。
func toolString(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	if v, ok := input[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
