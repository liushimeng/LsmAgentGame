import { useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { versionService } from '@/services/auth.service';
import { useAuth } from '@/hooks/useAuth';
import { useUiStore } from '@/store/ui.store';
import { useT } from '@/hooks/useT';
import { useWallet } from '@/hooks/useWallet';
import { LanguageSwitcher } from '@/components/layout/LanguageSwitcher';
import { GitLogModal } from '@/components/git/GitLogModal';
import { WikiModal } from '@/components/wiki/WikiModal';
import { SourceStatsModal } from '@/components/source-stats/SourceStatsModal';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import { WalletModal } from '@/components/wallet/WalletModal';
import type { VersionInfo } from '@/types/api';

// AppHeader —— 全站统一的顶部「标题栏 + 工具栏」。
// 标题栏左侧展示品牌与版本号/编译时间；右侧为工具栏（用户信息、登出等）。
// 整行可折叠:折叠时仅保留一个细窄返回/品牌条,工具栏全部隐藏。
export function AppHeader() {
  const isAuth = useAuth((s) => s.isAuthenticated);
  const logout = useAuth((s) => s.logout);
  const location = useLocation();
  const [ver, setVer] = useState<VersionInfo | null>(null);
  const [gitLogOpen, setGitLogOpen] = useState(false);
  const [wikiOpen, setWikiOpen] = useState(false);
  const [srcStatsOpen, setSrcStatsOpen] = useState(false);
  const [logoutConfirmOpen, setLogoutConfirmOpen] = useState(false);

  const collapsed = useUiStore((s) => s.headerCollapsed);
  const toggle = useUiStore((s) => s.toggleHeader);
  const toggleSidebar = useUiStore((s) => s.toggleSidebar);
  const breakpoint = useUiStore((s) => s.breakpoint);
  const t = useT();
  // BUG-R245-P3-01 (2026-08-06): 旁观者路由下隐藏「源码统计」按钮,
  // 避免点击穿透(旁观者页不应继承大厅 header 的开发工具按钮)。
  const isSpectatorRoute = location.pathname.includes('/spectate/');

  // 钱包余额（仅在按钮上展示，余额/流水/领取逻辑全部迁移到 WalletModal）
  const { formattedBalance, balance } = useWallet();
  const [walletOpen, setWalletOpen] = useState(false);

  // 余额变化高亮 —— 结算后 wallet.balance 推送更新余额时，短暂闪烁提示人类"金币变了"。
  // 记录上一次余额，diff 出方向（涨=绿 / 跌=红），触发一次性 CSS 动画后自动清空。
  const prevBalanceRef = useRef<number | null>(null);
  const [balanceFlash, setBalanceFlash] = useState<'gain' | 'loss' | null>(null);
  useEffect(() => {
    if (balance == null) return;
    const prev = prevBalanceRef.current;
    prevBalanceRef.current = balance;
    // 首次拿到余额（prev===null）不闪烁，只有真实变化才提示。
    if (prev == null || prev === balance) return;
    setBalanceFlash(balance > prev ? 'gain' : 'loss');
    const timer = window.setTimeout(() => setBalanceFlash(null), 1200);
    return () => window.clearTimeout(timer);
  }, [balance]);

  useEffect(() => {
    versionService.get()
      .then(setVer)
      .catch(() => setVer(null));
  }, []);

  // 打开/关闭「钱包管理」弹出窗口。
  // 旧实现是 inline dropdown 会把工具栏挤乱（absolute 定位 + 自适应宽度
  // 在窄屏挤兑下触发换行），重构为标准 modal 后该问题消失。
  const handleWalletClick = () => {
    setWalletOpen((cur) => !cur);
  };

  return (
    <header className={'app-header' + (collapsed ? ' app-header--collapsed' : '')}>
      {/* 标题栏 */}
      <div className="app-header__title">
        {/* 移动端汉堡按钮:用于弹出侧栏 */}
        {breakpoint === 'mobile' && (
          <button
            type="button"
            className="ghost app-header__hamburger"
            onClick={toggleSidebar}
            aria-label={t('header.openMenu')}
            title={t('header.openMenu')}
          >
            ☰
          </button>
        )}
        <Link to="/" className="brand">🎮 LsmWebGame</Link>
        {!collapsed && ver && (
          <span className="build-info" title={t('header.buildTime', { time: ver.build_time })}>
            <span className="build-info__ver">{ver.version}</span>
            {ver.git_sha && ver.git_sha !== 'nogit' && (
              <span className="build-info__sha" style={{ opacity: 0.7, marginLeft: 4 }}>
                @{ver.git_sha}
              </span>
            )}
            <span className="build-info__time">{ver.build_time}</span>
          </span>
        )}
      </div>
      {/* 工具栏 */}
      {!collapsed && (
        <div className="app-header__toolbar">
          {!isSpectatorRoute && (
            <button
              className="ghost"
              onClick={() => setSrcStatsOpen(true)}
              aria-label={t('header.srcStats')}
              title={t('header.srcStats')}
            >
              📊 {t('header.srcStats')}
            </button>
          )}
          <button
            className="ghost"
            onClick={() => setWikiOpen(true)}
            aria-label={t('header.wiki')}
            title={t('header.wiki')}
          >
            📚 Wiki
          </button>
          <button
            className="ghost"
            onClick={() => setGitLogOpen(true)}
            aria-label={t('header.gitLog')}
            title={t('header.gitLog')}
          >
            📜 {t('header.gitLog')}
          </button>
          <LanguageSwitcher />
          {isAuth && (
            <>
              <button
                type="button"
                className="wallet-trigger"
                onClick={handleWalletClick}
                title={t('wallet.title' as import('@/i18n').TKey)}
                aria-label={t('wallet.title' as import('@/i18n').TKey)}
              >
                <span className="wallet-trigger__icon">💰</span>
                <span
                  key={balanceFlash ?? 'idle'}
                  className={
                    'wallet-trigger__balance' +
                    (balanceFlash ? ' wallet-trigger__balance--flash-' + balanceFlash : '')
                  }
                >
                  {formattedBalance}
                </span>
              </button>
              <button
                className="ghost"
                onClick={() => setLogoutConfirmOpen(true)}
              >
                {t('header.logout')}
              </button>
            </>
          )}
        </div>
      )}
      {/* 折叠/展开按钮 —— 始终可见,作为统一的"收起/展开标题栏"入口 */}
      <button
        type="button"
        className="ghost app-header__toggle"
        onClick={toggle}
        aria-label={collapsed ? t('header.expand') : t('header.collapse')}
        title={collapsed ? t('header.expand') : t('header.collapse')}
      >
        {collapsed ? '▼' : '▲'}
      </button>
      <GitLogModal open={gitLogOpen} onClose={() => setGitLogOpen(false)} />
      <WikiModal open={wikiOpen} onClose={() => setWikiOpen(false)} />
      <SourceStatsModal open={srcStatsOpen} onClose={() => setSrcStatsOpen(false)} />
      <WalletModal open={walletOpen} onClose={() => setWalletOpen(false)} />
      {logoutConfirmOpen && (
        <ConfirmModal
          messageKey={'header.logoutConfirm' as import('@/i18n').TKey}
          cancelKey={'common.cancel' as import('@/i18n').TKey}
          confirmKey={'common.confirm' as import('@/i18n').TKey}
          onCancel={() => setLogoutConfirmOpen(false)}
          onConfirm={() => {
            setLogoutConfirmOpen(false);
            logout();
          }}
        />
      )}
    </header>
  );
}
