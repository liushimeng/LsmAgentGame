/**
 * 辩论阶段打分时间线 (2026-08-31 §20260831-09)
 *
 * 对齐 docs/辩论比赛/07-辩论比赛Agent统计与裁判实时打分设计.md §5.1 —
 * 展示最近 N 条 stage_score 提交的时间线,按提交时间倒序。
 *
 * 每条记录显示:阶段(phase_cn) + 裁判名 + 提交时间 + 各队总分变化。
 *
 * 数据来源:store.scoreboards 内每个 board.stage_history 的并集 + 排序。
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useMemo } from 'react';
import { useDebateStore } from '@/store/debate.store';
import type { DebateJudgeScoreboard, DebateTeamRanking } from '@/types/debate';

interface TimelineEntry {
  key: string;
  judge_id: number;
  judgeName: string;
  phase: string;
  phase_cn: string;
  submitted_at_ms: number;
  is_final: boolean;
  teamTotals: { team_id: number; total: number }[];
}

/** 把所有裁判的 stage_history 合并成时间线条目(按 submitted_at_ms 倒序)。 */
function buildTimeline(
  scoreboards: Record<number, DebateJudgeScoreboard>,
  judges: { judge_id: number; name?: string }[],
): TimelineEntry[] {
  const entries: TimelineEntry[] = [];
  for (const board of Object.values(scoreboards)) {
    const judge = judges.find((j) => j.judge_id === board.judge_id);
    const judgeName = judge?.name ?? `裁判 ${board.judge_id + 1}`;
    for (const ss of board.stage_history) {
      const teamTotals = ss.team_scores.map((ts: DebateTeamRanking) => ({
        team_id: ts.team_id,
        total: ts.total_score,
      }));
      entries.push({
        key: `${board.judge_id}-${ss.submitted_at_ms}-${ss.phase}`,
        judge_id: board.judge_id,
        judgeName,
        phase: ss.phase,
        phase_cn: ss.phase_cn,
        submitted_at_ms: ss.submitted_at_ms,
        is_final: ss.is_final,
        teamTotals,
      });
    }
  }
  entries.sort((a, b) => b.submitted_at_ms - a.submitted_at_ms);
  return entries;
}

function formatTime(ms: number): string {
  const d = new Date(ms);
  const h = String(d.getHours()).padStart(2, '0');
  const m = String(d.getMinutes()).padStart(2, '0');
  const s = String(d.getSeconds()).padStart(2, '0');
  return `${h}:${m}:${s}`;
}

export default function DebateStageScoreTimeline() {
  const scoreboards = useDebateStore((s) => s.scoreboards);
  const judges = useDebateStore((s) => s.currentRoom?.judges ?? []);

  const entries = useMemo(
    () => buildTimeline(scoreboards, judges),
    [scoreboards, judges],
  );

  if (entries.length === 0) {
    return (
      <div className="debate-stage-score-timeline debate-stage-score-timeline--empty">
        <h3>📈 阶段打分时间线</h3>
        <p>等待裁判提交阶段打分...</p>
      </div>
    );
  }

  return (
    <div className="debate-stage-score-timeline">
      <h3>📈 阶段打分时间线</h3>
      <ul className="timeline-list">
        {entries.map((e) => (
          <li key={e.key} className={`timeline-item${e.is_final ? ' timeline-item--final' : ''}`}>
            <div className="timeline-item__header">
              <span className="timeline-item__phase">{e.phase_cn ?? e.phase}</span>
              <span className="timeline-item__judge">{e.judgeName}</span>
              <span className="timeline-item__time">{formatTime(e.submitted_at_ms)}</span>
              {e.is_final && <span className="timeline-item__final-badge">终</span>}
            </div>
            <div className="timeline-item__scores">
              {e.teamTotals.map((t) => (
                <span key={t.team_id} className="timeline-item__score">
                  队伍{t.team_id + 1}: <strong>{t.total.toFixed(1)}</strong>
                </span>
              ))}
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
