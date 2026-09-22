/**
 * CityStatsPanel — 城市背景层面板（§20260921 建房解耦，契约 §2.4）。
 *
 * 数据源 game.state.city（WealthCitySnapshot，view.go omitempty）：
 * resident_count=0 的旧房不下发 → 本面板整块不渲染（return null）。
 *
 * 展示分四段：
 *   ① 头部 `🏙 城市 · N 人`（+ 「👥 居民档案」按钮 → ResidentProfileDrawer）
 *      + 锚定进度条（city.profiles：hydrating=done/total 进度条；
 *      ready=✓ 已锚定 N 份真实档案；failed/idle=灰字说明 —— 档案锚定设计 §8.3）
 *   ② 指标网格（就业率 / 收入中位数 / 居民储蓄合计 /
 *      压力率；金额千分位、率百分比 —— formatCny / formatPct 复用）
 *   ③ 城区人口迷你条形（v2.12 阶段 2：自适应 8→16 城区；auto-fill 网格 +
 *      底对齐竖条，相对最大区人口；城区数 >12 时紧凑模式仅显示 top 10 +
 *      「其他 N 个」；数值以文本显式标注在条外，色盲不依赖颜色判读 —— CLAUDE.md §26）
 *   ④ 「居民之声」最近列表（月份 + 姓名 + 一句话 + 服务模型小徽标；
 *      锚定后 resident_id 非空 → 显示「姓名·职业」并可点击打开档案抽屉定位该卡）
 *
 * 样式在同名 CityStatsPanel.css（组件内直接 import， precedent：
 * ChatSettingsModal.css / WalletModal.css；不新增 globals.css 入口）。
 * 抽屉样式在 styles/wealth-residents.css（globals.css 链尾 @import）。
 */

import { useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny, formatPct, type WealthCitySnapshot } from '@/types/wealth';
import { ResidentProfileDrawer } from '@/components/wealth/ResidentProfileDrawer';
import { CollapsibleSection } from '@/components/wealth/CollapsibleSection';
import './CityStatsPanel.css';

interface Props {
  /** 城市背景层快照；缺省（旧房 / 尚未到达）时整面板不渲染。 */
  city?: WealthCitySnapshot | null;
  /** 房间 ID（居民档案抽屉 REST 拉取用）。 */
  roomId: string;
}

/** 城区人口竖条单元（v2.12 阶段 2）：人口数 / 底对齐条形 / 区名（自上而下）。
 *  条形高度 = population / maxPop 百分比；条形为纯装饰通道（aria-hidden），
 *  数值走顶部显式文字（§26 色盲可判读）。dimmed = 紧凑模式「其他」聚合行。 */
function DistrictBarCell({
  id,
  name,
  population,
  maxPop,
  dimmed = false,
}: {
  id: number;
  name: string;
  population: number;
  maxPop: number;
  dimmed?: boolean;
}) {
  const pct = Math.max(4, Math.round((population / maxPop) * 100));
  return (
    <div
      className={`wealth-citypanel__district${dimmed ? ' wealth-citypanel__district--other' : ''}`}
      data-testid={`wealth-city-district-${id}`}
      title={`${name} · ${population.toLocaleString()}`}
    >
      <span className="wealth-citypanel__district-pop">{population.toLocaleString()}</span>
      <span className="wealth-citypanel__district-bar">
        <span
          className="wealth-citypanel__district-fill"
          style={{ height: `${pct}%` }}
          aria-hidden="true"
        />
      </span>
      <span className="wealth-citypanel__district-name">{name}</span>
    </div>
  );
}

export function CityStatsPanel({ city, roomId }: Props) {
  const t = useT();
  // 档案抽屉开关 + 定位卡号（城市之声 resident_id 点击进入）。
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerCardId, setDrawerCardId] = useState<string | null>(null);

  const openDrawer = (cardId: string | null) => {
    setDrawerCardId(cardId);
    setDrawerOpen(true);
  };

  if (!city) return null;

  const districts = city.districts ?? [];
  const maxPop = districts.reduce((m, d) => Math.max(m, d.population || 0), 0) || 1;
  // v2.12 阶段 2：16 城区自适应 —— >12 区切换紧凑模式（top 10 + 「其他 N 个」），
  // 其余区人口合计展示，避免面板无限增高。行序 = 人口降序（紧凑）/ 服务端序（≤12）。
  const COMPACT_THRESHOLD = 12;
  const COMPACT_TOP = 10;
  const compact = districts.length > COMPACT_THRESHOLD;
  const ranked = compact
    ? [...districts].sort((a, b) => (b.population || 0) - (a.population || 0))
    : districts;
  const restRows = compact ? ranked.slice(COMPACT_TOP) : [];
  const districtRows = compact ? ranked.slice(0, COMPACT_TOP) : ranked;
  const voices = (city.voices ?? []).slice(-8).reverse();
  const prof = city.profiles ?? null;
  const hydratePct =
    prof && prof.status === 'hydrating' && prof.total > 0
      ? Math.min(100, Math.round((prof.done / prof.total) * 100))
      : 0;

  return (
    <>
      {/* 阶段 E（13-3D城市渲染优化）：融合式折叠 —— 原 ① 标题头改为
          CollapsibleSection 标题行（title + headerExtra），不叠加第二层标题；
          折叠态 localStorage 持久化（wealth.ui.collapsed.city_stats）。 */}
      <CollapsibleSection
        className="wealth-citypanel"
        storageKey="wealth.ui.collapsed.city_stats"
        defaultCollapsed
        testId="wealth-city-panel"
        title={
          <>
            🏙 {t('wealth.cityTitle' as TKey)} · {(city.resident_count || 0).toLocaleString()}{' '}
            {t('wealth.cityPopulation' as TKey)}
          </>
        }
        headerExtra={
          <button
            type="button"
            className="wealth-citypanel__open"
            onClick={() => openDrawer(null)}
            aria-label={t('wealth.cityProfiles.openDrawer' as TKey)}
            data-testid="wealth-city-open-profiles"
          >
            👥 {t('wealth.cityProfiles.openDrawer' as TKey)}
          </button>
        }
      >

      {/* ①+ 锚定进度条（档案锚定设计 §8.3；city.profiles 缺省=旧房不渲染） */}
      {prof && (
        <div
          className={`wealth-citypanel__anchor wealth-citypanel__anchor--${prof.status}`}
          title={t('wealth.cityProfiles.title' as TKey)}
          data-testid="wealth-city-anchor"
        >
          {prof.status === 'hydrating' ? (
            <>
              <span className="wealth-citypanel__anchor-text">
                {t('wealth.cityProfiles.anchorProgress' as TKey, {
                  done: prof.done,
                  total: prof.total,
                  pool: prof.pool_size,
                })}
              </span>
              <span
                className="wealth-citypanel__anchor-bar"
                role="progressbar"
                aria-valuenow={hydratePct}
                aria-valuemin={0}
                aria-valuemax={100}
              >
                <span
                  className="wealth-citypanel__anchor-fill"
                  style={{ width: `${hydratePct}%` }}
                  aria-hidden="true"
                />
              </span>
              <span className="wealth-citypanel__anchor-pct">{hydratePct}%</span>
            </>
          ) : prof.status === 'ready' ? (
            <span className="wealth-citypanel__anchor-text wealth-citypanel__anchor-text--ready">
              ✓ {t('wealth.cityProfiles.anchorReady' as TKey, { n: prof.anchored })}
            </span>
          ) : (
            <span className="wealth-citypanel__anchor-text">
              {prof.status === 'failed'
                ? t('wealth.cityProfiles.anchorFailed' as TKey)
                : t('wealth.cityProfiles.anchorIdle' as TKey)}
            </span>
          )}
        </div>
      )}

      {/* 指标网格：就业率 / 收入中位数 / 居民储蓄合计 / 压力率 */}
      <div className="wealth-citypanel__stats">
        <div className="wealth-citypanel__stat">
          <span className="wealth-citypanel__stat-k">{t('wealth.cityEmployment' as TKey)}</span>
          <span className="wealth-citypanel__stat-v">{formatPct(city.employment_rate)}</span>
        </div>
        <div className="wealth-citypanel__stat">
          <span className="wealth-citypanel__stat-k">{t('wealth.cityMedianIncome' as TKey)}</span>
          <span className="wealth-citypanel__stat-v">{formatCny(city.median_income)}</span>
        </div>
        <div className="wealth-citypanel__stat">
          <span className="wealth-citypanel__stat-k">{t('wealth.citySavings' as TKey)}</span>
          <span className="wealth-citypanel__stat-v">{formatCny(city.total_savings)}</span>
        </div>
        <div className="wealth-citypanel__stat">
          <span className="wealth-citypanel__stat-k">{t('wealth.cityStress' as TKey)}</span>
          <span className="wealth-citypanel__stat-v">{formatPct(city.stressed_rate)}</span>
        </div>
      </div>

      {/* ② 城区人口迷你条形（v2.12 阶段 2：auto-fill 网格 + 底对齐竖条，§26 对比度：
          实底条形 + 显式文字；>12 区紧凑模式 top 10 + 「其他 N 个」） */}
      {districts.length > 0 && (
        <div className="wealth-citypanel__districts">
          {districtRows.map((d, i) => (
            <DistrictBarCell
              key={d.id >= 0 ? d.id : `row-${i}`}
              id={d.id}
              name={d.name}
              population={d.population || 0}
              maxPop={maxPop}
            />
          ))}
          {restRows.length > 0 && (
            <DistrictBarCell
              id={-1}
              name={t('wealth.cityOtherDistricts' as TKey, { n: restRows.length })}
              population={restRows.reduce((s, d) => s + (d.population || 0), 0)}
              maxPop={maxPop}
              dimmed
            />
          )}
        </div>
      )}

      {/* ③ 居民之声（最近若干条，倒序展示最新在前） */}
      {voices.length > 0 && (
        <div className="wealth-citypanel__voices">
          <div className="wealth-citypanel__voices-title">
            🗣 {t('wealth.cityVoices' as TKey)}
          </div>
          {voices.map((v, i) => (
            <div
              key={`${v.month}-${v.name}-${i}`}
              className="wealth-citypanel__voice"
              data-testid="wealth-city-voice"
            >
              <span className="wealth-citypanel__voice-meta">
                <span className="wealth-citypanel__voice-month">
                  {t('wealth.cityVoiceOfMonth' as TKey, { month: v.month })}
                </span>
                {/* 锚定后 resident_id 非空 → 「姓名·职业」可点击打开档案抽屉定位该卡 */}
                {v.resident_id ? (
                  <button
                    type="button"
                    className="wealth-citypanel__voice-name wealth-citypanel__voice-name--link"
                    onClick={() => openDrawer(v.resident_id ?? null)}
                    title={t('wealth.residentDrawer.voiceOf' as TKey, { name: v.name })}
                    data-testid={`wealth-city-voice-link-${i}`}
                  >
                    {v.name}
                    {v.occupation ? `·${v.occupation}` : ''}
                  </button>
                ) : (
                  <span className="wealth-citypanel__voice-name">{v.name}</span>
                )}
                {v.model && (
                  <span className="wealth-citypanel__voice-model" title={v.model}>
                    🤖 {v.model}
                  </span>
                )}
              </span>
              <span className="wealth-citypanel__voice-text">{v.text}</span>
            </div>
          ))}
        </div>
      )}

      {/* 居民档案抽屉（fixed 覆盖层，挂在折叠体之外：折叠不卸载抽屉；
          key=定位卡号：切换定位卡 / 重新打开时整体重挂载，state 归零）。 */}
      </CollapsibleSection>
      <ResidentProfileDrawer
        key={drawerCardId ?? 'all'}
        open={drawerOpen}
        roomId={roomId}
        focusCardId={drawerCardId}
        onClose={() => {
          setDrawerOpen(false);
          setDrawerCardId(null);
        }}
      />
    </>
  );
}

export default CityStatsPanel;
