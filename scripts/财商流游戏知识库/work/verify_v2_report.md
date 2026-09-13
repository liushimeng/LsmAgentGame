# v2.0 数字编号迁移验收报告

> 生成时间：2026-09-14T01:09:56
> 总卡数：69438
> 通过：16 项
> 失败：0 项

## 验收结果

✅ **全部 15 项验收通过**

## 通过项详情

- ✓ [1] occ_id 全覆盖: 全部 69437 张卡都有 occ_id 字段
- ✓ [2] 5 位流水同 L3 内唯一: 4535 个 v2.0 L3 桶内流水全部唯一（每桶从 00001 起算）
- ✓ [2.1] 5 位流水不超 99999: 全部 69437 个流水号在 5 位范围内
- ✓ [3] _legacy_ids 保留原 id: 全部 69437 张卡的 _legacy_ids 含原 id
- ✓ [4] 目录树纯数字: 25+ 个旧字母 L1 目录全部已 git mv
- ✓ [5] occ_id 格式正确: 全部 69437 个 occ_id 符合 OCC-XX-NNNNN
- ✓ [6] occ_industry_num 格式: 全部 69437 张卡 occ_industry_num 是 2 位数字
- ✓ [7] occ_l2/l3_num 格式: 全部 69437 张卡 4/6 位数字码合规
- ✓ [8] occ_l3_num 前缀 = occ_l2_num: 全部 69437 张卡 L3 前缀 = L2
- ✓ [9] 数值自洽（抽样 100）: 抽样 100 张卡的 monthly_cashflow = income - expense 100% 成立
- ✓ [10] 文件名 N<流水>-<姓名>.md 不动: 全部 69437 张卡文件名遵循规范
- ✓ [11] 旧 N id 仍存: 全部 69437 张卡保留 N<流水> id
- ✓ [12] v4.0 industry_l1 字母保留: 全部 69437 张卡保留 v4.0 字母 L1
- ✓ [13] _migration_v2 审计字段: 全部 69437 张卡含 _migration_v2
- ✓ [14] 子模块隔离: go-web-debug-tool 已通过 is_submodule() 过滤（按预期隔离）
- ✓ [15] _legacy_ids 是 list: 全部 _legacy_ids 字段类型正确

## 元数据

```json
{
  "ts": "2026-09-14T01:09:56",
  "total": 69438,
  "passed_count": 16,
  "failed_count": 0
}
```