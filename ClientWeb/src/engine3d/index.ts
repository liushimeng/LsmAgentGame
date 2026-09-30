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

// 批次 29：GLB 尺寸/落地/变换规约校验器（dev-only 护栏；目标尺寸由游戏侧注册）
export {
  registerModelSizeTarget,
  clearModelSizeTargets,
  registeredModelSizeTargets,
  checkModelSize,
  probeModelSize,
  animatedNodeNames,
  measureModelBox,
  measureModelSize,
} from './glbSizeGuard';
export type { ModelSizeTarget, ModelSizeReport, ModelSizeProbe } from './glbSizeGuard';

// 批次 28 二轮：通用几何合并（DC 攻坚；引擎层只依赖 three）
export {
  mergeGrouped,
  mergeParts,
  boxFaces,
  boxFacesUV,
  boxPart,
  cylPart,
} from './geoMerge';
export type { MergePart, GroupedMergePart, BoxUVSpec } from './geoMerge';

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

// 批次 27：昼夜循环光 rig + 雨/雪粒子（游戏无关，虚拟城市首次消费）
export { DayNightCycle } from './DayNightCycle';
export type {
  DayNightCycleProps,
  DayNightSample,
  DayNightSnapshot,
} from './DayNightCycle';
// 批次 30 B1：自写渐变天穹（四段色带 + 星 + 月；替代夜间恒白的 drei Sky）
export { DayNightSky } from './DayNightSky';
export type { DayNightSkyProps, DayNightSkyState } from './DayNightSky';
export { WeatherFX } from './WeatherFX';
export type { WeatherFXProps, WeatherSample } from './WeatherFX';

export {
  isSoftwareRenderer,
  detectQualityTier,
  knownQualityTier,
  urlQualityOverride,
  blenderModelsEnabled,
  QUALITY_PRESETS,
} from './quality';
export type { QualityTier, QualityPreset } from './quality';

export { CameraViewReporter } from './controls/CameraViewReporter';
export type { CameraView, TargetLike } from './controls/CameraViewReporter';

// 批次 32：自由视角系统（轨道 / 全自由六自由度 / 街景漫游 三态统一控制器，
// 替代批次 22 的 OrbitControls + WalkControls 双挂载）。
export { FreeViewControls } from './controls/FreeViewControls';
export type {
  FreeViewControlsProps,
  FreeViewTargetRef,
  FreeViewAim,
  FocusTarget,
} from './controls/FreeViewControls';
export { FreeViewHud } from './controls/FreeViewHud';
export type { FreeViewHudProps, FreeViewLabels } from './controls/FreeViewHud';
export {
  useFreeView,
  freeViewApi,
  FREE_VIEW_MODES,
  FREE_VIEW_TIERS,
  FREE_VIEW_TIER_COUNT,
} from './controls/freeViewStore';
export type {
  FreeViewMode,
  FreeViewState,
  FreeViewTierIndex,
} from './controls/freeViewStore';
export { resolveCameraCollision, boxFromCenter } from './controls/cameraCollision';
export type {
  CameraBox,
  CameraBounds,
  CollisionOptions,
  CollisionResult,
} from './controls/cameraCollision';
export {
  clamp,
  damp,
  dampVec3,
  dampFactor,
  easeInOutCubic,
  isTextEditingTarget,
  shouldCaptureKey,
} from './controls/cameraMath';
export type { PoseLerpMode, Spherical } from './controls/cameraMath';
