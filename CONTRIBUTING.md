# 贡献指南

> 感谢你愿意为 `LsmAgentGame` 添砖加瓦。
> 本仓库是 **100% AI Agent 自动编程** 的实验性项目，按下述流程提交贡献可最大化合并效率。

[🇨🇳 中文](CONTRIBUTING.md) · [🇬🇧 English](CONTRIBUTING.en.md) · [🇯🇵 日本語](CONTRIBUTING.ja.md)

---

## 1. 行为准则

参与本项目即表示你同意遵守 [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md)。
请以建设性、尊重、就事论事的态度参与讨论。

## 2. 提交 Issue

| 类别 | 模板 | 用途 |
|------|------|------|
| Bug 报告 | `.github/ISSUE_TEMPLATE/bug_report.md` | 复现步骤 + 期望 + 实际 + 日志 |
| 功能建议 | `.github/ISSUE_TEMPLATE/feature_request.md` | 痛点 + 方案 + 替代方案 |
| 文档改进 | `.github/ISSUE_TEMPLATE/documentation.md` | 链接 + 当前描述 + 期望描述 |
| 安全问题 | **不要** 公开 issue | 见 `SECURITY.md` 走私下邮箱 |

## 3. 提交 Pull Request

### 3.1 准备

1. **Fork** 本仓库 → 创建特性分支：`git checkout -b feat/<short-name>`
2. 同步子模块：`git submodule update --init --recursive`
3. 阅读 [`CLAUDE.md`](CLAUDE.md) §10（工作流程）与 §13（SubAgent 分工）了解代码规范与职责线划分。

### 3.2 编码规范

- **后端 Go** — `ServerGo/`
  - 文件 ≤ 1800 行（CLAUDE.md §4）
  - `go build -o LsmAgentGame main.go` 必须通过
  - `go test ./...` 必须通过
  - 数据库模型文件命名 `t_lsm_game_*.go`
  - 业务文件命名 `snake_case.go`
- **前端 React + TypeScript** — `ClientWeb/`
  - 文件 ≤ 1800 行
  - `tsc --noEmit` + `npm run build` 必须通过
  - 跨游戏 import **禁止**（CLAUDE.md §2.1）
  - 共享组件放 `components/common|chat|ui/`，游戏私有放 `components/<game>/`
- **CSS** — `ClientWeb/src/styles/`
  - `globals.css` 的 `@import` 顺序不可调整
  - 拆 CSS 时用「构建产物 CSS 字节一致」验证零回归

### 3.3 提交粒度

- 一个 commit 一个逻辑阶段（CLAUDE.md §10.1）。
- 提交须直接落在 `main` 分支（除非经维护者显式授权开特性分支）。
- commit message 中文优先，格式：

  ```
  <类型>: <一句话概括>

  - 改动 1
  - 改动 2
  - 关联 §编号或 issue 号
  ```

  类型：`feat` / `fix` / `refactor` / `docs` / `test` / `chore` / `perf`。

### 3.4 跨端联调

涉及 `ClientWeb/` + `ServerGo/` 同时改动的 PR，请在描述中说明：

- 触发链路（HTTP / WSS / 共享 state）
- 兼容旧版本客户端的策略（字段新增 / 旧字段保留）
- 端到端测试用例

### 3.5 PR 检查清单

- [ ] 代码遵循 CLAUDE.md §4 行数硬上限
- [ ] `go build` + `go test ./...` 通过
- [ ] `tsc --noEmit` + `npm run build` 通过
- [ ] 新增 `.proto` 已运行 `proto/gen.sh`
- [ ] README / docs 同步（新增/修改的 API、游戏规则、配置项）
- [ ] 涉数据库 schema 变更：附带迁移说明
- [ ] 涉密钥 / 内部账号：未硬编码到仓库

## 4. 自动化测试

仓库维护者使用 `AutoTestAndSaveReport.sh` 与 `go-web-debug-tool/` 子模块跑回归。
贡献者请在 PR 描述里附上本地回归脚本输出（不必粘贴全部日志，附 `TestReport/` 路径即可）。

## 5. 联系方式

- 一般讨论：GitHub / Gitee / GitCode Issue 区
- 安全问题：见 [`SECURITY.md`](SECURITY.md)（私下邮箱，不公开 issue）
- 商业合作：见仓库首页作者联系方式

---

> 本项目是「Loop Agent × Graphic Agent 24/7 不间断编程」的一次完整展示。
> 欢迎在 PR 描述里分享你自己团队里的 AI Agent 协作经验 —— 我们会汇总到 `docs/` 下的协作专题。
