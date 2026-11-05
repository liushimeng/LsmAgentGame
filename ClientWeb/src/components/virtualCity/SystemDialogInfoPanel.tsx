/**
 * SystemDialogInfoPanel —— 「系统信息」折叠面板（批次 84 §5.1）。
 *
 * 内容：**当前城市状态摘要**（人口 / CPI / 失业率 / 股指 / 国库 / 政策利率 /
 * 活动场景数 / 月份）+ Agent 的 `query_city_state` 回灌文本。
 *
 * 数据来源优先级（**优先复用，不新造后端接口**）：
 *   ① WS `game.state` 里**已有**的字段（`useVirtualCityStore.gameState`）：
 *      人口 `city.resident_count`、CPI `consumer_market.cpi_yoy`、
 *      失业率 `labor_market.unemployment_rate`、股指 `market.stock_index`、
 *      国库 `treasury.cash_cny`、政策利率 `central_bank.policy_rate`、
 *      活动场景 `prediction.active_scenarios`、月份 `month`；
 *   ② 不足部分由 Agent 的 `query_city_state` 工具结果文本落 store 后显示
 *      （`useVirtualCitySystemStore.systemInfo`）。
 *
 * 折叠区，默认收起。
 */

import { useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { useVirtualCitySystemStore } from '@/store/virtualCitySystem.store';
import { formatCny } from '@/types/virtualCity';

/** 小数 → 百分比字符串（1 位小数）。 */
function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`;
}

/** 大额金额 → 「亿 / 万」紧凑显示。 */
function money(v: number): string {
  const abs = Math.abs(v);
  if (abs >= 1e8) return `${(v / 1e8).toFixed(2)} 亿`;
  if (abs >= 1e4) return `${(v / 1e4).toFixed(1)} 万`;
  return formatCny(v);
}

interface Row {
  key: string;
  label: string;
  value: string;
}

export function SystemDialogInfoPanel() {
  const t = useT();
  const [open, setOpen] = useState(false);
  const gameState = useVirtualCityStore((s) => s.gameState);
  const systemInfo = useVirtualCitySystemStore((s) => s.systemInfo);

  const rows: Row[] = [];

  const month = gameState?.month;
  if (typeof month === 'number') {
    rows.push({ key: 'month', label: t('virtualCity.systemDialog.infoMonth' as TKey), value: `M${month}` });
  }

  const population = gameState?.city?.resident_count;
  if (typeof population === 'number') {
    rows.push({
      key: 'population',
      label: t('virtualCity.systemDialog.infoPopulation' as TKey),
      value: population.toLocaleString('en-US'),
    });
  }

  const cpi = gameState?.consumer_market?.cpi_yoy ?? gameState?.central_bank?.cpi;
  if (typeof cpi === 'number' && Number.isFinite(cpi)) {
    rows.push({
      key: 'cpi',
      label: t('virtualCity.systemDialog.infoCpi' as TKey),
      value: pct(cpi),
    });
  }

  const unemployment = gameState?.labor_market?.unemployment_rate;
  if (typeof unemployment === 'number' && Number.isFinite(unemployment)) {
    rows.push({
      key: 'unemployment',
      label: t('virtualCity.systemDialog.infoUnemployment' as TKey),
      value: pct(unemployment),
    });
  }

  const stock = gameState?.market?.stock_index;
  if (typeof stock === 'number' && Number.isFinite(stock)) {
    rows.push({
      key: 'stock',
      label: t('virtualCity.systemDialog.infoStock' as TKey),
      value: stock.toFixed(2),
    });
  }

  const cash = gameState?.treasury?.cash_cny;
  if (typeof cash === 'number' && Number.isFinite(cash)) {
    rows.push({
      key: 'treasury',
      label: t('virtualCity.systemDialog.infoTreasury' as TKey),
      value: `${money(cash)} 元`,
    });
  }

  const policyRate = gameState?.central_bank?.policy_rate;
  if (typeof policyRate === 'number' && Number.isFinite(policyRate)) {
    rows.push({
      key: 'policyRate',
      label: t('virtualCity.systemDialog.infoPolicyRate' as TKey),
      value: pct(policyRate),
    });
  }

  const scenarios = gameState?.prediction?.active_scenarios;
  if (typeof scenarios === 'number') {
    rows.push({
      key: 'scenarios',
      label: t('virtualCity.systemDialog.infoScenarios' as TKey),
      value: String(scenarios),
    });
  }

  return (
    <div className="virtualCity-sysdialog__section" data-testid="virtualCity-system-info">
      <button
        type="button"
        className="virtualCity-sysdialog__section-head"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        data-testid="virtualCity-system-info-toggle"
      >
        <span className="virtualCity-sysdialog__section-title">
          📊 {t('virtualCity.systemDialog.infoTitle' as TKey)}
        </span>
        <span className="virtualCity-sysdialog__section-caret">{open ? '▾' : '▸'}</span>
      </button>

      {open && (
        <div className="virtualCity-sysdialog__info">
          {rows.length === 0 ? (
            <div className="virtualCity-sysdialog__info-empty">
              {t('virtualCity.systemDialog.infoEmpty' as TKey)}
            </div>
          ) : (
            <div className="virtualCity-sysdialog__info-grid">
              {rows.map((r) => (
                <div className="virtualCity-sysdialog__info-cell" key={r.key}>
                  <span className="virtualCity-sysdialog__info-label">{r.label}</span>
                  <span className="virtualCity-sysdialog__info-value">{r.value}</span>
                </div>
              ))}
            </div>
          )}

          {systemInfo && (
            <div className="virtualCity-sysdialog__info-agent">
              <div className="virtualCity-sysdialog__info-agent-title">
                {t('virtualCity.systemDialog.infoAgentState' as TKey)}
              </div>
              <pre className="virtualCity-sysdialog__info-agent-text">{systemInfo}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default SystemDialogInfoPanel;
