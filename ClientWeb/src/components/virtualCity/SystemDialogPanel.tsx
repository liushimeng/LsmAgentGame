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
 *   - `✕` 关闭窗口（会话保留在服务端，重开经 history 帧恢复）。
 *
 * 错误展示（§7.1 双通道）：`error` 事件 → 消息内联红条 + `reportGlobalError`
 * 全局 toast（后者在 `useVirtualCitySystemAgent` 内上报）。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import type { KeyboardEvent, MutableRefObject } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { useVirtualCitySystemAgent } from '@/hooks/useVirtualCitySystemAgent';
import { freeViewApi } from '@/engine3d/controls/freeViewStore';
import { cityShowApi, useCityShowStore } from '@/components/virtualCity/cityShowStore';
import {
  applyDisplayCommand,
  type CityShowDeps,
} from '@/components/virtualCity/cityShowAdapter';
import { SystemDialogMessage } from '@/components/virtualCity/SystemDialogMessage';
import type { VirtualCityFocusTarget } from '@/components/virtualCity/VirtualCityCityMap';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';

/** 标注浮动标签自动消隐时长（方案 §4.4：8s）。 */
const ANNOTATION_TTL_MS = 8000;
/** 自动滚底的「贴底」容差（px）。 */
const STICK_BOTTOM_TOLERANCE = 48;

/**
 * Agent `annotate` 指令的 DOM 浮动标签（8s 自动消隐）。
 * 独立成组件：窗口关闭 / 最小化后标注仍要可见（视角已被拉动是既成事实）。
 */
function CityAnnotationLabel() {
  const annotation = useCityShowStore((s) => s.annotation);
  const clearAnnotation = useCityShowStore((s) => s.clearAnnotation);
  const timerRef = useRef<number | null>(null);

  useEffect(() => {
    if (!annotation) return undefined;
    const ts = annotation.ts;
    timerRef.current = window.setTimeout(() => {
      // 仅当标注未被更新（ts 未变）才消除，避免新标注被旧定时器误清。
      if (useCityShowStore.getState().annotation?.ts === ts) clearAnnotation();
    }, ANNOTATION_TTL_MS);
    return () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [annotation, clearAnnotation]);

  if (!annotation) return null;
  return (
    <div
      className="virtualCity-sysdialog-annotation"
      role="status"
      data-testid="virtualCity-system-annotation"
    >
      📍 {annotation.text}
    </div>
  );
}

interface Props {
  roomId: string;
  /** 窗口是否展开（false = 已关闭，会话仍在服务端）。 */
  open: boolean;
  /**
   * 批次81 §8 一键回退：后端 `[city_system] enabled=false`（game.state
   * `system_agent_enabled === false`）⇒ 面板内联提示 + 禁发。缺省 false。
   */
  disabled?: boolean;
  onClose: () => void;
  /** 与页面 `handleSelectDistrict` 共用的相机聚焦 ref。 */
  focusRef: MutableRefObject<VirtualCityFocusTarget | null>;
}

export function SystemDialogPanel({ roomId, open, disabled = false, onClose, focusRef }: Props) {
  const t = useT();
  const setSelectedDistrict = useVirtualCityStore((s) => s.setSelectedDistrict);

  // 显示指令 → 既有 3D 控制（focusRef / freeViewApi / cityShowStore）。
  const handleShowCommand = useCallback(
    (cmd: VirtualCityDisplayCommand) => {
      const deps: CityShowDeps = {
        focusRef,
        setSelectedDistrict,
        setMode: freeViewApi.setMode,
        setAnnotation: cityShowApi.setAnnotation,
        clearAnnotation: cityShowApi.clearAnnotation,
      };
      applyDisplayCommand(cmd, deps);
    },
    [focusRef, setSelectedDistrict],
  );

  const {
    messages, busy, connected, minimized, setMinimized, sendMessage, reset,
  } = useVirtualCitySystemAgent(roomId, { focusRef, onShowCommand: handleShowCommand });

  const [draft, setDraft] = useState('');
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);

  // 换房时退出最小化态（最小化是单例 store 的 UI 态，不应跨房残留）。
  useEffect(() => {
    setMinimized(false);
  }, [roomId, setMinimized]);

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

  return (
    <>
      <div
        className="virtualCity-sysdialog"
        role="dialog"
        aria-label={t('virtualCity.systemDialog.title')}
        data-testid="virtualCity-system-dialog"
      >
        <div className="virtualCity-sysdialog__head">
          <span className="virtualCity-sysdialog__title">
            💬 {t('virtualCity.systemDialog.title')}
          </span>
          <span className="virtualCity-sysdialog__subtitle">
            {t('virtualCity.systemDialog.subtitle')}
          </span>
          <span
            className={
              'virtualCity-sysdialog__conn'
              + (connected ? ' virtualCity-sysdialog__conn--on' : '')
            }
            title={connected
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
