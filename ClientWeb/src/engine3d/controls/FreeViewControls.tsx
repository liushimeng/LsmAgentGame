/**
 * engine3d/controls/FreeViewControls — 自由视角统一控制器（轨道 / 全自由六自由度 / 街景漫游）。
 *
 * 批次 32「自由视角系统」新增，**替代**批次 22 的 `OrbitControls + WalkControls` 双挂载方案。
 *
 * 为什么是替代而不是叠加（见 32-自由视角系统/01-方案设计.md §3.2）：
 *   原「orbit / walk」是两套互斥挂载的控制器，切换靠重挂载（epoch）实现，
 *   因此**无法做位姿插值** —— 每次切换画面瞬跳。本批把三种模式统一到
 *   「同一套积分器 + 按模式分流的输入映射」：切换时先 Seed 新模式的内部状态、
 *   再走 600 ms 位姿过渡（位置球面/线性插值 + 四元数 slerp + FOV 插值），零跳变。
 *
 * 交互契约（对应需求 R1–R17）：
 *   - orbit 轨道式：左拖绕目标公转 / 右拖平面平移 / 滚轮缩放视距 / WASD 平移聚焦点。
 *   - fly  全自由六自由度：**右拖**控制俯仰与水平旋转 / WASD 前后左右 /
 *     空格·E 上升、Q 下降 / **中拖**屏幕平面视口平移 / 滚轮改 FOV（长焦检视）。
 *   - walk 街景漫游：贴地第一人称，眼高恒定（不可穿地）。
 *   - 全模式：场景碰撞（AABB 推出）+ 地图边界钳制 + 地面钳制（避免穿模）。
 *   - 全模式：帧率无关指数阻尼（移动 / 转向 / 缩放），抑制抖动。
 *   - 全模式：禁用右键默认菜单；页面失焦立即归零速度；文本编辑不劫持按键。
 *   - 快捷键：V 循环 / 1·2·3 直达 / F 往返 / R 复位 / H 帮助 / [ ] 档位 / Esc 收起。
 *
 * 引擎层硬约束（CLAUDE.md §2.1-5）：本文件**禁止** import 任何游戏私有模块。
 */

import { useEffect, useRef } from 'react';
import * as THREE from 'three';
import { useFrame, useThree } from '@react-three/fiber';
import {
  clamp,
  damp,
  dampVec3,
  easeInOutCubic,
  lerpPosition,
  shouldCaptureKey,
  wrapAngle,
} from './cameraMath';
import type { PoseLerpMode } from './cameraMath';
import { resolveCameraCollision } from './cameraCollision';
import type { CameraBounds, CameraBox, CollisionResult } from './cameraCollision';
import { useFreeView, FREE_VIEW_MODES, FREE_VIEW_TIERS } from './freeViewStore';
import type { FreeViewMode } from './freeViewStore';
import type { TargetLike } from './CameraViewReporter';

/** 聚焦目标（地平面上一点；null = 无聚焦请求）。原 FocusLerpController 的契约，收编至此。 */
export interface FocusTarget {
  x: number;
  z: number;
}

/** 对外暴露的轨道聚焦点消费方（小地图视野上报 / 兼容 TargetLike 的旧代码）。 */
export type FreeViewTargetRef = React.MutableRefObject<TargetLike | null>;

/** 自由视角的相机朝向 / FOV 读回源（fly/walk 才有意义；orbit 时两项为 null）。 */
export interface FreeViewAim {
  yaw: number | null;
  fov: number | null;
}

export interface FreeViewControlsProps {
  /** 受控模式；缺省消费 `useFreeView` store（推荐，DOM HUD 也能改）。 */
  mode?: FreeViewMode;
  /** 允许的模式子集（对应 R12「建造/观察态才解锁全自由」）；缺省三种全开。 */
  allowedModes?: readonly FreeViewMode[];
  /** 初始/复位用的轨道聚焦点。缺省 [0,0,0]。 */
  initialTarget?: [number, number, number];
  /** 初始轨道半径。缺省 = 当前相机到 initialTarget 的距离。 */
  initialRadius?: number;
  /** 俯瞰归零点固定 theta（弧度）。传了它，进入 orbit = 固定 [target, radius, phi, theta]，
   *  不再从当前位置反解 —— 用于「只有一个固定落点」的俯瞰归零点语义。缺省 = 从相机反解。 */
  orbitFixedTheta?: number;
  /** 初始轨道俯仰角（弧度，从 +Y 轴起算）。缺省 0.95（约 55°，标准 3/4 俯瞰）。 */
  initialPhi?: number;
  /** 从贴地模式切入轨道时的默认半径（站在原地向上「拉起」的舒适视距）。 */
  defaultRadius?: number;
  /** 初始 FOV（度）；缺省取 Canvas 相机的 fov。 */
  fov?: number;
  /** FOV 上下限（滚轮与速度自适应共用）。 */
  fovMin?: number;
  fovMax?: number;
  /** 速度自适应 FOV 的最大增益（度）。0 = 关闭动态 FOV（R5，默认开启）。 */
  fovSpeedGain?: number;
  /** 自由飞行基础移速（世界单位/秒）。缺省同 `speed`。 */
  speed?: number;
  /** 轨道模式平移聚焦点的移速。缺省同 `speed`。 */
  orbitPanSpeed?: number;
  /**
   * 选中物体的聚焦点（世界坐标 + 环绕半径）。非空时 fly 进入「选中环绕」子状态：
   * W/S = 拉远/拉近（半径），A/D = 水平环绕，朝向 lookAt(focus)；仍支持垂直升降。
   * null（或未传）= 自由飞行（WASD 沿视线移动）。
   */
  selectedFocus?: { x: number; y: number; z: number; radius: number } | null;
  // 街景漫游（walk）已按需求删除，眼高不再需要。
  /** 轨道半径上下限。 */
  minDistance?: number;
  maxDistance?: number;
  /** 轨道俯仰角上下限（弧度，从 +Y 轴起算）。 */
  minPolar?: number;
  maxPolar?: number;
  /** 自由视角俯仰限制（弧度，默认 ±89° 防翻转）。 */
  pitchLimit?: number;
  /** 场景碰撞体（AABB 列表，对应 R3）。 */
  colliders?: readonly CameraBox[];
  /** 碰撞球半径（世界单位）。 */
  collisionRadius?: number;
  /** 地图边界钳制。 */
  bounds?: CameraBounds;
  /** 地面/水面安全高度。 */
  groundY?: number;
  /** 升降键位（R9；`Shift` 恒为加速键，见方案 §4.4 冲突裁决）。 */
  verticalKeys?: { up: readonly string[]; down: readonly string[] };
  /** 视角切换过渡时长（秒）。0 = 关闭过渡（硬切）。 */
  transitionDuration?: number;
  /** 过渡的位姿插值方式；'auto' = 两端都是绕点语义时用球面，否则线性。 */
  transitionMode?: 'auto' | PoseLerpMode;
  /** 对外暴露的聚焦点 ref；非 orbit 模式下置 null（消费方回落到「上报相机自身」）。 */
  targetRef?: FreeViewTargetRef;
  /** 聚焦请求（小地图 / 面板点击 → 平滑移焦），到位自动清空。 */
  focusRef?: React.MutableRefObject<FocusTarget | null>;
  /** 选中物体的聚焦点（ref，游戏侧薄适配层写入；null/未传 = 未选中 → 自由飞行）。 */
  selectedFocusRef?: React.MutableRefObject<SelectedFocus | null>;
  /** 视锥上报源（fly 时的相机朝向与 FOV，供小地图画扇形）。 */
  aimRefs?: React.MutableRefObject<FreeViewAim>;
  /** 命中此 URL 片段时把运行时诊断挂到 window（CDP 验收用）。 */
  debugGlobalName?: string;
  /** 灵敏度：滚轮缩放。 */
  zoomSpeed?: number;
  /** 灵敏度：拖拽旋转。 */
  rotateSpeed?: number;
  /** 灵敏度：拖拽平移倍率。 */
  panSpeed?: number;
}

/** 阻尼系数 λ（越大越跟手，详见方案 §6.1）。 */
const LAMBDA_ROT = 18;
const LAMBDA_MOVE = 12;
const LAMBDA_ZOOM = 10;
const LAMBDA_FOV = 3;
/** 速度自适应 FOV 的缺省增益（度）。R5 默认开启。 */
const DEFAULT_FOV_SPEED_GAIN = 10;

/** 拖拽位移超过该像素数即判定为「拖拽」，随后那一次 click 被吞掉（R10 误触选中）。 */
const DRAG_THRESHOLD_PX = 4;
/** 拖拽结束后多久内吞掉 click（毫秒）。 */
const CLICK_SUPPRESS_MS = 250;

/** 键盘捕获的移动键（按 KeyboardEvent.code）。 */
const MOVE_KEYS = new Set([
  'KeyW', 'KeyA', 'KeyS', 'KeyD',
  'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight',
  'Space', 'KeyQ', 'KeyE',
  'ShiftLeft', 'ShiftRight',
]);

/** 需要 preventDefault 的键（否则会滚动页面）。 */
const SCROLL_KEYS = new Set([
  'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Space',
]);

const UP = new THREE.Vector3(0, 1, 0);

// 复用的临时对象（每帧零分配）
const _v1 = new THREE.Vector3();
const _v2 = new THREE.Vector3();
const _velGoal = new THREE.Vector3();
const _right = new THREE.Vector3();
const _up = new THREE.Vector3();
const _mat = new THREE.Matrix4();
const _euler = new THREE.Euler(0, 0, 0, 'YXZ');
const _quat = new THREE.Quaternion();
const _coll: CollisionResult = { position: _v1, hit: false };

/** 由 yaw/pitch（YXZ 欧拉序）求朝向四元数。 */
function quatFromYawPitch(out: THREE.Quaternion, yaw: number, pitch: number): THREE.Quaternion {
  _euler.set(pitch, yaw, 0, 'YXZ');
  return out.setFromEuler(_euler);
}

/** 球坐标 → 笛卡尔偏移（写 out）。 */
function sphOffset(out: THREE.Vector3, radius: number, phi: number, theta: number): THREE.Vector3 {
  const sinPhi = Math.sin(phi);
  return out.set(
    radius * sinPhi * Math.sin(theta),
    radius * Math.cos(phi),
    radius * sinPhi * Math.cos(theta),
  );
}

/** 让相机始终看向聚焦点（碰撞推出后重算朝向，避免「推开后看偏」）。 */
function lookAtTarget(camera: THREE.PerspectiveCamera, target: THREE.Vector3): void {
  _mat.lookAt(camera.position, target, UP);
  camera.quaternion.setFromRotationMatrix(_mat);
}

interface RigState {
  mode: FreeViewMode;
  /** 过渡中「将要落到的模式」。 */
  pendingMode: FreeViewMode | null;

  // ── orbit：聚焦点 + 球坐标 ──
  target: THREE.Vector3;
  targetGoal: THREE.Vector3;
  radius: number;
  radiusGoal: number;
  phi: number;
  phiGoal: number;
  theta: number;
  thetaGoal: number;

  // ── fly / walk：位置 + 朝向 + 速度 ──
  pos: THREE.Vector3;
  yaw: number;
  yawGoal: number;
  pitch: number;
  pitchGoal: number;
  vel: THREE.Vector3;

  // ── 过渡 ──
  transitioning: boolean;
  tt: number;
  dur: number;
  lerpMode: PoseLerpMode;
  fromPos: THREE.Vector3;
  fromQuat: THREE.Quaternion;
  fromFov: number;
  toPos: THREE.Vector3;
  toQuat: THREE.Quaternion;
  toFov: number;

  // ── 其它 ──
  userFov: number;
  userFovGoal: number;
  collisionHit: boolean;
  /** 当前速度档位下标（从 store 同步，积分器直接读，避免逐函数传参）。 */
  tier: number;
  /** 挂载时的 home 位姿（`R` 键复位用）。 */
  home: {
    pos: THREE.Vector3;
    target: THREE.Vector3;
    radius: number;
    phi: number;
    theta: number;
    yaw: number;
    pitch: number;
    fov: number;
  };
}

function createRig(camera: THREE.PerspectiveCamera, pr: FreeViewControlsProps): RigState {
  const homeDist = pr.initialRadius
    ?? camera.position.distanceTo(_v1.fromArray(pr.initialTarget ?? [0, 0, 0]));
  // 球坐标必须由 **相机 - 目标** 反解（sphOffset 的 (sinθ, ·, cosθ) 约定与
  // Spherical.setFromVector3 一致）；用 target - camera 会把 theta 取反，首帧
  // 相机在 X/Z 上被镜像（首帧观感与换用本控制器之前不一致）。
  const sph = new THREE.Spherical().setFromVector3(
    _v1.copy(camera.position).sub(_v2.fromArray(pr.initialTarget ?? [0, 0, 0])),
  );
  const startFov = camera.fov || pr.fov || 45;
  const phi = clamp(pr.initialPhi ?? 0.95, pr.minPolar ?? 0.02, pr.maxPolar ?? 1.545);
  _euler.setFromQuaternion(camera.quaternion, 'YXZ');
  const pitch = clamp(_euler.x, -(pr.pitchLimit ?? 1.553), pr.pitchLimit ?? 1.553);
  const target = new THREE.Vector3().fromArray(pr.initialTarget ?? [0, 0, 0]);
  return {
    mode: pr.mode ?? 'orbit',
    pendingMode: null,
    target: target.clone(),
    targetGoal: target.clone(),
    radius: clamp(homeDist, pr.minDistance ?? 2, pr.maxDistance ?? 100),
    radiusGoal: clamp(homeDist, pr.minDistance ?? 2, pr.maxDistance ?? 100),
    phi,
    phiGoal: phi,
    theta: pr.orbitFixedTheta ?? sph.theta,
    thetaGoal: pr.orbitFixedTheta ?? sph.theta,
    pos: camera.position.clone(),
    yaw: _euler.y,
    yawGoal: _euler.y,
    pitch,
    pitchGoal: pitch,
    vel: new THREE.Vector3(),
    transitioning: false,
    tt: 0,
    dur: pr.transitionDuration ?? 0.6,
    lerpMode: 'linear',
    fromPos: new THREE.Vector3(),
    fromQuat: new THREE.Quaternion(),
    fromFov: startFov,
    toPos: new THREE.Vector3(),
    toQuat: new THREE.Quaternion(),
    toFov: startFov,
    userFov: startFov,
    userFovGoal: startFov,
    collisionHit: false,
    tier: 1,
    home: {
      pos: camera.position.clone(),
      target: target.clone(),
      radius: clamp(homeDist, pr.minDistance ?? 2, pr.maxDistance ?? 100),
      phi,
      theta: sph.theta,
      yaw: _euler.y,
      pitch,
      fov: startFov,
    },
  };
}

/** 某只鼠标键在当前模式下是否被相机占用（避免劫持纯点选操作）。 */
function isButtonUsedForCamera(mode: FreeViewMode, button: number): boolean {
  if (mode === 'orbit') return button === 0 || button === 2;
  return button === 1 || button === 2;
}

/** 键盘 → 目标速度矢量。`allowVertical` 为假时忽略升降键（walk 贴地）。 */
function inputVelocity(
  out: THREE.Vector3,
  keySet: Set<string>,
  yaw: number,
  pitch: number,
  mag: number,
  verticalKeys: { up: readonly string[]; down: readonly string[] },
  allowVertical: boolean,
): THREE.Vector3 {
  out.set(0, 0, 0);
  const sinY = Math.sin(yaw);
  const cosY = Math.cos(yaw);
  const cosP = Math.cos(pitch);
  // 前向 = (−sinY·cosP, sinP, −cosY·cosP)；右向 = (cosY, 0, −sinY)
  const fx = -sinY * cosP;
  const fy = Math.sin(pitch);
  const fz = -cosY * cosP;
  if (keySet.has('KeyW') || keySet.has('ArrowUp')) {
    out.x += fx;
    out.y += fy;
    out.z += fz;
  }
  if (keySet.has('KeyS') || keySet.has('ArrowDown')) {
    out.x -= fx;
    out.y -= fy;
    out.z -= fz;
  }
  if (keySet.has('KeyD') || keySet.has('ArrowRight')) {
    out.x += cosY;
    out.z += -sinY;
  }
  if (keySet.has('KeyA') || keySet.has('ArrowLeft')) {
    out.x -= cosY;
    out.z -= sinY;
  }
  if (allowVertical) {
    for (const k of verticalKeys.up) if (keySet.has(k)) out.y += 1;
    for (const k of verticalKeys.down) if (keySet.has(k)) out.y -= 1;
  }
  const len = out.length();
  if (len > 1e-6) out.multiplyScalar(mag / len);
  return out;
}

/** 当前有效移速（三态各有基速，再乘档位 × Shift 加速）。 */
function moveSpeed(
  base: number,
  keySet: Set<string>,
  tier: number,
): number {
  const boost = keySet.has('ShiftLeft') || keySet.has('ShiftRight') ? 4 : 1;
  return base * (FREE_VIEW_TIERS[tier] ?? 1) * boost;
}

/** 按模式取基速。 */
function baseSpeed(pr: FreeViewControlsProps, mode: FreeViewMode): number {
  const base = pr.speed ?? 1;
  if (mode === 'orbit') return pr.orbitPanSpeed ?? base;
  return base;
}

function clampTarget(v: THREE.Vector3, pr: FreeViewControlsProps): void {
  const b = pr.bounds;
  if (!b) return;
  v.x = clamp(v.x, b.min[0], b.max[0]);
  v.y = clamp(v.y, b.min[1], b.max[1]);
  v.z = clamp(v.z, b.min[2], b.max[2]);
}

/** 碰撞 + 边界 + 地面钳制（就地改写 pos）。 */
function collide(pos: THREE.Vector3, pr: FreeViewControlsProps): boolean {
  _coll.position = pos;
  _coll.hit = false;
  resolveCameraCollision(pos, {
    radius: pr.collisionRadius ?? 0,
    boxes: pr.colliders,
    bounds: pr.bounds,
    groundY: pr.groundY,
  }, _coll);
  return _coll.hit;
}

/** 轨道模式一步：目标量 → 阻尼 → 相机位姿 → 碰撞 → 朝向。 */
function stepOrbit(
  s: RigState,
  dt: number,
  pr: FreeViewControlsProps,
  camera: THREE.PerspectiveCamera,
  keySet: Set<string>,
  tier: number,
): void {
  // WASD 平移聚焦点（水平面），E/Q 升降 —— R7「前后左右平面平移」在轨道态的落点。
  const vkeys = pr.verticalKeys ?? { up: ['Space', 'KeyE'], down: ['KeyQ'] };
  const mag = moveSpeed(baseSpeed(pr, 'orbit'), keySet, tier);
  _velGoal.set(0, 0, 0);
  inputVelocity(_velGoal, keySet, s.theta, 0, mag, vkeys, false);
  if (_velGoal.lengthSq() > 1e-9) {
    s.targetGoal.addScaledVector(_velGoal, dt);
    for (const k of vkeys.up) if (keySet.has(k)) s.targetGoal.y += mag * dt;
    for (const k of vkeys.down) if (keySet.has(k)) s.targetGoal.y -= mag * dt;
    clampTarget(s.targetGoal, pr);
  }

  dampVec3(s.target, s.targetGoal, LAMBDA_MOVE, dt);
  s.radius = damp(s.radius, s.radiusGoal, LAMBDA_ZOOM, dt);
  s.phi = clamp(damp(s.phi, s.phiGoal, LAMBDA_ROT, dt), pr.minPolar ?? 0.02, pr.maxPolar ?? 1.545);
  // theta 走最短弧，避免跨越 ±π 时整圈甩动
  s.theta = wrapAngle(s.theta + wrapAngle(s.thetaGoal - s.theta) * dampFactorOf(LAMBDA_ROT, dt));

  sphOffset(_v1, s.radius, s.phi, s.theta);
  camera.position.copy(s.target).add(_v1);
  s.collisionHit = collide(camera.position, pr);
  lookAtTarget(camera, s.target);
}

function dampFactorOf(lambda: number, dt: number): number {
  return 1 - Math.exp(-lambda * Math.min(dt, 0.1));
}

/** 选中物体的聚焦点（世界坐标 + 环绕半径）。null = 未选中（自由飞行）。 */
export interface SelectedFocus {
  x: number;
  y: number;
  z: number;
  radius: number;
}

/** 全自由六自由度一步。
 *
 *  批次 32 v2：按需求分两种子状态 ——
 *    focus == null（未选中物体）：WASD 沿视线前后左右 + 垂直升降，可穿水平面任意位置；
 *    focus != null（选中物体）：WASD 变成围绕物体移动 —— W/S = 半径减小/增大（拉近/拉远），
 *      A/D = 水平环绕（绕 focus.y 轴公转），朝向 lookAt(focus)。垂直升降仍可用。
 */
function stepFly(
  s: RigState,
  dt: number,
  pr: FreeViewControlsProps,
  camera: THREE.PerspectiveCamera,
  keySet: Set<string>,
  tier: number,
  focus: SelectedFocus | null,
): void {
  const mag = moveSpeed(baseSpeed(pr, 'fly'), keySet, tier);
  const vkeys = pr.verticalKeys ?? { up: ['Space', 'KeyE'], down: ['KeyQ'] };

  if (focus) {
    // ── 选中环绕子状态 ──
    // 把当前 pos 投影到「以 focus 为中心、保持到 focus 距离」的球坐标（无跳变）。
    _v1.copy(s.pos).sub(_v2.set(focus.x, focus.y, focus.z));
    const sph = new THREE.Spherical().setFromVector3(_v1);
    let radius = sph.radius;
    let theta = sph.theta;
    let phi = sph.phi;
    // W/S 改变半径（拉近/拉远），A/D 环绕（水平公转），垂直升降由 Space/E/Q 直接改 pos.y
    if (keySet.has('KeyW') || keySet.has('ArrowUp')) radius -= mag * dt;
    if (keySet.has('KeyS') || keySet.has('ArrowDown')) radius += mag * dt;
    if (keySet.has('KeyD') || keySet.has('ArrowRight')) theta -= mag * dt / Math.max(radius, 1);
    if (keySet.has('KeyA') || keySet.has('ArrowLeft')) theta += mag * dt / Math.max(radius, 1);
    radius = clamp(radius, (pr.collisionRadius ?? 0) + 0.5, pr.maxDistance ?? 100);
    phi = clamp(phi, pr.minPolar ?? 0.02, pr.maxPolar ?? 1.545);
    // 垂直升降（仍可任意高度，不受水平面限制）
    let y = focus.y + radius * Math.cos(phi);
    for (const k of vkeys.up) if (keySet.has(k)) y += mag * dt;
    for (const k of vkeys.down) if (keySet.has(k)) y -= mag * dt;

    const sinPhi = Math.sin(phi);
    s.pos.set(
      focus.x + radius * sinPhi * Math.sin(theta),
      y,
      focus.z + radius * sinPhi * Math.cos(theta),
    );

    camera.position.copy(s.pos);
    s.collisionHit = collide(camera.position, pr);
    s.pos.copy(camera.position);
    // 朝向聚焦点（环绕语义）；右键拖拽 yaw/pitch 仍在改 goal，但每帧被 lookAt 覆盖 ——
    // 想脱离 lookAt 环绕就取消选中（左键点空白）。
    _mat.lookAt(camera.position, _v2.set(focus.x, focus.y, focus.z), UP);
    camera.quaternion.setFromRotationMatrix(_mat);
    // 同步 yaw/pitch 供 HUD / 后续切回自由飞行时无缝
    _euler.setFromQuaternion(camera.quaternion, 'YXZ');
    s.yaw = _euler.y;
    s.yawGoal = _euler.y;
    s.pitch = _euler.x;
    s.pitchGoal = _euler.x;
    // vel ≈ 0（环绕是位置直接赋值，不是速度积分），自适应 FOV 不额外张开
    s.vel.set(0, 0, 0);
    return;
  }

  // ── 自由飞行（未选中）──
  s.yaw = wrapAngle(s.yaw + wrapAngle(s.yawGoal - s.yaw) * dampFactorOf(LAMBDA_ROT, dt));
  const lim = pr.pitchLimit ?? 1.553;
  s.pitch = clamp(damp(s.pitch, s.pitchGoal, LAMBDA_ROT, dt), -lim, lim);

  inputVelocity(_velGoal, keySet, s.yaw, s.pitch, mag, vkeys, true);
  // 对「目标速度」做阻尼 ⇒ 松手有惯性滑行而非生硬停住（R4）
  dampVec3(s.vel, _velGoal, LAMBDA_MOVE, dt);
  s.pos.addScaledVector(s.vel, dt);

  camera.position.copy(s.pos);
  s.collisionHit = collide(camera.position, pr);
  s.pos.copy(camera.position);
  quatFromYawPitch(camera.quaternion, s.yaw, s.pitch);
}

/** 按当前目标量算出「稳态位姿」（过渡终点 / 复位共用）。 */
function poseFromGoals(s: RigState, mode: FreeViewMode, _pr: FreeViewControlsProps): void {
  if (mode === 'orbit') {
    sphOffset(_v1, s.radiusGoal, s.phiGoal, s.thetaGoal);
    s.toPos.copy(s.targetGoal).add(_v1);
    _mat.lookAt(s.toPos, s.targetGoal, UP);
    s.toQuat.setFromRotationMatrix(_mat);
  } else {
    s.toPos.copy(s.pos);
    quatFromYawPitch(s.toQuat, s.yawGoal, s.pitchGoal);
  }
  s.toFov = s.userFovGoal;
}

/** 落到稳态位姿（过渡结束 / 硬切）。 */
function settle(s: RigState): void {
  const m = s.pendingMode ?? s.mode;
  if (m === 'orbit') {
    s.target.copy(s.targetGoal);
    s.radius = s.radiusGoal;
    s.phi = s.phiGoal;
    s.theta = s.thetaGoal;
  } else {
    s.pos.copy(s.toPos);
    s.yaw = s.yawGoal;
    s.pitch = s.pitchGoal;
  }
  s.vel.set(0, 0, 0);
  s.userFov = s.userFovGoal;
  s.transitioning = false;
  s.pendingMode = null;
}

/**
 * 模式切换（或同模式换参数，如复位）：
 * 1. Seed 新模式参数 —— **从当前相机位姿推导**，保留玩家视线，不是重置到默认位姿；
 * 2. 算出新模式的稳态位姿；
 * 3. 进过渡（位置插值 + slerp + FOV 插值）。
 */
function beginTransition(
  s: RigState,
  from: FreeViewMode,
  to: FreeViewMode,
  camera: THREE.PerspectiveCamera,
  pr: FreeViewControlsProps,
): void {
  s.fromPos.copy(camera.position);
  s.fromQuat.copy(camera.quaternion);
  s.fromFov = camera.fov;

  if (to === 'orbit') {
    if (from !== 'orbit') {
      // 批次 32 v2：「俯瞰归零点」= 固定落点。用 props 里给定的
      // [initialTarget, initialRadius, initialPhi, orbitFixedTheta] 作为唯一目标，
      // 不再从当前相机位置反解（反解会让「归零点」随当前位置漂移，违背语义）。
      s.targetGoal.fromArray(pr.initialTarget ?? [0, 0, 0]);
      s.radiusGoal = clamp(
        pr.initialRadius ?? 42,
        pr.minDistance ?? 2,
        pr.maxDistance ?? 100,
      );
      s.phiGoal = clamp(pr.initialPhi ?? 0.85, pr.minPolar ?? 0.02, pr.maxPolar ?? 1.545);
      s.thetaGoal = pr.orbitFixedTheta ?? Math.PI / 4;
    }
  } else {
    // 从俯瞰切入自由视角：保留相机位置与朝向，只换约束方式
    s.pos.copy(camera.position);
    _euler.setFromQuaternion(camera.quaternion, 'YXZ');
    s.yawGoal = _euler.y;
    s.pitchGoal = clamp(_euler.x, -(pr.pitchLimit ?? 1.553), pr.pitchLimit ?? 1.553);
    s.vel.set(0, 0, 0);
  }

  s.mode = to;
  s.pendingMode = to;
  // 插值方式：两端都是「绕点」语义 ⇒ 球面（轨迹最短、不穿地）；否则线性
  s.lerpMode = pr.transitionMode === 'auto'
    ? (from === 'orbit' && to === 'orbit' ? 'spherical' : 'linear')
    : (pr.transitionMode as PoseLerpMode);
  poseFromGoals(s, to, pr);

  s.tt = 0;
  s.dur = pr.transitionDuration ?? 0.6;
  if (s.dur <= 0) {
    settle(s);
    return;
  }
  s.transitioning = true;
}

/** 过渡帧：位置插值 + 四元数 slerp + FOV 插值；期间输入锁定。 */
function stepTransition(
  s: RigState,
  dt: number,
  camera: THREE.PerspectiveCamera,
): void {
  s.tt += dt;
  const t = clamp(s.tt / (s.dur || 0.6), 0, 1);
  const e = easeInOutCubic(t);
  lerpPosition(camera.position, s.fromPos, s.toPos, e, s.lerpMode);
  camera.quaternion.copy(s.fromQuat).slerp(s.toQuat, e);
  const fov = s.fromFov + (s.toFov - s.fromFov) * e;
  if (Math.abs(camera.fov - fov) > 0.005) {
    camera.fov = fov;
    camera.updateProjectionMatrix();
  }
  // 让外部消费方（targetRef / aimRefs）在过渡期间也能读到连续变化的位姿
  if (s.pendingMode === 'orbit') {
    sphOffset(_v1, s.radiusGoal, s.phiGoal, s.thetaGoal);
    s.target.copy(camera.position).sub(_v1);
  } else {
    s.pos.copy(camera.position);
  }
  if (t >= 1) settle(s);
}

/** 速度自适应 FOV（R5）：越快视野越广 → 像素流速被摊薄，外周运动失配减小。 */
function computeFov(s: RigState, dt: number, pr: FreeViewControlsProps, mode: FreeViewMode): number {
  let goal = s.userFovGoal;
  const gain = pr.fovSpeedGain ?? DEFAULT_FOV_SPEED_GAIN;
  if (mode !== 'orbit' && gain > 0) {
    const base = Math.max(1e-6, baseSpeed(pr, 'fly') * (FREE_VIEW_TIERS[s.tier] ?? 1));
    const fast = Math.min(1, s.vel.length() / (base * 4));
    goal += fast * gain;
  }
  s.userFov = damp(s.userFov, goal, LAMBDA_FOV, dt);
  return clamp(s.userFov, Math.min(pr.fovMin ?? 25, s.userFovGoal), pr.fovMax ?? 80);
}

export function FreeViewControls(props: FreeViewControlsProps) {
  const {
    minPolar = 0.02,
    maxPolar = 1.545,
    pitchLimit = 1.553,
    fovMin = 25,
    fovMax = 80,
    targetRef,
    focusRef,
    selectedFocusRef,
    aimRefs,
    debugGlobalName = '__freeViewDebug',
    zoomSpeed = 0.0072,
  } = props;

  const camera = useThree((st) => st.camera) as THREE.PerspectiveCamera;
  const gl = useThree((st) => st.gl);

  // 受控模式 or store 模式
  const storeMode = useFreeView((s) => s.mode);
  const storeTier = useFreeView((s) => s.tier);
  const setStoreMode = useFreeView((s) => s.setMode);
  const setStoreReady = useFreeView((s) => s.setReady);
  const setStoreColliding = useFreeView((s) => s.setColliding);
  const store = useFreeView;

  const mode = props.mode ?? storeMode;
  const modeRef = useRef<FreeViewMode>(mode);
  modeRef.current = mode;

  /** 只读的最新 props 快照（useFrame / 事件回调内读取，避免闭包过期）。 */
  const pr = useRef(props);
  pr.current = props;

  const rig = useRef<RigState | null>(null);
  const keySet = useRef<Set<string>>(new Set());
  const drag = useRef({ active: false, button: -1, x: 0, y: 0, moved: 0 });
  const suppressUntil = useRef(0);
  const lastHit = useRef(false);
  const focusGoal = useRef<THREE.Vector3 | null>(null);

  /** 供积分器读取档位的桥接字段（避免逐函数传参扩散）。 */
  const tierRef = useRef(storeTier);
  tierRef.current = storeTier;

  // ── 初始化 / 卸载 ────────────────────────────────────────────────
  useEffect(() => {
    camera.rotation.order = 'YXZ';
    const s = createRig(camera, pr.current);
    s.tier = tierRef.current;
    rig.current = s;
    focusGoal.current = null;

    if (targetRef) targetRef.current = { target: s.target };
    setStoreReady(true);

    if (
      debugGlobalName &&
      typeof window !== 'undefined' &&
      window.location.search.includes('debug=1')
    ) {
      (window as unknown as Record<string, unknown>)[debugGlobalName] = {
        get mode() { return modeRef.current; },
        get position() { return camera.position.toArray(); },
        get target() { return (rig.current?.target ?? _v1).toArray(); },
        get yaw() { return rig.current?.yaw ?? 0; },
        get pitch() { return rig.current?.pitch ?? 0; },
        get fov() { return camera.fov; },
        get colliding() { return rig.current?.collisionHit ?? false; },
        get transitioning() { return rig.current?.transitioning ?? false; },
        get tier() { return rig.current?.tier ?? 1; },
        /** 内部状态引用：仅 ?debug=1 时挂出，供 CDP 验收直接驱动（碰撞/边界复测）。 */
        _rig: rig,
        /** 当前生效的碰撞体与边界：CDP 验收据此核对「碰撞体数据 == 渲染楼体」。 */
        get selectedFocus() { return pr.current.selectedFocusRef?.current ?? null; },
        get colliders() { return pr.current.colliders ?? []; },
        get bounds() { return pr.current.bounds ?? null; },
        get groundY() { return pr.current.groundY ?? null; },
        camera,
      };
    }

    return () => {
      camera.rotation.order = 'XYZ';
      if (targetRef) targetRef.current = null;
      if (aimRefs) {
        aimRefs.current.yaw = null;
        aimRefs.current.fov = null;
      }
      rig.current = null;
      setStoreReady(false);
      setStoreColliding(false);
    };
    // 仅挂载时初始化一次；后续 props 变化由 rig 内的目标量吸收
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [camera, gl]);

  // ── 模式切换（受控 prop 或 store 变化时）─────────────────────────
  useEffect(() => {
    const s = rig.current;
    if (!s) return;
    const allowed = pr.current.allowedModes;
    const next = allowed && !allowed.includes(mode) ? allowed[0] : mode;
    if (next === s.mode) return;
    const from = s.mode;
    beginTransition(s, from, next, camera, pr.current);
    // targetRef 仅在 orbit 模式暴露：自由/漫游没有「聚焦点」语义，
    // 让 CameraViewReporter 走「上报相机自身」的回落分支。
    if (targetRef) targetRef.current = next === 'orbit' ? { target: s.target } : null;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, camera]);

  /** 复位到挂载时的 home 位姿（同样走过渡，避免瞬移的方向感断裂）。 */
  const resetHome = () => {
    const s = rig.current;
    if (!s) return;
    const home = s.home;
    if (s.mode === 'orbit') {
      s.targetGoal.copy(home.target);
      s.radiusGoal = home.radius;
      s.phiGoal = home.phi;
      s.thetaGoal = home.theta;
    } else {
      s.pos.copy(home.pos);
      s.yawGoal = home.yaw;
      s.pitchGoal = home.pitch;
      s.vel.set(0, 0, 0);
    }
    s.userFovGoal = home.fov;
    beginTransition(s, s.mode, s.mode, camera, pr.current);
  };

  // ── 键盘 ────────────────────────────────────────────────────────
  useEffect(() => {
    const isVerticalKey = (code: string): boolean => {
      const vk = pr.current.verticalKeys;
      return !!vk && (vk.up.includes(code) || vk.down.includes(code));
    };
    const setModeIfAllowed = (m: FreeViewMode) => {
      const allowed = pr.current.allowedModes;
      if (allowed && !allowed.includes(m)) return;
      setStoreMode(m);
    };

    const onKeyDown = (e: KeyboardEvent) => {
      // 焦点在按钮上时，空格/回车属于「激活按钮」，不劫持为相机升降（可访问性）
      const active = document.activeElement as HTMLElement | null;
      if (active && (active.tagName === 'BUTTON' || active.tagName === 'A')
        && (e.code === 'Space' || e.code === 'Enter')) return;
      if (!shouldCaptureKey(e)) return;

      if (MOVE_KEYS.has(e.code) || isVerticalKey(e.code)) {
        keySet.current.add(e.code);
        if (SCROLL_KEYS.has(e.code)) e.preventDefault();
      }

      const s = rig.current;
      if (!s || e.repeat) return;

      switch (e.code) {
        case 'KeyV': {
          // 缺省 allowedModes ⇒ 三态全开，V 在其上循环（不能因为「未限制」就不切）
          const allowed = pr.current.allowedModes ?? FREE_VIEW_MODES;
          if (allowed.length > 0) {
            const i = allowed.indexOf(modeRef.current);
            setModeIfAllowed(allowed[(i + 1) % allowed.length]);
          }
          break;
        }
        case 'Digit1': setModeIfAllowed('orbit'); break;
        case 'Digit2': setModeIfAllowed('fly'); break;
        // 「俯瞰 ↔ 自由」是最高频往返
        case 'KeyF': setModeIfAllowed(modeRef.current === 'fly' ? 'orbit' : 'fly'); break;
        case 'KeyR': resetHome(); break;
        case 'KeyH':
        case 'Slash': store.getState().toggleHelp(); break;
        case 'BracketLeft': store.getState().stepTier(-1); break;
        case 'BracketRight': store.getState().stepTier(1); break;
        case 'Escape': store.getState().setHelpOpen(false); break;
        default: break;
      }
    };
    const onKeyUp = (e: KeyboardEvent) => {
      keySet.current.delete(e.code);
    };
    // 页面失焦：清空按键 **且** 速度归零 —— 否则切回窗口瞬间相机继续漂移（R16）
    const onBlur = () => {
      keySet.current.clear();
      drag.current.active = false;
      const s = rig.current;
      if (s) s.vel.set(0, 0, 0);
    };

    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('keyup', onKeyUp);
    window.addEventListener('blur', onBlur);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('keyup', onKeyUp);
      window.removeEventListener('blur', onBlur);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [setStoreMode, store]);

  // ── 鼠标 / 滚轮 / 右键菜单 ───────────────────────────────────────
  useEffect(() => {
    const el = gl.domElement;

    const onDown = (e: PointerEvent) => {
      if (!isButtonUsedForCamera(modeRef.current, e.button)) return;
      const d = drag.current;
      d.active = true;
      d.button = e.button;
      d.x = e.clientX;
      d.y = e.clientY;
      d.moved = 0;
      el.setPointerCapture(e.pointerId);
    };
    const onMove = (e: PointerEvent) => {
      const d = drag.current;
      if (!d.active) return;
      const dx = e.clientX - d.x;
      const dy = e.clientY - d.y;
      d.x = e.clientX;
      d.y = e.clientY;
      d.moved += Math.abs(dx) + Math.abs(dy);
      const s = rig.current;
      if (!s) return;
      const p = pr.current;
      const rs = p.rotateSpeed ?? 0.0032;
      if (s.mode === 'orbit') {
        if (d.button === 0) {
          // 左拖绕目标公转
          s.thetaGoal -= dx * rs;
          s.phiGoal = clamp(s.phiGoal - dy * rs, p.minPolar ?? minPolar, p.maxPolar ?? maxPolar);
        } else if (d.button === 2) {
          panOrbit(s, dx, dy, el.clientHeight, p);
        }
      } else if (d.button === 2) {
        // 右拖 look（fly / walk）
        const lim = p.pitchLimit ?? pitchLimit;
        s.yawGoal = wrapAngle(s.yawGoal - dx * rs);
        s.pitchGoal = clamp(s.pitchGoal - dy * rs, -lim, lim);
      } else if (d.button === 1) {
        // 中拖屏幕平面视口平移（朝向不变）
        panFly(s, dx, dy, el.clientHeight, p);
      }
    };
    const onUp = (e: PointerEvent) => {
      const d = drag.current;
      if (d.active && d.moved > DRAG_THRESHOLD_PX) {
        // 拖拽过 ⇒ 吞掉紧随其后的那一次 click（避免旋转相机顺带选中城区，R10）
        suppressUntil.current = performance.now() + CLICK_SUPPRESS_MS;
      }
      d.active = false;
      if (el.hasPointerCapture(e.pointerId)) el.releasePointerCapture(e.pointerId);
    };
    const onWheel = (e: WheelEvent) => {
      const s = rig.current;
      if (!s) return;
      e.preventDefault();
      const p = pr.current;
      if (s.mode === 'orbit') {
        // 滚轮缩放视距（指数律，跨倍率手感一致）
        s.radiusGoal = clamp(
          s.radiusGoal * Math.exp(e.deltaY * (p.zoomSpeed ?? zoomSpeed)),
          p.minDistance ?? 2,
          p.maxDistance ?? 100,
        );
      } else {
        s.userFovGoal = clamp(s.userFovGoal + e.deltaY * 0.03, p.fovMin ?? fovMin, p.fovMax ?? fovMax);
      }
    };
    // 禁用右键默认菜单（否则 pan/look 一按住就弹浏览器菜单，R16）
    const onContextMenu = (e: Event) => e.preventDefault();
    // 吞 click：挂在 window **捕获阶段**，R3F（挂在 canvas 上）的处理器就收不到。
    const onClickCapture = (e: MouseEvent) => {
      if (performance.now() > suppressUntil.current) return;
      if (e.target !== el) return;
      suppressUntil.current = 0;
      e.stopPropagation();
      e.preventDefault();
    };

    el.addEventListener('pointerdown', onDown);
    el.addEventListener('pointermove', onMove);
    el.addEventListener('pointerup', onUp);
    el.addEventListener('pointercancel', onUp);
    el.addEventListener('wheel', onWheel, { passive: false });
    el.addEventListener('contextmenu', onContextMenu);
    window.addEventListener('click', onClickCapture, true);
    return () => {
      el.removeEventListener('pointerdown', onDown);
      el.removeEventListener('pointermove', onMove);
      el.removeEventListener('pointerup', onUp);
      el.removeEventListener('pointercancel', onUp);
      el.removeEventListener('wheel', onWheel);
      el.removeEventListener('contextmenu', onContextMenu);
      window.removeEventListener('click', onClickCapture, true);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [gl]);

  // ── 每帧积分 ───────────────────────────────────────────────────
  useFrame((_state, rawDt) => {
    const s = rig.current;
    if (!s) return;
    const dt = Math.min(rawDt, 0.1);
    const p = pr.current;

    // 聚焦请求（小地图 / 面板点击 → 平滑移焦），到位自动清空
    if (focusRef?.current) {
      focusGoal.current = _v2.set(focusRef.current.x, 0, focusRef.current.z);
      focusRef.current = null;
    }
    if (s.mode === 'orbit') {
      if (focusGoal.current) {
        dampVec3(s.targetGoal, focusGoal.current, LAMBDA_MOVE, dt);
        clampTarget(s.targetGoal, p);
        if (s.target.distanceTo(focusGoal.current) < 0.05) focusGoal.current = null;
      }
    } else {
      focusGoal.current = null;
    }

    if (s.transitioning) {
      stepTransition(s, dt, camera);
      if (s.transitioning) return;
    }

    s.tier = tierRef.current;
    const keys = keySet.current;
    if (s.mode === 'orbit') stepOrbit(s, dt, p, camera, keys, s.tier);
    else stepFly(s, dt, p, camera, keys, s.tier, selectedFocusRef?.current ?? null);

    // 动态 FOV（R5）
    const appliedFov = computeFov(s, dt, p, s.mode);
    if (Math.abs(camera.fov - appliedFov) > 0.005) {
      camera.fov = appliedFov;
      camera.updateProjectionMatrix();
    }

    // 碰撞指示灯（仅状态翻转才写 store，见 freeViewStore 频率纪律）
    if (s.collisionHit !== lastHit.current) {
      lastHit.current = s.collisionHit;
      setStoreColliding(s.collisionHit);
    }

    // 视锥上报（fly/walk 才有朝向意义；orbit 交给聚焦点）
    if (aimRefs) {
      if (s.mode === 'orbit') {
        aimRefs.current.yaw = null;
        aimRefs.current.fov = null;
      } else {
        aimRefs.current.yaw = s.yaw;
        aimRefs.current.fov = camera.fov;
      }
    }
  });

  return null;
}

/** 轨道模式：右拖在相机屏幕平面内平移聚焦点（与 OrbitControls 的观感一致）。 */
function panOrbit(
  s: RigState,
  dx: number,
  dy: number,
  viewportH: number,
  pr: FreeViewControlsProps,
): void {
  if (viewportH <= 0) return;
  // 相机基向量（当前球坐标对应的朝向）
  quatFromYawPitch(_quat, s.theta, s.phi);
  _mat.makeRotationFromQuaternion(_quat);
  _right.setFromMatrixColumn(_mat, 0);
  _up.setFromMatrixColumn(_mat, 1);
  // 透视下 1 像素对应的世界尺度：2·r·tan(fov/2) / viewportH
  const scale = (2 * s.radius * Math.tan(THREE.MathUtils.degToRad(s.userFov) / 2)) / viewportH;
  const k = scale * (pr.panSpeed ?? 1);
  s.targetGoal.addScaledVector(_right, -dx * k);
  s.targetGoal.addScaledVector(_up, dy * k);
  clampTarget(s.targetGoal, pr);
}

/** 自由模式：中拖在垂直于视线的屏幕平面内平移相机（朝向不变）。 */
function panFly(
  s: RigState,
  dx: number,
  dy: number,
  viewportH: number,
  pr: FreeViewControlsProps,
): void {
  if (viewportH <= 0) return;
  quatFromYawPitch(_quat, s.yaw, s.pitch);
  _mat.makeRotationFromQuaternion(_quat);
  _right.setFromMatrixColumn(_mat, 0);
  _up.setFromMatrixColumn(_mat, 1);
  const scale = Math.max(0.05, 2 * Math.abs(s.pos.y) * Math.tan(THREE.MathUtils.degToRad(s.userFov) / 2)) / viewportH;
  const k = scale * (pr.panSpeed ?? 1);
  s.pos.addScaledVector(_right, -dx * k);
  s.pos.addScaledVector(_up, dy * k);
}
