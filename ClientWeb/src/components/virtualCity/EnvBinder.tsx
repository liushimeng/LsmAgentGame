/**
 * EnvBinder — 天空环境反射绑定（16-3D城市WebGL质感与城市补全 · 阶段 R）：
 *
 * 用离屏 <Sky>（与 VirtualCityCityMap 主天空同 uniforms）生成 PMREM 环境贴图，
 * 挂到 scene.environment —— 玻璃幕墙 / 金属件 / 车身等 PBR 材质自动获得
 * 天空反射（envMapIntensity 在 building_shapes / Vehicle 各材质上分别调）。
 *
 * 生命周期契约（02-架构设计 §2；2026-09-22 视觉验收教训修订）：
 *   - 一次性生成（等价 frames=1），无每帧开销。
 *   - PMREMGenerator 与 env RenderTarget **保持存活**直到组件卸载统一 dispose——
 *     「生成后立即 dispose 生成器」的长寿命 RT 在部分渲染器上会被采样成纯白，
 *     表现为「页面运行数十秒后整城泛白」（验收实测）。three 官方示例同款模式。
 *   - 组件卸载：environment 置 null + 释放 RT / 生成器 / 离屏 sky。
 *   - WebGL 上下文异常时 try/catch 静默降级（无环境反射 ≠ 不可用）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §2。
 */

import { useEffect } from 'react';
import { useThree } from '@react-three/fiber';
import * as THREE from 'three';
import { Sky as SkyImpl } from 'three-stdlib';

/** 与 VirtualCityCityMap.SKY_SUN_POSITION 同源（复制常量避免循环 import）。 */
const SKY_SUN: [number, number, number] = [51.6, 82.6, 41.3];
/** 与主天空一致的湍流 / 瑞利系数（反射色更接近真实天空观感）。 */
const SKY_TURBIDITY = 6;
const SKY_RAYLEIGH = 1.2;
/**
 * 全局环境反射强度。Sky PMREM 亮度很高，默认 1.0 会让未显式设置
 * envMapIntensity 的材质（地面/道路/底板）全向受光 → 整城过曝发白
 * （2026-09-22 视觉验收实测）。0.35 = 玻璃反射保留、漫反射面不过曝；
 * 各材质细调仍走自身 envMapIntensity（与该值相乘）。
 */
const ENV_INTENSITY = 0.35;

/**
 * 软件渲染器检测（无独立 GPU 的环境：CI 无头浏览器 / 远程桌面 / 集显省电模式）。
 * SwiftShader 等软件光栅器上，长寿命 PMREM RenderTarget 会在运行数十秒后被
 * 采样成纯白（2026-09-22 视觉验收实测：整城泛白）——此类环境直接跳过环境
 * 反射（材质 envMapIntensity 自然失效，城市保持稳定观感），真机 GPU 正常启用。
 */
function isSoftwareRenderer(gl: THREE.WebGLRenderer): boolean {
  try {
    const ctx = gl.getContext();
    const ext = ctx.getExtension('WEBGL_debug_renderer_info');
    if (!ext) return false;
    const renderer = String(ctx.getParameter(ext.UNMASKED_RENDERER_WEBGL) ?? '');
    return /swiftshader|software|llvmpipe|basic render/i.test(renderer);
  } catch {
    return false;
  }
}

export function EnvBinder() {
  const scene = useThree((s) => s.scene);
  const gl = useThree((s) => s.gl);

  useEffect(() => {
    let disposed = false;
    /** 统一释放（组件卸载时才调用——见文件头生命周期契约）。 */
    let cleanup: (() => void) | undefined;
    try {
      if (isSoftwareRenderer(gl)) {
        // 软件渲染器：跳过 PMREM（见函数头注释），仅保留 scene 清理语义
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
      skyMat.uniforms.sunPosition.value = new THREE.Vector3(...SKY_SUN);
      skyMat.uniforms.turbidity.value = SKY_TURBIDITY;
      skyMat.uniforms.rayleigh.value = SKY_RAYLEIGH;

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
        scene.environmentIntensity = ENV_INTENSITY;
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
  }, [scene, gl]);

  return null;
}

export default EnvBinder;
