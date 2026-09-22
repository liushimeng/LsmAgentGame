/**
 * Vehicle — 2.5D 街道车辆（P1-C）：
 *
 * 简化几何（缺失贴图）：小盒子 + 主色（按 variant）。
 * 加载 props/vehicle/<variant>_vehicle.png 后：Billboard 朝相机 sprite。
 *
 * 沿 from→to 路径循环移动：useFrame 内 lerp t += speed * dt，过 t ≥ 1 → 重置。
 * y=0.02（贴路面之上）；rotation 始终朝向运动方向（与 Billboard 不冲突，
 * sprite 始终朝相机，几何盒子朝向运动方向）。
 *
 * v2.13 阶段 D（13-3D城市渲染优化 02-架构 §3.3）：
 *   - 车身下 4 个车轮（黑色扁圆柱 r=u(0.35)，轴沿车宽 z 向），
 *     贴图/几何两分支都渲染（贴图 sprite 是侧视 Billboard，车轮在地面层补体积感）。
 *   - 车头 2 个暖白前灯 + 车尾 2 个红色尾灯（emissive 小方块，沿车长 x 轴 ±端）。
 */

import { useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/wealth';
import { u } from '../cityScale';
import { useSharedTexture } from '../textureCache';

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
}

const VEHICLE_COLORS: Record<VehicleVariant, string> = {
  sedan: '#3b6bb0',
  truck: '#8a6a3d',
  bus:  '#d8c44a',
  taxi: '#e8b930',
};

/**
 * 车身尺寸 长×高×宽（世界单位，1 单位 = 10m；2026-09-21 高度系统）：
 * 轿车 4.5×1.5×1.8 m；bus/truck 略高（×1.5 高）并加长。
 */
const VEHICLE_DIMS: Record<VehicleVariant, { l: number; h: number; w: number }> = {
  sedan: { l: 0.45, h: 0.15, w: 0.18 },
  taxi:  { l: 0.45, h: 0.15, w: 0.18 },
  truck: { l: 0.60, h: 0.23, w: 0.20 },
  bus:   { l: 0.75, h: 0.23, w: 0.20 },
};

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// ── v2.13 阶段 D：车轮 / 车灯常量（米制经 cityScale.u() 换算）──────────
/** 车轮半径 u(0.35) ≈ 0.35m，胎宽 u(0.2)。 */
const WHEEL_R = u(0.35);
const WHEEL_W = u(0.2);
const WHEEL_COLOR = '#17191d';
/** 车灯边长 u(0.15) ≈ 0.15m 方块。 */
const LIGHT_SIZE = u(0.15);
/** 车轮落位（相对车身中心）：车长 ±0.32×、车宽 ±0.5×。 */
function wheelPositions(l: number, w: number): Array<[number, number, number]> {
  return [
    [+l * 0.32, WHEEL_R, +w / 2],
    [+l * 0.32, WHEEL_R, -w / 2],
    [-l * 0.32, WHEEL_R, +w / 2],
    [-l * 0.32, WHEEL_R, -w / 2],
  ];
}

export function Vehicle({
  from,
  to,
  variant = 'sedan',
  speed = 0.06,
  phase = 0,
  laneOffset = 0,
}: Props) {
  const url = propUrl('vehicle', variant);
  // 14-3D渲染深化：共享贴图缓存（多车共用同 variant 贴图只上传一次）
  const tex = useSharedTexture(url);
  const groupRef = useRef<THREE.Group>(null);
  const dims = VEHICLE_DIMS[variant];

  // 路径向量
  const { dx, dz, angle } = useMemo(() => {
    const dx = to[0] - from[0];
    const dz = to[1] - from[1];
    return { dx, dz, angle: Math.atan2(dx, dz) };
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
    g.position.y = 0.02;
  });

  return (
    <group ref={groupRef} position={[from[0] + lane.ox, 0.02, from[1] + lane.oz]} rotation={[0, angle, 0]}>
      {tex ? (
        <Billboard position={[0, dims.h / 2 + 0.03, 0]}>
          <mesh>
            {/* 贴图平面与车身尺寸同步（含车底轮子余量） */}
            <planeGeometry args={[dims.l, dims.h + 0.06]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      ) : (
        // 缺失贴图：低矮盒子 + 主色（盒子底部贴地）
        <mesh castShadow={false} position={[0, dims.h / 2, 0]}>
          <boxGeometry args={[dims.l, dims.h, dims.w]} />
          <meshStandardMaterial color={VEHICLE_COLORS[variant]} roughness={0.6} metalness={0.3} />
        </mesh>
      )}
      {/* v2.13 阶段 D：4 车轮（轴沿车宽 z 向；贴图/几何两分支共用，地面层补体积感） */}
      {wheelPositions(dims.l, dims.w).map((pos, i) => (
        <mesh key={`wheel-${i}`} position={pos} rotation={[Math.PI / 2, 0, 0]}>
          <cylinderGeometry args={[WHEEL_R, WHEEL_R, WHEEL_W, 12]} />
          <meshStandardMaterial color={WHEEL_COLOR} roughness={0.9} metalness={0.1} />
        </mesh>
      ))}
      {/* v2.13 阶段 D：前灯（车长 +x 端，暖白）+ 尾灯（-x 端，红） */}
      {[+1, -1].map((side) => (
        <mesh
          key={`headlight-${side}`}
          position={[dims.l / 2, dims.h * 0.55, side * dims.w * 0.3]}
        >
          <boxGeometry args={[LIGHT_SIZE, LIGHT_SIZE, LIGHT_SIZE]} />
          <meshStandardMaterial
            color="#fff6d8"
            emissive="#fff6d8"
            emissiveIntensity={1.0}
            roughness={0.3}
          />
        </mesh>
      ))}
      {[+1, -1].map((side) => (
        <mesh
          key={`taillight-${side}`}
          position={[-dims.l / 2, dims.h * 0.55, side * dims.w * 0.3]}
        >
          <boxGeometry args={[LIGHT_SIZE, LIGHT_SIZE, LIGHT_SIZE]} />
          <meshStandardMaterial
            color="#e5484d"
            emissive="#e5484d"
            emissiveIntensity={0.8}
            roughness={0.3}
          />
        </mesh>
      ))}
    </group>
  );
}