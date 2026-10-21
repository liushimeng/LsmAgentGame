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

// ── 批次 67：视线俯仰自由度（抬头通道）────────────────────────────────────

/**
 * 视线俯仰偏移（`lookPitch`）的缺省限位。
 *
 * 背景：批次 67 修「视觉不能向上」。根因不是 `maxPolar` 太小，而是**视线朝向被锚点
 * 锁死**（orbit 锁 `lookAt(target)`、fly-选中锁 `lookAt(focus)`）—— 把 `maxPolar`
 * 放大到 >π/2 也不行（那样相机必须沉到锚点之下才能形成向上视线，与地面钳制冲突）。
 * 正解是**位置与视线解耦**：位置照旧由锚定/积分求解，视线 = 基朝向 + `lookPitch`。
 *
 * `1.40 rad ≈ 80.2°`：orbit 默认 `φ=0.85`（基仰角 −41°）⇒ 满抬头后仰角 +39°（正对
 * 天空上部）；`φ→1.54`（基仰角 −1.8°）⇒ 满抬头 +78°（近天顶）。
 */
export const LOOK_PITCH_LIMIT_DEFAULT = 1.4;
/** 允许比基朝向再低一点（低头看地面近处），与 `φ` 轨道俯仰互补。 */
export const LOOK_PITCH_MIN_DEFAULT = -0.35;

/** 相机局部 +X（屏幕右向）—— 视线俯仰的旋转轴。 */
const LOOK_PITCH_AXIS = new THREE.Vector3(1, 0, 0);

/** Rodrigues 中转用的临时向量。 */
const _tiltAxis = new THREE.Vector3();

/** 视线俯仰偏移（yaw/pitch 欧拉形式）。 */
export interface YawPitch {
  yaw: number;
  pitch: number;
}

/** 钳制视线俯仰偏移到 `[min, max]`。 */
export function clampLookPitch(v: number, min: number, max: number): number {
  return clamp(v, min, max);
}

/** 由四元数取视线方向（相机局部 −Z 经该四元数变换）。 */
export function forwardFromQuaternion(
  out: THREE.Vector3,
  quat: THREE.Quaternion,
): THREE.Vector3 {
  return out.set(0, 0, -1).applyQuaternion(quat);
}

/**
 * 绕「相机右向量」把视线方向抬起 `angle` 弧度（右手定则，正 = 抬头）。
 *
 * `right` 与 `out` 需正交（相机右向量 ⟂ 视线）。Rodrigues 展开后
 * `k(k·v)(1−cos a)` 项因 `k·v = 0` 而消失 ⇒ `v' = v·cos a + (k×v)·sin a`。
 *
 * 说明：控制器实际走**四元数右乘**路径（`lookPitchQuaternion` + `quaternion.multiply`），
 * 本函数是同一数学的向量形式，供纯函数测试与将来解析式求解复用。
 */
export function tiltForward(
  out: THREE.Vector3,
  right: THREE.Vector3,
  angle: number,
): THREE.Vector3 {
  if (Math.abs(angle) < 1e-9) return out;
  _tiltAxis.copy(right).cross(out);
  out.multiplyScalar(Math.cos(angle)).addScaledVector(_tiltAxis, Math.sin(angle));
  return out.normalize();
}

/**
 * 由视线方向反解 `(yaw, pitch)`（three YXZ 欧拉约定）。
 *
 * 欧拉约定：`forward = (−sinY·cosP, sinP, −cosY·cosP)` ⇒
 * `pitch = asin(fwd.y)`、`yaw = atan2(−fwd.x, −fwd.z)`。
 */
export function yawPitchFromDirection(out: YawPitch, dir: THREE.Vector3): YawPitch {
  out.pitch = Math.asin(clamp(dir.y, -1, 1));
  out.yaw = Math.atan2(-dir.x, -dir.z);
  return out;
}

/**
 * 视线俯仰偏移的**局部**旋转四元数（绕相机局部 +X，正 = 抬头）。
 *
 * 用法：`camera.quaternion.multiply(q)` —— 四元数右乘 = 在相机局部系施加旋转，
 * 等价于「相机抬头 a 度」，视线方向随之由 `(0,0,−1)` 变为 `(0, sin a, −cos a)`。
 */
export function lookPitchQuaternion(out: THREE.Quaternion, angle: number): THREE.Quaternion {
  return out.setFromAxisAngle(LOOK_PITCH_AXIS, angle);
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
