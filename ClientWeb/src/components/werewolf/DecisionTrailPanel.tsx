/**
 * DecisionTrailPanel — 决策留痕运行时回放(spectator only)
 *
 * 2026-08-11 §20260811-02 U2 — 接线修复。
 * 设计文档:`docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-02.md` §U2。
 *
 * 背景:后端 §20260810-12 D1 起就在 `agent.go:1309` 累积 `DecisionTrail`(30 条 FIFO),
 * 经 `bot_contexts[].decision_trail` 下发给观战者,且 `decision_trail.go:5` 注释声称
 * 「HistoryDrawer 第 6 sub-tab「🧠 决策」」—— 但该 sub-tab 从未存在。
 * 本组件即该字段的**首个消费者**(§130 第 N 次复现的修复)。
 *
 * §135:整个 tab 仅 spectator 可见 —— 后端 `room_state.go:557` 已在人类玩家分支
 * 清空该字段,前端过滤是纵深防御的第二道。
 * §128 对话即思考:trail 只存结构化步骤(轮/阶段/工具/耗时/一句话),不含 LLM CoT。
 */
import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import { phaseLabel } from '@/components/werewolf/phaseLabel';
import type { WerewolfGameState, DecisionEntryJSON } from '@/types/werewolf';

/** 一条 trail 记录 + 它属于哪个座位(扁平化后便于按时间统一排序)。 */
interface FlatEntry extends DecisionEntryJSON {
  seat: number;
}

interface DecisionTrailPanelProps {
  gameState: WerewolfGameState;
}

function DecisionTrailPanel({ gameState }: DecisionTrailPanelProps) {
  const t = useT();
  const [seatFilter, setSeatFilter] = useState<number>(-1);

  // 把每个 bot 的 trail 扁平化并按时间升序 —— 复盘时看的是「全场决策流」,
  // 而不是「一个 bot 说了什么」;按座位过滤时才退化为单 bot 视图。
  const all = useMemo<FlatEntry[]>(() => {
    const out: FlatEntry[] = [];
    for (const ctx of gameState.bot_contexts ?? []) {
      for (const e of ctx.decision_trail ?? []) {
        out.push({ ...e, seat: ctx.seat });
      }
    }
    return out.sort((a, b) => a.created_at - b.created_at);
  }, [gameState.bot_contexts]);

  const seats = useMemo(() => {
    const s = new Set<number>();
    for (const e of all) s.add(e.seat);
    return [...s].sort((a, b) => a - b);
  }, [all]);

  const visible = useMemo(
    () => (seatFilter < 0 ? all : all.filter((e) => e.seat === seatFilter)),
    [all, seatFilter],
  );

  if (all.length === 0) {
    return (
      <div className="ww-decision-panel__empty">
        {t('werewolf.history.decision.empty')}
      </div>
    );
  }

  return (
    <div className="ww-decision-panel" data-testid="ww-decision-panel">
      <div className="ww-decision-panel__filter">
        <button
          type="button"
          className={`ww-decision-panel__chip${seatFilter < 0 ? ' is-active' : ''}`}
          onClick={() => setSeatFilter(-1)}
        >
          {t('werewolf.history.seatFilterAll')}
        </button>
        {seats.map((s) => (
          <button
            key={s}
            type="button"
            className={`ww-decision-panel__chip${seatFilter === s ? ' is-active' : ''}`}
            onClick={() => setSeatFilter(s)}
          >
            #{s + 1}
          </button>
        ))}
      </div>

      <ol className="ww-decision-panel__list">
        {visible.map((e, i) => (
          <li key={`${e.seat}-${e.created_at}-${i}`} className="ww-decision-panel__item">
            <div className="ww-decision-panel__head">
              <span className="ww-decision-panel__seat">#{e.seat + 1}</span>
              <span className="ww-decision-panel__round">D{e.round}</span>
              <span className="ww-decision-panel__phase">{phaseLabel(t, e.phase)}</span>
              <span className="ww-decision-panel__tool">{e.tool_name}</span>
              {e.took_ms > 0 && (
                <span className="ww-decision-panel__took">
                  {t('werewolf.history.decision.took')} {(e.took_ms / 1000).toFixed(1)}s
                </span>
              )}
            </div>
            {e.tool_summary && (
              <div className="ww-decision-panel__summary">{e.tool_summary}</div>
            )}
          </li>
        ))}
      </ol>
    </div>
  );
}

export default DecisionTrailPanel;
