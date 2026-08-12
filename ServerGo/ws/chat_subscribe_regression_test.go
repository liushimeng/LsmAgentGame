package ws

// BUG-R244-P1-01 regression test (2026-08-06):
//   Human public chat silently dropped when the frontend skips `game.join`
//   in favor of an idempotent `game.state` polling (R210-05 optimization).
//   Without an explicit SubscribeRoom call, the client never enters
//   h.rooms[roomID], and BroadcastRoomIncludingSpectators iterates an empty
//   set, dropping the human's chat.message broadcast.
//
//   Fix: handleGetState (game_service_xiangqi.go) now calls SubscribeRoom
//   idempotently before responding to game.state. This test verifies the
//   hub-side invariant: after SubscribeRoom, BroadcastRoomIncludingSpectators
//   must deliver the envelope to the subscribed client.

import (
	"encoding/json"
	"testing"
)

// TestBugR244P1_01_HubSubscribeDeliversChat ensures that a client added via
// SubscribeRoom actually receives the chat fan-out. This guards against the
// R244 regression where the frontend's "isLikelyMember → requestState"
// fast path skipped game.join entirely, leaving the client out of
// h.rooms[roomID] and silently dropping their chat messages.
func TestBugR244P1_01_HubSubscribeDeliversChat(t *testing.T) {
	hub := NewHub()
	roomID := "r244-p1-01"

	// Simulate a human client that just connected via chat.subscribe (the
	// real-world R244 path: WerewolfGamePage's isLikelyMember branch sends
	// only chat.subscribe and requestState, never game.join).
	humanClient := &Client{
		UserID: "human_test_01",
		send:   make(chan Envelope, 16),
	}
	hub.SubscribeRoom(roomID, humanClient)

	// Bot was added earlier via game.join (the normal path).
	botClient := &Client{
		UserID: "bot_3",
		send:   make(chan Envelope, 16),
	}
	hub.SubscribeRoom(roomID, botClient)

	// Simulate bot sending chat (SendFromBot path); human should receive.
	payload, _ := json.Marshal(map[string]any{
		"id":         uint64(1),
		"scope":      "room",
		"room_id":    roomID,
		"from_user_id": "bot_3",
		"from_account": "Bot #3",
		"from_role":  "bot",
		"text":       "hello from bot",
		"ts":         int64(1700000000000),
	})
	env := Envelope{Type: "chat.message", Payload: payload}
	hub.BroadcastRoomIncludingSpectators(roomID, env)

	// Both clients must receive.
	for _, c := range []*Client{humanClient, botClient} {
		select {
		case got := <-c.send:
			if got.Type != "chat.message" {
				t.Errorf("[BUG-R244-P1-01] %s: expected chat.message, got %q",
					c.UserID, got.Type)
			}
		default:
			t.Errorf("[BUG-R244-P1-01] %s: did not receive chat fan-out "+
				"(client not in h.rooms[roomID])", c.UserID)
		}
	}
}

// TestBugR244P1_01_NoSubscribeDropsChat documents the pre-fix failure mode:
// without SubscribeRoom, BroadcastRoomIncludingSpectators iterates an empty
// set and the envelope is silently dropped.
func TestBugR244P1_01_NoSubscribeDropsChat(t *testing.T) {
	hub := NewHub()
	roomID := "r244-p1-01-no-sub"

	// Client deliberately NOT subscribed (simulating the pre-fix R244 state
	// where WerewolfGamePage's isLikelyMember branch skipped game.join).
	orphanClient := &Client{
		UserID: "orphan_test_01",
		send:   make(chan Envelope, 16),
	}

	// Send a chat envelope without ever subscribing.
	payload, _ := json.Marshal(map[string]any{
		"id":           uint64(1),
		"scope":        "room",
		"room_id":      roomID,
		"from_user_id": "orphan_test_01",
		"from_account": "test_01",
		"text":         "anyone there?",
		"ts":           int64(1700000000000),
	})
	env := Envelope{Type: "chat.message", Payload: payload}
	hub.BroadcastRoomIncludingSpectators(roomID, env)

	// The orphan client must NOT receive anything (this is the documented
	// bug: a human chat was silently dropped because their WS connection
	// was never registered in h.rooms[roomID]).
	select {
	case got := <-orphanClient.send:
		t.Errorf("[BUG-R244-P1-01] orphan unexpectedly received: %q "+
			"(this is the failure mode the handleGetState fix prevents)",
			got.Type)
	default:
		// Expected: empty set, no fan-out.
	}
}

// TestBugR244P1_01_SubscribeIsIdempotent confirms that repeated
// SubscribeRoom calls are safe — the handleGetState fix relies on this
// because both game.join AND game.state now call SubscribeRoom.
func TestBugR244P1_01_SubscribeIsIdempotent(t *testing.T) {
	hub := NewHub()
	roomID := "r244-p1-01-idem"

	c := &Client{
		UserID: "test_01",
		send:   make(chan Envelope, 16),
	}

	// Subscribe many times — should not panic or duplicate.
	for i := 0; i < 5; i++ {
		hub.SubscribeRoom(roomID, c)
	}

	// The set should contain exactly one entry for this client.
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	set, ok := hub.rooms[roomID]
	if !ok {
		t.Fatalf("expected room set to exist")
	}
	if len(set) != 1 {
		t.Errorf("expected 1 entry after idempotent subscribes, got %d", len(set))
	}
}