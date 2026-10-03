/**
 * RoofPlantLayer — 批次 53「建筑屋顶真实感」全城屋顶设备机组实例化层。
 *
 * ── 为什么替换掉 `props/RooftopAcc.tsx` ───────────────────────────────────
 * 旧 `RooftopAcc` 有三个各自独立的失效（详见方案 53 §1.2 D6/D7/D8）：
 *   1. **几何分支不可达**：`tex ? <Billboard/> : <几何/>`，而三张 rooftop 贴图
 *      全部在盘 ⇒ 永远走 Billboard，下方 30 行 3D 几何是死代码；
 *   2. **3D 是贴纸**：`meshBasicMaterial` 的 0.3×0.3 世界单位公告板，斜视即
 *      「屋顶上飘着一张图」，且不受光照，昼夜亮度不变；
 *   3. **两套尺寸口径**：`RooftopAcc` 每区 1~2 个 ⌀2.4 m 水箱（尺寸正确），
 *      而真正每栋楼都调的 `building_shapes::rooftopAccent` 是 ⌀**0.3 m**，
 *      同一物件在同一个项目里差 8 倍。
 * 本层一次性收口：设备本体改由 GLB 表达，程序化侧只剩盒几何表达不了的部分
 * （检修平台 / 马道 / 天线 / 接闪杆 / 爬梯），全部并入 `building_shapes`
 * 的区级点缀 mesh（0 新增 draw call）。
 *
 * ── 布点规则 ────────────────────────────────────────────────────────────
 * · 逐栋：经 `buildingsFor(def)` 取该区全部楼栋，屋面世界 y 走
 *   `DISTRICT_SURFACE_Y + buildingTopY(def.id, prosperity, spec.factor)`
 *   —— 与 `DistrictBuildings` 渲染楼高、`freeViewColliders` 相机碰撞体**同源**
 *   （批次 32 立此约定），三者永远同高。
 * · **只放 slab 平屋面**（12 个城区，全城体量最大宗）。其余 4 型一律跳过，
 *   理由逐条列在下方 `ROOFLESS` 注释里 —— 它们的「渲染楼顶」与
 *   `buildingTopY`（本层唯一的楼高事实来源）**不是同一个标高**，
 *   按 `buildingTopY` 放设备会让设备悬空或埋进屋面。
 * · **屋面够大才放**：设备组团含检修平台占 8.37×5.47 m，屋面须 ≥
 *   `9.2 × 6.3 m`（含女儿墙内缩 0.25 m 墙厚）才放得下 ⇒ 换算到世界单位
 *   `w ≥ 0.92 && d ≥ 0.63`。窄体量自动跳过，**绝不允许悬空出挑**。
 * · 落点在楼栋轮廓内按 hash 抖动 ±15%，朝向取 4 档（0/90/180/270°）——
 *   全城同一朝向会让「复制粘贴感」立刻显形。
 * · 确定性：`hashStr`（与 `building_shapes` 同款 FNV-1a），禁 `Math.random`，
 *   同楼同形，跨帧 / 跨重载稳定（§92a）。
 *
 * ── draw call ───────────────────────────────────────────────────────────
 * `GlbInstanced` 的 draw call 数 = GLB 子网格数（7 材质槽 → 7），
 * **与实例数无关** ⇒ 全城 ~100 栋有设备的楼合计 7 draw call。
 * 对比旧 `RooftopAcc` 逐件 `<Billboard>`（32 区 × 1~2 件各自材质）。
 *
 * 契约：lag_docs/虚拟城市/已实现/53-建筑屋顶真实感/01-方案设计.md §3.2。
 */

import { useMemo } from 'react';
import type { VirtualCityDistrictDef } from '@/types/virtualCity';
import { modelUrl } from '@/assets/models';
import { GlbInstanced, type GlbInstanceTRS } from '../edge/glbInstanced';
import { DISTRICT_SURFACE_Y, buildingTopY, sizeTargetFor, u } from '../cityScale';
import { buildingsFor } from '../building_layout';
import { DISTRICT_ARCHETYPE } from '../building_shapes';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

/*
 * 无可用平屋面的 4 型（`building_shapes` 的 archetype）—— 逐条理由：
 * （`propsForDistrict` 循环里以 `archetype !== 'slab'` 一刀切，此处只留判读依据）
 *
 * - **house**：坡屋顶。屋面是 `prismGeometry` 的斜面，设备必须骑在坡上，
 *   本件是水平设备区 ⇒ 必穿模。
 * - **pavilion**：四周 1.5 m 挑檐 + 8 根斜撑，屋面被檐下阴影与斜撑占满；
 *   且这 2 个区是公园（central_park / wetland_park），真实公园亭屋顶不放冷却塔。
 * - **tower**：`buildingTopY` 是**塔冠顶**，而塔冠只有 `w*0.55 × d*0.55`
 *   （9.35 × 7.4 m）。8.37 m 的设备区放上去两侧只剩 ~0.5 m 余量，旋转即悬空。
 *   塔冠另有自己的屋面帽 + 女儿墙 + 电梯机房 + 擦窗机轨道 + 接闪杆
 *   （`buildTowerParts`），裙楼退台只有 1.5~1.9 m 宽的环带，也放不下。
 * - **shed**：`buildingTopY` 是 `h`，而锯齿顶的实体顶在 `bH = h*0.8`，
 *   齿高 `u(1.2)`。`h` 小（2 层厂房 6 m）时 `0.2h = 1.2 m` 刚好齐平，
 *   `h` 再小就**设备埋进锯齿**。厂房真实设备在齿间的平屋面上，
 *   要做对得按齿位单独布点 —— 属独立课题，另立批次。
 */
/** 屋面最小净尺寸（世界单位）：设备组团 0.837×0.547 + 女儿墙内缩 0.0125×2 边距。 */
const MIN_ROOF_W = u(9.2);
const MIN_ROOF_D = u(6.3);

/** 有设备的楼占比：约 55%（其余留干净屋面，避免全城每栋都顶着一组设备）。 */
const KEEP_RATIO = 0.55;

/** 4 档朝向（度）—— 全城同朝向会立刻显形是「复制粘贴」。 */
const YAWS = [0, Math.PI / 2, Math.PI, (3 * Math.PI) / 2] as const;

/** 确定性伪随机（与 `building_shapes::hashStr` 同款 FNV-1a；禁 Math.random）。 */
function hashStr(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

/** GLB 缺失 / 加载失败 / 加载中时的程序化降级件（零代码分支，见 §27.5）。 */
function RoofPlantFallback({ x, y, z }: { x: number; y: number; z: number }) {
  return (
    <group position={[x, y, z]}>
      <mesh position={[-0.255, u(1.8), 0]}>
        <cylinderGeometry args={[u(1.1), u(1.1), u(2.6), 12]} />
        <meshStandardMaterial color="#c2c6cb" roughness={0.34} metalness={0.85} />
      </mesh>
      <mesh position={[u(0.05), u(1.66), u(0.03)]}>
        <boxGeometry args={[u(2.6), u(3.72), u(3.8)]} />
        <meshStandardMaterial color="#c8ccc4" roughness={0.62} metalness={0.05} />
      </mesh>
    </group>
  );
}

export interface RoofPlantLayerProps {
  /** 全城区表（与 `StreetPropsLayer` 消费同一份）。 */
  districts: VirtualCityDistrictDef[];
  /** 城区繁荣度（与楼高同源，缺省回落到 fallback）。 */
  prosperityByDistrict?: ReadonlyMap<string, number>;
  /** 繁荣度缺省值（与 StreetPropsLayer 同口径）。 */
  prosperityFallback: number;
}

export function RoofPlantLayer({
  districts,
  prosperityByDistrict,
  prosperityFallback,
}: RoofPlantLayerProps) {
  const url = modelUrl('civic', 'rooftop_plant');
  const sizeTarget = sizeTargetFor('roofPlant');

  const instances = useMemo<GlbInstanceTRS[]>(() => {
    const out: GlbInstanceTRS[] = [];
    for (const def of districts) {
      // 只放 slab 平屋面（见 ROOFLESS 注释）。
      if (DISTRICT_ARCHETYPE[def.id] !== 'slab') continue;
      const prosperity = prosperityByDistrict?.get(def.id) ?? prosperityFallback;
      for (const spec of buildingsFor(def)) {
        if (spec.w < MIN_ROOF_W || spec.d < MIN_ROOF_D) continue;
        const h = hashStr(`roofplant-${def.id}-${spec.idx ?? 0}`);
        if ((h % 1000) / 1000 > KEEP_RATIO) continue;
        // ±15% 轮廓内抖动，避免设备永远钉在楼栋正中
        const jx = (((h >>> 10) % 1000) / 1000 - 0.5) * spec.w * 0.30;
        const jz = (((h >>> 20) % 1000) / 1000 - 0.5) * spec.d * 0.30;
        out.push({
          position: [
            def.x + spec.x + jx,
            DISTRICT_SURFACE_Y + buildingTopY(def.id, prosperity, spec.factor),
            def.z + spec.z + jz,
          ],
          rotationY: YAWS[(h >>> 15) % 4],
        });
      }
    }
    return out;
  }, [districts, prosperityByDistrict, prosperityFallback]);

  // §禁止静默降级：URL 空（资产漏登记）时 dev 告警，而不是静默渲染空屋面。
  if (import.meta.env.DEV && !url) {
    console.warn(
      '[RoofPlantLayer] civic/rooftop_plant.glb 缺失 ⇒ 全城屋顶无设备。' +
      '重跑：cd 3d_script && blender --background --python build_rooftop_plant.py -- ' +
      '<repo>/ClientWeb/src/assets/models/civic/rooftop_plant.glb',
    );
  }

  const infoProps = useObjectInfoProps('prop.roof-plant', { anchorY: u(2.0) });

  if (!url) return null;
  return (
    <group {...infoProps}>
      <GlbInstanced
        url={url}
        instances={instances}
        sizeTarget={sizeTarget}
        castShadow
        receiveShadow
        fallback={instances.map((it, i) => (
          <RoofPlantFallback key={i} x={it.position[0]} y={it.position[1]} z={it.position[2]} />
        ))}
      />
    </group>
  );
}
