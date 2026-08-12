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
  let s: string = dict[key] ?? DICTS[DEFAULT_LANG][key] ?? key;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.replace(new RegExp(`\\{${k}\\}`, 'g'), String(v));
    }
  }
  return s;
}

export type { Dict, TKey };
