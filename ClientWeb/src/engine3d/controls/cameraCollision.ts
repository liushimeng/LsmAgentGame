/**
 * engine3d/controls/cameraCollision — 自由视角的场景碰撞 / 地图边界 / 地面钳制。
 *
 * 批次 32「自由视角系统」新增（对应规格「内置场景碰撞检测与地图边界限制，避免相机穿模」）。
 *
 * 算法：球 vs AABB 最近点推出。
 *   - 对每个盒（先按半径 r 膨胀）求相机位置在盒内的最近点 q；
 *   - 距离 < r ⇒ 沿 (p - q) 推出到恰好 r 处（侧面）；p 落在盒内（距离为 0）⇒
 *     沿最小穿透轴推出到盒面上（防止球心被包在楼里时方向不确定）。
 * 该做法保守、无抖动、天然处理盒角，且**不依赖 Raycaster / BVH**：
 *   遍历几百个静态 AABB + 平方距离早退，每帧成本远小于 0.1 ms。
 *
 * 本文件只依赖 three（引擎层硬约束 §2.1-5：不得 import 任何游戏私有模块）。
 */

import * as THREE from 'three';
import { clamp } from './cameraMath';

/** 轴对齐包围盒（世界坐标）。`minY` 通常取地面高度，`maxY` 取楼顶。 */
export interface CameraBox {
  minX: number;
  maxX: number;
  minY: number;
  maxY: number;
  minZ: number;
  maxZ: number;
}

/** 地图边界包围盒（相机位置被钳制在其内）+ 地面/水面安全高度。 */
export interface CameraBounds {
  min: [number, number, number];
  max: [number, number, number];
}

export interface CollisionOptions {
  /** 碰撞球半径（世界单位）。偏大 = 相机离墙更远；0 = 只做点判据。 */
  radius?: number;
  /** 场景静态碰撞盒（建筑、地形实体等）。 */
  boxes?: readonly CameraBox[];
  /** 地图边界；缺省不钳制。 */
  bounds?: CameraBounds;
  /** 地面安全高度；相机 y 不得低于此值（缺省不钳制）。 */
  groundY?: number;
}

export interface CollisionResult {
  /** 钳制后的相机位置（就地写入 out）。 */
  position: THREE.Vector3;
  /** 本帧是否发生了碰撞推出（R3 的可感知反馈来源 → HUD 碰撞指示灯）。 */
  hit: boolean;
}

const _closest = new THREE.Vector3();

/**
 * 就地解算相机位置。返回 `hit` 表示本帧被碰撞体推出过。
 * 顺序：先碰撞推出（保持在场景「外侧」），再边界/地面钳制（防止被推出到地图外）。
 */
export function resolveCameraCollision(
  position: THREE.Vector3,
  opts: CollisionOptions,
  out: CollisionResult,
): CollisionResult {
  const radius = opts.radius ?? 0;
  const boxes = opts.boxes;
  let hit = false;

  if (boxes && boxes.length > 0 && radius > 0) {
    for (let i = 0; i < boxes.length; i += 1) {
      const b = boxes[i];
      // 平方距离早退：球心到盒（已膨胀 radius）的最短距离 > radius ⇒ 必不相交。
      const dx = position.x < b.minX ? b.minX - position.x : position.x > b.maxX ? position.x - b.maxX : 0;
      const dy = position.y < b.minY ? b.minY - position.y : position.y > b.maxY ? position.y - b.maxY : 0;
      const dz = position.z < b.minZ ? b.minZ - position.z : position.z > b.maxZ ? position.z - b.maxZ : 0;
      if (dx * dx + dy * dy + dz * dz >= radius * radius) continue;

      // 最近点 q = clamp(p, box)
      _closest.set(
        clamp(position.x, b.minX, b.maxX),
        clamp(position.y, b.minY, b.maxY),
        clamp(position.z, b.minZ, b.maxZ),
      );
      const ox = position.x - _closest.x;
      const oy = position.y - _closest.y;
      const oz = position.z - _closest.z;
      const lenSq = ox * ox + oy * oy + oz * oz;

      if (lenSq > 1e-12) {
        const len = Math.sqrt(lenSq);
        const k = radius / len;
        position.x = _closest.x + ox * k;
        position.y = _closest.y + oy * k;
        position.z = _closest.z + oz * k;
      } else {
        // 球心在盒内：沿最小穿透轴推出（把相机放到最近的那个面上）。
        const px = Math.min(position.x - b.minX, b.maxX - position.x);
        const py = Math.min(position.y - b.minY, b.maxY - position.y);
        const pz = Math.min(position.z - b.minZ, b.maxZ - position.z);
        if (px <= py && px <= pz) {
          position.x = position.x - b.minX < b.maxX - position.x ? b.minX - radius : b.maxX + radius;
        } else if (py <= pz) {
          position.y = position.y - b.minY < b.maxY - position.y ? b.minY - radius : b.maxY + radius;
        } else {
          position.z = position.z - b.minZ < b.maxZ - position.z ? b.minZ - radius : b.maxZ + radius;
        }
      }
      hit = true;
    }
  }

  if (opts.groundY !== undefined && position.y < opts.groundY) {
    position.y = opts.groundY;
    hit = true;
  }
  if (opts.bounds) {
    const [bx, by, bz] = opts.bounds.min;
    const [ax, ay, az] = opts.bounds.max;
    if (position.x < bx) { position.x = bx; hit = true; }
    else if (position.x > ax) { position.x = ax; hit = true; }
    if (position.y < by) { position.y = by; hit = true; }
    else if (position.y > ay) { position.y = ay; hit = true; }
    if (position.z < bz) { position.z = bz; hit = true; }
    else if (position.z > az) { position.z = az; hit = true; }
  }

  out.position = position;
  out.hit = hit;
  return out;
}

/** 便捷构造：从中心点 + 全尺寸构造 AABB（楼体装配常用）。 */
export function boxFromCenter(
  cx: number,
  cy: number,
  cz: number,
  w: number,
  h: number,
  d: number,
): CameraBox {
  return {
    minX: cx - w / 2,
    maxX: cx + w / 2,
    minY: cy - h / 2,
    maxY: cy + h / 2,
    minZ: cz - d / 2,
    maxZ: cz + d / 2,
  };
}
