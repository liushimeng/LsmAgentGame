/**
 * Vehicle — 3D 城市场景车辆（P1-C）：
 *
 * 简化几何（缺失贴图）：小盒子 + 主色（按 variant）。
 * 加载 props/vehicle/<variant>_vehicle.png 后：Billboard 朝相机 sprite。
 *
 * 沿 from→to 路径循环移动：useFrame 内 lerp t += speed * dt，过 t ≥ 1 → 重置。
 * y = VEHICLE_GROUND_Y（路面 +5cm，轮底贴路面）；rotation 始终朝向运动方向
 * （与 Billboard 不冲突，sprite 始终朝相机，几何盒子朝向运动方向）。
 * 批次 29：车身尺寸与 GLB 目标尺寸同取 cityScale.REAL_DIMS_M（唯一事实来源）。
 *
 * ⚠️ 朝向约定（批次 29 修正 —— 修正前 yaw 取 `atan2(dx,dz)`，令**全城车辆横着开**）：
 *   **车辆模型长轴 = 局部 +X**（GLB 与 fallback 同口径）；yaw 取 `Math.atan2(-dz, dx)`
 *   使 +X 对齐行进方向。
 *   ⚠️ 与 `PedestrianV3.tsx` 的约定不同（行人模型 +Z 向前，故用 `atan2(dx, dz)`）——
 *   两者各自自洽，勿互相"对齐"。改朝向公式前务必先确认模型的向前轴是 +X 还是 +Z。
 *
 * v2.13 阶段 D（13-3D城市渲染优化 02-架构 §3.3）：
 *   - 车身下 4 个车轮（黑色扁圆柱 r=u(0.35)，轴沿车宽 z 向），
 *     贴图/几何两分支都渲染（贴图 sprite 是侧视 Billboard，车轮在地面层补体积感）。
 *   - 车头 2 个暖白前灯 + 车尾 2 个红色尾灯（emissive 小方块，沿车长 x 轴 ±端）。
 *
 * 18 · 阶段 Z（18-3D城市PBR材质与真实城市冲刺 02-架构 §4.2）：
 *   - 内部件补全：前挡风（半透明深蓝，后倾 25°）+ 侧窗 ×2 + 前大灯 ×2 + 尾灯 ×2
 *     + 轮毂 ×4（贴轮外侧）+ 后视镜 ×2 + 雨刮 1 根——「火柴盒」→ 可读车型。
 *   - 新可选 prop `palette`：消防车 / 巡逻车复用同组件换色（body/roof/accent 三色）；
 *     提供 palette 时强制走几何体分支（贴图 sprite 是固定车型彩绘，无法换色）。
 *
 * 批次 28 二轮（DC 攻坚）：18 个附件 mesh 按材质类合并为 2 mesh（几何逐件全等，
 * engine3d/geoMerge）：车轮/轮毂/雨刮/后视镜 → 顶点色 1 mesh；玻璃/前灯/尾灯 →
 * 3 group 1 mesh。GLB 等材质 group 归并在 modelCache 加载期完成（sedan 18→7 组）。
 * 每车 mesh：GLB/sprite/几何分支 1 + 附件 2 = 3。
 */

import { memo, useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame, useThree } from '@react-three/fiber';
import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/virtualCity';
import { modelUrl } from '@/assets/models';
import { u, worldDims, sizeTargetFor, VEHICLE_GROUND_Y } from '../cityScale';
import { useSharedTexture } from '@/engine3d';
import {
  type GroupedMergePart,
  type MergePart,
  boxPart,
  mergeGrouped,
  mergeParts,
  useSharedGLTF,
  blenderModelsEnabled,
  detectQualityTier,
} from '@/engine3d';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

type VehicleVariant = 'sedan' | 'truck' | 'bus' | 'taxi';

interface Props {
  /** 起始坐标。 */
  from: [number, number];
  /** 终点坐标（沿 from→to 直线路径循环）。 */
  to: [number, number];
  variant?: VehicleVariant;
  /** 循环速度（t / 秒）。 */
  speed?: number;
  /** 起始相位偏移 [0, 1)，避免多辆车完全同步。 */
  phase?: number;
  /**
   * 16 · 阶段 S：车道偏移（世界单位）。>0 = 行进方向右侧通行；
   * 0（默认）= 沿路中线（现行为）。双向车道两方向都传**同样的正值**——
   * 行进向量反转后世界侧自动翻转，两车自然各占一侧（传负会落到同侧对撞）。
   * bus/truck 建议 0.36，sedan/taxi 0.32。
   */
  laneOffset?: number;
  /**
   * 18 · 阶段 Z：可选涂装（消防车/巡逻车复用同组件换色）。
   * body = 车身主色；roof = 车顶（+Y 面）；accent = 前后端装饰色（±X 面，缺省回退 body）。
   * 提供后强制走几何体分支（贴图 sprite 是固定车型彩绘，无法换色）。
   */
  palette?: { body: string; roof: string; accent?: string };
}

const VEHICLE_COLORS: Record<VehicleVariant, string> = {
  sedan: '#3b6bb0',
  truck: '#8a6a3d',
  bus:  '#d8c44a',
  taxi: '#e8b930',
};

/**
 * 车身尺寸 长×高×宽（世界单位）—— **取自 cityScale.REAL_DIMS_M**（唯一事实来源，
 * 批次 29 起：与 `vehicles/<variant>.glb` 共用同一行表值，两条渲染路径包围盒一致）：
 * 轿车 4.6×1.45×1.82 m / 出租车 4.7×1.50×1.85 m / 公交 12.0×3.20×2.55 m
 * / 卡车 8.5×3.40×2.50 m（此前组件内硬编码的 4.5×1.5×1.8 等值已删除）。
 */
function vehicleDims(v: VehicleVariant): { l: number; h: number; w: number } {
  const d = worldDims(v);
  return { l: d.x, h: d.y, w: d.z };
}

const VEHICLE_DIMS: Record<VehicleVariant, { l: number; h: number; w: number }> = {
  sedan: vehicleDims('sedan'),
  taxi:  vehicleDims('taxi'),
  truck: vehicleDims('truck'),
  bus:   vehicleDims('bus'),
};

/** GLB 尺寸/落地校验目标（dev 态；见 engine3d/glbSizeGuard）——与 fallback 同表值。 */
const VEHICLE_SIZE_TARGETS: Record<VehicleVariant, ReturnType<typeof sizeTargetFor>> = {
  sedan: sizeTargetFor('sedan', { label: 'vehicles/sedan' }),
  taxi:  sizeTargetFor('taxi',  { label: 'vehicles/taxi' }),
  truck: sizeTargetFor('truck', { label: 'vehicles/truck' }),
  bus:   sizeTargetFor('bus',   { label: 'vehicles/bus' }),
};

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// ── v2.13 阶段 D：车轮常量（米制经 cityScale.u() 换算）──────────
/** 车轮半径 u(0.35) ≈ 0.35m，胎宽 u(0.2)。 */
const WHEEL_R = u(0.35);
const WHEEL_W = u(0.2);
const WHEEL_COLOR = '#17191d';
/** 车轮落位（相对车身中心）：车长 ±0.32×、车宽 ±0.5×。 */
function wheelPositions(l: number, w: number): Array<[number, number, number]> {
  return [
    [+l * 0.32, WHEEL_R, +w / 2],
    [+l * 0.32, WHEEL_R, -w / 2],
    [-l * 0.32, WHEEL_R, +w / 2],
    [-l * 0.32, WHEEL_R, -w / 2],
  ];
}

// ── 18 · 阶段 Z：车窗 / 车灯 / 轮毂 / 后视镜 / 雨刮（米制经 u()）────────
/** 玻璃（前挡风 + 侧窗共用）：半透明深蓝，高光镜面感。 */
const GLASS_COLOR = '#2a4a6e';
/** 车灯（box [宽 u(0.1), 高 u(0.06), 厚 u(0.04)]，沿车宽横向铺）。 */
const LAMP_W = u(0.1);
const LAMP_H = u(0.06);
const LAMP_D = u(0.04);
/** 轮毂圆片（贴轮外侧）：r=u(0.07) h=u(0.03)。 */
const HUB_R = u(0.07);
const HUB_H = u(0.03);
const HUB_COLOR = '#c5c8ce';
/** 后视镜小盒 u(0.05)³ 级。 */
const MIRROR_X = u(0.05);
const MIRROR_Y = u(0.04);
const MIRROR_Z = u(0.05);
const MIRROR_COLOR = '#3a414c';
/** 雨刮细条（前挡风下沿，横铺 u(0.3)）。 */
const WIPER_X = u(0.015);
const WIPER_Y = u(0.015);
const WIPER_Z = u(0.3);
const WIPER_COLOR = '#1f2733';
/** 前挡风后倾角（rad，25°）。 */
const WINDSHIELD_TILT = (25 * Math.PI) / 180;

/** 轮毂落位：与车轮同 x/y，z 贴轮外侧（轮宽一半 + 毂片自身半厚）。 */
function hubPositions(l: number, w: number): Array<[number, number, number]> {
  const z = w / 2 + WHEEL_W / 2 + HUB_H / 2;
  return [
    [+l * 0.32, WHEEL_R, +z],
    [+l * 0.32, WHEEL_R, -z],
    [-l * 0.32, WHEEL_R, +z],
    [-l * 0.32, WHEEL_R, -z],
  ];
}

// 批次 28 A1：memo —— from/to（layout useMemo 的元组）/ variant / speed / phase /
// laneOffset / palette 全为稳定引用（palette 由调用方常量表传入）。
export const Vehicle = memo(function Vehicle({
  from,
  to,
  variant = 'sedan',
  speed = 0.06,
  phase = 0,
  laneOffset = 0,
  palette,
}: Props) {
  const url = propUrl('vehicle', variant);
  // 14-3D渲染深化：共享贴图缓存（多车共用同 variant 贴图只上传一次）
  const tex = useSharedTexture(url);
  // 19-Blender3D模型集成：Blender 真实模型（优先级最高，绕过 sprite 和 palette）
  //   - GLB 缺失或加载失败 → scene=null → 走原有 useSprite / 几何体 / palette 分支
  //   - GLB 加载成功 → 仅渲染 .glb（视觉最丰富）
  //   - palette 模式（消防/巡逻车换色）→ 强制不走 GLB（GLB 颜色固定）
  const modelUrlStr = modelUrl('vehicles', variant);
  const blenderOn = blenderModelsEnabled();
  const useGLB = !palette && blenderOn && !!modelUrlStr;
  void useGLB; // 标记保留：未来 v19.5 通过此 flag 控制 GLB vs sprite / palette fallback
  // 批次 29：注册本车型目标尺寸（dev 态 glbSizeGuard 量测 GLB 包围盒/落地并比对表值）
  const { scene: glbScene } = useSharedGLTF(modelUrlStr, VEHICLE_SIZE_TARGETS[variant]);
  const glbCloned = useMemo(() => (glbScene ? glbScene.clone(true) : null), [glbScene]);
  // 批次 28 A3：车辆 GLB 投影仅 high 档保留（low 档裁掉车流的阴影 pass 几何）。
  // primitive 上的 castShadow 只落在根 Group，逐 mesh 须 traverse 设置。
  const gl = useThree((s) => s.gl);
  const tier = useMemo(() => detectQualityTier(gl), [gl]);
  useEffect(() => {
    if (!glbCloned) return;
    const shadow = tier === 'high';
    glbCloned.traverse((o) => {
      if ((o as THREE.Mesh).isMesh) o.castShadow = shadow;
    });
  }, [glbCloned, tier]);
  const groupRef = useRef<THREE.Group>(null);
  const dims = VEHICLE_DIMS[variant];
  // 18 · 阶段 Z：涂装三色（body 车身 / roof 车顶 / accent 前后端，缺省回退 body）
  const bodyColor = palette?.body ?? VEHICLE_COLORS[variant];
  const roofColor = palette?.roof ?? bodyColor;
  const accentColor = palette?.accent ?? bodyColor;
  // 自定义涂装必须走几何体（贴图 sprite 是固定车型彩绘，换不了色）
  const useSprite = !!tex && !palette && !glbCloned;

  // 路径向量
  //
  // ⚠️ 朝向约定（批次 29 修正，改前为 Math.atan2(dx, dz) —— 车辆"横着开"的缺陷根因）：
  //   **车辆模型的长轴 = 局部 +X**（GLB 与 fallback 同口径：`sedan.glb` 几何 [0.460, 0.145, 0.182]
  //   的 X 为车长；fallback 也是 `boxGeometry args={[dims.l, dims.h, dims.w]}`，l 在 X；
  //   车灯/挡风/侧窗/轮位等细节件同样以 X 为车长轴）。
  //   three.js 绕 Y 旋转 θ：局部 +X = (cosθ, 0, −sinθ)。欲令 +X 对齐行进方向 (dx, dz)/L
  //   ⇒ cosθ = dx/L 且 sinθ = −dz/L ⇒ **θ = Math.atan2(−dz, dx)**。
  //   取 atan2(dx, dz) 会把局部 +Z 对齐行进方向，而 +Z 是车宽轴 ⇒ 车长轴与行进方向垂直 90°。
  //
  // ⚠️ 与 `PedestrianV3.tsx` 的约定**不同**（行人模型 +Z 向前，故行人用 `atan2(dx, dz)`）——
  //   两者各自自洽，勿互相"对齐"。
  const { dx, dz, angle } = useMemo(() => {
    const dx = to[0] - from[0];
    const dz = to[1] - from[1];
    return { dx, dz, angle: Math.atan2(-dz, dx) };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from[0], from[1], to[0], to[1]]);

  // 16 · 阶段 S：右行偏移向量（行进方向右侧 = 左法线取反）
  const lane = useMemo(() => {
    const len = Math.sqrt(dx * dx + dz * dz) || 1;
    // 左法线 = (-dz, dx) / len；右行取其负
    return { ox: (dz / len) * laneOffset, oz: (-dx / len) * laneOffset };
  }, [dx, dz, laneOffset]);

  // 起始位置
  const tRef = useRef(phase);

  useFrame((_state, delta) => {
    const g = groupRef.current;
    if (!g) return;
    if (REDUCED_MOTION) return;
    tRef.current += delta * speed;
    if (tRef.current >= 1) tRef.current -= 1;
    const t = tRef.current;
    g.position.x = from[0] + dx * t + lane.ox;
    g.position.z = from[1] + dz * t + lane.oz;
    g.position.y = VEHICLE_GROUND_Y;
  });

  // 批次 28 B2：车型级简介（vehicle.<variant>，GLB 与 fallback 共用文案）。
  const info = useObjectInfoProps(`vehicle.${variant}`, { anchorY: dims.h + 0.4 });

  // ── 批次 28 二轮：附件按材质类合并（18 件独立 mesh → 2 mesh，几何逐件全等）──
  // solids：车轮/轮毂/雨刮/后视镜 —— 单材质 + 顶点色（颜色逐件保留；粗糙度统一
  //   0.65 / 金属 0.3，原 0.35–0.9 区间的视觉取舍见批次 28 报告）。
  // glassLamps：前挡风+侧窗（玻璃）/ 前大灯 / 尾灯 3 group —— 材质逐字段与原一致。
  const detailGeos = useMemo(() => {
    const qX = new THREE.Quaternion().setFromEuler(new THREE.Euler(Math.PI / 2, 0, 0));
    const one = new THREE.Vector3(1, 1, 1);
    const solids: MergePart[] = [];
    for (const [x, y, z] of wheelPositions(dims.l, dims.w)) {
      solids.push({
        geo: new THREE.CylinderGeometry(WHEEL_R, WHEEL_R, WHEEL_W, 12),
        matrix: new THREE.Matrix4().compose(new THREE.Vector3(x, y, z), qX, one),
        color: WHEEL_COLOR,
      });
    }
    for (const [x, y, z] of hubPositions(dims.l, dims.w)) {
      solids.push({
        geo: new THREE.CylinderGeometry(HUB_R, HUB_R, HUB_H, 10),
        matrix: new THREE.Matrix4().compose(new THREE.Vector3(x, y, z), qX, one),
        color: HUB_COLOR,
      });
    }
    // 雨刮 1 根（前挡风下沿横铺）+ 后视镜 ×2（前舱两侧）
    solids.push(boxPart(WIPER_X, WIPER_Y, WIPER_Z, dims.l * 0.22 + u(0.02), dims.h * 0.75 - u(0.13), 0, WIPER_COLOR));
    for (const side of [+1, -1]) {
      solids.push(boxPart(MIRROR_X, MIRROR_Y, MIRROR_Z, dims.l * 0.2, dims.h * 0.88, side * (dims.w / 2 + u(0.03)), MIRROR_COLOR));
    }
    const glassLamps: GroupedMergePart[] = [
      // mat0 玻璃：前挡风（后倾 25°）+ 侧窗 ×2
      { geo: new THREE.BoxGeometry(u(0.04), u(0.28), dims.w * 0.85), x: dims.l * 0.22, y: dims.h * 0.75, z: 0, rotZ: WINDSHIELD_TILT, mat: 0 },
      { geo: new THREE.BoxGeometry(dims.l * 0.4, u(0.22), u(0.04)), x: -dims.l * 0.05, y: dims.h * 0.75, z: +(dims.w / 2 + u(0.008)), mat: 0 },
      { geo: new THREE.BoxGeometry(dims.l * 0.4, u(0.22), u(0.04)), x: -dims.l * 0.05, y: dims.h * 0.75, z: -(dims.w / 2 + u(0.008)), mat: 0 },
      // mat1 前大灯 ×2（+x 端暖白 emissive 0.8）
      { geo: new THREE.BoxGeometry(LAMP_D, LAMP_H, LAMP_W), x: dims.l / 2 + u(0.01), y: dims.h * 0.5, z: +dims.w * 0.3, mat: 1 },
      { geo: new THREE.BoxGeometry(LAMP_D, LAMP_H, LAMP_W), x: dims.l / 2 + u(0.01), y: dims.h * 0.5, z: -dims.w * 0.3, mat: 1 },
      // mat2 尾灯 ×2（-x 端红 emissive 0.5）
      { geo: new THREE.BoxGeometry(LAMP_D, LAMP_H, LAMP_W), x: -dims.l / 2 - u(0.01), y: dims.h * 0.5, z: +dims.w * 0.3, mat: 2 },
      { geo: new THREE.BoxGeometry(LAMP_D, LAMP_H, LAMP_W), x: -dims.l / 2 - u(0.01), y: dims.h * 0.5, z: -dims.w * 0.3, mat: 2 },
    ];
    return { solids: mergeParts(solids), glassLamps: mergeGrouped(glassLamps) };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dims]);
  useEffect(() => () => {
    detailGeos.solids.dispose();
    detailGeos.glassLamps.dispose();
  }, [detailGeos]);

  const glassLampMaterials = useMemo(() => [
    new THREE.MeshStandardMaterial({
      color: GLASS_COLOR, transparent: true, opacity: 0.55, roughness: 0.1, metalness: 0.25, envMapIntensity: 0.9,
    }),
    new THREE.MeshStandardMaterial({ color: '#fff6d8', emissive: '#fff6d8', emissiveIntensity: 0.8, roughness: 0.3 }),
    new THREE.MeshStandardMaterial({ color: '#ff3b30', emissive: '#ff3b30', emissiveIntensity: 0.5, roughness: 0.3 }),
  ], []);
  useEffect(() => () => glassLampMaterials.forEach((m) => m.dispose()), [glassLampMaterials]);

  return (
    <group
      {...info}
      ref={groupRef}
      userData={{ bucket: 'vehicles' }}
      position={[from[0] + lane.ox, VEHICLE_GROUND_Y, from[1] + lane.oz]}
      rotation={[0, angle, 0]}
    >
      {/* 19-Blender3D模型集成：.glb 优先级最高，绕过 sprite 和 palette（palette 模式 useGLB=false）；
          批次 28 A3：castShadow 由上方 traverse 按质量档逐 mesh 设置（high 才投影）。
          批次 28 二轮：GLB 等材质 group 已在 modelCache 加载期归并（sedan 18→7 组）。
          批次 29 落地契约：GLB 原点在**轮底**（minY=0，与 REAL_DIMS_M 的 minY 同行），
          故 group 置于 VEHICLE_GROUND_Y（路面 +5cm）即轮子贴路面，**不得再加半高补偿**。 */}
      {glbCloned ? (
        <primitive object={glbCloned} />
      ) : useSprite ? (
        // 贴图 sprite：平面按车身尺寸，上下各留 3cm 余量（sprite 底边略入路面，既有行为）
        <Billboard position={[0, dims.h / 2 + 0.03, 0]}>
          <mesh>
            {/* 贴图平面与车身尺寸同步（含车底轮子余量） */}
            <planeGeometry args={[dims.l, dims.h + 0.06]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      ) : (
        // 几何车身（缺贴图 / 自定义涂装）：盒子底面贴地（y=0 起，轮子在 group 原点之上）
        <mesh castShadow={false} position={[0, dims.h / 2, 0]}>
          <boxGeometry args={[dims.l, dims.h, dims.w]} />
          {palette ? (
            // 六面材质：[+X 前, -X 后, +Y 顶, -Y 底, +Z, -Z]（同 building_shapes 口径）
            // roof → 车顶面；accent → 前后端；其余 body
            <>
              <meshStandardMaterial attach="material-0" color={accentColor} roughness={0.6} metalness={0.3} />
              <meshStandardMaterial attach="material-1" color={accentColor} roughness={0.6} metalness={0.3} />
              <meshStandardMaterial attach="material-2" color={roofColor} roughness={0.6} metalness={0.3} />
              <meshStandardMaterial attach="material-3" color={bodyColor} roughness={0.6} metalness={0.3} />
              <meshStandardMaterial attach="material-4" color={bodyColor} roughness={0.6} metalness={0.3} />
              <meshStandardMaterial attach="material-5" color={bodyColor} roughness={0.6} metalness={0.3} />
            </>
          ) : (
            <meshStandardMaterial color={bodyColor} roughness={0.6} metalness={0.3} />
          )}
        </mesh>
      )}
      {/* 批次 28 二轮：车轮/轮毂/雨刮/后视镜 —— 顶点色合并 1 mesh */}
      <mesh geometry={detailGeos.solids}>
        <meshStandardMaterial vertexColors roughness={0.65} metalness={0.3} />
      </mesh>
      {/* 批次 28 二轮：玻璃 + 前大灯 + 尾灯 —— 3 材质组合并 1 mesh */}
      <mesh geometry={detailGeos.glassLamps} material={glassLampMaterials} />
    </group>
  );
});