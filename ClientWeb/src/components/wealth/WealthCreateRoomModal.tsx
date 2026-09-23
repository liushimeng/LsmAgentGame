/**
 * WealthCreateRoomModal — 虚拟城市「创建城市」弹窗（2026-09-22 §CityHuman全民驱动）：
 *   - 城市名（可选，≤30 字）
 *   - 背景居民规模（数值控件 min=10 max=100000 + 预设档 10 / 1千 / 1万 / 10万，
 *     缺省 10000，clamp [10,100000]）：用户唯一可设置的城市规模；
 *     每位居民都是 `LsmAgentGame-City-Human`（契约 01 §1）。虚拟城市没有玩家、
 *     没有座位档位概念 —— 职业卡池 / 档位选择 / agent_seats 已全部退役。
 *   - LLM 线路池信息行：listModels() → Σ concurrency_lines；N=0 黄色警示，
 *     线路数=0 时提交拦截（与居民数无关）。
 *   - 模拟月节拍（3000 / 8000 / 15000ms 预设 + 3000–30000 滑杆）
 *   - 随机种子（可选，确定性复现）
 *
 * §7.1：提交失败 / 校验不通过内联红条（formError）、弹窗不关闭
 * （onSubmit 返回 false 或本地校验拦截）；listModels 失败内联提示 +
 * reportGlobalError 双通道上报。
 */

import React, { useEffect, useState } from 'react';
import { AppModal } from '@/components/ui/AppModal';
import { listModels, type ModelInfo } from '@/api/llm';
import { reportGlobalError } from '@/services/globalError';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

export interface WealthCreateRequest {
  name?: string;
  /** 背景居民规模（10..100000；前端 clamp，后端缺省 10000 / <10 归 10）。 */
  resident_count: number;
  month_ms: number;
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

/** 背景居民规模预设档（契约 01 §1.1：10 / 1千 / 1万 / 10万）。 */
const RESIDENT_PRESETS: { value: number; label: string }[] = [
  { value: 10, label: '10' },
  { value: 1000, label: '1千' },
  { value: 10000, label: '1万' },
  { value: 100000, label: '10万' },
];

const RESIDENT_MIN = 10;
const RESIDENT_MAX = 100000;
const RESIDENT_DEFAULT = 10000;

/** clamp 居民数到 [10, 100000]（非法输入回落默认值）。 */
function clampResidents(v: number): number {
  if (!Number.isFinite(v)) return RESIDENT_DEFAULT;
  return Math.min(RESIDENT_MAX, Math.max(RESIDENT_MIN, Math.round(v)));
}

export const WealthCreateRoomModal: React.FC<Props> = ({
  open, onClose, onSubmit, submitting = false,
}) => {
  const t = useT();
  const [name, setName] = useState('');
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [modelsError, setModelsError] = useState<string | null>(null);
  // §CityHuman全民驱动 — 背景居民规模（缺省 1 万，clamp 10..100000）。
  const [residentCount, setResidentCount] = useState(RESIDENT_DEFAULT);
  const [monthMs, setMonthMs] = useState(8000);
  const [seed, setSeed] = useState('');
  const [formError, setFormError] = useState<string | null>(null);
  const [localSubmitting, setLocalSubmitting] = useState(false);

  /** §20260921 线路池 — Σ concurrency_lines（/api/llm/models 只列可用模型）。 */
  const linePoolTotal = models.reduce((sum, m) => sum + (m.concurrency_lines ?? 1), 0);

  // 打开时拉模型列表（best-effort，失败不阻塞建房——仅拦截提交）。
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
    return () => {
      cancelled = true;
    };
  }, [open]);

  const busy = submitting || localSubmitting;

  const handleSubmit = async () => {
    setFormError(null);
    // 线路数=0 即告警（§CityHuman全民驱动：与居民数无关 —— 没有线路任何居民都无法驱动）。
    if (linePoolTotal === 0) {
      setFormError(modelsError || t('wealth.create.noModels' as TKey));
      return;
    }
    // 居民规模兜底校验（正常被 clamp 不会触发；契约 01 §3.3）。
    if (!Number.isFinite(residentCount) || residentCount < RESIDENT_MIN || residentCount > RESIDENT_MAX) {
      setFormError(t('wealth.residentCountRequired' as TKey));
      return;
    }
    const seedNum = Math.floor(Number(seed) || 0);
    setLocalSubmitting(true);
    try {
      const ok = await onSubmit({
        name: name.trim() || undefined,
        resident_count: clampResidents(residentCount),
        month_ms: monthMs,
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

        {/* 背景居民规模预设档（契约 01 §1.1：10 / 1千 / 1万 / 10万） */}
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
                onClick={() => setResidentCount(clampResidents(p.value))}
                disabled={busy}
                aria-pressed={residentCount === p.value}
                data-testid={`wealth-create-resident-tier-${p.value}`}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
        {/* 背景居民规模数值控件：min=10 max=100000，即时归位 + 焦点离开写入边界值 */}
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
            onBlur={() => setResidentCount(clampResidents(residentCount))}
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
        ) : linePoolTotal === 0 ? (
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

        {formError && <div className="wealth-action-form__error" role="alert">{formError}</div>}
      </div>
    </AppModal>
  );
};
