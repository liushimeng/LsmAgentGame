import { useCallback } from 'react';
import { translate, type TKey } from '@/i18n';
import { useI18nStore } from '@/store/i18n.store';

// useT 返回一个翻译函数,随当前语言变化而重渲染。
//   const t = useT();
//   t('header.logout')
//   t('chat.roomTitle', { id: '42' })
export function useT() {
  const lang = useI18nStore((s) => s.lang);
  return useCallback(
    (key: TKey, vars?: Record<string, string | number>) => translate(lang, key, vars),
    [lang],
  );
}
