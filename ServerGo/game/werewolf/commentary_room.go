package werewolf

// commentary_room.go — §20260811-09 U1 AI 实时解说的 manager 接线。
//
// 设计要点:
//   - 房间级 commentaryDesired / commentaryStyle / commentaryModelKey,创建时一次性写入;
//   - startCommentatorGoroutine 注入 Provider(§130 真实生产注入点);
//   - 输出通过 onBroadcastSpectator 回调 → 走 Hub.BroadcastRoomSpectators,
//     玩家收不到 + ClientGameState.CommentaryFeed(viewer<0 填充);
//   - §92a:buildCommentarySnapshotLocked / commentaryStyleLocked 等都是 *Locked 变体,
//     stopAgentsLocked 注释 cancel 模板。

import (
	"context"
	"encoding/json"
	"strings"

	"LsmWebGame/agent/wwcommentator"
	"LsmWebGame/logger"
	"go.uber.org/zap"
)

// CommentaryConfig 是创建房间时的解说设置(对应 service 层入参)。
// nil = 关闭,非 nil 时按 Style/ModelKey 启用。
type CommentaryConfig struct {
	Enabled  bool
	Style    string // "pro" | "fun";空/未知值 → "pro"
	ModelKey string // 空 → 复用 JudgeModelKey;再空 → 走随机
}

// commentaryLine 是 ClientGameState.CommentaryFeed / WS 帧 chat.commentary 的统一载荷。
type commentaryLine struct {
	Seq      uint64 `json:"seq"`
	Text     string `json:"text"`
	Style    string `json:"style"`
	ModelKey string `json:"model_key,omitempty"`
	Kind     string `json:"kind"`
	TsMs     int64  `json:"ts_ms"`
}

const commentaryFeedCap = 20

// buildCommentarySnapshotLocked 是 §92a 锁内变体:从 r.State / WolfPackRoom /
// chatQueue 等构造 wwcommentator.CommentarySnapshot。
// **绝不**返回任何会被误下发到玩家视图的对象——只走 prompt 渲染。
func (r *WerewolfRoom) buildCommentarySnapshotLocked() *wwcommentator.CommentarySnapshot {
	if r.State == nil {
		return nil
	}
	snap := &wwcommentator.CommentarySnapshot{
		RoomID:   r.RoomID,
		Style:    r.commentaryStyleLocked(),
		ModelKey: r.commentaryModelKeyLocked(),
		Phase:    r.State.Phase.String(),
		Round:    r.State.DayNumber, // GameState 没有独立 Round,用 DayNumber 作为轮次代理
		Day:      r.State.DayNumber,
	}
	for seat, p := range r.State.Players {
		if p.Alive {
			// 1-indexed 渲染,与 UI 对齐
			snap.Alive = append(snap.Alive, seat+1)
		}
	}
	// 上帝视角身份 —— 只进 prompt,**绝不**进 ws.Envelope 或 cs.CommentaryFeed。
	roles := make(map[int]string, len(r.State.Roles))
	factions := make(map[int]string, len(r.State.Roles))
	for seat, role := range r.State.Roles {
		if role == RoleUnknown {
			continue
		}
		roles[seat+1] = role.String()
		factions[seat+1] = FactionOf(role).String()
	}
	snap.Roles = roles
	snap.Factions = factions
	// 最近 6 条公开发言摘要(全场公开,安全)。
	if r.chatQueue != nil {
		entries := r.chatQueue.WindowFor(MaxPlayers)
		maxN := 6
		for i := len(entries) - 1; i >= 0 && len(snap.RecentPub) < maxN; i-- {
			e := entries[i]
			if e.IsActivity {
				continue
			}
			txt := trimExcerpt(e.Text, 60)
			if txt != "" {
				snap.RecentPub = append([]string{txt}, snap.RecentPub...)
			}
		}
	}
	// wolfpack 协商副本(协议层隔离已落实:仅狼 bot 可见),这里只读副本注入解说 prompt。
	if r.wolfPack != nil {
		msgs := r.wolfPack.Snapshot(20)
		for _, m := range msgs {
			snap.WolfVote = append(snap.WolfVote, trimExcerpt(m.Text, 60))
		}
	}
	return snap
}

// commentaryStyleLocked / commentaryModelKeyLocked —— 锁内读房间级配置。
func (r *WerewolfRoom) commentaryStyleLocked() string {
	if r.commentaryStyle == "" {
		return "pro"
	}
	return r.commentaryStyle
}

func (r *WerewolfRoom) commentaryModelKeyLocked() string {
	if strings.TrimSpace(r.commentaryModelKey) != "" {
		return r.commentaryModelKey
	}
	if strings.TrimSpace(r.JudgeModelKey) != "" {
		return r.JudgeModelKey
	}
	return ""
}

// setCommentaryConfigLocked —— §92a 锁内变体。
func (r *WerewolfRoom) setCommentaryConfigLocked(cfg *CommentaryConfig) {
	if cfg == nil || !cfg.Enabled {
		r.commentaryDesired = false
		return
	}
	r.commentaryDesired = true
	switch cfg.Style {
	case "pro", "fun":
		r.commentaryStyle = cfg.Style
	default:
		r.commentaryStyle = "pro"
	}
	r.commentaryModelKey = cfg.ModelKey
}

// ensureCommentaryFeedLocked push 一条解说并裁剪到 ≤ commentaryFeedCap。
// 锁内调用。
func (r *WerewolfRoom) ensureCommentaryFeedLocked(text, style, modelKey, kind string) commentaryLine {
	r.commentarySeq++
	line := commentaryLine{
		Seq:      r.commentarySeq,
		Text:     text,
		Style:    style,
		ModelKey: modelKey,
		Kind:     kind,
		TsMs:     nowUnixMilli(),
	}
	r.commentaryFeed = append(r.commentaryFeed, line)
	if len(r.commentaryFeed) > commentaryFeedCap {
		r.commentaryFeed = r.commentaryFeed[len(r.commentaryFeed)-commentaryFeedCap:]
	}
	return line
}

// commentaryFeedSnapshotLocked —— 锁内拷贝(给 spectator 视图补齐用)。
func (r *WerewolfRoom) commentaryFeedSnapshotLocked() []commentaryLine {
	if len(r.commentaryFeed) == 0 {
		return nil
	}
	out := make([]commentaryLine, len(r.commentaryFeed))
	copy(out, r.commentaryFeed)
	return out
}

// triggerCommentaryEventLocked —— §130 设计文档 §U1.2.3 触发点收敛:
// 由 manager 各事件路径调用。非阻塞投递,channel 满即丢。
func (r *WerewolfRoom) triggerCommentaryEventLocked(kind string, extra map[string]any) {
	if r.commentaryEvents == nil {
		return
	}
	if r.commentator == nil {
		return
	}
	if r.commentator.IsQuarantined() {
		return
	}
	select {
	case r.commentaryEvents <- wwcommentator.CommentaryEvent{Kind: kind, Extra: extra}:
	default:
		// 静默丢弃。
	}
}

// startCommentatorGoroutine —— §130 真实生产注入点。
// 必须在 RegisterAgentSeats 之后调用(同 SetJudgeConfig 时序)。
// onBroadcastSpectator 回调由调用方注入,内部走 Hub.BroadcastRoomSpectators。
// 当前 onBroadcastSpectator 由 main.go 经 SetCommentarySpectatorHook 注入 manager。
func (m *WerewolfManager) startCommentatorGoroutine(r *WerewolfRoom, onBroadcastSpectator func(roomID string, payload []byte)) {
	if m.registry == nil {
		return
	}
	r.mu.Lock()
	if !r.commentaryDesired || r.commentator != nil {
		r.mu.Unlock()
		return
	}
	modelKey := r.commentaryModelKeyLocked()
	if modelKey == "" {
		r.mu.Unlock()
		logger.L().Warn("commentary: no model key available, skip",
			zap.String("room_id", r.RoomID))
		return
	}
	style := r.commentaryStyleLocked()
	prov, apiKey, err := m.registry.Get(modelKey)
	if err != nil || prov == nil || apiKey == "" {
		r.mu.Unlock()
		logger.L().Warn("commentary: registry.Get failed, skip",
			zap.String("room_id", r.RoomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		return
	}
	ca := wwcommentator.NewCommentatorAgent(r.RoomID, style, modelKey)
	ca.SetProvider(prov, apiKey)
	ca.SetRegistry(m.registry)
	// onBroadcast:只把 line 写入 feed,然后调 spectator-only 回调广播。
	ca.SetOnBroadcast(func(roomID, text, st string) {
		r.mu.Lock()
		line := r.ensureCommentaryFeedLocked(text, st, r.commentaryModelKeyLocked(), "commentary")
		seq := line.Seq
		r.mu.Unlock()
		if onBroadcastSpectator != nil {
			payload, _ := json.Marshal(map[string]any{
				"room_id":   roomID,
				"text":      text,
				"style":     st,
				"model_key": modelKey,
				"seq":       seq,
				"ts_ms":     line.TsMs,
			})
			onBroadcastSpectator(roomID, payload)
		}
	})
	r.commentator = ca
	r.commentaryEvents = make(chan wwcommentator.CommentaryEvent, 32)
	ctx, cancel := context.WithCancel(context.Background())
	r.commentaryCancel = cancel
	snapProvider := func() *wwcommentator.CommentarySnapshot {
		rr := m.getRoom(r.RoomID)
		if rr == nil {
			return nil
		}
		rr.mu.Lock()
		defer rr.mu.Unlock()
		return rr.buildCommentarySnapshotLocked()
	}
	r.mu.Unlock()

	// 桥接:manager 持有 events channel,ca 持有自己的内部 channel。
	// 一个轻量级 goroutine 做转发(避免双重 buffer)。
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-r.commentaryEvents:
				ca.PushEvent(evt)
			}
		}
	}()
	go ca.Run(ctx, snapProvider)
	logger.L().Info("commentary: goroutine started",
		zap.String("room_id", r.RoomID),
		zap.String("model_key", modelKey),
		zap.String("style", style))
}

// stopCommentaryLocked —— §129「stopAgentsLocked 首行必须 cancel」模板:
func (r *WerewolfRoom) stopCommentaryLocked() {
	if r.commentaryCancel != nil {
		r.commentaryCancel()
		r.commentaryCancel = nil
	}
	r.commentator = nil
	// 不关 events channel —— Run 收到 ctx.Done 后退出,channel 自然 GC。
	r.commentaryEvents = nil
}

// trimExcerpt 把字符串截到 max 个 rune(超出加 …)。
func trimExcerpt(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// nowUnixMilli —— 单元测试可注入点(默认 time.Now().UnixMilli)。
var nowUnixMilli = func() int64 { return stdUnixMilli() }

func stdUnixMilli() int64 {
	return timeNowFunc().UnixMilli()
}

// commentarySpectatorHook 是 manager 上的 spectator-only 广播钩子;
// 由 main.go 在装配 WerewolfManager 时经 ws 层注入,内部走 Hub.BroadcastRoomSpectators。
// nil = 解说开启但无法下发(默认静默跳过,不影响对局)。
var commentarySpectatorHook func(roomID string, payload []byte)

// SetCommentarySpectatorHook 注册全局钩子(由 main.go 调用)。
func SetCommentarySpectatorHook(hook func(roomID string, payload []byte)) {
	commentarySpectatorHook = hook
}