/**
 * engine3d/controls/CameraViewReporter — 相机视野快照上报（小地图 / 缩略图用）。
 *
 * 自 VirtualCityCityMap 内联 CameraReporter 泛化提取（22-3D世界升级与引擎模块化）：
 * useFrame 每帧把「视野中心 x/z + 相机到中心距离」写入 viewRef（ref 覆写，
 * 不触发 React 渲染）。
 *
 * 泛化点：视野中心来源不再硬编码 OrbitControls——任何带 `target: Vector3`
 * 的控制器 ref 都可注入；`fallbackToCamera` 时 targetRef 为空则上报相机自身
 * 位置（街景漫游等无 orbit target 的模式）。
 */

import { useFrame, useThree } from '@react-three/fiber';
import type * as THREE from 'three';

/** 视野快照：中心点 x/z + 相机到中心的距离（2D 小地图画视野框用）。 */
export interface CameraView {
  x: number;
  z: number;
  dist: number;
}

/** 视野中心来源的最小契约（OrbitControls / MapControls 均满足）。 */
export interface TargetLike {
  target: THREE.Vector3;
}

interface Props {
  /** 视野中心来源；undefined 或 current=null 时按 fallbackToCamera 处理。 */
  targetRef?: React.MutableRefObject<TargetLike | null>;
  /** true：无 target 时上报相机自身位置（dist 用 fallbackDist）。 */
  fallbackToCamera?: boolean;
  /** fallbackToCamera 模式下上报的固定 dist（漫游模式视野框半径）。 */
  fallbackDist?: number;
  viewRef: React.MutableRefObject<CameraView>;
}

export function CameraViewReporter({
  targetRef,
  fallbackToCamera = false,
  fallbackDist = 12,
  viewRef,
}: Props) {
  const camera = useThree((s) => s.camera);
  useFrame(() => {
    const c = targetRef?.current;
    if (c) {
      viewRef.current.x = c.target.x;
      viewRef.current.z = c.target.z;
      viewRef.current.dist = camera.position.distanceTo(c.target);
    } else if (fallbackToCamera) {
      viewRef.current.x = camera.position.x;
      viewRef.current.z = camera.position.z;
      viewRef.current.dist = fallbackDist;
    }
  });
  return null;
}
