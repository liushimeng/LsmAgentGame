/**
 * EventLifecyclePanel — 批次 86 看板第 13 维度「事件生命周期」。
 *
 * 定位：把「触发 → 决策 → 执行 → 四阶段结果」的**结构化留痕**变成可查阅、
 * 可回放的时间线（方案 §3.1 / §5）。数据源两路：
 *   ① WS `game.state.event_lifecycle`（种子，首屏即时可见）；
 *   ② HTTP `GET /rooms/:id/event_lifecycle`（权威回读，含 total）。
 *
 * 每条留痕渲染 4 阶段时间线：
 *   ① 触发   —— 来源（manual / agent_tool / realworld_rule / auto_inject / replay / system）
 *              + 触发依据 + 触发时间
 *   ② 决策   —— 逐闸门 ✔/✘（idempotency / magnitude_limit / …）+ 决策结果
 *   ③ 执行   —— 强度 / 持续月数 / 目标 + 效果明细（metric / delta / text）
 *   ④ 结果   —— success / failed / rolled_back + 错误码 + 前后状态哈希 + 关联快照
 *
 * 回放：每条留痕一个「回放」按钮 —— 调 HTTP replay，展示 `state_hash_match`
 *   （回放重放后的状态哈希与原 trace 一致 ⇒ 确定性验证通过）。
 *   ⚠ 无 `snapshot_id` 的留痕不可回放（后端会返回 35066）⇒ 按钮禁用 + 原因。
 *
 * 口径纪律：
 *   - **空态 / 缺失绝不白屏**：后端未升级 ⇒ `traces` 为空数组 ⇒ 「暂无留痕」；
 *   - **未知枚举回落原字符串**（触发来源 / 结果状态 / 闸门名），绝不裸 undefined；
 *   - **§7.1**：加载 / 回放失败一律面板内联红条 + reportGlobalError 上报；
 *   - **观战态只读**：`readOnly` ⇒ 回放按钮禁用（title 明示原因，不用 opacity）。
 */

import { useCallback, useEffect, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  fetchVirtualCityEventLifecycle,
  replayVirtualCityTrace,
} from '@/api/virtualCity';
import { errorMessage, reportGlobalError } from '@/services/globalError';
import type {
  VirtualCityEventLifecycleTrace,
  VirtualCityGameState,
} from '@/types/virtualCity';

/** 触发来源 → i18n 键（未知回落原字符串）。 */
const TRIGGER_SOURCE_KEYS: Record<string, TKey> = {
  manual: 'virtualCity.dash.eventLifecycle.source.manual' as TKey,
  agent_tool: 'virtualCity.dash.eventLifecycle.source.agent_tool' as TKey,
  realworld_rule: 'virtualCity.dash.eventLifecycle.source.realworld_rule' as TKey,
  auto_inject: 'virtualCity.dash.eventLifecycle.source.auto_inject' as TKey,
  replay: 'virtualCity.dash.eventLifecycle.source.replay' as TKey,
  system: 'virtualCity.dash.eventLifecycle.source.system' as TKey,
};

/** 结果状态 → i18n 键（未知 / 空回落「待判定」）。 */
const RESULT_OUTCOME_KEYS: Record<string, TKey> = {
  success: 'virtualCity.dash.eventLifecycle.result.success' as TKey,
  failed: 'virtualCity.dash.eventLifecycle.result.failed' as TKey,
  rolled_back: 'virtualCity.dash.eventLifecycle.result.rolled_back' as TKey,
};

/** 回放结果展示（state_hash_match 三态：true / false / 未回放）。 */
type ReplayState = { match: boolean; at: number } | null;

export interface EventLifecyclePanelProps {
  /** 房间 id。 */
  roomId: string;
  /** game.state 快照（提供 event_lifecycle 种子；null = 对局未就绪）。 */
  gameState: VirtualCityGameState | null;
  /** 只读态（观战 / 服务端视角无本人座位）⇒ 禁用回放。 */
  readOnly: boolean;
}

/** 时间戳 → 本地时间字符串；非法 ⇒ 「—」。 */
function fmtTime(ts: number | undefined | null): string {
  if (typeof ts !== 'number' || !Number.isFinite(ts) || ts <= 0) return '—';
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
}

/** 截断长哈希 / id（保留首尾，便于肉眼比对）。 */
function shortHash(s: string | undefined | null): string {
  if (!s) return '—';
  if (s.length <= 16) return s;
  return `${s.slice(0, 8)}…${s.slice(-6)}`;
}

export function EventLifecyclePanel({
  roomId,
  gameState,
  readOnly,
}: EventLifecyclePanelProps) {
  const t = useT();

  const [traces, setTraces] = useState<VirtualCityEventLifecycleTrace[] | null>(null);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [replayingId, setReplayingId] = useState<string | null>(null);
  const [replayResults, setReplayResults] = useState<Record<string, ReplayState>>({});

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const r = await fetchVirtualCityEventLifecycle(roomId);
      setTraces(Array.isArray(r?.traces) ? r.traces : []);
      setTotal(typeof r?.total === 'number' ? r.total : 0);
    } catch (e) {
      // §7.1：内联红条 + 全局 toast 双通道（会话过期类由全局弹层接管，不重复展示）。
      setError(
        t('virtualCity.dash.eventLifecycle.loadFailed' as TKey, {
          msg: errorMessage(e, 'load failed'),
        }),
      );
      reportGlobalError(e);
    } finally {
      setLoading(false);
    }
  }, [roomId, t]);

  // 首屏 + 切房回读。
  useEffect(() => {
    void load();
  }, [load]);

  // WS 种子：HTTP 未回且 game.state 已带留痕 ⇒ 先用它渲染（避免空态闪烁）。
  useEffect(() => {
    const seed = gameState?.event_lifecycle;
    if (seed && traces === null) setTraces(seed);
  }, [gameState, traces]);

  const handleReplay = useCallback(
    async (traceId: string) => {
      if (replayingId || readOnly) return;
      setReplayingId(traceId);
      setError(null);
      try {
        const r = await replayVirtualCityTrace(roomId, traceId);
        setReplayResults((prev) => ({
          ...prev,
          [traceId]: { match: r?.state_hash_match === true, at: Date.now() },
        }));
        // 回放会产生一条新留痕（TriggerSourceReplay）⇒ 回读列表。
        void load();
      } catch (e) {
        setError(
          t('virtualCity.dash.eventLifecycle.replayFailed' as TKey, {
            msg: errorMessage(e, 'replay failed'),
          }),
        );
        reportGlobalError(e);
      } finally {
        setReplayingId(null);
      }
    },
    [roomId, replayingId, readOnly, t, load],
  );

  // ── 标签解析（未知枚举回落原字符串，绝不裸 undefined）──────────────
  const sourceLabel = (source: string): string => {
    const key = TRIGGER_SOURCE_KEYS[source];
    return key ? t(key) : source || '—';
  };
  const resultLabel = (outcome: string): string => {
    const key = RESULT_OUTCOME_KEYS[outcome];
    return key ? t(key) : (outcome || t('virtualCity.dash.eventLifecycle.result.pending' as TKey));
  };
  const decisionLabel = (outcome: string): string => {
    if (outcome === 'pass') return t('virtualCity.dash.eventLifecycle.passed' as TKey);
    if (outcome === 'reject') return t('virtualCity.dash.eventLifecycle.rejected' as TKey);
    return t('virtualCity.dash.eventLifecycle.result.pending' as TKey);
  };

  const list = traces ?? [];

  return (
    <div
      className="vc-elc"
      data-testid="virtualCity-event-lifecycle"
      data-vc-elc-count={String(list.length)}
      data-vc-elc-loading={loading ? '1' : '0'}
    >
      {/* 工具条：留痕条数 + 刷新 */}
      <div className="vc-elc__toolbar">
        <span className="vc-elc__total">
          {t('virtualCity.dash.eventLifecycle.total' as TKey, { n: total || list.length })}
        </span>
        <button
          type="button"
          className="vc-elc__refresh"
          onClick={() => void load()}
          disabled={loading}
          title={t('virtualCity.dash.eventLifecycle.refresh' as TKey)}
        >
          {loading
            ? t('virtualCity.dash.eventLifecycle.loading' as TKey)
            : t('virtualCity.dash.eventLifecycle.refresh' as TKey)}
        </button>
      </div>

      {/* §7.1：加载 / 回放失败内联红条 */}
      {error && (
        <div className="vc-elc__error" role="alert" data-testid="virtualCity-elc-error">
          <span>⚠️ {error}</span>
          <button type="button" onClick={() => setError(null)} aria-label="dismiss">
            ×
          </button>
        </div>
      )}

      {/* 空态 */}
      {list.length === 0 && !loading && (
        <div className="vc-elc__empty">
          {t('virtualCity.dash.eventLifecycle.empty' as TKey)}
        </div>
      )}

      {/* 留痕时间线列表 */}
      <div className="vc-elc__list">
        {list.map((tr) => {
          const replay = replayResults[tr.trace_id];
          const canReplay = !readOnly && !!tr.snapshot_id;
          const replayTitle = readOnly
            ? t('virtualCity.dash.eventLifecycle.readOnly' as TKey)
            : !tr.snapshot_id
              ? t('virtualCity.dash.eventLifecycle.noSnapshot' as TKey)
              : t('virtualCity.dash.eventLifecycle.replay' as TKey);
          return (
            <article
              className="vc-elc__trace"
              key={tr.trace_id}
              data-testid="virtualCity-elc-trace"
              data-vc-trace-id={tr.trace_id}
              data-vc-result-outcome={tr.result_outcome || ''}
              data-vc-trigger-source={tr.trigger_source || ''}
            >
              {/* 留痕头：场景 + 来源 + 结果 */}
              <header className="vc-elc__trace-head">
                <span className="vc-elc__trace-id" title={tr.trace_id}>
                  {shortHash(tr.trace_id)}
                </span>
                {tr.scenario_kind && (
                  <span className="vc-elc__badge vc-elc__badge--kind">
                    {tr.scenario_kind}
                  </span>
                )}
                <span className="vc-elc__badge vc-elc__badge--source">
                  {sourceLabel(tr.trigger_source)}
                </span>
                <span
                  className={
                    'vc-elc__badge vc-elc__badge--result--' +
                    (tr.result_outcome || 'pending')
                  }
                >
                  {resultLabel(tr.result_outcome)}
                </span>
              </header>

              {/* 4 阶段时间线 */}
              <div className="vc-elc__timeline">
                {/* ① 触发 */}
                <section className="vc-elc__phase">
                  <span className="vc-elc__phase-dot" aria-hidden="true" />
                  <div className="vc-elc__phase-body">
                    <div className="vc-elc__phase-title">
                      {t('virtualCity.dash.eventLifecycle.phase.trigger' as TKey)}
                    </div>
                    <div className="vc-elc__kv">
                      <span className="vc-elc__kv-label">
                        {t('virtualCity.dash.eventLifecycle.triggeredAt' as TKey)}
                      </span>
                      <span className="vc-elc__kv-value">{fmtTime(tr.triggered_at)}</span>
                    </div>
                    {tr.trigger_detail && (
                      <div className="vc-elc__kv">
                        <span className="vc-elc__kv-label">
                          {t('virtualCity.dash.eventLifecycle.detail' as TKey)}
                        </span>
                        <span className="vc-elc__kv-value">{tr.trigger_detail}</span>
                      </div>
                    )}
                  </div>
                </section>

                {/* ② 决策 */}
                <section className="vc-elc__phase">
                  <span className="vc-elc__phase-dot" aria-hidden="true" />
                  <div className="vc-elc__phase-body">
                    <div className="vc-elc__phase-title">
                      {t('virtualCity.dash.eventLifecycle.phase.decision' as TKey)}
                    </div>
                    {tr.decision_gates?.length ? (
                      <ul className="vc-elc__gates">
                        {tr.decision_gates.map((g, i) => (
                          <li className="vc-elc__gate" key={`${g.gate}-${i}`}>
                            <span
                              className={
                                'vc-elc__gate-icon ' +
                                (g.passed ? 'vc-elc__gate-icon--pass' : 'vc-elc__gate-icon--fail')
                              }
                              aria-hidden="true"
                            >
                              {g.passed ? '✔' : '✘'}
                            </span>
                            <span className="vc-elc__gate-name">{g.gate || '—'}</span>
                            <span className="vc-elc__gate-result">
                              {g.passed
                                ? t('virtualCity.dash.eventLifecycle.passed' as TKey)
                                : t('virtualCity.dash.eventLifecycle.rejected' as TKey)}
                            </span>
                            {g.detail && (
                              <span className="vc-elc__gate-detail">{g.detail}</span>
                            )}
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <div className="vc-elc__none">—</div>
                    )}
                    <div className="vc-elc__kv">
                      <span className="vc-elc__kv-label">
                        {t('virtualCity.dash.eventLifecycle.outcome' as TKey)}
                      </span>
                      <span className="vc-elc__kv-value">
                        {decisionLabel(tr.decision_outcome)}
                      </span>
                    </div>
                  </div>
                </section>

                {/* ③ 执行 */}
                <section className="vc-elc__phase">
                  <span className="vc-elc__phase-dot" aria-hidden="true" />
                  <div className="vc-elc__phase-body">
                    <div className="vc-elc__phase-title">
                      {t('virtualCity.dash.eventLifecycle.phase.execution' as TKey)}
                    </div>
                    <div className="vc-elc__kv">
                      <span className="vc-elc__kv-label">
                        {t('virtualCity.dash.eventLifecycle.magnitude' as TKey)}
                      </span>
                      <span className="vc-elc__kv-value">
                        {Number.isFinite(tr.execution_magnitude)
                          ? String(tr.execution_magnitude)
                          : '—'}
                      </span>
                    </div>
                    <div className="vc-elc__kv">
                      <span className="vc-elc__kv-label">
                        {t('virtualCity.dash.eventLifecycle.duration' as TKey)}
                      </span>
                      <span className="vc-elc__kv-value">
                        {Number.isFinite(tr.execution_duration)
                          ? String(tr.execution_duration)
                          : '—'}
                      </span>
                    </div>
                    {tr.execution_target && (
                      <div className="vc-elc__kv">
                        <span className="vc-elc__kv-label">
                          {t('virtualCity.dash.eventLifecycle.target' as TKey)}
                        </span>
                        <span className="vc-elc__kv-value">{tr.execution_target}</span>
                      </div>
                    )}
                    {tr.execution_effects?.length ? (
                      <ul className="vc-elc__effects">
                        {tr.execution_effects.map((ef, i) => (
                          <li className="vc-elc__effect" key={`${ef.metric}-${i}`}>
                            <span className="vc-elc__effect-metric">
                              {ef.metric || t('virtualCity.dash.eventLifecycle.metric' as TKey)}
                            </span>
                            <span
                              className={
                                'vc-elc__effect-delta ' +
                                (ef.delta > 0
                                  ? 'vc-elc__effect-delta--pos'
                                  : ef.delta < 0
                                    ? 'vc-elc__effect-delta--neg'
                                    : '')
                              }
                            >
                              {Number.isFinite(ef.delta) ? ef.delta.toFixed(2) : '—'}
                            </span>
                            {ef.text && (
                              <span className="vc-elc__effect-text">{ef.text}</span>
                            )}
                          </li>
                        ))}
                      </ul>
                    ) : null}
                  </div>
                </section>

                {/* ④ 结果 */}
                <section className="vc-elc__phase">
                  <span className="vc-elc__phase-dot" aria-hidden="true" />
                  <div className="vc-elc__phase-body">
                    <div className="vc-elc__phase-title">
                      {t('virtualCity.dash.eventLifecycle.phase.result' as TKey)}
                    </div>
                    <div className="vc-elc__kv">
                      <span className="vc-elc__kv-label">
                        {t('virtualCity.dash.eventLifecycle.resultOutcome' as TKey)}
                      </span>
                      <span className="vc-elc__kv-value">{resultLabel(tr.result_outcome)}</span>
                    </div>
                    {tr.result_err_code !== 0 && (
                      <div className="vc-elc__kv">
                        <span className="vc-elc__kv-label">
                          {t('virtualCity.dash.eventLifecycle.errCode' as TKey)}
                        </span>
                        <span className="vc-elc__kv-value">{tr.result_err_code}</span>
                      </div>
                    )}
                    {tr.result_err_msg && (
                      <div className="vc-elc__kv">
                        <span className="vc-elc__kv-label">
                          {t('virtualCity.dash.eventLifecycle.errMsg' as TKey)}
                        </span>
                        <span className="vc-elc__kv-value">{tr.result_err_msg}</span>
                      </div>
                    )}
                    <div className="vc-elc__kv">
                      <span className="vc-elc__kv-label">
                        {t('virtualCity.dash.eventLifecycle.stateHash' as TKey)}
                      </span>
                      <span className="vc-elc__kv-value vc-elc__hash">
                        {shortHash(tr.state_hash_before)} → {shortHash(tr.state_hash_after)}
                      </span>
                    </div>
                    {tr.snapshot_id && (
                      <div className="vc-elc__kv">
                        <span className="vc-elc__kv-label">
                          {t('virtualCity.dash.eventLifecycle.snapshot' as TKey)}
                        </span>
                        <span className="vc-elc__kv-value vc-elc__hash" title={tr.snapshot_id}>
                          {shortHash(tr.snapshot_id)}
                        </span>
                      </div>
                    )}
                  </div>
                </section>
              </div>

              {/* 回放操作 */}
              <footer className="vc-elc__trace-foot">
                <button
                  type="button"
                  className="vc-elc__replay"
                  onClick={() => void handleReplay(tr.trace_id)}
                  disabled={!canReplay || replayingId === tr.trace_id}
                  title={replayTitle}
                  data-testid="virtualCity-elc-replay"
                  data-vc-trace-id={tr.trace_id}
                >
                  {replayingId === tr.trace_id
                    ? t('virtualCity.dash.eventLifecycle.replaying' as TKey)
                    : t('virtualCity.dash.eventLifecycle.replay' as TKey)}
                </button>
                {replay && (
                  <span
                    className={
                      'vc-elc__replay-result ' +
                      (replay.match
                        ? 'vc-elc__replay-result--match'
                        : 'vc-elc__replay-result--mismatch')
                    }
                    data-testid="virtualCity-elc-replay-result"
                    data-vc-hash-match={replay.match ? '1' : '0'}
                  >
                    {replay.match
                      ? t('virtualCity.dash.eventLifecycle.hashMatch' as TKey)
                      : t('virtualCity.dash.eventLifecycle.hashMismatch' as TKey)}
                  </span>
                )}
              </footer>
            </article>
          );
        })}
      </div>
    </div>
  );
}

export default EventLifecyclePanel;
