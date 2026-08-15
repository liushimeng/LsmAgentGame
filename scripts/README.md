# scripts/ — 工程脚本目录

> 2026-08-15 §20260812-01 重组:历史 R14/R15 自动化脚本与编译产物已清除,
> 保留脚本按「谁调用它」分三层。方案见 `tmpPlan/scripts优化和解决方案-20260812-01.md`。

## 目录分层

```
scripts/
├── ci/          CI 守门脚本(被 .github/workflows/ci.yml 调用)
├── smoke/       后端冒烟测试(人/Agent 手动跑,需服务已启动)
└── screenshot/  狼人杀截图工具链(go-web-debug-tool CDP,python3)
```

### ci/ — CI 守门

| 文件 | 用途 | 调用方 |
|------|------|--------|
| `check_line_limit.sh` | CLAUDE.md §4「单文件 ≤ 1800 行」硬上限自检(带 baseline 棘轮:只许变短不许变长) | `.github/workflows/ci.yml` |

### smoke/ — 后端冒烟测试

规约见 [`docs/通用功能/自动化测试策略.md`](../docs/通用功能/自动化测试策略.md):
新增冒烟脚本必须放本目录、加 `test_` 前缀;**禁止**新增 Python 协议级测试脚本(§3 反模式)。

| 文件 | 用途 |
|------|------|
| `test_wallet_api.sh` | 钱包 HTTP 端到端(注册/登录/充值/每日领取) |
| `test_wallet_db_consistency.sh` | 钱包 DB 不变量(余额 = 流水累加) |
| `test_wallet_ws.sh` | 钱包 WS 推送(`wallet.balance` 帧);按需把 `wsclient.go` 编译为 `.wsclient`(gitignored) |
| `test_wallet_i18n.js` | 钱包 i18n 键 zh-CN/en/ja 三语对齐 |
| `wsclient.go` | 上方 WS 测试的客户端(`//go:build ignore`,不进主构建) |

### screenshot/ — 狼人杀截图工具链

提示词与流程见根目录 `AutoScreenshotWerewolf.md`。

| 文件 | 用途 |
|------|------|
| `atx.py` | go-web-debug-tool CDP REST 封装库(new_page/navigate/eval_js/screenshot/local_login);被同目录两个脚本 import,**不单独运行** |
| `werewolf_highlight_capture.py` | 【主入口】13 人局双页(玩家+观战)精彩画面采集,输出 `ProjectPic/raw/` |
| `werewolf_screenshot.py` | 旧版单页截图,保留兼容 |

## 迁移对照(2026-08-15)

| 旧路径 | 新路径 |
|--------|--------|
| `scripts/check_line_limit.sh` | `scripts/ci/check_line_limit.sh` |
| `scripts/test_wallet_*.{sh,js}` | `scripts/smoke/test_wallet_*` |
| `scripts/wsclient.go` | `scripts/smoke/wsclient.go` |
| `scripts/auto3/atx.py` | `scripts/screenshot/atx.py` |
| `scripts/werewolf_{screenshot,highlight_capture}.py` | `scripts/screenshot/` |
| `scripts/r14_*.py` / `scripts/r15/` / `scripts/.wsclient` / `auto3/login_screen.jpg` | **已删除**(历史轮次脚本/编译产物,见 `docs/通用功能/自动化测试策略.md` §4) |

> 历史归档文档(如 `docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260814-01.md`)中
> 出现的旧路径为时间快照,不回改;以本表为准。
