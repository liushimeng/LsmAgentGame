package ws

import (
	"testing"

	"LsmAgentGame/models"
)

// Debug-2026-08-12-01 regression test — chat.history pagination cursor contract.
//
// The client's infinite-scroll relies on three invariants that the server must
// uphold (see buildHistoryPayload's doc comment). A bug in any of them turned
// the spectator's room-chat panel into a permanent "正在加载历史消息…" spinner:
//
//   P0-1  next_cursor used to be msgs[len-1].id (the NEWEST row) instead of
//         msgs[0].id (the OLDEST). Because History() reverses the DESC query
//         into an ASCENDING slice, the old code advanced only one row per page
//         and has_more never went false.
//   P0-2  the response never echoed `before_id`, so the client could not tell a
//         "latest N" fetch (replace) from a keyset page load (prepend) — the
//         prepend branch was unreachable dead code.
//
// These tests exercise buildHistoryPayload directly (no DB needed) and assert
// the contract. They are the "双向验证" (defect → fail → fix → pass) guard.

func makeMsgs(ids ...uint64) []ChatMessage {
	out := make([]ChatMessage, 0, len(ids))
	for _, id := range ids {
		out = append(out, ChatMessage{ID: id})
	}
	return out
}

// ptrVal dereferences a *uint64 payload field, reporting whether it was a
// non-nil pointer. The payload carries pointers so encoding/json renders an
// absent cursor as JSON null rather than 0.
func ptrVal(v any) (uint64, bool) {
	p, ok := v.(*uint64)
	if !ok || p == nil {
		return 0, false
	}
	return *p, true
}

// isNilCursor reports whether a payload field marshals to JSON null — either an
// untyped nil or a typed nil *uint64.
func isNilCursor(v any) bool {
	if v == nil {
		return true
	}
	p, ok := v.(*uint64)
	return ok && p == nil
}

// T01: next_cursor must be the OLDEST id (first element of the ascending page).
func TestHistoryCursor_OldestIsNextCursor(t *testing.T) {
	msgs := makeMsgs(71, 72, 73, 120) // ascending: oldest=71, newest=120
	p := buildHistoryPayload("room", "r1", 121, msgs, true)

	cur, ok := ptrVal(p["next_cursor"])
	if !ok {
		t.Fatalf("next_cursor missing or nil: %#v", p["next_cursor"])
	}
	if cur != 71 {
		t.Fatalf("next_cursor = %d, want 71 (oldest). Taking the newest (120) is the P0-1 bug.", cur)
	}
}

// T02: an empty page yields a null next_cursor.
func TestHistoryCursor_EmptyPageNullCursor(t *testing.T) {
	p := buildHistoryPayload("room", "r1", 100, nil, false)
	if !isNilCursor(p["next_cursor"]) {
		t.Fatalf("next_cursor on empty page = %#v, want null", p["next_cursor"])
	}
}

// T03: before_id echo — a keyset request must round-trip its before_id.
func TestHistoryCursor_EchoesBeforeID(t *testing.T) {
	msgs := makeMsgs(20, 21, 22)
	p := buildHistoryPayload("room", "r1", 50, msgs, true)

	echo, ok := p["before_id"].(*uint64)
	if !ok || echo == nil {
		t.Fatalf("before_id echo missing: %#v (P0-2 bug — client cannot distinguish fetch kinds)", p["before_id"])
	}
	if *echo != 50 {
		t.Fatalf("before_id echo = %d, want 50", *echo)
	}
}

// T04: a "latest N" request (no before_id) must echo null before_id.
func TestHistoryCursor_NoBeforeIDRequestEchoesNull(t *testing.T) {
	msgs := makeMsgs(90, 91, 92)
	p := buildHistoryPayload("room", "r1", 0, msgs, false)

	if !isNilCursor(p["before_id"]) {
		t.Fatalf("before_id on latest-N request = %#v, want null", p["before_id"])
	}
}

// T05: end-to-end paging simulation — cursors must strictly decrease by ~limit
// and terminate. This is the load-bearing invariant: a 120-message room with
// limit=50 must reach has_more=false in 3 pages, with no duplicate ids.
func TestHistoryCursor_PagingSimulation_TerminatesNoDuplicates(t *testing.T) {
	const total = 120
	const limit = 50

	// Simulate the server's DESC query + ASC reversal for a given before_id.
	page := func(before uint64) (msgs []ChatMessage, hasMore bool) {
		// DESC ids below `before` (or all, if before==0), capped at limit.
		hi := before
		if hi == 0 {
			hi = total + 1
		}
		desc := []uint64{}
		for id := hi - 1; id >= 1 && len(desc) < limit; id-- {
			desc = append(desc, id)
		}
		// Reverse to ascending (what History() returns).
		for i := len(desc) - 1; i >= 0; i-- {
			msgs = append(msgs, ChatMessage{ID: desc[i]})
		}
		// has_more == full page (server heuristic).
		hasMore = len(desc) == limit
		return
	}

	seen := map[uint64]bool{}
	before := uint64(0)
	pages := 0
	for {
		msgs, hasMore := page(before)
		p := buildHistoryPayload("room", "r1", before, msgs, hasMore)

		// Every returned id must be unseen (no duplicates across pages).
		for _, m := range msgs {
			if seen[m.ID] {
				t.Fatalf("id %d returned on page %d was already seen — cursor did not advance", m.ID, pages)
			}
			seen[m.ID] = true
		}

		cur, _ := ptrVal(p["next_cursor"])
		if len(msgs) > 0 && cur >= before && before > 0 {
			t.Fatalf("cursor did not advance: before=%d next_cursor=%d (P0-1 bug)", before, cur)
		}

		if !hasMore {
			break
		}
		before = cur
		pages++
		if pages > total { // safety: must terminate well before this
			t.Fatalf("paging did not terminate after %d pages — infinite loop (P0-1 bug)", pages)
		}
	}

	if len(seen) != total {
		t.Fatalf("expected to page through all %d messages, saw %d", total, len(seen))
	}
	if pages != 2 { // 120/50 = 2 full pages + 1 partial = 3 fetches = 2 advances
		t.Fatalf("expected 2 cursor advances for 120 msgs @ limit 50, got %d", pages)
	}
}

// T06: the response must always carry the before_id key (even when null) so the
// client's `p.before_id === undefined` check is meaningful. A missing key and a
// null value are NOT equivalent in JSON unmarshalling.
func TestHistoryCursor_BeforeIDKeyAlwaysPresent(t *testing.T) {
	p := buildHistoryPayload("room", "r1", 0, nil, false)
	if _, ok := p["before_id"]; !ok {
		t.Fatalf("before_id key absent from payload — client cannot branch on it")
	}
}

// Silence an unused-import warning if models is not referenced elsewhere.
var _ = models.TLsmGameChatMessage{}
