/**
 * VirtualCityCityMap — r3f 主场景（08-UI优化 v2 → 22-3D世界升级）。
 *
 * P1-A 改造：
 *   - 删除 gridHelper 黑线（line 56 of v1），地面改用 RepeatWrapping 沥青贴图。
 *   - 单 plane Roads() 替换为分层 <Road />（4 层组合 + 路灯阵列）。
 *   - 追加 hemisphereLight（天/地反弹）+ <fog />（远景雾化）。
 *
 * P1-C 追加：
 *   - <StreetPropsLayer /> 在 Token 之前注入，绘制路灯、树、车辆、行人、屋顶杂物。
 *
 * v2.12 阶段 2（16 城区扩展）：
 *   - 地图 40×40 → 80×80（WORLD_SIZE 常量，地面贴图 / 雾化 / 相机 / 光照等比派生）。
 *   - <StreetPropsLayer districts={...} /> props 化（内部不再 import 城区静态表）。
 *
 * v2.13 阶段 C（13-3D城市渲染优化 · 环境氛围，02-架构设计 §1）：
 *   - drei <Sky /> 天空穹顶接管背景（删除纯色 <color>），sunPosition 与主方向光同向。
 *   - ACESFilmicToneMapping + exposure 1.05；阴影 PCFSoft + 2048 + bias/normalBias，
 *     显式阴影相机 ±WORLD_SIZE*0.6 覆盖全城（默认 ±5 曾导致大部分楼无阴影）。
 *   - 雾色随天际线改 #aeb8c6；ambient 0.45 / hemisphere 0.5 / 日光暖白 #fff2e0。
 *
 * 16-3D城市WebGL质感与城市补全（阶段 R/S/T/U）：
 *   - EnvBinder：PMREM 烘焙 Sky → scene.environment（幕墙反射）+ 冷色填充光。
 *   - Ground：urban_base 城市底色优先（替代满城沥青）。
 *   - RingRoad（CBD 环路）+ CanalBridges（运河 2 桥）+ LandmarksLayer
 *     （喷泉/园路/塔吊/停车场）+ CloudLayer（天空云层）。
 *
 * 批次 20（32 城区地图扩展 + 渲染性能专项，文档 1 §3）：
 *   - WORLD_SIZE 80 → 120 / WORLD_GROUND_SIZE → 160，fog 1.0W/2.0W、SUN ×1.5、
 *     SHADOW_HALF=0.5W、minDistance=0.1W、CANAL_HALF_X 32→48 全部随 W 派生或更新。
 *   - 导出 MAIN_ROAD_MIN_LEN = W×0.15（7 处 len>12 字面量统一派生）。
 *
 * 批次 22（2.5D → 3D 世界升级 + 引擎模块化，tmpPlan/虚拟城市-2.5D升级3D世界与引擎模块化方案-20260925.md）：
 *   - 通用渲染能力全部迁入 `@/engine3d`：EngineCanvas（ACES/PCFSoft/debug info）、
 *     Model/modelCache、textureCache/useSharedPBR、EnvBinder（参数化）、
 *     CameraViewReporter / FocusLerpController（原内联 CameraReporter/FocusController 泛化）。
 *   - 相机解锁 2.5D 俯视约束：maxPolarAngle 1.2 → 1.54（≈88°，可压到近街面视角）。
 *   - 新增「俯瞰 / 街景漫游」双模式：漫游模式挂载 engine3d WalkControls
 *     （WASD + 鼠标拖拽环视，眼高 1.7m），与 OrbitControls 互斥挂载、切换时衔接相机位姿。
 *   - ?debug=1 时挂 window.__cityRenderInfo = renderer.info（经 EngineCanvas 的
 *     debugGlobalName 保持原全局名，CDP 验收工具链兼容）。
 *
 * 批次 24「真实马路与交通设施」（文档 24 §6）：
 *   - RoadsLayer 追加 <TrafficSignals>（trafficSignalsForCity 布点：主干道双端 +
 *     环岛对角，全城实例化 ≈6 draw call + 16s 相位动画）与 <RoadsideBins>
 *     （roadsideBinsForNetwork 布点：主干道两侧 + 公交站台旁，GLB 实例化）；
 *   - 旧 <TrafficLight>（StreetPropsLayer）与 <IntersectionSignals>（CivicLayer）
 *     渲染移除，两文件删除。
 *
 * 批次 26「城市坐标系统与四缘环境带」（lag_docs/虚拟城市/已实现/26-坐标系统与城市边缘环境/01-现状分析与方案设计.md）：
 *   - AtmosphereLayer 之后注入 <CityEdgeLayer />：北雪山 / 西沙漠 / 东森林 /
 *     南海洋+南港 四缘真实环境带（z/x ∈ ±[62,80] 带域，海面延至 z=150）；
 *   - 罗盘契约确立：北 = −Z / 南 = +Z / 东 = +X / 西 = −X（小地图固定朝上=北）；
 *   - 四缘 FarBuildingSilhouette 灰盒剪影移除（真实环境带取代），文件删除。
 *
 * 批次 27「时间比例与昼夜季节天气」（lag_docs/虚拟城市/已实现/27-时间比例与昼夜季节天气/01-方案设计-v1.md）：
 *   - 静态正午光 rig（Sky/fog/四灯/EnvBinder）整段替换为 <CityEnvironmentLayer />
 *     （DayNightCycle 昼夜循环 + WeatherFX 雨/雪 + EnvBinder 正午 PMREM）；
 *     正午基准常量保留在文件顶部（昼夜沿此基准摆动）。
 *
 * 批次 32「自由视角系统」（lag_docs/虚拟城市/已实现/32-自由视角系统/01-方案设计.md）：
 *   - 相机从「drei OrbitControls + engine3d WalkControls 二选一重挂载」换成
 *     engine3d FreeViewControls 单控制器三态（俯瞰 orbit / 全自由六自由度 fly /
 *     街景漫游 walk），切换走 600 ms 位姿过渡（球面/线性插值 + slerp），零跳变；
 *   - 新增场景碰撞（32 区全部建筑 AABB，装配见 freeViewColliders.ts）+
 *     地图边界钳制（±88 / z→155，覆盖四缘带与南海洋）+ 地面钳制 ⇒ 相机不穿楼不出界；
 *   - 新增 Web 端操作界面 <FreeViewHud />（模式分段控件 / 速度档 / 碰撞指示灯 /
 *     键位帮助）与快捷键（V 循环、1·2·3 直达、F 往返、R 复位、H 帮助、[ ] 档位）；
 *   - 小地图视野框在自由视角下改画真实视锥扇形（viewRef 新增可选 yaw / fov）；
 *   - 相机拖拽后松开不再误触城区选中（捕获阶段吞 click）。
 */

import { memo, useMemo, useRef } from 'react';
import {
  EngineCanvas,
  useSharedTexture,
  CameraViewReporter,
  FreeViewControls,
  FreeViewHud,
} from '@/engine3d';
import type { CameraView, FocusTarget, FreeViewAim, TargetLike } from '@/engine3d';
import {
  buildCityCameraColliders,
  FREE_VIEW_BOUNDS,
  FREE_VIEW_COLLISION_RADIUS,
  FREE_VIEW_GROUND_Y,
  selectedTargetFor,
} from './freeViewColliders';
import { SelectionMarker } from './SelectionMarker';
import { useObjectInfoStore } from './objectInfo/objectInfoStore';

// 批次 32 v2：CDP 验收可直接读写 objectInfo selected 状态。
if (typeof window !== 'undefined' && window.location.search.includes('debug=1')) {
  (window as unknown as Record<string, unknown>).__objectInfoStore = useObjectInfoStore;
}
import { CityEnvironmentLayer } from './CityEnvironmentLayer';
import { DistrictBlock } from './DistrictBlock';
import { AgentToken } from './AgentToken';
import { CityVoiceBubbleLayer } from './CityVoiceBubbleLayer';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { Road, lampsForRoad } from './Road';
import { StreetLightsInstanced } from './props/StreetLightsInstanced';
import {
  StreetPropsLayer,
  trafficSignalsForCity,
  roadsideBinsForNetwork,
  busStopsForNetwork,
} from './StreetPropsLayer';
import { TrafficSignals } from './props/TrafficSignals';
import { RoadsideBins } from './props/RoadsideBins';
import { HighwayGates } from './props/HighwayGates';
import { WaterPlane } from './props/WaterPlane';
import { WaterMist } from './props/WaterMist';
import { AtmosphereLayer } from './AtmosphereLayer';
import { CityEdgeLayer } from './edge/CityEdgeLayer';
import { RingRoad } from './RingRoad';
import { FirstRingRoad } from './FirstRingRoad';
import { RoadMarkings } from './RoadMarkings';
import { buildRoadNetwork } from './roadNetwork';
import { CanalBridges } from './CanalBridge';
import { LandmarksLayer } from './LandmarksLayer';
import { CloudLayer } from './props/CloudLayer';
import { CivicLayer } from './CivicLayer';
import { PerfHud } from './PerfHud';
import { SceneDebugProbe } from './SceneDebugProbe';
import { ObjectInfoOverlay } from './objectInfo/ObjectInfoOverlay';
import { useObjectInfoProps } from './objectInfo/useObjectInfoProps';
import { groundTileUrl, streetTileUrl } from '@/assets/images/virtualCity';
import {
  VIRTUAL_CITY_DISTRICTS,
  districtCenter,
  type VirtualCityDistrictId,
  type VirtualCityGameState,
} from '@/types/virtualCity';
import { u } from './cityScale';

/** 主场景 → 小地图的相机视野快照（ref 每帧覆写，不触发 React 渲染）。 */
export type VirtualCityCameraView = CameraView;

// ── v2.12 阶段 2 世界尺寸常量（地图 40×40 → 80×80，面积 ×4）──────────
// 所有 40 相关魔法数收敛于此；地面贴图 / 雾化 / 相机 / 光照按比例派生。

/** 世界边长（世界单位；1 单位 = 10 米，见 cityScale.ts）。
 *  批次 20（文档 1 §3.1）：80 → 120（1200m×1200m，面积 ×2.25），唯一总闸，其余派生。 */
export const WORLD_SIZE = 120;
/** 18 · 阶段 AA：地面 plane 边长（建成区外的腹地）。只影响 Ground 与其贴图 repeat，
 *  建成区/相机/雾化语义不变。原 WORLD_SIZE=80 仍是「城区半径」语义。
 *  批次 20：120 → 160（腹地 + 远景环带用）。 */
export const WORLD_GROUND_SIZE = 160;
/** 地面贴图每 8 单位平铺一次（与城区底板 8×8 同标尺）。 */
export const GROUND_TILE = 8;
/** 地面贴图重复次数 = WORLD_GROUND_SIZE / GROUND_TILE（批次 20: 160/8=20）。 */
export const GROUND_REPEAT = WORLD_GROUND_SIZE / GROUND_TILE;
/** 主干道判定阈值：放射路 len > 此值 → main（车道/路灯/行道树/红绿灯/公交站台全随它）。
 *  批次 20（文档 1 §3.2）：原写死 12 → 派生 WORLD_SIZE*0.15（80→12 / 120→18），
 *  防止 32 区 100% 主干道化导致道具超线性爆炸。 */
export const MAIN_ROAD_MIN_LEN = WORLD_SIZE * 0.15;
/** 远景雾化近/远平面。批次 20（文档 1 §3.1）：批次 11 已证「不可等比外推」，
 *  按 80 时代绝对观感（100/200）近似保留 → 1.0×W / 2.0×W（=120 / 240），截图验收定稿。 */
export const FOG_NEAR = WORLD_SIZE * 1.0;
export const FOG_FAR = WORLD_SIZE * 2.0;
/** 相机初始位置与 OrbitControls maxDistance（随世界边长等比缩放）。 */
export const CAMERA_START: [number, number, number] = [WORLD_SIZE * 0.35, WORLD_SIZE * 0.3, WORLD_SIZE * 0.35];
export const ORBIT_MAX_DISTANCE = WORLD_SIZE;
/** 批次 20（文档 1 §3.1）：最近距离 8 写死 → W×0.1=12，防大地图穿地视角。 */
export const ORBIT_MIN_DISTANCE = WORLD_SIZE * 0.1;

// ── v2.13 阶段 C 光照/天空常量（批次 27 起为「正午基准」：静态光 rig 已整体
//    替换为 <CityEnvironmentLayer> → engine3d DayNightCycle 动态驱动，昼夜沿
//    此基准摆动；雾色 #aeb8c6 ↔ 夜 #0a0e18 的插值见 DayNightCycle 常量）──
/** 主方向光轨道半径（= 正午基准方向 [30,48,24] 模长；批次 20 [20,32,16] ×1.5 等比）。 */
const SUN_DISTANCE = Math.hypot(30, 48, 24);
/** Sky 太阳方向（正午基准；EnvBinder PMREM 烘焙同源。原 [30,48,24] 与
 *  旧 [20,32,16] 同射线，归一化值 [0.516,0.826,0.413] 不变）。 */
const SKY_SUN_POSITION: [number, number, number] = [51.6, 82.6, 41.3];
/** 方向光阴影相机半宽：批次 20 采纳批次 15 §9.3 建议 W×0.6→W×0.5=60
 *  （shadow map 保持 2048，low 质量档 1024 由 DayNightCycle 按质量档定）。 */
const SHADOW_CAMERA_HALF = WORLD_SIZE * 0.5;

// ── 批次 22：3D 世界相机常量 ─────────────────────────────────
/** 批次 22：俯仰角上限 1.2（≈69°，2.5D 锁定俯视）→ 1.54（≈88°，可压到近街面视角，
 *  仍留约 2° 余量防止完全平视时地平线穿帮）。 */
const ORBIT_MAX_POLAR_ANGLE = 1.54;
/** 自由飞行基础移速：20 m/s（无人机巡航量级）。批次 34 起由速度倍率档
 *  X1/X2/X4/X8/X16（Shift 切换）缩放 ⇒ 全域 20 ~ 320 m/s，最高档用来俯瞰全城。 */
const FLY_SPEED = u(20);
/** 轨道模式下平移聚焦点的移速：10 m/s（俯瞰时小幅挪移足够）。 */
const ORBIT_PAN_SPEED = u(10);
/** 俯瞰归零点固定落点（批次 32 v2）：城市中心 [0,0,0] 上空、视觉朝下。
 *  「俯瞰」不再是任意时刻的轨道相机，而是**只有一个固定落点** ——
 *  按 orbit / 俯瞰归零点按钮 = 飞到并停在这个点。坐标与批次 22 的 CAMERA_START 一致
 *  （[42,36,42]），首帧观感零回归。 */
const FOCUS_TARGET: [number, number, number] = [0, 0, 0];
const FOCUS_RADIUS = WORLD_SIZE * 0.35;       // 42 单位 = 420 m
const FOCUS_PHI = 0.85;                        // ≈ 49°，标准 3/4 俯瞰
const FOCUS_THETA = Math.PI / 4;               // 朝向城市中心的对角
/** 「俯瞰归零点」用 fixedRadius 覆盖初始半径（非 CAMERA_START 反解距离），
 *  保证按按钮回到的是**固定点**而不是「从当前位置拉一个轨道」。 */
const ORBIT_FIXED_RADIUS = FOCUS_RADIUS;


/** 小地图 / 面板 → 主场景的聚焦目标（null = 无聚焦请求）。 */
export type VirtualCityFocusTarget = FocusTarget;

interface Props {
  gameState: VirtualCityGameState | null;
  /** 小地图画视野框用（每帧覆写）。 */
  viewRef: React.MutableRefObject<VirtualCityCameraView>;
  /** 聚焦目标（写入后场景平滑移 target，到位自动清空）。 */
  focusRef: React.MutableRefObject<VirtualCityFocusTarget | null>;
  selectedDistrict: VirtualCityDistrictId | null;
  onSelectDistrict: (id: VirtualCityDistrictId) => void;
}

/**
 * 地面：80×80 plane + RepeatWrapping 城市底色贴图（16 · 阶段 R：urban_base
 * 优先，替代满城沥青；缺失 → asphalt_main → 纯色 #141a24）。
 * 旧 gridHelper 已删除（消除黑线）。v2.12 阶段 2：40×40 → 80×80（面积 ×4）。
 */
function Ground() {
  // 16 · 阶段 R：城市建成区底色（缺失回退沥青，降级链 §11）
  const urbanTex = useSharedTexture(groundTileUrl('urban_base'), {
    wrap: 'repeat',
    repeat: [GROUND_REPEAT, GROUND_REPEAT],
  });
  // 14-3D渲染深化：共享贴图缓存（缺失 → 纯色 #141a24 降级链不变）
  const asphaltTex = useSharedTexture(streetTileUrl('asphalt_main'), {
    wrap: 'repeat',
    repeat: [GROUND_REPEAT, GROUND_REPEAT], // 80 / 8 = 10
  });
  const tex = urbanTex ?? asphaltTex;
  // 批次 28 B2：城市地面信息交互（click 空白处也走详情卡，不穿到下层）。
  // 批次 32 v2：地面不参与「选中」（点空白 = 不选中任何物体，只关当前选中）。
  // hover 信息卡保留。
  const info = useObjectInfoProps('ground.city', { anchorY: 0.3, selectDisabled: true });

  return (
    <mesh {...info} rotation={[-Math.PI / 2, 0, 0]} position={[0, 0, 0]} receiveShadow>
      <planeGeometry args={[WORLD_GROUND_SIZE, WORLD_GROUND_SIZE]} />
      <meshStandardMaterial
        map={tex ?? undefined}
        color={tex ? '#ffffff' : '#141a24'}
        roughness={0.95}
        metalness={0.02}
      />
    </mesh>
  );
}

/**
 * 道路层：批次 31「混合式路网」（三轮：CBD 放射路已删）——邻接次干道 +
 * 方格骨干 + 高速联络线（ROAD_NETWORK 单一事实来源，模块级一次性计算），
 * 另挂 CBD 环路（RingRoad）、一环路（FirstRingRoad r=20）与高速收费站（HighwayGates）。
 * 主干道 vs 次干道按 kind 字段（批次 20 §3.2 派生阈值在 buildRoadNetwork 内应用）。
 */
const ROAD_NETWORK = buildRoadNetwork(VIRTUAL_CITY_DISTRICTS, MAIN_ROAD_MIN_LEN);

function RoadsLayer() {
  const { lamps, signals, bins } = useMemo(() => {
    // 批次 20 §3.3：全部道路的路灯点位汇总 → 全局 InstancedMesh（3 draw call）
    const lampList = ROAD_NETWORK.segments.flatMap((r) =>
      lampsForRoad(r.from, r.to, r.kind),
    );
    // 批次 24：红绿灯（双端 + 环岛对角，A/B 相位组）+ 批次 31 一环路口；
    // 路侧垃圾桶布点汇总，交 <TrafficSignals> / <RoadsideBins> 全局实例化渲染。
    const signalList = trafficSignalsForCity(
      ROAD_NETWORK.firstRingJunctionAngles,
      ROAD_NETWORK.arterialIntersections,
    );
    const binList = roadsideBinsForNetwork(
      ROAD_NETWORK.segments,
      busStopsForNetwork(ROAD_NETWORK.segments),
    );
    return { lamps: lampList, signals: signalList, bins: binList };
  }, []);

  return (
    <>
      {ROAD_NETWORK.segments.map((r) => (
        <Road
          key={r.key}
          from={r.from}
          to={r.to}
          kind={r.kind}
          yOffset={r.yOffset}
        />
      ))}
      {/* 批次 31：一环路（r=20 主路环 + 放射主路交点停止线，段合并 3 mesh） */}
      <FirstRingRoad junctionAngles={ROAD_NETWORK.firstRingJunctionAngles} />
      {/* 批次 31：全路网标线/人行道合并层（5 draw call，见 RoadMarkings） */}
      <RoadMarkings segments={ROAD_NETWORK.segments} />
      {/* 批次 31：高速联络线收费站（17 座龙门架 + 双亭，实例化 ≈5 draw call） */}
      <HighwayGates gates={ROAD_NETWORK.gates} />
      <StreetLightsInstanced lamps={lamps} />
      {/* 批次 24：全城红绿灯（≈6 draw call，16s 相位：绿 6/黄 2/红 8，A/B 组错半周期） */}
      <TrafficSignals signals={signals} />
      {/* 批次 24：全城路侧垃圾桶（GLB 实例化 2~6 draw call；缺失回退程序化桶密度减半） */}
      <RoadsideBins bins={bins} />
    </>
  );
}

/** 运河中心 z（与 CanalBridge 契约一致）。 */
export const CANAL_Z = 17;
/** 运河 x 半跨（水面横贯 x ∈ [-CANAL_HALF_X, CANAL_HALF_X]）。 */
export const CANAL_HALF_X = 48;

/**
 * 水系层（14-3D城市渲染深化 · 阶段 I + 15-3D城市全面真实感深化 · 阶段 P）：
 *   - 城市运河 + 物流港港池 + 两岸草皮收边（14 阶段 I）
 *   - 水面岸雾 WaterMist（15 阶段 P）：沿运河/港池两岸各加半透明雾 plane
 * 位置契约 01 文档 §3.1：运河 z=+17（滨河新区 riverside(14,10) 与教育/医疗城 z=22 之间），
 * 港池 (-30,-4) 物流港西侧。贴图缺失降级纯色水面（WaterPlane 内处理）。
 * 批次 20（文档 1 §3.4）：运河 x 半跨 32 → 48（东延入 software_park/airport_town 之间空隙；
 * 与 fin_sub/sports/bay/university 底板 z≥22 不交，CanalBridge 常量同源 import）。
 */
function WaterLayer() {
  const bankTex = useSharedTexture(streetTileUrl('sidewalk_side'), {
    wrap: 'repeat',
    repeat: [16, 1],
  });
  const canalLen = CANAL_HALF_X * 2;
  // 批次 28 B2：两岸草皮收边并入 water.canal 语义（运河岸线的一部分）。
  const bankInfo = useObjectInfoProps('water.canal', { anchorY: 0.3 });
  return (
    <>
      {/* 城市运河（96×3，横贯 x ∈ [-48, 48]；批次 20 东延） */}
      <WaterPlane x={0} z={CANAL_Z} w={canalLen} d={3} />
      {/* 物流港港池（6×8） */}
      <WaterPlane x={-30} z={-4} w={6} d={8} infoId="water.harbour" />
      {/* 两岸草皮收边（窄条，色 #3f7a3a 与中央公园草地呼应） */}
      {[-1, 1].map((side) => (
        <mesh
          {...bankInfo}
          key={`bank-${side}`}
          rotation={[-Math.PI / 2, 0, 0]}
          position={[0, 0.024, CANAL_Z + side * 1.85]}
          receiveShadow
        >
          <planeGeometry args={[canalLen, 0.7]} />
          <meshStandardMaterial
            map={bankTex ?? undefined}
            color={bankTex ? '#ffffff' : '#3f7a3a'}
            roughness={0.95}
          />
        </mesh>
      ))}
      {/* 阶段 P：运河两岸薄雾（覆盖水体外缘各 1.5 单位的过渡带） */}
      <WaterMist x={0} z={CANAL_Z + 2.35} w={canalLen} d={1.5} opacity={0.18} />
      <WaterMist x={0} z={CANAL_Z - 2.35} w={canalLen} d={1.5} opacity={0.18} />
      {/* 港池四周薄雾 */}
      <WaterMist x={-30 + 3.85} z={-4} w={1.5} d={8} opacity={0.15} />
      <WaterMist x={-30 - 3.85} z={-4} w={1.5} d={8} opacity={0.15} />
      <WaterMist x={-30} z={-4 + 4.85} w={6} d={1.5} opacity={0.15} />
      <WaterMist x={-30} z={-4 - 4.85} w={6} d={1.5} opacity={0.15} />
    </>
  );
}

/** 按城区聚合玩家，产出 token 布点参数。 */
function tokenLayout(gameState: VirtualCityGameState | null) {
  const players = gameState?.players ?? [];
  const byDistrict = new Map<string, number[]>(); // districtId → player indexes
  players.forEach((p, i) => {
    const list = byDistrict.get(p.district) ?? [];
    list.push(i);
    byDistrict.set(p.district, list);
  });
  const counts = new Map<string, number>();
  players.forEach((p) => {
    counts.set(p.district, (counts.get(p.district) ?? 0) + 1);
  });
  return { players, byDistrict, counts };
}

// 批次 28 A1：memo —— props 收敛为稳定引用（viewRef/focusRef 是 ref、
// selectedDistrict 原语、onSelectDistrict 为 useCallback），页面级 state
// （面板 Tab / 弹窗 / 4Hz 时钟）不再打进 R3F 场景树；仅 WS gameState 推送才重渲染。
export const VirtualCityCityMap = memo(function VirtualCityCityMap({
  gameState,
  viewRef,
  focusRef,
  selectedDistrict,
  onSelectDistrict,
}: Props) {
  /** FreeViewControls 在 orbit 模式下填充的聚焦点（TargetLike 形状）。 */
  const controlsRef = useRef<TargetLike | null>(null);
  const { players, byDistrict, counts } = useMemo(() => tokenLayout(gameState), [gameState]);
  const mySeat = gameState?.my_seat ?? -1;
  // 批次 23：座位居民语音气泡（useVirtualCitySpeech 写入；key = seat）。
  const speechBubbles = useVirtualCityStore((s) => s.speechBubbles);
  const marketById = useMemo(() => {
    const m = new Map<string, number>();
    // 2026-09-14 §财商流P0-bugfix: 半截可选链 `?.market.districts` 在
    // gameState 已到达但 market/districts 尚未填充(占位帧/竞态)时整页崩溃
    // (TypeError: null.forEach → ErrorBoundary)。双层防御。
    (gameState?.market?.districts ?? []).forEach((d) => m.set(d.id, d.price_index));
    return m;
  }, [gameState]);

  /**
   * 批次 34 §6.2：人流布点种子。协议里没有下发房间 seed（只在建房 payload 里），
   * 故用「房内稳定不变量」派生 —— 人口规模 + 各区人口分布，同房间恒同、
   * 跨房间几乎必不同；换班变化交给 `crowd.churn`。禁 Math.random（截图不可复现）。
   */
  const crowdRoomSeed = useMemo(() => {
    const c = gameState?.city;
    if (!c) return 0;
    let h = (c.resident_count | 0) >>> 0;
    for (const d of c.districts ?? []) {
      h = Math.imul(h ^ ((d.population | 0) + 1), 0x85ebca6b) >>> 0;
    }
    return (h ^ (h >>> 13)) >>> 0;
  }, [gameState]);

  // ── 批次 32：自由视角（俯瞰 / 全自由飞行 / 街景漫游 三态）。模式存于 engine3d
  //    store，Canvas 内的控制器与 Canvas 外的 <FreeViewHud /> 经同一 store 通信。────
  /** 相机碰撞体（32 区全部建筑 AABB）：随房价指数重装配，见 freeViewColliders.ts。 */
  const cameraColliders = useMemo(() => buildCityCameraColliders(marketById), [marketById]);
  /** 自由视角的相机朝向 / FOV 读回源（fly 才有意义），供小地图画真实视锥。 */
  const aimRefs = useRef<FreeViewAim>({ yaw: null, fov: null });
  /** 批次 32 v2：选中物体的聚焦点（世界坐标 + 半径），由 selectedTargetFor 检索得出。 */
  const selected = useObjectInfoStore((s) => s.selected);
  const selectedFocus = useMemo(() => {
    if (!selected) return null;
    // 不同类别的物体给不同的 fallback 半径（地面/水系大，街具/树小）
    const cat = selected.id.split('.')[0];
    const fallback = cat === 'ground' || cat === 'water' || cat === 'road' ? 6
      : cat === 'edge' || cat === 'sky' ? 12
      : cat === 'district' ? 8
      : 3;
    return selectedTargetFor(cameraColliders, selected.pos, fallback);
  }, [selected, cameraColliders]);
  const selectedFocusRef = useRef(selectedFocus);
  selectedFocusRef.current = selectedFocus;

  return (
    <div className="virtualCity-map-host">
      {/* 批次 30 C：显式 near/far（three 缺省 1m/20km —— 1.7 m 眼高漫游下 1 m 内被裁、
          far/near=20000 让路面 1~4 cm 分层偏置远距吃紧）。 */}
      <EngineCanvas
        camera={{ position: CAMERA_START, fov: 45, near: 0.05, far: 800 }}
        debugGlobalName="__cityRenderInfo"
      >
        {/* 批次 27 §4.3：整段静态正午光 rig（Sky/fog/ambient/hemisphere/填充光/
            主方向光/EnvBinder）替换为城市环境层 —— DayNightCycle 昼夜循环 +
            WeatherFX 雨/雪粒子 + EnvBinder 正午 PMREM（正午基准常量见上方注释块） */}
        <CityEnvironmentLayer
          fogNear={FOG_NEAR}
          fogFar={FOG_FAR}
          sunDistance={SUN_DISTANCE}
          skySunPosition={SKY_SUN_POSITION}
          shadowCameraHalf={SHADOW_CAMERA_HALF}
          shadowCameraFar={WORLD_SIZE * 2}
        />
        <Ground />
        {/* 批次 28 二轮：userData.bucket 供 ?debug=1 的 __cityBreakdown() 分桶归因
            （identity group，零 transform，零视觉影响）。楼栋/车辆/行人桶在各自
            组件根 group 上自标（BuildingMesh / Vehicle / PedestrianV3）。 */}
        {/* 14-3D渲染深化：城市水系（运河 + 港池 + 岸草皮） */}
        <group userData={{ bucket: 'water' }}>
          <WaterLayer />
        </group>
        {/* 15-3D渲染深化：氛围层（公园落叶 Sparkles；批次 26 起远景剪影移除） */}
        <AtmosphereLayer />
        {/* 批次 26：四缘环境带（北雪山 / 西沙漠 / 东森林 / 南海洋+南港，方案 §2.1） */}
        <group userData={{ bucket: 'edge' }}>
          <CityEdgeLayer />
        </group>
        {/* 16 · 阶段 U：天空云层（6 团 Billboard 云，慢速漂移；y=60-80 在相机
            轨道最高点之上，任何缩放都不会遮盖城市——视觉验收三轮回归结论） */}
        <CloudLayer />
        {/* 16 · 阶段 T：功能地标（喷泉 / 园路花坛 / 塔吊工地 / 停车场） */}
        <group userData={{ bucket: 'landmarks' }}>
          <LandmarksLayer />
        </group>
        {/* 18 · 阶段 AA：市政补全（码头 / 操场 / 轻轨 / 停机坪 / 加油站 / 生命线 / 外围腹地 / 路口信号灯） */}
        <group userData={{ bucket: 'civic' }}>
          <CivicLayer />
        </group>
        {VIRTUAL_CITY_DISTRICTS.map((d) => (
          <DistrictBlock
            key={d.id}
            def={d}
            priceIndex={marketById.get(d.id) ?? 1}
            playerCount={counts.get(d.id) ?? 0}
            selected={selectedDistrict === d.id}
            onSelect={onSelectDistrict}
          />
        ))}
        {/* P1-A：单 plane → 分层 <Road /> 道路（含路灯阵列）；16 · 阶段 S：CBD 环形路 + 运河跨河桥 */}
        <group userData={{ bucket: 'roads' }}>
          <RoadsLayer />
          <RingRoad junctionAngles={ROAD_NETWORK.cbdRingJunctionAngles} />
          <CanalBridges />
        </group>
        {/* P1-C：街道道具层（树 / 车辆 / 行人 / 标识 / 屋顶杂物）；
            v2.12 阶段 2：districts 由父层注入（props 化，适配 16 城区） */}
        <group userData={{ bucket: 'street-props' }}>
          <StreetPropsLayer
            districts={VIRTUAL_CITY_DISTRICTS}
            segments={ROAD_NETWORK.segments}
            crowd={gameState?.city?.crowd}
            residentCount={gameState?.city?.resident_count}
            roomSeed={crowdRoomSeed}
          />
        </group>
        {/* 批次 28 二轮：?debug=1 场景归因探针（__cityScene/__cityRenderer/__cityBreakdown） */}
        <SceneDebugProbe />
        {players.map((p, i) => {
          const inDistrict = byDistrict.get(p.district) ?? [];
          const index = inDistrict.indexOf(i);
          const c = districtCenter(p.district);
          return (
            <AgentToken
              key={`${p.seat}-${p.account}`}
              player={p}
              cx={c.x}
              cz={c.z}
              index={index < 0 ? 0 : index}
              total={inDistrict.length}
              isMe={p.seat === mySeat}
              speech={speechBubbles[p.seat] ?? null}
            />
          );
        })}
        {/* 批次 23：市民之声气泡层（市政厅上空，eventFeed city_voice 驱动，不依赖座位） */}
        <CityVoiceBubbleLayer />
        {/* 批次 32：自由视角控制器（替代批次 22 的 OrbitControls/WalkControls 双挂载）。
            三态共用一套积分器：俯瞰 orbit / 全自由六自由度 fly / 街景漫游 walk；
            切换走 600 ms 位姿过渡（球面·线性插值 + 四元数 slerp），无画面跳变。
            碰撞（建筑 AABB）+ 边界（±88 / z→155）+ 地面钳制在此一并生效。 */}
        <FreeViewControls
          targetRef={controlsRef}
          focusRef={focusRef}
          aimRefs={aimRefs}
          initialTarget={FOCUS_TARGET}
          initialPhi={FOCUS_PHI}
          initialRadius={ORBIT_FIXED_RADIUS}
          orbitFixedTheta={FOCUS_THETA}
          speed={FLY_SPEED}
          orbitPanSpeed={ORBIT_PAN_SPEED}
          minDistance={ORBIT_MIN_DISTANCE * 0.6}
          maxDistance={ORBIT_MAX_DISTANCE * 1.5}
          maxPolar={ORBIT_MAX_POLAR_ANGLE}
          fov={45}
          colliders={cameraColliders}
          collisionRadius={FREE_VIEW_COLLISION_RADIUS}
          bounds={FREE_VIEW_BOUNDS}
          groundY={FREE_VIEW_GROUND_Y}
          selectedFocusRef={selectedFocusRef}
        />
        {/* 批次 32 v2：选中特效（地面环 + 竖环 + 呼吸脉动）。通用 ——
            所有 objectInfo 接线的物体选中后都出现，半径由 selectedTargetFor 检索。 */}
        <SelectionMarker focus={selectedFocus} groundY={FREE_VIEW_GROUND_Y} />
        {/* 批次 22 → 32：视野快照上报。orbit 走 targetRef 分支（聚焦点），
            fly/walk 时 targetRef 为 null → 回落上报相机自身，并附 yaw/fov
            供小地图画真实视锥扇形。 */}
        <CameraViewReporter
          targetRef={controlsRef}
          fallbackToCamera
          aimRefs={aimRefs}
          viewRef={viewRef}
        />
        {/* 批次 28 B1：全场景唯一物件信息卡（hover 悬浮卡 + click 详情卡；
            zIndexRange [30,0] 不压 minimap/error/modal，见方案 §5.1） */}
        <ObjectInfoOverlay />
      </EngineCanvas>
      {/* 批次 32：自由视角 Web 操作界面（模式分段控件 / 速度档 / 碰撞指示灯 /
          键位帮助）。z-index 32 —— 高于物件信息 Html(≤30)，低于小地图(40)。
          文案在此注入中文包，引擎层的 FreeViewHud 不写死语种。 */}
      <FreeViewHud
        labels={{
          title: '相机视角',
          modes: ['俯瞰归零点', '自由飞行'],
          help: '键位说明',
          speed: '速度',
          tiers: ['1 倍', '2 倍', '4 倍', '8 倍', '16 倍'],
          clear: '视野畅通',
          colliding: '被场景阻挡',
          shortcuts: [
            ['V / F', '俯瞰 ⇄ 自由 往返'],
            ['1 / 2', '直达 俯瞰归零点 / 自由飞行'],
            ['WASD', '自由移动 / 选中时环绕物体'],
            ['空格 / E / Q', '上升 / 上升 / 下降'],
            ['Shift', '切换移动速度 ×1→×2→×4→×8→×16（回绕）'],
            ['右键拖拽', '自由飞行下转向（俯瞰为平移）'],
            ['中键拖拽', '自由飞行下视口平移'],
            ['滚轮', '俯瞰缩放视距 / 自由视角变焦'],
            ['[ / ]', '速度档 降 / 升（同 Shift，可回绕）'],
            ['R', '复位视角'],
            ['H', '展开 / 收起本说明'],
          ],
        }}
      />
      {/* 批次 28 A5：?debug=1 轻量性能浮层（DOM，不进 R3F 树；非 debug 零渲染） */}
      <PerfHud />
      {/* 小地图由 VirtualCityGamePage 以绝对定位叠加（左上角） */}
    </div>
  );
});
