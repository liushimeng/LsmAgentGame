/**
 * SystemDialogPanel —— 「💬 系统对话」浮动对话框（批次 81 方案 §4）。
 *
 * 取代原「🎛 场景推演台」（ScenarioPanel）：人类用自然语言与
 * `LsmAgentGame-City-System` 主 Agent 对话 —— 查看城市状态、注入场景、
 * 控制模拟、并把 3D 视角拉到事发地。**对话即控制面**。
 *
 * 形态（需求 7）：
 *   - `position: fixed` 浮动在 3D 地图左下，不遮挡右侧实时看板；
 *   - `width: min(1280px, 92vw)` + `aspect-ratio: 16 / 9`（默认 720p 级）；
 *   - `z-index: 60`（高于 3D 画布 / 小地图 40 / FreeViewHud 32，低于 modal 200）；
 *   - 头部 `—` 最小化 → 收起为一条浮动条（点击恢复），**不断线**
 *     （WS 连接由 AppLayout 唯一持有，§16）；
 *   - `✕` 关闭窗口（会话保留在服务端，重开经 history 帧恢复）；
 *   - 批次83 §3.1：header 为拖拽把手（pointer 捕获；拖拽中直改元素 style，
 *     不 setState），pointerup 提交 store + 持久化 localStorage
 *     `vc-sysdialog-pos-v1`；双击 header 复位默认位；窄屏（≤900px）禁用拖拽。
 *
 * 错误展示（§7.1 双通道）：`error` 事件 → 消息内联红条 + `reportGlobalError`
 * 全局 toast（后者在 `useVirtualCitySystemAgent` 内上报）。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import type {
  KeyboardEvent,
  MouseEvent as ReactMouseEvent,
  MutableRefObject,
  PointerEvent as ReactPointerEvent,
} from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import {
  DIALOG_POS_STORAGE_KEY,
  useVirtualCitySystemStore,
} from '@/store/virtualCitySystem.store';
import type { VirtualCitySystemDialogPos } from '@/store/virtualCitySystem.store';
import { useVirtualCitySystemAgent } from '@/hooks/useVirtualCitySystemAgent';
import { freeViewApi } from '@/engine3d/controls/freeViewStore';
import {
  CITY_LAYER_NAMES,
  cityCameraApi,
  cityEnvApi,
  cityEntityApi,
  cityPathApi,
  cityShowApi,
  resolveCityFilterCategory,
  useCityShowStore,
  type CityLayerName,
} from '@/components/virtualCity/cityShowStore';
import {
  applyDisplayCommand,
  type CityShowDeps,
} from '@/components/virtualCity/cityShowAdapter';
import {
  SystemDialogMessage,
  noticeText,
} from '@/components/virtualCity/SystemDialogMessage';
import { CityAnnotationLabel } from '@/components/virtualCity/cityLabels';
import { clearVehicleRegistry } from '@/components/virtualCity/cityVehicleRegistry';
import { SystemDialogCatalogPanel } from '@/components/virtualCity/SystemDialogCatalogPanel';
import { SystemDialogInfoPanel } from '@/components/virtualCity/SystemDialogInfoPanel';
import type { VirtualCityFocusTarget } from '@/components/virtualCity/VirtualCityCityMap';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';

/** 自动滚底的「贴底」容差（px）。 */
const STICK_BOTTOM_TOLERANCE = 48;

/** 批次83 §3.1：窄屏断点 —— 命中时禁用拖拽并忽略持久化位置（保持贴底满宽形态）。 */
const NARROW_VIEWPORT_QUERY = '(max-width: 900px)';
/** 拖拽钳制：水平方向至少保留的可抓回宽度（px）。 */
const DRAG_KEEP_X = 120;
/** 拖拽钳制：垂直方向至少保留的 header 高度（px）。 */
const DRAG_KEEP_Y = 56;
/** 「原地点击」与「拖拽」的分界位移（px，曼哈顿距离；吸收鼠标点击抖动）。 */
const DRAG_MOVE_THRESHOLD = 3;

/** 视口钳制（批次83 §3.1）：x ∈ [-(w-120), innerWidth-120]，y ∈ [0, innerHeight-56]。 */
function clampDialogPos(x: number, y: number, w: number): VirtualCitySystemDialogPos {
  const minX = DRAG_KEEP_X - w; // 即 -(w - 120)
  const maxX = window.innerWidth - DRAG_KEEP_X;
  const maxY = window.innerHeight - DRAG_KEEP_Y;
  return {
    x: Math.min(Math.max(x, minX), maxX),
    y: Math.min(Math.max(y, 0), maxY),
  };
}

/** 拖拽会话（ref 承载：pointermove 高频路径不 setState）。 */
interface DragSession {
  pointerId: number;
  startX: number;
  startY: number;
  /** 起步时窗口左上角（getBoundingClientRect）。 */
  baseX: number;
  baseY: number;
  /** 是否发生过有效位移（原地点击不提交位置）。 */
  moved: boolean;
}

/** header 内的按钮（—/✕）不是拖拽起点，也不是双击复位目标。 */
function isHeadButtonTarget(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest('button') !== null;
}

/**
 * Agent `annotate` 指令的 DOM 浮动标签 —— 批次 85 起抽到 `cityLabels.tsx`
 * （受 `labels` 图层闸门控制是否渲染，单测可直渲 DOM），此处仅引用。
 */

/**
 * 批次 84：显示指令提示文案 —— 批次 85 移至 `SystemDialogMessage.tsx`
 * （顶栏横幅与消息流留痕共用同一套文案逻辑，避免两处口径漂移）。
 */

interface Props {
  roomId: string;
  /** 窗口是否展开（false = 已关闭，会话仍在服务端）。 */
  open: boolean;
  /**
   * 批次81 §8 一键回退：后端 `[city_system] enabled=false`（game.state
   * `system_agent_enabled === false`）⇒ 面板内联提示 + 禁发。缺省 false。
   *
   * 批次 87 §4.6：**由页面统一解析**（`resolveSystemAgentDisabled`）——
   * 两路信号取或：① `game.state.system_agent_enabled` **明确 false**（字段缺失
   * 视为可用，向后兼容旧后端）；② 本地停用态（服务端回 35059「已停用」）。
   */
  disabled?: boolean;
  onClose: () => void;
  /** 与页面 `handleSelectDistrict` 共用的相机聚焦 ref。 */
  focusRef: MutableRefObject<VirtualCityFocusTarget | null>;
}

export function SystemDialogPanel({ roomId, open, disabled = false, onClose, focusRef }: Props) {
  const t = useT();
  const setSelectedDistrict = useVirtualCityStore((s) => s.setSelectedDistrict);
  /**
   * 批次 84：显示指令提示（未支持 / 未接引擎 / 参数非法）—— 内联红条。
   * 存在 cityShowStore（跨层单例）而非组件本地 state：提示由 adapter 产生，
   * 与显示指令的其它状态同源（场景组件亦可读）。
   */
  const notice = useCityShowStore((s) => s.notice);
  const setNotice = useCityShowStore((s) => s.setNotice);
  // 批次 85 C2：open 是 props —— onNotice 闭包读不到它的即时值（handleShowCommand
  // 的 useCallback 依赖表刻意精简），ref 承载、回调里读 current。
  const openRef = useRef(open);
  openRef.current = open;

  // 显示指令 → 既有 3D 控制（focusRef / freeViewApi / cityShowStore / cityTimeStore）。
  const handleShowCommand = useCallback(
    (cmd: VirtualCityDisplayCommand) => {
      const deps: CityShowDeps = {
        focusRef,
        setSelectedDistrict,
        setMode: freeViewApi.setMode,
        setAnnotation: cityShowApi.setAnnotation,
        clearAnnotation: cityShowApi.clearAnnotation,
        // 批次 84：高亮颜色（highlight_district 的 color 字段真正生效）。
        setHighlight: cityShowApi.setHighlight,
        layerApi: {
          setLayerVisibility: (layer, visible) => {
            if (!CITY_LAYER_NAMES.includes(layer as CityLayerName)) return false;
            cityShowApi.setLayerVisibility(layer as CityLayerName, visible);
            return true;
          },
          setFilter: (category, mode) => {
            if (!category) {
              cityShowApi.clearFilter();
              return true;
            }
            // 批次 85：类别词汇 = 图层名 / 物件类别 / 车辆类型（cityFilterCategories
            // 统一口径）；全不中才回 unknown_category。
            if (resolveCityFilterCategory(category).scope === 'unknown') return false;
            cityShowApi.setFilter(category, mode);
            return true;
          },
        },
        envApi: cityEnvApi,
        entityApi: cityEntityApi,
        pathApi: { flyPath: (points) => cityPathApi.flyPath(points, focusRef) },
        cameraApi: cityCameraApi,
        clearOverrides: () => {
          cityEnvApi.clear();
          cityShowApi.resetLayers();
          cityShowApi.clearFilter();
          cityShowApi.clearHighlight();
        },
        onNotice: (n) => {
          setNotice({ ...n, ts: Date.now() });
          // 批次 85 C2：关窗 / 最小化期间横幅不渲染 ⇒ 提示转入消息流**留痕**
          // （role='notice' 行，历史里永久可见，不丢提示）。
          if (!openRef.current || useVirtualCitySystemStore.getState().minimized) {
            useVirtualCitySystemStore.getState().appendNotice(n);
          }
        },
      };
      applyDisplayCommand(cmd, deps);
    },
    [focusRef, setSelectedDistrict],
  );

  const {
    messages, busy, transportConnected, minimized, setMinimized, sendMessage, reset,
  } = useVirtualCitySystemAgent(roomId, { focusRef, onShowCommand: handleShowCommand });

  const [draft, setDraft] = useState('');
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);

  // 换房时退出最小化态（最小化是单例 store 的 UI 态，不应跨房残留）。
  useEffect(() => {
    setMinimized(false);
  }, [roomId, setMinimized]);

  // 批次 84：换房时清掉**跨房残留**的显示侧单例态 ——
  // 实体位置注册表（旧房的居民坐标）与进行中的折线飞行。
  // （昼夜 / 天气 / 流速覆盖由 cityTimeStore.resetCityTime 在页面换房时清。）
  // 批次 85：车辆注册表同清（组件卸载的 unregister 兜底之外的显式清残留）。
  useEffect(() => {
    cityEntityApi.clear();
    cityPathApi.cancel();
    clearVehicleRegistry();
  }, [roomId]);

  // 新消息到达时贴底滚动（仅当用户本来就在底部附近，避免打断往上翻历史）。
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (stickToBottomRef.current) el.scrollTop = el.scrollHeight;
  }, [messages, minimized]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    stickToBottomRef.current =
      el.scrollHeight - el.scrollTop - el.clientHeight < STICK_BOTTOM_TOLERANCE;
  }, []);

  const submit = useCallback(() => {
    const text = draft.trim();
    // disabled（§8 一键回退）时 Enter 路径同样禁发，防止绕过禁用按钮。
    if (!text || busy || disabled) return;
    sendMessage(text);
    setDraft('');
    // 发送后立即回到底部，让 Agent 的回复进入视野。
    stickToBottomRef.current = true;
  }, [draft, busy, disabled, sendMessage]);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        submit();
      }
    },
    [submit],
  );

  const handleClear = useCallback(() => {
    reset();
    setDraft('');
    stickToBottomRef.current = true;
  }, [reset]);

  /**
   * 批次 84：目录条目点击 ⇒ **只回填输入框**（不发送）。
   * 发送必须由人点「发送」/ 回车 —— 场景注入不可被目录条目自动触发。
   */
  const handleCatalogPick = useCallback((text: string) => {
    setDraft(text);
    stickToBottomRef.current = true;
  }, []);

  // ── 批次83 §3.1 浮窗拖拽：header 为把手 ──────────────────────────────
  const dialogPos = useVirtualCitySystemStore((s) => s.dialogPos);
  const setDialogPos = useVirtualCitySystemStore((s) => s.setDialogPos);

  const rootRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<DragSession | null>(null);
  const [dragging, setDragging] = useState(false);

  // 窄屏（≤900px）响应式跟踪：命中时渲染忽略持久化位置（CSS 贴底满宽形态接管）。
  const [narrowViewport, setNarrowViewport] = useState(
    () => window.matchMedia(NARROW_VIEWPORT_QUERY).matches,
  );

  useEffect(() => {
    const mq = window.matchMedia(NARROW_VIEWPORT_QUERY);
    const onChange = (ev: MediaQueryListEvent) => setNarrowViewport(ev.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);

  // 批次83 §3.2 激活语义：open 变 true ⇒ 退出最小化
  // （覆盖「关闭态 → 打开」与「最小化态 → 还原」两条路径）。
  useEffect(() => {
    if (open) setMinimized(false);
  }, [open, setMinimized]);

  // 持久化位置重钳制（§3.1）：展开窗口挂载时 + 视口 resize 时拉回视口。
  // 关闭 / 最小化态没有可量的根元素（rootRef 为空），展开时 effect 重跑补钳。
  useEffect(() => {
    if (!open || minimized) return undefined;
    const reclamp = () => {
      const pos = useVirtualCitySystemStore.getState().dialogPos;
      if (!pos) return;
      const w = rootRef.current?.offsetWidth ?? 0;
      const next = clampDialogPos(pos.x, pos.y, w);
      if (next.x !== pos.x || next.y !== pos.y) setDialogPos(next);
    };
    reclamp();
    window.addEventListener('resize', reclamp);
    return () => window.removeEventListener('resize', reclamp);
  }, [open, minimized, setDialogPos]);

  const handleHeadPointerDown = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0 || dragRef.current) return;
    // 窄屏禁用拖拽（媒体查询已钉死贴底满宽布局）。
    if (window.matchMedia(NARROW_VIEWPORT_QUERY).matches) return;
    // header 内的 —/✕ 按钮不作为拖拽起点。
    if (isHeadButtonTarget(e.target)) return;
    const root = rootRef.current;
    if (!root) return;
    // 起步即把当前 CSS 定位（默认位 / 持久化位）换算成显式 left/top（bottom 置 auto），
    // 之后拖拽全程在该坐标系内直改 style，杜绝起步跳变。
    const rect = root.getBoundingClientRect();
    root.style.left = `${rect.left}px`;
    root.style.top = `${rect.top}px`;
    root.style.bottom = 'auto';
    dragRef.current = {
      pointerId: e.pointerId,
      startX: e.clientX,
      startY: e.clientY,
      baseX: rect.left,
      baseY: rect.top,
      moved: false,
    };
    setDragging(true);
    e.currentTarget.setPointerCapture(e.pointerId);
  }, []);

  const handleHeadPointerMove = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    const d = dragRef.current;
    const root = rootRef.current;
    if (!d || !root || e.pointerId !== d.pointerId) return;
    const dx = e.clientX - d.startX;
    const dy = e.clientY - d.startY;
    // 吸收点击抖动：未越过阈值视为原地点击（不移动、不标记 moved）。
    if (!d.moved && Math.abs(dx) + Math.abs(dy) <= DRAG_MOVE_THRESHOLD) return;
    d.moved = true;
    // 拖拽期间直接改元素 style（不 setState）；仅收尾提交 store（§3.1）。
    const next = clampDialogPos(d.baseX + dx, d.baseY + dy, root.offsetWidth);
    root.style.left = `${next.x}px`;
    root.style.top = `${next.y}px`;
  }, []);

  // pointerup 与 pointercancel 共用收尾：后者坐标为最后已知位置，同样可提交。
  const handleHeadPointerUp = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      const d = dragRef.current;
      if (!d || e.pointerId !== d.pointerId) return;
      dragRef.current = null;
      setDragging(false);
      if (e.currentTarget.hasPointerCapture(e.pointerId)) {
        e.currentTarget.releasePointerCapture(e.pointerId);
      }
      const root = rootRef.current;
      if (!root) return;
      if (!d.moved) {
        // 原地点击：回退起步时写入的内联定位覆盖（仅默认位时需要），不落库。
        if (useVirtualCitySystemStore.getState().dialogPos === null) {
          root.style.left = '';
          root.style.top = '';
          root.style.bottom = '';
        }
        return;
      }
      const next = clampDialogPos(
        d.baseX + (e.clientX - d.startX),
        d.baseY + (e.clientY - d.startY),
        root.offsetWidth,
      );
      setDialogPos(next);
      try {
        window.localStorage.setItem(DIALOG_POS_STORAGE_KEY, JSON.stringify(next));
      } catch {
        // 持久化失败（隐私模式 / 配额满）静默降级：本会话内位置仍生效。
      }
    },
    [setDialogPos],
  );

  // 双击 header 复位默认位（清除持久化位置）。
  const handleHeadDoubleClick = useCallback(
    (e: ReactMouseEvent<HTMLDivElement>) => {
      if (isHeadButtonTarget(e.target)) return;
      setDialogPos(null);
      try {
        window.localStorage.removeItem(DIALOG_POS_STORAGE_KEY);
      } catch {
        // 清理失败不影响内存态（已复位默认位）。
      }
      const root = rootRef.current;
      if (root) {
        root.style.left = '';
        root.style.top = '';
        root.style.bottom = '';
      }
    },
    [setDialogPos],
  );

  // 窗口关闭（✕）后仅隐藏，会话保留；标注标签仍需渲染（视角已动）。
  if (!open) return <CityAnnotationLabel />;

  if (minimized) {
    return (
      <>
        <button
          type="button"
          className="virtualCity-sysdialog-minbar"
          onClick={() => setMinimized(false)}
          title={t('virtualCity.systemDialog.restore')}
          aria-label={t('virtualCity.systemDialog.restore')}
          data-testid="virtualCity-system-dialog-restore"
        >
          💬 {t('virtualCity.systemDialog.minimizedBar')}
          {busy && <span className="virtualCity-sysdialog-minbar__dot" aria-hidden="true">·</span>}
        </button>
        <CityAnnotationLabel />
      </>
    );
  }

  // 位置渲染（§3.1）：持久化位存在且非窄屏 ⇒ 显式 left/top（bottom 置 auto）；
  // 否则不写 inline style —— CSS 默认位（left/bottom 16px）或窄屏贴底满宽媒体查询接管。
  const posStyle: { left: number; top: number; bottom: 'auto' } | undefined =
    !narrowViewport && dialogPos
      ? { left: dialogPos.x, top: dialogPos.y, bottom: 'auto' }
      : undefined;

  return (
    <>
      <div
        ref={rootRef}
        className="virtualCity-sysdialog"
        role="dialog"
        aria-label={t('virtualCity.systemDialog.title')}
        style={posStyle}
        data-testid="virtualCity-system-dialog"
      >
        <div
          className={
            'virtualCity-sysdialog__head'
            + (dragging ? ' virtualCity-sysdialog__head--dragging' : '')
          }
          onPointerDown={handleHeadPointerDown}
          onPointerMove={handleHeadPointerMove}
          onPointerUp={handleHeadPointerUp}
          onPointerCancel={handleHeadPointerUp}
          onDoubleClick={handleHeadDoubleClick}
          title={t('virtualCity.systemDialog.dragHint')}
          aria-label={t('virtualCity.systemDialog.dragHint')}
          data-testid="virtualCity-system-dialog-head"
        >
          <span className="virtualCity-sysdialog__title">
            💬 {t('virtualCity.systemDialog.title')}
          </span>
          <span className="virtualCity-sysdialog__subtitle">
            {t('virtualCity.systemDialog.subtitle')}
          </span>
          {/* 批次 87 P1-5：连接指示器改读**传输层**连接态（`wsClient.status`）——
              `store.connected` 只在收到 agent_start / agent_end 才为 true ⇒
              空闲时误显「系统 Agent 未连接」（误导性告警）。 */}
          <span
            className={
              'virtualCity-sysdialog__conn'
              + (transportConnected ? ' virtualCity-sysdialog__conn--on' : '')
            }
            title={transportConnected
              ? t('virtualCity.systemDialog.connected' as TKey)
              : t('virtualCity.systemDialog.disconnected' as TKey)}
            data-testid="virtualCity-system-dialog-conn"
          />
          <button
            type="button"
            className="virtualCity-sysdialog__btn"
            onClick={() => setMinimized(true)}
            title={t('virtualCity.systemDialog.minimize')}
            aria-label={t('virtualCity.systemDialog.minimize')}
            data-testid="virtualCity-system-dialog-minimize"
          >
            —
          </button>
          <button
            type="button"
            className="virtualCity-sysdialog__btn"
            onClick={onClose}
            title={t('virtualCity.systemDialog.close')}
            aria-label={t('virtualCity.systemDialog.close')}
            data-testid="virtualCity-system-dialog-close"
          >
            ✕
          </button>
        </div>

        {/* 批次 84 §5.1：目录浏览器 + 系统信息面板（窗口内可折叠区，默认收起；
            目录条目点击**只回填输入框**，不自动发送）。 */}
        <div className="virtualCity-sysdialog__sections">
          <SystemDialogCatalogPanel onPick={handleCatalogPick} />
          <SystemDialogInfoPanel />
        </div>

        {/* 批次 84：显示指令提示（未支持 / 未接引擎 / 参数非法）—— 显式反馈，
            杜绝「点了没反应」。 */}
        {notice && (
          <div
            className="virtualCity-sysdialog__notice"
            role="alert"
            data-testid="virtualCity-system-notice"
          >
            <span className="virtualCity-sysdialog__notice-text">
              {noticeText(t, notice)}
            </span>
            <button
              type="button"
              className="virtualCity-sysdialog__notice-close"
              onClick={() => setNotice(null)}
              aria-label={t('virtualCity.systemDialog.noticeDismiss' as TKey)}
            >
              {t('virtualCity.systemDialog.noticeDismiss' as TKey)}
            </button>
          </div>
        )}

        <div
          className="virtualCity-sysdialog__body"
          ref={scrollRef}
          onScroll={handleScroll}
          data-testid="virtualCity-system-dialog-body"
        >
          {messages.length === 0 ? (
            <div className="virtualCity-sysdialog__empty">
              <div className="virtualCity-sysdialog__empty-title">
                {t('virtualCity.systemDialog.emptyTitle')}
              </div>
              <div className="virtualCity-sysdialog__empty-hint">
                {t('virtualCity.systemDialog.emptyHint')}
              </div>
            </div>
          ) : (
            messages.map((m) => <SystemDialogMessage key={m.id} message={m} />)
          )}
        </div>

        {/* §8 一键回退：后端 [city_system] enabled=false ⇒ 打开态显示内联提示 + 禁发。 */}
        {disabled && (
          <div
            className="virtualCity-sysdialog__disabled"
            role="status"
            data-testid="virtualCity-system-dialog-disabled"
          >
            ⚠️ {t('virtualCity.systemDialog.disabled' as TKey)}
          </div>
        )}

        <div className="virtualCity-sysdialog__input">
          <textarea
            className="virtualCity-sysdialog__textarea"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={t('virtualCity.systemDialog.inputPlaceholder')}
            rows={2}
            data-testid="virtualCity-system-dialog-input"
          />
          <div className="virtualCity-sysdialog__actions">
            <button
              type="button"
              className="btn btn-secondary virtualCity-sysdialog__clear"
              onClick={handleClear}
              disabled={busy || messages.length === 0}
              data-testid="virtualCity-system-dialog-clear"
            >
              {t('virtualCity.systemDialog.clear')}
            </button>
            <button
              type="button"
              className="btn btn-primary virtualCity-sysdialog__send"
              onClick={submit}
              disabled={busy || !draft.trim() || disabled}
              data-testid="virtualCity-system-dialog-send"
            >
              {busy
                ? t('virtualCity.systemDialog.busy')
                : t('virtualCity.systemDialog.send')}
            </button>
          </div>
        </div>
      </div>
      <CityAnnotationLabel />
    </>
  );
}

export default SystemDialogPanel;
