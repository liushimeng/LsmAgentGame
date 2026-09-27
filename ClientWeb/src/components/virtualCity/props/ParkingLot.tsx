/**
 * ParkingLot — 划线停车场 + 静态车（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 商业中心东北角：沥青场面 + 8 条白色划线（双排 4 位）+ 3 辆简化静态车
 * （2-box 车身 + 4 轮；色取自 StreetPropsLayer::VEHICLE_COLORS 同源色板）。
 *
 * 批次 28 二轮（DC 攻坚）：27 mesh → 2 mesh（engine3d/geoMerge，几何全等）：
 *   1) 场地面（asphalt 贴图）+ 8 条划线 mergeGrouped 单 mesh 双 group
 *      （材质各自保留：贴图沥青 / 白线纯色，贴图 UV 逐位不动）；
 *   2) 3 辆静态车 ×6 件全部烘焙合并（车位平移 + 车头朝向 rotY 烘进部件
 *      matrix）→ 单顶点色 mesh。
 * 批次 28 二轮取舍：车漆 0.4/0.5 与轮胎 0.9/0 统一为 0.65/0.25
 * （envMapIntensity 0.9）；划线随合并 mesh 获 receiveShadow（原仅场面接收
 * 阴影，划线现也接受楼体投影，属增强向偏差）。
 * 批次 28 二轮取舍：caster 裁剪——地标不投影（shadow pass 实测 1044 DC
 * 超阈，全地标 castShadow=false；原车身/车顶投影随批取消，场面
 * receiveShadow 保留承接楼体投影）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §8.4。
 */

import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { streetTileUrl } from '@/assets/images/virtualCity';
import { u } from '../cityScale';
import {
  type GroupedMergePart,
  type MergePart,
  mergeGrouped,
  mergeParts,
  useSharedTexture,
} from '@/engine3d';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const LOT_X = 12.4; // commerce (10,-2) 东北角：pad 内缘 x 6..14 / z -6..2，
const LOT_Z = -4.9; // 楼群占中心 ±3.1 → 场地收窄到 2.8×2.0 贴 pad 东北角避让
const LOT_W = 2.8;
const LOT_D = 2.0;

/** 静态车色板（与 StreetPropsLayer VEHICLE_VARIANTS 主色同源；此处独立常量避免循环 import）。 */
const CAR_COLORS = ['#3b6bb0', '#e8b930', '#8a6a3d'];
const TIRE = '#1a1d22';

/** 整车合并统一材质参数（批次 28 二轮取舍：车漆 0.4/0.5 与轮胎 0.9/0 取中）。 */
const CAR_ROUGH = 0.65;
const CAR_METAL = 0.25;

/** 车位划线：双排 × 4 条竖线 + 中通道。 */
const STALL_X = [-1.2, -0.4, 0.4, 1.2];

interface Props {
  rotation?: number;
}

/** 平躺 plane 的 TRS 矩阵（等价 rotation=[-π/2,0,0]）。 */
function flatMatrix(x: number, y: number, z: number): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(-Math.PI / 2, 0, 0)),
    new THREE.Vector3(1, 1, 1),
  );
}

/** 部件局部 TRS 矩阵（车身/车顶无旋转；车轮绕 X 轴 90° 轴沿 x）。 */
function partMatrix(x: number, y: number, z: number, euler?: [number, number, number]): THREE.Matrix4 {
  const q = euler
    ? new THREE.Quaternion().setFromEuler(new THREE.Euler(euler[0], euler[1], euler[2]))
    : new THREE.Quaternion();
  return new THREE.Matrix4().compose(new THREE.Vector3(x, y, z), q, new THREE.Vector3(1, 1, 1));
}

interface CarSpec {
  x: number;
  z: number;
  color: string;
  rotation: number;
}

/** 单辆静态车 6 件（车位平移 + rotY 朝向烘进矩阵；几何与原 StaticCar 全等）。 */
function carParts(car: CarSpec): MergePart[] {
  const l = u(4.5) / 2; // 车长一半 ≈ 0.225（借 Vehicle sedan 尺寸）
  const w = 0.09;
  const carM = new THREE.Matrix4().compose(
    new THREE.Vector3(car.x, 0, car.z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(0, car.rotation, 0)),
    new THREE.Vector3(1, 1, 1),
  );
  const withCar = (m: THREE.Matrix4): THREE.Matrix4 => carM.clone().multiply(m);
  return [
    // 车身 + 车顶（略窄）
    {
      geo: new THREE.BoxGeometry(w, u(1.1), l * 2),
      matrix: withCar(partMatrix(0, u(0.55), 0)),
      color: car.color,
    },
    {
      geo: new THREE.BoxGeometry(w * 0.85, u(0.5), l * 1.1),
      matrix: withCar(partMatrix(0, u(1.25), -0.02)),
      color: car.color,
    },
    // 4 轮（扁圆柱，轴沿 x）
    ...([
      [-w / 2 - 0.005, l * 0.6],
      [w / 2 + 0.005, l * 0.6],
      [-w / 2 - 0.005, -l * 0.6],
      [w / 2 + 0.005, -l * 0.6],
    ] as Array<[number, number]>).map(([px, pz]) => ({
      geo: new THREE.CylinderGeometry(u(0.35), u(0.35), 0.02, 10),
      matrix: withCar(partMatrix(px, u(0.35), pz, [Math.PI / 2, 0, 0])),
      color: TIRE,
    })),
  ];
}

export function ParkingLot({ rotation }: Props) {
  const asphalt = useSharedTexture(streetTileUrl('asphalt_side'), {
    wrap: 'repeat',
    repeat: [3, 2],
  });
  // 静态车布点（确定性：3 辆停 1/3 排）
  const cars = useMemo<CarSpec[]>(
    () => [
      { x: -1.2, z: -0.55, color: CAR_COLORS[0], rotation: Math.PI },
      { x: -0.4, z: -0.55, color: CAR_COLORS[1], rotation: Math.PI },
      { x: 0.8, z: 0.55, color: CAR_COLORS[2], rotation: 0 },
    ],
    [],
  );

  // 场地面 + 划线 → 单 mesh 双 group（组 0 = 沥青贴图 / 组 1 = 白线）
  const lotGeo = useMemo(() => {
    const parts: GroupedMergePart[] = [
      { geo: new THREE.PlaneGeometry(LOT_W, LOT_D), matrix: flatMatrix(0, 0.026, 0), mat: 0 },
      ...[-0.55, 0.55].flatMap((rowZ) =>
        STALL_X.map((sx) => ({
          geo: new THREE.PlaneGeometry(0.04, 0.5),
          matrix: flatMatrix(sx, 0.028, rowZ),
          mat: 1,
        })),
      ),
    ];
    return mergeGrouped(parts);
  }, []);
  useEffect(() => () => lotGeo.dispose(), [lotGeo]);

  // 材质数组（组 0 沥青贴图随纹理异步加载重建 / 组 1 白线）
  const lotMats = useMemo(
    () => [
      new THREE.MeshStandardMaterial({
        map: asphalt ?? undefined,
        color: asphalt ? '#ffffff' : '#242c38',
        roughness: 0.94,
      }),
      new THREE.MeshStandardMaterial({ color: '#e8eaee', roughness: 0.85 }),
    ],
    [asphalt],
  );
  useEffect(() => () => lotMats.forEach((m) => m.dispose()), [lotMats]);

  // 3 辆静态车 → 单顶点色 mesh
  const carsGeo = useMemo(() => mergeParts(cars.flatMap(carParts)), [cars]);
  useEffect(() => () => carsGeo.dispose(), [carsGeo]);

  const info = useObjectInfoProps('landmark.parking-lot', { anchorY: 0.5 });
  return (
    <group
      {...info}
      position={[LOT_X, 0, LOT_Z]}
      rotation={[0, rotation ?? 0, 0]}
    >
      {/* 场地 + 划线合并（原场面 receiveShadow 保留；划线随之接收车影） */}
      <mesh geometry={lotGeo} material={lotMats} receiveShadow />
      {/* 静态车 ×3 合并（顶点色逐件保留；批次 28 二轮 caster 裁剪 → 不投影） */}
      <mesh geometry={carsGeo}>
        <meshStandardMaterial
          vertexColors
          roughness={CAR_ROUGH}
          metalness={CAR_METAL}
          envMapIntensity={0.9}
        />
      </mesh>
    </group>
  );
}

export default ParkingLot;
