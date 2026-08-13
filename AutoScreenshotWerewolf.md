## 自动化截图任务：狼人杀 13 人局 Agent 实机画面

> 本项目按 [MIT License](LICENSE) 开源。截图主目录与命名规则详见下方。

- **工作目录**: `/usr/local/LsmAgentGame/LsmAgentGame`
- **产物路径**:
  - 截图 PNG: `ProjectPic/werewolf-{NN}-{phase}.png`（NN 序号 01-12）
  - 截图报告: `TestReport/截图报告_YYYYMMDD_HHMMSS.md`
  - 进度文件: `AutoScreenshotProgress/截图进度_YYYYMMDD_HHMMSS.md`
  - 文件名时间戳统一 `YYYYMMDD_HHMMSS`,精度到秒
- **执行模式**: 主 Agent 跑核心截图流程;遇页面渲染异常 / Chrome 崩溃可按需委派 SubAgent,不阻塞主流程。
- **执行策略**: 1 名人类玩家 + 12 Agent 混合 13 人局,**重点强调 1 人 + 12 Agent**;串行单房间,以最近 10 次非缺陷修复提交划定截图范围,新增/优化项优先。
- **对局模式(唯一重点)**: 1 名人类玩家 + 12 个 Agent。
- **核心目标**: 采集 ≥ 8 张高质量 PNG 截图,展示 13 人局中人类玩家与 12 Agent 同场竞技的精彩画面,用于更新 README.md / README.en.md / README.ja.md。
- **代码变更限制**: 截图脚本只读运行,不修改业务代码。

### 1. 截图工具(MCP)

主工具是 `go-web-debug-tool`(MCP 服务,默认 `http://localhost:28999`),接口定义以
`go-web-debug-tool/MCP_Proc_Def.md` 为唯一事实来源。

- **新建页面**: `POST /NewChromePage { url, headless: false, wait_until: networkidle }`
- **导航**: `POST /ControlChromePage { page_id, action: "navigate", url }`
- **截图**: `POST /ControlChromePage { page_id, action: "screenshot", params: { format: "png", width: 1920, height: 1080 } }` → `{ image_base64 }`
- **JS 注入**: `POST /ControlChromePage { page_id, action: "eval_js", params: { expression } }`
- **关闭**: `POST /CloseChromePage { page_id }`

### 2. 截图脚本

主入口:`scripts/werewolf_screenshot.py`(Python,封装 go-web-debug-tool REST)

**单次截图**:
```bash
python3 scripts/werewolf_screenshot.py \
    --url "https://127.0.0.1:39001/werewolf" \
    --output ProjectPic/werewolf-01-room.png
```

**批量截图(推荐)**:
```bash
python3 scripts/werewolf_screenshot.py --batch \
    --output-dir ProjectPic --count 12 --interval 60 \
    --token "$JWT" --close
```

### 3. 截图清单(12 张目标)

按阶段优先级排序,每张图都必须有「场景标题 + 关键 UI 元素说明 + 截图时间」。

| 序号 | 截图文件名 | 阶段 | 关键元素 | 预期大小 |
|------|-----------|------|---------|---------|
| 01 | `werewolf-01-room-create.png` | 房间创建弹窗 | 房间名 / 模式 / 法官 / Agent 配置 | ≥ 80KB |
| 02 | `werewolf-02-role-pick.png` | 角色选择 | 13 个角色卡 / 选中态 / 难度分级 | ≥ 80KB |
| 03 | `werewolf-03-night-wolf.png` | 第 1 夜狼人决策 | 狼人座位 / 刀人目标 / 狼队暗号 | ≥ 80KB |
| 04 | `werewolf-04-night-witch.png` | 第 1 夜女巫用药 | 解药 / 毒药 / 目标选择 | ≥ 80KB |
| 05 | `werewolf-05-night-seer.png` | 第 1 夜预言家查验 | 查验目标 / 阵营结果 | ≥ 80KB |
| 06 | `werewolf-06-day-speak.png` | 白天发言 | Agent 发言流 / 表情 / 心口不一标记 | ≥ 100KB |
| 07 | `werewolf-07-vote.png` | 投票阶段 | 投票窗口 / 弃票 / 结果展示 | ≥ 80KB |
| 08 | `werewolf-08-prop.png` | 道具系统 | 急救箱 / 6 类 LLM 注入道具 | ≥ 80KB |
| 09 | `werewolf-09-death.png` | 死亡与遗言 | 死亡名单 / 遗言 / 身份翻开 | ≥ 80KB |
| 10 | `werewolf-10-judge.png` | 法官宣告 | ⚖️ 法官 Agent 旁白面板 | ≥ 80KB |
| 11 | `werewolf-11-summary.png` | 整局总结 | 5 段总结 / MVP / 狼人悍跳记录 | ≥ 80KB |
| 12 | `werewolf-12-memory.png` | MEMORY.md | Agent 持久化记忆 / 跨局迭代 | ≥ 60KB |

### 4. 启动前置

1. 读最近 10 次**非缺陷修复类**提交(`git log --oneline -10 -- ':!*fix' ':!*BUG'`)
2. 确认服务运行:
   - `curl -sk https://127.0.0.1:39001/api/health` 返回 `code: 0`
   - `curl -s http://localhost:28999/ListChromePages -X POST` 返回 `{"pages": []}`
3. 准备截图账号(JWT 从 `POST /api/auth/login` 获取,验证码旁路 `captcha_bypass: true`)
4. 创建 13 人局:`POST /api/games/werewolf/rooms` + `agent_seats: [12 items]`

### 5. 截图质量要求

- 分辨率: 1920 × 1080 (或更大)
- 文件大小: ≥ 50KB (避免空白页 / 加载未完成)
- 文件格式: PNG (无损)
- 关键 UI 元素可见: 玩家座位 / 发言区 / 状态栏 / 当前阶段提示
- **避免**: 加载占位符 / 错误页面 / 全黑画面

### 6. 截图后处理

1. **校验文件大小**: `find ProjectPic/werewolf-*.png -size -50k -delete` 删除过小文件
2. **重命名为 README 引用格式**: 序号 + 场景名
3. **更新 README 三语**:
   - `README.md` (中文): 新增「📸 实机截图（1 名人类 + 12 Agent）」板块
   - `README.en.md` (英文): 镜像中文版
   - `README.ja.md` (日文): 镜像中文版
4. **生成报告**: `TestReport/截图报告_YYYYMMDD_HHMMSS.md`

### 7. 终止条件

1. Chrome 页面崩溃 / 无法响应 ≥ 5 次 → 终止,**已采集截图保留**。
2. 服务端长时间无响应(超过 90 秒 `game.state`) → 终止,**降级为静态页面截图**。
3. 截图全部 ≥ 12 张 → 正常结束。
4. 无论正常结束或异常终止,均须出报告 + 进度文件。

### 8. 报告与后处理

1. 截图结束首先生成截图报告,完整保存(含截图清单 + 文件大小 + 截图时间)。
2. 同步生成进度文件,必备字段:
   - `本轮截图房间 ID` / `本轮对局模式(1人+12 Agent)`
   - `已采集截图清单`(文件名 + 大小 + 阶段)
   - `截图异常清单`(失败原因 + 重试次数)
3. **README 更新**: 三语 README 各更新「📸 实机截图」板块。
4. **git 提交**:
   ```bash
   git add ProjectPic/werewolf-*.png \
          ProjectPic/README*.md \
          scripts/werewolf_screenshot.py \
          AutoScreenshotWerewolf.md
   git commit -m "docs: 狼人杀 13 人局实机截图(1 人 + 12 Agent) + 自动化截图脚本"
   ```

### 9. 注意事项

- **数据安全**: 截图严禁明文显示 API Key / Token / Cookie,脱敏后再上传。
- **ProjectPic/ 入库策略**: `.gitignore` 已配置,README 中相对路径引用,本地浏览正常;GitHub 显示 broken image 可接受。
- **1 人 + 12 Agent 优先**: 全 AI 模式作为补充,主推人类玩家与 AI 同场。
- **截图脚本复用**: 复用 `scripts/auto3/atx.py` 已有封装,不重复造轮子。

### 10. 验收清单

- [ ] `ProjectPic/werewolf-*.png` 至少 8 张,每张 ≥ 50KB
- [ ] `scripts/werewolf_screenshot.py` 可重复运行(`python3 --help` 无错)
- [ ] `AutoScreenshotWerewolf.md` 提示词文件存在
- [ ] `README.md` / `README.en.md` / `README.ja.md` 全部新增「实机截图」板块
- [ ] `go build ./...` 通过
- [ ] `./rebuild_restart_app.sh` 退出码 0
- [ ] `git log` 有新提交
