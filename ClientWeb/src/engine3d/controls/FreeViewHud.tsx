/**
 * engine3d/controls/FreeViewHud — 自由视角的 Web 端操作界面（DOM，不进 R3F 树）。
 *
 * 批次 32「自由视角系统」新增。解决「Canvas 内的相机状态如何被 Canvas 外的按钮操控」
 * ——两者经 `freeViewStore` 单例通信，HUD 不需要知道宿主页面布局（引擎层解耦）。
 *
 * 设计取舍（详见方案 §9）：
 *   - **常驻一行**：不展开帮助时也显示「当前模式 + 速度档 + 碰撞态」，玩家随时知道
 *     「我现在能不能飞、是不是卡在楼里」，无需猜测；
 *   - **分段控件**：`role="radiogroup"` + `aria-checked`，Tab 可达、方向键可切换；
 *   - **文案全部经 props 注入**（`labels`）：引擎层不写死中文，调用方按 i18n 传包；
 *   - `pointer-events: none` 挂在容器上，只给控件本体开 `auto` ⇒ 帮助面板不挡 3D 视口点选。
 *
 * 样式：`./freeview.css` 组件本地 import（不动 `styles/globals.css` 的 @import 顺序，
 * CLAUDE.md §2.1 硬约束 4）。
 */

import { useFreeView, FREE_VIEW_TIERS } from './freeViewStore';
import type { FreeViewMode } from './freeViewStore';
import './freeview.css';

export interface FreeViewLabels {
  /** 分组标题（读屏用）。 */
  title?: string;
  /** 三个模式的短标签，按 `orbit / fly / walk` 顺序。 */
  modes: [string, string, string];
  /** 帮助按钮的无障碍名。 */
  help: string;
  /** 速度档位前缀。 */
  speed: string;
  /** 三个档位的短标签。 */
  tiers: [string, string, string];
  /** 畅通 / 碰撞 两态文案（碰撞指示灯）。 */
  clear: string;
  colliding: string;
  /** 键位帮助表：`[按键, 说明]`。 */
  shortcuts: Array<[string, string]>;
}

const DEFAULT_LABELS: FreeViewLabels = {
  title: 'Camera view',
  modes: ['Orbit', 'Free', 'Walk'],
  help: 'Shortcuts',
  speed: 'Speed',
  tiers: ['Slow', 'Normal', 'Fast'],
  clear: 'Clear',
  colliding: 'Blocked',
  shortcuts: [
    ['V', 'Cycle view mode'],
    ['1 / 2 / 3', 'Orbit / Free / Walk'],
    ['F', 'Toggle orbit ⇄ free'],
    ['R', 'Reset pose'],
    ['[ / ]', 'Speed tier'],
    ['H', 'This panel'],
  ],
};

const MODE_ORDER: FreeViewMode[] = ['orbit', 'fly', 'walk'];

export interface FreeViewHudProps {
  /** 文案包；缺省英文（引擎层不假设宿主语种）。 */
  labels?: Partial<FreeViewLabels>;
  /** 限定可见模式（R12 解锁策略）。缺省三种全开。 */
  allowedModes?: readonly FreeViewMode[];
}

export function FreeViewHud({ labels, allowedModes }: FreeViewHudProps) {
  const L: FreeViewLabels = { ...DEFAULT_LABELS, ...labels };
  const mode = useFreeView((s) => s.mode);
  const tier = useFreeView((s) => s.tier);
  const helpOpen = useFreeView((s) => s.helpOpen);
  const colliding = useFreeView((s) => s.colliding);
  const ready = useFreeView((s) => s.ready);
  const setMode = useFreeView((s) => s.setMode);
  const toggleHelp = useFreeView((s) => s.toggleHelp);

  const modes = allowedModes ?? MODE_ORDER;

  return (
    <div className="freeview-hud" data-ready={ready ? '1' : '0'}>
      <div className="freeview-hud__bar" role="radiogroup" aria-label={L.title}>
        {modes.map((m) => (
          <button
            key={m}
            type="button"
            role="radio"
            aria-checked={mode === m}
            className="freeview-hud__mode"
            data-active={mode === m ? '1' : '0'}
            onClick={() => setMode(m)}
          >
            {L.modes[MODE_ORDER.indexOf(m)]}
          </button>
        ))}
        <button
          type="button"
          className="freeview-hud__help-toggle"
          aria-expanded={helpOpen}
          aria-label={L.help}
          title={L.help}
          onClick={toggleHelp}
        >
          ?
        </button>
      </div>

      <div className="freeview-hud__status">
        <span className="freeview-hud__tier">
          {L.speed} ×{FREE_VIEW_TIERS[tier]}
        </span>
        <span
          className="freeview-hud__probe"
          data-hit={colliding ? '1' : '0'}
          aria-live="polite"
        >
          <i className="freeview-hud__probe-dot" aria-hidden="true" />
          {colliding ? L.colliding : L.clear}
        </span>
      </div>

      {helpOpen && (
        // 列优先双列布局：12 条键位若单列会长到 ~250px，顶边会撞上左上角小地图
        // （z-index 40 > HUD 32）而被裁掉前两行。切成两列后面板高度腰斩。
        <dl
          className="freeview-hud__help"
          style={{ ['--fv-help-rows' as string]: Math.ceil(L.shortcuts.length / 2) }}
        >
          {L.shortcuts.map(([k, d]) => (
            <div className="freeview-hud__help-row" key={k}>
              <dt><kbd>{k}</kbd></dt>
              <dd>{d}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}
