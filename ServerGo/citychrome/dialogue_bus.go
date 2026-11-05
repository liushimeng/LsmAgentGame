// Package citychrome — dialogue_bus.go: City-Chrome 的「Agent 双向对话」接线
// (2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/
// 01-方案设计.md §4.3(Agent 双向通讯) / §4.3.3(现有 Agent 的 handler 补齐)。
//
// 本文件提供 **City-Chrome 的统一总线入口** HandleBusMessage:
//   - 消息类型 "collect"(批次81 既有):触发一次采集 mission,返回人读摘要;
//   - 消息类型 "message"(批次84 新增):把提问转成一次**采集意图**
//     (复用既有确定性采集通道 + 既有节流),立即回「已受理,正在采集…」,
//     采集完成后用 NotifySystem 把**结果摘要上行**给 City-System
//     (「Agent 来信」链路:System 收到 ⇒ 推前端卡片 ⇒ 进下一轮上下文)。
//
// 依赖纪律(与批次81 一致):
//   - 本包 import citybus(总线路由)是批次84 的**显式例外**:统一入口的
//     签名需要 citybus.Message / *citybus.Hub。其余依赖方向不变:
//     **不 import ws / citysystem / game/virtual_city**;寻址 City-System
//     只用 class_names.go 的既有常量(§24),不 import citysystem 包。
//   - 既有 SystemBusPayload / BusTrigger 契约**零改动**(向后兼容)。
//
// 零 RNG / 零回归:提问→类目的映射是**确定性关键词表**(无随机、无 LLM);
// 采集本身仍走既有 mission 通道(批次65/78)与既有节流(llmSema + 60s 同类目
// 限流),不新增并行账本、不新增无节流的 LLM 调用。
package citychrome

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// 总线消息类型常量(批次84 §4.3.3)。
const (
	// BusTypeCollect 是批次81 既有采集触发消息类型(System 的
	// collect_realworld_data 工具使用)。
	BusTypeCollect = "collect"
	// BusTypeMessage 是批次84 新增的对话消息类型,载荷固定为
	// map[string]any{"text": "<消息正文>"}。
	BusTypeMessage = "message"
)

// BusMessageTypes 是本 Agent 支持的总线消息类型(装配层 RegisterWith 登记用)。
//
// 批次84:bus 已提供带能力描述的注册 API(RegisterWith),故把类型清单与
// 能力描述导出在本包,装配层直接引用,避免两处各写一份字面量(§130)。
var BusMessageTypes = []string{BusTypeCollect, BusTypeMessage}

// BusCapabilityDesc 是本 Agent 的一句话能力描述(装配层 RegisterWith 登记用;
// 会展示在 City-System 的 list_agents 输出里)。
const BusCapabilityDesc = "真实行情与新闻采集 Agent:可被提问并按类目触发一次真实站点采集(复用既有采集通道与节流),采集完成后主动把结果摘要回传给系统主 Agent。"

// dialogueResultTimeout 是「已受理 → 采集完成 → 上行结果」的等待上限。
// 略大于 mission 自身超时(AgentConfig.MissionTimeout 缺省 10 分钟)。
const dialogueResultTimeout = 12 * time.Minute

// dialogueUpstreamTimeout 是一次上行投递(总线 Send)的超时。
const dialogueUpstreamTimeout = 30 * time.Second

// busHubMu 保护 BusHub(装配层启动期写、运行期读)。
var (
	busHubMu sync.RWMutex
	busHub   *citybus.Hub
)

// SetBusHub 注入总线句柄(装配层 main.go 启动期调用一次)。
//
// 用途:City-Chrome 在采集完成后**主动上行**结果摘要给 City-System。
// 未注入时上行会被跳过并记 WARN(不影响既有采集链路)。
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

// dialogueServiceProvider 返回当前采集服务(默认 = 全局单例 Get)。
//
// 抽成包级变量是为了**单测可注入**(生产路径恒为 Get)。
var dialogueServiceProvider = Get

// HandleBusMessage 是 LsmAgentGame-City-Chrome 的**统一总线入口**(批次84)。
//
// 装配层接线(main.go;批次84 建议用带能力描述的 RegisterWith):
//
//	citybus.Default().RegisterWith(string(agentroot.AgentClassCityChrome),
//		citychrome.BusMessageTypes, citychrome.BusCapabilityDesc,
//		citychrome.HandleBusMessage)
//
// 消息类型:
//   - "collect":批次81 既有行为(触发一次采集),**零回归**;
//   - "message":批次84 新增的对话分支(提问 → 采集意图 → 结果上行);
//   - 其它类型:返回明确的中文失败说明(不 panic)。
func HandleBusMessage(ctx context.Context, msg citybus.Message) (any, error) {
	switch msg.Type {
	case BusTypeMessage:
		return handleDialogueMessage(ctx, msg)
	case "", BusTypeCollect:
		return handleCollectMessage(ctx, msg)
	default:
		return nil, fmt.Errorf("City-Chrome 不支持的消息类型 %q(本 Agent 支持 collect / message)", msg.Type)
	}
}

// handleCollectMessage 是批次81 的既有采集触发分支(行为逐字保留:
// category 取自载荷的 "category" 键,空 = 全部类目)。
func handleCollectMessage(ctx context.Context, msg citybus.Message) (any, error) {
	category := ""
	if payload, ok := msg.Payload.(map[string]any); ok {
		category, _ = payload["category"].(string)
	}
	return SystemBusPayload(ctx, category)
}

// handleDialogueMessage 是批次84 新增的对话分支:把提问转成一次采集意图。
//
// 流程:
//  1. 解析载荷 map[string]any{"text": "..."};缺 text / text 为空 / 载荷类型
//     不对 ⇒ 返回明确的中文失败说明(不 panic、不臆造);
//  2. 提问 → 采集类目的**确定性**映射(关键词表,零 RNG、零 LLM);
//  3. 经**既有** RefreshWithGoal 入队一次 mission(同一 60s 同类目限流 +
//     同一单飞队列 + 同一 llmSema 节流),并把提问写进 mission.Goal;
//  4. 立即回「已受理,正在采集…(结果稍后送达)」;
//  5. 采集完成后由 Service.AwaitMission 唤醒 → NotifySystem 上行结果摘要。
func handleDialogueMessage(ctx context.Context, msg citybus.Message) (any, error) {
	text, err := busMessageText(msg.Payload)
	if err != nil {
		return nil, err
	}
	svc := dialogueServiceProvider()
	if svc == nil {
		return "采集服务未启动,暂时无法受理采集请求(请稍后再试)。", nil
	}
	if !svc.Enabled() {
		return "采集数据通道未启用(city_chrome.enabled=false),暂时无法受理采集请求。", nil
	}
	if svc.sched == nil {
		return "采集服务尚未就绪(调度器未绑定),暂时无法受理采集请求。", nil
	}
	category := inferCategory(text)
	// 先登记完成等待者,再触发采集 —— 否则 mission 可能在登记前就完成(竞态)。
	ch := svc.AwaitMission(category)
	accepted, queued, e := svc.RefreshWithGoal(category, text)
	if e != nil {
		svc.DropMissionWaiter(category, ch)
		return fmt.Sprintf("未能受理采集请求:%s", e.Error()), nil
	}
	if !accepted {
		svc.DropMissionWaiter(category, ch)
		return "未能受理采集请求(同类目采集过于频繁或服务繁忙,请稍后再试)。", nil
	}
	// 已受理:采集结束后自动上行结果摘要给 City-System。
	svc.WaitMissionResult(ch, queued, msg.RoomID, text)
	return fmt.Sprintf("已受理,正在采集%s(采集完成后结果稍后送达)。", busCategoryLabel("", queued)), nil
}

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

// categoryKeyword 是「提问关键词 → 采集类目」的确定性映射项。
type categoryKeyword struct {
	category string
	keywords []string
}

// categoryKeywords 是提问 → 采集类目的关键词表(确定性,零 RNG、零 LLM)。
//
// 顺序即优先级(先具体后宽泛):A 股 → 港股 → 美股 → 贵金属 → 期货 →
// 加密货币 → 宏观 → 新闻。全部不命中时兜底到 news(分钟级、纯新闻产出,
// 最快且不误报行情数值)。
var categoryKeywords = []categoryKeyword{
	{CategoryAShare, []string{"a股", "a 股", "沪深", "上证", "深证", "创业板", "沪指", "深成指", "a股行情"}},
	{CategoryHK, []string{"港股", "恒生", "香港", "h股"}},
	{CategoryUS, []string{"美股", "纳斯达克", "道琼斯", "标普", "华尔街"}},
	{CategoryMetal, []string{"黄金", "白银", "贵金属", "金价", "银价"}},
	{CategoryFutures, []string{"原油", "期货", "大宗商品", "商品", "铜价", "天然气"}},
	{CategoryCrypto, []string{"比特币", "加密货币", "数字货币", "btc", "eth", "虚拟币"}},
	{CategoryMacro, []string{"宏观", "cpi", "gdp", "pmi", "lpr", "外汇储备", "利率", "通胀", "物价", "失业率"}},
	{CategoryNews, []string{"新闻", "快讯", "消息", "资讯", "热点", "时政", "电报", "新闻联播"}},
}

// inferCategory 把一句提问映射为采集类目(确定性关键词匹配)。
// 无命中 ⇒ news(见 categoryKeywords 注释)。
func inferCategory(text string) string {
	lower := strings.ToLower(text)
	for _, item := range categoryKeywords {
		for _, kw := range item.keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return item.category
			}
		}
	}
	return CategoryNews
}

// NotifySystem 把一条上行消息送给 City-System(批次84 §4.3.2「Agent 来信」)。
//
// 走总线 Send:寻址用 class_names.go 的既有常量(§24,不散写字面量),
// 目标方是 City-System 的房间级 handler(由装配线注册)⇒ System 收到后
// 推「Agent 来信」卡片给前端并写入下一轮对话上下文。
//
// 失败(hub 未注入 / 消息为空 / 总线寻址失败)一律返回 error,调用方记日志。
func NotifySystem(ctx context.Context, hub *citybus.Hub, roomID, text string) error {
	if hub == nil {
		return fmt.Errorf("citychrome: 总线未注入(BusHub 为 nil),无法上行消息")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("citychrome: 上行消息内容为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := hub.Send(ctx, roomID, string(agentroot.AgentClassCityChrome),
		string(agentroot.AgentClassCitySystem), BusTypeMessage,
		map[string]any{"text": text})
	return err
}

// ReportToSystem 用已注入的总线句柄上行一条消息(便捷入口;未注入时返回 error)。
func ReportToSystem(ctx context.Context, roomID, text string) error {
	return NotifySystem(ctx, currentBusHub(), roomID, text)
}

// missionOutcome 是一次采集 mission 的完成结果(批次84:对话分支等待用)。
type missionOutcome struct {
	payload *MissionPayload
	err     error
}

// AwaitMission 登记「等这个类目的采集完成后拿结果」的等待者(批次84)。
//
// ⚠️ **必须在触发采集之前调用**:mission 可能在登记之前就完成(极快的失败
// 路径,如熔断丢弃),届时等待者将永远收不到结果。触发失败时请调
// DropMissionWaiter 撤销登记。
func (s *Service) AwaitMission(category string) chan missionOutcome {
	return s.registerDialogueWaiter(category)
}

// DropMissionWaiter 撤销等待者登记(触发采集失败时调用;幂等)。
func (s *Service) DropMissionWaiter(category string, ch chan missionOutcome) {
	if s == nil || ch == nil {
		return
	}
	s.dialogueMu.Lock()
	defer s.dialogueMu.Unlock()
	list := s.dialogueWaiters[category]
	for i, c := range list {
		if c == ch {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(s.dialogueWaiters, category)
		return
	}
	s.dialogueWaiters[category] = list
}

// WaitMissionResult 在后台等待 mission 结束,并把**结果摘要**上行给
// City-System(批次84)。
//
// 零额外 LLM 调用:摘要由 mission payload 直接拼装;上行走总线 Send。
// 等待超时(12 分钟)也会上行一条失败说明,保证不留悬空承诺。
//
// 总线句柄在**调用时**捕获(而非完成时):避免运行期换 hub 后把上一轮的结果
// 投递给新的总线。
func (s *Service) WaitMissionResult(ch chan missionOutcome, category, roomID, question string) {
	if s == nil || ch == nil {
		return
	}
	hub := currentBusHub()
	go s.awaitMissionResult(ch, category, roomID, question, hub)
}

// awaitMissionResult 等待一次 mission 完成 → 拼摘要 → 上行给 City-System。
func (s *Service) awaitMissionResult(ch chan missionOutcome, category, roomID, question string, hub *citybus.Hub) {
	timer := time.NewTimer(dialogueResultTimeout)
	defer timer.Stop()
	var out missionOutcome
	select {
	case out = <-ch:
	case <-timer.C:
		out = missionOutcome{err: fmt.Errorf("采集超时(%s),本轮没有拿到结果", category)}
	}
	text := missionSummaryText(category, question, out.payload, out.err)
	if hub == nil {
		logger.L().Warn("citychrome 上行结果失败:总线未注入",
			zap.String("room_id", roomID), zap.String("category", category))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialogueUpstreamTimeout)
	defer cancel()
	if err := NotifySystem(ctx, hub, roomID, text); err != nil {
		logger.L().Warn("citychrome 上行结果失败",
			zap.String("room_id", roomID), zap.String("category", category), zap.Error(err))
	}
}

// registerDialogueWaiter 登记一个等待者(按类目);返回 nil 表示登记失败。
func (s *Service) registerDialogueWaiter(category string) chan missionOutcome {
	if s == nil {
		return nil
	}
	s.dialogueMu.Lock()
	defer s.dialogueMu.Unlock()
	if s.dialogueWaiters == nil {
		s.dialogueWaiters = map[string][]chan missionOutcome{}
	}
	ch := make(chan missionOutcome, 1)
	s.dialogueWaiters[category] = append(s.dialogueWaiters[category], ch)
	return ch
}

// onMissionComplete 是调度器 mission 完成回调(成功/失败各触发一次):
// 唤醒该类目的全部等待者(非阻塞投递;无等待者时零开销)。
//
// 锁纪律(§92a):本方法**不持**调度器锁;内部只锁自身的 dialogueMu。
func (s *Service) onMissionComplete(m Mission, payload *MissionPayload, err error) {
	if s == nil {
		return
	}
	s.dialogueMu.Lock()
	waiters := s.dialogueWaiters[m.Category]
	delete(s.dialogueWaiters, m.Category)
	s.dialogueMu.Unlock()
	for _, ch := range waiters {
		select {
		case ch <- missionOutcome{payload: payload, err: err}:
		default:
		}
	}
}

// missionSummaryText 把一次采集结果拼成中文摘要(批次84:上行给 City-System,
// 由它回灌给用户)。
//
// **只复述 payload 里的真实字段**(来源站点 / 报价 / 新闻),失败时只报错误
// 原因;绝不编造任何未采集到的数字。
func missionSummaryText(category, question string, payload *MissionPayload, err error) string {
	var b strings.Builder
	b.WriteString("【City-Chrome 采集结果】")
	if q := strings.TrimSpace(question); q != "" {
		b.WriteString("针对提问「" + truncate(q, 60) + "」")
	}
	b.WriteString("类目:" + category + "。")
	if err != nil {
		b.WriteString("采集失败:" + err.Error() + "。")
		return b.String()
	}
	if payload == nil {
		b.WriteString("采集已结束,但没有返回可用数据。")
		return b.String()
	}
	if payload.Source != "" {
		b.WriteString("数据来源:" + payload.Source + "。")
	}
	if len(payload.Quotes) > 0 {
		b.WriteString("\n行情:")
		for i, q := range payload.Quotes {
			if i >= 8 {
				b.WriteString(" …")
				break
			}
			name := q.Name
			if name == "" {
				name = q.Symbol
			}
			fmt.Fprintf(&b, " %s %.2f(涨跌幅 %.2f%%)", name, q.Price, q.ChangePct)
		}
	}
	if len(payload.News) > 0 {
		b.WriteString("\n新闻:")
		for i, n := range payload.News {
			if i >= 5 {
				b.WriteString(" …")
				break
			}
			b.WriteString(" " + truncate(n.Title, 40))
		}
	}
	if len(payload.Quotes) == 0 && len(payload.News) == 0 {
		b.WriteString("本次采集没有取得可展示的数据。")
	}
	return b.String()
}
