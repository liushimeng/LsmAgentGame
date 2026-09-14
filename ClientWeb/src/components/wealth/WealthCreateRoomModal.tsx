/**
 * WealthCreateRoomModal — 财商流建房弹窗：
 *   - 房间名（可选）
 *   - Agent 座位（0–7 个，创建者默认 0 号座 → 总座位 3–8；模型来自 /api/llm/models）
 *   - 月节拍速度（3000 / 8000 / 15000ms 预设 + 3000–30000 滑杆）
 *   - 职业卡池（curated 精选 10 卡 / docs 文档池）+ 职业卡一览
 *     （GET /api/games/wealth/professions，失败回落静态镜像，不阻塞建房）
 *   - 随机种子（可选，确定性复现）
 *
 * §7.1：提交失败内联红条、弹窗不关闭（onSubmit 返回 false）。
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

const SEAT_COUNT_OPTIONS = [2, 3, 4, 5, 6, 7]; // Agent 数（+创建者 0 号座 = 总 3–8）
const MONTH_MS_PRESETS = [
  { ms: 3000, key: 'wealth.monthMs.fast' },
  { ms: 8000, key: 'wealth.monthMs.normal' },
  { ms: 15000, key: 'wealth.monthMs.slow' },
] as const;

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
  const [agentCount, setAgentCount] = useState(2);
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
          // 默认座位 1..7 轮转分配不同模型（§14.2 去重随机化后端仍会兜底）。
          setSeatModels((prev) =>
            prev.length === 0
              ? Array.from({ length: 7 }, (_, i) => ms[i % ms.length]?.model ?? '')
              : prev,
          );
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

  // Agent 数调整时裁剪/补齐座位模型。
  useEffect(() => {
    setSeatModels((prev) => {
      const next = [...prev];
      while (next.length < 7) next.push(models.length > 0 ? models[next.length % models.length]?.model ?? '' : '');
      return next.slice(0, 7);
    });
  }, [models]);

  const agentSeats: AgentSeatRequest[] = useMemo(
    () =>
      Array.from({ length: agentCount }, (_, i) => ({
        seat: i + 1,
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

        {/* Agent 座位：数量 + 每座位模型 */}
        <div className="wealth-create-form__row">
          <span>{t('wealth.agentSeats' as TKey)}（+{t('wealth.create.meSeat' as TKey)} = {agentCount + 1}）</span>
          <div className="wealth-create-form__seatcount">
            {SEAT_COUNT_OPTIONS.map((n) => (
              <button
                key={n}
                type="button"
                className={'wealth-tier-btn' + (agentCount === n ? ' wealth-tier-btn--active' : '')}
                onClick={() => setAgentCount(n)}
                disabled={busy}
              >
                {n}
              </button>
            ))}
          </div>
        </div>
        {agentCount > 0 && (
          <div className="wealth-create-form__seats">
            {Array.from({ length: agentCount }, (_, i) => (
              <label key={i} className="wealth-create-form__seatrow">
                <span className="wealth-create-form__seatno">{i + 2}号位</span>
                <select
                  value={seatModels[i] ?? ''}
                  onChange={(e) => {
                    const next = [...seatModels];
                    next[i] = e.target.value;
                    setSeatModels(next);
                  }}
                  disabled={busy}
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
        {poolInfo && (
          <p className="wealth-create-form__hint">
            {t('wealth.pool.stats' as TKey, {
              total: poolInfo.total,
              indexed: poolInfo.indexed,
            })}
          </p>
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
            🎴 {t('wealth.pool.cards' as TKey)}（{professions.length}）
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
                  <span>¥{c.salary.toLocaleString('zh-CN')}/月</span>
                  <span>储蓄 ¥{(c.savings / 10000).toFixed(1)}万</span>
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
