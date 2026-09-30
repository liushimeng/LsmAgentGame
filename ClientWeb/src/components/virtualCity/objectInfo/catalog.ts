/**
 * catalog — 虚拟城市场景物件元数据注册表（批次 28 · 工作线 B1，唯一事实来源）。
 *
 * 契约：lag_docs/虚拟城市/已实现/28-3D性能优化与物件信息交互/01-方案设计-v1.md §5.2。
 *   - 每个可渲染物件类型一个 `ObjectInfoEntry`（id / category / icon / 三语 name+desc）；
 *   - 文案内联三语，**不进** `i18n/Dict`（避免撑爆 locale 1800 行上限；catalog 是
 *     「场景物件注册表」= 游戏数据，供后期按信息裁剪模型）；
 *   - 语言取值走 `useI18nStore.lang`（见 ObjectInfoOverlay）；
 *   - 动态实例行（楼层 / 所属城区 / #实例 …）由接线组件现场注入，不进 catalog；
 *   - 覆盖自检：`coverage.ts` 的 `ALL_SCENE_OBJECT_IDS` 必须 100% 命中本注册表。
 *
 * 数据来源拆分（防单文件膨胀）：
 *   - catalog-districts.ts → 32 个 `district.*`（名称复用 i18n `virtualCity.district.<id>`）
 *   - catalog-objects.ts   → building / prop / tree / vehicle / actor / road
 *   - catalog-scene.ts     → civic / landmark / edge / water / sky / ground
 */

import type { Lang } from '@/i18n';
import { DISTRICT_ENTRIES } from './catalog-districts';
import { OBJECT_ENTRIES } from './catalog-objects';
import { SCENE_ENTRIES } from './catalog-scene';

/** 物件分类（方案 §5.2 枚举；渲染端徽章按此分组着色）。 */
export type ObjectCategory =
  | 'district'
  | 'building'
  | 'street-prop'
  | 'tree'
  | 'vehicle'
  | 'pedestrian'
  | 'civic'
  | 'landmark'
  | 'road'
  | 'water'
  | 'edge'
  | 'sky'
  | 'agent'
  | 'ground';

/** 三语文案（Lang 与 i18n 一致：zh-CN / en / ja）。 */
export type TriText = Record<Lang, string>;

export interface ObjectInfoEntry {
  /** 稳定 id，如 `building.tower` / `prop.trash-can` / `district.finance`。 */
  id: string;
  category: ObjectCategory;
  /** emoji 分类徽章。 */
  icon: string;
  name: TriText;
  /** 简介正文（中文 30–80 字；en/ja 对应翻译）。 */
  desc: TriText;
}

/** 动态附加行（extra）：组件现场注入，label/value 为已本地化短文本。 */
export interface ObjectInfoExtra {
  label: string;
  value: string;
}

// ── 注册表装配 ────────────────────────────────────────────────────────

const ALL_ENTRIES: ObjectInfoEntry[] = [
  ...DISTRICT_ENTRIES,
  ...OBJECT_ENTRIES,
  ...SCENE_ENTRIES,
];

const BY_ID = new Map<string, ObjectInfoEntry>(ALL_ENTRIES.map((e) => [e.id, e]));

if (BY_ID.size !== ALL_ENTRIES.length) {
  // 重复 id 直接抛错（防两份文案静默互相覆盖）。
  const seen = new Set<string>();
  for (const e of ALL_ENTRIES) {
    if (seen.has(e.id)) throw new Error(`[objectInfo] duplicate catalog id: ${e.id}`);
    seen.add(e.id);
  }
}

/** 按 id 查物件元数据；缺失返回 undefined（调用方自行兜底）。 */
export function getObjectInfo(id: string): ObjectInfoEntry | undefined {
  return BY_ID.get(id);
}

/** 全部注册条目（覆盖自检 / 调试用）。 */
export function allObjectInfoEntries(): ObjectInfoEntry[] {
  return ALL_ENTRIES;
}

/** 读三语文案（未知语言回退 zh-CN）。 */
export function triText(t: TriText, lang: Lang): string {
  return t[lang] ?? t['zh-CN'];
}

/** 悬浮卡「一句话简介」：取 desc 首句（中英日句号/句点断句，兜底截 40 字）。 */
export function firstSentence(t: TriText, lang: Lang): string {
  const s = triText(t, lang);
  const m = s.match(/^[^。.！!？?]*[。.！!？?]/);
  const first = m ? m[0] : s.slice(0, 40);
  return first.trim();
}

// ── 动态附加行（extra）语义标签三语小词表 ────────────────────────────
// 组件注入 extra 时 label 直接用语义 key（如 'floors'），展示前由 hook 按当前
// 语言本地化（useObjectInfoProps.buildTarget）；未命中的 key 原样展示。
// 不进 i18n/Dict（与 catalog 同策略，见 §5.2 决策）。

export const EXTRA_LABELS: Record<string, TriText> = {
  district: { 'zh-CN': '所属城区', en: 'District', ja: '所属街区' },
  floors: { 'zh-CN': '楼层', en: 'Floors', ja: '階数' },
  serial: { 'zh-CN': '编号', en: 'Serial', ja: '番号' },
  instance: { 'zh-CN': '实例', en: 'Instance', ja: 'インスタンス' },
  name: { 'zh-CN': '昵称', en: 'Nickname', ja: 'ニックネーム' },
  profession: { 'zh-CN': '职业', en: 'Profession', ja: '職業' },
  netWorth: { 'zh-CN': '净资产', en: 'Net Worth', ja: '純資産' },
  seat: { 'zh-CN': '座位', en: 'Seat', ja: '席' },
  variant: { 'zh-CN': '款式', en: 'Variant', ja: '型式' },
  category: { 'zh-CN': '分类', en: 'Category', ja: '分類' },
  players: { 'zh-CN': '在区玩家', en: 'Players Here', ja: '区内プレイヤー' },
  housePrice: { 'zh-CN': '房价', en: 'House Price', ja: '住宅価格' },
  rent: { 'zh-CN': '租金', en: 'Rent', ja: '家賃' },
  beta: { 'zh-CN': '房价 β', en: 'Price β', ja: '価格 β' },
  // 批次 38 R7：运河桥衔接路段 key（canalBridgeSpots 的 roadKey）
  road: { 'zh-CN': '衔接路段', en: 'Road', ja: '接続道路' },
};

/** 语义 key → 当前语言标签；非 key 文本原样返回。 */
export function extraLabel(label: string, lang: Lang): string {
  const hit = EXTRA_LABELS[label];
  return hit ? triText(hit, lang) : label;
}
