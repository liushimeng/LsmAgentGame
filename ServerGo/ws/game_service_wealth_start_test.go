package ws

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/game/wealth"
	llmtypes "LsmAgentGame/llm/types"
)

type firstMonthWealthProvider struct {
	called chan struct{}
}

func (p *firstMonthWealthProvider) Chat(context.Context, string, llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	select {
	case p.called <- struct{}{}:
	default:
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "首月决策完成"}},
	}, nil
}

func (p *firstMonthWealthProvider) ChatStream(context.Context, string, llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (p *firstMonthWealthProvider) ProviderType() string { return "fake-first-month" }

type firstMonthWealthRegistry struct {
	provider llmtypes.LLMProvider
}

func (r firstMonthWealthRegistry) Get(string) (llmtypes.LLMProvider, string, error) {
	return r.provider, "fake-key", nil
}

func (r firstMonthWealthRegistry) GetThinkingEnabled(string) (bool, int) { return false, 0 }

// TestStartWealthRoom_EnsureAgentsBeforeStart 验证真实 ws 开局链路:
// agents 必须先装配,Start() 末尾 wakeBots 才能触发首月 LLM 调用。
// 旧顺序(Start → EnsureAgents)首月 wake 时 agents map 为空,要等 3s 月结
// 后才会补跑;本测试在首月窗口内等待 LLM 调用,可捕获该时序回退。
func TestStartWealthRoom_EnsureAgentsBeforeStart(t *testing.T) {
	provider := &firstMonthWealthProvider{called: make(chan struct{}, 1)}
	manager := wealth.NewManager(wealth.Config{
		MonthMs:                 3000,
		PoolDefault:             "curated",
		AgentEnabled:            true,
		AgentDecisionTimeoutSec: 5,
		AgentConcurrency:        wealth.DefaultAgentConcurrency,
	}, firstMonthWealthRegistry{provider: provider})
	room := manager.CreateRoom("room-ws-first-month")
	botUsers := make(map[int]string, wealth.MinSeats)
	botModels := make(map[int]string, wealth.MinSeats)
	for seat := 0; seat < wealth.MinSeats; seat++ {
		botUsers[seat] = "ws-bot-" + string(rune('0'+seat))
		botModels[seat] = "WSModel"
	}
	room.RegisterBotSeats(botUsers, botModels, nil)
	t.Cleanup(room.Close)

	gameService := &GameService{hub: NewHub(), wealthMgr: manager}
	if err := gameService.startWealthRoom(room.RoomID); err != nil {
		t.Fatalf("startWealthRoom: %v", err)
	}
	if room.GetStatus() != wealth.StatusPlaying {
		t.Fatalf("room status = %q, want playing", room.GetStatus())
	}

	select {
	case <-provider.called:
	case <-time.After(time.Second):
		t.Fatal("first-month agent was not woken before Start returned; EnsureAgents ordering regressed")
	}
}
