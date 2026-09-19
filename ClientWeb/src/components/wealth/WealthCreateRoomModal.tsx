/**
 * WealthCreateRoomModal — 财商流建房弹窗：
 *   - 房间名（可选）
 *   - Agent 座位（2026-09-16 §财商流10–12座位：档位 0–12，Agent 依次占座位号
 *     0..N-1，创建者由后端从剩余空位中随机入座；N = 12（= 房间容量）时后端
 *     freeSeats 为空 → 创建者自动降级为观战者，即「全 Agent 房」。
 *     默认 10 个 Agent（+ 创建者 = 11 座 ≥ MinSeats=10 → 后端自动开局）。
 *     每个 Agent 座位独立选模型，模型来自 /api/llm/models；模型按「随机均匀
 *     分配」（Fisher-Yates 多轮洗牌 + 相邻去重，各模型次数差 ≤ 1），支持一键
 *     重摇，详见 lag_docs/财商流游戏/已实现/08-UI优化/05-产品设计-创建房间Agent模型随机分配与均匀去重-v1.md。
 *   - 月节拍速度（3000 / 8000 / 15000ms 预设 + 3000–30000 滑杆）
 *   - 职业卡池（curated 精选 10 卡 / docs 文档池）+ 职业卡一览
 *     （GET /api/games/wealth/professions，失败回落静态镜像，不阻塞建房）
 *   - 随机种子（可选，确定性复现）
 *
 * §7.1：提交失败 / 校验不通过内联红条（formError）、弹窗不关闭
 * （onSubmit 返回 false 或本地校验拦截）。
 */

import React, { useEffect, useMemo, useState } from 'react';
import { AppModal } from '@/components/ui/AppModal';
import { listModels, type ModelInfo } from '@/api/llm';
import { fetchProfessions, type WealthProfessionCard } from '@/api/wealth';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { AgentSeatRequest } from '@/types/api';
import {
  CURATED_PROFESSIONS,
  WEALTH_DEFAULT_AGENT_COUNT,
  WEALTH_MAX_SEATS,
  WEALTH_MIN_SEATS,
  professionColor,
  professionEmoji,
} from '@/types/wealth';

export interface WealthCreateRequest {
  name?: string;
  agent_seats: AgentSeatRequest[];
  month_ms: number;
  pool: 'curated' | 'docs';
  seed?: number;
}

interface Props {
  open: boolean;
  onClose: () => void;
  /** true=创建成功（父组件关弹窗+导航）；false=失败（就地显示 formError）。 */
  onSubmit: (req: WealthCreateRequest) => Promise<boolean>;
  submitting?: boolean;
}

const MONTH_MS_PRESETS = [
  { ms: 3000, key: 'wealth.monthMs.fast' },
  { ms: 8000, key: 'wealth.monthMs.normal' },
  { ms: 15000, key: 'wealth.monthMs.slow' },
] as const;

/**
 * Agent 数档位 0..12（= 房间容量）。11 = 「1 人类 + 11 Agent」满员；
 * 12 = 全 Agent 房（创建者降级观战者）。0..9 允许创建「等人类加入」的房间，
 * 但会被 MinSeats 校验拦住提交（见 handleSubmit）。
 */
const SEAT_COUNT_OPTIONS = Array.from({ length: WEALTH_MAX_SEATS + 1 }, (_, i) => i);

/** API 卡片 → 展示行（补齐静态色 / emoji）。 */
function toDisplayCards(cards: WealthProfessionCard[]): WealthProfessionCard[] {
  return cards.map((c) => ({
    ...c,
    avatar: c.avatar || c.id.toLowerCase(),
  }));
}

/**
 * Fisher-Yates 洗牌，返回新数组（不改动入参）。
 * 每次调用独立随机 —— 多次「重新分配」得到不同排列（R1 随机性）。
 */
function shuffle<T>(arr: readonly T[]): T[] {
  const a = [...arr];
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
}

/**
 * 均匀随机分配座位模型（方案文档 §3.1）：
 * ceil(slots / models.length) 轮独立洗牌拼接 → 截断前 slots 个。
 * - models.length ≥ slots：等价于随机排列取前 slots 个 → 座位间零重复（R2）；
 * - models.length < slots：多轮洗牌拼接保证任意前缀中每个模型出现
 *   floor(slots/len) 或 ceil(slots/len) 次，次数差 ≤ 1（R3 均匀）；
 * - 相邻同模型修复：拼接边界可能出现「…A | A…」，从 i+1 起找最近的异值
 *   元素与 out[i] 交换消除相邻重复；交换只挪位置不改各模型总次数，
 *   均匀性保持。len == 1 时无模型可换（物理上限），保持原样；
 * - 与后端 alternateModelsLocked（Fisher-Yates 去重兜底，CLAUDE.md §14.2）
 *   对齐：即便客户端被绕过直发重复 key，服务端仍会随机改写重复项。
 */
function shuffledRoundRobinModels(models: ModelInfo[], slots: number): string[] {
  if (models.length === 0 || slots <= 0) return [];
  // ceil 轮洗牌拼接成候选池：任何前缀内各模型出现次数差 ≤ 1（均匀性来源）。
  const rounds = Math.ceil(slots / models.length);
  const pool: string[] = [];
  for (let r = 0; r < rounds; r++) {
    pool.push(...shuffle(models.map((m) => m.model)));
  }
  const out = pool.slice(0, slots);
  // 相邻同模型修复（len >= 2 才有交换空间）：向后找第一个异值元素交换，
  // 找不到（如尾部只剩同一模型）保持原样，交给后端 alternateModelsLocked 兜底。
  if (models.length >= 2) {
    for (let i = 1; i < out.length; i++) {
      if (out[i] !== out[i - 1]) continue;
      for (let j = i + 1; j < out.length; j++) {
        if (out[j] !== out[i]) {
          [out[i], out[j]] = [out[j], out[i]];
          break;
        }
      }
    }
  }
  return out;
}

export const WealthCreateRoomModal: React.FC<Props> = ({
  open, onClose, onSubmit, submitting = false,
}) => {
  const t = useT();
  const [name, setName] = useState('');
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [modelsError, setModelsError] = useState<string | null>(null);
  // 2026-09-16 §财商流10–12座位：默认 10 个 Agent（+ 创建者 = 11 座 ≥ MinSeats）。
  const [agentCount, setAgentCount] = useState(WEALTH_DEFAULT_AGENT_COUNT);
  const [seatModels, setSeatModels] = useState<string[]>([]);
  const [monthMs, setMonthMs] = useState(8000);
  const [pool, setPool] = useState<'curated' | 'docs'>('curated');
  const [seed, setSeed] = useState('');
  const [professions, setProfessions] = useState<WealthProfessionCard[]>(
    () => toDisplayCards(CURATED_PROFESSIONS),
  );
  const [poolInfo, setPoolInfo] = useState<{ available: boolean; total: number; indexed: number } | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [localSubmitting, setLocalSubmitting] = useState(false);

  /**
   * 全 Agent 房：Agent 数 == 房间容量(12) → 后端 freeSeats 为空 → 创建者自动降级
   * 为观战者（service/room_service_crud.go creatorAsSpectator）。此状态**只能**由
   * agentCount 推导，不做成独立开关：否则会出现「勾了观战者却只配 10 个 Agent」
   * 这种与后端矛盾的请求（后端照样把创建者塞进空位当玩家）。
   */
  const allAgentRoom = agentCount >= WEALTH_MAX_SEATS;
  /** 本房间总座位（Agent + 创建者；全 Agent 房创建者不占座）→ MinSeats 校验用。 */
  const totalSeats = Math.min(agentCount + (allAgentRoom ? 0 : 1), WEALTH_MAX_SEATS);

  // 打开时拉模型列表 + 职业卡（各自 best-effort，失败不阻塞建房）。
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setFormError(null);
    listModels()
      .then((ms) => {
        if (cancelled) return;
        setModels(ms ?? []);
        if (ms && ms.length > 0) {
          // 每次弹窗打开、模型加载成功都整体重新随机均匀分配 12 槽位
          // （方案文档 W1：R1 随机 —— 去掉「仅首次初始化」守卫，关闭重开
          // 组合大概率不同；用户上次的手动改选随默认值一并重置）。
          setSeatModels(shuffledRoundRobinModels(ms, WEALTH_MAX_SEATS));
        }
      })
      .catch((e: Error) => {
        if (!cancelled) setModelsError(e.message);
      });
    fetchProfessions()
      .then((r) => {
        if (cancelled || !r) return;
        if (r.curated?.length) setProfessions(toDisplayCards(r.curated));
        setPoolInfo(r.pool ?? null);
      })
      .catch(() => {
        // 静默回落 CURATED_PROFESSIONS 静态镜像（降级策略 §9）。
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  const busy = submitting || localSubmitting;

  // 座位模型槽位固定 12 个（= 房间容量）。模型列表仅由 listModels() 写入，
  // 与上面 W1 的随机分配同源同步，确定性轮询补齐不再需要 —— 这里只做纯
  // 长度对齐（截断到 12）；prev 长度已对齐时返回原引用，避免无效重渲染。
  useEffect(() => {
    setSeatModels((prev) =>
      prev.length === WEALTH_MAX_SEATS ? prev : prev.slice(0, WEALTH_MAX_SEATS),
    );
  }, [models]);

  const agentSeats: AgentSeatRequest[] = useMemo(
    () =>
      Array.from({ length: agentCount }, (_, i) => ({
        // Agent 占座位号 0..N-1；创建者由后端从剩余空位随机入座（全 Agent 房无空位）。
        seat: i,
        model_key: seatModels[i] || models[i % Math.max(1, models.length)]?.model || '',
      })).filter((s) => s.model_key !== ''),
    [agentCount, seatModels, models],
  );

  const handleSubmit = async () => {
    setFormError(null);
    if (agentCount > 0 && models.length === 0) {
      setFormError(modelsError || t('wealth.create.noModels' as TKey));
      return;
    }
    // MinSeats 门控（§7.1 优先级 1：就地内联红条，弹窗不关闭）。
    // 后端 room.Start 在 occupied < MinSeats 时返回 35003，前端提前拦截并给出
    // 可操作文案（增加 Agent 座位或改勾「以观战者身份建房」）。
    if (totalSeats < WEALTH_MIN_SEATS) {
      setFormError(
        t('wealth.create.needMinSeats' as TKey, { n: totalSeats, min: WEALTH_MIN_SEATS }),
      );
      return;
    }
    const seedNum = Math.floor(Number(seed) || 0);
    setLocalSubmitting(true);
    try {
      const ok = await onSubmit({
        name: name.trim() || undefined,
        agent_seats: agentSeats,
        month_ms: monthMs,
        pool,
        ...(seedNum > 0 ? { seed: seedNum } : {}),
      });
      if (!ok) {
        // 父组件已 setErr + reportGlobalError；这里不重复上报，仅保留弹窗。
        setFormError(t('wealth.create.failed' as TKey));
      }
    } finally {
      setLocalSubmitting(false);
    }
  };

  return (
    <AppModal
      title={`💰 ${t('wealth.createRoom' as TKey)}`}
      icon="💰"
      kind="info"
      maxWidth={560}
      dismissible={!busy}
      blockBackdropClose
      loading={busy}
      onClose={() => {
        if (!busy) onClose();
      }}
      footer={
        <>
          <button type="button" className="btn btn-secondary" onClick={onClose} disabled={busy}>
            {t('common.cancel')}
          </button>
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => void handleSubmit()}
            disabled={busy}
            data-testid="wealth-create-confirm"
          >
            {busy ? t('common.loading') : t('wealth.createRoom' as TKey)}
          </button>
        </>
      }
    >
      <div className="wealth-create-form">
        <label className="wealth-create-form__row">
          <span>{t('wealth.roomName' as TKey)}</span>
          <input
            type="text"
            value={name}
            maxLength={30}
            placeholder={t('wealth.create.namePlaceholder' as TKey)}
            onChange={(e) => setName(e.target.value)}
            disabled={busy}
          />
        </label>

        {/* Agent 座位：数量档位（0..12）+ 每座位模型 */}
        <div className="wealth-create-form__row">
          <span>
            {allAgentRoom
              ? `${t('wealth.agentSeats' as TKey)} = ${totalSeats}/${WEALTH_MAX_SEATS}`
              : `${t('wealth.agentSeats' as TKey)}（+${t('wealth.create.meSeat' as TKey)} = ${totalSeats}/${WEALTH_MAX_SEATS}）`}
          </span>
          <div className="wealth-create-form__seatcount" data-testid="wealth-create-agent-tiers">
            {SEAT_COUNT_OPTIONS.map((n) => (
              <button
                key={n}
                type="button"
                className={
                  'wealth-tier-btn wealth-tier-btn--sm' +
                  (agentCount === n ? ' wealth-tier-btn--active' : '')
                }
                onClick={() => setAgentCount(n)}
                disabled={busy}
                aria-pressed={agentCount === n}
                data-testid={`wealth-create-agent-tier-${n}`}
              >
                {n}
              </button>
            ))}
          </div>
        </div>

        {/* 创建者身份（由 Agent 数推导，不可独立勾选）：
            Agent < 12 → 入座玩家（后端随机空位）；Agent == 12 → 观战者（全 Agent 房） */}
        <div className="wealth-create-form__row">
          <span>{t('wealth.create.creatorRole' as TKey)}</span>
          <span
            className="wealth-create-form__creatorrole"
            data-testid="wealth-create-creator-role"
            aria-live="polite"
          >
            {allAgentRoom
              ? `👁 ${t('wealth.create.watchOnly' as TKey, { max: WEALTH_MAX_SEATS })}`
              : `🧑 ${t('wealth.create.creatorPlayer' as TKey)}`}
          </span>
        </div>

        {/* 座位数校验：MinSeats(10) 以下无法开局 —— 就地黄条提示 + 提交时红条拦截 */}
        {totalSeats < WEALTH_MIN_SEATS ? (
          <p className="wealth-create-form__hint" data-testid="wealth-create-seats-warning">
            ⚠️ {t('wealth.create.needMinSeats' as TKey, { n: totalSeats, min: WEALTH_MIN_SEATS })}
          </p>
        ) : (
          <p className="wealth-create-form__hint wealth-create-form__hint--ok">
            {t('wealth.create.seatsOk' as TKey, { n: totalSeats, max: WEALTH_MAX_SEATS })}
          </p>
        )}

        {agentCount > 0 && (
          <>
            {/* 座位区标题行（方案文档 W3）：座位数文案 + 一键「🎲 重新分配」
                整体重摇。内联 flex 两端对齐 + 复用 wealth-tier-btn 系列，
                不新增 CSS 规则（规避 §26.3 JSX 拼接零 CSS 命中缺陷）；
                结构参考狼人杀 seatblock-head，但零 import（跨游戏 import 硬禁令）。 */}
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <span>
                {t('wealth.agentSeats' as TKey)} × {agentCount}
              </span>
              <button
                type="button"
                className="wealth-tier-btn wealth-tier-btn--sm"
                data-testid="wealth-create-reshuffle"
                onClick={() => setSeatModels(shuffledRoundRobinModels(models, WEALTH_MAX_SEATS))}
                disabled={busy || models.length === 0}
              >
                {t('wealth.create.reshuffle' as TKey)}
              </button>
            </div>
            <div className="wealth-create-form__seats">
              {Array.from({ length: agentCount }, (_, i) => (
                <label key={i} className="wealth-create-form__seatrow">
                  <span className="wealth-create-form__seatno">
                    {t('wealth.create.seatNo' as TKey, { n: i + 1 })}
                  </span>
                  <select
                    value={seatModels[i] ?? ''}
                    onChange={(e) => {
                      const next = [...seatModels];
                      next[i] = e.target.value;
                      setSeatModels(next);
                    }}
                    disabled={busy}
                    data-testid={`wealth-create-seat-model-${i}`}
                  >
                    {models.length === 0 && <option value="">{t('wealth.create.noModels' as TKey)}</option>}
                    {models.map((m) => (
                      <option key={m.model} value={m.model}>
                        {m.agent_name}
                      </option>
                    ))}
                  </select>
                </label>
              ))}
            </div>
            {/* 模型数 < Agent 座位数（方案文档附带 W4）：重复是物理约束而非
                bug —— 前端已按均匀原则复用（次数差 ≤ 1 + 相邻去重），后端
                alternateModelsLocked 仍会随机改写重复项兜底。 */}
            {models.length > 0 && models.length < agentCount && (
              <p className="wealth-create-form__hint">
                ⚠️ {t('wealth.create.modelReuseHint' as TKey, { n: models.length, m: agentCount })}
              </p>
            )}
          </>
        )}

        {/* 月节拍速度 */}
        <div className="wealth-create-form__row">
          <span>{t('wealth.monthMs' as TKey)}</span>
          <div className="wealth-create-form__seatcount">
            {MONTH_MS_PRESETS.map((p) => (
              <button
                key={p.ms}
                type="button"
                className={'wealth-tier-btn' + (monthMs === p.ms ? ' wealth-tier-btn--active' : '')}
                onClick={() => setMonthMs(p.ms)}
                disabled={busy}
              >
                {t(p.key as TKey)}
              </button>
            ))}
          </div>
        </div>
        <label className="wealth-create-form__row">
          <span>{monthMs >= 1000 ? `${(monthMs / 1000).toFixed(1)}s` : `${monthMs}ms`}</span>
          <input
            type="range"
            min={3000}
            max={30000}
            step={1000}
            value={monthMs}
            onChange={(e) => setMonthMs(Number(e.target.value))}
            disabled={busy}
          />
        </label>

        {/* 职业池 */}
        <div className="wealth-create-form__row">
          <span>{t('wealth.pool' as TKey)}</span>
          <div className="wealth-create-form__seatcount">
            <button
              type="button"
              className={'wealth-tier-btn' + (pool === 'curated' ? ' wealth-tier-btn--active' : '')}
              onClick={() => setPool('curated')}
              disabled={busy}
            >
              {t('wealth.pool.curated' as TKey)}
            </button>
            <button
              type="button"
              className={'wealth-tier-btn' + (pool === 'docs' ? ' wealth-tier-btn--active' : '')}
              onClick={() => setPool('docs')}
              disabled={busy}
              title={poolInfo && !poolInfo.available ? t('wealth.pool.unavailable' as TKey) : undefined}
            >
              {t('wealth.pool.docs' as TKey)}
            </button>
          </div>
        </div>
        {pool === 'docs' && poolInfo && !poolInfo.available && (
          <p className="wealth-create-form__hint">⚠️ {t('wealth.pool.unavailable' as TKey)}</p>
        )}
        {poolInfo && poolInfo.total >= 0 && (
          <p className="wealth-create-form__hint">
            {t('wealth.pool.stats' as TKey, {
              total: poolInfo.total,
              indexed: poolInfo.indexed,
            })}
          </p>
        )}
        {/* 2026-09-14 §财商流P0-bugfix: total=-1 表示文档池索引尚未懒构建,
            原样渲染会显示"文档池共 -1 卡";此时显示统计中提示。 */}
        {poolInfo && poolInfo.total < 0 && (
          <p className="wealth-create-form__hint">{t('wealth.pool.indexing' as TKey)}</p>
        )}

        {/* 随机种子 */}
        <label className="wealth-create-form__row">
          <span>{t('wealth.seed' as TKey)}</span>
          <input
            type="number"
            min={0}
            value={seed}
            onChange={(e) => setSeed(e.target.value)}
            placeholder="0"
            disabled={busy}
          />
        </label>

        {/* 职业卡一览 */}
        <details className="wealth-create-form__cards">
          <summary>
            🎴 {t('wealth.pool.cardsTitle' as TKey, { n: professions.length })}
          </summary>
          <div className="wealth-cards-grid">
            {professions.map((c) => (
              <div
                key={c.id}
                className="wealth-card"
                style={{ borderColor: professionColor(c.id) }}
                title={c.opening_hook || c.title}
              >
                <div className="wealth-card__head">
                  <span className="wealth-card__emoji">{professionEmoji(c.id)}</span>
                  <b>{c.title}</b>
                  <small>{c.id}</small>
                </div>
                <div className="wealth-card__meta">
                  <span>{t('wealth.pool.cardSalary' as TKey, { v: c.salary.toLocaleString() })}</span>
                  <span>
                    {t('wealth.pool.cardSavings' as TKey, { v: (c.savings / 10000).toFixed(1) })}
                  </span>
                </div>
                {c.goals && c.goals.length > 0 && (
                  <div className="wealth-card__goal">🎯 {c.goals[c.goals.length - 1]}</div>
                )}
              </div>
            ))}
          </div>
        </details>

        {formError && <div className="wealth-action-form__error" role="alert">{formError}</div>}
      </div>
    </AppModal>
  );
};
