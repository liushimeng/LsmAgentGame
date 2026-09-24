/**
 * StreetLightsInstanced — 批次 20 渲染性能专项（文档 1 §3.3）：
 *
 * 全城主干/次干道路灯（80 时代 ~104 → 120 地图 ~220+）合并为 3 个
 * drei <Instances>（底座 / 主杆 / 灯头三段各自实例化，逐实例矩阵）→
 * 全部路灯总计 **3 个 draw call**（改造前每盏 3-4 mesh 挂在 Road 旋转组内）。
 *
 * 尺寸与 props/StreetLight KIND_DIMS.main 同源；次干道按总高比例
 * （0.70/1.10）整体缩放。原 variant a/b/c 贴图 Billboard 灯片在实例化下
 * 移除（灯头保留 emissive 暖光），StreetLight.tsx 文件保留可回退。
 * 路灯本就不投影阴影（castShadow=false 预算不变）。
 */

import { Instances, Instance } from '@react-three/drei';

export interface InstancedLamp {
  x: number;
  z: number;
  /** 朝向（弧度，绕 Y；随所在道路方向）。 */
  rot: number;
  kind: 'main' | 'side';
}

/** 主干道灯尺寸（= StreetLight KIND_DIMS.main，世界单位）。 */
const BASE_H = 0.06;
const POLE_H = 0.96;
const HEAD_H = 0.08;
const BASE_GEOM: [number, number, number, number] = [0.07, 0.1, BASE_H, 8];
const POLE_GEOM: [number, number, number, number] = [0.035, 0.05, POLE_H, 6];
const HEAD_GEOM: [number, number, number] = [0.16, HEAD_H, 0.16];
const BASE_Y = BASE_H / 2;
const POLE_Y = BASE_H + POLE_H / 2;
const HEAD_Y = BASE_H + POLE_H + HEAD_H / 2;
/** 次干道整体缩放（StreetLight 总高 side 0.70 / main 1.10）。 */
const SIDE_SCALE = 0.7 / 1.1;

export function StreetLightsInstanced({ lamps }: { lamps: InstancedLamp[] }) {
  const renderPart = (
    key: string,
    geometry: JSX.Element,
    material: JSX.Element,
    list: InstancedLamp[],
    yOf: (s: number) => number,
  ) => (
    <Instances key={key} limit={Math.max(1, list.length)} range={list.length}>
      {geometry}
      {material}
      {list.map((l, i) => {
        const s = l.kind === 'side' ? SIDE_SCALE : 1;
        return (
          <Instance
            key={`${key}-${i}`}
            position={[l.x, yOf(s), l.z]}
            rotation={[0, l.rot, 0]}
            scale={s}
          />
        );
      })}
    </Instances>
  );

  return (
    <group>
      {/* ① 底座 ×N → 1 draw call */}
      {renderPart(
        'lamp-base',
        <cylinderGeometry args={BASE_GEOM} />,
        <meshStandardMaterial color="#4a4f5a" roughness={0.7} metalness={0.4} />,
        lamps,
        (s) => BASE_Y * s,
      )}
      {/* ② 主杆 ×N → 1 draw call */}
      {renderPart(
        'lamp-pole',
        <cylinderGeometry args={POLE_GEOM} />,
        <meshStandardMaterial color="#6b7280" roughness={0.55} metalness={0.6} />,
        lamps,
        (s) => POLE_Y * s,
      )}
      {/* ③ 灯头 ×N → 1 draw call（emissive 暖光，与 StreetLight 灯头一致） */}
      {renderPart(
        'lamp-head',
        <boxGeometry args={HEAD_GEOM} />,
        <meshStandardMaterial
          color="#aaa9a0"
          emissive="#fff5b8"
          emissiveIntensity={0.55}
          roughness={0.4}
        />,
        lamps,
        (s) => HEAD_Y * s,
      )}
    </group>
  );
}
