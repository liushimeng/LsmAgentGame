/**
 * engine3d/glbSizeGuard — GLB 尺寸 / 落地 / 变换规约校验器（批次 29 新增防回归护栏）。
 *
 * 动机（真实事故，均已上线且长期无人发现）：
 *   - 批次 19 的 11 个 GLB 以旧「玩具尺度」+ 错误轴向导出 → 轿车长 1.04 m（应 4.6 m）、
 *     垃圾桶 1.11 m 高**比车还大**（用户可见反馈）、行人横躺成 8.6 m「原木」、5 个市政
 *     建筑与路灯全部侧躺；
 *   - 批次 26 的雪山一度按 10× 尺度上线；
 *   - **尺寸挂在节点变换上**（最阴的一条路）：trash_can.glb 曾以「单位尺度几何 +
 *     节点 scale [0.0375,0.09,0.0375]」导出 —— `Box3.setFromObject` 量出来"完全正常"，
 *     而 `props/RoadsideBins.tsx` 的 `collectPairs()` 用 `rootInv × mesh.matrixWorld`
 *     恰好**抵消掉变体根节点的 scale**，于是 instancedMesh 按单位几何渲染 ⇒ 全城
 *     每只桶 ≈20 m 直径 × 10 m 高。
 *   根因不是"没测试"，而是 GLB 的**单位口径 / 轴向 / 原点 / 变换规矩**只写在实施记录里，
 *   没有可执行的规约 —— 消费端只能"猜"美术产出，错了也没人报。
 *
 * 本模块补这个洞：**GLB 载入后立刻量三样东西**并比对调用方注册的目标尺寸
 * （⑤/⑥ 为本批（遗留 L11）新增的**轴向**断言，见下方判据对照表）：
 *   ① 量测包围盒（= **被测节点局部系**下的 `Box3`，逐 mesh 用相对矩阵 `rootInv × matrixWorld`
 *      烘焙）→ 尺寸 + 落地（minY）。**刻意不用 `Box3.setFromObject(sceneRoot)`**：
 *      后者含变体根节点自身的变换，而 `RoadsideBins.collectPairs()` 正是用 rootInv 把它
 *      抵消掉的 —— 局部系口径才等于"消费端最不设防的所见"（trash_can 旧版在局部系下
 *      是 20 m × 10 m 的怪物，而在 setFromObject 口径下"看着正常"）。
 *   ② 几何级包围盒（顶点自带尺寸，逐 mesh 剥掉节点 scale）→ 与 ① 不一致即
 *      "尺寸挂在**子树内部**节点变换上"；
 *   ③ 节点变换规约（scale 必须 1、rotation 必须 0）→ 批次 26 契约「`<Model>` 零旋转
 *      零 scale 直挂」的可执行版本；被测节点**自身**的 scale/rotation 由此条兜住
 *      （rootInv 会把它抵消，①② 都不足以单独发现）。
 *   ③ 是拦得住上述"最阴一条路"的关键判据：只量世界包围盒会把它判为"通过"。
 *
 * 三条契约：
 *   1. GLB 自带正确尺寸 / 轴向 / 原点，**顶点即最终尺寸**（不得用节点 scale 承载），
 *      消费端零旋转零 scale 直挂；
 *   2. 校验器**只告警，绝不修改几何** —— 自动缩放会掩盖美术错误，而"尺寸静默漂移"
 *      正是本护栏要根除的失效模式；
 *   3. 只在开发态（`import.meta.env.DEV`）生效：生产构建里注册与校验都被静态折叠，
 *      零运行时开销、零 console 噪音。
 *
 * 引擎层中立（CLAUDE.md §2.1 硬约束 5）：本文件只依赖 three，尺寸目标由调用方注入
 * （游戏侧从自己的尺寸表取，如 `components/virtualCity/cityScale.ts` 的
 * `REAL_DIMS_M` → `sizeTargetFor()`）；engine3d 不得反向 import 游戏私有模块。
 *
 *   ⑤ 直立（`upright`）：声明「竖直轴 = +Y」⇒ 要求 Y 为主导轴（抓"模型侧躺"）；
 *   ⑥ 向前轴（`forward`）：声明「水平长轴 = 消费端 yaw 公式假设的向前轴」（抓"横着
 *      开 90°"）—— 把"资产轴向 ↔ 消费端 yaw 公式"这条原本只在注释里的契约变成断言。
 *
 * 使用（游戏侧）：
 *   const TARGET = sizeTargetFor('sedan', { label: 'vehicles/sedan' });   // 模块级常量，引用稳定
 *   useSharedGLTF(url, TARGET);                                 // Vehicle / PedestrianV3
 *   <Model url={url} sizeTarget={TARGET}>…</Model>              // civic / TreeV3
 *   <GlbInstanced url={url} sizeTarget={TARGET} … />            // 边缘带实例化
 *   `upright` / `forward` 由 `cityScale.ts::MODEL_AXIS_CONVENTIONS` 按表键声明（也可经
 *   `sizeTargetFor(key, { upright, forward })` 逐调用点覆盖）。
 *
 * 已知边界（不影响本护栏用途，勿据此放宽判定）：
 *   - ⑤/⑥（直立 / 向前轴）是**尺寸主导性**判定（离散、无容差），只在物件"确实沿该轴
 *     拉长"时才有判别力，因此**只对能确证的物件声明**：
 *       · 扁平物件（车辆 长>宽>高、低层站房 宽>高）主导轴**本来就不是 Y**，且它们
 *         本就该扁平 ⇒ **不得声明 `upright`**：该断言语义下会把"正确"判成"侧躺"
 *         （轿车 4.6×1.45×1.82 中 Y 最小），而"车尾立起"（长轴竖直）反倒能通过 ——
 *         方向是反的。此类物件的轴向错误由 ① 兜住（三轴目标互异 ⇒ 任意 90° 置换
 *         都会让某一轴偏差超容差）；
 *       · `forward` 同理是"水平长轴"判定，只对**沿行进轴拉长**的物件有效（车辆）；
 *         行人宽 0.55 > 深 0.35 并不沿行进轴拉长，其" +Z 向前"约定无法由包围盒自证
 *         ⇒ 不声明（消费端 yaw 公式是该约定的唯一载体，见 PedestrianV3.tsx）。
 *   - 蒙皮网格（行人 walk）量测的是**绑定姿态**包围盒，非动画任意帧的姿态；
 *   - `Bone`（蒙皮关节）与**被动画剪辑驱动的节点**（`animatedNodeNames(animations)`）
 *     允许 rotation —— 前者是 rig 组成，后者姿态由 clip 决定（行人四肢的 π 翻转下垂）；
 *     但它们的 **scale 仍须为 1**；
 *   - **位移永远合法**（美术用它摆放部件/变体，不改变尺寸；变体内容的平移由消费端
 *     按包围盒归一化，见 RoadsideBins.variantPivotOffset）；仅 GLB 场景**根节点**
 *     额外要求位移为 0；
 *   - 多物件合成 GLB（如 road/trash_can 含 Green/Blue 两变体）用 `measureNode`
 *     指定单个变体节点量测，否则量到的是两变体的并集（GLB 内 Green 居中、
 *     Blue 平移摆放，场景盒比单桶宽 3.4 倍）。
 *
 * 判据对照（①②③④ 已在批次 29 用合成场景逐条验证；⑤⑥ 为本批新增，见下）：
 *   | 场景                                    | 命中判据 |
 *   | 单位几何 + 变体根 scale 0.0375（旧桶）   | ①(20 m/0.5 m=40×) + ③ |
 *   | 子节点 scale 承载尺寸                    | ② + ③ |
 *   | 变体根 rotation 90°（横躺）              | ③ |
 *   | 尺寸烘焙进顶点 + 子节点位移（合法）      | 无（静默） |
 *   | 变体根位移（合法，消费端已归一化）       | 无（静默） |
 *   | 合成：顶点级烘焙 90°（模型横躺）+ upright | ⑤ |
 *   | 声明 forward 与实测水平长轴不符          | ⑥ |
 *
 * ⑤/⑥ 的动机（遗留 L11，批次 29 真实漏网缺陷）：批次 29 修正前 `Vehicle.tsx` 的 yaw 取
 * `atan2(dz, dx)` 令**全城 50 辆车横着开 90°**；它逃过护栏的原因是该错在**消费端 yaw
 * 公式**（GLB 与程序化 fallback 一致地错）⇒ "两路径包围盒一致"这条不变量**反而掩盖**了它。
 * ⑤/⑥ 把"资产轴向 ↔ 消费端 yaw 公式"这条**只在代码注释里存在的契约**变成可执行断言：
 * 声明 `forward: 'x'` 即声明"消费端用 atan2(-dz, dx)（+X 向前）"，资产一旦被改成沿 Z 拉长
 * 就会立刻报出"会被摆成与行进方向垂直 90°"。
 */

import * as THREE from 'three';

/** 尺寸校验目标（世界单位；字段缺省 = 不校验该轴向 / 不校验落地）。 */
export interface ModelSizeTarget {
  /** X 向期望尺寸（世界单位）。 */
  x?: number;
  /** Y 向期望尺寸（世界单位）。 */
  y?: number;
  /** Z 向期望尺寸（世界单位）。 */
  z?: number;
  /**
   * 期望包围盒底面 y（世界单位）：`0` = 原点落在物件底面（轮底 / 脚底 / 桶底 / 建筑基座），
   * 即消费端零补偿直挂时自动贴地。缺省 = 不做落地校验（水面 / 地形类另有约定）。
   */
  minY?: number;
  /** 量测子节点名（合成 GLB 指定单变体；缺省量测整棵场景）。 */
  measureNode?: string;
  /** 尺寸相对容差（默认 0.05 = ±5%）。 */
  tol?: number;
  /** 日志标签（模型识别名，如 `civic/city_hall`）。 */
  label: string;
  /**
   * 直立约定（可选，缺省 = 不校验）：声明「该模型竖直轴 = +Y」。
   * 判据 ⑤：要求 Y 为**主导轴**（Y 尺寸 > X 尺寸 且 Y 尺寸 > Z 尺寸，严格、无容差）。
   * 违反的典型症状 = 模型**侧躺**（批次 19 那批把高度摆在 Z 上）。
   *
   * ⚠️ **适用前提（硬约束）**：`upright` 仅用于**高 > 长/宽**的**竖向物件**
   * （塔、楼、树、行人、桶）；**扁平物件（车辆 长>宽>高、低层站房 宽>高）不得声明** ——
   * 它们本来 Y 最小，声明即反向误报，而"车尾立起"（长轴竖直）反倒能通过（方向是反的）；
   * 这类物件的轴向错误由 ① 兜住（三轴目标两两互异 ⇒ 任意 90° 轴置换都超 ±5%）。
   * 声明与表值矛盾（表值 y 非最大却声明 upright）由游戏侧的**声明层自校验**在注册时拦下
   * （见 `cityScale.ts::axisConventionIssue`），无需载入模型。
   */
  upright?: boolean;
  /**
   * 向前轴约定（可选，缺省 = 不校验）：声明「该模型在水平面内的**长轴** = 消费端 yaw
   * 公式所假设的向前轴」。判据 ⑥：`'x'` ⇒ X 尺寸 > Z 尺寸；`'z'` ⇒ Z 尺寸 > X 尺寸。
   * 违反 ⇒ 模型会被摆成与行进方向**垂直 90°**（消费端 yaw 公式见下方告警文案）。
   *
   * ⚠️ **适用前提**：只对**沿行进方向拉长**的物件声明（车辆）；行人等不沿行进轴拉长的
   * 物件无法由包围盒自证"朝向"（其 +Z 向前由消费端 yaw 公式承载）⇒ 不得声明。
   */
  forward?: 'x' | 'z';
}

const DEV = import.meta.env.DEV;
/** 默认尺寸容差 ±5%。 */
const DEFAULT_TOL = 0.05;
/** minY 绝对容差（世界单位）= 0.2 m。 */
const MINY_TOL = 0.02;
/** 节点变换"是否 identity"的判定阈值（scale 偏移 / rotation 弧度 / 位移）。 */
const TRS_EPS = 1e-4;
const UNIT_SCALE = new THREE.Vector3(1, 1, 1);

/** url → 目标尺寸（调用方注册；dev 专用）。 */
const TARGETS = new Map<string, ModelSizeTarget>();

/** 注册某 url 的目标尺寸（幂等覆写；生产构建 no-op）。 */
export function registerModelSizeTarget(url: string, target: ModelSizeTarget): void {
  if (!DEV || !url) return;
  TARGETS.set(url, target);
}

/** 已注册目标（测试 / 调试读取）。 */
export function registeredModelSizeTargets(): ReadonlyMap<string, ModelSizeTarget> {
  return TARGETS;
}

/** 清空注册表（测试 / 热更新用）。 */
export function clearModelSizeTargets(): void {
  TARGETS.clear();
}

/**
 * 量测子树包围盒，返回**目标节点自身坐标系**下的 Box3
 * （GLB 根节点按契约恒为 identity ⇒ 即世界包围盒；指定 measureNode 时排除该节点
 * 自身位移，使 minY 语义恒为「原点相对物件底面」）。
 * 无可量测几何（空 GLB / 节点缺失）返回 null。
 */
export function measureModelBox(
  root: THREE.Object3D,
  measureNode?: string,
): THREE.Box3 | null {
  const node = (measureNode ? root.getObjectByName(measureNode) : null) ?? root;
  if (!node) return null;
  node.updateWorldMatrix(true, true);
  const inv = new THREE.Matrix4().copy(node.matrixWorld).invert();
  const local = new THREE.Matrix4();
  const tmp = new THREE.Box3();
  const box = new THREE.Box3();
  let found = false;
  node.traverse((o) => {
    const mesh = o as THREE.Mesh;
    if (!(mesh as unknown as { isMesh?: boolean }).isMesh) return;
    const geo = mesh.geometry;
    if (!geo) return;
    if (!geo.boundingBox) geo.computeBoundingBox();
    const bb = geo.boundingBox;
    if (!bb) return;
    local.multiplyMatrices(inv, o.matrixWorld);
    box.union(tmp.copy(bb).applyMatrix4(local));
    found = true;
  });
  return found ? box : null;
}

/** 量测结果（世界单位）。 */
export interface ModelSizeReport {
  /** 各轴尺寸。 */
  size: THREE.Vector3;
  min: THREE.Vector3;
  max: THREE.Vector3;
}

/** 量测（对外只读接口；`null` = 无可量测几何）。 */
export function measureModelSize(root: THREE.Object3D, measureNode?: string): ModelSizeReport | null {
  const box = measureModelBox(root, measureNode);
  if (!box) return null;
  return {
    size: box.getSize(new THREE.Vector3()),
    min: box.min.clone(),
    max: box.max.clone(),
  };
}

/**
 * 三口径量测（批次 29 判据本体）：
 *   - `world*`：含全部节点变换（= 消费端所见，`Box3` 口径）；
 *   - `geomSize`：**顶点自带尺寸**（逐 mesh 剥掉节点 scale 后并集）；
 *   - `transformIssues`：非 identity 变位的节点（scale ≠ 1 或 rotation ≠ 0；
 *     `Bone` 除外 —— 骨骼位移/翻转是蒙皮 rig 的正常组成）。
 */
export interface ModelSizeProbe {
  ok: boolean;
  /**
   * 量测尺寸：**被测节点局部系**下的包围盒（GLB 根节点按契约恒 identity ⇒ 量测根时
   * 即世界盒）。等于消费端 `rootInv × matrixWorld` 重算后的所见，见文件头 ①。
   */
  worldSize: THREE.Vector3;
  worldMin: THREE.Vector3;
  worldMax: THREE.Vector3;
  /** 顶点自带尺寸（剥掉节点 scale；rotation 保留，因为它不改变"几何能否自证尺寸"）。 */
  geomSize: THREE.Vector3;
  /** 非 identity 的节点描述（如 `TrashCan_Green S=0.04,0.09,0.04`）。 */
  transformIssues: string[];
  /** 量测节点是否为 GLB 场景根（根节点另有"位移必须为 0"的契约）。 */
  measuredRoot: boolean;
}

const isBone = (o: THREE.Object3D): boolean =>
  (o as unknown as { isBone?: boolean }).isBone === true;

/**
 * 节点变换规约：scale 必须 1、rotation 必须 0。
 *   - 位移**允许**：美术用它摆放部件/变体（不改变尺寸），消费端按内容包围盒归一化；
 *   - `Bone`（蒙皮关节）与**被动画驱动的节点**（`animated`）允许 rotation ——
 *     前者是 rig 组成，后者的姿态由 clip 决定（如行人四肢的 π 翻转下垂）；
 *     但它们的 **scale 仍须为 1**（用缩放承载尺寸就是本护栏要抓的失效模式）。
 */
function nodeTransformIssue(o: THREE.Object3D, animated: ReadonlySet<string>): string | null {
  const s = o.scale;
  const r = o.rotation;
  const scaleBad =
    Math.abs(s.x - 1) > TRS_EPS || Math.abs(s.y - 1) > TRS_EPS || Math.abs(s.z - 1) > TRS_EPS;
  const rotBad =
    Math.abs(r.x) > TRS_EPS || Math.abs(r.y) > TRS_EPS || Math.abs(r.z) > TRS_EPS;
  const rotExempt = isBone(o) || (o.name !== '' && animated.has(o.name));
  if (!scaleBad && !(rotBad && !rotExempt)) return null;
  const v = (n: number) => n.toFixed(4);
  return (
    `${o.name || o.type} ` +
    (scaleBad ? `S=[${v(s.x)},${v(s.y)},${v(s.z)}] ` : '') +
    (rotBad && !rotExempt ? `R=[${v(r.x)},${v(r.y)},${v(r.z)}]` : '')
  ).trim();
}

/** 从动画剪辑提取被驱动的节点名（rotation 检查的豁免集）。 */
export function animatedNodeNames(animations: readonly THREE.AnimationClip[]): Set<string> {
  const out = new Set<string>();
  for (const clip of animations) {
    for (const track of clip.tracks) {
      const name = track.name.split('.')[0];
      if (name) out.add(name);
    }
  }
  return out;
}

/** 三口径量测（`null` = 无可量测几何）。 */
export function probeModelSize(
  root: THREE.Object3D,
  measureNode?: string,
  animated: ReadonlySet<string> = new Set<string>(),
): ModelSizeProbe | null {
  const node = (measureNode ? root.getObjectByName(measureNode) : null) ?? root;
  if (!node) return null;
  node.updateWorldMatrix(true, true);
  const inv = new THREE.Matrix4().copy(node.matrixWorld).invert();
  const rel = new THREE.Matrix4();
  const geomMatrix = new THREE.Matrix4();
  const pos = new THREE.Vector3();
  const quat = new THREE.Quaternion();
  const scale = new THREE.Vector3();
  const worldBox = new THREE.Box3();
  const geomBox = new THREE.Box3();
  const tmp = new THREE.Box3();
  const transformIssues: string[] = [];
  let found = false;
  node.traverse((o) => {
    const issue = nodeTransformIssue(o, animated);
    if (issue) transformIssues.push(issue);
    const mesh = o as THREE.Mesh;
    if (!(mesh as unknown as { isMesh?: boolean }).isMesh) return;
    const geo = mesh.geometry;
    if (!geo) return;
    if (!geo.boundingBox) geo.computeBoundingBox();
    const bb = geo.boundingBox;
    if (!bb) return;
    // 相对量测节点的局部矩阵（根节点的自身变换按契约恒为 identity）
    rel.multiplyMatrices(inv, o.matrixWorld);
    worldBox.union(tmp.copy(bb).applyMatrix4(rel));
    // 几何级：剥掉 scale（保留 rotation / 位移）
    rel.decompose(pos, quat, scale);
    geomMatrix.compose(pos, quat, UNIT_SCALE);
    geomBox.union(tmp.copy(bb).applyMatrix4(geomMatrix));
    found = true;
  });
  if (!found) return null;
  const measuredRoot = !measureNode || node === root;
  if (measuredRoot && root.position.lengthSq() > TRS_EPS * TRS_EPS) {
    const p = root.position;
    transformIssues.push(`GLB 根节点位移非 0：T=[${p.x.toFixed(4)},${p.y.toFixed(4)},${p.z.toFixed(4)}]`);
  }
  return {
    ok: true,
    worldSize: worldBox.getSize(new THREE.Vector3()),
    worldMin: worldBox.min.clone(),
    worldMax: worldBox.max.clone(),
    geomSize: geomBox.getSize(new THREE.Vector3()),
    transformIssues,
    measuredRoot,
  };
}

const AXES = ['x', 'y', 'z'] as const;
const f = (v: number): string => (Math.abs(v) < 0.0005 ? '0.000' : v.toFixed(3));
const fmtVec = (v: THREE.Vector3): string => `[${f(v.x)},${f(v.y)},${f(v.z)}]`;

type Axis = (typeof AXES)[number];

/**
 * 主导轴（尺寸最大的轴；并列 ⇒ 返回 `'tie'`）。⑤/⑥ 的判定基元。
 * 刻意**无容差**：这是"谁最大"的离散判定，不是尺寸测量 —— 容差留给 ①。
 */
function dominantAxis(size: THREE.Vector3): Axis | 'tie' {
  const max = Math.max(size.x, size.y, size.z);
  const hits = AXES.filter((ax) => size[ax] === max);
  return hits.length === 1 ? hits[0] : 'tie';
}

/** 水平面（XZ）内的长轴（严格比较；并列 ⇒ `'tie'`）。 */
function horizontalLongAxis(size: THREE.Vector3): Axis | 'tie' {
  if (size.x === size.z) return 'tie';
  return size.x > size.z ? 'x' : 'z';
}

/** 轴名大写（日志用）。 */
const up = (a: Axis | 'tie'): string => (a === 'tie' ? '并列（无主导轴）' : a.toUpperCase());

/**
 * 校验某 url 的 GLB 场景（由 modelCache 在加载成功回调里调用）。
 * `animations` 用于把"被 clip 驱动的节点"从 rotation 检查中豁免（rig 姿态）。
 * 返回 `true` = 通过 / 无目标（不判定）；`false` = 报出了告警。
 * **绝不修改几何**（见文件头契约 2）。
 */
export function checkModelSize(
  root: THREE.Object3D,
  url: string,
  animations: readonly THREE.AnimationClip[] = [],
): boolean {
  if (!DEV) return true;
  const target = TARGETS.get(url);
  if (!target) return true;
  if (target.measureNode && !root.getObjectByName(target.measureNode)) {
    console.warn(
      `[glbSizeGuard] 量测子节点缺失，跳过尺寸校验：${target.label}（${url}）` +
        ` — 期望节点 "${target.measureNode}"（美术导出脚本改名了？）`,
    );
    return false;
  }
  const probe = probeModelSize(root, target.measureNode, animatedNodeNames(animations));
  if (!probe) return true; // 空 GLB：无可量测几何（降级链由调用方负责）
  const tol = target.tol ?? DEFAULT_TOL;
  const lines: string[] = [];

  // ① 世界尺寸 vs 目标
  for (const axis of AXES) {
    const want = target[axis];
    if (want === undefined || want <= 0) continue;
    const got = probe.worldSize[axis];
    const ratio = got / want;
    if (Math.abs(ratio - 1) > tol) {
      lines.push(
        `· ${axis.toUpperCase()} 世界尺寸 ${f(got)} / 目标 ${f(want)} → ${ratio.toFixed(2)}×` +
          `（容差 ±${Math.round(tol * 100)}%）`,
      );
    }
  }

  // ② 落地（minY）
  if (target.minY !== undefined && Math.abs(probe.worldMin.y - target.minY) > MINY_TOL) {
    const d = probe.worldMin.y - target.minY;
    lines.push(
      `· 落地 minY 实测 ${f(probe.worldMin.y)} / 目标 ${f(target.minY)} → ` +
        `${d < 0 ? '低于地面' : '悬空'} ${f(Math.abs(d))}（容差 ${f(MINY_TOL)}）`,
    );
  }

  // ③ 几何级 vs 世界级：不等 ⇒ 尺寸挂在节点变换上（"Box3 看着正常"的隐形错法）
  const geomWorldDiff: string[] = [];
  for (const axis of AXES) {
    const want = target[axis];
    if (want === undefined || want <= 0) continue;
    if (Math.abs(probe.worldSize[axis] - probe.geomSize[axis]) > tol * want) {
      geomWorldDiff.push(axis.toUpperCase());
    }
  }

  // ⑤ 直立：声明「竖直轴 = +Y」⇒ Y 必须为主导轴（严格、无容差）
  // 量测口径同 ①②③：**被测节点局部系**（= 消费端 post-rootInv 所见）。
  const dominant = dominantAxis(probe.worldSize);
  if (target.upright && dominant !== 'y') {
    const s = probe.worldSize;
    lines.push(
      `· 直立不符（⑤ upright）：声明「竖直轴 = +Y」，实测主导轴 = ${up(dominant)}；` +
        `Y ${f(s.y)} 未同时大于 X ${f(s.x)} 与 Z ${f(s.z)}。` +
        '⇒ 模型**侧躺**（批次 19 症状：把高度摆在 Z 上）。消费端 `<Model>` 零旋转直挂，' +
        '竖直轴只能是 +Y；请核对 3d_script/build_*.py 的导出轴向。' +
        '（若该物件本就扁平 —— 如车辆 长>宽>高、低层站房 宽>高 —— 请勿声明 upright：' +
        '该断言语义下它们必然报红，其轴向错误由 ① 兜住，见文件头「已知边界」。）',
    );
  }

  // ⑥ 向前轴：声明「水平长轴 = 消费端 yaw 公式所假设的向前轴」
  if (target.forward) {
    const longAxis = horizontalLongAxis(probe.worldSize);
    const ok = longAxis === target.forward;
    if (!ok) {
      const s = probe.worldSize;
      lines.push(
        `· 向前轴不符（⑥ forward）：声明 forward='${target.forward}'（= 消费端 yaw 公式假设的` +
          `向前轴），实测水平长轴 = ${up(longAxis)}（X ${f(s.x)} vs Z ${f(s.z)}）。` +
          '⇒ **因果**：`Vehicle.tsx` 用 `atan2(-dz, dx)` 假设模型 **+X 向前**、' +
          '`PedestrianV3.tsx` 用 `atan2(dx, dz)` 假设模型 **+Z 向前**；' +
          '声明与实际不符 ⇒ 该模型会被摆成与行进方向**垂直 90°**' +
          '（批次 29 修正前「全城 50 辆车横着开」正是此失效模式，' +
          '当时因 GLB 与程序化 fallback 一致地错而被"两路径包围盒一致"掩盖）。' +
          `请把反向轴改为 '${longAxis === 'x' ? 'x' : 'z'}'，或按新轴重导模型 + 同步消费端 yaw 公式。`,
      );
    }
  }

  if (lines.length === 0 && geomWorldDiff.length === 0 && probe.transformIssues.length === 0) {
    return true;
  }

  const detail: string[] = [...lines];
  detail.push(
    `· 世界尺寸（被测节点局部系 = 消费端 post-rootInv 所见）${fmtVec(probe.worldSize)} / ` +
      `几何尺寸（剥节点 scale）${fmtVec(probe.geomSize)}`,
  );
  if (geomWorldDiff.length > 0) {
    detail.push(
      `· ✗ 尺寸由**节点变换**承载（${geomWorldDiff.join('/')} 轴）：世界与几何口径不一致；` +
        '按「几何 × matrixWorld」重算相对矩阵的消费端（如 RoadsideBins.collectPairs）' +
        '会把该变换抵消 ⇒ 渲染尺寸 ≠ 设计尺寸。尺寸必须烘焙进顶点。',
    );
  }
  if (probe.transformIssues.length > 0) {
    detail.push(
      `· ✗ 节点变换非 identity（${probe.transformIssues.length} 个）：` +
        `${probe.transformIssues.slice(0, 8).join(' | ')}` +
        '（scale 必须 1、rotation 必须 0；位移允许，但要确认消费端已归一化变体内容）',
    );
  }
  console.warn(
    `[glbSizeGuard] 尺寸/变换规约不符：${target.label}（${url}）\n  ${detail.join('\n  ')}\n` +
      `  → 目标：世界尺寸 ${fmtVec(
        new THREE.Vector3(target.x ?? NaN, target.y ?? NaN, target.z ?? NaN),
      )}${target.minY !== undefined ? ` / minY ${f(target.minY)}` : ''}` +
      (target.upright ? ' / 竖直轴 +Y（主导轴）' : '') +
      (target.forward ? ` / 向前轴 +${target.forward.toUpperCase()}` : '') +
      '（= 游戏侧尺寸表 REAL_DIMS_M）。GLB 应自带正确尺寸/轴向/原点，' +
      '消费端零旋转零 scale；请核对 3d_script/build_*.py 并用 verify 脚本复核。',
  );
  return false;
}
