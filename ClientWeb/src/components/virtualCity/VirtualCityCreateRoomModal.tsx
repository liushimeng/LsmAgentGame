/**
 * VirtualCityCreateRoomModal — 虚拟城市「创建城市」弹窗（2026-09-22 §CityHuman全民驱动）：
 *   - 城市名（可选，≤30 字）
 *   - 背景居民规模（StepperRow 数值步进 [−] 输入 [+]，min=10 max=100000，
 *     缺省 10000，clamp [10,100000]）：用户唯一可设置的城市规模；
 *     每位居民都是 `LsmAgentGame-City-Human`（契约 01 §1）。虚拟城市没有玩家、
 *     没有座位档位概念 —— 职业卡池 / 档位选择 / agent_seats 已全部退役
 *     （2026-09-25 §LLM线路池配额：预设档 10/1千/1万/10万 按钮行与标签内
 *     重复数值显示一并退役，统一收敛为单行 stepper）。
 *   - LLM 线路池 stepper 行（2026-09-25 §LLM线路池配额）：min=1、
 *     max=min(listModels() Σ concurrency_lines, 64)，缺省 = 池上限，
 *     随 body.llm_lines 顶层字段提交；下方信息行 {n} = 池总量，
 *     N=0 黄色警示且提交拦截（与居民数无关）。
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

export interface VirtualCityCreateRequest {
  name?: string;
  /** 背景居民规模（10..100000；前端 clamp，后端缺省 10000 / <10 归 10）。 */
  resident_count: number;
  month_ms: number;
  seed?: number;
  /** 2026-09-19 §全Agent模式: 是否全 Agent 模式(默认 true)。 */
  full_agent?: boolean;
  /** 批次 20 文档 3 A2/A4：市长选举启用开关（缺省 false = 关闭，R8-2 语义）。 */
  civic_election_enabled?: boolean;
  /** 2026-09-25 §LLM线路池配额 — [1,64]，缺省不发送。 */
  llm_lines?: number;
}

interface Props {
  open: boolean;
  onClose: () => void;
  /** true=创建成功（父组件关弹窗+导航）；false=失败（就地显示 formError）。 */
  onSubmit: (req: VirtualCityCreateRequest) => Promise<boolean>;
  submitting?: boolean;
}

const MONTH_MS_PRESETS = [
  { ms: 3000, key: 'virtualCity.monthMs.fast' },
  { ms: 8000, key: 'virtualCity.monthMs.normal' },
  { ms: 15000, key: 'virtualCity.monthMs.slow' },
] as const;

const RESIDENT_MIN = 10;
const RESIDENT_MAX = 100000;
const RESIDENT_DEFAULT = 10;

/** clamp 居民数到 [10, 100000]（非法输入回落默认值）。 */
function clampResidents(v: number): number {
  if (!Number.isFinite(v)) return RESIDENT_DEFAULT;
  return Math.min(RESIDENT_MAX, Math.max(RESIDENT_MIN, Math.round(v)));
}

/** 2026-09-25 §LLM线路池配额 — 线路上限 = min(Σ concurrency_lines, 64)，下限 1。
 *  闭包内初始化 llmLines 用（勿依赖渲染期 linePoolTotal）；池空时返回 1 仅作
 *  展示兜底（stepper 禁用 + 提交拦截在渲染层把关）。 */
function lineCapOf(ms: ModelInfo[]): number {
  return Math.max(1, Math.min(ms.reduce((s, m) => s + (m.concurrency_lines ?? 1), 0), 64));
}

/** 数值步进行:label + [−] 数值输入 [+]（2026-09-25 §LLM线路池配额;
 *  背景居民规模与 LLM 线路池两行共用）。 */
function StepperRow(props: {
  label: string;
  value: number;
  min: number;
  max: number;
  disabled?: boolean;
  testId: string;
  onChange: (v: number) => void;
}) {
  const clamp = (v: number): number =>
    Number.isFinite(v) ? Math.min(props.max, Math.max(props.min, Math.round(v))) : props.value;
  return (
    <div className="virtualCity-create-form__row">
      <span>{props.label}</span>
      <div className="virtualCity-create-form__stepper" data-testid={props.testId}>
        <button
          type="button"
          className="virtualCity-create-form__stepper__btn"
          disabled={props.disabled || props.value <= props.min}
          aria-label={`Decrease ${props.testId}`}
          data-testid={`${props.testId}-minus`}
          onClick={() => props.onChange(clamp(props.value - 1))}
        >
          −
        </button>
        <input
          type="number"
          min={props.min}
          max={props.max}
          step={1}
          value={props.value}
          onChange={(e) => props.onChange(clamp(Number(e.target.value)))}
          onBlur={() => props.onChange(clamp(props.value))}
          disabled={props.disabled}
          data-testid={`${props.testId}-input`}
        />
        <button
          type="button"
          className="virtualCity-create-form__stepper__btn"
          disabled={props.disabled || props.value >= props.max}
          aria-label={`Increase ${props.testId}`}
          data-testid={`${props.testId}-plus`}
          onClick={() => props.onChange(clamp(props.value + 1))}
        >
          ＋
        </button>
      </div>
    </div>
  );
}

export const VirtualCityCreateRoomModal: React.FC<Props> = ({
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
  // 批次 20 文档 3 A4：市长选举启用（默认关，随 body.civic_election_enabled 提交）。
  const [election, setElection] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [localSubmitting, setLocalSubmitting] = useState(false);

  /** §20260921 线路池 — Σ concurrency_lines（/api/llm/models 只列可用模型）。 */
  const linePoolTotal = models.reduce((sum, m) => sum + (m.concurrency_lines ?? 1), 0);
  // 2026-09-25 §LLM线路池配额 — 本房 Agent 并发线路数（0=未初始化；
  // listModels 成功后置为池上限，用户已改动则只 clamp 不覆盖）。
  const [llmLines, setLlmLines] = useState(0);
  /** stepper 上限 = min(池总量, 64)（64 = 后端 concurrency_lines / driver worker 硬上限）。 */
  const lineCap = Math.max(1, Math.min(linePoolTotal, 64));

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
        // 2026-09-25 §LLM线路池配额 — 缺省 = 池上限（与后端缺省行为一致）；
        // 闭包内用 lineCapOf(ms) 重算，勿引用渲染期 linePoolTotal。
        setLlmLines((prev) => {
          const cap = lineCapOf(ms ?? []);
          return prev > 0 ? Math.min(prev, cap) : cap;
        });
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
      setFormError(modelsError || t('virtualCity.create.noModels' as TKey));
      return;
    }
    // 居民规模兜底校验（正常被 clamp 不会触发；契约 01 §3.3）。
    if (!Number.isFinite(residentCount) || residentCount < RESIDENT_MIN || residentCount > RESIDENT_MAX) {
      setFormError(t('virtualCity.residentCountRequired' as TKey));
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
        // 批次 20 文档 3 A2：仅勾选时置 true（后端零值=false，勿做归一化）。
        civic_election_enabled: election,
        // 2026-09-25 §LLM线路池配额 — 未初始化/0 不发送（后端按池总量运行）。
        llm_lines: llmLines > 0 ? llmLines : undefined,
      });
      if (!ok) {
        // 父组件已 setErr + reportGlobalError；这里不重复上报，仅保留弹窗。
        setFormError(t('virtualCity.create.failed' as TKey));
      }
    } finally {
      setLocalSubmitting(false);
    }
  };

  return (
    <AppModal
      title={`🏙 ${t('virtualCity.createRoom' as TKey)}`}
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
            data-testid="virtualCity-create-confirm"
          >
            {busy ? t('common.loading') : t('virtualCity.createRoom' as TKey)}
          </button>
        </>
      }
    >
      <div className="virtualCity-create-form">
        <label className="virtualCity-create-form__row">
          <span>{t('virtualCity.roomName' as TKey)}</span>
          <input
            type="text"
            value={name}
            maxLength={30}
            placeholder={t('virtualCity.create.namePlaceholder' as TKey)}
            onChange={(e) => setName(e.target.value)}
            disabled={busy}
          />
        </label>

        {/* 背景居民规模 stepper：min=10 max=100000，即时归位 + 焦点离开写入边界值。
            2026-09-25 §LLM线路池配额 — 预设档按钮行与标签内重复数值显示退役，
            收敛为单行 StepperRow（testid 输入框后缀 -input，全库无旧 testid 消费方）。 */}
        <StepperRow
          label={t('virtualCity.residentCount' as TKey)}
          value={residentCount}
          min={RESIDENT_MIN}
          max={RESIDENT_MAX}
          disabled={busy}
          testId="virtualCity-create-resident-count"
          onChange={(v) => setResidentCount(clampResidents(v))}
        />
        <p className="virtualCity-create-form__hint" data-testid="virtualCity-create-resident-hint">
          🏙 {t('virtualCity.residentCountHint' as TKey)}
        </p>

        {/* 2026-09-25 §LLM线路池配额 — 本房 Agent 并发线路数 stepper：
            min=1、max=min(池总量,64)，缺省 = 池上限；池空（<1）禁用。 */}
        <StepperRow
          label={t('virtualCity.llmLines' as TKey)}
          value={Math.max(llmLines, 1)}
          min={1}
          max={lineCap}
          disabled={busy || linePoolTotal < 1}
          testId="virtualCity-create-llm-lines"
          onChange={(v) => setLlmLines(Math.min(lineCap, Math.max(1, Math.round(v))))}
        />

        {/* §20260921 线路池信息行 — N=0 黄色警示（无可调模型，先去模型管理配置）。
            listModels 失败：内联红条展示错误原文（reportGlobalError 已在 catch 上报）。 */}
        {modelsError ? (
          <p className="virtualCity-create-form__hint" role="alert" data-testid="virtualCity-create-linepool-error">
            ⚠️ {modelsError}
          </p>
        ) : linePoolTotal === 0 ? (
          <p className="virtualCity-create-form__hint" data-testid="virtualCity-create-linepool-empty">
            ⚠️ {t('virtualCity.linePoolEmpty' as TKey)}
          </p>
        ) : (
          <p className="virtualCity-create-form__hint virtualCity-create-form__hint--ok" data-testid="virtualCity-create-linepool-info">
            🔌 {t('virtualCity.linePoolInfo' as TKey, { n: linePoolTotal })}
          </p>
        )}

        {/* 模拟月节拍 */}
        <div className="virtualCity-create-form__row">
          <span>{t('virtualCity.monthMs' as TKey)}</span>
          <div className="virtualCity-create-form__seatcount">
            {MONTH_MS_PRESETS.map((p) => (
              <button
                key={p.ms}
                type="button"
                className={'virtualCity-tier-btn' + (monthMs === p.ms ? ' virtualCity-tier-btn--active' : '')}
                onClick={() => setMonthMs(p.ms)}
                disabled={busy}
              >
                {t(p.key as TKey)}
              </button>
            ))}
          </div>
        </div>
        <label className="virtualCity-create-form__row">
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
        <label className="virtualCity-create-form__row">
          <span>{t('virtualCity.seed' as TKey)}</span>
          <input
            type="number"
            min={0}
            value={seed}
            onChange={(e) => setSeed(e.target.value)}
            placeholder="0"
            disabled={busy}
          />
        </label>

        {/* 批次 20 文档 3 A4：市长选举启用开关（默认关 = 引擎完全 no-op，旧行为零偏移） */}
        {/* 复用既有 virtualCity-action-form__check 样式（globals virtualCity.css），不新增类 */}
        <label className="virtualCity-action-form__check">
          <input
            type="checkbox"
            checked={election}
            onChange={(e) => setElection(e.target.checked)}
            disabled={busy}
            data-testid="virtualCity-create-election"
          />
          🗳 {t('virtualCity.election.switch' as TKey)}
        </label>

        {formError && <div className="virtualCity-action-form__error" role="alert">{formError}</div>}
      </div>
    </AppModal>
  );
};
