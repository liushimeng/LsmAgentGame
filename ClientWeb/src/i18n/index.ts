import type { Dict, TKey } from './types';
import zhCN from './locales/zh-CN';
import en from './locales/en';
import ja from './locales/ja';

// 受支持的语言代码。必须与服务器 service.SupportedLanguages 保持一致。
export type Lang = 'zh-CN' | 'en' | 'ja';

// 默认语言 —— 中文。必须与服务器 service.DefaultLanguage 一致。
export const DEFAULT_LANG: Lang = 'zh-CN';

// 语言切换器中展示的标签(用各自语言书写)。
export const LANG_LABELS: Record<Lang, string> = {
  'zh-CN': '中文',
  en: 'English',
  ja: '日本語',
};

export const SUPPORTED: Lang[] = ['zh-CN', 'en', 'ja'];

const DICTS: Record<Lang, Dict> = {
  'zh-CN': zhCN,
  en,
  ja,
};

export function isLang(v: unknown): v is Lang {
  return typeof v === 'string' && (SUPPORTED as string[]).includes(v);
}

// translate 按指定语言查 key,缺失则回退默认语言,再回退 key 本身。
// vars 用于替换形如 {name} 的占位符。
export function translate(lang: Lang, key: TKey, vars?: Record<string, string | number>): string {
  const dict = DICTS[lang] ?? DICTS[DEFAULT_LANG];
  // ⚠ **key 可能是 undefined**：调用方常写 `t(SOME_MAP[x] as TKey)`，而 SOME_MAP
  //   在后端新增枚举值时会漏项 ⇒ 旧实现 `dict[key] ?? DICTS[DEFAULT_LANG][key] ?? key`
  //   在 key 为 undefined 时会返回 **undefined（不是字符串）** ⇒ 下游任何
  //   `label.includes(...)` / `.slice(...)` 直接抛 TypeError ⇒ 整个面板进
  //   ErrorBoundary 白屏（批次69 实测：flow_stat 的 family 节点让「经济」维度整块崩）。
  //   ⇒ 这里兜底成空串：**任何映射表漏项都只降级为「空标签」，绝不崩页面**。
  const safeKey: string = typeof key === 'string' ? key : '';
  let s: string =
    dict[safeKey as TKey] ?? DICTS[DEFAULT_LANG][safeKey as TKey] ?? safeKey;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.replace(new RegExp(`\\{${k}\\}`, 'g'), String(v));
    }
  }
  return s;
}

export type { Dict, TKey };
