/**
 * 批次 26 · edge 共享：边缘带植被的程序化 fallback（§27.3 契约）。
 *
 * GLB 缺失 / 加载中 / 失败时，由 GlbInstanced 渲染本文件的程序化几何：
 *   - ProceduralPines   针叶树（棕干 + 3 层深绿圆锥）→ 2 draw call
 *   - ProceduralOaks    阔叶树（棕干 + 球冠，按 season 取 oakTree / oakTreeWinter 表值）→ 2 draw call
 *   - ProceduralCacti   仙人掌（主干 + 双臂）          → 2 draw call
 *
 * 全部走 drei <Instances>（同 geometry 同 material 逐实例矩阵），与
 * props/TreesInstanced.tsx 同模式。布点由调用方确定性生成后传入。
 *
 * 批次 29：三种植被的包围盒归一化到 cityScale.REAL_DIMS_M（见下方 PINE/OAK/CACTUS），
 * 与 nature/*.glb 同表值 —— 此前 fallback 只有 GLB 的 1/2~1/3 高（禁用 GLB 时全城
 * 植被"缩水"，属"同物件两条路径尺寸漂移"）。
 */

import { Instances, Instance } from '@react-three/drei';
import { u, worldDims } from '../cityScale';
import type { CitySeason } from '../cityTimeStore';

/** 单棵植被的布点（y 恒为地表 0）。 */
export interface FloraSpot {
  x: number;
  z: number;
  scale: number;
  rotY: number;
}

/**
 * 尺寸口径（批次 29）：三种植被的**包围盒取自 cityScale.REAL_DIMS_M**
 * （pineTree 3.4×8.0 / oakTree 4.5×10.1 / cactus 1.7×2.9 m），与 nature/*.glb
 * 共用同一行表值。做法 = 把原程序化几何的基准（松 6.1 m / 橡 4.2 m / 仙 3.0 m 高）
 * 按表值**归一化**（高度 kY、宽度 kX），保留原有造型比例 ⇒ 两条渲染路径包围盒一致。
 * 此前 fallback 比 GLB 小 2~3 倍（禁用 GLB 时全城植被整体"缩水"）。
 */
const PINE = worldDims('pineTree');
const OAK = worldDims('oakTree');
const OAK_WINTER = worldDims('oakTreeWinter');
const CACTUS = worldDims('cactus');

/**
 * 归一化系数：表值 / 原程序化基准。
 * 基准（= 改造前的包围盒）：松 3.0 宽 × 6.1 高；橡 3.2 宽 × 4.2 高；
 * 仙 1.24 宽（双臂外伸 2×(0.5+0.12)）× 3.0 高。
 */
const PINE_KY = PINE.y / u(6.1);
const PINE_KX = PINE.x / u(3.0);
const CACTUS_KY = CACTUS.y / u(3.0);
const CACTUS_KX = CACTUS.x / (2 * (u(0.5) + u(0.12)));

/**
 * 阔叶树按季归一化参数（**同一行表值驱动 GLB 与 fallback**）：
 *   - leafy（夏/春/秋）= REAL_DIMS_M.oakTree；
 *   - winter（冬）= REAL_DIMS_M.oakTreeWinter（落叶 ⇒ 冠幅/总高本就更细）。
 * 冠半径 = 表值冠幅一半（icosahedron detail=1 的顶点全在半径 1 球面上 ⇒ 包围盒恰为 2r），
 * 冠心 = 总高 − 冠半径 ⇒ 冠顶恰为表值总高。
 */
const OAK_SHAPE: Record<'leafy' | 'winter', { ky: number; kx: number; crownR: number }> = {
  leafy: { ky: OAK.y / u(4.2), kx: OAK.x / u(3.2), crownR: OAK.x / 2 },
  winter: { ky: OAK_WINTER.y / u(4.2), kx: OAK_WINTER.x / u(3.2), crownR: OAK_WINTER.x / 2 },
};

const TRUNK_COLOR = '#5d4037';
const PINE_COLOR = '#2e5d3a';
const OAK_CROWN_COLOR = '#4a7a3c';
const CACTUS_COLOR = '#4e7a3f';

/** 针叶树 fallback：棕干（cylinder）+ 3 层深绿圆锥（cone），总高/冠幅归一化到表值 8.0 × 3.4 m。 */
export function ProceduralPines({ spots }: { spots: FloraSpot[] }) {
  return (
    <group>
      {/* 主干 ×N → 1 draw call（高度同乘 kY ⇒ 干底恰在 y=0，不下穿地面） */}
      <Instances limit={Math.max(1, spots.length)} range={spots.length}>
        <cylinderGeometry args={[u(0.12) * PINE_KX, u(0.18) * PINE_KX, u(1.6) * PINE_KY, 6]} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
        {spots.map((t, i) => (
          <Instance
            key={`trunk-${i}`}
            position={[t.x, u(0.8) * PINE_KY * t.scale, t.z]}
            rotation={[0, t.rotY, 0]}
            scale={t.scale}
          />
        ))}
      </Instances>
      {/* 3 层圆锥冠 ×N → 1 draw call（单位圆锥，逐实例缩放出错落三层） */}
      <Instances limit={Math.max(1, spots.length * 3)} range={spots.length * 3}>
        <coneGeometry args={[1, 1, 8]} />
        <meshStandardMaterial color={PINE_COLOR} roughness={0.85} />
        {spots.flatMap((t, i) =>
          [
            { r: u(1.5), h: u(2.2), y: u(2.2) },
            { r: u(1.1), h: u(1.9), y: u(3.4) },
            { r: u(0.7), h: u(1.6), y: u(4.5) },
          ].map((layer, li) => (
            <Instance
              key={`cone-${i}-${li}`}
              position={[t.x, (layer.y * PINE_KY + (layer.h * PINE_KY) / 2) * t.scale, t.z]}
              rotation={[0, t.rotY, 0]}
              scale={[layer.r * PINE_KX * t.scale, layer.h * PINE_KY * t.scale, layer.r * PINE_KX * t.scale]}
            />
          )),
        )}
      </Instances>
    </group>
  );
}

/**
 * 阔叶树 fallback：棕干 + icosahedron 球冠；总高/冠幅归一化到表值
 * （夏/春/秋 10.1 × 4.5 m；冬 9.55 × 4.12 m）。`season` 决定取哪一行 ——
 * 必须与 EastForest 切 GLB 变体的季节一致，否则两条路径尺寸漂移。
 */
export function ProceduralOaks({ spots, season = 'summer' }: { spots: FloraSpot[]; season?: CitySeason }) {
  const { ky, kx, crownR } = season === 'winter' ? OAK_SHAPE.winter : OAK_SHAPE.leafy;
  const totalH = season === 'winter' ? OAK_WINTER.y : OAK.y;
  return (
    <group>
      <Instances limit={Math.max(1, spots.length)} range={spots.length}>
        <cylinderGeometry args={[u(0.12) * kx, u(0.18) * kx, u(1.8) * ky, 6]} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
        {spots.map((t, i) => (
          <Instance
            key={`trunk-${i}`}
            position={[t.x, u(0.9) * ky * t.scale, t.z]}
            rotation={[0, t.rotY, 0]}
            scale={t.scale}
          />
        ))}
      </Instances>
      <Instances limit={Math.max(1, spots.length)} range={spots.length}>
        <icosahedronGeometry args={[1, 1]} />
        <meshStandardMaterial color={OAK_CROWN_COLOR} roughness={0.85} />
        {spots.map((t, i) => (
          <Instance
            key={`crown-${i}`}
            // 冠心 = 总高 − 冠半径 ⇒ 冠顶恰为表值总高
            position={[t.x, (totalH - crownR) * t.scale, t.z]}
            rotation={[0, t.rotY, 0]}
            scale={crownR * t.scale}
          />
        ))}
      </Instances>
    </group>
  );
}

/** 仙人掌 fallback：柱状主干 + 2 臂（≈ cactus.glb：主干 + 2 臂），归一化到表值 2.9 m 高。 */
export function ProceduralCacti({ spots }: { spots: FloraSpot[] }) {
  return (
    <group>
      {/* 主干 ×N → 1 draw call */}
      <Instances limit={Math.max(1, spots.length)} range={spots.length}>
        <cylinderGeometry args={[u(0.22) * CACTUS_KX, u(0.26) * CACTUS_KX, u(3) * CACTUS_KY, 8]} />
        <meshStandardMaterial color={CACTUS_COLOR} roughness={0.8} />
        {spots.map((t, i) => (
          <Instance
            key={`body-${i}`}
            position={[t.x, u(1.5) * CACTUS_KY * t.scale, t.z]}
            rotation={[0, t.rotY, 0]}
            scale={t.scale}
          />
        ))}
      </Instances>
      {/* 双臂 ×2N → 1 draw call（横臂 + 竖臂合并为单位圆柱逐实例变换） */}
      <Instances limit={Math.max(1, spots.length * 4)} range={spots.length * 4}>
        <cylinderGeometry args={[u(0.12) * CACTUS_KX, u(0.12) * CACTUS_KX, 1, 6]} />
        <meshStandardMaterial color={CACTUS_COLOR} roughness={0.8} />
        {spots.flatMap((t, i) =>
          [-1, 1].flatMap((side) => {
            const cos = Math.cos(t.rotY);
            const sin = Math.sin(t.rotY);
            // 横臂：沿树的本地 x 方向伸出；竖臂：在横臂末端向上
            const armLen = u(0.5) * CACTUS_KX * t.scale;
            const hx = t.x + cos * side * armLen * 0.5;
            const hz = t.z - sin * side * armLen * 0.5;
            const ex = t.x + cos * side * armLen;
            const ez = t.z - sin * side * armLen;
            const upLen = u(0.9) * CACTUS_KY * t.scale;
            return [
              <Instance
                key={`arm-h-${i}-${side}`}
                position={[hx, u(1.7) * CACTUS_KY * t.scale, hz]}
                rotation={[0, t.rotY, (Math.PI / 2) * side]}
                scale={[1, armLen, 1]}
              />,
              <Instance
                key={`arm-v-${i}-${side}`}
                position={[ex, u(1.7) * CACTUS_KY * t.scale + upLen / 2, ez]}
                scale={[1, upLen, 1]}
              />,
            ];
          }),
        )}
      </Instances>
    </group>
  );
}
