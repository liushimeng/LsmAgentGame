package wwjudge

import "fmt"

// BuildJudgeMetadataUserID produces the stringified JSON `metadata.user_id` blob
// for an outbound LLM call made by the Werewolf judge (主持人/法官). Mirrors
// `buildMetadataUserID` (regular bot) on the wire so upstream proxies that
// expect ClaudeCode's exact field layout still recognize the traffic.
//
// Layout:
//
//	{ "device_id":"bot:room-<room-id>:role-judge",
//	  "account_uuid":"<model-key>",
//	  "session_id":"<room-id>:judge" }
//
// Differences vs the regular bot builder:
//   - `device_id` uses the sentinel `:role-judge` instead of `:seat-<N>`
//     because the judge is not a seat occupant (AgentJudge has no Seat
//     field; chat inserts use FromSeat=-1).
//   - `account_uuid` is the room-stable judge model key rather than a
//     bot user id — the judge is a single per-room actor, so the model
//     key is the most stable identifier we have for traffic attribution.
//   - `session_id` reuses the `<room-id>:<role>` shape, with `:judge`
//     as the role suffix, so a room's bot traffic and judge traffic
//     share the same room-scoped session key prefix.
//
// The Anthropic API caps user_id at 256 chars; the layout above is well under
// that limit even for long room IDs / model keys.
//
// Exported because `game/werewolf/judge_summary_bridge.go` (整局总结 LLM
// 调用) 在包外调用,需要构造同样的 blob;同包的 `judge.go` 也调用。
func BuildJudgeMetadataUserID(roomID, modelKey string) string {
	blob := fmt.Sprintf(`{"device_id":%q,"account_uuid":%q,"session_id":%q}`,
		fmt.Sprintf("bot:room-%s:role-judge", roomID),
		modelKey,
		fmt.Sprintf("%s:judge", roomID),
	)
	if len(blob) > 256 {
		// Defensive cap — should never trigger in practice.
		return blob[:256]
	}
	return blob
}
