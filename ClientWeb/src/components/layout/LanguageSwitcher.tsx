import { LANG_LABELS, SUPPORTED, type Lang } from '@/i18n';
import { useI18nStore } from '@/store/i18n.store';
import { useT } from '@/hooks/useT';

// LanguageSwitcher —— 标题栏工具栏中的语言切换下拉。
// 未登录也可用(切换存本地);登录后切换会同步到服务器。
export function LanguageSwitcher() {
  const lang = useI18nStore((s) => s.lang);
  const setLang = useI18nStore((s) => s.setLang);
  const t = useT();

  return (
    <select
      className="lang-switcher"
      value={lang}
      onChange={(e) => setLang(e.target.value as Lang)}
      aria-label={t('header.language')}
      title={t('header.language')}
    >
      {SUPPORTED.map((l) => (
        <option key={l} value={l}>
          {LANG_LABELS[l]}
        </option>
      ))}
    </select>
  );
}
