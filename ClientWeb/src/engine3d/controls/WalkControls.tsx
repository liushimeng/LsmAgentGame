/**
 * engine3d/controls/WalkControls — 街景漫游控制器（第一人称 WASD + 鼠标拖拽环视）。
 *
 * 22-3D世界升级与引擎模块化新增：3D 世界的标志性能力——从「2.5D 锁定俯视」
 * 解放为可压到街面高度的自由漫游视角。
 *
 * 交互契约：
 *   - 鼠标左键拖拽：改变朝向（yaw/pitch，pitch 限 ±83° 防翻转）。
 *   - W/S/A/D 或方向键：沿视线朝向水平移动；Shift ×3 加速。
 *   - 相机高度恒为 eyeHeight（贴地行走，不穿地）。
 *   - 活动范围 clamp 到 ±bounds（防走出世界）。
 *   - 输入焦点在 input/textarea/contentEditable（如聊天框）时按键不劫持。
 *
 * 与 OrbitControls **互斥挂载**（调用方按视角模式条件渲染其一），
 * 卸载时恢复 camera.rotation.order = 'XYZ'。
 *
 * 注：桌面优先（键鼠）；触屏双摇杆留待后续批次。
 */

import { useEffect, useRef } from 'react';
import { useFrame, useThree } from '@react-three/fiber';

export interface WalkControlsProps {
  /** 眼高（世界单位）。 */
  eyeHeight?: number;
  /** 活动范围半边长（|x|,|z| 各 clamp 到此值）。 */
  bounds?: number;
  /** 基础移速（世界单位/秒），Shift ×3。 */
  speed?: number;
  /** 进入漫游的落点 [x, z]（仅挂载时应用一次）。 */
  start?: [number, number];
  /** 初始朝向 yaw（弧度；0 = 朝 -z）。 */
  startYaw?: number;
}

const MOVE_KEYS = new Set([
  'KeyW', 'KeyA', 'KeyS', 'KeyD',
  'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight',
]);

/** 焦点在文本输入处时不劫持按键（聊天框 / 表单优先）。 */
function isTextEditingTarget(): boolean {
  const el = typeof document !== 'undefined' ? document.activeElement : null;
  if (!el) return false;
  const tag = el.tagName;
  return (
    tag === 'INPUT' ||
    tag === 'TEXTAREA' ||
    (el as HTMLElement).isContentEditable === true
  );
}

export function WalkControls({
  eyeHeight = 0.17,
  bounds = 58,
  speed = 3,
  start = [0, 0],
  startYaw = 0,
}: WalkControlsProps) {
  const camera = useThree((s) => s.camera);
  const gl = useThree((s) => s.gl);

  const yawRef = useRef(startYaw);
  const pitchRef = useRef(0);
  const keysRef = useRef<Set<string>>(new Set());
  const dragRef = useRef(false);

  // 挂载：落点 + 朝向初始化；卸载：恢复 rotation order
  useEffect(() => {
    camera.rotation.order = 'YXZ';
    camera.position.set(start[0], eyeHeight, start[1]);
    yawRef.current = startYaw;
    pitchRef.current = 0;
    camera.rotation.set(0, startYaw, 0);
    return () => {
      camera.rotation.order = 'XYZ';
    };
    // start/startYaw 仅在进入漫游时应用一次（切换模式时由调用方重挂载）
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [camera, eyeHeight]);

  // 键盘（window 级；文本输入聚焦时不劫持；方向键阻止页面滚动）
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (isTextEditingTarget()) return;
      if (MOVE_KEYS.has(e.code)) {
        keysRef.current.add(e.code);
        if (e.code.startsWith('Arrow')) e.preventDefault();
      }
      if (e.code === 'ShiftLeft' || e.code === 'ShiftRight') keysRef.current.add(e.code);
    };
    const onKeyUp = (e: KeyboardEvent) => {
      keysRef.current.delete(e.code);
    };
    const onBlur = () => keysRef.current.clear();
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('keyup', onKeyUp);
    window.addEventListener('blur', onBlur);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('keyup', onKeyUp);
      window.removeEventListener('blur', onBlur);
    };
  }, []);

  // 鼠标拖拽环视（canvas 级，pointer capture 保证拖出 canvas 不丢轨迹）
  useEffect(() => {
    const el = gl.domElement;
    const onDown = (e: PointerEvent) => {
      if (e.button !== 0) return;
      dragRef.current = true;
      el.setPointerCapture(e.pointerId);
    };
    const onMove = (e: PointerEvent) => {
      if (!dragRef.current) return;
      yawRef.current -= e.movementX * 0.0032;
      pitchRef.current -= e.movementY * 0.0032;
      const LIMIT = 1.45; // ≈83°
      pitchRef.current = Math.max(-LIMIT, Math.min(LIMIT, pitchRef.current));
    };
    const onUp = (e: PointerEvent) => {
      dragRef.current = false;
      if (el.hasPointerCapture(e.pointerId)) el.releasePointerCapture(e.pointerId);
    };
    el.addEventListener('pointerdown', onDown);
    el.addEventListener('pointermove', onMove);
    el.addEventListener('pointerup', onUp);
    el.addEventListener('pointercancel', onUp);
    return () => {
      el.removeEventListener('pointerdown', onDown);
      el.removeEventListener('pointermove', onMove);
      el.removeEventListener('pointerup', onUp);
      el.removeEventListener('pointercancel', onUp);
    };
  }, [gl]);

  useFrame((_state, dt) => {
    const keys = keysRef.current;
    const boost = keys.has('ShiftLeft') || keys.has('ShiftRight') ? 3 : 1;

    // 前向（水平投影）：rotation order YXZ 下 forward = (−sin yaw, 0, −cos yaw)
    const yaw = yawRef.current;
    const fx = -Math.sin(yaw);
    const fz = -Math.cos(yaw);
    const rx = Math.cos(yaw);
    const rz = -Math.sin(yaw);

    let mx = 0;
    let mz = 0;
    if (keys.has('KeyW') || keys.has('ArrowUp')) { mx += fx; mz += fz; }
    if (keys.has('KeyS') || keys.has('ArrowDown')) { mx -= fx; mz -= fz; }
    if (keys.has('KeyD') || keys.has('ArrowRight')) { mx += rx; mz += rz; }
    if (keys.has('KeyA') || keys.has('ArrowLeft')) { mx -= rx; mz -= rz; }

    const len = Math.hypot(mx, mz);
    if (len > 0) {
      const step = (speed * boost * dt) / len;
      camera.position.x = Math.max(-bounds, Math.min(bounds, camera.position.x + mx * step));
      camera.position.z = Math.max(-bounds, Math.min(bounds, camera.position.z + mz * step));
    }
    camera.position.y = eyeHeight;
    camera.rotation.set(pitchRef.current, yawRef.current, 0);
  });

  return null;
}
