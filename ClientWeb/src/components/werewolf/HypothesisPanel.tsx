/**
 * HypothesisPanel — 多假说并行推演观战视图(spectator only)
 *
 * 2026-08-11 §20260811-02 U2 — 接线修复。
 * 设计文档:`docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-02.md` §U2。
 *
 * 背景:后端 §20260810-07 起就在 `view.go:1066` 向观战者下发 `bot_hypotheses`,
 * 且注释声称「HistoryDrawer 第 5 sub-tab「🔮 假说」渲染折线图」——
 * 但该 sub-tab 从未存在,字段在每次 spectator 广播中白白消耗 wire 字节。
 * 本组件即该字段的**首个消费者**(§130 第 N 次复现的修复)。
 *
 * 渲染形态改为**条形图**而非后端注释说的折线图:`HypothesisTable` 只存最新一份
 * 快照(`hypothesis_tracker.go:97` GetLocked 返回单表),没有历史序列,画不出折线。
 * —— 以实际数据形状为准,不迁就注释里的愿望。
 *
 * §135:整个 tab 仅 spectator 可见(后端 viewer>=0 已 omitempty,前端过滤是第二道防线)。
 * §26.4:置信度条走既有色相库(红 ≥70 / 黄 40~69 / 灰 <40),不新创色相。
 */
import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { WerewolfGameState, BotHypothesisJSON } from '@/types/werewolf';

/** 置信度分档 → CSS 类后缀。§26.4 色相库:高=红(威胁)/中=黄(存疑)/低=灰(存在感弱)。 */
function confidenceTier(c: number): 'high' | 'mid' | 'low' {
  if (c >= 70) return 'high';
  if (c >= 40) return 'mid';
  return 'low';
}

interface HypothesisPanelProps {
  gameState: WerewolfGameState;
}

function HypothesisPanel({ gameState }: HypothesisPanelProps) {
  const t = useT();
  const tables: BotHypothesisJSON[] = gameState.bot_hypotheses ?? [];
  // -1 = 全部座位
  const [seatFilter, setSeatFilter] = useState<number>(-1);

  // 有假说记录的座位(升序),用于渲染 chip 过滤条。
  const seats = useMemo(
    () => tables.map((tb) => tb.seat).sort((a, b) => a - b),
    [tables],
  );

  const visible = useMemo(
    () => (seatFilter < 0 ? tables : tables.filter((tb) => tb.seat === seatFilter)),
    [tables, seatFilter],
  );

  if (tables.length === 0) {
    return (
      <div className="ww-hypothesis-panel__empty">
        {t('werewolf.history.hypothesis.empty')}
      </div>
    );
  }

  return (
    <div className="ww-hypothesis-panel" data-testid="ww-hypothesis-panel">
      {/* 座位 chip 过滤条 */}
      <div className="ww-hypothesis-panel__filter">
        <button
          type="button"
          className={`ww-hypothesis-panel__chip${seatFilter < 0 ? ' is-active' : ''}`}
          onClick={() => setSeatFilter(-1)}
        >
          {t('werewolf.history.seatFilterAll')}
        </button>
        {seats.map((s) => (
          <button
            key={s}
            type="button"
            className={`ww-hypothesis-panel__chip${seatFilter === s ? ' is-active' : ''}`}
            onClick={() => setSeatFilter(s)}
          >
            #{s + 1}
          </button>
        ))}
      </div>

      {visible.map((tb) => (
        <section key={tb.seat} className="ww-hypothesis-panel__bot">
          <h5 className="ww-hypothesis-panel__bot-title">
            #{tb.seat + 1}
            <span className="ww-hypothesis-panel__round">
              {t('werewolf.history.hypothesis.round').replace('{round}', String(tb.round))}
            </span>
          </h5>
          <ul className="ww-hypothesis-panel__list">
            {/* 按置信度降序 —— 最笃定的猜测排最前,复盘时一眼看出 Agent 的主判断。 */}
            {[...tb.entries]
              .sort((a, b) => b.confidence - a.confidence)
              .map((e) => (
                <li key={e.target_seat} className="ww-hypothesis-panel__row">
                  <div className="ww-hypothesis-panel__row-head">
                    <span className="ww-hypothesis-panel__target">#{e.target_seat + 1}</span>
                    <span className="ww-hypothesis-panel__guess">{e.role_guess}</span>
                    <span className="ww-hypothesis-panel__conf">{e.confidence}%</span>
                  </div>
                  {/* 置信度条形图 */}
                  <div className="ww-hypothesis-panel__bar-track">
                    <div
                      className={`ww-hypothesis-panel__bar is-${confidenceTier(e.confidence)}`}
                      style={{ width: `${Math.max(0, Math.min(100, e.confidence))}%` }}
                    />
                  </div>
                  {e.supporting && (
                    <div className="ww-hypothesis-panel__evidence is-for">
                      {t('werewolf.history.hypothesis.supporting')}: {e.supporting}
                    </div>
                  )}
                  {e.refuting && (
                    <div className="ww-hypothesis-panel__evidence is-against">
                      {t('werewolf.history.hypothesis.refuting')}: {e.refuting}
                    </div>
                  )}
                </li>
              ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

export default HypothesisPanel;
