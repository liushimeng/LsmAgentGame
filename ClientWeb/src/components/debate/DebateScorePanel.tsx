/**
 * 辩论评分结果面板 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §3.5 评分面板设计。
 * §20260831-04 — 每队新增 5 维 SVG 雷达图(DebateRadarChart)+ 维度中文标签;
 * 多队模式按 team_scores 数量渲染(不硬编码两队)。
 *
 * §20260831-11 — R8 P2-B 修复:裁判点评块加固 ——
 * overall_comment 为空回退 comment → rankings 队伍评语,显示 model_key
 * 与 is_fallback「默认评分」徽章,整条全空才跳过(配合 useDebate 的
 * game_over 帧 merge,保证服务端有数据时前端必渲染)。
 */
import { useDebateStore } from '@/store/debate.store';
import type { DebateJudgeScore } from '@/types/debate';
import DebateRadarChart, { TEAM_RADAR_COLORS } from './DebateRadarChart';

/** 维度 key → 中文标签(与后端 ScoreDimensions json tag 对应)
 *  §20260831-08 — 导出供 DebateReplayModal(复盘详情)复用。 */
export const DIM_LABELS: Record<string, string> = {
  argument_quality: '论证质量',
  logic_rigor: '逻辑严谨',
  language_expression: '语言表达',
  team_coordination: '团队配合',
  rebuttal_effectiveness: '反驳效力',
};

export default function DebateScorePanel() {
  const { result, phase } = useDebateStore();

  // §20260831-10(P1-1 修复):BroadcastResult 在 advanceTo(PhaseResult) 之前触发,
  // debate.game_over 帧到达时 phase 可能仍为 judging。因此只要 result 数据到达即渲染,
  // 不必等待 phase 变更为 result/game_over。
  //
  // §20260831-11(P2-B 修复):预计算可展示的裁判点评条目 —— 每条取
  // overall_comment → comment(扁平兼容字段)→ rankings 内队伍评语,
  // 整条全空的过滤掉;全部为空时整块不渲染(避免空 details)。
  const judgeComments = (result?.judge_details ?? [])
    .map((js) => {
      const teamComments = (js.rankings ?? [])
        .map((r) => r.comment)
        .filter(Boolean)
        .join('\n');
      const text = js.overall_comment || js.comment || teamComments;
      return text ? { js, text } : null;
    })
    .filter((x): x is { js: DebateJudgeScore; text: string } => x !== null);

  if (result) {
    return (
      <div className="debate-score-panel">
        <h3>🏆 评审结果</h3>

        {/* 胜方高亮 */}
        <div className="winner-banner">
          🥇 胜方:{result.winner_team_name}
        </div>

        {/* 各队伍分数(含雷达图;多队模式按数组渲染) */}
        {result.team_scores.map((ts, idx) => {
          const isWinner = ts.team_id === result.winner_team_id;
          const radarColor = TEAM_RADAR_COLORS[idx % TEAM_RADAR_COLORS.length];
          return (
            <div
              key={ts.team_id}
              className={`team-score${isWinner ? ' team-score--winner' : ''}`}
            >
              <header className="team-score__header">
                {isWinner ? '🥇' : '🥈'} {ts.team_name} #{ts.rank}
                <span className="total">{ts.total_score.toFixed(1)}</span>
              </header>
              <div className="team-score__body">
                <DebateRadarChart
                  dimensionScores={ts.dimension_scores}
                  color={radarColor}
                  size={128}
                />
                <div className="team-score__bars">
                  {Object.entries(ts.dimension_scores).map(([dim, score]) => (
                    <div key={dim} className="score-bar">
                      <span className="score-bar__label">{DIM_LABELS[dim] ?? dim}</span>
                      <div className="score-bar__track">
                        <div
                          className="score-bar__fill"
                          style={{ width: `${(score / 10) * 100}%` }}
                        />
                      </div>
                      <span className="score-bar__value">{score.toFixed(1)}</span>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          );
        })}

        {/* 最佳辩手 */}
        <div className="best-debater">
          🌟 最佳辩手:{result.best_debater.name} (座位 {result.best_debater.seat})
        </div>

        {/* 整体评语 — §20260831-11(P2-B 修复):
            见上方 judgeComments 预计算;显示 model_key 与「默认评分」徽章。 */}
        {judgeComments.length > 0 && (
          <details className="judge-comments">
            <summary>📝 裁判点评</summary>
            <ul>
              {judgeComments.map(({ js, text }) => (
                <li key={js.judge_id} className="judge-comment">
                  <strong className="judge-comment__title">
                    裁判 {js.judge_id + 1}({js.model_key})
                    {js.is_fallback && (
                      <span className="judge-comment__fallback">默认评分</span>
                    )}
                  </strong>
                  <p className="judge-comment__text">{text}</p>
                </li>
              ))}
            </ul>
          </details>
        )}
      </div>
    );
  }

  // result 数据尚未到达:根据阶段显示不同提示
  if (phase !== 'result' && phase !== 'game_over') {
    return (
      <div className="debate-score-panel debate-score-panel--pending">
        <h3>🏆 评审结果</h3>
        <p>评审结束后公布</p>
      </div>
    );
  }

  // phase 已是 result/game_over 但 result 数据仍未到达(异常路径)
  return (
    <div className="debate-score-panel">
      <h3>🏆 评审结果</h3>
      <p>评审中...</p>
    </div>
  );
}
