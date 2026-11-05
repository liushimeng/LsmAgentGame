// Package vcplayer — dialogue_bus.go: City-Human 的「Agent 双向对话」接线
// (2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/
// 01-方案设计.md §4.3(Agent 双向通讯) / §4.3.3(现有 Agent 的 handler 补齐)。
//
// 本文件提供 **City-Human 的统一总线入口** HandleBusMessage:
//   - 消息类型 "message"(批次84 新增):产出**居民口吻**的一句中文回应;
//   - 其它类型:走批次81 既有分支(返回本房居民摘要),**零回归**。
//
// 成本纪律(§4.3.3「复用既有 LLM 通道,零额外成本优先」):
//  1. 优先复用既有 City-Human LLM 通道 —— Agent.callProvider(§197 流式)
//     + 既有令牌桶节流(a.rate),**不新增 LLM 通道、不新增节流策略**;
//  2. 该通道不可用(装配层未注入 / 令牌耗尽 / LLM 失败)⇒ 退化为**基于既有
//     房间快照的确定性说明**:只复述快照里已有的事实(座位/昵称/存活/现金/
//     城区),**绝不编造**任何行情或事件数据。
//
// 依赖纪律:
//   - 本包 import citybus(总线路由)是批次84 的**显式例外**:统一入口的
//     签名需要 citybus.Message / *citybus.Hub。仍**禁止** import
//     game/virtual_city(反向依赖会成环)与 citysystem(寻址只用
//     class_names.go 的既有常量,§24)。
//   - AgentClassName 只用 agentroot.AgentClassCityHuman(唯一居民 AgentClass,
//     §29.1 纪律:不得新增别名)。
package vcplayer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/agent/vctypes"
	llmtypes "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// BusTypeMessage 是批次84 的对话消息类型,载荷固定为
// map[string]any{"text": "<消息正文>"}。
const BusTypeMessage = "message"

// BusMessageTypes 是本 Agent 支持的总线消息类型(装配层 RegisterWith 登记用)。
//
// 批次84:bus 已提供带能力描述的注册 API(RegisterWith),故把类型清单与
// 能力描述导出在本包,装配层直接引用,避免两处各写一份字面量(§130)。
var BusMessageTypes = []string{BusTypeMessage}

// BusCapabilityDesc 是本 Agent 的一句话能力描述(装配层 RegisterWith 登记用;
// 会展示在 City-System 的 list_agents 输出里)。
const BusCapabilityDesc = "城市居民 Agent:可用居民口吻回答城市生活类提问(复用既有居民 LLM 通道与既有节流,无编造数据),其它类型返回本房居民摘要。"

// busHubMu 保护 BusHub(装配层启动期写、运行期读)。
var (
	busHubMu sync.RWMutex
	busHub   *citybus.Hub
)

// SetBusHub 注入总线句柄(装配层 main.go 启动期调用一次)。
//
// 用途:City-Human **主动汇报**时把消息送给 City-System。未注入时
// ReportToSystem 返回明确错误(不影响既有链路)。
func SetBusHub(hub *citybus.Hub) {
	busHubMu.Lock()
	busHub = hub
	busHubMu.Unlock()
}

// currentBusHub 返回当前总线句柄(副本;nil = 未注入)。
func currentBusHub() *citybus.Hub {
	busHubMu.RLock()
	defer busHubMu.RUnlock()
	return busHub
}

// ResidentAnswerSource 是**可选**注入点:用房间内既有 City-Human Agent
// (既有 LLM 通道 + 既有令牌桶节流)回答居民视角提问。
//
// 实现方(装配层)负责:按 roomID 取一个居民 Agent 与其人物卡,再调
// (*Agent).AnswerQuestion。未注入 ⇒ 对话分支退化为「基于房间快照的确定性
// 说明」(不编造数据)。
type ResidentAnswerSource interface {
	ResidentAnswer(ctx context.Context, roomID, question string) (string, error)
}

// CityResidentAnswerer 是 ResidentAnswerSource 的全局注入点(装配层设置)。
var CityResidentAnswerer ResidentAnswerSource

// HandleBusMessage 是 LsmAgentGame-City-Human 的**统一总线入口**(批次84)。
//
// 装配层接线(main.go;批次84 建议用带能力描述的 RegisterWith):
//
//	citybus.Default().RegisterWith(string(agentroot.AgentClassCityHuman),
//		vcplayer.BusMessageTypes, vcplayer.BusCapabilityDesc,
//		vcplayer.HandleBusMessage)
//
// 消息类型:
//   - "message":批次84 新增的对话分支(居民口吻回应);
//   - 其它(含空串):批次81 既有分支(本房居民摘要),**零回归**。
func HandleBusMessage(ctx context.Context, msg citybus.Message) (any, error) {
	if msg.Type == BusTypeMessage {
		return handleResidentMessage(ctx, msg)
	}
	// 既有行为(批次81):返回本房居民摘要。
	return SystemBusResidents(ctx, msg.RoomID)
}

// handleResidentMessage 处理一次居民视角提问。
func handleResidentMessage(ctx context.Context, msg citybus.Message) (any, error) {
	text, err := busMessageText(msg.Payload)
	if err != nil {
		return nil, err
	}
	// 1) 优先走既有 City-Human LLM 通道(装配层注入时)。
	if CityResidentAnswerer != nil {
		reply, err := CityResidentAnswerer.ResidentAnswer(ctx, msg.RoomID, text)
		if err == nil && strings.TrimSpace(reply) != "" {
			return reply, nil
		}
		logger.L().Warn("vcplayer 居民对话回退到确定性说明",
			zap.String("room_id", msg.RoomID), zap.Error(err))
	}
	// 2) 兜底:基于既有房间快照的确定性说明(绝不编造数据)。
	briefs, available, err := loadResidentBriefs(ctx, msg.RoomID)
	if err != nil {
		return nil, err
	}
	if !available {
		return cityDialogueUnavailableMsg, nil
	}
	return residentFallbackReply(briefs, text), nil
}

// cityDialogueUnavailableMsg 是对话分支下「居民快照不可用」的可读说明
// (装配层未注入 CityRoomSource;是状态而非故障,故不返回 error)。
const cityDialogueUnavailableMsg = "居民视角暂不可用:CityRoomSource 未注入(装配层尚未完成 vcplayer 总线接线)"

// busMessageText 解析 "message" 类型的载荷 map[string]any{"text": "..."}。
//
// 三类非法载荷各有明确的中文失败说明(不 panic):
//   - 载荷不是 map 也不是字符串(如 nil / 数字 / 数组);
//   - 载荷缺 "text" 键;
//   - "text" 不是字符串 或 为空串。
//
// **兼容说明(批次81 既有契约)**:批次81 的 send_agent_message 工具经总线
// 传的是**裸字符串**(citysystem/dispatch.go),因此裸字符串载荷按「消息正文」
// 解析(与 map{"text":...} 等价),避免既有工具路径回归;批次84 的
// ask_agent / 上行来信一律走 map{"text":...} 契约。
func busMessageText(payload any) (string, error) {
	switch p := payload.(type) {
	case string:
		text := strings.TrimSpace(p)
		if text == "" {
			return "", fmt.Errorf("消息内容为空:请在 text 字段中给出具体问题")
		}
		return text, nil
	case map[string]any:
		raw, ok := p["text"]
		if !ok {
			return "", fmt.Errorf("消息载荷缺少 text 字段:期望 {\"text\": \"...\"}")
		}
		text, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("消息载荷的 text 字段类型错误:期望字符串,实际类型 %T", raw)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return "", fmt.Errorf("消息内容为空:请在 text 字段中给出具体问题")
		}
		return text, nil
	default:
		return "", fmt.Errorf("消息载荷格式错误:期望 {\"text\": \"...\"},实际类型 %T", payload)
	}
}

// loadResidentBriefs 读取本房居民快照(只读)。
//
// 返回值:
//
//	(briefs, true, nil)  —— 快照可用;
//	(nil, false, nil)    —— CityRoomSource 未注入(装配缺口,状态而非故障);
//	(nil, false, err)    —— 快照读取失败(ctx 取消 / 房间不存在)。
func loadResidentBriefs(ctx context.Context, roomID string) ([]ResidentBrief, bool, error) {
	if CityRoomSource == nil {
		return nil, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	var (
		briefs []ResidentBrief
		err    error
	)
	if cs, ok := CityRoomSource.(ctxAwareResidentsSource); ok {
		briefs, err = cs.SystemBusResidentsContext(ctx, roomID)
	} else {
		briefs, err = CityRoomSource.SystemBusResidents(roomID)
	}
	if err != nil {
		return nil, false, err
	}
	return briefs, true, nil
}

// residentFallbackReply 在**没有可用 LLM 通道**时给出的居民口吻回应。
//
// 只复述房间快照里已有的事实(座位/昵称/存活/现金/城区),并明确说明城市级
// 数据要看实时看板 —— **绝不编造**任何行情、物价或事件数值。
func residentFallbackReply(briefs []ResidentBrief, question string) string {
	q := strings.TrimSpace(question)
	if len(briefs) == 0 {
		return fmt.Sprintf("我是这座城市的居民。你问的「%s」——我这边现在没有可引用的城市数据,不敢凭空回答;"+
			"城市层面的行情与物价请看实时看板。", q)
	}
	speaker, alive := pickSpeaker(briefs)
	status := "还活着,日子照过"
	if !alive {
		status = "已经不在了"
	}
	district := speaker.District
	if district == "" {
		district = "未记录城区"
	}
	return fmt.Sprintf("我是 %s(#%d 的居民),住在%s区,%s,手头现金 %d 元。你问的「%s」——"+
		"我能确认的只有自己这一摊:我现在的情况就是这样;城市层面的行情、物价、就业这些数字我不敢替城市编,"+
		"要看准数请查实时看板。",
		speaker.Nickname, speaker.Seat+1, district, status, speaker.Cash, q)
}

// pickSpeaker 选一个发言居民(座位升序取第一个存活者;全员不在则取第一个)。
// 返回 (居民, 是否存活)。
func pickSpeaker(briefs []ResidentBrief) (ResidentBrief, bool) {
	ordered := make([]ResidentBrief, len(briefs))
	copy(ordered, briefs)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Seat < ordered[j].Seat })
	for _, b := range ordered {
		if b.Alive {
			return b, true
		}
	}
	if len(ordered) == 0 {
		return ResidentBrief{}, false
	}
	return ordered[0], ordered[0].Alive
}

// NotifySystem 把一条上行消息送给 City-System(批次84 §4.3.2「Agent 来信」)。
//
// 走总线 Send:寻址用 class_names.go 的既有常量(§24),from 为唯一的居民
// AgentClassName「LsmAgentGame-City-Human」(§29.1:不得新增别名)。
func NotifySystem(ctx context.Context, hub *citybus.Hub, roomID, text string) error {
	if hub == nil {
		return fmt.Errorf("vcplayer: 总线未注入(BusHub 为 nil),无法上行消息")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("vcplayer: 上行消息内容为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := hub.Send(ctx, roomID, string(agentroot.AgentClassCityHuman),
		string(agentroot.AgentClassCitySystem), BusTypeMessage,
		map[string]any{"text": text})
	return err
}

// ReportToSystem 用已注入的总线句柄上行一条消息(便捷入口)。
func ReportToSystem(ctx context.Context, roomID, text string) error {
	return NotifySystem(ctx, currentBusHub(), roomID, text)
}

// AnswerQuestion 让本居民用**既有 LLM 通道**回答一句提问(批次84)。
//
// 复用既有 callProvider(§197 流式续命)+ 既有令牌桶节流(a.rate),不新增
// LLM 通道、不新增节流策略;纯问答、**不带工具**,因此不触发任何游戏内动作
// (不消耗动作预算、不影响月结)。
//
// 失败情形(全部返回 error,调用方据此退化为确定性说明):
//   - Agent 未绑定 LLM 通道;
//   - 令牌桶暂不可用(节流);
//   - 模型调用失败 / 未给出文字回应。
func (a *Agent) AnswerQuestion(ctx context.Context, card vctypes.CardBrief, question string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("vcplayer: 居民 Agent 未绑定")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("vcplayer: 提问为空")
	}
	if a.registry == nil && a.linePoolSource == nil {
		return "", fmt.Errorf("vcplayer: 居民 LLM 通道未绑定")
	}
	// 既有节流:每次 LLM 调用消耗 1 枚令牌(桶空即中止,绝不无限重试)。
	if !a.rateAllowLLM() {
		return "", fmt.Errorf("vcplayer: 居民 LLM 节流(令牌暂不可用),请稍后再问")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req := a.BuildLLMRequest(SystemPromptBlocks(card), "", nil)
	req.Messages = []llmtypes.Message{{
		Role:    "user",
		Content: []llmtypes.ContentBlock{{Type: "text", Text: dialogueUserPrompt(question)}},
	}}
	req.MaxTokens = 512
	resp, err := a.callProvider(ctx, *req, func(llmtypes.StreamEvent) error { return nil })
	if err != nil {
		return "", err
	}
	reply := strings.TrimSpace(textConcatenate(resp))
	if reply == "" {
		return "", fmt.Errorf("vcplayer: 模型未给出文字回应")
	}
	return reply, nil
}

// dialogueUserPrompt 构造居民问答的 user 文本(只问一件事、不带工具)。
func dialogueUserPrompt(question string) string {
	return fmt.Sprintf("有人在向你(这座城市的一名普通居民)打听情况,问题是:「%s」。\n\n"+
		"请用**你自己的口吻**回答(1-2 句话,像街坊聊天):\n"+
		"1. 只说你能确认的情况,不知道就说不知道,**严禁编造任何数字、行情或事件**;\n"+
		"2. 不要罗列规则、不要使用工具、不要输出 JSON 或列表;\n"+
		"3. 允许你用自己的生活经验与感受作答。", question)
}
