package ws

// hub_broadcast_dedup_test.go — 广播去重回归测试(2026-11-03)。
//
// 背景缺陷:客户端经 game.spectate 注册进 h.spectators 后,handleGetState
// (game_service_xiangqi.go) 的无条件 SubscribeRoom 又把它放进 h.rooms。
// BroadcastRoomIncludingSpectators / …Excluding 原先分别遍历两个集合并各
// 发一次 ⇒ 同一连接每帧收两份(流式文本重复成 "TheThe user…",同一
// tool_start 双投递渲染出两张工具卡)。虚拟城市观战页(spectate + 每 8s
// requestState 轮询)必命中。
//
// 本文件锁定三条不变量:
//  1. 双注册(rooms ∩ spectators)客户端每次广播只收 1 帧;
//  2. 只在 rooms / 只在 spectators 的客户端各收 1 帧;
//  3. 并发广播(-race)不 panic、不丢帧。
//
// 注:drainFrames 复用 chat_service_judge_qa_test.go 的同包 helper。

import (
	"fmt"
	"sync"
	"testing"
)

// newDedupTestClient 构造一个可直接接收广播的 fake Client。
func newDedupTestClient(userID string, buf int) *Client {
	return &Client{UserID: userID, send: make(chan Envelope, buf)}
}

// TestBroadcastRoomIncludingSpectators_DedupDualRegisteredClient — 同一 *Client
// 同时在 rooms 与 spectators(handleGetState 自动订阅 + game.spectate 的真实
// 叠加形态)时,BroadcastRoomIncludingSpectators 每帧只投递一次;纯玩家 /
// 纯观战者各收一次。
func TestBroadcastRoomIncludingSpectators_DedupDualRegisteredClient(t *testing.T) {
	hub := NewHub()
	roomID := "r-dedup-1"

	dual := newDedupTestClient("u_dual", 8)
	roomOnly := newDedupTestClient("u_room", 8)
	specOnly := newDedupTestClient("u_spec", 8)

	// 双注册:先 spectate,再被 handleGetState 的 SubscribeRoom 拉进 rooms。
	hub.SpectateRoom(roomID, dual)
	hub.SubscribeRoom(roomID, dual)
	hub.SubscribeRoom(roomID, roomOnly)
	hub.SpectateRoom(roomID, specOnly)

	env := Envelope{Type: "game.virtual_city_system_event"}
	hub.BroadcastRoomIncludingSpectators(roomID, env)

	for _, tc := range []struct {
		name string
		c    *Client
		want int
	}{
		{"dual", dual, 1},
		{"roomOnly", roomOnly, 1},
		{"specOnly", specOnly, 1},
	} {
		got := len(drainFrames(tc.c))
		if got != tc.want {
			t.Fatalf("%s: received %d frames, want %d", tc.name, got, tc.want)
		}
	}
}

// TestBroadcastRoomIncludingSpectatorsExcluding_DedupAndExclude — Excluding
// 变体同样按 *Client 去重,且被排除的 userID(双注册也一样)一帧不收。
func TestBroadcastRoomIncludingSpectatorsExcluding_DedupAndExclude(t *testing.T) {
	hub := NewHub()
	roomID := "r-dedup-2"

	dual := newDedupTestClient("u_dual", 8)
	excludedDual := newDedupTestClient("u_excluded", 8)
	specOnly := newDedupTestClient("u_spec", 8)

	hub.SpectateRoom(roomID, dual)
	hub.SubscribeRoom(roomID, dual)
	hub.SpectateRoom(roomID, excludedDual)
	hub.SubscribeRoom(roomID, excludedDual)
	hub.SpectateRoom(roomID, specOnly)

	env := Envelope{Type: "chat.message"}
	hub.BroadcastRoomIncludingSpectatorsExcluding(roomID, env, "u_excluded", "sender-not-here")

	for _, tc := range []struct {
		name string
		c    *Client
		want int
	}{
		{"dual", dual, 1},
		{"excludedDual", excludedDual, 0},
		{"specOnly", specOnly, 1},
	} {
		got := len(drainFrames(tc.c))
		if got != tc.want {
			t.Fatalf("%s: received %d frames, want %d", tc.name, got, tc.want)
		}
	}

	// 无排除参数时退化为普通含观战者广播,双注册客户端仍只收 1 帧。
	hub.BroadcastRoomIncludingSpectatorsExcluding(roomID, env)
	if got := len(drainFrames(dual)); got != 1 {
		t.Fatalf("no-exclude fallback: dual received %d frames, want 1", got)
	}
}

// TestBroadcastRoomIncludingSpectators_ConcurrentNoLoss — 并发广播 + 并发
// 订阅/退订(-race 下运行)不得 panic;稳定注册的客户端不丢帧、不重复。
func TestBroadcastRoomIncludingSpectators_ConcurrentNoLoss(t *testing.T) {
	hub := NewHub()
	roomID := "r-dedup-3"

	const (
		broadcasters   = 8
		perBroadcaster = 50
		total          = broadcasters * perBroadcaster
	)

	// 缓冲开满所有帧,保证"default 丢帧"分支不会触发 ⇒ 计数精确。
	dual := newDedupTestClient("u_dual", total)
	roomOnly := newDedupTestClient("u_room", total)
	specOnly := newDedupTestClient("u_spec", total)

	hub.SpectateRoom(roomID, dual)
	hub.SubscribeRoom(roomID, dual)
	hub.SubscribeRoom(roomID, roomOnly)
	hub.SpectateRoom(roomID, specOnly)

	var wg sync.WaitGroup
	// 并发广播:混合两种入口,和真实业务(chat / game.event / system_event)一致。
	for i := 0; i < broadcasters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			env := Envelope{Type: fmt.Sprintf("game.event-%d", i)}
			for n := 0; n < perBroadcaster; n++ {
				if i%2 == 0 {
					hub.BroadcastRoomIncludingSpectators(roomID, env)
				} else {
					hub.BroadcastRoomIncludingSpectatorsExcluding(roomID, env, "nobody")
				}
			}
		}(i)
	}
	// 并发订阅/退订一把"搅动"客户端,压 RWMutex 竞争面(其帧不计数)。
	wg.Add(1)
	go func() {
		defer wg.Done()
		churn := newDedupTestClient("u_churn", 4)
		for n := 0; n < total; n++ {
			hub.SubscribeRoom(roomID, churn)
			hub.UnsubscribeRoom(roomID, churn)
		}
	}()
	wg.Wait()

	for _, tc := range []struct {
		name string
		c    *Client
	}{
		{"dual", dual},
		{"roomOnly", roomOnly},
		{"specOnly", specOnly},
	} {
		if got := len(drainFrames(tc.c)); got != total {
			t.Fatalf("%s: received %d frames, want %d (no loss, no dup)", tc.name, got, total)
		}
	}
}
