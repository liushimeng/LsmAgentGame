#!/usr/bin/env python3
# =============================================================================
#  crosscheck_glb_dims.py — 「前端尺寸表 ↔ 资产真值」自动交叉校验（§27.3 / 批次 30）
#
#  背景：批次 29 把 cityScale.REAL_DIMS_M 定为「尺寸唯一事实来源」，但同一个
#  数字实际存在三处人工同步副本：
#    ① ClientWeb/src/components/virtualCity/cityScale.ts  REAL_DIMS_M（米）
#    ② 3d_script/build_*.py 内的几何常量（世界单位）
#    ③ 3d_script/verify_glb_aabb.py 的 TARGETS（世界单位）
#  批次 29 方案 §3.7 自己就建议过「脚本加 --emit-json 与前端表做 diff」但没做，
#  于是三处靠人手同步 —— 本脚本把这条闭环真正接上。
#
#  做法：
#    1. 解析 cityScale.ts 的 REAL_DIMS_M 表（**只读**，不引入 TS 编译器）；
#    2. 经 KEY_MAP 把 camelCase 表键映射到资产键 `<category>/<name>.glb`
#       （工具侧刻意不建第二份名字表，映射只此一处，防两表漂移）；
#    3. 交给 3d_script/verify_glb_aabb.py --check-table 比对实测包围盒
#       （容差 ±5%，与护栏 1 同口径）。
#
#  用法：
#    python3 scripts/ci/crosscheck_glb_dims.py              # 全量比对，非 0 退出即失败
#    python3 scripts/ci/crosscheck_glb_dims.py --emit       # 只打印将要送检的表（调试）
#
#  退出码：0 = 通过；1 = 有条目超差 / 表键未接线；2 = 解析失败
# =============================================================================

from __future__ import annotations

import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CITY_SCALE = ROOT / "ClientWeb/src/components/virtualCity/cityScale.ts"
VERIFIER = ROOT / "3d_script/verify_glb_aabb.py"

# ── 表键 → 资产键（唯一映射表；只登记**有对应 GLB** 的键）────────────────
KEY_MAP = {
    "trashCan": "road/trash_can",
    "sedan": "vehicles/sedan",
    "taxi": "vehicles/taxi",
    "bus": "vehicles/bus",
    "truck": "vehicles/truck",
    "pedestrian": "characters/pedestrian_walk",
    "oakTree": "nature/oak_tree",            # 夏 / 春 / 秋 三变体同尺寸
    "oakTreeWinter": "nature/oak_tree_winter",
    "pineTree": "nature/pine_tree",
    "cactus": "nature/cactus",
    "cityHall": "civic/city_hall",
    "commTower": "civic/comm_tower",
    "waterTower": "civic/water_tower",
    "policeStation": "civic/police_station",
    "fireStation": "civic/fire_station",
    # 批次 42 街具五件（设计 42 §3）
    "busStop": "road/bus_stop",
    "mailbox": "road/mailbox",
    "streetSign": "road/street_sign",
    "parkingMeter": "road/parking_meter",
    "bikeRack": "road/bike_rack",
    # 批次 43 交通信号灯三件（设计 43 §3）
    "trafficSignal": "road/traffic_signal",
    "pedestrianSignal": "road/pedestrian_signal",
    "mastArmSignal": "road/mast_arm_signal",
    # 批次 44 城市树（设计 44 §3）。streetTree 由 PROCEDURAL_ONLY 移入本表 ——
    #   它现在真的有 GLB 了（批次 30 起该键只是占位、零消费点，见批次 44 T12）。
    "streetTree": "nature/street_tree",
    "palmTree": "nature/palm_tree",
    # 批次 45 公园设施五件（设计 45 §3）
    "parkPavilion": "civic/park_pavilion",
    "parkPlayground": "civic/park_playground",
    "parkFitness": "civic/park_fitness",
    "parkBench": "civic/park_bench",
    "parkLamp": "civic/park_lamp",
    # 批次 46 城市公用设施三件（设计 46 §3）
    "substation": "civic/substation",
    "gasStation": "civic/gas_station",
    "heliPad": "civic/heli_pad",
    # 批次 47 体育场（设计 47 §3）
    "sportsField": "civic/sports_field",
    # 批次 48 港口码头（设计 48 §3）
    "portTerminal": "civic/port_terminal",
    "railSpan": "civic/rail_span",
    "railStation": "civic/rail_station",
}

# 程序化 fallback 专用、无 GLB ⇒ 不送检（但解析出来打印，便于人眼核对）。
# 批次 42 增补 streetLight：road_props.glb 批次 31 已删（路灯改由
# props/StreetLightsInstanced.tsx 程序化实例化），旧 KEY_MAP 映射指向空文件
# 导致 crosscheck 常红 —— 移入本集合（与 streetLightSide 同口径）。
# 批次 44：streetTree 已移出本集合（接入 nature/street_tree.glb）。
PROCEDURAL_ONLY = {"streetLight", "streetLightSide", "trafficSignalPole"}

LINE_RE = re.compile(r"^\s*(\w+)\s*:\s*\{([^}]*)\}")
FIELD_RE = re.compile(r"\b(x|y|z|minY)\s*:\s*(-?\d+(?:\.\d+)?)")


def parse_real_dims(text: str) -> dict[str, dict[str, float]]:
    """抽出 REAL_DIMS_M 块内每个键的 {x?,y?,z?,minY?}（单位 = 米）。"""
    start = text.find("export const REAL_DIMS_M = {")
    if start < 0:
        raise SystemExit(f"❌ 在 {CITY_SCALE} 里找不到 REAL_DIMS_M 定义")
    end = text.find("} as const satisfies", start)
    if end < 0:
        raise SystemExit(f"❌ REAL_DIMS_M 块结尾 `}} as const satisfies` 未找到")
    block = text[start:end]

    out: dict[str, dict[str, float]] = {}
    for line in block.splitlines():
        if line.lstrip().startswith(("/*", "*", "//")):
            continue
        m = LINE_RE.match(line)
        if not m:
            continue
        key, body = m.group(1), m.group(2)
        dims = {k: float(v) for k, v in FIELD_RE.findall(body)}
        if dims:
            out[key] = dims
    return out


def build_check_table(dims: dict[str, dict[str, float]]) -> dict:
    """REAL_DIMS_M（米）→ --check-table 送检表。"""
    table: dict = {"__unit__": "m"}
    for key, fields in dims.items():
        asset = KEY_MAP.get(key)
        if not asset:
            continue
        entry = {k: fields[k] for k in ("x", "y", "z") if k in fields}
        if "minY" in fields:
            entry["minY"] = fields["minY"]
        table[asset] = entry
    return table


def main() -> int:
    emit_only = "--emit" in sys.argv

    text = CITY_SCALE.read_text(encoding="utf-8")
    dims = parse_real_dims(text)
    if not dims:
        print("❌ REAL_DIMS_M 解析出 0 条 —— 正则或表结构变了，请更新本脚本", file=sys.stderr)
        return 2

    table = build_check_table(dims)

    unmapped = [k for k in dims if k not in KEY_MAP and k not in PROCEDURAL_ONLY]
    if unmapped:
        print(f"⚠ REAL_DIMS_M 里有未接线的新键：{unmapped}")
        print("  —— 有 GLB 的请加进 KEY_MAP；纯程序化的请加进 PROCEDURAL_ONLY（防静默漏检）")
        return 1

    print(f"REAL_DIMS_M 共 {len(dims)} 条；送检 {len(table) - 1} 条；"
          f"程序化不送检 {sorted(PROCEDURAL_ONLY & set(dims))}")

    if emit_only:
        print(json.dumps(table, ensure_ascii=False, indent=2))
        return 0

    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False, encoding="utf-8") as f:
        json.dump(table, f, ensure_ascii=False)
        tmp = f.name

    r = subprocess.run(
        [sys.executable, str(VERIFIER), "--check-table", tmp],
        cwd=str(ROOT), capture_output=True, text=True,
    )
    # 只透出比对段落，避免全量扫描输出淹没结论。
    keep = [ln for ln in (r.stdout + r.stderr).splitlines()
            if "check-table" in ln or "✗" in ln or "FAIL" in ln or "扫描" in ln or "不符合" in ln]
    print("\n".join(keep))
    Path(tmp).unlink(missing_ok=True)
    return r.returncode


if __name__ == "__main__":
    sys.exit(main())
