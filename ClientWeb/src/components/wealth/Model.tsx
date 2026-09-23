/**
 * Model — 通用 GLB 渲染组件（19-Blender3D模型集成 · 阶段 A）。
 *
 * 契约（02-架构设计 §4）：
 *   - 必传：url + 任一 children fallback（缺失 / 失败 / 加载中 三态都走 fallback）
 *   - 每实例独立 scene.clone(true)：共享 GLTFLoader 缓存的 scene 会导致多组件 transform 互相污染
 *   - playAnimation=true 时自动调度 animations[0] clip（行人 walk）
 *   - 降级链：url==='' 或加载失败 → children 渲染，零代码分支
 *
 * 设计动机（为什么用 children fallback 而不是 if/else 切换）：
 *   - 调用方接入零侵入：<Model url={...}>{原程序化几何}</Model>，加载成功 → 渲染 .glb 不渲染 children
 *   - 单 .glb 致命问题（mesh 错位 / 骨骼丢失）→ 改 modelUrl 调用即可，组件无需改动
 *   - 删除 .glb 文件 + 不动一行代码 → 自动回退原行为（与原程序化几何像素一致）
 *
 * 性能注：
 *   - 56 个 PedestrianV3 → 56 次 .clone(true)（~1ms/次，总 < 60ms）
 *   - clone 是 shallow copy：material / geometry 引用共享 → GPU 上传只一次
 *   - mixer per-instance（不能共享）：mixer 持有 root 引用，独立 update / dispose
 */
import { useEffect, useMemo, useRef } from 'react';
import type { ReactNode } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { useSharedGLTF } from './modelCache';

interface Props {
  /** .glb URL；空字符串 = 走 fallback（不发起网络请求） */
  url: string;
  /** 模型 root 位置；fallback 不感知此 prop（由 children 自行决定 position） */
  position?: [number, number, number];
  rotation?: [number, number, number];
  scale?: number | [number, number, number];
  /** 是否播放 animations[0]（仅 characters/pedestrian_walk 用） */
  playAnimation?: boolean;
  /** 加载中 / 失败 / url 缺失 时渲染的 fallback 几何（必传；modelUrl 返回 '' 时原行为即 children） */
  children?: ReactNode;
  castShadow?: boolean;
  receiveShadow?: boolean;
}

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export function Model({
  url,
  position,
  rotation,
  scale,
  playAnimation = false,
  children,
  castShadow = false,
  receiveShadow = false,
}: Props) {
  const { scene, animations } = useSharedGLTF(url);

  // 必须 .clone(true) —— 共享 scene 会让多组件 transform 互相覆盖
  const cloned = useMemo(() => (scene ? scene.clone(true) : null), [scene]);

  // 动画 mixer（per-instance；animations[0] clip 共享）
  const mixerRef = useRef<THREE.AnimationMixer | null>(null);
  useEffect(() => {
    if (!cloned || animations.length === 0 || !playAnimation) return;
    const m = new THREE.AnimationMixer(cloned);
    const action = m.clipAction(animations[0]);
    if (REDUCED_MOTION) {
      m.stopAllAction();
    } else {
      action.play();
    }
    mixerRef.current = m;
    return () => {
      m.stopAllAction();
      m.uncacheRoot(cloned);
      mixerRef.current = null;
    };
  }, [cloned, animations, playAnimation]);

  useFrame((_state, delta) => {
    mixerRef.current?.update(delta);
  });

  // 降级链核心：url 缺失 / 加载失败 / 加载中 → 直接渲染 fallback
  if (!cloned) return <>{children}</>;
  return (
    <primitive
      object={cloned}
      position={position}
      rotation={rotation}
      scale={scale}
      castShadow={castShadow}
      receiveShadow={receiveShadow}
    />
  );
}
