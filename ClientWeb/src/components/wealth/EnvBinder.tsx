/**
 * EnvBinder — 天空环境反射绑定（16-3D城市WebGL质感与城市补全 · 阶段 R）：
 *
 * 用离屏 <Sky>（与 WealthCityMap 主天空同 uniforms）生成 PMREM 环境贴图，
 * 挂到 scene.environment —— 玻璃幕墙 / 金属件 / 车身等 PBR 材质自动获得
 * 天空反射（envMapIntensity 在 building_shapes / Vehicle 各材质上分别调）。
 *
 * 性能契约（02-架构设计 §2）：
 *   - 一次性生成（等价 frames=1），无每帧开销；cubeUV 256。
 *   - 生成后立即 dispose 离屏 sky 几何/材质与 PMREMGenerator；
 *     env.texture 归 scene 所有，组件卸载时随 effect cleanup 释放。
 *   - WebGL 上下文异常时 try/catch 静默降级（无环境反射 ≠ 不可用）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §2。
 */

import { useEffect } from 'react';
import { useThree } from '@react-three/fiber';
import * as THREE from 'three';
import { Sky as SkyImpl } from 'three-stdlib';

/** 与 WealthCityMap.SKY_SUN_POSITION 同源（复制常量避免循环 import）。 */
const SKY_SUN: [number, number, number] = [51.6, 82.6, 41.3];
/** 与主天空一致的湍流 / 瑞利系数（反射色更接近真实天空观感）。 */
const SKY_TURBIDITY = 6;
const SKY_RAYLEIGH = 1.2;

export function EnvBinder() {
  const scene = useThree((s) => s.scene);
  const gl = useThree((s) => s.gl);

  useEffect(() => {
    let disposed = false;
    let cleanup: (() => void) | undefined;
    try {
      // three-stdlib Sky 类（drei <Sky /> 的底层实现），离屏构造不进 React 树
      const sky = new SkyImpl();
      sky.scale.setScalar(1000);
      const mat = sky.material as THREE.ShaderMaterial;
      mat.uniforms.sunPosition.value = new THREE.Vector3(...SKY_SUN);
      mat.uniforms.turbidity.value = SKY_TURBIDITY;
      mat.uniforms.rayleigh.value = SKY_RAYLEIGH;

      const pmrem = new THREE.PMREMGenerator(gl);
      // fromScene 需要 Scene 容器：离屏挂载 sky 后烘焙
      const envScene = new THREE.Scene();
      envScene.add(sky);
      const env = pmrem.fromScene(envScene, 0.04);
      envScene.remove(sky);
      if (!disposed) {
        scene.environment = env.texture;
        cleanup = () => {
          scene.environment = null;
          env.dispose();
        };
      }
      // 离屏资源立即释放（env.texture 已由 PMREM 烘焙，不依赖 sky 存活）
      pmrem.dispose();
      sky.geometry.dispose();
      mat.dispose();
    } catch {
      // 降级：无环境反射（材质 envMapIntensity 自然无效），不报错
      cleanup = () => {
        scene.environment = null;
      };
    }
    return () => {
      disposed = true;
      cleanup?.();
    };
  }, [scene, gl]);

  return null;
}

export default EnvBinder;
