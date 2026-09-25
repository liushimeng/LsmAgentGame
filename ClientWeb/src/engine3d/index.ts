/**
 * engine3d — 通用 WebGL/3D 渲染引擎模块（统一出口）。
 *
 * 22-3D世界升级与引擎模块化（tmpPlan/虚拟城市-2.5D升级3D世界与引擎模块化方案-20260925.md）：
 * 从虚拟城市业务代码中抽取的**游戏无关**渲染能力，供当前与未来所有 3D 游戏/程序复用。
 *
 * 分层契约：
 *   - 本目录**禁止** import 任何 `components/<game>/`、`types/<game>`、`assets/images/<game>`
 *     等游戏私有模块（引擎只依赖 three / @react-three/fiber / @react-three/drei / three-stdlib）。
 *   - 游戏业务代码经 `@/engine3d` 单点引入；游戏私有的资产路径拼接留在游戏侧薄适配层
 *     （如 virtualCity/cityPbr.ts）。
 */

export { EngineCanvas } from './EngineCanvas';
export type { EngineCanvasProps } from './EngineCanvas';

export { Model } from './Model';
export { useSharedGLTF, clearModelCache } from './modelCache';
export type { SharedGLTF } from './modelCache';

export {
  useSharedTexture,
  clearTextureCache,
  useSharedPBR,
  withPBR,
} from './textureCache';
export type {
  SharedTextureOpts,
  SharedPBR,
  SharedPBROpts,
} from './textureCache';

export { EnvBinder } from './EnvBinder';
export type { EnvBinderProps } from './EnvBinder';

export {
  isSoftwareRenderer,
  detectQualityTier,
  urlQualityOverride,
  blenderModelsEnabled,
  QUALITY_PRESETS,
} from './quality';
export type { QualityTier, QualityPreset } from './quality';

export { CameraViewReporter } from './controls/CameraViewReporter';
export type { CameraView, TargetLike } from './controls/CameraViewReporter';
export { FocusLerpController } from './controls/FocusLerpController';
export type { FocusTarget } from './controls/FocusLerpController';
export { WalkControls } from './controls/WalkControls';
export type { WalkControlsProps } from './controls/WalkControls';
