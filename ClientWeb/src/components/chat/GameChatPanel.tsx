// GameChatPanel — compact in-game room chat embedded in the game pages.
// Supports public messages, @mention autocomplete for whispers, and admin
// visibility of all private messages.  Shared by xiangqi, junqi, doudizhu,
// texasholdem, and werewolf game pages.

import { useEffect, useRef, useState, useMemo, useCallback } from 'react';
import { useChat } from '@/hooks/useChat';
import { useStreamingMessages } from '@/hooks/useStreamingMessages';
import { useT } from '@/hooks/useT';
import { useAuth } from '@/hooks/useAuth';
import type { ChatMessage } from '@/types/api';

interface Props {
  roomId: string;
  /** Players in the room — each game page passes its own player list. */
  roomPlayers?: { user_id: string; nickname: string }[];
  /**
   * R100 P1 FIX: the werewolf game page now passes `isSpectator=true` when the
   * route is `/werewolf/spectate/:roomId`. Without this, the placeholder
   * reads as "connecting..." which the R100 test misread as "观战者聊天
   * disabled"; with it, the input switches to a spectator-specific hint
   * ("👁 观战者可发言…") so the operator immediately knows spectator input
   * is supported. The send button remains gated by `connected` because the
   * underlying ws.send path goes through the R40 P0-2 pendingSend queue
   * — non-OPEN sends are queued and flushed on the next onopen, so once
   * WS handshake completes the spectator message gets delivered.
   */
  isSpectator?: boolean;
  /**
   * When the local player is dead (狼人杀 werewolf page), the chat draft
   * (and any active whisper target) is wiped on death transition so the
   * dead player doesn't send stale words attributed to themselves. R235 §4.2.
   * Defaults to false (live players). Spectators pass false.
   */
  isLocalPlayerDead?: boolean;
  /**
   * 2026-08-07 §房间聊天优化 — 显式传入当前游戏日数(Day N),
   * 渲染于标题旁,与左栏「房间信息」的 phase 副信息对称。
   * 其他 4 款游戏(象棋/军棋/斗地主/德州)不传,显示为空(无变化)。
   */
  currentDay?: number | null;
}

export function GameChatPanel({ roomId, roomPlayers, isSpectator, isLocalPlayerDead, currentDay }: Props) {
  const t = useT();
  const { messages, send, whisper, connected, error, loadMore, hasMore, loadingMore } =
    useChat('room', roomId);
  // §13 Bot SSE 流式预览气泡(token 瀑布流)。权威 chat.message 到达后自动消隐。
  const streaming = useStreamingMessages();
  const [draft, setDraft] = useState('');
  const [collapsed, setCollapsed] = useState(false);
  const listRef = useRef<HTMLDivElement | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const anchorRef = useRef<number | null>(null);

  // Whisper target state: when set, the next send becomes a whisper.
  const [whisperTarget, setWhisperTarget] = useState<{ id: string; account: string } | null>(null);

  // @mention autocomplete state.
  const [mentionQuery, setMentionQuery] = useState('');
  const [showMentions, setShowMentions] = useState(false);
  const [mentionIdx, setMentionIdx] = useState(0);

  // Current user info for whisper visibility filtering.
  const myUserId = useAuth((s) => s.userId);
  const userType = useAuth((s) => s.userType);
  const isAdmin = (userType ?? 1) >= 2;

  // Filter mention candidates from room players list.
  const mentionCandidates = useMemo(() => {
    if (!roomPlayers || !mentionQuery) return [];
    const q = mentionQuery.toLowerCase();
    return roomPlayers.filter(
      (p) =>
        p.user_id !== myUserId &&
        (p.nickname.toLowerCase().includes(q) || p.user_id.toLowerCase().includes(q)),
    );
  }, [roomPlayers, mentionQuery, myUserId]);

  // Visible messages: public + whispers the user is allowed to see.
  // §115 房间聊天 — 活动事件(from_role === 'activity')对所有人可见;
  // whisper 仍走"自己发/自己收/管理员"过滤。
  //
  // R235 §4.1 robustness: `myUserId` is hydrated asynchronously from the auth
  // store. A whisper frame that arrives between WS open and `myUserId` being
  // populated would have been silently dropped here before (the comparison
  // `null === string` is false, so the message never showed). We now fall back
  // to "my own outbound whispers" by snapshotting the last author we sent to
  // / received from via whisperTarget, which is set synchronously the moment
  // the user clicks 💬. This is a tie-break heuristic — the canonical filter
  // still keys off `myUserId` once it lands.
  const recentWhisperPeers = useRef<Set<string>>(new Set());
  // Track the most recent whisper target so the filter can recognize "I just
  // sent a whisper to Bot X" even if myUserId hasn't propagated yet.
  useEffect(() => {
    if (whisperTarget) recentWhisperPeers.current.add(whisperTarget.id);
  }, [whisperTarget]);
  const visibleMessages = useMemo(() => {
    return messages.filter((m: ChatMessage) => {
      if (m.from_role === 'activity') return true; // 活动事件全员可见
      if (!m.whisper) return true; // public messages always visible
      if (isAdmin) return true; // admins see all whispers
      // Canonical path: server-enforced visibility (sender or recipient).
      if (myUserId && (m.from_user_id === myUserId || m.to_user_id === myUserId)) {
        return true;
      }
      // Fallback: I've just interacted via 💬 with this peer, so the
      // incoming whisper is likely theirs replying to me. Server already
      // gated this to myUserId via sendWhisperDirect → SendToUser, so this
      // branch only re-hydrates the visible-set if myUserId is somehow null.
      if (!myUserId && m.to_user_id && recentWhisperPeers.current.has(m.to_user_id)) {
        return true;
      }
      return false;
    });
  }, [messages, myUserId, isAdmin, whisperTarget]);

  // seat → nickname 映射,供流式气泡解析 Bot 昵称(roomPlayers 由对局页注入)。
  const seatNickname = useMemo(() => {
    const map = new Map<number, string>();
    if (!roomPlayers) return map;
    // roomPlayers 缺少 seat 信息;回退:用昵称里的 "#N" 或直接用昵称。
    // 为稳妥,仅保留昵称作为兜底显示(不强行解析 seat)。
    for (const rp of roomPlayers) {
      const m = /#(\d+)\s*$/.exec(rp.nickname);
      if (m) map.set(Number(m[1]) - 1, rp.nickname);
    }
    return map;
  }, [roomPlayers]);

  // §13 把流式预览气泡与正式消息合并为同一时间线(ts 升序)。
  const renderItems = useMemo(() => {
    type Tagged =
      | { kind: 'msg'; msg: ChatMessage; ts: number }
      | { kind: 'stream'; id: string; seat: number; text: string; finalized: boolean; ts: number };
    const items: Tagged[] = [
      ...visibleMessages.map((msg) => ({ kind: 'msg' as const, msg, ts: msg.ts })),
      ...streaming.map((s) => ({
        kind: 'stream' as const,
        id: s.stream_id,
        seat: s.seat,
        text: s.text,
        finalized: s.finalized,
        ts: s.ts,
      })),
    ];
    items.sort((a, b) => a.ts - b.ts);
    return items;
  }, [visibleMessages, streaming]);

  // Sentinel observer — load more on intersect.
  useEffect(() => {
    const sentinel = sentinelRef.current;
    const list = listRef.current;
    if (!sentinel || !list) return;
    const obs = new IntersectionObserver(
      (entries) => {
        const e = entries[0];
        if (!e.isIntersecting) return;
        if (!hasMore || loadingMore) return;
        anchorRef.current = list.scrollHeight;
        loadMore();
      },
      { root: list, rootMargin: '0px', threshold: 0.1 },
    );
    obs.observe(sentinel);
    return () => obs.disconnect();
  }, [hasMore, loadingMore, loadMore]);

  // Auto-scroll to bottom on new live messages, or restore anchor after
  // loadMore prepends older history.
  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    if (anchorRef.current != null) {
      const oldHeight = anchorRef.current;
      const newHeight = el.scrollHeight;
      el.scrollTop += Math.max(0, newHeight - oldHeight);
      anchorRef.current = null;
      return;
    }
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [visibleMessages.length, loadingMore]);

  // Safety: release the anchor if loadMore finishes without new messages.
  useEffect(() => {
    if (!loadingMore) anchorRef.current = null;
  }, [loadingMore]);

  // Parse @mention from input text.
  const handleDraftChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const val = e.target.value;
      setDraft(val);
      // Detect @mention pattern at the end of the input.
      const atMatch = val.match(/@(\S*)$/);
      if (atMatch && roomPlayers && roomPlayers.length > 0) {
        setMentionQuery(atMatch[1]);
        setShowMentions(true);
        setMentionIdx(0);
      } else {
        setShowMentions(false);
        setMentionQuery('');
      }
    },
    [roomPlayers],
  );

  const selectMention = useCallback(
    (player: { user_id: string; nickname: string }) => {
      // BUG-R74-5 (2026-07-09): 进入 whisper 模式时,清除 draft 中的 @nickname 前缀,
      // 避免 input_text(clear:true) 清空后残留 "@Bot 3号" 误发。whisper 通过
      // wsClient.send('chat.whisper', {text, to_user_id}) 走独立路径,不依赖
      // @ 前缀识别目标,所以这里安全删除 prefix。
      const stripped = draft.replace(/@\S+\s?$/, '');
      setDraft(stripped);
      setWhisperTarget({ id: player.user_id, account: player.nickname });
      setShowMentions(false);
      setMentionQuery('');
    },
    [draft],
  );

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const text = draft.trim();
    if (!text) return;
    if (whisperTarget) {
      whisper(whisperTarget.id, whisperTarget.account, text);
      setWhisperTarget(null);
    } else {
      send(text);
    }
    setDraft('');
    setShowMentions(false);
  };

  // Handle keyboard in mention dropdown.
  const onInputKeyDown = (e: React.KeyboardEvent) => {
    if (!showMentions || mentionCandidates.length === 0) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setMentionIdx((i) => (i + 1) % mentionCandidates.length);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setMentionIdx((i) => (i - 1 + mentionCandidates.length) % mentionCandidates.length);
    } else if (e.key === 'Enter' && showMentions) {
      e.preventDefault();
      selectMention(mentionCandidates[mentionIdx]);
    } else if (e.key === 'Escape') {
      setShowMentions(false);
    }
  };

  const cancelWhisper = () => {
    setWhisperTarget(null);
    setDraft((d) => d.replace(/@\S+\s?$/, ''));
  };

  // R235 §4.2: when the local player transitions to dead, automatically wipe
  // any pending chat draft + active whisper target. Dead players don't get
  // to talk (other than the legacy "last words" panel which is separate), so
  // leaving a half-typed message in the form invites confusion: the input is
  // visually still editable, but the meaning of the message changes ("this
  // is what I was about to say as a corpse"). Clearing the draft here makes
  // the rule explicit and matches the existing placeholder switch from
  // "说点什么..." to "私聊给 Bot X号..." observation.
  //
  // We clear on the rising edge of `isLocalPlayerDead` (false → true) so we
  // don't wipe player input on every re-render. The previous `false` is
  // tracked in a ref rather than via `useEffect` dependency because we want
  // the side-effect to run once per transition only.
  const wasDeadRef = useRef(false);
  useEffect(() => {
    if (isLocalPlayerDead && !wasDeadRef.current) {
      setDraft('');
      setWhisperTarget(null);
      setShowMentions(false);
      setMentionQuery('');
    }
    wasDeadRef.current = !!isLocalPlayerDead;
  }, [isLocalPlayerDead]);

  return (
    <div className={'game-chat' + (collapsed ? ' game-chat--collapsed' : '')}>
      <header className="game-chat__header">
        {/* 2026-08-07 §房间聊天优化 — 前置 emoji(图标感)与左栏「🏠 房间信息」对称:
            观战者用 👁、玩家用 💬。emoji 是渲染层硬编码(非 i18n 范围),
            与左栏 `▶ {spectator ? '👁 房间信息' : '🏠 房间信息'}` 写法一致。 */}
        <div className="game-chat__title">
          <span className="game-chat__title-icon" aria-hidden>
            {isSpectator ? '👁' : '💬'}
          </span>
          <span className={'game-chat__dot' + (connected ? ' game-chat__dot--on' : '')} />
          <span>{t('chat.gameTitle')}</span>
          {currentDay != null && (
            <span className="game-chat__day">· Day {currentDay}</span>
          )}
        </div>
        <button
          type="button"
          className="ghost game-chat__toggle"
          onClick={() => setCollapsed(!collapsed)}
          aria-label={collapsed ? t('chat.expand') : t('chat.collapse')}
          title={collapsed ? t('chat.expand') : t('chat.collapse')}
        >
          {collapsed ? '▲' : '▼'}
        </button>
      </header>

      {!collapsed && (
        <>
          <div
            className="game-chat__list"
            ref={listRef}
            style={{ maxHeight: '100%', overflowY: 'auto' }}
          >
            <div ref={sentinelRef} style={{ height: 1, width: '100%' }} />
            {loadingMore && (
              <div className="game-chat__loading-more">
                <span className="game-chat__spinner" /> {t('chat.loadingMore')}
              </div>
            )}
            {!hasMore && messages.length > 0 && (
              <div className="game-chat__no-more">{t('chat.noOlderMessages')}</div>
            )}
            {visibleMessages.length === 0 && !loadingMore && (
              <div className="game-chat__empty">{t('chat.empty')}</div>
            )}
            {/* 正式消息 + §13 Bot 流式预览气泡合并渲染(ts 升序)。
                 流式气泡是预览,权威 chat.message 到达后自动消隐,避免重复。 */}
            {renderItems.map((item) =>
              item.kind === 'stream' ? (
                <StreamBubble
                  key={`stream-${item.id}`}
                  stream={{ seat: item.seat, text: item.text, finalized: item.finalized }}
                  nickname={seatNickname.get(item.seat) || ''}
                />
              ) : item.msg.from_role === 'activity' ? (
                // §115 房间聊天 — 活动事件条。颜色按 severity,左边细线按
                // phase 区分(夜晚=紫/白天=黄/投票=灰)。仅展示,无 whisper
                // 按钮(玩家不能对系统活动发私聊)。
                <ActivityChip key={item.msg.id} m={item.msg} />
              ) : (
              <div
                key={item.msg.id}
                className={
                  'game-chat-msg' +
                  (item.msg.whisper ? ' game-chat-msg--whisper' : '') +
                  (item.msg.is_interject ? ' game-chat-msg--interject' : '') +
                  // 2026-08-05 §Agent聊天显示优化 F5 — Bot 消息左侧青色边条,
                  // 13 个 Agent 同屏刷屏时人类可一眼分层出 AI 发言。
                  (item.msg.from_role === 'bot' ? ' game-chat-msg--bot' : '')
                }
              >
                <span className="game-chat-msg__author">{item.msg.from_account}</span>
                {item.msg.whisper && (
                  <>
                    <span className="game-chat-msg__whisper-tag">
                      🔒 {t('chat.whisperTag')}
                    </span>
                    <span className="game-chat-msg__whisper-to">
                      → {item.msg.to_account}
                    </span>
                  </>
                )}
                {item.msg.is_interject && (
                  <span
                    className="game-chat-msg__interject-tag"
                    title={t('chat.interjectTag' as any, { defaultValue: '插话 / 主动发言' })}
                  >
                    💬 {t('chat.interjectTag' as any, { defaultValue: '插话' })}
                  </span>
                )}
                {item.msg.from_role === 'spectator' && (
                  <span className="game-chat-msg__role-badge" title={t('chat.spectatorTag')}>
                    👁 {t('chat.spectatorTag')}
                  </span>
                )}
                {item.msg.from_role === 'bot' && item.msg.from_agent_name && (
                  <span
                    className="game-chat-msg__role-badge game-chat-msg__role-badge--bot"
                    title={`AI 模型: ${item.msg.from_agent_name}`}
                  >
                    🤖 {item.msg.from_agent_name}
                  </span>
                )}
                {/* 2026-07-16 主持人 Agent 重构 — 法官公屏播报(⚖️ 前缀 + 金底)。
                    后端 SendFromJudge 走 chat.message,from_role="judge",
                    from_account="[法官·{model}]"(对齐设计 §5.4/§6.5)。 */}
                {item.msg.from_role === 'judge' && (
                  <span
                    className="game-chat-msg__role-badge game-chat-msg__role-badge--judge"
                    title={item.msg.from_account}
                  >
                    ⚖️ {item.msg.from_account}
                  </span>
                )}
                <span className="game-chat-msg__time">{formatChatTime(item.msg.ts)}</span>
                <div className="game-chat-msg__text">{item.msg.text}</div>
                {/* Whisper button — click to start a DM to this sender.
                    BUG-R234-6.1: 此前 onClick 同时 setDraft(`@${name} `) +
                    setWhisperTarget(),导致 whisper 状态双重存储(draft 前缀 + state)。
                    当用户/CDP input_text(clear:true) 清掉前缀时只剩 state 状态,
                    用户失去视觉反馈但 onSubmit 仍按 whisperTarget 发送,与 R74-5
                    selectMention 路径已 strip 前缀的语义不对称。

                    修复:与 selectMention 对齐 — 仅 setWhisperTarget,不写 draft 前缀。
                    whisperTarget 由 game-chat__whisper-indicator 单独渲染带 🔒 视觉提示,
                    onSubmit 走 wsClient.send('chat.whisper', {text, to_user_id}) 独立路径,
                    不依赖 draft 文本前缀。 */}
                {!item.msg.whisper && item.msg.from_user_id !== myUserId && roomPlayers && (
                  <button
                    type="button"
                    className="game-chat-msg__whisper-btn"
                    title={t('chat.whisperTo', { name: item.msg.from_account })}
                    onClick={() => {
                      setWhisperTarget({ id: item.msg.from_user_id, account: item.msg.from_account });
                    }}
                  >
                    💬
                  </button>
                )}
              </div>
              )
            )}
          </div>

          {error && <div className="game-chat__error">{error}</div>}

          {/* Whisper target indicator */}
          {whisperTarget && (
            <div className="game-chat__whisper-indicator">
              <span>
                🔒 {t('chat.whisperTo', { name: whisperTarget.account })}
              </span>
              <button type="button" className="ghost" onClick={cancelWhisper}>
                ✕
              </button>
            </div>
          )}

          {/* @mention autocomplete dropdown */}
          {showMentions && mentionCandidates.length > 0 && (
            <div className="game-chat__mention-dropdown">
              {mentionCandidates.map((p, i) => (
                <div
                  key={p.user_id}
                  className={
                    'game-chat__mention-item' + (i === mentionIdx ? ' game-chat__mention-item--active' : '')
                  }
                  onMouseDown={(e) => {
                    e.preventDefault();
                    selectMention(p);
                  }}
                  onMouseEnter={() => setMentionIdx(i)}
                >
                  {p.nickname}
                </div>
              ))}
            </div>
          )}

          <form className="game-chat__form" onSubmit={onSubmit}>
            <input
              type="text"
              value={draft}
              maxLength={1024}
              placeholder={
                whisperTarget
                  ? t('chat.whisperPlaceholder', { name: whisperTarget.account })
                  : isSpectator
                    // R100 P1 FIX: spectator-specific hint. Without this the
                    // panel read as "正在连接..." for any spectator whose WS
                    // handshake hadn't completed by the time the test asserted
                    // — the test author mistook this for "观战者聊天 disabled".
                    ? '👁 观战者可发言,Agents 将看到你的消息'
                    : connected
                      ? t('chat.placeholder')
                      : t('chat.connecting')
              }
              onChange={handleDraftChange}
              onKeyDown={onInputKeyDown}
              disabled={!!isLocalPlayerDead}
            />
            <button type="submit" disabled={!connected || !draft.trim() || !!isLocalPlayerDead}>
              {whisperTarget ? t('chat.whisperSend') : t('chat.send')}
            </button>
          </form>
        </>
      )}
    </div>
  );
}

// 2026-07-12 §13 增强 — Bot SSE 流式预览气泡(token 瀑布流)。
// 后端广播 chat.stream_start/delta/end 时在权威 chat.message 到达前显示;
// 权威消息到达后由 useStreamingMessages 自动按 ts 窗口消隐,避免重复。
// 视觉:头像 + Bot N号 / agent_name 标、增量文本、0.6s 闪烁光标(▍);
// 终态(finalized)时去掉光标;a11y:prefers-reduced-motion 时静止。
function StreamBubble({ stream, nickname }: { stream: { seat: number; text: string; finalized: boolean }; nickname: string }) {
  const author = nickname || `Bot #${stream.seat + 1}`;
  return (
    <div
      className="game-chat-msg game-chat-msg--stream"
      data-testid={`game-chat-stream-${stream.seat}`}
      role="status"
      aria-live="polite"
    >
      <span className="game-chat-msg__author">
        🤖 {author}
      </span>
      <span className="game-chat-msg__time" aria-hidden>直播</span>
      <div className="game-chat-msg__text">
        {stream.text}
        {!stream.finalized && <span className="game-chat-msg__cursor" aria-hidden>▍</span>}
      </div>
    </div>
  );
}

// formatChatTime — today's messages show only HH:mm; older ones show MM-DD HH:mm.
function formatChatTime(ts: number): string {
  const d = new Date(ts);
  const now = new Date();
  const pad = (n: number) => n.toString().padStart(2, '0');
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  if (sameDay) return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// ActivityChip — §115 房间聊天增强。Phase / vote / kill / seer / witch /
// hunter / sheriff / game-over / quarantine / auto-skip 等结构化活动事件
// 的渲染组件,带 severity 着色 + phase 修饰。Props.m 来自 useChat 把
// chat.activity 帧转成的 ChatMessage(带动态附加字段)。
function ActivityChip({ m }: { m: ChatMessage }): JSX.Element {
  const t = useT();
  const severity = (m as any).severity || 'info';
  const phase = (m as any).phase as string | undefined;
  const eventKind = (m as any).event_kind as string | undefined;
  const phaseClass = (() => {
    if (!phase) return '';
    const night = ['night', 'wolves', 'seer', 'witch', 'pre_wolves'];
    const day = ['day', 'speak', 'sheriff_election', 'pk_speak', 'last_words'];
    if (night.includes(phase)) return ' game-chat-activity--night';
    if (day.includes(phase)) return ' game-chat-activity--day';
    return '';
  })();
  // 2026-08-05 §Agent聊天显示优化 F4 — 遗言(last_words)按「发言体」渲染。
  // 后端 last_words 工具走 emitActivity(death_lyric_spoken) → chat.activity,
  // 不走 SendFromBot(否则会与 bot 的 500K 队列重复投递,见设计 §4)。
  // 但遗言在语义上是一次真正的公开发言,渲染成系统小灰条会被人类完全略过。
  // 这里只改前端呈现:💀 遗言徽章 + 正文字号,不动后端投递路径。
  //
  // 2026-08-08 §20260808-02 — 遗言三节点视觉成体系(缺陷 C)。
  // death_lyric_start / death_lyric_skipped 原本是普通系统小灰条,与正文事件
  // (death_lyric_spoken)视觉权重差距过大,13 人高频聊天流里一刷即没。
  // 三节点统一为「遗言家族」:共用 game-chat-activity--lastwords-chain 左侧
  // 3px 竖条(同一事件链的视觉分组),spoken 保持既有 --speech 发言体不动,
  // start/skipped 各加徽章(紫=仪式性节点 / 灰=死亡灰,§26.4 既有色相库)。
  const isSpeechLike = eventKind === 'death_lyric_spoken';
  const isLastWordsFamily =
    eventKind === 'death_lyric_start' ||
    eventKind === 'death_lyric_spoken' ||
    eventKind === 'death_lyric_skipped';
  // §20260811-07 U1 — 死后幽灵语音。紫底 + 👻 icon + 协议层隔离 tooltip
  // (说明此为「死亡瞬间内心独白」,与 §119 HeartThought 协议层隔离对称)。
  const isGhostVoice = eventKind === 'ghost_voice';
  const lastWordsBadge =
    eventKind === 'death_lyric_start'
      ? { cls: 'game-chat-activity__speech-tag game-chat-activity__speech-tag--lastwords-start', label: t('chat.lastWordsStartTag') }
      : eventKind === 'death_lyric_skipped'
        ? { cls: 'game-chat-activity__speech-tag game-chat-activity__speech-tag--lastwords-skip', label: t('chat.lastWordsSkipTag') }
        : null;
  return (
    <div
      className={
        `game-chat-activity game-chat-activity--${severity}${phaseClass}` +
        (isSpeechLike ? ' game-chat-activity--speech' : '') +
        (isLastWordsFamily ? ' game-chat-activity--lastwords-chain' : '') +
        (isGhostVoice ? ' game-chat-activity--ghost-voice' : '')
      }
      title={isGhostVoice ? t('werewolf.ghostVoice.label') : undefined}
    >
      <span className="game-chat-activity__icon">{(m as any).icon || (isSpeechLike ? '💀' : isGhostVoice ? '👻' : 'ℹ')}</span>
      {isSpeechLike && (
        <span className="game-chat-activity__speech-tag">{t('chat.lastWordsTag')}</span>
      )}
      {lastWordsBadge && (
        <span className={lastWordsBadge.cls}>{lastWordsBadge.label}</span>
      )}
      <span className="game-chat-activity__text">{m.text}</span>
      <span className="game-chat-activity__time">{formatChatTime(m.ts)}</span>
    </div>
  );
}
