## 自动化截图任务：德州扑克 2-6 人 Agent 实机画面

> 本项目按 [MIT License](LICENSE) 开源。截图主目录与命名规则详见下方。
> 本文件为德州扑克专用入口，**仅产出** `texaspoker-*.png` 系列截图；狼人杀请使用 [`AutoScreenshotWerewolf.md`](AutoScreenshotWerewolf.md)。

- **工作目录**: `/usr/local/LsmAgentGame/LsmAgentGame`
- **入口脚本**: `AutoScreenshotTexasPoker.sh`
- **产物路径(仅德扑)**:
  - 截图 PNG: `ProjectPic/texaspoker-{NN}-{phase}.png`（NN 序号 01-12）
  - 截图报告: `TestReport/德州扑克截图报告_YYYYMMDD_HHMMSS.md`
  - 进度文件: `AutoScreenshotProgress/德州扑克截图进度_YYYYMMDD_HHMMSS.md`
  - 文件名时间戳统一 `YYYYMMDD_HHMMSS`,精度到秒
- **执行模式**: 主 Agent 跑核心截图流程;遇页面渲染异常 / Chrome 崩溃可按需委派 SubAgent,不阻塞主流程。
- **执行策略**: 人类玩家与 N 个 Agent 混合对局,**重点强调 1 人 + 2~5 Bot**;**轮换玩家数(3~6) 与 Bot 比例**,以最近 10 次非缺陷修复提交划定截图范围,新增/优化项优先。
- **核心目标**: 采集 ≥ 8 张高质量 PNG 截图,展示 2-6 人桌德扑中人类玩家与 N Bot 同场竞技的精彩画面,用于更新 README 三语种版本。

### 1. 截图工具(MCP)

主工具是 `go-web-debug-tool`(MCP 服务,默认 `http://localhost:28999`),接口定义以
`go-web-debug-tool/MCP_Proc_Def.md` 为唯一事实来源。

- **新建页面**: `POST /NewChromePage { url, headless: false, wait_until: networkidle }`
- **导航**: `POST /ControlChromePage { page_id, action: "navigate", url }`
- **截图**: `POST /ControlChromePage { page_id, action: "screenshot", params: { format: "png", width: 1920, height: 1080 } }` → `{ image_base64 }`
- **JS 注入**: `POST /ControlChromePage { page_id, action: "eval_js", params: { expression } }`
- **关闭**: `POST /CloseChromePage { page_id }`

### 2. 截图脚本

主入口:`scripts/screenshot/texasholdem_highlight_capture.py`(2026-08-19 起,双页采集器,复用 `scripts/screenshot/atx.py`)

**核心特性**(与狼人杀版同形态,特化德扑):
- **双页同采**: 玩家页 `/texasholdem/<room>` + 观战页 `/texasholdem/spectate/<room>` 每 60s 各截 1 张
- **认证注入**: `localStorage` 写 `lsm.token` + `lsm.auth`(zustand persist 形状 `{"state":{...},"version":0}`)
- **emoji Web 字体注入**: headless Chrome 无 emoji 字体 → 豆腐块;CSS 同狼人杀版
- **Dealer 按钮旋转补抓**: 每轮轮询 REST `GET /api/rooms/:id`,`dealer_button` 变化时 3s 后补抓
- **崩溃自愈**: 连续 3 次双页截图失败自动重建页面;终止条件 = Showdown + 60s settle / 75min 硬超时

```bash
python3 scripts/screenshot/texasholdem_highlight_capture.py --room-id <uuid> --interval 60
```

**GIF 合成(ffmpeg)**:
```bash
ffmpeg -y -framerate 1 -i frames%02d.png \
  -vf "scale=1152:-2:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=192[p];[s1][p]paletteuse=dither=bayer" \
  ProjectPic/texaspoker-highlights.gif
```

> ⚠️ go-web-debug-tool 的 `screenshot(format:"png")` 实际返回 **JPEG 字节**;精选后需 `ffmpeg -i in.png -frames:v 1 out.png` 转码为真 PNG。

### 3. 截图清单(12 张目标)

按阶段优先级排序,每张图都必须有「场景标题 + 关键 UI 元素说明 + 截图时间」。

| 序号 | 截图文件名 | 阶段 | 关键元素 | 预期大小 |
|------|-----------|------|---------|---------|
| 01 | `texaspoker-01-room-create.png` | 房间创建弹窗 | AI slider 0-6 / 模型分配 / Buy-in | ≥ 80KB |
| 02 | `texaspoker-02-preflop.png` | 翻牌前 | 2 张底牌 / Dealer 按钮 / 盲注 | ≥ 80KB |
| 03 | `texaspoker-03-flop.png` | 翻牌阶段 | 3 张公共牌 / 下注面板 / 筹码 | ≥ 100KB |
| 04 | `texaspoker-04-turn.png` | 转牌阶段 | 4 张公共牌 / Pot 当前值 | ≥ 80KB |
| 05 | `texaspoker-05-river.png` | 河牌阶段 | 5 张公共牌 / All-in 提示 | ≥ 80KB |
| 06 | `texaspoker-06-bet-slider.png` | 下注面板 | Raise slider + 4 个快捷(½/pot/2×/All-in) | ≥ 80KB |
| 07 | `texaspoker-07-bot-think.png` | Bot 思考中 | 多个 Bot 思考中排队 + Bot 心口不一 tip | ≥ 100KB |
| 08 | `texaspoker-08-sidepot.png` | All-in 旁注池 | 多玩家 All-in + 旁注池分割 | ≥ 80KB |
| 09 | `texaspoker-09-showdown.png` | 摊牌 | 全部底牌翻开 + 牌型评估 | ≥ 100KB |
| 10 | `texaspoker-10-pot-distribute.png` | 筹码分配 | 赢家收 Pot + 筹码动画 | ≥ 80KB |
| 11 | `texaspoker-11-spectator.png` | 观战者视图 | 隐藏底牌 + 公开牌面 + 行动流 | ≥ 60KB |
| 12 | `texaspoker-12-statistics.png` | 数据面板 | 胜率 / 手牌数 / Bot 模型画像 | ≥ 60KB |

### 4. 启动前置

1. 读最近 10 次**非缺陷修复类**提交(`git log --oneline -10 -- ':!*fix' ':!*BUG'`)
2. 确认服务运行:
   - `curl -sk https://127.0.0.1:39001/api/health` 返回 `code: 0`
   - `curl -s http://localhost:28999/ListChromePages -X POST` 返回 `{"pages": []}`
3. 准备截图账号(JWT 从 `POST /api/auth/login` 获取,验证码旁路 `captcha_bypass: true`)
4. 创建 2-6 人局:`POST /api/games/texasholdem/rooms` + `agent_seats: [N items]`

### 5. 截图质量要求

- 分辨率: 1920 × 1080 (或更大)
- 文件大小: ≥ 50KB (避免空白页 / 加载未完成)
- 文件格式: PNG (无损)
- 关键 UI 元素可见: 玩家座位 / 底牌 + 公共牌 / Dealer 按钮 / 当前阶段提示 / 筹码数字
- **避免**: 加载占位符 / 错误页面 / 全黑画面

### 6. 截图后处理

1. **校验文件大小**: `find ProjectPic/texaspoker-*.png -size -50k -delete` 删除过小文件
2. **重命名为 README 引用格式**: 序号 + 场景名
3. **更新 README 三语**:
   - `README.md` (中文): 新增「📸 实机截图（德扑 1 人 + N Agent）」板块
   - `README.en.md` (英文): 镜像中文版
   - `README.ja.md` (日文): 镜像中文版
4. **生成报告**: `TestReport/德州扑克截图报告_YYYYMMDD_HHMMSS.md`

### 7. 终止条件

1. Chrome 页面崩溃 / 无法响应 ≥ 5 次 → 终止,**已采集截图保留**。
2. 服务端长时间无响应(超过 90 秒 `game.state`) → 终止,**降级为静态页面截图**。
3. 截图全部 ≥ 12 张 → 正常结束。
4. 无论正常结束或异常终止,均须出报告 + 进度文件。

### 8. 报告与后处理

1. 截图结束首先生成德扑截图报告,完整保存(含截图清单 + 文件大小 + 截图时间)。
2. 同步生成进度文件,必备字段:
   - `本轮截图房间 ID` / `本轮对局模式(1 人 + N Bot)`
   - `已采集截图清单`(文件名 + 大小 + 阶段)
   - `截图异常清单`(失败原因 + 重试次数)
3. **README 更新**: 三语 README 各更新「📸 实机截图」板块。
4. **git 提交**:
   ```bash
   git add ProjectPic/texaspoker-*.png \
          ProjectPic/README*.md \
          scripts/screenshot/texasholdem_highlight_capture.py \
          AutoScreenshotTexasPoker.md \
          AutoScreenshotTexasPoker.sh
   git commit -m "docs: 德州扑克 2-6 人局实机截图(1 人 + N Agent) + 自动化截图脚本"
   ```

### 9. 注意事项

- **数据安全**: 截图严禁明文显示 API Key / Token / Cookie,脱敏后再上传。
- **ProjectPic/ 入库策略**: `ProjectPic/` **已加入 git 跟踪**(README 相对路径引用,GitHub 可直接渲染);
  仅 `ProjectPic/raw/` 过程产物不入库。单张 PNG 控制在 ~1.5MB 以内,GIF ≤ 5MB。
- **截图脚本复用**: 复用 `scripts/screenshot/atx.py` 已有封装,不重复造轮子。
- **与狼人杀截图版并存**: 本脚本与 `AutoScreenshotWerewolf.sh` 共用 `ProjectPic/` 与 `AutoScreenshotProgress/`,但前缀分别为 `texaspoker-*` 与 `werewolf-*`(或 `werewolf-2026-*`),不互相覆盖。

### 10. 验收清单

- [ ] `ProjectPic/texaspoker-*.png` 至少 8 张,每张 ≥ 50KB
- [ ] `scripts/screenshot/texasholdem_highlight_capture.py` 可重复运行(`python3 --help` 无错)
- [ ] `AutoScreenshotTexasPoker.md` 提示词文件存在
- [ ] `README.md` / `README.en.md` / `README.ja.md` 全部新增「实机截图」板块
- [ ] `go build ./...` 通过
- [ ] `./rebuild_restart_app.sh` 退出码 0
- [ ] `git log` 有新提交
