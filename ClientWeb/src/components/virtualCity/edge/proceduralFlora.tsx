/**
 * 批次 26 · edge 共享：边缘带植被的程序化 fallback（§27.3 契约）。
 *
 * GLB 缺失 / 加载中 / 失败时，由 GlbInstanced 渲染本文件的程序化几何：
 *   - ProceduralPines   针叶树（棕干 + 3 层深绿圆锥）→ 2 draw call
 *   - ProceduralOaks    阔叶树（棕干 + 球冠）          → 2 draw call
 *   - ProceduralCacti   仙人掌（主干 + 双臂）          → 2 draw call
 *
 * 全部走 drei <Instances>（同 geometry 同 material 逐实例矩阵），与
 * props/TreesInstanced.tsx 同模式。布点由调用方确定性生成后传入。
 */

import { Instances, Instance } from '@react-three/drei';
import { u } from '../cityScale';

/** 单棵植被的布点（y 恒为地表 0）。 */
export interface FloraSpot {
  x: number;
  z: number;
  scale: number;
  rotY: number;
}

const TRUNK_COLOR = '#5d4037';
const PINE_COLOR = '#2e5d3a';
const OAK_CROWN_COLOR = '#4a7a3c';
const CACTUS_COLOR = '#4e7a3f';

/** 针叶树 fallback：棕干（cylinder）+ 3 层深绿圆锥（cone），基准高 ~6m（≈ pine_tree.glb）。 */
export function ProceduralPines({ spots }: { spots: FloraSpot[] }) {
  return (
    <group>
      {/* 主干 ×N → 1 draw call */}
      <Instances limit={Math.max(1, spots.length)} range={spots.length}>
        <cylinderGeometry args={[u(0.12), u(0.18), u(1.6), 6]} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
        {spots.map((t, i) => (
          <Instance
            key={`trunk-${i}`}
            position={[t.x, u(0.8) * t.scale, t.z]}
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
              position={[t.x, (layer.y + layer.h / 2) * t.scale, t.z]}
              rotation={[0, t.rotY, 0]}
              scale={[layer.r * t.scale, layer.h * t.scale, layer.r * t.scale]}
            />
          )),
        )}
      </Instances>
    </group>
  );
}

/** 阔叶树 fallback：棕干 + icosahedron 球冠，基准高 ~5m（≈ oak_tree.glb 视觉体量）。 */
export function ProceduralOaks({ spots }: { spots: FloraSpot[] }) {
  return (
    <group>
      <Instances limit={Math.max(1, spots.length)} range={spots.length}>
        <cylinderGeometry args={[u(0.12), u(0.18), u(1.8), 6]} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
        {spots.map((t, i) => (
          <Instance
            key={`trunk-${i}`}
            position={[t.x, u(0.9) * t.scale, t.z]}
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
            position={[t.x, u(2.6) * t.scale, t.z]}
            rotation={[0, t.rotY, 0]}
            scale={u(1.6) * t.scale}
          />
        ))}
      </Instances>
    </group>
  );
}

/** 仙人掌 fallback：柱状主干 + 2 臂（≈ cactus.glb：主干 + 2 臂，基准高 ~3m）。 */
export function ProceduralCacti({ spots }: { spots: FloraSpot[] }) {
  return (
    <group>
      {/* 主干 ×N → 1 draw call */}
      <Instances limit={Math.max(1, spots.length)} range={spots.length}>
        <cylinderGeometry args={[u(0.22), u(0.26), u(3), 8]} />
        <meshStandardMaterial color={CACTUS_COLOR} roughness={0.8} />
        {spots.map((t, i) => (
          <Instance
            key={`body-${i}`}
            position={[t.x, u(1.5) * t.scale, t.z]}
            rotation={[0, t.rotY, 0]}
            scale={t.scale}
          />
        ))}
      </Instances>
      {/* 双臂 ×2N → 1 draw call（横臂 + 竖臂合并为单位圆柱逐实例变换） */}
      <Instances limit={Math.max(1, spots.length * 4)} range={spots.length * 4}>
        <cylinderGeometry args={[u(0.12), u(0.12), 1, 6]} />
        <meshStandardMaterial color={CACTUS_COLOR} roughness={0.8} />
        {spots.flatMap((t, i) =>
          [-1, 1].flatMap((side) => {
            const cos = Math.cos(t.rotY);
            const sin = Math.sin(t.rotY);
            // 横臂：沿树的本地 x 方向伸出；竖臂：在横臂末端向上
            const armLen = u(0.5) * t.scale;
            const hx = t.x + cos * side * armLen * 0.5;
            const hz = t.z - sin * side * armLen * 0.5;
            const ex = t.x + cos * side * armLen;
            const ez = t.z - sin * side * armLen;
            const upLen = u(0.9) * t.scale;
            return [
              <Instance
                key={`arm-h-${i}-${side}`}
                position={[hx, u(1.7) * t.scale, hz]}
                rotation={[0, t.rotY, (Math.PI / 2) * side]}
                scale={[1, armLen, 1]}
              />,
              <Instance
                key={`arm-v-${i}-${side}`}
                position={[ex, u(1.7) * t.scale + upLen / 2, ez]}
                scale={[1, upLen, 1]}
              />,
            ];
          }),
        )}
      </Instances>
    </group>
  );
}
