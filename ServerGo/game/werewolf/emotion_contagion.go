// Package werewolf — emotion_contagion.go: 狼人杀 13 人局「群体情绪传染」(§20260812-01 U3 轻量版)。
//
// 设计动机 (§7.8 / DeepSeek §四.1):
//   - 复用 §213 emotion_switch_speak 系统,仅在玩家情绪切换时向相邻座位注入传染效果。
//   - **4 类传染**(confident / nervous / angry / calm),系数硬上限 0.3,半径硬上限 2 座。
//   - 严格 §128「对话即思考」 — 仅影响发言风格(tone),不污染推理 / 不写 Memory / 不改 DecisionTrail。
//   - 协议层隔离(§119) — EmotionContagion 不进 chat_message / chat_history。
//
// 全局约束(CLAUDE.md §13 / Agent-Surpport-01 §12):
//   - §92a:本文件用 process-wide 同步,不依赖 WerewolfRoom 内部字段。
//   - §130:触发点 grep — emotion_switch_speak 派发后必须调 SpreadContagion。
//   - §197:传染效果仅注入 prompt 文本,不开新 LLM 调用,无需长预算。
//   - 不覆盖人类玩家(§7.8 风险条款)。
//   - 系数硬上限 0.45(0.3 × 1.5);若需扩大,需重新走产品决策。
package werewolf

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// EmotionContagionKind 4 类传染枚举(严格对应 §7.8 表格)。
type EmotionContagionKind string

const (
	ContagionConfident EmotionContagionKind = "confident"
	ContagionNervous   EmotionContagionKind = "nervous"
	ContagionAngry     EmotionContagionKind = "angry"
	ContagionCalm      EmotionContagionKind = "calm"
)

// ContagionDefaultStrength 系数硬上限 0.3(初始)。
const ContagionDefaultStrength = 0.3

// ContagionMaxRadiusHardCap 传染半径硬上限 2。
const ContagionMaxRadiusHardCap = 2

// ContagionStrengthHardCap 强度硬上限 = 0.3 × 1.5 = 0.45。
const ContagionStrengthHardCap = 0.45

// EmotionContagionEntry 是单次传染事件(§20260812-01 U3 数据结构)。
type EmotionContagionEntry struct {
	SourceSeat     int                  `json:"source_seat"`
	Kind           EmotionContagionKind `json:"kind"`
	Strength       float64              `json:"strength"`
	Distance       int                  `json:"distance"`
	ExpiresRound   int                  `json:"expires_round"`
	ContagionRound int                  `json:"contagion_round"`
	ContagionAtMS  int64                `json:"contagion_at_ms"`
}

// =============================================================================
// 情绪 → 传染半径 / 文本注入
// =============================================================================

// RadiusForEmotion 返回情绪对应的传染半径(§7.8 表格)。
func RadiusForEmotion(kind EmotionContagionKind) int {
	switch kind {
	case ContagionConfident, ContagionCalm:
		return 2
	case ContagionNervous, ContagionAngry:
		return 1
	}
	return 0
}

// PromptBlockForContagion 返回传染提示文本(注入 prompt 末段) — 严格 §128 仅影响 tone。
func PromptBlockForContagion(e EmotionContagionEntry) string {
	switch e.Kind {
	case ContagionConfident:
		return fmt.Sprintf("📢 自信传染(来自 #%d,强度 %.2f,距离 %d 座):你的发言可以更果断,适度使用「确信/我们都知道/只能是他」等语气词。",
			e.SourceSeat+1, e.Strength, e.Distance)
	case ContagionNervous:
		return fmt.Sprintf("⚠️ 紧张传染(来自 #%d,强度 %.2f,距离 %d 座):你下一轮发言控制在 60 字以内,避免冗长。",
			e.SourceSeat+1, e.Strength, e.Distance)
	case ContagionAngry:
		return fmt.Sprintf("🔥 愤怒传染(来自 #%d,强度 %.2f,距离 %d 座):你倾向指出 #%d 的漏洞,语气更直接。",
			e.SourceSeat+1, e.Strength, e.Distance, e.SourceSeat+1)
	case ContagionCalm:
		return fmt.Sprintf("🌊 平静传染(来自 #%d,强度 %.2f,距离 %d 座):你的发言更理性,减少情绪化反应。",
			e.SourceSeat+1, e.Strength, e.Distance)
	}
	return ""
}

// =============================================================================
// 传染主入口:SpreadContagion
// =============================================================================

// SpreadContagion 把情绪从 sourceSeat 传染到相邻座位(process-wide 同步,
// 不假定持 r.mu — 由调用方按需在 r.mu 内调用)。
//
// 防御性边界:
//   - 强度硬上限 0.45
//   - 半径硬上限 2 座
//   - 同源同情绪 1 轮内仅注入 1 次(不累加)
//   - 不传染给 sourceSeat 自身
//
// 参数:
//   - roomID:房间 ID(用于 key 隔离)
//   - numSeats:本局最大座位数(13 人局 = 13)
//   - sourceSeat:源座位(0..numSeats-1)
//   - kind:情绪种类
//   - currentRound:当前轮数(用于 1 轮过期)
//   - isAlive:每位座位的存活状态数组
//   - isBot:每位座位是否 bot 数组(真人=不会被传染)
//   - isHumanSource:源座位是否是真人(真人不传染)
func SpreadContagion(
	roomID string,
	numSeats, sourceSeat int,
	kind EmotionContagionKind,
	currentRound int,
	isAlive []bool,
	isBot []bool,
	isHumanSource bool,
) {
	if roomID == "" || numSeats <= 0 {
		return
	}
	if isHumanSource {
		return
	}
	if sourceSeat < 0 || sourceSeat >= numSeats {
		return
	}
	if sourceSeat < len(isAlive) && !isAlive[sourceSeat] {
		return
	}
	radius := RadiusForEmotion(kind)
	if radius == 0 {
		return
	}
	if radius > ContagionMaxRadiusHardCap {
		radius = ContagionMaxRadiusHardCap
	}
	strength := ContagionDefaultStrength
	if strength > ContagionStrengthHardCap {
		strength = ContagionStrengthHardCap
	}
	now := time.Now().UnixMilli()
	// 环形 ±radius 遍历
	for dist := 1; dist <= radius; dist++ {
		for _, dir := range []int{-1, 1} {
			target := (sourceSeat + dir*dist + numSeats) % numSeats
			if target == sourceSeat {
				continue
			}
			if target < len(isAlive) && !isAlive[target] {
				continue
			}
			if target < len(isBot) && !isBot[target] {
				continue // 真人不会被传染
			}
			// 同源同情绪 1 轮内仅注入 1 次
			if hasRecentContagion(roomID, numSeats, target, sourceSeat, kind, currentRound) {
				continue
			}
			entry := EmotionContagionEntry{
				SourceSeat:     sourceSeat,
				Kind:           kind,
				Strength:       strength,
				Distance:       dist,
				ExpiresRound:   currentRound + 1,
				ContagionRound: currentRound,
				ContagionAtMS:  now,
			}
			appendContagion(roomID, numSeats, target, entry)
		}
	}
}

// hasRecentContagion 检测同源同情绪 1 轮内是否已注入(防止累加)。
func hasRecentContagion(roomID string, numSeats, target, source int, kind EmotionContagionKind, round int) bool {
	queue := getContagionQueue(roomID, numSeats, target)
	for _, e := range queue {
		if e.SourceSeat == source && e.Kind == kind && e.ContagionRound == round {
			return true
		}
	}
	return false
}

// =============================================================================
// 状态存储:process-wide sync.Map
// =============================================================================

var (
	contagionStoreMu sync.RWMutex
	contagionStore   = make(map[string][]EmotionContagionEntry) // key = roomID+"|"+seat
)

func contagionKey(roomID string, numSeats, seat int) string {
	return fmt.Sprintf("%s|%d|%d", roomID, numSeats, seat)
}

// appendContagion 写入单条传染事件(内部已加锁)。
func appendContagion(roomID string, numSeats, seat int, e EmotionContagionEntry) {
	if roomID == "" || numSeats <= 0 {
		return
	}
	k := contagionKey(roomID, numSeats, seat)
	contagionStoreMu.Lock()
	defer contagionStoreMu.Unlock()
	queue := contagionStore[k]
	queue = append(queue, e)
	if len(queue) > 20 {
		queue = queue[len(queue)-20:]
	}
	contagionStore[k] = queue
}

// getContagionQueue 读取某座位的当前传染队列(内部加 RLock)。
func getContagionQueue(roomID string, numSeats, seat int) []EmotionContagionEntry {
	if roomID == "" || numSeats <= 0 {
		return nil
	}
	k := contagionKey(roomID, numSeats, seat)
	contagionStoreMu.RLock()
	defer contagionStoreMu.RUnlock()
	src := contagionStore[k]
	out := make([]EmotionContagionEntry, len(src))
	copy(out, src)
	return out
}

// DrainContagionForPrompt 拉取当前轮还未失效的传染事件,组装成 prompt 文本块。
func DrainContagionForPrompt(roomID string, numSeats, seat, currentRound int) string {
	if roomID == "" || numSeats <= 0 {
		return ""
	}
	queue := getContagionQueue(roomID, numSeats, seat)
	if len(queue) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n【情绪传染状态(发言风格参考)】\n")
	anyValid := false
	for _, e := range queue {
		if e.ExpiresRound < currentRound {
			continue
		}
		sb.WriteString(PromptBlockForContagion(e))
		sb.WriteString("\n")
		anyValid = true
	}
	if !anyValid {
		return ""
	}
	return sb.String()
}

// ContagionBuffForView 透传给 view.go 渲染(只返回当前轮尚未失效的最强一条)。
func ContagionBuffForView(roomID string, numSeats, seat, currentRound int) (EmotionContagionKind, float64, bool) {
	if roomID == "" || numSeats <= 0 {
		return "", 0, false
	}
	queue := getContagionQueue(roomID, numSeats, seat)
	var best *EmotionContagionEntry
	for i := range queue {
		e := &queue[i]
		if e.ExpiresRound < currentRound {
			continue
		}
		if best == nil || e.Strength > best.Strength {
			best = e
		}
	}
	if best == nil {
		return "", 0, false
	}
	return best.Kind, best.Strength, true
}

// ClearContagionForRoom 房间销毁时清理传染队列(避免内存泄漏)。
func ClearContagionForRoom(roomID string) {
	if roomID == "" {
		return
	}
	contagionStoreMu.Lock()
	defer contagionStoreMu.Unlock()
	prefix := roomID + "|"
	for k := range contagionStore {
		if strings.HasPrefix(k, prefix) {
			delete(contagionStore, k)
		}
	}
}
