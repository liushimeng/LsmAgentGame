/**
 * SystemDialogCatalogPanel —— 「场景目录浏览器」（批次 84 §5.1）。
 *
 * 目录规模在批次 84 经模板参数化派生可达 ≥1,100 条 / 13 个维度 ⇒ 对话里
 * 不可能靠自然语言逐条点名，必须给一个**可检索的目录浏览器**：
 *
 *   - 维度 Tab（横向可滚动，来自后端 `dimensions[]`）+「全部」；
 *   - 搜索框（输入防抖 300 ms，避免每敲一个字打一次后端）；
 *   - 条目列表（kind / 名称 / 组 / 默认强度 L1–L5 / 描述）。
 *
 * ⚠️ **安全约束**：点击条目**只把「注入 <名称>（L<强度>，<kind>）」填入输入
 * 框**，由人确认后再发送 —— 绝不自动发消息（目录条目会触发真实注入）。
 * 纯逻辑见 `systemDialogCatalog.ts`（可单测）。
 *
 * 数据源：`GET /api/games/virtual_city/scenario_catalog?group=&q=&limit=`
 * （只读端点，由后端批次 84 实现）。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { reportGlobalError } from '@/services/globalError';
import { fetchVirtualCityScenarioCatalog } from '@/api/virtualCity';
import type {
  VirtualCityScenarioCatalogDimension,
  VirtualCityScenarioCatalogItem,
} from '@/types/virtualCitySystem';
import { applyCatalogPick, clampMagnitude } from './systemDialogCatalog';

/** 搜索防抖（ms）—— 批次 84 §5.1 要求 ≥ 300。 */
const SEARCH_DEBOUNCE_MS = 300;
/** 单页条目数（后端上限 200；此处取 60 兼顾信息量与响应速度）。 */
const PAGE_LIMIT = 60;

interface Props {
  /** 点击条目 ⇒ 回填输入框（**不发送**）。 */
  onPick: (text: string) => void;
}

export function SystemDialogCatalogPanel({ onPick }: Props) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [dimensions, setDimensions] = useState<VirtualCityScenarioCatalogDimension[]>([]);
  const [group, setGroup] = useState('');
  const [query, setQuery] = useState('');
  const [debounced, setDebounced] = useState('');
  const [items, setItems] = useState<VirtualCityScenarioCatalogItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  /** 请求序号（只认最后一次响应，避免竞态覆盖）。 */
  const reqSeqRef = useRef(0);

  // 搜索防抖：输入停止 300 ms 后才发请求。
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [query]);

  // 目录拉取：展开时 / 维度切换 / 防抖词变化。
  useEffect(() => {
    if (!open) return undefined;
    const seq = ++reqSeqRef.current;
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchVirtualCityScenarioCatalog({ group, q: debounced, limit: PAGE_LIMIT })
      .then((res) => {
        if (cancelled || seq !== reqSeqRef.current) return;
        setDimensions(res.dimensions);
        setItems(res.items);
        setTotal(res.total);
      })
      .catch((e: unknown) => {
        if (cancelled || seq !== reqSeqRef.current) return;
        const msg = e instanceof Error ? e.message : String(e);
        setError(msg);
        // §7.1：失败必须有可见反馈（面板内联红条 + 全局 toast）。
        reportGlobalError({
          message: `${t('virtualCity.systemDialog.catalogError' as TKey)}：${msg}`,
          severity: 'error',
        });
      })
      .finally(() => {
        if (cancelled || seq !== reqSeqRef.current) return;
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, group, debounced, t]);

  const handlePick = useCallback(
    (item: VirtualCityScenarioCatalogItem) => {
      applyCatalogPick(item, {
        fillDraft: onPick,
        verb: t('virtualCity.systemDialog.catalogInject' as TKey),
      });
    },
    [onPick, t],
  );

  return (
    <div className="virtualCity-sysdialog__section" data-testid="virtualCity-system-catalog">
      <button
        type="button"
        className="virtualCity-sysdialog__section-head"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        data-testid="virtualCity-system-catalog-toggle"
      >
        <span className="virtualCity-sysdialog__section-title">
          📚 {t('virtualCity.systemDialog.catalogTitle' as TKey)}
        </span>
        <span className="virtualCity-sysdialog__section-caret">{open ? '▾' : '▸'}</span>
      </button>

      {open && (
        <div className="virtualCity-sysdialog__catalog">
          <div className="virtualCity-sysdialog__catalog-search">
            <input
              className="virtualCity-sysdialog__catalog-input"
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('virtualCity.systemDialog.catalogSearch' as TKey)}
              data-testid="virtualCity-system-catalog-search"
            />
            <span className="virtualCity-sysdialog__catalog-total">
              {t('virtualCity.systemDialog.catalogTotal' as TKey, { n: total })}
            </span>
          </div>

          <div className="virtualCity-sysdialog__catalog-tabs" role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={group === ''}
              className={
                'virtualCity-sysdialog__catalog-tab'
                + (group === '' ? ' virtualCity-sysdialog__catalog-tab--active' : '')
              }
              onClick={() => setGroup('')}
            >
              {t('virtualCity.systemDialog.catalogAll' as TKey)}
            </button>
            {dimensions.map((d) => (
              <button
                type="button"
                role="tab"
                aria-selected={group === d.group}
                key={d.group}
                className={
                  'virtualCity-sysdialog__catalog-tab'
                  + (group === d.group ? ' virtualCity-sysdialog__catalog-tab--active' : '')
                }
                onClick={() => setGroup(d.group)}
              >
                {d.group} <span className="virtualCity-sysdialog__catalog-count">{d.count}</span>
              </button>
            ))}
          </div>

          {error && (
            <div className="virtualCity-sysdialog__catalog-error" role="alert">
              {t('virtualCity.systemDialog.catalogError' as TKey)}：{error}
            </div>
          )}

          {loading && items.length === 0 && (
            <div className="virtualCity-sysdialog__catalog-hint">
              {t('virtualCity.systemDialog.catalogLoading' as TKey)}
            </div>
          )}

          {!loading && !error && items.length === 0 && (
            <div className="virtualCity-sysdialog__catalog-hint">
              {t('virtualCity.systemDialog.catalogEmpty' as TKey)}
            </div>
          )}

          <div className="virtualCity-sysdialog__catalog-list">
            {items.map((it) => (
              <button
                type="button"
                className="virtualCity-sysdialog__catalog-item"
                key={`${it.group}-${it.kind}`}
                onClick={() => handlePick(it)}
                title={it.description || it.name}
                data-testid={`virtualCity-system-catalog-item-${it.kind}`}
              >
                <span className="virtualCity-sysdialog__catalog-item-top">
                  <span className="virtualCity-sysdialog__catalog-item-name">{it.name}</span>
                  <span className="virtualCity-sysdialog__catalog-item-mag">
                    L{clampMagnitude(it.default_magnitude)}
                  </span>
                </span>
                <span className="virtualCity-sysdialog__catalog-item-meta">
                  <span className="virtualCity-sysdialog__catalog-item-group">{it.group}</span>
                  <code className="virtualCity-sysdialog__catalog-item-kind">{it.kind}</code>
                </span>
                {it.description && (
                  <span className="virtualCity-sysdialog__catalog-item-desc">{it.description}</span>
                )}
              </button>
            ))}
          </div>

          <div className="virtualCity-sysdialog__catalog-hint">
            {t('virtualCity.systemDialog.catalogHint' as TKey)}
          </div>
        </div>
      )}
    </div>
  );
}

export default SystemDialogCatalogPanel;
