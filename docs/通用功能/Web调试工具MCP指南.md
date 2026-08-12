# GoWebDebugTool — MCP 使用指南

> 本文档是 GoWebDebugTool 的快速入门指南，供 Agent 自动加载使用。
> 完整 API 文档请参考子模块中的 `MCP_Proc_Def.md`。

---

## 1. 工具简介

**GoWebDebugTool** 是一个基于 Chrome DevTools Protocol (CDP) 的本地 HTTP 调试服务，专门用于让 LLM Agent 远程驱动真实 Chrome 浏览器进行：

- 页面浏览与调试
- 自动化操作（点击、输入、导航）
- 数据采集（截图、DOM、Console、Network）
- 多标签页管理

---

## 2. 快速启动

### 2.1 启动服务

```bash
# 前台运行
cd go-web-debug-tool
./GoWebDebugTool

# 守护进程模式
./GoWebDebugTool -d

# 停止服务
./GoWebDebugTool -u
```

### 2.2 服务地址

- 默认监听: `http://localhost:28999`
- 所有接口: `POST + application/json`

---

## 3. 核心接口

| 接口 | URL | 用途 |
|------|-----|------|
| 新建页面 | `POST /NewChromePage` | 打开新标签页 |
| 控制页面 | `POST /ControlChromePage` | 执行交互动作 |
| 查询页面 | `POST /LookChromePageInfo` | 读取页面信息 |
| 关闭页面 | `POST /CloseChromePage` | 关闭并释放资源 |
| 列出页面 | `POST /ListChromePages` | 枚举所有页面 |

---

## 4. 典型使用流程

### 4.1 基础流程

```bash
# 1. 列出当前页面
curl -X POST http://localhost:28999/ListChromePages

# 2. 新建页面
curl -X POST http://localhost:28999/NewChromePage \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com"}'
# 返回: {"code":0, "data":{"page_id":"p_xxxxxxxx"}}

# 3. 控制页面 (点击、输入等)
curl -X POST http://localhost:28999/ControlChromePage \
  -H "Content-Type: application/json" \
  -d '{"page_id":"p_xxxxxxxx", "action":"click", "selector":"#button"}'

# 4. 查询页面信息
curl -X POST http://localhost:28999/LookChromePageInfo \
  -H "Content-Type: application/json" \
  -d '{"page_id":"p_xxxxxxxx", "info":"screenshot"}'

# 5. 关闭页面
curl -X POST http://localhost:28999/CloseChromePage \
  -H "Content-Type: application/json" \
  -d '{"page_id":"p_xxxxxxxx"}'
```

---

## 5. 常用动作 (ControlChromePage)

| action | 说明 | 必需参数 |
|--------|------|----------|
| `click` | 点击元素 | `selector` |
| `input_text` | 输入文本 | `selector`, `text` |
| `navigate` | 导航到 URL | `url` |
| `scroll` | 滚动页面 | `direction` (up/down) |
| `wait` | 等待元素出现 | `selector` |
| `close_tab` | 关闭当前标签页 | 无 |

### 5.1 输入文本示例

```json
{
  "page_id": "p_xxxxxxxx",
  "action": "input_text",
  "selector": "input[name='username']",
  "text": "admin",
  "use_js": false
}
```

> **提示**: 对于 React 受控组件，如果 `use_js=false` 失败，尝试 `use_js: true`

---

## 6. 查询信息类型 (LookChromePageInfo)

| info | 说明 | 返回数据 |
|------|------|----------|
| `screenshot` | 页面截图 | Base64 编码的 PNG |
| `console` | Console 日志 | 最近 500 条日志 |
| `network` | Network 请求 | 最近 500 条请求 |
| `dom` | DOM 树 | HTML 字符串 |
| `url` | 当前 URL | URL 字符串 |
| `title` | 页面标题 | 标题字符串 |

---

## 7. 错误处理

| 错误码 | 含义 | 处理建议 |
|--------|------|----------|
| 0 | 成功 | 正常处理 |
| 1000 | JSON 格式错误 | 检查请求体 |
| 1001 | 参数缺失/无效 | 检查参数 |
| 1002 | 未授权 | 检查 auth_token |
| 2000 | page_id 不存在 | 调用 ListChromePages 刷新 |
| 2001 | CDP 断开 | 等待重连或重新打开页面 |
| 2002 | 动作执行失败 | 检查 selector，可重试 |
| 2003 | 页面崩溃 | 关闭后重新打开 |
| 3000 | 服务器内部错误 | 查看日志 |

---

## 8. Agent 协作建议

### 8.1 单页面任务

适用于简单的页面操作流程，由单个 Agent 完成。

### 8.2 多页面任务

当需要同时操作多个页面时，建议：

1. 主 Agent 负责页面管理（打开/关闭）
2. 为每个页面分配 SubAgent
3. SubAgent 持有 `page_id`，执行具体操作
4. 主 Agent 汇总结果

### 8.3 长流程任务

超过 10 个步骤的流程，建议拆分为多个 SubAgent，避免上下文溢出。

---

## 9. 配置文件

配置文件: `GoWebDebugTool.conf` (JSON 格式)

```json
{
  "listen_host": "localhost",
  "listen_port": 28999,
  "auth_token": "",
  "log_level": "info",
  "page_timeout_seconds": 30,
  "chrome_headless": false,
  "max_pages": 32
}
```

---

## 10. 反自动化检测 (anti_detect)

为绕过常见 WAF/反爬对 `navigator.webdriver === true` 的判定，服务默认应用三层防护：

1. **启动 flag** `--enable-automation=false`：覆盖 chromedp 默认 `true`，禁止 Chrome 自动设置 `navigator.webdriver`
2. **启动 flag** `--disable-blink-features=AutomationControlled`：关闭 Blink 引擎的 AutomationControlled 特性
3. **页面级 shim**：`Page.addScriptToEvaluateOnNewDocument` 在每个新文档创建前执行 `Object.defineProperty(navigator, 'webdriver', {get: () => false})`

三层互为冗余，任何一层回归不会单独暴露指纹。如需进一步定制，使用 `action=add_script` 跨导航注入自定义脚本。

---

## 11. Chrome 进程生命周期管理

服务内置 3 层兜底，防止 Chrome 子进程泄漏：

### 11.1 Janitor 后台扫描

配置 `chrome_zombie_scan_interval_seconds`（默认 30s）周期触发：

- **闲置回收**：`time.Since(entry.LastUsedAt) > chrome_idle_timeout_seconds` 时强制清理
- **寿命回收**：`time.Since(entry.CreatedAt) > chrome_max_lifetime_seconds` 时强制清理
- **僵尸回收**：`entry.Ctx.Err() != nil`（CDP 连接断开 / Chrome 已死）时清理

### 11.2 Close Tab 后同步清理

`ControlChromePage close_tab` 成功后立即清理，不依赖 `target.TargetDestroyed` 异步事件。

### 11.3 启动时孤儿 Chrome 清理

配置 `kill_stale_chrome_on_startup=true`（默认）：
- 通过 `/proc/<pid>/task/<tid>/children` 枚举本进程 + 祖先链上的子进程
- 用 `/proc/<pid>/comm` 判定是否为 Chrome
- 先 SIGTERM 5s grace，再 SIGKILL
- 只杀"祖先链能追溯到上一次 GWDT 进程"的 Chrome，避免误杀用户工作流里的浏览器

---

## 12. 页面关闭自动感知

服务端通过 CDP `target.TargetDestroyed` 事件实时监听标签页的关闭（包括用户手动点击 X、JS 调用 `window.close()`、标签页崩溃等情况），关闭时自动清理对应的 `PageEntry` 数据对象。

- **自动清理时机**：CDP 事件触发时即时清理；调用 `/ListChromePages` 时也会做存活性校验
- **Agent 侧影响**：手动关闭 Chrome 标签页后，再调用 `/ListChromePages` 将不再看到该 `page_id`，若仍持有该 `page_id` 并调用接口，将收到 `2000` (page_id not found)
- **close_tab action**：调用 `ControlChromePage` 的 `close_tab` 后，服务端会立即清理该 entry，无需再手动调用 `/CloseChromePage`
- **日志区分**：服务日志中会记录关闭原因 (`reason=` 字段)：
  - `api` — 通过 `/CloseChromePage` 接口关闭
  - `close_tab` — 通过 `ControlChromePage` 的 `close_tab` action 关闭
  - `target_destroyed` — 外部事件触发（手动/自动/崩溃）

---

## 13. 常见问题

### Q: 元素点击失败怎么办？

A: 尝试以下方案：
1. 先用 `wait` 等待元素出现
2. 使用 `use_js: true` 绕过 React 事件系统
3. 检查 selector 是否正确

### Q: 页面没有响应？

A: 检查：
1. Chrome 进程是否正常运行
2. 调用 `ListChromePages` 确认页面是否存在
3. 查看日志文件 `GoWebDebugTool.log`

### Q: 如何处理登录态？

A:
1. 使用 `chrome_user_data_dir` 指定用户数据目录
2. 或在配置中设置 `auto_connect: true` 连接已登录的 Chrome

### Q: 如何处理 React 受控组件的输入？

A: 对于 React 受控组件，如果 `use_js=false` 失败（输入后值被清空），尝试 `use_js: true` 绕过 React 事件系统。

---

## 14. 相关文件

- 完整 API 文档: `go-web-debug-tool/MCP_Proc_Def.md`
- 配置说明: `go-web-debug-tool/GoWebDebugTool.conf`
- 日志文件: `go-web-debug-tool/GoWebDebugTool.log`

---

## 15. 集成到 LsmAgentGame

GoWebDebugTool 可用于：

1. **前端调试**: 自动测试 UI 交互流程
2. **E2E 测试**: 模拟用户操作验证功能
3. **数据采集**: 抓取页面状态用于分析
4. **自动化演示**: 录制操作流程

完整集成说明见 `CLAUDE.md` §30。

### 使用场景示例

```bash
# 测试登录流程
curl -X POST http://localhost:28999/NewChromePage \
  -d '{"url": "https://localhost:39001"}'

# 输入账号密码
curl -X POST http://localhost:28999/ControlChromePage \
  -d '{"page_id":"p_xxx", "action":"input_text", "selector":"#account", "text":"testuser"}'

# 点击登录按钮
curl -X POST http://localhost:28999/ControlChromePage \
  -d '{"page_id":"p_xxx", "action":"click", "selector":"#login-btn"}'

# 截图验证
curl -X POST http://localhost:28999/LookChromePageInfo \
  -d '{"page_id":"p_xxx", "info":"screenshot"}'
```
