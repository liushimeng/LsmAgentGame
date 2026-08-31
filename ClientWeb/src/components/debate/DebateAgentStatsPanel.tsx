/**
 * 辩论 Agent 统计面板 (2026-08-31 §20260831-09)
 *
 * 对齐 docs/辩论比赛/07-辩论比赛Agent统计与裁判实时打分设计.md §5.1。
 *
 * 参考狼人杀 GameStatusHeader 的 token chip 实现:
 * 顶部一个紧凑可折叠的条状面板,显示:
 *   - 房间运行时长(⏱ MM:SS)
 *   - 房间总 Token(辩方 + 裁判)
 *   - 每小时 Token 速率(≥60s 且 total_api_tokens > 0 时显示)
 *   - 辩方 API 调用统计(成功/失败)
 *   - 裁判 API 调用统计(成功/失败)
 *
 * 折叠态:仅显示运行时长 + 总 Token + 速率 chip。
 * 展开态:追加每个 Bot / 裁判的详细卡片(DebateBotTokenCard)。
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useState } from 'react';
import { useDebateStore } from '@/store/debate.store';
import { formatK } from '@/shared/utils/format';
import DebateBotTokenCard from './DebateBotTokenCard';

const LS_KEY = 'debate.agentStats.collapsed';

function readCollapsed(): boolean {
  try {
    return typeof window !== 'undefined' && localStorage.getItem(LS_KEY) === '1';
  } catch {
    return false;
  }
}
function writeCollapsed(v: boolean): void {
  try {
    if (typeof window !== 'undefined') localStorage.setItem(LS_KEY, v ? '1' : '0');
  } catch { /* incognito */ }
}

function formatHMS(totalSec: number): string {
  const s = Math.max(0, Math.floor(totalSec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = s % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`;
  return `${m}:${String(ss).padStart(2, '0')}`;
}

export default function DebateAgentStatsPanel() {
  const agentStatsDetail = useDebateStore((s) => s.agentStatsDetail);
  const startedAt = useDebateStore((s) => s.currentRoom?.started_at ?? 0);
  const [collapsed, setCollapsed] = useState(readCollapsed);

  // 运行时长(从 startedAt 算,精度到秒;agentStatsDetail 变化时重算保持 10s 粒度)
  const elapsed = startedAt && startedAt > 0
    ? Math.max(0, Math.floor(Date.now() / 1000 - startedAt))
    : (agentStatsDetail?.aggregate.elapsed_sec ?? 0);

  const agg = agentStatsDetail?.aggregate;

  // 防止 elapsec > 0 但 agg 仍为空(首轮 state 未到时)
  if (!agg || agg.total_api_tokens === 0 && agg.bot_count === 0 && agg.judge_count === 0 && elapsed < 10) {
    return null;
  }

  const showRate = agg.show_token_rate && agg.tokens_per_hour > 0;
  const handleToggle = () => {
    const next = !collapsed;
    setCollapsed(next);
    writeCollapsed(next);
  };

  return (
    <div
      className={`debate-stats${collapsed ? ' debate-stats--collapsed' : ''}`}
      data-testid="debate-agent-stats-panel"
    >
      {/* 标题行(始终可见) */}
      <div className="debate-stats__header">
        <span className="debate-stats__title">📊 Agent 统计</span>
        <div className="debate-stats__chips">
          <span
            className="debate-stats__chip debate-stats__chip--clock"
            data-testid="debate-elapsed"
            title="房间运行时长"
          >
            ⏱ {formatHMS(elapsed)}
          </span>
          <span
            className="debate-stats__chip debate-stats__chip--total-tokens"
            data-testid="debate-total-tokens"
            title={`辩方 ${formatK(agg.bot_total_api_tokens)} + 裁判 ${formatK(agg.judge_total_api_tokens)}`}
          >
            ⚡ {formatK(agg.total_api_tokens)} Token
          </span>
          {showRate && (
            <span
              className="debate-stats__chip debate-stats__chip--tokenrate"
              data-testid="debate-token-rate"
              title={`累计 ${formatK(agg.total_api_tokens)} Token / ${(elapsed / 3600).toFixed(2)} h`}
            >
              🚀 {formatK(agg.tokens_per_hour)}/h
            </span>
          )}
          <span
            className="debate-stats__chip debate-stats__chip--api"
            title="辩方 API 调用(成功/失败)"
          >
            📡 辩方 {agg.bot_api_call_count} 次
            {agg.bot_api_fail_count > 0 && (
              <span className="debate-stats__badge debate-stats__badge--fail">
                {agg.bot_api_fail_count} ✗
              </span>
            )}
          </span>
          <span
            className="debate-stats__chip debate-stats__chip--judge-api"
            title="裁判 API 调用(成功/失败)"
          >
            ⚖️ 裁判 {agg.judge_api_call_count} 次
            {agg.judge_api_fail_count > 0 && (
              <span className="debate-stats__badge debate-stats__badge--fail">
                {agg.judge_api_fail_count} ✗
              </span>
            )}
          </span>
          <button
            type="button"
            className="debate-stats__fold"
            onClick={handleToggle}
            aria-label={collapsed ? '展开统计' : '折叠统计'}
          >
            {collapsed ? '▼' : '▲'}
          </button>
        </div>
      </div>

      {/* 展开态:Bot + 裁判详情卡片 */}
      {!collapsed && (
        <div className="debate-stats__details">
          <div className="debate-stats__bot-list">
            {agentStatsDetail!.bots.map((b) => (
              <DebateBotTokenCard key={`${b.team_id}:${b.seat}`} bot={b} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
