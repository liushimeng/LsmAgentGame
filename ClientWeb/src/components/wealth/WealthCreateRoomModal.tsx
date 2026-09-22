/**
 * WealthCreateRoomModal — 虚拟城市「创建城市」弹窗（2026-09-22 §CityHuman重构）：
 *   - 城市名（可选）
 *   - 焦点居民数（档位 1–12，默认 12）：由 LLM 线路池全工具深度驱动的居民
 *     （agent_seats[].model_key 一律空串 = 线路池驱动）。虚拟城市是全 Agent
 *     城市模拟器 —— 没有玩家座位，创建者一律为观察者。
 *   - 背景居民规模（§20260921 城市背景层）：数字输入 + 预设档 12 / 1千 / 1万 /
 *     10万，clamp 1..100000，默认 10000。数据模拟驱动，从 10 万人物卡知识库
 *     加载专属档案（档案锚定设计 §8.4），逐月演化、抽样发声。
 *   - LLM 线路池信息行：listModels() → Σ concurrency_lines；N=0 黄色警示。
 *   - 模拟月节拍（3000 / 8000 / 15000ms 预设 + 3000–30000 滑杆）
 *   - 职业卡池（curated 精选 10 卡 / docs 文档池）+ 职业卡一览
 *     （GET /api/games/wealth/professions，失败回落静态镜像，不阻塞建房）
 *   - 随机种子（可选，确定性复现）
 *
 * §7.1：提交失败 / 校验不通过内联红条（formError）、弹窗不关闭
 * （onSubmit 返回 false 或本地校验拦截）；listModels 失败内联提示 +
 * reportGlobalError 双通道上报。
 */

import React, { useEffect, useState } from 'react';
import { AppModal } from '@/components/ui/AppModal';
import { listModels, type ModelInfo } from '@/api/llm';
import { fetchProfessions, type WealthProfessionCard } from '@/api/wealth';
import { reportGlobalError } from '@/services/globalError';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { AgentSeatRequest } from '@/types/api';
import {
  CURATED_PROFESSIONS,
  WEALTH_MAX_SEATS,
  WEALTH_MIN_SEATS,
  professionColor,
  professionEmoji,
} from '@/types/wealth';

export interface WealthCreateRequest {
  name?: string;
  agent_seats: AgentSeatRequest[];
  /** §20260921 城市背景层居民数（1..100000；后端 clamp）。 */
  resident_count: number;
  month_ms: number;
  pool: 'curated' | 'docs';
  seed?: number;
  /** 2026-09-19 §全Agent模式: 是否全 Agent 模式(默认 true)。 */
  full_agent?: boolean;
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
 * 焦点居民数档位 1..12（= 房间容量）。2026-09-22 §CityHuman重构：
 * 全 Agent 城市没有玩家座位、不允许 0 档（焦点层不能为空）；
 * 后端 MinSeats(10) 校验仍生效（< 10 无法自动启动模拟，见 handleSubmit）。
 */
const SEAT_COUNT_OPTIONS = Array.from({ length: WEALTH_MAX_SEATS }, (_, i) => i + 1);

/** 城市居民数预设档（§20260921 建房解耦契约 §2.1）。 */
const RESIDENT_PRESETS: { value: number; label: string }[] = [
  { value: 12, label: '12' },
  { value: 1000, label: '1千' },
  { value: 10000, label: '1万' },
  { value: 100000, label: '10万' },
];

const RESIDENT_MIN = 1;
const RESIDENT_MAX = 100000;
const RESIDENT_DEFAULT = 10000;

/** clamp 居民数到 [1, 100000]（非法输入回落默认值）。 */
function clampResidents(v: number): number {
  if (!Number.isFinite(v)) return RESIDENT_DEFAULT;
  return Math.min(RESIDENT_MAX, Math.max(RESIDENT_MIN, Math.round(v)));
}

/** API 卡片 → 展示行（补齐静态色 / emoji）。 */
function toDisplayCards(cards: WealthProfessionCard[]): WealthProfessionCard[] {
  return cards.map((c) => ({
    ...c,
    avatar: c.avatar || c.id.toLowerCase(),
  }));
}

export const WealthCreateRoomModal: React.FC<Props> = ({
  open, onClose, onSubmit, submitting = false,
}) => {
  const t = useT();
  const [name, setName] = useState('');
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [modelsError, setModelsError] = useState<string | null>(null);
  // 2026-09-22 §CityHuman重构：焦点居民数默认 12（全城深度驱动）。
  const [agentCount, setAgentCount] = useState(WEALTH_MAX_SEATS);
  // §20260921 城市背景层 — 居民数（默认 1 万，clamp 1..100000）。
  const [residentCount, setResidentCount] = useState(RESIDENT_DEFAULT);
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
   * 2026-09-22 §CityHuman重构：虚拟城市是全 Agent 城市 —— full_agent 恒 true，
   * 创建者一律为观察者，不存在「创建者入座」；焦点居民数即 bot 座位数。
   */
  const totalSeats = agentCount;

  /** §20260921 线路池 — Σ concurrency_lines（/api/llm/models 只列可用模型）。 */
  const linePoolTotal = models.reduce((sum, m) => sum + (m.concurrency_lines ?? 1), 0);

  // 打开时拉模型列表 + 职业卡（各自 best-effort，失败不阻塞建房）。
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setFormError(null);
    // §7.1 — listModels 失败：弹窗内联提示 + reportGlobalError 双通道
    //（线路池信息行就地渲染 modelsError，不吞进 console）。
    listModels()
      .then((ms) => {
        if (cancelled) return;
        setModels(ms ?? []);
      })
      .catch((e: Error) => {
        if (cancelled) return;
        setModelsError(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
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

  // §20260921 建房解耦 — 居民座位不绑定模型：model_key 空串 = 线路池驱动
  //（后端 ValidateAgentSeats 对 kind=wealth 放行空 key）。
  const agentSeats: AgentSeatRequest[] = Array.from({ length: agentCount }, (_, i) => ({
    seat: i,
    model_key: '',
  }));

  const handleSubmit = async () => {
    setFormError(null);
    if (agentCount > 0 && models.length === 0) {
      setFormError(modelsError || t('wealth.create.noModels' as TKey));
      return;
    }
    // MinSeats 门控（§7.1 优先级 1：就地内联红条，弹窗不关闭）。
    // 后端 room.Start 在 occupied < MinSeats 时返回 35003，前端提前拦截。
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
        resident_count: clampResidents(residentCount),
        month_ms: monthMs,
        pool,
        ...(seedNum > 0 ? { seed: seedNum } : {}),
        full_agent: true, // 2026-09-19 §全Agent模式: 虚拟城市默认全 Agent
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
      title={`🏙 ${t('wealth.createRoom' as TKey)}`}
      icon="🏙"
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

        {/* 焦点居民数：1..12 档位；由 LLM 线路池统一驱动（全 Agent 城市，无玩家座位） */}
        <div className="wealth-create-form__row">
          <span>
            {`${t('wealth.agentSeats' as TKey)} = ${totalSeats}/${WEALTH_MAX_SEATS}`}
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

        {/* 座位数校验：MinSeats(10) 以下无法启动模拟 —— 就地黄条提示 + 提交时红条拦截 */}
        {totalSeats < WEALTH_MIN_SEATS ? (
          <p className="wealth-create-form__hint" data-testid="wealth-create-seats-warning">
            ⚠️ {t('wealth.create.needMinSeats' as TKey, { n: totalSeats, min: WEALTH_MIN_SEATS })}
          </p>
        ) : (
          <p className="wealth-create-form__hint wealth-create-form__hint--ok">
            {t('wealth.create.seatsOk' as TKey, { n: totalSeats, max: WEALTH_MAX_SEATS })}
          </p>
        )}

        {/* 背景居民规模（§20260921 城市背景层 + §CityHuman重构改名）：数字输入 + 预设档 12/1千/1万/10万 */}
        <div className="wealth-create-form__row">
          <span>{t('wealth.residentCount' as TKey)}</span>
          <div className="wealth-create-form__seatcount" data-testid="wealth-create-resident-tiers">
            {RESIDENT_PRESETS.map((p) => (
              <button
                key={p.value}
                type="button"
                className={
                  'wealth-tier-btn wealth-tier-btn--sm' +
                  (residentCount === p.value ? ' wealth-tier-btn--active' : '')
                }
                onClick={() => setResidentCount(p.value)}
                disabled={busy}
                aria-pressed={residentCount === p.value}
                data-testid={`wealth-create-resident-tier-${p.value}`}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
        <label className="wealth-create-form__row">
          <span>
            {t('wealth.residentCount' as TKey)} · {residentCount.toLocaleString()}
          </span>
          <input
            type="number"
            min={RESIDENT_MIN}
            max={RESIDENT_MAX}
            step={1}
            value={residentCount}
            onChange={(e) => setResidentCount(clampResidents(Number(e.target.value)))}
            disabled={busy}
            data-testid="wealth-create-resident-count"
          />
        </label>
        <p className="wealth-create-form__hint" data-testid="wealth-create-resident-hint">
          🏙 {t('wealth.residentCountHint' as TKey)}
        </p>

        {/* §20260921 线路池信息行 — N=0 黄色警示（无可调模型，先去模型管理配置）。
            listModels 失败：内联红条展示错误原文（reportGlobalError 已在 catch 上报）。 */}
        {modelsError ? (
          <p className="wealth-create-form__hint" role="alert" data-testid="wealth-create-linepool-error">
            ⚠️ {modelsError}
          </p>
        ) : models.length === 0 ? (
          <p className="wealth-create-form__hint" data-testid="wealth-create-linepool-empty">
            ⚠️ {t('wealth.linePoolEmpty' as TKey)}
          </p>
        ) : (
          <p className="wealth-create-form__hint wealth-create-form__hint--ok" data-testid="wealth-create-linepool-info">
            🔌 {t('wealth.linePoolInfo' as TKey, { n: linePoolTotal })}
          </p>
        )}

        {/* 模拟月节拍 */}
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
        {/* 档案锚定设计 §8.3 — docs 池人物卡库提示（≈10 万张，开局自动锚定专属档案） */}
        {pool === 'docs' && (
          <p className="wealth-create-form__hint" data-testid="wealth-create-docs-pool-hint">
            📦 {t('wealth.pool.docsProfiles' as TKey)}
          </p>
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
