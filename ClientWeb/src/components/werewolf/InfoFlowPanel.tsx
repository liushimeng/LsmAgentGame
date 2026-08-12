/**
 * InfoFlowPanel — 信息传播时序图(spectator only)
 *
 * 2026-08-10 §20260810-08 — 信息账本二期观战侧。
 * 设计文档:`docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260810-08.md` §2.4。
 *
 * 三块:① 顶部「疑似说漏嘴」告警区(leaks 为空时整块不渲染,文案含「仅供复盘参考」);
 * ② 主体「信息传播时序图」—— 横轴 round,纵轴 0..12 座位,每条 InfoEntryJSON 在
 *   knower_seats 行 / round 列打一个色点;纯 CSS Grid,无新依赖(§8);
 * ③ 底部 6 类来源图例(公开/私聊/狼队/夜间技能/道具/系统)。
 * §135:服务端 redacted,不含身份明文;§26:色点直径 10px + 1px 描边 + 对比度 ≥ 4.5:1。
 */
import { useMemo } from 'react';
import { useT } from '@/hooks/useT';
import type { WerewolfGameState, InfoEntryJSON, InfoLeakJSON } from '@/types/werewolf';

/** InfoSource 6 大族(见设计文档 §2.4)。 */
type SourceFamily = 'public' | 'whisper' | 'wolfpack' | 'night' | 'prop' | 'system';

function sourceToFamily(source: string): SourceFamily {
  if (source === 'whisper') return 'whisper';
  if (source === 'wolf_pack') return 'wolfpack';
  if (source === 'prop_inject') return 'prop';
  if (source === 'role_deal') return 'system';
  if (
    source === 'night_seer' ||
    source === 'night_witch' ||
    source === 'night_guard' ||
    source === 'night_wolf_vote'
  ) return 'night';
  return 'public';
}

/** 把账本按 (round, seat) 分桶;grid[seat][round] = SourceFamily[]。 */
function buildGrid(
  entries: InfoEntryJSON[],
  seatCount: number,
): { grid: SourceFamily[][][]; maxRound: number } {
  const maxRound = entries.reduce((m, e) => Math.max(m, e.round), 0);
  const grid: SourceFamily[][][] = Array.from({ length: seatCount }, () =>
    Array.from({ length: maxRound + 1 }, () => [] as SourceFamily[]),
  );
  for (const e of entries) {
    const family = sourceToFamily(e.source);
    if (e.round < 0 || e.round > maxRound) continue;
    for (const seat of e.knower_seats) {
      if (seat < 0 || seat >= seatCount) continue;
      grid[seat][e.round].push(family);
    }
  }
  return { grid, maxRound };
}

interface InfoFlowPanelProps {
  gameState: WerewolfGameState;
}

function InfoFlowPanel({ gameState }: InfoFlowPanelProps) {
  const t = useT();
  const entries = gameState.info_ledger ?? [];
  const leaks = gameState.info_leaks ?? [];
  const seatCount = 13; // 13 人局固定;兼容历史 12 人局数据(空行直接渲染空 cell)

  const { grid, maxRound } = useMemo(() => buildGrid(entries, seatCount), [entries, seatCount]);

  if (entries.length === 0) {
    return <div className="ww-infoflow__empty">{t('werewolf.infoflow.empty' as any)}</div>;
  }

  const cols = `60px repeat(${maxRound + 1}, minmax(40px, 1fr))`;

  return (
    <div className="ww-infoflow" data-testid="ww-infoflow">
      {leaks.length > 0 && (
        <section className="ww-infoflow__leak-alert" data-testid="ww-infoflow-leak-alert">
          <h4 className="ww-infoflow__leak-alert-title">
            {t('werewolf.infoflow.leakAlertTitle' as any, { count: leaks.length })}
          </h4>
          <p className="ww-infoflow__leak-disclaimer">
            {t('werewolf.infoflow.leakDisclaimer' as any)}
          </p>
          <ul className="ww-infoflow__leak-list">
            {leaks.map((lk: InfoLeakJSON) => (
              <li
                key={`${lk.seq}-${lk.seat}-${lk.hint_seat}`}
                className="ww-infoflow__leak-row"
                data-testid="ww-infoflow-leak-row"
              >
                <span className="ww-infoflow__leak-row-main">
                  {t('werewolf.infoflow.leakEntry' as any, {
                    day: lk.round,
                    seat: lk.seat + 1,
                    hintSeat: lk.hint_seat + 1,
                    source: t(
                      `werewolf.infoflow.source.${sourceToFamily(lk.from_source)}` as any,
                    ),
                  })}
                </span>
                {lk.excerpt && (
                  <span className="ww-infoflow__leak-row-excerpt">「{lk.excerpt}」</span>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="ww-infoflow__timeline" data-testid="ww-infoflow-timeline">
        <h4 className="ww-infoflow__timeline-title">
          {t('werewolf.infoflow.timelineTitle' as any, { count: entries.length })}
        </h4>
        <div className="ww-infoflow__grid-wrap">
          <div className="ww-infoflow__grid" style={{ gridTemplateColumns: cols }}>
            <div className="ww-infoflow__cell ww-infoflow__cell--head">
              {t('werewolf.infoflow.seatLabel' as any)}
            </div>
            {Array.from({ length: maxRound + 1 }, (_, r) => (
              <div
                key={`hd-${r}`}
                className="ww-infoflow__cell ww-infoflow__cell--head"
                data-testid={`ww-infoflow-head-d${r}`}
              >
                {t('werewolf.infoflow.dayPrefix' as any)}
                {r}
              </div>
            ))}
            {Array.from({ length: seatCount }, (_, seat) => (
              <Row
                key={`r-${seat}`}
                seat={seat}
                maxRound={maxRound}
                cells={grid[seat]}
                entries={entries}
                t={t}
              />
            ))}
          </div>
        </div>
      </section>

      <section className="ww-infoflow__legend" data-testid="ww-infoflow-legend">
        {(['public', 'whisper', 'wolfpack', 'night', 'prop', 'system'] as SourceFamily[]).map(
          (family) => (
            <span key={family} className="ww-infoflow__legend-item">
              <span className={`ww-infoflow__legend-dot ww-infoflow__dot--${family}`} />
              {t(`werewolf.infoflow.source.${family}` as any)}
            </span>
          ),
        )}
      </section>
    </div>
  );
}

/** 单行:座位标签 + 若干天 cell,每个 cell 渲染所有色点 + hover tooltip。 */
function Row({
  seat,
  maxRound,
  cells,
  entries,
  t,
}: {
  seat: number;
  maxRound: number;
  cells: SourceFamily[][];
  entries: InfoEntryJSON[];
  t: (k: any, vars?: Record<string, string | number>) => string;
}) {
  return (
    <>
      <div
        className="ww-infoflow__cell ww-infoflow__cell--seat"
        data-testid={`ww-infoflow-seat-${seat}`}
      >
        #{seat + 1}
      </div>
      {Array.from({ length: maxRound + 1 }, (_, r) => {
        const families = cells[r] ?? [];
        const relevant = entries.filter(
          (e) => e.round === r && e.knower_seats.includes(seat),
        );
        const tooltip =
          relevant.length === 0
            ? ''
            : relevant
                .map((e) => {
                  const familyLabel = t(
                    `werewolf.infoflow.source.${sourceToFamily(e.source)}` as any,
                  );
                  const knowerList = e.knower_seats.map((s) => `#${s + 1}`).join(', ');
                  return `[${familyLabel}] ${e.fact}\n${t(
                    'werewolf.infoflow.knowerSeats' as any,
                    { seats: knowerList },
                  )}`;
                })
                .join('\n\n');
        const hasDots = families.length > 0;
        return (
          <div
            key={`c-${seat}-${r}`}
            className={`ww-infoflow__cell${hasDots ? ' ww-infoflow__cell--has-dots' : ''}`}
            data-testid={`ww-infoflow-cell-${seat}-${r}`}
            title={tooltip || undefined}
          >
            {hasDots && (
              <span className="ww-infoflow__dots">
                {families.map((f, i) => (
                  <span
                    key={i}
                    className={`ww-infoflow__dot ww-infoflow__dot--${f}`}
                    aria-hidden
                  />
                ))}
              </span>
            )}
          </div>
        );
      })}
    </>
  );
}

export default InfoFlowPanel;