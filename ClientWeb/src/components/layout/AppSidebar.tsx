import { NavLink } from 'react-router-dom';
import { useUiStore } from '@/store/ui.store';
import { useAuthStore } from '@/store/auth.store';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

// ─── 菜单项 & 分组定义 ───────────────────────────────────────
interface MenuItem {
  to: string;
  labelKey: TKey;
  icon: string;
  end?: boolean;
  /** 最低可见用户类型 (1=普通, 2=管理员, 3=超级管理员) */
  minUserType?: number;
}

interface MenuGroup {
  /** 唯一标识，用于 localStorage 持久化展开状态 */
  id: string;
  /** 分组标题 i18n 键；空字符串表示无标题（全局/其他组） */
  labelKey: TKey | '';
  /** 分组图标 */
  icon: string;
  /** 分组内菜单项 */
  items: MenuItem[];
  /** 默认是否展开 (默认 true) */
  defaultOpen?: boolean;
  /** 分组整体的权限门槛 */
  minUserType?: number;
}

const MENU_GROUPS: MenuGroup[] = [
  // ── 全局项 ──
  {
    id: '_global', labelKey: '', icon: '', items: [
      { to: '/',       labelKey: 'nav.home',  icon: '🏠', end: true },
      { to: '/games',  labelKey: 'nav.games', icon: '🎲' },
    ],
  },

  // ── 传统游戏 ──
  {
    id: 'traditional', labelKey: 'nav.group.traditional', icon: '🎲', items: [
      { to: '/xiangqi',     labelKey: 'nav.xiangqi',     icon: '♟' },
      { to: '/chess',       labelKey: 'nav.chess',       icon: '♚' },
      { to: '/junqi',       labelKey: 'nav.junqi',       icon: '🚩' },
      { to: '/doudizhu',    labelKey: 'nav.doudizhu',    icon: '🃏' },
      { to: '/texasholdem', labelKey: 'nav.texasholdem', icon: '🎰' },
    ],
  },

  // ── Agent 游戏 ──
  {
    id: 'agent', labelKey: 'nav.group.agent', icon: '🤖', items: [
      { to: '/werewolf', labelKey: 'nav.werewolf', icon: '🐺' },
    ],
  },

  // ── 其他 ──
  {
    id: '_other', labelKey: '', icon: '', items: [
      { to: '/profile',      labelKey: 'nav.profile',     icon: '👤' },
      { to: '/admin/users',  labelKey: 'nav.adminUsers',  icon: '👥', minUserType: 1 },
      { to: '/admin/models', labelKey: 'nav.adminModels', icon: '🤖', minUserType: 2 },
      { to: '/about',        labelKey: 'nav.about',       icon: 'ℹ️' },
    ],
  },
];

// ─── AppSidebar ──────────────────────────────────────────────
// 全站统一的左侧「菜单栏」，支持 2 级分组折叠。
// 折叠后仅留图标条；移动端额外作为浮层 drawer 展示。
export function AppSidebar() {
  const collapsed = useUiStore((s) => s.sidebarCollapsed);
  const toggle = useUiStore((s) => s.toggleSidebar);
  const breakpoint = useUiStore((s) => s.breakpoint);
  const groupStates = useUiStore((s) => s.sidebarGroupStates);
  const toggleGroup = useUiStore((s) => s.toggleSidebarGroup);
  const userType = useAuthStore((s) => s.userType);
  const t = useT();

  const isMobile = breakpoint === 'mobile';
  const drawerOpen = isMobile && !collapsed;

  // 按权限过滤分组及组内项
  const filteredGroups = MENU_GROUPS.map((group) => {
    // 分组本身有权限门槛
    if (group.minUserType && (!userType || userType < group.minUserType)) {
      return null;
    }
    const filteredItems = group.items.filter((item) => {
      if (!item.minUserType) return true;
      return userType && userType >= item.minUserType;
    });
    if (filteredItems.length === 0) return null;
    return { ...group, items: filteredItems };
  }).filter(Boolean) as MenuGroup[];

  return (
    <>
      {/* 移动端遮罩 */}
      {isMobile && drawerOpen && (
        <div
          className="app-sidebar__backdrop"
          onClick={toggle}
          aria-hidden="true"
        />
      )}
      <aside
        className={
          'app-sidebar'
          + (collapsed ? ' app-sidebar--collapsed' : '')
          + (isMobile ? ' app-sidebar--mobile' : '')
          + (drawerOpen ? ' app-sidebar--drawer-open' : '')
        }
      >
        <nav className="app-sidebar__nav">
          {filteredGroups.map((group) => {
            const hasHeader = !!group.labelKey;
            // 分组展开状态：默认展开 (undefined 或 true 都视为展开)
            const isOpen = groupStates[group.id] !== false;

            // 无标题分组（全局项、其他项）直接渲染列表
            if (!hasHeader) {
              return (
                <div key={group.id} className="sidebar-group sidebar-group--plain">
                  {group.items.map((item) => (
                    <SidebarItem key={item.to} item={item} isMobile={isMobile} toggle={toggle} t={t} />
                  ))}
                </div>
              );
            }

            return (
              <div
                key={group.id}
                className={'sidebar-group' + (!isOpen ? ' sidebar-group--collapsed' : '')}
              >
                <button
                  type="button"
                  className="sidebar-group__header"
                  onClick={() => toggleGroup(group.id)}
                  title={collapsed ? t(group.labelKey as TKey) : undefined}
                  aria-expanded={isOpen}
                >
                  <span className="sidebar-group__icon">{group.icon}</span>
                  <span className="sidebar-group__label">{t(group.labelKey as TKey)}</span>
                  <span className="sidebar-group__arrow">{isOpen ? '▾' : '▸'}</span>
                </button>
                {isOpen && (
                  <div className="sidebar-group__items">
                    {group.items.map((item) => (
                      <SidebarItem key={item.to} item={item} isMobile={isMobile} toggle={toggle} t={t} />
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </nav>
        {!isMobile && (
          <button
            type="button"
            className="ghost app-sidebar__toggle"
            onClick={toggle}
            aria-label={collapsed ? t('sidebar.expand') : t('sidebar.collapse')}
            title={collapsed ? t('sidebar.expand') : t('sidebar.collapse')}
          >
            {collapsed ? '▶' : '◀'}
          </button>
        )}
      </aside>
    </>
  );
}

// ─── 单个菜单项渲染 ─────────────────────────────────────────
function SidebarItem({
  item,
  isMobile,
  toggle,
  t,
}: {
  item: MenuItem;
  isMobile: boolean;
  toggle: () => void;
  t: (key: TKey) => string;
}) {
  return (
    <NavLink
      to={item.to}
      end={item.end}
      onClick={() => { if (isMobile) toggle(); }}
      className={({ isActive }) =>
        'sidebar-link' + (isActive ? ' sidebar-link--active' : '')
      }
      title={t(item.labelKey)}
    >
      <span className="sidebar-link__icon">{item.icon}</span>
      <span className="sidebar-link__label">{t(item.labelKey)}</span>
    </NavLink>
  );
}
