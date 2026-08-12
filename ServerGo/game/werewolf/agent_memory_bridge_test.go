// Package werewolf — agent_memory_bridge 轻量单测(2026-07-20 §131)。
//
// 覆盖设计文档 §9 的 no-op 路径:
//   - store=nil 时 IterateAgentMemoriesAsync no-op 且不 panic;
//   - agent_memory_enabled=false(测试环境无 config,cfgAgentMemoryEnabled
//     兜底 false)时 no-op;
//   - 空房间 / 无 bot 座位时 no-op。
//
// 注意:werewolf 测试常见 config.Load() panic(无配置文件环境),
// cfgAgentMemoryEnabled / cfgAgentMemoryMaxTokens 已用 defer recover 兜底,
// 与 §122 / cfgWerewolfCoolingSec 等现有测试安全模式一致。
package werewolf

import (
	"context"
	"testing"
)

// stubAgentMemoryStore 记录调用次数,用于断言 no-op 路径完全不触 DB。
type stubAgentMemoryStore struct {
	loadCalls int
	saveCalls int
}

func (s *stubAgentMemoryStore) Load(ctx context.Context, modelKey string) (string, error) {
	s.loadCalls++
	return "", nil
}

func (s *stubAgentMemoryStore) SaveIterated(ctx context.Context, modelKey, newMD, gameID string) error {
	s.saveCalls++
	return nil
}

func TestIterateAgentMemoriesAsync_NilStoreNoop(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{RoomID: "test-room-nil-store"}
	// store=nil → no-op,不 panic。
	m.IterateAgentMemoriesAsync(r, "法官总结")
}

func TestIterateAgentMemoriesAsync_ConfigDisabledNoop(t *testing.T) {
	m := NewWerewolfManager()
	stub := &stubAgentMemoryStore{}
	m.SetAgentMemoryStore(stub)
	r := &WerewolfRoom{RoomID: "test-room-cfg-off"}
	// 测试环境无 LsmAgentGame.conf → cfgAgentMemoryEnabled recover 兜底 false → no-op。
	m.IterateAgentMemoriesAsync(r, "法官总结")
	if stub.loadCalls != 0 || stub.saveCalls != 0 {
		t.Fatalf("config-disabled path must not touch store, got load=%d save=%d",
			stub.loadCalls, stub.saveCalls)
	}
}

func TestIterateAgentMemoriesAsync_NilRoomNoop(t *testing.T) {
	m := NewWerewolfManager()
	stub := &stubAgentMemoryStore{}
	m.SetAgentMemoryStore(stub)
	// nil room → no-op,不 panic。
	m.IterateAgentMemoriesAsync(nil, "法官总结")
	if stub.loadCalls != 0 || stub.saveCalls != 0 {
		t.Fatalf("nil-room path must not touch store, got load=%d save=%d",
			stub.loadCalls, stub.saveCalls)
	}
}

func TestIterateOneModelMemory_RecoverOnStorePanic(t *testing.T) {
	// 即使 store 实现 panic,goroutine 入口 defer recover 也必须兜住,不外溢。
	m := NewWerewolfManager()
	pStore := &panicAgentMemoryStore{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 直接调用(不经 IterateAgentMemoriesAsync 的开关守卫),
		// 验证 recover 兜底;registry=nil 时应在 store.Load 前/后都不 panic 外溢。
		m.iterateOneModelMemory(&WerewolfRoom{RoomID: "panic-room"}, "panic-room", "X-model", 0, "summary", pStore, nil)
	}()
	<-done
}

type panicAgentMemoryStore struct{}

func (p *panicAgentMemoryStore) Load(ctx context.Context, modelKey string) (string, error) {
	panic("boom")
}
func (p *panicAgentMemoryStore) SaveIterated(ctx context.Context, modelKey, newMD, gameID string) error {
	return nil
}
