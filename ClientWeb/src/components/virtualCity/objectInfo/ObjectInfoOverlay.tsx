/**
 * ObjectInfoOverlay — 全场景唯一物件信息卡（批次 28 · 工作线 B1）。
 *
 * 契约：01-方案设计-v1.md §5.1 / §5.5。
 *   - hover：紧凑卡（名称 + 分类徽章 + 一句话简介），`pointer-events:none` 不挡射线；
 *   - click：详情卡（名称 + 完整简介 + 动态 extra 行 + 关闭按钮；ESC / × 关闭）；
 *   - drei `<Html position={anchor}>` 单实例挂载（仅 hovered/selected 时挂），
 *     锚点 = store 里的世界坐标，跟随 3D；
 *   - `zIndexRange={[30, 0]}` —— 不压 minimap z40 / error z50 / modal z200（契约 04 §4）；
 *   - 文案取 catalog 三语，语言跟 `useI18nStore.lang`。
 */

import { useEffect } from 'react';
import { Html } from '@react-three/drei';
import { useI18nStore } from '@/store/i18n.store';
import {
  firstSentence,
  getObjectInfo,
  triText,
  type ObjectCategory,
} from './catalog';
import { useObjectInfoStore, type ObjectInfoTarget } from './objectInfoStore';

/** 分类徽章文案（UI 侧小词表，不进 i18n/Dict —— 与 catalog 同策略）。 */
const CATEGORY_LABELS: Record<ObjectCategory, { 'zh-CN': string; en: string; ja: string }> = {
  district: { 'zh-CN': '城区', en: 'District', ja: '街区' },
  building: { 'zh-CN': '楼宇', en: 'Building', ja: '建物' },
  'street-prop': { 'zh-CN': '街具', en: 'Street Prop', ja: '街具' },
  tree: { 'zh-CN': '树木', en: 'Tree', ja: '樹木' },
  vehicle: { 'zh-CN': '车辆', en: 'Vehicle', ja: '車両' },
  pedestrian: { 'zh-CN': '行人', en: 'Pedestrian', ja: '歩行者' },
  civic: { 'zh-CN': '市政', en: 'Civic', ja: '公益' },
  landmark: { 'zh-CN': '地标', en: 'Landmark', ja: '名所' },
  road: { 'zh-CN': '道路', en: 'Road', ja: '道路' },
  water: { 'zh-CN': '水系', en: 'Water', ja: '水域' },
  edge: { 'zh-CN': '城缘', en: 'City Edge', ja: '都市外縁' },
  sky: { 'zh-CN': '天空', en: 'Sky', ja: '空' },
  agent: { 'zh-CN': '玩家', en: 'Player', ja: 'プレイヤー' },
  ground: { 'zh-CN': '地面', en: 'Ground', ja: '地面' },
};

const CLOSE_LABEL = { 'zh-CN': '关闭', en: 'Close', ja: '閉じる' } as const;

function HoverCard({ target }: { target: ObjectInfoTarget }) {
  const lang = useI18nStore((s) => s.lang);
  const entry = getObjectInfo(target.id);
  if (!entry) return null;
  return (
    <div className="virtualCity-objectinfo-card virtualCity-objectinfo-card--hover" aria-hidden="true">
      <div className="virtualCity-objectinfo-card__head">
        <span className="virtualCity-objectinfo-card__icon">{entry.icon}</span>
        <span className="virtualCity-objectinfo-card__name">{triText(entry.name, lang)}</span>
        <span className="virtualCity-objectinfo-card__badge">{triText(CATEGORY_LABELS[entry.category], lang)}</span>
      </div>
      <div className="virtualCity-objectinfo-card__desc">{firstSentence(entry.desc, lang)}</div>
    </div>
  );
}

function DetailCard({ target }: { target: ObjectInfoTarget }) {
  const lang = useI18nStore((s) => s.lang);
  const clearSelected = useObjectInfoStore((s) => s.clearSelected);
  const entry = getObjectInfo(target.id);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') clearSelected();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [clearSelected]);
  if (!entry) return null;
  return (
    <div className="virtualCity-objectinfo-card virtualCity-objectinfo-card--detail" role="dialog">
      <div className="virtualCity-objectinfo-card__head">
        <span className="virtualCity-objectinfo-card__icon">{entry.icon}</span>
        <span className="virtualCity-objectinfo-card__name">{triText(entry.name, lang)}</span>
        <span className="virtualCity-objectinfo-card__badge">{triText(CATEGORY_LABELS[entry.category], lang)}</span>
        <button
          type="button"
          className="virtualCity-objectinfo-card__close"
          onClick={clearSelected}
          aria-label={CLOSE_LABEL[lang] ?? CLOSE_LABEL['zh-CN']}
        >
          ×
        </button>
      </div>
      <div className="virtualCity-objectinfo-card__desc virtualCity-objectinfo-card__desc--full">
        {triText(entry.desc, lang)}
      </div>
      {target.extra && target.extra.length > 0 && (
        <div className="virtualCity-objectinfo-card__extras">
          {target.extra.map((row) => (
            <div className="virtualCity-objectinfo-card__row" key={row.label}>
              <span>{row.label}</span>
              <b>{row.value}</b>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/** 单例挂载于 VirtualCityCityMap 的 Canvas 内（仅 hovered/selected 时挂 Html）。 */
export function ObjectInfoOverlay() {
  const hovered = useObjectInfoStore((s) => s.hovered);
  const selected = useObjectInfoStore((s) => s.selected);
  // 详情卡打开时抑制悬浮卡，避免叠卡。
  const hoverTarget = selected ? null : hovered;
  const target = selected ?? hoverTarget;
  if (!target) return null;
  return (
    <Html
      position={target.pos}
      center
      zIndexRange={[30, 0]}
      // 非 transform 模式下 drei Html 忽略 pointerEvents prop，须经 style 传递：
      // hover 卡不挡射线（pointer-events:none），详情卡可点关闭按钮。
      style={{ pointerEvents: selected ? 'auto' : 'none' }}
    >
      {selected ? <DetailCard target={selected} /> : <HoverCard target={hoverTarget!} />}
    </Html>
  );
}

export default ObjectInfoOverlay;
