import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { DEFAULT_LANG, isLang, type Lang } from '@/i18n';
import { userService } from '@/services/auth.service';
import { useAuthStore } from '@/store/auth.store';

export interface I18nState {
  lang: Lang;
  // setLang:用户主动切换语言。本地立即生效;若已登录则后台落库(失败仅告警)。
  setLang: (lang: Lang) => void;
  // applyServerLang:登录/注册成功后调用,用服务器保存的语言覆盖本地。
  applyServerLang: (lang: string | null | undefined) => void;
}

// 同步 <html lang> 便于无障碍与浏览器本地化。
function syncHtmlLang(lang: Lang) {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = lang;
  }
}

export const useI18nStore = create<I18nState>()(
  persist(
    (set, get) => ({
      lang: DEFAULT_LANG,

      setLang: (lang) => {
        if (lang === get().lang) {
          syncHtmlLang(lang);
          return;
        }
        set({ lang });
        syncHtmlLang(lang);
        // 已登录则把偏好写回服务器,实现「换浏览器登录仍保留」。
        if (useAuthStore.getState().isAuthenticated) {
          userService.updateLanguage(lang).catch(() => {
            // 落库失败不回滚本地选择;下次登录会以服务器值为准。
            console.warn('[i18n] failed to persist language to server');
          });
        }
      },

      applyServerLang: (lang) => {
        if (isLang(lang) && lang !== get().lang) {
          set({ lang });
          syncHtmlLang(lang);
        }
      },
    }),
    {
      name: 'lsm.lang',
      partialize: (s) => ({ lang: s.lang }),
    },
  ),
);
