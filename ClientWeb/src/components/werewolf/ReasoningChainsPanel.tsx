/**
 * ReasoningChainsPanel — 公开推理链渲染(spectator only)
 *
 * 2026-08-11 §20260811-06 U3 — reasoning_chain 工具首个前端消费者。
 * 设计文档:`docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-06.md` §U3。
 *
 * 背景:LLM 在关键决策(speak / vote / night_action)前可显性调用
 * reasoning_chain 工具,公开自己的推理链(steps / evidence / conclusion /
 * confidence)。BotTranscript.ReasoningChains 字段(上限 10 条 FIFO)经
 * `bot_contexts[].reasoning_chains` 下发给观战者。
 *
 * §135:整个 tab 仅 spectator 可见 —— 后端 `room_state.go::sanitizeBotTranscript`
 * 已在人类玩家分支清空该字段,前端过滤是纵深防御的第二道。
 * §128 对话即思考:ReasoningChain 是 LLM 显性公开的结构化推理,不是内部独白。
 *
 * 数据来源:`gameState.bot_contexts[].reasoning_chains: ReasoningChainEntryJSON[]`。
 * 渲染:按座位筛选 + 按时间倒序 + 卡片化 steps/evidence/conclusion/confidence。
 */
import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import { phaseLabel } from '@/components/werewolf/phaseLabel';
import type { WerewolfGameState, ReasoningChainEntryJSON } from '@/types/werewolf';

/** 一条推理链 + 它属于哪个座位(扁平化后便于按时间统一排序)。 */
interface FlatChain extends ReasoningChainEntryJSON {
  seat: number;
}

interface ReasoningChainsPanelProps {
  gameState: WerewolfGameState;
}

export function ReasoningChainsPanel({ gameState }: ReasoningChainsPanelProps) {
  const t = useT();
  const [seatFilter, setSeatFilter] = useState<number>(-1);

  // 扁平化:把所有 bot 的 reasoning_chains 拼成一个数组,按 created_at 降序。
  const all = useMemo<FlatChain[]>(() => {
    const out: FlatChain[] = [];
    for (const ctx of gameState.bot_contexts ?? []) {
      for (const c of ctx.reasoning_chains ?? []) {
        out.push({ ...c, seat: ctx.seat });
      }
    }
    out.sort((a, b) => (b.created_at ?? 0) - (a.created_at ?? 0));
    return out;
  }, [gameState.bot_contexts]);

  // 收集出现过推理链的座位(用于座位筛选 chip)
  const seatSet = useMemo(() => {
    const s = new Set<number>();
    for (const c of all) s.add(c.seat);
    return Array.from(s).sort((a, b) => a - b);
  }, [all]);

  const filtered = useMemo(
    () => (seatFilter < 0 ? all : all.filter((c) => c.seat === seatFilter)),
    [all, seatFilter],
  );

  if (all.length === 0) {
    return (
      <div className="ww-history-reasoning-empty" data-testid="reasoning-empty">
        🧩 {t('werewolf.history.reasoning.empty')}
      </div>
    );
  }

  return (
    <div className="ww-history-reasoning" data-testid="reasoning-panel">
      {/* 座位过滤 chips */}
      <div className="ww-history-reasoning__filter">
        <button
          type="button"
          className={`chip ${seatFilter === -1 ? 'is-selected' : ''}`}
          onClick={() => setSeatFilter(-1)}
          data-testid="reasoning-filter-all"
        >
          {t('werewolf.history.reasoning.filterAll')}
        </button>
        {seatSet.map((s) => (
          <button
            key={s}
            type="button"
            className={`chip ${seatFilter === s ? 'is-selected' : ''}`}
            onClick={() => setSeatFilter(s)}
            data-testid={`reasoning-filter-seat-${s}`}
          >
            #{s + 1}
          </button>
        ))}
      </div>

      <ul className="ww-history-reasoning__list">
        {filtered.map((c, i) => (
          <li
            key={`${c.seat}-${c.created_at}-${i}`}
            className="ww-history-reasoning__card"
            data-testid={`reasoning-card-${c.seat}-${i}`}
          >
            <div className="ww-history-reasoning__head">
              <span className="ww-history-reasoning__seat">#{c.seat + 1}</span>
              <span className="ww-history-reasoning__topic">🧩 {c.topic || '—'}</span>
              {typeof c.confidence === 'number' && (
                <span
                  className={`ww-history-reasoning__confidence ww-history-reasoning__confidence--${
                    c.confidence >= 70 ? 'high' : c.confidence >= 40 ? 'mid' : 'low'
                  }`}
                >
                  🎯 {c.confidence}%
                </span>
              )}
            </div>
            <div className="ww-history-reasoning__meta">
              {c.round ? `第 ${c.round} 天` : ''}
              {c.phase ? ` · ${phaseLabel(c.phase as any, t as any)}` : ''}
            </div>
            {Array.isArray(c.steps) && c.steps.length > 0 && (
              <ol className="ww-history-reasoning__steps">
                {c.steps.map((s, j) => (
                  <li key={`step-${j}`}>{s}</li>
                ))}
              </ol>
            )}
            {Array.isArray(c.evidence) && c.evidence.length > 0 && (
              <div className="ww-history-reasoning__evidence">
                <div className="ww-history-reasoning__evidence-label">📎 证据</div>
                <ul>
                  {c.evidence.map((e, j) => (
                    <li key={`evi-${j}`}>{e}</li>
                  ))}
                </ul>
              </div>
            )}
            {c.conclusion && (
              <div className="ww-history-reasoning__conclusion">
                <span className="ww-history-reasoning__conclusion-label">→ 结论:</span>
                {c.conclusion}
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

export default ReasoningChainsPanel;
