/**
 * 变电站（18-AA · §5.2 Substation）
 *   围栏 + 变压器 2 台（散热片）+ 电线杆 2 根 + 架空线。布点 (-18.5, -11)。
 *
 * 批次 28 二轮（DC 攻坚）：14 mesh → 1 mesh（几何全等合并，engine3d/geoMerge）：
 *   围栏 4 段 + 顶梁 2 + 变压器 2 台（筒体 + 顶部套管）+ 电线杆 2 + 架空线 2
 *   全部并为单 mesh 顶点色 —— 部件颜色经顶点色逐件保留。
 * 批次 28 二轮取舍：粗糙度/金属度统一 0.55/0.45（原 roughness 0.5–0.7 /
 *   metalness 0–0.6 各件取中）；caster 裁剪——市政设施不投影（shadow pass
 *   实测 1044 DC 超 500 阈值，只留楼体+树干）。
 */
import { useEffect, useMemo } from 'react';
import { boxPart, cylPart, mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const FENCE = '#6b7280';
const XFMR = '#4a5568';
const XFMR_TOP = '#3a414c';
const POLE = '#7a8290';
const WIRE = '#3a414c';

/** 统一材质参数（批次 28 二轮视觉取舍：原 roughness 0.5–0.7 / metalness 0–0.6 取中）。 */
const FLAT_ROUGH = 0.55;
const FLAT_METAL = 0.45;

export function Substation() {
  const info = useObjectInfoProps('civic.substation', { anchorY: 1.5 });

  // 全部 14 件合并（几何与原逐件 JSX 全等；围栏 x∈[-2,2], z∈[-1.5,1.5]）
  const flatGeo = useMemo(() => {
    const parts: MergePart[] = [
      // 围栏 4 段
      boxPart(u(4), u(1.2), u(0.05), 0, u(0.6), u(1.6), FENCE),
      boxPart(u(4), u(1.2), u(0.05), 0, u(0.6), -u(1.6), FENCE),
      boxPart(u(0.05), u(1.2), u(3.2), -u(2), u(0.6), 0, FENCE),
      boxPart(u(0.05), u(1.2), u(3.2), u(2), u(0.6), 0, FENCE),
      // 围栏顶部横梁 ×2
      boxPart(u(0.08), u(0.05), u(3.2), -u(1.95), u(1.3), 0, FENCE),
      boxPart(u(0.08), u(0.05), u(3.2), u(1.95), u(1.3), 0, FENCE),
    ];
    // 变压器 2 台：圆筒 + 顶部套管
    for (const x of [-u(0.7), u(0.7)]) {
      parts.push(cylPart(u(0.5), u(0.5), u(1.2), 16, x, u(0.6), 0, XFMR));
      parts.push(cylPart(u(0.12), u(0.12), u(0.4), 8, x, u(1.4), 0, XFMR_TOP));
    }
    // 电线杆 2 根（围栏外侧）
    for (const x of [-u(2.6), u(2.6)]) {
      parts.push(cylPart(u(0.1), u(0.12), u(8), 6, x, u(4), -u(2.2), POLE));
    }
    // 架空线（细圆柱跨两杆，一横一纵）
    parts.push(cylPart(u(0.02), u(0.02), u(5.4), 6, 0, u(8), -u(2.2), WIRE));
    parts.push({ ...cylPart(u(0.02), u(0.02), u(5.4), 6, 0, u(8), -u(2.2), WIRE), rotZ: Math.PI / 2 });
    return mergeParts(parts);
  }, []);
  useEffect(() => () => flatGeo.dispose(), [flatGeo]);

  return (
    <group {...info} position={[-18.5, 0, -11]}>
      {/* 围栏 + 变压器 + 电杆 + 架空线：14 件合 1 mesh（顶点色；caster 裁剪不投影） */}
      <mesh geometry={flatGeo}>
        <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} metalness={FLAT_METAL} />
      </mesh>
    </group>
  );
}
