// Package citysystem — dispatch_ext_test.go: 批次84 新增 / 扩展工具的派发单测
// (2026-11-05 §批次84)。
//
// 覆盖:search_scenarios / list_scenarios(维度索引)/ query_metric_history /
// query_districts / query_surveys / apply_display / ask_agent / list_agents。
// 每个工具至少 1 正 1 负;apply_display 逐分支校验(合法产指令 / 非法 isErr)。
package citysystem

import (
	"context"
	"strings"
	"testing"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/game/virtual_city"
)

// extFixture 是批次84 工具的夹具(真实房间 + 已注册总线目标)。
type extFixture struct {
	agent  *Agent
	room   *virtual_city.VirtualCityRoom
	events *[]SystemEvent
}

func newExtFixture(t *testing.T) *extFixture {
	t.Helper()
	room := newTestRoom(t, "R-ext")
	room.OwnerID = "owner-1"
	reg := &fakeRegistry{p: &fakeProvider{}}
	a, events := newTestAgent(t, room, reg)

	// City-WebGL-Show(房间级):按 displayRequest 回显指令。
	if err := a.hub.RegisterRoomWith(room.RoomID, agentClassCityWebGLShow,
		[]string{"run_display"}, "渲染显示 Agent",
		func(_ context.Context, msg citybus.Message) (any, error) {
			req, ok := msg.Payload.(displayRequest)
			if !ok {
				return []displayCommand{}, nil
			}
			var cmds []displayCommand
			if req.Weather != "" {
				cmds = append(cmds, displayCommand{Type: DisplaySetWeather, Weather: req.Weather})
			}
			if req.Phase != "" {
				cmds = append(cmds, displayCommand{Type: DisplaySetTimeOfDay, Phase: req.Phase})
			}
			if req.Speed != "" {
				cmds = append(cmds, displayCommand{Type: DisplaySetTimeSpeed, Speed: req.Speed})
			}
			if req.Layer != "" {
				cmds = append(cmds, displayCommand{Type: DisplaySetLayerVisibility, Layer: req.Layer, Visible: req.Visible})
			}
			if req.EntityID != "" {
				cmds = append(cmds, displayCommand{Type: DisplayFocusEntity, EntityID: req.EntityID})
			}
			if req.Preset != "" {
				cmds = append(cmds, displayCommand{Type: DisplayCameraPreset, Preset: req.Preset})
			}
			if len(req.Points) > 0 {
				cmds = append(cmds, displayCommand{Type: DisplayFlyPath, Points: req.Points})
			}
			return cmds, nil
		}); err != nil {
		t.Fatalf("register webgl: %v", err)
	}
	// City-Chrome(类级,带能力描述)。
	if err := a.hub.RegisterWith(string(agentroot.AgentClassCityChrome),
		[]string{"collect", "message"}, "采集真实世界行情与新闻",
		func(context.Context, citybus.Message) (any, error) {
			return "采集完成:3 条行情", nil
		}); err != nil {
		t.Fatalf("register chrome: %v", err)
	}
	// City-Human(类级,慢回包:测 ask_agent 超时)。
	if err := a.hub.RegisterWith(string(agentroot.AgentClassCityHuman),
		[]string{"message"}, "城市居民 Agent",
		func(ctx context.Context, msg citybus.Message) (any, error) {
			if payload, ok := msg.Payload.(map[string]any); ok {
				if q, _ := payload["text"].(string); strings.Contains(q, "慢") {
					select {
					case <-time.After(2 * time.Second):
					case <-ctx.Done():
					}
					return "太慢了", nil
				}
			}
			return "居民:日子照过", nil
		}); err != nil {
		t.Fatalf("register human: %v", err)
	}
	return &extFixture{agent: a, room: room, events: events}
}

// TestSearchScenarios 正:关键词命中;负:未知分组 35064。
func TestSearchScenarios(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolSearchScenarios,
		map[string]any{"query": "地震", "limit": 10})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, virtual_city.ScenarioEarthquake) {
		t.Fatalf("search must hit %q, got %q", virtual_city.ScenarioEarthquake, text)
	}
	if !strings.Contains(text, "inject_scenario") {
		t.Error("search result must guide inject_scenario")
	}
	// 未知分组 → 35064 失败文本。
	_, isErr = f.agent.Dispatch(context.Background(), "owner-1", ToolSearchScenarios,
		map[string]any{"group": "不存在"})
	if !isErr {
		t.Fatal("unknown group must fail")
	}
}

// TestListScenarios_DimensionIndex 无参 ⇒ 维度索引(批次84 §4.2)。
func TestListScenarios_DimensionIndex(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolListScenarios, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	// 维度索引:13 组各一行。
	for _, g := range virtual_city.ScenarioGroupOrder() {
		if !strings.Contains(text, g) {
			t.Errorf("dimension index missing group %q", g)
		}
	}
	// 体积恒定(不随目录条目膨胀)。
	if n := len([]rune(text)); n > 4000 {
		t.Errorf("dimension index too large: %d runes", n)
	}
	// 不含描述(描述只能经 search_scenarios / list_scenarios(group))。
	if strings.Contains(text, "城区建筑受损") {
		t.Error("dimension index must not contain descriptions")
	}
}

// TestListScenarios_Group 传 group ⇒ 该组条目;未知组 ⇒ 35064 失败文本。
func TestListScenarios_Group(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolListScenarios,
		map[string]any{"group": virtual_city.ScenarioGroupNature})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, virtual_city.ScenarioBlizzard) {
		t.Fatalf("group listing must contain %q, got %q", virtual_city.ScenarioBlizzard, text)
	}
	if !strings.Contains(text, "极端暴雪阻断物流干线") {
		t.Error("group listing must contain descriptions")
	}
	// 未知组 ⇒ 失败文本 + 35064。
	bad, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolListScenarios,
		map[string]any{"group": "不存在"})
	if !isErr {
		t.Fatal("unknown group must fail")
	}
	if !strings.Contains(bad, "35064") || !strings.Contains(bad, "未知分组") {
		t.Fatalf("unknown group text = %q, want 35064 + 未知分组", bad)
	}
}

// TestQueryMetricHistory 正:给出时间序列;负:非法 metric。
func TestQueryMetricHistory(t *testing.T) {
	f := newExtFixture(t)
	// 尚无历史采样(月结未完成)⇒ 明确「尚无历史」,不编造。
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryMetricHistory,
		map[string]any{"metric": "cpi", "months": 12})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, "cpi") {
		t.Errorf("metric history text = %q", text)
	}
	// 非法 metric ⇒ 失败。
	bad, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryMetricHistory,
		map[string]any{"metric": "gdp"})
	if !isErr || !strings.Contains(bad, "metric 只能是") {
		t.Fatalf("bad metric text = %q isErr=%v", bad, isErr)
	}
	// nil room ⇒ 失败。
	na := NewAgent(nil, Config{ModelKey: "m"}, &fakeRegistry{p: &fakeProvider{}},
		newTestHub(t), NewConversationStore(40), nil)
	if _, isErr := na.Dispatch(context.Background(), "u1", ToolQueryMetricHistory, map[string]any{}); !isErr {
		t.Fatal("nil room must fail")
	}
}

// TestQueryDistricts 正:返回城区目录;filter 命中单个;负:无匹配。
func TestQueryDistricts(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryDistricts, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, "finance") || !strings.Contains(text, "金融CBD") {
		t.Fatalf("district listing must contain finance/金融CBD, got %q", text)
	}
	// filter 命中单个。
	single, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryDistricts,
		map[string]any{"district": "finance"})
	if isErr {
		t.Fatalf("unexpected error: %s", single)
	}
	if strings.Contains(single, "suburb") {
		t.Errorf("filtered listing must not contain other districts, got %q", single)
	}
	// 无匹配 ⇒ 失败文本。
	bad, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryDistricts,
		map[string]any{"district": "no_such_district"})
	if !isErr || !strings.Contains(bad, "未找到匹配的城区") {
		t.Fatalf("no-match text = %q isErr=%v", bad, isErr)
	}
}

// TestQuerySurveys 正:返回调研列表;空房 ⇒ 明确「暂无」。
func TestQuerySurveys(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQuerySurveys, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, "暂无社会调研记录") {
		t.Fatalf("empty surveys text = %q", text)
	}
	// 发起一次调研后可查到。
	if _, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolLaunchSurvey,
		map[string]any{"question": "你满意吗?", "options": []any{"满意", "不满意"}}); isErr {
		t.Fatal("launch survey must succeed")
	}
	text, isErr = f.agent.Dispatch(context.Background(), "owner-1", ToolQuerySurveys, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, "你满意吗?") || !strings.Contains(text, "进行中") {
		t.Fatalf("surveys text = %q", text)
	}
}

// TestApplyDisplay_Valid apply_display 各分支:合法参数产出对应 type 的命令。
func TestApplyDisplay_Valid(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"weather", map[string]any{"weather": "rain"}, DisplaySetWeather},
		{"phase", map[string]any{"phase": "night"}, DisplaySetTimeOfDay},
		{"speed", map[string]any{"speed": "fast"}, DisplaySetTimeSpeed},
		{"layer", map[string]any{"layer": "trees", "visible": false}, DisplaySetLayerVisibility},
		{"entity", map[string]any{"entity_id": "b-1", "kind": "building"}, DisplayFocusEntity},
		{"preset", map[string]any{"preset": "bird"}, DisplayCameraPreset},
		{"flypath", map[string]any{"points": []any{
			map[string]any{"x": 1, "z": 2}, map[string]any{"x": 3, "z": 4},
		}}, DisplayFlyPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newExtFixture(t)
			text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolApplyDisplay, tc.input)
			if isErr {
				t.Fatalf("unexpected error: %s", text)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want command type %q", text, tc.want)
			}
			// show_command 事件下发给前端。
			if !hasEventKind(collectEvents(f.events), EventShowCommand) {
				t.Fatal("apply_display must emit show_command")
			}
		})
	}
}

// TestApplyDisplay_Invalid 非法参数 ⇒ isErr 失败文本(不转发、不产指令)。
func TestApplyDisplay_Invalid(t *testing.T) {
	f := newExtFixture(t)
	bigPoints := make([]any, flyPathMaxPoints+1)
	for i := range bigPoints {
		bigPoints[i] = map[string]any{"x": float64(i), "z": 0}
	}
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"unknown phase", map[string]any{"phase": "midnight"}, "phase 只能"},
		{"unknown weather", map[string]any{"weather": "sunny"}, "weather 只能"},
		{"unknown layer", map[string]any{"layer": "clouds", "visible": true}, "layer 只能"},
		{"unknown preset", map[string]any{"preset": "sky"}, "preset 只能"},
		{"points over 8", map[string]any{"points": bigPoints}, "点数须在 2-8 之间"},
		{"no params", map[string]any{}, "至少给出一个显示参数"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolApplyDisplay, tc.input)
			if !isErr {
				t.Fatalf("expected error, got %q", text)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want substring %q", text, tc.want)
			}
			// 失败时不得产指令。
			if hasEventKind(collectEvents(f.events), EventShowCommand) {
				t.Fatal("invalid apply_display must not emit show_command")
			}
		})
	}
}

// TestAskAgent_Reply 正:目标注册且回复 ⇒ 回灌回复内容。
func TestAskAgent_Reply(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolAskAgent,
		map[string]any{
			"target_agent": string(agentroot.AgentClassCityHuman),
			"message":      "最近过得怎么样?",
		})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, "居民:日子照过") {
		t.Fatalf("ask_agent reply = %q", text)
	}
}

// TestAskAgent_NotRegistered 目标未注册 ⇒ 回灌 35061 文本。
func TestAskAgent_NotRegistered(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolAskAgent,
		map[string]any{
			"target_agent": "LsmAgentGame-City-Nobody",
			"message":      "hello",
		})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "35061") || !strings.Contains(text, "agent not registered") {
		t.Fatalf("text = %q, want 35061 + agent not registered", text)
	}
}

// TestAskAgent_Timeout 目标注册但超时 ⇒ 回灌 35062 文本。
func TestAskAgent_Timeout(t *testing.T) {
	f := newExtFixture(t)
	f.agent.askTimeout = 50 * time.Millisecond // 单测注入更短的等待上限
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolAskAgent,
		map[string]any{
			"target_agent": string(agentroot.AgentClassCityHuman),
			"message":      "请慢一点回答",
		})
	if !isErr {
		t.Fatalf("expected timeout error, got %q", text)
	}
	if !strings.Contains(text, "35062") || !strings.Contains(text, "未在时限内回应") {
		t.Fatalf("text = %q, want 35062 + 未在时限内回应", text)
	}
}

// TestAskAgent_ReplyTooLong 回包超长 ⇒ 截断 + 35063。
func TestAskAgent_ReplyTooLong(t *testing.T) {
	f := newExtFixture(t)
	// 用一个专回长文本的目标。
	if err := f.agent.hub.RegisterRoom(f.room.RoomID, "LsmAgentGame-City-Chatty",
		func(context.Context, citybus.Message) (any, error) {
			return strings.Repeat("很长", 5000), nil
		}); err != nil {
		t.Fatalf("register chatty: %v", err)
	}
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolAskAgent,
		map[string]any{"target_agent": "LsmAgentGame-City-Chatty", "message": "说"})
	if !isErr {
		t.Fatalf("expected error for too-long reply, got %q", text)
	}
	if !strings.Contains(text, "35063") {
		t.Fatalf("text = %q, want 35063 marker", text)
	}
	// 截断后长度受 ToolResultLimit 约束(前缀 + 截断正文)。
	if len([]rune(text)) > f.agent.cfg.ToolResultLimit+200 {
		t.Errorf("truncated reply still too long: %d runes", len([]rune(text)))
	}
}

// TestAskAgent_SelfRejection 不得向自己提问(防回环)。
func TestAskAgent_SelfRejection(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolAskAgent,
		map[string]any{
			"target_agent": string(agentroot.AgentClassCitySystem),
			"message":      "自问自答",
		})
	if !isErr || !strings.Contains(text, "不能向自己") {
		t.Fatalf("text = %q isErr=%v", text, isErr)
	}
}

// TestListAgents_Extended list_agents 返回类级 + 房间级两类,且含描述。
func TestListAgents_Extended(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolListAgents, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	// 类级。
	if !strings.Contains(text, string(agentroot.AgentClassCityChrome)) {
		t.Errorf("missing class-level agent: %q", text)
	}
	if !strings.Contains(text, "类级") {
		t.Errorf("missing class-level marker: %q", text)
	}
	// 房间级。
	if !strings.Contains(text, agentClassCityWebGLShow) {
		t.Errorf("missing room-level agent: %q", text)
	}
	if !strings.Contains(text, "房间级") {
		t.Errorf("missing room-level marker: %q", text)
	}
	// 描述。
	if !strings.Contains(text, "采集真实世界行情与新闻") ||
		!strings.Contains(text, "渲染显示 Agent") {
		t.Errorf("missing capability descriptions: %q", text)
	}
	// 消息类型。
	if !strings.Contains(text, "run_display") || !strings.Contains(text, "collect") {
		t.Errorf("missing message types: %q", text)
	}
}

// TestSendAgentMessage_Post send_agent_message 走 Post(不等待回包)。
func TestSendAgentMessage_Post(t *testing.T) {
	f := newExtFixture(t)
	// 用一个**慢** handler:走 Send 会阻塞 2s;走 Post 立即返回。
	if err := f.agent.hub.RegisterRoom(f.room.RoomID, "LsmAgentGame-City-Slow",
		func(ctx context.Context, _ citybus.Message) (any, error) {
			time.Sleep(500 * time.Millisecond)
			return "慢回复", nil
		}); err != nil {
		t.Fatalf("register slow: %v", err)
	}
	start := time.Now()
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolSendAgentMessage,
		map[string]any{"target_agent": "LsmAgentGame-City-Slow", "message": "hi"})
	elapsed := time.Since(start)
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("send_agent_message blocked %v, want fire-and-forget (Post)", elapsed)
	}
	if !strings.Contains(text, "已向 LsmAgentGame-City-Slow 发送消息") {
		t.Fatalf("text = %q", text)
	}
	// 未注册目标 ⇒ 35061(Post 对未注册目标是静默丢弃,须先查注册表)。
	bad, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolSendAgentMessage,
		map[string]any{"target_agent": "LsmAgentGame-City-Nobody", "message": "hi"})
	if !isErr || !strings.Contains(bad, "35061") {
		t.Fatalf("bad target text = %q isErr=%v", bad, isErr)
	}
}

// TestApplyDisplay_ForwardsNormalizedRequest apply_display 转发给渲染 Agent
// 的 displayRequest 必须是**归一化后**的中性结构(枚举小写、数值已取)。
//
// 该断言独立于渲染侧的参数→type 映射(那是 citywebgl 的职责),只钉死
// citysystem 侧的转发契约:字段名 / 归一化 / 中性形状。
func TestApplyDisplay_ForwardsNormalizedRequest(t *testing.T) {
	room := newTestRoom(t, "R-fwd")
	a, events := newTestAgent(t, room, &fakeRegistry{p: &fakeProvider{}})
	var got displayRequest
	var gotType string
	if err := a.hub.RegisterRoom(room.RoomID, agentClassCityWebGLShow,
		func(_ context.Context, msg citybus.Message) (any, error) {
			gotType = msg.Type
			req, ok := msg.Payload.(displayRequest)
			if !ok {
				t.Errorf("payload type = %T, want displayRequest", msg.Payload)
				return []displayCommand{}, nil
			}
			got = req
			return []displayCommand{{Type: DisplaySetWeather, Weather: req.Weather}}, nil
		}); err != nil {
		t.Fatalf("register: %v", err)
	}
	hour := 21.0
	visible := false
	text, isErr := a.Dispatch(context.Background(), "owner-1", ToolApplyDisplay, map[string]any{
		"phase": "Night", "hour": hour, "weather": "Rain", "speed": "Fast",
		"layer": "Trees", "visible": visible, "preset": "Bird",
		"points": []any{map[string]any{"x": 1, "z": 2}, map[string]any{"x": 3, "z": 4}},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if gotType != "run_display" {
		t.Fatalf("bus message type = %q, want run_display", gotType)
	}
	// 枚举归一化为小写。
	for _, p := range []struct{ got, want string }{
		{got.Phase, "night"}, {got.Weather, "rain"}, {got.Speed, "fast"},
		{got.Layer, "trees"}, {got.Preset, "bird"},
	} {
		if p.got != p.want {
			t.Errorf("normalized field = %q, want %q", p.got, p.want)
		}
	}
	if got.Hour == nil || *got.Hour != 21.0 {
		t.Errorf("hour = %v, want 21.0", got.Hour)
	}
	if got.Visible == nil || *got.Visible != false {
		t.Errorf("visible = %v, want false", got.Visible)
	}
	if len(got.Points) != 2 || got.Points[1].X != 3 {
		t.Errorf("points = %+v", got.Points)
	}
	// 发出的 show_command 与渲染侧返回一致。
	if !hasEventKind(collectEvents(events), EventShowCommand) {
		t.Fatal("missing show_command event")
	}
}

// TestQueryCityState_AllCoversExtended scope=all 覆盖批次84 新增 7 个子系统
// (全维度口径)。
//
// ⚠ 批次85 §3.1.2 契约更新:environment / security **不再**答「子系统未启用」
// (那是批次84 的旧契约,批次85 已重写为「活动场景 + 世界事件 + 可得指标」);
// 本用例同步改钉新契约 —— 两 scope 给出实质指标且不含「子系统未启用」字样。
func TestQueryCityState_AllCoversExtended(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
		map[string]any{"scope": "all"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	for _, want := range []string{"城市状态", "基础设施", "公共卫生", "环境生态", "公共安全", "交通物流", "文化教育", "民生日常"} {
		if !strings.Contains(text, want) {
			t.Errorf("scope=all missing %q in %q", want, text)
		}
	}
	// 批次85:environment / security 不再「子系统未启用」,给出实质内容。
	if strings.Contains(text, "环境生态子系统未启用") {
		t.Errorf("environment must not report 子系统未启用 (batch85), got %q", text)
	}
	if strings.Contains(text, "公共安全(治安)子系统未启用") {
		t.Errorf("security must not report 子系统未启用 (batch85), got %q", text)
	}
	if !strings.Contains(text, "食品类商品价格指数") {
		t.Errorf("environment must give nearest indicator, got %q", text)
	}
	if !strings.Contains(text, "参考:市场监管(证券/反垄断/消协/隐私)累计罚款") {
		t.Errorf("security must keep regulator fines (zero regression), got %q", text)
	}
	// 观察闭环两段(global 口径;空态有明确回执)。
	if !strings.Contains(text, "全局进行中") && !strings.Contains(text, "当前该维度无进行中场景、无相关事件") {
		t.Errorf("scope=all must carry observation loop, got %q", text)
	}
}

// TestQueryCityState_ExtendedScope 单个扩展 scope 也可查。
func TestQueryCityState_ExtendedScope(t *testing.T) {
	f := newExtFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
		map[string]any{"scope": "public_health"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, "公共卫生") {
		t.Fatalf("text = %q", text)
	}
}
