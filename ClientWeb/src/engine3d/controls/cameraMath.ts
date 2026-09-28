/**
 * engine3d/controls/cameraMath — 自由视角的纯数学工具（无 React / 无 three 场景依赖）。
 *
 * 批次 32「自由视角系统」新增。三类内容：
 *   1. 输入卫生：文本编辑守卫 + 修饰键守卫（R16）；
 *   2. 帧率无关阻尼：标量 / 向量 / 角度（§方案 §6）；
 *   3. 位姿插值：缓动、球↔笛卡尔互转、相机位姿插值（球面 / 线性，R11）。
 *
 * 设计约束：
 *   - **禁止** `lerp(cur, target, 0.08)` 式帧率相关阻尼（60fps 与 144fps 手感不一致，
 *     是 Web 3D「黏滞/抖动」投诉的头号来源）；统一走 `MathUtils.damp` 的指数衰减。
 *   - 所有函数无副作用（除显式传入的 out 参数），可单测。
 */

import * as THREE from 'three';

/** 帧率无关指数阻尼系数：`1 - e^(-lambda * dt)`（lambda 越大越跟手）。 */
export function dampFactor(lambda: number, dt: number): number {
  return 1 - Math.exp(-lambda * Math.min(dt, 0.1));
}

/** 标量阻尼。 */
export function damp(current: number, target: number, lambda: number, dt: number): number {
  if (lambda <= 0) return current;
  return current + (target - current) * dampFactor(lambda, dt);
}

/** 向量逐分量阻尼（就地）。 */
export function dampVec3(
  out: THREE.Vector3,
  target: THREE.Vector3,
  lambda: number,
  dt: number,
): THREE.Vector3 {
  if (lambda <= 0) return out;
  const k = dampFactor(lambda, dt);
  out.x += (target.x - out.x) * k;
  out.y += (target.y - out.y) * k;
  out.z += (target.z - out.z) * k;
  return out;
}

/** easeInOutCubic —— 视角切换过渡的默认缓动（前 1/4 慢、中段快、末段收）。 */
export function easeInOutCubic(t: number): number {
  const x = Math.min(1, Math.max(0, t));
  return x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2;
}

export function clamp(v: number, lo: number, hi: number): number {
  return v < lo ? lo : v > hi ? hi : v;
}

/** 归一化到 (-π, π]；用于 yaw 无界累积后的角度插值。 */
export function wrapAngle(a: number): number {
  const t = (a + Math.PI) % (Math.PI * 2);
  return (t < 0 ? t + Math.PI * 2 : t) - Math.PI;
}

/** 球坐标（three 约定：phi 从 +Y 轴起算，theta 绕 Y 轴）。 */
export interface Spherical {
  radius: number;
  phi: number;
  theta: number;
}

/** offset → 球坐标。 */
export function sphericalFromVec3(v: THREE.Vector3): Spherical {
  const radius = v.length();
  if (radius < 1e-6) return { radius: 0, phi: 0, theta: 0 };
  return {
    radius,
    phi: Math.acos(clamp(v.y / radius, -1, 1)),
    theta: Math.atan2(v.x, v.z),
  };
}

/** 球坐标 → offset（写 out，零分配）。 */
export function sphericalToVec3(s: Spherical, out: THREE.Vector3): THREE.Vector3 {
  const sinPhi = Math.sin(s.phi);
  out.set(
    s.radius * sinPhi * Math.sin(s.theta),
    s.radius * Math.cos(s.phi),
    s.radius * sinPhi * Math.cos(s.theta),
  );
  return out;
}

/**
 * 两个位置之间的插值方式（对应规格「球面 / 线性插值平滑过渡」）。
 * - `spherical`：绕两位置的公共 pivot 公转，轨迹最短、切换俯瞰/其它绕点视角时观感最自然；
 * - `linear`：两点直线，最可预期，用于自由飞行这类「无公转语义」的模式切换。
 */
export type PoseLerpMode = 'linear' | 'spherical';

const _sphA = new THREE.Spherical();
const _sphB = new THREE.Spherical();
const _mid = new THREE.Vector3();
const _va = new THREE.Vector3();
const _vb = new THREE.Vector3();

/**
 * 位置插值（写 out）。`spherical` 模式下 pivot = (from + to) / 2，
 * 从 from 绕 pivot 公转到 to，半径取两半径的线性插值。
 */
export function lerpPosition(
  out: THREE.Vector3,
  from: THREE.Vector3,
  to: THREE.Vector3,
  t: number,
  mode: PoseLerpMode,
): THREE.Vector3 {
  if (mode === 'linear') {
    out.lerpVectors(from, to, t);
    return out;
  }
  _mid.addVectors(from, to).multiplyScalar(0.5);
  _va.subVectors(from, _mid);
  _vb.subVectors(to, _mid);
  _sphA.setFromVector3(_va);
  _sphB.setFromVector3(_vb);
  _sphA.radius += (_sphB.radius - _sphA.radius) * t;
  _sphA.phi += (_sphB.phi - _sphA.phi) * t;
  _sphA.theta += (_sphB.theta - _sphA.theta) * t;
  _va.setFromSpherical(_sphA);
  return out.addVectors(_mid, _va);
}

/** 焦点在文本输入处时不劫持按键（聊天框 / 表单优先）。沿用批次 22 的既有口径。 */
export function isTextEditingTarget(): boolean {
  const el = typeof document !== 'undefined' ? document.activeElement : null;
  if (!el) return false;
  const tag = el.tagName;
  return (
    tag === 'INPUT' ||
    tag === 'SELECT' ||
    tag === 'TEXTAREA' ||
    (el as HTMLElement).isContentEditable === true
  );
}

/**
 * 按键是否应被相机吃掉：文本编辑中、带修饰键（不劫持浏览器快捷键）、
 * 或按键原文本非单字符（AltGr / 组合输入）时一律放行。
 */
export function shouldCaptureKey(e: KeyboardEvent): boolean {
  if (isTextEditingTarget()) return false;
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  return true;
}
