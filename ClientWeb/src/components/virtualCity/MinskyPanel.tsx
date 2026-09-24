/**
 * MinskyPanel — 明斯基状态仪表盘（P1 明斯基引擎 v2.60 N11-4/N11-5）。
 *
 * 展示当前全局明斯基概览：
 *   - 三档融资等级玩家数（hedge/speculative/ponzi）
 *   - 庞氏玩家占比温度计（>30% 触发明斯基时刻阈值警示）
 *   - 明斯基冷却倒计时
 *   - 历史触发次数
 *   - 教育向「明斯基时刻」说明提示
 *
 * 数据源：game.state.minsky_overview（后端 BuildClientState 下发）。
 * 防御性：后端字段可能缺失（旧房间 / 未实现），缺则显示占位空态。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  minskyTierColor,
  type VirtualCityGameState,
  type VirtualCityMinskyTier,
} from '@/types/virtualCity';

interface Props {
  gameState: VirtualCityGameState | null;
}

/** 明斯基等级 i18n key。 */
function tierLabel(tier: VirtualCityMinskyTier): TKey {
  return `minsky.${tier}` as TKey;
}

/** 明斯基等级徽章（颜色编码 + 白字对比度 ≥5:1）。 */
function MinskyBadge({ tier, count }: { tier: VirtualCityMinskyTier; count: number }) {
  const t = useT();
  const color = minskyTierColor(tier);
  return (
    <span
      className="virtualCity-badge virtualCity-minsky-badge"
      style={{ background: color, color: '#ffffff' }}
    >
      {t(tierLabel(tier))} · {count}
    </span>
  );
}

/** 庞氏占比温度计（0–100%，≥30% 红色警示阈值线）。 */
function PonziMeter({ ratio }: { ratio: number }) {
  const t = useT();
  const pct = Math.max(0, Math.min(1, ratio)) * 100;
  const danger = ratio > 0.3;
  return (
    <div className="virtualCity-minsky__meter" aria-label={t('minsky.ponziRatio' as TKey)}>
      <div className="virtualCity-minsky__meter-head">
        <span className="virtualCity-minsky__meter-label">
          {t('minsky.ponziRatio' as TKey)}
        </span>
        <span
          className={danger ? 'virtualCity-minsky__meter-value virtualCity-num--neg' : 'virtualCity-minsky__meter-value'}
        >
          {pct.toFixed(1)}%
        </span>
      </div>
      <div className="virtualCity-minsky__meter-track">
        <div
          className={danger ? 'virtualCity-minsky__meter-fill virtualCity-minsky__meter-fill--danger' : 'virtualCity-minsky__meter-fill'}
          style={{ width: `${pct}%` }}
        />
        {/* 30% 阈值标记线 */}
        <span className="virtualCity-minsky__meter-threshold" title={t('minsky.threshold' as TKey)} />
      </div>
      <div className="virtualCity-minsky__meter-foot">
        <span>0%</span>
        <span className="virtualCity-minsky__meter-threshold-label">
          {t('minsky.threshold' as TKey)} 30%
        </span>
        <span>100%</span>
      </div>
    </div>
  );
}

export function MinskyPanel({ gameState }: Props) {
  const t = useT();
  const overview = gameState?.minsky_overview;
  const cycle = gameState?.cycle;
  const totalAlive = (gameState?.players ?? []).filter((p) => p.alive).length || 1;

  // 空态（后端未下发概览 —— 旧房间 / 过渡期兼容）。
  if (!overview) {
    return (
      <div className="virtualCity-minsky">
        <div className="virtualCity-minsky__head">
          <span className="virtualCity-minsky__title">{t('minsky.title' as TKey)}</span>
        </div>
        <div className="virtualCity-panel__empty">
          <p>{t('minsky.unavailable' as TKey)}</p>
        </div>
      </div>
    );
  }

  const total = Math.max(1, overview.ponzi_count + overview.spec_count + overview.hedge_count);
  const hedgePct = (overview.hedge_count / total) * 100;
  const specPct = (overview.spec_count / total) * 100;
  const ponziPct = (overview.ponzi_count / total) * 100;

  return (
    <div className="virtualCity-minsky">
      <div className="virtualCity-minsky__head">
        <span className="virtualCity-minsky__title">{t('minsky.title' as TKey)}</span>
        {(cycle?.minsky_moment_count ?? 0) > 0 && (
          <span className="virtualCity-badge virtualCity-minsky-badge--count">
            {t('minsky.triggeredCount' as TKey, { n: cycle?.minsky_moment_count ?? 0 })}
          </span>
        )}
      </div>

      {/* 三档融资等级分布条 */}
      <div className="virtualCity-minsky__tiers">
        <MinskyBadge tier="hedge" count={overview.hedge_count} />
        <MinskyBadge tier="speculative" count={overview.spec_count} />
        <MinskyBadge tier="ponzi" count={overview.ponzi_count} />
      </div>

      {/* 堆叠分布条（对冲/投机/庞氏） */}
      <div
        className="virtualCity-minsky__stack"
        title={`${t('minsky.hedge' as TKey)} ${hedgePct.toFixed(0)}% / ${t('minsky.speculative' as TKey)} ${specPct.toFixed(0)}% / ${t('minsky.ponzi' as TKey)} ${ponziPct.toFixed(0)}%`}
      >
        {hedgePct > 0 && (
          <span
            className="virtualCity-minsky__stack-fill virtualCity-minsky__stack-fill--hedge"
            style={{ width: `${hedgePct}%` }}
          />
        )}
        {specPct > 0 && (
          <span
            className="virtualCity-minsky__stack-fill virtualCity-minsky__stack-fill--spec"
            style={{ width: `${specPct}%` }}
          />
        )}
        {ponziPct > 0 && (
          <span
            className="virtualCity-minsky__stack-fill virtualCity-minsky__stack-fill--ponzi"
            style={{ width: `${ponziPct}%` }}
          />
        )}
      </div>

      {/* 庞氏占比温度计 */}
      <PonziMeter ratio={overview.ponzi_ratio} />

      {/* 冷却倒计时 */}
      {overview.cooldown_left > 0 && (
        <div className="virtualCity-minsky__cooldown">
          <span className="virtualCity-minsky__cooldown-icon" aria-hidden="true">🛡️</span>
          <span>
            {t('minsky.cooldown' as TKey, { n: overview.cooldown_left })}
          </span>
        </div>
      )}

      {/* 存活人数备注 */}
      <div className="virtualCity-minsky__total">
        {t('minsky.totalAlive' as TKey, { n: totalAlive })}
      </div>

      {/* 教育向提示 */}
      <details className="virtualCity-minsky__edu">
        <summary>{t('minsky.eduToggle' as TKey)}</summary>
        <div className="virtualCity-minsky__edu-body">
          <p>{t('minsky.eduP1' as TKey)}</p>
          <p>{t('minsky.eduP2' as TKey)}</p>
          <p className="virtualCity-minsky__edu-warn">{t('minsky.eduWarn' as TKey)}</p>
        </div>
      </details>
    </div>
  );
}
