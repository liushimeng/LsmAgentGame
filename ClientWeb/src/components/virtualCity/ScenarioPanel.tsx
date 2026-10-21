/**
 * ScenarioPanel — 场景推演台（条件触发者）。
 *
 * 2026-10-21 §城市模拟定位：虚拟城市 = 科研级全 Agent 城市模拟系统，人类只有
 * 观察者与条件触发者两种角色。本面板让任意登录用户（含观察者）向运行中的模拟
 * 注入场景条件（市场冲击 / 舆情 / 自然灾害 / 未知灾难），并就地观察指标效果。
 *
 * 契约：tmpPlan/虚拟城市-模拟程序化与预测运行优化_20261021_095114.md §5.1/§5.3
 *   GET  /api/games/virtual_city/scenario_catalog
 *   POST /api/games/virtual_city/rooms/:id/scenario
 *   GET  /api/games/virtual_city/rooms/:id/scenarios（最近 20 条）
 *
 * 四块：
 *   ① 场景目录：按后端 `group` 分组；点选场景卡片切换当前目标。
 *   ② 注入参数：强度档位（magnitudes，默认 default_magnitude）+ 可选持续月数 /
 *      作用目标（仅 params 含 target 时显示）/ 备注。
 *   ③ 触发：二次确认（ConfirmModal）后 POST；返回的 effects 就地展示，随后刷新历史。
 *   ④ 最近场景事件：kind → 目录名映射 + 强度 + 注入月 + 备注。
 *
 * 批次 68 追加第 ⑤ 块「预测运行」（注入表单之后、注入历史之前）：
 *   状态行（run / 观测月 / 注入次数，无 run 引导文案）+ 「生成预测报告」按钮 →
 *   GET /prediction → PredictionReportView 渲染（§7.2 清单）。
 *
 * 错误展示（CLAUDE.md §7.1）：所有 API 失败都同时「面板内联红条」+ 「全局 toast」
 * (`reportGlobalError`)，绝不 console-only；会话过期走 AuthModal 通道不重复展示。
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { isSessionExpiredError } from '@/services/http';
import { errorMessage, reportGlobalError } from '@/services/globalError';
import {
  applyVirtualCityScenario,
  fetchScenarioCatalog,
  fetchVirtualCityScenarios,
  getPredictionReport,
  type VirtualCityPredictionReport,
  type VirtualCityPredictionRunSummary,
  type VirtualCityScenarioApplyRequest,
  type VirtualCityScenarioEffect,
  type VirtualCityScenarioRecord,
  type VirtualCityScenarioSpec,
} from '@/api/virtualCity';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import { PredictionReportView } from '@/components/virtualCity/PredictionReportView';

interface Props {
  roomId: string;
  /** 模拟是否运行中（status === 'playing'）。未运行时禁止注入。 */
  playing: boolean;
}

/** 后端 group 中文名 → i18n（未知分组原样展示，不吞掉后端新分组）。 */
const GROUP_KEY: Record<string, TKey> = {
  '市场与经济': 'virtualCity.scenario.group.market' as TKey,
  '社会与舆情': 'virtualCity.scenario.group.society' as TKey,
  '自然灾害': 'virtualCity.scenario.group.nature' as TKey,
  '未知灾难': 'virtualCity.scenario.group.unknown' as TKey,
};

/** 已知分组的稳定展示顺序；目录里出现的新分组追加在后。 */
const GROUP_ORDER = ['市场与经济', '社会与舆情', '自然灾害', '未知灾难'];

/**
 * 后端 target 语义（`scenarioValidateTarget`）：
 *   - `params` 含 `goods` → 八大类之一（留空 = 全品类）；
 *   - `params` 含 `stance` → tight / loose（留空 = tight）；
 *   - 其余场景不消费 target。
 * 这里用 datalist 给出合法取值，自由输入仍由后端校验兜底（错误内联展示）。
 */
const GOODS_TARGETS = ['food', 'clothing', 'housing', 'household', 'transport', 'education', 'healthcare', 'misc'];
const STANCE_TARGETS = ['tight', 'loose'];
const TARGET_OPTIONS_ID = 'virtualCity-scenario-target-options';

/** 指标 delta 展示：整数原样，小数保留两位；正数带 +。 */
function fmtDelta(delta: number): string {
  if (!Number.isFinite(delta)) return '-';
  const abs = Number.isInteger(delta) ? String(delta) : delta.toFixed(2);
  return delta > 0 ? `+${abs}` : abs;
}

/** 注入时间戳展示：unix 毫秒 / ISO → `YYYY-MM-DD HH:mm`，解析失败原样返回。 */
function fmtWhen(raw?: number | string): string {
  if (raw === undefined || raw === null || raw === '') return '';
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return String(raw);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

export function ScenarioPanel({ roomId, playing }: Props) {
  const t = useT();

  const [specs, setSpecs] = useState<VirtualCityScenarioSpec[] | null>(null);
  const [catalogErr, setCatalogErr] = useState<string | null>(null);
  const [history, setHistory] = useState<VirtualCityScenarioRecord[] | null>(null);
  const [historyErr, setHistoryErr] = useState<string | null>(null);

  // ── 批次 68 预测运行 ──────────────────────────────────
  // runs 只为状态行；report 仅在点「生成预测报告」后拉取并渲染（§7.2）。
  const [predRuns, setPredRuns] = useState<VirtualCityPredictionRunSummary[] | null>(null);
  const [predStateErr, setPredStateErr] = useState<string | null>(null);
  const [selectedRunId, setSelectedRunId] = useState(''); // '' = 最新 run
  const [reportLoading, setReportLoading] = useState(false);
  const [report, setReport] = useState<VirtualCityPredictionReport | null>(null);
  const [reportRunId, setReportRunId] = useState('');
  const [reportErr, setReportErr] = useState<string | null>(null);

  const [selectedKind, setSelectedKind] = useState('');
  const [magnitude, setMagnitude] = useState(3);
  const [duration, setDuration] = useState('');
  const [target, setTarget] = useState('');
  const [note, setNote] = useState('');

  const [submitting, setSubmitting] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [submitErr, setSubmitErr] = useState<string | null>(null);
  const [effects, setEffects] = useState<VirtualCityScenarioEffect[] | null>(null);
  const [triggeredName, setTriggeredName] = useState<string | null>(null);

  const selected = useMemo(
    () => (specs ?? []).find((s) => s.kind === selectedKind) ?? null,
    [specs, selectedKind],
  );

  const nameByKind = useMemo(() => {
    const map: Record<string, string> = {};
    for (const s of specs ?? []) map[s.kind] = s.name;
    return map;
  }, [specs]);

  // 状态行取「最新」run = 观测推进最远者（start_month + months_observed 最大）。
  const latestRun = useMemo(() => {
    if (!predRuns || predRuns.length === 0) return null;
    return predRuns.reduce((a, b) =>
      b.start_month + b.months_observed >= a.start_month + a.months_observed ? b : a,
    );
  }, [predRuns]);

  // 分组（保持后端出现的分组语义；已知分组按固定顺序，未知分组追加）。
  const groups = useMemo(() => {
    const buckets = new Map<string, VirtualCityScenarioSpec[]>();
    for (const s of specs ?? []) {
      const g = s.group ?? '';
      const arr = buckets.get(g) ?? [];
      arr.push(s);
      buckets.set(g, arr);
    }
    const ordered: { group: string; items: VirtualCityScenarioSpec[] }[] = [];
    for (const g of GROUP_ORDER) {
      const items = buckets.get(g);
      if (items && items.length > 0) {
        ordered.push({ group: g, items });
        buckets.delete(g);
      }
    }
    for (const [group, items] of buckets) ordered.push({ group, items });
    return ordered;
  }, [specs]);

  // 目录：挂载 / 换房 / 切语言时拉取。
  useEffect(() => {
    let alive = true;
    setSpecs(null);
    setCatalogErr(null);
    fetchScenarioCatalog()
      .then((r) => {
        if (!alive) return;
        const list = r?.scenarios ?? [];
        setSpecs(list);
        setSelectedKind((prev) =>
          prev && list.some((s) => s.kind === prev) ? prev : (list[0]?.kind ?? ''),
        );
      })
      .catch((e) => {
        if (!alive || isSessionExpiredError(e)) return;
        const msg = t('virtualCity.scenario.loadFailed' as TKey, {
          msg: errorMessage(e, String(e)),
        });
        setCatalogErr(msg);
        setSpecs([]);
        // §7.1 双通道：内联红条（上文）+ 全局 toast（此处）。
        reportGlobalError(e);
      });
    return () => {
      alive = false;
    };
  }, [roomId, t]);

  // 当前选中场景变化（含目录首次到达 / 换场景）→ 强度回到该场景默认档。
  useEffect(() => {
    const spec = (specs ?? []).find((s) => s.kind === selectedKind);
    if (spec) setMagnitude(spec.default_magnitude || 1);
  }, [selectedKind, specs]);

  // 最近注入记录：挂载即拉，注入成功后再刷新。
  useEffect(() => {
    let alive = true;
    setHistory(null);
    setHistoryErr(null);
    fetchVirtualCityScenarios(roomId)
      .then((r) => {
        if (alive) setHistory(r?.scenarios ?? []);
      })
      .catch((e) => {
        if (!alive || isSessionExpiredError(e)) return;
        setHistoryErr(
          t('virtualCity.scenario.historyFailed' as TKey, {
            msg: errorMessage(e, String(e)),
          }),
        );
        reportGlobalError(e);
      });
    return () => {
      alive = false;
    };
  }, [roomId, t]);

  const refreshHistory = useCallback(async () => {
    try {
      const r = await fetchVirtualCityScenarios(roomId);
      setHistory(r?.scenarios ?? []);
      setHistoryErr(null);
    } catch (e) {
      if (isSessionExpiredError(e)) return;
      setHistoryErr(
        t('virtualCity.scenario.historyFailed' as TKey, {
          msg: errorMessage(e, String(e)),
        }),
      );
      reportGlobalError(e);
    }
  }, [roomId, t]);

  // 预测运行状态（runs 列表）：挂载 / 换房 / 注入成功后刷新。无 run 不报错
  // （runs:[] 且 report:null → 状态行展示引导文案）。
  const refreshPredictionState = useCallback(async () => {
    try {
      const r = await getPredictionReport(roomId);
      setPredRuns(r?.runs ?? []);
      setPredStateErr(null);
    } catch (e) {
      if (isSessionExpiredError(e)) return;
      setPredStateErr(
        t('virtualCity.prediction.stateFailed' as TKey, {
          msg: errorMessage(e, String(e)),
        }),
      );
      setPredRuns([]);
      reportGlobalError(e);
    }
  }, [roomId, t]);

  useEffect(() => {
    let alive = true;
    setPredRuns(null);
    setPredStateErr(null);
    setReport(null);
    setReportErr(null);
    setSelectedRunId('');
    getPredictionReport(roomId)
      .then((r) => {
        if (alive) setPredRuns(r?.runs ?? []);
      })
      .catch((e) => {
        if (!alive || isSessionExpiredError(e)) return;
        setPredStateErr(
          t('virtualCity.prediction.stateFailed' as TKey, {
            msg: errorMessage(e, String(e)),
          }),
        );
        setPredRuns([]);
        reportGlobalError(e);
      });
    return () => {
      alive = false;
    };
  }, [roomId, t]);

  // 「生成预测报告」：GET（runId 缺省 = 最新）→ 就地渲染，失败 §7.1 双通道。
  const generateReport = useCallback(async () => {
    setReportLoading(true);
    setReportErr(null);
    try {
      const r = await getPredictionReport(roomId, selectedRunId || undefined);
      setPredRuns(r?.runs ?? []);
      setReport(r?.report ?? null);
      setReportRunId(selectedRunId);
      if (!r?.report) {
        setReportErr(t('virtualCity.prediction.noRun' as TKey));
      }
    } catch (e) {
      if (isSessionExpiredError(e)) return;
      const msg = t('virtualCity.prediction.reportFailed' as TKey, {
        msg: errorMessage(e, String(e)),
      });
      setReportErr(msg);
      reportGlobalError(e);
    } finally {
      setReportLoading(false);
    }
  }, [roomId, selectedRunId, t]);

  const selectSpec = (spec: VirtualCityScenarioSpec) => {
    setSelectedKind(spec.kind);
    const mags = spec.magnitudes?.length ? spec.magnitudes : [1, 2, 3, 4, 5];
    const def = spec.default_magnitude;
    setMagnitude(mags.includes(def) ? def : mags[Math.floor(mags.length / 2)]);
    setSubmitErr(null);
  };

  const submit = useCallback(async () => {
    if (!selected) return;
    setConfirmOpen(false);
    setSubmitting(true);
    setSubmitErr(null);
    setTriggeredName(null);
    try {
      const body: VirtualCityScenarioApplyRequest = {
        kind: selected.kind,
        magnitude,
      };
      const dur = Number.parseInt(duration, 10);
      if (Number.isFinite(dur) && dur > 0) body.duration_months = dur;
      const tg = target.trim();
      if (tg) body.target = tg;
      const nt = note.trim();
      if (nt) body.note = nt;

      const r = await applyVirtualCityScenario(roomId, body);
      setEffects(r?.effects ?? []);
      setTriggeredName(selected.name);
      setNote('');
      setTarget('');
      setDuration('');
      void refreshHistory();
      // 注入会开启/并入预测 run（§3 生命周期），同步刷新状态行。
      void refreshPredictionState();
    } catch (e) {
      if (isSessionExpiredError(e)) return;
      const msg = t('virtualCity.scenario.applyFailed' as TKey, {
        msg: errorMessage(e, String(e)),
      });
      setSubmitErr(msg);
      reportGlobalError(e);
    } finally {
      setSubmitting(false);
    }
  }, [selected, magnitude, duration, target, note, roomId, t, refreshHistory, refreshPredictionState]);

  const magnitudes = useMemo(() => {
    const list = selected?.magnitudes?.length ? selected.magnitudes : [1, 2, 3, 4, 5];
    return list;
  }, [selected]);

  // params 决定 target 语义（goods / stance）；两者都不含则不展示目标输入。
  const targetMode: 'goods' | 'stance' | null = selected?.params?.includes('goods')
    ? 'goods'
    : selected?.params?.includes('stance')
      ? 'stance'
      : null;
  const targetAllowed = targetMode !== null;
  const targetOptions = targetMode === 'goods' ? GOODS_TARGETS : STANCE_TARGETS;
  const targetPlaceholder = targetMode === 'goods'
    ? t('virtualCity.scenario.targetHintGoods' as TKey)
    : targetMode === 'stance'
      ? t('virtualCity.scenario.targetHintStance' as TKey)
      : t('virtualCity.scenario.targetPlaceholder' as TKey);

  const catalogLoading = specs === null && !catalogErr;

  return (
    <div className="virtualCity-scenario" data-testid="virtualCity-scenario-panel">
      <div className="virtualCity-scenario__head">
        <span className="virtualCity-scenario__title">
          🎛 {t('virtualCity.scenario.title' as TKey)}
        </span>
        <span className="virtualCity-scenario__subtitle">
          {t('virtualCity.scenario.subtitle' as TKey)}
        </span>
      </div>

      {catalogLoading && (
        <p className="virtualCity-scenario__hint">{t('virtualCity.scenario.loading' as TKey)}</p>
      )}

      {/* 目录加载失败（§7.1 内联层） */}
      {catalogErr && (
        <div className="virtualCity-scenario__error" role="alert">
          {catalogErr}
        </div>
      )}

      {specs !== null && specs.length === 0 && !catalogErr && (
        <p className="virtualCity-scenario__hint virtualCity-scenario__hint--dim">
          {t('virtualCity.scenario.catalogEmpty' as TKey)}
        </p>
      )}

      {groups.length > 0 && (
        <div className="virtualCity-scenario__groups">
          {groups.map((g) => (
            <div className="virtualCity-scenario__group" key={g.group || '_other'}>
              <div className="virtualCity-scenario__group-title">
                {GROUP_KEY[g.group]
                  ? t(GROUP_KEY[g.group])
                  : (g.group || t('virtualCity.scenario.group.other' as TKey))}
              </div>
              <div className="virtualCity-scenario__items">
                {g.items.map((s) => {
                  const active = s.kind === selectedKind;
                  return (
                    <button
                      type="button"
                      key={s.kind}
                      className={
                        'virtualCity-scenario__item'
                        + (active ? ' virtualCity-scenario__item--active' : '')
                      }
                      onClick={() => selectSpec(s)}
                      aria-pressed={active}
                      disabled={submitting}
                      data-testid={`virtualCity-scenario-item-${s.kind}`}
                    >
                      <span className="virtualCity-scenario__item-name">{s.name}</span>
                      {s.description && (
                        <span className="virtualCity-scenario__item-desc">{s.description}</span>
                      )}
                    </button>
                  );
                })}
              </div>
            </div>
          ))}
        </div>
      )}

      {selected && (
        <div className="virtualCity-scenario__form">
          <div className="virtualCity-scenario__field">
            <span className="virtualCity-scenario__field-label">
              {t('virtualCity.scenario.magnitude' as TKey)}
            </span>
            <div
              className="virtualCity-scenario__magnitudes"
              role="group"
              aria-label={t('virtualCity.scenario.magnitude' as TKey)}
            >
              {magnitudes.map((m) => (
                <button
                  type="button"
                  key={m}
                  className={
                    'virtualCity-scenario__mag'
                    + (m === magnitude ? ' virtualCity-scenario__mag--active' : '')
                  }
                  onClick={() => setMagnitude(m)}
                  aria-pressed={m === magnitude}
                  disabled={submitting}
                  data-testid={`virtualCity-scenario-magnitude-${m}`}
                >
                  {m}
                </button>
              ))}
            </div>
          </div>

          <div className="virtualCity-scenario__grid">
            <label className="virtualCity-scenario__field">
              <span className="virtualCity-scenario__field-label">
                {t('virtualCity.scenario.duration' as TKey)}
              </span>
              <input
                type="number"
                className="virtualCity-scenario__input"
                min={1}
                max={24}
                value={duration}
                onChange={(e) => setDuration(e.target.value)}
                disabled={submitting}
                placeholder={t('virtualCity.scenario.durationPlaceholder' as TKey)}
                data-testid="virtualCity-scenario-duration"
              />
            </label>
            {targetAllowed && (
              <label className="virtualCity-scenario__field">
                <span className="virtualCity-scenario__field-label">
                  {t('virtualCity.scenario.target' as TKey)}
                </span>
                <input
                  type="text"
                  className="virtualCity-scenario__input"
                  maxLength={40}
                  list={TARGET_OPTIONS_ID}
                  value={target}
                  onChange={(e) => setTarget(e.target.value)}
                  disabled={submitting}
                  placeholder={targetPlaceholder}
                  data-testid="virtualCity-scenario-target"
                />
                <datalist id={TARGET_OPTIONS_ID}>
                  {targetOptions.map((v) => (
                    <option key={v} value={v} />
                  ))}
                </datalist>
              </label>
            )}
          </div>

          <label className="virtualCity-scenario__field">
            <span className="virtualCity-scenario__field-label">
              {t('virtualCity.scenario.note' as TKey)}
            </span>
            <input
              type="text"
              className="virtualCity-scenario__input"
              maxLength={60}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              disabled={submitting}
              placeholder={t('virtualCity.scenario.notePlaceholder' as TKey)}
              data-testid="virtualCity-scenario-note"
            />
          </label>

          <div className="virtualCity-scenario__actions">
            <button
              type="button"
              className="btn btn-primary virtualCity-scenario__submit"
              onClick={() => setConfirmOpen(true)}
              disabled={submitting || !playing}
              data-testid="virtualCity-scenario-trigger"
            >
              {submitting
                ? t('common.loading')
                : `🎛 ${t('virtualCity.scenario.trigger' as TKey)}`}
            </button>
          </div>

          {!playing && (
            <p className="virtualCity-scenario__hint">
              {t('virtualCity.scenario.notPlaying' as TKey)}
            </p>
          )}
          <p className="virtualCity-scenario__hint virtualCity-scenario__hint--dim">
            {t('virtualCity.scenario.spectatorHint' as TKey)}
          </p>

          {/* 注入失败（§7.1 内联层；全局 toast 已由 catch 上报） */}
          {submitErr && (
            <div className="virtualCity-scenario__error" role="alert">
              {submitErr}
            </div>
          )}
        </div>
      )}

      {/* 注入结果：指标效果（就地展示，不关面板） */}
      {effects && (
        <div className="virtualCity-scenario__effects" data-testid="virtualCity-scenario-effects">
          <div className="virtualCity-scenario__section-title">
            {t('virtualCity.scenario.effects' as TKey)}
            {triggeredName
              ? ` · ${t('virtualCity.scenario.triggered' as TKey, { name: triggeredName })}`
              : ''}
          </div>
          <p className="virtualCity-scenario__hint virtualCity-scenario__hint--dim">
            {t('virtualCity.scenario.effectsNote' as TKey)}
          </p>
          {effects.length === 0 ? (
            <p className="virtualCity-scenario__hint">
              {t('virtualCity.scenario.effectsEmpty' as TKey)}
            </p>
          ) : (
            <ul className="virtualCity-scenario__effect-list">
              {effects.map((ef, i) => (
                <li className="virtualCity-scenario__effect" key={`${ef.metric}-${i}`}>
                  <span className="virtualCity-scenario__effect-metric">{ef.metric}</span>
                  <span
                    className={
                      'virtualCity-scenario__effect-delta'
                      + (ef.delta > 0
                        ? ' virtualCity-scenario__effect-delta--up'
                        : ef.delta < 0
                          ? ' virtualCity-scenario__effect-delta--down'
                          : '')
                    }
                  >
                    {fmtDelta(ef.delta)}
                  </span>
                  <span className="virtualCity-scenario__effect-text">{ef.text}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {/* 批次 68 · 预测运行：状态行 + 生成报告 + 报告视图 */}
      <div className="virtualCity-prediction" data-testid="virtualCity-prediction">
        <div className="virtualCity-scenario__section-title">
          {t('virtualCity.prediction.title' as TKey)}
        </div>
        <p className="virtualCity-scenario__hint virtualCity-scenario__hint--dim">
          {t('virtualCity.prediction.subtitle' as TKey)}
        </p>

        {predStateErr && (
          <div className="virtualCity-scenario__error" role="alert">
            {predStateErr}
          </div>
        )}

        {predRuns === null && !predStateErr && (
          <p className="virtualCity-scenario__hint">{t('virtualCity.scenario.loading' as TKey)}</p>
        )}

        {predRuns !== null && predRuns.length === 0 && (
          <p className="virtualCity-scenario__hint" data-testid="virtualCity-prediction-empty">
            {t('virtualCity.prediction.statusEmpty' as TKey)}
          </p>
        )}

        {latestRun && (
          <p className="virtualCity-prediction__status" data-testid="virtualCity-prediction-status">
            {t('virtualCity.prediction.statusActive' as TKey, {
              run: latestRun.id,
              month: latestRun.start_month + latestRun.months_observed,
              n: latestRun.injections?.length ?? 0,
            })}
          </p>
        )}

        {predRuns !== null && predRuns.length > 1 && (
          <label className="virtualCity-scenario__field">
            <span className="virtualCity-scenario__field-label">
              {t('virtualCity.prediction.runLabel' as TKey)}
            </span>
            <select
              className="virtualCity-scenario__input"
              value={selectedRunId}
              onChange={(e) => setSelectedRunId(e.target.value)}
              disabled={reportLoading}
              data-testid="virtualCity-prediction-run-select"
            >
              <option value="">{t('virtualCity.prediction.runLatest' as TKey)}</option>
              {predRuns.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.id}
                </option>
              ))}
            </select>
          </label>
        )}

        <div className="virtualCity-scenario__actions">
          <button
            type="button"
            className="btn btn-primary virtualCity-scenario__submit"
            onClick={() => void generateReport()}
            disabled={reportLoading || predRuns === null || predRuns.length === 0}
            data-testid="virtualCity-prediction-generate"
          >
            {reportLoading
              ? t('virtualCity.prediction.generating' as TKey)
              : t('virtualCity.prediction.generate' as TKey)}
          </button>
        </div>

        {/* 生成失败（§7.1 内联层；全局 toast 已由 catch 上报） */}
        {reportErr && (
          <div className="virtualCity-scenario__error" role="alert">
            {reportErr}
          </div>
        )}

        {report && (
          <>
            {reportRunId && (
              <p className="virtualCity-prediction__note virtualCity-prediction__note--dim">
                {t('virtualCity.prediction.reportOf' as TKey, { run: report.meta?.run_id ?? reportRunId })}
              </p>
            )}
            <PredictionReportView report={report} />
          </>
        )}
      </div>

      {/* 最近场景事件 */}
      <div className="virtualCity-scenario__history">
        <div className="virtualCity-scenario__section-title">
          {t('virtualCity.scenario.history' as TKey)}
        </div>
        {historyErr && (
          <div className="virtualCity-scenario__error" role="alert">
            {historyErr}
          </div>
        )}
        {history === null && !historyErr && (
          <p className="virtualCity-scenario__hint">
            {t('virtualCity.scenario.loading' as TKey)}
          </p>
        )}
        {history !== null && history.length === 0 && (
          <p className="virtualCity-scenario__hint virtualCity-scenario__hint--dim">
            {t('virtualCity.scenario.historyEmpty' as TKey)}
          </p>
        )}
        {history !== null && history.length > 0 && (
          <ul className="virtualCity-scenario__history-list">
            {/* 后端 ListScenarios 为「旧→新」；展示层反转成最近事件在前。 */}
            {history.slice().reverse().map((rec) => (
              <li className="virtualCity-scenario__history-item" key={rec.id}>
                <span className="virtualCity-scenario__history-kind">
                  {nameByKind[rec.kind] ?? rec.kind}
                </span>
                <span className="virtualCity-scenario__history-mag">
                  {t('virtualCity.scenario.magnitudeLevel' as TKey, { n: rec.magnitude })}
                </span>
                <span className="virtualCity-scenario__history-month">
                  {t('virtualCity.scenario.appliedAt' as TKey, { m: rec.applied_month })}
                </span>
                {rec.at && (
                  <span className="virtualCity-scenario__history-at">{fmtWhen(rec.at)}</span>
                )}
                {rec.note && (
                  <span className="virtualCity-scenario__history-note">{rec.note}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>

      {confirmOpen && selected && (
        <ConfirmModal
          message={t('virtualCity.scenario.confirmBody' as TKey, {
            name: selected.name,
            level: magnitude,
          })}
          confirmKey={'virtualCity.scenario.trigger' as TKey}
          danger
          onConfirm={() => void submit()}
          onCancel={() => setConfirmOpen(false)}
        />
      )}
    </div>
  );
}

export default ScenarioPanel;
