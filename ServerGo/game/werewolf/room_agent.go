package werewolf

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"LsmAgentGame/agent/core"
	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/errcode"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

func (m *WerewolfManager) StartAgentsLocked(r *WerewolfRoom) {
	if m.registry == nil {
		logger.L().Info("werewolf: registry nil, skipping agent start",
			zap.String("room_id", r.RoomID))
		return
	}
	if r.BotAgents == nil {
		r.BotAgents = make(map[int]*wwplayer.Agent)
	}
	// v3 增强：批量计算本局狼人互知配对（保证对称）。
	// PickWolfTeammatePairs 内部按 30% 概率启用；启用后最多 WolfTeammateHintMaxPairs 对。
	// 所有狼 bot 在下面的循环中**共用同一组配对**，避免独立调用导致的不对称。
	allWolfSeats := collectWolfSeatsLocked(r)
	// v4 §13.1：初始化/复用狼小队交流通道。StartAgents 多次调用时复用同一对象
	// （restartGameLocked 会通过 resetPropStateLocked 重置留言+成员）。
	if r.wolfPack == nil {
		r.wolfPack = NewWolfPackRoom(r.RoomID, 50)
	}
	// §20260812-03 U2 — 私下通道懒初始化。
	if r.secretLetter == nil {
		r.secretLetter = newSecretLetterRoomLocked(r.RoomID)
	}
	r.wolfPack.SetMembers(allWolfSeats)
	// §20260810-10 U1 — 自动战术分工(确定性,按座位升序套模板)。
	// StartAgentsLocked 多次调用时幂等(同座位集合重算结果一致)。
	r.wolfPack.AutoAssignRoles(allWolfSeats)

	// 2026-08-11 §20260811-05 U1 — 玩家行为画像房间级预取。
	// 异步触发(PrefetchPlayerProfiles 内部 lockRoomBriefly + 锁外 DB),
	// 不阻塞 StartAgentsLocked 的持锁调用方;全 AI / 开关关闭时 no-op。
	// §130 接线验证:buildAgentContextLocked 末尾 fillPlayerProfilesLocked 消费。
	go m.PrefetchPlayerProfiles(r)

	// §20260810-10 U2 — 一次性拉取本房间全部 modelKey 的自画像聚合。
	// 一局仅一次 DB 查询(§118 缓存友好);失败/开关关闭/nil reader →
	// portraits 为空 map,后续按"通用自画像"降级,不阻塞游戏流。
	portraits := map[string]*SelfPortraitData{}
	if m.selfPortraitReader != nil && cfgWerewolfModelSelfPortraitEnabled() {
		keys := make([]string, 0, len(r.seatModelKeys))
		seen := map[string]bool{}
		for _, mk := range r.seatModelKeys {
			if mk != "" && !seen[mk] {
				seen[mk] = true
				keys = append(keys, mk)
			}
		}
		if len(keys) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			got, pErr := m.selfPortraitReader.SelfPortraits(ctx, keys)
			cancel()
			if pErr != nil {
				logger.L().Warn("werewolf: self portrait query failed, fallback to generic",
					zap.String("room_id", r.RoomID), zap.Error(pErr))
			} else {
				portraits = got
			}
		}
	}
	hintPairs := wwplayer.PickWolfTeammatePairs(
		allWolfSeats,
		cfgWerewolfWolfTeammateHintRate(),
		cfgWerewolfWolfTeammateHintMaxPairs(),
		nil,
	)
	if len(hintPairs) > 0 {
		for _, pair := range hintPairs {
			logger.L().Info("werewolf: v3 §G4 wolf teammate pair selected",
				zap.String("room_id", r.RoomID),
				zap.Ints("pair", []int{pair[0], pair[1]}))
		}
	}
	for seat, modelKey := range r.seatModelKeys {
		if modelKey == "" {
			continue
		}
		if _, ok := r.BotAgents[seat]; ok {
			continue
		}
		userID := r.Seats[seat]
		if userID == "" {
			continue
		}
		// Resolve role/faction/win from the now-started game state.
		state := r.State
		if state == nil {
			continue
		}
		roleName := "villager"
		if int(seat) < len(state.Roles) {
			roleName = state.Roles[seat].String()
		}
		role := state.Roles[seat]
		faction := "good"
		if f := FactionOf(role); f != FactionUnknown {
			faction = f.String()
		}
		win := "放逐全部狼人"
		if role == RoleWerewolf {
			win = "屠边 — 杀光所有神职或所有平民"
		}
		// 2026-07-15 狼人杀 13 人局金币系统 — bot 长期激励：彩池制下追求金币。
		win += "；并在彩池制下赚到比其他模型更多金币（胜方分输家底注、平局 0、负方输底注）"

		ag, err := wwplayer.NewWithRoom(seat, modelKey, roleName, faction, win, m.registry, r.RoomID, userID)
		if err != nil {
			logger.L().Warn("werewolf: wwplayer.New failed",
				zap.String("room_id", r.RoomID),
				zap.Int("seat", seat),
				zap.String("model_key", modelKey),
				zap.Error(err))
			continue
		}
		// §20260810-10 U2 — 注入模型自画像(纯文本,system 末尾)。
		// 开关关闭时 cfgWerewolfModelSelfPortraitEnabled()=false → 保持空串,
		// BuildSystemPrompt 输出与旧版逐字节一致(向后兼容)。
		if cfgWerewolfModelSelfPortraitEnabled() {
			ag.SelfPortraitText = buildSelfPortraitTextFor(modelKey, portraits[modelKey])
		}
		// §20260811-04 U2 — 注入人设倾向参数(5 维向量,system 末尾)。
		// 开关关闭 / DB 无数据 → 零向量,BuildSystemPrompt 输出与 §20260810-10 字节一致。
		if cfgWerewolfAgentPersonalityEnabled() {
			if vec, key := resolvePersonalityForSeatLocked(r, seat); !vec.IsZeroVector() {
				ag.Personality = vec
				ag.PersonalityPresetKey = key
			}
		}
		// §20260811-09 U2 — 注入 Agent 难度档位(4 档:easy/normal/hard/hell)。
		// PromptDirective 已在 difficulty.go ProfileFor 归一化(空/normal = 空串,
		// 走 BuildSystemPrompt 末尾追加路径)。startAgentsLocked 已持 r.mu,
		// 直接读 r.agentDifficulty 即可(§92a)。
		ag.DifficultyDirective = ProfileFor(AgentDifficulty(r.agentDifficulty)).PromptDirective
		// §5: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		if faction == "wolf" {
			for _, pair := range hintPairs {
				if pair[0] == seat {
					ag.SetWolfTeammateSeat(pair[1])
					logger.L().Info("werewolf: v3 §G4 wolf teammate hint injected",
						zap.String("room_id", r.RoomID),
						zap.Int("seat", seat),
						zap.Int("wolf_teammate_seat", pair[1]))
					break
				}
			}
		}
		// 2026-07-20 §131 新增 — 加载持久化记忆(MEMORY.md)。
		// 一次性同步 Load(2s timeout);失败仅 log 不阻塞 agent 启动。
		// 空串 = 该模型首次参与 / 开关关闭 / store 未注入,后续 run.go 不注入。
		if m.agentMemoryStore != nil && cfgAgentMemoryEnabled() &&
			ProfileFor(AgentDifficulty(r.agentDifficulty)).InjectLongMemory {
			memCtx, memCancel := context.WithTimeout(context.Background(), 2*time.Second)
			md, memErr := m.agentMemoryStore.Load(memCtx, modelKey)
			memCancel()
			if memErr != nil {
				logger.L().Warn("werewolf: agent memory load failed",
					zap.String("room_id", r.RoomID),
					zap.Int("seat", seat),
					zap.String("model_key", modelKey),
					zap.Error(memErr))
			} else if md != "" {
				ag.SetMemoryMD(md)
				// 2026-08-12 §20260812-04 U4 — 接线难度档位的记忆注入预算。
				// difficulty.MemoryInjectRunes(easy 1500 / normal 4000 / hard 6000)
				// 此前 4 处赋值 0 处读取,难度分级对记忆注入量完全无效(§130)。
				ag.SetMemoryInjectRunes(ProfileFor(AgentDifficulty(r.agentDifficulty)).MemoryInjectRunes)
			}
		}
		// 2026-07-10 §4: 注入 RecordLog + 同步调 GameStarted 拿 game_log.id。
		// 失败仅 log,不阻塞 agent 启动(测试 / 老代码路径 RecordLog=nil 时 no-op)。
		if m.RecordLog != nil {
			ag.RecordLog = m.RecordLog
			// GameStarted 返回 game_log.id,失败不阻塞(后续 RecordChatMessage
			// / RecordAction / GameEnded 走 no-op 路径)。
			gameLogID, gsErr := m.RecordLog.GameStarted(
				context.Background(),
				"", // providerID 暂未从 registry 派生;TODO 阶段 5 接 LlmProviderID 解析
				userID,
				r.RoomID,
				"werewolf",
				seat,
				roleName,
			)
			if gsErr != nil {
				logger.L().Warn("werewolf: RecordLog.GameStarted failed",
					zap.String("room_id", r.RoomID),
					zap.Int("seat", seat),
					zap.Error(gsErr))
			}
			ag.GameLogID = gameLogID
		}
		// Truncate model_name to fit db column / sanity.
		evCh := make(chan wwplayer.AgentEvent, 16)
		ag.SetEvents(evCh)
		// BUG-WEREWOLF-CHAT-BOTNAME: pass a seat-aware display name so the
		// chat panel shows "Bot N号" instead of the bare "Bot" string. This
		// keeps live SendFromBot/WhisperFromBot frames consistent with the
		// server-side History() decoration for replayed messages.
		botAccount := fmt.Sprintf("Bot %d号", seat+1)
		runner := newAgentRunner(m, r.RoomID, Seat(seat), userID, botAccount, modelKey, m.chatSvc)
		// BUG-R74-1 (2026-07-09): 把 Agent.Limiter(45s 间隔) 注入 runner,
		// 让 runner.Speak 派发前能强制 Allow() 检查。Agent.Limiter 此前只
		// 在 run.go Mark() 写入,Allow() 从未被调用,等同于死代码。
		runner.speakLimiter = ag.Limiter
		// R76 P1-3 (2026-07-10): 注入 Agent 反向引用,让 runner.Interject 能
		// 调 Agent.AllowInterject/MarkInterject 实现"5min/4条 单 bot 插话 quota"。
		runner.agent = ag

		// §127: 聊天 SSE 流式解析接线 — 把 Agent 的 LLM 流式回调桥接成
		// chat.stream_start/delta/end WS 帧,前端 bot 气泡实时 token 瀑布流。
		// chatSvc 是 wwplayer.BotChatSender 接口,不含 SendBotStream* 方法;
		// 这里通过本地 streamChatSvc 接口 duck-type 匹配,*ws.ChatService 已实现。
		// 在 agent 赋值给 r.BotAgents[seat] 之前接线,确保所有 wake 都能触达。
		streamSeat := seat
		streamRoomID := r.RoomID
		if streamSvc, ok := m.chatSvc.(streamChatSvc); ok {
			// BUG-R121-SEC-01: 复述段落已压缩 — git blame 与 docs/ 索引可还原

			streamEnableIdentity := runner.filterCfg.EnableIdentityFilter
			streamSeatRef := streamSeat
			ag.OnLLMStreamStart(func(streamID string) {
				_ = streamSvc.SendBotStreamStart(streamRoomID, streamSeatRef, streamID)
			})
			ag.OnLLMStreamDelta(func(streamID, delta string) {
				if streamEnableIdentity {
					if scrubbed, hit := ScrubIdentityLeak(delta); hit {
						delta = scrubbed
					}
					if cleaned, hit := StripLLMInternalTags(delta); hit {
						delta = cleaned
					}
				}
				_ = streamSvc.SendBotStreamDelta(streamRoomID, streamID, delta)
			})
			ag.OnLLMStreamEnd(func(streamID, fullText string) {
				if streamEnableIdentity {
					if scrubbed, hit := ScrubIdentityLeak(fullText); hit {
						fullText = scrubbed
					}
					if cleaned, hit := StripLLMInternalTags(fullText); hit {
						fullText = cleaned
					}
				}
				_ = streamSvc.SendBotStreamEnd(streamRoomID, streamID, fullText)
			})
		}

		r.BotAgents[seat] = ag
		// BUG-R242-P1-01: 恢复房间级 LLM 并发信号量(§130 曾删除)。
		// 首个 bot 启动时创建;后续 bot 共用同一引用。cap=0/负值 = 禁用(完全并发)。
		if r.llmSema == nil && cfgWerewolfRoomLLMConcurrency() > 0 {
			r.llmSema = make(chan struct{}, cfgWerewolfRoomLLMConcurrency())
		}
		ag.SetLLMSemaphore(r.llmSema)

		// 2026-07-09 §13-bugfix 改造 — 注入**房间共享** 500K 聊天历史队列。
		// 第一个 bot 启动时分配,后续 bot 共用同一队列引用;
		// 每 bot 通过 ReadPointer 跟踪消费进度,appendRoomMessage / RecordRoomActivity
		// Append 一次,所有 bot 通过序号各自看到一致事件流;
		// buildAgentContextLocked 改为调 q.WindowFor(seat) 而非 q.Snapshot()。
		if r.chatQueue == nil {
			chatCap := cfgWerewolfChatHistoryBytes()
			r.chatQueue = agentcore.NewChatHistoryQueue(chatCap)
		}
		ag.SetChatQueue(r.chatQueue)
		// 每个 bot 初始化 ReadPointer = 当前 nextSeq;之后由 Advance() 推进。
		// 这一点保证首个 bot 不会"看到"Append 之前的任何(已存在)历史消息;
		// 首个 bot 创建队列后立即把它的 read pointer 设为 0(从未消费),
		// 后续 push 的消息会按 WindowFor 全部可见;新加入的 bot 同样初始化为 0。
		r.chatQueue.SetReadPointer(seat, agentcore.ReadPointerNil)

		// BUG-WEREWOLF-P0-NEW-27: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		quarantineSeat := seat
		quarantineRoomID := r.RoomID
		ag.SetOnQuarantine(func() {
			m.notifyQuarantine(quarantineRoomID, quarantineSeat)
		})

		// 2026-08-05 §表情实时同步:agent 发布新 transcript 后(含 emotion_switch_speak
		// 切换情绪 + fx),触发房间回调 broadcast game.state,前端座位卡 EmotionAvatar
		// 即时刷新表情 + fx 特效,无需等到下一阶段广播。回调在 agent 内部以 goroutine
		// 触发,闭包仅读 r.RoomID(不变量) + 调 r.onTranscriptPublished(线程安全)。
		transcriptRoomID := r.RoomID
		ag.SetOnTranscriptPublished(func() {
			cb := r.onTranscriptPublished
			if cb != nil {
				cb(transcriptRoomID)
			}
		})

		// Phase-4 rolePhase callback returns live state so the agent sees the
		// current phase/role/alive list even if it missed an event.
		// BUG-WEREWOLF-P0-NEW-33 (Round 33): also surface live SpeakTurnSeat
		// and TurnActingSeat so the auto-skip guard inside Agent.handleEvent
		// can detect a stale-snapshot race when the LLM call outlasts another
		// bot's auto-skip that already advanced the engine.
		rp := func() (phase, role string, s int, alive []int, speakTurn int, turnActing int, done bool) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.State == nil {
				return "", "", -1, nil, -1, -1, true
			}
			phase = r.State.Phase.String()
			done = r.State.Phase == PhaseGameOver
			s = seat
			role = r.State.Roles[seat].String()
			for i := 0; i < MaxPlayers; i++ {
				if r.State.AliveSeat(Seat(i)) {
					alive = append(alive, i)
				}
			}
			if r.State.SpeakTurnSeat != NoSeat {
				speakTurn = int(r.State.SpeakTurnSeat)
			}
			if r.State.TurnActingSeat != NoSeat {
				turnActing = int(r.State.TurnActingSeat)
			}
			return phase, role, s, alive, speakTurn, turnActing, done
		}

		ctx, cancel := context.WithCancel(context.Background())
		// Stash cancel so RemoveGame / RemovePlayer can stop the agent
		// goroutine r.cleanupAgents will cancel + shutdown on demand.
		if r.agentCancels == nil {
			r.agentCancels = make(map[int]context.CancelFunc)
		}
		r.agentCancels[seat] = cancel

		// BUG-WEREWOLF-DISBAND-LEAK: register the goroutine on agentWG so
		// stopAgentsLocked can wait for it to actually return. Without this,
		// in-flight LLM HTTP calls + 1s/2s/4s backoff retries would outlive
		// the disband call (up to 8s) and race with the cleared BotAgents
		// map. See WerewolfRoom.agentWG doc.
		r.agentWG.Add(1)
		go func(a *wwplayer.Agent, seatIdx int) {
			defer r.agentWG.Done()
			defer func() {
				if rec := recover(); rec != nil {
					// BUG-OBS-R185-001(P3): 之前的 recover 只打印了 panic 值,
					// 没有栈;e2e 报告只能看到 "index out of range [13] with length 13"
					// 这类表象,无法定位触发点。补上 debug.Stack() 后,
					// 下次报告可直接读栈判定是 seats[] 越界 / alive[] 越界 / role[] 越界。
					logger.L().Error("werewolf agent panicked",
						zap.String("room_id", r.RoomID),
						zap.Int("seat", seatIdx),
						zap.Any("recover", rec),
						zap.String("stack", string(debug.Stack())))
				}
			}()
			a.Run(ctx, runner, rp)
		}(ag, seat)

		logger.L().Info("werewolf: agent started",
			zap.String("room_id", r.RoomID),
			zap.Int("seat", seat),
			zap.String("model_key", modelKey),
			zap.String("role", roleName))
	}
	// BUG-WEREWOLF-P0-NEW-42b: launch the phase watchdog only once per room
	// lifecycle. The watchdog polls phase+actingSeat every 5s and forces a
	// skip when a phase is stuck for > phaseWatchdogDeadlineFor(seatCount).
	if r.watchdogCancel == nil {
		wctx, wcancel := context.WithCancel(context.Background())
		r.watchdogCancel = wcancel
		go m.startPhaseWatchdog(wctx, r)
		logger.L().Info("werewolf: phase watchdog started",
			zap.String("room_id", r.RoomID),
			zap.Duration("tick", phaseWatchdogTickInterval),
			zap.Duration("deadline", phaseWatchdogDeadlineFor(r.State.SeatCount)))
	}

	// 2026-07-09 §13 增强 — 启动 speak floor watchdog(5s tick)。每 bot 60s 窗口
	// < minSpeaks 且距上次 wake ≥ 20s 时 push AgentEvent{Kind:"speak_floor_tick"}。
	// 独立于 phase watchdog,不在同一 goroutine 内串联(避免 §92b 路径漂移)。
	m.startSpeakFloorWatchdog(r)
}

func tryDispatchQuarantinedActingSkip(m *WerewolfManager, r *WerewolfRoom, ag *wwplayer.Agent, gc wwtypes.GameContext) bool {
	if !ag.IsQuarantined() || !gc.MyTurn {
		return false
	}
	// BUG-WEREWOLF-P0-NEW-43: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	r.quarantineSkipDepth++
	defer func() { r.quarantineSkipDepth-- }()
	if r.quarantineSkipDepth > quarantineSkipDepthLimit {
		if r.quarantineSkipDepth == quarantineSkipDepthLimit+1 {
			// Log exactly once per over-budget episode — anything louder
			// (WARN every iteration) was the noise floor in the R38 log
			// (hundreds of entries in one millisecond).
			logger.L().Error("werewolf: quarantine-skip recursion depth exceeded; yielding to phase watchdog",
				zap.String("room_id", r.RoomID), zap.Int("seat", ag.Seat),
				zap.String("phase", gc.Phase), zap.Int("depth", r.quarantineSkipDepth))
		}
		return false
	}

	// BUG-R48-P0-3: 同一 seat 在同一 phase 内重复派发 skip 会导致无限递归
	// (dayVoteLocked → wakeActingAgentsLocked → 同 seat MyTurn=true → 再次
	// 派发 vote_skip)。用 skippingSeats 做重入保护: 同 seat 只派发一次,
	// 第二次直接返回 false 让调用方跳过。
	if r.skippingSeats == nil {
		r.skippingSeats = make(map[int]bool)
	}
	// phase 变化时清空, 允许新 phase 正常派发
	if r.lastSkipPhase != gc.Phase {
		r.lastSkipPhase = gc.Phase
		r.skippingSeats = make(map[int]bool)
	}
	if r.skippingSeats[ag.Seat] {
		// 已经为该 seat 派发过 skip, 不再重入
		return false
	}
	r.skippingSeats[ag.Seat] = true

	if skipName, skipArg := wwplayer.SkipPhaseAction(gc.Phase, gc.Role); skipName != "" {
		logger.L().Warn("werewolf: quarantined bot acting; auto-skipping in manager",
			zap.String("room_id", r.RoomID), zap.Int("seat", ag.Seat),
			zap.String("phase", gc.Phase), zap.String("skip_tool", skipName))
		if derr := m.dispatchQuarantinedSkipLocked(r, ag.Seat, skipName, skipArg); derr != nil {
			logger.L().Warn("werewolf: quarantined-skip dispatch failed",
				zap.String("room_id", r.RoomID), zap.Int("seat", ag.Seat),
				zap.String("skip_tool", skipName), zap.Error(derr))
		}
	}
	return true
}

func (m *WerewolfManager) notifyQuarantine(roomID string, seat int) {
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ag := r.BotAgents[seat]
	if ag == nil || !ag.IsQuarantined() {
		return
	}
	driverSeat := lowestActiveBotSeatLocked(r)
	gc := buildAgentContextLocked(r, seat, driverSeat)
	if gc.Phase == "" {
		return
	}
	if !gc.MyTurn {
		// Quarantined bot is NOT the current acting seat — nothing to skip.
		// The bot will be handled by the next WakeActingAgents /
		// WakeAllAgents cycle (PushEvent → IsQuarantined guard → no-op).
		logger.L().Debug("werewolf: quarantine notification for non-acting bot; no skip needed",
			zap.String("room_id", roomID), zap.Int("seat", seat),
			zap.String("phase", gc.Phase))
		return
	}
	// Quarantined bot IS the acting seat — dispatch skip now.
	logger.L().Warn("werewolf: quarantine notification for acting bot; dispatching skip",
		zap.String("room_id", roomID), zap.Int("seat", seat),
		zap.String("phase", gc.Phase), zap.String("role", gc.Role))
	tryDispatchQuarantinedActingSkip(m, r, ag, gc)
}

func (m *WerewolfManager) WakeAllAgents(roomID string, kind string, snap wwtypes.GameContext) {
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	r.mu.Lock()
	m.wakeAllAgentsLocked(r, kind, snap)
	r.mu.Unlock()
}

func (m *WerewolfManager) wakeAllAgentsLocked(r *WerewolfRoom, kind string, snap wwtypes.GameContext) {
	driverSeat := lowestActiveBotSeatLocked(r)
	for seat, ag := range r.BotAgents {
		if ag == nil {
			continue
		}
		gc := buildAgentContextLocked(r, seat, driverSeat)
		if gc.Phase == "" {
			// State unavailable — fall back to caller-supplied snapshot so we
			// still nudge the agent (it'll no-op via the empty-Phase guard).
			gc = snap
		}
		// BUG-WEREWOLF-P0-NEW-27: only the acting seat's quarantined bot gets
		// an in-place skip; healthy bots (and quarantined non-acting bots)
		// still receive the wake so their Memory stays synced. Falling through
		// to PushEvent when the helper returns false is the critical fix —
		// previously the broadcast path had no quarantine handling at all.
		if tryDispatchQuarantinedActingSkip(m, r, ag, gc) {
			continue
		}
		ag.PushEvent(wwplayer.AgentEvent{Kind: kind, Context: gc})
	}
}

func (m *WerewolfManager) WakeActingAgents(roomID string, kind string) {
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	r.mu.Lock()
	driverSeat := lowestActiveBotSeatLocked(r)
	for seat, ag := range r.BotAgents {
		if ag == nil {
			continue
		}
		gc := buildAgentContextLocked(r, seat, driverSeat)
		if gc.Phase == "" || !gc.MyTurn {
			continue
		}
		// Quarantined acting bot — bypass the stalled agent and skip
		// the turn in-process. Mirrors run.go's skipPhaseAction logic
		// but does it *before* waking the agent so the agent isn't
		// responsible for advancing a phase its LLM can't drive.
		// BUG-WEREWOLF-P0-NEW-27: helper only dispatches when the bot is
		// BOTH quarantined AND the acting seat (gc.MyTurn=true guarantees
		// this in this branch); fall-through is unreachable here but kept
		// for symmetry with the broadcast path.
		if tryDispatchQuarantinedActingSkip(m, r, ag, gc) {
			continue
		}
		ag.PushEvent(wwplayer.AgentEvent{Kind: kind, Context: gc})
	}
	r.mu.Unlock()
}

func (m *WerewolfManager) dispatchQuarantinedSkipLocked(r *WerewolfRoom, seat int, skipName string, skipArg int) *errcode.Error {
	userID := r.Seats[seat]
	switch skipName {
	case "wolf_kill":
		// Empty kill (target = -1) is rejected by the engine as a wolf
		// strategy violation; pick an alive non-self, non-wolf seat that
		// the engine will accept, otherwise no-op.
		// BUG-R73-P1 (wolf_kill watchdog skip): when only one wolf remains
		// alive, scanning everything except `seat` always lands on a dead
		// fellow-wolf or, in mixed scenarios where other wolves are dead,
		// may briefly re-pick a wolf. NightWolfKill rejects "wolves cannot
		// kill each other" [20001], the watchdog loop re-dispatches forever
		// and the phase never advances. Filter targets to non-wolves; if
		// none exist, fall through to no-op so watchdog retry is skip-safe.
		target := Seat(skipArg)
		if target < 0 {
			for i := 0; i < MaxPlayers; i++ {
				if i != seat && r.State.AliveSeat(Seat(i)) && r.State.Roles[Seat(i)] != RoleWerewolf {
					target = Seat(i)
					break
				}
			}
		} else {
			// Engine-validated explicit target: only pass it through if it
			// is actually a legal wolf-kill target (alive, non-wolf).
			if !r.State.AliveSeat(target) || r.State.Roles[target] == RoleWerewolf {
				target = -1
			}
		}
		if target < 0 {
			// BUG-R73-P1: legal no-op. Emitting explicit progress log so
			// operators can see why the watchdog skipped wolf_kill rather
			// than retrying into a dead loop.
			logger.L().Info("werewolf: wolf_kill skip — no legal non-wolf target, treating as no-op",
				zap.String("room_id", r.RoomID),
				zap.Int("seat", seat))
			return nil
		}
		// BUG-R213-P1 (R213 报告 §4.1): 当 watchdog 派出 wolf_kill skip 时,
		// 若该 wolf 在本轮已投过票(WolfVoteCast[seat]=true),NightWolfKill 会
		// 返回 [30201] "you have already voted this round",skip 被静默吞掉,
		// 阶段不再推进 → D1→Night 2 延迟 ~4 分钟。
		// 修复:在调用 wolfKillLocked 之前复位本轮的投票状态,把 watchdog skip
		// 视为一次合法的"重新弃权"。仅清当前 seat 的两个槽位,不影响其它狼。
		if r.State.WolfVoteCast[seat] {
			logger.L().Warn("werewolf: wolf_kill watchdog skip — resetting prior vote for acting wolf",
				zap.String("room_id", r.RoomID),
				zap.Int("seat", seat))
			r.State.WolfVoteCast[seat] = false
			r.State.WolfVotes[seat] = NoSeat
		}
		// 2026-08-01 BUG-R225-P1-01: 弃权复位已下沉到 wolfKillLocked
		// (room_quarantine_skip_locked.go),manager/agent 两条路径共用
		// 同一入口语义。原 R213-P1 复位块删除,避免双源真理。
		return m.wolfKillLocked(r, userID, target)
	case "seer_check":
		target := -1
		for i := 0; i < MaxPlayers; i++ {
			if i != seat && r.State.AliveSeat(Seat(i)) {
				target = i
				break
			}
		}
		if target < 0 {
			return nil
		}
		return m.seerCheckLocked(r, userID, Seat(target))
	case "witch_act_skip":
		// BUG-WEREWOLF-P0-NEW-42: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		return m.witchLocked(r, userID, "none", NoSeat)
	case "guard_protect_skip":
		// §134: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		return m.guardProtectFallbackLocked(r, userID)
	case "finish_speak":
		// BUG-WEREWOLF-P0-NEW-16: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		return m.finishSpeakLocked(r, userID)
	case "vote_skip":
		// BUG-WEREWOLF-P0-NEW-35: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		return m.dayVoteLocked(r, userID, NoSeat)
	case "sheriff_elect":
		// BUG-WEREWOLF-P0-NEW-42: use sheriffElectLocked (not the public
		// Action_SheriffElect) to avoid the r.mu self-deadlock.
		return m.sheriffElectLocked(r)
	case "sheriff_set_speak_order":
		// §20260810-09 — 警长定序 skip。watchdog 兜底默认值(顺时针 + 警长先发言),
		// 这是 §97 必须显式列出的 fallback —— 否则警长被 quarantine 时
		// PhaseSheriffOrder 会卡 90s,且无法推进到 PhaseSpeak。
		return m.sheriffSetSpeakOrderLocked(r, userID,
			SheriffOrderDefaultDirection, SheriffOrderDefaultSelfPos)
	case "start_day":
		// BUG-WEREWOLF-P1-NEW-1: dawn→speak transition is driven by the
		// host driver bot calling start_day. When the driver is
		// quarantined, WakeActingAgents now dispatches the skip in-place
		// so the room advances out of PhaseDawn instead of dead-locking.
		// BUG-WEREWOLF-P0-NEW-42: use startDayLocked (not the public
		// Action_StartDay) to avoid the r.mu self-deadlock.
		return m.startDayLocked(r)
	case "hunter_shoot":
		// BUG-WEREWOLF-P0-2 (R42): hunter dies then shoots — the
		// ability triggers on death. When the dead hunter is
		// quarantined or the watchdog needs to force-skip the
		// phase, call HunterShoot(hunterSeat, NoSeat) = "don't
		// shoot", which advances the day via advanceDay().
		return m.hunterShootLocked(r, userID, NoSeat)
	case "last_words_skip":
		// BUG 2026-07-09: 遗言 actor 被 quarantine 时,watchdog 强制跳过其遗言,
		// 推进遗言队列直至清空,恢复原路径。
		return m.skipLastWordsLocked(r, Seat(seat))
	case "idiot_reveal", "idiot_reveal_skip":
		// 2026-07-10: 白痴翻牌阶段,quarantined bot 默认 skip(放弃翻牌 → 正常放逐)。
		// wwplayer.SkipPhaseAction 返回 "idiot_reveal_skip";watchdog/派发路径亦兼容 "idiot_reveal"。
		return m.idiotRevealLocked(r, userID, "skip")
	case "demon_hunter_hunt_skip":
		// §猎魔人 猎魔人被 quarantine 时,manager 路径派发让阶段推进。
		// 关键决策:watchdog 兜底**永远空过**(target=-1)而非随机狩猎某个存活玩家 —
		// 随机狩猎的失败率约 80%(场上 12 个玩家,4 狼+8 好,盲射命中狼 = 4/12 = 33%),
		// 误杀好人会让猎魔人+1 个好人出局,直接屠边。
		// 空过只是「本晚不动」,第二天白天仍可基于讨论再次狩猎,代价最低。
		return m.demonHunterHuntLocked(r, userID, NoSeat)
	}
	return nil
}

func lowestAliveBotSeatLocked(r *WerewolfRoom) int {
	driver := -1
	for seat, ag := range r.BotAgents {
		if ag == nil {
			continue
		}
		if r.State == nil || !r.State.AliveSeat(Seat(seat)) {
			continue
		}
		if driver == -1 || seat < driver {
			driver = seat
		}
	}
	return driver
}

func lowestActiveBotSeatLocked(r *WerewolfRoom) int {
	driver := -1
	for seat, ag := range r.BotAgents {
		if ag == nil {
			continue
		}
		if r.State == nil || !r.State.AliveSeat(Seat(seat)) {
			continue
		}
		if ag.IsQuarantined() {
			continue
		}
		if driver == -1 || seat < driver {
			driver = seat
		}
	}
	return driver
}

func syncQuarantinedLocked(r *WerewolfRoom) {
	if r.State == nil {
		return
	}
	var qs [MaxPlayers]bool
	for seat, ag := range r.BotAgents {
		if ag != nil && ag.IsQuarantined() {
			qs[seat] = true
		}
	}
	r.State.QuarantinedSeats = qs
}

// ─── 2026-08-13 §20260813-01 U2 — GameContext 分层缓存 ───
//
// getStaticContext 获取 seat 的静态上下文缓存。未命中时调用 builder 构建并缓存。
// 静态上下文一局只构建一次(座位/角色/玩家列表等整局不变信息)。
func getStaticContext(r *WerewolfRoom, seat int, builder func() *wwtypes.StaticContext) *wwtypes.StaticContext {
	if r.staticContextCache == nil {
		r.staticContextCache = make(map[int]*wwtypes.StaticContext)
	}
	if sc, ok := r.staticContextCache[seat]; ok {
		return sc
	}
	sc := builder()
	r.staticContextCache[seat] = sc
	return sc
}

// getPhaseStateContext 获取 seat 的阶段状态缓存。phase 变化时自动失效重建。
// 阶段状态在阶段内不变(警长/屠边计数等),阶段切换时重建。
func getPhaseStateContext(r *WerewolfRoom, seat int, currentPhase string, builder func() *wwtypes.PhaseStateContext) *wwtypes.PhaseStateContext {
	if r.phaseStatePhase != currentPhase {
		// 阶段变化,失效旧缓存
		r.phaseStateCache = make(map[int]*wwtypes.PhaseStateContext)
		r.phaseStatePhase = currentPhase
	}
	if r.phaseStateCache == nil {
		r.phaseStateCache = make(map[int]*wwtypes.PhaseStateContext)
	}
	if psc, ok := r.phaseStateCache[seat]; ok {
		return psc
	}
	psc := builder()
	r.phaseStateCache[seat] = psc
	return psc
}

// invalidateContextCaches 失效所有上下文缓存(游戏重开时调用)。
func invalidateContextCaches(r *WerewolfRoom) {
	r.staticContextCache = make(map[int]*wwtypes.StaticContext)
	r.phaseStateCache = make(map[int]*wwtypes.PhaseStateContext)
	r.phaseStatePhase = ""
}

// winConditionFor 返回角色+阵营对应的胜利条件描述。
func winConditionFor(role Role, faction Faction) string {
	switch faction {
	case FactionWolf:
		return "狼人屠边(杀光全部神职或杀光全部平民)"
	case FactionGood:
		if role == RoleWerewolf {
			return "放逐全部 4 狼人(你被发到狼人身份但阵营为好人,这是异常状态)"
		}
		return "放逐全部 4 狼人"
	default:
		return "存活到最后"
	}
}

// buildGodRolePoolLocked 返回本局实际发牌的神职池(中文名称列表)。
// 从 GameState.Roles 中提取所有实际发牌的神职角色(去重),
// 转换为中文名称供 Agent prompt 使用。
func buildGodRolePoolLocked(r *WerewolfRoom) []string {
	if r.State == nil {
		return nil
	}
	// 角色名映射(英文 → 中文)
	roleNameCN := map[string]string{
		"seer":         "预言家",
		"witch":        "女巫",
		"hunter":       "猎人",
		"idiot":        "白痴",
		"guard":        "守卫",
		"knight":       "骑士",
		"demon_hunter": "猎魔人",
	}
	seen := make(map[string]bool)
	var pool []string
	for i := 0; i < r.State.SeatCount; i++ {
		role := r.State.Roles[i]
		if !IsGodRole(role) {
			continue
		}
		name := role.String()
		if seen[name] {
			continue
		}
		seen[name] = true
		if cn, ok := roleNameCN[name]; ok {
			pool = append(pool, cn)
		} else {
			pool = append(pool, name)
		}
	}
	return pool
}

func buildAgentContextLocked(r *WerewolfRoom, seat int, driverSeat int) wwtypes.GameContext {
	if r.State == nil || seat < 0 || seat >= MaxPlayers {
		return wwtypes.GameContext{}
	}
	gs := r.State
	gc := wwtypes.GameContext{
		Round:         gs.DayNumber,
		Phase:         gs.Phase.String(),
		Role:          gs.Roles[seat].String(),
		MySeat:        seat, // BUG-WEREWOLF-P0-NEW-10: prompt uses seat+1 as the 1-indexed 玩家编号
		SpeakTurn:     -1,
		MySeerCheck:   -1,
		WolfTarget:    -1,
		IsDriver:      seat == driverSeat,
		GameStartedAt: r.gameStartedAt,
		// 2026-07-10 §13: 本局实际人数(13/12/7),prompt.go 据此选择对应规则摘要渲染。
		SeatCount: gs.SeatCount,
	}

	// 2026-08-13 §20260813-01 U2 — 分层缓存:静态层一局构建一次,
	// 阶段层阶段切换时重建。减少每轮重复计算 ~3-5KB。
	gc.Static = getStaticContext(r, seat, func() *wwtypes.StaticContext {
		return &wwtypes.StaticContext{
			SeatCount:    gs.SeatCount,
			MySeat:       seat,
			Role:         gs.Roles[seat].String(),
			Faction:      FactionOf(gs.Roles[seat]).String(),
			WinCondition: winConditionFor(gs.Roles[seat], FactionOf(gs.Roles[seat])),
			AllPlayers:   buildAllPlayersLocked(r),
			GodRolePool:  buildGodRolePoolLocked(r),
		}
	})
	gc.PhaseState = getPhaseStateContext(r, seat, gs.Phase.String(), func() *wwtypes.PhaseStateContext {
		cands := gs.SheriffCandidates()
		sc := &wwtypes.PhaseStateContext{
			Phase:            gs.Phase.String(),
			SheriffSeat:      int(gs.SheriffSeat),
			SheriffStream:    [2]int{int(gs.SheriffStreams[0]), int(gs.SheriffStreams[1])},
			IdiotRevealedSeats: gs.idiotRevealedSeats(),
			DivineCnt:        gs.DivineCnt,
			PlainCnt:         gs.PlainCnt,
			WolfAliveCnt:     gs.WolfAliveCnt,
			VoteProposed:     gs.VoteProposed,
			VoteProposer:     int(gs.VoteProposer),
		}
		if len(cands) > 0 {
			sc.SheriffCandidates = make([]int, len(cands))
			for i, s := range cands {
				sc.SheriffCandidates[i] = int(s)
			}
		}
		return sc
	})
	if gs.SpeakTurnSeat != NoSeat {
		gc.SpeakTurn = int(gs.SpeakTurnSeat)
	}
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) {
			gc.AliveSeats = append(gc.AliveSeats, i)
		}
	}
	for _, d := range gs.LastNightDeaths {
		gc.LastNightDeaths = append(gc.LastNightDeaths, int(d))
	}

	// BUG: 狼人杀 7 人局 Agent 多轮上下文 — feed the room transcript
	// (recentSpeeches / whisperInbox) into the per-agent GameContext. Each
	// agent sees the same public speech history; whisperInbox is per-seat
	// so a wolf's whispered plan is only visible to the recipient.
	if len(r.recentSpeeches) > 0 {
		// Defensive copy: the underlying slice is mutated in place as new
		// messages arrive, and the prompt builder may read it later.
		gc.RecentSpeeches = make([]wwtypes.SpeechEvent, len(r.recentSpeeches))
		copy(gc.RecentSpeeches, r.recentSpeeches)
	}
	if inbox, ok := r.whisperInbox[seat]; ok && len(inbox) > 0 {
		gc.WhisperInbox = make([]wwtypes.WhisperEvent, len(inbox))
		copy(gc.WhisperInbox, inbox)
	}
	// Role-private info.
	switch gs.Roles[seat] {
	case RoleSeer:
		if gs.Players[seat].LastSeerCheck != NoSeat {
			gc.MySeerCheck = int(gs.Players[seat].LastSeerCheck)
			// 2026-08-12 §20260812-04 U1 (P0-1) — 补算查验结果阵营。
			// 缺陷:此前只填座位号,LLM 拿到「我查了 4 号」却不知道结果,
			// 等于技能失效。人类玩家走 BuildSeerInform 一直能看到阵营。
			if FactionOf(gs.Roles[gs.Players[seat].LastSeerCheck]) == FactionWolf {
				gc.MySeerCheckFaction = "wolf"
			} else {
				gc.MySeerCheckFaction = "good"
			}
		}
		// 全量查验历史:预言家的核心价值是这张表(单看上一晚会丢失前几轮金水/查杀)。
		// 复用 InformationLedger 单一事实来源(§20260810-05/08),不新增镜像字段。
		gc.MySeerCheckHistory = r.buildSeerCheckHistoryLocked(seat)
	case RoleWitch:
		if gs.WolfKillTarget != NoSeat {
			gc.WolfTarget = int(gs.WolfKillTarget)
		}
		// §20260812-04 U1 — 女巫药剂剩余状态(此前仅由 tool enum 隐式表达)。
		gc.WitchAntidoteUsed = gs.Players[seat].WitchAntidoteUsed
		gc.WitchPoisonUsed = gs.Players[seat].WitchPoisonUsed
	case RoleGuard:
		// §134 守卫 GameContext:仅填 GuardLastProtect(让 LLM 知道 G1 连守约束);
		// 绝对不填 WolfTarget —— "盲守"语义:守卫看不到狼刀目标。
		gc.GuardLastProtect = int(gs.GuardLastProtect)
	case RoleWerewolf:
		// 2026-07-17: 狼人投票快照(仅 night_wolves 阶段)
		if gs.Phase == PhaseNightWolves {
			votes := map[int]int{}
			reasons := map[int]string{} // §20260810-04 U2 — 刀人理由快照
			cast := 0
			total := 0
			for i := 0; i < MaxPlayers; i++ {
				if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
					total++
					if gs.WolfVoteCast[i] {
						cast++
						if gs.WolfVotes[i] != NoSeat {
							votes[i] = int(gs.WolfVotes[i])
							if gs.WolfVoteReasons[i] != "" {
								reasons[i] = gs.WolfVoteReasons[i]
							}
						}
					}
				}
			}
			gc.WolfVotes = votes
			gc.WolfVoteReasons = reasons
			gc.WolfVotesCast = cast
			gc.WolfTotalWolves = total
			gc.WolfVoting = gs.WolfKillTarget == NoSeat
			// 2026-07-24 R196-P1: 把"我是否已投票"显式注入 bot 上下文,
			// 让 prompt 渲染时 LLM 看到自己已投票,不再循环调 wolf_kill。
			if gs.Roles[seat] == RoleWerewolf {
				gc.MyWolfVoteCast = gs.WolfVoteCast[seat]
			}
			if gs.WolfVoteTally != nil {
				gc.WolfVoteTally = &wwtypes.WolfVoteTally{
					Counts: gs.WolfVoteTally.Counts,
					Tied:   gs.WolfVoteTally.Tied,
					Reason: gs.WolfVoteTally.Reason,
					Final:  gs.WolfVoteTally.Final,
				}
			}
		}
	}

	// §20260809-02 U2 Bot 票型回灌 —— 把上一轮白天投票的「谁投了谁」
	// 快照注入 GameContext(供 BuildUserPrompt 渲染)。仅在非 night 阶段
	// 注入(狼人杀夜间不需要看白天票型,反而会污染推理);nil-guard 兜底。
	if gs.LastDayVoteMap != nil && len(gs.LastDayVoteMap) > 0 &&
		gs.Phase != PhaseNightWolves && gs.Phase != PhaseNightGuard &&
		gs.Phase != PhaseNightSeer && gs.Phase != PhaseNightWitch &&
		gs.Phase != PhaseNightDemonHunter {
		voteMap := make(map[int]int, len(gs.LastDayVoteMap))
		for f, t := range gs.LastDayVoteMap {
			voteMap[int(f)] = int(t)
		}
		gc.LastDayVoteMap = voteMap
	}

	alive := gs.AliveSeat(Seat(seat))
	switch gs.Phase {
	case PhasePreWolves:
		// BUG 2026-07-08: 缓冲期所有存活玩家都"可以说话",但严格说
		// 并不是"轮到他行动"——所有玩家可同时发 speak / interject / whisper。
		// 把 MyTurn 设为 false,prompt.go 会走"非发言轮"分支(不显示
		// 【现在轮到你行动】),改由驱动在 remaining 字段提示。
		gc.MyTurn = false
	case PhaseNightWolves:
		// 2026-07-17: 所有存活狼人都可以投票(未投票者 MyTurn=true)
		gc.MyTurn = alive && gs.Roles[seat] == RoleWerewolf && !gs.WolfVoteCast[seat]
	case PhaseNightGuard:
		// §134 守卫守护阶段:仅存活守卫 MyTurn=true。
		gc.MyTurn = alive && Seat(seat) == gs.GuardSeat
	case PhaseNightSeer, PhaseNightWitch:
		gc.MyTurn = alive && gs.TurnActingSeat == Seat(seat)
	case PhaseDawn:
		gc.MyTurn = alive && seat == driverSeat
	case PhaseSheriff:
		// BUG-08: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		gc.MyTurn = alive && (!gs.Players[seat].HasSpoken || !gs.Players[seat].Voted || seat == driverSeat)
	case PhaseSpeak:
		gc.MyTurn = alive && gs.SpeakTurnSeat == Seat(seat)
	case PhaseVote:
		// BUG-R193-001: 同步 quarantine 状态,让 AllVoted(拼入 LLM prompt)与
		// MyTurn(驱动派发)都感知 quarantined bot。buildAgentContextLocked 是
		// 所有 wake 路径的公共入口,在此处单次同步成本可忽略。
		syncQuarantinedLocked(r)
		gc.MyVoted = gs.Players[seat].Voted
		gc.AllVoted = gs.allActiveVoted()
		// Non-driver votes once then yields; the driver keeps MyTurn so it
		// can call finish_vote once everyone has voted.
		// 注意: 此处 MyTurn 不禁用 quarantined bot —— 禁用的 bot 仍需 MyTurn=true
		// 以触发 tryDispatchQuarantinedActingSkip 派发 vote_skip(弃权),让阶段能
		// 推进。quarantined bot 的 MyTurn 由 PushEvent → IsQuarantined guard 短路,
		// 不会真正进入 LLM 调用。allActiveVoted() 已排除 quarantined 座位,故一旦
		// 所有活跃玩家投票,DayVote/dayVoteLocked 立即 auto-tally,触发阶段推进。
		gc.MyTurn = alive && (!gc.MyVoted || seat == driverSeat)
	case PhaseHunterShoot:
		// BUG-WEREWOLF-P0-2 (R42): hunter dies THEN shoots — the
		// ability triggers on death, so the hunter is already
		// dead (Alive=false) when PhaseHunterShoot starts. The
		// `alive` guard previously blocked MyTurn for the dead
		// hunter, leaving the room permanently stuck with no
		// acting seat and watchdogActingSeat returning -1.
		gc.MyTurn = gs.Roles[seat] == RoleHunter && gs.HunterPendingShoot
	case PhaseDeathLyric:
		// BUG 2026-07-09: 遗言阶段。仅当前遗言座位(已是死者)可发遗言;
		// alive=false 不再 §86 acting-bot 逻辑,改用权威字段 DeathLyricCurrent。
		gc.MyTurn = Seat(seat) == gs.DeathLyricCurrent
		gc.DeathLyricCurrent = int(gs.DeathLyricCurrent)
	case PhaseRestartVote:
		// 2026-07-10: 重开局投票阶段。每个有资格的入座座位都可以调
		// restart_vote (即使已死),因此 MyTurn=true 让 BuildUserPrompt
		// 渲染"现在轮到你行动"分支;具体工具暴露由 BuildTools("restart_vote",…)
		// 完成。
		gc.MyTurn = r.Seats[seat] != ""
		gc.LastWinner = gs.Winner
		if !gs.PhaseDeadlineAt.IsZero() {
			rem := time.Until(gs.PhaseDeadlineAt)
			if rem < 0 {
				rem = 0
			}
			gc.RestartVoteRemainingSec = int(rem.Seconds())
		}
		gc.RestartVoteDecided = gs.RestartVoteDone
		gc.RestartVoteResult = gs.RestartVoteResult
	case PhaseIdiotReveal:
		// 2026-07-10: 白痴翻牌阶段。仅最高票白痴(DayEliminated)可行动;
		// 翻牌或放弃后阶段即结束,故不全天 MyTurn。
		gc.MyTurn = Seat(seat) == gs.DayEliminated
	case PhaseGameOver, PhaseFilling:
		gc.MyTurn = false
	}

	// BUG 2026-07-08: 填充每玩家档案表(AllPlayers),让 LLM 每轮都看到
	// 完整的座位→(ID,昵称,AgentName)映射。座位顺序稳定(0..6),真人
	// 玩家昵称从 r.recentSpeeches 最近的同 seat speech 中取;bot 玩家
	// 昵称统一为 "Bot N号" + AgentName 来自 r.seatModelKeys(由 manager
	// 启动 Agent 时写入)。空座位显示 "(空)"。
	// 2026-08-13 §20260813-01 U2: 从静态缓存读取,避免每轮重复构建。
	gc.AllPlayers = gc.Static.AllPlayers

	// BUG 2026-07-08: 缓冲期剩余秒数,仅在 pre_wolves 阶段 > 0。
	if gs.Phase == PhasePreWolves && !gs.FirstNightGraceEnd.IsZero() {
		rem := time.Until(gs.FirstNightGraceEnd)
		if rem < 0 {
			rem = 0
		}
		gc.GraceRemainingSec = int(rem.Seconds())
		// BUG Round 40 §95: 强制发言进度字段。
		gc.PreWolvesRound = gs.PreWolvesSpeakRound
		gc.PreWolvesRoundsTotal = gs.PreWolvesSpeakRoundsPerPlayer
		gc.PreWolvesCountForMySeat = gs.PreWolvesSpeakCount[seat]
	}

	// 2026-07-09 §13-bugfix 改造 — 由 r.chatQueues[seat].Snapshot() 改为
	// r.chatQueue.WindowFor(seat)。WindowFor 返回自该 bot read pointer
	// 之后的所有新增消息(公平性保证);防御性 copy 由 Queue 内部完成,
	// 锁顺序 r.mu → q.mu 不反向,持锁调用是安全的。
	if r.chatQueue != nil {
		gc.ChatHistory = r.chatQueue.WindowFor(int(seat))
	}

	// 2026-07-10 §120 增强 — 模型响应速率公平性字段填充。
	// 1. 当前 bot 自己的耗时统计:从 r.BotAgents[seat] 取得 Agent,调 getter。
	//    Agent 在启动时由 StartAgentsLocked 注入 r.BotAgents 映射;
	//    nil-safe:human seat 没有 Agent → 跳过(真人玩家不需要这个块)。
	if botAgent, ok := r.BotAgents[seat]; ok && botAgent != nil {
		gc.ModelName = botAgent.ModelKey // 真实 key(例 "DeepSeek-model");AgentName 由前端格式化
		gc.MyAvgLLMLatencyMs = botAgent.AvgLLMLatencyMs()
		gc.MyLastLLMLatencyMs = botAgent.LastLLMLatencyMs()
		gc.MyTotalLLMCalls = botAgent.TotalLLMCalls()
	}
	// 2. 扫描所有 bot 计算房间最快/最慢模型(基于每个 bot 累计 ≥1 次的 LLM 调用)。
	//    跳过 0 平均耗时(尚未调过 LLM)的 bot。O(7) 遍历,锁内开销可忽略。
	{
		var fastestMs, slowestMs int64
		var fastestName, slowestName string
		// 初始化:首次遍历赋值,后续比较。
		for _, ba := range r.BotAgents {
			if ba == nil || ba.Seat < 0 {
				continue
			}
			avg := ba.AvgLLMLatencyMs()
			if avg <= 0 {
				continue
			}
			if fastestMs == 0 || avg < fastestMs {
				fastestMs = avg
				fastestName = ba.ModelKey
			}
			if slowestMs == 0 || avg > slowestMs {
				slowestMs = avg
				slowestName = ba.ModelKey
			}
		}
		gc.RoomFastestModel = fastestName
		gc.RoomFastestLatencyMs = fastestMs
		gc.RoomSlowestModel = slowestName
		gc.RoomSlowestLatencyMs = slowestMs
	}
	// 3. 是否房间里有真人玩家(非观战者):遍历 r.Seats 看是否有非 bot 占座
	//    的真人 userID(由 StartAgentsLocked 时填充的 BotAgents / botSeats 区分)。
	//    真人座位不在 BotAgents 映射中,简单遍历即可。
	for i := 0; i < MaxPlayers; i++ {
		if r.Seats[i] == "" {
			continue
		}
		if _, isBot := r.BotAgents[i]; !isBot {
			gc.IsHumanInRoom = true
			break
		}
	}
	// 2026-07-24 优化:下发房间暂停状态。Agent handleEvent 入口据此跳过 LLM,
	// 让真人玩家在 UI ⏸ 暂停时不消耗 LLM 配额 / 防批量 quarantine。
	gc.RoomPaused = r.paused

	// 2026-07-10: 12 人标准竞技局字段注入。
	// 2026-08-13 §20260813-01 U2: 从阶段状态缓存读取,避免每轮重复计算。
	gc.SheriffSeat = gc.PhaseState.SheriffSeat
	gc.SheriffStream = gc.PhaseState.SheriffStream
	gc.SheriffCandidates = gc.PhaseState.SheriffCandidates
	gc.IdiotRevealedSeats = gc.PhaseState.IdiotRevealedSeats
	gc.DivineCnt = gc.PhaseState.DivineCnt
	gc.PlainCnt = gc.PhaseState.PlainCnt
	gc.WolfAliveCnt = gc.PhaseState.WolfAliveCnt
	gc.VoteProposed = gc.PhaseState.VoteProposed
	gc.VoteProposer = gc.PhaseState.VoteProposer
	// MyCandidate 是 per-seat 字段,不在阶段状态缓存中。
	if gs.Phase == PhaseSheriff {
		gc.MyCandidate = gs.Players[seat].HasSpoken
	}

	// 2026-07-10 §124 增强 — 情绪字段填充。
	// 1. 当前 bot 自己的情绪:从 r.BotAgents[seat] 取得 Agent,调 getter。
	//    nil-safe:human seat 没有 Agent → 跳过(真人玩家不需要这个块)。
	if botAgent, ok := r.BotAgents[seat]; ok && botAgent != nil {
		gc.MyEmotion = botAgent.CurrentEmotion()
		gc.MyEmotionReason = botAgent.CurrentEmotionReason()
	}
	// 2. 其它 Agent 的情绪:遍历 r.BotAgents,跳过自己与 nil。
	for s, ba := range r.BotAgents {
		if s == seat || ba == nil {
			continue
		}
		gc.OthersEmotion = append(gc.OthersEmotion, wwtypes.SeatEmotionBrief{
			Seat:    s,
			Emotion: ba.CurrentEmotion(),
			// BUG-R238-P0-1 (2026-08-04): Reason 是 LLM 自由文本,可能包含身份自述
			// (如「继续隐藏预言家身份」)。emotion key 是封闭枚举、公开合理,但 reason
			// 不是 —— 填 "" 阻断 agent→agent 身份泄露潜伏路径
			// (对齐 Fix A 的人类侧脱敏;§130「声明了却从不接线」模式)。
			Reason:    "",
			UpdatedAt: ba.EmotionUpdatedAtMs(),
		})
	}

	// 2026-07-12 §127 — 阶段剩余秒数,从 gs.PhaseDeadlineAt 计算供 BuildUserPrompt 注入紧迫感提示。
	if !gs.PhaseDeadlineAt.IsZero() {
		rem := time.Until(gs.PhaseDeadlineAt)
		if rem < 0 {
			rem = 0
		}
		gc.PhaseDeadlineRemainingSec = int(rem.Seconds())
	}

	// 2026-07-21 道具系统 v2 — 填充 GameContext 道具字段（对齐设计文档 §4.1/§7）。
	// · speak 阶段:把可购买道具快照注入 gc.PropSnapshot，驱动 use_prop 工具 schema 动态生成。
	// · 消费 propInjectQueue：把命中者的注入文本 + 干扰信号落地到 GameContext。
	// · 填充个人/全局道具预算 + 冷却状态（供 PropUserPromptBlock 渲染决策辅助）。
	// 注意：gc.EffectXxx 干扰信号是"每轮 transient"——本轮已被消费，必须清零后再应用，
	// 避免上一轮命中信号跨轮残留（v2 防御性重置）。
	if r.propCatalog != nil {
		// personal budget used（全局 / 个人）
		gc.PropSeatBudgetUsed = r.propSeatBudgetUsedLocked(seat)
		gc.RoomPropBudget = r.roomPropBudget()
		gc.RoomPropBudgetUsed = r.roomPropBudgetUsed
		// 冷却剩余秒数
		gc.PropCooldownRemainingSec = r.propCooldownRemainLocked(seat, defaultPropCooldownSec(r))
		// 个人已用 / 上限（默认上限 3，与 defaultProps 对齐）
		gc.PropUsedThisGame = r.propCountForSeatLocked(seat)
		gc.PropMaxPerGame = defaultPropMaxPerGame(r)

		// 干扰信号防御性重置（命中效果仅存活 1 轮，本轮已被上一轮下面消费过）。
		// 2026-08-07 §20260807-04 P2-2:重置前把上一轮被击中的道具 key 转存到
		// r.lastPropHitEffect[seat],下一轮填入 gc.PropHitLastRound 形成反馈闭环。
		gc.EffectExpose = false
		gc.EffectAttentionScatter = false
		gc.ToolUseMaxOverride = 0
		gc.EffectTargetTwistSeat = -1
		gc.EffectForceEmotion = ""
		gc.HumanDebuff = nil
		if r.lastPropHitEffect != nil {
			gc.PropHitLastRound = r.lastPropHitEffect[seat]
		}
		// 本座位的 lastPropHitEffect 在本轮消费后清空(下一轮若未命中即不再提示)。
		if r.lastPropHitEffect == nil {
			r.lastPropHitEffect = make(map[int]string)
		} else {
			delete(r.lastPropHitEffect, seat)
		}

		// 消费注入队列，应用本轮命中效果 + 注入文本。
		if entries := r.drainPropInjectQueueLocked(seat); len(entries) > 0 {
			var injectBuf strings.Builder
			// 合并多个命中效果（任意一个命中即应用其效果；注入文本拼接）。
			for _, entry := range entries {
				if !entry.Hit {
					continue
				}
				if entry.InjectText != "" {
					injectBuf.WriteString(entry.InjectText)
					injectBuf.WriteString("\n")
				}
				// 落地干扰信号到 GameContext（EffectRegistry）。
				// effs 作用域提升到本轮 entry 循环，便于下方 last-effect 文案引用。
				effs := entry.ParseEffectTypes()
				if len(effs) > 0 {
					ApplyEffects(&gc, seat, entry, EffectApplyContext{
						Room: r, Entry: entry, FromSeat: entry.FromSeat,
					})
				}
				gc.PropLastEffect = fmt.Sprintf("%d号对%d号使用了%s(%s)",
					entry.FromSeat+1, seat+1, entry.PropKey, strings.Join(effs, "/"))
				// 2026-08-07 §20260807-04 P2-2:记录本轮被击中的道具 key,供下一轮
				// PropHitLastRound 渲染「上一轮被击中」提示。
				r.lastPropHitEffect[seat] = propHitSummary(entry.PropKey)
			}
			if injectBuf.Len() > 0 {
				gc.PropInjectText = injectBuf.String()
			}
		}

		// speak 阶段：生成可购买道具快照（动态生成 use_prop tool schema）。
		// 过滤：已达个人上限 / 冷却中 / 超全局预算 → 不出现在 snapshot。
		if gc.Phase == "PhaseSpeak" || gc.Phase == "speak" {
			gc.PropSnapshot = buildPropSnapshotLocked(r, gc)
		}

		// v4 §13.2 — 经济档位：按房间总金币存量动态判定(供 EconTierFeedbackBlock 渲染)。
		gc.EconTier = string(ComputeEconTier(r.roomTotalCoin()))

		// v4 §13.1 — 狼小队交流通道快照：仅狼 bot 注入 WolfPackSnapshot + Faction + WolfTeammateSeat。
		// wolf_whisper 工具在 BuildTools 阶段按 faction+WolfTeammateSeat 条件挂载。
		if seat >= 0 && seat < len(r.State.Roles) {
			if f := FactionOf(r.State.Roles[seat]); f != FactionUnknown {
				gc.Faction = f.String()
			}
			if r.State.Roles[seat] == RoleWerewolf {
				if ag, ok := r.BotAgents[seat]; ok && ag != nil {
					gc.WolfTeammateSeat = ag.WolfTeammateSeat
				}
				if r.wolfPack != nil {
					if rawMsgs := r.wolfPack.Snapshot(WolfPackSnapshotMax); len(rawMsgs) > 0 {
						snap := make([]wwtypes.WolfPackMsg, 0, len(rawMsgs))
						for _, m := range rawMsgs {
							snap = append(snap, wwtypes.WolfPackMsg{
								FromSeat:  m.FromSeat,
								Text:      m.Text,
								CreatedAt: m.CreatedAt.Unix(),
							})
						}
						gc.WolfPackSnapshot = snap
					}
					// §20260810-10 U1 — 战术分工 + 轮值狼王快照(仅狼 bot 可见)。
					roleTable, kingSeat := r.wolfPack.RoleSnapshot()
					if len(roleTable) > 0 {
						gc.WolfPackRoleTable = roleTable
						gc.WolfPackRole = roleTable[seat]
						gc.WolfKingSeat = kingSeat
					}
					// §20260811-04 U1 — 暗号系统快照(仅狼 bot 可见,§119 协议层隔离)。
					if r.wolfPackCipher != nil {
						bundle := r.wolfPackCipher.Get(seat, r.State.DayNumber)
						if len(bundle.Templates) > 0 {
							spec := CipherBundleToAgentSpec(bundle)
							gc.WolfPackCipher = &spec
						}
					}
				}
			}
		}

		// v3 §G3 增强 — 把 bot 金币余额 + 单局底注写入 GameContext,
		// 让 WalletSustainabilityBlock 能渲染"可承受局数 + 紧急度"。
		// 失败 / 玩家非 bot → 余额=0,WalletSustainabilityBlock 自然不渲染。
		if userID := r.Seats[seat]; userID != "" {
			if walletSvc := mgrPropEngineWalletSvc(r); walletSvc != nil {
				if bal, balErr := walletSvc.GetBalance(context.Background(), userID); balErr == nil {
					gc.WalletBalance = bal
				}
			}
		}
		gc.AnteAmount = werewolfAnteAmountLocked(r)

		// v3 §G5 — 把最近 20 条道具使用公开历史写入 GameContext,
		// 供 prop_history 工具查询（环形 buffer 由 room 维护）。
		// 类型转换 werewolf.PropHistoryRecord → wwtypes.PropHistoryRecord（字段同形）。
		if rawHist := r.GetPropHistoryLocked(20); len(rawHist) > 0 {
			snapHist := make([]wwtypes.PropHistoryRecord, 0, len(rawHist))
			for _, h := range rawHist {
				snapHist = append(snapHist, wwtypes.PropHistoryRecord{
					FromSeat:   h.FromSeat,
					ToSeat:     h.ToSeat,
					PropKey:    h.PropKey,
					PropNameZh: h.PropNameZh,
					Hit:        h.Hit,
					EffectHint: h.EffectHint,
					Phase:      h.Phase,
					Round:      h.Round,
					CreatedAt:  h.CreatedAt,
				})
			}
			gc.PropHistorySnapshot = snapHist
		}
	}

	// R176 P2 补缺(v4 链式效果):tick 调度表,把已到期的延迟 step 应用到本座位的 gc。
	// 单调用方构建的 gc 在持锁期内不会跨座位共享,所以这里仅 tick 本座位。
	if len(r.propEffectSchedule) > 0 {
		r.propEffectRoundCounter++
		for key, item := range r.propEffectSchedule {
			if item.TargetSeat != seat {
				continue
			}
			elapsed := r.propEffectRoundCounter - item.CreatedAtCall
			if elapsed < item.DueAfterCalls {
				continue
			}
			if !evaluatePropStepCondition(item.Step.Condition, &gc, seat) {
				delete(r.propEffectSchedule, key)
				continue
			}
			ApplyEffects(&gc, seat, PropInjectEntry{
				PropKey:     item.PropKey,
				FromSeat:    item.FromSeat,
				EffectTypes: item.Step.EffectType,
				TwistSeat:   -1,
				Hit:         true,
			}, EffectApplyContext{FromSeat: item.FromSeat, Entry: PropInjectEntry{
				PropKey:     item.PropKey,
				EffectTypes: item.Step.EffectType,
				TwistSeat:   -1,
			}})
			delete(r.propEffectSchedule, key)
		}
	}

	// 2026-08-10 §20260810-06 — 注入承诺数据到 GameContext。
	// MyCommitments: 本 bot 自己的承诺（含真实状态）。
	// PublicCommitments: 公开可见的他人承诺（仅 pending 状态）。
	if r.commitmentLedger != nil {
		myComms := r.commitmentLedger.GetCommitmentsForViewerLocked(seat)
		publicComms := r.commitmentLedger.GetCommitmentsForViewerLocked(-2) // -2 = 他人 pending 视图
		// 分离自己的承诺和他人的公开承诺
		for _, c := range myComms {
			if c.Seat == seat {
				gc.MyCommitments = append(gc.MyCommitments, wwtypes.CommitmentInfo{
					ID:        c.ID,
					Round:     c.Round,
					Template:  string(c.Template),
					ParamSeat: c.ParamSeat,
					Reason:    c.Reason,
					Status:    string(c.Status),
				})
			}
		}
		for _, c := range publicComms {
			if c.Seat != seat {
				gc.PublicCommitments = append(gc.PublicCommitments, wwtypes.CommitmentInfo{
					ID:        c.ID,
					Round:     c.Round,
					Template:  string(c.Template),
					ParamSeat: c.ParamSeat,
					Reason:    c.Reason,
					Status:    string(c.Status),
				})
			}
		}
	}

	// 2026-08-10 §20260810-08 — 信息账本二期行为侧消费。
	// 锁内从单一账本聚合本 seat 的知情清单；只注入 GameContext→prompt，
	// 不进入 chat_message / chat_history / BotTranscript（§119）。
	if digest := r.ledgerLocked().DigestForSeat(seat, 2); digest != nil {
		gc.KnowledgeDigest = &wwtypes.KnowledgeDigest{
			Seat:        digest.Seat,
			TotalKnown:  digest.TotalKnown,
			TotalInRoom: digest.TotalInRoom,
			Entries:     make([]wwtypes.KnowledgeDigestEntry, 0, len(digest.Entries)),
		}
		for _, entry := range digest.Entries {
			gc.KnowledgeDigest.Entries = append(gc.KnowledgeDigest.Entries, wwtypes.KnowledgeDigestEntry{
				Source: string(entry.Source), Count: entry.Count, LastRound: entry.LastRound,
				Highlights: append([]string(nil), entry.Highlights...),
			})
		}
	}

	// 2026-08-10 §20260810-07 — 多假说并行推演。
	// §128 对话即思考:bot 在 LastDecisionSummary 末尾的「📊 [...]」JSON 段
	// 提交假说更新,由 HypothesisStore 解析后填到 gc.HypothesisTable,LLM
	// 在 user prompt 末尾追加「📊 你的当前假说」块;§119 协议层隔离 —
	// 不进 chat_message / chat_history,仅本人 bot 可见。
	// §20260811-09 U2 — easy 档不注入假说表(降低 LLM 上下文复杂度,
	// 新手 Agent 不需要看自己的历史推理)。normal/hard/hell 维持现状。
	if !ProfileFor(AgentDifficulty(r.agentDifficulty)).InjectHypotheses {
		// skip
	} else if ht := r.hypothesisStoreLocked().GetLocked(seat); ht != nil {
		snap := &wwtypes.HypothesisTableSnapshot{
			Seat:      ht.Seat,
			Round:     ht.Round,
			UpdatedAt: ht.UpdatedAt,
			Entries:   make([]wwtypes.HypothesisEntrySnapshot, 0, len(ht.Entries)),
		}
		for _, e := range ht.Entries {
			snap.Entries = append(snap.Entries, wwtypes.HypothesisEntrySnapshot{
				TargetSeat: e.TargetSeat,
				RoleGuess:  e.RoleGuess,
				Confidence: e.Confidence,
				Supporting: e.Supporting,
				Refuting:   e.Refuting,
				UpdatedAt:  e.UpdatedAt,
			})
		}
		gc.HypothesisTable = snap
	}

	// §20260810-11 H2 — 公开质疑:本 bot 是被质疑者时,把 LastChallengedBy/Question
	// 拷贝到 GameContext.LastChallenge,ChallengeBlock 据此渲染。
	// 协议层:质疑内容已通过活动流公开(§119),本镜像仅做 LLM 上下文传递。
	if seat >= 0 && seat < len(r.State.Players) {
		p := r.State.Players[seat]
		if p.LastChallengedBy >= 0 && p.LastChallengeQuestion != "" {
			gc.LastChallenge = &wwtypes.LastChallengeSpec{
				BySeat:   p.LastChallengedBy,
				Question: p.LastChallengeQuestion,
			}
		}
		// §20260811-06 U4 — 行为一致性校验:
		// BotTranscript.LastConsistencyCheck 字段已在 recordTranscript 末尾自动
		// 填充;prompt 渲染链路仅在 BotTranscript→BotContextJSON 路径上读该字段。
		// 玩家侧 sanitizeBotTranscript 不清空 LastConsistencyCheck(本身不揭示身份)。

		// §20260811-06 U5 — 黎明流言:把房间级 LastRumors 拷贝到 GameContext,
		// prompt.go::RumorBlock 据此渲染给本 bot。
		if rumors := buildAgentContextRumorBlock(r); len(rumors) > 0 {
			gc.LastRumors = rumors
		}
	}

	// §20260811-02 U1 — 发言影响力生态:把全场公开影响力快照注入 GameContext。
	// tracker 为空(第 1 天首次投票结算前)时整段 omitempty,InfluenceBlock 渲染空串。
	if snap := r.influenceTrackerLocked().SnapshotLocked(); len(snap) > 0 {
		briefs := make([]wwtypes.InfluenceBrief, 0, len(snap))
		for _, s := range snap {
			b := wwtypes.InfluenceBrief{
				Seat:       s.Seat,
				Total:      s.Total,
				Persuasion: s.Persuasion,
				Attention:  s.Attention,
				Presence:   s.Presence,
				Survival:   s.Survival,
			}
			briefs = append(briefs, b)
			if s.Seat == seat {
				mine := b
				gc.MyInfluence = &mine
			}
		}
		gc.InfluenceScores = briefs
	}

	// 2026-08-11 §20260811-05 U1 — 玩家行为画像注入(纯内存读房间缓存,
	// 热路径零 DB 查询;缓存由 PrefetchPlayerProfiles 在开局/入座时预取)。
	// 仅人类座位有值;全 AI 房间或无画像时 nil,PlayerProfileBlock 渲染空串。
	fillPlayerProfilesLocked(r, seat, &gc)

	return gc
}

func NewWerewolfManager() *WerewolfManager {
	return NewWerewolfManagerWithRegistry(nil)
}

func NewWerewolfManagerWithRegistry(registry *llm.Registry) *WerewolfManager {
	return &WerewolfManager{
		rooms:                make(map[string]*WerewolfRoom),
		seedFn:               func() int64 { return time.Now().UnixNano() },
		registry:             registry,
		spectatorWakeLimiter: agentcore.NewSpeakLimiter(15 * time.Second), // 2026-07-08 §13.6
	}
}

// SetRewardService §20260810-11 P1 — 注入终局奖励服务(可选)。
func (m *WerewolfManager) SetRewardService(svc *SettlementRewardService) {
	m.rewardSvc = svc
}

func (m *WerewolfManager) Registry() *llm.Registry {
	if m == nil {
		return nil
	}
	return m.registry
}

func (m *WerewolfManager) getRoom(roomID string) *WerewolfRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[roomID]
}

func (m *WerewolfManager) GetRoom(roomID string) *WerewolfRoom {
	return m.getRoom(roomID)
}

func (m *WerewolfManager) FindUserRoom(userID string) (*WerewolfRoom, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.findUserRoomLocked(userID)
}

func (m *WerewolfManager) findUserRoomLocked(userID string) (*WerewolfRoom, int) {
	for _, r := range m.rooms {
		if r == nil {
			continue
		}
		seat, ok := r.seatOfLocked(userID)
		if ok {
			return r, int(seat)
		}
	}
	return nil, -1
}

func (r *WerewolfRoom) seatOfLocked(userID string) (Seat, bool) {
	for i, u := range r.Seats {
		if u == userID {
			return Seat(i), true
		}
	}
	return NoSeat, false
}

func (m *WerewolfManager) ChatQueueSnapshot(roomID string) ([]agentcore.ChatMessage, map[int]uint64, int, bool) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, nil, 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.chatQueue == nil {
		return nil, nil, 0, true // room exists, but queue not yet allocated
	}
	msgs := r.chatQueue.Snapshot()
	pointers := map[int]uint64{}
	for seat := range r.BotAgents {
		pointers[seat] = r.chatQueue.ReadPointer(seat)
	}
	return msgs, pointers, r.chatQueue.TotalBytes(), true
}

func (m *WerewolfManager) RestartVoteSnapshot(roomID string) (map[string]any, bool) {
	r := m.getRoom(roomID)
	if r == nil || r.State == nil {
		return nil, false
	}
	if r.State.Phase != PhaseRestartVote || r.State.RestartVoteDone {
		return nil, false
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return nil, false
	}
	defer r.mu.Unlock()

	eligible := restartVoteEligibleSeatsLocked(r)
	yesList := make([]int, 0, len(r.State.RestartVoteYes))
	for s := range r.State.RestartVoteYes {
		yesList = append(yesList, int(s))
	}
	noList := make([]int, 0, len(r.State.RestartVoteNo))
	for s := range r.State.RestartVoteNo {
		noList = append(noList, int(s))
	}
	absList := make([]int, 0, len(r.State.RestartVoteAbstain))
	for s := range r.State.RestartVoteAbstain {
		absList = append(absList, int(s))
	}
	sort.Ints(yesList)
	sort.Ints(noList)
	sort.Ints(absList)

	num, den := restartVoteQuorumFromConfig()
	// FastRestart 模式降为简单多数。
	if r.State.FastRestart {
		num, den = 1, 2
	}
	yesQuota := (len(eligible)*num + den - 1) / den
	if yesQuota < 1 {
		yesQuota = 1
	}
	payload := map[string]any{
		"room_id":        roomID,
		"phase":          r.State.Phase.String(),
		"yes":            yesList,
		"no":             noList,
		"abstain":        absList,
		"eligible_count": len(eligible),
		"yes_quota":      yesQuota + 1,
		"decided":        false,
		"result":         "",
		"winner":         r.State.Winner,
		"fast_restart":   r.State.FastRestart,
	}
	if !r.State.PhaseDeadlineAt.IsZero() {
		payload["deadline_at"] = r.State.PhaseDeadlineAt.UTC().Format(time.RFC3339)
		rem := time.Until(r.State.PhaseDeadlineAt)
		if rem < 0 {
			rem = 0
		}
		payload["remaining_sec"] = int(rem.Seconds())
	}
	return payload, true
}

func (m *WerewolfManager) JoinGame(roomID, userID string) (*WerewolfRoom, bool, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &WerewolfRoom{
			RoomID:         roomID,
			createdAt:      time.Now(),
			recentSpeeches: make([]wwtypes.SpeechEvent, 0, recentSpeechBufferSize),
			whisperInbox:   make(map[int][]wwtypes.WhisperEvent, MaxPlayers),
			// 2026-07-21 道具系统 — 初始化道具状态。
			propCooldown:    make(map[int]time.Time),
			propCount:       make(map[int]int),
			propInjectQueue: make(map[int][]PropInjectEntry),
			// 2026-07-22 R176 P2 补缺 — v4 链式效果延迟调度表。
			propEffectSchedule: make(map[string]PropEffectScheduledItem),
			// BUG-R242-P1-01: llmSema 由 StartAgentsLocked 懒创建(不在此处设)。
		}
		m.rooms[roomID] = r
		logger.L().Info("werewolf room created",
			zap.String("room_id", roomID), zap.String("by", userID))
	}
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// BUG-WEREWOLF-P0-7 FIX: a freshly-created (empty) room for a roomID that
	// already has persisted agent seats means we just restarted. Hydrate the
	// in-memory room from DB BEFORE the new player joins, otherwise the
	// player creates an empty room that the auto-start path will never
	// recognize as "7-AI ready" (no ManagerAddPlayerAt, no SetSeatModelKey).
	if m.hydrator != nil && len(r.seatModelKeys) == 0 && r.Occupied() == 0 {
		if agents, e := m.hydrator(roomID); e == nil && len(agents) > 0 {
			m.restoreBotsLocked(r, agents)
		}
	}

	// 幂等
	if _, seated := r.SeatOf(userID); seated {
		return r, false, nil
	}

	if r.State != nil && r.State.Phase != PhaseFilling {
		// 已开局不能再入座;但允许"补位"(离线后的占位)→ 简化:
		// 7人局固定,开局后不允许任何加座
		return nil, false, errcode.Code(errcode.ErrRoomFull)
	}

	seat, e := r.State_Begin().AddPlayer(userID)
	if e != nil {
		return nil, false, e
	}
	r.Seats[seat] = userID

	// 2026-08-06 §20260806-03: 创建者(人类)入座后回填其角色偏好。
	// SetSeatRolePrefs 在 RegisterAgentSeats 之前调用时创建者尚无座位,
	// 偏好被暂存为 pending;此处人类实际入座,立即落到具体座位。
	//
	// 注意:不能用 isBotUserIDLocked 判定"是否人类" — 该函数通过 hydrator
	// (SeatsForRoom) 拉全房间 player 行(含人类)做匹配,混合房间重启恢复的
	// 设计使人类也会被匹配到 → 误判为 bot,回填被跳过(实测 creator e2bd5733
	// 被 hydrator 返回,isBot=true)。改用 Players[seat].IsBot:SetSeatModelKey
	// 在 RegisterAgentSeats 阶段仅对 bot 座位置位,人类座位保持 false。
	if r.pendingCreatorRolePref > RoleUnknown {
		isBot := seat >= 0 && seat < MaxPlayers && r.State != nil && r.State.Players[seat].IsBot
		if !isBot {
			if r.seatPreferredRoles == nil {
				r.seatPreferredRoles = make(map[int]Role)
			}
			r.seatPreferredRoles[int(seat)] = r.pendingCreatorRolePref
			r.pendingCreatorRolePref = RoleUnknown
			// 2026-08-11 BUG-ROLE-MISMATCH-P0:创建者偏好回填立即记 Info 日志
			// (此前静默回填,实测「选猎人发猎魔人」无法定位是回填失败还是
			// ApplyPreferredRoles unmet;有这行日志即可区分两类根因)。
			logger.L().Info("werewolf creator role pref consumed",
				zap.String("room_id", roomID),
				zap.Int("seat", int(seat)),
				zap.String("role", r.seatPreferredRoles[int(seat)].String()))
		}
	}

	started := false
	if r.State == nil || r.State.Phase == PhaseFilling {
		// 首次开:创建 GameState
		if r.State == nil {
			r.State = NewGame(m.seedFn())
		}
	}

	// 同步 SeatCount 到 State,驱动发牌选择(默认 12;werewolf_7 已由 SetSeatCount 设为 7)。
	if r.State.SeatCount == 0 {
		r.State.SeatCount = MaxPlayers
	}
	if r.SeatCount > 0 && r.SeatCount != r.State.SeatCount {
		r.State.SeatCount = r.SeatCount
	}

	// 本局人数齐 → 自动开局
	if r.IsReady() {
		// §130 重构(2026-07-13):有 Agent + 真人时,先进入"人类等待窗口"。
		// 等待期间人类可在聊天室自由发言;watchdog 在 deadline 到期后
		// 才会执行真正的 StartGame + StartAgentsLocked + wake。
		if m.tryStartWithHumanWaitLocked(r) {
			// 等待窗口已建立,稍后由 watchdog 启动;此处不立即开始。
			started = false
		} else {
			// 2026-08-06 §20260806-03: 发牌前同步座位角色偏好。
			syncPreferredRolesLocked(r)
			if err := r.State.StartGame(); err != nil {
				logger.L().Warn("werewolf start game failed",
					zap.String("room_id", roomID), zap.Error(err))
			} else {
				started = true
				r.gameStartedAt = time.Now().Unix()
				// 2026-08-10 §20260810-05 — 信息账本 role_deal 登记(发牌成功路径)。
				r.ledgerRegisterRoleDealLocked()
				// 2026-07-14 BUG-R116-03: 新一局开始时重置单座位发言冷却。
				r.seatLastPublicSpeak = make(map[int]time.Time)
				// 2026-07-15 BUG-R124-UI-001: 新一局开始时清零单座位每阶段发言计数。
				r.seatSpeakCountThisPhase = make(map[int]int)
				r.seatSpeakCountPhaseTag = ""
				logger.L().Info("werewolf game started",
					zap.String("room_id", roomID),
					zap.Int64("seed", r.State.Seed))
				// Phase 4: start agent goroutines for each registered bot seat.
				m.StartAgentsLocked(r)
				// 2026-07-10 §125 增强 — 启动法官 goroutine(若 JudgeMode 启用)。
				// 2026-07-30 §重构:启动条件改为 cfgWerewolfJudgeMode() != "off"
				// 且 r.JudgeDesired=true。
				m.startJudgeGoroutine(r)
				// §20260811-09 U1 — 启动 AI 实时解说 goroutine(若 commentaryDesired=true)。
				// spectator-only 回调走 Hub.BroadcastRoomSpectators(玩家收不到)。
				m.startCommentatorGoroutine(r, commentarySpectatorHook)
				// BUG-WEREWOLF-NO-WAKE: 复述段落已压缩 — git blame 与 docs/ 索引可还原

				m.wakeAllAgentsLocked(r, "state_change", wwtypes.GameContext{Phase: r.State.Phase.String()})
				// Notify caller to update DB room status from "open" to "playing".
				if m.onGameStarted != nil {
					m.onGameStarted(roomID)
				}
			}
		}
	}
	// 同步 Seats
	for i := range r.Seats {
		if r.Seats[i] == "" && r.State.Players[i].UserID != "" {
			r.Seats[i] = r.State.Players[i].UserID
		}
	}
	return r, started, nil
}

func (m *WerewolfManager) restoreBotsLocked(r *WerewolfRoom, agents []AgentSeatInfo) {
	if r.seatModelKeys == nil {
		r.seatModelKeys = make(map[int]string, len(agents))
	}
	if r.State == nil {
		r.State = NewGame(m.seedFn())
	}
	for _, a := range agents {
		if a.Seat < 0 || a.Seat >= MaxPlayers {
			continue
		}
		if _, e := r.State.AddPlayerAt(a.UserID, Seat(a.Seat)); e != nil {
			logger.L().Warn("werewolf restoreBots: AddPlayerAt failed",
				zap.String("room_id", r.RoomID),
				zap.Int("seat", a.Seat),
				zap.String("msg", e.Message))
			continue
		}
		// §127: 标记 bot 座位供 setPhaseAndDeadline 人机 deadline 区分。
		r.State.Players[a.Seat].IsBot = true
		r.Seats[a.Seat] = a.UserID
		r.seatModelKeys[a.Seat] = a.ModelKey
	}
	logger.L().Info("werewolf room hydrated from DB after restart",
		zap.String("room_id", r.RoomID),
		zap.Int("bots", len(agents)))
}

func buildAllPlayersLocked(r *WerewolfRoom) []wwtypes.PlayerBrief {
	if r.State == nil {
		return nil
	}
	// Build a quick lookup for human-account-from-recent-speech.
	lastAcct := make(map[int]string)
	for i := len(r.recentSpeeches) - 1; i >= 0; i-- {
		s := r.recentSpeeches[i]
		if s.IsBot || s.Account == "" || s.IsSpectator {
			continue
		}
		if _, ok := lastAcct[s.Seat]; !ok {
			lastAcct[s.Seat] = s.Account
		}
	}
	out := make([]wwtypes.PlayerBrief, 0, MaxPlayers)
	for i := 0; i < MaxPlayers; i++ {
		uid := r.State.Seats[i]
		brief := wwtypes.PlayerBrief{Seat: i, UserID: uid}
		if uid == "" {
			brief.Account = "(空座位)"
			out = append(out, brief)
			continue
		}
		if modelKey, ok := r.seatModelKeys[i]; ok {
			brief.IsBot = true
			brief.AgentName = modelKey
			brief.Account = "Bot " + strconv.Itoa(i+1) + "号"
		} else {
			brief.IsBot = false
			if a, ok := lastAcct[i]; ok {
				brief.Account = a
			} else {
				brief.Account = "玩家" + strconv.Itoa(i+1) + "号"
			}
		}
		out = append(out, brief)
	}
	return out
}

const stopAgentsWaitTimeout = 5 * time.Second

// SetSeatRolePrefs 2026-08-06 §20260806-03 — 房间创建时落地座位级角色偏好。
// prefs: seat → 角色名字符串(白名单见 ParseRoleName);"random"/"" = 不指定。
// creatorPref: 创建者(人类)的角色偏好 — 此时创建者尚未 SyncSeat 入座,
// 暂存到 pendingCreatorRolePref,JoinGame 入座成功后回填。
// 必须在 RegisterAgentSeats 之前调用(对齐 BUG-R136-RACE-001 的 SetJudgeConfig
// 时序:RegisterAgentSeats 在 13/13 满座时可能立即触发 ForceStartIfReady →
// StartGame 发牌,偏好必须先就位)。
func (m *WerewolfManager) SetSeatRolePrefs(roomID string, prefs map[int]string, creatorPref string) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &WerewolfRoom{
			RoomID:         roomID,
			createdAt:      time.Now(),
			recentSpeeches: make([]wwtypes.SpeechEvent, 0, recentSpeechBufferSize),
			whisperInbox:   make(map[int][]wwtypes.WhisperEvent, MaxPlayers),
		}
		m.rooms[roomID] = r
	}
	m.mu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seatPreferredRoles == nil {
		r.seatPreferredRoles = make(map[int]Role)
	}
	for seat, name := range prefs {
		if role, valid := ParseRoleName(name); valid && role > RoleUnknown {
			r.seatPreferredRoles[seat] = role
		}
	}
	if role, valid := ParseRoleName(creatorPref); valid && role > RoleUnknown {
		r.pendingCreatorRolePref = role
	}
}

// syncPreferredRolesLocked 把 r.seatPreferredRoles 同步到 r.State.PreferredRoles,
// 供 StartGame 发牌后做偏好置换。caller 必须持 r.mu(§92a)。
func syncPreferredRolesLocked(r *WerewolfRoom) {
	if r == nil || r.State == nil || len(r.seatPreferredRoles) == 0 {
		return
	}
	prefs := make(map[int]Role, len(r.seatPreferredRoles))
	for seat, role := range r.seatPreferredRoles {
		prefs[seat] = role
	}
	r.State.PreferredRoles = prefs
}

func (m *WerewolfManager) SetSeatModelKey(roomID string, seat int, modelKey string) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &WerewolfRoom{
			RoomID:         roomID,
			createdAt:      time.Now(),
			recentSpeeches: make([]wwtypes.SpeechEvent, 0, recentSpeechBufferSize),
			whisperInbox:   make(map[int][]wwtypes.WhisperEvent, MaxPlayers),
			// BUG-R242-P1-01: llmSema 由 StartAgentsLocked 懒创建(不在此处设)。
		}
		m.rooms[roomID] = r
	}
	m.mu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seatModelKeys == nil {
		r.seatModelKeys = make(map[int]string)
	}
	r.seatModelKeys[seat] = modelKey
	// §127: 标记该座位为 bot,供 setPhaseAndDeadline 人机 deadline 区分使用。
	if r.State != nil && seat >= 0 && seat < len(r.State.Players) {
		r.State.Players[seat].IsBot = true
	}
}

func (m *WerewolfManager) SetAgentDifficulty(roomID string, difficulty string) {
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setAgentDifficultyLocked(difficulty)
}

// SetCommentaryConfig §20260811-09 U1 — 把房间级 AI 解说配置落到 in-memory WerewolfRoom。
// 同 SetJudgeConfig 时序约束:必须在 RegisterAgentSeats 之前调用。
func (m *WerewolfManager) SetCommentaryConfig(roomID string, cfg *CommentaryConfig) {
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setCommentaryConfigLocked(cfg)
}

func (m *WerewolfManager) SetJudgeConfig(roomID string, desired bool, mode string, modelKey string) {
	r := m.getRoom(roomID)
	if r == nil {
		m.mu.Lock()
		// BUG-R137-P0-001 修复:在外层预先声明 ok,使赋值 `r, ok = m.rooms[roomID]`
		// 复用外层 r,避免 if 块作用域内 `:=` 创建新局部变量导致外层 r 仍为 nil
		// → 后续 r.mu.Lock() 触发 nil pointer panic。
		var ok bool
		r, ok = m.rooms[roomID]
		if !ok {
			r = &WerewolfRoom{
				RoomID:         roomID,
				createdAt:      time.Now(),
				recentSpeeches: make([]wwtypes.SpeechEvent, 0, recentSpeechBufferSize),
				whisperInbox:   make(map[int][]wwtypes.WhisperEvent, MaxPlayers),
			}
			m.rooms[roomID] = r
		}
		m.mu.Unlock()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.JudgeDesired = desired
	// 2026-07-30 §重构:透传房间级法官模式字符串(agent/human)。
	// 归一化:未知值回退 "agent",与 JudgeConfig 默认值对齐。
	switch mode {
	case "agent", "human":
		r.JudgeMode = mode
	default:
		r.JudgeMode = "agent"
	}
	r.JudgeModelKey = modelKey
	// 初值哨兵:确保首个真实 phase 的切换能被检测到(PhaseFilling=0 是零值,
	// 若不加哨兵,首 tick 在 PhasePreWolves 时 lastJudgePhase=PhaseFilling 仍会触发,
	// 但显式哨兵更稳健,且与 docs/狼人杀-重构方案/主持人Agent重构设计.md §1.3 一致)。
	r.lastJudgePhase = Phase(-1)
}

func (m *WerewolfManager) SetSeatCount(roomID string, n int) {
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 {
		n = MaxPlayers
	}
	r.SeatCount = n
	if r.State != nil {
		r.State.SeatCount = n
	}
}

func (m *WerewolfManager) SeatModelKey(roomID string, seat int) string {
	r := m.getRoom(roomID)
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seatModelKeys[seat]
}

func (m *WerewolfManager) AgentSeats(roomID string) map[int]string {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[int]string, len(r.seatModelKeys))
	for seat, mk := range r.seatModelKeys {
		out[seat] = mk
	}
	return out
}
