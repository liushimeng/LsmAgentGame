/**
 * CloudLayer — 天空云层（16-3D城市WebGL质感与城市补全 · 阶段 U；
 *                批次 58 云层真实感：3 云型 + 昼夜着色 + 分层体积）。
 *
 * 批次 58 之前的形态（保留为对照）：6 团云，每团 3-4 片 Billboard 云朵，
 * 单一 `cloud_puff.png`（实为近纯白 RGB 252-255 / alpha ≤54% 的**平面贴片**），
 * 全天一个颜色 ⇒ 「白色纸片飘在天上」的贴片感。
 *
 * 批次 58 四处改造：
 * 1. **3 种真实云型**（程序化贴图，源 `generate_virtual_city_cloud_assets.py`）：
 *    积云 cumulus（底平、顶 cauliflower 分瓣、顶部受光亮白 / 底部冷灰背光）
 *    层云 stratus（横向延展灰层，无立体感）
 *    卷云 cirrus（高空横向丝缕，薄半透明）
 *    —— 旧版 6 团全是同一种云；真实天空是**多型共存**。
 * 2. **天空尺度**（本批最重要的修正）：旧 puff 宽 22~36 m、张角仅 2.5°，
 *    是「道具云」不是「天空云」；改为 400~1100 m、张角 12°~20°。
 *    详见 `cloudsFor` 上方注释里被推翻的批次 16 两处尺度。
 * 3. **云底高于相机上限**：批次 32 自由视角把相机上限抬到 y=140，而云只有
 *    y=60~80 ⇒ 可飞到云上。现云底 175~370，恒 > 140。
 * 4. **云层延伸到地平线**：旧布点半径仅 25~70（城市正上方一小片）⇒ 任何街景
 *    位置看云都在近乎天顶（仰角 ~70°），而 Billboard 朝向由**应用相机**决定，
 *    从下方看即背面 ⇒ `side: FrontSide` 背面剔除 ⇒ 云「消失」。
 *    半径铺开到 30~330 后，远云仰角降到 ~32°，符合人抬头可见的自然云高。
 * 5. **分层体积 + 昼夜着色**：每团云 5-8 层 puff，层间沿视线深度错位产生视差
 *    （消解单片贴片的纸片感）；按 timeOfDay01 染色——正午近白、黄昏橙红
 *    （低角度日光的前向散射）、夜间深蓝低对比（仅余城市光污染）。
 *    着色以 material.color 调制，**不改贴图**（贴图是云的固有色）。
 *
 * 保持不变的契约（批次 16/27 确立，不得回归）：
 * - 沿 +x 漂移 0.15-0.3 单位/s（回绕边界随云体尺度放大，见 CLOUD_WRAP_X）
 * - 天气系数 cloudOpacity（晴 0.10 → 暴雪 0.90），~1.2s 时间常数渐变
 * - prefers-reduced-motion 静止
 * - 布点确定性 mulberry32('clouds-v1')
 * - 天空不参与「选中」（点空白 = 不选中），objectInfo id = sky.cloud
 *
 * 契约：lag_docs/虚拟城市/已实现/58-云层真实感/01-方案与实施记录.md
 */

import { useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard } from '@react-three/drei';
import { skyUrl } from '@/assets/images/virtualCity';
import { useSharedTexture } from '@/engine3d';
import { sample, weatherVisual } from '../cityTimeStore';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/**
 * 云 opacity 基准。
 *
 * 批次 27 定的是 **0.35**，标定对象是批次 16 的「道具云」（单片宽 2.2~3.6 世界
 * 单位 = 22~36 m、张角 2.5°）。批次 58 把云改成天空尺度（400~1100 m、张角
 * 12°~20°）后，0.35 这个基准**低了约 2.7 倍**：
 *   贴图自身 alpha 峰值 0.90、均值 0.36 ⇒ 材质 0.35 时有效逐像素 alpha 仅
 *   0.14（峰值）/ 0.13（均值）。CDP 实测算得：正午蓝天背景 [85,137,196]，
 *   云区混色后 [108,152,203]，**只亮 15/255** —— 41 个云网格全部在画面内，
 *   却与蓝天几乎无差（"亮于背景 +22" 的像素占比 0.00%）。
 * 真实积云是会**遮蔽**天空的，云体峰值有效 alpha 应在 0.7~0.9。
 * 故基准提到 0.92：让贴图自带的 alpha 结构（薄处透、中心厚）如实呈现。
 */
const CLOUD_BASE_TEX = 0.92;
/** 兜底白球无贴图 alpha 结构兜着，需要更高基准才不至于看不见。 */
const CLOUD_BASE_FALLBACK = 0.30;

/**
 * 天气系数下限。`cityTimeStore.ts` 的 cloudOpacity 表跨度 0.10（晴）~0.90（暴雪），
 * 是批次 27 为「点缀性小云」设计的：晴天就该几乎看不见云。但云放大成天空主体后，
 * 晴天取 0.10 会让整片天空重新变成空白色 —— 而真实晴天仍有疏散的淡积云。
 * 这里对系数取下限 0.35（晴天仍是「淡」，暴雨仍是 0.90 的「厚」，
 * 相对关系与批次 27 的意图一致）。
 */
const WEATHER_FACTOR_FLOOR = 0.35;

/** 批次 58：沿 +x 漂移的回绕边界（世界单位）。须 ≥ 云层水平铺开半径，
 *  否则回绕线落在云体内部 ⇒ 云被拦腰折断（见 Cloud 组件 useFrame）。
 *  取 340 = 铺开半径上界 330 + 一点余量；漂移 0.15~0.3 单位/s，
 *  一次跨越需 ~38 min，正常对局内基本不会触发回绕。 */
const CLOUD_WRAP_X = 340;

/** 云型（§27.0-1 调研三种真实云型）。 */
type CloudKind = 'cumulus' | 'stratus' | 'cirrus';

/** 确定性伪随机（同源 StreetPropsLayer）。 */
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** 昼夜云色（§1 调研表）。线性 RGB 关键色 × 全局 tint。
 *  - 正午 (tod≈0.5)：云自身反照率最高，近纯白微冷
 *  - 黄昏 (tod≈0.25/0.75)：低角度太阳的米氏前向散射 ⇒ 整片云泛橙红
 *  - 夜间 (tod≈0/1)：无日光，仅余城市光污染把云底染成暗橙灰
 *  注意：贴图 RGB 已是云的**固有色**（积云顶 255 / 底 171-188），
 *  这里只做色调调制，幅度刻意保守（最高 1.0，最暗 0.16）。 */
const CLOUD_TINT = {
  noon: new THREE.Color('#f2f6fb'),   // 近白微冷
  afternoon: new THREE.Color('#eef2f7'), // 午后略暖白
  dusk: new THREE.Color('#e8955c'),    // 日落橙
  duskDeep: new THREE.Color('#b45f45'), // 日落后余晖红
  night: new THREE.Color('#2a3040'),   // 夜云暗蓝
  nightCity: new THREE.Color('#3d3a44'), // 夜云被城市光污染染暖
} as const;

/**
 * 按城市当地时刻返回云的染色（批次 58）。
 * 时刻轴用 timeOfDay01（0=午夜 0.5=正午），返回 [color, brightness]。
 * brightness 夜间显著压低——真实夜空里云几乎不可见，只比背景略亮。
 */
function cloudTintFor(timeOfDay01: number, sunrise01: number, sunset01: number): [THREE.Color, number] {
  // 白昼弧：日出 → 日落之间为白昼
  const isDay = timeOfDay01 >= sunrise01 && timeOfDay01 <= sunset01;
  if (isDay) {
    /**
     * ⚠️ 这里**不能**用 `|tod - 0.5| * 2` 当「距正午多远」——
     * 那只对「昼弧恰好是 0.25~0.75」成立，且它的量程是「距**午夜**多远」：
     * 日落（tod=0.75）时该值只有 0.5，永远够不到 0.72/0.88 的金色阈值
     * ⇒ `#e8955c` 是**死代码**（批次 58 离线断言实测：18:00 前仍返回
     * `#f2f6fb`，18:30 直接跳到夜云色，金色时段从未出现）。
     *
     * 正解：按**真实昼弧**归一化 —— 以太阳正午为 0、日出/日落为 1。
     * 这样也自动适配不同纬度的昼长（批次 33 真实城市档案里日出日落逐月不同）。
     */
    const solarNoon = (sunrise01 + sunset01) / 2;
    const halfDay = Math.max(1e-6, (sunset01 - sunrise01) / 2);
    const dayFrac = Math.abs(timeOfDay01 - solarNoon) / halfDay; // 0=正午 1=日出/日落
    if (dayFrac < 0.72) return [CLOUD_TINT.noon, 1.0];
    if (dayFrac < 0.88) return [CLOUD_TINT.afternoon, 0.94];
    // 日出/日落前约最后一刻：金色时段
    return [CLOUD_TINT.dusk, 0.8];
  }
  // 夜间：区分「刚入夜（城市光污染可见暗云）」与「深夜（几乎不可见）」。
  // 同样按**真实夜弧**归一化，不用写死的 0.15。
  const nightArc = 1 - Math.max(0, sunset01 - sunrise01);
  const halfNight = Math.max(1e-6, nightArc / 2);
  const afterSunset = timeOfDay01 > sunset01;
  const nightDepth = Math.min(1, afterSunset
    ? (timeOfDay01 - sunset01) / halfNight
    : (sunrise01 + 1 - timeOfDay01) / halfNight);
  // nightDepth 0=刚入夜 → 城市光污染可见；1=深夜 → 几乎不可见
  if (nightDepth < 0.5) {
    return [CLOUD_TINT.nightCity, 0.22];
  }
  return [CLOUD_TINT.night, 0.16 * (1 - nightDepth * 0.5)];
}

interface CloudSpec {
  x: number;
  y: number;
  z: number;
  speed: number;
  kind: CloudKind;
  /** 每片云朵：偏移 + 尺寸。 */
  puffs: Array<{ dx: number; dy: number; dz: number; w: number; h: number }>;
}

/**
 * 云层布点：**天空尺度**（批次 58 修正的核心）。
 *
 * ⚠️ 批次 58 实测推翻了批次 16 的两处「看起来能跑」的尺度：
 *
 * (1) **旧版云是道具尺度，不是天空尺度。** 旧 puff `w=2.2~3.6` 世界单位
 *     （1 单位 = 10 m ⇒ 22~36 m），布在 y=60~80、距相机约 68 单位 ⇒
 *     视角张角 `arctan(30/680) ≈ 2.5°`。真实积云 500~2000 m 宽、1~3 km 外，
 *     张角 10°~45°。**差一个数量级** ⇒ 街景抬头只见几缕若有若无的灰痕，
 *     俯瞰只见几个小白点。CDP 实测：正午天空「亮于背景+25」的像素仅占
 *     上半屏 1.2%，且把 opacity 拉到完全不透明（×6.3）也只升到 1.44%
 *     —— 说明问题不在透明度而在**张角**。
 *
 * (2) **云层顶低于相机上限。** 批次 16 的注释写「相机最高点 ≈45」，那是
 *     OrbitControls(maxDistance=80, maxPolar=1.2) 时代的上界；批次 32 引入
 *     自由视角后 `FREE_VIEW_BOUNDS.max[1] = 140`，**相机能飞到 y=140 而云只有
 *     60~80 ⇒ 可以飞到云层之上**，物理上错误、视觉上穿帮。此注释与
 *     `cloudsFor` 高度自批次 16 起从未与 `freeViewColliders.ts` 对账。
 *
 * 本版按「张角正确」反解尺度：
 *   - 街景相机 y≈3（30 m）抬头看积云：距离 ≈194 单位（1.94 km），
 *     要占 12°~20° ⇒ 宽度 40~70 单位（400~700 m）。
 *   - 云底一律高于相机上限 140：积云 175~215、层云 240~280、卷云 320~370。
 *   - 云层最远点 sqrt(88²+88²+370²) ≈ 391 < 天穹半径 500 < 相机 far 800
 *     ⇒ 既不被天穹遮挡，也不被远裁面切掉。
 *
 * 2026-09-22 视觉验收三轮修正（16/05 验收清单 §3.4 回归项）中仍然有效的两条：
 *  (1) 俯瞰时 0.85 大板把城市盖白 → opacity 降档；
 *  (2) 云不得插进「相机↔城市」视线 ⇒ 云底恒高于相机上限（已由上表保证）。
 */
function cloudsFor(seedStr: string): CloudSpec[] {
  let h = 2166136261;
  for (let i = 0; i < seedStr.length; i++) {
    h ^= seedStr.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  const rnd = mulberry32(h >>> 0);
  // 云型配方：多数积云（晴天天空主角），高层云作底幕，高空卷云点缀。
  const kinds: CloudKind[] = [
    'cumulus', 'cumulus', 'stratus', 'cumulus', 'cirrus', 'cumulus',
    'stratus', 'cirrus', 'cumulus', 'cirrus', 'cumulus', 'stratus',
    'cumulus', 'cirrus',
  ];
  const out: CloudSpec[] = [];
  for (let c = 0; c < kinds.length; c++) {
    const ang = (c / kinds.length) * Math.PI * 2 + rnd() * 0.7;
    /**
     * 水平散布半径 30~330。
     *
     * ⚠️ 批次 58 第二处被实测推翻的尺度：原先铺在 25~70（城市正上方一小片），
     * 于是**任何街景位置看云都在近乎天顶**（仰角 atan(190/70) ≈ 70°）。
     * 而 drei `<Billboard>` 的朝向由**应用相机**决定（不是诊断相机）——
     * 从下方/侧后方看过去就是背面，`side: FrontSide` 直接背面剔除 ⇒ 云「消失」。
     * 实测症状极具迷惑性：云网格全部在视锥内、尺寸正确、贴图正常、
     * 连「强制纯红不透明 + 关闭视锥剔除」都渲染出 **0 像素**，
     * 而同一场景里新塞的裸 mesh 正常显示 —— 差点误判成渲染管线坏了。
     *
     * 真实云层一直延伸到**地平线**：铺开到 330 后，水平距离 300、层高 190 的
     * 云仰角 atan(190/300) ≈ 32°，正是人抬头能看到的自然云高。
     * 上界 330 的约束：与天穹半径 500（sqrt(330²+200²)≈386 < 500）及
     * 相机 far=800 同时相容。
     */
    const rad = 30 + rnd() * 300;
    const kind = kinds[c];
    const puffCount =
      kind === 'stratus' ? 3 + Math.floor(rnd() * 2)
      : kind === 'cirrus' ? 4 + Math.floor(rnd() * 2)
      : 5 + Math.floor(rnd() * 4);
    // 主体尺度（世界单位 = 10 m）：积云 400~700 m 宽，层云 700~1100 m，卷云 500~900 m
    const bodyW =
      kind === 'stratus' ? 70 + rnd() * 40
      : kind === 'cirrus' ? 50 + rnd() * 40
      : 40 + rnd() * 30;
    const bodyH =
      kind === 'stratus' ? 18 + rnd() * 8
      : kind === 'cirrus' ? 10 + rnd() * 6
      : 24 + rnd() * 16;
    const puffs = Array.from({ length: puffCount }, (_, p) => {
      // 层间横向铺开：间距取 ~0.5×单片宽 ⇒ 相邻层显著重叠（真实云是连续体）
      const spread = (p - (puffCount - 1) / 2) * bodyW * 0.5;
      return {
        dx: spread * (kind === 'cirrus' ? 1.15 : 1.0),
        // 垂向抖动：层云几乎不抖（层状），积云抖得多（有 cauliflower 起伏）
        dy: (rnd() - 0.5) * bodyH * (kind === 'stratus' ? 0.25 : 0.55),
        // 深度错位 —— 批次 58 的体积感来源：层间视差消解「纸片感」
        dz: (rnd() - 0.5) * bodyW * 0.18,
        w: bodyW * (0.62 + rnd() * 0.3),
        h: bodyH * (0.7 + rnd() * 0.4),
      };
    });
    out.push({
      x: Math.cos(ang) * rad,
      // 对流层分层：积云底 ~2 km / 层云 ~2.5 km / 卷冰云 ~3.5 km（世界单位 ×10）
      y: kind === 'cumulus' ? 175 + rnd() * 40
        : kind === 'stratus' ? 240 + rnd() * 40
        : 320 + rnd() * 50,
      z: Math.sin(ang) * rad,
      speed: 0.15 + rnd() * 0.15,
      kind,
      puffs,
    });
  }
  return out;
}

/** 单团云（贴图 billboard 或白色扁球兜底；材质共享于本团 puff，opacity 随天气渐变）。 */
function Cloud({ spec, tex }: { spec: CloudSpec; tex: THREE.Texture | null }) {
  const groupRef = useRef<THREE.Group>(null);
  // 批次 27：一团一材质（puff 共享），useFrame 只改 opacity —— 免逐 puff 材质 ref 收集。
  // 批次 58：材质 color 承载昼夜染色（贴图是云固有色，不随时刻改写）。
  const material = useMemo(() => {
    const m = new THREE.MeshBasicMaterial({
      transparent: true,
      depthWrite: false,
      fog: false,
    });
    if (tex) {
      m.map = tex;
      m.alphaTest = 0.01;
      m.opacity = CLOUD_BASE_TEX;
    } else {
      m.color = new THREE.Color('#f4f6f9');
      m.opacity = CLOUD_BASE_FALLBACK;
    }
    return m;
  }, [tex]);
  useEffect(() => () => material.dispose(), [material]);
  const baseOpacity = tex ? CLOUD_BASE_TEX : CLOUD_BASE_FALLBACK;

  useFrame((_state, delta) => {
    const g = groupRef.current;
    if (g && !REDUCED_MOTION) {
      g.position.x += spec.speed * delta;
      // 批次 58：回绕边界随云体尺度放大。旧值 ±42 是按 2~3 单位的「道具云」
      // 定的；云放大到 40~110 单位后 ±42 会在云体**内部**回绕 ⇒ 看到云被
      // 拦腰折断。改为 ±110（> 最大云宽的一半 + 布点半径，留出无缝衔接）。
      if (g.position.x > CLOUD_WRAP_X) g.position.x = -CLOUD_WRAP_X;
    }
    const s = sample();
    // 批次 27 契约：云量 × 天气系数（§3.3 镜像表；无天气数据 = 1）。
    // 批次 58：系数取下限 WEATHER_FACTOR_FLOOR（晴天不再把天空清空）。
    const weatherFactor = Math.max(
      WEATHER_FACTOR_FLOOR,
      weatherVisual(s.weather).cloudOpacity,
    );
    // 批次 58：昼夜染色 + 夜间可见度压制。
    const [tint, brightness] = cloudTintFor(s.timeOfDay01, s.sunrise01, s.sunset01);
    // 单一目标值 = 基准 × 天气 × 昼夜亮度，统一用 ~1.2s 时间常数指数趋近。
    const targetOpacity = baseOpacity * weatherFactor * brightness;
    material.opacity += (targetOpacity - material.opacity) * Math.min(1, delta * 0.8);
    // 颜色渐变更快（~0.5s），避免日落瞬间云色硬跳。
    material.color.lerp(tint, Math.min(1, delta * 2.0));
  });
  return (
    <group ref={groupRef} position={[spec.x, spec.y, spec.z]}>
      {spec.puffs.map((p, i) =>
        tex ? (
          <Billboard key={`puff-${i}`} position={[p.dx, p.dy, p.dz]}>
            <mesh material={material}>
              <planeGeometry args={[p.w, p.h]} />
            </mesh>
          </Billboard>
        ) : (
          <mesh
            key={`puff-${i}`}
            material={material}
            position={[p.dx, p.dy, p.dz]}
            scale={[p.w * 0.4, p.h * 0.4, p.w * 0.4]}
          >
            <sphereGeometry args={[1, 8, 6]} />
          </mesh>
        ),
      )}
    </group>
  );
}

export function CloudLayer() {
  // 贴图按云型加载（批次 58：三型共存）；任一缺失回落兜底。
  const puffTex = useSharedTexture(skyUrl('cloud_puff'));
  const layerTex = useSharedTexture(skyUrl('cloud_layer'));
  const cirrusTex = useSharedTexture(skyUrl('cloud_cirrus'));
  const texByKind: Record<CloudKind, THREE.Texture | null> = useMemo(
    () => ({
      cumulus: puffTex ?? null,
      stratus: layerTex ?? null,
      cirrus: cirrusTex ?? null,
    }),
    [puffTex, layerTex, cirrusTex],
  );
  const clouds = useMemo(() => cloudsFor('clouds-v1'), []);
  // 批次 28 B2：云层信息交互（根组承接子云事件冒泡）。
  // 批次 32 v2：天空不参与「选中」（点空白 = 不选中）。
  const info = useObjectInfoProps('sky.cloud', { anchorY: 0, selectDisabled: true });
  return (
    <group {...info}>
      {clouds.map((c, i) => (
        <Cloud key={`cloud-${i}`} spec={c} tex={texByKind[c.kind]} />
      ))}
    </group>
  );
}

export default CloudLayer;
