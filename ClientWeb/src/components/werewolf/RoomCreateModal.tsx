/**
 * RoomCreateModal — Werewolf room-create dialog with an optional AI-player
 * picker. Lets the caller choose 0..MAX_AI_SEATS (default 13) AI seats, each
 * with a model from /api/llm/models. Falls back to a simple spinner if the
 * LLM registry is unavailable (no providers configured).
 */

import React, { useEffect, useMemo, useState } from 'react';
import { listModels, type ModelInfo } from '@/api/llm';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { CreateRoomOptions } from '@/types/api';
import { normalizeProviderProtocol } from '@/types/model';

export interface AgentSeatInput {
  seat: number;
  model_key: string;
  // 2026-08-06 §20260806-03 自选角色(可选):'random'/'' = 随机。
  role?: string;
}

// 2026-08-06 §20260806-03 — 可选角色白名单(与后端 ParseRoleName 同步)。
// 仅含全链路已实现的角色;已退役角色(magician 等)不可选。
const SELECTABLE_ROLES = [
  'werewolf', 'seer', 'witch', 'hunter', 'idiot',
  'guard', 'knight', 'demon_hunter', 'villager',
] as const;

interface JudgeModeOpt {
  mode: NonNullable<CreateRoomOptions['judge']>['mode'];
  labelKey: TKey;
}

// 2026-07-30 §重构 — 法官配置改为两选项(Agent 法官 / 真人法官)。
// Agent 法官 = 原"主持人 Agent (法官)/ AI 法官";真人法官当前等同 Agent 法官
// 运行(后端真人接入尚未实现,UI 占位对齐 docs/狼人杀-重构方案/主持人Agent重构设计.md §2)。
const JUDGE_MODES: JudgeModeOpt[] = [
  { mode: 'agent', labelKey: 'werewolf.judge.mode.agent' },
  { mode: 'human', labelKey: 'werewolf.judge.mode.human' },
];

interface Props {
  open: boolean;
  onClose: () => void;
  // BUG-R210-04 (2026-07-30): onSubmit 返回 Promise<boolean> —
  //   true  → 创建成功,父组件已关闭弹窗(我们这里不主动关)
  //   false → 创建失败,弹窗不关闭,formError 已就地显示
  // 之前是 () => void:父组件 try/catch 后无论成败都 setCreateModalOpen(false),
  // 导致失败时弹窗消失、错误只在 toast 里一闪,违反 CLAUDE.md §7.1。
  onSubmit: (req: {
    name?: string;
    agent_seats: AgentSeatInput[];
    judge?: NonNullable<CreateRoomOptions['judge']>;
    // 2026-08-11 §20260811-09 U2 — Agent 难度档位。undefined = 后端默认 normal。
    agent_difficulty?: NonNullable<CreateRoomOptions['agent_difficulty']>;
    // 2026-08-11 §20260811-09 U1 — AI 实时解说(仅观战者可见)。可选。
    commentary?: { enabled?: boolean; style?: 'pro' | 'fun'; model_key?: string };
    // 2026-08-06 §20260806-03 — 创建者(人类座位)角色偏好;'random'/'' = 随机。
    creator_role?: string;
  }) => Promise<boolean>;
  // BUG-R200-P1-03 (2026-07-30): 父组件持有「创建中」状态 → 透传给弹窗,
  // 弹窗提交按钮在 in-flight 时 disabled 防重复提交。
  submitting?: boolean;
}

// 2026-07-10 13 人局:默认容量 13;同时保留 12/7 人局历史兼容(werewolf_12/werewolf_7)。
// 这里采用 max_seat=13 发给后端,便于未来动态容量;后端按 game_kind 决定实际容量。
const MAX_AI_SEATS = 13;
const ALL_SEATS = Array.from({ length: MAX_AI_SEATS }, (_, i) => i);

const RoomCreateModal: React.FC<Props> = ({ open, onClose, onSubmit, submitting = false }) => {
  const t = useT();
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [agentCount, setAgentCount] = useState(0);
  const [seats, setSeats] = useState<AgentSeatInput[]>([]);
  // 2026-07-30 §重构 — 房间级法官设置(默认 Agent 法官)。
  const [judgeMode, setJudgeMode] = useState<NonNullable<CreateRoomOptions['judge']>['mode']>('agent');
  const [judgeModelKey, setJudgeModelKey] = useState<string>('');
  // 2026-08-11 §20260811-09 U2 — Agent 难度档位(默认 normal)。
  const [agentDifficulty, setAgentDifficulty] =
    useState<NonNullable<CreateRoomOptions['agent_difficulty']>>('normal');
  // 2026-08-11 §20260811-09 U1 — AI 实时解说开关(默认关闭)。
  const [commentaryEnabled, setCommentaryEnabled] = useState(false);
  const [commentaryStyle, setCommentaryStyle] = useState<'pro' | 'fun'>('pro');
  // 2026-08-06 §20260806-03 — 创建者(人类座位)角色偏好,默认随机。
  const [creatorRole, setCreatorRole] = useState<string>('random');
  // BUG-R210-04 (2026-07-30): 内联表单错误条。父组件 onSubmit 返回 false
  // 时显示,弹窗不关闭,允许用户调整后重试(对齐 CLAUDE.md §7.1)。
  const [formError, setFormError] = useState<string | null>(null);
  // 2026-07-30 §R210-04: 弹窗本地 submitting 状态 — 父组件 prop submitting
  // 由 handleCreateWithAgents 用 finally 设置,但若父组件 throw,父组件
  // finally 不会执行。本地状态双保险。
  const [localSubmitting, setLocalSubmitting] = useState(false);
  // 2026-08-08 §20260808-01 — 「🎲 重新分配」计数器。递增即触发下方
  // 座位分配 useEffect 重跑;因该次重跑前 seats 已被清空,首遍"保留既有
  // 合法座位"的循环无内容可保留,13 个座位号与模型会整体重摇。
  // 不新增任何分配逻辑分支 —— 完全复用既有 effect 路径。
  const [shuffleNonce, setShuffleNonce] = useState(0);

  // 弹窗关闭 / 打开时清掉残留错误,避免下一次开窗还显示上次的报错。
  useEffect(() => {
    if (!open) {
      setFormError(null);
    }
  }, [open]);

  // Load models once when the modal opens.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    listModels()
      .then((ms) => {
        if (cancelled) return;
        // §20260814-01 — 双协议均可开 AI 房间;兼容存量旧值 anthropic/openai。
        setModels(ms.filter((m) => normalizeProviderProtocol(m.provider_type) === 'anthropic-messages' ||
          normalizeProviderProtocol(m.provider_type) === 'openai-completions'));
      })
      .catch((e) => {
        if (cancelled) return;
        setError(String(e?.message ?? e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  // Reset seats when agent count changes. New seats are auto-assigned models
  // via Fisher-Yates shuffle so adjacent AIs avoid the same model_key. User
  // manual picks on existing seats are preserved. Backend
  // `alternateModelsLocked` is the final dedup safeguard — never trust the
  // client (CDP / eval_js can bypass React state).
  //
  // BUG-R123-SLIDER-01 fix: expose a programmatic bridge on `window` so
  // automation (eval_js / CDP) can update `agentCount` through React state
  // instead of mutating the underlying `<input>` value. Direct DOM value
  // assignment does NOT fire React's synthetic `onChange`, leaving the label
  // stale even though `input.value === '12'`. The bridge is a dev-mode
  // convenience — production users still drive the slider with mouse events.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    (window as unknown as { __wwTestBridge?: { setAiCount: (n: number) => void } }).__wwTestBridge = {
      setAiCount: (n: number) => {
        const clamped = Math.max(0, Math.min(MAX_AI_SEATS, Math.floor(n)));
        setAgentCount(clamped);
      },
    };
    return () => {
      const w = window as unknown as { __wwTestBridge?: unknown };
      if (w.__wwTestBridge) delete w.__wwTestBridge;
    };
  }, []);
  useEffect(() => {
    if (agentCount === 0) {
      setSeats([]);
      return;
    }
    if (models.length === 0) {
      // Models not loaded yet — keep existing seats but trim to agentCount.
      setSeats((prev) => prev.slice(0, agentCount));
      return;
    }

    setSeats((prev) => {
      // Fisher-Yates shuffle of model keys.
      const shuffled = models.map((m) => m.model);
      for (let i = shuffled.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
      }

      const validModelKeys = new Set(models.map((m) => m.model));
      const usedModelKeys = new Set<string>();
      const usedSeatNumbers = new Set<number>();
      const next: AgentSeatInput[] = [];

      // First pass: keep existing valid seats.
      for (let i = 0; i < prev.length && i < agentCount; i++) {
        const existing = prev[i];
        if (existing && existing.model_key && validModelKeys.has(existing.model_key) &&
            existing.seat >= 0 && existing.seat < MAX_AI_SEATS && !usedSeatNumbers.has(existing.seat)) {
          next.push(existing);
          usedModelKeys.add(existing.model_key);
          usedSeatNumbers.add(existing.seat);
        }
      }

      // Second pass: fill remaining seats by randomly picking from all free
      // seats. Fisher-Yates shuffle of the free-seat pool so AI seats are
      // uniformly distributed — the human seat (complement) is then random
      // too, instead of being pinned to the lowest free index.
      //
      // 2026-07-22 修复: 旧逻辑把 AI 分配到"后 N 个固定座位"
      // (targetSeat = MAX_AI_SEATS - agentCount + i),导致人类座位固定
      // (1人类+12bot 时人类永远坐 0 号位)。改为从所有空位中随机选 N 个给 AI,
      // 人类座位(补集)自然随机化 —— 满足"人类玩家可能是任意号码"的需求。
      //
      // BUG-RoomCreateModal-freeSeat-cursor 修复: 旧写法
      //   `targetSeat = freeSeats[i - next.length]`
      // 在循环里读取了 push 进来的 next.length(每轮 +1),导致 `i - next.length`
      // 恒等于 0,12 个 bot 全部挤进 freeSeats[0](同一座位重复 12 次)且因
      // usedSeatNumbers 阻断下游 push,最终 seats.length < agentCount,
      // 「创建」按钮永久 disabled。改用独立游标 fillCursor 自增,确保每轮
      // 取走一个不同的 freeSeat。
      const freeSeats: number[] = [];
      for (let s = 0; s < MAX_AI_SEATS; s++) {
        if (!usedSeatNumbers.has(s)) freeSeats.push(s);
      }
      for (let i = freeSeats.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [freeSeats[i], freeSeats[j]] = [freeSeats[j], freeSeats[i]];
      }
      // fillCursor 必须从 0 开始,与 freeSeats 的索引对齐。freeSeats 的长度
      // = MAX_AI_SEATS - next.length(已保留的合法座位数),所以可安全遍历的
      // 索引范围是 0 .. (freeSeats.length-1)。若从 next.length 开始,当
      // next.length > 0 时不仅跳过前 next.length 个空位,还会在
      // agentCount + next.length > MAX_AI_SEATS 时越界读到 undefined →
      // seat 字段缺失 → 后端 JSON 反序列化默认 seat=0 → 多个 seat=0 触发
      // "duplicate agent seat" 错误(典型触发路径:滑块 0→12 连续 onChange,
      // 最后一步 prev 已保留 11 座,freeSeats 仅剩 2 座,freeSeats[11]=undefined)。
      let fillCursor = 0;
      for (let i = next.length; i < agentCount; i++) {
        const targetSeat = freeSeats[fillCursor];
        let pick = shuffled.find((k) => !usedModelKeys.has(k));
        if (pick === undefined) pick = shuffled[fillCursor % shuffled.length];
        next.push({ seat: targetSeat, model_key: pick });
        usedModelKeys.add(pick);
        usedSeatNumbers.add(targetSeat);
        fillCursor += 1;
      }

      return next;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentCount, models, shuffleNonce]);

  const usedSeats = useMemo(() => new Set(seats.map((s) => s.seat)), [seats]);

  const updateSeat = (idx: number, patch: Partial<AgentSeatInput>) => {
    setSeats((prev) =>
      prev.map((s, i) => (i === idx ? { ...s, ...patch } : s)),
    );
  };

  const valid = useMemo(() => {
    if (agentCount === 0) return true;
    if (models.length === 0) return false;
    // 2026-07-22 修复: 当 useEffect 的 push 循环因 free-seat bug 没填满时,
    // 这里再校验 seats.length === agentCount,确保「创建」按钮不被错误启用。
    if (seats.length !== agentCount) return false;
    const seen = new Set<number>();
    for (const s of seats) {
      if (seen.has(s.seat)) return false;
      if (s.seat < 0 || s.seat >= MAX_AI_SEATS) return false;
      if (!s.model_key) return false;
      seen.add(s.seat);
    }
    return true;
  }, [agentCount, seats, models.length]);

  if (!open) return null;

  return (
    <div className="ww-create-modal" role="dialog" aria-modal="true">
      <div className="ww-create-modal__card">
        <header className="ww-create-modal__header">
          <h2>{t('werewolf.createModal.title')}</h2>
          <button className="ww-create-modal__close" onClick={onClose}>
            ×
          </button>
        </header>

        <div className="ww-create-modal__body">
          {/* ROW 1 — 房间名 + AI 玩家数量。2026-08-08 §20260808-01:
              原为两个独立纵向区块,合并为双列以省出约 55px。 */}
          <div className="ww-create-modal__row">
            <label className="ww-create-modal__field">
              <span>{t('werewolf.createModal.roomName')}</span>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('werewolf.createModal.roomNamePlaceholder')}
                maxLength={32}
              />
            </label>

            <div className="ww-create-modal__field">
              {/* 标签与 hint 同排,不再各占一行。三个 data-testid 保持不变。 */}
              <div className="ww-create-modal__field-head">
                <span data-testid="ww-create-modal__ai-count-label" aria-live="polite">
                  {t('werewolf.createModal.aiCount', { count: agentCount })}
                </span>
                <span className="ww-create-modal__hint" data-testid="ww-create-modal__ai-count-hint">
                  {agentCount === 0
                    ? t('werewolf.createModal.allHuman')
                    : t('werewolf.createModal.humanAiMix', { human: MAX_AI_SEATS - agentCount, ai: agentCount })}
                </span>
              </div>
              <input
                type="range"
                min={0}
                max={MAX_AI_SEATS}
                value={agentCount}
                data-testid="ww-create-modal__ai-count-slider"
                aria-label={t('werewolf.createModal.aiCount', { count: agentCount })}
                onChange={(e) => setAgentCount(Number(e.target.value))}
              />
            </div>
          </div>

          {/* ROW 2 — 我的角色 + 法官模式 + Agent 难度 + 法官模型。
              2026-08-12 §20260812-01: 由 3 列 4 item 改为 4 列紧凑布局,
              Agent 难度 4 档 radio 同行。
              2026-08-17 §20260817-02: 4 列改为「最小宽度契约 + AI 解说下放 ROW3」,
              法官模型 select 永远 ≥ 220px,长模型名完整显示。AI 解说不再与
              法官模型挤在同一列(原 §20260812-01 旧版会撑爆法官模型宽度)。
              列数随场景变化:
                agentCount === 0            → 仅我的角色 + 全人类空态(--row--1)
                agentCount === MAX_AI_SEATS → 三列无"我的角色"(--row--2)
                其余                        → 四列           (--row--4) */}
          <div
            className={
              agentCount === 0
                ? 'ww-create-modal__row ww-create-modal__row--1'
                : agentCount === MAX_AI_SEATS
                  ? 'ww-create-modal__row ww-create-modal__row--2'
                  : 'ww-create-modal__row ww-create-modal__row--4'
            }
          >
            {/* 2026-08-06 §20260806-03 — 创建者(人类)角色选择。
                仅当存在人类座位(agentCount < 13)时显示;全 AI 房创建者是观众,无座位。 */}
            {agentCount < MAX_AI_SEATS && (
              <label className="ww-create-modal__field">
                <span>{t('werewolf.rolePick.label')}</span>
                <select
                  value={creatorRole}
                  onChange={(e) => setCreatorRole(e.target.value)}
                  aria-label={t('werewolf.rolePick.label')}
                  data-testid="ww-create-modal__creator-role"
                >
                  <option value="random">{t('werewolf.role.random')}</option>
                  {SELECTABLE_ROLES.map((r) => (
                    <option key={r} value={r}>
                      {t(`werewolf.role.${r}` as TKey)}
                    </option>
                  ))}
                </select>
              </label>
            )}

            {/* 2026-08-17 §20260817-02 — 全人类房 ROW2 右列填满说明文字。
                原 §20260808-01 旧布局此处空白,视觉上"中间空了一大块"。
                文字 12.5px 灰度 0.72 + 左竖线分隔,与"我的角色"select 同 27px 高度。 */}
            {agentCount === 0 && (
              <div className="ww-create-modal__empty-state">
                {t('werewolf.createModal.allHumanEmptyState')}
              </div>
            )}

            {/* 2026-07-16 主持人 Agent 重构 — 主持人(法官)模式 radio。
                仅当 agentCount>0 时渲染(全人类房无法官,对齐设计 §1.2)。
                2026-08-17 §20260817-02: 从右侧列提为 ROW2 第二列,获得独立列宽契约。 */}
            {agentCount > 0 && (
              <div className="ww-create-modal__judge-inline">
                <div className="ww-create-modal__judge-title">{t('werewolf.judge.modeLabel')}</div>
                <div className="ww-create-modal__judge-modes">
                  {JUDGE_MODES.map((m) => (
                    <label key={m.mode} className="ww-create-modal__judge-mode">
                      <input
                        type="radio"
                        name="ww-judge-mode"
                        value={m.mode}
                        checked={judgeMode === m.mode}
                        onChange={() => setJudgeMode(m.mode)}
                      />
                      {t(m.labelKey)}
                    </label>
                  ))}
                </div>
              </div>
            )}

            {/* §20260811-09 U2 — Agent 难度分级(easy/normal/hard/hell)。
                仅当有 Agent 时显示;normal 是默认值不向请求体提交(后端兜底)。
                2026-08-12 §20260812-01: 4 档 radio 同行显示,flex-wrap 窄屏折回。
                2026-08-17 §20260817-02: 4 档按 easy/normal/hard/hell 加
                视觉分组 className(冷暖色阶 + 选中紫色光晕)。 */}
            {agentCount > 0 && (
              <div className="ww-create-modal__difficulty-inline">
                <div className="ww-create-modal__difficulty-modes">
                  {(['easy', 'normal', 'hard', 'hell'] as const).map((d) => (
                    <label
                      key={d}
                      className={`ww-create-modal__difficulty-mode ww-create-modal__difficulty-mode--${d}`}
                    >
                      <input
                        type="radio"
                        name="ww-agent-difficulty"
                        value={d}
                        checked={agentDifficulty === d}
                        onChange={() => setAgentDifficulty(d)}
                      />
                      {t(`werewolf.difficulty.${d}` as TKey)}
                    </label>
                  ))}
                </div>
                <p className="ww-create-modal__difficulty-hint">
                  {t(`werewolf.difficulty.hint.${agentDifficulty}` as TKey)}
                </p>
              </div>
            )}

            {/* 法官模型 — 2026-08-17 §20260817-02 提为 ROW2 末列,
                独立列宽契约 minmax(220px, 1.1fr),长模型名完整显示。 */}
            {agentCount > 0 && (
              <label className="ww-create-modal__field">
                <span>{t('werewolf.judge.model')}</span>
                <select
                  value={judgeModelKey}
                  onChange={(e) => setJudgeModelKey(e.target.value)}
                  aria-label={t('werewolf.judge.model')}
                  disabled={judgeMode !== 'agent'}
                  title={judgeModelKey ? judgeModelKey : t('werewolf.judge.modelPlaceholder')}
                >
                  <option value="">{t('werewolf.judge.modelPlaceholder')}</option>
                  {models.map((m) => (
                    <option key={m.model} value={m.model}>
                      {m.agent_name}
                    </option>
                  ))}
                </select>
              </label>
            )}
          </div>

          {/* ROW 3 — AI 实时解说(可折叠)。
              2026-08-17 §20260817-02: 从原 ROW2 右侧列(§20260812-01)下放独立行。
              默认关闭时仅 24px 高(一行),不与其他控件挤。
              启用时展开 56px,显示 pro/fun 风格 radio。真人法官模式的 select
              已禁用占位,AI 解说在两种模式下都可启用(AI 解说与法官互相独立)。 */}
          {agentCount > 0 && (
            <div
              className="ww-create-modal__row--commentary"
              data-open={commentaryEnabled ? 'true' : 'false'}
            >
              <label className="ww-create-modal__commentary-toggle">
                <input
                  type="checkbox"
                  checked={commentaryEnabled}
                  onChange={(e) => setCommentaryEnabled(e.target.checked)}
                />
                {t('werewolf.commentary.enable')}
              </label>
              {commentaryEnabled ? (
                <div className="ww-create-modal__commentary-styles-inline">
                  {(['pro', 'fun'] as const).map((s) => (
                    <label key={s} className="ww-create-modal__commentary-style">
                      <input
                        type="radio"
                        name="ww-commentary-style"
                        value={s}
                        checked={commentaryStyle === s}
                        onChange={() => setCommentaryStyle(s)}
                      />
                      {t(`werewolf.commentary.style${s === 'pro' ? 'Pro' : 'Fun'}` as TKey)}
                    </label>
                  ))}
                </div>
              ) : (
                <span className="ww-create-modal__hint">
                  {t('werewolf.createModal.commentaryRowHint')}
                </span>
              )}
            </div>
          )}

          {loading && <p className="ww-create-modal__hint">{t('werewolf.createModal.loadingModels')}</p>}{error && (
            <p className="ww-create-modal__error">
              {t('werewolf.createModal.modelsUnavailable', { error })}
            </p>
          )}

          {/* AI 座位区 — 2026-08-08 §20260808-01 改为弹性主体(flex:1),
              吃掉固定块之外的全部剩余高度。标题条利用横向空白承载
              计数与「重新分配」,不额外增加纵向开销。 */}
          {agentCount > 0 && models.length > 0 && (
            <div className="ww-create-modal__seatblock">
              <div className="ww-create-modal__seatblock-head">
                <span>{t('werewolf.createModal.aiSeats', { count: seats.length })}</span>
                <button
                  type="button"
                  className="ww-create-modal__reshuffle"
                  data-testid="ww-create-modal__reshuffle"
                  onClick={() => {
                    setSeats([]);
                    setShuffleNonce((n) => n + 1);
                  }}
                >
                  {t('werewolf.createModal.reshuffle')}
                </button>
              </div>
              <div className="ww-create-modal__seats">
                {seats.map((s, i) => (
                  <div key={i} className="ww-create-modal__seatrow">
                    <span className="ww-create-modal__seatidx">AI {i + 1}</span>
                    <select
                      value={s.seat}
                      onChange={(e) => updateSeat(i, { seat: Number(e.target.value) })}
                      aria-label={t('werewolf.createModal.aiSeatLabel', { index: i + 1 })}
                    >
                      {ALL_SEATS.map((n) => (
                        <option
                          key={n}
                          value={n}
                          disabled={usedSeats.has(n) && n !== s.seat}
                        >
                          {/* 2026-08-08 §20260808-01: 去掉旧的「(当前)」后缀 ——
                            select 本就显示当前选中项、展开时浏览器也会高亮它,
                            该后缀纯冗余,却让「12号位(当前)」在紧凑列宽下被截断。 */}
                        {t('werewolf.createModal.seatNumber', { n })}
                        </option>
                      ))}
                    </select>
                    <select
                      value={s.model_key}
                      onChange={(e) => updateSeat(i, { model_key: e.target.value })}
                      aria-label={t('werewolf.createModal.aiModelLabel', { index: i + 1 })}
                    >
                      {models.map((m) => (
                        <option key={m.model} value={m.model}>
                          {m.agent_name}
                        </option>
                      ))}
                    </select>
                    {/* 2026-08-06 §20260806-03 — AI 座位角色偏好(默认随机)。 */}
                    <select
                      value={s.role ?? 'random'}
                      onChange={(e) => updateSeat(i, { role: e.target.value })}
                      aria-label={t('werewolf.createModal.aiRoleLabel', { index: i + 1 })}
                      data-testid={`ww-create-modal__seat-role-${i}`}
                    >
                      <option value="random">{t('werewolf.role.random')}</option>
                      {SELECTABLE_ROLES.map((r) => (
                        <option key={r} value={r}>
                          {t(`werewolf.role.${r}` as TKey)}
                        </option>
                      ))}
                    </select>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* 2026-08-08 §20260808-01 — 法官说明与角色规则说明合并为单行,
              超出省略;全文见 title。原为两条独立段落,合计约 72px。 */}
          <p
            className="ww-create-modal__foot-hint"
            title={
              agentCount > 0
                ? `${t('werewolf.rolePick.hint')} ${t('werewolf.judge.hint')}`
                : t('werewolf.rolePick.hint')
            }
          >
            ⓘ {t('werewolf.rolePick.hint')}
            {agentCount > 0 ? ` · ${t('werewolf.judge.hint')}` : ''}
          </p>
        </div>

        {formError && (
          <p className="ww-create-modal__error" role="alert" data-testid="ww-create-modal__form-error">
            {formError}
          </p>
        )}

        <footer className="ww-create-modal__footer">
          <button className="ww-create-modal__cancel" onClick={onClose}>
            {t('werewolf.createModal.cancel')}
          </button>
          <button
            className="ww-create-modal__submit"
            // BUG-R200-P1-03 (2026-07-30): 在父组件 in-flight 时禁用提交按钮,
            // 避免重复点击造成多个僵尸房间(报告里观察到误建 3 个 13AI 房间)。
            disabled={!valid || loading || submitting || localSubmitting}
            onClick={async () => {
              setFormError(null);
              // 2026-07-30 §R210-04 双保险: 立刻设个本地 submitting 标志,
              // 不依赖父组件 setLoading 的 React batch,避免极端场景下
              // (父组件 try/catch 抛错、loading 永远 false) 按钮可重复点击。
              setLocalSubmitting(true);
              try {
                const ok = await onSubmit({
                  name: name.trim() || undefined,
                  agent_seats: seats.map((s) => ({
                    seat: s.seat,
                    model_key: s.model_key,
                    // 2026-08-06 §20260806-03 — 角色偏好透传;'random' 归一化为缺省。
                    ...(s.role && s.role !== 'random' ? { role: s.role } : {}),
                  })),
                  // 2026-08-06 §20260806-03 — 创建者角色偏好(仅有人类座位时提交)。
                  ...(agentCount < MAX_AI_SEATS && creatorRole !== 'random'
                    ? { creator_role: creatorRole }
                    : {}),
                  // 2026-07-16 主持人 Agent 重构 — 有 Agent 时一并提交法官设置。
                  ...(agentCount > 0
                    ? {
                        judge: {
                          mode: judgeMode,
                          // 2026-08-08 §20260808-01: 真人法官不提交 model_key。
                          // 模型下拉在 human 模式下是 disabled 占位(防列消失抖动),
                          // 其残留值不应混入提交体。
                          model_key:
                            judgeMode === 'agent' ? judgeModelKey || undefined : undefined,
                        },
                      }
                    : {}),
                  // 2026-08-11 §20260811-09 U2 — 难度档位透传。normal 是默认值,
                  // 不显式提交以减少请求体(后端缺省字段即 normal,§121 wrapper 兼容)。
                  ...(agentCount > 0 && agentDifficulty !== 'normal'
                    ? { agent_difficulty: agentDifficulty }
                    : {}),
                  // §20260811-09 U1 — AI 解说(默认关闭)。关闭时不提交请求体。
                  ...(commentaryEnabled
                    ? { commentary: { enabled: true, style: commentaryStyle } }
                    : {}),
                });
                if (ok === false) {
                  // 父组件失败路径已 reportGlobalError;这里再就地红条展示一次。
                  // 父组件失败时不应关闭弹窗;但用户可能没意识到,可以再弹一次
                  // 提示「上次提交失败,请检查后再试」,确保不漏。
                  setFormError(t('werewolf.createModal.submitFailed'));
                }
                // 成功路径由父组件负责 setCreateModalOpen(false)。
              } catch (e: any) {
                // 2026-07-30 §R210-04: 父组件 throw(unexpected) 时兜底红条,
                // 避免弹窗「卡死」+ 按钮永远 disabled 的极端场景。
                setFormError(t('werewolf.createModal.submitError', { message: e?.message ?? String(e) }));
              } finally {
                setLocalSubmitting(false);
              }
            }}
          >
            {t('werewolf.createModal.submit')}
          </button>
        </footer>
      </div>
    </div>
  );
};

export default RoomCreateModal;
