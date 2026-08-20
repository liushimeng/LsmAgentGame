## 自动化截图任务：德州扑克 2-6 人 Agent 实机画面

> 本项目按 [MIT License](LICENSE) 开源。截图主目录与命名规则详见下方。
> 本文件为德州扑克专用入口，**仅产出** `texaspoker-*.png` 系列截图。

- **工作目录**: `/usr/local/LsmAgentGame/LsmAgentGame`
- **入口脚本**: `AutoScreenshot_TexasPoker.sh`
- **产物路径(仅德扑,§20260820-01 起 glob 集中到 `auto_run_common.sh::GAME_GLOBS`)**:
  - 截图 PNG: `ProjectPic/texaspoker-{NN}-{phase}.png`（NN 序号 01-12）
  - 截图报告: `TestReport/德州扑克截图报告_YYYYMMDD_HHMMSS.md`
  - 进度文件: `AutoScreenshotProgress/德州扑克截图进度_YYYYMMDD_HHMMSS.md`
  - 文件名时间戳统一 `YYYYMMDD_HHMMSS`,精度到秒
  - **Glob 单一事实源**: 所有扫描 glob 在 `auto_run_common.sh::GAME_GLOBS` 中声明。
- **执行模式**: 主 Agent 跑核心截图流程;遇页面渲染异常 / Chrome 崩溃可按需委派 SubAgent,不阻塞主流程。
- **执行策略(本版本硬约束)**:
  - **主场景**:**1 名真人类玩家 + 5 Bot = 满 6 人桌**(撞满平台上限,**README 展示最强对抗画面**),12 张截图清单主要来自该主场景。
  - **辅助场景(用于补缺)**:`1+3`(4 人桌)用于独立测试 Dealer 按钮旋转细节 / 小桌 Dealer 首到位置;`1+1`(2 人桌)用于覆盖 Heads-up 双人牌局。如资源不足,允许只跑主场景。
  - **N 不允许 < 1**(低于 2 人无法成局);`1+5` 无法调度时降为 `1+3`,不允许再低。
  - **拟人节奏**:Bot 决策间隔保留 0.8~1.5s;不下无意义 dev mode 截图;**避免连续 6 Bot 同步 All-in 的「冻结画面」**(该画面不代表真实对局)。
  - **重大异常 / 卡顿 / 不及预期**:不等待超时,立即退出,生成报告 + 接力修复。
- **核心目标**: 采集 ≥ 8 张高质量 PNG 截图,展示 2-6 人桌德扑中人类玩家与 N Bot 同场竞技的精彩画面,用于更新 README 三语种版本。

### 1. 截图工具(MCP)

主工具是 `go-web-debug-tool`(MCP 服务,默认 `http://localhost:28999`),接口定义以
`go-web-debug-tool/MCP_Proc_Def.md` 为唯一事实来源。

- **新建页面**: `POST /NewChromePage { url, headless: false, wait_until: networkidle }`
- **导航**: `POST /ControlChromePage { page_id, action: "navigate", url }`
- **截图**: `POST /ControlChromePage { page_id, action: "screenshot", params: { format: "png", width: 1920, height: 1080 } }` → `{ image_base64 }`
- **JS 注入**: `POST /ControlChromePage { page_id, action: "eval_js", params: { expression } }`(只读状态快照用,不用于一键改多状态)
- **关闭**: `POST /CloseChromePage { page_id }`

### 2. 截图脚本

主入口:`scripts/screenshot/texasholdem_highlight_capture.py`(2026-08-19 起,双页采集器,复用 `scripts/screenshot/atx.py`)

**核心特性**(德扑特化):
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

### 3. 截图清单(12 张目标,主场景 1+5 Bot)

按阶段优先级排序,每张图都必须有「场景标题 + 关键 UI 元素说明 + 截图时间」。**主场景来源**:`1 + 5 Bot` 满桌;**辅助来源**:`1 + 3 Bot`(Dealer 细节)与 `1 + 1 Bot`(Heads-up)。

| 序号 | 截图文件名 | 阶段 | 关键元素 | 主场景来源 | 预期大小 |
|------|-----------|------|---------|-----------|---------|
| 01 | `texaspoker-01-room-create.png` | 房间创建弹窗 | AI slider 0-6 / 模型分配 / Buy-in | 通用 | ≥ 80KB |
| 02 | `texaspoker-02-preflop.png` | 翻牌前 | 2 张底牌 / Dealer 按钮 / 盲注 | 主场景 1+5 | ≥ 80KB |
| 03 | `texaspoker-03-flop.png` | 翻牌阶段 | 3 张公共牌 / 下注面板 / 筹码 | 主场景 1+5 | ≥ 100KB |
| 04 | `texaspoker-04-turn.png` | 转牌阶段 | 4 张公共牌 / Pot 当前值 | 主场景 1+5 | ≥ 80KB |
| 05 | `texaspoker-05-river.png` | 河牌阶段 | 5 张公共牌 / All-in 提示 | 主场景 1+5 | ≥ 80KB |
| 06 | `texaspoker-06-bet-slider.png` | 下注面板 | Raise slider + 4 个快捷(½/pot/2×/All-in) | 主场景 1+5 | ≥ 80KB |
| 07 | `texaspoker-07-bot-think.png` | Bot 思考中 | 多 Bot 思考中排队 + Bot 心口不一 tip | 主场景 1+5 | ≥ 100KB |
| 08 | `texaspoker-08-sidepot.png` | All-in 旁注池 | 多玩家 All-in + 旁注池分割 | 辅助 1+3(人少更易触发 side pot) | ≥ 80KB |
| 09 | `texaspoker-09-showdown.png` | 摊牌 | 全部底牌翻开 + 牌型评估 | 主场景 1+5 | ≥ 100KB |
| 10 | `texaspoker-10-pot-distribute.png` | 筹码分配 | 赢家收 Pot + 筹码动画 | 主场景 1+5 | ≥ 80KB |
| 11 | `texaspoker-11-spectator.png` | 观战者视图 | 隐藏底牌 + 公开牌面 + 行动流 | 通用 | ≥ 60KB |
| 12 | `texaspoker-12-statistics.png` | 数据面板 | 胜率 / 手牌数 / Bot 模型画像 | 主场景 1+5 | ≥ 60KB |

> **辅助场景触发条件**:`texaspoker-08-sidepot` 在主场景 6 人桌 side pot 不易触发的桌台效率较低,可优先在 `1+3` 桌补抓;`texaspoker-01/11` 不依赖玩家数。

### 4. 启动前置

1. 读最近 10 次**非缺陷修复类**提交(`git log --oneline -10 -- ':!*fix' ':!*BUG'`)
2. 确认服务运行:
   - `curl -sk https://127.0.0.1:39001/api/health` 返回 `code: 0`
   - `curl -s http://localhost:28999/ListChromePages -X POST` 返回 `{"pages": []}`
3. 准备截图账号(JWT 从 `POST /api/auth/login` 获取,验证码旁路 `captcha_bypass: true`)
4. **首选**创建 6 人局(1 人 + 5 Bot):`POST /api/games/texasholdem/rooms` + `agent_seats: [5 items]`;**次选**4 人局;**禁止 < 3 人**。

### 5. 截图质量要求

- 分辨率: 1920 × 1080 (或更大)
- 文件大小: ≥ 50KB (避免空白页 / 加载未完成)
- 文件格式: PNG (无损)
- 关键 UI 元素可见: 玩家座位 / 底牌 + 公共牌 / Dealer 按钮 / 当前阶段提示 / 筹码数字
- **避免**: 加载占位符 / 错误页面 / 全黑画面 / 6 Bot 同步 All-in 冻结画面
- **主场景强约束**:`texaspoker-02` ~ `texaspoker-10` 中至少 6 张必须来自 `1+5` 桌。

### 6. 截图后处理

1. **校验文件大小**: `find ProjectPic/texaspoker-*.png -size -50k -delete` 删除过小文件
2. **重命名为 README 引用格式**: 序号 + 场景名
3. **更新 README 三语**(以 1+5 Bot 满桌画面为看板):
   - `README.md` (中文): 新增「📸 实机截图（德扑 1 人 + 5 Agent 满桌）」板块
   - `README.en.md` (英文): 镜像中文版
   - `README.ja.md` (日文): 镜像中文版
4. **生成报告**: `TestReport/德州扑克截图报告_YYYYMMDD_HHMMSS.md`(必须注明主场景「1+5」占比与辅助场景「1+3」补抓情况)

### 7. 终止条件

1. Chrome 页面崩溃 / 无法响应 ≥ 5 次 → 终止,**已采集截图保留**。
2. 服务端长时间无响应(超过 90 秒 `game.state`) → 终止,**降级为静态页面截图**。
3. 截图全部 ≥ 12 张 → 正常结束。
4. 重大异常 / 严重不及预期 / 卡住 → 立即终止,不待超时,出报告 + 接力。
5. 无论正常结束或异常终止,均须出报告 + 进度文件。

### 8. 报告与后处理

1. 截图结束首先生成德扑截图报告,完整保存(含截图清单 + 文件大小 + 截图时间 + 主/辅助场景标注)。
2. 同步生成进度文件,必备字段:
   - `本轮截图房间 ID` / `本轮主场景模式(1 人 + 5 Bot)` / `本轮辅助场景(若有)`
   - `已采集截图清单`(文件名 + 大小 + 阶段 + 来源主/辅)
   - `截图异常清单`(失败原因 + 重试次数)
3. **README 更新**: 三语 README 各更新「📸 实机截图」板块,**以 1+5 Bot 满桌为主视觉**。
4. **git 提交**:
   ```bash
   git add ProjectPic/texaspoker-*.png \
          ProjectPic/README*.md \
          scripts/screenshot/texasholdem_highlight_capture.py \
          AutoScreenshot_TexasPoker.md \
          AutoScreenshot_TexasPoker.sh
   git commit -m "docs: 德州扑克 2-6 人局实机截图(1 人 + 5 Bot 满桌) + 自动化截图脚本"
   ```

### 9. 注意事项

- **数据安全**: 截图严禁明文显示 API Key / Token / Cookie,脱敏后再上传。
- **ProjectPic/ 入库策略**: `ProjectPic/` **已加入 git 跟踪**(README 相对路径引用,GitHub 可直接渲染);
  仅 `ProjectPic/raw/` 过程产物不入库。单张 PNG 控制在 ~1.5MB 以内,GIF ≤ 5MB。
- **截图脚本复用**: 复用 `scripts/screenshot/atx.py` 已有封装,不重复造轮子。
- **拟人节奏**:Bot 思考期间保留 0.8~1.5s,截图前确保动画已结束(避免 Sliding chip / 牌面 flip 进行中)。
- **Jenkins/CI 提交顺序**: 截图与 README 更新在同一提交,保证 README 引用路径有效。

### 10. 验收清单

- [ ] `ProjectPic/texaspoker-*.png` 至少 8 张,每张 ≥ 50KB
- [ ] 主场景为 1 人 + 5 Bot 满桌(`texaspoker-02`~`10` 中 ≥ 6 张来源此桌)
- [ ] `scripts/screenshot/texasholdem_highlight_capture.py` 可重复运行(`python3 --help` 无错)
- [ ] `AutoScreenshot_TexasPoker.md` 提示词文件存在
- [ ] `README.md` / `README.en.md` / `README.ja.md` 全部新增「实机截图」板块
- [ ] `go build ./...` 通过
- [ ] `./rebuild_restart_app.sh` 退出码 0
- [ ] `git log` 有新提交
