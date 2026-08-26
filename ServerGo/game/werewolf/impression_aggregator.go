// Package werewolf — impression_aggregator.go: 狼人杀 13 人局 Agent 印象自动聚合（§20260826-01 U3）。
//
// 设计动机（M3 §5 P1 / DeepSeek §一.5）：
//   - Agent 不需要主动调「更新印象」工具，服务端在每个事件后**自动聚合**。
//   - 5 个事件触发点（E1~E5）见 §三。
//   - 纯计算（不调 LLM），保证零 LLM 调用增量。
//
// §92a：所有 aggregator 方法 *Locked 语义；调用方必须已持 r.mu。
// §119 协议层隔离：聚合事件不进 chat_message / chat_history。
package werewolf

import (
	"time"
)

// ImpressionEventKind 5 类印象事件枚举。
type ImpressionEventKind string

const (
	ImpressionEventSpeechMysteryHit ImpressionEventKind = "speech_mystery_hit" // E1
	ImpressionEventVoteAccurate     ImpressionEventKind = "vote_accurate"      // E2
	ImpressionEventChallenged       ImpressionEventKind = "challenged"          // E3
	ImpressionEventFollowVote       ImpressionEventKind = "follow_vote"         // E4
	ImpressionEventFramedByOther    ImpressionEventKind = "framed_by_other"     // E5
)

// AggregatedImpressionDelta 是 5 类事件的预设 delta 修正量。
//
// 算法：event_weight = 0.05~0.15（按事件重要性），emotion_factor
// 由调用方根据当前 bot 情绪乘上去。
var aggregatedImpressionDelta = map[ImpressionEventKind]ImpressionDims{
	// E1: 发言被 MysteryMaskText 命中 → 听者对说者:Threat+, Sincerity-
	ImpressionEventSpeechMysteryHit: {
		Trust:       -0.02,
		Sincerity:   -0.05,
		Threat:      +0.05,
		Competence:  0,
		Cooperation: 0,
	},
	// E2: 成功指认某玩家为狼人 → 指认者对目标:Cooperation+, Competence+
	ImpressionEventVoteAccurate: {
		Trust:       +0.03,
		Competence:  +0.10,
		Cooperation: +0.10,
		Sincerity:   0,
		Threat:      0,
	},
	// E3: 被公开质疑 → 被质疑者对质疑者:Sincerity-, Threat+
	ImpressionEventChallenged: {
		Sincerity: -0.05,
		Threat:    +0.05,
	},
	// E4: 跟票（同阵营玩家一致投票）→ 跟票者对被跟票者:Cooperation+
	ImpressionEventFollowVote: {
		Cooperation: +0.05,
		Trust:       +0.02,
	},
	// E5: 被嫁祸（其他 bot 调 frame_player 指向自己）→ 被嫁祸者对嫁祸者:Threat+
	ImpressionEventFramedByOther: {
		Threat:      +0.15,
		Sincerity:   -0.10,
		Cooperation: -0.05,
	},
}

// AggregateImpressionFromEventLocked 是 5 类事件的总入口。
//
// 调用前置：必须已持 r.mu。
//
// 参数：
//   - observerSeat: 观察者 bot 座位（谁的印象要更新）
//   - targetSeat:   被观察玩家座位（印象的对象）
//   - kind:         事件类型
//   - emotionFactor: 当前 observer 情绪因子（wary→0.7x, guilty→0.5x, excited→1.3x, 默认 1.0）
//   - now:          时间
//
// 副作用：impressionStore.AddOrUpdateDimLocked 已聚合好的 delta。
func (r *WerewolfRoom) AggregateImpressionFromEventLocked(
	observerSeat, targetSeat int,
	kind ImpressionEventKind,
	emotionFactor float32,
	now time.Time,
) {
	if r == nil {
		return
	}
	if observerSeat < 0 || observerSeat >= MaxPlayers {
		return
	}
	if targetSeat < 0 || targetSeat >= MaxPlayers || observerSeat == targetSeat {
		return
	}
	baseDelta, ok := aggregatedImpressionDelta[kind]
	if !ok {
		return
	}
	// 应用 emotion_factor 缩放
	scaled := ImpressionDims{
		Trust:       baseDelta.Trust * emotionFactor,
		Competence:  baseDelta.Competence * emotionFactor,
		Sincerity:   baseDelta.Sincerity * emotionFactor,
		Cooperation: baseDelta.Cooperation * emotionFactor,
		Threat:      baseDelta.Threat * emotionFactor,
	}
	// 死亡玩家不更新印象（避免对死人继续调权）
	if r.State != nil && targetSeat < len(r.State.Players) && !r.State.Players[targetSeat].Alive {
		return
	}
	store := r.impressionStoreLocked()
	store.AddOrUpdateDimLocked(observerSeat, targetSeat, scaled, string(kind), now)
}

// EmitImpressionOnSpeechMysteryHitLocked 在 speak_mystery 命中时调。
//
// 调用前置：必须已持 r.mu（典型:ScrubIdentityLeak 的 post-process 路径）。
func (r *WerewolfRoom) EmitImpressionOnSpeechMysteryHitLocked(speakerSeat, listenerSeat int, now time.Time) {
	if r == nil || speakerSeat == listenerSeat {
		return
	}
	emotionFactor := float32(1.0)
	// 仅对 bot listener 起效（人类不需要）
	if r.State != nil && listenerSeat < len(r.State.Players) && r.State.Players[listenerSeat].IsBot {
		r.AggregateImpressionFromEventLocked(listenerSeat, speakerSeat, ImpressionEventSpeechMysteryHit, emotionFactor, now)
	}
}

// EmitImpressionOnVoteAccurateLocked 投票命中狼人时调（voter→voted狼）。
//
// 调用前置：必须已持 r.mu（典型:FinishVote 末尾）。
func (r *WerewolfRoom) EmitImpressionOnVoteAccurateLocked(voterSeat, votedWerewolfSeat int, now time.Time) {
	if r == nil || voterSeat == votedWerewolfSeat {
		return
	}
	r.AggregateImpressionFromEventLocked(voterSeat, votedWerewolfSeat, ImpressionEventVoteAccurate, 1.0, now)
}

// EmitImpressionOnChallengeLocked 公开质疑时调（challenger→challenged）。
//
// 调用前置：必须已持 r.mu（典型:Action_Challenge 末尾）。
func (r *WerewolfRoom) EmitImpressionOnChallengeLocked(challengerSeat, challengedSeat int, now time.Time) {
	if r == nil || challengerSeat == challengedSeat {
		return
	}
	r.AggregateImpressionFromEventLocked(challengedSeat, challengerSeat, ImpressionEventChallenged, 1.0, now)
}

// EmitImpressionOnFollowVoteLocked 跟票时调（follower→leader）。
//
// 调用前置：必须已持 r.mu（典型:FinishVote 末尾遍历票型时）。
func (r *WerewolfRoom) EmitImpressionOnFollowVoteLocked(followerSeat, leaderSeat int, now time.Time) {
	if r == nil || followerSeat == leaderSeat {
		return
	}
	r.AggregateImpressionFromEventLocked(followerSeat, leaderSeat, ImpressionEventFollowVote, 1.0, now)
}

// EmitImpressionOnFrameLocked 被 frame_player 嫁祸时调（target→framer）。
//
// 调用前置：必须已持 r.mu（典型:Action_FramePlayer 末尾）。
func (r *WerewolfRoom) EmitImpressionOnFrameLocked(targetSeat, framerSeat int, now time.Time) {
	if r == nil || targetSeat == framerSeat {
		return
	}
	r.AggregateImpressionFromEventLocked(targetSeat, framerSeat, ImpressionEventFramedByOther, 1.0, now)
}

// emotionFactorForEmotion 返回 emotion 名对应的 factor。
//
// 设计动机：让 wary/guilty 等情绪弱化印象聚合（保留怀疑不放大），
// excited 放大（情绪激动时容易过度修正）。
func emotionFactorForEmotion(emotion string) float32 {
	switch emotion {
	case "wary":
		return 0.7
	case "guilty":
		return 0.5
	case "confused":
		return 0.6
	case "irritated":
		return 1.2
	case "excited":
		return 1.3
	case "calm":
		return 1.0
	case "confident":
		return 1.1
	default:
		return 1.0
	}
}