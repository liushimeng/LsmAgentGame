/**
 * engine3d/controls/FocusLerpController — 平滑聚焦控制器。
 *
 * 自 VirtualCityCityMap 内联 FocusController 泛化提取（22-3D世界升级与引擎模块化）：
 * focusRef 有值 → 每帧 lerp 控制器 target 到目标点（y=0 地平面），到位自动清空。
 * 小地图 / 面板点击 → 主场景平滑移焦的标准模式。
 */

import { useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import type { TargetLike } from './CameraViewReporter';

/** 聚焦目标（地平面上一点；null = 无聚焦请求）。 */
export interface FocusTarget {
  x: number;
  z: number;
}

interface Props {
  controlsRef: React.MutableRefObject<TargetLike | null>;
  focusRef: React.MutableRefObject<FocusTarget | null>;
  /** lerp 系数（每帧），默认 0.08。 */
  speed?: number;
  /** 到位判定阈值（世界单位），默认 0.05。 */
  epsilon?: number;
}

export function FocusLerpController({ controlsRef, focusRef, speed = 0.08, epsilon = 0.05 }: Props) {
  const tmpRef = useRef(new THREE.Vector3());
  useFrame(() => {
    const c = controlsRef.current;
    const f = focusRef.current;
    if (!c || !f) return;
    const tmp = tmpRef.current;
    tmp.x = f.x;
    tmp.y = 0;
    tmp.z = f.z;
    c.target.lerp(tmp, speed);
    if (c.target.distanceTo(tmp) < epsilon) {
      focusRef.current = null;
    }
  });
  return null;
}
