/**
 * engine3d/controls/CameraViewReporter — 相机视野快照上报（小地图 / 缩略图用）。
 *
 * 自 VirtualCityCityMap 内联 CameraReporter 泛化提取（22-3D世界升级与引擎模块化）：
 * useFrame 每帧把「视野中心 x/z + 相机到中心距离」写入 viewRef（ref 覆写，
 * 不触发 React 渲染）。
 *
 * 泛化点：视野中心来源不再硬编码 OrbitControls——任何带 `target: Vector3`
 * 的控制器 ref 都可注入；`fallbackToCamera` 时 targetRef 为空则上报相机自身
 * 位置（自由飞行 / 街景漫游等无 orbit target 的模式）。
 *
 * 批次 32「自由视角系统」增补 `yaw` / `fov` 两个**可选**字段：
 * 轨道俯瞰画轴对齐方框即可，而自由视角需要真实朝向才能画视锥扇形。
 * 字段可选 ⇒ 既有消费方零改动；`targetRef` 有值时（orbit）主动清空二者。
 */

import { useFrame, useThree } from '@react-three/fiber';
import type * as THREE from 'three';
import type { FreeViewAim } from './FreeViewControls';

/** 视野快照：中心点 x/z + 相机到中心的距离（2D 小地图画视野框用）。 */
export interface CameraView {
  x: number;
  z: number;
  dist: number;
  /** 相机水平朝向（弧度，0 = 朝 -Z，与 three 欧拉约定一致）。仅自由视角上报。 */
  yaw?: number;
  /** 垂直视场角（度）。仅自由视角上报。 */
  fov?: number;
}

/** 视野中心来源的最小契约（带 `target: Vector3` 的控制器）。 */
export interface TargetLike {
  target: THREE.Vector3;
}

interface Props {
  /** 视野中心来源；undefined 或 current=null 时按 fallbackToCamera 处理。 */
  targetRef?: React.MutableRefObject<TargetLike | null>;
  /** true：无 target 时上报相机自身位置（dist 用 fallbackDist）。 */
  fallbackToCamera?: boolean;
  /** fallbackToCamera 模式下上报的固定 dist（自由视角的视野框半径）。 */
  fallbackDist?: number;
  /**
   * 自由视角的朝向 / FOV 来源（由 FreeViewControls 每帧写入）。
   * 仅在 fallbackToCamera 分支生效；轨道模式下这两个 ref 为 null。
   */
  aimRefs?: React.MutableRefObject<FreeViewAim>;
  viewRef: React.MutableRefObject<CameraView>;
}

export function CameraViewReporter({
  targetRef,
  fallbackToCamera = false,
  fallbackDist = 12,
  aimRefs,
  viewRef,
}: Props) {
  const camera = useThree((s) => s.camera);
  useFrame(() => {
    const c = targetRef?.current;
    const v = viewRef.current;
    if (c) {
      v.x = c.target.x;
      v.z = c.target.z;
      v.dist = camera.position.distanceTo(c.target);
      // 轨道模式：朝向由消费方自行取道（画轴对齐方框），显式清空避免读到上一次的残留
      v.yaw = undefined;
      v.fov = undefined;
    } else if (fallbackToCamera) {
      v.x = camera.position.x;
      v.z = camera.position.z;
      v.dist = fallbackDist;
      v.yaw = aimRefs?.current.yaw ?? undefined;
      v.fov = aimRefs?.current.fov ?? undefined;
    }
  });
  return null;
}
