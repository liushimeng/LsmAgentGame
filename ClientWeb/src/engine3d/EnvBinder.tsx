/**
 * engine3d/EnvBinder — 天空环境反射绑定（PMREM 烘焙 Sky → scene.environment）。
 *
 * 自 components/virtualCity/EnvBinder.tsx 迁入并参数化（22-3D世界升级与引擎模块化，
 * 16-3D城市WebGL质感与城市补全 · 阶段 R 的原始契约不变）：
 * 玻璃幕墙 / 金属件 / 车身等 PBR 材质自动获得天空反射
 * （各材质用自身 envMapIntensity 细调，与全局 intensity 相乘）。
 *
 * 生命周期契约（2026-09-22 视觉验收教训）：
 *   - 一次性生成（等价 frames=1），无每帧开销。
 *   - PMREMGenerator 与 env RenderTarget **保持存活**直到组件卸载统一 dispose——
 *     「生成后立即 dispose 生成器」的长寿命 RT 在部分渲染器上会被采样成纯白，
 *     表现为「页面运行数十秒后整场景泛白」（验收实测）。three 官方示例同款模式。
 *   - 组件卸载：environment 置 null + 释放 RT / 生成器 / 离屏 sky。
 *   - WebGL 上下文异常时 try/catch 静默降级（无环境反射 ≠ 不可用）。
 *   - 软件渲染器（SwiftShader 等）直接跳过 PMREM（quality.ts::isSoftwareRenderer）。
 */

import { useEffect } from 'react';
import { useThree } from '@react-three/fiber';
import * as THREE from 'three';
import { Sky as SkyImpl } from 'three-stdlib';
import { isSoftwareRenderer } from './quality';

export interface EnvBinderProps {
  /** Sky 太阳方向（归一化 ×100 量级，与场景主方向光同向观感最佳）。 */
  sunPosition?: [number, number, number];
  /** 大气湍流系数（drei <Sky> 同款语义）。 */
  turbidity?: number;
  /** 瑞利散射系数。 */
  rayleigh?: number;
  /**
   * 全局环境反射强度（scene.environmentIntensity，r163+）。
   * Sky PMREM 亮度很高，1.0 会让未显式设置 envMapIntensity 的漫反射面全向受光
   * 过曝发白（2026-09-22 验收实测）；0.35 = 反射保留、漫反射不过曝。
   */
  intensity?: number;
}

const DEFAULT_SUN: [number, number, number] = [51.6, 82.6, 41.3];

export function EnvBinder({
  sunPosition = DEFAULT_SUN,
  turbidity = 6,
  rayleigh = 1.2,
  intensity = 0.35,
}: EnvBinderProps) {
  const scene = useThree((s) => s.scene);
  const gl = useThree((s) => s.gl);

  useEffect(() => {
    let disposed = false;
    /** 统一释放（组件卸载时才调用——见文件头生命周期契约）。 */
    let cleanup: (() => void) | undefined;
    try {
      if (isSoftwareRenderer(gl)) {
        // 软件渲染器：跳过 PMREM（见 quality.ts），仅保留 scene 清理语义
        cleanup = () => {
          scene.environment = null;
          scene.environmentIntensity = 1.0;
        };
        return;
      }
      // three-stdlib Sky 类（drei <Sky /> 的底层实现），离屏构造不进 React 树
      const sky = new SkyImpl();
      sky.scale.setScalar(1000);
      const skyMat = sky.material as THREE.ShaderMaterial;
      skyMat.uniforms.sunPosition.value = new THREE.Vector3(...sunPosition);
      skyMat.uniforms.turbidity.value = turbidity;
      skyMat.uniforms.rayleigh.value = rayleigh;

      const pmrem = new THREE.PMREMGenerator(gl);
      // fromScene 需要 Scene 容器：离屏挂载 sky 后烘焙
      const envScene = new THREE.Scene();
      envScene.add(sky);
      const env = pmrem.fromScene(envScene, 0.04);
      envScene.remove(sky);

      const release = () => {
        env.dispose();
        pmrem.dispose();
        sky.geometry.dispose();
        skyMat.dispose();
      };

      if (!disposed) {
        scene.environment = env.texture;
        // r163+：全局环境强度（所有材质的环境贡献 × 此值）
        scene.environmentIntensity = intensity;
        cleanup = () => {
          scene.environment = null;
          scene.environmentIntensity = 1.0;
          release();
        };
      } else {
        // 竞态：effect 尚未提交即被卸载 → 只释放资源，不碰 scene
        release();
      }
    } catch {
      // 降级：无环境反射（材质 envMapIntensity 自然无效），不报错
      cleanup = () => {
        scene.environment = null;
        scene.environmentIntensity = 1.0;
      };
    }
    return () => {
      disposed = true;
      cleanup?.();
    };
    // sunPosition/turbidity/rayleigh/intensity 视为静态配置（与天空 uniforms 同源），不列依赖
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scene, gl]);

  return null;
}

export default EnvBinder;
