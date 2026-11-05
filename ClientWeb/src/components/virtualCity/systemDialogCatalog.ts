/**
 * systemDialogCatalog —— 「场景目录浏览器」的纯逻辑（构建指令文本 / 检索参数）。
 *
 * 批次 84 §5.1：目录浏览器点条目 ⇒ **只把「注入 <名称>（L<默认强度>，<kind>）」
 * 填入输入框**，由人确认后再发送 —— **绝不自动发消息**（安全约束：
 * 目录条目会触发真实的城市场景注入，不能被 Agent 的目录检索结果自动执行）。
 *
 * 本模块刻意做成**纯函数 + 无 React 依赖**，便于零依赖单测直接断言
 * 「点击只回填、不发送」（`__tests__/systemDialogCatalog.test.ts`）。
 */

import type { VirtualCityScenarioCatalogItem } from '@/types/virtualCitySystem';

/** 默认强度（后端缺省时用 3 —— 目录里的「中等强度」）。 */
export const DEFAULT_MAGNITUDE = 3;
/** 强度取值范围（L1–L5）。 */
export const MAGNITUDE_MIN = 1;
export const MAGNITUDE_MAX = 5;

/** 强度归一（缺省 / 越界 → 3）。 */
export function clampMagnitude(raw: unknown): number {
  const n = typeof raw === 'number' ? raw : Number(raw);
  if (!Number.isFinite(n)) return DEFAULT_MAGNITUDE;
  const i = Math.round(n);
  if (i < MAGNITUDE_MIN) return MAGNITUDE_MIN;
  if (i > MAGNITUDE_MAX) return MAGNITUDE_MAX;
  return i;
}

/**
 * 构建「填入输入框」的指令文本。
 *
 * 例：`注入 5 级地震（L3，earthquake__magnitude_5）`
 *
 * @param item 目录条目
 * @param verb 动作动词（由调用方按当前语言注入，如「注入」/「Inject」）
 */
export function buildCatalogPickText(
  item: VirtualCityScenarioCatalogItem,
  verb = '注入',
): string {
  const name = (item?.name ?? '').trim() || (item?.kind ?? '').trim() || '—';
  const kind = (item?.kind ?? '').trim() || '—';
  return `${verb} ${name}（L${clampMagnitude(item?.default_magnitude)}，${kind}）`;
}

/** 点击条目的依赖（**只有回填**，没有发送）。 */
export interface CatalogPickDeps {
  /** 回填输入框。 */
  fillDraft: (text: string) => void;
  /** 动作动词（按当前语言）。 */
  verb?: string;
}

/**
 * 处理「点击目录条目」。
 *
 * ⚠️ 本函数**刻意不接受** sendMessage —— 条目点击永远只是回填输入框，
 * 发送必须由人点击「发送」按钮（或按 Enter）完成。
 */
export function applyCatalogPick(
  item: VirtualCityScenarioCatalogItem,
  deps: CatalogPickDeps,
): string {
  const text = buildCatalogPickText(item, deps.verb);
  deps.fillDraft(text);
  return text;
}

/** 目录检索参数（group / q / limit）→ URL query 串。 */
export function catalogQuery(
  group: string,
  q: string,
  limit: number,
): Record<string, string> {
  const out: Record<string, string> = {};
  if (group && group.trim()) out.group = group.trim();
  if (q && q.trim()) out.q = q.trim();
  out.limit = String(Math.min(200, Math.max(1, Math.floor(limit) || 30)));
  return out;
}
