package ws

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/game/virtual_city"
	llmtypes "LsmAgentGame/llm/types"
)

type firstMonthVirtualCityProvider struct {
	called chan struct{}
}

func (p *firstMonthVirtualCityProvider) Chat(context.Context, string, llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	select {
	case p.called <- struct{}{}:
	default:
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "首月决策完成"}},
	}, nil
}

func (p *firstMonthVirtualCityProvider) ChatStream(context.Context, string, llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (p *firstMonthVirtualCityProvider) ProviderType() string { return "fake-first-month" }

type firstMonthVirtualCityRegistry struct {
	provider llmtypes.LLMProvider
}

func (r firstMonthVirtualCityRegistry) Get(string) (llmtypes.LLMProvider, string, error) {
	return r.provider, "fake-key", nil
}

func (r firstMonthVirtualCityRegistry) GetThinkingEnabled(string) (bool, int) { return false, 0 }

// TestStartVirtualCityRoom_EnsureAgentsBeforeStart 验证真实 ws 开局链路:
// agents 必须先装配,Start() 末尾 wakeBots 才能触发首月 LLM 调用。
// 旧顺序(Start → EnsureAgents)首月 wake 时 agents map 为空,要等 3s 月结
// 后才会补跑;本测试在首月窗口内等待 LLM 调用,可捕获该时序回退。
func TestStartVirtualCityRoom_EnsureAgentsBeforeStart(t *testing.T) {
	provider := &firstMonthVirtualCityProvider{called: make(chan struct{}, 1)}
	manager := virtual_city.NewManager(virtual_city.Config{
		MonthMs:                 3000,
		AgentEnabled:            true,
		AgentDecisionTimeoutSec: 5,
		AgentConcurrency:        virtual_city.DefaultAgentConcurrency,
	}, firstMonthVirtualCityRegistry{provider: provider})
	room := manager.CreateRoom("room-ws-first-month")
	botUsers := make(map[int]string, virtual_city.MinSeats)
	botModels := make(map[int]string, virtual_city.MinSeats)
	for seat := 0; seat < virtual_city.MinSeats; seat++ {
		botUsers[seat] = "ws-bot-" + string(rune('0'+seat))
		botModels[seat] = "WSModel"
	}
	room.RegisterBotSeats(botUsers, botModels)
	t.Cleanup(room.Close)

	gameService := &GameService{hub: NewHub(), vcMgr: manager}
	if err := gameService.startVirtualCityRoom(room.RoomID); err != nil {
		t.Fatalf("startVirtualCityRoom: %v", err)
	}
	if room.GetStatus() != virtual_city.StatusPlaying {
		t.Fatalf("room status = %q, want playing", room.GetStatus())
	}

	select {
	case <-provider.called:
	case <-time.After(time.Second):
		t.Fatal("first-month agent was not woken before Start returned; EnsureAgents ordering regressed")
	}
}
