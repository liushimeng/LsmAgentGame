/**
 * 加油站（18-AA · §5.2 GasStation）
 *   大挑檐雨棚 + 4 柱 + 4 号加油机 + 便利店小屋 + 高杆招牌。布点 (5.8, 6.7)。
 *
 * 批次 28 二轮（DC 攻坚）：18 mesh → 3 mesh（几何全等合并，engine3d/geoMerge）：
 *   1) 纯色件 mesh（顶点色）：雨棚/底沿/4 支柱/4 加油机（体+顶）/便利店檐/
 *      招牌杆 共 16 件 —— 部件颜色经顶点色逐件保留。
 *   2) 便利店小屋 mesh：保持独立（原 receiveShadow 接收楼体投影专属）。
 *   3) 高杆招牌面板 mesh：保留自发光 #ffe066（emissive 件不并入顶点色 mesh）。
 * 批次 28 二轮取舍：纯色件粗糙度/金属度统一 0.7/0.25（原 0.5–1.0 / 0–0.5 区间取中）；
 *   caster 裁剪——市政设施不投影（shadow pass 实测 1044 DC 超 500 阈值，只留楼体+树干）。
 */
import { useEffect, useMemo } from 'react';
import { boxPart, cylPart, mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const CANOPY_RED = '#c0392b';
const PUMP_GREY = '#5a6270';
const SHOP_WHITE = '#e8e8e8';
const SIGN_GLOW = '#ffe066';
const COLUMN_GREY = '#7a8290';
const CANOPY_EDGE = '#e8e3dc';
const PUMP_TOP_DARK = '#161d28';
const SHOP_ROOF_RED = '#a85040';

/** 纯色件统一材质参数（批次 28 二轮视觉取舍：原 roughness 0.5–1.0 / metalness 0–0.5 取中）。 */
const FLAT_ROUGH = 0.7;
const FLAT_METAL = 0.25;

export function GasStation() {
  const info = useObjectInfoProps('civic.gas-station', { anchorY: 4 });

  // 纯色件（几何与原逐件 JSX 全等；加油机随 (±5, ±4.5) 组偏移展开）
  const flatGeo = useMemo(() => {
    const parts: MergePart[] = [
      // 雨棚 + 底沿
      boxPart(u(22), u(0.15), u(14), 0, u(5), 0, CANOPY_RED),
      boxPart(u(22.1), u(0.06), u(14.1), 0, u(4.92), 0, CANOPY_EDGE),
      // 便利店檐口（小屋本体独立 mesh 保 receiveShadow）
      boxPart(u(6.1), u(0.15), u(3.1), 0, u(3.1), u(11), SHOP_ROOF_RED),
      // 高杆招牌杆
      cylPart(u(0.15), u(0.15), u(8), 6, u(13), u(4), 0, COLUMN_GREY),
    ];
    // 雨棚支柱 ×4
    for (const sx of [-1, 1]) {
      for (const sz of [-1, 1]) {
        parts.push(cylPart(u(0.18), u(0.18), u(5), 8, sx * u(10), u(2.5), sz * u(6), COLUMN_GREY));
      }
    }
    // 4 号加油机（柱 + 机 + 顶，随组偏移展开）
    for (const sx of [-1, 1]) {
      for (const sz of [-1, 1]) {
        const px = sx * u(5);
        const pz = sz * u(4.5);
        parts.push(boxPart(u(0.9), u(1.6), u(0.5), px, u(0.8), pz, PUMP_GREY));
        parts.push(boxPart(u(0.7), u(0.3), u(0.4), px, u(2), pz, PUMP_TOP_DARK));
      }
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => flatGeo.dispose(), [flatGeo]);

  // 招牌面板（自发光件，独立 mesh 保留 emissive）
  const signGeo = useMemo(() => mergeParts([boxPart(u(3), u(2.5), u(0.15), u(13), u(8), 0)]), []);
  useEffect(() => () => signGeo.dispose(), [signGeo]);

  // 便利店小屋（原 receiveShadow：接收楼体/树影）
  const shopGeo = useMemo(() => mergeParts([boxPart(u(6), u(3), u(3), 0, u(1.5), u(11))]), []);
  useEffect(() => () => shopGeo.dispose(), [shopGeo]);

  return (
    <group {...info} position={[5.8, 0, 6.7]}>
      {/* 纯色件：16 件合 1 mesh（顶点色逐件保留；caster 裁剪不投影） */}
      <mesh geometry={flatGeo}>
        <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} metalness={FLAT_METAL} />
      </mesh>
      {/* 便利店小屋（caster 裁剪不投影，仍接收投影） */}
      <mesh geometry={shopGeo} receiveShadow>
        <meshStandardMaterial color={SHOP_WHITE} roughness={0.85} />
      </mesh>
      {/* 高杆招牌：自发光面板 */}
      <mesh geometry={signGeo}>
        <meshStandardMaterial color={SIGN_GLOW} emissive={SIGN_GLOW} emissiveIntensity={0.6} />
      </mesh>
    </group>
  );
}
