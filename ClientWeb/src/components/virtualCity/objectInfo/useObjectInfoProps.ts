/**
 * useObjectInfoProps — 物件信息交互 handler 工厂（批次 28 · 工作线 B1/B2）。
 *
 * 用法（在组件内部生成并展开到本组件根节点，**禁止**由父层传内联函数 props）：
 *
 *   const info = useObjectInfoProps('prop.trash-can', { anchorY: 0.6 });
 *   return <group {...info} position={...}>…</group>;
 *
 * 契约：
 *   - handler 引用稳定（useCallback + opts ref），配合 React.memo 不破坏 A1 静止 bail out；
 *   - 一律 `e.stopPropagation()`（three 由近及远命中，保住最外层获胜者）；
 *   - hover 置 store + `useCursor('pointer')`；out 只清「自己写的那条」hover；
 *   - click 置 selected（可选 `onSelected` 回调，如 DistrictBlock 继续 onSelect）；
 *   - `e.instanceId != null`（InstancedMesh）自动追加 `#实例` 动态行；
 *   - `idFor` 可按事件动态解析 catalog id（树 road/park 按实例区分）。
 */

import { useCallback, useRef, useState } from 'react';
import type { ThreeEvent } from '@react-three/fiber';
import { useCursor } from '@react-three/drei';
import * as THREE from 'three';
import type { ObjectInfoExtra } from './catalog';
import { extraLabel } from './catalog';
import { useI18nStore } from '@/store/i18n.store';
import { useObjectInfoStore, type ObjectInfoTarget } from './objectInfoStore';

/**
 * drei `<Instances>` 默认 `raycast: () => null`（父 instancedMesh 初始无 geometry
 * 防误命中）——这让实例层收不到任何 pointer 事件。接线实例化组件时把它作为
 * `raycast` prop 传回 `<Instances>`，恢复 THREE.InstancedMesh 原生命中
 * （事件带 `e.instanceId`），渲染/几何零改动。
 */
export const instancedEventsRaycast = THREE.InstancedMesh.prototype.raycast;

/** R3F 事件（pointer 类与 click 的 MouseEvent 两种 wire 形态都收）。 */
type ObjectInfoEvent = ThreeEvent<PointerEvent> | ThreeEvent<MouseEvent>;

export interface UseObjectInfoPropsOpts {
  /** 悬浮卡锚点相对命中点的 y 偏移（默认 0.4）。 */
  anchorY?: number;
  /** 动态附加行（引用每帧变也没关系——走 ref 读取，handler 保持稳定）。 */
  extra?: ObjectInfoExtra[];
  /** 按事件动态解析 catalog id（默认用传入的 id）。 */
  idFor?: (e: ObjectInfoEvent) => string;
  /** false = 不写 hover（如 DistrictBlock 自带 hover 卡，只接 click 详情）。 */
  hover?: boolean;
  /** true = 整体禁用（handler 变 no-op，也不置手型）。 */
  disabled?: boolean;
  /** click 成功置 selected 后回调。 */
  onSelected?: () => void;
}

export interface ObjectInfoHandlers {
  onPointerOver: (e: ThreeEvent<PointerEvent>) => void;
  onPointerOut: (e: ThreeEvent<PointerEvent>) => void;
  // R3F 的 onClick wire 类型是 ThreeEvent<MouseEvent>（fiber EventHandlers）。
  onClick: (e: ThreeEvent<MouseEvent>) => void;
}

const DEFAULT_ANCHOR_Y = 0.4;

/** 由命中事件构建展示目标（extra 尾部自动补 `#实例` 行）。 */
function buildTarget(
  e: ObjectInfoEvent,
  id: string,
  anchorY: number,
  extra?: ObjectInfoExtra[],
): ObjectInfoTarget {
  const lang = useI18nStore.getState().lang;
  const rows: ObjectInfoExtra[] = (extra ?? []).map((r) => ({
    label: extraLabel(r.label, lang),
    value: r.value,
  }));
  if (e.instanceId !== undefined && e.instanceId !== null) {
    rows.push({ label: extraLabel('instance', lang), value: `#${e.instanceId}` });
  }
  return {
    id,
    pos: [e.point.x, e.point.y + anchorY, e.point.z],
    extra: rows.length > 0 ? rows : undefined,
  };
}

export function useObjectInfoProps(
  id: string,
  opts?: UseObjectInfoPropsOpts,
): ObjectInfoHandlers {
  // opts/id 每 render 覆写 ref：handler 引用稳定且始终读到最新 extra/idFor。
  const optsRef = useRef(opts);
  optsRef.current = opts;
  const idRef = useRef(id);
  idRef.current = id;
  /** 本组件最近一次写入 store 的 hover 目标（out 时按引用比对，防止清掉别人的卡）。 */
  const myHoverRef = useRef<ObjectInfoTarget | null>(null);
  const [cursor, setCursor] = useState(false);
  useCursor(cursor);

  const onPointerOver = useCallback((e: ThreeEvent<PointerEvent>) => {
    e.stopPropagation();
    const o = optsRef.current;
    if (o?.disabled) return;
    setCursor(true);
    if (o?.hover === false) return;
    const targetId = o?.idFor ? o.idFor(e) : idRef.current;
    const target = buildTarget(e, targetId, o?.anchorY ?? DEFAULT_ANCHOR_Y, o?.extra);
    myHoverRef.current = target;
    useObjectInfoStore.getState().setHovered(target);
  }, []);

  const onPointerOut = useCallback((e: ThreeEvent<PointerEvent>) => {
    e.stopPropagation();
    setCursor(false);
    const o = optsRef.current;
    if (o?.disabled || o?.hover === false) return;
    const st = useObjectInfoStore.getState();
    // 只清自己写入的那条（pointer 从 A 移到 B 时 out(A) 可能晚于 over(B)）。
    if (myHoverRef.current && st.hovered === myHoverRef.current) {
      st.setHovered(null);
    }
    myHoverRef.current = null;
  }, []);

  const onClick = useCallback((e: ThreeEvent<MouseEvent>) => {
    e.stopPropagation();
    const o = optsRef.current;
    if (o?.disabled) return;
    const targetId = o?.idFor ? o.idFor(e) : idRef.current;
    const target = buildTarget(e, targetId, o?.anchorY ?? DEFAULT_ANCHOR_Y, o?.extra);
    useObjectInfoStore.getState().setSelected(target);
    o?.onSelected?.();
  }, []);

  return { onPointerOver, onPointerOut, onClick };
}
